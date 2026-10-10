from pathlib import Path
import subprocess
import tempfile
import unittest

from scripts.check_doc_links import broken_links


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


if __name__ == '__main__':
    unittest.main()
