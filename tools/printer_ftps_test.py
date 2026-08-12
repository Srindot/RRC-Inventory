#!/usr/bin/env python3
"""
Can we put a file onto a Bambu printer's storage over the network?

This is the one unknown behind "upload a sliced file from the website instead
of switching wifi to use Bambu Studio". It answers three questions without
anyone standing at the printer:

  1. Does the printer accept an FTPS login with the LAN access code?
  2. Can we upload a file, and does it come back in a directory listing?
  3. Can we delete it again (so the test leaves nothing behind)?

Nothing is printed and nothing is started - this only moves a file.

Usage:
    cd /tmp && python3 ~/projects/RRC-Inventory/tools/printer_ftps_test.py \\
        --ip 192.168.2.101 --code 89a8541a

Add --keep to leave the test file on the printer, so you can check whether it
shows up in the printer's own screen menu next time you walk past.
"""

import argparse
import ftplib
import os
import socket
import ssl
import sys
import time

FTPS_PORT = 990
TEST_NAME = "rrc-upload-test.txt"


def ok(msg):
    print(f"  \033[32m✔\033[0m {msg}")


def fail(msg):
    print(f"  \033[31m✘\033[0m {msg}")


def warn(msg):
    print(f"  \033[33m!\033[0m {msg}")


def heading(msg):
    print(f"\n\033[1m{msg}\033[0m")


class ImplicitFTPS(ftplib.FTP_TLS):
    """Bambu printers speak implicit FTPS: the socket is encrypted from the
    first byte, rather than starting plain and issuing AUTH TLS. Python's
    ftplib only does explicit FTPS, so the control socket is wrapped by hand.
    """

    def __init__(self, *args, **kwargs):
        self._sock = None
        super().__init__(*args, **kwargs)

    @property
    def sock(self):
        return self._sock

    @sock.setter
    def sock(self, value):
        if value is not None and not isinstance(value, ssl.SSLSocket):
            value = self.context.wrap_socket(value)
        self._sock = value

    def ntransfercmd(self, cmd, rest=None):
        # Data connections must be wrapped too, and Bambu's server does not
        # support session reuse on them.
        conn, size = ftplib.FTP.ntransfercmd(self, cmd, rest)
        conn = self.context.wrap_socket(conn, server_hostname=self.host)
        return conn, size

    def storbinary(self, cmd, fp, blocksize=8192, callback=None, rest=None):
        """Upload without the TLS shutdown handshake.

        ftplib's own version calls conn.unwrap() after sending, which blocks
        waiting for the server's close_notify. Bambu's FTPS server does not
        send one, so the client hangs until it times out - even though the
        file transferred fine. Closing the socket is enough for the server to
        see the end of the data.
        """
        self.voidcmd("TYPE I")
        conn = self.transfercmd(cmd, rest)
        try:
            while True:
                buf = fp.read(blocksize)
                if not buf:
                    break
                conn.sendall(buf)
                if callback:
                    callback(buf)
        finally:
            try:
                conn.close()
            except Exception:  # noqa: BLE001
                pass
        return self.voidresp()


def main():
    parser = argparse.ArgumentParser(description="Test FTPS upload to a Bambu printer")
    parser.add_argument("--ip", default=os.getenv("PRINTER_IP"))
    parser.add_argument("--code", default=os.getenv("PRINTER_ACCESS_CODE"))
    parser.add_argument("--user", default="bblp")
    parser.add_argument("--keep", action="store_true",
                        help="leave the test file on the printer")
    args = parser.parse_args()

    if not (args.ip and args.code):
        print("Need the printer's IP and access code.\n")
        print("  python3 tools/printer_ftps_test.py --ip <IP> --code <ACCESS_CODE>")
        return 2

    print(f"Testing file upload to {args.ip}:{FTPS_PORT}")

    # --- 1. is the port even open? ------------------------------------
    heading("1. Reachability")
    try:
        with socket.create_connection((args.ip, FTPS_PORT), timeout=8):
            ok(f"port {FTPS_PORT} is open")
    except Exception as e:  # noqa: BLE001
        fail(f"cannot reach port {FTPS_PORT}: {e}")
        print("\n  The printer may not run FTPS, or LAN mode is off.")
        return 1

    # --- 2. log in ----------------------------------------------------
    heading("2. Login")
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE

    ftp = ImplicitFTPS(context=context)
    ftp.encoding = "utf-8"

    try:
        ftp.connect(host=args.ip, port=FTPS_PORT, timeout=20)
        ftp.login(user=args.user, passwd=args.code)
        ftp.prot_p()
        ok(f"logged in as {args.user!r}")
    except Exception as e:  # noqa: BLE001
        fail(f"login failed: {type(e).__name__}: {e}")
        print("\n  Wrong access code, or this firmware does not allow FTPS.")
        return 1

    landed = False
    upload_error = None

    try:
        # --- 3. what is already there --------------------------------
        heading("3. Existing files")
        try:
            entries = ftp.nlst()
            ok(f"listing works - {len(entries)} entries at the root")
            for entry in entries[:12]:
                print(f"      {entry}")
            if len(entries) > 12:
                print(f"      ... and {len(entries) - 12} more")
        except Exception as e:  # noqa: BLE001
            warn(f"could not list the root: {type(e).__name__}: {e}")
            entries = []

        # --- 4. upload ------------------------------------------------
        heading("4. Upload")
        payload = (b"RRC Inventory upload test.\n"
                   b"If you are reading this on the printer, the website can "
                   b"send files.\n")

        import io
        try:
            ftp.storbinary(f"STOR {TEST_NAME}", io.BytesIO(payload))
            ok(f"uploaded {TEST_NAME} ({len(payload)} bytes)")
        except Exception as e:  # noqa: BLE001
            upload_error = e
            warn(f"the upload call raised: {type(e).__name__}: {e}")
            print("      Checking whether the file arrived anyway - the data can")
            print("      transfer fine while the client waits for a handshake")
            print("      the printer never completes.")

            # The control connection may be unusable after a timeout
            try:
                ftp.voidcmd("NOOP")
            except Exception:  # noqa: BLE001
                warn("reconnecting to check")
                try:
                    ftp.close()
                except Exception:  # noqa: BLE001
                    pass
                try:
                    ftp = ImplicitFTPS(context=context)
                    ftp.encoding = "utf-8"
                    ftp.connect(host=args.ip, port=FTPS_PORT, timeout=20)
                    ftp.login(user=args.user, passwd=args.code)
                    ftp.prot_p()
                except Exception as reconnect_error:  # noqa: BLE001
                    fail(f"could not reconnect to check: {reconnect_error}")
                    return 1

        # --- 5. did it actually land? ---------------------------------
        heading("5. Confirm it arrived")
        time.sleep(2)
        try:
            after = ftp.nlst()
            landed = any(TEST_NAME in name for name in after)
            if landed:
                ok(f"{TEST_NAME} is on the printer - confirmed over the network")
            else:
                warn("upload reported success but the file is not in the listing")
                print(f"      listing: {after[:12]}")
        except Exception as e:  # noqa: BLE001
            warn(f"could not confirm by listing: {e}")
            landed = False

        # --- 6. clean up ----------------------------------------------
        heading("6. Cleanup")
        if args.keep:
            warn(f"leaving {TEST_NAME} on the printer (--keep)")
            print("      Check the printer's screen later to see whether files "
                  "uploaded this way appear in its print menu.")
        else:
            try:
                ftp.delete(TEST_NAME)
                ok("test file deleted - nothing left behind")
            except Exception as e:  # noqa: BLE001
                warn(f"could not delete it: {e} - remove {TEST_NAME} by hand")

    finally:
        try:
            ftp.quit()
        except Exception:  # noqa: BLE001
            pass

    heading("Result")
    if landed and upload_error:
        print("  Upload WORKS - the file arrived. The client only hung waiting")
        print("  for a TLS shutdown the printer never sends, which the Go")
        print("  backend does not wait for.")
        return 0

    if landed:
        print("  Upload WORKS. Sending sliced files from the website is buildable.")
        print("  Re-run with --keep, then check the printer's screen to see if")
        print("  the file appears in its print menu.")
        return 0

    if upload_error:
        fail("The file did not arrive - writing really does not work.")
    else:
        print("  Upload did not clearly work - see the messages above.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
