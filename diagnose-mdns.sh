#!/bin/bash

echo "🔍 RRC Inventory - mDNS Diagnostic Script"
echo "=========================================="
echo ""

echo "1. Checking if Avahi daemon is running..."
if systemctl is-active --quiet avahi-daemon; then
    echo "   ✅ Avahi daemon is running"
else
    echo "   ❌ Avahi daemon is NOT running"
    echo "   Fix: sudo systemctl start avahi-daemon"
fi
echo ""

echo "2. Checking mDNS hostname mapping..."
if grep -q "rrc-inventory.local" /etc/avahi/hosts 2>/dev/null; then
    echo "   ✅ Hostname mapping exists:"
    grep "rrc-inventory.local" /etc/avahi/hosts
else
    echo "   ❌ No hostname mapping found"
    echo "   Fix: Run sudo ./scripts/mdns_setup.sh"
fi
echo ""

echo "3. Testing local hostname resolution..."
if avahi-resolve -n rrc-inventory.local >/dev/null 2>&1; then
    echo "   ✅ Hostname resolves via Avahi:"
    avahi-resolve -n rrc-inventory.local
else
    echo "   ⚠️  Avahi resolution timeout (this is normal in WSL2)"
    echo "   Note: /etc/avahi/hosts doesn't broadcast to network"
fi
echo ""

echo "4. Testing with ping..."
if ping -c 1 rrc-inventory.local >/dev/null 2>&1; then
    echo "   ✅ Hostname resolves via ping"
else
    echo "   ⚠️  Ping failed (expected in WSL2)"
fi
echo ""

echo "5. Checking network interfaces..."
ip -4 addr show | grep -E "inet " | grep -v "127.0.0.1"
echo ""

echo "6. Checking if services are published..."
echo "   Looking for RRC Inventory services..."
avahi-browse -a -t 2>/dev/null | grep -i "rrc\|inventory" | head -5 || echo "   ⚠️  No services found"
echo ""

echo "7. Current server IP:"
SERVER_IP=$(ip -4 addr show eth0 2>/dev/null | awk '/inet / {print $2}' | cut -d/ -f1)
if [[ -n "$SERVER_IP" ]]; then
    echo "   📱 $SERVER_IP"
    echo ""
    echo "🌐 Access URLs:"
    echo "   From this server:  http://localhost"
    echo "   From other devices: http://$SERVER_IP"
    echo "   mDNS (if working):  http://rrc-inventory.local"
else
    echo "   ⚠️  Could not detect IP"
fi
echo ""

echo "=========================================="
echo "📝 Note: Run this script ON THE UBUNTU SERVER where the site is hosted"
echo "   mDNS won't work from remote machines testing other servers"
