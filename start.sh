#!/bin/bash

# Simple Start Script for RRC Inventory
echo "🚀 Starting RRC Inventory..."
echo "📋 This will start all services in the background"

# Check if docker-compose is available
if ! command -v docker-compose &> /dev/null; then
    echo "❌ docker-compose is not installed. Please install it first."
    exit 1
fi

# Start the services
docker-compose up -d

# Check if services started successfully
if [ $? -eq 0 ]; then
    echo ""
    echo "✅ RRC Inventory started successfully!"
    echo ""
    echo "🌐 Access your application at:"
    echo "   Frontend: http://localhost"
    echo "   Backend API: http://localhost/api"
    echo ""
    echo "📊 To view logs: ./logs.sh"
    echo "🛑 To stop: ./stop.sh"
    echo ""
    echo "🔐 Super Admin Login:"
    echo "   Username: Srinath"
    echo "   Password: rrc@srinath"
else
    echo "❌ Failed to start RRC Inventory"
    exit 1
fi