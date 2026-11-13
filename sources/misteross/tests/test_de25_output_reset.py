"""Approved terminal-reset review still rejects incomplete or unsafe evidence."""
from pathlib import Path
import tempfile
import unittest

from boards.de25nano.scanout import output_reset as reset, video_outputs
from scripts.fes_build_common import BuildError


class OutputResetTests(unittest.TestCase):
    def test_exact_fanout_and_every_model_load_and_reset_check_are_required(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            inventory = '0\t' + reset.STAGES[1] + '\n' + ''.join('1\t' + n + '\n' for n in sorted(reset.LOADS))
            lines = ['\t'.join((m, k, reset.STAGES[1], n, '0.100', reset.CLOCK, reset.CLOCK))
                     for m in sorted(video_outputs.MODELS) for k in ('recovery', 'removal') for n in sorted(reset.LOADS)]
            paths = '\n'.join(lines) + '\n'
            (root / 'reset-inventory.tsv').write_text(inventory)
            (root / 'reset-paths.tsv').write_text(paths)
            self.assertEqual(reset.review_reports(root)['path_count'], 208)
            for text in (inventory + '1\tsoc_inst|dma_state\n', inventory + '0\t' + reset.STAGES[1] + '\n',
                         inventory.replace(reset.STAGES[1], reset.STAGES[0])):
                (root / 'reset-inventory.tsv').write_text(text)
                with self.assertRaises(BuildError): reset.review_reports(root)
            (root / 'reset-inventory.tsv').write_text(inventory)
            for text in ('\n'.join(lines[:-1]) + '\n', paths + lines[0] + '\n',
                         paths.replace('0.100', '-0.001', 1), paths.replace('0.100', 'NaN', 1),
                         paths.replace('removal', 'setup', 1), paths.replace(reset.CLOCK, 'wrong', 1)):
                (root / 'reset-paths.tsv').write_text(text)
                with self.assertRaises(BuildError): reset.review_reports(root)

    def test_only_the_approved_single_rdc_domain_and_two_heads_are_reviewed(self):
        raw = ('Status: FAIL\n; RDC-50003 - Multiple Asynchronous Reset Synchronizers in the Same Clock Domain ;\n'
               '; Reset Clock Domain ; Number of Reset Chains ; Reset Chain Register Heads ; Reset Sources ; Waived ;\n'
               f'; {reset.CLOCK} ; 2 ; ' + '   '.join(sorted(reset.HEADS)) + ' ; ' +
               '   '.join(sorted(reset.SOURCES)) + ' ; ;\n')
        self.assertEqual(reset.findings(raw)['violations'], 1)
        for before, after in [(' ; 2 ; ', ' ; 3 ; '), (reset.STAGES[0], 'other_reset[0]'),
                              ('fes_video_inst|visible_allow', 'other_source'), (' ; ;\n', ' ; Yes ;\n')]:
            with self.assertRaises(BuildError): reset.findings(raw.replace(before, after, 1))
        with self.assertRaises(BuildError): reset.findings(raw + raw)
