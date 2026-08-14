#!/bin/bash

# Start Script for RRC Inventory Management System
#
# Pass --build after pulling new code. Without it Docker reuses the images it
# already has, so a plain start after a git pull silently runs the old binary.

source "$(dirname "$0")/compose-cmd.sh"

REBUILD=false
if [ "$1" = "--build" ] || [ "$1" = "-b" ]; then
    REBUILD=true
fi

echo "🚀 Starting RRC Inventory..."
echo "📋 This will start all services in the background"

if ! compose_recreate "$REBUILD"; then
    echo "❌ Failed to start RRC Inventory"
    exit 1
fi

echo ""
echo "✅ RRC Inventory started successfully!"
echo ""
echo "🌐 Access your application at:"
echo "   🖥️  Local:        http://localhost"

echo "   📱 Network (IP):   http://$(hostname -I | awk '{print $1}')"
echo "   🔌 Backend API:    http://localhost/api"
echo ""
echo "📊 To view logs: ./logs.sh"
echo "🔄 To restart:   ./restart.sh"
echo "🛑 To stop: ./stop.sh"
echo ""
echo "🔐 Super Admin Login:"
echo "   Credentials come from .env (ADMIN_USERNAME / ADMIN_PASSWORD)"
echo "   If ADMIN_PASSWORD was empty, the generated password is in ./logs.sh"

if [ "$REBUILD" = false ]; then
    echo ""
    echo "ℹ️  Started from the existing images. After a git pull run: ./start.sh --build"
fi
