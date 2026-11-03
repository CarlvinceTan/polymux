"""Check which flows scripts/e2e.py picks and how it splits them, without running any."""

import importlib.util
from pathlib import Path
import sys
import threading
import time
import unittest

spec = importlib.util.spec_from_file_location("e2e_plan", Path(__file__).with_name("e2e.py"))
plan = importlib.util.module_from_spec(spec)
sys.modules["e2e_plan"] = plan
spec.loader.exec_module(plan)

CONFIG = {
    "ignore": ["docs/**", "*.md"],
    "always": ["smoke.js"],
    "areas": [{"paths": ["*/Views/ChatPane.swift"], "flows": ["chat.js", "fixture-chat*"]}],
}
AVAILABLE = ["chat.js", "fixture-chat-place.js", "fixture-chat.js", "other.js", "smoke.js"]


class PlanTests(unittest.TestCase):
    def test_a_mapped_file_runs_its_area_and_the_smoke_check(self):
        chosen = plan.affected(["apps/Polymux/Sources/Views/ChatPane.swift"], CONFIG, AVAILABLE)
        self.assertEqual(chosen, ["chat.js", "fixture-chat-place.js", "fixture-chat.js", "smoke.js"])

    def test_an_unmapped_app_file_runs_everything(self):
        self.assertEqual(plan.affected(["apps/Polymux/Sources/Views/Unknown.swift"], CONFIG, AVAILABLE), AVAILABLE)

    def test_docs_run_nothing(self):
        self.assertEqual(plan.affected(["docs/control.md", "README.md"], CONFIG, AVAILABLE), [])

    def test_a_changed_flow_runs_itself(self):
        self.assertEqual(plan.affected(["e2e/flows/other.js"], CONFIG, AVAILABLE), ["other.js", "smoke.js"])

    def test_cua_flows_are_kept_apart(self):
        texts = {"a.js": "// @fixture --x\n", "b.js": "await app.click(1);\n", "c.js": "// @fixture --x\n// @cua\nawait 1;\n",
                 "d.js": "// @cua\nawait 1;\n"}
        self.assertEqual(plan.lanes(sorted(texts), texts.get), (["a.js"], ["b.js"], ["c.js", "d.js"]))

    def test_batches_stay_short_so_slots_free_up_often(self):
        times = {"a": 70, "b": 30, "c": 30, "d": 10, "e": 10}
        self.assertEqual(plan.batches(list(times), times), [["a"], ["b", "c"], ["d", "e"]])

    def test_pace_holds_when_flows_slow_down(self):
        pace = plan.Pace(4, 8)
        pace.stretch = plan.BUSY + 0.1
        for _ in range(20):
            pace.passed()
        self.assertEqual(pace.limit, 4)

    def test_pace_gives_a_job_back_when_flows_crawl(self):
        pace = plan.Pace(6, 8)
        pace.stretch = plan.CROWDED + 0.1
        for _ in range(6):
            pace.passed()
        self.assertEqual(pace.limit, 5)

    def test_slow_flows_raise_the_stretch_gradually(self):
        pace = plan.Pace(2, 8)
        for _ in range(30):
            pace.observe(30, 10)
        self.assertAlmostEqual(pace.stretch, 3, places=1)

    def test_pace_rises_after_a_clean_stretch(self):
        pace = plan.Pace(2, 6)
        pace.passed()
        self.assertEqual(pace.limit, 2)
        pace.passed()
        self.assertEqual(pace.limit, 3)

    def test_pace_drops_after_a_failure_but_stays_on_the_floor(self):
        pace = plan.Pace(8, 8)
        pace.failed()
        self.assertEqual(pace.limit, 6)
        for _ in range(6):
            pace.failed()
        self.assertEqual(pace.limit, plan.FLOOR)

    def test_pace_never_passes_its_ceiling(self):
        pace = plan.Pace(9, 4)
        for _ in range(20):
            pace.passed()
        self.assertEqual(pace.limit, 4)

    def test_fixed_jobs_never_move(self):
        pace = plan.Pace(3, 3, 3)
        pace.failed()
        for _ in range(9):
            pace.passed()
        self.assertEqual((pace.limit, pace.peak), (3, 3))

    def run_groups(self, groups, pace, execute, has_room=lambda: True):
        seen, lock = [], threading.Lock()

        def run(group):
            with lock:
                seen.append(group)
            return execute(group)

        plan.dispatch(list(groups), pace, run, has_room)
        return seen

    def test_dispatch_never_runs_more_than_the_pace(self):
        lock = threading.Lock()
        state = {"now": 0, "most": 0}

        def execute(_):
            with lock:
                state["now"] += 1
                state["most"] = max(state["most"], state["now"])
            time.sleep(0.05)
            with lock:
                state["now"] -= 1
            return True

        pace = plan.Pace(2, 2)
        seen = self.run_groups([[str(n)] for n in range(6)], pace, execute)
        self.assertEqual(len(seen), 6)
        self.assertEqual(state["most"], 2)

    def test_a_busy_machine_slows_the_run_without_stalling_it(self):
        seen = self.run_groups([["a"], ["b"], ["c"]], plan.Pace(6, 6), lambda _: True, has_room=lambda: False)
        self.assertEqual(len(seen), 3)

    def test_a_hopeless_run_stops_instead_of_working_through_the_queue(self):
        run = plan.Run()
        work = [["%d.js" % n] for n in range(20)]

        def execute(group):
            run.results[group[0]] = False
            return False

        plan.dispatch(work, plan.Pace(1, 1, 1), execute, lambda: True, hopeless=run.hopeless)
        self.assertEqual(len(run.results), plan.HOPELESS)

    def test_one_pass_means_failures_are_about_the_flows(self):
        run = plan.Run()
        run.results = {"a.js": True, **{"%d.js" % n: False for n in range(20)}}
        self.assertFalse(run.hopeless())

    def test_the_driver_is_checked_before_every_group(self):
        checks = []
        plan.dispatch([["a"], ["b"], ["c"]], plan.Pace(2, 2), lambda _: True, lambda: True, prepare=lambda: checks.append(1))
        self.assertEqual(len(checks), 3)

    def test_a_failure_lowers_the_pace_of_the_run(self):
        pace = plan.Pace(4, 4)
        self.run_groups([["a"]], pace, lambda _: False)
        self.assertEqual(pace.limit, 3)


if __name__ == "__main__":
    unittest.main()
