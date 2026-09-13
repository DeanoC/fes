import importlib.util
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import tomllib
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

    def test_prepare_replaces_only_megadrive_input_and_lock_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fogcast = root / "FogCast"
            cache = fogcast / "build/cache/target-image/native"
            cache.mkdir(parents=True)
            (cache / "idle.rbf").write_bytes(b"idle")
            (cache / "megadrive.rbf").write_bytes(b"upstream")
            lock = fogcast / "build/native-runtime.inputs.lock.toml"
            lock.write_text("""format = 1
[mister_runtime]
commit = '1111111111111111111111111111111111111111'
mount_path = '/runtime-source'
[idle_rbf]
sha256 = 'idle'
[megadrive_rbf]
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
commit = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
path = 'releases/MegaDrive_20260603.rbf'
sha256 = 'upstream'
size = 8
install_path = '/usr/share/mister-runtime/cores/megadrive.rbf'
""")
            bundle = root / "bundle"
            bundle.mkdir()
            payload = b"source-built-rbf"
            (bundle / "megadrive.rbf").write_bytes(payload)
            digest = hashlib.sha256(payload).hexdigest()
            (bundle / "megadrive-rbf.toml").write_text(f"""format = 1
abi = "mister"
system = "megadrive"
artifact = "megadrive.rbf"
sha256 = "{digest}"
size = {len(payload)}
repository = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
revision = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
recipe = "scripts/rebuild_core.py"
recipe_sha256 = "{'2' * 64}"
toolchain = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
""")

            module = self.module()
            original_lock = lock.read_text()
            with self.assertRaisesRegex(ValueError, "recipe digest differs"):
                module.load(bundle, expected_recipe_sha256="3" * 64)
            with self.assertRaisesRegex(ValueError, "recipe digest differs"):
                module.prepare(fogcast, bundle, expected_recipe_sha256="3" * 64)
            self.assertEqual(lock.read_text(), original_lock)
            self.assertEqual((cache / "megadrive.rbf").read_bytes(), b"upstream")
            self.assertEqual(module.load(bundle)["recipe_sha256"], "2" * 64)
            self.assertEqual(module.load(bundle, "2" * 64)["recipe_sha256"], "2" * 64)

            result = module.prepare(fogcast, bundle, expected_recipe_sha256="2" * 64)

            self.assertEqual((cache / "megadrive.rbf").read_bytes(), payload)
            self.assertEqual((cache / "idle.rbf").read_bytes(), b"idle")
            parsed = tomllib.loads(lock.read_text())
            self.assertEqual(parsed["megadrive_rbf"]["sha256"], digest)
            self.assertEqual(parsed["megadrive_rbf"]["size"], len(payload))
            self.assertEqual(parsed["megadrive_rbf"]["artifact_kind"], "source_rebuild_bundle")
            self.assertEqual(result["sha256"], digest)

    def test_rejects_manifest_or_payload_mismatch(self):
        with tempfile.TemporaryDirectory() as temporary:
            bundle = Path(temporary)
            (bundle / "megadrive.rbf").write_bytes(b"wrong")
            (bundle / "megadrive-rbf.toml").write_text("""format = 1
abi = "mister"
system = "megadrive"
artifact = "megadrive.rbf"
sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
size = 5
repository = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
revision = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
recipe = "scripts/rebuild_core.py"
recipe_sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
toolchain = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
""")
            with self.assertRaisesRegex(ValueError, "digest"):
                self.module().load(bundle)

    def test_all_systems_and_identity_drift(self):
        module = self.module()
        for system, repository, revision, recipe in (
            ("megadrive", module.REPOSITORY, module.REVISION, "scripts/rebuild_core.py"),
            ("snes", "https://github.com/MiSTer-devel/SNES_MiSTer",
             "93d359e6f23c734ae3928984e88bed1d9b53cbac", "scripts/rebuild_core.py"),
            ("nes", "https://github.com/MiSTer-devel/NES_MiSTer",
             "9a63821173b6da4d6e95dcbe2e2a322ec8171144", "scripts/rebuild_core.py"),
            ("pong", "https://github.com/DeanoC/misteross", "1" * 40, "scripts/build_pong.py"),
        ):
            with self.subTest(system=system), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                artifact = directory / f"{system}.rbf"
                artifact.write_bytes(b"rbf")
                manifest_path = directory / f"{system}-rbf.toml"
                manifest = dict(format=1, abi="mister", system=system,
                                artifact=artifact.name, repository=repository,
                                revision=revision, recipe=recipe, toolchain=module.TOOLCHAIN,
                                sha256=hashlib.sha256(b"rbf").hexdigest(), size=3,
                                recipe_sha256="2" * 64)
                def write(values):
                    manifest_path.write_text("".join(f"{key} = {value!r}\n" for key, value in values.items()))
                def load():
                    return module.load(directory, "2" * 64, system=system, expected_revision=revision)
                write(manifest)
                self.assertEqual(load(), manifest)
                for field, value in (("system", "other"), ("repository", "https://example.org/wrong"),
                                     ("revision", "9" * 40), ("recipe", "scripts/wrong.py"),
                                     ("recipe_sha256", "3" * 64), ("toolchain", "wrong"),
                                     ("artifact", "../other.rbf"), ("sha256", "4" * 64),
                                     ("size", 4), ("extra", "unexpected")):
                    with self.subTest(field=field):
                        write(dict(manifest, **{field: value}))
                        with self.assertRaises(ValueError):
                            load()
                write(manifest)
                artifact.write_bytes(b"bad")
                with self.assertRaisesRegex(ValueError, "digest"):
                    load()
                artifact.write_bytes(b"")
                with self.assertRaisesRegex(ValueError, "empty"):
                    load()
                artifact.unlink()
                artifact.mkdir()
                with self.assertRaisesRegex(ValueError, "regular"):
                    load()
                artifact.rmdir()
                target = directory / "payload"
                target.write_bytes(b"rbf")
                artifact.symlink_to(target)
                with self.assertRaisesRegex(ValueError, "symlink"):
                    load()
                artifact.unlink()
                artifact.write_bytes(b"rbf")
                if system == "pong":
                    with self.assertRaisesRegex(ValueError, "requires.*revision"):
                        module.load(directory, system="pong")
                    with self.assertRaisesRegex(ValueError, "revision"):
                        module.load(directory, system="pong", expected_revision="9" * 40)
                else:
                    with self.assertRaisesRegex(ValueError, "revision"):
                        module.load(directory, system=system, expected_revision="9" * 40)
                manifest_path.unlink()
                target.write_text("irrelevant")
                manifest_path.symlink_to(target)
                with self.assertRaisesRegex(ValueError, "symlink"):
                    load()

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
                 patch.object(module, '_build_fes_pong', side_effect=AssertionError('unexpected build')):
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
                              side_effect=lambda _, package, __: dict(
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
            def build_once(_):
                store.mkdir(parents=True)
                (store / f'{identity}.build-inputs.json').write_bytes(record)
                (store / f'{identity}.build-inputs.json').chmod(0o444)
                (store / identity).mkdir()
                (store / identity / 'manifest.toml').write_bytes(b'manifest')
                (store / identity / 'core.rbf').write_bytes(b'payload')
            with patch.object(module, 'canonical_package_record', return_value=record), \
                 patch.object(module, '_inspect_package_candidate', return_value=inspected), \
                 patch.object(module, '_build_fes_pong', side_effect=build_once) as build_package:
                module.resolve_core_package(source, 'd' * 40, source / 'selection.toml')
            build_package.assert_called_once_with(source)

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
        self.assertIn('d7b0a66345b1b2a6a9d4e0577afd1708a055eb17', staged)
        module = self.module()
        self.assertEqual(module.TOOLCHAIN_CACHE_ROOT, root / 'out/cache/misteross-toolchains')
        docs = (root / 'docs/core-packages.md').read_text()
        self.assertIn('FES_TOOLCHAIN_CACHE_ROOT="$PWD/out/cache/misteross-toolchains"', docs)
        self.assertIn('make -C "out/work/misteross-$revision" toolchain', docs)
        self.assertIn('make -C "out/work/misteross-$revision" doctor-strict', docs)

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
            self.assertEqual(env['FES_TOOLCHAIN_CACHE_ROOT'], str(module.TOOLCHAIN_CACHE_ROOT))
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
                self.assertEqual(env['FES_TOOLCHAIN_CACHE_ROOT'], str(module.TOOLCHAIN_CACHE_ROOT))
                self.assertNotIn('MAKEFLAGS', env)
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', caller)

    def test_build_fes_pong_opts_into_shared_toolchain_cache_without_mutating_environ(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            completed = subprocess.CompletedProcess(['python'], 0)
            sentinel = os.environ.get('FES_TOOLCHAIN_CACHE_ROOT')
            caller = {'KEEP': '1'}
            with patch.object(module.subprocess, 'run', return_value=completed) as run:
                module._build_fes_pong(source, env=caller)
            env = run.call_args.kwargs['env']
            self.assertEqual(env['KEEP'], '1')
            self.assertEqual(env['FES_TOOLCHAIN_CACHE_ROOT'], str(module.TOOLCHAIN_CACHE_ROOT))
            self.assertNotIn('FES_TOOLCHAIN_CACHE_ROOT', caller)
            self.assertEqual(os.environ.get('FES_TOOLCHAIN_CACHE_ROOT'), sentinel)
            with patch.object(module.subprocess, 'run', return_value=completed) as run:
                module._build_fes_pong(source)
            self.assertEqual(run.call_args.kwargs['env']['FES_TOOLCHAIN_CACHE_ROOT'],
                             str(module.TOOLCHAIN_CACHE_ROOT))

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
                self.assertEqual(env.get('FES_TOOLCHAIN_CACHE_ROOT'),
                                 str(module.TOOLCHAIN_CACHE_ROOT))
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
