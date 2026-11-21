#!/usr/bin/env python3
"""Drive a throwaway Polymux server, with the real Computer Use and Context packages, from a scripted model.

Needs a Mac with CuaDriver.app running and a Finder window open. Usage: python3 plugins/computer-use/verify.py
"""
import json
import os
import re
import shutil
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
WORK = tempfile.mkdtemp(prefix="polymux-computer-use-")
PLUGINS = ("computer-use", "context")
API, HEADERS = "", {}
scripts, results, consumed, offered = {}, {}, set(), []
failures = []


def parts(message):
    content = message.get("content")
    return content if isinstance(content, list) else [{"type": "text", "text": content or ""}]


def text_of(message):
    return " ".join(part.get("text", "") for part in parts(message) if isinstance(part, dict))


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
            return self.reply({"data": [{"id": "fake", "object": "model", "context_length": 200000, "tags": ["text", "vision"]}]})
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        self.chat(body)

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
        offered.append({tool["function"]["name"]: tool["function"].get("description", "") for tool in body.get("tools", [])})
        messages = body.get("messages", [])
        for n, message in enumerate(reversed(messages)):
            if message.get("role") == "tool":
                ident = message.get("tool_call_id", "")
                if ident in consumed:
                    break
                consumed.add(ident)
                # Pictures can follow the tool message as a user message, depending on the provider shape.
                after = messages[len(messages) - n:]
                kinds = [part.get("type") for m in [message, *after] for part in parts(m) if isinstance(part, dict)]
                results[ident] = {"text": text_of(message), "kinds": kinds}
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


def wait(what, ready, seconds=60):
    end = time.time() + seconds
    while time.time() < end:
        value = ready()
        if value:
            return value
        time.sleep(0.1)
    raise TimeoutError(what)


def code(chat, script):
    key = uuid.uuid4().hex[:12]
    scripts[key] = ("codemode", {"code": script})
    api("POST", f"/threads/{chat}/messages", {"id": str(uuid.uuid4()), "text": f"RUN#{key}#", "attachmentIDs": []})
    return wait(f"codemode in {chat}", lambda: results.get("call_" + key), 120)


def check(name, good, detail=""):
    print(("  ok   " if good else "  FAIL ") + name + ("" if good else f"\n       got: {str(detail)[:400]}"), flush=True)
    if not good:
        failures.append(name)


def start():
    """Package both plugins and start a server whose index lists only those builds."""
    global API, HEADERS
    packages, index = os.path.join(WORK, "packages"), os.path.join(WORK, "index.json")
    for name in PLUGINS:
        subprocess.run([ROOT / "plugins/scripts/package.sh", name, packages], check=True, stdout=subprocess.DEVNULL)
        subprocess.run([sys.executable, ROOT / "plugins/scripts/index.py", name, packages, "file://" + packages,
                        "--index", index], check=True, stdout=subprocess.DEVNULL)
    log = os.path.join(WORK, "run.log")
    with open(log, "w") as out:
        server = subprocess.Popen([ROOT / "scripts/run.sh", "--server"], cwd=ROOT, stdin=subprocess.PIPE, stdout=out,
                                  stderr=subprocess.STDOUT, env={**os.environ, "POLYMUX_PLUGIN_INDEX": "file://" + index})
    found = wait("server", lambda: re.search(r"server: (http://\S+).*\n\s*data:\s+(\S+)", Path(log).read_text()), 600)
    API = found[1]
    HEADERS = {"Authorization": "Bearer " + Path(found[2], "devices/local-token").read_text().strip(),
               "X-Polymux-Client": "polymux-kit"}
    return server


def checks():
    fake = ThreadingHTTPServer(("127.0.0.1", 0), Fake)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{fake.server_port}"
    provider = api("POST", "/providers", {"id": "scripted-" + uuid.uuid4().hex[:6], "type": "custom", "name": "Scripted", "enabled": True,
                                         "baseURL": base + "/v1", "models": [{"id": "fake", "tags": ["text", "vision"], "enabled": True, "contextWindow": 200000}]})
    bots = {}
    for name in ("Alpha", "Beta"):
        created = api("POST", "/agents", {"name": name, "role": "Test", "prompt": "Test bot.", "provider": provider["id"], "model": "fake"})
        bots[name] = created.get("id") or created["agent"]["id"]
    alpha, beta = bots["Alpha"], bots["Beta"]
    for name in PLUGINS:
        api("POST", "/plugins", {"name": name})
        wait("install " + name, lambda: next((p for p in api("GET", "/plugins")["installed"] if p["name"] == name and p["state"] == "ready"), None))
        api("PATCH", f"/plugins/{name}", {"chats": [alpha]})
    time.sleep(1)

    print("codemode", flush=True)
    offered.clear()
    summary = code(alpha, "text((await tools.context__summary({})).content[0].text)")
    tools = offered[0] if offered else {}
    check("plugin tools aren't offered directly", not any("__" in name for name in tools), list(tools))
    check("codemode lists the plugin tools", "computer_use__call" in tools.get("codemode", "") and "context__summary" in tools.get("codemode", ""), tools.get("codemode", "")[:300])
    offered.clear()
    free = code(beta, "text(await tools.context__summary({}))")
    check("a chat without the plugins doesn't get them", "Apps:" not in free["text"] and not any("context__summary" in d for d in offered[0].values()), free["text"])

    print("context", flush=True)
    missing = [key for key in ("Front:", "Apps:", "Windows:", "Tabs:", "network ") if key not in summary["text"]]
    check("summary reads apps, windows, tabs and network", not missing, missing)
    check("network reads the Wi-Fi details, which take several seconds", "network " in code(alpha, "text((await tools.context__network({})).content[0].text)")["text"], "")
    check("windows filter by app", "Finder" in code(alpha, "text(await tools.context__windows({app: 'Finder'}))")["text"], "")
    check("installed apps are listed", "Safari" in code(alpha, "text(await tools.context__apps({installed: true}))")["text"], "")

    print("computer use", flush=True)
    listed = code(alpha, "text(await tools.computer_use__describe({}))")
    check("describe lists the driver's tools", "get_window_state" in listed["text"] and "set_config" not in listed["text"], listed["text"][:300])
    size = code(alpha, "const r = await tools.computer_use__call({tool: 'get_screen_size'}); text(JSON.stringify(r.structuredContent))")
    check("structured content reaches the script", '"width"' in size["text"], size["text"])
    shot = code(alpha, """
const listed = await tools.computer_use__call({tool: 'list_windows', args: {app: 'Finder'}});
const w = listed.structuredContent.windows.find(w => w.is_on_screen && w.bounds.width > 200 && w.bounds.height > 200);
const s = await tools.computer_use__call({tool: 'get_window_state', args: {pid: w.pid, window_id: w.window_id, include_accessibility_tree: false, max_image_dimension: 400}});
const picture = s.content.find(c => c.type === 'image');
if (picture) image(picture); else text(JSON.stringify(s.content));
""")
    check("a screenshot reaches the model as a picture", any(kind in ("image", "image_url") for kind in shot["kinds"]), shot)
    blocked = code(alpha, "text((await tools.computer_use__call({tool: 'set_config', args: {}})).content[0].text)")
    check("tools that change the driver are refused", "doesn't offer it" in blocked["text"], blocked["text"])

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
