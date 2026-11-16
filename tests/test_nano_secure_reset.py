"""Execute the real held-lock controller and generated cold provider off-board."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'
sys.path.insert(0, str(FW))
import prepare_nano_owned
from verify import function


class NanoSecureResetTests(unittest.TestCase):
    def test_held_lock_reset_admission_and_consumed_failures(self):
        platform = (FW / 'nano_bridge_platform.inc').read_text()
        bodies = ''.join(function(platform, name) for name in (
            'fes_nano_bridge_platform_lock', 'fes_nano_bridge_platform_unlock',
            'fes_nano_bridge_platform_invalidate_held',
            'fes_nano_bridge_platform_invalidate', 'fes_nano_reset_binding',
            'fes_nano_bridge_platform_cold_reset_held'))
        names = ('socfpga_pwr_domain_on', 'socfpga_pwr_domain_off',
                 'socfpga_pwr_domain_suspend', 'socfpga_pwr_domain_on_finish',
                 'socfpga_pwr_domain_suspend_finish', 'socfpga_system_off',
                 'socfpga_system_reset', 'socfpga_system_reset2')
        # Exercise the real generator, rather than maintaining a test copy of
        # the new cold path. The existing PSCI entries stay separate.
        generated = prepare_nano_owned.psci_source(
            '\n'.join('void ' + name + '(void) {}' for name in names))
        provider = function(generated, 'fes_nano_platform_cold_reset_request')
        with tempfile.TemporaryDirectory(prefix='fes-held-reset-') as temp:
            base = Path(temp)
            (base / 'reset_body.inc').write_text(bodies + provider)
            for cache in (0, 1):
                binary = base / ('reset-' + str(cache))
                result = subprocess.run([
                    'cc', '-std=c11', '-O1', '-g', '-Wall', '-Wextra', '-Werror',
                    '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                    '-DFES_NANO_COLD_SERIAL=1', '-DCACHE_FLUSH=' + str(cache),
                    '-I' + str(FW), '-I' + temp,
                    str(FW / 'nano_boot.c'), str(FW / 'nano_bridges.c'),
                    str(FW / 'tests/nano_secure_reset_test.c'), '-o', str(binary)],
                    capture_output=True, text=True, timeout=40)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                result = subprocess.run([str(binary)], capture_output=True,
                                        text=True, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn('PASS 27 held-lock reset cases', result.stdout)


if __name__ == '__main__':
    unittest.main()
