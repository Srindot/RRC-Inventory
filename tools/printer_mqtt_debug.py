#!/usr/bin/env python3
"""
Find out exactly why the printer's MQTT status feed refuses us.

The camera already works with the access code, so the code and LAN mode are
fine. This talks to the printer's MQTT server directly (no wrapper library) and
prints the raw result, which distinguishes:

  * "Not authorized"  -> credentials really are rejected for MQTT
  * TLS errors        -> handshake problem
  * connected         -> the wrapper library is at fault, not the printer

It also subscribes with a wildcard, so if the printer publishes anything we
learn its REAL serial number from the topic name.

Usage:
    pip install "paho-mqtt>=2.0.0"
    cd /tmp && python3 ~/projects/RRC-Inventory/tools/printer_mqtt_debug.py \
        --ip 192.168.2.101 --code 89a8541a [--serial 01P00A123456789]
"""

import argparse
import json
import os
import ssl
import sys
import time

LISTEN_SECONDS = 25

seen_topics = {}
connect_result = {"done": False, "code": None, "text": ""}


def on_connect(client, userdata, flags, reason_code, properties=None):
    connect_result["done"] = True
    connect_result["code"] = reason_code
    connect_result["text"] = str(reason_code)
    print(f"  CONNACK from printer: {reason_code!s}")

    is_failure = getattr(reason_code, "is_failure", None)
    if is_failure is True or (isinstance(reason_code, int) and reason_code != 0):
        print("  → The printer actively rejected the login.")
        return

    print("  → Connected. Subscribing...")

    # Wildcards get the connection dropped by this broker - it only allows a
    # device its own topic. Opt in with --wildcard only to re-test that.
    if userdata.get("wildcard"):
        for topic in ("device/+/report", "#"):
            result = client.subscribe(topic)
            print(f"     subscribe {topic!r}: {result} (expect a disconnect)")

    if userdata.get("serial"):
        serial = userdata["serial"]
        client.subscribe(f"device/{serial}/report")
        # Ask the printer to dump its full state instead of waiting for a
        # periodic push.
        client.publish(
            f"device/{serial}/request",
            json.dumps({"pushing": {"command": "pushall"},
                        "info": {"command": "get_version"}}),
        )
        print(f"     asked device/{serial}/request for a full state push")


def on_message(client, userdata, msg):
    parts = msg.topic.split("/")
    serial = parts[1] if len(parts) > 2 else "?"
    seen_topics.setdefault(msg.topic, 0)
    seen_topics[msg.topic] += 1

    if seen_topics[msg.topic] == 1:
        print(f"\n  📨 message on {msg.topic}")
        print(f"     serial in topic: {serial}")
        try:
            payload = json.loads(msg.payload)
            top = list(payload.keys())
            print(f"     payload keys: {top}")
            info = payload.get("print", {})
            for key in ("gcode_state", "mc_percent", "mc_remaining_time",
                        "subtask_name", "nozzle_temper", "bed_temper"):
                if key in info:
                    print(f"       {key}: {info[key]}")
        except Exception:  # noqa: BLE001
            print(f"     raw payload (first 200 bytes): {msg.payload[:200]!r}")


def on_disconnect(client, userdata, *args):
    print("  (disconnected)")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--ip", default=os.getenv("PRINTER_IP"))
    parser.add_argument("--code", default=os.getenv("PRINTER_ACCESS_CODE"))
    parser.add_argument("--serial", default=os.getenv("PRINTER_SERIAL"))
    parser.add_argument("--user", default="bblp",
                        help="MQTT username (default: bblp)")
    parser.add_argument("--wildcard", action="store_true",
                        help="also try wildcard subscriptions (gets you kicked)")
    args = parser.parse_args()

    if not (args.ip and args.code):
        print("Need at least --ip and --code (serial optional).")
        return 2

    try:
        import paho.mqtt.client as mqtt
        from paho.mqtt.client import CallbackAPIVersion
    except ImportError:
        print("paho-mqtt is not installed. Run: pip install 'paho-mqtt>=2.0.0'")
        return 1

    print(f"paho-mqtt version: {getattr(mqtt, '__version__', 'unknown')}")
    print(f"Connecting to {args.ip}:8883 as user {args.user!r}...")

    client = mqtt.Client(CallbackAPIVersion.VERSION2, protocol=mqtt.MQTTv311,
                         userdata={"serial": args.serial,
                                   "wildcard": args.wildcard})
    client.username_pw_set(args.user, args.code)
    client.tls_set(tls_version=ssl.PROTOCOL_TLS, cert_reqs=ssl.CERT_NONE)
    client.tls_insecure_set(True)
    client.on_connect = on_connect
    client.on_message = on_message
    client.on_disconnect = on_disconnect

    # Surface paho's own diagnostics - this is where TLS errors show up.
    client.enable_logger()

    try:
        client.connect(args.ip, 8883, 60)
    except Exception as e:  # noqa: BLE001
        print(f"  ✘ Could not open the connection: {type(e).__name__}: {e}")
        return 1

    client.loop_start()

    started = time.time()
    try:
        while time.time() - started < LISTEN_SECONDS:
            time.sleep(0.5)
            # Once messages flow there is nothing more to learn by waiting
            if seen_topics and time.time() - started > 8:
                break
    except KeyboardInterrupt:
        print("\n(stopped early)")

    client.loop_stop()
    client.disconnect()

    print("\n--- Result ---")
    if not args.serial and not args.wildcard:
        print("No --serial given, so nothing was subscribed to.")
        print("Run printer_discover.py to get the real serial, then pass it here.")

    if not connect_result["done"]:
        print("No CONNACK at all - the TLS handshake probably failed.")
        print("Look for SSL errors in the log lines above.")
        return 1

    print(f"Connection result: {connect_result['text']}")

    if seen_topics:
        print("Topics that produced messages:")
        for topic, count in seen_topics.items():
            print(f"  {topic}  ({count} message(s))")
        serials = {t.split('/')[1] for t in seen_topics if t.count('/') >= 2}
        if serials:
            print(f"\n👉 REAL SERIAL NUMBER(S): {', '.join(sorted(serials))}")
            print("   Use that with printer_test.py")
    else:
        print("Connected but received no messages.")
        print("If the wildcard subscribe was refused, the printer only allows")
        print("its own serial's topic - so we need the real serial from the")
        print("printer screen or Bambu Studio.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
