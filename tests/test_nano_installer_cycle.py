"""Cycle/replay the captured C/menu model with explicit install/discovery mode."""
import copy
from pathlib import Path
import sys
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
from installer_cycle import exercise,replay
from test_nano_installer_client import Model
from test_nano_program_client import SPEC
from test_nano_tenfoot_trace import PROGRAMS

class Capture:
    def __init__(self,discovered=False,shared=True):
        self.model=Model(installed=discovered,shared=shared)
        self.client=self.model.client()
        self.raw=bytearray(self.model.initial)
        self.windows=[]
        self.startup=copy.deepcopy(self.client.view)
        self.boot=copy.deepcopy(self.client.boot)
        self.discovered=discovered
        self.options=dict(program=PROGRAMS[0],build_record=self.model.profile,build_id=SPEC['build_id'],
                          owner_seed=SPEC['owner_seed'],atf=SPEC['atf'],package=SPEC['package'],
                          shared_usb=self.model.policy if shared else None)
        self.client.exchange=self.exchange
    def exchange(self,payload,bound,done):
        start=len(self.raw);before=self.model.service.clock
        raw=self.model.exchange(payload,bound,lambda _:False)
        self.raw.extend(raw)
        elapsed=self.model.service.clock-before
        self.windows.append(dict(request_hex=payload.hex(),start=start,end=len(self.raw),
                                 bound_seconds=bound,status='received',elapsed_seconds=elapsed,
                                 offers=[dict(end=len(self.raw),elapsed_seconds=elapsed)]))
        self.windows[-1]['status']='received' if done(raw) else 'incomplete'
        return raw
    def run(self):
        def settle(seconds):self.model.service.clock+=seconds
        service=exercise(self.client,discovered=self.discovered,settle=settle)
        return dict(initial_capture_end=len(self.model.initial),startup_reply=self.startup,
                    startup_service_boot=self.boot,service=service,
                    command_windows=copy.deepcopy(self.windows),app_commands=copy.deepcopy(self.client.records))

class CycleTests(unittest.TestCase):
    def test_first_install_and_independent_discovery_cycles(self):
        for discovered in (False,True):
            with self.subTest(discovered=discovered):
                c=Capture(discovered);r=c.run()
                result=replay(bytes(c.raw),r,discovered=discovered,**c.options)
                self.assertEqual(result['discovered'],discovered)
                self.assertFalse(result['persistence_verified'])
                self.assertFalse(r['service']['hardware_acceptance'])
                self.assertFalse(r['service']['persistence_verified'])
                self.assertEqual(r['service']['exit_generation'],3 if discovered else 4)
                self.assertEqual(sum(v['action']=='install' for v in r['app_commands']),int(not discovered))
    def test_uart_only_model_has_no_fabricated_usb_reports(self):
        c=Capture(True,shared=False);r=c.run()
        self.assertIsNone(r['service']['shared_usb_reports'])
        self.assertIsNone(r['service']['shared_usb_polls'])
        replay(bytes(c.raw),r,discovered=True,**c.options)
    def test_context_mismatch_sends_nothing(self):
        for discovered in (False,True,None,1):
            c=Capture(not discovered if type(discovered) is bool else False)
            with self.subTest(discovered=discovered),self.assertRaises(ValueError):
                exercise(c.client,discovered=discovered,settle=lambda _:None)
            self.assertEqual(c.model.keys,[])
    def test_discovery_cannot_replay_install_wire_or_inverse(self):
        for discovered in (False,True):
            c=Capture(discovered);r=c.run()
            with self.subTest(discovered=discovered),self.assertRaises(ValueError):
                replay(bytes(c.raw),r,discovered=not discovered,**c.options)
    def test_operation_failure_retires_without_launch_or_retry(self):
        c=Capture();c.model.installer_fault='owner'
        with self.assertRaises(ValueError):c.run()
        self.assertTrue(c.client.retired);self.assertEqual(c.client.loads,0)
        before=list(c.model.keys)
        with self.assertRaises(ValueError):c.run()
        self.assertEqual(c.model.keys,before)
    def test_invalid_capture_offsets_polling_times_and_receipts_reject(self):
        c=Capture(True);r=c.run()
        mutations=[lambda r:r.update(initial_capture_end=True),
                   lambda r:r['command_windows'][0].update(start=0),
                   lambda r:r['command_windows'][-1].update(end=len(c.raw)+1),
                   lambda r:r['command_windows'][0].update(elapsed_seconds=float('nan')),
                   lambda r:r['command_windows'][0].update(bound_seconds=True),
                   lambda r:r['command_windows'][0].update(bound_seconds=10**1000),
                   lambda r:r['command_windows'][0].update(offers=[]),
                   lambda r:r['command_windows'][0]['offers'].append(copy.deepcopy(r['command_windows'][0]['offers'][0])),
                   lambda r:r['command_windows'][0]['offers'][0].update(elapsed_seconds=-1),
                   lambda r:r['command_windows'][0].update(request_hex='71'),
                   lambda r:r['service'].update(persistence_verified=True),
                   lambda r:r['app_commands'][-1].update(exit_generation=4),
                   lambda r:r['startup_reply']['native'].update(Owner=1)]
        for i,mutate in enumerate(mutations):
            bad=copy.deepcopy(r);mutate(bad)
            with self.subTest(i=i),self.assertRaises(ValueError):replay(bytes(c.raw),bad,discovered=True,**c.options)
    def test_wire_tampering_and_late_output_reject(self):
        c=Capture(True);r=c.run()
        for raw in (bytes(c.raw)+b'unexpected\n',bytes(c.raw).replace(b'"Underflows":0',b'"Underflows":1')):
            with self.assertRaises(ValueError):replay(raw,r,discovered=True,**c.options)

if __name__=='__main__':unittest.main()
