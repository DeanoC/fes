"""Recovery enables the vendor network/SD path, separate from native ownership."""
import importlib.util
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
                'CONFIG_LOCALVERSION="'+recovery.LOCALVERSION+'"\n'+
                'CONFIG_IDENT_STRING="socfpga_agilex5"\n')

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
        for bad in ('', 'socfpga_agilex5_other'):
            with self.subTest(board=bad), self.assertRaises(ValueError):
                recovery.check(config.replace('CONFIG_IDENT_STRING="socfpga_agilex5"\n',
                                             'CONFIG_IDENT_STRING="'+bad+'"\n'))

    symbols=('   1: 0000000080200000 64 FUNC LOCAL DEFAULT 1 ksz9031_config\n'
             '   2: 0000000080210000 80 OBJECT GLOBAL DEFAULT 2 _u_boot_list_2_phy_driver_2_ksz9031\n'
             '   3: 0000000080211000 128 FUNC GLOBAL DEFAULT 1 fes_nano_recovery_phy_prepare\n'
             '   4: 0000000080212000 80 OBJECT GLOBAL DEFAULT 2 _u_boot_list_2_cmd_2_fesphy\n')

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
            elf=root/'build/u-boot';elf.write_bytes(('U-Boot 2025.01'+recovery.LOCALVERSION+' (fixture; readelf explicitly mocked)').encode())
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
            shutil.copytree(ROOT/'tests/fixtures/nano-recovery-phy',root/'source')
            receipt['network_recovery']['phy_sources']=recovery.prepare_phy_sources(root/'source')
            record = root/'result.json'
            record.write_text(json.dumps(receipt));package.checked_build(root)
            actual_elf=elf.read_bytes()
            elf.write_bytes(actual_elf.replace(recovery.LOCALVERSION.encode(),b'-wrong-profile'))
            record.write_text(json.dumps(dict(receipt,uboot_elf_sha256=package.sha(elf))))
            with self.assertRaisesRegex(ValueError,'selected version banner'):package.checked_build(root)
            elf.write_bytes(actual_elf);record.write_text(json.dumps(receipt))
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


class RecoveryPHYSourceTests(unittest.TestCase):
    fixture=ROOT/'tests/fixtures/nano-recovery-phy'

    def test_only_pinned_sources_transform_and_reconstruct_without_reapplication(self):
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp)/'source';shutil.copytree(self.fixture,source)
            record=recovery.prepare_phy_sources(source)
            self.assertEqual(recovery.check_phy_sources(source,record),record)
            with self.assertRaises(ValueError):recovery.prepare_phy_sources(source)
        for name in recovery.PINNED_PHY_SOURCES:
            with self.subTest(name=name),tempfile.TemporaryDirectory() as temp:
                source=Path(temp)/'source';shutil.copytree(self.fixture,source)
                (source/name).write_bytes((source/name).read_bytes()+b'changed\n')
                before={str(p.relative_to(source)):p.read_bytes() for p in source.rglob('*') if p.is_file()}
                with self.assertRaises(ValueError):recovery.prepare_phy_sources(source)
                self.assertEqual(before,{str(p.relative_to(source)):p.read_bytes() for p in source.rglob('*') if p.is_file()})

    def test_self_consistent_modified_driver_state_command_or_receipt_rejects(self):
        for name in (*recovery.PINNED_PHY_SOURCES,'cmd/fes_nano_phy.c'):
            with self.subTest(name=name),tempfile.TemporaryDirectory() as temp:
                source=Path(temp)/'source';shutil.copytree(self.fixture,source)
                record=recovery.prepare_phy_sources(source)
                (source/name).write_bytes((source/name).read_bytes()+b'/* unrelated modification */\n')
                changed=copy.deepcopy(record)
                changed['prepared'][name]=hashlib.sha256((source/name).read_bytes()).hexdigest()
                with self.assertRaises(ValueError):recovery.check_phy_sources(source,changed)
        for change in ({},None,{'scope':'fabricated'}):
            with self.subTest(record=change),self.assertRaises(ValueError):
                recovery.check_phy_sources(self.fixture,change)

    @unittest.skipUnless(shutil.which('cc'),'host C compiler required')
    def test_actual_vendor_start_prefix_resets_after_readiness_and_prepared_start_does_not(self):
        with tempfile.TemporaryDirectory() as temp:
            directory=Path(temp);source=directory/'source';shutil.copytree(self.fixture,source)
            old=(source/'drivers/net/dwc_eth_xgmac.c').read_text()
            recovery.prepare_phy_sources(source)
            new=(source/'drivers/net/dwc_eth_xgmac.c').read_text()
            for prepared,driver in ((False,old),(True,new)):
                start=driver.index('static int xgmac_start(struct udevice *dev)')
                begin=driver.index('\t/* fesphy prepared' if prepared else '\t/*\n\t * if PHY was already',start)
                end=driver.index('\n\tif (!xgmac->phy->link)',begin)
                prefix=driver[begin:end]
                helper=recovery.PHY_PREPARE if prepared else ''
                self.assertIn(helper,new)
                code=helper+'\nstatic int start_under_test(struct udevice *dev)\n{\n'
                code+='struct xgmac_priv *xgmac=dev_get_priv(dev);\nint ret=0;\n'
                if not prepared:code+='int addr=-1;\n'
                code+=prefix+'\nreturn 0;\nerr_shutdown_phy: phy_shutdown(xgmac->phy);\nerr_stop_resets: return ret ? ret : -EIO;\n}\n'
                (directory/'driver.inc').write_text(code)
                executable=directory/('prepared' if prepared else 'vendor')
                subprocess.run([shutil.which('cc'),'-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                    '-fno-sanitize-recover=all','-I',str(directory),'-DFES_TEST_PREPARED='+str(int(prepared)),
                    str(ROOT/'platform/embedded/firmware/tests/nano_recovery_phy_test.c'),'-o',str(executable)],
                    capture_output=True,check=True,timeout=30)
                for fault in range(14) if prepared else (0,):
                    with self.subTest(prepared=prepared,fault=fault):
                        subprocess.run([str(executable),str(fault)],capture_output=True,check=True,timeout=5)



if __name__ == '__main__':
    unittest.main()
