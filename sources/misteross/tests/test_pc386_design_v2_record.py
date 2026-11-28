"""#1984 S4: the pc386 producer writes design-v2 records.

Each test edits one file of a synthetic copy of the producer's closure and
records it with the real create_build_record, then checks which identities
moved. The cases are the D14 record's counterexamples
(docs/validation/2026-11-18-1984-pc386-identity-scope, section 5); every
assertion message states the claim it checks.
"""
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools'))
import build_pc386_product as seal
from scripts import input_identity, source_closure
from scripts.export_core_package import build_identity

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'https://github.com/DeanoC/fes.git'
TOOLS = {'yosys': 'synthetic-yosys', 'quartus': 'synthetic-quartus'}
EXECUTION = {'toolchain': 'quartus', 'gpu_device': 'lease'}
SEALED_1951 = (ROOT.parents[1] / 'docs/validation/2026-11-18-1984-pc386-identity-scope'
               / 'sealed-1951-build-inputs.json')


def append(path, text):
    with path.open('a', encoding='utf-8') as stream:
        stream.write(text)


def edit_lock(root, change):
    path = root / seal.FIRMWARE_LOCK
    lock = json.loads(path.read_text(encoding='utf-8'))
    change(lock)
    path.write_text(json.dumps(lock, indent=2) + '\n', encoding='utf-8')


class DesignV2RecordTests(unittest.TestCase):
    """The producer's own record of a synthetic checkout, before and after one edit."""

    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.repo = Path(cls.temp.name) / 'repo'
        cls.source = cls.repo / 'sources/misteross'
        cls.source.mkdir(parents=True)
        paths = set(seal.PINNED_INPUTS) | source_closure.imports("pc386_quartus", ROOT)
        paths |= {'scripts/source_closure.py', 'scripts/bootstrap.sh', *source_closure.TOOLCHAIN_RECIPE_FILES}
        for path in sorted(paths):
            target = cls.source / path
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / path, target)
        (cls.repo / 'config').mkdir()
        shutil.copy2(ROOT.parents[1] / 'config/core-recipes.toml', cls.repo / 'config/core-recipes.toml')
        git = ['git', '-C', str(cls.repo)]
        subprocess.run(git + ['init', '-q'], check=True)
        (cls.repo / '.gitignore').write_text('__pycache__/\nbuild/\n')
        subprocess.run(git + ['add', '.'], check=True)
        subprocess.run(git + ['-c', 'user.name=fixture', '-c', 'user.email=fixture@example.invalid',
                              'commit', '-qm', 'synthetic source snapshot'], check=True)
        cls.base = cls.record(cls.source)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    @staticmethod
    def record(root):
        with mock.patch.object(seal, 'ROOT', root):
            return json.loads(seal.create_build_record(root, REPOSITORY, 'a' * 40, TOOLS, execution=EXECUTION))

    def edited(self, change):
        """The record of a fresh copy of the synthetic checkout after change(root)."""
        with tempfile.TemporaryDirectory() as temporary:
            repo = Path(temporary) / 'repo'
            shutil.copytree(self.repo, repo, symlinks=True)
            root = repo / 'sources/misteross'
            change(root)
            subprocess.run(['git', '-C', str(repo), 'add', '.'], check=True)
            return self.record(root)

    def test_cache_coordinator_edits_do_not_move_pc386_producer_identity(self):
        self.assertNotIn('scripts/bootstrap.sh', self.base['source_inputs'])
        coordinator = self.edited(lambda root: append(root / 'scripts/bootstrap.sh', '\n# cache maintenance\n'))
        self.assertEqual(self.base['identities'], coordinator['identities'])
        for path in ('scripts/bootstrap-build.sh', 'scripts/bootstrap-build.py', 'scripts/lockfile.py'):
            with self.subTest(path=path):
                changed = self.edited(lambda root: append(root / path, '\n# build input\n'))
                self.assertEqual(self.base['identities']['design'], changed['identities']['design'])
                self.assertNotEqual(self.base['identities']['toolchain'], changed['identities']['toolchain'])

    def test_the_producer_writes_a_design_v2_record_that_binds_the_rom_hex(self):
        parameters = self.base['parameters']
        self.assertEqual(parameters['identity_policy'], input_identity.DESIGN_V2,
                         'the pc386 producer writes design-v2 records')
        self.assertEqual(json.loads(parameters['firmware']),
                         {**seal.firmware_digests(self.source), 'rom_hex': seal.pinned_rom_digest(self.source)},
                         "the record's firmware parameter carries the BIOS, VGA BIOS and pinned ROM hex digests")
        self.assertEqual(set(self.base['identities']), {'design', 'toolchain', 'evidence', 'record'},
                         'a design-v2 record carries its own evidence identity')
        self.assertEqual(input_identity.functional_key(self.base), self.base['identities']['design'],
                         'the cache key of a design-v2 record is its design identity')
        roles = {path: input_identity.input_role(path, input_identity.DESIGN_V2) for path in self.base['source_inputs']}
        self.assertEqual({path for path, role in roles.items() if role == 'firmware-generator'},
                         set(input_identity.FIRMWARE_GENERATOR_INPUTS),
                         'the firmware recipe and lock are in the record as firmware generators')
        self.assertTrue({path for path, role in roles.items() if role == 'evidence'},
                        'the oracle and suite are in the record as evidence')

    def test_evidence_edits_keep_the_design_id_and_move_the_evidence_id(self):
        cases = {
            'E1 lockstep_tb.cpp (#1930)': 'cores/fes-common/sim/i386/lockstep_tb.cpp',
            'E2 bochs_oracle.cpp (#1976)': 'cores/fes-common/sim/i386/bochs_oracle.cpp',
            'E3 test_fes_i386_lockstep.py': 'scripts/test_fes_i386_lockstep.py',
            'E4 pc386_run_lockstep.py': 'cores/fes-pc386/tools/pc386_run_lockstep.py',
        }
        for case, path in cases.items():
            with self.subTest(case):
                after = self.edited(lambda root: append(root / path, '\n// evidence-only edit\n'
                                                        if not path.endswith('.py') else '\n# evidence-only edit\n'))
                self.assertEqual(after['identities']['design'], self.base['identities']['design'],
                                 f'{case}: an evidence-only edit must not move the design ID')
                self.assertNotEqual(after['identities']['evidence'], self.base['identities']['evidence'],
                                    f'{case}: an evidence edit must move the evidence ID')
                self.assertNotEqual(after['identities']['record'], self.base['identities']['record'],
                                    f'{case}: the record identity still binds every evidence byte')

    def test_design_edits_move_the_design_id(self):
        cases = {
            'D1 CPU RTL': (seal.yosys_netlist.CPU_SOURCES[-1], '\n// design edit\n'),
            'D2 yosys_netlist.py (suite source that is the seal program)': ('scripts/yosys_netlist.py', '\n# design edit\n'),
            'D3 pin constraints QSF': (seal.QSF, '\n# design edit\n'),
        }
        for case, (path, text) in cases.items():
            with self.subTest(case):
                after = self.edited(lambda root: append(root / path, text))
                self.assertNotEqual(after['identities']['design'], self.base['identities']['design'],
                                    f'{case}: a design input edit must move the design ID')

    def test_firmware_generator_edits_that_keep_the_outputs_keep_the_design_id(self):
        def mirror(lock):
            lock['dev86']['mirror']['url'] += '?moved'
        cases = {
            'R1 recipe failure-report path (#1983)': lambda root: append(
                root / 'scripts/build_pc386_firmware.py', '\n# diagnostics-only edit\n'),
            'R2 dev86 mirror in the lock (#1978)': lambda root: edit_lock(root, mirror),
        }
        for case, change in cases.items():
            with self.subTest(case):
                after = self.edited(change)
                self.assertEqual(after['identities']['design'], self.base['identities']['design'],
                                 f'{case}: a firmware-generator edit with the same outputs must not move the design ID')
                self.assertNotEqual(after['identities']['record'], self.base['identities']['record'],
                                    f'{case}: the record identity still binds the recipe and lock bytes')

    def test_a_new_firmware_output_pin_moves_the_design_id(self):
        def rom(lock):
            lock['rom_hex']['sha256'] = '0' * 64
        def bios(lock):
            lock['images']['bios']['sha256'] = '1' * 64
        for case, change in {'R3u a re-pinned ROM hex': rom, 'a re-pinned BIOS image': bios}.items():
            with self.subTest(case):
                after = self.edited(lambda root: edit_lock(root, change))
                self.assertNotEqual(after['identities']['design'], self.base['identities']['design'],
                                    f'{case}: a changed firmware output must move the design ID')

    def test_a_design_v2_record_without_the_rom_hex_digest_is_refused(self):
        fields = {key: value for key, value in self.base.items() if key != 'identities'}
        firmware = json.loads(fields['parameters']['firmware'])
        firmware.pop('rom_hex')
        fields['parameters'] = dict(fields['parameters'],
                                    firmware=json.dumps(firmware, sort_keys=True, separators=(',', ':')))
        with self.assertRaisesRegex(ValueError, 'must pin the firmware output digests',
                                    msg='a design-v2 record that drops the ROM hex digest must not get an identity'):
            input_identity.with_identities(fields, policy=input_identity.DESIGN_V2)

    def test_the_migration_moves_the_design_id_and_build_id(self):
        fields = {key: value for key, value in self.base.items() if key != 'identities'}
        parameters = {key: value for key, value in fields['parameters'].items() if key != 'identity_policy'}
        firmware = json.loads(parameters['firmware'])
        firmware.pop('rom_hex')
        parameters['firmware'] = json.dumps(firmware, sort_keys=True, separators=(',', ':'))
        v1 = input_identity.with_identities(dict(fields, parameters=parameters), policy=input_identity.DESIGN_V1)
        self.assertNotEqual(input_identity.functional_key(v1), input_identity.functional_key(self.base),
                            'a design-v1 and a design-v2 record of one tree never share a cache key')
        self.assertNotEqual(build_identity(seal.encode_build_record(v1)),
                            build_identity(seal.encode_build_record(self.base)),
                            'the one-time migration moves pc386 BUILD_ID')

    def test_the_sealed_1951_design_v1_record_still_validates(self):
        record = json.loads(SEALED_1951.read_bytes())
        self.assertEqual(record['parameters']['identity_policy'], input_identity.DESIGN_V1,
                         "#1951's sealed record is design-v1")
        input_identity.validate_identities(record)
        self.assertEqual(build_identity(SEALED_1951.read_bytes()), 'bb7369101d3a5a15d832f58af59c7123',
                         "#1951's sealed BUILD_ID is unchanged by the migration")


if __name__ == '__main__':
    unittest.main()
