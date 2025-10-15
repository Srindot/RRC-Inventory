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
fi

# Check /etc/avahi/hosts
echo ""
echo "3. Checking mDNS hostname mapping..."
if grep -q "rrc-inventory.local" /etc/avahi/hosts 2>/dev/null; then
    echo "   ✅ Hostname mapping found:"
    grep "rrc-inventory.local" /etc/avahi/hosts | sed 's/^/      /'
else
    echo "   ❌ No mapping found. Run: sudo ./scripts/mdns_setup.sh"
    exit 1
fi

# Check if systemd service is enabled
echo ""
echo "4. Checking auto-start service..."
if systemctl is-enabled --quiet rrc-inventory-mdns.service 2>/dev/null; then
    echo "   ✅ Auto-start service is enabled"
else
    echo "   ⚠️  Auto-start service not enabled (optional)"
    echo "      To enable: sudo systemctl enable rrc-inventory-mdns.service"
fi

# Try to resolve the hostname
echo ""
echo "5. Testing hostname resolution..."
if command -v avahi-resolve &> /dev/null; then
    if avahi-resolve -n rrc-inventory.local 2>/dev/null | grep -q "rrc-inventory.local"; then
        echo "   ✅ Hostname resolves successfully:"
        avahi-resolve -n rrc-inventory.local | sed 's/^/      /'
    else
        echo "   ⚠️  Resolution test inconclusive"
    fi
else
    echo "   ⚠️  avahi-resolve not available for testing"
fi

# Check if Docker containers are running
echo ""
echo "6. Checking RRC Inventory containers..."
if docker compose ps 2>/dev/null | grep -q "Up"; then
    echo "   ✅ Containers are running"
else
    echo "   ⚠️  Containers not running. Start with: ./start.sh"
fi

echo ""
echo "================================================"
echo "🎉 mDNS configuration looks good!"
echo ""
echo "Access your system at: http://rrc-inventory.local"
echo ""
