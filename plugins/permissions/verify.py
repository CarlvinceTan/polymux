#!/usr/bin/env python3
"""Drive a throwaway Polymux server, with the real Permissions package, from a scripted model.

Usage: python3 plugins/permissions/verify.py
"""
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORK = tempfile.mkdtemp(prefix="polymux-permissions-")
SECRET = os.path.join(WORK, "secret.txt")
OPEN = os.path.join(WORK, "open.txt")
API, DATA, HEADERS = "", "", {}
scripts, results, consumed, probes = {}, {}, set(), []
failures = []


def text_of(message):
    content = message.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return " ".join(part.get("text", "") for part in content if isinstance(part, dict))
    return ""


class Fake(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def reply(self, payload):
        data = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path.endswith("/models"):
            return self.reply({"data": [{"id": "fake", "object": "model", "context_length": 200000}]})
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        if self.path.endswith("/mcp"):
            return self.mcp(body)
        self.chat(body)

    def mcp(self, body):
        method, ident = body.get("method"), body.get("id")
        if method == "initialize":
            return self.reply({"jsonrpc": "2.0", "id": ident, "result": {"protocolVersion": body["params"]["protocolVersion"],
                               "capabilities": {"tools": {}}, "serverInfo": {"name": "fake", "version": "1"}}})
        if method == "tools/list":
            return self.reply({"jsonrpc": "2.0", "id": ident, "result": {"tools": [{"name": "probe", "description": "Probe.",
                               "inputSchema": {"type": "object", "properties": {}}}]}})
        if method == "tools/call":
            probes.append(1)
            return self.reply({"jsonrpc": "2.0", "id": ident, "result": {"content": [{"type": "text", "text": "probe-ok"}]}})
        self.send_response(202)
        self.end_headers()

    def stream(self, delta, finish):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for payload in ({"choices": [{"index": 0, "delta": delta, "finish_reason": None}]},
                        {"choices": [{"index": 0, "delta": {}, "finish_reason": finish}]}):
            payload.update({"id": "x", "object": "chat.completion.chunk", "model": "fake"})
            self.wfile.write(b"data: " + json.dumps(payload).encode() + b"\n\n")
        self.wfile.write(b"data: [DONE]\n\n")

    def chat(self, body):
        for message in reversed(body.get("messages", [])):
            if message.get("role") == "tool":
                ident = message.get("tool_call_id", "")
                if ident in consumed:
                    break
                consumed.add(ident)
                results[ident] = text_of(message)
                break
            found = re.search(r"RUN#(\w+)#", text_of(message)) if message.get("role") == "user" else None
            if found:
                key = found.group(1)
                if key in consumed:
                    break
                consumed.add(key)
                tool, args = scripts[key]
                call = {"index": 0, "id": "call_" + key, "type": "function", "function": {"name": tool, "arguments": json.dumps(args)}}
                return self.stream({"role": "assistant", "tool_calls": [call]}, "tool_calls")
        self.stream({"role": "assistant", "content": "done"}, "stop")


def api(method, path, payload=None):
    data = None if payload is None else json.dumps(payload).encode()
    request = urllib.request.Request(API + path, data=data, method=method, headers={"Content-Type": "application/json", **HEADERS})
    with urllib.request.urlopen(request, timeout=30) as response:
        body = response.read()
    return json.loads(body) if body else None


def wait(what, ready, seconds=45):
    end = time.time() + seconds
    while time.time() < end:
        value = ready()
        if value:
            return value
        time.sleep(0.1)
    raise TimeoutError(what)


def run(chat, tool, args, mentions=None):
    key = uuid.uuid4().hex[:12]
    scripts[key] = (tool, args)
    request = {"id": str(uuid.uuid4()), "text": f"RUN#{key}#", "attachmentIDs": []}
    if mentions:
        request["mentions"] = mentions
    api("POST", f"/threads/{chat}/messages", request)
    return wait(f"{tool} in {chat}", lambda: results.get("call_" + key))


def later(tool, args):
    key = uuid.uuid4().hex[:12]
    scripts[key] = (tool, args)
    return key


def check(name, text, want, refuse=()):
    good = want in text and not any(item in text for item in refuse)
    print(("  ok   " if good else "  FAIL ") + name + ("" if good else f"\n       got: {text[:300]}"), flush=True)
    if not good:
        failures.append(name)


def start():
    """Package the plugin and start a server whose index lists only that build."""
    global API, DATA, HEADERS
    packages, index = os.path.join(WORK, "packages"), os.path.join(WORK, "index.json")
    subprocess.run([ROOT / "plugins/scripts/package.sh", "permissions", packages], check=True, stdout=subprocess.DEVNULL)
    subprocess.run([sys.executable, ROOT / "plugins/scripts/index.py", "permissions", packages, "file://" + packages,
                    "--index", index], check=True)
    log = os.path.join(WORK, "run.log")
    with open(log, "w") as out:
        server = subprocess.Popen([ROOT / "scripts/run.sh", "--server"], cwd=ROOT, stdin=subprocess.PIPE, stdout=out,
                                  stderr=subprocess.STDOUT, env={**os.environ, "POLYMUX_PLUGIN_INDEX": "file://" + index})
    found = wait("server", lambda: re.search(r"server: (http://\S+).*\n\s*data:\s+(\S+)", Path(log).read_text()), 600)
    API, DATA = found[1], found[2]
    HEADERS = {"Authorization": "Bearer " + Path(DATA, "devices/local-token").read_text().strip(),
               "X-Polymux-Client": "polymux-kit"}
    return server


def checks():
    with open(SECRET, "w") as out:
        out.write("top-secret-value\n")
    with open(OPEN, "w") as out:
        out.write("open-value\n")
    fake = ThreadingHTTPServer(("127.0.0.1", 0), Fake)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{fake.server_port}"
    provider = api("POST", "/providers", {"id": "scripted-" + uuid.uuid4().hex[:6], "type": "custom", "name": "Scripted", "enabled": True, "baseURL": base + "/v1",
                                         "models": [{"id": "fake", "tags": ["text"], "enabled": True, "contextWindow": 200000}]})
    bots = {}
    for name in ("Alpha", "Beta"):
        created = api("POST", "/agents", {"name": name, "role": "Test", "prompt": "Test bot.", "provider": provider["id"], "model": "fake"})
        bots[name] = created.get("id") or created["agent"]["id"]
    alpha, beta = bots["Alpha"], bots["Beta"]
    api("POST", "/mcp/servers", {"name": "fake", "url": base + "/mcp"})
    for bot in (alpha, beta):
        api("PATCH", f"/agents/{bot}/mcp/fake", {"enabled": True})
    api("POST", "/plugins", {"name": "permissions"})
    wait("install", lambda: next((p for p in api("GET", "/plugins")["installed"] if p["name"] == "permissions" and p["state"] == "ready"), None))
    group = api("POST", "/threads", {"title": "Pair", "agentIDs": [alpha, beta]})["id"]
    api("PATCH", "/plugins/permissions", {"chats": [alpha, group]})
    time.sleep(1)
    rules = [
        {"id": "secret", "effect": "deny", "kind": "read", "pattern": SECRET},
        {"id": "echo", "effect": "allow", "kind": "command", "pattern": "echo *"},
        {"id": "mcp", "effect": "deny", "kind": "tool", "pattern": "fake__*"},
    ]
    api("PUT", f"/plugins/permissions/api/chats/{alpha}/rules", {"rules": rules})
    api("PUT", f"/plugins/permissions/api/chats/{group}/rules", {"rules": rules})

    print("file tools", flush=True)
    check("denied read is blocked in the bound chat", run(alpha, "read", {"path": SECRET}), "permission rules blocked", ["top-secret"])
    check("the same read works in a free chat", run(beta, "read", {"path": SECRET}), "top-secret-value")
    check("an unrelated read still works", run(alpha, "read", {"path": OPEN}), "open-value")

    print("commands", flush=True)
    check("allow-listed command runs", run(alpha, "bash", {"command": "echo hello-there"}), "hello-there")
    check("a second command after ; is blocked", run(alpha, "bash", {"command": f"echo hi; cat {SECRET}"}), "permission rules blocked", ["top-secret"])
    check("command substitution is blocked", run(alpha, "bash", {"command": f"echo $(cat {SECRET})"}), "permission rules blocked", ["top-secret"])
    check("the free chat is unrestricted", run(beta, "bash", {"command": f"cat {SECRET}"}), "top-secret-value")

    print("MCP inside codemode", flush=True)
    code = {"code": "return await tools.fake__probe({})"}
    check("free chat reaches the MCP tool", run(beta, "codemode", code), "probe-ok")
    before = len(probes)
    check("bound chat is refused", run(alpha, "codemode", code), "permission rules blocked", ["probe-ok"])
    check("the MCP server never saw the refused call", "called" if len(probes) > before else "not called", "not called")

    print("group chat", flush=True)
    check("group rules bind its bots", run(group, "read", {"path": SECRET}, mentions=[alpha]), "permission rules blocked", ["top-secret"])

    print("work handed elsewhere", flush=True)
    key = later("read", {"path": SECRET})
    run(alpha, "task", {"action": "run", "agent_id": beta, "prompt": f"RUN#{key}#"})
    check("a child task on another bot inherits the rules", wait("child", lambda: results.get("call_" + key)), "permission rules blocked", ["top-secret"])
    key = later("read", {"path": SECRET})
    run(alpha, "message_agent", {"agent_id": beta, "intent": "request", "message": f"RUN#{key}#"})
    check("a peer request inherits the rules", wait("peer", lambda: results.get("call_" + key)), "permission rules blocked", ["top-secret"])
    key = later("read", {"path": SECRET})
    run(alpha, "create_task", {"name": "Leak", "description": f"RUN#{key}#"})
    check("a task thread inherits the rules", wait("task thread", lambda: results.get("call_" + key)), "permission rules blocked", ["top-secret"])

    print("the agent can't lift its own rules", flush=True)
    address = API + f"/plugins/permissions/api/chats/{alpha}/rules"
    attempt = {"url": address, "method": "PUT", "headers": HEADERS, "body": '{"rules":[]}'}
    check("http_request can't use a stolen local token", run(alpha, "http_request", attempt), "API request failed")
    probe = {"url": API + "/plugins", "headers": HEADERS}
    check("the same token does reach the API from a free chat", run(beta, "http_request", probe), "permissions")
    check("rules survived that attempt", json.dumps(api("GET", f"/plugins/permissions/api/chats/{alpha}/rules")), SECRET)
    folder = os.path.join(DATA, "plugins/permissions")
    check("file tools can't reach the plugin's folder", run(alpha, "write", {"path": folder + "/data/rules.json", "content": "{}"}), "permission rules blocked")

    print("a stopped service keeps the chat blocked", flush=True)
    # The full path keeps this to this server's service, not another run's.
    pid = int(subprocess.check_output(["pgrep", "-f", folder + "/package/bin/polymux-permissions"], text=True).split()[0])
    os.kill(pid, signal.SIGSTOP)
    try:
        check("a hung service blocks the call", run(alpha, "read", {"path": OPEN}), "isn't running", ["open-value"])
        check("a free chat doesn't depend on it", run(beta, "read", {"path": OPEN}), "open-value")
    finally:
        os.kill(pid, signal.SIGCONT)
    os.kill(pid, signal.SIGTERM)

    def restarted():
        text = run(alpha, "read", {"path": SECRET})
        return text if "permission rules blocked" in text else None
    check("rules are back after a restart", wait("restart", restarted, 60), "permission rules blocked", ["top-secret"])
    print("FAILED: " + ", ".join(failures) if failures else "ALL PASSED", flush=True)
    return 1 if failures else 0


def main():
    server = None
    try:
        server = start()
        return checks()
    except urllib.error.HTTPError as error:
        print("HTTP", error.code, error.url, error.read()[:400], flush=True)
        return 2
    finally:
        if server:
            server.terminate()
            server.wait()
        shutil.rmtree(WORK, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
