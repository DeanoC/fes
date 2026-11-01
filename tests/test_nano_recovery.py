"""Recovery enables the vendor network/SD path, separate from native ownership."""
import importlib.util
import json
from pathlib import Path
import shutil
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
        return (''.join('CONFIG_' + name + '=y\n' for name in recovery.ENABLE) +
                'CONFIG_BOOTDELAY=-1\nCONFIG_BOOTCOMMAND=""\nCONFIG_PHY_ANEG_TIMEOUT=4000\n'+
                'CONFIG_LOCALVERSION="'+recovery.LOCALVERSION+'"\n')

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
        with self.assertRaises(ValueError):
            recovery.check(config.replace('CONFIG_PHY_ANEG_TIMEOUT=4000','CONFIG_PHY_ANEG_TIMEOUT=15000'))

    symbols=('   1: 0000000080200000 64 FUNC LOCAL DEFAULT 1 ksz9031_config\n'
             '   2: 0000000080210000 80 OBJECT GLOBAL DEFAULT 2 _u_boot_list_2_phy_driver_2_ksz9031\n')

    def test_driver_must_be_linked_not_only_enabled(self):
        recovery.check_linked(self.symbols)
        for bad in ('',self.symbols+self.symbols,self.symbols.replace('DEFAULT 1','DEFAULT UND'),
                    self.symbols.replace('FUNC LOCAL','OBJECT LOCAL'),self.symbols.replace('LOCAL','WEAK'),
                    self.symbols.replace('64 FUNC','0 FUNC'),self.symbols.replace('ksz9031','ksz9021')):
            with self.subTest(symbols=bad),self.assertRaises(ValueError):recovery.check_linked(bad)

    def test_recovery_cannot_be_combined_with_native_handoff(self):
        result = subprocess.run([sys.executable, str(ROOT / 'platform/embedded/firmware/build_terasic_uboot.py'),
                                 '--reference-repo', '/nonexistent', '--bl31-build', '/nonexistent',
                                 '--cross-prefix', '/nonexistent-', '--output', '/nonexistent',
                                 '--native-entry-build', '/nonexistent', '--network-recovery'],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 2)
        self.assertIn('separate boot profiles', result.stderr)

    @patch.object(package.subprocess,'check_output',return_value=symbols)
    def test_packaging_binds_recovery_profile_artifacts_and_boot_paths(self,readelf):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root/'build/spl').mkdir(parents=True)
            fit, spl, config = (root/name for name in ('build/u-boot.itb','build/spl/u-boot-spl-dtb.hex','build/.config'))
            fit.write_bytes(b'synthetic FIT'); spl.write_bytes(b'synthetic SPL')
            elf=root/'build/u-boot';elf.write_bytes(b'host fixture; readelf output mocked explicitly')
            boot_settings = ('CONFIG_SYS_MMCSD_RAW_MODE_U_BOOT_SECTOR=0x200\n'
                             'CONFIG_SPL_FS_FAT=y\nCONFIG_SYS_MMCSD_FS_BOOT=y\n'
                             'CONFIG_SYS_MMCSD_FS_BOOT_PARTITION=1\n'
                             'CONFIG_SPL_FS_LOAD_PAYLOAD_NAME="u-boot.itb"\n')
            config.write_text(self.config()+boot_settings)
            receipt = {'status':'built','fit_atf_verified':True,'fit_sha256':package.sha(fit),
                       'spl_sha256':package.sha(spl),'config_sha256':package.sha(config),
                       'uboot_elf_sha256':package.sha(elf),
                       'producer_sha256':package.sha(ROOT/'platform/embedded/firmware/build_terasic_uboot.py'),
                       'network_recovery':{'profile_sha256':package.sha(ROOT/'platform/embedded/nano/recovery.py'),
                                           'manual_prompt':True,'automatic_writes':False,'random_mac':False,
                                           'sd_dma':False,'native_handoff':False,
                                           'phy':recovery.PHY,'phy_driver_linked':True}}
            record = root/'result.json'
            record.write_text(json.dumps(receipt));package.checked_build(root)
            for key in ('fit_sha256','spl_sha256','config_sha256','producer_sha256','uboot_elf_sha256'):
                bad = dict(receipt, **{key:'0'*64});record.write_text(json.dumps(bad))
                with self.subTest(key=key), self.assertRaises(ValueError):package.checked_build(root)
            mode=dict(receipt['network_recovery'],phy_driver_linked=False)
            record.write_text(json.dumps(dict(receipt,network_recovery=mode)))
            with self.assertRaises(ValueError):package.checked_build(root)
            record.write_text(json.dumps(dict(receipt,native_handoff={})))
            with self.assertRaises(ValueError):package.checked_build(root)
            for setting in boot_settings.splitlines():
                config.write_text(self.config()+boot_settings.replace(setting+'\n',''))
                receipt['config_sha256']=package.sha(config);record.write_text(json.dumps(receipt))
                with self.subTest(missing=setting), self.assertRaises(ValueError):package.checked_build(root)
            config.write_text(self.config()+boot_settings)
            config.write_text(config.read_text().replace('SECTOR=0x200','SECTOR=0x4000'))
            receipt['config_sha256']=package.sha(config);record.write_text(json.dumps(receipt))
            with self.assertRaises(ValueError):package.checked_build(root)
            config.write_text(config.read_text().replace('SECTOR=0x4000','SECTOR=0x200'))
            receipt['config_sha256']=package.sha(config)
            with fit.open('r+b') as stream:
                stream.truncate((package.EXPERIMENT_SECTOR-package.FIT_SECTOR)*512+1)
            receipt['fit_sha256']=package.sha(fit);record.write_text(json.dumps(receipt))
            with self.assertRaises(ValueError):package.checked_build(root)

    @unittest.skipUnless(all(shutil.which(name) for name in ('mformat','mmd','mcopy')), 'mtools required')
    def test_card_contains_exact_filesystem_boot_fit_and_rejects_bad_readback(self):
        tools={name:Path(shutil.which(name)).absolute() for name in ('mformat','mmd','mcopy')}
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp)
            fit=root/'input.itb';fit.write_bytes(b'FIT payload\n'+bytes(range(256))*4096)
            def run(label,argv):
                subprocess.run([str(x) for x in argv],check=True,capture_output=True,timeout=20)
            good=root/'good';good.mkdir();(good/'RECOVERY.JSON').write_text('{"test":true}\n')
            disk=package.make_card(good,fit,tools,run)
            self.assertEqual((good/'u-boot.itb.readback').read_bytes(),fit.read_bytes())
            with disk.open('rb') as stream:
                stream.seek(package.FIT_SECTOR*512)
                self.assertEqual(stream.read(fit.stat().st_size),fit.read_bytes())
            bad=root/'bad';bad.mkdir();(bad/'RECOVERY.JSON').write_text('{}\n')
            def corrupt_readback(label,argv):
                run(label,argv)
                if label=='fat-boot-readback':(bad/'u-boot.itb.readback').write_bytes(b'corrupt')
            with self.assertRaisesRegex(ValueError,'FAT boot FIT readback differs'):
                package.make_card(bad,fit,tools,corrupt_readback)

    def test_packager_refuses_output_outside_private_worktree(self):
        result=subprocess.run([sys.executable,str(ROOT/'platform/embedded/nano/recovery_package.py'),
                               '--uboot-build','/nonexistent','--vendor-sof','/nonexistent',
                               '--quartus-pfg','/nonexistent','--output','/dev/fes-not-a-card'],capture_output=True,text=True)
        self.assertEqual(result.returncode,2)
        self.assertIn('fresh private directory',result.stderr)


if __name__ == '__main__':
    unittest.main()
