"""Compile the real scan producer with controlled APIs; never open a device."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FIXTURE=ROOT/'tests/fixtures/nano-jtag-diagnostic'
CABLE='DE25-Nano on localhost:1234 [3-11.1-iface0]'
spec=importlib.util.spec_from_file_location('scan_programming',ROOT/'platform/embedded/nano/jtag_programming.py')
policy=importlib.util.module_from_spec(spec);spec.loader.exec_module(policy)


class ScanTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();cls.binary=Path(cls.temp.name)/'scan'
        result=subprocess.run(['g++','-std=c++17','-Wall','-Wextra','-Werror','-fsanitize=undefined',
            '-fno-sanitize-recover=all','-I'+str(FIXTURE),str(ROOT/'platform/embedded/nano/jtag_scan.cpp'),
            str(FIXTURE/'mock.cpp'),'-ldl','-o',str(cls.binary)],capture_output=True,text=True,timeout=30)
        if result.returncode:raise AssertionError(result.stderr)

    @classmethod
    def tearDownClass(cls):cls.temp.cleanup()

    def probe(self,mode='',args=None,environment=None):
        env=os.environ|dict(QUARTUS_JTAG_CLIENT_CONFIG='/tmp/fes-controlled-client.conf',
            QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER='1',FES_MOCK_FAIL=mode,FES_MOCK_FIXED_CLOCK='1')
        return subprocess.run([str(self.binary),*([CABLE] if args is None else args)],
            env=env|(environment or {}),capture_output=True,text=True,timeout=5)

    def transcript(self,result):
        # A mock executable has no vendor maps/binding. Supply only that fixture
        # metadata; every phase, clock, device and result comes from real C++.
        rows=[r for r in result.stdout.splitlines() if not r.startswith('MOCK_CALL ')]
        self.assertEqual(sum(r.startswith('FES_JTAG_SCAN BINDING ') for r in rows),1)
        rows=[r for r in rows if not r.startswith('FES_JTAG_SCAN BINDING ')]
        prefix=['FES_JTAG_SCAN LIBRARY '+p for p in policy.LIBRARIES]
        return ('\n'.join(prefix+['FES_JTAG_SCAN BINDING forced-scan '+policy.BINDING]+rows)+'\n').encode()

    def identity(self):
        return dict(serial='TRWU8CBL',source_commit='1'*40,claim_token='2'*16,pid=1234,
            start_ticks='5678',cable=CABLE,config_sha256='3'*64,clock='3M',auto_adjust=0,
            client_sha256={p:'4'*64 for p in policy.LIBRARIES},probe_sha256=hashlib.sha256(self.binary.read_bytes()).hexdigest())

    def test_success_unknown_and_observed_type_two_require_fixed_clock(self):
        for mode in ('','unknown-chain','unrecognized-chain'):
            with self.subTest(mode=mode):
                result=self.probe(mode);self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                calls=re.findall(r'^MOCK_CALL (.+)$',result.stdout,re.M)
                self.assertEqual(calls,['find','name','lock','auto-before','clock','forced-scan','cached-after','clock-after','auto-after','unlock'])
                raw=self.transcript(result)
                self.assertEqual(policy.live_scan(raw,CABLE),hashlib.sha256(raw).hexdigest())

    def test_api_and_identity_failure_stop_without_rescan_or_cached_fallback(self):
        expected={
            'find':['find'], 'name':['find','name'], 'wrong-name':['find','name'],
            'unterminated-name':['find','name'], 'lock':['find','name','lock'],
            'auto-before':['find','name','lock','auto-before','unlock'],
            'auto-enabled':['find','name','lock','auto-before','unlock'],
            'clock':['find','name','lock','auto-before','clock','unlock'],
            'wrong-clock':['find','name','lock','auto-before','clock','unlock'],
            'forced-scan':['find','name','lock','auto-before','clock','forced-scan','unlock'],
            'cached-after':['find','name','lock','auto-before','clock','forced-scan','cached-after','unlock'],
        }
        full=['find','name','lock','auto-before','clock','forced-scan','cached-after','clock-after','auto-after','unlock']
        for mode in (*expected,'wrong-count','wrong-id','wrong-mask','wrong-ir','clock-after','clock-drift','auto-after','auto-drift','unlock'):
            with self.subTest(mode=mode):
                result=self.probe(mode);self.assertEqual(result.returncode,1,result.stdout+result.stderr)
                calls=re.findall(r'^MOCK_CALL (.+)$',result.stdout,re.M)
                if mode in ('wrong-count','wrong-id','wrong-mask','wrong-ir'):wanted=full[:7]+['unlock']
                elif mode in ('clock-after','clock-drift'):wanted=full[:8]+['unlock']
                else:wanted=expected.get(mode,full)
                self.assertEqual(calls,wanted)
                with self.assertRaises(ValueError):policy.live_scan(self.transcript(result),CABLE)

    def test_invalid_target_environment_or_port_calls_no_api(self):
        for args,env in [([],{}),([CABLE,'program'],{}),([CABLE.replace('3-11.1','3-11.3')],{}),
            ([CABLE.replace('localhost','foreign')],{}),([CABLE.replace('1234','0')],{}),
            ([CABLE.replace('1234','65536')],{}),([CABLE],{'QUARTUS_JTAG_CLIENT_CONFIG':'relative'}),
            ([CABLE],{'QUARTUS_JTAG_CLIENT_NO_LOCAL_SERVER':'0'})]:
            with self.subTest(args=args,env=env):
                result=self.probe(args=args,environment=env)
                self.assertEqual(result.returncode,2);self.assertNotIn('MOCK_CALL',result.stdout)

    def test_actual_producer_to_durable_one_use_attempt_and_injected_program(self):
        with tempfile.TemporaryDirectory() as temp:
            consumed=Path(temp)/'attempt.json';events=[]
            def scan(cable):
                self.assertEqual(cable,CABLE)
                self.assertEqual(json.loads(consumed.read_text())['state'],'reserved-before-scan')
                events.append('scan');result=self.probe('unrecognized-chain')
                self.assertEqual(result.returncode,0);return self.transcript(result)
            def program(cable):
                events.append('program');self.assertEqual(cable,CABLE)
                record=json.loads(consumed.with_name('attempt.json.scan.json').read_text())
                self.assertEqual(record['state'],'consumed-before-programming')
                self.assertEqual(record['image_sha256'],'a'*64)
                return 'controlled callback only'
            attempt=policy.ProgramAttempt(consumed,self.identity,scan,program)
            record,result=attempt.run('recovery','a'*64)
            self.assertEqual(result,'controlled callback only');self.assertEqual(events,['scan','program'])
            self.assertEqual(record['transport'],self.identity())
            with self.assertRaises(ValueError):attempt.run('recovery','a'*64)
            with self.assertRaises(FileExistsError):policy.ProgramAttempt(consumed,self.identity,scan,program).run('recovery','a'*64)
            self.assertEqual(events,['scan','program'])

    def test_actual_failed_producer_durably_retires_before_program(self):
        for mode in ('forced-scan','wrong-count','wrong-id','wrong-mask','wrong-ir','unlock'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as temp:
                consumed=Path(temp)/'attempt.json';events=[]
                def scan(cable):
                    events.append('scan');return self.transcript(self.probe(mode))
                def program(cable):events.append('program')
                attempt=policy.ProgramAttempt(consumed,self.identity,scan,program)
                with self.assertRaises(ValueError):attempt.run('native','a'*64)
                self.assertEqual(events,['scan']);self.assertTrue(consumed.exists())
                self.assertFalse(consumed.with_name('attempt.json.scan.json').exists())
                with self.assertRaises(FileExistsError):policy.ProgramAttempt(consumed,self.identity,scan,program).run('native','a'*64)
                self.assertEqual(events,['scan'])


if __name__=='__main__':unittest.main()
