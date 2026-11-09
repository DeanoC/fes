"""Selected MMU policy and complete boot-trace admission, without hardware."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path[:0]=[str(ROOT/'platform/embedded/firmware'),str(ROOT/'platform/embedded/nano')]
import nano_firmware_mmu as mmu
import mmu_trace
from test_nano_native_trace import trace,fixture,firmware_progress,atf_progress,BUILD,SEED
from prepare_uboot import atf_descriptor


def proof():
    root=0x8003c000
    return dict(selected_policy=dict(mmu.RUNTIME_CONTRACT),witness=0x5f000,witness_bytes=64,
        startup_cache_helper=dict(address=0x80023000,bytes=44,sha256=mmu_trace.CACHE_HELPER_SHA),
        layout=dict(tables_start=root,tables_end=root+24576,
            tables={name:root+i*4096 for i,name in enumerate(mmu.NAMES)}))


def row(phase):
    r=dict(el=12,midr=0x412fd050,mpidr=0x81000000,mmfr0=2,daif=0x2c0,scr=0x530,
        sctlr=0x30c5183a,tcr=0,root=0,mair=0,witness=0x5f000)
    if phase=='after':r.update(sctlr=r['sctlr']|0x80005,tcr=mmu.TCR,root=0x8003c000,mair=mmu.MAIR)
    return 'NOTICE:  FES_NANO_EL3_MMU phase='+phase+' '+\
        ' '.join(name+'='+format(r[name],'x') for name in mmu_trace.FIELDS)+\
        (' writes=4 memory=1\r\n' if phase=='after' else ' writes=0 memory=0\r\n')


def base_trace():
    anchor='NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
    descriptor=('FES_NANO_SPL ATF target=80000000 bl32=0 bl33=80200000 fdt=0 params=bff00008 cold=21768 '
        'cookie=464e433200000002 el=3 mpidr=81000000 sctlr=1000 scr=501 daif=3c0 bytes=90173 sha256='+'1'*64+'\r\n')
    return firmware_progress(fixture().replace(anchor,''.join(atf_progress())+descriptor+anchor))


def decode(raw,selected=True):
    return trace.decode_native_boot(raw,BUILD,SEED,atf=atf_descriptor('1'*64,90173),
        firmware_ram_trace=True,firmware_mmu=proof() if selected else None)


class NanoMMUBootTests(unittest.TestCase):
    def test_vendor_cache_repair_preserves_the_original_invalidation_and_returns(self):
        source='before\n'+mmu.VENDOR_CACHE_BODY+'\nafter\n'
        repaired=mmu.restore_vendor_cache(source)
        self.assertEqual(repaired.replace('\tmsr\tSCR_EL3, x0\n\tisb\n\tret\n',''),source)
        for bad in (source+mmu.VENDOR_CACHE_BODY,source.replace('ALLE2','ALLE1'),repaired):
            with self.assertRaises(ValueError):mmu.restore_vendor_cache(bad)

    def installation(self, source):
        # Small source exports exercise transformations and their hash binding.
        # The real vendor hashes are separately exercised by the matched build.
        files={mmu.LD:'before\n    __RW_START__ = . ;\n#else\n    __RW_START__ = . ;\n',
            mmu.COMMON_LD:'*(SORT_BY_ALIGNMENT(.bss*))\n',
            mmu.VENDOR_CACHE:mmu.VENDOR_CACHE_BODY+'\n',
            mmu.ram.ENTRY:'fes_ram_bss_clean\n\tfes_ram_uart_text fes_nano_ram_runtime_exit_before\n\tb\tel3_exit\n',
            'plat/intel/soc/agilex5/platform.mk':'BL31_SOURCES += original.c\n'}
        for name,text in files.items():
            path=source/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_text(text)
        (source/'plat/intel/soc/common/include').mkdir(parents=True)
        binding=dict(sources={mmu.ram.ENTRY:hashlib.sha256(files[mmu.ram.ENTRY].encode()).hexdigest()})
        return files,binding

    def test_install_composes_ram_binding_and_detects_every_changed_installed_file(self):
        with tempfile.TemporaryDirectory() as temp:
            source=Path(temp);files,ram_binding=self.installation(source)
            profile=dict(usb_shared=True,initial_tape=True,computer_reload=True,hps_menu_program={},
                         firmware_mmu=dict(mmu.RUNTIME_CONTRACT))
            with patch.object(mmu.ram,'checked',return_value=mmu.ram.CONTRACT),\
                    patch.object(mmu,'LD_SHA',hashlib.sha256(files[mmu.LD].encode()).hexdigest()),\
                    patch.object(mmu,'COMMON_LD_SHA',hashlib.sha256(files[mmu.COMMON_LD].encode()).hexdigest()),\
                    patch.object(mmu,'VENDOR_CACHE_SHA',hashlib.sha256(files[mmu.VENDOR_CACHE].encode()).hexdigest()):
                binding=mmu.install(source,profile,ram_binding)
            mmu.ram.verify_installed(source,ram_binding);mmu.verify_installed(source,binding)
            self.assertEqual(binding['prior_ram_entry_sha256'],hashlib.sha256(files[mmu.ram.ENTRY].encode()).hexdigest())
            entry=(source/mmu.ram.ENTRY).read_text()
            self.assertLess(entry.index('fes_ram_bss_clean'),entry.index('bl\tfes_nano_mmu_platform_boot'))
            self.assertIn('cbnz\tx0, .Lfes_ram_park\n\tb\tel3_exit',entry)
            self.assertIn('\tmsr\tSCR_EL3, x0\n\tisb\n\tret\nendfunc invalidate_cache_low_el',
                          (source/mmu.VENDOR_CACHE).read_text())
            for name in binding['sources']:
                with self.subTest(name=name):
                    path=source/name;original=path.read_bytes();path.write_bytes(original+b' changed')
                    with self.assertRaises(ValueError):mmu.verify_installed(source,binding)
                    path.write_bytes(original)
            with self.assertRaises(ValueError):mmu.verify_installed(source,dict(binding,contract={}))

    def test_install_rejects_stale_ram_or_unreviewed_vendor_source_before_writing(self):
        for changed in (mmu.ram.ENTRY,mmu.LD,mmu.COMMON_LD,mmu.VENDOR_CACHE):
            with self.subTest(changed=changed),tempfile.TemporaryDirectory() as temp:
                source=Path(temp);files,ram_binding=self.installation(source)
                (source/changed).write_text(files[changed]+' changed')
                before={str(p.relative_to(source)):p.read_bytes() for p in source.rglob('*') if p.is_file()}
                profile=dict(usb_shared=True,initial_tape=True,computer_reload=True,hps_menu_program={},
                             firmware_mmu=dict(mmu.RUNTIME_CONTRACT))
                with patch.object(mmu.ram,'checked',return_value=mmu.ram.CONTRACT),\
                        patch.object(mmu,'LD_SHA',hashlib.sha256(files[mmu.LD].encode()).hexdigest()),\
                        patch.object(mmu,'COMMON_LD_SHA',hashlib.sha256(files[mmu.COMMON_LD].encode()).hexdigest()),\
                        patch.object(mmu,'VENDOR_CACHE_SHA',hashlib.sha256(files[mmu.VENDOR_CACHE].encode()).hexdigest()):
                    with self.assertRaises(ValueError):mmu.install(source,profile,ram_binding)
                self.assertEqual(before,{str(p.relative_to(source)):p.read_bytes() for p in source.rglob('*') if p.is_file()})

    def test_policy_is_fixed_and_requires_complete_ram_and_role_selection(self):
        self.assertEqual(hashlib.sha256(json.dumps(mmu.RUNTIME_CONTRACT,sort_keys=True,separators=(',',':')).encode()).hexdigest(),mmu_trace.POLICY_SHA)
        self.assertEqual(hashlib.sha256(mmu.VENDOR_CACHE_CODE).hexdigest(),mmu_trace.CACHE_HELPER_SHA)
        p=dict(usb_shared=True,initial_tape=True,computer_reload=True,hps_menu_program={})
        with patch.object(mmu.ram,'checked',return_value=mmu.ram.CONTRACT):
            self.assertEqual(mmu.selected(p,True),mmu.RUNTIME_CONTRACT)
            for name in p:
                bad=dict(p);bad.pop(name)
                with self.subTest(name=name),self.assertRaises(ValueError):mmu.selected(bad,True)
            bad=dict(p,firmware_mmu=dict(mmu.RUNTIME_CONTRACT,mair=0x4404ff))
            with self.assertRaises(ValueError):mmu.checked(bad)
        with self.assertRaises(ValueError):mmu.selected(p,1)
        self.assertIsNone(mmu.selected({},False))

    def test_complete_post_cleanup_transition_is_required_before_loader(self):
        raw=base_trace().replace('U-Boot 2025.01-fes-nano-',row('before')+row('after')+'U-Boot 2025.01-fes-nano-',1)
        good=decode(raw);self.assertTrue(good['complete'],good['errors'])
        self.assertTrue(good['firmware_mmu_trace']['complete'])
        self.assertFalse(decode(raw,False)['loader_ready'])
        self.assertFalse(decode(base_trace())['loader_ready'])
        for rows in (row('before'),row('after'),row('after')+row('before'),row('before')*2+row('after'),row('before')+row('after')*2):
            damaged=raw.replace(row('before')+row('after'),rows)
            self.assertFalse(decode(damaged)['loader_ready'])
        early=raw.replace(row('before')+row('after'),'').replace('FES_NANO_RAM RUNTIME phase=main-before',row('before')+row('after')+'FES_NANO_RAM RUNTIME phase=main-before')
        self.assertFalse(decode(early)['loader_ready'])

    def test_every_register_memory_or_write_count_mismatch_rejects(self):
        rows=row('before')+row('after');raw=base_trace().replace('U-Boot 2025.01-fes-nano-',rows+'U-Boot 2025.01-fes-nano-',1)
        for name in (*mmu_trace.FIELDS,'writes','memory'):
            token=next(t for t in row('after').split() if t.startswith(name+'='))
            value=int(token.split('=')[1],16);replacement=name+'='+format(value^1,'x')
            damaged=raw.replace(row('after'),row('after').replace(token,replacement))
            with self.subTest(name=name):self.assertFalse(decode(damaged)['loader_ready'])

    def test_bad_proof_and_flooded_failed_records_never_recover(self):
        for key,value in (('witness',0x50000),('witness_bytes',128),('selected_policy',{}),
                          ('startup_cache_helper',{}),('startup_cache_helper',dict(address=0x80023000,bytes=44,sha256='0'*64))):
            p=proof();p[key]=value
            with self.assertRaises(ValueError):mmu_trace.parameters(p)
        s=mmu_trace.state();args=dict(expected=mmu_trace.parameters(proof()),startup_complete=True,
            exit_cache_complete=True,uboot_seen=False,prior_valid=True)
        self.assertIsNotNone(mmu_trace.observe(s,'FES_NANO_EL3_MMU FAIL status=-7',1,**args))
        for i in range(100):mmu_trace.observe(s,row('after').removeprefix('NOTICE:  ').strip(),i+2,**args)
        self.assertFalse(s['valid'] or s['complete']);self.assertLessEqual(len(s['records']),2)


if __name__=='__main__':unittest.main()
