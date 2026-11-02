"""The real Nano U-Boot handoff policy rejects live owners before entry."""
import ctypes
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parents[1] / "platform/embedded/nano"
spec = importlib.util.spec_from_file_location("nano_prepare", HERE / "prepare_uboot.py")
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)


class Range(ctypes.Structure):
    _fields_ = [("start", ctypes.c_uint64), ("size", ctypes.c_uint64)]


class State(ctypes.Structure):
    _fields_ = [("affinity", ctypes.c_uint64), ("counter_hz", ctypes.c_uint64),
                ("cpu_state", ctypes.c_int32 * 4), ("per0_reset", ctypes.c_uint32),
                ("per1_reset", ctypes.c_uint32), ("bridge_reset", ctypes.c_uint32),
                ("watchdog_control", ctypes.c_uint32 * 5), ("uart_lcr", ctypes.c_uint32), ("uart_lsr", ctypes.c_uint32)]


class NanoHandoffTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        library = Path(cls.temp.name) / "handoff.so"
        subprocess.run([shutil.which("cc"), "-std=c11", "-Wall", "-Wextra", "-Werror", "-shared", "-fPIC",
                        "-fsanitize=undefined", "-fno-sanitize-recover=all", str(HERE / "handoff.c"), "-o", str(library)], check=True)
        cls.lib = ctypes.CDLL(str(library))
        cls.lib.fes_nano_memory_ok.argtypes = [ctypes.POINTER(Range), ctypes.c_uint, ctypes.POINTER(Range), ctypes.c_uint, ctypes.c_uint64,
                                              ctypes.POINTER(Range), ctypes.c_uint]
        cls.lib.fes_nano_state_ok.argtypes = [ctypes.POINTER(State)]

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def state(self, affinity=0):
        return State(affinity, 400000000, (ctypes.c_int32 * 4)(*[0 if i == affinity // 256 else 1 for i in range(4)]),
                     0xff21bfbf, 0, 0xf, (ctypes.c_uint32 * 5)(), 3, 0x60)

    def test_primary_a55_or_a76_with_other_cores_off(self):
        for affinity in (0, 0x200):
            self.assertEqual(self.lib.fes_nano_state_ok(ctypes.byref(self.state(affinity))), 1)

    def test_every_cpu_dma_bridge_timer_watchdog_and_uart_gate(self):
        mutations = [lambda s: setattr(s, "affinity", 0x100), lambda s: setattr(s, "counter_hz", 0),
                     lambda s: setattr(s, "counter_hz", 62500000), lambda s: s.cpu_state.__setitem__(1, 0),
                     lambda s: s.cpu_state.__setitem__(3, -1), lambda s: setattr(s, "per0_reset", 0xff20bfbf),
                     lambda s: setattr(s, "bridge_reset", 7), lambda s: setattr(s, "per1_reset", 1 << 17),
                     lambda s: s.watchdog_control.__setitem__(4, 1), lambda s: setattr(s, "uart_lcr", 0x83),
                     lambda s: setattr(s, "uart_lsr", 0x20)]
        for mutate in mutations:
            s = self.state(); mutate(s)
            self.assertEqual(self.lib.fes_nano_state_ok(ctypes.byref(s)), 0)
        for bit in range(32):
            if 0xff21bfbf & (1 << bit):
                s = self.state(); s.per0_reset &= ~(1 << bit)
                self.assertEqual(self.lib.fes_nano_state_ok(ctypes.byref(s)), 0)

    def test_live_reservations_cannot_overlap_go_or_staging(self):
        banks = (Range * 1)(Range(0x80000000, 0x40000000))
        for start, size, expected in ((0x80000000, 0x4000000, 1), (0xb0000000, 0x10000000, 1),
                                       (0x84000000, 4096, 0), (0x93fff000, 8192, 0),
                                       (0x96080000, 4096, 0), (0xfffffffffffff000, 8192, 0)):
            used = (Range * 1)(Range(start, size))
            self.assertEqual(self.lib.fes_nano_memory_ok(banks, 1, used, 1, 0x400000, None, 0), expected)
        owned=(Range*2)(Range(0x94800000,8<<20),Range(0x95000000,2<<20))
        for start,size,expected in ((0x80000000,0x4000000,1),(0xb0000000,0x10000000,1),
                                    (0x94000000,8<<20,1),(0x95200000,2<<20,1),
                                    (0x947ff000,8192,0),(0x94800000,4096,0),(0x94fff000,8192,0),
                                    (0x95000000,4096,0),(0x951ff000,8192,0),(0x95002000,4096,0)):
            with self.subTest(start=start,size=size):
                used=(Range*1)(Range(start,size))
                self.assertEqual(self.lib.fes_nano_memory_ok(banks,1,used,1,0x400000,owned,2),expected)
        for start,size in ((0,4096),(0x84000000,4096),(0x96080000,4096),(0x94800000,4096),
                           (0x95000000,0),(0xbffff000,8192),(0xfffffffffffff000,8192)):
            changed=(Range*2)(owned[0],Range(start,size))
            with self.subTest(owned_start=start,owned_size=size):
                self.assertEqual(self.lib.fes_nano_memory_ok(banks,1,None,0,0x400000,changed,2),0)
        banks[0].size = 0x20000000
        self.assertEqual(self.lib.fes_nano_memory_ok(banks, 1, None, 0, 0x400000, None, 0), 0)


class NanoExportTests(unittest.TestCase):
    def fixture(self, base):
        source, entry = base / "source", base / "entry"
        (source / "cmd").mkdir(parents=True)
        (source / "arch/arm/lib").mkdir(parents=True)
        (source / "cmd/Makefile").write_text("# private test export\n")
        bootm = source / "arch/arm/lib/bootm.c"
        bootm.write_text("static void do_nonsec_virt_switch(void)\n{\n\tsmp_kick_all_cpus();\n}\n"
                         "\tdm_remove_devices_active();\n\n\tcleanup_before_linux();\n")
        soc=source/'arch/arm/mach-socfpga';soc.mkdir(parents=True)
        (soc/'Makefile').write_text('ifdef CONFIG_TARGET_SOCFPGA_AGILEX5\nobj-y += rsu.o\nobj-y += rsu_ll_qspi.o\nobj-y += rsu_misc.o\nobj-$(CONFIG_CADENCE_QSPI) += rsu_s10.o\nobj-$(CONFIG_ARMV8_PSCI) += smc_rsu_s10.o\nendif\nifdef CONFIG_TARGET_SOCFPGA_AGILEX7M\nendif\n')
        (soc/'spl_agilex5.c').write_text('\tret = spl_early_init();\n\tpreloader_console_init();\n')
        (source/'common/spl').mkdir(parents=True)
        (source/'common/spl/spl_atf.c').write_text('struct bl31_params;\n'
            'typedef void __noreturn (*atf_entry_t)(struct bl31_params *params, void *plat_params);\n'
            'static void invoke_atf(uintptr_t bl31_entry, uintptr_t bl32_entry, uintptr_t bl33_entry, struct bl31_params *bl31_params, unsigned long fdt_addr, atf_entry_t atf_entry)\n{\n'
            '\tatf_entry(bl31_params, (void *)fdt_addr);\n}\n')
        (source/'common/spl/spl_fat.c').write_text('static ulong spl_fit_read(struct spl_load_info *load, ulong file_offset, ulong size, void *buf)\n{\n\treturn 0;\n}\nvoid load(void) {\n\terr = spl_register_fat_device(block_dev, partition);\n\tif (err)\n\t\tgoto end;\nend:;\n}\n')
        (source/'common/board_r.c').write_text('static int initr_mmc(void)\n{\n\treturn 0;\n}\n\tinitr_mmc,\n')
        (source/'arch/arm/dts').mkdir(parents=True)
        (source/'arch/arm/dts/socfpga_agilex5_de25_nano-u-boot.dtsi').write_text('// pinned board fixture\n')
        (source/'include/configs').mkdir(parents=True)
        (source/'include/configs/socfpga_agilex5_de25_nano.h').write_text('#include <configs/socfpga_soc64_common.h>\n')
        (source/'drivers/reset').mkdir(parents=True)
        shutil.copyfile(Path(__file__).parent/'fixtures/nano/reset-socfpga.c',source/'drivers/reset/reset-socfpga.c')
        entry.mkdir()
        image = entry / "entry.img"; image.write_bytes(bytes(64))
        receipt = {"status": "built", "target": "de25-nano", "entry_image_address": 0x96080000,
                   "ram_start": 0x84000000, "ram_end": 0x94000000, "uart_base": 0x10c02100,
                   "entry_image_sha256": prepare.sha(image), "entry_image_size": 64,
                   "firmware_profile_sha256": prepare.sha(HERE / "probe-profile.json"), "sources": {},
                   "native_build_sources":{"platform/embedded/tamago/build.py":prepare.sha(HERE.parent/'tamago/build.py')}}
        (entry / "result.json").write_text(json.dumps(receipt))
        return source, entry, bootm

    def test_actual_adapter_is_installed_after_device_remove_before_cpu_cleanup(self):
        with tempfile.TemporaryDirectory() as tmp:
            source, entry, bootm = self.fixture(Path(tmp))
            result = prepare.prepare(source, entry, "a" * 64, 16)
            text = bootm.read_text()
            self.assertLess(text.index("dm_remove_devices_active();"), text.index("fes_nano_late_handoff();"))
            self.assertLess(text.index("fes_nano_late_handoff();"), text.index("cleanup_before_linux();"))
            self.assertNotIn("smp_kick_all_cpus();", text)
            self.assertEqual((source / "cmd/fes_nano.c").read_bytes(), (HERE / "uboot.c").read_bytes())
            self.assertEqual(len(result["build_id"]), 16)
            self.assertEqual(result['atf_handoff']['bytes'],16)
            self.assertEqual(result['atf_handoff']['sha256'],'a'*64)
            self.assertTrue(result['cold_boot_profile']['spl_atf_trace'])
            self.assertIn('#define FES_NANO_ATF_SIZE 16UL',
                          (source/'arch/arm/mach-socfpga/fes_nano_atf_seal.h').read_text())

    def test_mutated_image_rejects_before_export_is_changed(self):
        with tempfile.TemporaryDirectory() as tmp:
            source, entry, bootm = self.fixture(Path(tmp)); before = bootm.read_bytes()
            (entry / "entry.img").write_bytes(b"changed")
            with self.assertRaises(ValueError):
                prepare.prepare(source, entry, "a" * 64, 16)
            self.assertEqual(bootm.read_bytes(), before)

    def test_controlled_sd_selects_only_the_exact_source_profile(self):
        with tempfile.TemporaryDirectory() as tmp:
            source,entry,_=self.fixture(Path(tmp))
            path=entry/'result.json';receipt=json.loads(path.read_text())
            receipt['firmware_profile_sha256']=prepare.sha(HERE/'sd-profile.json')
            path.write_text(json.dumps(receipt))
            result=prepare.prepare(source,entry,'a'*64, 16)
            self.assertEqual(result['profile_sha256'],prepare.sha(HERE/'sd-profile.json'))
            self.assertTrue(result['cold_boot_profile']['sd_pio'])
            self.assertIn('#define FES_NANO_CONTROLLED_SD 1',
                          (source/'arch/arm/mach-socfpga/fes_nano_cold_profile.h').read_text())
        with tempfile.TemporaryDirectory() as tmp:
            source,entry,_=self.fixture(Path(tmp))
            path=entry/'result.json';receipt=json.loads(path.read_text())
            receipt['firmware_profile_sha256']=prepare.sha(HERE/'smmu-profile.json')
            receipt['native_program']='probe';path.write_text(json.dumps(receipt))
            result=prepare.prepare(source,entry,'a'*64, 16)
            self.assertTrue(result['cold_boot_profile']['smmu_observe'])
            self.assertFalse(result['cold_boot_profile']['sd_pio'])
            header=(source/'arch/arm/mach-socfpga/fes_nano_cold_profile.h').read_text()
            self.assertIn('#define FES_NANO_SMMU_BOOT_OBSERVE 1',header)
            receipt['native_program']='storage';path.write_text(json.dumps(receipt))
            with self.assertRaisesRegex(ValueError,'source-bound serial probe'):
                prepare.prepare(source,entry,'a'*64, 16)

    def test_stale_native_sources_and_unknown_profiles_reject_before_export(self):
        for mutation in ('source','missing','escape','profile'):
            with self.subTest(mutation=mutation),tempfile.TemporaryDirectory() as tmp:
                source,entry,bootm=self.fixture(Path(tmp));before=bootm.read_bytes()
                path=entry/'result.json';receipt=json.loads(path.read_text())
                if mutation=='source':receipt['native_build_sources']['platform/embedded/tamago/build.py']='0'*64
                elif mutation=='missing':receipt.pop('native_build_sources')
                elif mutation=='escape':receipt['native_build_sources']={'../AGENTS.md':'0'*64}
                else:receipt['firmware_profile_sha256']='a'*64
                path.write_text(json.dumps(receipt))
                with self.assertRaises(ValueError):prepare.prepare(source,entry,'a'*64, 16)
                self.assertEqual(bootm.read_bytes(),before)

    def test_firmware_identity_changes_loader_identity(self):
        identities = []
        for digest in ("a" * 64, "b" * 64):
            with tempfile.TemporaryDirectory() as tmp:
                source, entry, _ = self.fixture(Path(tmp))
                result = prepare.prepare(source, entry, digest, 16)
                self.assertEqual(result["bl31_sha256"], digest)
                self.assertEqual((source / "cmd/fes_embedded_bridges.h").read_bytes(),
                                 (HERE.parent / "firmware/generated/fes_embedded_bridges.h").read_bytes())
                self.assertTrue(result["preloaded_entry"])
                identities.append(result["build_id"])
        self.assertNotEqual(*identities)

    def test_generated_spl_atf_call_passes_current_cookie_and_record(self):
        """Execute the generated caller; a valid record with the old cookie rejects at EL3."""
        with tempfile.TemporaryDirectory() as tmp:
            base=Path(tmp)
            source,entry,_=self.fixture(base)
            prepare.prepare(source,entry,"a"*64, 16)
            include=source/'include'
            (include/'linux').mkdir()
            (include/'linux/types.h').write_text('#include <stdint.h>\n')
            test=base/'atf-call.c';binary=base/'atf-call'
            test.write_text('#include <assert.h>\n#include <stdint.h>\n#define __noreturn\n'+
                (source/'common/spl/spl_atf.c').read_text()+r'''
static unsigned record,calls,observations;
static uintptr_t target_expected;
const void *fes_nano_spl_handoff(void) { calls++;return &record; }
void fes_nano_spl_atf_trace(uintptr_t target,uintptr_t bl32,uintptr_t bl33,uintptr_t fdt,uintptr_t params,uintptr_t cold)
{
    assert(calls==1 && !observations++ && target==target_expected);
    assert(bl32==0 && bl33==0x80200000 && fdt==0x94000000 && params==0x10000);
    assert(cold==(uintptr_t)&record);
}
static void capture(struct bl31_params *params,void *fdt,const void *cold,unsigned long cookie)
{
    assert(params==(void *)0x10000 && fdt==(void *)0x94000000);
    assert(cold==&record && calls==1 && observations==1);
    assert(cookie==FES_NANO_COLD_COOKIE && cookie!=0x464e433100000001ULL);
}
int main(void)
{
    target_expected=(uintptr_t)capture;
    invoke_atf(target_expected,0,0x80200000,(void *)0x10000,0x94000000,capture);
    assert(calls==1 && observations==1);
}
''')
            subprocess.run([shutil.which('cc'),'-std=c11','-Wall','-Wextra','-Werror',
                            '-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(include),
                            str(test),'-o',str(binary)],check=True)
            subprocess.run([str(binary)],check=True)

    def test_actual_reset_cleanup_retains_owner_while_vendor_cleanup_retires_it(self):
        """Run the pinned removal callback against the actual cold owner implementation."""
        reference=(Path(__file__).parent/'fixtures/nano/reset-socfpga.c').read_text()
        patched=prepare.cold_reset_source(reference)
        with self.assertRaisesRegex(ValueError,'unreviewed Nano reset cleanup source'):
            prepare.cold_reset_source(reference+'\n')
        prefix=reference[reference.index('#define BANK_INCREMENT'):reference.index('/*\n * For compatibility')]
        for retained,text in ((False,reference),(True,patched)):
            functions='\n'.join(re.findall(
                r'static (?:bool socfpga_reset_keep_enabled\(void\)|int socfpga_reset_remove\(struct udevice \*dev\))\n\{.*?\n\}',
                text,re.S))
            with tempfile.TemporaryDirectory() as tmp:
                base=Path(tmp);test=base/'reset.c';binary=base/'reset'
                test.write_text(r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "nano_cold.h"
#define __iomem
#define BIT(n) (1U<<(n))
#define GENMASK(h,l) ((~0U<<(l)) & (~0U>>(31-(h))))
#define CONFIG_TARGET_SOCFPGA_AGILEX5 1
#define CONFIG_IS_ENABLED(option) 1
#define RSTMGR_SOC64_PER0MODRST 0x24
typedef uint32_t u32;
struct udevice { unsigned unused; };
'''+prefix+r'''
static uint32_t per0=0xff7fbfbf,per1=0x12345678;
static unsigned writes,legacy;
static struct socfpga_reset_data data={.modrst_base=(void *)0x10d11024UL};
void *dev_get_priv(struct udevice *dev) { (void)dev;return &data; }
uintptr_t socfpga_get_rstmgr_addr(void) { return 0x10d11000UL; }
const char *env_get(const char *key) { assert(!strcmp(key,"socfpga_legacy_reset_compat"));return legacy?"1":NULL; }
long simple_strtol(const char *s,char **end,unsigned base) { return strtol(s,end,base); }
void clrbits_le32(uintptr_t address,uint32_t bits) { assert(address==0x10d11024UL);writes++;per0&=~bits; }
void writel(uint32_t value,void *address) { assert(address==(void *)0x10d11028UL);writes++;per1=value; }
static int read_reg(void *context,unsigned reg,uint32_t *value)
{
    (void)context;
    switch(reg) {
    case FES_COLD_CAUSE:*value=4;break;case FES_COLD_PER0:*value=per0;break;
    case FES_COLD_BRG:case FES_COLD_CPUS:*value=15;break;
    case FES_COLD_RELEASE:case FES_COLD_CPU1_POWER:case FES_COLD_CPU2_POWER:case FES_COLD_CPU3_POWER:*value=0;break;
    case FES_COLD_BOOT_KIND:*value=4;break;case FES_COLD_CPU0_POWER:*value=0x90000021;break;
    case FES_COLD_PCH_REQUEST:case FES_COLD_PCH_STATUS:*value=0x23;break;
    case FES_COLD_BOOT_CONFIG:*value=0x0070021e;break;default:assert(0);return -1;
    }
    return 0;
}
static int write_reg(void *p,unsigned reg,uint32_t value) { (void)p;(void)reg;(void)value;assert(0);return -1; }
static void barrier(void *p) { (void)p; }
'''+functions+f'\n#define EXPECT_RETAINED {int(retained)}\n'+r'''
int main(int argc,char **argv)
{
    assert(argc==2);legacy=strtoul(argv[1],0,0);
    static const uint8_t chain[32]={0x42};
    struct fes_nano_cold_record r={.magic=FES_NANO_COLD_MAGIC,.version=FES_NANO_COLD_VERSION,.size=sizeof(r),.flags=FES_NANO_COLD_FLAGS,
        .boot={.magic=FES_NANO_BOOT_MAGIC,.version=1,.size=sizeof(r.boot),.owner=17,.epoch=7,.history=1,.fabric=1,.producers=FES_NANO_PRODUCERS_ALL},
        .first={.cause=4,.per0=0xff7fbfbf,.bridge=15,.cpus=15,.boot_kind=4,
                .power={0x90000021,0,0,0},.pch_request=0x23,.pch_status=0x23,.boot_config=0x0070021e}};
    r.last=r.first;memcpy(r.boot.chain,chain,32);r.checksum=fes_nano_cold_checksum(&r);
    struct fes_nano_cold owner={0};
    struct fes_nano_cold_io io={.read=read_reg,.write=write_reg,.barrier=barrier,.wait=barrier};
    assert(!fes_nano_cold_consume(&owner,&r,io,chain,17));
    assert(fes_nano_cold_owns(&owner,17,7)==1);
    struct udevice dev={0};assert(!socfpga_reset_remove(&dev));
    if(EXPECT_RETAINED) {
        assert(!writes && per0==0xff7fbfbf && per1==0x12345678);
        assert(fes_nano_cold_owns(&owner,17,7)==1);
        puts("PASS cleanup retains cold owner");
    } else {
        assert(writes==1+legacy && per0!=(uint32_t)0xff7fbfbf);
        assert(fes_nano_cold_owns(&owner,17,7)!=1);
        per0=0xff7fbfbf;assert(fes_nano_cold_owns(&owner,17,7)!=1);
        puts("PASS vendor cleanup retires cold owner");
    }
}
''')
                subprocess.run([shutil.which('cc'),'-std=c11','-Wall','-Wextra','-Werror',
                                '-fsanitize=undefined','-fno-sanitize-recover=all',
                                '-I'+str(HERE.parent/'firmware'),str(test),str(HERE.parent/'firmware/nano_cold.c'),'-o',str(binary)],check=True)
                for legacy in (0,1):
                    result=subprocess.run([str(binary),str(legacy)],capture_output=True,text=True,check=True)
                    self.assertIn('PASS cleanup retains cold owner' if retained else 'PASS vendor cleanup retires cold owner',result.stdout)


class NanoOwnedSnapshotTests(unittest.TestCase):
    """Execute the actual native adapter's private, read-only ownership check."""
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();base=Path(cls.temp.name)
        adapter=(HERE/'uboot.c').read_text()
        fragment=adapter[adapter.index('static u64 owned_owner'):adapter.index('/* Called after dm_remove_devices_active()')]
        source=base/'snapshot.c'
        source.write_text(r'''
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include "handoff.h"
#include "fes_embedded_bridges.h"
typedef uint32_t u32;
typedef uint64_t u64;
static int status;
static u64 words[3];
static unsigned calls;
static int invoke_smc(u32 fid,u64 *args,int count,u64 *reply,int replies) {
    assert(fid==FES_EMBEDDED_BRIDGE_CALL && count==4 && replies==3);
    assert(args[0]==FES_EMBEDDED_BRIDGE_SNAPSHOT && !args[1] && args[2]==FES_EMBEDDED_BRIDGE_COOKIE && !args[3]);
    for(unsigned i=0;i<3;i++)reply[i]=words[i];
    calls++;return status;
}
'''+fragment+r'''
int main(int argc,char **argv) {
    assert(argc==5 || argc==7);
    status=atoi(argv[1]);
    for(unsigned i=0;i<3;i++)words[i]=strtoull(argv[i+2],0,0);
    int first=observe_owned("before-entry"),second=-1;
    if(argc==7) {words[1]=strtoull(argv[5],0,0);words[2]=strtoull(argv[6],0,0);second=observe_owned("after-remove");}
    printf("HARNESS first=%d second=%d calls=%u\n",first,second,calls);
}
''')
        cls.binary=base/'snapshot'
        subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all',
                        '-I'+str(HERE),'-I'+str(HERE.parent/'firmware/generated'),str(source),'-o',str(cls.binary)],check=True)
    @classmethod
    def tearDownClass(cls):cls.temp.cleanup()
    def run_reply(self,status=0,flags=(0x46424f31<<32)|0xdf,owner=17,generation=1,second=None):
        args=[str(self.binary),str(status),hex(flags),str(owner),str(generation)]
        if second:args.extend(str(v) for v in second)
        return subprocess.check_output(args,text=True)
    def test_exact_cold_proof_and_owner_continuity(self):
        self.assertIn('HARNESS first=1 second=1 calls=2',self.run_reply(second=(17,1)))
        for second in ((18,1),(17,2),(0,1)):
            with self.subTest(second=second):self.assertIn('HARNESS first=1 second=0 calls=2',self.run_reply(second=second))
    def test_unready_wrong_tag_and_every_missing_or_extra_flag_reject(self):
        for status in (1,4,5,-1):
            self.assertIn('HARNESS first=0',self.run_reply(status=status))
        for bit in range(32):
            self.assertIn('HARNESS first=0',self.run_reply(flags=(0x46424f31<<32)|(0xdf^(1<<bit))))
        self.assertIn('HARNESS first=0',self.run_reply(flags=0xdf))
        self.assertIn('HARNESS first=0',self.run_reply(owner=0))
        self.assertIn('HARNESS first=0',self.run_reply(generation=2))


if __name__ == "__main__":unittest.main()
