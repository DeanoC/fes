import copy
import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from scripts import export_core_package as exporter
from scripts import build_fes_pcxt_quartus as pcxt_quartus
from boards.de25nano.package import export_core_package as de25
from scripts.input_identity import (
    PACKAGING_INPUTS, TOOLCHAIN_INPUTS, TOOLCHAIN_PARAMETERS, identities,
    functional_key, with_identities,
    manifest_toolchain,
)
from tests.test_identity_report import fields


class InputIdentityTests(unittest.TestCase):
    def record(self):
        return with_identities(fields())

    def test_packaging_mutations_only_move_full_record_identity(self):
        for path in PACKAGING_INPUTS:
            with self.subTest(path=path):
                before = self.record()
                before['source_inputs'][path] = '0' * 64
                before = with_identities(before)
                after = copy.deepcopy(before)
                after['source_inputs'][path] = '1' * 64
                after = with_identities(after)
                self.assertEqual(before['identities']['design'], after['identities']['design'])
                self.assertEqual(before['identities']['toolchain'], after['identities']['toolchain'])
                self.assertNotEqual(before['identities']['record'], after['identities']['record'])

    def test_toolchain_mutations_are_advisory_for_design_and_move_execution(self):
        for path in (*TOOLCHAIN_INPUTS, 'cores/toolchain.lock', 'boards/de25nano/quartus.lock'):
            with self.subTest(path=path):
                before = self.record()
                after = copy.deepcopy(before)
                after['source_inputs'][path] = '1' * 64
                after = with_identities(after)
                self.assertEqual(before['identities']['design'], after['identities']['design'])
                self.assertNotEqual(before['identities']['toolchain'], after['identities']['toolchain'])
        before = self.record()
        for key in TOOLCHAIN_PARAMETERS - {'selection_only'}:
            after = copy.deepcopy(before)
            after['parameters'][key] = 'changed'
            after = with_identities(after)
            self.assertEqual(before['identities']['design'], after['identities']['design'], key)
            self.assertNotEqual(before['identities']['toolchain'], after['identities']['toolchain'], key)

    def test_cache_coordinator_edit_is_outside_producer_identity(self):
        from scripts.source_closure import load_manifest
        actual_root = Path(__file__).resolve().parents[1]
        recipe_files = ('scripts/bootstrap-build.py', 'scripts/bootstrap-build.sh',
                        'scripts/lockfile.py', 'scripts/toolchain_cache.py')
        # Use the actual registered producer closure, not a hand-created list.
        roots = load_manifest()['build_fes_pong']
        self.assertTrue(all(path in roots for path in recipe_files))
        self.assertNotIn('scripts/bootstrap.sh', roots)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'scripts').mkdir()
            for name in (*recipe_files, 'scripts/bootstrap.sh'):
                shutil.copy2(actual_root / name, root / name)
            subprocess.run(['git', '-C', str(root), 'init', '-q'], check=True)
            subprocess.run(['git', '-C', str(root), 'add', 'scripts'], check=True)
            def record():
                result = fields()
                result['source_inputs'] = exporter.source_input_closure(root, list(recipe_files))
                return with_identities(result)
            before = record()
            coordinator = root / 'scripts/bootstrap.sh'
            coordinator.write_text(coordinator.read_text().replace(
                '==> leaving %s build leftovers', '==> deferring %s build leftovers'))
            self.assertEqual(before['identities'], record()['identities'])
            for name in ('scripts/bootstrap-build.sh', 'scripts/bootstrap-build.py', 'scripts/lockfile.py'):
                path = root / name
                original = path.read_text()
                path.write_text(original + '\n# changed build input\n')
                after = record()
                self.assertEqual(before['identities']['design'], after['identities']['design'])
                self.assertNotEqual(before['identities']['toolchain'], after['identities']['toolchain'], name)
                path.write_text(original)

    def test_broad_and_historical_closures_keep_coordinator_toolchain_role(self):
        # Existing v1/v2 records must retain the role of their recorded path.
        # Broad fallback still intentionally includes it, so its toolchain
        # identity moves on a coordinator edit until that closure is audited.
        for policy in ('design-v1', 'design-v2'):
            before = fields()
            before['source_inputs']['scripts/bootstrap.sh'] = '0' * 64
            if policy == 'design-v2':
                before['source_inputs'] = {k: v for k, v in before['source_inputs'].items()
                                           if k not in ('scripts/build_pc386_firmware.py',
                                                        'cores/fes-pc386/firmware/firmware.lock')}
            before = with_identities(before, policy=policy)
            after = copy.deepcopy(before)
            after['source_inputs']['scripts/bootstrap.sh'] = '1' * 64
            after = with_identities(after, policy=policy)
            self.assertEqual(before['identities']['design'], after['identities']['design'])
            self.assertNotEqual(before['identities']['toolchain'], after['identities']['toolchain'])

    def test_pc386_evidence_and_capture_roles_do_not_hide_design_inputs(self):
        before = self.record()
        for path, toolchain_changes in (("cores/fes-pc386/tools/pc386_seal_evidence.py", False),
                                        ("cores/fes-pc386/tools/pc386_execution.py", True),
                                        ("cores/fes-pc386/tools/pc386_owned_job.py", True)):
            current = copy.deepcopy(before)
            current['source_inputs'][path] = '0' * 64
            current = with_identities(current)
            changed = copy.deepcopy(current)
            changed['source_inputs'][path] = '1' * 64
            changed = with_identities(changed)
            self.assertEqual(current['identities']['design'], changed['identities']['design'])
            self.assertEqual(current['identities']['toolchain'] != changed['identities']['toolchain'],
                             toolchain_changes)
            self.assertNotEqual(current['identities']['record'], changed['identities']['record'])
        changed = copy.deepcopy(before)
        changed['source_inputs']['scripts/pc386_quartus.py'] = '1' * 64
        self.assertNotEqual(identities(changed)['design'], before['identities']['design'])

    def test_quartus_recipe_version_drift_is_toolchain_only(self):
        recipe = {'version': 'Quartus Lite 17.0', 'seeds': [1, 2, 3], 'threads': 2}
        before = self.record()
        before['parameters'].update(pcxt_quartus.quartus_recipe_parameters(recipe))
        before = with_identities(before)
        changed = dict(recipe, version='Quartus Lite 17.1')
        after = copy.deepcopy(before)
        after['parameters'].update(pcxt_quartus.quartus_recipe_parameters(changed))
        after = with_identities(after)
        self.assertEqual(before['identities']['design'], after['identities']['design'])
        self.assertNotEqual(before['identities']['toolchain'], after['identities']['toolchain'])

    def test_quartus_first_seed_drift_moves_design_identity(self):
        recipe = {'version': 'Quartus Lite 17.0', 'seeds': [1, 2, 3], 'threads': 2}
        before = self.record()
        before['parameters'].update(pcxt_quartus.quartus_recipe_parameters(recipe))
        before = with_identities(before)
        changed = dict(recipe, seeds=[4, 2, 3])
        after = copy.deepcopy(before)
        after['parameters'].update(pcxt_quartus.quartus_recipe_parameters(changed))
        after = with_identities(after)
        self.assertNotEqual(before['identities']['design'], after['identities']['design'])
        self.assertEqual(before['identities']['toolchain'], after['identities']['toolchain'])

    def test_quartus_thread_count_is_toolchain_metadata(self):
        recipe = {'version': 'Quartus Lite 17.0', 'seeds': [1, 2, 3], 'threads': 2}
        before = self.record()
        before['parameters'].update(pcxt_quartus.quartus_recipe_parameters(recipe))
        before = with_identities(before)
        changed = dict(recipe, threads=4)
        after = copy.deepcopy(before)
        after['parameters'].update(pcxt_quartus.quartus_recipe_parameters(changed))
        after = with_identities(after)
        self.assertEqual(before['identities']['design'], after['identities']['design'])
        self.assertNotEqual(before['identities']['toolchain'], after['identities']['toolchain'])

    def test_abi_rtl_constraints_recipe_unknown_helpers_and_parameters_invalidate(self):
        before = self.record()
        for path in ('cores/core.sv', 'cores/clocks.sdc', 'cores/constraints.qsf',
                     'scripts/build.py', 'scripts/unknown.py'):
            after = copy.deepcopy(before)
            after['source_inputs'][path] = '0' * 64
            self.assertNotEqual(identities(after)['design'], before['identities']['design'], path)
        for key in ('seed', 'pnr_design_sha256', 'unknown'):
            after = copy.deepcopy(before)
            after['parameters'][key] = 'changed'
            self.assertNotEqual(identities(after)['design'], before['identities']['design'], key)
        for key in ('abi_definition_sha256', 'recipe_sha256'):
            after = copy.deepcopy(before)
            after[key] = '0' * 64
            self.assertNotEqual(identities(after)['design'], before['identities']['design'], key)

    def test_both_boards_use_the_same_validated_identity_and_reject_tampering(self):
        before = self.record()
        encoded = exporter.encode_build_record(before)
        self.assertEqual(exporter.build_identity(encoded), before['identities']['design'][:32])
        self.assertEqual(de25.build_identity(encoded), exporter.build_identity(encoded))
        self.assertEqual(functional_key(before), before['identities']['design'])
        for reader in (exporter, de25):
            for key in ('design', 'toolchain', 'record'):
                damaged = copy.deepcopy(before)
                damaged['identities'][key] = '0' * 64
                with self.assertRaisesRegex(reader.PackageExportError, 'identities'):
                    reader._decode_build_record(json.dumps(damaged).encode())
            damaged = copy.deepcopy(before)
            damaged['parameters']['identity_policy'] = 'unknown'
            with self.assertRaises(reader.PackageExportError):
                reader.encode_build_record(damaged)

    def test_source_selection_requests_cannot_seal_on_either_board(self):
        requested = self.record()
        requested['parameters']['selection_only'] = True
        requested = with_identities(requested)
        for reader in (exporter, de25):
            encoded = reader.encode_build_record(requested)
            with self.assertRaisesRegex(reader.PackageExportError, 'selection-only'):
                reader._verify_build_evidence(None, encoded, requested, {})

    def test_legacy_cache_key_is_not_the_new_design_key(self):
        legacy = fields()
        self.assertNotEqual(functional_key(legacy), functional_key(with_identities(legacy)))

    def test_recording_provenance_does_not_change_manifest_toolchain(self):
        before = self.record()
        after = copy.deepcopy(before)
        after['revision'] = 'e' * 40
        after['repository'] = 'https://example.invalid/another-origin'
        after['source_path'] = 'another/checkout'
        after = with_identities(after)
        self.assertEqual(before['identities']['design'], after['identities']['design'])
        self.assertEqual(before['identities']['toolchain'], after['identities']['toolchain'])
        self.assertEqual(manifest_toolchain(before), manifest_toolchain(after))
        self.assertNotEqual(before['identities']['record'], after['identities']['record'])


if __name__ == '__main__':
    unittest.main()
