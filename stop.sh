#!/bin/bash

# Simple Stop Script for RRC Inventory
#
# Stopping means "keep it down", so the boot autostart is disabled too. Use
# ./restart.sh if you only want to bounce the services.

source "$(dirname "$0")/compose-cmd.sh"

echo "🛑 Stopping RRC Inventory..."

# Stop the services
if ! $DOCKER_COMPOSE_CMD down; then
    echo "❌ Failed to stop RRC Inventory"
    exit 1
fi

echo "✅ RRC Inventory stopped successfully!"
echo ""
echo "🚀 To start again: ./start.sh"

# Disable rrc-inventory.service autostart (user and system).
#
# Checked with is-enabled, not is-active: disable changes whether the service
# starts at boot, and a service that is enabled but currently stopped still
# needs disabling. The old is-active check missed exactly that case.
if systemctl --user --quiet is-enabled rrc-inventory.service 2>/dev/null; then
    echo "Disabling user rrc-inventory.service autostart..."
    systemctl --user disable rrc-inventory.service
fi
if systemctl --quiet is-enabled rrc-inventory.service 2>/dev/null; then
    echo "Disabling system rrc-inventory.service autostart..."
    sudo systemctl disable rrc-inventory.service
fi
echo "rrc-inventory.service autostart disabled (if it was enabled)."
