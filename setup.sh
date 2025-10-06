#!/bin/bash

# RRC Inventory Management System - Setup Script
# This script builds and sets up the Docker environment

set -e  # Exit on any error

echo "🚀 Setting up RRC Inventory Management System..."
echo "================================================"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Docker is installed
print_status "Checking Docker installation..."
if ! command -v docker &> /dev/null; then
    print_error "Docker is not installed. Please install Docker first."
    exit 1
fi

# Check if Docker Compose is installed
print_status "Checking Docker Compose installation..."
if ! command -v docker-compose &> /dev/null; then
    print_error "Docker Compose is not installed. Please install Docker Compose first."
    exit 1
fi

print_success "Docker and Docker Compose are installed."

# Stop any running containers
print_status "Stopping any existing containers..."
docker-compose down 2>/dev/null || true

# Clean up any existing images (optional)
print_status "Cleaning up old Docker images..."
docker system prune -f --volumes 2>/dev/null || true

# Build all services
print_status "Building Docker images..."
print_status "This may take a few minutes on first run..."

if docker-compose build --no-cache; then
    print_success "Docker images built successfully!"
else
    print_error "Failed to build Docker images."
    exit 1
fi

# Create necessary directories
print_status "Creating necessary directories..."
mkdir -p backend/uploads 2>/dev/null || true

# Set proper permissions
print_status "Setting up permissions..."
chmod +x start.sh 2>/dev/null || true
chmod +x stop.sh 2>/dev/null || true
chmod +x logs.sh 2>/dev/null || true

print_success "Setup completed successfully!"
echo ""
echo "================================================"
echo -e "${GREEN}🎉 RRC Inventory System is ready!${NC}"
echo ""
echo "Next steps:"
echo -e "  1. Run ${BLUE}./start.sh${NC} to start the system"
echo -e "  2. Visit ${BLUE}http://localhost${NC} in your browser"
echo -e "  3. Use admin login - Username: ${BLUE}Srinath${NC}, Password: ${BLUE}rrc@srinath${NC}"
echo ""
echo -e "${YELLOW}📡 Network Note:${NC} Make sure you're connected to wifi@iiith or using OpenVPN"
echo "================================================"