from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from scripts.check_doc_links import broken_links, main


AGENTS = Path(__file__).resolve().parents[1] / "AGENTS.md"
BASELINE_SECTION = AGENTS.read_text().split("### Kit HIL baseline tracking\n", 1)[1].split(
    "\n### ", 1
)[0]


class CheckDocLinksTests(unittest.TestCase):
    def repo(self, files):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        for name, text in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        subprocess.run(['git', 'init', '-q', str(root)], check=True)
        subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
        return root

    def test_reports_missing_relative_targets_with_line(self):
        root = self.repo({'docs/a.md': 'intro\n[gone](missing.md)\n[ok](b.md#part)\n',
                          'docs/b.md': '# Part\n'})
        self.assertEqual(broken_links(root), ['docs/a.md:2: missing.md'])

    def test_ignores_urls_anchors_and_fenced_code(self):
        root = self.repo({'a.md': '[web](https://example.com/x.md) [here](#top)\n'
                                  '```\n[sample](not-there.md)\n```\n[root](/a.md)\n'})
        self.assertEqual(broken_links(root), [])

    def test_destinations_may_contain_balanced_parentheses(self):
        root = self.repo({'a.md': '[v2](manual(v2).md) [gone](old(v1).md)\n',
                          'manual(v2).md': 'ok\n'})
        self.assertEqual(broken_links(root), ['a.md:1: old(v1).md'])

    def test_indented_and_nested_fences_hide_examples(self):
        root = self.repo({'a.md': '- item\n\n  ```md\n  [example](missing.md)\n  ```\n'
                                  '````md\n```\n[inner](missing.md)\n```\n````\n'
                                  '[after](gone.md)\n'})
        self.assertEqual(broken_links(root), ['a.md:11: gone.md'])

    def test_reference_definitions_are_checked(self):
        root = self.repo({'a.md': '[guide][setup] [ok][b]\n\n[setup]: missing.md\n'
                                  '[b]: b.md\n[web]: https://example.com\n',
                          'b.md': 'ok\n'})
        self.assertEqual(broken_links(root), ['a.md:3: missing.md'])

    def test_untracked_markdown_is_not_checked(self):
        root = self.repo({'a.md': 'ok\n'})
        (root / 'scratch.md').write_text('[gone](missing.md)\n')
        self.assertEqual(broken_links(root), [])

    def test_main_runs_support_matrix_check_when_source_exists(self):
        root = self.repo({'README.md': 'No links here.\n',
                          'docs/support-matrix.json': '{}\n',
                          'docs/support-matrix.py': '# support matrix checker\n'})
        expected = [sys.executable, str(root / 'docs/support-matrix.py'), '--check',
                    '--root', str(root)]
        with patch('scripts.check_doc_links.broken_links', return_value=[]), \
                patch('scripts.check_doc_links.subprocess.run',
                      return_value=subprocess.CompletedProcess(expected, 0)) as run, \
                patch.object(sys, 'argv', ['check_doc_links.py', '--root', str(root)]), \
                redirect_stdout(StringIO()):
            main()
        run.assert_called_once_with(expected, check=False)


class HilBaselineDocsTests(unittest.TestCase):
    def test_promotion_uses_timer_and_cumulative_unchecked_range(self):
        section = BASELINE_SECTION
        self.assertIn("lander-baseline-promote.timer", section)
        self.assertIn("every 15 minutes", section)
        self.assertIn("lander-baseline-nightly.timer", section)
        self.assertIn("02:30 Europe/Sofia", section)
        self.assertIn("baseline-auto.py", section)
        self.assertIn("--since <last_checked>", section)
        self.assertIn("AUTO_WAIT", section)
        self.assertIn("do not run a manual promote after each qualifying merge", section)
        self.assertNotIn("run it by hand after such merges", section)

    def test_promotion_filter_and_hil_mode_are_separate(self):
        section = BASELINE_SECTION
        for path in (
            "sources/FogCast/", "sources/libmister-runtime/",
            "sources/mister-packages/", "platform/", "image/", "Makefile",
            "scripts/build.py", "scripts/kit_software.py", "scripts/environment.py",
        ):
            with self.subTest(path=path):
                self.assertIn(path, section)
        self.assertIn("non-documentation, non-test file", section)
        self.assertIn("abi_path", section)
        self.assertIn("core+sw", section)
        self.assertIn("does not decide whether to promote the baseline", section)

    def test_busy_kits_wait_and_baseline_publishes_after_smoke(self):
        section = BASELINE_SECTION
        self.assertIn("logs busy kits as `BUSY`", section)
        self.assertIn("waits for Kit B's lock until it is `FREE`", section)
        self.assertIn("publishes `current.json` only after smoke and activation succeed", section)

    def test_only_dispatchers_and_promote_smoke_write_baseline_stamps(self):
        section = BASELINE_SECTION
        for fact in (
            "`hilq.py` dispatcher writes `baseline.json`",
            "Promote smoke writes `baseline.json` and `stamp.json`",
            "`BASELINE_CHANGE` to `~/lander-poller/gate.log`",
            "Direct `hil-run.sh` and `hil-core.sh` runs do not write that stamp",
            "have no `baseline.json`",
        ):
            with self.subTest(fact=fact):
                self.assertIn(fact, section)


if __name__ == '__main__':
    unittest.main()
