"""Check both clock windows, paged retirements, and the G5 cross-boundary pair."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from opportunity import opportunity

SOURCE = r'''
#include "pair_window.hpp"
struct Root {
    unsigned fes_i386_cpu__DOT__p5_x87__DOT__sample_bits = 0;
    unsigned fes_i386_cpu__DOT__p5_x87__DOT__pair_bits = 0;
} root;
int main(int argc, char**) {
    ReplayPairWindows win;
    const uint8_t mov[15] = {0xba, 1, 0}, add[15] = {0x01, 0xc3};
    auto retire = [&](unsigned pc, const uint8_t (&bytes)[15], unsigned len, bool pg = false, bool fault = false) {
        win.sample(&root);
        const std::uint64_t tag = (1ull << 33) | (std::uint64_t(len) << 24)
                                | (std::uint64_t(bytes[2]) << 16) | (bytes[1] << 8) | bytes[0];
        win.retire(pc, tag, fault, false, pg, bytes, pc, len);
    };
    if (argc == 1) {
        win.snapshot(3, true);
        retire(10, mov, 3);
        win.snapshot(4, true); // The G4 h2 window starts here.
        retire(13, add, 2);    // Cannot pair with an instruction outside h2.
        retire(15, mov, 3, true);
        retire(18, add, 2);
        retire(20, mov, 3, false, true);
        retire(23, add, 2);
        win.park();
        win.snapshot(10, true);
        retire(25, mov, 3);
        win.finish(11);
    } else {
        win.snapshot(1678571582, false);
        retire(10, mov, 3);
        win.snapshot(1678571583, false);
        retire(13, add, 2); // Pairs across the boundary in full G5 only.
        win.park();
        win.finish(1678571585);
    }
    win.write();
}
'''


class ReplayWindowTest(unittest.TestCase):
    def test_windows(self):
        with tempfile.TemporaryDirectory() as tmp:
            work = Path(tmp)
            src = work / 'test.cpp'
            src.write_text(SOURCE)
            binary = work / 'test'
            subprocess.run(['c++', '-Wall', '-Wextra', '-Werror', '-std=c++17',
                            '-I', str(Path(__file__).resolve().parent), str(src), '-o', str(binary)], check=True)
            def check(name, start, end, retired, pairs, paged, exceptions):
                d = json.loads((work / name).read_text())
                self.assertEqual([d[k] for k in ('from_cycle', 'to_cycle', 'cycles', 'retired', 'pairs',
                                                'paged_retirements', 'exceptions', 'parked_cycles')],
                                 [start, end, end - start, retired, pairs, paged, exceptions, 1])
                self.assertEqual(d['x87']['sampled_cpu_clocks'], end - start - 1)
                metrics = opportunity(d, 100_000_000)
                self.assertAlmostEqual(metrics['pairing_only_best_case_break_even_mhz'],
                                       100 * (end - start - pairs) / (end - start))
                d['x87']['sole_x87_issue_blocker'] += 1
                with self.assertRaises(ValueError):
                    opportunity(d, 100_000_000)
            subprocess.run([binary], cwd=work, check=True)
            check('pairing.json', 4, 10, 5, 1, 1, 1)
            self.assertFalse((work / 'post-first-render.json').exists())
            subprocess.run([binary, 'quake'], cwd=work, check=True)
            check('pairing.json', 1678571582, 1678571585, 2, 1, 0, 0)
            check('post-first-render.json', 1678571583, 1678571585, 1, 0, 0, 0)


if __name__ == '__main__':
    unittest.main()
