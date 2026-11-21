import unittest
from unittest.mock import patch

from promotion import has_baseline, reusable, run_status, verify


def run(status="completed", conclusion="success", attempt=1, **extra):
    return {"id": 1, "run_number": 3, "run_attempt": attempt, "event": "push",
            "head_branch": "dev", "head_sha": "candidate", "status": status,
            "conclusion": conclusion, **extra}


def changes(baseline, attempt=1, conclusion="success"):
    return {"name": "changes", "run_attempt": attempt, "conclusion": conclusion,
            "steps": [{"name": f"Promotion baseline {baseline}", "conclusion": conclusion}]}


class PromotionTests(unittest.TestCase):
    def test_only_exact_dev_push_counts(self):
        for key, wrong in (("head_sha", "older"), ("head_branch", "main"), ("event", "pull_request")):
            with self.subTest(key=key), self.assertRaises(ValueError):
                run_status([{**run(), key: wrong}], "candidate")

    def test_older_success_cannot_hide_new_attempt(self):
        self.assertEqual(run_status([run(), run("in_progress", None, 2)], "candidate"), ("pending", 1))
        self.assertEqual(run_status([run(), run(conclusion="cancelled", attempt=2)], "candidate"), ("cancelled", 1))
        self.assertFalse(has_baseline([changes("base"), changes("other", 2)], "base"))
        self.assertFalse(has_baseline([changes("base"), changes("base", 2, "failure")], "base"))
        self.assertTrue(has_baseline([changes("full")], "moved"))

    def test_promotion_requires_site_and_tests_to_cover_the_same_base(self):
        def request(endpoint, *_):
            if "workflows/tests.yml" in endpoint:
                return {"workflow_runs": [{**run(), "id": 1}]}
            if "workflows/site.yml" in endpoint:
                return {"workflow_runs": [{**run(), "id": 2}]}
            if "workflows/skills.yml" in endpoint:
                return {"workflow_runs": [{**run(), "id": 3}]}
            return [{"jobs": [changes("base" if "/1/jobs" in endpoint else "other")]}]

        with patch("promotion.api", side_effect=request), self.assertRaisesRegex(ValueError, "site.yml did not cover"):
            verify("owner/repo", "candidate", "base")

    def test_scheduled_fallback_only_for_an_absent_run(self):
        with patch("promotion.api", return_value={"workflow_runs": []}):
            self.assertFalse(verify("owner/repo", "candidate", allow_missing=True))
        with patch("promotion.api", return_value={"workflow_runs": [run(conclusion="failure")]}):
            with self.assertRaisesRegex(ValueError, "did not pass"):
                verify("owner/repo", "candidate", allow_missing=True)
        with patch("promotion.api", return_value={"workflow_runs": [run()]}) as api:
            self.assertTrue(verify("owner/repo", "candidate", allow_missing=True))
            self.assertEqual(api.call_count, 1)

    def test_main_push_reuses_only_the_matching_verified_dev_commit(self):
        pull = {"merged_at": "t", "base": {"ref": "main"}, "head": {"ref": "dev", "sha": "candidate",
                                                                 "repo": {"full_name": "owner/repo"}}}

        def request(endpoint, *_):
            if endpoint.endswith("/pulls"):
                return [pull]
            if "/git/commits/" in endpoint:
                return {"tree": {"sha": "tree"}}
            return {"workflow_runs": [run()]} if "/workflows/" in endpoint else [{"jobs": [changes("base")]}]

        with patch("promotion.api", side_effect=request) as api:
            self.assertEqual(reusable("owner/repo", "merge", "base", "tree"), "candidate")
            self.assertEqual(api.call_args_list[-1].args[0].split("/")[-1], "jobs?filter=all&per_page=100")
        with patch("promotion.api", side_effect=request):
            self.assertIsNone(reusable("owner/repo", "merge", "base", "other"))  # tree differs
            self.assertIsNone(reusable("owner/repo", "merge", "moved", "tree"))  # base not covered
            self.assertIsNone(reusable("owner/repo", "merge", "0" * 40, "tree"))
        for broken in ({**pull, "merged_at": None}, {**pull, "head": {**pull["head"], "ref": "feature"}}):
            with patch("promotion.api", return_value=[broken]):
                self.assertIsNone(reusable("owner/repo", "merge", "base", "tree"))
        with patch("promotion.api", side_effect=ValueError("api")):
            self.assertIsNone(reusable("owner/repo", "merge", "base", "tree"))
        pending = {"workflow_runs": [run("in_progress", None)]}
        with patch("promotion.api", side_effect=lambda e, *_: [pull] if e.endswith("/pulls")
                   else {"tree": {"sha": "tree"}} if "/git/" in e else pending), patch("promotion.time.sleep"):
            self.assertIsNone(reusable("owner/repo", "merge", "base", "tree"))  # unfinished: full run


if __name__ == "__main__":
    unittest.main()
