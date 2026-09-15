"""FES platform builder selects temporary workspace inputs and bindable provenance."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from scripts import appliance, platform as fes_platform
from scripts.environment import build_environment


ROOT = Path(__file__).resolve().parents[1]
FOGCAST = ROOT.parent / 'FogCast'


def write_tree(root, files):
    root = Path(root)
    for relative, data in files.items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data if isinstance(data, bytes) else data.encode())
    return root


class PlatformWorkspaceTests(unittest.TestCase):
    def test_scripts_platform_keeps_stdlib_imports_when_loaded_top_level(self):
        script_dir = ROOT / 'scripts'
        code = (
            'import platform, uuid; '
            "assert platform.system(); assert platform.machine(); "
            'assert platform.tree_identity; uuid.uuid4()'
        )
        result = subprocess.run(
            [sys.executable, '-c', code],
            env={**os.environ, 'PYTHONPATH': str(script_dir)},
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_temporary_workspace_uses_platform_and_selected_appliance_module(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            platform_dir = write_tree(root / 'platform', {'go.mod': 'module github.com/DeanoC/fes/platform\n\ngo 1.26.5\n'})
            appliance_dir = write_tree(root / 'FogCast' / 'appliance', {
                'go.mod': 'module github.com/DeanoC/FogCast/appliance\n\ngo 1.26.5\n',
                'release.go': 'package appliance\n',
            })
            fogcast_cmd = write_tree(root / 'FogCast' / 'cmd' / 'fes-boot', {'main.go': 'package main\n'})
            workspace = fes_platform.write_workspace(root / 'work', platform_dir, appliance_dir)
            text = workspace.read_text()
            self.assertIn(str(platform_dir), text)
            self.assertIn(str(appliance_dir), text)
            self.assertIn('github.com/DeanoC/FogCast/appliance v0.0.0 =>', text)
            self.assertNotIn(str(fogcast_cmd), text)
            self.assertNotIn('FogCast/cmd/fes-boot', text)
            self.assertFalse((platform_dir / 'go.work').exists())
            self.assertFalse((ROOT / 'platform' / 'go.work').exists())
            self.assertNotIn('replace github.com/DeanoC/FogCast/appliance', (platform_dir / 'go.mod').read_text())

    def test_appliance_module_identity_changes_and_mismatch_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first = write_tree(root / 'a', {'go.mod': 'module github.com/DeanoC/FogCast/appliance\n', 'release.go': 'package appliance\n'})
            second = write_tree(root / 'b', {'go.mod': 'module github.com/DeanoC/FogCast/appliance\n', 'release.go': 'package appliance\nfunc Changed() {}\n'})
            self.assertNotEqual(fes_platform.tree_identity(first), fes_platform.tree_identity(second))
            fes = write_tree(root / 'fes' / 'platform', {'go.mod': 'module github.com/DeanoC/fes/platform\n'})
            record = fes_platform.platform_build_record(
                fes_platform.tree_identity(fes), fes_platform.tree_identity(first), 'c' * 40, 'go1.26.5', 'd' * 64
            )
            with self.assertRaisesRegex(ValueError, 'appliance.module|module identity'):
                fes_platform.verify_identities(root / 'fes', second, record)

    def test_selected_appliance_requires_an_exact_module_declaration(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'go.mod').write_text(
                'module github.com/example/fogcast\n\n'
                'require github.com/DeanoC/FogCast/appliance v0.0.0\n'
            )
            with self.assertRaisesRegex(ValueError, 'appliance module'):
                fes_platform.selected_appliance(root)

    def test_build_record_binds_flags_toolchain_and_omits_paths(self):
        record = fes_platform.platform_build_record(
            platform_sha256='a' * 64,
            appliance_module_sha256='b' * 64,
            fogcast_revision='c' * 40,
            go_version='go1.26.5',
            go_sha256='d' * 64,
        )
        self.assertEqual(record['build_flags'], ['-trimpath', '-buildvcs=false', '-ldflags=-s -w -buildid='])
        self.assertEqual(record['build_env']['GOOS'], 'linux')
        self.assertEqual(record['build_env']['GOARCH'], 'arm')
        self.assertEqual(record['build_env']['GOARM'], '7')
        self.assertEqual(record['build_env']['CGO_ENABLED'], '0')
        self.assertEqual(record['package'], './cmd/fes-boot')
        self.assertEqual(record['go_version'], 'go1.26.5')
        self.assertEqual(record['fogcast_revision'], 'c' * 40)
        encoded = json.dumps(record)
        self.assertNotRegex(encoded, r'(^|["\\s])/home/')
        self.assertNotIn(str(ROOT), encoded)
        self.assertNotIn(str(FOGCAST), encoded)
        fes_platform.validate_platform_build(record)

    def test_bootstrap_identity_invalidates_when_platform_source_changes(self):
        factory = {'format': 1, 'board': 'de10-nano'}
        recipe = {'scripts/appliance.py': '1' * 64}
        first = fes_platform.platform_build_record('a' * 64, 'b' * 64, 'c' * 40, 'go1.26.5', 'd' * 64)
        second = fes_platform.platform_build_record('e' * 64, 'b' * 64, 'c' * 40, 'go1.26.5', 'd' * 64)
        identity = appliance.bootstrap_identity('2' * 64, factory, 'sha256:' + '3' * 64, '4' * 40, recipe, first)
        self.assertNotEqual(identity, appliance.bootstrap_identity('2' * 64, factory, 'sha256:' + '3' * 64, '4' * 40, recipe, second))
        self.assertNotEqual(identity, appliance.bootstrap_identity('2' * 64, factory, 'sha256:' + '3' * 64, '4' * 40, recipe))


class PlatformBuildTests(unittest.TestCase):
    def test_build_static_arm_consumes_selected_module_without_copying(self):
        if not FOGCAST.is_dir():
            self.skipTest('selected FogCast checkout is absent')
        env = build_environment()
        with tempfile.TemporaryDirectory(prefix='fes-platform-arm-') as temporary:
            output = Path(temporary) / 'fes-boot'
            with mock.patch('shutil.copytree') as copied:
                digest, record = fes_platform.build_static_arm(ROOT, FOGCAST, output, env)
            copied.assert_not_called()
            self.assertEqual(digest, appliance.validate_static_arm(output))
            self.assertEqual(record['platform_sha256'], fes_platform.tree_identity(ROOT / 'platform'))
            self.assertEqual(record['appliance_module_sha256'], fes_platform.tree_identity(FOGCAST / 'appliance'))
            self.assertEqual(record['fogcast_revision'], subprocess.check_output(['git', '-C', str(FOGCAST), 'rev-parse', 'HEAD'], text=True).strip())
            self.assertEqual(record['package'], './cmd/fes-boot')
            encoded = json.dumps(record)
            self.assertNotIn(str(ROOT), encoded)
            self.assertNotIn(str(FOGCAST), encoded)
            self.assertNotRegex(encoded, r'/home/deano/')
            self.assertFalse((ROOT / 'platform' / 'go.work').exists())
            self.assertNotIn('replace github.com/DeanoC/FogCast/appliance', (ROOT / 'platform' / 'go.mod').read_text())
