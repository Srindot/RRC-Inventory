#!/bin/bash

# RRC Inventory - Domain Setup Script
# This script helps set up the local domain name for easier access

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}  RRC Inventory - Domain Setup${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo ""
}

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

# Get current server IP
get_server_ip() {
    # Try to get the main network interface IP
    local ip=$(ip route get 8.8.8.8 | awk '{print $7; exit}' 2>/dev/null)
    if [ -z "$ip" ]; then
        # Fallback method
        ip=$(hostname -I | awk '{print $1}')
    fi
    echo "$ip"
}

print_header

print_status "Detecting server IP address..."
SERVER_IP=$(get_server_ip)

if [ -z "$SERVER_IP" ]; then
    print_error "Could not detect server IP address"
    exit 1
fi

print_success "Server IP detected: $SERVER_IP"
echo ""

print_status "Setting up local access instructions (mDNS removed)"
echo ""

echo -e "${YELLOW}To access the RRC Inventory system with a friendly domain name:${NC}"
echo ""
echo "1. ${BLUE}For the current server (${SERVER_IP}):${NC}"
echo "   Add this line to your hosts file (example):"
echo -e "   ${GREEN}$SERVER_IP    my-rrc-server${NC}"
echo ""

echo "2. ${BLUE}Location of hosts file:${NC}"
echo "   • Linux/Mac: /etc/hosts"
echo "   • Windows: C:\\Windows\\System32\\drivers\\etc\\hosts"
echo ""

echo "3. ${BLUE}To edit hosts file:${NC}"
echo "   • Linux/Mac: sudo nano /etc/hosts"
echo "   • Windows: Run notepad as administrator, then open the hosts file"
echo ""

echo "4. ${BLUE}Add this line to the hosts file (example):${NC}"
echo -e "   ${GREEN}$SERVER_IP    my-rrc-server${NC}"
echo ""

echo "5. ${BLUE}After editing, save the file and restart your browser${NC}"
echo ""

print_success "Once configured, you can access the system at:"
echo -e "   ${GREEN}http://$SERVER_IP${NC}"
echo ""

print_warning "Note: This setup needs to be done on each device that wants to access the system"
print_warning "When the server IP changes, update the hosts file with the new IP"

echo ""
echo -e "${BLUE}================================================${NC}"
echo -e "${YELLOW}Quick Commands:${NC}"
echo ""
echo "Linux/Mac users can run (example):"
echo -e "${GREEN}echo '$SERVER_IP    my-rrc-server' | sudo tee -a /etc/hosts${NC}"
echo ""
echo "To remove the entry later:"
echo -e "${GREEN}sudo sed -i '/my-rrc-server/d' /etc/hosts${NC}"
echo -e "${BLUE}================================================${NC}"