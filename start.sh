#!/bin/bash

# Start Script for RRC Inventory Management System
echo "🚀 Starting RRC Inventory..."
echo "📋 This will start all services in the background"

# Check if docker-compose is available (support both v1 and v2)
if docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
elif docker-compose --version &> /dev/null; then
    COMPOSE_CMD="docker-compose"
else
    echo "❌ docker compose is not installed. Please install it first."
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

# Start the services
$DOCKER_COMPOSE_CMD up -d

# Check if services started successfully
if [ $? -eq 0 ]; then
    echo ""
    echo "✅ RRC Inventory started successfully!"
    echo ""
    echo "🌐 Access your application at:"
    echo "   🖥️  Local:        http://localhost"
    
    echo "   📱 Network (IP):   http://$(hostname -I | awk '{print $1}')"
    echo "   🔌 Backend API:    http://localhost/api"
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