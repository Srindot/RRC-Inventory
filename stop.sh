#!/bin/bash

# Simple Stop Script for RRC Inventory
#
# Stopping means "keep it down", so the boot autostart is disabled too. Use
# ./restart.sh if you only want to bounce the services.
#
#   ./stop.sh --no-disable   bring the containers down, leave autostart alone
#
# The systemd unit's ExecStop uses --no-disable. Without it every shutdown
# disabled autostart, so the system stayed off after the next boot. Running
# under systemd (INVOCATION_ID is set) is treated the same way, which also
# covers units installed before the flag existed.

source "$(dirname "$0")/compose-cmd.sh"

DISABLE_AUTOSTART=true
if [ "${1:-}" = "--no-disable" ] || [ -n "${INVOCATION_ID:-}" ]; then
    DISABLE_AUTOSTART=false
fi

echo "🛑 Stopping RRC Inventory..."

# Stop the services
if ! $DOCKER_COMPOSE_CMD down; then
    echo "❌ Failed to stop RRC Inventory"
    exit 1
fi

echo "✅ RRC Inventory stopped successfully!"

if [ "$DISABLE_AUTOSTART" = false ]; then
    exit 0
fi

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
