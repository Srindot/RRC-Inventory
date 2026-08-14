#!/bin/bash

# Status Script for RRC Inventory Management System

source "$(dirname "$0")/compose-cmd.sh"

echo "📊 RRC Inventory System Status"
echo "================================"

# Check service status
echo "🐳 Docker Containers:"
$DOCKER_COMPOSE_CMD ps

echo ""
echo "🌐 Network Status:"
if curl -s -o /dev/null -w "%{http_code}" http://localhost | grep -q "200"; then
    echo "   ✅ Web interface: http://localhost (accessible)"
else
    echo "   ❌ Web interface: http://localhost (not accessible)"
fi

if curl -s -o /dev/null -w "%{http_code}" http://localhost/api/items | grep -q "200"; then
    echo "   ✅ Backend API: http://localhost/api (accessible)"
else
    echo "   ❌ Backend API: http://localhost/api (not accessible)"
fi

# mDNS support removed from this distribution. Access using server IP instead.
echo "   ℹ️  mDNS support removed. Use the server IP (http://<server-ip>) to access the app."

echo ""
echo "🔄 Auto-start Status:"
if systemctl is-enabled rrc-inventory.service &>/dev/null; then
    if systemctl is-active rrc-inventory.service &>/dev/null; then
        echo "   ✅ Auto-start: Enabled and active"
    else
        echo "   ⚠️  Auto-start: Enabled but not active"
    fi
else
    echo "   ❌ Auto-start: Not enabled"
fi

# mDNS auto-update removed
echo "   ℹ️  mDNS auto-update service removed from instructions"

echo ""
echo "🔧 Management Commands:"
echo "   ./start.sh    - Start all services (--build after a git pull)"
echo "   ./restart.sh  - Restart all services (--build after a git pull)"
echo "   ./stop.sh     - Stop all services and disable autostart"
echo "   ./logs.sh     - View system logs"
echo "   ./status.sh   - Show this status"