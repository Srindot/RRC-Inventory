#!/usr/bin/env bash
set -euo pipefail

# mDNS Publisher Script for RRC Inventory
# Updates Avahi hosts file with current IP address
# Run on boot or when network changes

HOSTNAME_LABEL="rrc-inventory.local"
AVAHI_HOSTS="/etc/avahi/hosts"

get_ip() {
  # Preferred method: ask kernel which source IP would be used to reach the public internet.
  # This works across different interface naming schemes (eth0, enp0s3, wlan0, etc.).
  local ipv=$(ip route get 8.8.8.8 2>/dev/null | awk '/src/ {for(i=1;i<=NF;i++){if($i=="src"){print $(i+1); exit}}}')
  if [[ -n "$ipv" ]]; then
    echo "$ipv"
    return
  fi

  # Fallback: get first non-loopback, non-docker IPv4 address
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
