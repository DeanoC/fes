"""Exercise the real pre-storage register selection and its passive UART replay."""
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parents[1] / "platform/embedded/nano/mac_spl"
spec = importlib.util.spec_from_file_location("mac_spl_uart", HERE / "verify.py")
uart = importlib.util.module_from_spec(spec)
spec.loader.exec_module(uart)
ID = "dcba9876543210fe"

C_FIXTURE = r'''
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static const char *mode;
static unsigned version_reads, register_reads;
static unsigned current_el(void) { return 3; }
static unsigned readl(void *ptr)
{
	unsigned addr = (unsigned)(uintptr_t)ptr;
	if (addr == 0x10810110) {
		version_reads++;
		return 0x7631;
	}
	register_reads++;
	switch (addr) {
	case 0x10d10004:
		if (!strcmp(mode, "busy")) return 0x02030301;
		if (!strcmp(mode, "main-transition")) return 0x02030100;
		if (!strcmp(mode, "per-transition")) return 0x02010300;
		return 0x02030300; /* Actual settled CBL clock status. */
	case 0x10d10024: return !strcmp(mode, "gate") ? 6 : 14;
	case 0x10d1007c: return 1;
	case 0x10d10030: return !strcmp(mode, "source") ? 2 : 0;
	case 0x10d11024: return !strcmp(mode, "reset") ? 0x101 : 0;
	case 0x10d1219c: return !strcmp(mode, "unstable") && register_reads > 15 ? 2 : 3;
	case 0x10d121c8: return 0x10001;
	case 0x10d10040: case 0x10d10044: case 0x10d10088: case 0x10d10094:
	case 0x10d100f4: case 0x10d12044: case 0x10d2102c: case 0x10d120d4: return 0;
	default: abort();
	}
}
#include "observe.inc"
int main(int argc, char **argv)
{
	if (argc != 2) return 2;
	mode = argv[1];
	printf("U-Boot SPL 2025.01-fes-nano-cpu-%s (fixture)\n", FES_CPU_DIAG_ID);
	for (unsigned i=1; i<18; i++)
		printf("FES_NANO_CPU_TRACE id=%s stage=%02x value=00000000 mpidr=0000000081000000 el=3\n", FES_CPU_DIAG_ID,i);
	fes_nano_mac_observe();
	printf("FES_NANO_CPU_TRACE id=%s stage=12 value=00000000 mpidr=0000000081000000 el=3\n", FES_CPU_DIAG_ID);
	puts("FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31");
	if (version_reads != (unsigned)!strcmp(mode, "ready") || register_reads!=30) abort();
}
'''


class MACObservationTest(unittest.TestCase):
    def test_trace_and_read_selection(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            (directory / "fixture.c").write_text(C_FIXTURE)
            (directory / "mac_spl_snapshot.h").write_bytes((HERE / "snapshot.h").read_bytes())
            (directory / "fes_cpu_diag_config.h").write_text(f'#define FES_CPU_DIAG_ID "{ID}"\n')
            binary = directory / "fixture"
            subprocess.run(["cc", "-std=c11", "-Wall", "-Werror", "-I", HERE, "-I", directory,
                            directory / "fixture.c", "-o", binary], check=True)
            for mode, reason in (("ready", None), ("reset", "tsn0-reset"), ("busy", "clock-state"),
                                 ("main-transition", "clock-state"), ("per-transition", "clock-state"),
                                 ("gate", "clock-gate"), ("source", "clock-source")):
                raw = subprocess.check_output([binary, mode])
                with self.subTest(mode=mode):
                    result = uart.verify(raw, ID)
                    self.assertEqual(reason, result["mac_skip_reason"])
                    self.assertEqual(0x7631 if reason is None else None, result["mac_version"])
                    self.assertTrue(result["parked"])
                    self.assertFalse(result["dma_started"])
            with self.assertRaises(ValueError):
                uart.verify(subprocess.check_output([binary, "unstable"]), ID)
            raw = subprocess.check_output([binary, "reset"]).replace(b"\n", b"\r\n")
            self.assertTrue(uart.verify(raw, ID)["parked"])
            for bad in (raw[:-1], raw+raw, raw+b"unexpected runtime\r\n", raw.replace(b"2025.01",b"2024.01"),
                        raw.replace(b"81000000",b"81010000"), raw.replace(b"stage=01", b"stage=ff"),
                        raw.replace(ID.encode(), b"bad"), raw.replace(b"pass=1 name=tsn0", b"pass=0 name=tsn0"),
                        raw.replace(b"skipped=tsn0-reset", b"read\r\nFES_NANO_MAC_SPL VERSION value=00007631")):
                with self.assertRaises(ValueError):
                    uart.verify(bad, ID)


if __name__ == "__main__":
    unittest.main()
