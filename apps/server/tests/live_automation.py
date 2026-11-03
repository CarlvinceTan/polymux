#!/usr/bin/env python3
"""Run an isolated, synthetic checkpoint/restart probe against a chosen model."""
import argparse
import json
import os
import pathlib
import socket
import sqlite3
import subprocess
import time
import urllib.error
import urllib.request


def request(url, method="GET", body=None):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=5) as response:
        return json.load(response)


def wait(check, seconds=120):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except (OSError, urllib.error.URLError, sqlite3.Error):
            pass
        time.sleep(0.2)
    raise RuntimeError("Synthetic probe did not reach its expected state")


def ports():
    for _ in range(100):
        with socket.socket() as first, socket.socket() as second:
            first.bind(("127.0.0.1", 0))
            port = first.getsockname()[1]
            if port == 65535:
                continue
            try:
                second.bind(("127.0.0.1", port + 1))
                return port
            except OSError:
                continue
    raise RuntimeError("No adjacent fixture ports available")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True)
    parser.add_argument("--directory", required=True)
    parser.add_argument("--model-url", required=True)
    parser.add_argument("--model", required=True)
    args = parser.parse_args()
    directory = pathlib.Path(args.directory).resolve()
    directory.mkdir(mode=0o700, parents=True, exist_ok=False)
    definitions = [{"agent": {"id": "smoke", "name": "Synthetic probe", "role": "Test agent", "model": args.model},
                    "prompt": "You are running an isolated synthetic test. Do not configure integrations, access accounts or use memory. "
                              "For the first stage call manage_tasks with action=checkpoint, goal=Synthetic probe, "
                              "next=Reply READY-RESUMED without tools, evidence=No external effects, after_seconds=30. "
                              "In the resumed task reply only READY-RESUMED and do not checkpoint again.", "tools": []}]
    definitions_path = directory / "agents.json"
    definitions_path.write_text(json.dumps(definitions))
    definitions_path.chmod(0o600)
    environment = os.environ.copy()
    environment.pop("POLYMUX_MODEL_KEY", None)
    environment.pop("POLYMUX_APNS_KEY_FILE", None)
    for name in ("POLYMUX_LLAMA", "POLYMUX_EMBED_URL", "POLYMUX_EMBED_MODEL", "POLYMUX_EXTRACTOR"):
        environment.pop(name, None)
    environment.update(POLYMUX_AGENTS=str(definitions_path), POLYMUX_MODEL_URL=args.model_url, POLYMUX_MODEL=args.model)
    port = ports()
    backend = f"http://127.0.0.1:{port}"
    control = f"http://127.0.0.1:{port + 1}/host"
    log = (directory / "fixture.log").open("w")
    process = subprocess.Popen([str(pathlib.Path(args.binary).resolve()), "host", "run", "-dev", "-data", str(directory),
                                "-listen", f"127.0.0.1:{port}"], env=environment, stdout=log, stderr=log)

    def task_status():
        with sqlite3.connect(directory / "polymux.db") as database:
            row = database.execute("SELECT status FROM tasks ORDER BY rowid DESC LIMIT 1").fetchone()
            return row[0] if row else None

    try:
        wait(lambda: request(backend + "/health"), 20)
        request(backend + "/threads/smoke/messages", "POST", {"id": "synthetic-root", "text": "Run the synthetic two-stage checkpoint test now.", "attachmentIDs": []})
        wait(lambda: task_status() == "sleeping")
        request(control, "PATCH", {"enabled": False})
        wait(lambda: not request(control)["running"], 25)
        assert request(control)["enabled"] is False
        assert task_status() == "sleeping"
        request(control, "PATCH", {"enabled": True})
        wait(lambda: request(backend + "/health"), 20)
        wait(lambda: task_status() == "completed")
        messages = request(backend + "/threads/smoke/messages")
        assert any("READY-RESUMED" in (part.get("text") or "") for message in messages for part in message.get("parts", []))
        with sqlite3.connect(directory / "polymux.db") as database:
            stages = database.execute("SELECT count(*) FROM runs WHERE json_extract(data,'$.trigger')='task'").fetchone()[0]
            assert stages == 1
        notices = request(backend + "/notifications?after=0")
        assert notices["notices"]
        print(json.dumps({"checkpoint_restart": "passed", "host_off_api": "passed", "continuations": stages,
                          "resumed_reply": "passed", "notification_feed": "passed", "model": args.model}))
    finally:
        process.terminate()
        try:
            process.wait(timeout=30)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
        log.close()


if __name__ == "__main__":
    main()
