"""Always-available host checks for the unbound Nano firmware service."""
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
FIRMWARE = ROOT / "platform/embedded/firmware"


class NanoBridgeServiceTests(unittest.TestCase):
    def test_owned_closure_and_configuration_guard(self):
        with tempfile.TemporaryDirectory(prefix="fes-nano-bridge-service-") as temp:
            executable = pathlib.Path(temp) / "service"
            subprocess.run(["cc", "-std=c11", "-Wall", "-Wextra", "-Werror",
                            "-fsanitize=undefined", "-fno-sanitize-recover=all",
                            "-I" + str(FIRMWARE), str(FIRMWARE / "nano_bridges.c"),
                            str(FIRMWARE / "tests/nano_bridges_test.c"), "-o", str(executable)],
                           check=True, capture_output=True, text=True)
            result = subprocess.run([str(executable)], check=True, capture_output=True, text=True)
            self.assertIn("PASS 62 firmware service cases", result.stdout)


if __name__ == "__main__":
    unittest.main()
