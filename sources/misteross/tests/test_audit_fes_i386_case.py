# SPDX-License-Identifier: MIT
"""Fail-closed checks for the netlist-only i386 assertion audit."""
import json
from pathlib import Path
import tempfile
import unittest

from scripts import audit_fes_i386_case as audit


def netlist(count=65):
    text = 'module fes_i386_cpu(clk, x);\n  input clk;\n  output x;\n'
    for i in range(count):
        text += (f'  function [0:0] _{i}_;\n'
                 '    input [0:0] a;\n    input [1:0] b;\n    input [1:0] s;\n'
                 '    casez (s) // synopsys parallel_case\n'
                 f"      2'b?1: _{i}_ = b[0];\n"
                 f"      2'b1?: _{i}_ = b[1];\n"
                 f'      default: _{i}_ = a;\n'
                 '    endcase\n  endfunction\n'
                 f'  assign x{i} = _{i}_(x, {{a, b}}, {{c{i}, d{i}}});\n')
    return text + 'endmodule\n'


class CaseAuditTest(unittest.TestCase):
    def test_exposes_each_selector_and_maps_instrumented_case_line(self):
        value, sites = audit.instrument_netlist(netlist())
        self.assertEqual(len(sites), 65)
        self.assertEqual(sites[0]['selector'], '{c0, d0}')
        self.assertIn('output [64:0] audit_bad;', value)
        self.assertIn("assign audit_bad[0] = |(__audit_sel_0 & (__audit_sel_0 - 1'b1));", value)
        self.assertIn(audit.CASE, value.splitlines()[sites[0]['line'] - 1])

    def test_rejects_incomplete_or_non_onehot_patterns(self):
        for value in (netlist().replace("2'b1?:", "2'b01:", 1),
                      netlist().replace('default:', 'otherwise:', 1),
                      netlist().replace('synopsys parallel_case', 'unknown annotation', 1)):
            with self.subTest(value=value[:20]), self.assertRaises(audit.AuditError):
                audit.instrument_netlist(value)

    def test_rejects_reserved_probe_names(self):
        for name in ('audit_bad', '__audit_sel_12'):
            with self.subTest(name=name), self.assertRaises(audit.AuditError):
                audit.instrument_netlist(netlist().replace('output x;', f'output x; wire {name};'))

    def test_negative_control_forces_the_real_and_observed_selector(self):
        value, sites = audit.instrument_netlist(netlist(), 3)
        self.assertEqual(sites[3]['selector'], "2'd3")
        self.assertIn("assign x3 = _3_(x, {a, b}, 2'd3);", value)
        self.assertIn("assign __audit_sel_3 = 2'd3;", value)
        with self.assertRaises(audit.AuditError):
            audit.instrument_netlist(netlist(), 65)

    def test_wraps_all_known_eval_calls_and_refuses_new_call_sites(self):
        src = '#include "Vfes_i386_cpu.h"\n' + 'top.eval();\n' * 3 + 'rtl.top.eval();\n' * 2
        result = audit.instrument_bench(src)
        self.assertEqual(result.count('case_audit_before()'), 5)
        self.assertIn('case_audit_after(rtl.top)', result)
        with self.assertRaises(audit.AuditError):
            audit.instrument_bench(src + 'top.eval();\n')

    def test_stops_must_match_sites_and_messages_exactly(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / 'generated.cpp'
            p.write_text('VL_WRITEF_NX("parallel_case, but multiple",0);\n'
                         'VL_STOP_MT("fes_i386_cpu.netlist.v", 12, "");\n')
            self.assertEqual(audit.patch_displays(Path(tmp), [{'line': 12}]), 1)
            self.assertNotIn('parallel_case, but', p.read_text())
            self.assertIn('VL_STOP_MT', p.read_text())
            with self.assertRaises(audit.AuditError):
                audit.patch_displays(Path(tmp), [{'line': 13}])

    def test_collect_preserves_events_and_settled_counts(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            (path / 'case-1.json').write_text(json.dumps({'evals': 17, 'sites': [
                {'index': 0, 'line': 9, 'events': 3, 'transient_evals': 2, 'settled_evals': 1}]}))
            evals, rows = audit.collect(path, [{'line': 9, 'selector': 'wire'}])
            self.assertEqual(evals, 17)
            self.assertEqual((rows[0]['events'], rows[0]['transient_evals'], rows[0]['settled_evals']),
                             (3, 2, 1))
            with self.assertRaises(audit.AuditError):
                audit.collect(path, [{'line': 10}])

    def test_corpus_rejects_missing_or_extra_receipt_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            (directory / '00.MOO.gz').write_bytes(b'one')
            (directory / 'revocation_list.txt').write_bytes(b'')
            receipt = {'repository': audit.vectors.REPOSITORY, 'revision': audit.vectors.REVISION,
                       'files': {'00.MOO.gz': audit.vectors.git_blob(b'one'),
                                 '01.MOO.gz': audit.vectors.git_blob(b'two'),
                                 'revocation_list.txt': audit.vectors.git_blob(b'')}}
            (directory / 'fes-i386-corpus.json').write_text(json.dumps(receipt))
            with self.assertRaisesRegex(audit.AuditError, 'missing'):
                audit.corpus_paths(directory)
            (directory / '01.MOO.gz').write_bytes(b'two')
            self.assertEqual(len(audit.corpus_paths(directory)), 2)
            (directory / '02.MOO.gz').write_bytes(b'extra')
            with self.assertRaisesRegex(audit.AuditError, 'extra'):
                audit.corpus_paths(directory)

    def test_test_environment_discards_inherited_mode_flags(self):
        env = audit.test_environment({'FES_I386_TASKS': '1', 'FES_I386_CASE': '9',
                                      'FES_I386_LATENCY': '0', 'PATH': '/bin'},
                                     Path('/audit/counts'), Path('/audit/table'))
        self.assertNotIn('FES_I386_TASKS', env)
        self.assertNotIn('FES_I386_CASE', env)
        self.assertEqual(env['FES_I386_LATENCY'], '1')
        self.assertEqual(env['FES_I386_PRESET'], '0')
        self.assertEqual(env['PATH'], '/bin')


if __name__ == '__main__':
    unittest.main()
