#!/bin/bash

# Status Script for RRC Inventory Management System
echo "📊 RRC Inventory System Status"
echo "================================"

# Check if docker-compose is available
if ! docker compose version &> /dev/null; then
    echo "❌ docker compose is not installed."
    exit 1
fi

# Check Docker permissions and set command accordingly
if ! docker ps &> /dev/null; then
    if groups $USER | grep -q docker; then
        echo "⚠️  Using sudo for Docker commands (session needs refresh)"
        DOCKER_COMPOSE_CMD="sudo docker compose"
    else
        echo "❌ Docker permission error. Please run: sudo usermod -aG docker \$USER"
        exit 1
    fi
else
    DOCKER_COMPOSE_CMD="docker compose"
fi

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

echo ""
echo "🔧 Management Commands:"
echo "   ./start.sh    - Start all services"
echo "   ./stop.sh     - Stop all services"
echo "   ./logs.sh     - View system logs"
echo "   ./status.sh   - Show this status"