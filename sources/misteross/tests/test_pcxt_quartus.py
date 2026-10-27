"""Whole-board diagnostic capture must retain failures and reject missing data."""
import csv
import copy
import gzip
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from scripts import quartus_fes_pcxt as builder
from scripts.pcxt_quartus_mailbox import validate_mailbox

YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


class MailboxBindingTests(unittest.TestCase):
    def setUp(self):
        path = Path(__file__).parent / 'fixtures/pcxt_quartus_mailbox.json.gz'
        fixture = json.loads(gzip.decompress(path.read_bytes()))
        self.before, self.after = fixture['before'], fixture['after']

    def flop(self, name):
        bits = self.after['netnames'][name]['bits']
        return next(c for c in self.after['cells'].values() if c['connections'].get('Q') == bits)

    def test_real_generic_declaration_and_chain_binding(self):
        result = validate_mailbox(self.before, self.after)
        self.assertEqual(len(result['chains']), 3)
        self.assertTrue(result['common_reset_assertion_verified'])
        self.assertEqual(result['held_data_stable_pixel_samples'], 2)
        self.assertFalse(result['physical_reset_and_metastability_qualified'])

    def test_missing_intent_or_ambiguous_declaration_rejects(self):
        self.before['netnames']['system.video.adapter.ack_meta']['attributes'].pop('async_reg')
        self.after['netnames']['system_video_adapter_ack_meta']['attributes'].pop('async_reg')
        with self.assertRaisesRegex(ValueError, 'missing synchronizer intent'):
            validate_mailbox(self.before, self.after)

    def test_ambiguous_declaration_rejects(self):
        self.after['netnames']['unowned_alias'] = copy.deepcopy(self.after['netnames']['system_video_adapter_sent'])
        with self.assertRaisesRegex(ValueError, 'ambiguous declaration'):
            validate_mailbox(self.before, self.after)

    def test_bypassed_stage_rejects(self):
        self.flop('system_video_adapter_sent_sync')['connections']['D'] = self.after['netnames']['system_video_adapter_sent']['bits']
        with self.assertRaisesRegex(ValueError, 'changed synchronizer connectivity'):
            validate_mailbox(self.before, self.after)

    def test_interstage_fanout_rejects(self):
        self.after['cells']['extra_consumer'] = {'type': '$not', 'parameters': {},
            'port_directions': {'A': 'input', 'Y': 'output'},
            'connections': {'A': self.after['netnames']['system_video_adapter_ack_meta']['bits'], 'Y': [999999]}}
        with self.assertRaisesRegex(ValueError, 'interstage logic or fanout'):
            validate_mailbox(self.before, self.after)

    def test_wrong_payload_clock_rejects(self):
        self.flop('system_video_adapter_front')['connections']['CLK'] = self.after['cells']['memory_clock']['connections']['outclk'][2:3]
        with self.assertRaisesRegex(ValueError, 'wrong launch/capture register front'):
            validate_mailbox(self.before, self.after)

    def test_independent_reset_or_changed_lock_logic_rejects(self):
        self.flop('board_reset_domain_board_reset')['connections']['ARST'] = [999999]
        with self.assertRaisesRegex(ValueError, 'independent reset assertion'):
            validate_mailbox(self.before, self.after)

    def test_changed_lock_logic_rejects(self):
        rst = self.flop('pixel_reset')['connections']['ARST']
        gate = next(c for c in self.after['cells'].values() if c['connections'].get('Y') == rst)
        gate['type'] = '$logic_or'
        with self.assertRaisesRegex(ValueError, 'changed common PLL-lock assertion'):
            validate_mailbox(self.before, self.after)

    def test_real_source_capture_contains_the_standalone_proof(self):
        with tempfile.TemporaryDirectory(prefix='pcxt-quartus-inputs-') as directory:
            source, identities = builder.capture_sources(builder.ROOT, Path(directory))
            for name in ('cores/fes-common/generated/fes_pcxt_isa.vh',
                         'cores/fes-common/vendor/kf8259/source.json',
                         'cores/fes-common/vendor/kf8253/source.json',
                         'cores/fes-common/vendor/kf8253/KF8253_Definitions.svh',
                         'tests/test_pcxt_video_mailbox.py', 'cores/fes-pcxt/quartus/mailbox.sdc',
                         'scripts/pcxt_quartus_mailbox.py'):
                self.assertEqual(builder.sha(source / name), identities[name])


@unittest.skipUnless(YOSYS, 'requires Yosys for the real elaboration path')
class QuartusElaborationTests(unittest.TestCase):
    def test_headers_byte_write_memory_and_signed_pll_parameters_survive(self):
        with tempfile.TemporaryDirectory(prefix='pcxt-quartus-elaborate-') as directory:
            base = Path(directory)
            source, work = base / 'source', base / 'work'
            includes = source / 'cores/fes-common/generated'
            includes.mkdir(parents=True)
            work.mkdir()
            (includes / 'fixture.vh').write_text('`define WORDS 8192\n')
            (source / 'adapter.v').write_text('// no boundary wrapper in this focused fixture\n')
            (source / 'top.sv').write_text('''
`include "fixture.vh"
module top #(parameter MDA=0, SCANLINES=0, HALF_RATE_BOARD=0)(
 input clk, we, high_byte, input [12:0] a,b, input[7:0] data,
 output reg[15:0] q_a,q_b, output pll_clock, locked);
reg[15:0] memory[0:`WORDS-1];
always @(posedge clk) begin
 q_a <= memory[a]; q_b <= memory[b];
 if(we) begin
  if(high_byte) memory[a][15:8] <= data;
  else memory[a][7:0] <= data;
 end
end
altera_pll #(.number_of_clocks(1),.duty_cycle0(50),
 .reference_clock_frequency("50.0 MHz"),.output_clock_frequency0("100.0 MHz"))
 pll(.refclk(clk),.rst(1'b0),.outclk(pll_clock),.locked(locked));
endmodule
''')
            library = Path(YOSYS).resolve().parent.parent / 'share/yosys/intel_alm/common/megafunction_bb.v'
            if not library.is_file():
                self.skipTest('Yosys installation lacks Intel ALM blackboxes')
            with mock.patch.object(builder, 'RTL', ('top.sv',)), \
                    mock.patch.object(builder, 'ADAPTERS', Path('adapter.v')):
                program = builder.elaboration(source, work, library)
            result = subprocess.run([YOSYS, '-p', program], cwd=work, capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            model = json.loads((work / 'elaborated.json').read_text())['modules']['top']
            memories = [c for c in model['cells'].values() if c['type'] == '$mem_v2']
            self.assertEqual(len(memories), 1)
            parameters = memories[0]['parameters']
            self.assertEqual(int(parameters['SIZE'], 2), 8192)
            self.assertEqual(int(parameters['RD_PORTS'], 2), 2)
            self.assertEqual(int(parameters['WR_PORTS'], 2), 1)
            self.assertEqual(int(parameters['RD_CLK_ENABLE'], 2), 3)
            self.assertEqual(int(parameters['RD_TRANSPARENCY_MASK'], 2), 0)
            self.assertEqual(int(parameters['RD_COLLISION_X_MASK'], 2), 0)
            emitted = (work / 'elaborated.v').read_text()
            self.assertIn(".duty_cycle0(32'sd50)", emitted)
            self.assertIn(".number_of_clocks(32'sd1)", emitted)


class QuartusCaptureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='pcxt-quartus-')
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        self.audit = self.work / 'audit'
        self.audit.mkdir()
        (self.work / 'audit.log').write_text('PCXT_AUDIT_CAPTURE_COMPLETE\n')
        self.corners = ['7_slow_1100mv_100c', '7_slow_1100mv_-40c',
                        'MIN_fast_1100mv_100c', 'MIN_fast_1100mv_-40c']
        self.clocks = ['cpu', 'pixel']
        (self.audit / 'clocks.tsv').write_text('name\tperiod_ns\n' +
                                               'cpu\t10.000\npixel\t13.464\n')
        self.counts, self.rows = [], []
        for corner in self.corners:
            folder = self.audit / corner
            folder.mkdir()
            (folder / 'pulse-width.rpt').write_text(
                'Report Minimum Pulse Width: Found 20 results (0 violated).  Worst case slack is 1.000\n')
            for kind in ('setup', 'hold'):
                for a in self.clocks:
                    for b in self.clocks:
                        count = int(a == b)
                        self.counts.append([corner, kind, a, b, count])
                        if count:
                            self.rows.append([corner, kind, a, b, 0, 0, 'launch', 'capture', 0.1, 10, 2])
            for kind in ('recovery', 'removal'):
                self.counts.append([corner, kind, '*', '*', 0])
        (self.audit / 'check-timing.rpt').write_text(
            '; no_clock ; 617 ;\n; no_input_delay ; 18 ;\n; no_output_delay ; 69 ;\n'
            '; loops ; 0 ;\n; latches ; 0 ;\n')
        (self.audit / 'fmax.tsv').write_text('corner\tclock\tfmax_mhz\trestricted_fmax_mhz\n' +
                                           ''.join(c + '\tcpu\t150\t150\n' for c in self.corners))
        self.save()

    def save(self):
        with (self.audit / 'counts.tsv').open('w') as stream:
            out = csv.writer(stream, delimiter='\t')
            out.writerow(['corner', 'kind', 'from_clock', 'to_clock', 'count'])
            out.writerows(self.counts)
        with (self.audit / 'endpoints.tsv').open('w') as stream:
            out = csv.writer(stream, delimiter='\t')
            out.writerow(['corner', 'kind', 'from_clock', 'to_clock', 'from_inverted', 'to_inverted',
                          'from', 'to', 'slack_ns', 'relationship_ns', 'data_delay_ns'])
            out.writerows(self.rows)

    def test_complete_capture_keeps_unqualified_interfaces_explicit(self):
        result = builder.timing_summary(self.work)
        self.assertTrue(result['capture_complete'])
        self.assertTrue(result['reported_endpoint_checks_pass'])
        self.assertFalse(result['whole_board_qualified'])
        self.assertEqual(result['timing_coverage_issues']['no_clock'], 617)
        self.assertEqual(result['timing_coverage_issues']['no_input_delay'], 18)
        self.assertEqual(len(result['minimum_pulse_width']), 4)

    def test_negative_slack_is_retained_as_a_failure(self):
        self.rows[0][8] = -0.125
        self.save()
        result = builder.timing_summary(self.work)
        self.assertFalse(result['reported_endpoint_checks_pass'])
        self.assertEqual(result['by_corner_and_check'][self.corners[0] + '/setup']['failures'], 1)
        self.assertFalse(result['whole_board_qualified'])

    def test_missing_endpoint_rejects(self):
        self.rows.pop()
        self.save()
        with self.assertRaisesRegex(ValueError, 'declared counts'):
            builder.timing_summary(self.work)

    def test_omitted_zero_count_clock_pair_rejects(self):
        # Even a pair with no paths is part of the declared comparison domain.
        self.counts = [r for r in self.counts if r[:4] != [self.corners[0], 'setup', 'cpu', 'pixel']]
        self.save()
        with self.assertRaisesRegex(ValueError, 'clock-pair/check matrix'):
            builder.timing_summary(self.work)

    def test_duplicate_or_truncated_count_rejects(self):
        self.counts.append(self.counts[0])
        self.save()
        with self.assertRaisesRegex(ValueError, 'invalid or truncated'):
            builder.timing_summary(self.work)
        self.counts.pop()
        self.counts[0][4] = 100000
        self.save()
        with self.assertRaisesRegex(ValueError, 'invalid or truncated'):
            builder.timing_summary(self.work)

    def test_partial_corner_or_nonfinite_path_rejects(self):
        self.counts = [r for r in self.counts if r[0] != self.corners[-1]]
        self.rows = [r for r in self.rows if r[0] != self.corners[-1]]
        self.save()
        with self.assertRaisesRegex(ValueError, 'four operating conditions'):
            builder.timing_summary(self.work)

    def test_nonfinite_path_rejects(self):
        self.rows[0][8] = 'nan'
        self.save()
        with self.assertRaisesRegex(ValueError, 'non-finite'):
            builder.timing_summary(self.work)

    def test_missing_completion_pulse_or_coverage_report_rejects(self):
        (self.work / 'audit.log').write_text('interrupted\n')
        with self.assertRaisesRegex(ValueError, 'did not complete'):
            builder.timing_summary(self.work)
        (self.work / 'audit.log').write_text('PCXT_AUDIT_CAPTURE_COMPLETE\n')
        p = self.audit / self.corners[0] / 'pulse-width.rpt'
        original = p.read_text()
        p.write_text('Report Minimum Pulse Width: Found 100000 results (0 violated). Worst case slack is 1.000')
        with self.assertRaisesRegex(ValueError, 'pulse-width'):
            builder.timing_summary(self.work)
        p.write_text(original)
        (self.audit / 'check-timing.rpt').write_text('missing\n')
        with self.assertRaisesRegex(ValueError, 'coverage checks'):
            builder.timing_summary(self.work)

    def test_existing_evidence_is_never_overwritten(self):
        summary = self.work / 'summary.json'
        summary.write_text('{"keep": "original evidence"}\n')
        original = summary.read_bytes()
        yosys = self.work / 'yosys'
        yosys.write_text('placeholder')
        with mock.patch.object(sys, 'argv', ['quartus_fes_pcxt.py', '--yosys', str(yosys),
                                           '--output', str(self.work)]), \
                mock.patch.object(builder, 'locate_quartus', return_value=(Path('/tools'), Path('/tools/bin/quartus_sh'))), \
                mock.patch.object(builder, 'quartus_version_line', return_value='17.0.2 Build 602 Lite Edition'), \
                mock.patch.object(builder, 'sha', return_value='hash'), \
                mock.patch.object(builder.subprocess, 'check_output', return_value='Yosys (git sha1 3a5accbd7)'), \
                mock.patch.object(sys, 'stderr', new_callable=io.StringIO) as err:
            self.assertEqual(builder.main(), 1)
            self.assertIn('File exists', err.getvalue())
        self.assertEqual(summary.read_bytes(), original)

    def test_selected_contract_requires_direct_interstage_capture(self):
        with (self.audit / 'mailbox-interstage.tsv').open('w') as stream:
            out = csv.writer(stream, delimiter='\t')
            out.writerow(['corner', 'kind', 'from', 'to', 'slack_ns'])
            for corner in self.corners:
                for kind in ('setup', 'hold'):
                    for role in ('ack', 'sent', 'hold'):
                        out.writerow([corner, kind, 'system_video_adapter_' + role + '_meta',
                                      'system_video_adapter_' + role + '_sync', '0.2'])
        result = builder.timing_summary(self.work, mailbox_contract=True)
        self.assertEqual(len(result['mailbox_interstage']), 24)
        lines = (self.audit / 'mailbox-interstage.tsv').read_text().splitlines()
        (self.audit / 'mailbox-interstage.tsv').write_text('\n'.join(lines[:-1]) + '\n')
        with self.assertRaisesRegex(ValueError, 'interstage checks'):
            builder.timing_summary(self.work, mailbox_contract=True)


if __name__ == '__main__':
    unittest.main()
