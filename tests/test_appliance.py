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
import io
from unittest import mock

from scripts import appliance, appliance_inside, appliance_media, platform as fes_platform

ROOT = Path(__file__).resolve().parents[1]
INSIDE = os.environ.get('FES_APPLIANCE_TEST_INSIDE') == '1'


def arm_elf():
    data = bytearray(256)
    data[:16] = b'\x7fELF\x01\x01\x01' + bytes(9)
    struct.pack_into('<HHIIIIIHHHHHH', data, 16, 2, 40, 1, 0x10054, 52, 0, 0x05000000, 52, 32, 1, 0, 0, 0)
    struct.pack_into('<IIIIIIII', data, 52, 1, 0, 0x10000, 0x10000, len(data), len(data), 5, 4096)
    return data


def aarch64_elf():
    data = bytearray(256)
    data[:16] = b'\x7fELF\x02\x01\x01' + bytes(9)
    struct.pack_into('<HHIQQQIHHHHHH', data, 16, 2, 183, 1, 0x400080, 64, 0, 0, 64, 56, 1, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', data, 64, 1, 5, 0, 0x400000, 0x400000, len(data), len(data), 4096)
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

    def test_single_pass_release_records_pass_count_without_special_version(self):
        p=dataclasses.replace(provenance(self.rootfs,self.kernel),image_passes=1)
        result=appliance.export_release(self.root/'release-1p',self.rootfs,self.kernel,version='dev.1',provenance=p)
        self.assertEqual(appliance.load_manifest(result.manifest)['version'],'dev.1')
        self.assertEqual(json.loads(result.evidence.read_text())['image_passes'],1)
        self.assertEqual(sorted(x.name for x in result.directory.iterdir()),['evidence.json','release.json','rootfs.img'])
        self.assertEqual(appliance.export_release(result.directory,self.rootfs,self.kernel,version='dev.1',provenance=p),result)
        with self.assertRaisesRegex(ValueError,'pass count differs'):
            appliance.export_release(self.root/'wrong-passes',self.rootfs,self.kernel,version='dev.1',provenance=p,image_passes=2)

    def test_two_pass_manifest_keeps_legacy_bytes_and_legacy_manifest_loads(self):
        p=provenance(self.rootfs,self.kernel)
        manifest=appliance.release_manifest(self.rootfs,version='v1',provenance=p)
        self.assertNotIn('image_passes',manifest)
        legacy=self.root/'legacy.json';legacy.write_bytes(appliance.canonical(manifest))
        self.assertEqual(appliance.load_manifest(legacy),manifest)
        explicit=dict(manifest,image_passes=2)
        legacy.write_bytes(appliance.canonical(explicit))
        self.assertEqual(appliance.load_manifest(legacy)['image_passes'],2)
        for bad in (0,-1,True,1.5,'1',1 << 63):
            with self.subTest(bad=bad):
                legacy.write_bytes(appliance.canonical(dict(manifest,image_passes=bad)))
                with self.assertRaisesRegex(ValueError,'invalid release image passes'):
                    appliance.load_manifest(legacy)

    def test_produced_manifest_matches_deployed_decode_allow_list(self):
        # mister-agent d7273fb6 DecodeManifest accepts only this closed set.
        legacy={'format','board','boot_abi','version','kernel_sha256','image_sha256','image_size','fes_revision','fogcast_revision','runtime_revision'}
        self.assertEqual(appliance.MANIFEST_FIELDS,legacy)
        p=provenance(self.rootfs,self.kernel)
        for passes,version in ((1,'hil-1p'),(2,'v1')):
            produced=appliance.release_manifest(self.rootfs,version=version,provenance=p,image_passes=passes)
            self.assertEqual(set(produced),legacy)
            self.assertNotIn(b'image_passes',appliance.canonical(produced))
        one=appliance.export_release(self.root/'legacy-1p',self.rootfs,self.kernel,version='dev.1',provenance=dataclasses.replace(p,image_passes=1))
        self.assertEqual(set(json.loads(one.manifest.read_text())),legacy)
        self.assertNotIn(b'image_passes',one.manifest.read_bytes())
        self.assertEqual(json.loads(one.evidence.read_text())['image_passes'],1)
        two=appliance.export_release(self.root/'legacy-2p',self.rootfs,self.kernel,version='v1',provenance=p)
        self.assertEqual(set(json.loads(two.manifest.read_text())),legacy)
        self.assertEqual(json.loads(two.evidence.read_text())['image_passes'],2)
        once=appliance.canonical(dict(json.loads(one.manifest.read_text()),image_passes=1))
        duplicate=once.decode().rstrip()[:-1]+',"image_passes":1}\n'
        malformed=self.root/'duplicate.json';malformed.write_text(duplicate)
        with self.assertRaisesRegex(ValueError,'duplicate JSON field'):
            appliance.load_manifest(malformed)
    def test_bootstrap_and_media_admit_single_pass_but_reject_mismatched_evidence(self):
        import sys
        import media
        p=dataclasses.replace(provenance(self.rootfs,self.kernel),image_passes=1)
        release=appliance.export_release(self.root/'single-release',self.rootfs,self.kernel,version='dev.1',provenance=p)
        inputs=(self.rootfs,self.kernel,p,self.root,{},None)
        with contextlib.ExitStack() as stack:
            stack.enter_context(mock.patch.object(sys,'argv',['appliance.py','bootstrap','--release',str(release.directory)]))
            stack.enter_context(mock.patch.object(media,'operation',return_value=contextlib.nullcontext()))
            stack.enter_context(mock.patch.object(appliance,'verified_inputs',return_value=inputs))
            stack.enter_context(mock.patch.object(appliance,'cached_media_container',side_effect=RuntimeError('reached assembly')))
            with self.assertRaisesRegex(RuntimeError,'reached assembly'):
                appliance.main()
            with self.assertRaisesRegex(RuntimeError,'reached assembly'):
                appliance_media.prepare(self.root,'native-integration-dev',release.directory,self.root/'bootstrap',self.root/'media-scratch')
        wrong=(self.rootfs,self.kernel,dataclasses.replace(p,image_passes=2),self.root,{},None)
        with mock.patch.object(appliance,'verified_inputs',return_value=wrong):
            with self.assertRaisesRegex(ValueError,'pass count differs'):
                appliance_media.prepare(self.root,'native-integration-dev',release.directory,self.root/'bootstrap',self.root/'scratch')

    def test_media_kernel_cache_runner_forwards_verified_pass_count(self):
        import sys
        sys.path.insert(0,str(ROOT/'scripts'))
        import media
        runner=object.__new__(media.Runner)
        runner.runtime='fake-runtime'
        staged=self.root/'staged';staged.mkdir()
        with mock.patch.dict(os.environ,{'IMAGE_PASSES':'1'}), mock.patch.object(runner,'create_and_start') as start:
            runner.child_verify(self.root/'image',staged,{'TARGET_IMAGE_CONTAINER_RUNTIME':'docker','FES_IMAGE_PASSES':'2'})
        self.assertEqual(start.call_args.kwargs['env']['FES_IMAGE_PASSES'],'2')

    def test_release_cli_defaults_to_one_pass_in_ci_without_special_version(self):
        import sys
        import media
        p=dataclasses.replace(provenance(self.rootfs,self.kernel),image_passes=1)
        for env in ({}, {'CI':'true'}, {'GITHUB_ACTIONS':'true'}):
            with self.subTest(env=env), contextlib.ExitStack() as stack:
                stack.enter_context(mock.patch.dict(os.environ,env,clear=True))
                stack.enter_context(mock.patch.object(sys,'argv',['appliance.py','release','--version','dev.1']))
                stack.enter_context(mock.patch.object(media,'operation',return_value=contextlib.nullcontext()))
                stack.enter_context(mock.patch.object(appliance,'verified_inputs',return_value=(self.rootfs,self.kernel,p,self.root,{},None)))
                export=stack.enter_context(mock.patch.object(appliance,'export_release'))
                stack.enter_context(mock.patch('builtins.print'))
                appliance.main()
                self.assertEqual(export.call_args.kwargs['image_passes'],1)
                self.assertEqual(export.call_args.kwargs['version'],'dev.1')

    def test_release_cli_default_directory_separates_pass_evidence(self):
        import sys
        import media
        outputs={}
        for passes in (1,2):
            p=dataclasses.replace(provenance(self.rootfs,self.kernel),image_passes=passes)
            with contextlib.ExitStack() as stack:
                stack.enter_context(mock.patch.object(sys,'argv',['appliance.py','release','--version','dev.1','--image-passes',str(passes)]))
                stack.enter_context(mock.patch.object(media,'operation',return_value=contextlib.nullcontext()))
                stack.enter_context(mock.patch.object(appliance,'verified_inputs',return_value=(self.rootfs,self.kernel,p,self.root,{},None)))
                export=stack.enter_context(mock.patch.object(appliance,'export_release'))
                stack.enter_context(mock.patch('builtins.print'))
                appliance.main()
            outputs[passes]=(export.call_args.args[0],p)
        (one,p1),(two,p2)=outputs[1],outputs[2]
        self.assertEqual(one.parent,two.parent)
        self.assertEqual(one.parent.name,appliance.digest(self.rootfs))
        self.assertEqual((one.name[-6:],two.name[-6:]),('-1pass','-2pass'))
        # Identical rootfs and manifest bytes publish both pass records side by side.
        first=appliance.export_release(self.root/one.name,self.rootfs,self.kernel,version='dev.1',provenance=p1)
        second=appliance.export_release(self.root/two.name,self.rootfs,self.kernel,version='dev.1',provenance=p2)
        self.assertEqual(first.manifest.read_bytes(),second.manifest.read_bytes())
        self.assertEqual(appliance.export_release(second.directory,self.rootfs,self.kernel,version='dev.1',provenance=p2),second)
        with self.assertRaisesRegex(ValueError,'immutable release destination differs'):
            appliance.export_release(first.directory,self.rootfs,self.kernel,version='dev.1',provenance=p2)

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

    def test_aarch64_static_validation_requires_explicit_target_and_closed_segments(self):
        binary = self.root / 'helper'; binary.write_bytes(aarch64_elf())
        self.assertEqual(appliance.validate_static_arm(binary, 'aarch64'), appliance.digest(binary))
        with self.assertRaises(ValueError):
            appliance.validate_static_arm(binary)
        for offset, layout, value in [(18, '<H', 62), (64, '<I', 2), (64, '<I', 3),
                                       (24, '<Q', 0x500000), (96, '<Q', 1024), (48, '<I', 1)]:
            data = aarch64_elf(); struct.pack_into(layout, data, offset, value); binary.write_bytes(data)
            with self.subTest(offset=offset, value=value), self.assertRaises(ValueError):
                appliance.validate_static_arm(binary, 'aarch64')
        binary.write_bytes(arm_elf())
        with self.assertRaises(ValueError):
            appliance.validate_static_arm(binary, 'aarch64')

    def test_bootstrap_source_may_differ_from_packaged_software_revision(self):
        software=provenance(self.rootfs,self.kernel)
        factory_software=dataclasses.replace(software,fogcast_revision='f'*40)
        factory=self.root/'factory.json'
        factory.write_bytes(appliance.canonical(
            appliance.release_manifest(self.rootfs,version='v1',provenance=factory_software)))
        binary=self.root/'fes-boot';binary.write_bytes(arm_elf())
        runner_root=self.root/'runner'
        for name in appliance.BOOTSTRAP_RECIPE_FILES:
            destination=runner_root/name
            destination.parent.mkdir(parents=True,exist_ok=True)
            shutil.copyfile(ROOT/name,destination)

        class LocalRunner:
            container='sha256:'+'8'*64
            root=runner_root
            def path(self,path):
                Path(path).relative_to(self.root)
                return str(path)
            def disk(self,command):
                output=Path(command[command.index('--output')+1])
                output.write_bytes(b'bootstrap image')
                return json.dumps({'image_sha256':appliance.digest(output)})

        platform_build=fes_platform.platform_build_record(
            '1'*64,'2'*64,software.fogcast_revision,'go1.26.5','3'*64)
        result=appliance.assemble_bootstrap(runner_root/'out/bootstrap',binary,factory,self.kernel,
            runner=LocalRunner(),binary_source_revision=platform_build['fogcast_revision'],
            assembly_revision='a'*40,platform_build=platform_build)
        evidence=json.loads(result.evidence.read_bytes())
        self.assertNotEqual(factory_software.fogcast_revision,platform_build['fogcast_revision'])
        self.assertEqual(evidence['binary_source_revision'],platform_build['fogcast_revision'])
        self.assertEqual(evidence['platform_build']['fogcast_revision'],platform_build['fogcast_revision'])

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
        selected_revision='9'*40
        def build_arm(root,fogcast,output,env):
            Path(output).write_bytes(arm_elf())
            result.manifest.chmod(0o644);result.manifest.write_bytes(appliance.canonical(mutated))
            return appliance.digest(output),fes_platform.platform_build_record('1'*64,'2'*64,selected_revision,'go1.26.5','3'*64)
        def assemble(output,binary,factory,kernel,**kwargs):
            self.assertEqual(Path(factory).read_bytes(),original,'CLI reopened mutable factory after verification')
            self.assertTrue(Path(output).is_absolute())
            self.assertIn('platform_build',kwargs)
            self.assertEqual(kwargs['binary_source_revision'],selected_revision)
            self.assertNotEqual(kwargs['binary_source_revision'],p.fogcast_revision)
            return appliance.BootstrapResult(Path(output))
        with mock.patch.object(sys,'argv',['appliance.py','bootstrap','--release',str(result.directory),'--output','out/test-bootstrap']), \
             mock.patch.object(media,'operation',return_value=contextlib.nullcontext()), \
             mock.patch.object(appliance,'verified_inputs',return_value=(self.rootfs,self.kernel,p,self.root,{},None)), \
             mock.patch.object(appliance,'cached_media_container',return_value='sha256:'+'8'*64), \
             mock.patch.object(media.cold_build,'git',return_value='7'*40), \
             mock.patch.object(appliance.fes_platform,'build_static_arm',side_effect=build_arm), \
             mock.patch.object(appliance,'assemble_bootstrap',side_effect=assemble), \
             mock.patch('builtins.print'):
            appliance.main()

    def test_appliance_media_rebuild_uses_bootstrap_source_revision(self):
        software=dataclasses.replace(provenance(self.rootfs,self.kernel),fogcast_revision='b'*40)
        release=appliance.export_release(self.root/'release-media',self.rootfs,self.kernel,
                                         version='v1',provenance=software)
        platform_build=fes_platform.platform_build_record('1'*64,'2'*64,'9'*40,'go1.26.5','3'*64)

        class StopAtBootstrap(Exception):
            pass

        def build_binary(root,fogcast,output,env):
            Path(output).write_bytes(arm_elf())
            return appliance.digest(output),platform_build

        def capture_bootstrap(*args,**kwargs):
            self.assertEqual(kwargs['binary_source_revision'],platform_build['fogcast_revision'])
            self.assertNotEqual(kwargs['binary_source_revision'],software.fogcast_revision)
            raise StopAtBootstrap

        with mock.patch.object(appliance,'verified_inputs',return_value=(
                self.rootfs,self.kernel,software,self.root,{},None)), \
             mock.patch.object(appliance_media,'compare_bundle'), \
             mock.patch.object(appliance,'cached_media_container',return_value='sha256:'+'8'*64), \
             mock.patch.object(appliance_media,'retained_assembly_revision',return_value='a'*40), \
             mock.patch.object(appliance_media,'build_selected_binary',side_effect=build_binary), \
             mock.patch.object(appliance,'assemble_bootstrap',side_effect=capture_bootstrap):
            with self.assertRaises(StopAtBootstrap):
                appliance_media.prepare(self.root,'native-integration-dev',release.directory,
                    self.root/'bootstrap',self.root/'media-scratch')

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
        platform_build=fes_platform.platform_build_record('8'*64,'9'*64,'a'*40,'go1.26.5','b'*64)
        self.assertNotEqual(identity,appliance.bootstrap_identity('2'*64,factory,'sha256:'+'3'*64,'4'*40,recipe,platform_build))


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
            platform_build=fes_platform.platform_build_record('1'*64,'2'*64,'d'*40,'go1.26.5','3'*64)
            result=appliance.assemble_bootstrap(root/'bound',binary,factory,kernel,runner=runner,
                binary_source_revision='d'*40,assembly_revision='c'*40,platform_build=platform_build)
            evidence=json.loads(result.evidence.read_bytes())
            self.assertEqual(evidence['assembly_revision'],'c'*40)
            self.assertEqual(evidence['assembly_recipe']['scripts/appliance_inside.py'],appliance.digest(ROOT/'scripts/appliance_inside.py'))
            self.assertEqual(evidence['classification'],'source-bound-host-artifact')
            self.assertEqual(evidence['platform_build'],platform_build)
            self.assertEqual(result.directory.stat().st_mode&0o777,0o555)
            self.assertEqual(result.image.stat().st_mode&0o777,0o444)
            result.image.chmod(0o644)
            with self.assertRaisesRegex(ValueError,'sealed'):
                appliance.assemble_bootstrap(root/'bound',binary,factory,kernel,runner=runner,
                    binary_source_revision='d'*40,assembly_revision='c'*40,platform_build=platform_build)
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
            with mock.patch.object(media.cold_build,'git',return_value=''), mock.patch.object(media,'select',return_value=('image-fingerprint','host-fingerprint',root/'fogcast',{})):
                with self.assertRaisesRegex(ValueError,'cold image receipt'):
                    appliance.verified_inputs(root,'native-integration-dev')

    def test_verified_inputs_consumes_real_media_selection_shape(self):
        import sys
        sys.path.insert(0,str(ROOT/'scripts'))
        import media
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            (root/'profiles').mkdir()
            (root/'profiles/native-integration-dev.toml').write_text('version = "test"\n')
            revisions={'FogCast':'a'*40,'libmister-runtime':'b'*40}
            def git_output(_root,*args):
                return '' if args[0]=='status' else 'module example.invalid/fixture\ngo 1.26.5'
            with mock.patch.object(media.cold_build,'git',side_effect=git_output), \
                 mock.patch.object(media.cold_build,'validate',return_value=revisions), \
                 mock.patch.object(media.cold_build,'native_image_mode',return_value='package-only'), \
                 mock.patch.object(media.cold_build,'selected_packages',return_value=('fes.coleco',)), \
                 mock.patch.object(media.subprocess,'check_output',return_value='go version test'), \
                 mock.patch.object(media.cold_build,'build_fingerprint',return_value=('image-fingerprint',{})), \
                 mock.patch.object(media.cold_build,'host_fingerprint',return_value=('host-fingerprint',{})), \
                 mock.patch.object(media.cold_build,'source_checkout',return_value=root/'fogcast'), \
                 mock.patch.object(media.cold_build,'load_verified_image',side_effect=ValueError('verified image boundary')) as verify:
                # Keep select() real: mocking its tuple hid the previous five/four mismatch.
                with self.assertRaisesRegex(ValueError,'verified image boundary'):
                    appliance.verified_inputs(root,'native-integration-dev')
                verify.assert_called_once_with(root/'out/native-integration-dev','image-fingerprint')

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
