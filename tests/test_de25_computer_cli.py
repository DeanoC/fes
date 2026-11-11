"""Check maintenance admission before any device access; never run on a kit."""
import pathlib
import platform
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


@unittest.skipUnless(shutil.which("c++") and shutil.which("make"), "native C++ build tools required")
class De25ComputerCliTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory(prefix="fes-de25-cli-")
        cls.addClassCleanup(cls.directory.cleanup)
        output = pathlib.Path(cls.directory.name)
        subprocess.run(["make", "-C", str(ROOT / "sources/libmister-runtime"),
                        "de25-computer", "CXX=c++", "BUILD_DIR=" + str(output)],
                       check=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        cls.binary = output / "tools/de25-computer"

    def invoke(self, *arguments):
        return subprocess.run([str(self.binary), *arguments], text=True,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)

    def test_help_requires_no_board(self):
        result = self.invoke("--help")
        self.assertEqual(result.returncode, 0)
        self.assertIn("no FPGA programming or daemon admission", result.stdout)

    def test_malformed_identity_and_address_refuse_before_admission(self):
        boot = "11111111-2222-3333-4444-555555555555"
        build = "1" * 32
        cases = [("invalid", build, "0x20010300", "probe", "invalid boot UUID"),
                 (boot, "0" * 32, "0x20010300", "probe", "invalid selected build ID"),
                 (boot, build, "0x20110300", "probe", "invalid explicit CPU resource base"),
                 (boot, build, "0x120010300", "probe", "invalid explicit CPU resource base"),
                 (boot, build, "0x20010300", "program", "unknown maintenance command")]
        for *arguments, message in cases:
            with self.subTest(arguments=arguments):
                result = self.invoke(*arguments)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)

    @unittest.skipIf(platform.machine().lower() in ("aarch64", "arm64"), "never attempt native admission on an ARM kit")
    def test_other_architecture_refuses_even_valid_probe(self):
        result = self.invoke("11111111-2222-3333-4444-555555555555", "1" * 32,
                             "0x20010300", "probe")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires AArch64 Linux", result.stderr)
