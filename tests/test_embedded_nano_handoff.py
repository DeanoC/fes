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
            result = prepare.prepare(source, entry)
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
                prepare.prepare(source, entry)
            self.assertEqual(bootm.read_bytes(), before)


if __name__ == "__main__":
    unittest.main()
