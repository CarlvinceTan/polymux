"""Verify the push checks for the exact dev commit being promoted."""

import json
import os
import subprocess
import sys
import time


WORKFLOWS = ("tests.yml", "site.yml", "skills.yml")
BASELINE = "Promotion baseline "


def api(endpoint, *args):
    return json.loads(subprocess.check_output(["gh", "api", endpoint, *args], text=True))


def run_status(runs, head):
    if not runs:
        return "pending", None
    run = max(runs, key=lambda item: (item["run_number"], item["run_attempt"]))
    if (run["head_sha"], run["head_branch"], run["event"]) != (head, "dev", "push"):
        raise ValueError("Workflow run does not belong to this dev push.")
    return (run["conclusion"] if run["status"] == "completed" else "pending"), run["id"]


def has_baseline(jobs, base):
    changes = max((job for job in jobs if job["name"] == "changes"),
                  key=lambda job: job["run_attempt"], default=None)
    return bool(changes and changes["conclusion"] == "success" and any(
        step["conclusion"] == "success" and step["name"] in {BASELINE + base, BASELINE + "full"}
        for step in changes["steps"]
    ))


def verify(repo, head, base=None, allow_missing=False, attempts=75):
    actions = f"repos/{repo}/actions"
    workflows = WORKFLOWS if base else ("tests.yml",)
    for _ in range(attempts):
        pending = []
        validated = {}
        for workflow in workflows:
            runs = api(f"{actions}/workflows/{workflow}/runs", "-X", "GET",
                       "-f", "branch=dev", "-f", "event=push", "-f", f"head_sha={head}",
                       "-F", "per_page=1")
            status, run = run_status(runs["workflow_runs"], head)
            if run is None and allow_missing:
                return False
            if status == "pending":
                pending.append(workflow)
                continue
            url = f"https://github.com/{repo}/actions/runs/{run}"
            if status != "success":
                raise ValueError(f"{workflow} did not pass ({status}): {url}")
            validated[workflow] = run
        if not pending:
            for workflow in ("tests.yml", "site.yml") if base else ():
                run = validated[workflow]
                url = f"https://github.com/{repo}/actions/runs/{run}"
                pages = api(f"{actions}/runs/{run}/jobs?filter=all&per_page=100", "--paginate", "--slurp")
                if not has_baseline((job for page in pages for job in page["jobs"]), base):
                    raise ValueError(f"{workflow} did not cover this main base. Rerun all jobs: {url}")
            print(f"Dev push checks passed for {head}.", flush=True)
            return True
        print(f"Waiting for {head[:8]}: {', '.join(pending)}", flush=True)
        time.sleep(20)
    raise ValueError("Timed out waiting for the dev push checks.")


def reusable(repo, head, base, tree):
    """Return the dev commit whose finished checks already cover this main push, else None."""
    try:
        if not base or set(base) == {"0"}:
            return None
        pulls = api(f"repos/{repo}/commits/{head}/pulls")
        for pull in pulls:
            if (pull.get("merged_at") and pull["base"]["ref"] == "main" and pull["head"]["ref"] == "dev"
                    and pull["head"]["repo"]["full_name"] == repo):
                candidate = pull["head"]["sha"]
                if api(f"repos/{repo}/git/commits/{candidate}")["tree"]["sha"] != tree:
                    continue
                # One attempt: unfinished runs fall back to a full run instead of waiting.
                if verify(repo, candidate, base, attempts=1):
                    return candidate
    except (ValueError, KeyError, TypeError, subprocess.CalledProcessError):
        pass
    return None


if __name__ == "__main__":
    try:
        reuse = sys.argv[1:]
        base = None if reuse else os.environ["BASE_SHA"]
        if not verify(os.environ["GITHUB_REPOSITORY"], os.environ["HEAD_SHA"], base,
                      allow_missing=reuse == ["--tests-if-present"]):
            with open(os.environ["GITHUB_OUTPUT"], "a") as output:
                print("missing=true", file=output)
            print("No Tests push run for this commit; verify it directly.")
    except (ValueError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error))
