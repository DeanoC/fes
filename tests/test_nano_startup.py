"""Native startup admission: identities, compiled DT override and failure modes."""
from copy import deepcopy
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('nano_startup',ROOT/'platform/embedded/nano/startup.py')
startup=importlib.util.module_from_spec(spec);spec.loader.exec_module(startup)


class NativeStartupTests(unittest.TestCase):
    def launcher(self):
        return dict(native_program='fpgaload',selected_programs=True,tenfoot_serial=True,
                    computer_storage_return=True,hps_menu_program={'sealed':'fixture'})

    def config(self,auto):
        value=startup.policy(auto)
        return ('CONFIG_BOOTCOMMAND="fesboot"\nCONFIG_BOOTDELAY='+str(value['bootdelay'])+'\n'+
                ''.join('CONFIG_'+name+'=y\n' for name in startup.ENABLE))

    def test_manual_and_autoboot_have_separate_source_bound_identities(self):
        manual=startup.select(False,{})
        auto=startup.select(True,self.launcher())
        self.assertEqual(manual['bootdelay'],-1)
        self.assertEqual(auto['bootdelay'],2)
        sources={'producer':'a'*64}
        self.assertNotEqual(startup.identity(sources,'b'*64,'c'*64,manual),
                            startup.identity(sources,'b'*64,'c'*64,auto))
        self.assertNotEqual(startup.identity(sources,'b'*64,'c'*64,auto),
                            startup.identity(sources,'b'*64,'d'*64,auto))

    def test_only_explicit_complete_launcher_selection_can_autoboot(self):
        for field in self.launcher():
            for replacement in (None,False,1,'true'):
                value=self.launcher();value[field]=replacement
                with self.subTest(field=field,replacement=replacement):
                    with self.assertRaises(ValueError):startup.select(True,value)
        for bad in (None,0,1,'true'):
            with self.assertRaises(ValueError):startup.select(bad,self.launcher())

    def test_missing_changed_or_extra_policy_is_rejected(self):
        for auto in (False,True):
            good=startup.policy(auto)
            bad_values=[None,{},dict(good,unexpected=True),dict(good,bootdelay=True),
                        dict(good,bootdelay=-2),dict(good,bootcommand='run distro_bootcmd')]
            for key in good:
                missing=deepcopy(good);missing.pop(key);bad_values.append(missing)
            for value in bad_values:
                with self.subTest(auto=auto,value=value):
                    with self.assertRaises(ValueError):startup.checked(value)

    def test_actual_config_must_match_policy_with_no_alternate_or_retry_path(self):
        for auto in (False,True):
            value=startup.policy(auto);config=self.config(auto)
            self.assertEqual(startup.check_config(value,config),value)
            mutations=[self.config(not auto),config.replace('"fesboot"','"run bootcmd"'),
                       config+'CONFIG_BOOTDELAY=-2\n']
            mutations += [config.replace('CONFIG_'+name+'=y\n','') for name in startup.ENABLE]
            mutations += [config+'CONFIG_'+name+'=y\n' for name in startup.DISABLE]
            for bad in mutations:
                with self.subTest(auto=auto,config=bad):
                    with self.assertRaises(ValueError):startup.check_config(value,bad)

    @unittest.skipUnless(shutil.which('dtc') and shutil.which('fdtget'),'device-tree tools required')
    def test_real_compiled_device_tree_override_must_match_startup_policy(self):
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp)/'boot.dts';dtb=Path(temp)/'boot.dtb'
            for auto in (False,True):
                value=startup.policy(auto)
                source.write_text('/dts-v1/;\n/ {};\n'+startup.dts(value))
                subprocess.run(['dtc','-I','dts','-O','dtb','-o',str(dtb),str(source)],check=True,capture_output=True)
                startup.check_dtb(value,dtb)
                with self.assertRaises(ValueError):startup.check_dtb(startup.policy(not auto),dtb)
            source.write_text('/dts-v1/;\n/ {};\n')
            subprocess.run(['dtc','-I','dts','-O','dtb','-o',str(dtb),str(source)],check=True,capture_output=True)
            with self.assertRaises(ValueError):startup.check_dtb(startup.policy(True),dtb)

    def test_cli_rejects_autoboot_without_native_or_with_recovery_before_io(self):
        args=['python3',str(ROOT/'platform/embedded/firmware/build_terasic_uboot.py'),
              '--reference-repo','/missing','--bl31-build','/missing',
              '--cross-prefix','/missing-','--output','/missing','--native-autoboot']
        for extra in ([],['--network-recovery'],['--network-recovery','--native-entry-build','/missing']):
            result=subprocess.run(args+extra,capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,2,result.stderr)
            self.assertRegex(result.stderr,'native autoboot requires|separate boot profiles')

    @unittest.skipUnless(shutil.which('dtc') and shutil.which('fdtget'),'device-tree tools required')
    def test_package_admission_rechecks_startup_after_other_receipts_match(self):
        # Entry derivation is a separate checked boundary. Synthetic regular
        # files here exercise package admission only and cannot admit hardware.
        import sys
        sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
        try:
            spec=importlib.util.spec_from_file_location('startup_package',ROOT/'platform/embedded/nano/package.py')
            package=importlib.util.module_from_spec(spec);spec.loader.exec_module(package)
            import prepare_uboot
        finally:sys.path.pop(0)
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);uboot=base/'uboot';entry=base/'entry'
            (uboot/'build/spl').mkdir(parents=True);entry.mkdir()
            fit=uboot/'build/u-boot.itb';fit.write_bytes(b'not a physical FIT')
            spl=uboot/'build/spl/u-boot-spl-dtb.hex';spl.write_bytes(b'not a physical SPL')
            image=entry/'entry.img';image.write_bytes(b'not an executable')
            profile=base/'profile';profile.write_bytes(b'entry profile fixture')
            dtb=uboot/'extracted-fdt-0.bin';dts=base/'boot.dts'
            selected=startup.policy(True)
            dts.write_text('/dts-v1/;\n/ {};\n'+startup.dts(selected))
            subprocess.run(['dtc','-I','dts','-O','dtb','-o',str(dtb),str(dts)],check=True,capture_output=True)
            e=self.launcher();e.update(target='de25-nano',entry_image_sha256=package.sha(image))
            (entry/'result.json').write_text(json.dumps(e))
            producer=ROOT/'platform/embedded/nano/startup.py'
            sources={str(producer.relative_to(ROOT)):package.sha(producer)}
            build_id=startup.identity(sources,package.sha(image),'a'*64,selected)
            config=self.config(True)+''.join('CONFIG_'+n+'=y\n' for n in prepare_uboot.ENABLE if n not in startup.ENABLE)
            config+='CONFIG_LOCALVERSION="-fes-nano-'+build_id+'"\n'
            config+='CONFIG_SPL_FS_LOAD_PAYLOAD_NAME="FES/UPLOAD.BIN"\nCONFIG_SYS_MMCSD_FS_BOOT_PARTITION=1\n'
            config+='CONFIG_SPL_BSS_START_ADDR=0xbff00000\nCONFIG_SPL_BSS_MAX_SIZE=0x100000\n'
            config+='CONFIG_SPL_ATF_NO_PLATFORM_PARAM=y\n# CONFIG_SPL_ATF_LOAD_IMAGE_V2 is not set\n'
            (uboot/'build/.config').write_text(config)
            native=dict(startup=selected,build_id=build_id,producer_sources=sources,preloaded_entry=True,
                sealed_fit_sha256=package.sha(fit),sealed_fit_size=fit.stat().st_size,experimental_fit_path='FES/UPLOAD.BIN',
                entry_image_sha256=package.sha(image),entry_receipt_sha256=package.sha(entry/'result.json'),
                bl31_sha256='a'*64,profile_sha256=package.sha(profile),cold_boot_profile={'spl_atf_trace':True},
                atf_handoff=prepare_uboot.atf_descriptor('a'*64,16))
            record=dict(status='built',bl31_bridge_mode='owned-cold-serial',fit_sha256=package.sha(fit),
                spl_sha256=package.sha(spl),fit_atf_verified=True,bl31_sha256='a'*64,
                config_sha256=package.sha(uboot/'build/.config'),producer_sha256=package.sha(ROOT/'platform/embedded/firmware/build_terasic_uboot.py'),
                native_handoff=native,fit_components={'fes-entry':{'sha256':package.sha(image)},
                    'atf':{'sha256':'a'*64,'size':16},'fdt-0':{'sha256':package.sha(dtb)}})
            def admit(value):
                (uboot/'result.json').write_text(json.dumps(value))
                with patch.object(package,'checked_entry'),patch.object(package,'selected_profile',return_value=profile):
                    return package.checked_native(uboot,entry)
            admit(record)
            for mode in ('missing-policy','manual-policy','identity','tree-hash','config-delay','tree-delay'):
                altered=deepcopy(record)
                if mode=='missing-policy':altered['native_handoff'].pop('startup')
                elif mode=='manual-policy':altered['native_handoff']['startup']=startup.policy(False)
                elif mode=='identity':altered['native_handoff']['build_id']='0'*16
                elif mode=='tree-hash':altered['fit_components']['fdt-0']['sha256']='0'*64
                elif mode=='config-delay':
                    (uboot/'build/.config').write_text(config.replace('CONFIG_BOOTDELAY=2','CONFIG_BOOTDELAY=-1'))
                    altered['config_sha256']=package.sha(uboot/'build/.config')
                elif mode=='tree-delay':
                    dts.write_text('/dts-v1/;\n/ {};\n'+startup.dts(startup.policy(False)))
                    subprocess.run(['dtc','-I','dts','-O','dtb','-o',str(dtb),str(dts)],check=True,capture_output=True)
                    altered['fit_components']['fdt-0']['sha256']=package.sha(dtb)
                with self.subTest(mode=mode):
                    with self.assertRaises(ValueError):admit(altered)
                (uboot/'build/.config').write_text(config)


if __name__=='__main__':unittest.main()
