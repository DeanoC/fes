import json
from datetime import datetime, timezone
from pathlib import Path
import re
import subprocess
import unittest
from unittest.mock import patch
from scripts.coordination import findings, gh_list, records, report

NOW = datetime(2026, 10, 1, tzinfo=timezone.utc)
ACK = dict(runner="codex-1", vendor="Codex", host="mac", worktree="/fes",
           branch="work", base="a" * 40)


def issue(state="working", closed=False, acceptance=False):
    labels = [{"name": "task:" + state}]
    if acceptance:
        labels.append({"name": "kind:acceptance"})
    return dict(number=1, title="Title | <script>\nnext", state="closed" if closed else "open",
                labels=labels)


def comment(kind, data, at="2026-09-27T00:00:00Z"):
    return dict(body="FES-TASK-" + kind + "\n```json\n" + json.dumps(data) + "\n```",
                created_at=at)


class CoordinationTest(unittest.TestCase):
    def test_exact_documented_ack_example_parses_as_a_complete_claim(self):
        guide = (Path(__file__).resolve().parents[1] / "docs/agent-workflow.md").read_text()
        example = re.search(r"````text\n(FES-TASK-ACK\n.*?)\n````", guide, re.S)
        self.assertIsNotNone(example)
        note = dict(body=example[1], created_at="2026-09-30T23:00:00Z")
        state, runner, flags = findings(issue(), [note], [], NOW, 24)
        self.assertEqual(state, "working")
        self.assertNotEqual(runner, "unclaimed")
        self.assertEqual(flags, [])

    def test_malformed_records_are_diagnosed_and_valid_neighbors_survive(self):
        for invalid, message in (("{broken", "invalid JSON"),
                                 ("[]", "JSON must be an object"),
                                 ("null", "JSON must be an object")):
            with self.subTest(invalid=invalid):
                note = dict(body="FES-TASK-UPDATE\n```json\n" + invalid + "\n```\n" +
                            comment("ACK", ACK)["body"], created_at="2026-09-30T23:00:00Z",
                            html_url="https://github.com/DeanoC/fes/issues/1#issuecomment-10")
                _, runner, flags = findings(issue(), [note], [], NOW, 24)
                self.assertEqual(runner, ACK["runner"])
                self.assertTrue(any("unparseable FES-TASK-UPDATE" in flag and message in flag
                                    and note["html_url"] in flag for flag in flags))

    def test_documented_bot_verdict_link_records_a_review(self):
        guide = (Path(__file__).resolve().parents[1] / "docs/agent-workflow.md").read_text()
        example = re.search(r"````text\n(FES-TASK-REVIEW\n.*?)\n````", guide, re.S)
        self.assertIsNotNone(example)
        notes = [comment("ACK", ACK),
                 comment("UPDATE", dict(runner=ACK["runner"], pr="https://github.com/DeanoC/fes/pull/123")),
                 dict(body=example[1], created_at="2026-09-30T23:00:00Z")]
        self.assertEqual(findings(issue("validation"), notes, [], NOW, 24)[2], [])

    def test_missing_json_fence_and_old_documented_layout_get_a_diagnostic(self):
        for body in ("FES-TASK-ACK\n{}", "```text\nFES-TASK-ACK\n```\n```json\n{}\n```"):
            with self.subTest(body=body):
                _, errors = records([dict(body=body, created_at="2026-09-30T23:00:00Z")])
                self.assertTrue(any("unparseable FES-TASK-ACK" in error for error in errors))

    def test_ready_without_ack_is_information_not_a_warning(self):
        self.assertEqual(findings(issue("ready"), [], [], NOW, 24)[2], [])
        with patch("scripts.coordination.gh_list", return_value=[]):
            text = report("DeanoC/fes", 1, [issue("ready")], NOW, 24)
        self.assertIn("Ready to claim (informational): [#1]", text)
        self.assertNotIn("no runner acknowledgement", text)

    def test_ready_with_unmet_dependency_is_not_a_dispatch_candidate(self):
        with patch("scripts.coordination.gh_list", side_effect=[[], [dict(number=2, state="open")]]):
            text = report("DeanoC/fes", 1, [issue("ready")], NOW, 24)
        self.assertIn("Ready to claim (informational): none.", text)
        self.assertIn("state conflicts with unmet dependencies", text)

    def test_a_thirty_hour_claim_is_stale_at_twenty_four_hours(self):
        notes = [comment("ACK", ACK, "2026-09-29T18:00:00Z")]
        self.assertTrue(any("older than 24 hours" in flag for flag in findings(issue(), notes, [], NOW, 24)[2]))
        self.assertFalse(any("older than" in flag for flag in findings(issue(), notes, [], NOW, 48)[2]))

    def test_another_runner_cannot_refresh_a_stale_claim(self):
        notes = [comment("ACK", ACK),
                 comment("UPDATE", dict(runner="other", state="working"), "2026-09-30T23:59:00Z")]
        flags = findings(issue(), notes, [], NOW, 48)[2]
        self.assertTrue(any("older than" in flag for flag in flags))
        notes.append(comment("UPDATE", dict(runner=ACK["runner"]), "2026-09-30T23:59:00Z"))
        self.assertFalse(any("older than" in flag for flag in findings(issue(), notes, [], NOW, 48)[2]))

    def test_unmet_dependency_overrides_ready_projection(self):
        flags = findings(issue("ready"), [], [dict(number=2, state="open")], NOW, 48)[2]
        self.assertIn("state conflicts with unmet dependencies", flags)
        self.assertIn("blocked by #2", flags)

    def test_acceptance_closure_needs_recorded_evidence_and_review(self):
        flags = findings(issue("done", closed=True, acceptance=True),
                         [comment("ACK", ACK)], [], NOW, 48)[2]
        self.assertTrue(any("without recorded evidence" in flag for flag in flags))
        self.assertTrue(any("independent review" in flag for flag in flags))

    def test_self_review_does_not_clear_review_flag(self):
        notes = [comment("ACK", ACK), comment("REVIEW", dict(reviewer=ACK["runner"], url="https://github.com/x/y/pull/2"))]
        self.assertIn("awaiting recorded independent review", findings(issue("review"), notes, [], NOW, 48)[2])

    def test_old_claim_review_and_evidence_do_not_certify_a_new_claim(self):
        old = dict(ACK, runner="old-worker")
        notes = [comment("ACK", old, "2026-09-25T00:00:00Z"),
                 comment("UPDATE", dict(runner="old-worker", evidence="old artifact"), "2026-09-26T00:00:00Z"),
                 comment("REVIEW", dict(reviewer="reviewer", url="https://github.com/x/y/pull/2"), "2026-09-27T00:00:00Z"),
                 comment("ACK", ACK, "2026-09-28T00:00:00Z")]
        flags = findings(issue("done", closed=True, acceptance=True), notes, [], NOW, 48)[2]
        self.assertTrue(any("independent review" in flag for flag in flags))
        self.assertTrue(any("without recorded evidence" in flag for flag in flags))

    def test_review_for_another_pr_does_not_clear_current_review_flag(self):
        notes = [comment("ACK", ACK),
                 comment("UPDATE", dict(runner=ACK["runner"], pr="https://github.com/x/y/pull/3"), "2026-09-28T00:00:00Z"),
                 comment("REVIEW", dict(reviewer="reviewer", url="https://github.com/x/y/pull/2#pullrequestreview-1"), "2026-09-29T00:00:00Z")]
        self.assertIn("awaiting recorded independent review", findings(issue("review"), notes, [], NOW, 48)[2])
        notes.append(comment("REVIEW", dict(reviewer="reviewer", url="https://github.com/x/y/pull/3#pullrequestreview-2"), "2026-09-30T00:00:00Z"))
        self.assertNotIn("awaiting recorded independent review", findings(issue("review"), notes, [], NOW, 48)[2])

    def test_incomplete_claim_and_conflicting_labels_are_visible(self):
        entry = issue()
        entry["labels"].append({"name": "task:ready"})
        self.assertIn("missing, unknown or conflicting task state", findings(entry, [], [], NOW, 48)[2])
        partial = dict(ACK, base="branch-name")
        self.assertIn("missing complete runner acknowledgement",
                      findings(issue(), [comment("ACK", partial)], [], NOW, 48)[2])

    def test_paginated_comments_are_not_truncated(self):
        with patch("scripts.coordination.subprocess.run") as run:
            run.return_value.stdout = json.dumps([[dict(number=1)], [dict(number=2)]])
            self.assertEqual([x["number"] for x in gh_list("path")], [1, 2])
            self.assertIn("--paginate", run.call_args[0][0])

    def test_api_failure_does_not_publish_an_all_clear(self):
        with patch("scripts.coordination.gh_list", side_effect=subprocess.CalledProcessError(1, "gh")):
            with self.assertRaises(subprocess.CalledProcessError):
                report("DeanoC/fes", 1, [issue()], NOW, 48)
        with self.assertRaises(ValueError):
            report("DeanoC/fes", 1, [], NOW, 48)

    def test_untrusted_title_stays_in_one_table_row(self):
        with patch("scripts.coordination.gh_list", return_value=[]):
            text = report("DeanoC/fes", 1, [issue("ready")], NOW, 48)
        self.assertIn("Title &#124; &lt;script&gt; next", text)
        self.assertEqual(sum(line.startswith("| [#") for line in text.splitlines()), 1)


if __name__ == "__main__":
    unittest.main()
