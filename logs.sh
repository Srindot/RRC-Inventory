#!/bin/bash

# Simple Logs Script for RRC Inventory
#
# Optionally takes a service name: ./logs.sh backend

source "$(dirname "$0")/compose-cmd.sh"

echo "📋 Showing RRC Inventory logs..."
echo "Press Ctrl+C to exit"
echo ""

# Show logs with follow
$DOCKER_COMPOSE_CMD logs -f "$@"
