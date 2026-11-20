"""Replay each fixed/service producer's own strict configuration deadline."""
import re
import unittest

import test_nano_native_trace as computer_fixture
import test_nano_window_reload_trace as window_fixture
import test_nano_window_service_trace as service_fixture


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

    def test_fixed_window_reload_keeps_strict_twenty_seconds(self):
        fixture = window_fixture.WindowReloadTraceTests()
        fixture.setUp()
        for elapsed in (19999999, 20000000, 21000000, 24000000, 25000000):
            with self.subTest(elapsed=elapsed):
                raw = fixture.first.raw + fixture.begin + stretch(fixture.program, {10: elapsed}) + fixture.window + fixture.receipt
                report = fixture.decode(raw)
                self.assertEqual(report['complete'], elapsed < 20000000, report['errors'])
                if elapsed >= 20000000:
                    self.assertIn('window reload lacks bounded current configuration timing', report['errors'])

    def test_window_startup_twenty_and_service_relaunch_twenty_five(self):
        fixture = service_fixture.WindowServiceTraceTests()
        fixture.setUp()
        for elapsed in (19999999, 20000000, 21000000, 24000000, 25000000):
            with self.subTest(startup_us=elapsed):
                packet = fixture.packet(fixture.initial, stretch(fixture.cold, {4: elapsed}) + fixture.open)
                if elapsed < 20000000:
                    self.assertEqual(fixture.startup(packet)['reply']['state'], 'running')
                else:
                    with self.assertRaisesRegex(ValueError, 'configuration exceeded declared source budget'):
                        fixture.startup(packet)
        previous = fixture.reply('return', 2, 2, 4, closed=4, state='ready')
        launched = fixture.reply('launch', 3, 1, 10, closed=4)
        for elapsed in (19999999, 20000000, 24000000, 24999999, 25000000, 25000001):
            with self.subTest(service_us=elapsed):
                packet = fixture.packet(launched, stretch(fixture.warm_prefix(10, 4), {10: elapsed}))
                if elapsed < 25000000:
                    self.assertEqual(fixture.transition(packet, previous, 'launch')['reply']['state'], 'running')
                else:
                    with self.assertRaisesRegex(ValueError, 'configuration exceeded declared source budget'):
                        fixture.transition(packet, previous, 'launch')


if __name__ == '__main__':
    unittest.main()
