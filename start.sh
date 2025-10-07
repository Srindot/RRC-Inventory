#!/bin/bash

# Start Script for RRC Inventory Management System
echo "🚀 Starting RRC Inventory..."
echo "📋 This will start all services in the background"

# Check if docker-compose is available
if ! docker compose version &> /dev/null; then
    echo "❌ docker compose is not installed. Please install it first."
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

# Start the services
$DOCKER_COMPOSE_CMD up -d

# Check if services started successfully
if [ $? -eq 0 ]; then
    echo ""
    echo "✅ RRC Inventory started successfully!"
    echo ""
    echo "🌐 Access your application at:"
    echo "   🖥️  Local:        http://localhost"
    echo "   📱 Remote/Mobile: http://$(hostname -I | awk '{print $1}')"
    echo "   🔌 Backend API:   http://localhost/api"
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