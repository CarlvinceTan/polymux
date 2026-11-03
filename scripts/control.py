"""Build Control only when its source, toolchain, or executable changes."""

import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys


def fingerprint(root, toolchain):
    package = root / "tools/control"
    paths = [package / "Package.swift", *sorted((package / "Sources").rglob("*.swift"))]
    resolved = package / "Package.resolved"
    if resolved.exists():
        paths.append(resolved)
    digest = hashlib.sha256(toolchain.encode())
    for path in paths:
        digest.update(str(path.relative_to(package)).encode() + b"\0" + path.read_bytes())
    return digest.hexdigest()


def identity(path):
    try:
        stat = path.stat()
        return [str(path.resolve()), stat.st_ino, stat.st_size, stat.st_mtime_ns]
    except FileNotFoundError:
        return None


def prepare(root, toolchain):
    output = root / ".build/control"
    executable = output / "debug/control"
    stamp = output / "source.json"
    locks = root / ".build/locks"
    locks.mkdir(parents=True, exist_ok=True)
    with (locks / "control.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        source = fingerprint(root, toolchain)
        try:
            saved = json.loads(stamp.read_text())
        except (OSError, ValueError):
            saved = {}
        if saved == {"source": source, "executable": identity(executable)} and executable.is_file():
            return executable
        subprocess.run(["swift", "build", "--package-path", str(root / "tools/control"),
                        "--scratch-path", str(output), "--product", "control"],
                       stdout=sys.stderr, check=True, pass_fds=(lock.fileno(),))
        if fingerprint(root, toolchain) != source:
            raise SystemExit("Control sources changed during compilation; rerun the command.")
        temporary = stamp.with_suffix(f".{os.getpid()}.tmp")
        try:
            temporary.write_text(json.dumps({"source": source, "executable": identity(executable)}))
            temporary.replace(stamp)
        finally:
            temporary.unlink(missing_ok=True)
    return executable


def main():
    root = Path(__file__).resolve().parents[1]
    swift = shutil.which("swift")
    if not swift:
        raise SystemExit("Swift is required to build Control.")
    developer = subprocess.check_output(["xcode-select", "-p"], text=True).strip()
    compiler = Path(os.environ.get("DEVELOPER_DIR", developer)) / "Toolchains/XcodeDefault.xctoolchain/usr/bin/swift"
    toolchain = json.dumps([identity(Path(swift)), identity(compiler), developer,
                           os.environ.get("DEVELOPER_DIR"), os.environ.get("SDKROOT"), platform.machine()])
    executable = prepare(root, toolchain)
    os.execv(executable, [str(executable), *sys.argv[1:]])


if __name__ == "__main__":
    main()
