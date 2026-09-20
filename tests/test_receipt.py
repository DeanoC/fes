import importlib.util
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"

class ReceiptTest(unittest.TestCase):
    def make_verified_output(self, build, output):
        image = b"cold image"
        qemu_log = b"qemu passed\n"
        image_sha256 = hashlib.sha256(image).hexdigest()
        qemu_log_sha256 = hashlib.sha256(qemu_log).hexdigest()
        (output / "linux.img").write_bytes(image)
        (output / "qemu-smoke.log").write_bytes(qemu_log)
        (output / "reproducibility.txt").write_text(
            f"run_1_sha256={image_sha256}\nrun_2_sha256={image_sha256}\n")
        (output / "verification.json").write_text(json.dumps({
            "image_sha256": image_sha256,
            "qemu_log_sha256": qemu_log_sha256,
            "qemu_packaging": "pass",
            "structural": "pass",
            "two_pass_reproducibility": "pass",
        }))
        build.write_receipt(output, "image", "cold-fingerprint",
                            ["linux.img", "reproducibility.txt"])
        return image_sha256, qemu_log_sha256

    def test_host_sidecar_is_bound_and_diagnostic_rejected_by_cold_reader(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            for name in ('fogcast', 'fogcast-api'):
                (output / name).write_bytes(name.encode())
            for diagnostic in (False, True):
                metadata = {'sources': {'FogCast': 'a' * 40}}
                if diagnostic:
                    metadata['development_snapshot'] = {'classification': 'development-only'}
                fingerprint = hashlib.sha256(json.dumps(metadata, sort_keys=True).encode()).hexdigest()
                with mock.patch.object(build, 'git', return_value='a' * 40):
                    build.write_receipt(output, 'host', fingerprint, ['fogcast', 'fogcast-api'], os_name='linux', arch='amd64')
                receipt = (output / 'host.json').read_bytes()
                build.publish_host_inputs(output, fingerprint, metadata)
                self.assertEqual((output / 'host.json').read_bytes(), receipt)
                self.assertEqual(build.load_host_inputs(output, fingerprint), metadata)
                self.assertEqual(build.digest(output / 'host-inputs.json'), fingerprint)
                # Reuse publishes the same preimage without changing host.json.
                build.publish_host_inputs(output, fingerprint, metadata)
                if diagnostic:
                    with self.assertRaisesRegex(ValueError, 'cold host receipt'):
                        build.load_verified_host(output, fingerprint)
                else:
                    self.assertEqual(build.load_verified_host(output, fingerprint)['fes_revision'], 'a' * 40)
                (output / 'host-inputs.json').write_text('{}')
                with self.assertRaisesRegex(ValueError, 'cold host receipt'):
                    build.load_verified_host(output, fingerprint)
            (output / 'host-inputs.json').unlink()
            self.assertIsNone(build.load_host_inputs(output, fingerprint))
            # Historical ordinary receipt compatibility is retained without a sidecar.
            fingerprint = hashlib.sha256(json.dumps({'sources': {'FogCast': 'a' * 40}}, sort_keys=True).encode()).hexdigest()
            with mock.patch.object(build, 'git', return_value='a' * 40):
                build.write_receipt(output, 'host', fingerprint, ['fogcast', 'fogcast-api'], os_name='linux', arch='amd64')
            self.assertEqual(build.load_verified_host(output, fingerprint)['fes_revision'], 'a' * 40)

    def test_host_sidecar_publication_rejects_wrong_fingerprint(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temporary:
            with self.assertRaisesRegex(ValueError, 'fingerprint'):
                build.publish_host_inputs(Path(temporary), '0' * 64, {'sources': {}})
            self.assertEqual(list(Path(temporary).iterdir()), [])

    def test_media_sources_do_not_invalidate_cold_build_fingerprint(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        revisions = {"FogCast": "a" * 40}
        profile = {"version": "test"}
        before, _ = build.build_fingerprint(revisions, profile, "go test")
        with mock.patch.object(build, "MEDIA_RECIPE_FILES", (Path("changed-media.py"),)):
            after, _ = build.build_fingerprint(revisions, profile, "go test")
        self.assertEqual(before, after)

    def test_host_fingerprint_excludes_unrelated_system_inputs_but_binds_host_inputs(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        revisions = {'FogCast': 'a' * 40, 'libmister-runtime': 'b' * 40,
                     'misteross': 'c' * 40, 'mister-packages': 'd' * 40}
        profile = {'host_os': 'linux', 'host_arch': 'amd64', 'version': '1',
                   'host_build_options': {'tags': ['native']}}
        with mock.patch.object(build, 'recipe_fingerprint', return_value={'host': 'recipe'}):
            before, info = build.host_fingerprint(revisions, profile, 'go1')
            changed_system, _ = build.host_fingerprint(
                dict(revisions, **{'libmister-runtime': 'e' * 40,
                                   'misteross': 'f' * 40, 'mister-packages': '0' * 40}),
                profile, 'go1')
        self.assertEqual(before, changed_system)
        self.assertEqual({path.relative_to(build.ROOT).as_posix() for path in build.HOST_RECIPE_FILES},
                         {'scripts/build.py', 'scripts/environment.py'})
        self.assertEqual(info['sources'], {'FogCast': 'a' * 40})
        self.assertEqual(info['profile'], profile)

        def key(revision='a' * 40, toolchain='go1', selected_profile=None,
                recipe='recipe'):
            selected_profile = selected_profile or profile
            with mock.patch.object(build, 'recipe_fingerprint', return_value={'host': recipe}):
                return build.host_fingerprint(
                    dict(revisions, FogCast=revision), selected_profile, toolchain)[0]

        self.assertNotEqual(before, key(revision='1' * 40))
        self.assertNotEqual(before, key(toolchain='go2'))
        self.assertNotEqual(before, key(selected_profile=dict(profile, host_os='darwin')))
        self.assertNotEqual(before, key(selected_profile=dict(profile, host_os='darwin', host_arch='arm64')))
        self.assertNotEqual(before, key(selected_profile=dict(profile, version='2')))
        self.assertNotEqual(before, key(selected_profile=dict(profile, host_build_options={'tags': ['debug']})))
        self.assertNotEqual(before, key(recipe='changed-recipe'))

    def test_reuse_status_explains_hits_and_fail_closed_misses(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            (output / 'linux.img').write_bytes(b'image')
            build.write_receipt(output, 'image', 'inputs-a', ['linux.img'])
            self.assertEqual(build.reuse_status(output, 'image', 'inputs-a'),
                             (True, 'verified receipt and output digests match selected inputs'))
            self.assertEqual(build.reuse_status(output, 'image', 'inputs-b'),
                             (False, 'selected inputs changed'))
            self.assertTrue(build.reusable(output, 'image', 'inputs-a'))

            cases = (
                ('receipt missing', lambda: (output / 'image.json').unlink()),
                ('receipt malformed', lambda: (output / 'image.json').write_text('{')),
                ('receipt has no outputs', lambda: (output / 'image.json').write_text(
                    json.dumps({'inputs': 'inputs-a', 'files': {}, 'fes_revision': 'a' * 40}))),
                ('output missing or digest changed', lambda: (output / 'linux.img').unlink()),
            )
            for reason, mutate in cases:
                with self.subTest(reason=reason):
                    (output / 'linux.img').write_bytes(b'image')
                    build.write_receipt(output, 'image', 'inputs-a', ['linux.img'])
                    mutate()
                    self.assertEqual(build.reuse_status(output, 'image', 'inputs-a'),
                                     (False, reason))
                    self.assertFalse(build.reusable(output, 'image', 'inputs-a'))

    def test_diagnostic_sidecar_changes_do_not_invalidate_receipt(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        from build_diagnostics import BuildDiagnostics
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            (output / 'linux.img').write_bytes(b'image')
            build.write_receipt(output, 'image', 'inputs-a', ['linux.img'])
            report = BuildDiagnostics(output, 'image')
            report.finish('success')
            report.path.write_text('{"status":"updated"}\n')
            self.assertTrue(build.reusable(output, 'image', 'inputs-a'))
            recipe_names = {str(path.relative_to(build.ROOT)) for path in build.BUILD_RECIPE_FILES}
            self.assertNotIn('scripts/build_diagnostics.py', recipe_names)

    def test_declarative_recipe_registry_is_fingerprinted(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        from recipes import REGISTRY_PATH
        self.assertIn(REGISTRY_PATH, build.BUILD_RECIPE_FILES)
        self.assertEqual(build.recipe_fingerprint([REGISTRY_PATH]),
                         {'config/core-recipes.toml': build.digest(REGISTRY_PATH)})

    def test_verified_image_rejects_missing_or_mismatched_evidence(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            (output / "linux.img").write_bytes(b"cold image")
            (output / "reproducibility.txt").write_text("run_1_sha256=wrong\nrun_2_sha256=wrong\n")
            build.write_receipt(output, "image", "cold-fingerprint",
                                ["linux.img", "reproducibility.txt"])
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_verified_image_rejects_receipt_without_linux_image(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            self.make_verified_output(build, output)
            build.write_receipt(output, "image", "cold-fingerprint", ["reproducibility.txt"])
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_verified_image_rejects_missing_qemu_log(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            self.make_verified_output(build, output)
            (output / "qemu-smoke.log").unlink()
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_verified_image_rejects_mismatched_qemu_log_digest(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            self.make_verified_output(build, output)
            (output / "qemu-smoke.log").write_bytes(b"qemu changed\n")
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_image_receipt_requires_canonical_artifact_revision(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            self.make_verified_output(build, output)
            path = output / 'image.json'
            original = json.loads(path.read_text())
            for revision in (None, '', 'a' * 39, 'A' * 40, 12):
                with self.subTest(revision=revision):
                    receipt = dict(original)
                    receipt.pop('fes_revision', None)
                    if revision is not None:
                        receipt['fes_revision'] = revision
                    path.write_text(json.dumps(receipt))
                    with self.assertRaises(ValueError):
                        build.load_verified_image(output, 'cold-fingerprint')
                    self.assertFalse(build.reusable(output, 'image', 'cold-fingerprint'))

    def test_verified_image_returns_bound_evidence_digests(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            image_sha256, qemu_log_sha256 = self.make_verified_output(build, output)
            self.assertEqual(build.load_verified_image(output, "cold-fingerprint"), {
                "fes_revision": json.loads((output / "image.json").read_text())["fes_revision"],
                "rootfs_sha256": image_sha256,
                "image_receipt_sha256": hashlib.sha256((output / "image.json").read_bytes()).hexdigest(),
                "verification_sha256": hashlib.sha256((output / "verification.json").read_bytes()).hexdigest(),
                "qemu_log_sha256": qemu_log_sha256,
            })

    def test_verified_image_accepts_derived_package_fingerprint_from_bound_inputs(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            image_sha256, _ = self.make_verified_output(build, output)
            package_inputs = {'selection': {'package_id': 'a' * 64},
                              'selection_sha256': 'b' * 64,
                              'manifest_sha256': 'c' * 64,
                              'core_rbf_sha256': 'd' * 64}
            derived, info = build.image_fingerprint('base-fingerprint', {'sources': {}},
                {'inputs': package_inputs})
            (output / 'inputs.json').write_text(json.dumps(info, sort_keys=True))
            build.write_receipt(output, 'image', derived,
                                ['linux.img', 'reproducibility.txt', 'inputs.json'])
            self.assertEqual(build.load_verified_image(output, 'base-fingerprint')['rootfs_sha256'],
                             image_sha256)
            data = json.loads((output / 'inputs.json').read_text())
            data['fpga_packages'][0]['selection_sha256'] = 'e' * 64
            (output / 'inputs.json').write_text(json.dumps(data, sort_keys=True))
            with self.assertRaisesRegex(ValueError, 'stale'):
                build.load_verified_image(output, 'base-fingerprint')

    def test_verified_image_accepts_and_binds_an_ordered_package_set(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            image_sha256, _ = self.make_verified_output(build, output)
            packages = tuple({
                'inputs': {
                    'selection': {'core_id': core_id, 'package_id': package_id},
                    'selection_sha256': selection_sha,
                    'manifest_sha256': manifest_sha,
                    'core_rbf_sha256': payload_sha,
                }
            } for core_id, package_id, selection_sha, manifest_sha, payload_sha in (
                ('fes.pong', 'a' * 64, '1' * 64, '2' * 64, '3' * 64),
                ('fes.zx81', 'b' * 64, '4' * 64, '5' * 64, '6' * 64)))
            derived, info = build.image_fingerprint(
                'base-fingerprint', {'sources': {}}, packages)
            (output / 'inputs.json').write_text(json.dumps(info, sort_keys=True))
            build.write_receipt(output, 'image', derived,
                                ['linux.img', 'reproducibility.txt', 'inputs.json'])
            self.assertEqual(build.recorded_image_fingerprint(info), derived)
            self.assertEqual(build.load_verified_image(output, 'base-fingerprint')['rootfs_sha256'],
                             image_sha256)
            reversed_info = dict(info, fpga_packages=list(reversed(info['fpga_packages'])))
            with self.assertRaisesRegex(ValueError, 'differs'):
                build.recorded_image_fingerprint(reversed_info)
            duplicate_info = dict(info, fpga_packages=[
                info['fpga_packages'][0], info['fpga_packages'][0]])
            with self.assertRaisesRegex(ValueError, 'duplicate'):
                build.recorded_image_fingerprint(duplicate_info)

    def test_host_only_refresh_preserves_package_bound_image_inputs_and_media_lookup(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            image_sha256, _ = self.make_verified_output(build, output)
            package_inputs = {'selection': {'package_id': 'a' * 64},
                              'selection_sha256': 'b' * 64,
                              'manifest_sha256': 'c' * 64,
                              'core_rbf_sha256': 'd' * 64}
            derived, image_info = build.image_fingerprint('base-fingerprint',
                {'sources': {}}, {'inputs': package_inputs})
            (output / 'inputs.json').write_text(json.dumps(image_info, sort_keys=True))
            build.write_receipt(output, 'image', derived,
                                ['linux.img', 'reproducibility.txt', 'inputs.json'])

            build.publish_action_inputs(output, 'host', {'sources': {}, 'profile': {}})

            self.assertEqual(json.loads((output / 'inputs.json').read_text()), image_info)
            self.assertEqual(build.load_verified_image(output, 'base-fingerprint')['rootfs_sha256'],
                             image_sha256)

    def test_republish_read_only_selection(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / "selection"
            dest = root / "published"
            source.write_bytes(b"first")
            source.chmod(0o444)
            build.publish_file(source, dest)
            source.unlink()
            source.write_bytes(b"second")
            source.chmod(0o444)
            build.publish_file(source, dest)
            self.assertEqual(dest.read_bytes(), b"second")
            self.assertEqual(dest.stat().st_mode & 0o777, 0o444)

    def test_changed_inputs_and_corrupt_outputs_are_not_reused(self):
        self.assertTrue((SCRIPTS / "build.py").exists(), "build receipt is not implemented")
        sys.path.insert(0, str(SCRIPTS))
        spec = importlib.util.spec_from_file_location("parent_build", SCRIPTS / "build.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "linux.img").write_bytes(b"image")
            module.write_receipt(root, "image", "inputs-a", ["linux.img"])
            self.assertTrue(module.reusable(root, "image", "inputs-a"))
            self.assertFalse(module.reusable(root, "image", "inputs-b"))
            (root / "linux.img").write_bytes(b"corrupt")
            self.assertFalse(module.reusable(root, "image", "inputs-a"))
            (root / "linux.img").unlink()
            self.assertFalse(module.reusable(root, "image", "inputs-a"))
