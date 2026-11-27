"""Host-only execution of the freestanding port with genuine and injected clock readings."""
import importlib.util
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import unittest

FW = Path(__file__).resolve().parents[1] / "platform/embedded/firmware"
PORT = FW / "nano_jitter_port"
FIXTURE = FW / "tests/nano_jitter_adapter_test.c"


@unittest.skipUnless(all(shutil.which(x) for x in ("gcc", "objcopy", "ld")), "requires GNU C and binutils")
class JitterAdapterTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        tmp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(tmp.cleanup)
        cls.directory = Path(tmp.name)
        spec = importlib.util.spec_from_file_location("test_jitter_build", PORT / "build_jitter.py")
        builder = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(builder)
        compiler = ["gcc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror", "-no-pie"]
        subprocess.run([*compiler, "-I", PORT, FW / "tests/nano_jitter_arena_test.c", PORT / "arena.c",
                        "-o", cls.directory / "arena"], check=True)
        for mode in ("seed", "capture"):
            actual = cls.directory / (mode + ".o")
            controlled = cls.directory / (mode + "-clock.o")
            builder.produce("gcc", actual, mode, host_fixture=True)
            # Keep every allocator, watchdog, health and collector function
            # intact. Replace only the test executable's raw timestamp source.
            subprocess.run(["objcopy", "--weaken-symbol=fes_jent_clock_raw", actual, controlled], check=True)
            for label, obj in (("controlled", controlled), ("native", actual)):
                if label == "native" and mode == "capture":
                    continue
                exe = cls.directory / (mode + "-" + label)
                flags = ["-D" + mode.upper()]
                if label == "controlled":
                    flags.append("-DCONTROLLED_CLOCK")
                subprocess.run([*compiler, *flags, FIXTURE, obj, "-o", exe], check=True)
            if mode == "capture":
                # Replace the clock consumer in this one executable so the
                # genuine watchdog can reach its read cap on a frozen timer
                # without 32 million full upstream cryptographic operations.
                reader = cls.directory / "capture-reader.o"
                subprocess.run(["objcopy", "--weaken-symbol=jent_measure_jitter", controlled, reader], check=True)
                subprocess.run([*compiler, "-DCONTROLLED_CLOCK", "-DREAD_CAP", FIXTURE, reader,
                                "-o", cls.directory / "capture-reader"], check=True)

    def case(self, mode, name, clock="controlled"):
        exe = self.directory / (mode + "-" + clock)
        p = subprocess.run([exe, name], capture_output=True, text=True, timeout=30)
        self.assertEqual(p.returncode, 0, p.stdout + p.stderr)
        return p.stdout

    def test_seed_and_gcd_with_controlled_clock(self):
        self.assertIn("rc=0 gcd=1", self.case("seed", "seed"))
        self.assertIn("rc=-5 gcd=6", self.case("seed", "gcd"))

    def test_seed_watchdog_and_health_scrub(self):
        for fault in ("deadline", "backward", "health"):
            with self.subTest(fault=fault):
                self.case("seed", fault)

    @unittest.skipUnless(platform.machine() == "x86_64", "x86 hardware clock control")
    def test_native_x86_clock_result(self):
        self.assertIn("native rc=", self.case("seed", "native", "native"))

    def test_raw_keeps_stuck_samples_and_releases_arena(self):
        self.case("capture", "raw")

    def test_raw_watchdog_scrubs_and_invalidates(self):
        for name in ("backward", "deadline", "held-deadline"):
            with self.subTest(fault=name):
                self.case("capture", name)

    def test_frozen_timer_exhausts_read_cap(self):
        self.case("capture", "read-cap", clock="reader")

    def test_raw_close_watchdog(self):
        for name in ("close-backward", "close-deadline"):
            with self.subTest(fault=name):
                self.case("capture", name)

    def test_allocator_alignment_exhaustion_fragmentation_and_wipe(self):
        subprocess.run([self.directory / "arena"], check=True, capture_output=True, timeout=5)


if __name__ == "__main__":
    unittest.main()
