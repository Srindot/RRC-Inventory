#!/bin/bash
# Create a single portable archive containing the database and every item photo.
# The archive is self-contained - copy it anywhere and restore.sh will rebuild
# the system from it.
#
#   ./backup.sh                 interactive, keeps every archive
#   ./backup.sh --cron          for cron: no output unless something fails,
#                               keeps the newest 14 archives and deletes older ones
#   ./backup.sh --cron --keep 30
#   ./backup.sh --quiet --print-path --prefix rrc-pre-restore
#                               (used by restore.sh for its safety copy; prints
#                               only the archive path)
#
# Any failure exits non-zero with a message on stderr, so cron logs and mails it.

set -euo pipefail
cd "$(dirname "$0")"

# Archives hold every borrower's name and phone number - owner-only.
umask 077

QUIET=false
PRINT_PATH=false
KEEP=""
PREFIX="rrc-backup"
while [ $# -gt 0 ]; do
    case "$1" in
        --cron)   QUIET=true; KEEP="${KEEP:-14}" ;;
        --quiet)  QUIET=true ;;
        --print-path) PRINT_PATH=true ;;
        --keep)   KEEP="${2:-}"; shift ;;
        --prefix) PREFIX="${2:-}"; shift ;;
        *)
            echo "Usage: ./backup.sh [--cron] [--quiet] [--print-path] [--keep N] [--prefix NAME]" >&2
            exit 1
            ;;
    esac
    shift
done

if [ -n "$KEEP" ] && ! [[ "$KEEP" =~ ^[1-9][0-9]*$ ]]; then
    echo "❌ --keep needs a positive number, got '$KEEP'." >&2
    exit 1
fi
if ! [[ "$PREFIX" =~ ^[A-Za-z0-9._-]+$ ]]; then
    echo "❌ --prefix may only contain letters, digits, '.', '_' and '-'." >&2
    exit 1
fi

say() {
    if [ "$QUIET" = false ]; then
        echo "$@"
    fi
}

die() {
    echo "❌ $*" >&2
    exit 1
}

if docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
elif docker-compose --version &> /dev/null; then
    COMPOSE_CMD="docker-compose"
else
    die "docker compose is not installed."
fi

DOCKER_CMD="docker"
if ! docker ps &> /dev/null; then
    # sudo would sit waiting for a password that never comes under cron.
    if [ ! -t 0 ]; then
        die "Cannot reach Docker as $(id -un). Add the user to the docker group (sudo usermod -aG docker $(id -un))."
    fi
    COMPOSE_CMD="sudo $COMPOSE_CMD"
    DOCKER_CMD="sudo docker"
fi

if [ ! -f .env ]; then
    die "No .env file found. It holds the database credentials."
fi

# Read just the values we need. Do NOT source .env - values like PRINTERS
# contain spaces and pipe characters, which the shell would try to execute.
read_env_value() {
    grep -E "^[[:space:]]*$1=" .env 2>/dev/null | tail -1 |
        cut -d= -f2- |
        sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
            -e "s/^'\(.*\)'$/\1/" -e 's/^"\(.*\)"$/\1/'
}

POSTGRES_USER="$(read_env_value POSTGRES_USER)"
POSTGRES_DB="$(read_env_value POSTGRES_DB)"
POSTGRES_USER="${POSTGRES_USER:-user}"
POSTGRES_DB="${POSTGRES_DB:-mydatabase}"

# "ps -q <service>" works on both compose v1 and v2, unlike "ps --status"
DB_CONTAINER="$($COMPOSE_CMD ps -q db 2>/dev/null | head -1)"
BACKEND_CONTAINER="$($COMPOSE_CMD ps -q backend 2>/dev/null | head -1)"

if [ -z "$DB_CONTAINER" ]; then
    die "The database container is not running. Start the system first (./start.sh)."
fi

# Without the backend there is no way to reach the photos. Failing beats
# writing an archive that silently says "photos: 0" and restoring it later
# wipes every photo.
if [ -z "$BACKEND_CONTAINER" ]; then
    die "The backend container is not running, so the photos cannot be copied. Start the system first (./start.sh)."
fi

BACKUP_DIR="backups"
STAMP="$(date +%Y%m%d-%H%M%S)"
ARCHIVE="$BACKUP_DIR/$PREFIX-$STAMP.tar.gz"
# Two runs in the same second (restore.sh's safety copy right after a manual
# backup) must not overwrite each other.
if [ -e "$ARCHIVE" ]; then
    ARCHIVE="$BACKUP_DIR/$PREFIX-$STAMP-$$.tar.gz"
fi
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT

mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

say "📦 Backing up the database..."
if ! $DOCKER_CMD exec -i "$DB_CONTAINER" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists > "$STAGING/database.sql"; then
    die "pg_dump failed - check the credentials in .env and ./logs.sh db."
fi

if [ ! -s "$STAGING/database.sql" ]; then
    die "The database dump came out empty - check the credentials in .env."
fi

say "🖼️  Backing up item photos..."
mkdir -p "$STAGING/uploads"
# "docker cp" rather than "compose cp", which only exists in compose v2
if ! $DOCKER_CMD cp "$BACKEND_CONTAINER:/app/uploads/." "$STAGING/uploads/"; then
    die "Copying the photos out of the backend container failed. No archive was written."
fi

PHOTO_COUNT="$(find "$STAGING/uploads" -type f | wc -l | tr -d ' ')"

echo "$STAMP" > "$STAGING/BACKUP_INFO"
echo "photos: $PHOTO_COUNT" >> "$STAGING/BACKUP_INFO"

# Write to a temporary name first, so a full disk never leaves a truncated
# archive that looks like a good backup (or pushes a good one out of --keep).
if ! tar -czf "$ARCHIVE.partial" -C "$STAGING" .; then
    rm -f "$ARCHIVE.partial"
    die "Writing the archive failed (disk full?)."
fi
mv "$ARCHIVE.partial" "$ARCHIVE"
chmod 600 "$ARCHIVE"

# Keep the newest $KEEP archives with this prefix. Names sort by date, so a
# plain sort is chronological; safety copies from restore.sh use another
# prefix and are never pruned here.
PRUNED=0
if [ -n "$KEEP" ]; then
    while IFS= read -r OLD; do
        rm -f -- "$OLD"
        PRUNED=$((PRUNED + 1))
    done < <(find "$BACKUP_DIR" -maxdepth 1 -type f -name "$PREFIX-*.tar.gz" | sort -r | tail -n +"$((KEEP + 1))")
fi

if [ "$PRINT_PATH" = true ]; then
    echo "$ARCHIVE"
fi
if [ "$QUIET" = true ]; then
    exit 0
fi

echo ""
echo "✅ Backup complete"
echo "   File:   $ARCHIVE"
echo "   Size:   $(du -h "$ARCHIVE" | cut -f1)"
echo "   Photos: $PHOTO_COUNT"
if [ -n "$KEEP" ]; then
    echo "   Pruned: $PRUNED older archive(s), keeping the newest $KEEP"
fi
echo ""
echo "   Copy this single file anywhere to move or archive the system."
echo "   Restore it with: ./restore.sh $ARCHIVE"
