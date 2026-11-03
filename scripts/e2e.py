#!/usr/bin/env python3
"""Run UI flows as fast as they safely go, on top of run-e2e.sh.

Usage: scripts/e2e.py [--jobs N] [--changed [BASE]] [--dry-run] [flow.js ...]

Flows run side by side on Cua at the pace that gets the most done: the run starts from the last good pace, adds a job
while flows keep close to their best times, stops when they slow, gives one back when they crawl, drops by a quarter
after a failure, and waits while the machine is busy. --jobs N holds it at N.
With --driver control, flows with a "// @cua" line take turns on Cua alongside. Flows with a "// @control" line always
run alone on Control. Fixture flows share an instance per batch; real-server flows each get a fresh server, so one
flow's data can't affect the next. A failure is retried alone before it counts.
"""

import argparse
import fnmatch
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parent.parent
FLOWS = ROOT / "e2e/flows"
TIMES = ROOT / ".build/e2e-times.json"
PACE = ROOT / ".build/e2e-pace"
FLOOR = 2
BATCH_SECONDS = 60.0
HOPELESS = 6
# How many times slower than their best flows run: past BUSY more jobs add little, past CROWDED they cost.
BUSY = 1.6
CROWDED = 2.2
RESULT = re.compile(r"^\s*(ok|FAIL)\s+(\S+\.js)\b(?:.*?\(([\d.]+)s)?")
CUA = re.compile(r"^// @cua", re.M)
CONTROL = re.compile(r"^// @control", re.M)
APP_SOURCES = ("apps/Polymux/", "apps/server/", "packages/", "contracts/", "Package.swift", "go.work", "scripts/run")


def matches(path, pattern):
    return fnmatch.fnmatch(path, pattern) or fnmatch.fnmatch(path, "*/" + pattern)


def affected(changed, config, available):
    """The flows a set of changed files can affect. Anything not mapped runs everything."""
    chosen = set()
    for path in changed:
        name = Path(path).name
        if path.startswith("e2e/flows/"):
            if name in available:
                chosen.add(name)
            continue
        if any(matches(path, pattern) for pattern in config["ignore"]):
            continue
        areas = [area for area in config["areas"] if any(matches(path, pattern) for pattern in area["paths"])]
        if not areas and path.startswith(APP_SOURCES):
            return sorted(available)
        for area in areas:
            for pattern in area["flows"]:
                chosen.update(flow for flow in available if fnmatch.fnmatch(flow, pattern))
    if chosen:
        chosen.update(flow for flow in config["always"] if flow in available)
    return sorted(chosen)


def lanes(names, read):
    """Splits flows into (fixture, server, cua)."""
    fixture, server, cua = [], [], []
    for name in names:
        text = read(name)
        if CUA.search(text):
            cua.append(name)
        elif text.startswith("// @fixture"):
            fixture.append(name)
        else:
            server.append(name)
    return fixture, server, cua


def batches(names, times):
    """Deals flows longest first into small groups, so a free slot can always take the next one."""
    groups, group, total = [], [], 0.0
    for name in sorted(names, key=lambda flow: (-times.get(flow, 15.0), flow)):
        group.append(name)
        total += times.get(name, 15.0)
        if total >= BATCH_SECONDS:
            groups.append(group)
            group, total = [], 0.0
    return groups + [group] if group else groups


class Pace:
    """How many groups run at once, adjusted after each stretch of results to what flows can keep up with."""

    def __init__(self, limit, ceiling, floor=FLOOR):
        self.ceiling = ceiling
        self.floor = min(floor, ceiling)
        self.limit = max(self.floor, min(limit, ceiling))
        self.peak = self.limit
        self.clean = 0
        self.stretch = 1.0

    def observe(self, took, best):
        self.stretch += (took / best - self.stretch) * 0.2

    def passed(self):
        self.clean += 1
        if self.clean < self.limit:
            return
        self.clean = 0
        if self.stretch >= CROWDED:
            self.limit = max(self.floor, self.limit - 1)
        elif self.stretch < BUSY and self.limit < self.ceiling:
            self.limit += 1
            self.peak = max(self.peak, self.limit)

    def failed(self):
        self.limit = max(self.floor, self.limit * 3 // 4)
        self.clean = 0


def sysctl(name):
    return subprocess.run(["sysctl", "-n", name], capture_output=True, text=True).stdout.strip()


def ceiling():
    """The most instances this machine should try: two gigabytes and two cores for each."""
    memory = sysctl("hw.memsize")
    gigabytes = int(memory) // 2**30 if memory.isdigit() else 8
    return max(FLOOR, min((os.cpu_count() or 2) // 2, gigabytes // 2))


def room():
    """Whether the machine can take another instance, whoever else is using it."""
    if os.getloadavg()[0] >= (os.cpu_count() or 1):
        return False
    level = sysctl("kern.memorystatus_level")
    return not level.isdigit() or int(level) >= 15


def dispatch(work, pace, execute, has_room=room, lock=None, hopeless=lambda: False, prepare=lambda: None):
    """Starts each group as soon as the pace and the machine allow, and always keeps one going."""
    lock = lock or threading.Lock()
    active = []

    def finish(group):
        clean = execute(group)
        with lock:
            pace.passed() if clean else pace.failed()
            if hopeless():
                work.clear()

    while work or active:
        active = [thread for thread in active if thread.is_alive()]
        if work and len(active) < pace.limit and (not active or has_room()):
            prepare()
            thread = threading.Thread(target=finish, args=(work.pop(0),))
            thread.start()
            active.append(thread)
        else:
            time.sleep(0.2)


def start_driver():
    """Every flow needs the Cua daemon, which stops with a logout and can be closed by macOS when it looks idle."""
    running = lambda: subprocess.run(["cua-driver", "status"], capture_output=True).returncode == 0
    if running():
        return
    subprocess.run(["defaults", "write", "com.trycua.driver", "NSDisableAutomaticTermination", "-bool", "true"], check=True)
    subprocess.run(["open", "-g", "-n", "-a", "CuaDriver", "--args", "serve"], check=True)
    for _ in range(50):
        if running():
            return
        time.sleep(0.2)
    sys.exit("Cua Driver didn't start. Open /Applications/CuaDriver.app and check its permissions.")


def changed_files(base):
    tracked = subprocess.run(["git", "diff", "--name-only", base], cwd=ROOT, capture_output=True, text=True, check=True)
    fresh = subprocess.run(["git", "ls-files", "--others", "--exclude-standard"], cwd=ROOT, capture_output=True, text=True, check=True)
    return sorted(set(tracked.stdout.split("\n") + fresh.stdout.split("\n")) - {""})


class Run:
    def __init__(self):
        self.lock = threading.Lock()
        self.results = {}
        self.times = json.loads(TIMES.read_text()) if TIMES.exists() else {}
        self.pace = None

    def execute(self, names, driver, parallel):
        environment = dict(os.environ, POLYMUX_E2E_PREBUILT="1")
        if parallel:
            environment["POLYMUX_E2E_PARALLEL"] = "1"
        paths = [str(FLOWS / name) for name in names]
        process = subprocess.Popen([str(ROOT / "scripts/run-e2e.sh"), "--driver", driver, *paths], cwd=ROOT, env=environment,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        seen, last = set(), None
        for line in process.stdout:
            found = RESULT.match(line)
            if found:
                state, name, took = found.groups()
                seen.add(name)
                last = name if state == "FAIL" else None
                with self.lock:
                    self.results[name] = state == "ok"
                    if state == "ok" and took:
                        took = float(took)
                        best = self.times.get(name, took)
                        if self.pace:
                            self.pace.observe(took, best)
                        # The best time, so a slow run can't make the next one look healthy.
                        self.times[name] = min(took, best)
                    print("  %s %s" % (state, name), flush=True)
            elif last and ('"message"' in line or '"error": "' in line):
                print("     " + line.strip()[:200], flush=True)
        process.wait()
        with self.lock:
            for name in names:
                if name not in seen:
                    # The shard stopped early; these haven't failed, they just need another go.
                    self.results[name] = False
            return all(self.results[name] for name in names)

    def hopeless(self):
        """Nothing has passed after several tries, so the setup is broken, not the flows."""
        return len(self.results) >= HOPELESS and not any(self.results.values())


def build():
    app = ROOT / ".build/e2e-app/Polymux.app"
    shutil.rmtree(app, ignore_errors=True)
    app.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([str(ROOT / "scripts/check.sh"), "macos", str(app)], cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
    server = ROOT / ".build/e2e-server"
    subprocess.run(["go", "build", "-tags", "sqlite_fts5", "-o", str(server), "./cmd/polymux"], cwd=ROOT / "apps/server",
                   check=True, stderr=subprocess.DEVNULL)


def main():
    parser = argparse.ArgumentParser(description="Run UI flows quickly.")
    parser.add_argument("--jobs", type=int, help="run exactly this many at once instead of pacing")
    parser.add_argument("--driver", choices=["control", "cua"], default="cua")
    parser.add_argument("--changed", nargs="?", const="HEAD", help="only flows affected by changes since this ref")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("flows", nargs="*")
    options = parser.parse_args()
    available = sorted(path.name for path in FLOWS.glob("*.js"))
    names = [Path(flow).name for flow in options.flows] or available
    if options.changed:
        names = affected(changed_files(options.changed), json.loads((ROOT / "e2e/areas.json").read_text()), available)
    read = lambda name: (FLOWS / name).read_text()
    control = [name for name in names if CONTROL.search(read(name))]
    fixture, server, cua = lanes([name for name in names if name not in control], read)
    if options.driver == "cua":
        for name in cua:
            (fixture if read(name).startswith("// @fixture") else server).append(name)
        cua = []
    print("%d flows: %d fixture, %d real server, %d on Cua, %d on Control" % (len(names), len(fixture), len(server), len(cua), len(control)))
    if options.dry_run or not names:
        print("\n".join(names))
        return 0
    started = time.monotonic()
    prepare = start_driver if options.driver == "cua" or cua else lambda: None
    prepare()
    build()
    print("Built in %ds" % (time.monotonic() - started), flush=True)
    run = Run()
    work = batches(fixture, run.times) + [[name] for name in server]
    work.sort(key=lambda group: -sum(run.times.get(name, 15.0) for name in group))
    saved = PACE.read_text().strip() if PACE.exists() else ""
    pace = Pace(options.jobs, options.jobs, options.jobs) if options.jobs else Pace(int(saved or FLOOR), ceiling())
    run.pace = pace

    def cua_lane():
        for name in cua:
            run.execute([name], "cua", True)

    lane = threading.Thread(target=cua_lane)
    lane.start()
    dispatch(work, pace, lambda group: run.execute(group, options.driver, True), lock=run.lock, hopeless=run.hopeless,
             prepare=prepare)
    lane.join()
    if run.hopeless():
        print("Stopped early: the first %d flows all failed, so check the setup, not the flows." % len(run.results))
        return 1
    for name in control:
        run.execute([name], "control", False)
    failed = [name for name, passed in run.results.items() if not passed]
    print("All flows done in %ds" % (time.monotonic() - started), flush=True)
    if failed:
        print("Retrying %d alone" % len(failed), flush=True)
    for name in failed:
        prepare()
        run.execute([name], "control" if name in control else "cua" if name in cua else options.driver, False)
    still = sorted(name for name, passed in run.results.items() if not passed)
    TIMES.parent.mkdir(exist_ok=True)
    TIMES.write_text(json.dumps(run.times, indent=1, sort_keys=True))
    if not options.jobs:
        PACE.write_text(str(pace.limit))
    print("Pace reached %d at once, ended at %d, with flows %.1f times their best" % (pace.peak, pace.limit, pace.stretch))
    flaky = sorted(set(failed) - set(still))
    print("%d passed, %d failed, %d flaky in %ds" % (len(run.results) - len(still), len(still), len(flaky),
                                                    time.monotonic() - started))
    for name in flaky:
        print("  flaky " + name)
    for name in still:
        print("  failed " + name)
    return 1 if still else 0


if __name__ == "__main__":
    sys.exit(main())
