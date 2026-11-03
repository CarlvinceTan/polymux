"""Capture an isolated iPhone app and delete only this invocation's simulator."""

import json
import os
from pathlib import Path
import plistlib
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def command(*args):
    return subprocess.check_output(args, text=True).strip()


def device_spec(devices, selected=None):
    for runtime, entries in devices.items():
        for device in entries:
            if device.get("isAvailable") and "iOS-" in runtime and (
                device.get("udid") == selected if selected else device.get("name", "").startswith("iPhone")
            ):
                return device["deviceTypeIdentifier"], runtime
    raise ValueError("No matching available iPhone simulator; install an iOS runtime in Xcode.")


def capture(output, flags):
    devices = json.loads(command("xcrun", "simctl", "list", "devices", "available", "--json"))["devices"]
    hardware, runtime = device_spec(devices, os.environ.get("POLYMUX_SIM_DEVICE"))
    parent = ROOT / ".build/sim"
    parent.mkdir(parents=True, exist_ok=True)
    name = f"Polymux {os.getpid()}"
    device = command("xcrun", "simctl", "create", name, hardware, runtime)
    try:
        with tempfile.TemporaryDirectory(prefix="capture-", dir=parent) as temporary:
            bundle = Path(temporary) / f"{name}.app"
            source = os.environ.get("POLYMUX_SIM_APP")
            if source:
                command("ditto", source, str(bundle))
            else:
                subprocess.run([str(ROOT / "scripts/check.sh"), "ios", str(bundle)], check=True)
            plist = bundle / "Info.plist"
            values = plistlib.loads(plist.read_bytes())
            identifier = f"com.flarehq.polymux.app.test.sim.{os.getpid()}"
            original = values["CFBundleIdentifier"]
            values.update(CFBundleIdentifier=identifier, CFBundleName=name, CFBundleDisplayName=name)
            plist.write_bytes(plistlib.dumps(values))
            # simctl refuses an extension whose identifier isn't under its app's.
            for extension in bundle.glob("PlugIns/*.appex/Info.plist"):
                nested = plistlib.loads(extension.read_bytes())
                nested["CFBundleIdentifier"] = identifier + nested["CFBundleIdentifier"].removeprefix(original)
                extension.write_bytes(plistlib.dumps(nested))
            command("xcrun", "simctl", "boot", device)
            command("xcrun", "simctl", "bootstatus", device, "-b")
            command("xcrun", "simctl", "install", device, str(bundle))
            command("xcrun", "simctl", "launch", device, identifier, *flags)
            time.sleep(float(os.environ.get("SIM_WAIT", "25")))
            # iPhone Duo has two displays, and simctl won't choose one on its own.
            command("xcrun", "simctl", "io", device, "screenshot", "--display=primary", str(output))
    finally:
        # A second signal must not interrupt cleanup of the device we created.
        previous = {number: signal.signal(number, signal.SIG_IGN)
                    for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        try:
            subprocess.run(["xcrun", "simctl", "shutdown", device], capture_output=True)
            subprocess.run(["xcrun", "simctl", "delete", device], check=True, capture_output=True)
        finally:
            for number, handler in previous.items():
                signal.signal(number, handler)
    print(output)


def main():
    if sys.argv[1:] == ["--stop"]:
        print("Each capture removes its own simulator automatically.")
        return
    if len(sys.argv) < 2:
        raise SystemExit("usage: scripts/sim.sh out.png [app flags...]")
    for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(number, lambda number, _frame: sys.exit(128 + number))
    capture(Path(sys.argv[1]).resolve(), sys.argv[2:])


if __name__ == "__main__":
    main()
