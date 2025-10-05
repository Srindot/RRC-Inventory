#!/bin/bash

# Simple Stop Script for RRC Inventory
echo "🛑 Stopping RRC Inventory..."

# Check if docker-compose is available
if ! command -v docker-compose &> /dev/null; then
    echo "❌ docker-compose is not installed."
    exit 1
fi

# Stop the services
docker-compose down

# Check if services stopped successfully
if [ $? -eq 0 ]; then
    echo "✅ RRC Inventory stopped successfully!"
    echo ""
    echo "🚀 To start again: ./start.sh"
else
    echo "❌ Failed to stop RRC Inventory"
    exit 1
fi