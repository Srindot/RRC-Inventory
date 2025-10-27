#!/bin/bash

# Simple Stop Script for RRC Inventory
echo "🛑 Stopping RRC Inventory..."

# Check if docker-compose is available (support both v1 and v2)
if docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
elif docker-compose --version &> /dev/null; then
    COMPOSE_CMD="docker-compose"
else
    echo "❌ docker compose is not installed."
    exit 1
fi

# Check Docker permissions and set command accordingly
if ! docker ps &> /dev/null; then
    if groups $USER | grep -q docker; then
        echo "⚠️  Using sudo for Docker commands (session needs refresh)"
        if [[ "$COMPOSE_CMD" == "docker compose" ]]; then
            DOCKER_COMPOSE_CMD="sudo docker compose"
        else
            DOCKER_COMPOSE_CMD="sudo docker-compose"
        fi
    else
        echo "❌ Docker permission error. Please run: sudo usermod -aG docker \$USER"
        exit 1
    fi
else
    DOCKER_COMPOSE_CMD="$COMPOSE_CMD"
fi

# Stop the services
$DOCKER_COMPOSE_CMD down

# Check if services stopped successfully
if [ $? -eq 0 ]; then
    echo "✅ RRC Inventory stopped successfully!"
    echo ""
    echo "🚀 To start again: ./start.sh"
else
    echo "❌ Failed to stop RRC Inventory"
    exit 1
fi
# Disable rrc-inventory.service autostart (user and system)
if systemctl --user --quiet is-active rrc-inventory.service 2>/dev/null; then
    echo "Disabling user rrc-inventory.service autostart..."
    systemctl --user disable rrc-inventory.service
fi
if systemctl --quiet is-active rrc-inventory.service 2>/dev/null; then
    echo "Disabling system rrc-inventory.service autostart..."
    sudo systemctl disable rrc-inventory.service
fi
echo "rrc-inventory.service autostart disabled (if it was enabled)."