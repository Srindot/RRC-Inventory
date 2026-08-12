#!/bin/bash
# Restore the database and item photos from an archive made by ./backup.sh
#
#   ./restore.sh backups/rrc-backup-20260811-120000.tar.gz
#
# This REPLACES the current data. It asks for confirmation first.

set -euo pipefail
cd "$(dirname "$0")"

ARCHIVE="${1:-}"

if [ -z "$ARCHIVE" ]; then
    echo "Usage: ./restore.sh <backup-archive.tar.gz>"
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
BACKEND_CONTAINER="$($COMPOSE_CMD ps -q backend 2>/dev/null | head -1)"

if [ -z "$DB_CONTAINER" ]; then
    echo "❌ The database container is not running. Start the system first (./start.sh)."
    exit 1
fi

STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
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
read -r -p "Type 'restore' to continue: " CONFIRM

if [ "$CONFIRM" != "restore" ]; then
    echo "Cancelled. Nothing was changed."
    exit 1
fi

echo "📦 Restoring the database..."
$DOCKER_CMD exec -i "$DB_CONTAINER" psql -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" < "$STAGING/database.sql" > /dev/null

echo "🖼️  Restoring item photos..."
if [ -n "$BACKEND_CONTAINER" ]; then
    $DOCKER_CMD exec "$BACKEND_CONTAINER" sh -c 'rm -rf /app/uploads/* || true'
    if [ "$PHOTO_COUNT" != "0" ]; then
        $DOCKER_CMD cp "$STAGING/uploads/." "$BACKEND_CONTAINER:/app/uploads/"
    fi
fi

echo "🔄 Restarting the backend..."
$COMPOSE_CMD restart backend > /dev/null

echo ""
echo "✅ Restore complete - $PHOTO_COUNT photo(s) and the full database are back."
