"""The DE25 pc386 reviewed signoff findings and the gate's per-rule coverage.

The DRC text below is synthetic, in the signoff report's table format; it
exercises the matching rules and never stands in for a fitted report.
"""
from contextlib import nullcontext
import csv
import json
import re
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

from boards.de25nano.pc386 import retimed_tension
from boards.de25nano.producers import build_machine_package as shared
from boards.de25nano.producers import build_menu_package as common
from boards.de25nano.producers import build_pc386_package as pc386
from boards.de25nano.scanout import machine_findings as mf

TABLE = mf.bases()

S = 'soc_inst|subsys_periph|fes_scanout|fes_scanout|system|'
MACHINE, PIXEL = mf.MACHINE, mf.PIXEL
ROOT = Path(__file__).resolve().parents[1]


def report(rules, *, severity='High'):
    """rules: {rule: (header, rows)}; the summary counts each rule's rows."""
    summary = ''.join(f'; {rule} - Title ; {severity} ; {len(rows)} ; 0 ; tags ;\n'
                      for rule, (_, rows) in rules.items())
    detail = ''
    for rule, (header, rows) in rules.items():
        detail += (f'Status:\t\tFAIL\tSeverity:\t{severity}\tNumber of violations: \t{len(rows)}\n'
                   f'+---\n; {rule} - Title ;\n+---\n; ' + ' ; '.join(header) + ' ;\n+---\n' +
                   ''.join('; ' + ' ; '.join(row) + ' ;\n' for row in rows))
    return summary + detail


CDC = ['From', 'To', 'From Clock', 'To Clock', 'Reason', 'Waived']
TENSION = ['Register Name', 'Register Location', 'Number of Endpoints', 'Endpoint Centroid',
           'Average Distance of Endpoints to Centroid', 'Total Distance of Endpoints to Centroid (Tension)',
           'Waived']


def cdc(source, sink, launch=MACHINE, latch=PIXEL):
    return [S + source, S + sink, launch, latch, 'Asynchronous transfer', '']


def sdc_text():
    return '\n'.join({line for basis in TABLE.BASES for line in basis['sdc']}) + '\n'


class PerRuleCoverageTests(unittest.TestCase):
    def test_several_entries_for_one_rule_cannot_hide_another_rule(self):
        rows = [('CDC-50001', 'High', '90', '0'), ('CDC-50002', 'High', '3', '0'), ('PAS-00001', 'Low', '0', '0')]
        # The old count (failed - entries) would read 2 - 2 = 0 here.
        two_for_one = [{'id': 'CDC-50001'}, {'id': 'CDC-50001'}]
        with self.assertRaisesRegex(common.BuildError, 'one distinct failed rule'):
            common.unreviewed_rules(two_for_one, rows)
        self.assertEqual(common.unreviewed_rules([{'id': 'CDC-50001'}], rows), 1)
        self.assertEqual(common.unreviewed_rules([{'id': 'CDC-50001'}, {'id': 'CDC-50002'}], rows), 0)
        with self.assertRaisesRegex(common.BuildError, 'one distinct failed rule'):
            common.unreviewed_rules([{'id': 'PAS-00001'}], rows)  # a rule that passed


class RegisteredTensionTests(unittest.TestCase):
    def test_registered_builds_gate_retiming_by_recorded_model(self):
        retimed = '_17339_~1_R6_X56_Y33_N0_I13_dff'
        cases = (
            ('486sx', '486sx', 'cpu_md_busy', False, None),
            ('486dx', '486dx', 'cpu_md_busy', False, None),
            ('486dx', '486dx', retimed, True, None),
            ('486dx', '486dx', retimed, True, 'fixture trace failed'),
            ('486dx', '486dx', retimed.replace('_17339_', '_17340_'), True, None),
            ('486dx', '486dx', retimed.replace('_R6_', '_R7_'), False, 'matches 0 bases'),
            ('486sx', '486sx', retimed, False, 'matches 0 bases'),
            ('486dx', '486dx', 'cpu_biu_drain~DUPLICATESCLR_dff', True, None),
            ('486sx', '486sx', 'cpu_biu_drain~DUPLICATESCLR_dff', False, 'matches 0 bases'),
            ('486dx', 'unknown', retimed, False, 'registered fit has unknown model'),
            ('486dx', '486sx', retimed, False, 'differs from prepared model'),
            ('486dx', None, retimed, False, 'lacks a recorded or prepared model'),
        )
        for compile_only in (False, True):
            for requested, recorded, name, trace, error in cases:
                with self.subTest(compile_only=compile_only, recorded=recorded, node=name):
                    self.check_registered_fit(compile_only, requested, recorded, name, trace, error)

    def check_registered_fit(self, compile_only, requested, recorded, name, trace, error):
        machine = pc386.MACHINE
        spec = machine.spec(ROOT)
        row = [S + name, '(56, 33)', '16759', '(38, 27)', '24', '407264', '']
        drc = report({'TMC-20602': (TENSION, [row])}, severity='Low')
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            output = root / 'attempt'
            selected = output / 'signoff'
            report_path = selected / 'combined/hps/project/output_files/golden_top.tq.drc.signoff.rpt'
            payload = selected / 'combined/hps/linux-boot.core.rbf'
            parameters = {'model': recorded} if recorded is not None else {}
            record = json.dumps({'parameters': parameters}).encode()

            def prepare_fixture(*_, **__):
                inputs = selected / 'combined/hps/inputs.json'
                inputs.parent.mkdir(parents=True)
                inputs.write_text(json.dumps({spec.board.RECORD: {'model': requested}}))
                return selected, None, None, None, None

            def fit_fixture(*_):
                report_path.parent.mkdir(parents=True)
                report_path.write_text(drc)
                payload.write_bytes(b'fixture payload')
                return payload

            def timing_fixture(*_, **__):
                mf.review(report_path.read_text(), '', TABLE.BASES)
                return {'package_timing_admission': not compile_only}

            proof_error = mf.BuildError(error) if trace and error else None
            proof = mock.Mock(side_effect=proof_error)
            actual_load = spec.load

            def load_fixture(path, module_name):
                module = actual_load(path, module_name)
                if path.name == 'retimed_tension.py':
                    module.collect = proof
                return module

            with (
                mock.patch.multiple(machine, require_clean_source=mock.Mock(return_value=('fixture', 'revision')),
                                    create_build_record=mock.Mock(return_value=record),
                                    manifest=mock.Mock(return_value=b'fixture manifest')),
                mock.patch.object(machine, 'spec', return_value=spec),
                mock.patch.object(spec, 'authenticate_front_end', return_value=mock.Mock(identity='fixture')),
                mock.patch.object(spec, 'load', side_effect=load_fixture),
                mock.patch.multiple(spec.board.delegate, capture=mock.Mock(return_value={}), generate=mock.Mock(),
                                    prepare=mock.Mock(side_effect=prepare_fixture)),
                mock.patch.object(spec, 'quartus_lease', return_value=nullcontext()),
                mock.patch.object(spec.board.delegate, 'compile_board', side_effect=fit_fixture) as fit,
                mock.patch.multiple(common, authenticate_tools=mock.Mock(return_value=(
                    {'quartus': root / 'quartus_sh'}, {'quartus': 'fixture'})),
                    execution_environment=mock.Mock(return_value={}), execution_inputs=mock.Mock(return_value={})),
                mock.patch.object(common, 'timing_evidence', side_effect=timing_fixture) as timing,
                mock.patch.object(shared.async_inputs, 'collect', return_value=None),
                mock.patch.object(shared.audio_reference, 'collect', return_value=None),
                mock.patch.object(shared, 'build_identity', return_value='fixture'),
                mock.patch.object(shared, 'export_package', return_value=root / 'fixture.fcore') as export,
            ):
                arguments = dict(root=root, quartus_root='', vendor_archive=root / 'fixture.zip',
                                 model=requested, output=output, compile_only=compile_only)
                if error:
                    with self.assertRaisesRegex(mf.BuildError, error):
                        machine.build(**arguments)
                    self.assertEqual(fit.call_count, int(recorded == requested))
                    timing.assert_not_called()
                    export.assert_not_called()
                    self.assertFalse((output / 'build-summary.json').exists())
                else:
                    self.assertEqual(machine.build(**arguments), output if compile_only else root / 'fixture.fcore')
                    fit.assert_called_once()
                    timing.assert_called_once()
                    self.assertEqual(export.call_count, int(not compile_only))
                if trace:
                    proof.assert_called_once_with(selected, root / 'quartus_sta', (S + name,))
                else:
                    proof.assert_not_called()


class RetimedTensionProofTests(unittest.TestCase):
    NODE = S + 'machine_at_select_fdc~0_BLOCK_INPUT_MUX_PASSTHROUGH_X88_Y14_N0_I62_dff'
    VERSION = 'Version 26.3.0 Build 114 10/14/2026 SC Pro Edition'

    def test_located_targets_bind_to_drc_coordinates(self):
        device_classes = ('C1', 'C4', 'C8', 'R0', 'R1', 'R2', 'R4', 'R6', 'R12')
        located = [S + f'machine_at_cycle_strobe~1_{kind}_X79_Y8_N0_I4_dff' for kind in device_classes]
        duplicate = S + 'cpu_biu_reset~_Duplicate_2_BLOCK_INPUT_MUX_PASSTHROUGH_X50_Y18_N0_I20_dff'
        enable = S + 'cpu_wb_deliver~ENA_dff'
        rows = [[name, '(79, 8)', '4820', '(37, 28)', '40', '193686', ''] for name in located]
        rows.append([duplicate, '(50, 18)', '17059', '(40, 28)', '23', '406656', ''])
        rows.append([enable, '(31, 44)', '4820', '(37, 28)', '40', '193686', ''])
        bases = TABLE.CPU_CONTROL_TENSION, TABLE.RETIMED_CONTROL_TENSION
        drc = report({'TMC-20602': (TENSION, rows)}, severity='Low')
        self.assertEqual(retimed_tension.reviewed_targets(drc, *bases), (*located, duplicate, enable))
        report_row = rows[2]
        for location in ('(78, 8)', '(79, 9)', '(79, 8) extra'):
            with self.subTest(location=location):
                candidate = [report_row[0], location, *report_row[2:]]
                drc = report({'TMC-20602': (TENSION, [candidate])}, severity='Low')
                with self.assertRaisesRegex(mf.BuildError, 'register location does not match'):
                    retimed_tension.reviewed_targets(drc, *bases)
        renamed = [TENSION[0], 'Location', *TENSION[2:]]
        drc = report({'TMC-20602': (renamed, [report_row])}, severity='Low')
        with self.assertRaisesRegex(mf.BuildError, 'register location column changed'):
            retimed_tension.reviewed_targets(drc, *bases)

    def test_refusal_diagnostic_still_dumps_unknown_targets(self):
        unknown = self.NODE.replace('BLOCK_INPUT_MUX_PASSTHROUGH', 'C99')
        row = [unknown, '(7, 9)', '4820', '(37, 28)', '40', '193686', '']
        drc = report({'TMC-20602': (TENSION, [row])}, severity='Low')
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            report_path, targets = root / 'fit.rpt', root / 'targets.txt'
            report_path.write_text(drc)
            self.assertEqual(retimed_tension.main(['targets', str(report_path), str(targets)]), 0)
            self.assertEqual(targets.read_text().splitlines(), [unknown])
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            retimed_tension.reviewed_targets(drc, TABLE.CPU_CONTROL_TENSION, TABLE.RETIMED_CONTROL_TENSION)

    def trace_rows(self):
        for corner in sorted(retimed_tension.CORNERS):
            for kind, direction in sorted(retimed_tension.CHECKS):
                row = dict.fromkeys(retimed_tension.COLUMNS, '')
                row.update(corner=corner, node=self.NODE, check=kind, direction=direction,
                           exact_matches='1', owner_clocks=MACHINE, async_edges='0', sync_stage='0', status='ok')
                if kind in ('setup', 'hold'):
                    row.update(from_clock=MACHINE, to_clock=MACHINE, slack_ns='0.000')
                    row.update({'from': S + 'reset', 'to': S + 'reset', direction: self.NODE})
                elif kind in ('recovery', 'removal'):
                    row['status'] = 'n/a'
                yield row

    def write_trace(self, path, rows, columns=retimed_tension.COLUMNS):
        with path.open('w', newline='') as stream:
            writer = csv.DictWriter(stream, fieldnames=columns, delimiter='\t')
            writer.writeheader()
            writer.writerows(rows)

    def structure_rows(self):
        for corner in sorted(retimed_tension.CORNERS):
            for direction in ('from', 'to'):
                row = dict.fromkeys(retimed_tension.STRUCTURE_COLUMNS, '')
                row.update(dict.fromkeys(retimed_tension.STRUCTURE_ZEROS, '0'))
                row.update(corner=corner, node=self.NODE, direction=direction, exact_matches='1', owner_clocks=MACHINE,
                           keepers='21', machine='21', status='clear', async_outputs='1', async_output_own_q='1',
                           async_output_ids='edge1', async_output_details=(
                               '{edge1 asynchronous 0 reg1 reg ' + self.NODE + ' pin1 pin ' + self.NODE + '|q}'))
                yield row

    def ucp(self, peer=S + 'unrelated'):
        def table(title, header=None, *rows):
            text = '+---+\n; ' + ' ; '.join(title) + ' ;\n+---+\n'
            if header:
                text += '; ' + ' ; '.join(header) + ' ;\n+---+\n'
                text += ''.join('; ' + ' ; '.join(row) + ' ;\n' for row in rows) + '+---+\n'
            return text + '\n'

        properties = retimed_tension.UCP_PROPERTIES
        output = table(('Unconstrained Paths',))
        output += table(('Unconstrained Paths Summary',), ('Property', 'Setup', 'Hold'),
                        *((name, '0', '0') for name in properties[:4]),
                        *((name, '1', '1') for name in properties[4:]))
        output += table(('Clock Status Summary',), ('Target', 'Clock', 'Type', 'Status'),
                        ('pll_output', MACHINE, 'Generated', 'Constrained'))
        for phase in ('Setup', 'Hold'):
            output += table((phase + ' Analysis',))
            output += table((properties[4],), ('Output Port', 'Comment'), ('HDMI_MCLK', 'No output delay'))
            output += table((properties[5],), ('From', 'To', 'From Clocks'), (peer, 'HDMI_MCLK', MACHINE))
        return output

    def test_proof_refuses_missing_duplicate_or_disqualified_paths(self):
        with tempfile.TemporaryDirectory() as work:
            target_file = Path(work) / 'targets.txt'
            paths = Path(work) / 'paths.tsv'
            target_file.write_text(self.NODE + '\n')
            rows = list(self.trace_rows())
            self.write_trace(paths, rows)
            self.assertTrue(retimed_tension.check(target_file, paths)['pass'])
            setup = next(index for index, row in enumerate(rows) if row['check'] == 'setup')
            cases = {'missing': rows[:-1], 'duplicate': rows + rows[:1]}
            for column, value in (('to_clock', PIXEL), ('owner_clocks', PIXEL), ('sync_stage', '1'),
                                  ('async_edges', '1'), ('exact_matches', '2'), ('slack_ns', 'nan'),
                                  ('slack_ns', '-0.001')):
                bad = {**rows[setup], column: value}
                cases[(column, value)] = rows[:setup] + [bad] + rows[setup + 1:]
            for damage, result in cases.items():
                with self.subTest(damage=damage):
                    self.write_trace(paths, result)
                    self.assertFalse(retimed_tension.check(target_file, paths)['pass'])

    def test_structure_refuses_incomplete_or_foreign_topology_and_cuts(self):
        rows = list(self.structure_rows())
        cases = {'missing': rows[:-1], 'duplicate': rows + rows[:1]}
        for column, value in (('owner_clocks', PIXEL), ('sync_stage', '1'), ('is_latch', '1'), ('async_inputs', '1'),
                              ('clock_outputs', '1'), ('exact_matches', '2'), ('foreign', '1'), ('unclocked', '1'),
                              ('port', '1'), ('unresolved', '1'), ('non_register', '1'), ('machine', '20'),
                              ('setup_inter_clock', '1'), ('setup_false_path', '1'), ('hold_inter_clock', '1'),
                              ('hold_false_path', '1'), ('async_output_other', '1'), ('async_output_unknown', '1'),
                              ('async_output_details', rows[0]['async_output_details'].replace('|q}', '|other}')),
                              ('async_output_details', rows[0]['async_output_details'].replace('asynchronous 0', 'asynchronous 1'))):
            cases[column, value] = [{**rows[0], column: value}, *rows[1:]]
        with tempfile.TemporaryDirectory() as work:
            structure = Path(work) / 'structure.tsv'
            self.write_trace(structure, rows, columns=retimed_tension.STRUCTURE_COLUMNS)
            self.assertTrue(retimed_tension.check_structure([self.NODE], structure)['pass'])
            for damage, bad in cases.items():
                with self.subTest(damage=damage):
                    self.write_trace(structure, bad, columns=retimed_tension.STRUCTURE_COLUMNS)
                    self.assertFalse(retimed_tension.check_structure([self.NODE], structure)['pass'])

    def test_unconstrained_report_refuses_target_or_missing_details(self):
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            target_file, paths, structure, ucp = (root / name for name in ('targets', 'paths', 'structure', 'ucp'))
            target_file.write_text(self.NODE + '\n')
            self.write_trace(paths, self.trace_rows())
            self.write_trace(structure, self.structure_rows(), columns=retimed_tension.STRUCTURE_COLUMNS)
            with self.assertRaisesRegex(mf.BuildError, 'requires both structure'):
                retimed_tension.check(target_file, paths, structure_path=structure)
            with self.assertRaisesRegex(mf.BuildError, 'missing/linked retimed'):
                retimed_tension.check(target_file, paths, structure_path=structure, unconstrained_path=ucp)
            for peer, passed in ((self.NODE + '_neighbor', True), (self.NODE, False), (self.NODE + '|q', False)):
                with self.subTest(peer=peer):
                    ucp.write_text(self.ucp(peer))
                    summary = retimed_tension.check(target_file, paths, structure_path=structure, unconstrained_path=ucp)
                    self.assertEqual(summary['pass'], passed)
                    self.assertEqual(summary['unconstrained']['matching_subjects'], [] if passed else [self.NODE])
            good = self.ucp()
            for damaged in (good.split('; Hold Analysis')[0], good.replace('; ' + S + 'unrelated ; HDMI_MCLK ; ' + MACHINE + ' ;\n', ''),
                            good.replace(' ; Constrained ;', ' ; Partially constrained ;'),
                            good.rsplit('+---+', 1)[0], good + 'Table was truncated at 10 items.\n'):
                ucp.write_text(damaged)
                with self.assertRaisesRegex(mf.BuildError, 'unconstrained-path'):
                    retimed_tension.check(target_file, paths, structure_path=structure, unconstrained_path=ucp)

    def test_collector_uses_a_private_fitted_copy_and_binds_its_receipt(self):
        sha = retimed_tension.async_inputs.sha
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            selected = root / 'signoff'
            report_path = selected / retimed_tension.DRC
            qdb = retimed_tension.PROJECT / 'qdb/report.rdb'
            original = selected / qdb
            original.parent.mkdir(parents=True)
            original.write_bytes(b'fitted database')
            report_path.parent.mkdir()
            fit_version_path = selected / retimed_tension.FIT_VERSION
            fit_version_path.write_text(self.VERSION + '\n')
            row = [self.NODE, '(88, 14)', '4820', '(37, 28)', '40', '193686', '']
            report_path.write_text(report({'TMC-20602': (TENSION, [row])}, severity='Low'))
            sources = {path.relative_to(ROOT).as_posix(): sha(path)
                       for path in (Path(retimed_tension.__file__), retimed_tension.TCL, retimed_tension.STRUCTURE_TCL)}
            (root / 'build-inputs.json').write_text(json.dumps({'source_inputs': sources, 'parameters': {'model': '486dx'}}))
            tool = root / 'quartus_sta'
            tool.write_bytes(b'fixture tool')

            def bindings(directory):
                return {'database': sha(directory / qdb), 'record': sha(directory.parent / 'build-inputs.json')}

            def run_fixture(command, *, cwd, **options):
                self.assertEqual(Path(command[0]), tool)
                self.assertEqual(options['timeout'], retimed_tension.TIMEOUT_SECONDS)
                self.assertNotEqual(cwd, selected / retimed_tension.PROJECT)
                self.assertTrue(cwd.is_relative_to(selected / 'retimed-tension'))
                self.assertEqual((cwd / 'qdb/report.rdb').read_bytes(), original.read_bytes())
                (cwd / 'qdb/report.rdb').write_bytes(b'mutated private report cache')
                options['stdout'].write(('    Info: ' + self.VERSION + '\n').encode())
                self.write_trace(Path(command[-3]), self.trace_rows())
                self.write_trace(Path(command[-2]), self.structure_rows(), columns=retimed_tension.STRUCTURE_COLUMNS)
                Path(command[-1]).write_text(self.ucp())
                return subprocess.CompletedProcess(command, 0)

            with (mock.patch.object(retimed_tension, 'held_quartus_lease', return_value='/fixture/pro.lock'),
                  mock.patch.object(retimed_tension.async_inputs, 'bindings', side_effect=bindings) as bound,
                  mock.patch.object(retimed_tension.subprocess, 'run', side_effect=run_fixture)):
                output = retimed_tension.collect(selected, tool, (self.NODE,))
            self.assertEqual(original.read_bytes(), b'fitted database')
            self.assertEqual(bound.call_args_list[0], bound.call_args_list[-1])
            receipt = json.loads((output / 'receipt.json').read_text())
            self.assertEqual(receipt['bindings'], bindings(selected))
            self.assertEqual(receipt['sources_sha256'], sources)
            self.assertEqual(receipt['tool']['version'], self.VERSION)
            self.assertEqual(receipt['tool']['fit_version_sha256'], sha(fit_version_path))
            self.assertEqual(receipt['tool']['log_sha256'], sha(output / 'quartus-sta.log'))
            self.assertEqual(receipt['reports_sha256'], {name: sha(output / name) for name in retimed_tension.REPORTS})
            self.assertTrue(json.loads((output / 'summary.json').read_text())['pass'])

    def test_collector_refuses_to_write_without_a_pro_lease(self):
        with tempfile.TemporaryDirectory() as work:
            selected = Path(work)
            with (mock.patch.object(retimed_tension, 'held_quartus_lease', return_value=None) as held,
                  self.assertRaisesRegex(mf.BuildError, 'requires a Quartus Pro lease')):
                retimed_tension.collect(selected, selected / 'quartus_sta', (self.NODE,))
            held.assert_called_once_with('pro')
            self.assertEqual(list(selected.iterdir()), [])


class MachineFindingsTests(unittest.TestCase):
    def capture(self):
        return [cdc(f'frame_width[{i}]', f'machine_presenter_latest_width[{i}]') for i in range(10)] + [
            cdc(f'frame_height[{i}]', f'machine_presenter_latest_height[{i}]') for i in range(10)]

    def bases(self, *names):
        return [b for b in TABLE.BASES if b['name'] in names]

    def test_every_row_matches_exactly_one_basis_with_its_constraint(self):
        drc = report({'CDC-50001': (CDC, self.capture())})
        [entry] = mf.review(drc, sdc_text(), self.bases('presenter-capture'))
        self.assertEqual((entry['id'], entry['violations'], entry['basis']), ('CDC-50001', 20, mf.POLICY))
        [group] = entry['groups']
        self.assertEqual((group['name'], group['rows']), ('presenter-capture', 20))
        self.assertIn('machine_presenter_sequence_sync', group['qualifier'])
        self.assertTrue(any('frame_sequence' in line for line in group['constraint']))

    def test_an_unreviewed_row_a_wrong_count_a_wrong_clock_or_a_missing_constraint_refuses(self):
        bases = self.bases('presenter-capture')
        extra = self.capture() + [[S.replace('system|', '') + 'window_control|base_lo[3]',
                                   S.replace('system|', '') + 'arbiter|w_address[3]',
                                   'pll_inst|iopll_0_outclk0', MACHINE, 'Asynchronous transfer', '']]
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'CDC-50001': (CDC, extra)}), sdc_text(), bases)
        with self.assertRaisesRegex(mf.BuildError, 'covers 19 rows, reviewed 20'):
            mf.review(report({'CDC-50001': (CDC, self.capture()[:-1])}), sdc_text(), bases)
        reversed_clock = self.capture()[:-1] + [cdc('frame_height[9]', 'machine_presenter_latest_height[9]',
                                                    PIXEL, MACHINE)]
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'CDC-50001': (CDC, reversed_clock)}), sdc_text(), bases)
        missing = sdc_text().replace(TABLE.CAPTURE_SDC[2] + '\n', '')
        with self.assertRaisesRegex(mf.BuildError, 'not in the compiled SDC'):
            mf.review(report({'CDC-50001': (CDC, self.capture())}), missing, bases)
        waived = self.capture()
        waived[0] = waived[0][:-1] + ['Yes']
        with self.assertRaisesRegex(mf.BuildError, 'waived'):
            mf.review(report({'CDC-50001': (CDC, waived)}), sdc_text(), bases)

    def test_advisories_bind_by_hierarchy_and_record_their_count(self):
        rows = [[S + name, '(1, 1)', '9000', '(2, 2)', '20', '200000', '']
                for name in ('cpu_md_busy', 'cpu_wb_fault[4]', '_36426_[140]~DUPLICATE', 'reset',
                             'cpu_biu_reset~_Duplicate_2', 'cpu_biu_reset~_Duplicate', 'reset~DUPLICATE',
                             'machine_at_decoder_state[1]', 'machine_at_decoder_state[0]~DUPLICATE',
                             'machine_at_decoder_phase[5]', 'machine_at_decoder_phase[2]~DUPLICATE',
                             'machine_at_decoder_cycle_port[15]', 'audio_dsp_write', 'audio_dsp_write~_Duplicate_1',
                             # Every copy spelling the recorded fits use.
                             'cpu_md_busy~_Duplicate1', 'cpu_md_busy~_Duplicate0', 'reset~_Duplicate1DUPLICATE',
                             'machine_at_decoder_state[1]~_Duplicate_2DUPLICATE', 'cpu_wb_fault[4]~_DuplicateDUPLICATE')]
        [entry] = mf.review(report({'TMC-20602': (TENSION, rows)}), '', self.bases('cpu-control-tension'))
        self.assertEqual(entry['groups'][0]['rows'], 19)
        # A fitter copy is admitted only for a name the basis already names; neighbouring decoder,
        # audio and machine names are not.
        for name in ('machine_at_pic_mask[3]~_Duplicate_2', 'cpu_biu_reset~_Duplicate_', 'cpu_biu_reset_Duplicate_2',
                     'machine_at_decoder_cycle_write', 'machine_at_decoder_next_port[3]~DUPLICATE',
                     'machine_at_decoder_phase_x[1]', 'audio_dsp_read~DUPLICATE', 'audio_dsp_write_data[3]',
                     'machine_audio_dsp_write', 'machine_at_decoder_state_x[1]',
                     'cpu_md_busy~Duplicate', 'reset~DUPLICATE~DUPLICATE',
                     # Malformed copy suffixes.
                     'reset~DUPLICATE1', 'reset~_Duplicate1_', 'reset~_Duplicate1DUPLICATE2', 'reset~_Duplicate~DUPLICATE',
                     'reset~_Duplicate1duplicate', 'reset~_duplicate1', 'reset~_Duplicate_1_2', 'reset~_DuplicateX',
                     # Stacked mixtures of the two spellings: no recorded fit writes one.
                     'reset~DUPLICATE~_Duplicate_1', 'reset~DUPLICATE~_Duplicate_2DUPLICATE', 'reset~DUPLICATE~_Duplicate', 'reset~DUPLICATE~_DuplicateDUPLICATE', 'cpu_biu_reset~DUPLICATE~_Duplicate1', '_36426_[140]~DUPLICATE~_Duplicate_1'):
            copy = rows + [[S + name, '(1, 1)', '9000', '(2, 2)', '20', '200000', '']]
            with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                mf.review(report({'TMC-20602': (TENSION, copy)}), '', self.bases('cpu-control-tension'))
        outside = rows + [['fes_video_inst|output_release[0]', '(1, 1)', '9000', '(2, 2)', '20', '200000', '']]
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'TMC-20602': (TENSION, outside)}), '', self.bases('cpu-control-tension'))

    def test_retimed_tension_covers_measured_roles_and_anonymous_aliases(self):
        names = (
            'cpu_m1_tx~1_BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I13_dff',
            'cpu_l1d_hit_m1[0]~13xsyn_BLOCK_INPUT_MUX_PASSTHROUGH_X64_Y27_N0_I1_dff',
            'cpu_m2_load~ENA_dff',
            'cpu_m2_load~SCLR_dff',
            'machine_at_select_fdc~0_R12_X84_Y16_N0_I4_dff',
            '_36681_[49]~BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I20_dff',
            '_36681_[715]~DUPLICATE_BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I3_dff',
            '_05547_~0_BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I2_dff',
            '_14602_~2_BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I39_dff',
            '_17339_~0_BLOCK_INPUT_MUX_PASSTHROUGH_X64_Y27_N0_I3_dff',
            'reduce_nor_1931~BLOCK_INPUT_MUX_PASSTHROUGH_X46_Y19_N0_I31_dff',
            # Seed 2 adds these fitter spellings for the same logical roles.
            'cpu_m1_tx~3xsyn_BLOCK_INPUT_MUX_PASSTHROUGH_X48_Y33_N0_I76_dff',
            'cpu_m1_tx~3xsyn_5_BLOCK_INPUT_MUX_PASSTHROUGH_X48_Y33_N0_I77_dff',
            'cpu_m1_tx~3xsyn_6_BLOCK_INPUT_MUX_PASSTHROUGH_X48_Y33_N0_I74_dff',
            'cpu_m1_tx~3xsyn_7_BLOCK_INPUT_MUX_PASSTHROUGH_X48_Y33_N0_I79_dff',
            'cpu_m1_tx~3xsyn_7_BLOCK_INPUT_MUX_PASSTHROUGH_X48_Y33_N0_I73_dff',
            'cpu_m1_out_reg[1178]~DUPLICATE_BLOCK_INPUT_MUX_PASSTHROUGH_X49_Y25_N0_I5_dff',
            '_17339_~1_R6_X56_Y33_N0_I13_dff',
            'machine_at_select_fdc~0_R6_X84_Y17_N0_I13_dff',
            # Seed 3 adds one M1 qualifier variant and three latched I/O port bits.
            '_05547_~1_C4_X50_Y28_N0_I9_dff',
            'machine_at_decoder_cycle_port[6]~R6_X83_Y16_N0_I9_dff',
            'machine_at_decoder_cycle_port[7]~R6_X82_Y16_N0_I7_dff',
            'machine_at_decoder_cycle_port[5]~R12_X82_Y16_N0_I0_dff',
            # Seed 4 traces more port bits, plus the ISA strobe and CPU writeback.
            '_17339_~1_R4_X60_Y28_N0_I15_dff',
            'machine_at_decoder_cycle_port[10]~R6_X84_Y16_N0_I11_dff',
            'machine_at_decoder_cycle_port[4]~R6_X87_Y16_N0_I3_dff',
            'machine_at_decoder_cycle_port[9]~R6_X85_Y14_N0_I9_dff',
            'machine_at_decoder_cycle_port[8]~R6_X85_Y14_N0_I7_dff',
            'machine_at_decoder_cycle_port[11]~R6_X84_Y14_N0_I11_dff',
            'cpu_wb_fault[1]~BLOCK_INPUT_MUX_PASSTHROUGH_X53_Y20_N0_I4_dff',
            'cpu_wb_s_reg[1178]~C4_X40_Y16_N0_I30_dff',
            'machine_at_decoder_cycle_port[5]~R6_X89_Y14_N0_I19_dff',
            'machine_at_decoder_cycle_port[7]~R4_X90_Y14_N0_I15_dff',
            'machine_at_cycle_strobe~1_R4_X86_Y14_N0_I13_dff',
            'machine_at_decoder_cycle_port[6]~R4_X84_Y16_N0_I13_dff',
            # The changed BUILD_ID moves these existing I/O roles to input muxes.
            'machine_at_select_fdc~0_BLOCK_INPUT_MUX_PASSTHROUGH_X88_Y14_N0_I62_dff',
            'machine_at_cycle_strobe~0_BLOCK_INPUT_MUX_PASSTHROUGH_X83_Y15_N0_I78_dff',
            'cpu_m1_out[20]~1_BLOCK_INPUT_MUX_PASSTHROUGH_X51_Y34_N0_I66_dff',
            'cpu_m1_alias~43xsyn_BLOCK_INPUT_MUX_PASSTHROUGH_X51_Y34_N0_I79_dff',
            'add_146~1_BLOCK_INPUT_MUX_PASSTHROUGH_X51_Y34_N0_I68_dff',
            'reduce_or_2303~BLOCK_INPUT_MUX_PASSTHROUGH_X51_Y34_N0_I69_dff',
            'cpu_m1_okA~0_BLOCK_INPUT_MUX_PASSTHROUGH_X51_Y34_N0_I71_dff',
            'cpu_m1_out_reg[1178]~DUPLICATEENA_dff',
            'cpu_m1_out_reg[1178]~DUPLICATESCLR_dff',
            'machine_at_decoder_cycle_port[7]~C4_X82_Y13_N0_I13_dff',
            'machine_at_decoder_phase[3]~DUPLICATE_BLOCK_INPUT_MUX_PASSTHROUGH_X84_Y15_N0_I13_dff',
            'cpu_m1_tx~3xsyn_8_BLOCK_INPUT_MUX_PASSTHROUGH_X47_Y27_N0_I23_dff',
            'machine_at_select_fdc~0_R4_X92_Y17_N0_I13_dff',
            'machine_at_decoder_cycle_port[8]~BLOCK_INPUT_MUX_PASSTHROUGH_X99_Y17_N0_I37_dff',
            'add_307~2_BLOCK_INPUT_MUX_PASSTHROUGH_X69_Y37_N0_I25_dff',
            'add_307~7_BLOCK_INPUT_MUX_PASSTHROUGH_X69_Y37_N0_I27_dff',
            'add_307~12_BLOCK_INPUT_MUX_PASSTHROUGH_X69_Y37_N0_I29_dff',
            'add_307~17_BLOCK_INPUT_MUX_PASSTHROUGH_X69_Y37_N0_I28_dff',
            '_06220_~0_BLOCK_INPUT_MUX_PASSTHROUGH_X69_Y37_N0_I30_dff',
            'i11525~0_BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I52_dff',
            'i11562~0_BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I53_dff',
            'machine_at_decoder_state[0]~BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I54_dff',
            'i11562~xsyn_BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I49_dff',
            'machine_at_decoder_next_vga~3xsyn_BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I51_dff',
            'i11562~xsyn_BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I48_dff',
            'machine_at_decoder_next_vga~3xsyn_BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I50_dff',
            'machine_at_decoder_state[1]~DUPLICATE_BLOCK_INPUT_MUX_PASSTHROUGH_X81_Y14_N0_I55_dff',
            'machine_at_decoder_cycle_port[9]~BLOCK_INPUT_MUX_PASSTHROUGH_X84_Y15_N0_I56_dff',
            'cpu_wb_fault[2]~C4_X52_Y18_N0_I38_dff',
            'machine_at_decoder_cycle_port[5]~BLOCK_INPUT_MUX_PASSTHROUGH_X83_Y15_N0_I1_dff',
            'i48682~0_C4_X54_Y18_N0_I38_dff',
            '_17339_~1_C4_X57_Y26_N0_I7_dff',
            'add_309~3_BLOCK_INPUT_MUX_PASSTHROUGH_X69_Y37_N0_I25_dff',
            'cpu_biu_drain~DUPLICATESCLR_dff',
            'cpu_wb_deliver~ENA_dff',
            'cpu_wb_deliver~SCLR_dff',
            '_36683_[140]~ENA_dff',
            '_36683_[182]~ENA_dff',
            '_36689_[205]~ENA_dff',
            'cpu_wb_fault[4]~ENA_dff',
            '_36683_[141]~ENA_dff',
        )
        rows = [[S + name, '(58, 23)', '9000', '(38, 28)', '20', '200000', '']
                for name in (*names, 'cpu_md_busy')]
        bases = self.bases('cpu-control-tension', 'retimed-machine-control-tension')
        [entry] = mf.review(report({'TMC-20602': (TENSION, rows)}), '', bases)
        self.assertEqual({group['name']: group['rows'] for group in entry['groups']},
                         {'cpu-control-tension': 1, 'retimed-machine-control-tension': len(names)})

    def test_retimed_tension_rejects_names_without_verified_placement_shapes(self):
        names = (
            S + 'machine_presenter_loaded_flag',
            S + 'plain_port[12]',
            S + 'cpu_m1_tx~1_BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I13_dff_extra',
            S.replace('system|', 'window_control|') + 'cpu_m2_load~ENA_dff',
            S.replace('system|', 'window_control|') + 'i48682~0_C4_X54_Y18_N0_I38_dff',
            S + 'nested|i48682~0_C4_X54_Y18_N0_I38_dff',
            S + 'i48682~0_C4_X54_Y18_N0_I38_dff|q',
            S + 'i48682~0_C6_X54_Y18_N0_I38_dff',
            S + 'cpu_m1_tx~3xsyn__8_BLOCK_INPUT_MUX_PASSTHROUGH_X48_Y33_N0_I79_dff',
            S + 'cpu_biu_reset~_Duplicate__2_BLOCK_INPUT_MUX_PASSTHROUGH_X50_Y18_N0_I20_dff',
            S + '_17339_~1_R7_X56_Y33_N0_I13_dff',
            S + '_05548_~1_C4_X-1_Y28_N0_I9_dff',
            S + 'machine_at_decoder_cycle_port[*]~R6_X83_Y16_N0_I9_dff',
            S + 'cpu_m1_alias~43xsyn~BLOCK_INPUT_MUX_PASSTHROUGH_X51_Y34_N0_I79_dff',
            S + 'cpu_biu_reset~_Duplicate.2_BLOCK_INPUT_MUX_PASSTHROUGH_X50_Y18_N0_I20_dff',
            S + 'cpu_m1_out_reg[1178]~DUPLICATE_ENA_dff',
            S + 'cpu_m2_load~ACLR_dff',
            S + 'cpu_m2_load~1SCLR_dff',
            S + 'machine_presenter_loaded_flag~SCLR_dff',
            S + 'cpu_wb_deliver~DUPLICATEENA_dff_extra',
            S + '_36689_[*]~SCLR_dff',
        )
        bases = self.bases('cpu-control-tension', 'retimed-machine-control-tension')
        for name in names:
            with self.subTest(name), self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                rows = [[name, '(58, 23)', '9000', '(38, 28)', '20', '200000', '']]
                mf.review(report({'TMC-20602': (TENSION, rows)}), '', bases)

    def test_the_lint_bases_admit_their_drivers_and_fitter_copies_of_them_only(self):
        header = ['Driver', 'Asynchronous Reset Signal', 'Synchronous Reset Signal', 'Clock Enable Signal',
                  'Number of Asynchronous Reset Signals', 'Number of Synchronous Reset Signals',
                  'Number of Clock Enable Signals', 'Waived']

        def row(driver):
            return [S + driver, '--', S + 'cpu_int_take|SCLR', S + 'cpu_q16|ENA', '0', '1482', '1', '']

        bases = self.bases('biu-reset', 'machine-reset')
        # slat23b seed 2's three rows, then the other copy spellings.
        drivers = ['cpu_biu_reset~_Duplicate_1', 'cpu_biu_reset~_Duplicate', 'reset', 'cpu_biu_reset',
                   'cpu_biu_reset~DUPLICATE', 'cpu_biu_reset~_Duplicate1',
                   'cpu_biu_reset~_Duplicate1DUPLICATE', 'reset~_Duplicate_2DUPLICATE', 'reset~_DuplicateDUPLICATE']
        [entry] = mf.review(report({'LNT-30010': (header, [row(d) for d in drivers])}), '', bases)
        self.assertEqual([g['rows'] for g in entry['groups']], [6, 3])
        for driver in ('machine_at_decoder_state[1]~DUPLICATE', 'cpu_biu_reset~_Duplicate_', 'cpu_md_busy~DUPLICATE',
                       'reset_n~DUPLICATE', 'cpu_biu_reset_q', 'reset~_Duplicate~DUPLICATE',
                       'cpu_biu_reset~_Duplicate1_', 'reset~DUPLICATE2', 'cpu_biu_reset~_Duplicate1DUPLICATE~DUPLICATE',
                       'reset~DUPLICATE~_Duplicate_1', 'reset~DUPLICATE~_Duplicate_2DUPLICATE', 'reset~DUPLICATE~_Duplicate', 'reset~DUPLICATE~_DuplicateDUPLICATE', 'cpu_biu_reset~DUPLICATE~_Duplicate1', 'reset~DUPLICATE~_Duplicate_3'):
            with self.subTest(driver), self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                mf.review(report({'LNT-30010': (header, [row(driver)])}), '', bases)

    def test_the_toggle_basis_needs_its_direction_and_the_board_assignments(self):
        rows = [cdc('machine_frame_sequence[0]', 'machine_presenter_sequence_sync[0]'),
                cdc('machine_presenter_loaded_flag', 'machine_presenter_loaded_sync[0]'),
                cdc('machine_presenter_request_toggle', 'machine_presenter_request_sync[0]', PIXEL, MACHINE)]
        bases = self.bases('presenter-sequence-toggle', 'presenter-loaded-toggle', 'presenter-toggle-to-machine')
        [entry] = mf.review(report({'CDC-50002': (CDC, rows)}), sdc_text(), bases)
        self.assertEqual([g['rows'] for g in entry['groups']], [1, 1, 1])
        # Each toggle's SDC cuts bit 0 into the first stage only.
        for label, row in (('a later sequence bit', cdc('machine_frame_sequence[3]', 'machine_presenter_sequence_sync[0]')),
                           ('a later stage', cdc('machine_frame_sequence[0]', 'machine_presenter_sequence_sync[1]')),
                           ('an indexed loaded flag', cdc('machine_presenter_loaded_flag[0]',
                                                          'machine_presenter_loaded_sync[0]')),
                           ('one toggle into another\'s synchroniser',
                            cdc('machine_frame_sequence[0]', 'machine_presenter_loaded_sync[0]'))):
            with self.subTest(label), self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                mf.review(report({'CDC-50002': (CDC, rows[:1] + [row] + rows[2:])}), sdc_text(), bases)
        rows[2] = cdc('machine_presenter_request_toggle', 'machine_presenter_request_sync[0]')
        with self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
            mf.review(report({'CDC-50002': (CDC, rows)}), sdc_text(), bases)
        qsf = ''.join('set_instance_assignment -name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS '
                      f'-to "{S}{name}"\n' for name in TABLE.TOGGLE_ASSIGNMENT)
        mf.assignments(qsf, TABLE.TOGGLE_ASSIGNMENT)
        with self.assertRaisesRegex(mf.BuildError, 'machine_presenter_loaded_sync'):
            mf.assignments(qsf.replace('machine_presenter_loaded_sync', 'other'), TABLE.TOGGLE_ASSIGNMENT)
        from boards.de25nano.producers import build_pc386_package as pc386
        spec = pc386.MACHINE.spec(ROOT)
        self.assertEqual({node for _, _, node in spec.ASSIGNMENTS} - {'system|machine_pixel_reset_sync[*]'},
                         {'system|' + name for name in TABLE.TOGGLE_ASSIGNMENT})

    def test_the_bases_are_bound_to_the_handshake_sources_the_record_captured(self):
        import hashlib
        record = {'source_inputs': {**TABLE.SOURCES, TABLE.TABLE: hashlib.sha256(
            (ROOT / TABLE.TABLE).read_bytes()).hexdigest()}}
        self.assertEqual(set(mf.sources(record, TABLE)), {*TABLE.SOURCES, TABLE.TABLE})
        for name in TABLE.SOURCES:
            self.assertEqual(hashlib.sha256((ROOT / name).read_bytes()).hexdigest(), TABLE.SOURCES[name])
            changed = {'source_inputs': {**record['source_inputs'], name: '0' * 64}}
            with self.assertRaisesRegex(mf.BuildError, 'review it again'):
                mf.sources(changed, TABLE)
        with self.assertRaisesRegex(mf.BuildError, 'basis table'):
            mf.sources({'source_inputs': {**record['source_inputs'], TABLE.TABLE: '0' * 64}}, TABLE)

    def presenter_cdc_50001(self):
        return self.capture() + [
            cdc(f'machine_presenter_epoch[{i}]', f'machine_presenter_f_epoch[{i}]', PIXEL, MACHINE)
            for i in range(35)] + [
            cdc(f'machine_presenter_loaded_in[{i}]', f'machine_presenter_loaded[{i}]') for i in range(12)]

    def window_groups(self, rows):
        [entry] = mf.review(report({'CDC-50001': (CDC, rows)}), sdc_text(), TABLE.BASES)
        return {g['name']: g['rows'] for g in entry['groups'] if g['name'].startswith('window-')}

    def test_the_window_bases_admit_their_bundles_in_any_number_and_nothing_else(self):
        w = S.replace('system|', '') + 'window_control|'
        # i486DX slice 1 seed 1's nine rows, verbatim; #2036's own fits report none.
        base = [[w + f'base_lo[{i}]', w + f'base_machine[{i}]', TABLE.SYS, MACHINE, 'Asynchronous transfer', '']
                for i in (13, 14, 15, 16, 22, 28, 29, 30, 31)]
        fault = [[w + f'fault_launch[{i}]', w + f'machine_fault[{i}]', MACHINE, TABLE.SYS, 'Asynchronous transfer', '']
                 for i in range(5)]
        duplicate = [[w + 'base_lo[20]~DUPLICATE', w + 'base_machine[20]', TABLE.SYS, MACHINE,
                      'Asynchronous transfer', '']]
        presenter = self.presenter_cdc_50001()
        self.assertEqual(self.window_groups(presenter), {'window-base': 0, 'window-fault-record': 0})
        self.assertEqual(self.window_groups(presenter + base + fault + duplicate),
                         {'window-base': 10, 'window-fault-record': 5})
        unreviewed = {
            'a base_lo outside window_control': [S + 'base_lo[13]', w + 'base_machine[13]', TABLE.SYS, MACHINE],
            'an arbiter base_lo': [S.replace('system|', '') + 'arbiter|base_lo[13]', w + 'base_machine[13]',
                                   TABLE.SYS, MACHINE],
            'base_lo into another sink': [w + 'base_lo[3]', S.replace('system|', '') + 'arbiter|w_address[3]',
                                          TABLE.SYS, MACHINE],
            'base_hi': [w + 'base_hi[13]', w + 'base_machine[13]', TABLE.SYS, MACHINE],
            'the base in reverse': [w + 'base_lo[13]', w + 'base_machine[13]', MACHINE, TABLE.SYS],
            'the base from the pixel clock': [w + 'base_lo[13]', w + 'base_machine[13]', PIXEL, MACHINE],
            'the fault record in reverse': [w + 'fault_launch[0]', w + 'machine_fault[0]', TABLE.SYS, MACHINE],
            'the fault record into the base': [w + 'fault_launch[0]', w + 'base_machine[0]', MACHINE, TABLE.SYS],
            'a ~_Duplicate base_lo, outside the SDC bundle': [w + 'base_lo[13]~_Duplicate', w + 'base_machine[13]',
                                                              TABLE.SYS, MACHINE],
            'a ~_Duplicate_1 base_lo': [w + 'base_lo[13]~_Duplicate_1', w + 'base_machine[13]', TABLE.SYS, MACHINE],
            'a ~DUPLICATE fault_launch, outside the SDC bundle': [w + 'fault_launch[0]~DUPLICATE', w + 'machine_fault[0]',
                                                                  MACHINE, TABLE.SYS],
            'a ~_Duplicate fault_launch': [w + 'fault_launch[0]~_Duplicate1', w + 'machine_fault[0]', MACHINE, TABLE.SYS],
            'a stacked copy mixture': [w + 'base_lo[13]~DUPLICATE~_Duplicate1', w + 'base_machine[13]', TABLE.SYS, MACHINE],
            'a malformed copy': [w + 'base_lo[13]~DUPLICATE_x', w + 'base_machine[13]', TABLE.SYS, MACHINE],
        }
        for label, row in unreviewed.items():
            with self.subTest(label), self.assertRaisesRegex(mf.BuildError, 'matches 0 bases'):
                self.window_groups(presenter + base + [row + ['Asynchronous transfer', '']])
        for basis, line in (('window-base', TABLE.WINDOW_BASE_SDC[0]), ('window-fault-record', TABLE.WINDOW_FAULT_SDC[0])):
            with self.subTest(basis), self.assertRaisesRegex(mf.BuildError, basis + ' constraint not in the compiled SDC'):
                mf.review(report({'CDC-50001': (CDC, presenter)}), sdc_text().replace(line + '\n', ''), TABLE.BASES)

    def test_every_crossing_basis_admits_only_registers_its_sdc_collects(self):
        # A row a crossing basis admits must be a launch and a capture of one SDC entry the basis quotes,
        # so nothing the SDC leaves unbounded is approved. The entries come from pc386-clocks.sdc itself
        # (a basis may quote only part of one): a foreach entry's launches are its source(s), plus
        # source~DUPLICATE where the block collects it, and its captures are its sink(s) or the head
        # whose d pin it cuts. Quartus name patterns: * is any run of characters, brackets are literal.
        text = (ROOT / 'boards/de25nano/pc386/pc386-clocks.sdc').read_text()
        entries = {}
        for names, listing, body in re.findall(r'^foreach \{([^}]*)\} \{\n(.*?)\n\} \{\n(.*?)\n\}$', text, re.M | re.S):
            names = names.split()
            if not {'source', 'sources'} & set(names):
                continue  # the audio edge list names no register pair
            tokens = re.findall(r'\{([^}]*)\}|(\S+)', listing)
            tokens = [braced or bare for braced, bare in tokens]
            for i in range(0, len(tokens), len(names)):
                entry = dict(zip(names, tokens[i:i + len(names)]))
                launches = entry['sources'].split() if 'sources' in entry else [entry['source']]
                if 'source' in entry and '${source}~DUPLICATE' in body:
                    launches.append(entry['source'] + '~DUPLICATE')
                captures = (entry.get('sinks') or entry.get('sink') or entry['head']).split()
                entries.setdefault(entry['label'], []).append((launches, captures))
        launch = re.search(r'set launches \[get_registers "\$\{base\}\|(system\|reset)"\]', text).group(1)
        head = re.search(r'"\$\{base\}\|(system\|machine_pixel_reset_sync\[0\])\|d"', text).group(1)
        entries['pixel_reset'] = [([launch], [head])]

        def collects(globs, name):
            return any(re.fullmatch(re.escape(g).replace(r'\*', '[^ ]*'), name) for g in globs)

        prefix = 'soc_inst|subsys_periph|fes_scanout|fes_scanout|'
        stems = {re.sub(r'[\[~].*', '', g) for found in entries.values() for launches, captures in found
                 for g in launches + captures}
        suffixes = ['', '[0]', '[1]', '[3]', '[*]', '~DUPLICATE', '[0]~DUPLICATE', '[3]~DUPLICATE', '~_Duplicate',
                    '[3]~_Duplicate', '[3]~_Duplicate_1', '[3]~DUPLICATE~_Duplicate1', '_q', '[3]_q']
        corpus = [stem + suffix for stem in sorted(stems) for suffix in suffixes]
        bases = [b for b in TABLE.BASES if b['rule'].startswith('CDC-') and b['sdc']]
        self.assertEqual(len(bases), 11)
        for basis in bases:
            quoted = [label for line in basis['sdc'] for label in re.findall(r'^\s*(\w+) \{', line)]
            if basis['name'] == 'pixel-reset':
                quoted = ['pixel_reset']
            # Each quoted label names one entry (the GP blocks reuse theirs, but no basis quotes them).
            self.assertTrue(quoted and all(len(entries.get(label, ())) == 1 for label in quoted), (basis['name'], quoted))
            sources = [n for n in corpus if re.fullmatch(basis['match'][0], prefix + n)]
            sinks = [n for n in corpus if re.fullmatch(basis['match'][1], prefix + n)]
            self.assertTrue(sources and sinks, basis['name'])
            for source in sources:
                for sink in sinks:
                    self.assertTrue(any(collects(entries[label][0][0], source) and collects(entries[label][0][1], sink)
                                        for label in quoted), (basis['name'], source, sink))

    def test_a_rule_that_passed_has_no_entry(self):
        self.assertEqual(mf.review(report({'CDC-50001': (CDC, [])}), sdc_text(), TABLE.BASES), [])

    def test_only_pc386_selects_the_policy_and_the_pc386_record_does(self):
        with tempfile.TemporaryDirectory() as temporary:
            fit = Path(temporary) / 'signoff'
            fit.mkdir()
            for core, policy, selected in (('fes.pc386', mf.POLICY, True), ('fes.spectrum', None, False)):
                parameters = {'core_id': core, **({'signoff_findings_review_policy': policy} if policy else {})}
                (fit.parent / 'build-inputs.json').write_text(json.dumps({'parameters': parameters}))
                self.assertIs(mf.selected(fit), selected)
            (fit.parent / 'build-inputs.json').write_text(json.dumps(
                {'parameters': {'core_id': 'fes.spectrum', 'signoff_findings_review_policy': mf.POLICY}}))
            with self.assertRaisesRegex(mf.BuildError, 'another core'):
                mf.selected(fit)
        from boards.de25nano.producers import build_pc386_package as pc386
        self.assertEqual(pc386.MACHINE.spec(ROOT).record_parameters(ROOT)['signoff_findings_review_policy'],
                         mf.POLICY)


if __name__ == '__main__':
    unittest.main()
