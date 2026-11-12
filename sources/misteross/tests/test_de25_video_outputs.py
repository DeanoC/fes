"""Exercise complete fitted path coverage and fail-closed output findings."""
from pathlib import Path
import tempfile
import unittest

from boards.de25nano.scanout import vendor_reference, video_outputs as video
from scripts.fes_build_common import BuildError


class VideoOutputTests(unittest.TestCase):
    def fixture(self, root):
        inventory = []
        for model in sorted(video.MODELS):
            for port in sorted(video.PORTS):
                for kind in ('setup', 'hold'):
                    name = f'path-{len(inventory):03d}.rpt'
                    inventory.append('\t'.join((name, model, port, kind, '0.100')))
                    rows = [('From Node', 'fes_video_inst|' + port), ('To Node', port),
                            ('Launch Clock', video.CLOCK), ('Latch Clock', 'HDMI_TX_CLK'),
                            ('SDC Exception', 'No SDC Exception on Path'),
                            ('Worst-Case Operating Conditions', model), ('Slack', '0.100')]
                    (root / name).write_text('Snapshot:\n    final\n' +
                        '\n'.join(f'; {key} ; {value} ;' for key, value in rows))
        (root / 'inventory.tsv').write_text('\n'.join(inventory) + '\n')

    def test_every_port_both_checks_and_all_models_are_required(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); self.fixture(root)
            result = video.review_reports(root)
            self.assertEqual(result['path_count'], 216)
            path = root / 'inventory.tsv'; original = path.read_text()
            for changed in ('\n'.join(original.splitlines()[:-1]) + '\n',
                            original + original.splitlines()[0] + '\n',
                            original.replace('Slow fix4a 0C Model', 'unknown Model')):
                path.write_text(changed)
                with self.subTest(changed=changed[-100:]), self.assertRaises(BuildError):
                    video.review_reports(root)
            path.write_text(original)
            report = root / 'path-000.rpt'; original = report.read_text()
            for before, after in [('final', 'post_synth'), ('HDMI_TX_CLK', 'wrong_clock'),
                                  ('No SDC Exception on Path', 'False Path'),
                                  ('fes_video_inst|' + sorted(video.PORTS)[0], 'other|' + sorted(video.PORTS)[0]),
                                  ('0.100', '-0.001'), ('0.100', 'NaN')]:
                report.write_text(original.replace(before, after))
                with self.subTest(after=after), self.assertRaises(BuildError):
                    video.review_reports(root)
            report.write_text(original)
            (root / 'extra.rpt').write_text('unselected')
            with self.assertRaises(BuildError): video.review_reports(root)

    def test_reduced_output_findings_need_the_numerical_profile(self):
        ports = vendor_reference.OUTPUTS - video.PORTS
        self.assertEqual(len(ports), 3)
        drc = ('Status: FAIL\n; TMC-20012 - Missing output delay ;\n; Port ; Reason ; Waived ;\n' +
               ''.join(f'; {p} ; No output delay was set on the output port. ; ;\n' for p in sorted(ports)))
        with self.assertRaises(BuildError): vendor_reference.output_findings(drc)
        self.assertEqual(set(vendor_reference.output_findings(drc, timed_video=True)), ports)
        with self.assertRaises(BuildError):
            vendor_reference.output_findings(drc + '; HDMI_TX_DE ; No output delay was set on the output port. ; ;\n', timed_video=True)
