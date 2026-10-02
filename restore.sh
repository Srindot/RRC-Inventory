#!/bin/bash
# Restore the database and item photos from an archive made by ./backup.sh
#
#   ./restore.sh backups/rrc-backup-20260811-120000.tar.gz
#
# This REPLACES the current data. It asks for confirmation first, then saves a
# safety copy of the current data (backups/rrc-pre-restore-*.tar.gz) before it
# touches anything. Pass --skip-safety-backup only when the current data is
# already lost and the safety copy cannot be made.

set -euo pipefail
cd "$(dirname "$0")"
umask 077

ARCHIVE=""
SAFETY_BACKUP=true
for ARG in "$@"; do
    case "$ARG" in
        --skip-safety-backup) SAFETY_BACKUP=false ;;
        *) ARCHIVE="$ARG" ;;
    esac
done

if [ -z "$ARCHIVE" ]; then
    echo "Usage: ./restore.sh [--skip-safety-backup] <backup-archive.tar.gz>"
    echo ""
    echo "Available backups:"
    ls -1t backups/*.tar.gz 2>/dev/null || echo "  (none found in ./backups)"
    exit 1
fi

if [ ! -f "$ARCHIVE" ]; then
    echo "❌ No such file: $ARCHIVE"
    exit 1
fi

if docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
elif docker-compose --version &> /dev/null; then
    COMPOSE_CMD="docker-compose"
else
    echo "❌ docker compose is not installed."
    exit 1
fi

if ! docker ps &> /dev/null; then
    COMPOSE_CMD="sudo $COMPOSE_CMD"
fi

if [ ! -f .env ]; then
    echo "❌ No .env file found. It holds the database credentials."
    exit 1
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

DOCKER_CMD="docker"
if ! docker ps &> /dev/null; then
    DOCKER_CMD="sudo docker"
fi

DB_CONTAINER="$($COMPOSE_CMD ps -q db 2>/dev/null | head -1)"
# -a: also find the backend if it is stopped. The photos are copied into it
# while it is down, and "docker cp" works on a stopped container.
BACKEND_CONTAINER="$($COMPOSE_CMD ps -a -q backend 2>/dev/null | head -1)"

if [ -z "$DB_CONTAINER" ]; then
    echo "❌ The database container is not running. Start the system first (./start.sh)."
    exit 1
fi

if [ -z "$BACKEND_CONTAINER" ]; then
    echo "❌ There is no backend container. Start the system first (./start.sh)."
    exit 1
fi

STAGING="$(mktemp -d)"
BACKEND_STOPPED=false
cleanup() {
    rm -rf "$STAGING"
    # Never leave the site down because a step below failed.
    if [ "$BACKEND_STOPPED" = true ]; then
        echo "🔄 Starting the backend again..."
        $COMPOSE_CMD start backend > /dev/null || echo "❌ Could not start the backend - run ./start.sh"
    fi
}
trap cleanup EXIT

tar -xzf "$ARCHIVE" -C "$STAGING"

if [ ! -f "$STAGING/database.sql" ]; then
    echo "❌ That archive does not look like an RRC backup (no database.sql inside)."
    exit 1
fi

PHOTO_COUNT="$(find "$STAGING/uploads" -type f 2>/dev/null | wc -l | tr -d ' ')"

echo "About to restore:"
echo "   Archive: $ARCHIVE"
[ -f "$STAGING/BACKUP_INFO" ] && sed 's/^/   /' "$STAGING/BACKUP_INFO"
echo "   Photos:  $PHOTO_COUNT"
echo ""
echo "⚠️  This REPLACES all current loans, bookings, admins and photos."
if [ "$PHOTO_COUNT" = "0" ]; then
    echo "⚠️  The archive has no photos, so the current photos will be KEPT as they are."
    echo "   (Older backups could silently miss them; wiping on that basis would lose them.)"
fi
read -r -p "Type 'restore' to continue: " CONFIRM

if [ "$CONFIRM" != "restore" ]; then
    echo "Cancelled. Nothing was changed."
    exit 1
fi

if [ "$SAFETY_BACKUP" = true ]; then
    echo "🛟 Saving a safety copy of the current data first..."
    if ! SAFETY_ARCHIVE="$(./backup.sh --quiet --print-path --prefix rrc-pre-restore)"; then
        echo "❌ The safety backup failed, so nothing was changed."
        echo "   Fix the error above, or rerun with --skip-safety-backup if the current data is not worth keeping."
        exit 1
    fi
    echo "   Saved to $SAFETY_ARCHIVE"
fi

# The backend holds database connections and writes photos; stop it so the
# restore cannot race a request or be blocked by its locks.
echo "⏸️  Stopping the backend..."
$COMPOSE_CMD stop backend > /dev/null
BACKEND_STOPPED=true

echo "📦 Restoring the database..."
# One transaction, stop at the first error: a bad dump rolls back and leaves
# the current data untouched instead of half-replaced.
if ! $DOCKER_CMD exec -i "$DB_CONTAINER" psql -q -v ON_ERROR_STOP=1 --single-transaction \
        -U "$POSTGRES_USER" -d "$POSTGRES_DB" < "$STAGING/database.sql" > /dev/null; then
    echo "❌ The database restore failed and was rolled back. The current data is unchanged."
    exit 1
fi

if [ "$PHOTO_COUNT" = "0" ]; then
    echo "🖼️  No photos in the archive - leaving the current photos in place."
else
    echo "🖼️  Restoring item photos..."
    # A one-off container on the same volume, since the backend is stopped.
    $COMPOSE_CMD run --rm --no-deps -T --entrypoint sh backend \
        -c 'find /app/uploads -mindepth 1 -delete' > /dev/null
    $DOCKER_CMD cp "$STAGING/uploads/." "$BACKEND_CONTAINER:/app/uploads/"
fi

echo "🔄 Starting the backend..."
$COMPOSE_CMD start backend > /dev/null
BACKEND_STOPPED=false

echo ""
if [ "$PHOTO_COUNT" = "0" ]; then
    echo "✅ Restore complete - the full database is back (photos left unchanged)."
else
    echo "✅ Restore complete - $PHOTO_COUNT photo(s) and the full database are back."
fi
if [ "$SAFETY_BACKUP" = true ]; then
    echo "   The data from before the restore is in $SAFETY_ARCHIVE"
fi
