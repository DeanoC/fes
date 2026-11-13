"""Approved audio exceptions must remain confined to their reviewed sources."""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from boards.de25nano.scanout import audio_reference as audio, vendor_reference

ROOT = Path(__file__).resolve().parents[1]


class AudioReferenceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='de25-audio-reference-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.fit = self.root / 'signoff'
        self.project = self.fit / 'combined/hps/project'
        self.local = self.project / 'fes_mailbox'
        self.local.mkdir(parents=True)
        self.record = {'parameters': {'core_id': 'fes.zx81',
            'programming_profile': 'de25-linux-computer-v1', 'standard_core': True,
            'expansion_socket': False, 'internal_ram_kib': 16,
            'audio_clock_reference_policy': audio.POLICY}, 'source_inputs': dict(audio.SOURCES)}
        for name, expected in audio.SOURCES.items():
            source = ROOT / name
            self.assertEqual(hashlib.sha256(source.read_bytes()).hexdigest(), expected)
            if not name.endswith(('/build.py', '_hw.tcl')):
                (self.local / source.name).write_bytes(source.read_bytes())
        # Synthetic wiring exercises policy only; it is never compiler evidence.
        self.top = '''wire [15:0] standard_audio_sample;
        fes_de25_audio fes_audio_inst (
          .CLOCK1_50(CLOCK1_50), .source_clk(system_clk_100_internal),
          .reset_n(top_reset_n), .hold(standard_exec_reset || scanout_clock_fault),
          .left_sample(standard_audio_sample), .right_sample(standard_audio_sample), .locked(),
          .HDMI_MCLK(HDMI_MCLK), .HDMI_SCLK(HDMI_SCLK),
          .HDMI_LRCLK(HDMI_LRCLK), .HDMI_I2S(HDMI_I2S) );
        qsys_top soc_inst (.fes_scanout_audio_sample(standard_audio_sample),
          .fes_scanout_expansion_expansion_running(1'b1),
          .fes_scanout_expansion_bus_peek_data(8'b0),
          .fes_scanout_expansion_bus_ram_present(1'b0));'''
        (self.project / 'golden_top.v').write_text(self.top)
        self.write_record()

    def write_record(self):
        (self.root / 'build-inputs.json').write_text(json.dumps(self.record))

    def test_only_reviewed_standard_internal16k_is_eligible(self):
        self.assertIn('golden_top.v', audio.eligible(self.fit))
        for key, value in (('core_id', 'fes.menu'), ('standard_core', 1),
                ('expansion_socket', True), ('internal_ram_kib', 1),
                ('programming_profile', 'de25-hps-first-v1'),
                ('audio_clock_reference_policy', 'other')):
            original = copy.deepcopy(self.record)
            self.record['parameters'][key] = value
            self.write_record()
            with self.subTest(key=key), self.assertRaises(audio.BuildError):
                audio.eligible(self.fit)
            self.record = original
            self.write_record()

    def test_source_and_compiled_serializer_changes_require_review(self):
        for name in audio.SOURCES:
            original = self.record['source_inputs'][name]
            self.record['source_inputs'][name] = '0' * 64
            self.write_record()
            with self.subTest(source=name), self.assertRaises(audio.BuildError):
                audio.eligible(self.fit)
            self.record['source_inputs'][name] = original
        self.write_record()
        path = self.local / 'fes_audio_i2s.v'
        path.write_bytes(path.read_bytes() + b'// changed serializer')
        with self.assertRaises(audio.BuildError):
            audio.eligible(self.fit)

    def test_wrapper_cannot_change_clock_reset_pcm_or_expansion(self):
        for before, after in (('system_clk_100_internal', 'other_clk'),
                ('top_reset_n', 'other_reset'), ('|| scanout_clock_fault', ''),
                ('left_sample(standard_audio_sample)', 'left_sample(another_sample)'),
                ("bus_peek_data(8'b0)", "bus_peek_data(8'b1)"),
                ("expansion_running(1'b1)", "expansion_running(1'b0)")):
            (self.project / 'golden_top.v').write_text(self.top.replace(before, after))
            with self.subTest(connection=before), self.assertRaises(audio.BuildError):
                audio.eligible(self.fit)

    def finding_report(self):
        return 'Status: FAIL\n; TMC-20012 - Missing output delay;\n; Port ; Reason ; Waived ;\n' + ''.join(
            '; ' + port + ' ; No output delay was set on the output port. ; ;\n'
            for port in sorted(vendor_reference.OUTPUTS | audio.PORTS))

    def test_exact32_unwaived_findings_and_unchanged_menu_policy(self):
        report = self.finding_report()
        self.assertEqual(len(audio.output_findings(report)), 32)
        self.assertEqual(len(vendor_reference.OUTPUTS), 30)
        with self.assertRaises(audio.BuildError):
            vendor_reference.output_findings(report)
        for before, after in (('HDMI_SCLK', 'other_output'),
                ('No output delay was set on the output port.', 'Another reason'),
                (' ; ;\n', ' ; Yes ;\n')):
            with self.subTest(after=after), self.assertRaises(audio.BuildError):
                audio.output_findings(report.replace(before, after, 1))
        with self.assertRaises(audio.BuildError):
            audio.output_findings(report + '; HDMI_SCLK ; No output delay was set on the output port. ; ;\n')

    def test_receipt_binding_inventory_budgets_and_receiver_cannot_drift(self):
        self.record['revision'] = 'a' * 40
        self.write_record()
        review = self.root / 'review'
        reports = review / 'reports'
        reports.mkdir(parents=True)
        for name in audio.REPORTS:
            (reports / name).write_text('synthetic report: ' + name)
        tool = self.root / 'quartus_sta'
        tool.write_text('synthetic identity; never executed')
        selected = {'policy': audio.POLICY, 'compiled_files_sha256': audio.eligible(self.fit),
                    'archive': '/synthetic-unused-vendor.zip'}
        (review / 'approved-reference.json').write_text(json.dumps(selected))
        output = self.project / 'output_files'
        output.mkdir()
        (output / 'golden_top.tq.drc.signoff.rpt').write_text(self.finding_report())
        observation = {'clocks': {}, 'fitted_corner_count': 4, 'fitted_path_count': 32,
            'conditional_pin_budgets': [{
                'setup_budget_before_unmodeled_skew_and_jitter_ns': '100',
                'hold_budget_before_unmodeled_skew_and_jitter_ns': '100'}]}
        # Mock physical observations here to isolate receipt binding. Actual
        # clock/path parsing has its separate malformed-report regressions.
        receipt = dict(observation, bindings={'fit': 'one'}, source_revision='a' * 40,
            stock_audio={'stock': 'one'}, vendor_hdmi_reference={'receiver': 'one'},
            command=[str(tool), '-t', str(audio.fitted.TCL.resolve()),
                     str(review / 'project-inputs/combined/hps/project'), str(reports)],
            tools_sha256={str(p): audio.async_inputs.sha(p) for p in (
                tool, audio.fitted.TCL.resolve(), Path(audio.fitted.__file__).resolve())},
            reports_sha256={p.name: audio.async_inputs.sha(p) for p in reports.iterdir()})
        def verify(candidate):
            (review / 'receipt.json').write_text(json.dumps(candidate))
            return audio.verify(self.fit, review)
        with mock.patch.object(audio.async_inputs, 'bindings', return_value={'fit': 'one'}), \
                mock.patch.object(audio.fitted, 'observations', return_value=observation), \
                mock.patch.object(audio.fitted, 'stock_audio', return_value={'stock': 'one'}), \
                mock.patch.object(audio.vendor_reference, 'verify', return_value={'receiver': 'one'}):
            self.assertEqual(verify(receipt)['fitted_path_count'], 32)
            for field, value in (('bindings', {'fit': 'other'}), ('source_revision', 'b' * 40),
                    ('tools_sha256', {}), ('reports_sha256', {}),
                    ('stock_audio', {'stock': 'other'}),
                    ('vendor_hdmi_reference', {'receiver': 'other'}),
                    ('command', [str(tool), '-t', '/other.tcl', 'project', 'reports'])):
                changed = copy.deepcopy(receipt)
                changed[field] = value
                with self.subTest(field=field), self.assertRaises(audio.BuildError):
                    verify(changed)
            for value in ('0', '-1', 'NaN'):
                observation['conditional_pin_budgets'][0][
                    'setup_budget_before_unmodeled_skew_and_jitter_ns'] = value
                with self.subTest(budget=value), self.assertRaises(audio.BuildError):
                    verify(dict(receipt, conditional_pin_budgets=observation['conditional_pin_budgets']))
