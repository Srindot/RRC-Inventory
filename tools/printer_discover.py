#!/usr/bin/env python3
"""
Find every Bambu Lab printer on this network segment and print its real serial.

Bambu printers announce themselves over SSDP (the same way Bambu Studio finds
them). Those announcements contain the serial number, model and IP - which
saves reading stickers off the back of three printers.

Must be run on a machine directly on the printers' network (your server, via
the USB-ethernet side). Multicast does not cross routers, and it does not work
from inside a Docker bridge network.

Usage:
    cd /tmp && python3 ~/projects/RRC-Inventory/tools/printer_discover.py
    cd /tmp && python3 ~/projects/RRC-Inventory/tools/printer_discover.py --seconds 60
"""

import argparse
import socket
import struct
import sys
import time

SSDP_MULTICAST = "239.255.255.250"
# Bambu devices announce on 2021 and listen for searches on 1990
LISTEN_PORTS = (2021, 1990)

# Best-effort model code mapping - the raw code is always printed too
MODEL_NAMES = {
    "C11": "P1P",
    "C12": "P1S",
    "C13": "P1S",
    "N1": "A1 mini",
    "N2S": "A1",
    "BL-P001": "X1 Carbon",
    "BL-P002": "X1",
}


def parse_ssdp(payload):
    """Pull the headers out of an SSDP NOTIFY/response packet."""
    try:
        text = payload.decode("utf-8", errors="replace")
    except Exception:  # noqa: BLE001
        return None

    lines = text.split("\r\n")
    if not lines or not lines[0].upper().startswith(("NOTIFY", "HTTP/1.1", "M-SEARCH")):
        return None

    headers = {}
    for line in lines[1:]:
        if ":" in line:
            key, _, value = line.partition(":")
            headers[key.strip().upper()] = value.strip()
    return headers


def make_socket(port):
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    try:
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEPORT, 1)
    except (AttributeError, OSError):
        pass  # not available everywhere, not essential

    sock.bind(("", port))

    # Join the SSDP multicast group on every interface
    try:
        mreq = struct.pack("4sl", socket.inet_aton(SSDP_MULTICAST), socket.INADDR_ANY)
        sock.setsockopt(socket.IPPROTO_IP, socket.IP_ADD_MEMBERSHIP, mreq)
    except OSError as e:
        print(f"  ! Could not join multicast group on port {port}: {e}")

    sock.setblocking(False)
    return sock


def send_search(sockets):
    """Nudge printers to answer instead of waiting for their next broadcast."""
    msg = "\r\n".join([
        "M-SEARCH * HTTP/1.1",
        f"HOST: {SSDP_MULTICAST}:1990",
        'MAN: "ssdp:discover"',
        "MX: 1",
        "ST: urn:bambulab-com:device:3dprinter:1",
        "", "",
    ]).encode()

    for sock in sockets:
        for port in LISTEN_PORTS:
            try:
                sock.sendto(msg, (SSDP_MULTICAST, port))
            except OSError:
                pass


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--seconds", type=int, default=40,
                        help="how long to listen (default 40)")
    args = parser.parse_args()

    print("Listening for Bambu printers on this network...")
    print(f"(up to {args.seconds}s - printers announce themselves periodically)\n")

    sockets = []
    for port in LISTEN_PORTS:
        try:
            sockets.append(make_socket(port))
        except OSError as e:
            print(f"  ! Cannot listen on port {port}: {e}")

    if not sockets:
        print("Could not listen on any port. Is something else using them?")
        return 1

    found = {}
    last_search = 0.0
    deadline = time.time() + args.seconds

    try:
        while time.time() < deadline:
            # Re-send the search every few seconds
            if time.time() - last_search > 5:
                send_search(sockets)
                last_search = time.time()

            for sock in sockets:
                try:
                    payload, addr = sock.recvfrom(9000)
                except BlockingIOError:
                    continue
                except OSError:
                    continue

                headers = parse_ssdp(payload)
                if not headers:
                    continue

                serial = headers.get("USN", "")
                model_code = headers.get("DEVMODEL.BAMBU.COM", "")
                name = headers.get("DEVNAME.BAMBU.COM", "")
                location = headers.get("LOCATION", addr[0])

                # Ignore our own M-SEARCH packets echoing back
                if not serial:
                    continue

                if serial not in found:
                    found[serial] = {
                        "ip": location or addr[0],
                        "model_code": model_code,
                        "name": name,
                    }
                    friendly = MODEL_NAMES.get(model_code, model_code or "unknown")
                    print(f"  ✔ Found a printer")
                    print(f"      Name:   {name or '(unnamed)'}")
                    print(f"      Model:  {friendly}"
                          + (f" (code {model_code})" if model_code else ""))
                    print(f"      IP:     {found[serial]['ip']}")
                    print(f"      SERIAL: {serial}\n")

            time.sleep(0.05)

    except KeyboardInterrupt:
        print("\n(stopped early)")

    for sock in sockets:
        sock.close()

    print("--- Result ---")
    if not found:
        print("No printers announced themselves.")
        print("Either this machine is not on the printers' network segment,")
        print("or the switch is blocking multicast. In that case read the")
        print("serial off the sticker on the back of each printer instead.")
        return 1

    print(f"Found {len(found)} printer(s):\n")
    for serial, info in found.items():
        print(f"  {info['name'] or '(unnamed)'}  {info['ip']}  serial={serial}")

    first_serial, first = next(iter(found.items()))
    print("\nNow re-run the connection test with a real serial, e.g.:")
    print(f"  python3 ~/projects/RRC-Inventory/tools/printer_test.py \\")
    print(f"      --ip {first['ip']} --serial {first_serial} --code <ACCESS_CODE>")
    return 0


if __name__ == "__main__":
    sys.exit(main())
