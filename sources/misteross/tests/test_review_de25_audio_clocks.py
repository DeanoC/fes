"""Fail-closed coverage for fitted audio observations, never timing admission."""
import csv
import io
from pathlib import Path
import tempfile
import unittest

from boards.de25nano.producers import review_audio_clocks as review


class AudioObservationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='fes-audio-observations-')
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.clocks = [{'clock': review.MASTER, 'period_ns': '81.379',
                        'waveform': '0 40.689', 'edges': '', 'master': 'refclk'}]
        for label, period, waveform, edges in (
                ('bit_clock', '325.516', '81.379 244.137', '3 7 11'),
                ('word_clock', '20833.024', '10335.133 20751.645', '255 511 767')):
            name = 'de25_i2s_' + label
            self.clocks.extend([
                {'clock': name, 'period_ns': period, 'waveform': waveform,
                 'edges': edges, 'master': review.MASTER},
                {'clock': name + '_output', 'period_ns': period, 'waveform': waveform,
                 'edges': '', 'master': name},
            ])
        self.paths = [{'corner': corner, 'clock': clock, 'path': path, 'bound': bound,
                       'count': '1', 'delay_ns': str({'network': {'min': 2, 'max': 3},
                                                    'output': {'min': 4, 'max': 6}}[path][bound])}
                      for corner in sorted(review.CORNERS) for clock in ('bit_clock', 'word_clock')
                      for path in ('network', 'output') for bound in ('min', 'max')]

    def observe(self):
        for name, rows in (('clocks.tsv', self.clocks), ('paths.tsv', self.paths)):
            stream = io.StringIO()
            writer = csv.DictWriter(stream, fieldnames=rows[0].keys(), delimiter='\t')
            writer.writeheader()
            writer.writerows(rows)
            (self.directory / name).write_text(stream.getvalue())
        return review.observations(self.directory)

    def test_complete_reports_keep_unmeasured_skew_out_of_the_budget(self):
        result = self.observe()
        self.assertEqual(result['fitted_path_count'], 32)
        self.assertEqual(result['fitted_corner_count'], 4)
        for row in result['conditional_pin_budgets']:
            self.assertEqual(row['setup_budget_before_unmodeled_skew_and_jitter_ns'], '157.758')
            self.assertEqual(row['hold_budget_before_unmodeled_skew_and_jitter_ns'], '157.758')
        self.assertNotIn('package_timing_admission', result)

    def test_frequency_only_or_wrongly_forwarded_clocks_are_rejected(self):
        for field, value in (('edges', '1 5 9'), ('waveform', '0 162.758'),
                             ('period_ns', '325'), ('master', 'other_clock')):
            original = self.clocks[1][field]
            self.clocks[1][field] = value
            with self.subTest(field=field), self.assertRaises(review.BuildError):
                self.observe()
            self.clocks[1][field] = original
        self.clocks[2]['master'] = review.MASTER
        with self.assertRaises(review.BuildError):
            self.observe()

    def test_master_clock_cannot_drift_to_another_rate(self):
        self.clocks[0]['period_ns'] = '80'
        with self.assertRaises(review.BuildError):
            self.observe()

    def test_missing_corner_or_duplicate_path_never_counts_as_coverage(self):
        removed = self.paths.pop()
        with self.assertRaises(review.BuildError):
            self.observe()
        self.paths.append(removed)
        self.paths.append(self.paths[0])
        with self.assertRaises(review.BuildError):
            self.observe()

    def test_invalid_path_numbers_and_counts_are_rejected(self):
        for value in ('NaN', 'Infinity', '-1', 'not-a-number'):
            self.paths[0]['delay_ns'] = value
            with self.subTest(value=value), self.assertRaises(review.BuildError):
                self.observe()
        self.paths[0]['delay_ns'] = '4'
        self.paths[0]['count'] = '0'
        with self.assertRaises(review.BuildError):
            self.observe()

    def test_inverted_min_max_is_rejected(self):
        self.paths[0]['delay_ns'] = '10'
        with self.assertRaises(review.BuildError):
            self.observe()
