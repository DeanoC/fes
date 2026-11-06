"""Exercise the actual final SPL exclusion check; no memory grant or board I/O."""
import importlib.util
import re
from pathlib import Path
import subprocess
import struct
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'
NANO = ROOT / 'platform/embedded/nano'


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


sys.path.insert(0, str(FW))
try:
    extract = module('firmware_ram_extract', FW / 'verify.py').function
    builder = module('firmware_ram_builder', FW / 'build_terasic.py')
finally:
    sys.path.pop(0)
prepare = module('firmware_ram_uboot', NANO / 'prepare_uboot.py')
PREFIX = '''
#include <assert.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "nano_firmware_ram.h"
static struct fes_nano_spl_ram_view canonical(void) {
    return (struct fes_nano_spl_ram_view){
        0x6e000,0x6ed00,0x300,0x6f000,0x2000,0x252a8,
        0x21e90,sizeof(struct fes_nano_cold_record),0xbff00008,
        0xbfa00000,0x500000};
}
'''


class NanoFirmwareRamTests(unittest.TestCase):
    def test_native_exclusion_uses_the_same_candidate_as_spl(self):
        header = (FW / 'nano_firmware_ram.h').read_text()
        values = {name: int(value, 16) for name, value in re.findall(
            r'#define FES_NANO_FW_RAM_(BASE|LIMIT) (0x[0-9a-fA-F]+)ULL', header)}
        native = ROOT / 'platform/embedded/tamago/board/arm64map'
        go = (native / 'inspect.go').read_text()
        base, size = (int(v, 16) for v in re.search(
            r'const FirmwareRAMBase, FirmwareRAMBytes = uint64\((0x[0-9a-f]+)\), uint64\((0x[0-9a-f]+)\)', go).groups())
        self.assertEqual((base, base + size), (values['BASE'], values['LIMIT']))
        assembly = (native / 'firmware_tamago.s').read_text()
        self.assertEqual(assembly.count('MOVD $'+hex(base)+', R13'), 2)
        self.assertIn('ADD $'+hex((base >> 12) * 8)+', R10, R8', assembly)
        self.assertIn('ADD $'+hex((size >> 12) * 8)+', R8, R9', assembly)
        board = (ROOT / 'platform/embedded/tamago/board/de25nano/board.go').read_text()
        self.assertLess(board.index('arm64map.ExcludeFirmwareRAM(profileRAMStart)'),
                        board.index('StartupCheckpoint("go_hwinit")'))
        devices = (ROOT / 'platform/embedded/tamago/board/de25nano/devices.go').read_text()
        self.assertLess(devices.index('arm64map.RequireFirmwareExclusion'),
                        devices.index('arm64map.NormalUncached'))

    def test_exclusion_guard_does_not_admit_separate_nobits_load(self):
        image = bytearray(176)
        image[:6] = b'\x7fELF\x02\x01'
        struct.pack_into('<H', image, 18, 183)
        struct.pack_into('<Q', image, 24, 0x80000000)
        struct.pack_into('<Q', image, 32, 64)
        struct.pack_into('<HH', image, 54, 56, 2)
        struct.pack_into('<IIQQQQQQ', image, 64, 1, 5, 0, 0x80000000, 0x80000000,
                         len(image), 0x1000, 0x1000)
        struct.pack_into('<IIQQQQQQ', image, 120, 1, 6, 0, 0x80001000, 0x80001000,
                         0, 0x1000, 0x1000)
        self.assertEqual(builder.bl31_load_segments(image)[0], 0x80000000)
        for base in (0x50000, 0x4f000, 0x60000):
            struct.pack_into('<QQ', image, 136, base, base)
            with self.subTest(base=base), self.assertRaises(ValueError):
                builder.bl31_load_segments(image)

    def run_c(self, source, cases=1):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'ram.c'
            binary = Path(temp) / 'ram'
            (Path(temp) / 'hang.h').write_text('void hang(void) __attribute__((noreturn));\n')
            path.write_text(source)
            built = subprocess.run(['cc', '-std=c11', '-O2', '-Wall', '-Wextra', '-Werror',
                                    '-fsanitize=undefined', '-fno-sanitize-recover=all',
                                    '-I', str(FW), '-I', temp, str(path), '-o', str(binary)],
                                   capture_output=True, text=True)
            self.assertEqual(built.returncode, 0, built.stderr)
            for case in range(cases):
                with self.subTest(case=case):
                    run = subprocess.run([str(binary), str(case)], capture_output=True, text=True)
                    self.assertEqual(run.returncode, 0, run.stderr)

    def test_actual_range_predicate_rejects_overlap_drift_alignment_and_overflow(self):
        self.run_c(PREFIX + r'''
int main(void) {
    struct fes_nano_spl_ram_view v=canonical();
    assert(fes_nano_spl_ram_excluded(&v));
    assert(!fes_nano_spl_ram_excluded(NULL));
#define BAD(field,value) do { v=canonical();v.field=(value);assert(!fes_nano_spl_ram_excluded(&v)); } while(0)
    BAD(sp,0x50000);BAD(sp,0x60000);BAD(sp,0x60ff0);BAD(sp,0x61001);
    BAD(sp,0x71000);BAD(sp,0x6ed10);BAD(sp,UINT64_MAX-15);
    BAD(gd_base,0x5fff0);BAD(gd_base,0x6ed01);BAD(gd_base,0x6f000);
    BAD(gd_base,UINT64_MAX-15);BAD(gd_bytes,0);BAD(gd_bytes,0x1001);
    BAD(gd_bytes,0x301);BAD(gd_bytes,UINT64_MAX);
    BAD(early_base,0x6e000);BAD(early_bytes,0x1fff);BAD(early_bytes,0x2001);
    BAD(image_bytes,0);BAD(image_bytes,0x40001);BAD(image_bytes,0x21f00);
    BAD(cold_base,0x21e91);BAD(cold_base,0);BAD(cold_base,0x50000);
    BAD(cold_base,UINT64_MAX-7);BAD(cold_bytes,0);BAD(cold_bytes,223);BAD(cold_bytes,225);
    BAD(params_base,0xbfeffff8);BAD(params_base,0xbfffffc8);
    BAD(params_base,0xbff00001);BAD(params_base,UINT64_MAX-7);
    BAD(heap_base,0xbfa00001);BAD(heap_base,0x50000);
    BAD(heap_bytes,0x500001);BAD(heap_bytes,UINT64_MAX);
    v=canonical();v.sp=FES_NANO_FW_RAM_STACK_FLOOR;assert(fes_nano_spl_ram_excluded(&v));
    v=canonical();v.params_base=0xbfffffc0;assert(fes_nano_spl_ram_excluded(&v));
    v=canonical();v.image_bytes=0x40000;assert(fes_nano_spl_ram_excluded(&v));
    assert(!fes_nano_ram_within(UINT64_MAX-7,16,0,UINT64_MAX));
    return 0;
}
''')

    def test_actual_guard_consumes_before_observation_and_never_replays(self):
        source = (NANO / 'spl_cold.c').read_text()
        guard = extract(source, 'fes_nano_spl_firmware_ram_guard')
        self.run_c(PREFIX + r'''
static unsigned ram_guard_attempted,observations,mode;
static int fes_nano_spl_ram_snapshot(uintptr_t params,uintptr_t cold,struct fes_nano_spl_ram_view *v) {
    assert(ram_guard_attempted==1 && !observations++ && params==11 && cold==22);
    *v=canonical();
    if(mode==1)return -1;
    if(mode==2)v->sp=0x50000;
    return 0;
}
''' + guard + r'''
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],NULL,0);
    if(mode==3)ram_guard_attempted=UINT32_MAX;
    assert(fes_nano_spl_firmware_ram_guard(11,22)==(mode==0?0:-1));
    assert(observations==(mode==3?0:1));
    for(unsigned i=0;i<4;i++)assert(fes_nano_spl_firmware_ram_guard(11,22)==-1);
    assert(observations==(mode==3?0:1));
    assert(ram_guard_attempted==(mode==3?UINT32_MAX:1));
    return 0;
}
''', 4)

    def test_actual_snapshot_requires_finished_cold_and_current_cpu_state(self):
        source = (NANO / 'spl_cold.c').read_text()
        snapshot = extract(source, 'fes_nano_spl_ram_snapshot')
        for instruction, replacement in (
            ('asm volatile("mov %0, sp" : "=r"(sp));', 'sp=sample_sp; samples++;'),
            ('asm volatile("mrs %0, sctlr_el3" : "=r"(sctlr));', 'sctlr=sample_sctlr; samples++;'),
            ('asm volatile("mrs %0, daif" : "=r"(daif));', 'daif=sample_daif; samples++;'),
        ):
            self.assertEqual(snapshot.count(instruction), 1)
            snapshot = snapshot.replace(instruction, replacement)
        self.run_c(PREFIX + r'''
#define CONFIG_SPL_SYS_MALLOC_F_LEN 0x2000
static unsigned el=3,samples;
static uint64_t sample_sp=0x6e000,sample_sctlr=0x1000,sample_daif=0x3c0,mpidr=0x200;
static unsigned current_el(void) { return el; }
static uint64_t read_mpidr(void) { return mpidr; }
static struct fes_nano_cold source;
static struct fes_nano_cold_record fes_nano_spl_record;
static struct { uint64_t malloc_base; } data;
#define gd (&data)
static char _image_binary_end[1];
static unsigned long mem_malloc_start=0xbfa00000,mem_malloc_end=0xbff00000;
''' + snapshot + r'''
int main(int argc,char **argv) {
    assert(argc==2);unsigned mode=strtoul(argv[1],NULL,0);
    struct fes_nano_spl_ram_view view;
    source.finish_stage=FES_COLD_FINISH_COMPLETE;fes_nano_spl_record.core=2;data.malloc_base=0x6f000;
    uintptr_t cold=(uintptr_t)&fes_nano_spl_record;
    switch(mode) {
    case 1:el=2;break;
    case 2:source.failed=1;break;
    case 3:source.finish_stage=FES_COLD_FINISH_ENTRY;break;
    case 4:cold++;break;
    case 5:sample_sctlr|=1;break;
    case 6:sample_sctlr|=4;break;
    case 7:sample_daif=0x340;break;
    case 8:mpidr=0x300;break;
    case 9:mem_malloc_end=mem_malloc_start-1;break;
    }
    assert(fes_nano_spl_ram_snapshot(0xbff00008,cold,&view)==(mode? -1:0));
    assert(samples==((mode>=1 && mode<=4)?0:3));
    if(!mode) {
        struct fes_nano_spl_ram_view expected=canonical();
        expected.gd_base=(uintptr_t)gd;expected.gd_bytes=sizeof(*gd);
        expected.image_bytes=(uintptr_t)_image_binary_end;expected.cold_base=cold;
        assert(!memcmp(&view,&expected,sizeof(view)));
    }
    return 0;
}
''', 10)

    def test_generated_caller_gates_final_branch_after_trace(self):
        fixture = '''
typedef void __noreturn (*atf_entry_t)(struct bl31_params *params, void *plat_params);
static void __noreturn entry(uintptr_t target) {
    atf_entry_t atf_entry=(atf_entry_t)target;
    void *bl31_params=(void *)11;uintptr_t fdt_addr=0;
    uintptr_t bl31_entry=target,bl32_entry=0,bl33_entry=0x80200000;
\tatf_entry(bl31_params, (void *)fdt_addr);
}
'''
        output = prepare.atf_source(fixture, firmware_ram=True)
        self.assertLess(output.index('fes_nano_spl_atf_trace(bl31_entry'),
                        output.index('if (fes_nano_spl_firmware_ram_guard'))
        self.assertLess(output.index('if (fes_nano_spl_firmware_ram_guard'),
                        output.index('atf_entry(bl31_params'))
        prefix = r'''
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#include <stdlib.h>
#include "nano_cold.h"
#define __noreturn __attribute__((noreturn))
struct bl31_params { unsigned value; };
static unsigned mode,stage,branched,parked;
static jmp_buf stopped;
void __attribute__((noreturn)) hang(void) { assert(mode && stage==3);parked=1;longjmp(stopped,1); }
'''
        suffix = r'''
const void *fes_nano_spl_handoff(void) { assert(stage++==0);return (void *)22; }
void fes_nano_spl_atf_trace(uintptr_t t,uintptr_t b32,uintptr_t b33,uintptr_t fdt,uintptr_t p,uintptr_t cold) {
    assert(stage++==1 && t && !b32 && b33==0x80200000 && !fdt && p==11 && cold==22);
}
int fes_nano_spl_firmware_ram_guard(uintptr_t p,uintptr_t cold) {
    assert(stage++==2 && p==11 && cold==22);return mode?-1:0;
}
static void __attribute__((noreturn)) destination(struct bl31_params *p,void *fdt,const void *cold,unsigned long cookie) {
    assert(!mode && stage++==3 && (uintptr_t)p==11 && !fdt && (uintptr_t)cold==22 && cookie==FES_NANO_COLD_COOKIE);
    branched=1;longjmp(stopped,1);
}
int main(int argc,char **argv) {
    assert(argc==2);mode=strtoul(argv[1],NULL,0);
    if(!setjmp(stopped))entry((uintptr_t)destination);
    assert(stage==(mode?3:4) && branched==!mode && parked==!!mode);return 0;
}
'''
        self.run_c(prefix + output + suffix, 2)
        self.assertNotIn('firmware_ram_guard', prepare.atf_source(fixture))
        for bad in (None, 0, 1, 'true', {}):
            with self.assertRaises(ValueError):
                prepare.atf_source(fixture, firmware_ram=bad)
        for bad in (fixture + fixture, fixture.replace('atf_entry(bl31_params', 'foreign(bl31_params')):
            with self.assertRaises(ValueError):
                prepare.atf_source(bad, firmware_ram=True)


if __name__ == '__main__':
    unittest.main()
