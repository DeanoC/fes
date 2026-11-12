import copy
import hashlib
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

from scripts import de25_builder as builder


class ImmutableSDKBuilderTests(unittest.TestCase):
    def setUp(self):
        self.lock, self.manifest, self.size = builder.locked_manifest()
        self.config = self.lock['builder_image']
        self.oci = self.lock['builder_manifest']
        self.image = {
            'Id': self.oci, 'Os': 'linux', 'Architecture': 'amd64',
            'Descriptor': {'digest': self.oci, 'mediaType': builder.MANIFEST_TYPE, 'size': self.size},
            'RootFS': {'Type': 'layers', 'Layers': [v['digest'] for v in self.manifest['layers']]}}

    def result(self, image=None, code=0):
        return SimpleNamespace(returncode=code, stdout=json.dumps([image or self.image]))

    def test_docker29_uses_only_the_content_bound_manifest_after_missing_config(self):
        with mock.patch.object(builder.subprocess, 'run', side_effect=[
                self.result(code=1), self.result()]) as run:
            self.assertEqual(builder.resolve('docker', self.config), self.oci)
        self.assertEqual([c.args[0] for c in run.call_args_list], [
            ['docker', 'image', 'inspect', self.config], ['docker', 'image', 'inspect', self.oci]])
        for call in run.call_args_list:
            self.assertEqual(call.kwargs, dict(capture_output=True, text=True, env=None, timeout=30))

    def test_docker26_keeps_the_same_pinned_config_execution_address(self):
        image = copy.deepcopy(self.image)
        image['Id'] = self.config
        del image['Descriptor']
        with mock.patch.object(builder.subprocess, 'run', return_value=self.result(image)) as run:
            self.assertEqual(builder.resolve('docker', self.config), self.config)
        self.assertEqual(run.call_count, 1)

    def test_substitution_and_wrong_platform_or_layers_never_try_another_address(self):
        changes = [
            lambda v: v.update(Id='sha256:' + 'a'*64),
            lambda v: v.update(Os='windows'),
            lambda v: v.update(Architecture='arm64'),
            lambda v: v.update(RootFS=None),
            lambda v: v['RootFS'].update(Type='different'),
            lambda v: v['RootFS']['Layers'].pop(),
            lambda v: v['RootFS']['Layers'].reverse(),
            lambda v: v['RootFS']['Layers'].__setitem__(0, 'sha256:' + 'a'*64),
            lambda v: v['Descriptor'].update(digest=self.config),
            lambda v: v['Descriptor'].update(mediaType='application/vnd.oci.image.index.v1+json'),
            lambda v: v['Descriptor'].update(size=self.size + 1),
            lambda v: v.update(Descriptor=None),
        ]
        for index, change in enumerate(changes):
            with self.subTest(case=index):
                image = copy.deepcopy(self.image); change(image)
                with mock.patch.object(builder.subprocess, 'run', return_value=self.result(image)) as run:
                    with self.assertRaises(ValueError):
                        builder.resolve('docker', self.config)
                self.assertEqual(run.call_count, 1)

    def test_missing_and_malformed_responses_fail_without_running_a_container(self):
        with mock.patch.object(builder.subprocess, 'run', return_value=self.result(code=1)) as run:
            with self.assertRaisesRegex(ValueError, 'unavailable or differs'):
                builder.resolve('docker', self.config)
        self.assertEqual(run.call_count, 2)
        for raw in ('not json', '{}', '[]', '[null]', '[{},{}]'):
            with self.subTest(raw=raw), mock.patch.object(builder.subprocess, 'run', return_value=SimpleNamespace(
                    returncode=0, stdout=raw)) as run:
                with self.assertRaises(ValueError):
                    builder.resolve('docker', self.config)
                self.assertEqual(run.call_count, 1)

    def test_changed_manifest_and_wrong_config_binding_fail_before_engine_access(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); lane = root / builder.EXECUTION; lane.mkdir(parents=True)
            compiler = root / builder.PIN / 'linux.lock.toml'; compiler.parent.mkdir(parents=True)
            compiler.write_text((builder.ROOT / builder.PIN / 'linux.lock.toml').read_text())
            lock = lane / 'identity.lock.toml'
            manifest = lane / 'manifest.json'
            raw = (builder.ROOT / builder.EXECUTION / 'manifest.json').read_bytes()
            lock.write_text((builder.ROOT / builder.EXECUTION / 'identity.lock.toml').read_text())
            manifest.write_bytes(raw + b'\n')
            with mock.patch.object(builder.subprocess, 'run') as run:
                with self.assertRaisesRegex(ValueError, 'manifest differs'):
                    builder.resolve('docker', self.config, root=root)
                run.assert_not_called()
            changed = copy.deepcopy(self.manifest)
            changed['config']['digest'] = 'sha256:' + 'a'*64
            raw = json.dumps(changed).encode()
            manifest.write_bytes(raw)
            lock.write_text(lock.read_text().replace(self.oci, 'sha256:' + hashlib.sha256(raw).hexdigest()))
            with mock.patch.object(builder.subprocess, 'run') as run:
                with self.assertRaisesRegex(ValueError, 'content contract differs'):
                    builder.resolve('docker', self.config, root=root)
                run.assert_not_called()

    def test_other_recorded_sdk_config_cannot_use_the_owner_sdk_manifest(self):
        other = 'sha256:' + 'a'*64
        with mock.patch.object(builder.subprocess, 'run', return_value=self.result(code=1)) as run:
            with self.assertRaises(ValueError):
                builder.resolve('docker', other)
        self.assertEqual([c.args[0] for c in run.call_args_list], [['docker', 'image', 'inspect', other]])

    def test_mutable_tags_are_not_builder_identities(self):
        for name in ('sdk:latest', 'sha256:' + 'A'*64, self.config + '\nother'):
            with self.subTest(name=name), mock.patch.object(builder.subprocess, 'run') as run:
                with self.assertRaises(ValueError):
                    builder.resolve('docker', name)
                run.assert_not_called()

    def test_workspace_accepts_only_an_immutable_image_and_named_volume(self):
        for image, volume in (('sdk:latest', 'private'), (self.oci, '/shared/path'),
                              (self.oci, 'private:/other'), (self.oci, '--volume')):
            with self.subTest(image=image, volume=volume), self.assertRaises(ValueError):
                builder.workspace_command('docker', image, volume)
        command = builder.workspace_command('docker', self.oci, 'private-sdk-volume')
        self.assertEqual(command[:9], ['docker', 'run', '--rm', '--network', 'none',
                                     '--platform', 'linux/amd64', '--user', '0:0'])
        self.assertEqual(command[9:12], ['--mount',
                         'type=volume,src=private-sdk-volume,dst=/target-image-output,volume-nocopy', self.oci])
