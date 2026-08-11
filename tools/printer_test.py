#!/usr/bin/env python3
"""
Connection test for a Bambu Lab P1S on the local network.

Answers three questions before we build anything:
  1. Can we reach the printer at all?
  2. Does MQTT give us live print status?
  3. Does the camera give us JPEG frames, and how fast?

Usage:
    pip install bambulabs_api
    python3 tools/printer_test.py --ip 192.168.1.50 --serial 01P00A000000000 --code 12345678

Or set PRINTER_IP / PRINTER_SERIAL / PRINTER_ACCESS_CODE in the environment
and run it with no arguments. Nothing is written anywhere except a single
sample frame (printer-test-frame.jpg) in the current directory.
"""

import argparse
import logging
import os
import socket
import sys
import time

MQTT_PORT = 8883
CAMERA_PORT = 6000
CAMERA_WAIT_SECONDS = 20
FRAME_RATE_SAMPLE_SECONDS = 15


def ok(msg):
    print(f"  \033[32m✔\033[0m {msg}")


def fail(msg):
    print(f"  \033[31m✘\033[0m {msg}")


def warn(msg):
    print(f"  \033[33m!\033[0m {msg}")


def heading(msg):
    print(f"\n\033[1m{msg}\033[0m")


def check_port(ip, port, label):
    """Plain TCP reachability, before any protocol work."""
    try:
        with socket.create_connection((ip, port), timeout=5):
            ok(f"{label} (port {port}) is open")
            return True
    except socket.timeout:
        fail(f"{label} (port {port}) timed out - is the IP right and on this network?")
    except ConnectionRefusedError:
        fail(f"{label} (port {port}) refused the connection - is LAN mode enabled?")
    except OSError as e:
        fail(f"{label} (port {port}) unreachable: {e}")
    return False


def main():
    parser = argparse.ArgumentParser(description="Test a Bambu P1S connection")
    parser.add_argument("--ip", default=os.getenv("PRINTER_IP"))
    parser.add_argument("--serial", default=os.getenv("PRINTER_SERIAL"))
    parser.add_argument("--code", default=os.getenv("PRINTER_ACCESS_CODE"))
    args = parser.parse_args()

    if not (args.ip and args.serial and args.code):
        print("Need the printer's IP, serial and access code.\n")
        print("  python3 tools/printer_test.py --ip <IP> --serial <SERIAL> --code <ACCESS_CODE>\n")
        print("All three are on the printer screen under Settings, with LAN mode enabled.")
        return 2

    print(f"Testing printer at {args.ip} (serial {args.serial})")

    # --- 1. reachability -----------------------------------------------
    heading("1. Network reachability")
    mqtt_open = check_port(args.ip, MQTT_PORT, "MQTT / status")
    camera_open = check_port(args.ip, CAMERA_PORT, "Camera")

    if not mqtt_open and not camera_open:
        fail("Cannot reach the printer at all. Check the IP, the cable, "
             "and that this machine is on the printer's network.")
        return 1

    try:
        import bambulabs_api as bl
    except ImportError:
        fail("bambulabs_api is not installed. Run: pip install bambulabs_api")
        return 1

    # The cloned repo folder is also called bambulabs_api, so running from the
    # project root imports an empty directory instead of the real library.
    if not hasattr(bl, "Printer"):
        fail("Found a folder named 'bambulabs_api' instead of the installed "
             "library.")
        print(f"      (imported from: {list(getattr(bl, '__path__', ['?']))[0]})")
        print("      Run this from a different directory, e.g.:")
        print("        cd /tmp && python3 ~/projects/RRC-Inventory/tools/printer_test.py ...")
        return 1

    # The library logs its own retry chatter at error level; we report status
    # ourselves, so keep the output readable.
    logging.getLogger("bambulabs_api").setLevel(logging.CRITICAL)

    printer = bl.Printer(args.ip, args.code, args.serial)
    printer.connect()

    status_ok = False
    camera_ok = False
    frame_rate = 0.0

    try:
        # --- 2. status via MQTT ----------------------------------------
        heading("2. Live status (MQTT)")
        print("  waiting for the first status message...")

        # Connecting is not enough - anything can accept a TCP connection.
        # Real status means the MQTT session authenticated AND reported a state.
        connected = False
        state = None
        for _ in range(20):
            time.sleep(1)
            try:
                connected = printer.mqtt_client_connected()
            except Exception:  # noqa: BLE001
                connected = False
            try:
                state = printer.get_state()
            except Exception:  # noqa: BLE001 - library raises assorted errors early on
                state = None
            if connected and state is not None and str(state) != "GcodeState.UNKNOWN":
                break

        if not connected:
            fail("Could not authenticate to the printer's MQTT server. "
                 "Usually a wrong access code, or LAN mode is off.")
        elif state is None or str(state) == "GcodeState.UNKNOWN":
            fail("Connected, but the printer reported no usable state. "
                 "Check the serial number - it selects which printer to listen to.")
        else:
            status_ok = True
            ok(f"Printer state: {state}")

            # Everything below is best-effort - fields vary by state and firmware
            for label, getter in [
                ("Current file", printer.get_file_name),
                ("Progress", printer.get_percentage),
                ("Time remaining (s)", printer.get_time),
                ("Nozzle temp", printer.get_nozzle_temperature),
                ("Bed temp", printer.get_bed_temperature),
                ("Chamber temp", printer.get_chamber_temperature),
                ("Print speed", printer.get_print_speed),
                ("Light", printer.get_light_state),
            ]:
                try:
                    value = getter()
                    print(f"      {label}: {value}")
                except Exception as e:  # noqa: BLE001
                    print(f"      {label}: unavailable ({type(e).__name__})")

        # --- 3. camera --------------------------------------------------
        heading("3. Camera")
        print(f"  waiting up to {CAMERA_WAIT_SECONDS}s for the first frame...")

        frame = None
        deadline = time.time() + CAMERA_WAIT_SECONDS
        while time.time() < deadline:
            if printer.camera_client.last_frame is not None:
                frame = printer.camera_client.last_frame
                break
            time.sleep(0.5)

        if frame is None:
            fail("No camera frame received. The camera may be disabled, or "
                 "this firmware may not allow LAN camera access.")
        else:
            camera_ok = True
            ok(f"Got a frame: {len(frame):,} bytes")

            with open("printer-test-frame.jpg", "wb") as f:
                f.write(frame)
            ok("Saved a sample frame to printer-test-frame.jpg - open it to "
               "check the camera actually sees the print area")

            try:
                from PIL import Image
                from io import BytesIO
                img = Image.open(BytesIO(frame))
                ok(f"Frame resolution: {img.width}x{img.height}")
            except Exception:  # noqa: BLE001
                warn("Could not read the frame's resolution (frame still saved)")

            # How fast do frames actually arrive? This decides whether the
            # feed feels like video or a slideshow.
            print(f"  measuring frame rate for {FRAME_RATE_SAMPLE_SECONDS}s...")
            frames_seen = 0
            last = frame
            end = time.time() + FRAME_RATE_SAMPLE_SECONDS
            while time.time() < end:
                current = printer.camera_client.last_frame
                if current is not None and current is not last:
                    frames_seen += 1
                    last = current
                time.sleep(0.05)

            frame_rate = frames_seen / FRAME_RATE_SAMPLE_SECONDS
            ok(f"{frames_seen} frames in {FRAME_RATE_SAMPLE_SECONDS}s "
               f"(~{frame_rate:.1f} fps)")

    finally:
        printer.disconnect()

    # --- verdict -------------------------------------------------------
    heading("Result")
    print(f"  Status feed: {'WORKS' if status_ok else 'FAILED'}")
    print(f"  Camera feed: {'WORKS' if camera_ok else 'FAILED'}"
          + (f" (~{frame_rate:.1f} fps)" if camera_ok else ""))

    if status_ok and camera_ok:
        print("\n  Both work - a printer page with live status and a camera "
              "view is buildable.")
    elif status_ok:
        print("\n  Status works, camera does not - a status-only printer page "
              "is still worth building.")
    elif camera_ok:
        print("\n  Camera works but status does not - check the serial number, "
              "it is only used for the status feed.")
    else:
        print("\n  Neither works. Check LAN mode, access code and serial "
              "before we invest any more time in this.")

    return 0 if (status_ok or camera_ok) else 1


if __name__ == "__main__":
    sys.exit(main())
