"""Actual diagnostic control-flow checks; no physical DMA or kernel syscalls."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
WRAPPERS = ('fcntl', 'close', 'clock_gettime', 'nanosleep', 'mmap', 'munmap', 'ioctl')


class OwnerScanoutTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.temp.name) / 'model'
        subprocess.run(['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-O2',
                        str(ROOT / 'tests/fixtures/de25_owner_scanout_model.c'),
                        *('-Wl,--wrap=' + name for name in WRAPPERS), '-o', str(cls.binary)],
                       check=True, capture_output=True)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def scenario(self, name):
        r = subprocess.run([str(self.binary), name], capture_output=True, text=True, timeout=5)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.last_stderr = r.stderr
        return json.loads(r.stdout.splitlines()[-1])

    def test_three_complete_frames_reuse_both_slots_before_one_reap(self):
        r = self.scenario('success')
        self.assertEqual(r, dict(result=0, allocations=1, maps=3, unmaps=3,
                                 publishes=3, polls=7, reaps=1, closes=1, seen_slots=3))

    def test_invalid_initial_grant_never_maps_or_publishes(self):
        for name in ('alloc-error', 'fcntl-error', 'no-cloexec', 'bad-version',
                     'bad-geometry', 'zero-build', 'zero-generation', 'initial-outstanding', 'mmap-error'):
            with self.subTest(name=name):
                r = self.scenario(name)
                self.assertEqual(r['result'], 1)
                self.assertEqual(r['publishes'], 0)
                self.assertEqual(r['reaps'], 0)
                self.assertEqual(r['maps'], 0)

    def test_owner_abi_refusal_precedes_allocation(self):
        for name in ('abi-error', 'old-abi'):
            with self.subTest(name=name):
                r = self.scenario(name)
                self.assertEqual(r['result'], 1)
                self.assertEqual(r['allocations'], 0)
                self.assertEqual(r['publishes'], 0)
                self.assertEqual(r['reaps'], 0)
                self.assertEqual(r['closes'], 0)

    def test_uncertainty_never_retries_or_reaps(self):
        for name in ('publish-error', 'poll-error', 'timeout', 'wrong-ack',
                     'changed-generation', 'changed-build', 'underflow', 'uncertain', 'clock-error', 'hardware-fault'):
            with self.subTest(name=name):
                r = self.scenario(name)
                self.assertEqual(r['result'], 1)
                self.assertEqual(r['publishes'], 1)
                self.assertEqual(r['reaps'], 0)
                self.assertEqual(r['closes'], 1)
                self.assertEqual(r['maps'], r['unmaps'])

    def test_fault_after_last_display_is_checked_before_reap(self):
        r = self.scenario('late-fault')
        self.assertEqual(r['publishes'], 3)
        self.assertEqual(r['result'], 1)
        self.assertEqual(r['reaps'], 0)

    def test_reap_failure_is_not_retried(self):
        r = self.scenario('reap-error')
        self.assertEqual(r['result'], 1)
        self.assertEqual(r['reaps'], 1)
        self.assertEqual(r['publishes'], 3)

    def test_failure_retains_cached_metadata_without_repoll_or_reap(self):
        for name in ('poll-error', 'clock-error'):
            r = self.scenario(name)
            self.assertIn('CACHED_FAILURE_INFO result=0 generation=7', self.last_stderr)
            self.assertIn('pending=1', self.last_stderr)
            self.assertEqual(r['reaps'], 0)
            self.assertEqual(r['publishes'], 1)
            self.assertEqual(r['polls'], 1 if name == 'poll-error' else 0)

    def test_allocation_observation_never_maps_publishes_or_polls(self):
        r = self.scenario('allocation-only')
        self.assertEqual(r, dict(result=0, allocations=1, maps=0, unmaps=0,
                                 publishes=0, polls=0, reaps=1, closes=1, seen_slots=0))
