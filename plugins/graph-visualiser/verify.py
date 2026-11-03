#!/usr/bin/env python3
"""Drive a throwaway Polymux server, with the real Graph Visualiser package, from a scripted model.

GitHub goes through a stand-in gh, so nothing here reaches a real account.

Usage: python3 plugins/graph-visualiser/verify.py
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
WORK = tempfile.mkdtemp(prefix="polymux-graph-")
NAME = "graph-visualiser"
REPORT, READ = "graph_visualiser__report_work", "graph_visualiser__read_work"
API, DATA, HEADERS = "", "", {}
GH_LOG = os.path.join(WORK, "gh.log")
STAND_IN = f"""#!/bin/sh
echo "$*" >> {GH_LOG}
case "$1 $2" in
  "pr list") echo '[{{"number":7,"title":"Fix login","headRefName":"fix","isDraft":false}}]' ;;
  "run list") echo '[{{"databaseId":9,"name":"CI","status":"completed","conclusion":"failure","headBranch":"dev"}}]' ;;
  *) exit 1 ;;
esac
"""
scripts, results, consumed = {}, {}, set()
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

    def do_GET(self):
        if self.path.endswith("/models"):
            data = json.dumps({"data": [{"id": "fake", "object": "model", "context_length": 200000}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        self.send_response(404)
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

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
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


def run(chat, tool, args):
    key = uuid.uuid4().hex[:12]
    scripts[key] = (tool, args)
    api("POST", f"/threads/{chat}/messages", {"id": str(uuid.uuid4()), "text": f"RUN#{key}#", "attachmentIDs": []})
    return wait(f"{tool} in {chat}", lambda: results.get("call_" + key))


def check(name, good, detail=""):
    print(("  ok   " if good else "  FAIL ") + name + ("" if good else f"\n       got: {str(detail)[:300]}"), flush=True)
    if not good:
        failures.append(name)


def start():
    """Package the plugin and start a server whose index lists only that build."""
    global API, DATA, HEADERS
    packages, index = os.path.join(WORK, "packages"), os.path.join(WORK, "index.json")
    subprocess.run([ROOT / "plugins/scripts/package.sh", NAME, packages], check=True, stdout=subprocess.DEVNULL)
    subprocess.run([sys.executable, ROOT / "plugins/scripts/index.py", NAME, packages, "file://" + packages, "--index", index], check=True)
    tools = os.path.join(WORK, "tools")
    os.mkdir(tools)
    Path(tools, "gh").write_text(STAND_IN)
    os.chmod(os.path.join(tools, "gh"), 0o755)
    log = os.path.join(WORK, "run.log")
    # With no SHELL, the server keeps this PATH instead of putting the login shell's first.
    env = {**os.environ, "POLYMUX_PLUGIN_INDEX": "file://" + index, "PATH": tools + os.pathsep + os.environ["PATH"], "SHELL": ""}
    with open(log, "w") as out:
        server = subprocess.Popen([ROOT / "scripts/run.sh", "--server"], cwd=ROOT, stdin=subprocess.PIPE, stdout=out,
                                  stderr=subprocess.STDOUT, env=env)
    found = wait("server", lambda: re.search(r"server: (http://\S+).*\n\s*data:\s+(\S+)", Path(log).read_text()), 600)
    API, DATA = found[1], found[2]
    HEADERS = {"Authorization": "Bearer " + Path(DATA, "devices/local-token").read_text().strip(), "X-Polymux-Client": "polymux-kit"}
    return server


def repository():
    path = os.path.join(WORK, "polymux")
    os.mkdir(path)
    def git(*args):
        subprocess.run(["git", "-C", path, "-c", "user.name=T", "-c", "user.email=t@example.com", *args], check=True, capture_output=True)
    git("init", "-b", "dev")
    Path(path, "a.txt").write_text("a")
    git("add", ".")
    git("commit", "-m", "First")
    Path(path, "b.txt").write_text("b")
    return path


def checks():
    fake = ThreadingHTTPServer(("127.0.0.1", 0), Fake)
    threading.Thread(target=fake.serve_forever, daemon=True).start()
    provider = api("POST", "/providers", {"id": "scripted-" + uuid.uuid4().hex[:6], "type": "custom", "name": "Scripted", "enabled": True,
                                         "baseURL": f"http://127.0.0.1:{fake.server_port}/v1",
                                         "models": [{"id": "fake", "tags": ["text"], "enabled": True, "contextWindow": 200000}]})
    bots = {}
    for name in ("Linus", "Nova"):
        created = api("POST", "/agents", {"name": name, "role": "Test", "prompt": "Test bot.", "provider": provider["id"], "model": "fake"})
        bots[name] = created.get("id") or created["agent"]["id"]
    linus, nova = bots["Linus"], bots["Nova"]

    print("installing", flush=True)
    market = api("GET", "/plugins/market")
    check("the marketplace lists it as both", market and market[0]["kind"] == "both", market)
    api("POST", "/plugins", {"name": NAME})
    plugin = wait("install", lambda: next((p for p in api("GET", "/plugins")["installed"] if p["name"] == NAME and p["state"] == "ready"), None))
    check("it's called Graph Visualiser", plugin["title"] == "Graph Visualiser", plugin)
    check("it installs as both", plugin["kind"] == "both" and plugin["surface"] == NAME, plugin)
    layout = api("GET", "/plugins")["layout"]
    check("it gets a chat button, not a switcher tab", NAME not in layout["switcher"] and [NAME] in layout["buttons"], layout)
    check("it takes a chat list like a Scoped plugin", api("PATCH", f"/plugins/{NAME}", {"chats": [linus]})["chats"] == [linus])

    print("repositories", flush=True)
    path = repository()
    api("PUT", f"/plugins/{NAME}/settings", {"repositories": path, "github": False})
    def scanned():
        try:
            graph = api("GET", f"/plugins/{NAME}/api/graph")
        except urllib.error.HTTPError:
            return None
        return next((n for n in graph["nodes"] if n.get("kind") == "repo"), None)
    repo = wait("repository scan", scanned, 40)
    check("a repository shows its branch", repo["title"] == "polymux" and repo["subtitle"].startswith("dev at "), repo)
    check("uncommitted work shows on it", repo["blocks"][0]["text"] == "1 change", repo)
    check("it sits on the server's machine", repo["parent"] == "device:server", repo)

    print("GitHub", flush=True)
    subprocess.run(["git", "-C", path, "remote", "add", "origin", "git@github.com:example/polymux.git"], check=True)
    api("PUT", f"/plugins/{NAME}/settings", {"repositories": path, "github": True})
    def hosted():
        try:
            nodes = api("GET", f"/plugins/{NAME}/api/graph")["nodes"]
        except urllib.error.HTTPError:
            return None
        return nodes if any(n.get("kind") == "check" for n in nodes) else None
    nodes = wait("GitHub scan", hosted, 40)
    pull = next((n for n in nodes if n.get("kind") == "pull"), {})
    check("an open pull request shows", pull.get("title") == "Fix login" and pull.get("parent") == "github:example/polymux", pull)
    run_node = next(n for n in nodes if n.get("kind") == "check")
    check("a failed run shows as failed", run_node["title"] == "CI" and run_node["state"] == "failed", run_node)
    verbs = {" ".join(line.split()[:2]) for line in Path(GH_LOG).read_text().splitlines()}
    check("it only lists from GitHub", verbs == {"pr list", "run list"}, verbs)
    api("PUT", f"/plugins/{NAME}/settings", {"repositories": path, "github": False})
    def offline():
        try:
            nodes = api("GET", f"/plugins/{NAME}/api/graph")["nodes"]
        except urllib.error.HTTPError:
            return False
        return not any(n.get("kind") == "check" for n in nodes)
    check("turning GitHub off removes it", wait("GitHub off", offline, 40))

    print("reporting", flush=True)
    report = {"nodes": [{"id": "claude-1", "title": "Claude Code", "parent": "me", "kind": "agent", "state": "working"},
                        {"id": "safari", "title": "Safari", "parent": "claude-1", "kind": "app"},
                        {"id": "tab-1", "title": "Docs", "parent": "safari", "kind": "tab"}],
             "wires": [{"from": "me", "to": "claude-1", "label": "Fix the bug", "state": "active"}, {"from": "me", "to": repo["id"], "label": "Works in"}]}
    reported = run(linus, REPORT, report)
    check("a bot reports work through its tool", "Reported 3 nodes" in reported, reported)
    graph = api("GET", f"/plugins/{NAME}/api/graph")
    claude = next(n for n in graph["nodes"] if n["id"] == "claude-1")
    check("me becomes the bot, and the chat tags it", claude["parent"] == f"agent:{linus}" and claude["chats"] == [linus], claude)
    check("wires come through", any(w["to"] == repo["id"] and w["from"] == f"agent:{linus}" for w in graph["wires"]), graph["wires"])
    listed = run(nova, READ, {})
    check("a bot in another chat can read IDs to link to", repo["id"] in listed and "claude-1" in listed, listed)
    refused = run(linus, REPORT, {"nodes": [{"id": "loop", "title": "Loop", "parent": "loop"}]})
    check("a bad report tells the bot why", "own parent" in refused, refused)
    outside = {"nodes": [{"id": "outside", "title": "Outside worker"}]}
    check("an outside process can report through the API", api("POST", f"/plugins/{NAME}/api/report", outside) == {})
    check("what it reported arrives", any(n["id"] == "outside" for n in api("GET", f"/plugins/{NAME}/api/graph")["nodes"]))

    print("Polymux's own activity", flush=True)
    secret = "output-that-stays-out"
    run(linus, "bash", {"command": f"echo {secret}"})
    run(linus, "message_agent", {"agent_id": nova, "intent": "request", "message": "Please look at this."})
    work = wait("delivery", lambda: (lambda w: w if w["deliveries"] else None)(api("GET", "/work")))
    check("handoffs are listed across chats", work["deliveries"][0]["targetAgentID"] == nova, work["deliveries"])
    tools = [tool for item in work["runs"] for tool in item.get("tools") or [] if tool["name"] == "bash"]
    check("tool use is listed", tools and secret in tools[0].get("summary", ""), tools)
    check("tool output stays out", secret not in json.dumps([tool.get("output") for tool in tools]))
    check("the build is reported", isinstance(work["build"], str))

    print("a stopped service", flush=True)
    folder = os.path.join(DATA, "plugins", NAME)
    pid = int(subprocess.check_output(["pgrep", "-f", folder + "/package/bin/polymux-graph-visualiser"], text=True).split()[0])
    os.kill(pid, signal.SIGSTOP)
    try:
        answer = run(linus, REPORT, {"nodes": [{"id": "lost", "title": "Lost"}]})
        check("the bot hears it couldn't report", "couldn't answer" in answer, answer)
        check("the chat keeps working", "hello" in run(linus, "bash", {"command": "echo hello"}))
        check("Polymux's own activity doesn't depend on it", api("GET", "/work")["runs"])
    finally:
        os.kill(pid, signal.SIGCONT)
    os.kill(pid, signal.SIGTERM)
    def recovered():
        try:
            nodes = api("GET", f"/plugins/{NAME}/api/graph")["nodes"]
        except urllib.error.HTTPError:
            return None
        return nodes if any(n["id"] == "claude-1" for n in nodes) else None
    check("reports survive a restart", wait("restart", recovered, 60))

    print("removing", flush=True)
    api("DELETE", f"/plugins/{NAME}")
    check("uninstalling removes its data", not os.path.exists(folder))
    check("the button goes with it", NAME not in json.dumps(api("GET", "/plugins")["layout"]))
    check("Polymux's activity stays", api("GET", "/work")["runs"])
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
