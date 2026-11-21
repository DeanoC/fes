"""Approved terminal-reset review still rejects incomplete or unsafe evidence."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from boards.de25nano.scanout import async_inputs, output_reset as reset, video_outputs
from boards.de25nano.spectrum import native_hps
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

    def test_native_source_projection_is_bound_to_the_selected_profile(self):
        original = (reset.HERE / 'fes_de25_scanout_board.v').read_text()
        projected = native_hps.separate_scanout_reset(original)
        parameters = {
            'core_id': 'fes.spectrum', 'model': 'native48',
            'programming_profile': 'de25-native-spectrum48-v1', 'firmware_bytes': 16384,
            'hdmi_output_reset_review_policy': reset.POLICY,
        }
        with tempfile.TemporaryDirectory() as temp, patch.object(async_inputs, 'bindings', return_value={}):
            root = Path(temp)
            candidate = root / 'candidate'
            board = candidate / 'combined/hps/project/fes_mailbox/fes_de25_scanout_board.v'
            board.parent.mkdir(parents=True)
            record = root / 'build-inputs.json'
            record.write_text(json.dumps({'parameters': parameters}))
            board.write_text(projected)
            self.assertEqual(reset.bindings(candidate)['compiled_board_sha256'], async_inputs.sha(board))
            board.write_text(projected.replace('else scanout_release <= first_lock_seen', 'else scanout_release <= !first_lock_seen'))
            with self.assertRaises(BuildError):
                reset.bindings(candidate)
            board.write_text(original)
            with self.assertRaises(BuildError):
                reset.bindings(candidate)
            parameters['programming_profile'] = 'de25-linux-computer-v1'
            record.write_text(json.dumps({'parameters': parameters}))
            self.assertEqual(reset.bindings(candidate)['compiled_board_sha256'], async_inputs.sha(board))
            board.write_text(projected)
            with self.assertRaises(BuildError):
                reset.bindings(candidate)

    def test_only_the_approved_single_rdc_domain_and_two_heads_are_reviewed(self):
        raw = ('Status: FAIL\n; RDC-50003 - Multiple Asynchronous Reset Synchronizers in the Same Clock Domain ;\n'
               '; Reset Clock Domain ; Number of Reset Chains ; Reset Chain Register Heads ; Reset Sources ; Waived ;\n'
               f'; {reset.CLOCK} ; 2 ; ' + '   '.join(sorted(reset.HEADS)) + ' ; ' +
               '   '.join(sorted(reset.SOURCES)) + ' ; ;\n')
        self.assertEqual(reset.findings(raw)['violations'], 1)
        native = raw.replace('fes_video_inst|first_lock_seen', 'fes_video_inst|scanout_release')
        self.assertEqual(reset.findings(native, native_spectrum=True)['violations'], 1)
        with self.assertRaises(BuildError):
            reset.findings(native)
        with self.assertRaises(BuildError):
            reset.findings(raw, native_spectrum=True)
        for before, after in [(' ; 2 ; ', ' ; 3 ; '), (reset.STAGES[0], 'other_reset[0]'),
                              ('fes_video_inst|visible_allow', 'other_source'), (' ; ;\n', ' ; Yes ;\n')]:
            with self.assertRaises(BuildError): reset.findings(raw.replace(before, after, 1))
        with self.assertRaises(BuildError): reset.findings(raw + raw)
