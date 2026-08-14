#!/bin/bash

# Restart Script for RRC Inventory Management System
#
# Pass --build to pick up new code after a git pull.
#
# This deliberately does not call stop.sh: stopping is treated as "take the
# service down and keep it down", so it disables the boot autostart. A restart
# should leave that setting exactly as it found it.

source "$(dirname "$0")/compose-cmd.sh"

REBUILD=false
if [ "$1" = "--build" ] || [ "$1" = "-b" ]; then
    REBUILD=true
fi

echo "🔄 Restarting RRC Inventory..."

$DOCKER_COMPOSE_CMD down

if ! compose_recreate "$REBUILD"; then
    echo "❌ Failed to restart RRC Inventory"
    exit 1
fi

echo ""
echo "✅ RRC Inventory restarted successfully!"
echo ""
echo "   🖥️  Local:        http://localhost"
echo "   📱 Network (IP):   http://$(hostname -I | awk '{print $1}')"
echo ""
echo "📊 To view logs: ./logs.sh"
echo "📈 To check status: ./status.sh"

if [ "$REBUILD" = false ]; then
    echo ""
    echo "ℹ️  Restarted from the existing images. After a git pull run: ./restart.sh --build"
fi
