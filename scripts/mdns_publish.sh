#!/usr/bin/env bash
set -euo pipefail

# mDNS Publisher Script for RRC Inventory
# Updates Avahi hosts file with current IP address
# Run on boot or when network changes

HOSTNAME_LABEL="rrc-inventory.local"
AVAHI_HOSTS="/etc/avahi/hosts"

get_ip() {
  # Try to get IP from eth0 first (main network interface)
  local eth0_ip=$(ip -4 addr show eth0 2>/dev/null | awk '/inet / {print $2}' | cut -d/ -f1 | head -n1)
  if [[ -n "$eth0_ip" ]]; then
    echo "$eth0_ip"
    return
  fi
  
  # Fallback: Get first non-loopback, non-docker IP
  ip -4 addr show | awk '/inet / && $2 !~ /^127\./ && $2 !~ /^172\.(1[7-9]|2[0-9]|3[0-1])\./ {print $2}' | head -n1 | cut -d/ -f1
}

require_sudo() {
  if [[ $EUID -ne 0 ]]; then
    echo "ERROR: This script requires sudo privileges" >&2
    echo "Please run: sudo $0" >&2
    exit 1
  fi
}

main() {
  require_sudo
  
  # Get current IP
  local_ip=$(get_ip || true)
  
  if [[ -z "${local_ip}" ]]; then
    echo "ERROR: Could not detect a non-loopback IPv4 address" >&2
    exit 1
  fi
  
  # Ensure hosts file exists
  touch "${AVAHI_HOSTS}"
  
  # Remove any existing entry for this hostname
  sed -i "/[[:space:]]${HOSTNAME_LABEL}$/d" "${AVAHI_HOSTS}"
  
  # Add new entry
  echo "${local_ip} ${HOSTNAME_LABEL}" >> "${AVAHI_HOSTS}"
  
  # Restart Avahi to pick up changes
  systemctl restart avahi-daemon
  
  echo "Published ${HOSTNAME_LABEL} → ${local_ip}"
}

main "$@"
