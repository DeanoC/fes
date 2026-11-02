"""Public workflow selection and receipt boundaries; fixtures do not admit MENU."""
import contextlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

from scripts import appliance, appliance_media, de25_appliance, media

ROOT = Path(__file__).resolve().parents[1]


class WorkflowTests(unittest.TestCase):
    def test_profile_and_factory_cannot_override_independent_board_selection(self):
        for board, profile, factory in (
                ('de25-nano', None, None), ('de10-nano', None, Path('/factory')),
                ('de25-nano', 'native-integration-dev', Path('/factory')),
                ('de10-nano', 'de25-nano', None), ('unknown', None, None)):
            with self.subTest(board=board, profile=profile), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                with mock.patch.object(de25_appliance, 'modules') as modules:
                    with self.assertRaises(ValueError):
                        appliance.verified_inputs(root, profile, board=board, factory=factory)
                    modules.assert_not_called()
                self.assertEqual(list(root.iterdir()), [])

    def test_intermediate_and_ambiguous_receipts_fail_before_verifier_build(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for names in (('base.json',), ('image.json',), ('factory.json', 'base.json')):
                with self.subTest(receipts=names):
                    for path in root.iterdir():
                        path.unlink()
                    for name in names:
                        (root / name).write_text('{}')
                    with mock.patch.object(de25_appliance, 'modules') as modules, \
                            mock.patch.object(de25_appliance.subprocess, 'run') as run:
                        with self.assertRaisesRegex(ValueError, 'actual managed-factory'):
                            de25_appliance.verified_inputs(ROOT, 'de25-nano', root)
                        modules.assert_not_called(); run.assert_not_called()

    def test_de25_lease_requires_its_explicit_board_and_remains_exclusive(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for profile, board in (('de25-nano', 'de10-nano'), ('native-integration-dev', 'de25-nano'),
                                   ('de25-nano', 'unknown')):
                with self.assertRaises(ValueError), media.operation(root, profile, board=board):
                    self.fail('foreign profile obtained a lease')
            self.assertFalse((root / 'out').exists())
            with media.operation(root, 'de25-nano', board='de25-nano'):
                with self.assertRaisesRegex(ValueError, 'holds this profile lease'), \
                        media.operation(root, 'de25-nano', board='de25-nano'):
                    self.fail('second operation obtained the lease')

    def test_release_cli_routes_only_explicit_de25_to_selected_provenance(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); image = root / 'rootfs'; image.write_bytes(bytes(4096))
            kernel = root / 'Image'; kernel.write_bytes(b'fixture kernel')
            p = appliance.ReleaseProvenance('a'*40, 'b'*40, 'c'*40,
                appliance.digest(image), appliance.digest(kernel), 'd'*64, 'e'*64, 'f'*64, 1)
            argv = ['appliance.py', 'release', '--board', 'de25-nano', '--factory', str(root),
                    '--version', 'fixture.1', '--image-passes', '1']
            with mock.patch.object(sys, 'argv', argv), \
                    mock.patch.object(media, 'operation', return_value=contextlib.nullcontext()) as operation, \
                    mock.patch.object(appliance, 'verified_inputs', return_value=(image,kernel,p,root,{},None)) as inputs, \
                    mock.patch.object(appliance, 'export_release') as export, mock.patch('builtins.print'):
                appliance.main()
            self.assertEqual(inputs.call_args.kwargs, {'board':'de25-nano','factory':root})
            self.assertEqual(operation.call_args.args[1], 'de25-nano')
            self.assertEqual(operation.call_args.kwargs, {'board':'de25-nano'})
            self.assertEqual(export.call_args.kwargs['board'], 'de25-nano')
            self.assertEqual(export.call_args.kwargs['image_passes'], 1)
            self.assertEqual(export.call_args.kwargs['provenance'], p)

    def test_foreign_card_selection_cannot_reach_preparation_or_publication(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); output = root / 'published'
            for kwargs in ({'board':'de25-nano'}, {'board':'de10-nano','factory':root},
                           {'board':'de10-nano','vendor_boot':root}):
                with self.subTest(kwargs=kwargs), mock.patch.object(appliance_media, 'prepare') as prepare:
                    with self.assertRaises(ValueError):
                        appliance_media.execute(root,'build',root/'release',root/'bootstrap',output,**kwargs)
                    prepare.assert_not_called()
                    self.assertFalse(output.exists())

    def test_aarch64_bootstrap_build_and_retained_source_use_selected_architecture(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary); binary = root / 'fes-boot'
            with mock.patch.object(appliance_media.fes_platform, 'build_static_arm', return_value=('hash',{})) as build:
                appliance_media.build_selected_binary(ROOT,ROOT/'sources/FogCast',binary,{},board='de25-nano')
                self.assertEqual(build.call_args.kwargs, {'architecture':'aarch64'})
            revision = subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
            (root/'evidence.json').write_text(json.dumps({'assembly_revision':revision,'platform_build':{'fixture':True}}))
            with mock.patch.object(appliance_media.fes_platform, 'verify_retained') as verify:
                self.assertEqual(appliance_media.retained_assembly_revision(ROOT,root,ROOT/'sources/FogCast',board='de25-nano'),revision)
                self.assertEqual(verify.call_args.kwargs['architecture'],'aarch64')


class CardSnapshotTests(unittest.TestCase):
    def fixture(self, root, base):
        factory = root/'factory'; (factory/'kernel').mkdir(parents=True)
        (factory/'factory.json').write_text('unqualified snapshot unit fixture')
        files = {}
        for name in base.KERNEL_FILES:
            path = factory/'kernel'/name; path.write_bytes(('fixture '+name).encode())
            files['kernel/'+name] = appliance.digest(path)
        idle = factory/'artifacts/core.rbf'; idle.parent.mkdir(); idle.write_bytes(b'fixture idle')
        files['artifacts/core.rbf'] = appliance.digest(idle)
        vendor = root/'vendor'; vendor.mkdir(); (vendor/'vendor-boot-prefix.bin').write_bytes(b'fixture prefix')
        selected = de25_appliance.Factory(factory, {'files':files}, {}, idle)
        release = SimpleNamespace(image=root/'release.img', manifest=root/'release.json')
        bootstrap = SimpleNamespace(image=root/'bootstrap.img')
        return selected,vendor,release,bootstrap

    def test_snapshots_stay_bound_when_original_inputs_are_later_changed(self):
        base,_,_,lane = de25_appliance.modules(ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); selected,vendor,release,bootstrap=self.fixture(root,base)
            scratch=root/'scratch'; scratch.mkdir()
            with mock.patch.object(lane.boot,'_verify_prepared_boot'), mock.patch.object(base.owner,'selected_kernel'):
                inputs,bindings=de25_appliance.card_inputs(ROOT,selected,vendor,scratch,release,bootstrap,None,None)
            (selected.directory/'kernel/Image').write_bytes(b'changed original')
            selected.idle.write_bytes(b'changed original')
            (vendor/'vendor-boot-prefix.bin').write_bytes(b'changed original')
            self.assertEqual((inputs.kernel/'Image').read_bytes(),b'fixture Image')
            self.assertEqual(inputs.idle.read_bytes(),b'fixture idle')
            self.assertEqual((inputs.vendor_boot/'vendor-boot-prefix.bin').read_bytes(),b'fixture prefix')
            self.assertEqual(bindings['kernel_sha256'],appliance.digest(inputs.kernel/'Image'))

    def test_changed_kernel_is_refused_before_it_can_enter_card_inputs(self):
        base,_,_,lane = de25_appliance.modules(ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); selected,vendor,release,bootstrap=self.fixture(root,base)
            (selected.directory/'kernel/Image').write_bytes(b'changed source')
            scratch=root/'scratch'; scratch.mkdir()
            with mock.patch.object(lane.boot,'_verify_prepared_boot'), mock.patch.object(base.owner,'selected_kernel'):
                with self.assertRaisesRegex(ValueError,'kernel changed'):
                    de25_appliance.card_inputs(ROOT,selected,vendor,scratch,release,bootstrap,None,None)


class FactoryReceiptTests(unittest.TestCase):
    def fixture(self, root, factory):
        artifacts = root / 'artifacts'; package = artifacts / 'core-packages' / ('1'*64)
        package.mkdir(parents=True)
        (package/'core.rbf').write_bytes(b'UNQUALIFIED UNIT FIXTURE')
        (package/'manifest.toml').write_text('fixture only\n')
        selection = artifacts/'fes-menu.package-selection.toml'; selection.write_text('fixture only\n')
        manifest = {'provenance':{'fogcast':'b'*40,'runtime':'c'*40}}
        packages = [{'directory':package,'selection_path':selection}]
        with mock.patch.object(factory,'selected',return_value=(manifest,packages)):
            names = factory.artifact_names(root)
        for name in names:
            path=root/name; path.parent.mkdir(parents=True,exist_ok=True)
            if not path.exists():path.write_bytes(b'unit fixture; not an actual artifact')
        inputs={'fes_revision':'a'*40,'recipe_sha256':factory.recipe_key(),
                'sdk_sha256':factory.base.sdk_lock()['sdk_sha256'],'profile':'managed-factory'}
        (root/'base-inputs.json').write_text(json.dumps(inputs))
        (root/'factory-verify.json').write_text(json.dumps({'classification':'managed-factory-candidate',
            'board':'de25-nano','normal_runtime_admission':'closed','base_contract':{'board':'de25-nano'}}))
        receipt={'format':1,'board':'de25-nano','architecture':'aarch64',
                 'classification':'managed-factory-candidate','fes_revision':'a'*40,
                 'recipe_sha256':factory.recipe_key(),'builder_image':factory.base.sdk_lock()['builder_image'],
                 'normal_runtime_admission':'closed','image_passes':1,
                 'files':{name:appliance.digest(root/name) for name in names}}
        (root/'factory.json').write_text(json.dumps(receipt))
        return receipt,manifest,packages

    def test_structural_report_tampering_and_invented_two_pass_are_refused(self):
        base,factory,boot,_=de25_appliance.modules(ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); receipt,manifest,packages=self.fixture(root,factory)
            with mock.patch.object(factory,'selected',return_value=(manifest,packages)), \
                    mock.patch.object(base.owner,'selected_kernel'), mock.patch.object(boot,'verify_report'):
                factory.verify_receipt(root)
                path=root/'factory-verify.json'; before=path.read_bytes(); path.write_bytes(before+b' ')
                with self.assertRaisesRegex(ValueError,'factory artifact differs: factory-verify.json'):
                    factory.verify_receipt(root)
                path.write_bytes(before)
                receipt['image_passes']=2; (root/'factory.json').write_text(json.dumps(receipt))
                with self.assertRaisesRegex(ValueError,'source or profile'):
                    factory.verify_receipt(root)

    def test_saved_pass_claim_cannot_skip_fresh_real_verification(self):
        base,factory,boot,_=de25_appliance.modules(ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); self.fixture(root,factory)
            with mock.patch.object(boot,'authenticated_image'), \
                    mock.patch.object(de25_appliance.subprocess,'check_output',return_value=''), \
                    mock.patch.object(de25_appliance.subprocess,'run',side_effect=subprocess.CalledProcessError(1,['verifier'])) as run:
                with self.assertRaises(subprocess.CalledProcessError):
                    de25_appliance.verified_inputs(ROOT,'de25-nano',root)
                self.assertIn('verify-factory',run.call_args.args[0])

    def test_provenance_uses_closed_factory_report_and_actual_boot_log_hashes(self):
        base,factory,boot,_=de25_appliance.modules(ROOT)
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); receipt,manifest,packages=self.fixture(root,factory)
            with mock.patch.object(factory,'selected',return_value=(manifest,packages)), \
                    mock.patch.object(base.owner,'selected_kernel'),mock.patch.object(boot,'verify_report'), \
                    mock.patch.object(boot,'authenticated_image'), \
                    mock.patch.object(de25_appliance.subprocess,'check_output',return_value=''), \
                    mock.patch.object(de25_appliance.subprocess,'run'):
                _,_,p,_,_,selected=de25_appliance.verified_inputs(ROOT,'de25-nano',root)
            self.assertEqual(p.verification_sha256,receipt['files']['factory-verify.json'])
            self.assertEqual(p.qemu_log_sha256,receipt['files']['qemu-smoke.log'])
            self.assertEqual(p.image_passes,1)
            self.assertEqual(selected.idle,packages[0]['directory']/'core.rbf')


if __name__=='__main__':unittest.main()
