#!/usr/bin/env bash
set -euo pipefail

# mDNS Setup Script for RRC Inventory
# Configures Avahi to advertise rrc-inventory.local on the local network

HOSTNAME_LABEL="rrc-inventory.local"
AVAHI_HOSTS="/etc/avahi/hosts"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error() { echo -e "${RED}[ERROR]${NC} $1"; }

get_ip() {
  # Preferred method: ask kernel which source IP would be used to reach the public internet.
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
    print_error "This script requires sudo privileges"
    echo "Please run: sudo $0"
    exit 1
  fi
}

main() {
  echo "🌐 RRC Inventory - mDNS Setup"
  echo "================================"
  
  require_sudo
  
  # Install Avahi if needed
  print_status "Checking Avahi installation..."
  if ! command -v avahi-daemon &> /dev/null; then
    print_status "Installing avahi-daemon and avahi-utils..."
    apt-get update -qq
    apt-get install -y avahi-daemon avahi-utils
    print_success "Avahi installed"
  else
    print_success "Avahi already installed"
  fi
  
  # Get current IP
  print_status "Detecting network IP..."
  local_ip=$(get_ip || true)
  
  if [[ -z "${local_ip}" ]]; then
    print_error "Could not detect a non-loopback IPv4 address"
    print_error "Please ensure you are connected to a network"
    exit 1
  fi
  
  print_success "Detected IP: ${local_ip}"
  
  # Configure Avahi hosts file
  print_status "Configuring ${AVAHI_HOSTS}..."
  touch "${AVAHI_HOSTS}"
  
  # Remove any existing entry for this hostname
  sed -i "/[[:space:]]${HOSTNAME_LABEL}$/d" "${AVAHI_HOSTS}"
  
  # Add new entry
  echo "${local_ip} ${HOSTNAME_LABEL}" >> "${AVAHI_HOSTS}"
  print_success "Added mapping: ${local_ip} → ${HOSTNAME_LABEL}"
  
  # Restart Avahi daemon
  print_status "Restarting Avahi daemon..."
  systemctl restart avahi-daemon
  systemctl enable avahi-daemon
  print_success "Avahi daemon restarted and enabled"
  
  echo ""
  echo "================================"
  print_success "mDNS setup complete!"
  echo ""
  echo "Your RRC Inventory system is now accessible at:"
  echo -e "  ${GREEN}http://rrc-inventory.local${NC}"
  echo ""
  echo "Notes:"
  echo "  • Devices must be on the same local network"
  echo "  • Devices must support mDNS/Bonjour (most do)"
  echo "  • Firewall must allow mDNS (UDP port 5353)"
  echo ""
  echo "To test from another device:"
  echo -e "  ${BLUE}ping rrc-inventory.local${NC}"
  echo -e "  ${BLUE}avahi-resolve -n rrc-inventory.local${NC}"
  echo ""
}

main "$@"
