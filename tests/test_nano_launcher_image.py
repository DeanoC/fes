"""Real FAT assembly/readback; native source admission is independently injected."""
import copy
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import launcher_image as image
import recovery_package
import recovery


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'))+'\n').encode()


@unittest.skipUnless(all(shutil.which(n) for n in ('mformat', 'mmd', 'mcopy', 'mtype', 'mdir')), 'mtools required')
class LauncherImageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.patch_root = patch.object(image, 'ROOT', self.root)
        self.patch_root.start(); self.addCleanup(self.patch_root.stop)
        self.recovery = self.root/'recovery'; self.recovery.mkdir()
        fit = self.recovery/'u-boot.itb'; fit.write_bytes(b'synthetic protected recovery FIT')
        manifest = dict(format=1, fit_sha256=digest(fit.read_bytes()), spl_sha256='a'*64,
                        bl31_sha256='b'*64, sof_sha256='c'*64, fat32_start_sector=32768,
                        fat32_sectors=262144, native_handoff=False, automatic_writes=False,
                        spl_boot_mode='fat', spl_payload_path='u-boot.itb', spl_fat_partition=1,
                        recovery_phy=dict(recovery.PHY), phy_driver_linked=True)
        (self.recovery/'RECOVERY.JSON').write_bytes(encoded(manifest))
        tools = {n: Path(shutil.which(n)) for n in ('mformat', 'mmd', 'mcopy')}
        recovery_package.make_card(self.recovery, fit, tools,
            lambda label, argv: subprocess.run(list(map(str, argv)), check=True, capture_output=True, timeout=30))
        disk = self.recovery/'nano-recovery-sd.img'
        (self.recovery/'result.json').write_bytes(encoded(dict(manifest, status='prepared',
            sd_image_size=disk.stat().st_size, sd_image_sha256=image.sha(disk))))
        base = image.load_kit(self.recovery)
        with disk.open('rb') as stream:
            mbr = stream.read(512)
        self.staged = self.root/'staged'; (self.staged/'FES/IMPORT').mkdir(parents=True)
        self.payload = {
            'FES/CORESEL.JSN': encoded(dict(format=1, scope='nano-program-selection-v1', programs=[])),
            'FES/UI.JSON': encoded(dict(format=1, scope='nano-tenfoot-catalog-v1', entries=[])),
            'FES/PREFS.JSON': encoded(dict(home='library', attract_enabled=False)).ljust(65536, b' '),
            'FES/MENU.FCP': b'synthetic menu archive', 'FES/MENU.JSN': encoded({'synthetic': True}),
            'FES/IMPORT/CORE.FCP': b'synthetic computer archive',
        }
        self.payload['FES/INSTALL.JSN'] = encoded(dict(format=1, scope='nano-staged-program-catalog-v1', programs=[
            dict(id='zx81', title='ZX81', program={}, files={'FES/IMPORT/CORE.FCP': digest(self.payload['FES/IMPORT/CORE.FCP'])})]))
        self.payload['FES/MENUSEL.JSN'] = encoded(dict(format=1, scope='nano-program-selection-v1', programs=[
            dict(path='FES/MENU.FCP', package_id='d'*64, metadata_path='FES/MENU.JSN', metadata_sha256=digest(self.payload['FES/MENU.JSN']))]))
        self.write_staging()
        self.uboot = self.root/'uboot'; self.uboot.mkdir()
        self.entry = self.root/'entry'; self.entry.mkdir()
        self.sdk = self.root/'sdk'; self.sdk.mkdir()
        self.native_fit = self.uboot/'native.itb'; self.native_fit.write_bytes(b'synthetic candidate FIT')
        self.boot = dict(fit_sha256=digest(self.native_fit.read_bytes()), native_handoff=dict(build_id='1234567890abcdef', startup=dict(mode='autoboot')))
        self.loaded = dict(hps_menu_program={'synthetic': True}, payload_sha256='e'*64)
        self.selected = dict(status='cross-built', program='fpgaload', elf_sha256='e'*64,
            usb_shared=True, usb_input='boot-keyboard', hps_menu_program=self.loaded['hps_menu_program'],
            writable_storage_plan=dict(scope='nano-owned-fat-storage-v1', mbr_sha256=digest(mbr),
                fit_sha256=base['fit_sha256'], fit_bytes=len(base['fit']),
                recovery_sha256=digest(base['manifest']), recovery_bytes=len(base['manifest'])),
            menu_assets={name: dict(size=len(self.payload['FES/'+name]), sha256=digest(self.payload['FES/'+name]))
                         for name in ('MENU.FCP', 'MENU.JSN', 'MENUSEL.JSN')})
        for folder in (self.uboot, self.entry):
            (folder/'result.json').write_bytes(encoded({'synthetic-admission': True}))
        self.bind_sdk()
        self.output = self.root/'out/image'

    def bind_sdk(self):
        raw = encoded(self.selected)
        (self.sdk/'result.json').write_bytes(raw)
        self.loaded['native_build_sha256'] = digest(raw)

    def write_staging(self):
        for name, raw in self.payload.items():
            (self.staged/name).write_bytes(raw)

    def prepare(self):
        # File composition does not replace real checked_native. This test
        # injects that boundary and uses the real recovery/media/mtools path.
        with patch.object(image, 'checked_native', return_value=(self.native_fit, None, None, self.boot, self.loaded, None)):
            return image.prepare(self.uboot, self.entry, self.sdk, self.recovery, self.staged, self.output)

    def test_image_preserves_recovery_and_reads_back_every_launcher_file(self):
        result = self.prepare()
        self.assertEqual(result['status'], 'prepared')
        self.assertFalse(result['hardware_actions'])
        self.assertFalse(result['power_on_qualified'])
        self.assertFalse(result['reboot_persistence_qualified'])
        disk = str(self.output/'native-launcher-sd.img')+'@@'+str(image.PREFIX_SIZE)
        for name, raw in dict(self.payload, **{image.EXPERIMENTAL_FILE: self.native_fit.read_bytes()}).items():
            self.assertEqual(subprocess.check_output(['mtype', '-i', disk, '::'+name]), raw)
        self.assertEqual(subprocess.check_output(['mtype', '-i', disk, '::u-boot.itb']),
                         (self.recovery/'u-boot.itb').read_bytes())
        with self.assertRaisesRegex(ValueError, 'fresh private'):
            self.prepare()

    def test_packaging_readback_rejects_substituted_payloads_and_extra_files(self):
        receipt = self.prepare()
        disk = self.output/'native-launcher-sd.img'
        fat = str(disk)+'@@'+str(image.PREFIX_SIZE)
        inputs = {Path(name): expected for name, expected in receipt['inputs'].items()}

        def verify(label):
            output = self.root/label
            output.mkdir()

            def run(name, argv):
                log = output/(name+'.log')
                with log.open('wb') as stream:
                    subprocess.run(argv, stdout=stream, check=True, timeout=30)
                return log

            image.verify_prepared_image(disk, receipt, inputs, self.native_fit.read_bytes(), output, run)

        verify('unchanged')
        replacement = self.root/'replacement'
        for index, (name, original) in enumerate((('FES/MENU.FCP', self.payload['FES/MENU.FCP']),
                                                  ('FES/RECOVERY.JSON', (self.recovery/'RECOVERY.JSON').read_bytes()))):
            with self.subTest(name=name):
                replacement.write_bytes(b'x'*len(original))
                subprocess.run(['mcopy', '-o', '-i', fat, replacement, '::'+name], check=True)
                receipt['image_sha256'] = image.sha(disk)
                with self.assertRaisesRegex(ValueError, 'disk payload differs: '+name):
                    verify('changed-'+str(index))
                if name in receipt['files']:
                    saved = receipt['files'][name]['sha256']
                    receipt['files'][name]['sha256'] = image.sha(replacement)
                    with self.assertRaisesRegex(ValueError, 'receipt differs from pinned payloads'):
                        verify('changed-file-receipt')
                    receipt['files'][name]['sha256'] = saved
                replacement.write_bytes(original)
                subprocess.run(['mcopy', '-o', '-i', fat, replacement, '::'+name], check=True)
        subprocess.run(['mcopy', '-i', fat, replacement, '::FES/EXTRA.BIN'], check=True)
        with self.assertRaisesRegex(ValueError, 'extra or aliased FAT entries'):
            verify('extra')

    def test_return_trace_profile_is_explicit_and_rechecked_without_fallback(self):
        base = image.load_kit(self.recovery)
        with patch.object(image, 'checked_native', return_value=(self.native_fit,None,None,self.boot,self.loaded,None)), \
                patch.object(image, 'load_return_trace_kit', return_value=base) as traced, \
                patch.object(image, 'load_kit', side_effect=AssertionError('no standard fallback')):
            result = image.prepare(self.uboot,self.entry,self.sdk,self.recovery,self.staged,self.output,
                                   recovery_profile='return-trace', recovery_source_root=self.root)
            self.assertEqual(result['recovery_profile'], 'return-trace')
            self.assertEqual(traced.call_count, 2)
            traced.assert_called_with(self.recovery, source_root=self.root)

    def test_unselected_recovery_roots_and_profiles_refuse(self):
        for options in (dict(recovery_profile='automatic'), dict(recovery_source_root=self.root)):
            with self.subTest(options=options), self.assertRaisesRegex(ValueError, 'explicit supported'):
                image.prepare(self.uboot,self.entry,self.sdk,self.recovery,self.staged,self.output,**options)
            self.assertFalse(self.output.exists())

    def test_changed_native_binding_and_boot_policy_refuse_before_output(self):
        for fault in ('sdk-binding', 'manual', 'gamepad', 'menu-assets', 'protected-plan', 'no-write-plan'):
            with self.subTest(fault=fault):
                original_selected, original_loaded, original_boot = map(copy.deepcopy, (self.selected, self.loaded, self.boot))
                try:
                    if fault == 'sdk-binding': self.loaded['native_build_sha256'] = '0'*64
                    elif fault == 'manual': self.boot['native_handoff']['startup']['mode'] = 'manual'
                    elif fault == 'gamepad': self.selected['usb_input'] = 'gamepad'; self.bind_sdk()
                    elif fault == 'menu-assets': self.selected['menu_assets']['MENU.FCP']['sha256'] = '0'*64; self.bind_sdk()
                    elif fault == 'protected-plan': self.selected['writable_storage_plan']['mbr_sha256'] = '0'*64; self.bind_sdk()
                    elif fault == 'no-write-plan': self.selected['writable_storage_plan'] = None; self.bind_sdk()
                    with self.assertRaises(ValueError): self.prepare()
                    self.assertFalse(self.output.exists())
                finally:
                    self.selected, self.loaded, self.boot = original_selected, original_loaded, original_boot
                    self.bind_sdk()

    def test_staging_rejects_missing_corrupt_extra_and_fat_aliases(self):
        for fault in ('missing', 'corrupt-source', 'extra', 'protected', 'alias', 'prefs-size', 'prefs-type', 'bootstrap-type', 'symlink'):
            with self.subTest(fault=fault):
                original = copy.deepcopy(self.payload)
                extra = None
                try:
                    if fault == 'missing': (self.staged/'FES/MENU.FCP').unlink()
                    elif fault == 'corrupt-source': (self.staged/'FES/IMPORT/CORE.FCP').write_bytes(b'changed')
                    elif fault in ('extra', 'protected', 'alias'):
                        extra = self.staged/('FES/OTHER.BIN' if fault == 'extra' else 'FES/recovery.JSON' if fault == 'protected' else 'FES/menu.fcp')
                        extra.write_bytes(b'extra')
                    elif fault == 'prefs-size': (self.staged/'FES/PREFS.JSON').write_bytes(b'{}')
                    elif fault == 'prefs-type': (self.staged/'FES/PREFS.JSON').write_bytes(encoded(dict(home='library',attract_enabled=0)).ljust(65536,b' '))
                    elif fault == 'bootstrap-type': (self.staged/'FES/CORESEL.JSN').write_bytes(encoded(dict(format=True,scope='nano-program-selection-v1',programs=[])))
                    elif fault == 'symlink':
                        p = self.staged/'FES/MENU.FCP'; p.unlink(); p.symlink_to(self.staged/'FES/MENU.JSN')
                    with self.assertRaises(ValueError): self.prepare()
                    self.assertFalse(self.output.exists())
                finally:
                    if extra is not None: extra.unlink()
                    for p in self.staged.rglob('*'):
                        if p.is_symlink(): p.unlink()
                    self.payload = original; self.write_staging()

    def test_receipt_and_fit_changes_during_native_admission_refuse_before_copy(self):
        for fault in ('sdk', 'fit'):
            with self.subTest(fault=fault):
                original_sdk = copy.deepcopy(self.selected)
                original_fit = self.native_fit.read_bytes()
                def admitted(*_):
                    if fault == 'sdk':
                        self.selected['changed'] = True
                        self.bind_sdk()
                    else:
                        self.native_fit.write_bytes(b'changed candidate FIT')
                    return self.native_fit, None, None, self.boot, self.loaded, None
                try:
                    with patch.object(image, 'checked_native', side_effect=admitted):
                        with self.assertRaisesRegex(ValueError, 'changed'):
                            image.prepare(self.uboot, self.entry, self.sdk, self.recovery, self.staged, self.output)
                    self.assertFalse(self.output.exists())
                finally:
                    self.selected = original_sdk
                    self.bind_sdk()
                    self.native_fit.write_bytes(original_fit)

    def test_uncertain_readback_stops_without_publishing_a_result(self):
        real_run = subprocess.run
        calls = []
        def corrupt(argv, **kwargs):
            result = real_run(argv, **kwargs)
            calls.append(argv)
            if argv[0] == 'mcopy' and str(argv[-2]).startswith('::'):
                Path(argv[-1]).write_bytes(b'changed readback')
            return result
        with patch.object(image.subprocess, 'run', side_effect=corrupt):
            with self.assertRaisesRegex(ValueError, 'readback differs'):
                self.prepare()
        self.assertEqual(sum(argv[0] == 'mcopy' and not str(argv[-2]).startswith('::') for argv in calls), 1)
        self.assertFalse((self.output/'result.json').exists())
