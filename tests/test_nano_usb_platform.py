import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoUSBPlatformTests(unittest.TestCase):
    def test_actual_cold_scope_and_usb_lifecycle(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-usb-platform-') as temp:
            executable = pathlib.Path(temp) / 'usb-platform'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I', str(FW)]
            command.extend(str(FW / name) for name in (
                'nano_cold.c', 'nano_usb_platform.c', 'tests/nano_usb_platform_test.c'))
            subprocess.run([*command, '-o', str(executable)], check=True, timeout=30)
            result = subprocess.run([str(executable)], check=True, timeout=10,
                                    capture_output=True, text=True)
            self.assertIn('PASS USB-only trusted acquire/observe/drain/reset', result.stdout)


if __name__ == '__main__':
    unittest.main()
