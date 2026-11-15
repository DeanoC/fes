"""Run actual mailbox control paths with physical and secure virtual bases."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from test_nano_isolation_io import PREFIX,SUFFIX,NAMES
from test_nano_mailbox import function,mailbox,ROOT,FW,prepare_tfa,DRIVER_FUNCTIONS,ENGINE_FUNCTIONS,PREFIX as FULL_PREFIX
sys.path.insert(0,str(FW))
import nano_firmware_mmu as mmu


class Tests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        ref=Path(os.environ.get('FES_NANO_TFA_ROOT',str(ROOT/'out/terasic-git-restore-tfa')))
        if not (ref/mailbox.SOURCE).exists():raise unittest.SkipTest('pinned vendor source required')
        cls.source=(ref/mailbox.SOURCE).read_bytes();cls.header=(ref/mailbox.HEADER).read_bytes()
        mailbox.checked(cls.source,cls.header)

    def test_selects_only_after_the_complete_bound_inputs_validate(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp)
            self.assertIsNone(mailbox.select_secure_base(root,None,None));self.assertEqual(list(root.iterdir()),[])
            source=root/mailbox.SOURCE;source.parent.mkdir(parents=True);source.write_bytes(self.source)
            header=root/mailbox.HEADER;header.parent.mkdir(parents=True,exist_ok=True);header.write_bytes(self.header)
            binding=mailbox.install(root,True)
            fixture=root/'selected-mmu-source';fixture.write_bytes(b'source-binding test fixture, never a firmware image')
            mmu_binding=dict(contract=dict(mmu.RUNTIME_CONTRACT),sources={'selected-mmu-source':hashlib.sha256(fixture.read_bytes()).hexdigest()})
            for wrong in (None,{},dict(mmu_binding,contract={}),dict(mmu_binding,sources={}),
                          dict(mmu_binding,sources={'selected-mmu-source':'0'*64})):
                if wrong is None:continue
                with self.subTest(wrong=wrong),self.assertRaises(ValueError):mailbox.select_secure_base(root,binding,wrong)
                self.assertEqual(header.read_bytes(),self.header)
            changed=source.read_bytes()+b'\n';source.write_bytes(changed)
            with self.assertRaises(ValueError):mailbox.select_secure_base(root,binding,mmu_binding)
            self.assertEqual(header.read_bytes(),self.header)
            source.write_bytes(changed[:-1]);selected=mailbox.select_secure_base(root,binding,mmu_binding)
            self.assertEqual(header.read_bytes(),mailbox.secure_header(self.header))
            self.assertEqual(selected['header_sha256'],hashlib.sha256(header.read_bytes()).hexdigest())
            self.assertEqual(selected['address_selection']['mode'],mailbox.ADDRESS_MODE)
            self.assertEqual(source.read_bytes(),changed[:-1])
            with self.assertRaises(ValueError):mailbox.select_secure_base(root,selected,mmu_binding)
            with self.assertRaises(ValueError):mailbox.secure_header(self.header+b'\n')

    def test_actual_driver_preserves_commands_events_delays_results_and_failure_states(self):
        backend=r'''
#define PHYSICAL_BASE 0x10a30000UL
static uintptr_t physical_address(uintptr_t address) {
#ifdef FES_TEST_SECURE_HEADER
 uintptr_t expected=(fake_sctlr&1U) ? 0x90000UL:PHYSICAL_BASE;
#else
 uintptr_t expected=PHYSICAL_BASE;
#endif
 assert(address>=expected && address<expected+0x500);
 return PHYSICAL_BASE+(address-expected);
}
'''
        prefix=PREFIX.replace('#include "socfpga_mailbox.h"','#include "arch_helpers.h"\n#include "socfpga_mailbox.h"')
        prefix=prefix.replace('uint32_t mmio_read_32(uintptr_t addr) {','uint32_t mmio_read_32(uintptr_t addr) {\n addr=physical_address(addr);')
        prefix=prefix.replace('void mmio_write_32(uintptr_t addr,uint32_t value) {','void mmio_write_32(uintptr_t addr,uint32_t value) {\n addr=physical_address(addr);')
        prefix=prefix.replace('MBOX_OFFSET','PHYSICAL_BASE').replace('static void event(',backend+'\nstatic void event(')
        suffix=SUFFIX.replace('assert(argc==2);mode=', 'assert(argc==3);fake_sctlr=strtoull(argv[2],NULL,0);mode=')
        suffix=suffix.replace(' assert(!mbox_write_lock);return 0;',r'''
 assert(!mbox_write_lock);
#ifdef FES_TEST_SECURE_HEADER
 assert(base_reads>0);
#else
 assert(base_reads==0);
#endif
 return 0;''')
        driver='\n'.join(function(self.source.decode(),n) for n in NAMES)
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);binaries=[]
            for name,header in [('original',self.header),('selected',mailbox.secure_header(self.header))]:
                inc=base/name;(inc/'lib').mkdir(parents=True)
                (inc/'lib/utils_def.h').write_text('#define BIT(n) (1U << (n))\n#define U(n) n##U\n#define ARRAY_SIZE(a) (sizeof(a)/sizeof((a)[0]))\n')
                (inc/'arch_helpers.h').write_text('#ifndef TEST_ARCH_HELPERS\n#define TEST_ARCH_HELPERS\n#include <stdint.h>\nstatic uint64_t fake_sctlr;\nstatic unsigned base_reads;\nstatic inline uint64_t read_sctlr_el3(void) {base_reads++;return fake_sctlr;}\n#endif\n')
                (inc/'socfpga_mailbox.h').write_bytes(header)
                code=base/(name+'.c');binary=base/(name+'-model')
                code.write_text(('#define FES_TEST_SECURE_HEADER 1\n' if name=='selected' else '')+prefix+driver+suffix)
                result=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-Wshadow','-Wno-sign-compare','-Wno-unused-parameter',
                    '-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(inc),str(code),'-o',str(binary)],capture_output=True,text=True,timeout=30)
                self.assertEqual(result.returncode,0,result.stderr);binaries.append(binary)
            for mode in range(10):
                old=subprocess.run([str(binaries[0]),str(mode),'0'],capture_output=True,text=True,timeout=5)
                self.assertEqual(old.returncode,0,old.stderr)
                for state in (0,0x1000,1,0x81005,0x30c5183f):
                    with self.subTest(mode=mode,sctlr=state):
                        new=subprocess.run([str(binaries[1]),str(mode),str(state)],capture_output=True,text=True,timeout=5)
                        self.assertEqual(new.returncode,0,new.stderr);self.assertEqual(new.stdout,old.stdout)

    def test_actual_full_parser_stream_wrap_credit_and_failure_paths_in_both_modes(self):
        ref=Path(os.environ.get('FES_NANO_TFA_ROOT',str(ROOT/'out/terasic-git-restore-tfa')))
        engine=prepare_tfa.contained_source((ref/prepare_tfa.SOURCE).read_bytes()).decode()
        parts=[function(self.source.decode(),name) for name in DRIVER_FUNCTIONS]
        parts += [function(engine,name) for name in ENGINE_FUNCTIONS]
        prototypes=''.join(part[:part.index('{')].rstrip()+';\n' for part in parts)
        model=(FW/'tests/nano_mailbox_test.c').read_text().replace('assert(argc==3);mode=', 'assert(argc==4);fake_sctlr=strtoull(argv[3],NULL,0);mode=')
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);inc=base/'include';(inc/'lib').mkdir(parents=True)
            (inc/'lib/utils_def.h').write_text('#define BIT(n) (1U << (n))\n#define U(n) n##U\n#define ARRAY_SIZE(a) (sizeof(a)/sizeof((a)[0]))\n')
            (inc/'arch_helpers.h').write_text('#include <stdint.h>\nstatic uint64_t fake_sctlr;\nstatic inline uint64_t read_sctlr_el3(void) {return fake_sctlr;}\n')
            (inc/'socfpga_mailbox.h').write_bytes(mailbox.secure_header(self.header))
            source=base/'stream.c';binary=base/'stream'
            source.write_text(FULL_PREFIX+prototypes+'\n'.join(parts)+(FW/'nano_mailbox_observe.inc').read_text()+model)
            r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-Wno-sign-compare','-Wno-unused-parameter',
                '-fsanitize=address,undefined','-fno-sanitize-recover=all','-I'+str(inc),'-I'+str(FW),str(source),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(r.returncode,0,r.stderr)
            for mode in range(9):
                for initial in range(16):
                    results=[]
                    for cpu_state in (0x1000,0x81005):
                        with self.subTest(mode=mode,initial=initial,cpu_state=cpu_state):
                            r=subprocess.run([str(binary),str(mode),str(initial),str(cpu_state)],capture_output=True,text=True,timeout=10)
                            self.assertEqual(r.returncode,0,r.stderr);results.append(r.stdout)
                    self.assertEqual(results[0],results[1])

if __name__=='__main__':unittest.main()
