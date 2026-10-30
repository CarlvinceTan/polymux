"""Resolve app-scoped Keychain entitlements from a Developer ID profile."""

import datetime
import fnmatch
import plistlib
from pathlib import Path
import subprocess
import sys


def entitlements(profile, base, bundle):
    allowed = profile.get("Entitlements", {})
    identifier = allowed.get("com.apple.application-identifier", "")
    teams = profile.get("TeamIdentifier", [])
    expires = profile.get("ExpirationDate")
    if (not isinstance(expires, datetime.datetime) or expires <= datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)
            or profile.get("ProvisionsAllDevices") is not True):
        raise ValueError("Choose a current Developer ID provisioning profile.")
    if len(teams) != 1 or identifier != f"{teams[0]}.{bundle}":
        raise ValueError("The provisioning profile must match this app and its signing team.")
    groups = allowed.get("keychain-access-groups", [])
    if not any(fnmatch.fnmatchcase(identifier, group) for group in groups):
        raise ValueError("The provisioning profile does not allow this app's Keychain access.")
    result = dict(base)
    result.update({"com.apple.application-identifier": identifier,
                   "com.apple.developer.team-identifier": teams[0],
                   "keychain-access-groups": [identifier]})
    return result


def main():
    profile, source, info = map(Path, sys.argv[1:])
    decoded = subprocess.run(["security", "cms", "-D", "-i", str(profile)], capture_output=True, check=True)
    value = entitlements(plistlib.loads(decoded.stdout), plistlib.loads(source.read_bytes()),
                         plistlib.loads(info.read_bytes())["CFBundleIdentifier"])
    sys.stdout.buffer.write(plistlib.dumps(value))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Keychain signing: {error}")
