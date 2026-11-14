"""Programmer-shaped enumeration over controlled APIs; no device access."""
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FIXTURE=ROOT/'tests/fixtures/nano-jtag-enumeration'
SOURCE=ROOT/'platform/embedded/nano/jtag_enumeration.cpp'
CABLE='DE25-Nano on localhost:1234 [3-11.1-iface0]'
NORMAL=['register','find','name','lock','auto-before','clock-before','count','list','clock-after','auto-after','unlock']

class EnumerationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();cls.binary=Path(cls.temp.name)/'enumeration'
        r=subprocess.run(['g++','-std=c++17','-Wall','-Wextra','-Werror','-fsanitize=undefined',
            '-fno-sanitize-recover=all','-I'+str(FIXTURE),str(SOURCE),str(FIXTURE/'mock.cpp'),
            '-ldl','-o',str(cls.binary)],capture_output=True,text=True,timeout=30)
        if r.returncode:raise AssertionError(r.stderr)

    @classmethod
    def tearDownClass(cls):cls.temp.cleanup()

    def run_case(self,mode='',args=None,environment=None):
        env=os.environ|dict(QUARTUS_JTAG_CLIENT_CONFIG='/tmp/fes-enumeration-test.conf',
            QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER='1',FES_MOCK_FAIL=mode)
        return subprocess.run([str(self.binary),*([CABLE] if args is None else args)],
            env=env|(environment or {}),capture_output=True,text=True,timeout=5)

    def calls(self,result):return re.findall(r'^MOCK_CALL (.+)$',result.stdout,re.M)

    def test_two_pass_automatic_query_and_list_under_fixed_clock(self):
        r=self.run_case();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
        self.assertEqual(self.calls(r),NORMAL)
        self.assertIn('COUNT rc=47 count=1 automatic=1',r.stdout)
        self.assertIn('DEVICE count=1 id=4362C0DD mask=00000000 ir=10',r.stdout)
        self.assertIn('passed=1 unlocked=1 programming=0 cpu_reset=0 grant=0',r.stdout)

    def test_api_failures_stop_then_unlock_without_retry_or_fallback(self):
        for phase in NORMAL[1:]:
            expected=NORMAL[:NORMAL.index(phase)+1]
            if phase not in ('find','name','lock','unlock'):expected+=['unlock']
            r=self.run_case(phase)
            with self.subTest(phase=phase):
                self.assertEqual(r.returncode,1,r.stdout+r.stderr)
                self.assertEqual(self.calls(r),expected)
                self.assertNotIn('passed=1',r.stdout)

    def test_wrong_query_response_never_reads_list_or_cached_identity(self):
        for mode in ('zero-count','large-count','wrong-query-status'):
            r=self.run_case(mode)
            with self.subTest(mode=mode):
                self.assertEqual(r.returncode,1,r.stdout+r.stderr)
                self.assertEqual(self.calls(r),NORMAL[:7]+['unlock'])
                self.assertNotIn('DEVICE ',r.stdout)

    def test_wrong_device_or_post_read_clock_is_not_success(self):
        for mode in ('list-count','wrong-id','wrong-mask','wrong-ir','clock-drift','auto-drift'):
            r=self.run_case(mode)
            with self.subTest(mode=mode):
                self.assertEqual(r.returncode,1,r.stdout+r.stderr)
                self.assertNotIn('passed=1',r.stdout)
                self.assertEqual(self.calls(r).count('count'),1)
                self.assertEqual(self.calls(r).count('list'),1)
                self.assertEqual(self.calls(r).count('unlock'),1)

    def test_wrong_clock_or_auto_adjustment_prevents_enumeration(self):
        for mode,end in (('wrong-clock',6),('auto-enabled',5)):
            r=self.run_case(mode)
            with self.subTest(mode=mode):
                self.assertEqual(r.returncode,1,r.stdout+r.stderr)
                self.assertEqual(self.calls(r),NORMAL[:end]+['unlock'])

    def test_wrong_or_truncated_cable_prevents_lock(self):
        for mode in ('wrong-name','unterminated-name'):
            r=self.run_case(mode)
            with self.subTest(mode=mode):
                self.assertEqual(r.returncode,1,r.stdout+r.stderr)
                self.assertEqual(self.calls(r),NORMAL[:3])

    def test_private_top_only_arguments_and_environment_before_any_api(self):
        cases=[dict(args=[]),dict(args=[CABLE,CABLE]),dict(args=['DE25-Nano [bottom]']),
            dict(args=[CABLE.replace(':1234',':0')]),dict(args=[CABLE.replace(':1234',':65536')]),
            dict(args=[CABLE.replace('3-11.1','3-11.2')]),
            dict(environment={'QUARTUS_JTAG_CLIENT_CONFIG':'relative'}),
            dict(environment={'QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER':'0'})]
        for options in cases:
            r=self.run_case(**options)
            with self.subTest(options=options):
                self.assertEqual(r.returncode,2,r.stdout+r.stderr)
                self.assertEqual(self.calls(r),[])

    def test_vendor_messages_are_bounded_and_cannot_inject_records(self):
        r=self.run_case('messages');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
        messages=[l for l in r.stdout.splitlines() if l.startswith('FES_JTAG_ENUM MESSAGE')]
        self.assertEqual(len(messages),16)
        self.assertTrue(all('bytes=512 capped=1 hex=' in l and len(l.split('hex=')[1])==1024 for l in messages))
        r=self.run_case('injection');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
        self.assertNotIn('\nFES_JTAG_SCAN RESULT',r.stdout)

    def test_diagnostic_output_cannot_be_a_programming_grant(self):
        r=self.run_case();self.assertEqual(r.returncode,0,r.stdout+r.stderr)
        spec=importlib.util.spec_from_file_location('enumeration_policy',ROOT/'platform/embedded/nano/jtag_programming.py')
        policy=importlib.util.module_from_spec(spec);spec.loader.exec_module(policy)
        with self.assertRaises(ValueError):policy.live_scan(r.stdout.encode(),CABLE)

if __name__=='__main__':unittest.main(verbosity=2)
