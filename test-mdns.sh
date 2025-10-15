#!/bin/bash

# Quick test script for mDNS configuration

echo "🧪 Testing mDNS Configuration for RRC Inventory"
echo "================================================"
echo ""

# Check if Avahi is installed
echo "1. Checking Avahi installation..."
if command -v avahi-daemon &> /dev/null; then
    echo "   ✅ Avahi is installed"
else
    echo "   ❌ Avahi not found. Run: sudo ./scripts/mdns_setup.sh"
    exit 1
fi

# Check if Avahi daemon is running
echo ""
echo "2. Checking Avahi daemon status..."
if systemctl is-active --quiet avahi-daemon; then
    echo "   ✅ Avahi daemon is running"
else
    echo "   ❌ Avahi daemon not running. Run: sudo systemctl start avahi-daemon"
    exit 1
    echo "mDNS testing utilities have been removed from this repository."
    echo "Use the server IP address to access the application or run custom network diagnostics."
# Check /etc/avahi/hosts
