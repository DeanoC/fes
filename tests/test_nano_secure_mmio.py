"""Execute the actual MMIO header and SMMU observer in both CPU states."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from test_nano_start_trace import ROOT,FW,trace,prepare_nano_owned
sys.path.insert(0,str(FW))
import nano_secure_mmio as secure
import nano_firmware_mmu as mmu


class Tests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        ref=Path(os.environ.get('FES_NANO_TFA_ROOT',str(ROOT/'out/terasic-git-restore-tfa')))
        if not (ref/secure.SOURCE).is_file():raise unittest.SkipTest('exact vendor header required')
        cls.original=(ref/secure.SOURCE).read_bytes();cls.selected=secure.selected_header(cls.original)
        cls.service=trace.observed(prepare_nano_owned.service_source((ref/trace.SOURCE).read_bytes(),cold_serial=True))
        cls.start=dict(mode=trace.MODE,input_sha256=trace.INPUT_SHA256,generated_sha256=hashlib.sha256(cls.service).hexdigest())

    def test_complete_bindings_and_exact_sources_validate_before_publication(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);self.assertIsNone(secure.install(root,None,None));self.assertEqual(list(root.iterdir()),[])
            p=root/secure.SOURCE;p.parent.mkdir(parents=True);p.write_bytes(self.original)
            service=root/trace.SOURCE;service.parent.mkdir(parents=True);service.write_bytes(self.service)
            fixture=root/'selected-mmu-fixture';fixture.write_bytes(b'unit source-binding fixture, never a firmware image')
            binding=dict(contract=dict(mmu.RUNTIME_CONTRACT),sources={'selected-mmu-fixture':hashlib.sha256(fixture.read_bytes()).hexdigest()})
            bad=[({},self.start),(dict(binding,contract={}),self.start),(dict(binding,sources={}),self.start),
                 (dict(binding,sources={'selected-mmu-fixture':'0'*64}),self.start),(binding,None),
                 (binding,dict(self.start,generated_sha256='0'*64)),(binding,dict(self.start,mode='unbound'))]
            for selected,start in bad:
                with self.subTest(selected=selected,start=start),self.assertRaises(ValueError):secure.install(root,selected,start)
                self.assertEqual(p.read_bytes(),self.original);self.assertFalse((root/secure.HEADER).exists())
            for changed in (self.original+b'\n',self.original.replace(b'uint32_t*)addr',b'uint32_t*)(addr+4)')):
                p.write_bytes(changed)
                with self.assertRaises(ValueError):secure.install(root,binding,self.start)
                self.assertEqual(p.read_bytes(),changed);self.assertFalse((root/secure.HEADER).exists())
            p.write_bytes(self.original);service.write_bytes(self.service+b'\n')
            with self.assertRaises(ValueError):secure.install(root,binding,self.start)
            self.assertFalse((root/secure.HEADER).exists());service.write_bytes(self.service)
            record=secure.install(root,binding,self.start);self.assertEqual(p.read_bytes(),self.selected)
            self.assertEqual(service.read_bytes(),self.service);self.assertEqual((root/secure.HEADER).read_bytes(),(FW/'nano_secure_mmio.h').read_bytes())
            self.assertEqual((record['control_reads_added'],record['control_writes_added'],record['retries_added']),(0,0,0));self.assertFalse(record['tcu_secure_bank']['write'])
            mmu.verify_installed(root,binding)
            with self.assertRaises(ValueError):secure.install(root,binding,self.start)
            self.assertEqual(p.read_bytes(),self.selected)

    def compile(self,base,selected,body,extra=()):
        directory=base/('selected' if selected else 'original');(directory/'lib').mkdir(parents=True)
        (directory/'lib/mmio.h').write_bytes(self.selected if selected else self.original)
        (directory/'lib/nano_secure_mmio.h').write_bytes((FW/'nano_secure_mmio.h').read_bytes())
        (directory/'arch_helpers.h').write_text('#include <stdint.h>\nextern uint64_t cpu_state;\nextern unsigned cpu_reads;\nstatic inline uint64_t read_sctlr_el3(void) {cpu_reads++;return cpu_state;}\n')
        source=directory/'model.c';source.write_text(('#define SELECTED 1\n' if selected else '')+body);binary=directory/'model'
        r=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(directory),'-I'+str(FW),str(source),*[str(x) for x in extra],'-o',str(binary)],capture_output=True,text=True,timeout=30)
        self.assertEqual(r.returncode,0,r.stderr);return binary

    def test_actual_accessors_widths_bit_operations_boundaries_and_unrelated_addresses(self):
        body=r'''
#define _GNU_SOURCE
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/mman.h>
#include <lib/mmio.h>
uint64_t cpu_state;unsigned cpu_reads;
static void page(uintptr_t a) {assert(mmap((void*)a,4096,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS|MAP_FIXED_NOREPLACE,-1,0)==(void*)a);}
int main(int argc,char **argv) {
 assert(argc==2);cpu_state=strtoull(argv[1],NULL,0);
 page(0x10d12000);page(0xa0000);page(0x16008000);page(0xb0000);page(0x30000000);
 uintptr_t target=0x10d12000;
#ifdef SELECTED
 if(cpu_state&1U)target=0xa0000;
#endif
 uintptr_t other=target==0xa0000?0x10d12000:0xa0000;
 assert(mprotect((void*)other,4096,PROT_NONE)==0);
 /* Direct physical inputs exercise bridge controls and the cold boot-kind read. */
 mmio_write_32(0x10d1205c,3);mmio_setbits_32(0x10d1205c,4);mmio_clrbits_32(0x10d1205c,1);mmio_clrsetbits_32(0x10d1205c,2,8);
 assert(mmio_read_32(0x10d1205c)==12 && *(volatile uint32_t*)(target+0x5c)==12);
 mmio_write_32(0x10d1220c,4U<<29);assert((mmio_read_32(0x10d1220c)>>29)==4);
 mmio_write_8(0x10d12fff,0x81);assert(mmio_read_8(0x10d12fff)==0x81);
 mmio_write_16(0x10d12ffe,0x9876);mmio_clrsetbits_16(0x10d12ffe,0x70,1);assert(mmio_read_16(0x10d12ffe)==0x9807);
 mmio_write_32(0x10d12ffc,0x12345678);assert(mmio_read_32(0x10d12ffc)==0x12345678);
 mmio_write_64(0x10d12ff8,0x123456789abcdef0ULL);assert(mmio_read_64(0x10d12ff8)==0x123456789abcdef0ULL);
 *(uint32_t*)0x16008004=0;*(uint32_t*)0xb0004=0x80000018U;
 assert(mprotect((void*)0xb0000,4096,PROT_READ)==0);
 uint32_t id=mmio_read_32(0x16008004);
#ifdef SELECTED
 assert(id==((cpu_state&1U)?0x80000018U:0));
 /* The secure-bank write must stay on its original physical page. */
#endif
 mmio_write_32(0x16008020,0x1357);assert(*(uint32_t*)0x16008020==0x1357 && *(uint32_t*)0xb0020==0);
 unsigned before=cpu_reads;mmio_write_64(0x30000008,0xabcdef9876543210ULL);assert(mmio_read_64(0x30000008)==0xabcdef9876543210ULL);assert(cpu_reads==before);
#ifdef SELECTED
 assert(cpu_reads>0);
 for(uintptr_t a=0x10d11ff8;a<=0x10d13008;a+=8) {
  uintptr_t expected=(cpu_state&1U)&&a>=0x10d12000&&a<0x10d13000?0xa0000+(a&4095):a;
  assert(fes_nano_secure_mmio_address(a,0)==expected && fes_nano_secure_mmio_address(a,1)==expected);
 }
 for(uintptr_t a=0x16007ff8;a<=0x16009008;a+=8) {
  uintptr_t expected=(cpu_state&1U)&&a>=0x16008000&&a<0x16009000?0xb0000+(a&4095):a;
  assert(fes_nano_secure_mmio_address(a,0)==expected && fes_nano_secure_mmio_address(a,1)==a);
 }
 before=cpu_reads;
 for(uintptr_t a=0x90000;a<0xc0000;a+=4096)assert(fes_nano_secure_mmio_address(a,0)==a);
 assert(cpu_reads==before);
#else
 assert(cpu_reads==0 && id==0);
#endif
 puts("PASS accessors, bit operations, direct cold/bridge addresses and unmapped neighbors");return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp)
            for selected in (False,True):
                binary=self.compile(base,selected,body)
                for state in (0,0x1000,1,0x81005):
                    r=subprocess.run([str(binary),str(state)],capture_output=True,text=True,timeout=5);self.assertEqual(r.returncode,0,r.stdout+r.stderr)

    def test_actual_smmu_engine_restores_secure_bank_visibility_and_keeps_ownership_failures(self):
        body=r'''
#define _GNU_SOURCE
#include <assert.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <sys/mman.h>
#include <lib/mmio.h>
#include "nano_smmu.h"
uint64_t cpu_state;unsigned cpu_reads;
static unsigned ownership_calls,fail_at;
static int owns(void *c) {(void)c;return ++ownership_calls==fail_at?0:1;}
static int read32(void *c,uint32_t offset,uint32_t *out) {(void)c;assert(fes_nano_smmu_offset(offset));*out=mmio_read_32(FES_NANO_SMMU_BASE+offset);return 0;}
static void page(uintptr_t a) {assert(mmap((void*)a,4096,PROT_READ|PROT_WRITE,MAP_PRIVATE|MAP_ANONYMOUS|MAP_FIXED_NOREPLACE,-1,0)==(void*)a);}
int main(int argc,char **argv) {
 assert(argc==3);cpu_state=strtoull(argv[1],NULL,0);fail_at=(unsigned)strtoul(argv[2],NULL,0);
 page(0x16000000);page(0x16008000);page(0xb0000);
 *(uint32_t*)0x16000000=0x080f7e3f;*(uint32_t*)0x16000004=0x0e739d18;*(uint32_t*)0x1600001c=1;
 *(uint32_t*)0x16008004=(cpu_state&1U)?0:0x80000018U;*(uint32_t*)0xb0004=0x80000018U;
 assert(mprotect((void*)0xb0000,4096,PROT_READ)==0);
 struct fes_nano_smmu_probe p={.io={NULL,owns,read32}};struct fes_nano_smmu_observation out={0};int r=fes_nano_smmu_observe(&p,&out);
 if(p.failed) {assert(r<0);unsigned reads=p.reads,cpu=cpu_reads;assert(fes_nano_smmu_observe(&p,&out)==r && p.reads==reads && cpu_reads==cpu);}
 else {
  assert(r==0 && out.identified && out.controls_settled && out.bypass_controls_clear);
  unsigned secure=!(cpu_state&1U);
#ifdef SELECTED
  secure=1;
#endif
  assert(out.secure_implemented==secure && out.registers.s_idr1==(secure?0x80000018U:0) && p.reads==(secure?26U:16U));
 }
 printf("PASS r=%d failed=%u reads=%u\n",r,p.failed,p.reads);return 0;
}
'''
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp)
            for selected in (False,True):
                binary=self.compile(base,selected,body,(FW/'nano_smmu.c',))
                for state in (0,1,0x81005):
                    for fail in (0,*range(1,54)):
                        r=subprocess.run([str(binary),str(state),str(fail)],capture_output=True,text=True,timeout=5);self.assertEqual(r.returncode,0,r.stdout+r.stderr)


if __name__=='__main__':unittest.main()
