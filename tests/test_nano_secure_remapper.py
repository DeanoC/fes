"""Execute the real remapper with physical/secure bases and interrupted accesses."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

from test_nano_start_trace import ROOT,FW,trace,prepare_nano_owned,verify
sys.path.insert(0,str(FW))
import nano_remapper as remapper
import nano_firmware_mmu as mmu


class Tests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.ref=Path(os.environ.get('FES_NANO_TFA_ROOT',str(ROOT/'out/terasic-git-restore-tfa')))
        p=cls.ref/trace.SOURCE
        if not p.is_file():raise unittest.SkipTest('exact vendor source required')
        cls.original=trace.observed(prepare_nano_owned.service_source(p.read_bytes(),cold_serial=True))
        cls.selected=remapper.secure_source(cls.original)
        cls.start=dict(mode=trace.MODE,input_sha256=trace.INPUT_SHA256,
                       generated_sha256=hashlib.sha256(cls.original).hexdigest())

    def test_exact_bindings_validate_before_any_source_is_published(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);self.assertIsNone(remapper.install(root,None,None));self.assertEqual(list(root.iterdir()),[])
            p=root/remapper.SOURCE;p.parent.mkdir(parents=True);p.write_bytes(self.original)
            fixture=root/'selected-mmu-fixture';fixture.write_bytes(b'unit source-binding fixture, never a compiled image')
            binding=dict(contract=dict(mmu.RUNTIME_CONTRACT),sources={'selected-mmu-fixture':hashlib.sha256(fixture.read_bytes()).hexdigest()})
            wrong=[({},self.start),(dict(binding,contract={}),self.start),
                   (dict(binding,sources={}),self.start),(dict(binding,sources={'selected-mmu-fixture':'0'*64}),self.start),
                   (binding,None),(binding,dict(self.start,mode='unbound')),
                   (binding,dict(self.start,input_sha256='0'*64)),(binding,dict(self.start,generated_sha256='0'*64))]
            for selected,start in wrong:
                with self.subTest(selected=selected,start=start),self.assertRaises(ValueError):remapper.install(root,selected,start)
                self.assertEqual(p.read_bytes(),self.original)
            for source in (self.original+b'\n',self.original.replace(b'intel_smmu_hps_remapper_init(0U)',b'intel_smmu_hps_remapper_init(1U)')):
                p.write_bytes(source)
                with self.assertRaises(ValueError):remapper.install(root,binding,self.start)
                self.assertEqual(p.read_bytes(),source)
            p.write_bytes(self.original);record=remapper.install(root,binding,self.start)
            self.assertEqual(p.read_bytes(),self.selected)
            self.assertEqual(record['generated_sha256'],hashlib.sha256(self.selected).hexdigest())
            self.assertEqual((record['control_reads_added'],record['control_writes_added'],record['retries_added']),(0,0,0))
            self.assertEqual(record['offsets'],remapper.REGISTERS)
            with self.assertRaises(ValueError):remapper.install(root,binding,self.start)
            self.assertEqual(p.read_bytes(),self.selected)

    def test_actual_original_branches_values_pointer_updates_and_interrupted_accesses_match(self):
        header=(self.ref/'plat/intel/soc/agilex5/include/agilex5_system_manager.h').read_text()
        defines=[]
        for name,value in remapper.REGISTERS.items():
            match=re.search(r'^#define SOCFPGA_SYSMGR_'+name+r'\s+(0x[0-9a-fA-F]+)\b',header,re.M)
            self.assertIsNotNone(match);self.assertEqual(int(match[1],16),value)
            defines.append('#define SOCFPGA_SYSMGR_'+name+' '+match[1]+'U')
        platform=(self.ref/'plat/intel/soc/agilex5/include/socfpga_plat_def.h').read_text()
        self.assertRegex(platform,r'#define DRAM_BASE\s+\(0x80000000\)')
        prefix=r'''
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <setjmp.h>
#define DRAM_BASE 0x80000000UL
#define SOCFPGA_SYSMGR(name) (0x10d12000UL+SOCFPGA_SYSMGR_##name)
static uint64_t cpu_state,address;
static uint32_t boot_bits,g_remapper_bypass;
static unsigned calls,writes,base_reads,fault_at;
static jmp_buf interrupted;
uint64_t read_sctlr_el3(void) {base_reads++;return cpu_state;}
static uintptr_t physical(uintptr_t actual) {
#ifdef SELECTED
 uintptr_t base=(cpu_state&1U) ? 0xa0000UL:0x10d12000UL;
#else
 uintptr_t base=0x10d12000UL;
#endif
 assert(actual>=base && actual<base+4096);
 return 0x10d12000UL+actual-base;
}
static void access_event(void) {calls++;if(calls==fault_at)longjmp(interrupted,1);}
uint32_t mmio_read_32(uintptr_t actual) {
 uintptr_t p=physical(actual);assert(p==0x10d1225cUL);
 printf("READ %lx\n",(unsigned long)p);access_event();return boot_bits;
}
void mmio_write_32(uintptr_t actual,uint32_t value) {
 uintptr_t p=physical(actual);assert(p==(writes ? 0x10d12280UL:0x10d12284UL));
 assert(value==DRAM_BASE);printf("WRITE %lx %x\n",(unsigned long)p,value);writes++;access_event();
}
'''
        suffix=r'''
int main(int argc,char **argv) {
 assert(argc==7);cpu_state=strtoull(argv[1],NULL,0);boot_bits=(uint32_t)strtoul(argv[2],NULL,0);
 g_remapper_bypass=(uint32_t)strtoul(argv[3],NULL,0);unsigned pointer=(unsigned)strtoul(argv[4],NULL,0);
 address=strtoull(argv[5],NULL,0);fault_at=(unsigned)strtoul(argv[6],NULL,0);
 unsigned returned=0;
 if(setjmp(interrupted)==0) {intel_smmu_hps_remapper_init(pointer ? &address:NULL);returned=1;}
#ifdef SELECTED
 assert(base_reads==calls);
#else
 assert(base_reads==0);
#endif
 printf("RESULT returned=%u address=%llx calls=%u writes=%u bypass=%u\n",returned,(unsigned long long)address,calls,writes,g_remapper_bypass);return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);binaries=[]
            for name,source in [('original',self.original),('selected',self.selected)]:
                code=base/(name+'.c');binary=base/name
                text=source.decode();helper=''
                if name=='selected':
                    begin=text.index('static inline uintptr_t fes_nano_sdm_remapper_address(')
                    helper=text[begin:text.index('\n}',begin)+2]
                code.write_text(('\n#define SELECTED 1\n' if name=='selected' else '')+'\n'.join(defines)+prefix+helper+verify.function(source.decode(),'intel_smmu_hps_remapper_init')+suffix)
                r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',str(code),'-o',str(binary)],capture_output=True,text=True,timeout=30)
                self.assertEqual(r.returncode,0,r.stderr);binaries.append(binary)
            for state in (0,0x1000,1,0x81005):
                for boot in (0,2):
                    for bypass in (0,1):
                        for pointer,address in ((0,0),(1,0x80000000),(1,0x8aaab000)):
                            for fault in range(4):
                                args=[str(v) for v in (state,boot,bypass,pointer,address,fault)]
                                with self.subTest(args=args):
                                    old,new=[subprocess.run([str(b),*args],capture_output=True,text=True,timeout=5) for b in binaries]
                                    self.assertEqual(old.returncode,0,old.stderr);self.assertEqual(new.returncode,0,new.stderr);self.assertEqual(new.stdout,old.stdout)


if __name__=='__main__':unittest.main()
