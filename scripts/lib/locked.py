"""Run a command under an OS lock, retaining it until canceled children exit."""

import fcntl
import os
from pathlib import Path
import signal
import subprocess
import sys
import time


def run(path, command):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    canceled = 0
    child = None

    def cancel(number, _frame):
        nonlocal canceled
        canceled = number
        if child is not None:
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass

    previous = {number: signal.signal(number, cancel)
                for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
    try:
        # Keep the inode: unlinking a lock lets new callers lock a different file.
        with path.open("a") as lock:
            while not canceled:
                try:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    time.sleep(0.05)
            if canceled:
                return 128 + canceled
            child = subprocess.Popen(command, start_new_session=True, pass_fds=(lock.fileno(),))
            deadline = None
            while True:
                status = child.poll()
                if canceled:
                    if deadline is None:
                        deadline = time.monotonic() + 3
                        cancel(canceled, None)
                    try:
                        os.killpg(child.pid, 0)
                    except ProcessLookupError:
                        break
                    if time.monotonic() >= deadline:
                        try:
                            os.killpg(child.pid, signal.SIGKILL)
                        except ProcessLookupError:
                            pass
                        child.wait()
                        break
                elif status is not None:
                    return status if status >= 0 else 128 - status
                time.sleep(0.05)
            return 128 + canceled
    finally:
        for number, handler in previous.items():
            signal.signal(number, handler)


if __name__ == "__main__":
    if len(sys.argv) < 4 or sys.argv[2] != "--":
        raise SystemExit("usage: locked.py LOCK -- COMMAND [ARG...]")
    raise SystemExit(run(sys.argv[1], sys.argv[3:]))
