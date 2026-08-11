#!/bin/bash
# Create a single portable archive containing the database and every item photo.
# The archive is self-contained - copy it anywhere and restore.sh will rebuild
# the system from it.

set -euo pipefail
cd "$(dirname "$0")"

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

# "ps -q <service>" works on both compose v1 and v2, unlike "ps --status"
DOCKER_CMD="docker"
if ! docker ps &> /dev/null; then
    DOCKER_CMD="sudo docker"
fi

DB_CONTAINER="$($COMPOSE_CMD ps -q db 2>/dev/null | head -1)"
BACKEND_CONTAINER="$($COMPOSE_CMD ps -q backend 2>/dev/null | head -1)"

if [ -z "$DB_CONTAINER" ]; then
    echo "❌ The database container is not running. Start the system first (./start.sh)."
    exit 1
fi

BACKUP_DIR="backups"
STAMP="$(date +%Y%m%d-%H%M%S)"
ARCHIVE="$BACKUP_DIR/rrc-backup-$STAMP.tar.gz"
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT

mkdir -p "$BACKUP_DIR"

echo "📦 Backing up the database..."
$DOCKER_CMD exec -i "$DB_CONTAINER" pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists > "$STAGING/database.sql"

if [ ! -s "$STAGING/database.sql" ]; then
    echo "❌ The database dump came out empty - check the credentials in .env."
    exit 1
fi

echo "🖼️  Backing up item photos..."
mkdir -p "$STAGING/uploads"
# "docker cp" rather than "compose cp", which only exists in compose v2
if [ -n "$BACKEND_CONTAINER" ]; then
    $DOCKER_CMD cp "$BACKEND_CONTAINER:/app/uploads/." "$STAGING/uploads/" 2>/dev/null || true
fi

PHOTO_COUNT="$(find "$STAGING/uploads" -type f | wc -l | tr -d ' ')"

echo "$STAMP" > "$STAGING/BACKUP_INFO"
echo "photos: $PHOTO_COUNT" >> "$STAGING/BACKUP_INFO"

tar -czf "$ARCHIVE" -C "$STAGING" .

echo ""
echo "✅ Backup complete"
echo "   File:   $ARCHIVE"
echo "   Size:   $(du -h "$ARCHIVE" | cut -f1)"
echo "   Photos: $PHOTO_COUNT"
echo ""
echo "   Copy this single file anywhere to move or archive the system."
echo "   Restore it with: ./restore.sh $ARCHIVE"
