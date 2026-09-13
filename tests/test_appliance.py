"""Release validation and real pinned-tool bootstrap reproducibility tests."""
import dataclasses
import contextlib
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest
from unittest import mock

from scripts import appliance, appliance_inside

ROOT = Path(__file__).resolve().parents[1]
INSIDE = os.environ.get('FES_APPLIANCE_TEST_INSIDE') == '1'


def arm_elf():
    data = bytearray(256)
    data[:16] = b'\x7fELF\x01\x01\x01' + bytes(9)
    struct.pack_into('<HHIIIIIHHHHHH', data, 16, 2, 40, 1, 0x10054, 52, 0, 0x05000000, 52, 32, 1, 0, 0, 0)
    struct.pack_into('<IIIIIIII', data, 52, 1, 0, 0x10000, 0x10000, len(data), len(data), 5, 4096)
    return data


def provenance(rootfs, kernel):
    return appliance.ReleaseProvenance('a'*40, 'b'*40, 'c'*40, appliance.digest(rootfs), appliance.digest(kernel), 'd'*64, 'e'*64, 'f'*64)


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.kernel = self.root/'kernel'; self.kernel.write_bytes(b'locked-kernel')
        self.rootfs = self.root/'system.img'
        data = bytearray(4096)
        struct.pack_into('<I', data, 1028, 4)
        struct.pack_into('<H', data, 1080, 0xef53)
        struct.pack_into('<I', data, 1120, 0x40)
        self.rootfs.write_bytes(data)

    def test_export_binds_inputs_and_is_immutable(self):
        result = appliance.export_release(self.root/'release', self.rootfs, self.kernel, version='v1', provenance=provenance(self.rootfs, self.kernel))
        manifest = appliance.load_manifest(result.manifest)
        self.assertEqual(manifest['image_sha256'], appliance.digest(self.rootfs))
        self.assertEqual(manifest['image_size'], 4096)
        self.assertEqual(result.image.read_bytes(), self.rootfs.read_bytes())
        inode = result.image.stat().st_ino
        again = appliance.export_release(self.root/'release', self.rootfs, self.kernel, version='v1', provenance=provenance(self.rootfs, self.kernel))
        self.assertEqual(again, result)
        self.assertEqual(result.image.stat().st_ino, inode)
        with self.assertRaises(ValueError):
            appliance.export_release(self.root/'release', self.rootfs, self.kernel, version='v2', provenance=provenance(self.rootfs, self.kernel))

    def test_reject_mismatched_provenance_and_unknown_manifest(self):
        p = dataclasses.replace(provenance(self.rootfs,self.kernel),rootfs_sha256='0'*64)
        with self.assertRaisesRegex(ValueError,'rootfs'):
            appliance.export_release(self.root/'bad',self.rootfs,self.kernel,version='v1',provenance=p)
        self.assertFalse((self.root/'bad').exists())
        result=appliance.export_release(self.root/'release',self.rootfs,self.kernel,version='v1',provenance=provenance(self.rootfs,self.kernel))
        manifest=json.loads(result.manifest.read_text());manifest['unknown']=1
        malformed=self.root/'malformed';malformed.write_text(json.dumps(manifest))
        with self.assertRaises(ValueError):appliance.load_manifest(malformed)
        malformed.write_text(result.manifest.read_text().rstrip()[:-1]+',"format":1}')
        with self.assertRaises(ValueError):appliance.load_manifest(malformed)

    def test_arm_static_validation_rejects_dynamic_and_wrong_machine(self):
        binary=self.root/'fes-boot';binary.write_bytes(arm_elf());appliance.validate_static_arm(binary)
        for offset,value in ((18,62),(52,3),(52,2)):
            data=arm_elf();struct.pack_into('<H' if offset==18 else '<I',data,offset,value);binary.write_bytes(data)
            with self.assertRaises(ValueError):appliance.validate_static_arm(binary)

    def test_reject_existing_unsealed_or_open_ended_bundle(self):
        for mutation in ('directory','file','extra'):
            with self.subTest(mutation=mutation):
                result=appliance.export_release(self.root/mutation,self.rootfs,self.kernel,version='v1',provenance=provenance(self.rootfs,self.kernel))
                if mutation=='directory':result.directory.chmod(0o755)
                elif mutation=='file':result.image.chmod(0o644)
                else:
                    result.directory.chmod(0o755)
                    (result.directory/'extra').write_text('unexpected')
                    result.directory.chmod(0o555)
                with self.assertRaisesRegex(ValueError,'sealed|entries'):
                    appliance.export_release(result.directory,self.rootfs,self.kernel,version='v1',provenance=provenance(self.rootfs,self.kernel))

    def test_cli_bootstrap_uses_validated_factory_snapshot(self):
        import sys
        sys.path.insert(0,str(ROOT/'scripts'))
        import media
        result=appliance.export_release(self.root/'release',self.rootfs,self.kernel,version='v1',provenance=provenance(self.rootfs,self.kernel))
        original=result.manifest.read_bytes()
        mutated=json.loads(original);mutated['image_sha256']='9'*64
        p=provenance(self.rootfs,self.kernel)
        def build(command,**kwargs):
            Path(command[command.index('-o')+1]).write_bytes(arm_elf())
            result.manifest.chmod(0o644);result.manifest.write_bytes(appliance.canonical(mutated))
        def assemble(output,binary,factory,kernel,**kwargs):
            self.assertEqual(Path(factory).read_bytes(),original,'CLI reopened mutable factory after verification')
            self.assertTrue(Path(output).is_absolute())
            return appliance.BootstrapResult(Path(output))
        with mock.patch.object(sys,'argv',['appliance.py','bootstrap','--release',str(result.directory),'--output','out/test-bootstrap']), \
             mock.patch.object(media,'operation',return_value=contextlib.nullcontext()), \
             mock.patch.object(appliance,'verified_inputs',return_value=(self.rootfs,self.kernel,p,self.root,{},None)), \
             mock.patch.object(appliance,'cached_media_container',return_value='sha256:'+'8'*64), \
             mock.patch.object(media.cold_build,'git',return_value='7'*40), \
             mock.patch.object(appliance.subprocess,'check_output',return_value='/cached/go\n'), \
             mock.patch.object(appliance.subprocess,'run',side_effect=build), \
             mock.patch.object(appliance,'assemble_bootstrap',side_effect=assemble), \
             mock.patch('builtins.print'):
            appliance.main()

    def test_cli_rejects_bootstrap_output_outside_checkout(self):
        import sys
        with mock.patch.object(sys,'argv',['appliance.py','bootstrap','--release',str(self.root),'--output',str(self.root/'outside')]), \
             mock.patch.object(appliance,'verified_inputs') as inputs, \
             mock.patch.object(appliance.argparse.ArgumentParser,'exit',side_effect=ValueError) as exit:
            with self.assertRaises(ValueError):appliance.main()
        self.assertIn('inside',str(exit.call_args))
        inputs.assert_not_called()

    def test_bootstrap_identity_changes_with_assembly_inputs(self):
        factory=appliance.release_manifest(self.rootfs,version='v1',provenance=provenance(self.rootfs,self.kernel))
        recipe={'scripts/appliance_inside.py':'1'*64}
        identity=appliance.bootstrap_identity('2'*64,factory,'sha256:'+'3'*64,'4'*40,recipe)
        self.assertNotEqual(identity,appliance.bootstrap_identity('2'*64,factory,'sha256:'+'5'*64,'4'*40,recipe))
        self.assertNotEqual(identity,appliance.bootstrap_identity('2'*64,factory,'sha256:'+'3'*64,'4'*40,{'scripts/appliance_inside.py':'6'*64}))
        self.assertNotEqual(identity,appliance.bootstrap_identity('2'*64,factory,'sha256:'+'3'*64,'7'*40,recipe))


@unittest.skipIf(INSIDE,'host container driver')
class ContainerTests(unittest.TestCase):
    def test_real_pinned_bootstrap_suite(self):
        if not shutil.which('docker') or subprocess.run(['docker','info'],capture_output=True).returncode:
            self.skipTest('Docker unavailable')
        from scripts.media_inputs import MediaLock
        image=appliance.cached_media_container(ROOT,'docker',MediaLock.load(ROOT/'boot-media.lock.toml'))
        subprocess.run(['docker','run','--rm','--network=none','--cap-drop=ALL','--security-opt=no-new-privileges','-e','FES_APPLIANCE_TEST_INSIDE=1','-v',f'{ROOT}:/work:ro','--entrypoint','python3',image,'-m','unittest','tests.test_appliance.RealBootstrapTests','-v'],check=True)


@unittest.skipUnless(INSIDE,'executed in pinned media container')
class RealBootstrapTests(unittest.TestCase):
    def test_two_pass_contents_devices_timestamps_and_features(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);binary=root/'fes-boot';binary.write_bytes(arm_elf())
            factory=root/'factory.json'
            factory.write_text(json.dumps({'format':1,'board':'de10-nano','boot_abi':'fes-bootstrap-v1','version':'v1','kernel_sha256':'a'*64,'image_sha256':'b'*64,'image_size':4096,'fes_revision':'c'*40,'fogcast_revision':'d'*40,'runtime_revision':'e'*40}))
            first=appliance_inside.assemble(root/'first.img',binary,factory)
            # Delay source inode ctime to catch unpatched mkfs -d copying it.
            import time
            time.sleep(1.1)
            binary.chmod(0o700)
            second=appliance_inside.assemble(root/'second.img',binary,factory)
            self.assertEqual(first['image_sha256'],second['image_sha256'])
            for path in ('/sbin/init','/etc/fes/factory.json','/dev/console','/dev/null','/.fes-bootstrap','/run','/tmp','/proc','/sys','/media/fat'):
                text=appliance_inside.debugfs(root/'first.img',f'stat {path}')
                self.assertIn('Inode:',text,path)
                self.assertIn('User:     0',text,path)
            self.assertIn('character special',appliance_inside.debugfs(root/'first.img','stat /dev/console'))
            extracted=root/'extracted'
            appliance_inside.debugfs(root/'first.img',f'dump /sbin/init {extracted}')
            self.assertEqual(extracted.read_bytes(),binary.read_bytes())
            appliance.validate_ext4(root/'first.img')

    def test_outer_wrapper_binds_recipe_and_rejects_unsealed_bootstrap(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            (root/'scripts').mkdir()
            for name in appliance.BOOTSTRAP_RECIPE_FILES:shutil.copyfile(ROOT/name,root/name)
            binary=root/'fes-boot';binary.write_bytes(arm_elf())
            kernel=root/'kernel';kernel.write_bytes(b'locked kernel')
            factory=root/'factory.json'
            factory.write_bytes(appliance.canonical({'format':1,'board':'de10-nano','boot_abi':'fes-bootstrap-v1','version':'v1',
                'kernel_sha256':appliance.digest(kernel),'image_sha256':'b'*64,'image_size':4096,
                'fes_revision':'c'*40,'fogcast_revision':'d'*40,'runtime_revision':'e'*40}))
            class LocalRunner:
                container='sha256:'+'8'*64
                def __init__(self):self.root=root;self.calls=0;self.alter_recipe=False
                def path(self,path):
                    Path(path).relative_to(root)
                    return str(path)
                def disk(self,command):
                    args=dict(zip(command[3::2],command[4::2]))
                    result=json.dumps(appliance_inside.assemble(args['--output'],args['--binary'],args['--factory']))
                    self.calls+=1
                    if self.alter_recipe and self.calls==1:
                        path=root/'scripts/appliance_inside.py';path.write_bytes(path.read_bytes()+b'\n')
                    return result
            runner=LocalRunner()
            result=appliance.assemble_bootstrap(root/'bound',binary,factory,kernel,runner=runner,
                binary_source_revision='d'*40,assembly_revision='c'*40)
            evidence=json.loads(result.evidence.read_bytes())
            self.assertEqual(evidence['assembly_revision'],'c'*40)
            self.assertEqual(evidence['assembly_recipe']['scripts/appliance_inside.py'],appliance.digest(ROOT/'scripts/appliance_inside.py'))
            self.assertEqual(evidence['classification'],'source-bound-host-artifact')
            self.assertEqual(result.directory.stat().st_mode&0o777,0o555)
            self.assertEqual(result.image.stat().st_mode&0o777,0o444)
            result.image.chmod(0o644)
            with self.assertRaisesRegex(ValueError,'sealed'):
                appliance.assemble_bootstrap(root/'bound',binary,factory,kernel,runner=runner,
                    binary_source_revision='d'*40,assembly_revision='c'*40)
            diagnostic=appliance.assemble_bootstrap(root/'diagnostic',binary,factory,kernel,runner=runner)
            evidence=json.loads(diagnostic.evidence.read_bytes())
            self.assertFalse(evidence['binary_source_proven'])
            self.assertFalse(evidence['assembly_source_proven'])
            self.assertEqual(evidence['classification'],'diagnostic-unproven-source')
            runner=LocalRunner();runner.alter_recipe=True
            with self.assertRaisesRegex(ValueError,'recipe changed'):
                appliance.assemble_bootstrap(root/'changed',binary,factory,kernel,runner=runner)
            self.assertFalse((root/'changed').exists())

class VerificationBoundaryTests(unittest.TestCase):
    def test_cli_input_boundary_rejects_missing_cold_verification(self):
        import sys
        from unittest import mock
        sys.path.insert(0,str(ROOT/'scripts'))
        import media
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            with mock.patch.object(media.cold_build,'git',return_value=''), mock.patch.object(media,'select',return_value=('image-fingerprint','host-fingerprint',root/'fogcast',[],{})):
                with self.assertRaisesRegex(ValueError,'cold image receipt'):
                    appliance.verified_inputs(root,'native-integration-dev')

    def test_ext4_rejects_new_kernel_features_and_symlink(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);path=root/'image';data=bytearray(4096)
            struct.pack_into('<I',data,1028,4);struct.pack_into('<H',data,1080,0xef53)
            struct.pack_into('<I',data,1120,0x40);path.write_bytes(data)
            appliance.validate_ext4(path)
            struct.pack_into('<I',data,1116,0x1000);path.write_bytes(data)
            with self.assertRaises(ValueError):appliance.validate_ext4(path)
            link=root/'link';link.symlink_to(path)
            with self.assertRaises(ValueError):appliance.validate_ext4(link)
