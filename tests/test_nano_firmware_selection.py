"""Selected OCRAM contracts, early-entry transform and actual ELF admission."""
import copy
import hashlib
import importlib.util
from pathlib import Path
import struct
import sys
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
sys.path.insert(0,str(FW))
try:
    import nano_firmware_ram as ram
    import build_terasic as build
finally:
    sys.path.pop(0)
import test_nano_firmware_ram as base_fixture
prepare=base_fixture.prepare


def profile():
    return dict(sd_pio=True,usb_pio=True,usb_shared=True,hps_rom={'bytes':8192},
        cpu_mapping=dict(format=2,scope='pinned-tamago-el1-resources-excluded-v2',firmware_base=0x50000,firmware_bytes=0x10000))


def elf():
    image=bytearray(0x800);image[:7]=b'\x7fELF\x02\x01\x01'
    struct.pack_into('<HHI',image,16,2,183,1)
    struct.pack_into('<QQQ',image,24,0x80000000,64,0x700)
    struct.pack_into('<HHHHHH',image,52,64,56,2,64,3,0)
    struct.pack_into('<IIQQQQQQ',image,64,1,6,0,0x50000,0x50000,0,0xdec0,1)
    struct.pack_into('<IIQQQQQQ',image,120,1,7,0x200,0x80000000,0x80000000,0x80,0x80,1)
    image[0x210:0x230]=bytes.fromhex('42'*32)
    values=[('bl31_entrypoint',0x80000000,16),('__NOBITS_START__',0x50000,0),
        ('__NOBITS_END__',0x5dec0,0),('__STACKS_START__',0x50000,0),('__STACKS_END__',0x58000,0),
        ('__BSS_START__',0x58000,0),('__BSS_END__',0x5dec0,0),
        ('fes_nano_ram_attempt',0x80000040,8),('fes_nano_ram_expected_chain',0x80000010,32)]
    strings=bytearray(b'\0');positions={}
    for i,(name,value,size) in enumerate(values,1):
        offset=len(strings);strings.extend(name.encode()+b'\0')
        struct.pack_into('<IBBHQQ',image,0x400+i*24,offset,0x11,0,0xfff1,value,size)
        positions[name]=0x400+i*24
    assert len(strings)<=0x100
    image[0x300:0x300+len(strings)]=strings
    struct.pack_into('<IIQQQQIIQQ',image,0x740,0,3,0,0,0x300,len(strings),0,0,1,0)
    struct.pack_into('<IIQQQQIIQQ',image,0x780,0,2,0,0,0x400,24*(len(values)+1),1,1,8,24)
    return image,positions


class SelectedFirmwareRAMTests(unittest.TestCase):
    run_c=base_fixture.NanoFirmwareRamTests.run_c

    def test_selection_requires_exact_native_exclusion_and_shared_rom(self):
        p=profile();self.assertEqual(ram.selected(p,True),ram.CONTRACT)
        self.assertIsNone(ram.selected({},False));self.assertIsNone(ram.checked(None))
        p['firmware_ram']=ram.selected(p,True);self.assertEqual(ram.checked(p),ram.CONTRACT)
        for key in ('sd_pio','usb_pio','usb_shared','hps_rom','cpu_mapping'):
            q=copy.deepcopy(p);q.pop(key)
            with self.subTest(missing=key),self.assertRaises(ValueError):ram.selected(q,True)
        for key,value in [('format',1),('scope','old'),('firmware_base',0x60000),('firmware_bytes',0x20000)]:
            q=copy.deepcopy(p);q['cpu_mapping'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):ram.selected(q,True)
        for key in ram.CONTRACT:
            q=copy.deepcopy(p);q['firmware_ram'][key]=True
            with self.subTest(contract=key),self.assertRaises(ValueError):ram.checked(q)
        for selected in (None,1,'true',{}):
            with self.subTest(selected=selected),self.assertRaises(ValueError):ram.selected(p,selected)

    def test_transform_puts_guard_before_stash_and_vendor_initialization(self):
        fixture='func bl31_entrypoint\n\tmov x20, x0\n\tel3_entrypoint_common\nendfunc bl31_entrypoint\n'
        with patch.object(ram,'ENTRY_SHA',hashlib.sha256(fixture.encode()).hexdigest()):
            result=ram.entry_source(fixture,'42'*32)
            self.assertLess(result.index('#include "nano_firmware_entry.inc"'),result.index('mov x20'))
            self.assertIn('.quad 0',result);self.assertIn('.data.fes_ram_attempt',result)
            with self.assertRaises(ValueError):ram.entry_source(fixture+'\n','42'*32)
        with self.assertRaises(ValueError):ram.entry_source(fixture,'42'*32)
        for chain in (None,'00'*32,'42'*31,'X'*64):
            with self.assertRaises(ValueError):ram.assembly_header(chain)

    def test_selected_elf_requires_layout_and_initialized_matching_chain_latch(self):
        original,locations=elf()
        def checked(image,**kwargs):
            return build.bl31_load_segments(image,firmware_ram=dict(ram.CONTRACT),cold_chain='42'*32,**kwargs)
        self.assertEqual(checked(original),(0x80000000,[(0x50000,0x5dec0),(0x80000000,0x80000080)]))
        with self.assertRaises(ValueError):build.bl31_load_segments(original)
        for chain in (None,'43'*32,'00'*32):
            with self.subTest(chain=chain),self.assertRaises(ValueError):
                build.bl31_load_segments(original,firmware_ram=dict(ram.CONTRACT),cold_chain=chain)
        for key,value in [('base',0x40000),('cookie',True),('format',True)]:
            with self.assertRaises(ValueError):
                build.bl31_load_segments(original,firmware_ram=dict(ram.CONTRACT,**{key:value}),cold_chain='42'*32)
        changes=[(68,'I',7),(80,'Q',0x50008),(96,'Q',1),(104,'Q',0xdec1),
                 (24,'Q',0x50000),(24,'Q',0x80000080),(160,'Q',0x300000),
                 (60,'H',0),(0x240,'B',1),(0x210,'B',0)]
        for name,value in [('__STACKS_START__',0x50010),('__STACKS_END__',0x57000),
                           ('__BSS_START__',0x57000),('__BSS_END__',0x60001),
                           ('fes_nano_ram_attempt',0x50000)]:
            changes.append((locations[name]+8,'Q',value))
        for offset,kind,value in changes:
            image=original.copy();struct.pack_into('<'+kind,image,offset,value)
            with self.subTest(offset=offset,value=value),self.assertRaises(ValueError):checked(image)

    def test_selected_spl_caller_checks_then_passes_private_fifth_argument(self):
        fixture='''
typedef void __noreturn (*atf_entry_t)(struct bl31_params *params, void *plat_params);
static void __noreturn entry(uintptr_t target) {
    atf_entry_t atf_entry=(atf_entry_t)target;
    void *bl31_params=(void *)11;uintptr_t fdt_addr=0;
    uintptr_t bl31_entry=target,bl32_entry=0,bl33_entry=0x80200000;
\tatf_entry(bl31_params, (void *)fdt_addr);
}
'''
        generated=prepare.atf_source(fixture,firmware_ram=True,firmware_ocram=True)
        prefix='''
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#include <stdlib.h>
#define __noreturn __attribute__((noreturn))
struct bl31_params { unsigned value; };
static unsigned stage,mode,branched,parked;
static jmp_buf stopped;
void __attribute__((noreturn)) hang(void) { assert(mode && stage==3);parked=1;longjmp(stopped,1); }
'''
        suffix='''
const void *fes_nano_spl_handoff(void) { assert(stage++==0);return (void *)22; }
void fes_nano_spl_atf_trace(uintptr_t t,uintptr_t b32,uintptr_t b33,uintptr_t fdt,uintptr_t p,uintptr_t cold) {
    assert(stage++==1 && t && !b32 && b33==0x80200000 && !fdt && p==11 && cold==22);
}
int fes_nano_spl_firmware_ram_guard(uintptr_t p,uintptr_t cold) { assert(stage++==2 && p==11 && cold==22);return mode?-1:0; }
static void __attribute__((noreturn)) destination(struct bl31_params *p,void *fdt,const void *cold,unsigned long cookie,unsigned long ram_cookie) {
    assert(stage++==3 && (uintptr_t)p==11 && !fdt && (uintptr_t)cold==22 && cookie==FES_NANO_COLD_COOKIE && ram_cookie==FES_NANO_FW_RAM_COOKIE);
    branched=1;longjmp(stopped,1);
}
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],NULL,0);
    if(!setjmp(stopped))entry((uintptr_t)destination);
    assert(stage==(mode?3:4) && branched==!mode && parked==!!mode);return 0;
}
'''
        self.run_c(prefix+generated+suffix,2)
        for bad in (None,1,'true'):
            with self.assertRaises(ValueError):prepare.atf_source(fixture,firmware_ram=True,firmware_ocram=bad)
        with self.assertRaises(ValueError):prepare.atf_source(fixture,firmware_ocram=True)


if __name__=='__main__':unittest.main()
