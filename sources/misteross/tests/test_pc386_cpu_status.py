# SPDX-License-Identifier: MIT
"""pc386_cpu_status.py's remote loop and record decoding, on lines as the
kit's busybox loop prints them. No kit needed."""
import unittest

from scripts import pc386_cpu_status as reader

MAGIC = reader.MAGIC


def lines(n: int, s0: int, s1: int, name_last: int | None = None) -> list[str]:
    head = MAGIC << 32 | (n + 1)
    name = n << 32 | MAGIC
    return ['S 123.45 0x%016X' % head, 'W 0 0x%016X' % name, 'W 1 0x%016X' % s0, 'W 2 0x%016X' % s1,
            'W 3 0x%016X' % (7 << 32 | 1234), 'W 5 0x%016X' % (48000 << 32), 'W 7 0x%016X' % 61440000,
            'W 0 0x%016X' % (name if name_last is None else name_last), 'E']


class StatusReader(unittest.TestCase):
    def test_decodes_a_whole_record(self):
        # Retired 1000, two CPU resets, POST 0x10, halted and running;
        # 12B3:000000DA after vector 08h.
        s0 = 1000 << 32 | 2 << 16 | 0x10 << 8 | 0x11
        s1 = 0x000000DA << 32 | 0x08 << 24 | 0x12B3
        (sample,) = reader.parse(lines(41, s0, s1), host_time=lambda: 5.0)
        self.assertTrue(sample['whole'])
        self.assertEqual(sample['record'], 41)
        self.assertEqual((sample['cs'], sample['eip'], sample['vector']), ('12b3', '000000da', '08'))
        self.assertEqual((sample['halted'], sample['running'], sample['stopped']), (1, 1, 0))
        self.assertEqual((sample['retired'], sample['cpu_resets'], sample['post']), (1000, 2, '10'))
        self.assertEqual((sample['frames_shown'], sample['scanout_underflows'], sample['samples']), (1234, 7, 48000))
        self.assertEqual(sample['core_seconds'], 1.0)
        self.assertEqual((sample['host_time'], sample['kit_uptime']), (5.0, 123.45))
        self.assertIn('12b3:000000da vec 08 HR', reader.line(sample))

    def test_rejects_a_record_rewritten_while_read(self):
        (sample,) = reader.parse(lines(41, 0, 0, name_last=(41 + 256) << 32 | MAGIC))
        self.assertFalse(sample['whole'])
        self.assertIn('torn record 41', reader.line(sample))

    def test_reports_no_ring_without_the_magic(self):
        (sample,) = reader.parse(['S 1.00 0xFFF4F0E8FFF4F0E8', 'W 0 0xFFF4F0E8FFF4F0E8', 'E'])
        self.assertFalse(sample['magic'])
        self.assertIn('no status ring', reader.line(sample))

    def test_skips_garbled_lines(self):
        samples = list(reader.parse(['S 1.00 garbage', 'W 1 0x1', 'E', *lines(0, 0, 0)]))
        self.assertEqual([s['record'] for s in samples], [0])

    def test_remote_loop_reads_the_newest_record(self):
        loop = reader.remote_loop(reader.SAMPLE_WORDS, 10, 0.05)
        self.assertIn('devmem 0x31000000 64', loop)
        self.assertIn('r=$(( 0x31001000 + 64 * (n & 255) ))', loop)
        self.assertIn('usleep 50000', loop)
        # Word 0 first and again last, so a rewritten record is seen.
        self.assertLess(loop.index('W 0'), loop.index('W 1'))
        self.assertGreater(loop.rindex('W 0'), loop.index('W 7'))


if __name__ == '__main__':
    unittest.main()
