"""Recovery enables the vendor network/SD path, separate from native ownership."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('nano_recovery', ROOT / 'platform/embedded/nano/recovery.py')
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)
with patch.dict(sys.modules, {'recovery': recovery}):
    pack_spec = importlib.util.spec_from_file_location('nano_recovery_package', ROOT / 'platform/embedded/nano/recovery_package.py')
    package = importlib.util.module_from_spec(pack_spec)
    pack_spec.loader.exec_module(package)


class NanoRecoveryTests(unittest.TestCase):
    def config(self):
        return ''.join('CONFIG_' + name + '=y\n' for name in recovery.ENABLE) + 'CONFIG_BOOTDELAY=-1\nCONFIG_BOOTCOMMAND=""\n'

    def test_requires_network_sd_checksum_and_manual_boot(self):
        config = self.config()
        recovery.check(config)
        for name in recovery.ENABLE:
            with self.subTest(missing=name), self.assertRaises(ValueError):
                recovery.check(config.replace('CONFIG_' + name + '=y\n', ''))
        for name in recovery.DISABLE:
            with self.subTest(excluded=name), self.assertRaises(ValueError):
                recovery.check(config + 'CONFIG_' + name + '=y\n')
        with self.assertRaises(ValueError):
            recovery.check(config.replace('CONFIG_BOOTDELAY=-1', 'CONFIG_BOOTDELAY=0'))

    def test_recovery_cannot_be_combined_with_native_handoff(self):
        result = subprocess.run([sys.executable, str(ROOT / 'platform/embedded/firmware/build_terasic_uboot.py'),
                                 '--reference-repo', '/nonexistent', '--bl31-build', '/nonexistent',
                                 '--cross-prefix', '/nonexistent-', '--output', '/nonexistent',
                                 '--native-entry-build', '/nonexistent', '--network-recovery'],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 2)
        self.assertIn('separate boot profiles', result.stderr)

    def test_packaging_binds_recovery_profile_artifacts_and_raw_slot(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root/'build/spl').mkdir(parents=True)
            fit, spl, config = (root/name for name in ('build/u-boot.itb','build/spl/u-boot-spl-dtb.hex','build/.config'))
            fit.write_bytes(b'synthetic FIT'); spl.write_bytes(b'synthetic SPL')
            config.write_text(self.config()+'CONFIG_SYS_MMCSD_RAW_MODE_U_BOOT_SECTOR=0x200\n')
            receipt = {'status':'built','fit_atf_verified':True,'fit_sha256':package.sha(fit),
                       'spl_sha256':package.sha(spl),'config_sha256':package.sha(config),
                       'producer_sha256':package.sha(ROOT/'platform/embedded/firmware/build_terasic_uboot.py'),
                       'network_recovery':{'profile_sha256':package.sha(ROOT/'platform/embedded/nano/recovery.py'),
                                           'manual_prompt':True,'automatic_writes':False,'random_mac':False,
                                           'sd_dma':False,'native_handoff':False}}
            record = root/'result.json'
            record.write_text(json.dumps(receipt));package.checked_build(root)
            for key in ('fit_sha256','spl_sha256','config_sha256','producer_sha256'):
                bad = dict(receipt, **{key:'0'*64});record.write_text(json.dumps(bad))
                with self.subTest(key=key), self.assertRaises(ValueError):package.checked_build(root)
            record.write_text(json.dumps(dict(receipt,native_handoff={})))
            with self.assertRaises(ValueError):package.checked_build(root)
            config.write_text(config.read_text().replace('SECTOR=0x200','SECTOR=0x4000'))
            receipt['config_sha256']=package.sha(config);record.write_text(json.dumps(receipt))
            with self.assertRaises(ValueError):package.checked_build(root)
            config.write_text(config.read_text().replace('SECTOR=0x4000','SECTOR=0x200'))
            receipt['config_sha256']=package.sha(config)
            with fit.open('r+b') as stream:
                stream.truncate((package.EXPERIMENT_SECTOR-package.FIT_SECTOR)*512+1)
            receipt['fit_sha256']=package.sha(fit);record.write_text(json.dumps(receipt))
            with self.assertRaises(ValueError):package.checked_build(root)

    def test_packager_refuses_output_outside_private_worktree(self):
        result=subprocess.run([sys.executable,str(ROOT/'platform/embedded/nano/recovery_package.py'),
                               '--uboot-build','/nonexistent','--vendor-sof','/nonexistent',
                               '--quartus-pfg','/nonexistent','--output','/dev/fes-not-a-card'],capture_output=True,text=True)
        self.assertEqual(result.returncode,2)
        self.assertIn('fresh private directory',result.stderr)


if __name__ == '__main__':
    unittest.main()
