"""The reviewed window-bridge FIFO reset synchronizers (RDC-50003).

Synthetic reports in the STA query's and the signoff report's formats; they
exercise the rules and never stand in for a fit.
"""
from pathlib import Path
import tempfile
import unittest

from boards.de25nano.scanout import bridge_reset as br, output_reset, video_outputs
from scripts.fes_build_common import BuildError


def rdc(rows):
    head = ['Reset Clock Domain', 'Number of Reset Chains', 'Reset Chain Register Heads', 'Reset Sources', 'Waived']
    body = ''.join('; ' + ' ; '.join(r) + ' ;\n' for r in [head] + rows)
    return (f'; RDC-50003 - Multiple ; Medium ; {len(rows)} ; 0 ; tags ;\n'
            f'Status:\t\tFAIL\tSeverity:\tMedium\tNumber of violations: \t{len(rows)}\n'
            '+---\n; RDC-50003 - Multiple Asynchronous Reset Synchronizers ;\n+---\n' + body)


def row(expected, **change):
    clock, count, heads, sources = expected
    r = [clock, count, '   '.join(sorted(heads)), '   '.join(sorted(sources)), '']
    for index, value in change.items():
        r[int(index[1:])] = value
    return r


VIDEO = [output_reset.CLOCK, '2', '   '.join(sorted(output_reset.HEADS)), '   '.join(sorted(output_reset.SOURCES)), '']


class BridgeResetTests(unittest.TestCase):
    def test_the_output_reset_row_stays_reviewed_and_the_bridge_rows_are_set_aside_exactly(self):
        rows = [row(br.ROWS[0]), row(br.ROWS[1]), VIDEO]
        with self.assertRaises(BuildError):
            output_reset.findings(rdc(rows))  # without the bridge policy, three rows are refused
        self.assertEqual(output_reset.findings(rdc(rows), bridge=True)['violations'], 1)
        changed = row(br.ROWS[0], c2='fes_top_reset_inst|sync_regs[0]   some|other|chain[3]')
        with self.assertRaises(BuildError):
            output_reset.findings(rdc([changed, row(br.ROWS[1]), VIDEO]), bridge=True)
        with self.assertRaises(Exception):  # a waived bridge row is not this basis
            output_reset.findings(rdc([row(br.ROWS[0], c4='Yes'), row(br.ROWS[1]), VIDEO]), bridge=True)
        with self.assertRaises(Exception):  # a fourth domain is reviewed by nobody
            output_reset.findings(rdc(rows + [['other_clock', '2', 'a[0]   b[0]', 'c', '']]), bridge=True)

    def reports(self, directory, *, drop=None, slack='0.259', outside=False, source=None):
        directory = Path(directory)
        chains, loads, paths = [], [], []
        for inst, clock in br.INSTANCES.items():
            fifo = inst.rsplit('|', 1)[0]
            for stage in br.STAGES:
                for s in sorted(br.SOURCE_RESETS if source is None else {source}):
                    chains.append('\t'.join((inst, inst + '|' + stage, s)))
            targets = [fifo + f'|wr_ptr[{i}]' for i in range(3)]
            if outside:
                targets.append('soc_inst|subsys_periph|fes_scanout|fes_scanout|window|elsewhere')
            loads += ['\t'.join((inst, t)) for t in targets]
            for model in sorted(video_outputs.MODELS):
                for kind in ('recovery', 'removal'):
                    for t in targets:
                        if (model, kind, t) != drop:
                            paths.append('\t'.join((model, kind, inst, t, slack, clock, clock)))
        for name, lines in (('chains.tsv', chains), ('loads.tsv', loads), ('paths.tsv', paths)):
            (directory / name).write_text('\n'.join(lines) + '\n')
        return directory

    def test_every_load_needs_recovery_and_removal_at_every_corner_inside_its_own_fifo(self):
        with tempfile.TemporaryDirectory() as t:
            result = br.review_reports(self.reports(t))
            self.assertEqual(result['path_count'], 4 * 3 * 4 * 2)
            self.assertEqual(result['source_resets'], sorted(br.SOURCE_RESETS))
        first = next(iter(br.INSTANCES)).rsplit('|', 1)[0] + '|wr_ptr[0]'
        for kwargs, message in ((dict(drop=('Fast fix4 0C Model', 'removal', first)), 'lacks recovery or removal'),
                                (dict(slack='-0.010'), 'nonpassing'),
                                (dict(outside=True), 'outside its own FIFO'),
                                (dict(source='some|other|reset'), 'source resets changed')):
            with self.subTest(**{k: str(v) for k, v in kwargs.items()}), tempfile.TemporaryDirectory() as t:
                with self.assertRaisesRegex(BuildError, message):
                    br.review_reports(self.reports(t, **kwargs))

    def test_the_basis_cites_where_the_synchronizers_are_fixed(self):
        doc = br.__doc__
        self.assertIn('lines 238-255', doc)
        self.assertIn('outside the', doc)
        self.assertEqual(len(br.INSTANCES), 4)
        self.assertEqual({c for c in br.INSTANCES.values()}, {br.MACHINE, br.PERIPHERAL})


if __name__ == '__main__':
    unittest.main()
