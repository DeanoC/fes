"""fes.pcxt-dos on design-v2: its Verilator lint configuration and testbenches are evidence.

The record is docs/validation/2026-11-22-pcxt-sim-evidence in the parent
repository. Its classification.json measured which files under
cores/fes-pcxt/sim the seal reads; PCXT_EVIDENCE_INPUTS is the set it found
read by nothing the seal runs.
"""
from __future__ import annotations

import copy
import json
from pathlib import Path
import subprocess
import unittest

from scripts import build_fes_pcxt_quartus as producer
from scripts import input_identity as ii
from scripts import quartus_fes_pcxt as board

ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT.parents[1] / 'docs/validation/2026-11-22-pcxt-sim-evidence'
SIM = 'cores/fes-pcxt/sim/'
# Added to the evidence set after the record was measured: Hercules/dynamic/DOS video testbenches.
ADDED_SINCE_RECORD = frozenset({
    'cores/fes-pcxt/sim/hercules_tb.cpp',
    'cores/fes-pcxt/sim/hercules_top.sv',
    'cores/fes-pcxt/sim/machine_settings_tb.cpp',
    'cores/fes-pcxt/sim/machine_settings_top.sv',
    'cores/fes-pcxt/sim/video_dos_tb.cpp',
    'cores/fes-pcxt/sim/video_dos_top.sv',
    'cores/fes-pcxt/sim/video_dynamic_tb.cpp',
    'cores/fes-pcxt/sim/video_dynamic_top.sv',
})
# What only a Verilator simulation consumes: lint configuration, testbench C++
# and testbench top modules. The .S programs and the BIOS and FreeDOS locks under
# sim/ are not in this class and stay design.
SIMULATION_ONLY = ('.vlt', '.cpp', '.h', '.hpp', '.sv', '.v')


def cleared_now(added: frozenset) -> frozenset:
    return frozenset(path for path in added if not compiler_reads_now(path))


def classification() -> dict:
    return json.loads((RECORD / 'classification.json').read_text())


def tracked_simulation_files() -> set[str]:
    listed = subprocess.check_output(['git', 'ls-files', '--', SIM], cwd=ROOT, text=True).splitlines()
    return {path for path in listed if path.endswith(SIMULATION_ONLY)}


def compiler_reads_now(path: str) -> list[str]:
    """How the DOS package's compilers and generators could reach a file today."""
    reasons = []
    if path in board.RTL or path in board.SYSTEM_SOURCES:
        reasons.append('in the seal\'s RTL list')
    name = Path(path).name
    program = board.canonical_elaboration(dos_profile=True)
    if name in program:
        reasons.append('named in the elaboration program')
    if name in board.canonical_project_qsf(ROOT, 1, dos_profile=True):
        reasons.append('named in the generated QSF')
    return reasons


def v2_record() -> dict:
    record = producer.create_build_record(
        producer.ROOT, 'https://github.com/DeanoC/fes.git', 'a' * 40,
        {'quartus': 'pinned quartus', 'yosys': 'pinned yosys'}, execution={'toolchain': 'quartus', 'files': {}})
    return json.loads(record)


def edit(fields: dict, *paths: str, policy=ii.DESIGN_V2) -> dict:
    changed = copy.deepcopy({key: value for key, value in fields.items() if key != 'identities'})
    for path in paths:
        changed['source_inputs'][path] = '1' * 64
    return ii.with_identities(changed, policy=policy)


class ClassificationTest(unittest.TestCase):
    def test_the_set_is_the_measured_role_table(self):
        record = classification()
        self.assertEqual(ii.PCXT_EVIDENCE_INPUTS - ADDED_SINCE_RECORD, set(record['evidence']))
        self.assertFalse(ADDED_SINCE_RECORD & {row['path'] for row in record['candidates']})
        self.assertEqual(record['dropped'], [])
        self.assertEqual(record['yosys_exit'], 0)
        for proof in record['proofs'].values():
            self.assertEqual(proof['exit'], 0)

    def test_no_member_is_read_by_a_compiler(self):
        self.assertEqual(cleared_now(ADDED_SINCE_RECORD), ADDED_SINCE_RECORD)
        ii.refuse_compiler_read(
            classification()['candidates'], ii.PCXT_EVIDENCE_INPUTS, frozenset(),
            cleared=cleared_now(ADDED_SINCE_RECORD))
        for path in sorted(ii.PCXT_EVIDENCE_INPUTS):
            with self.subTest(path=path):
                self.assertEqual(compiler_reads_now(path), [])

    def test_a_seal_input_given_the_evidence_role_is_refused(self):
        rows = copy.deepcopy(classification()['candidates'])
        row = next(row for row in rows if row['path'] == SIM + 'cpu.vlt')
        row['yosys_read'] = True
        with self.assertRaisesRegex(ValueError, 'cpu.vlt: read by a compiler'):
            ii.refuse_compiler_read(rows, ii.PCXT_EVIDENCE_INPUTS, frozenset())
        self.assertTrue(compiler_reads_now('cores/fes-pcxt/rtl/pcxt_system.sv'))

    def test_every_simulation_only_file_is_evidence(self):
        # The growth rule: a PR that adds a lint configuration, testbench or
        # testbench top under cores/fes-pcxt/sim adds it to PCXT_EVIDENCE_INPUTS,
        # and test_no_member_is_read_by_a_compiler checks it live.
        self.assertEqual(tracked_simulation_files() - ii.PCXT_EVIDENCE_INPUTS, set())
        for path in ii.PCXT_EVIDENCE_INPUTS:
            self.assertTrue((ROOT / path).is_file(), path)

    def test_the_sim_driver_and_the_locks_stay_design(self):
        # The seal runs sim_fes_pcxt.py (vendor checks, ROM generation, the CGA
        # font) and reads the BIOS and FreeDOS locks.
        for path in ('scripts/sim_fes_pcxt.py', SIM + 'bios.lock', SIM + 'freedos.lock', SIM + 'diagnostic.S'):
            self.assertEqual(ii.input_role(path, ii.DESIGN_V2), 'design')


class CounterexampleTest(unittest.TestCase):
    def test_the_producer_writes_design_v2(self):
        record = v2_record()
        self.assertEqual(record['parameters']['identity_policy'], ii.DESIGN_V2)
        ii.validate_identities(record)
        self.assertTrue(ii.PCXT_EVIDENCE_INPUTS <= set(record['source_inputs']))

    def test_a_lint_configuration_edit_leaves_build_id_and_moves_evidence(self):
        record = v2_record()
        for path in (SIM + 'cpu.vlt', SIM + 'chipset.vlt', SIM + 'system_tb.cpp', SIM + 'system_top.sv'):
            with self.subTest(path=path):
                changed = edit(record, path)
                self.assertEqual(changed['identities']['design'], record['identities']['design'])
                self.assertEqual(ii.functional_key(changed)[:32], ii.functional_key(record)[:32])
                self.assertNotEqual(changed['identities']['evidence'], record['identities']['evidence'])

    def test_an_rtl_or_driver_edit_moves_build_id(self):
        record = v2_record()
        for path in ('cores/fes-pcxt/rtl/pcxt_system.sv', 'scripts/sim_fes_pcxt.py', SIM + 'bios.lock'):
            with self.subTest(path=path):
                changed = edit(record, path)
                self.assertNotEqual(changed['identities']['design'], record['identities']['design'])
                self.assertNotEqual(ii.functional_key(changed)[:32], ii.functional_key(record)[:32])

    def test_design_v1_keeps_the_lint_configuration_in_the_design(self):
        record = v2_record()
        v1 = edit(record, policy=ii.DESIGN_V1)
        changed = edit(record, SIM + 'cpu.vlt', policy=ii.DESIGN_V1)
        self.assertNotEqual(changed['identities']['design'], v1['identities']['design'])


if __name__ == '__main__':
    unittest.main()
