#!/bin/bash

# Simple Logs Script for RRC Inventory
echo "📋 Showing RRC Inventory logs..."
echo "Press Ctrl+C to exit"
echo ""

# Check if docker-compose is available
if ! command -v docker-compose &> /dev/null; then
    echo "❌ docker-compose is not installed."
    exit 1
fi

# Show logs with follow
docker-compose logs -f