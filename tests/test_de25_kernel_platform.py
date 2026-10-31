import difflib
import hashlib
import json
import os
from pathlib import Path
import platform as host_platform
import shutil
import subprocess
import tempfile
import unittest

from scripts import de25_kernel_platform as platform


def sha(data):
    return hashlib.sha256(data.encode()).hexdigest()


@unittest.skipUnless(shutil.which('cc') and host_platform.system() == 'Linux', 'Linux C compiler required')
class UserspaceOwnerCheckTests(unittest.TestCase):
    def test_native_cli_refuses_before_opening_a_device(self):
        with tempfile.TemporaryDirectory() as temp:
            binary = Path(temp) / 'owner_unconfigured'
            source = platform.ROOT / platform.PROFILE / 'owner_unconfigured.c'
            subprocess.run(['cc', '-std=c11', '-O2', '-Wall', '-Wextra', '-Werror',
                            '-o', str(binary), str(source)], check=True, capture_output=True)
            cases = [([], 'usage:'), (['--boot-id', 'bad'], 'invalid expected boot UUID')]
            if host_platform.machine() != 'aarch64' or os.uname().release != platform.RELEASE:
                cases.append((['--boot-id', '00000000-0000-4000-8000-000000000001'],
                              'requires the AArch64 owner-candidate board kernel'))
            for args, message in cases:
                with self.subTest(args=args):
                    result = subprocess.run([str(binary), *args], capture_output=True, text=True, timeout=5)
                    self.assertEqual(result.returncode, 1)
                    self.assertIn(message, result.stderr)
                    self.assertNotIn('USERSPACE_PASS', result.stdout)


class KernelPlatformTests(unittest.TestCase):
    def fixture(self, root):
        kernel, profile = root / 'kernel', root / 'profile'
        kernel.mkdir()
        profile.mkdir()
        before, after = 'first\nold\nlast\n', 'first\nprotected\nlast\n'
        (kernel / 'supplier.c').write_text(before)
        patch = ''.join(difflib.unified_diff(before.splitlines(True), after.splitlines(True),
                                           fromfile='a/supplier.c', tofile='b/supplier.c'))
        (profile / 'permanent-suppliers.patch').write_text(patch)
        record = {'format': 1, 'kernel_revision': platform.dep.selected_lock()['revision'],
                  'patch_sha256': sha(patch), 'unchanged_supplier_sources': {},
                  'files': {'supplier.c': {'before_sha256': sha(before), 'after_sha256': sha(after)}}}
        (profile / 'source.json').write_text(json.dumps(record))
        return kernel, profile, record, before, after

    def test_patch_authenticates_entire_input_and_exact_result(self):
        with tempfile.TemporaryDirectory() as temp:
            kernel, profile, _, _, after = self.fixture(Path(temp))
            platform.apply_profile(kernel, profile)
            self.assertEqual((kernel / 'supplier.c').read_text(), after)
            with self.assertRaisesRegex(ValueError, 'supplier input differs'):
                platform.apply_profile(kernel, profile)

    def test_changed_source_or_patch_refuses_before_mutation(self):
        for change in ('source', 'patch'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temp:
                kernel, profile, _, before, _ = self.fixture(Path(temp))
                if change == 'source':
                    before += 'unrelated edit\n'
                    (kernel / 'supplier.c').write_text(before)
                else:
                    (profile / 'permanent-suppliers.patch').write_text('corrupt\n')
                with self.assertRaises(ValueError):
                    platform.apply_profile(kernel, profile)
                self.assertEqual((kernel / 'supplier.c').read_text(), before)

    def test_bad_result_hash_is_never_reported_as_success(self):
        with tempfile.TemporaryDirectory() as temp:
            kernel, profile, record, _, after = self.fixture(Path(temp))
            record['files']['supplier.c']['after_sha256'] = '0' * 64
            (profile / 'source.json').write_text(json.dumps(record))
            with self.assertRaisesRegex(ValueError, 'supplier result differs'):
                platform.apply_profile(kernel, profile)
            # This is a private failed output, not a transaction on shared source.
            self.assertEqual((kernel / 'supplier.c').read_text(), after)

    def test_modular_owner_or_test_hooks_cannot_pass_config_verification(self):
        text = ''.join('CONFIG_' + n + '=y\n' for n in platform.ENABLED)
        text += ''.join('# CONFIG_' + n + ' is not set\n' for n in platform.DISABLED)
        text += 'CONFIG_LOCALVERSION="-fes-de25-owner"\n'
        platform.verify_config(text)
        platform.verify_config(text.replace('# CONFIG_FES_DE25_DMA_KUNIT_TEST is not set\n', ''))
        for changed in (text.replace('CONFIG_FES_DE25_DMA=y', 'CONFIG_FES_DE25_DMA=m'),
                        text.replace('# CONFIG_FES_DE25_DMA_KUNIT_TEST is not set',
                                     'CONFIG_FES_DE25_DMA_KUNIT_TEST=y')):
            with self.assertRaises(ValueError):
                platform.verify_config(changed)
