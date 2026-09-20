import importlib.util
import hashlib
import json
from dataclasses import replace
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/bundle.py"


class BundleTest(unittest.TestCase):
    def module(self):
        self.assertTrue(SCRIPT.exists(), "bundle integration is not implemented")
        spec = importlib.util.spec_from_file_location("bundle", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_misteross_origin_is_reset_to_the_authenticated_repository(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            import subprocess
            subprocess.run(['git', 'init', '-q', source], check=True)
            subprocess.run(['git', '-C', source, 'remote', 'add', 'origin', '/local/source'], check=True)
            module.authenticate_misteross_origin(source)
            self.assertEqual(subprocess.check_output(
                ['git', '-C', source, 'remote', 'get-url', '--all', 'origin'], text=True).strip(),
                module.MISTEROSS_REPOSITORY)

    def test_monorepo_origin_is_preserved_without_claiming_child_repository(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / 'sources/misteross'
            source.mkdir(parents=True)
            subprocess.run(['git', 'init', '-q', root], check=True)
            origin = module.FES_REPOSITORY.removesuffix('.git')
            subprocess.run(['git', '-C', root, 'remote', 'add', 'origin', origin], check=True)
            module.authenticate_misteross_origin(source)
            self.assertEqual(subprocess.check_output(
                ['git', '-C', root, 'remote', 'get-url', 'origin'], text=True).strip(), origin)

    def test_package_resolution_reuses_only_one_matching_validated_candidate(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            store = source / 'build/packages'
            store.mkdir(parents=True)
            identity = 'a' * 64
            record = b'{"canonical":true}\n'
            sidecar = store / f'{identity}.build-inputs.json'
            sidecar.write_bytes(record)
            sidecar.chmod(0o444)
            package = store / identity
            package.mkdir()
            (package / 'manifest.toml').write_bytes(b'manifest')
            (package / 'core.rbf').write_bytes(b'payload')
            payload_sha = hashlib.sha256(b'payload').hexdigest()
            selection_path = source / 'selection.toml'
            inspected = {
                'package_id': identity,
                'manifest': {'core': {'id': 'fes.pong'},
                             'payload': {'sha256': payload_sha},
                             'build': {'revision': 'c' * 40}},
                'manifest_sha256': hashlib.sha256(b'manifest').hexdigest(),
                'core_rbf_sha256': payload_sha,
            }
            with patch.object(module, 'canonical_package_record', return_value=record), \
                 patch.object(module, '_inspect_package_candidate', return_value=inspected), \
                 patch.object(module, '_build_package', side_effect=AssertionError('unexpected build')):
                resolved = module.resolve_core_package(source, 'd' * 40, selection_path)
            self.assertEqual(resolved['directory'], package)
            self.assertEqual(resolved['inputs']['selection']['package_id'], identity)
            self.assertEqual(resolved['inputs']['selection']['mister_packages_revision'], 'd' * 40)
            self.assertEqual(resolved['selection_path'], selection_path)
            self.assertEqual(selection_path.stat().st_mode & 0o777, 0o444)

            second = store / (('e' * 64) + '.build-inputs.json')
            second.write_bytes(record)
            second.chmod(0o444)
            (store / ('e' * 64)).mkdir()
            (store / ('e' * 64) / 'manifest.toml').write_bytes(b'manifest')
            (store / ('e' * 64) / 'core.rbf').write_bytes(b'payload')
            with patch.object(module, 'canonical_package_record', return_value=record), \
                 patch.object(module, '_inspect_package_candidate',
                              side_effect=lambda _, package, __, **_kwargs: dict(
                                  inspected, package_id=Path(package).name)):
                with self.assertRaisesRegex(ValueError, 'multiple'):
                    module.resolve_core_package(source, 'd' * 40, selection_path)

    def test_package_resolution_builds_once_when_no_canonical_candidate_exists(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            store = source / 'build/packages'
            identity = 'a' * 64
            record = b'{"canonical":true}\n'
            inspected = {
                'package_id': identity,
                'manifest': {'core': {'id': 'fes.pong'},
                             'payload': {'sha256': hashlib.sha256(b'payload').hexdigest()},
                             'build': {'revision': 'c' * 40}},
                'manifest_sha256': hashlib.sha256(b'manifest').hexdigest(),
                'core_rbf_sha256': hashlib.sha256(b'payload').hexdigest(),
            }
            def build_once(*_args, **_kwargs):
                store.mkdir(parents=True)
                (store / f'{identity}.build-inputs.json').write_bytes(record)
                (store / f'{identity}.build-inputs.json').chmod(0o444)
                (store / identity).mkdir()
                (store / identity / 'manifest.toml').write_bytes(b'manifest')
                (store / identity / 'core.rbf').write_bytes(b'payload')
            with patch.object(module, 'canonical_package_record', return_value=record), \
                 patch.object(module, '_inspect_package_candidate', return_value=inspected), \
                 patch.object(module, '_build_package', side_effect=build_once) as build_package:
                module.resolve_core_package(source, 'd' * 40, source / 'selection.toml')
            build_package.assert_called_once()
            self.assertEqual(build_package.call_args.args[0], source)

    def test_package_resolution_rejects_symlinked_store_and_candidate(self):
        module = self.module()
        for link in ('store', 'package'):
            with self.subTest(link=link), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                source = root / 'source'
                ambient = root / 'ambient'
                (source / 'build').mkdir(parents=True)
                ambient.mkdir()
                identity = 'a' * 64
                record = b'{"canonical":true}\n'
                if link == 'store':
                    store = ambient / 'packages'
                    store.mkdir()
                    (source / 'build/packages').symlink_to(store, target_is_directory=True)
                else:
                    store = source / 'build/packages'
                    store.mkdir()
                sidecar = store / f'{identity}.build-inputs.json'
                sidecar.write_bytes(record)
                sidecar.chmod(0o444)
                ambient_package = ambient / identity
                ambient_package.mkdir(exist_ok=True)
                if link == 'store':
                    package = store / identity
                    package.mkdir()
                else:
                    (store / identity).symlink_to(ambient_package, target_is_directory=True)
                with patch.object(module, 'canonical_package_record', return_value=record), \
                     patch.object(module, '_inspect_package_candidate',
                                  side_effect=AssertionError('symlink reached reader')):
                    with self.assertRaisesRegex(ValueError, 'symlink|contained|store'):
                        module.resolve_core_package(source, 'd' * 40, source / 'selection.toml')

    def test_selected_misteross_pin_enables_shared_toolchain_cache(self):
        root = Path(__file__).resolve().parents[1]
        staged = subprocess.check_output(
            ['git', '-C', str(root), 'ls-files', '--stage', '--', 'sources/misteross'],
            text=True)
        mode, revision, stage, path = staged.split()
        self.assertEqual((mode, stage, path), ('160000', '0', 'sources/misteross'))
        source = root / path
        self.assertEqual(subprocess.check_output(
            ['git', '-C', str(source), 'rev-parse', 'HEAD'], text=True).strip(), revision)
        subprocess.run(['git', '-C', str(source), 'diff', '--exit-code', revision, '--'],
                       check=True, capture_output=True, text=True)
        producer = root / 'sources/misteross/scripts/build_fes_sms_oss.py'
        self.assertTrue(producer.is_file())
        self.assertTrue((root / 'sources/misteross/cores/fes-sms/toolchain.lock').is_file())
        text = producer.read_text()
        self.assertRegex(text, r'(?m)^PLACER_SEEDS = \(10,')
        self.assertRegex(text, r'(?m)^SEED = PLACER_SEEDS\[0\]$')
        module = self.module()
        # Probe the selected producer's CLI and authentication call contract,
        # without building tools or accepting an unrelated local checkout.
        probe = '''
import importlib
import inspect
import sys
from pathlib import Path
producer = importlib.import_module(sys.argv[1])
inspect.signature(getattr(producer, sys.argv[2])).bind(
    Path.cwd(), cache_root=Path("unused-cache-probe"))
'''
        for recipe in module.FORMAT2_RECIPES.values():
            with self.subTest(core=recipe.core_id):
                self.assertTrue((source / recipe.lock_path).is_file())
                help_text = subprocess.check_output(
                    [sys.executable, str(source / recipe.producer_script), '--help'],
                    cwd=source, text=True, timeout=30)
                self.assertIn('--cache-root', help_text)
                subprocess.run(
                    [sys.executable, '-c', probe, recipe.producer_module, recipe.authenticate],
                    cwd=source, check=True, capture_output=True, text=True, timeout=30)
        common = Path(subprocess.check_output(
            ['git', '-C', root, 'rev-parse', '--path-format=absolute', '--git-common-dir'], text=True).strip())
        self.assertEqual(module.TOOLCHAIN_CACHE_ROOT, common.parent / 'out/cache/misteross-toolchains')
        docs = (root / 'docs/core-packages.md').read_text()
        self.assertIn(
            'FES_TOOLCHAIN_CACHE_ROOT="$cache" \\\n  make -C "$work" toolchain-fes', docs)
        self.assertIn(
            'FES_TOOLCHAIN_CACHE_ROOT="$cache" \\\n  FES_TOOLCHAIN_LOCKFILE=cores/fes-coleco/toolchain.lock \\\n  FES_TOOLCHAIN_GPU_ROUTER=HIP \\\n  FES_TOOLCHAIN_HIP_ARCHITECTURES=\'gfx1100;gfx1201\' \\\n  make -C "$work" doctor-strict', docs)

    def test_canonical_package_record_forwards_package_environment(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            payload = b'{"canonical":true}\n'
            caller = {'KEEP': '1', 'PATH': '/bin'}
            sentinel = os.environ.get('FES_TOOLCHAIN_CACHE_ROOT')
            with patch.object(module, 'authenticate_misteross_origin'), \
                 patch.object(module.subprocess, 'check_output', return_value=payload) as check:
                module.canonical_package_record(source, env=caller)
            env = check.call_args.kwargs['env']
            self.assertEqual(env['KEEP'], '1')
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', env)
            self.assertIn('--cache-root', [str(part) for part in check.call_args.args[0]])
            self.assertEqual(env['FES_TOOLCHAIN_GPU_ROUTER'], 'HIP')
            self.assertNotIn('MAKEFLAGS', env)
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', caller)
            self.assertEqual(os.environ.get('FES_TOOLCHAIN_CACHE_ROOT'), sentinel)

    def test_package_resolution_forwards_env_to_record_and_build_subprocesses(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            store = source / 'build/packages'
            identity = 'a' * 64
            record = b'{"canonical":true}\n'
            inspected = {
                'package_id': identity,
                'manifest': {'core': {'id': 'fes.pong'},
                             'payload': {'sha256': hashlib.sha256(b'payload').hexdigest()},
                             'build': {'revision': 'c' * 40}},
                'manifest_sha256': hashlib.sha256(b'manifest').hexdigest(),
                'core_rbf_sha256': hashlib.sha256(b'payload').hexdigest(),
            }
            received = []
            caller = {'KEEP': '1', 'PATH': '/bin'}

            def fake_check_output(args, **kwargs):
                received.append(kwargs.get('env'))
                return record

            def fake_run(args, **kwargs):
                received.append(kwargs.get('env'))
                store.mkdir(parents=True)
                (store / f'{identity}.build-inputs.json').write_bytes(record)
                (store / f'{identity}.build-inputs.json').chmod(0o444)
                (store / identity).mkdir()
                (store / identity / 'manifest.toml').write_bytes(b'manifest')
                (store / identity / 'core.rbf').write_bytes(b'payload')
                return subprocess.CompletedProcess(args, 0)

            with patch.object(module, 'authenticate_misteross_origin'), \
                 patch.object(module.subprocess, 'check_output', side_effect=fake_check_output), \
                 patch.object(module.subprocess, 'run', side_effect=fake_run), \
                 patch.object(module, '_inspect_package_candidate', return_value=inspected):
                module.resolve_core_package(
                    source, 'd' * 40, source / 'selection.toml', env=caller)
            self.assertEqual(len(received), 2)
            for env in received:
                self.assertEqual(env['KEEP'], '1')
                self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', env)
                self.assertEqual(env['FES_TOOLCHAIN_GPU_ROUTER'], 'HIP')
                self.assertNotIn('MAKEFLAGS', env)
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', caller)

    def test_package_build_environment_strips_shared_lane_overrides(self):
        module = self.module()
        caller = {
            'PATH': '/bin',
            'HOME': '/home/operator',
            'KEEP': '1',
            'LD_LIBRARY_PATH': '/opt/rocm/lib',
            'PKG_CONFIG_PATH': '/opt/rocm/lib/pkgconfig',
            'PYTHON': 'python3',
            'CC': 'clang',
            'CXX': 'clang++',
            'CPPFLAGS': '-I/opt',
            'CFLAGS': '-O0',
            'CXXFLAGS': '-O0',
            'LDFLAGS': '-L/opt',
            'MAKEFLAGS': 's',
            'MFLAGS': '-j2',
            'FES_TOOLCHAIN_GPU_ROUTER': 'OFF',
            'ROCM_PATH': '/opt/rocm/core-7.14',
            'HIPCC': '/opt/rocm/bin/hipcc',
        }
        sentinel = os.environ.get('LD_LIBRARY_PATH')
        mapped = module.package_build_environment(caller)
        self.assertEqual(mapped['PATH'], '/bin')
        self.assertEqual(mapped['HOME'], '/home/operator')
        self.assertEqual(mapped['KEEP'], '1')
        self.assertEqual(mapped['FES_TOOLCHAIN_GPU_ROUTER'], 'HIP')
        self.assertEqual(mapped['FES_TOOLCHAIN_HIP_ARCHITECTURES'], 'gfx1100;gfx1201')
        self.assertEqual(mapped['ROCM_PATH'], '/opt/rocm/core-7.14')
        self.assertEqual(mapped['HIPCC'], '/opt/rocm/bin/hipcc')
        self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', mapped)
        for name in ('LD_LIBRARY_PATH', 'PKG_CONFIG_PATH', 'PYTHON', 'CC', 'CXX',
                     'CPPFLAGS', 'CFLAGS', 'CXXFLAGS', 'LDFLAGS', 'MAKEFLAGS', 'MFLAGS'):
            self.assertNotIn(name, mapped)
        self.assertEqual(caller['LD_LIBRARY_PATH'], '/opt/rocm/lib')
        self.assertEqual(caller['MAKEFLAGS'], 's')
        self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', caller)
        self.assertEqual(os.environ.get('LD_LIBRARY_PATH'), sentinel)
        with patch.dict('os.environ', {'LD_LIBRARY_PATH': '/opt/rocm/lib', 'CC': 'gcc'}, clear=False):
            from_environ = module.package_build_environment()
            self.assertNotIn('LD_LIBRARY_PATH', from_environ)
            self.assertNotIn('CC', from_environ)
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', from_environ)
            self.assertEqual(from_environ['FES_TOOLCHAIN_GPU_ROUTER'], 'HIP')
            self.assertEqual(os.environ.get('LD_LIBRARY_PATH'), '/opt/rocm/lib')
            self.assertEqual(os.environ.get('CC'), 'gcc')

    def test_build_fes_pong_opts_into_shared_toolchain_cache_without_mutating_environ(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            completed = subprocess.CompletedProcess(['python'], 0)
            sentinel = os.environ.get('FES_TOOLCHAIN_CACHE_ROOT')
            caller = {'KEEP': '1', 'LD_LIBRARY_PATH': '/opt/rocm/lib', 'MAKEFLAGS': 's'}
            with patch.object(module.subprocess, 'run', return_value=completed) as run:
                module._build_fes_pong(source, env=caller)
            env = run.call_args.kwargs['env']
            args = [str(part) for part in run.call_args.args[0]]
            self.assertEqual(env['KEEP'], '1')
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', env)
            self.assertIn('--cache-root', args)
            self.assertEqual(args[args.index('--cache-root') + 1], str(module.TOOLCHAIN_CACHE_ROOT))
            self.assertEqual(env['FES_TOOLCHAIN_GPU_ROUTER'], 'HIP')
            self.assertNotIn('LD_LIBRARY_PATH', env)
            self.assertNotIn('MAKEFLAGS', env)
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', caller)
            self.assertEqual(caller['LD_LIBRARY_PATH'], '/opt/rocm/lib')
            self.assertEqual(os.environ.get('FES_TOOLCHAIN_CACHE_ROOT'), sentinel)
            with patch.object(module.subprocess, 'run', return_value=completed) as run:
                module._build_fes_pong(source)
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', run.call_args.kwargs['env'])
            self.assertIn('--cache-root', [str(part) for part in run.call_args.args[0]])

    def test_package_resolution_opts_into_shared_toolchain_cache(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            store = source / 'build/packages'
            identity = 'a' * 64
            record = b'{"canonical":true}\n'
            inspected = {
                'package_id': identity,
                'manifest': {'core': {'id': 'fes.pong'},
                             'payload': {'sha256': hashlib.sha256(b'payload').hexdigest()},
                             'build': {'revision': 'c' * 40}},
                'manifest_sha256': hashlib.sha256(b'manifest').hexdigest(),
                'core_rbf_sha256': hashlib.sha256(b'payload').hexdigest(),
            }

            def fake_run(args, **kwargs):
                env = kwargs.get('env') or {}
                self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', env)
                self.assertEqual(env.get('FES_TOOLCHAIN_GPU_ROUTER'), 'HIP')
                self.assertIn('--cache-root', [str(part) for part in args])
                store.mkdir(parents=True)
                (store / f'{identity}.build-inputs.json').write_bytes(record)
                (store / f'{identity}.build-inputs.json').chmod(0o444)
                (store / identity).mkdir()
                (store / identity / 'manifest.toml').write_bytes(b'manifest')
                (store / identity / 'core.rbf').write_bytes(b'payload')
                return subprocess.CompletedProcess(args, 0)

            with patch.object(module, 'canonical_package_record', return_value=record), \
                 patch.object(module, '_inspect_package_candidate', return_value=inspected), \
                 patch.object(module.subprocess, 'run', side_effect=fake_run) as run:
                module.resolve_core_package(source, 'd' * 40, source / 'selection.toml')
            run.assert_called()

    def test_package_resolution_rejects_symlinked_source_checkout(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            actual = root / 'actual'
            actual.mkdir()
            source = root / 'source'
            source.symlink_to(actual, target_is_directory=True)
            with patch.object(module, 'canonical_package_record',
                              side_effect=AssertionError('symlink reached producer')):
                with self.assertRaisesRegex(ValueError, 'non-symlink'):
                    module.resolve_core_package(source, 'd' * 40, root / 'selection.toml')


if __name__ == "__main__":
    unittest.main()


class FunctionalReuseTest(unittest.TestCase):
    def test_new_revision_reuses_original_bytes_and_records_both_sources(self):
        import artifact_cache
        import bundle
        from recipes import recipe_for
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / 'selected'
            source.mkdir()
            package = root / ('a' * 64)
            package.mkdir()
            (package / 'manifest.toml').write_bytes(b'original manifest')
            (package / 'core.rbf').write_bytes(b'original payload')
            original = {'format': 2, 'repository': 'https://example.invalid/fes',
                        'revision': '1' * 40, 'source_path': 'sources/misteross',
                        'source_inputs': {'rtl/top.v': 'b' * 64}, 'parameters': {'clock': 52}}
            selected = dict(original, revision='2' * 40)
            encoded = lambda fields: (json.dumps(fields, sort_keys=True, separators=(',', ':')) + '\n').encode()
            record_path = root / 'original.build-inputs.json'
            record_path.write_bytes(encoded(original))
            cache = root / 'cache'
            cached = artifact_cache.publish(cache, record_path, package)
            staging = artifact_cache.store_for(cache, encoded(selected)) / '.publish-in-progress'
            staging.mkdir()
            (staging / 'build-inputs.json').write_bytes(encoded(original))
            payload_sha = hashlib.sha256(b'original payload').hexdigest()
            inspected = {'package_id': package.name,
                         'manifest': {'core': {'id': 'fes.pong'}, 'payload': {'sha256': payload_sha},
                                      'build': {'revision': original['revision']}},
                         'manifest_sha256': hashlib.sha256(b'original manifest').hexdigest(),
                         'core_rbf_sha256': payload_sha}
            recipe = replace(recipe_for('fes.pong'), identity_version=2)
            with patch.object(bundle, 'ARTIFACT_CACHE_ROOT', cache), \
                 patch.object(bundle, 'canonical_package_record', return_value=encoded(selected)), \
                 patch.object(bundle, '_inspect_package_candidate', return_value=inspected) as inspect, \
                 patch.object(bundle, '_build_package', side_effect=AssertionError('unnecessary FPGA rebuild')):
                result = bundle.resolve_core_package(source, '3' * 40, root / 'selection.toml', recipe=recipe)
            self.assertEqual(result['directory'], cached)
            self.assertEqual(inspect.call_args.args[2].read_bytes(), encoded(original))
            self.assertEqual((cached / 'manifest.toml').read_bytes(), b'original manifest')
            self.assertEqual((cached / 'core.rbf').read_bytes(), b'original payload')
            receipt = json.loads((root / 'selection.provenance.json').read_bytes())
            self.assertEqual(receipt['original_revision'], original['revision'])
            self.assertEqual(receipt['selected_revision'], selected['revision'])
            self.assertEqual(result['inputs']['source_selection'], receipt)
            self.assertEqual(result['inputs']['selection']['misteross_revision'], original['revision'])
            for directory in sorted(cache.rglob('*'), key=lambda p: len(p.parts), reverse=True):
                if directory.is_dir():
                    directory.chmod(0o755)
