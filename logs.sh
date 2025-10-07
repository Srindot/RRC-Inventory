#!/bin/bash

# Simple Logs Script for RRC Inventory
echo "📋 Showing RRC Inventory logs..."
echo "Press Ctrl+C to exit"
echo ""

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

# Show logs with follow
$DOCKER_COMPOSE_CMD logs -f