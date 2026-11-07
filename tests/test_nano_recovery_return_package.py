"""Trace-aware build gates and regular-file packaging with explicit ELF mocks."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import recovery_return_package as package
import recovery
import recovery_return_trace as trace
import test_nano_recovery as recovery_tests
from test_nano_recovery_return import fixture

SYMBOLS=recovery_tests.NanoRecoveryTests.symbols+''.join(
    f' {i}: 0000000080280000 40 FUNC GLOBAL DEFAULT 1 {name}\n'
    for i,name in enumerate(('fes_recovery_return_begin','fes_recovery_return_mark','fes_recovery_return_end'),10))


def build_fixture(directory):
    source=directory/'source';phy=fixture(source);observed=trace.prepare(source)
    config=recovery_tests.NanoRecoveryTests().config()+''.join(s+'\n' for s in (
        'CONFIG_SYS_MMCSD_RAW_MODE_U_BOOT_SECTOR=0x200','CONFIG_SPL_FS_FAT=y','CONFIG_SYS_MMCSD_FS_BOOT=y',
        'CONFIG_SYS_MMCSD_FS_BOOT_PARTITION=1','CONFIG_SPL_FS_LOAD_PAYLOAD_NAME="u-boot.itb"'))
    blobs={'build/u-boot.itb':b'new trace FIT fixture','build/spl/u-boot-spl-dtb.hex':b'known SPL fixture',
        'build/.config':config.encode(),'build/u-boot':('U-Boot 2025.01'+recovery.LOCALVERSION+' (fixture; readelf mocked)').encode()}
    for name,data in blobs.items():
        p=directory/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(data)
    mode=dict(profile_sha256=package.sha(package.HERE/'recovery.py'),manual_prompt=True,automatic_writes=False,
        random_mac=False,sd_dma=False,native_handoff=False,phy=recovery.PHY,phy_driver_linked=True,phy_sources=phy)
    r=dict(status='built',native_handoff=None,network_recovery=mode,
        producer_sha256=package.sha(package.HERE.parent/'firmware/build_terasic_uboot.py'),
        fit_sha256=package.sha(directory/'build/u-boot.itb'),spl_sha256=package.sha(directory/'build/spl/u-boot-spl-dtb.hex'),
        config_sha256=package.sha(directory/'build/.config'),uboot_elf_sha256=package.sha(directory/'build/u-boot'),
        bl31_sha256=package.BASE_BL31,fit_atf_verified=True,network_return_trace=dict(
            profile_sha256=package.sha(package.HERE/'recovery_return_trace.py'),sources=observed,
            source_inverse_verified=True,phases=34,UART_poll_limit=100000,string_byte_limit=96,
            hardware_qualified=False,default_recovery_profile_compatible=False))
    (directory/'result.json').write_text(json.dumps(r));return r


class ReturnPackageTests(unittest.TestCase):
    @patch('subprocess.check_output',return_value=SYMBOLS)
    def test_actual_base_and_trace_validators_execute_on_reconstructed_source(self,readelf):
        with tempfile.TemporaryDirectory() as d:
            directory=Path(d)/'build';build_fixture(directory)
            package.checked_build(directory)
            self.assertGreaterEqual(readelf.call_count,2)
            with self.assertRaises(ValueError):package.base.checked_build(directory)

    @patch('subprocess.check_output',return_value=SYMBOLS)
    def test_changed_trace_source_identity_flags_and_artifacts_reject(self,_readelf):
        cases=['missing-mode','profile','bound','phases','physical','default-compatible','producer','elf-hash','config-hash','fit-hash','SPL-hash','unsealed-FIT','source','self-consistent-source']
        for case in cases:
            with self.subTest(case=case),tempfile.TemporaryDirectory() as d:
                directory=Path(d)/'build';r=build_fixture(directory)
                if case=='missing-mode':r.pop('network_return_trace')
                if case=='profile':r['network_return_trace']['profile_sha256']='0'*64
                if case=='bound':r['network_return_trace']['UART_poll_limit']+=1
                if case=='phases':r['network_return_trace']['phases']-=1
                if case=='physical':r['network_return_trace']['hardware_qualified']=True
                if case=='default-compatible':r['network_return_trace']['default_recovery_profile_compatible']=True
                for key,name in [('producer','producer_sha256'),('elf-hash','uboot_elf_sha256'),('config-hash','config_sha256'),('fit-hash','fit_sha256'),('SPL-hash','spl_sha256')]:
                    if case==key:r[name]='0'*64
                if case=='unsealed-FIT':r['fit_atf_verified']=False
                if case in ('source','self-consistent-source'):
                    p=directory/'source/net/net.c';p.write_text(p.read_text()+'/* foreign call */\n')
                    if case=='self-consistent-source':r['network_return_trace']['sources']['prepared']['net/net.c']=package.sha(p)
                (directory/'result.json').write_text(json.dumps(r))
                with self.assertRaises(ValueError):package.checked_build(directory)

    @unittest.skipUnless(all(shutil.which(n) for n in ('mformat','mmd','mcopy')),'mtools required for file-only image checks')
    @patch('subprocess.check_output',return_value=SYMBOLS)
    def test_real_regular_file_images_tool_dispatch_and_explicit_trace_loader(self,_readelf):
        with tempfile.TemporaryDirectory(dir=ROOT/'out',prefix='return-package-fixture-') as d:
            root=Path(d);build=root/'build';r=build_fixture(build);baseline=root/'baseline';baseline.mkdir()
            (baseline/'u-boot.itb').write_bytes(b'old recovery FIT fixture');(baseline/'network-recovery.sof').write_bytes(b'known qualified SOF fixture')
            m=dict(format=1,fit_sha256=package.sha(baseline/'u-boot.itb'),spl_sha256=r['spl_sha256'],bl31_sha256=r['bl31_sha256'],
                sof_sha256=package.sha(baseline/'network-recovery.sof'),fat32_start_sector=32768,fat32_sectors=262144,
                native_handoff=False,automatic_writes=False,spl_boot_mode='fat',spl_payload_path='u-boot.itb',spl_fat_partition=1,
                recovery_phy=recovery.PHY,phy_driver_linked=True)
            (baseline/'RECOVERY.JSON').write_text(json.dumps(m))
            tools={n:Path(shutil.which(n)).absolute() for n in ('mformat','mmd','mcopy')}
            def run(_label,argv):subprocess.run([str(x) for x in argv],check=True,capture_output=True,timeout=30)
            disk=package.base.make_card(baseline,baseline/'u-boot.itb',tools,run)
            (baseline/'result.json').write_text(json.dumps(dict(m,status='prepared',sd_image_size=disk.stat().st_size,sd_image_sha256=package.sha(disk))))
            with patch.multiple(package,BASE_FIT=m['fit_sha256'],BASE_SPL=m['spl_sha256'],BASE_SOF=m['sof_sha256']):
                output=root/'output';record=package.prepare(build,baseline,output)
                loaded=package.load_kit(output)
                self.assertEqual(loaded['scope'],package.MODE);self.assertTrue(record['prefix_change_only_fit_mirror'])
                self.assertEqual(record['sof_sha256'],m['sof_sha256'])
                self.assertEqual(set(record['tools']),{'mformat','mmd','mcopy'})
                with self.assertRaises(ValueError):package.load_base_kit(output)
                original=(output/'result.json').read_text();manifest=(output/'RECOVERY.JSON').read_text()
                for case in ('generic-status','missing-source','outside-build','wrong-trace','wrong-tool','missing-manifest-field','manifest-native','wrong-image','wrong-SOF'):
                    value=json.loads(original);(output/'RECOVERY.JSON').write_text(manifest)
                    if case=='generic-status':value['status']='prepared'
                    if case=='missing-source':value['inputs']={}
                    if case=='outside-build':value['uboot_build']='../../outside'
                    if case=='wrong-trace':value['return_observations']['phases']=33
                    if case=='wrong-tool':value['tools']['mcopy']['resolved_path']='/usr/bin/false'
                    if case=='wrong-image':value['sd_image_sha256']='0'*64
                    if case=='wrong-SOF':value['sof_sha256']='0'*64
                    if case in ('missing-manifest-field','manifest-native'):
                        bad=json.loads(manifest)
                        if case=='missing-manifest-field':bad.pop('native_handoff')
                        else:bad['native_handoff']=True;value['native_handoff']=True
                        (output/'RECOVERY.JSON').write_text(json.dumps(bad))
                    (output/'result.json').write_text(json.dumps(value))
                    with self.subTest(case=case),self.assertRaises((ValueError,OSError)):package.load_kit(output)


if __name__=='__main__':unittest.main()
