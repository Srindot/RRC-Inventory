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

# shellcheck disable=SC1091
set -a; source .env; set +a

POSTGRES_USER="${POSTGRES_USER:-user}"
POSTGRES_DB="${POSTGRES_DB:-mydatabase}"

if ! $COMPOSE_CMD ps --status running 2>/dev/null | grep -q db; then
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
$COMPOSE_CMD exec -T db pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists > "$STAGING/database.sql"

echo "🖼️  Backing up item photos..."
mkdir -p "$STAGING/uploads"
# Copying out of the container works whether or not any photos exist yet
$COMPOSE_CMD cp backend:/app/uploads/. "$STAGING/uploads/" 2>/dev/null || true

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
