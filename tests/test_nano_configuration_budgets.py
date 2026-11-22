"""Replay each fixed/service producer's own strict configuration deadline."""
import re
import unittest

import test_nano_native_trace as computer_fixture


def stretch(raw, durations):
    """Keep all proof/ownership rows, changing only sampled transfer counters."""
    lines = raw.splitlines(keepends=True)
    groups = {}
    for index, line in enumerate(lines):
        if 'FES_NANO_CORE_TRANSFER ' in line:
            generation = int(re.search(r'\bgeneration=(\d+)', line)[1])
            groups.setdefault(generation, []).append(index)
    for cycle, (generation, positions) in enumerate(groups.items()):
        elapsed_us = durations.get(generation, 10000)
        start = 0x10000000 + cycle * 30 * 400000000
        for ordinal, index in enumerate(positions):
            counter = start + elapsed_us * 400 * ordinal // (len(positions) - 1)
            lines[index], count = re.subn(r'\bcounter=[0-9a-f]+', f'counter={counter:x}', lines[index])
            if generation in durations:
                assert count == 1, 'the selected producer needs its real counter observations'
    return ''.join(lines)


class ConfigurationProducerBudgetTests(unittest.TestCase):
    def test_each_fixed_computer_reload_keeps_strict_twenty_seconds(self):
        fixture = computer_fixture.NativeComputerReloadTraceTests()
        fixture.setUp()
        raw = fixture.capture()
        for generation in (4, 6):
            for elapsed in (19999999, 20000000, 21000000, 24000000, 25000000):
                with self.subTest(generation=generation, elapsed=elapsed):
                    report = fixture.decode(stretch(raw, {generation: elapsed}))
                    self.assertEqual(report['complete'], elapsed < 20000000, report['errors'])
                    if elapsed >= 20000000:
                        self.assertIn('reload lacks bounded configuration timing', report['errors'])


if __name__ == '__main__':
    unittest.main()
