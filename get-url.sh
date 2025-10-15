#!/bin/bash

# Quick Access URL Display Script

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🌐 RRC Inventory - Access Information"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Get WSL IP
WSL_IP=$(ip -4 addr show eth0 2>/dev/null | awk '/inet / {print $2}' | cut -d/ -f1)

if [[ -n "$WSL_IP" ]]; then
    echo "📱 Share this URL with users:"
    echo ""
    echo "   ┌─────────────────────────────────────────┐"
    echo "   │  http://$WSL_IP         │"
    echo "   └─────────────────────────────────────────┘"
    echo ""
else
    echo "❌ Could not detect IP address"
    exit 1
fi

# Check if running in WSL
if grep -qi microsoft /proc/version; then
    echo "⚠️  Note: Running in WSL2"
    echo ""
    echo "   mDNS (rrc-inventory.local) won't work from other devices"
    echo "   due to WSL2's virtual networking."
    echo ""
    echo "📖 For alternatives, see: MDNS_WSL2_TROUBLESHOOTING.md"
    echo ""
    echo "💡 Quick Solutions:"
    echo "   1. Share the IP above (easiest)"
    echo "   2. Setup Windows port forwarding (recommended)"
    echo "   3. Configure router DNS (best for production)"
fi

# Check if containers are running
if docker ps 2>/dev/null | grep -q "rrc-inventory"; then
    echo ""
    echo "✅ Services: Running"
else
    echo ""
    echo "⚠️  Services: Not running (run ./start.sh)"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
