"""#1984 S2: the design-v2 identity policy and its evidence role.

The record is docs/validation/2026-11-18-1984-pc386-identity-scope in the parent
repository. Its classification.json is the generated role table these sets are
checked against, and sealed-1951-build-inputs.json is the sealed #1951 pc386
record, the design-v1 record that must stay valid unchanged.
"""
from __future__ import annotations

import copy
import json
from pathlib import Path
import sys
import tomllib
import unittest
from unittest import mock

from scripts import build_fes_pc386_quartus as quartus
from scripts import input_identity as ii
from tests.test_pc386_seal_process import SEAL_EXCLUDED

ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT.parents[1] / 'docs/validation/2026-11-18-1984-pc386-identity-scope'
# #1951's sealed BUILD_ID, the first 32 hex digits of its design identity.
SEALED_BUILD_ID = 'bb7369101d3a5a15d832f58af59c7123'
# The ROM hex digest firmware.lock pins (#1984 S1), which a v2 pc386 record carries.
ROM_HEX = '815620361ef28a2bbdd97b1964700a4678a26c0ffc9582bb95544473fe099d18'
# Added to the evidence set after the record was measured: S1's data file, which
# the record (README section 3) names as an evidence member.
ADDED_SINCE_RECORD = frozenset({'cores/fes-common/sim/i386/oracle_sources.toml'})
ORACLE_SOURCES = 'cores/fes-common/sim/i386/oracle_sources.toml'


def compiler_reads_now(path: str) -> list[str]:
    """Live rule (a) for a member the record never measured: how pc386's compilers reach it today."""
    reasons = []
    named = {*quartus.MACHINE_SOURCES, *quartus.SHELL_SOURCES, *quartus.INCLUDES, quartus.QSF, quartus.SDC,
             quartus.ROM_HEX}
    if path in named:
        reasons.append('named in the seal\'s source lists')
    name = Path(path).name
    for source in sorted({*quartus.MACHINE_SOURCES, *quartus.SHELL_SOURCES, *quartus.INCLUDES, quartus.QSF}):
        if name in (ROOT / source).read_text(errors='replace'):
            reasons.append(f'named in {source}')
    if str(ROOT / 'cores/fes-pc386/tools') not in sys.path:
        sys.path.insert(0, str(ROOT / 'cores/fes-pc386/tools'))
    import build_pc386_product as product
    qsf = product.project_qsf(ROOT, ROOT / product.OUTPUT / 'project', '0' * 32, 1)
    if name in qsf:
        reasons.append('named in the generated QSF')
    return reasons


def cleared_now(added: frozenset) -> frozenset:
    return frozenset(path for path in added if not compiler_reads_now(path))


def sealed_record() -> dict:
    return json.loads((RECORD / 'sealed-1951-build-inputs.json').read_text())


def classification() -> list[dict]:
    return json.loads((RECORD / 'classification.json').read_text())['files']


def v2_record() -> dict:
    """The sealed record's inputs as design-v2 would record them (S4 adds rom_hex)."""
    fields = {key: value for key, value in sealed_record().items() if key != 'identities'}
    firmware = json.loads(fields['parameters']['firmware'])
    fields['parameters'] = dict(fields['parameters'], firmware=json.dumps(
        dict(firmware, rom_hex=ROM_HEX), sort_keys=True, separators=(',', ':')))
    return ii.with_identities(fields, policy=ii.DESIGN_V2)


def edit(fields: dict, *paths: str, policy: str) -> dict:
    changed = copy.deepcopy({key: value for key, value in fields.items() if key != 'identities'})
    for path in paths:
        changed['source_inputs'][path] = '1' * 64
    return ii.with_identities(changed, policy=policy)


class DesignV1IsFrozenTest(unittest.TestCase):
    def test_the_sealed_1951_record_still_validates_unchanged(self):
        record = sealed_record()
        self.assertEqual(record['parameters']['identity_policy'], ii.DESIGN_V1)
        ii.validate_identities(record)
        self.assertEqual(ii.identities(record), record['identities'])
        self.assertEqual(ii.functional_key(record)[:32], SEALED_BUILD_ID)

    def test_extending_v1_instead_would_orphan_the_sealed_record(self):
        # The record's immutability case: why v2 is a new policy.
        record = sealed_record()
        with mock.patch.object(ii, 'PACKAGING_INPUTS', ii.PACKAGING_INPUTS | ii.EVIDENCE_INPUTS):
            with self.assertRaisesRegex(ValueError, 'do not match canonical inputs'):
                ii.validate_identities(record)

    def test_v1_keeps_evidence_and_firmware_files_in_the_design(self):
        for path in sorted(ii.EVIDENCE_INPUTS | ii.FIRMWARE_GENERATOR_INPUTS):
            with self.subTest(path=path):
                self.assertEqual(ii.input_role(path), 'design')
                self.assertNotEqual(ii.identities(edit(sealed_record(), path, policy=ii.DESIGN_V1))['design'],
                                    sealed_record()['identities']['design'])

    def test_a_record_cannot_be_relabelled_to_the_other_policy(self):
        for record in (sealed_record(), v2_record()):
            other = ii.DESIGN_V2 if record['parameters']['identity_policy'] == ii.DESIGN_V1 else ii.DESIGN_V1
            relabelled = copy.deepcopy(record)
            relabelled['parameters']['identity_policy'] = other
            with self.subTest(policy=other), self.assertRaises(ValueError):
                ii.validate_identities(relabelled)

    def test_an_unknown_policy_is_refused(self):
        record = sealed_record()
        record['parameters']['identity_policy'] = 'design-v3'
        with self.assertRaises(ValueError):
            ii.validate_identities(record)
        with self.assertRaises(ValueError):
            ii.with_identities(v2_record(), policy='design-v3')
        with self.assertRaises(ValueError):
            ii.input_role('scripts/fes_i386_oracle.py', 'design-v3')


class ClassificationTest(unittest.TestCase):
    def test_the_sets_are_the_generated_role_table(self):
        rows = classification()
        self.assertEqual(ii.EVIDENCE_INPUTS - ADDED_SINCE_RECORD,
                         {row['path'] for row in rows if row['proposed_role'] == 'evidence'})
        self.assertEqual(ii.FIRMWARE_GENERATOR_INPUTS,
                         {row['path'] for row in rows if row['proposed_role'] == 'firmware-generator'})
        self.assertFalse(ADDED_SINCE_RECORD & {row['path'] for row in rows})

    def test_no_member_is_read_by_a_compiler(self):
        self.assertEqual(cleared_now(ADDED_SINCE_RECORD), ADDED_SINCE_RECORD)
        ii.refuse_compiler_read(classification(), cleared=cleared_now(ADDED_SINCE_RECORD))
        reads = set(json.loads((RECORD / 'yosys-reads.json').read_text())['opened_under_root'])
        self.assertFalse((ii.EVIDENCE_INPUTS | ii.FIRMWARE_GENERATOR_INPUTS) & reads)

    def test_a_compiler_read_file_given_the_evidence_role_is_refused(self):
        rows = classification()
        compiler_read = [row['path'] for row in rows
                         if row['yosys_read'] or row['quartus_read'] or row['inlined_in_qsf']]
        self.assertIn('cores/fes-common/rtl/i386/fes_i386_cpu.sv', compiler_read)
        self.assertIn('cores/fes-pc386/constraints-oss.qsf', compiler_read)
        for path in ('cores/fes-common/rtl/i386/fes_i386_cpu.sv', 'cores/fes-pc386/constraints-oss.qsf'):
            for evidence, firmware in ((ii.EVIDENCE_INPUTS | {path}, ii.FIRMWARE_GENERATOR_INPUTS),
                                       (ii.EVIDENCE_INPUTS, ii.FIRMWARE_GENERATOR_INPUTS | {path})):
                with self.subTest(path=path), self.assertRaisesRegex(ValueError, path + ': read by a compiler'):
                    ii.refuse_compiler_read(rows, evidence, firmware, cleared=ADDED_SINCE_RECORD)

    def test_a_member_the_record_never_measured_is_refused_unless_cleared_live(self):
        with self.assertRaisesRegex(ValueError, 'oracle_sources.toml: not in the classification'):
            ii.refuse_compiler_read(classification())
        # An RTL file the record predates, added to both the set and the added list: the live check
        # finds the compiler reading it, so it is never cleared.
        # The record is made to predate it by dropping its row.
        rtl = 'cores/fes-common/rtl/i386/fes_x87_core.sv'
        rows = [row for row in classification() if row['path'] != rtl]
        added = ADDED_SINCE_RECORD | {rtl}
        self.assertTrue(compiler_reads_now(rtl))
        self.assertEqual(cleared_now(added), ADDED_SINCE_RECORD)
        with self.assertRaisesRegex(ValueError, 'fes_x87_core.sv: not in the classification'):
            ii.refuse_compiler_read(rows, ii.EVIDENCE_INPUTS | added, cleared=cleared_now(added))

    def test_an_evidence_file_that_starts_being_read_is_refused(self):
        rows = copy.deepcopy(classification())
        row = next(row for row in rows if row['path'] == 'cores/fes-common/sim/i386/smc_stale.h')
        row['yosys_read'] = True
        with self.assertRaisesRegex(ValueError, 'smc_stale.h: read by a compiler'):
            ii.refuse_compiler_read(rows, cleared=ADDED_SINCE_RECORD)

    def test_the_seal_program_stays_design_though_the_suite_lists_it(self):
        # Case D2: yosys_netlist.py is a suite source and the seal's Yosys program.
        rows = classification()
        self.assertTrue(next(row for row in rows if row['path'] == 'scripts/yosys_netlist.py')['generator_code'])
        with self.assertRaisesRegex(ValueError, 'yosys_netlist.py: generates compiler input'):
            ii.refuse_compiler_read(rows, ii.EVIDENCE_INPUTS | {'scripts/yosys_netlist.py'}, cleared=ADDED_SINCE_RECORD)
        self.assertEqual(ii.input_role('scripts/yosys_netlist.py', ii.DESIGN_V2), 'design')

    def test_python_members_never_run_in_the_seal_process(self):
        # Rule (b): S1's seal-process tests exclude every Python member.
        python = {path for path in ii.EVIDENCE_INPUTS | ii.FIRMWARE_GENERATOR_INPUTS if path.endswith('.py')}
        self.assertEqual(python - set(SEAL_EXCLUDED), set())

    def test_the_oracle_and_suite_lists_are_evidence(self):
        # The growth rule: the PR that adds a file to these lists adds it to EVIDENCE_INPUTS.
        data = tomllib.loads((ROOT / ORACLE_SOURCES).read_text())
        listed = {ORACLE_SOURCES, 'scripts/fes_i386_oracle.py', data['bochs_patch'],
                  *(f'cores/fes-common/sim/i386/{name}' for name in data['oracle_sources']),
                  *(name for name in data['suite_sources'] if name != 'scripts/yosys_netlist.py')}
        self.assertEqual(listed - ii.EVIDENCE_INPUTS, set())
        for path in ii.EVIDENCE_INPUTS | ii.FIRMWARE_GENERATOR_INPUTS:
            self.assertTrue((ROOT / path).is_file(), path)


class CounterexampleMatrixTest(unittest.TestCase):
    """The record's cases over synthetic records built from the sealed #1951 inputs."""

    def assert_case(self, record, policy, paths, *, design_moves, evidence_moves=False):
        changed = edit(record, *paths, policy=policy)
        self.assertEqual(record['identities']['design'] != changed['identities']['design'], design_moves)
        self.assertNotEqual(record['identities']['record'], changed['identities']['record'])
        self.assertEqual(record['identities']['toolchain'], changed['identities']['toolchain'])
        if policy == ii.DESIGN_V2:
            self.assertEqual(record['identities']['evidence'] != changed['identities']['evidence'], evidence_moves)
        ii.validate_identities(changed)

    def test_cases(self):
        cases = (
            # case, edited paths, moves under v1, moves under v2, evidence digest moves
            ('E1', ('cores/fes-common/sim/i386/lockstep_tb.cpp',), True, False, True),
            ('E2', ('cores/fes-common/sim/i386/bochs_oracle.cpp',), True, False, True),
            ('E3', ('scripts/test_fes_i386_lockstep.py',), True, False, True),
            ('D1', ('cores/fes-common/rtl/i386/fes_i386_cpu.sv',), True, True, False),
            ('D2', ('scripts/yosys_netlist.py',), True, True, False),
            ('D3', ('cores/fes-pc386/constraints-oss.qsf',), True, True, False),
            ('P1', ('scripts/input_identity.py',), False, False, False),
            ('R1', ('scripts/build_pc386_firmware.py',), True, False, False),
            ('R2', ('scripts/build_pc386_firmware.py', 'cores/fes-pc386/firmware/firmware.lock'), True, False, False),
        )
        for case, paths, v1_moves, v2_moves, evidence_moves in cases:
            for path in paths:
                self.assertIn(path, sealed_record()['source_inputs'], path)
            with self.subTest(case=case, policy=ii.DESIGN_V1):
                self.assert_case(sealed_record(), ii.DESIGN_V1, paths, design_moves=v1_moves)
            with self.subTest(case=case, policy=ii.DESIGN_V2):
                self.assert_case(v2_record(), ii.DESIGN_V2, paths, design_moves=v2_moves,
                                 evidence_moves=evidence_moves)

    def test_r3u_a_new_rom_pin_moves_the_v2_design(self):
        record = v2_record()
        changed = {key: value for key, value in copy.deepcopy(record).items() if key != 'identities'}
        firmware = json.loads(changed['parameters']['firmware'])
        changed['parameters']['firmware'] = json.dumps(dict(firmware, rom_hex='2' * 64),
                                                       sort_keys=True, separators=(',', ':'))
        changed = ii.with_identities(changed, policy=ii.DESIGN_V2)
        self.assertNotEqual(record['identities']['design'], changed['identities']['design'])
        self.assertEqual(record['identities']['evidence'], changed['identities']['evidence'])

    def test_a_v2_record_without_a_pinned_rom_is_refused(self):
        fields = {key: value for key, value in sealed_record().items() if key != 'identities'}
        with self.assertRaisesRegex(ValueError, 'rom_hex'):
            ii.with_identities(fields, policy=ii.DESIGN_V2)
        for firmware in ('not json', '[]', json.dumps({'bios': '0' * 64, 'rom_hex': 'nothex'})):
            broken = copy.deepcopy(fields)
            broken['parameters']['firmware'] = firmware
            with self.subTest(firmware=firmware), self.assertRaisesRegex(ValueError, 'rom_hex'):
                ii.with_identities(broken, policy=ii.DESIGN_V2)
        # A record with no firmware-generator input needs no firmware parameter.
        for path in ii.FIRMWARE_GENERATOR_INPUTS:
            del fields['source_inputs'][path]
        ii.validate_identities(ii.with_identities(fields, policy=ii.DESIGN_V2))

    def test_v2_has_its_own_domains_and_an_evidence_digest(self):
        record = v2_record()
        self.assertEqual(set(record['identities']), {'design', 'toolchain', 'evidence', 'record'})
        self.assertEqual(set(sealed_record()['identities']), {'design', 'toolchain', 'record'})
        self.assertNotEqual(record['identities']['design'][:32], SEALED_BUILD_ID)
        evidence = {path: digest for path, digest in record['source_inputs'].items() if path in ii.EVIDENCE_INPUTS}
        self.assertEqual(record['identities']['evidence'], ii.digest('fes-evidence-v2', evidence))
        self.assertEqual(ii.functional_key(record), record['identities']['design'])
        design, _ = ii.projections(record)
        self.assertFalse((ii.EVIDENCE_INPUTS | ii.FIRMWARE_GENERATOR_INPUTS) & set(design['source_inputs']))
        self.assertIn('scripts/yosys_netlist.py', design['source_inputs'])


if __name__ == '__main__':
    unittest.main()
