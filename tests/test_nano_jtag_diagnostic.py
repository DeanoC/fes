"""Exercise the scan-only tool with a controlled API, never a board."""
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
SOURCE=ROOT/'platform/embedded/nano/jtag_diagnostic.cpp'
FIXTURE=ROOT/'tests/fixtures/nano-jtag-diagnostic'
CABLE='DE25-Nano on localhost:1234 [3-11.1-iface0]'
CAPTURES=['capture-'+name for name in ('CapturedDR','CapturedIR','CapturedBypass','NormalBypass')]


class JtagDiagnosticTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();cls.binary=Path(cls.temp.name)/'diagnostic'
        result=subprocess.run(['g++','-std=c++17','-Wall','-Wextra','-Werror','-fsanitize=undefined',
            '-fno-sanitize-recover=all','-I'+str(FIXTURE),str(SOURCE),str(FIXTURE/'mock.cpp'),
            '-ldl','-o',str(cls.binary)],capture_output=True,text=True,timeout=30)
        if result.returncode:raise AssertionError(result.stderr)

    @classmethod
    def tearDownClass(cls):cls.temp.cleanup()

    def run_case(self,mode='',args=None,environment=None):
        env=os.environ|dict(QUARTUS_JTAG_CLIENT_CONFIG='/tmp/fes-fake-private.conf',
            QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER='1',FES_MOCK_FAIL=mode)
        return subprocess.run([str(self.binary),*(args if args is not None else [CABLE])],
            env=env|(environment or {}),capture_output=True,text=True,timeout=5)

    def test_one_scan_captures_and_no_programming_grant(self):
        normal=['register','find','name','lock','clock','auto-before','forced-scan','cached-after',*CAPTURES,'clock-after','auto-after','unlock']
        cases=[('',normal,0)]
        for phase in ('find','name','lock','clock','forced-scan','cached-after','unlock'):
            calls=normal[:normal.index(phase)+1]
            if phase=='forced-scan':calls+=CAPTURES+['clock-after','auto-after','unlock']
            elif phase=='cached-after':calls+=CAPTURES+['clock-after','auto-after','unlock']
            elif phase=='clock':calls+=['unlock']
            cases.append((phase,calls,1))
        cases += [('wrong-name',normal[:3],1),('wrong-clock',normal[:5]+['unlock'],1)]
        cases += [(mode,normal,1) for mode in ('wrong-count','wrong-id','wrong-ir')]
        for mode,calls,code in cases:
            with self.subTest(mode=mode):
                result=self.run_case(mode)
                self.assertEqual(result.returncode,code,result.stdout+result.stderr)
                self.assertEqual(re.findall(r'^MOCK_CALL (.+)$',result.stdout,re.M),calls)
                self.assertLessEqual(result.stdout.count('MOCK_CALL forced-scan'),1)
                self.assertNotIn('\nFES_JTAG_SCAN RESULT pass=1',result.stdout)
                if 'unlock' in calls:self.assertIn('programming=0 cpu_reset=0 grant=0',result.stdout)
                if mode=='forced-scan':
                    self.assertIn('FES_JTAG_DIAG MESSAGE level=0',result.stdout)
                    self.assertIn('scan_ok=0 identity_ok=0',result.stdout)
                    self.assertNotIn('MOCK_CALL cached-after',result.stdout)
                # Even a successful diagnostic is not a valid scan grant.
                spec=importlib.util.spec_from_file_location('diagnostic_gate',ROOT/'platform/embedded/nano/jtag_programming.py')
                gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)
                with self.assertRaises(ValueError):gate.live_scan(result.stdout.encode(),CABLE)

    def test_capture_bounds_and_unsupported_values_are_explicit(self):
        for mode in ('full','oversized','unsupported','messages'):
            with self.subTest(mode=mode):
                result=self.run_case(mode);self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertEqual(result.stdout.count('MOCK_CALL forced-scan'),1)
                rows=[line for line in result.stdout.splitlines() if line.startswith('FES_JTAG_DIAG CAPTURE')]
                self.assertEqual(len(rows),4)
                if mode=='full':self.assertTrue(all('bytes=256 valid_length=1 hex='+'41'*256 in line for line in rows))
                if mode=='oversized':self.assertTrue(all(line.endswith('bytes=257 valid_length=0 hex=') for line in rows))
                if mode=='unsupported':self.assertTrue(all('rc=126' in line and line.endswith('hex=') for line in rows))
                if mode=='messages':
                    messages=[line for line in result.stdout.splitlines() if line.startswith('FES_JTAG_DIAG MESSAGE')]
                    self.assertEqual(len(messages),16)
                    self.assertTrue(all('bytes=512 capped=1 hex='+'58'*512 in line for line in messages))

    def test_bad_target_environment_or_arguments_call_no_api(self):
        cases=[([CABLE.replace('3-11.1','3-11.3')],{}),([CABLE.replace('1234','0')],{}),
            ([CABLE.replace('1234','65536')],{}),([CABLE.replace('localhost','elsewhere')],{}),
            ([CABLE,'program'],{}),([],{ }),(None,{'QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER':'0'}),
            (None,{'QUARTUS_JTAG_CLIENT_CONFIG':'relative.conf'})]
        for args,environment in cases:
            with self.subTest(args=args,environment=environment):
                result=self.run_case(args=args,environment=environment)
                self.assertEqual(result.returncode,2);self.assertIn('REFUSE',result.stdout)
                self.assertNotIn('MOCK_CALL',result.stdout)

    def test_context_query_errors_stay_explicit_and_never_rescan(self):
        for phase in ('auto-before','clock-after','auto-after'):
            with self.subTest(phase=phase):
                result=self.run_case(phase)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertEqual(result.stdout.count('MOCK_CALL forced-scan'),1)
                self.assertEqual(result.stdout.count('MOCK_CALL cached-after'),1)
                self.assertIn('rc=87 value=',result.stdout)
                self.assertIn('programming=0 cpu_reset=0 grant=0',result.stdout)

    def test_name_refusal_reports_predicates_without_locking(self):
        for mode,shape in (
            ('wrong-name','valid_length=1 matches=0 chain_type=1'),
            ('unterminated-name','bytes=512 valid_length=0 matches=0 chain_type=1')):
            with self.subTest(mode=mode):
                result=self.run_case(mode)
                self.assertEqual(result.returncode,1,result.stdout+result.stderr)
                self.assertIn(shape,result.stdout)
                self.assertEqual(re.findall(r'^MOCK_CALL (.+)$',result.stdout,re.M),['register','find','name'])
                self.assertNotIn('FES_JTAG_DIAG CABLE ',result.stdout)

    def test_raw_chain_type_is_observation_and_never_a_programming_grant(self):
        for mode,value in [('unknown-chain',0),('unrecognized-chain',2)]:
            with self.subTest(mode=mode):
                result=self.run_case(mode)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                self.assertIn('matches=1 chain_type='+str(value),result.stdout)
                self.assertEqual(result.stdout.count('MOCK_CALL forced-scan'),1)
                self.assertIn('grant=0',result.stdout)
                spec=importlib.util.spec_from_file_location('diagnostic_gate',ROOT/'platform/embedded/nano/jtag_programming.py')
                gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)
                with self.assertRaises(ValueError):gate.live_scan(result.stdout.encode(),CABLE)


if __name__=='__main__':unittest.main()
