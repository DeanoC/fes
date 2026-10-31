"""The real Nano U-Boot handoff policy rejects live owners before entry."""
import ctypes
import importlib.util
import json
from pathlib import Path
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
        cls.lib.fes_nano_memory_ok.argtypes = [ctypes.POINTER(Range), ctypes.c_uint, ctypes.POINTER(Range), ctypes.c_uint, ctypes.c_uint64]
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
            self.assertEqual(self.lib.fes_nano_memory_ok(banks, 1, used, 1, 0x400000), expected)
        banks[0].size = 0x20000000
        self.assertEqual(self.lib.fes_nano_memory_ok(banks, 1, None, 0, 0x400000), 0)


class NanoExportTests(unittest.TestCase):
    def fixture(self, base):
        source, entry = base / "source", base / "entry"
        (source / "cmd").mkdir(parents=True)
        (source / "arch/arm/lib").mkdir(parents=True)
        (source / "cmd/Makefile").write_text("# private test export\n")
        bootm = source / "arch/arm/lib/bootm.c"
        bootm.write_text("static void do_nonsec_virt_switch(void)\n{\n\tsmp_kick_all_cpus();\n}\n"
                         "\tdm_remove_devices_active();\n\n\tcleanup_before_linux();\n")
        entry.mkdir()
        image = entry / "entry.img"; image.write_bytes(bytes(64))
        receipt = {"status": "built", "target": "de25-nano", "entry_image_address": 0x96080000,
                   "ram_start": 0x84000000, "ram_end": 0x94000000, "uart_base": 0x10c02100,
                   "entry_image_sha256": prepare.sha(image), "entry_image_size": 64,
                   "firmware_profile_sha256": prepare.sha(HERE / "probe-profile.json"), "sources": {}}
        (entry / "result.json").write_text(json.dumps(receipt))
        return source, entry, bootm

    def test_actual_adapter_is_installed_after_device_remove_before_cpu_cleanup(self):
        with tempfile.TemporaryDirectory() as tmp:
            source, entry, bootm = self.fixture(Path(tmp))
            result = prepare.prepare(source, entry, "a" * 64)
            text = bootm.read_text()
            self.assertLess(text.index("dm_remove_devices_active();"), text.index("fes_nano_late_handoff();"))
            self.assertLess(text.index("fes_nano_late_handoff();"), text.index("cleanup_before_linux();"))
            self.assertNotIn("smp_kick_all_cpus();", text)
            self.assertEqual((source / "cmd/fes_nano.c").read_bytes(), (HERE / "uboot.c").read_bytes())
            self.assertEqual(len(result["build_id"]), 16)

    def test_mutated_image_rejects_before_export_is_changed(self):
        with tempfile.TemporaryDirectory() as tmp:
            source, entry, bootm = self.fixture(Path(tmp)); before = bootm.read_bytes()
            (entry / "entry.img").write_bytes(b"changed")
            with self.assertRaises(ValueError):
                prepare.prepare(source, entry, "a" * 64)
            self.assertEqual(bootm.read_bytes(), before)

    def test_firmware_identity_changes_loader_identity(self):
        identities = []
        for digest in ("a" * 64, "b" * 64):
            with tempfile.TemporaryDirectory() as tmp:
                source, entry, _ = self.fixture(Path(tmp))
                result = prepare.prepare(source, entry, digest)
                self.assertEqual(result["bl31_sha256"], digest)
                self.assertEqual((source / "cmd/nano_bridge_diag.h").read_bytes(),
                                 (HERE.parent / "firmware/nano_bridge_diag.h").read_bytes())
                identities.append(result["build_id"])
        self.assertNotEqual(*identities)


class NanoBridgeDiagnosticsTests(unittest.TestCase):
    """Execute the real adapter's bridge call/readback with injected replies."""
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        base = Path(cls.temp.name)
        adapter = (HERE / "uboot.c").read_text()
        start = adapter.index("static const char *bridge_stage_name(")
        stop = adapter.index("static void contain(", start)
        source = base / "bridge.c"
        source.write_text(r'''
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include "handoff.h"
#include "nano_bridge_diag.h"
typedef uint32_t u32;
typedef uint64_t u64;
#define RSTMGR 0x10d11000UL
#define INTEL_SIP_SMC_HPS_SET_BRIDGES 0xc2000032U
static u32 before, after;
static int status;
static u64 detail[3] = {(u64)FES_NANO_BRIDGE_DIAG_TAG << 32, 0, 0};
static unsigned reads, calls;
static u32 readl(unsigned long address) {
    assert(address == RSTMGR + 0x2c);
    assert(reads < 2);
    if (reads++ == 0) { assert(calls == 0); return before; }
    assert(calls == 1);
    return after;
}
static int invoke_smc(u32 function, u64 *args, int count, u64 *reply, int replies) {
    assert(function == INTEL_SIP_SMC_HPS_SET_BRIDGES);
    assert(count == 3 && args[0] == 2 && args[1] == FES_NANO_BRIDGES && args[2] == FES_NANO_BRIDGE_DIAG_REQUEST);
    assert(reply != NULL && replies == 3 && reads == 1 && calls == 0);
    for (unsigned i = 0; i < 3; i++) reply[i] = detail[i];
    calls++;
    return status;
}
''' + adapter[start:stop] + r'''
int main(int argc, char **argv) {
    assert(argc == 5 || argc == 8);
    status = (int)strtol(argv[1], NULL, 0);
    before = (u32)strtoul(argv[2], NULL, 0);
    after = (u32)strtoul(argv[3], NULL, 0);
    if (argc == 8) for (unsigned i = 0; i < 3; i++) detail[i] = strtoull(argv[5+i], NULL, 0);
    int accepted = close_bridges(argv[4]);
    assert(reads == 2 && calls == 1);
    printf("HARNESS accepted=%d reads=%u calls=%u\n", accepted, reads, calls);
    return 0;
}
''')
        cls.binary = base / "bridge"
        subprocess.run([shutil.which("cc"), "-std=c11", "-Wall", "-Wextra", "-Werror",
                        "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I", str(HERE),
                        "-I", str(HERE.parent / "firmware"),
                        str(source), "-o", str(cls.binary)], check=True)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def check_reply(self, status, before, after, phase="before-load", detail=None, diagnostic_ok=True):
        with self.subTest(status=status, before=before, after=after, phase=phase):
            output = subprocess.check_output([str(self.binary), str(status), hex(before), hex(after), phase,
                                              *([] if detail is None else [hex(word) for word in detail])], text=True)
            smc_error, reset_mismatch = int(status != 0), int(after & 0xf != 0xf)
            begin = f"FES_NANO_BRIDGE BEGIN phase={phase} mask=0000000f before={before:08x}"
            result = (f"FES_NANO_BRIDGE RESULT phase={phase} smc={status} smc_hex={status & 0xffffffff:08x} "
                      f"mask=0000000f before={before:08x} after={after:08x} "
                      f"smc_error={smc_error} reset_mismatch={reset_mismatch}")
            self.assertIn(begin, output)
            self.assertIn(result, output)
            self.assertLess(output.index(begin), output.index(result))
            rejected = smc_error or reset_mismatch
            self.assertIn(f"HARNESS accepted={int(not rejected and diagnostic_ok)} reads=2 calls=1", output)
            if rejected:
                self.assertIn(f"FES_NANO_HANDOFF REJECT bridge phase={phase} smc_error={smc_error} reset_mismatch={reset_mismatch}", output)
            elif diagnostic_ok:
                self.assertNotIn("REJECT", output)
            else:
                self.assertIn("FES_NANO_HANDOFF REJECT bridge diagnostic identity/state", output)
            return output

    def test_service_error_and_reset_mismatch_are_independent(self):
        for phase in ("before-load", "after-remove"):
            for status in (0, 4, -1):
                for after in (0, 7, 0xf, 0x8000000f):
                    self.check_reply(status, 0x80000003, after, phase)

    def test_all_required_reset_bits_and_already_closed_bridge(self):
        for bit in range(4):
            self.check_reply(0, 0xf, 0xf & ~(1 << bit))
        self.check_reply(0, 0xf, 0xf)

    def test_first_failed_stage_and_register_snapshot_are_decoded(self):
        names = ("s2f_ack_clear", "lw_ack_clear", "f2s_fpga_ack_set", "f2s_flush_ack_set",
                 "sdram_flush_ack_clear", "sdram_fpga_ack_clear", "noc_idle_ack", "noc_idle_status")
        for stage, name in enumerate(names, 1):
            detail = ((0x46424431 << 32) | stage, ((-110 & 0xffffffff) << 32) | 0x12345678,
                      (0xabcdef01 << 32) | 0xf)
            output = self.check_reply(4, 0xf, 0xf, detail=detail)
            self.assertIn(f"valid=1 captured=1 stage={stage} name={name} error=-110", output)
            self.assertIn("failure_ack=12345678 failure_req=abcdef01 failure_reset=0000000f", output)

    def test_missing_or_inconsistent_details_cannot_accept_success(self):
        tag = 0x46424431 << 32
        for detail in ((0, 0, 0), (tag | 1, 0xffffff9200000001, 0xf),
                       (tag | 9, 0xffffff9200000001, 0xf), (tag, 0xffffff9200000001, 0xf),
                       (tag | 1, 0, 0xf)):
            self.check_reply(0, 0xf, 0xf, detail=detail, diagnostic_ok=False)


if __name__ == "__main__":
    unittest.main()
