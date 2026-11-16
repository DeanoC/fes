"""Real one-use scan admission with injected dispatch and durable failures."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import native_program_dispatch as native
from test_nano_jtag_programming import jtag, identity, RAW, IMAGE


class DispatchTests(unittest.TestCase):
    def attempt(self, directory, *, scan=None, program=None, observe=None):
        marker = Path(directory)/'native-execution-attempted.json'
        bridge = native.NativeProgramDispatch(marker, image_sha256=IMAGE,
                    transport_epoch='1234:5678', owns=lambda:True)
        calls = []
        def selected(cable):
            calls.append(cable)
            self.assertEqual(json.loads(marker.read_text()), bridge.binding)
            return 'programmed'
        def admitted(dispatch):
            gate = jtag.ProgramAttempt(Path(directory)/'native-program-consumed.json',
                        observe or identity, scan or (lambda _:RAW), dispatch)
            return gate.run('native', IMAGE)
        return bridge, admitted, program or selected, calls

    def test_inventory_failure_cannot_mark_or_call_native_program(self):
        with tempfile.TemporaryDirectory() as directory:
            def failed(_):raise ValueError('vendor FPGA list differs')
            bridge, admitted, program, calls = self.attempt(directory, scan=failed)
            with self.assertRaisesRegex(ValueError, 'vendor FPGA list differs'):
                bridge.run(admitted, program)
            self.assertFalse(bridge.marker.exists());self.assertEqual(calls, [])
            result = bridge.observation()
            self.assertTrue(result['operation_finished']);self.assertFalse(result['programming_may_have_started'])
            self.assertFalse(result['recovery_authority'])
            with self.assertRaises(ValueError):bridge.run(admitted, program)
            other, retry, program, calls = self.attempt(directory)
            with self.assertRaises(FileExistsError):other.run(retry, program)
            self.assertEqual(calls, [])

    def test_scan_clock_drift_and_last_ownership_change_never_dispatch(self):
        for failing in (2, 3):
            with self.subTest(observation=failing),tempfile.TemporaryDirectory() as directory:
                count = 0
                def observe():
                    nonlocal count
                    count += 1
                    value=identity()
                    if count==failing:value['clock']='15M'
                    return value
                bridge, admitted, program, calls = self.attempt(directory, observe=observe)
                with self.assertRaises(ValueError):bridge.run(admitted, program)
                self.assertEqual(calls, []);self.assertFalse(bridge.marker.exists())
                self.assertFalse(bridge.observation()['programming_may_have_started'])

    def test_durable_marker_precedes_the_actual_admitted_program(self):
        with tempfile.TemporaryDirectory() as directory:
            bridge, admitted, program, calls = self.attempt(directory)
            record, result = bridge.run(admitted, program)
            self.assertEqual(result, 'programmed');self.assertEqual(calls, [identity()['cable']])
            self.assertEqual(record['state'],'consumed-before-programming')
            self.assertTrue(bridge.observation()['programming_may_have_started'])
            other, retry, _, _ = self.attempt(directory)
            with self.assertRaises(FileExistsError):other.run(retry, program)

    def test_program_failure_and_partial_marker_persistence_remain_uncertain(self):
        for failing in ('program', 'file-fsync', 'directory-fsync'):
            with self.subTest(failing=failing), tempfile.TemporaryDirectory() as directory:
                def failed(_):raise RuntimeError('programmer failure')
                bridge, admitted, program, calls = self.attempt(directory, program=failed if failing=='program' else None)
                real=native.os.fsync;count=0
                def fsync(fd):
                    nonlocal count
                    count+=1
                    # Existing ProgramAttempt persists reservation+scan before
                    # our marker: four earlier fsync calls stay unchanged.
                    if count==(5 if failing=='file-fsync' else 6 if failing=='directory-fsync' else -1):
                        raise OSError('marker fsync failed')
                    real(fd)
                with patch.object(native.os,'fsync',fsync),self.assertRaises((RuntimeError,OSError)):
                    bridge.run(admitted,program)
                self.assertEqual(calls, []);self.assertTrue(bridge.entered)
                self.assertTrue(bridge.observation()['programming_may_have_started'])
                with self.assertRaises(ValueError):bridge.run(admitted,program)

    def test_unknown_active_operation_or_lost_owner_cannot_report_no_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            bridge, admitted, program, _ = self.attempt(directory)
            self.assertTrue(bridge.observation()['programming_may_have_started'])
            bridge.owns=lambda:False
            with self.assertRaises(ValueError):bridge.run(admitted,program)
            self.assertTrue(bridge.observation()['programming_may_have_started'])
            bridge.owns=lambda:(_ for _ in ()).throw(ValueError('owner unavailable'))
            self.assertTrue(bridge.observation()['programming_may_have_started'])

    def test_admission_cannot_claim_success_without_dispatch_or_call_twice(self):
        for mode in ('none','twice'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                bridge, _, program, calls = self.attempt(directory)
                def admitted(dispatch):
                    if mode=='none':return 'foreign success'
                    dispatch(identity()['cable'])
                    try:dispatch(identity()['cable'])
                    except ValueError:pass
                    return 'swallowed duplicate'
                with self.assertRaises(ValueError):bridge.run(admitted,program)
                self.assertEqual(len(calls),int(mode=='twice'))
                self.assertIs(bridge.observation()['programming_may_have_started'],mode=='twice')

    def test_swallowed_program_failure_still_cannot_report_success(self):
        with tempfile.TemporaryDirectory() as directory:
            bridge, _, _, _ = self.attempt(directory)
            def admitted(dispatch):
                try:dispatch(identity()['cable'])
                except RuntimeError:pass
                return 'foreign success after failed program'
            def program(_):raise RuntimeError('program failed')
            with self.assertRaises(ValueError):bridge.run(admitted,program)
            self.assertTrue(bridge.observation()['programming_may_have_started'])

    def test_missing_marker_after_dispatch_or_inaccessible_marker_stays_uncertain(self):
        with tempfile.TemporaryDirectory() as directory:
            bridge, admitted, program, _ = self.attempt(directory)
            bridge.run(admitted,program);bridge.marker.unlink()
            self.assertTrue(bridge.observation()['programming_may_have_started'])
        with tempfile.TemporaryDirectory() as directory:
            bridge, _, _, _ = self.attempt(directory)
            bridge.finished=True
            with patch.object(native.Path,'exists',side_effect=PermissionError('unreadable marker')):
                self.assertTrue(bridge.observation()['programming_may_have_started'])

    def test_lost_ownership_cannot_be_cleared_by_later_successful_guard(self):
        with tempfile.TemporaryDirectory() as directory:
            bridge, admitted, program, calls = self.attempt(directory)
            bridge.owns=lambda:False
            with self.assertRaises(ValueError):bridge.run(admitted,program)
            bridge.owns=lambda:True
            observation=bridge.observation()
            self.assertTrue(observation['programming_may_have_started'])
            self.assertTrue(observation['ownership_failed'])
            self.assertFalse(observation['ownership_verified'])
            self.assertEqual(calls,[])

if __name__=='__main__':unittest.main()
