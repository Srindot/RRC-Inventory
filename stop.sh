#!/bin/bash

# Simple Stop Script for RRC Inventory
echo "🛑 Stopping RRC Inventory..."

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