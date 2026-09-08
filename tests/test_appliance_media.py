"""Appliance card checks use real locked FAT/ext4 tools and full-byte comparison."""
import dataclasses
import contextlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from scripts import appliance, appliance_inside, appliance_media, appliance_media_inside
from scripts.media_inputs import MediaLock, Payload
from tests.test_appliance import arm_elf

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'scripts'))
import media
INSIDE=os.environ.get('FES_APPLIANCE_MEDIA_TEST_INSIDE')=='1'


class LayoutTests(unittest.TestCase):
    def test_derivative_geometry_preserves_locked_boot_inputs(self):
        original=MediaLock.load(ROOT/'boot-media.lock.toml')
        layout=appliance_media_inside.appliance_layout(original)
        self.assertEqual(layout.layout,'de10-nano-appliance-1g-v1')
        self.assertEqual(layout.partition_1.start_sector,2048)
        self.assertEqual(layout.partition_1.sector_count*512,1<<30)
        self.assertEqual(layout.partition_2.start_sector,2099200)
        self.assertEqual(layout.total_sectors,2101248)
        self.assertEqual(layout.partition_2.type,0xa2)
        self.assertEqual(layout.kernel,original.kernel)
        self.assertEqual(layout.uboot,original.uboot)
        self.assertEqual(layout.environment,original.environment)
        self.assertEqual(original.partition_1.sector_count,524288)
        self.assertGreater(layout.partition_1.sector_count*512,4*(64<<20)+(32<<20)+(16<<20))

    def test_derivation_rejects_unexpected_source_layout(self):
        original=MediaLock.load(ROOT/'boot-media.lock.toml')
        for changed in (dataclasses.replace(original,total_sectors=123),
                dataclasses.replace(original,partition_1=dataclasses.replace(original.partition_1,type=0xa2))):
            with self.assertRaises(ValueError):appliance_media_inside.appliance_layout(changed)


@unittest.skipIf(INSIDE,'host container driver')
class ContainerTests(unittest.TestCase):
    def test_locked_appliance_card_suite(self):
        if not shutil.which('docker') or subprocess.run(['docker','info'],capture_output=True).returncode:
            self.skipTest('Docker unavailable')
        image=appliance.cached_media_container(ROOT,'docker',MediaLock.load(ROOT/'boot-media.lock.toml'))
        subprocess.run(['docker','run','--rm','--pull=never','--network=none','--cap-drop=ALL','--security-opt=no-new-privileges',
            '-e','FES_APPLIANCE_MEDIA_TEST_INSIDE=1','-v',f'{ROOT}:/work:ro','--entrypoint','python3',image,
            '-m','unittest','tests.test_appliance_media.RealCardTests','-v'],check=True)


@unittest.skipUnless(INSIDE,'executed in pinned media container')
class RealCardTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary=tempfile.TemporaryDirectory(prefix='appliance-card-');cls.root=Path(cls.temporary.name)
        root=cls.root;cls.idle=root/'idle.rbf';cls.idle.write_bytes(b'locked-idle'*100)
        tree=root/'tree';(tree/'usr/share/mister-runtime').mkdir(parents=True);(tree/'.fes-bootstrap').mkdir()
        shutil.copyfile(cls.idle,tree/'usr/share/mister-runtime/idle.rbf')
        (tree/'sbin').mkdir();(tree/'sbin/init').write_bytes(arm_elf());(tree/'sbin/init').chmod(0o755)
        cls.factory=root/'factory.img'
        with cls.factory.open('wb') as stream:stream.truncate(8<<20)
        subprocess.run(['mke2fs','-q','-F','-t','ext4','-d',str(tree),str(cls.factory)],check=True)
        cls.kernel=root/'kernel';cls.kernel.write_bytes(b'kernel'*100)
        original=MediaLock.load(ROOT/'boot-media.lock.toml');cls.uboot=root/'uboot'
        cls.uboot.write_bytes(b'uboot'+b'\0'.join(value.encode() for value in original.environment))
        cls.lock=dataclasses.replace(original,kernel=Payload('zImage_dtb',cls.kernel.stat().st_size,appliance.digest(cls.kernel)),
            uboot=Payload('uboot.img',cls.uboot.stat().st_size,appliance.digest(cls.uboot)))
        manifest={'format':1,'board':'de10-nano','boot_abi':'fes-bootstrap-v1','version':'test','kernel_sha256':appliance.digest(cls.kernel),
            'image_sha256':appliance.digest(cls.factory),'image_size':cls.factory.stat().st_size,'fes_revision':'a'*40,'fogcast_revision':'b'*40,'runtime_revision':'c'*40}
        cls.manifest=root/'release.json';cls.manifest.write_bytes(appliance.canonical(manifest))
        binary=root/'fes-boot';binary.write_bytes(arm_elf());cls.bootstrap=root/'bootstrap.img';appliance_inside.assemble(cls.bootstrap,binary,cls.manifest)
        cls.agent=root/'agent.toml';cls.agent.write_text('token="private-agent-token"\n');cls.agent.chmod(0o600)
        cls.launcher=root/'launcher.json';cls.launcher.write_text('{"token":"private-launcher-token"}\n');cls.launcher.chmod(0o600)
        cls.inputs=appliance_media_inside.Inputs(cls.factory,cls.bootstrap,cls.manifest,cls.idle,cls.kernel,cls.uboot,cls.agent,cls.launcher)

    @classmethod
    def tearDownClass(cls):cls.temporary.cleanup()

    def test_two_pass_and_exact_owned_payloads(self):
        with tempfile.TemporaryDirectory(dir=self.root) as temporary:
            root=Path(temporary)
            first=appliance_media_inside.assemble(root/'first.img',self.inputs,self.lock)
            second=appliance_media_inside.assemble(root/'second.img',self.inputs,self.lock)
            self.assertEqual(first,second)
            self.assertEqual(first['layout_id'],'de10-nano-appliance-1g-v1')
            self.assertEqual(first['geometry']['partition_1']['sector_count'],2097152)
            self.assertEqual((root/'first.img').stat().st_size,2101248*512)
            with (root/'first.img').open('rb') as stream:
                mbr=stream.read(512)
                self.assertEqual(struct.unpack_from('<II',mbr,454),(2048,2097152))
                self.assertEqual(struct.unpack_from('<II',mbr,470),(2099200,2048))
                stream.seek(2099200*512)
                self.assertEqual(stream.read(self.uboot.stat().st_size),self.uboot.read_bytes())
            expected=f'/fogcast/releases/images/{appliance.digest(self.factory)}.img'
            self.assertIn(expected,first['paths'])
            self.assertIn('/fogcast/agent.toml',first['paths'])
            self.assertEqual(first['factory_sha256'],appliance.digest(self.factory))
            self.assertNotIn('private-agent-token',json.dumps(first))
            appliance_media_inside.verify(root/'first.img',self.inputs,self.lock)
            changed=root/'changed.img';shutil.copyfile(root/'first.img',changed)
            with changed.open('r+b') as stream:stream.seek(900000);stream.write(b'x')
            with self.assertRaises(ValueError):appliance_media.compare_card(changed,root/'first.img')

    def test_outer_reconstruct_uses_two_real_assemblies_and_binds_evidence(self):
        with tempfile.TemporaryDirectory(dir=self.root) as temporary:
            root=Path(temporary)
            from scripts import media_inside
            lock_data=dataclasses.asdict(self.lock)
            lock_data['uboot']['environment']=list(lock_data.pop('environment'))
            lock_path=root/'lock.toml';media_inside.write_manifest(lock_path,lock_data)
            class LocalRunner:
                def path(self,path):return str(path)
                def disk(self,command):
                    return subprocess.check_output([*command,'--lock',str(lock_path)],text=True)
            image,evidence=appliance_media.reconstruct(root,self.inputs,{'format':1,'test_fixture':True},LocalRunner())
            record=json.loads(evidence.read_text())
            self.assertEqual(record['output']['sha256'],appliance.digest(image))
            self.assertEqual(record['assembly_sha256'],[appliance.digest(image)]*2)
            self.assertEqual(record['layout_id'],'de10-nano-appliance-1g-v1')
            self.assertEqual(record['geometry']['total_sectors'],2101248)
            published=appliance_media.publish(root/'published',image,evidence)
            appliance_media.compare_card(published.image,image)
            self.assertEqual(published.evidence.read_bytes(),evidence.read_bytes())

    def test_extra_fat_file_and_unprovisioned_outputs(self):
        with tempfile.TemporaryDirectory(dir=self.root) as temporary:
            root=Path(temporary);inputs=dataclasses.replace(self.inputs,agent_config=None,launcher_config=None)
            image=root/'card.img';result=appliance_media_inside.assemble(image,inputs,self.lock)
            self.assertNotIn('/fogcast/agent.toml',result['paths'])
            extra=root/'extra';extra.write_bytes(b'hidden data')
            from scripts import media_inside
            media_inside.run('mcopy','-i',f'{image}@@{media_inside.PART1_OFFSET}',extra,'::/unexpected')
            with self.assertRaisesRegex(ValueError,'paths'):
                appliance_media_inside.verify(image,inputs,self.lock)

class PublicationTests(unittest.TestCase):
    def test_changed_recipe_during_reconstruction_preserves_existing_artifact(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);recipe=root/'recipe.py';recipe.write_bytes(b'original recipe')
            image=root/'source.img';image.write_bytes(b'private card')
            evidence=root/'source.json';evidence.write_bytes(b'{}')
            result=appliance_media.publish(root/'published',image,evidence)
            inode=result.image.stat().st_ino
            bindings={'recipe':{'recipe.py':appliance.digest(recipe)}}
            def reconstruct(scratch,*args):
                output=scratch/'first.img';output.write_bytes(image.read_bytes())
                proof=scratch/'evidence.json';proof.write_bytes(evidence.read_bytes())
                recipe.write_bytes(b'changed recipe')
                return output,proof
            with mock.patch.object(appliance_media,'RECIPE_FILES',('recipe.py',)), \
                    mock.patch.object(appliance_media,'prepare',return_value=(None,bindings,None,None)), \
                    mock.patch.object(appliance_media,'reconstruct',side_effect=reconstruct), \
                    mock.patch.object(media,'operation',return_value=contextlib.nullcontext()):
                with self.assertRaisesRegex(ValueError,'recipe changed'):
                    appliance_media.execute(root,'build',root/'release',root/'bootstrap',result.directory)
            self.assertEqual(result.image.read_bytes(),b'private card')
            self.assertEqual(result.image.stat().st_ino,inode)

    def test_recipe_rechecked_immediately_before_publication(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);recipe=root/'recipe.py';recipe.write_bytes(b'original recipe')
            image=root/'source.img';image.write_bytes(b'private card')
            evidence=root/'source.json';evidence.write_bytes(b'{}')
            bindings={'recipe':{'recipe.py':appliance.digest(recipe)}}
            original_copy=shutil.copyfile
            def changed_copy(source,destination,*args,**kwargs):
                result=original_copy(source,destination,*args,**kwargs)
                recipe.write_bytes(b'changed while staging publication')
                return result
            with mock.patch.object(appliance_media,'RECIPE_FILES',('recipe.py',)), \
                    mock.patch.object(appliance_media,'prepare',return_value=(None,bindings,None,None)), \
                    mock.patch.object(appliance_media,'reconstruct',return_value=(image,evidence)), \
                    mock.patch.object(media,'operation',return_value=contextlib.nullcontext()), \
                    mock.patch.object(appliance_media.shutil,'copyfile',side_effect=changed_copy):
                with self.assertRaisesRegex(ValueError,'recipe changed'):
                    appliance_media.execute(root,'build',root/'release',root/'bootstrap',root/'published')
            self.assertFalse((root/'published').exists())

    def test_recipe_rechecked_after_retained_card_verification(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);recipe=root/'recipe.py';recipe.write_bytes(b'original recipe')
            image=root/'source.img';image.write_bytes(b'private card')
            evidence=root/'source.json';evidence.write_bytes(b'{}')
            result=appliance_media.publish(root/'published',image,evidence)
            bindings={'recipe':{'recipe.py':appliance.digest(recipe)}}
            original_compare=appliance_media.compare_card
            def changed_compare(actual,expected):
                original_compare(actual,expected)
                recipe.write_bytes(b'changed during retained card verification')
            with mock.patch.object(appliance_media,'RECIPE_FILES',('recipe.py',)), \
                    mock.patch.object(appliance_media,'prepare',return_value=(None,bindings,None,None)), \
                    mock.patch.object(appliance_media,'reconstruct',return_value=(image,evidence)), \
                    mock.patch.object(media,'operation',return_value=contextlib.nullcontext()), \
                    mock.patch.object(appliance_media,'compare_card',side_effect=changed_compare):
                with self.assertRaisesRegex(ValueError,'recipe changed'):
                    appliance_media.execute(root,'verify',root/'release',root/'bootstrap',result.directory)
            self.assertEqual(result.image.read_bytes(),b'private card')

    def test_private_publication_is_idempotent_and_preserves_existing(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);image=root/'source.img';image.write_bytes(b'private card payload')
            evidence=root/'source.json';evidence.write_bytes(b'{"format":1}\n')
            result=appliance_media.publish(root/'published',image,evidence)
            self.assertEqual(result.image.stat().st_mode&0o777,0o600)
            self.assertEqual(result.evidence.stat().st_mode&0o777,0o600)
            self.assertEqual(result.directory.stat().st_mode&0o777,0o700)
            inode=result.image.stat().st_ino
            self.assertEqual(appliance_media.publish(root/'published',image,evidence),result)
            self.assertEqual(result.image.stat().st_ino,inode)
            image.write_bytes(b'changed')
            with self.assertRaises(ValueError):appliance_media.publish(root/'published',image,evidence)
            self.assertEqual(result.image.read_bytes(),b'private card payload')
            self.assertEqual(result.image.stat().st_ino,inode)

    def test_existing_world_readable_card_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);image=root/'source.img';image.write_bytes(b'private card')
            evidence=root/'source.json';evidence.write_bytes(b'{}')
            result=appliance_media.publish(root/'published',image,evidence)
            result.image.chmod(0o644)
            with self.assertRaisesRegex(ValueError,'private'):
                appliance_media.publish(root/'published',image,evidence)

    def test_bundle_evidence_cannot_override_reconstruction(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);expected=root/'expected';actual=root/'actual'
            for directory in (expected,actual):
                directory.mkdir();(directory/'linux.img').write_bytes(b'image');(directory/'evidence.json').write_bytes(b'{"verified":true}')
                for file in directory.iterdir():file.chmod(0o444)
                directory.chmod(0o555)
            appliance_media.compare_bundle(actual,expected,('linux.img','evidence.json'))
            (actual/'evidence.json').chmod(0o600);(actual/'evidence.json').write_bytes(b'{"verified":true,"claimed_source":"other"}');(actual/'evidence.json').chmod(0o444)
            with self.assertRaisesRegex(ValueError,'verified inputs'):
                appliance_media.compare_bundle(actual,expected,('linux.img','evidence.json'))
