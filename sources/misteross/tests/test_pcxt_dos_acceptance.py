"""Regress the application-fixture and paired-frame acceptance boundaries."""
import hashlib
import json
import os
from pathlib import Path
import signal
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import pcxt_acceptance_media as media
import sim_fes_pcxt_dos as simulation


class AcceptanceMediaTest(unittest.TestCase):
    @unittest.skipUnless(all(shutil.which(x) for x in ('as', 'ld', 'mcopy')), 'binutils and mtools required')
    def test_com_recreates_pinned_disk_and_rejects_damage(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'com.img'
            receipt = media.build('com', output)
            self.assertEqual(receipt['image_sha256'], media.SCENARIOS['com']['image_sha256'])
            self.assertEqual(receipt['program_sha256'], media.SCENARIOS['com']['program_sha256'])
            original = output.read_bytes()
            media.build('com', output)
            self.assertEqual(output.read_bytes(), original)
            damaged = bytearray(original)
            damaged[-1] ^= 1
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                media.validate(bytes(damaged), 'com')

    def test_game_executable_is_external_and_must_match(self):
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            output = temp / 'game.img'
            with self.assertRaisesRegex(ValueError, 'independently acquired'):
                media.build('game', output)
            bogus = temp / 'DIGGER.EXE'
            bogus.write_bytes(b'not the selected game')
            with self.assertRaisesRegex(ValueError, 'executable digest mismatch'):
                media.build('game', output, bogus)
            self.assertFalse(output.exists())


class GameFrameOracleTest(unittest.TestCase):
    def test_rejects_changed_frame_without_publishing_result(self):
        header = b'P6\n1280 720\n255\n'
        with tempfile.TemporaryDirectory() as temp:
            build = Path(temp)
            (build / 'result-pending.json').write_text(json.dumps({'classification': 'candidate'}))
            refs = {}
            for index, name in enumerate(simulation.GAME_FRAME_SHA256):
                data = header + bytes([index]) * (1280 * 720 * 3)
                (build / (name + '.ppm')).write_bytes(data)
                refs[name] = hashlib.sha256(data).hexdigest()
            (build / 'right-key.ppm').write_bytes(header + bytes([9]) * (1280 * 720 * 3))
            with mock.patch.object(simulation, 'GAME_FRAME_SHA256', refs):
                with self.assertRaisesRegex(ValueError, 'differs from reviewed reference'):
                    simulation.verify_game(build)
            self.assertFalse((build / 'result.json').exists())
            self.assertTrue((build / 'right-key.ppm').exists())
            self.assertTrue((build / 'result-pending.json').exists())


class SimulationProcessTest(unittest.TestCase):
    def test_dos_runs_without_the_long_scenario_disk_preflight(self):
        with mock.patch.object(simulation.shutil, 'disk_usage') as usage:
            usage.return_value = type('Disk', (), {'free': 30 * 1024**3 - 1})()
            simulation.check_space('.', 'dos')
            usage.assert_not_called()

    def test_long_application_scenarios_are_rejected_when_disk_is_low(self):
        with mock.patch.object(simulation.shutil, 'disk_usage') as usage:
            usage.return_value = type('Disk', (), {'free': 30 * 1024**3 - 1})()
            for scenario in ('com', 'game'):
                with self.subTest(scenario=scenario), self.assertRaisesRegex(ValueError, '30 GiB'):
                    simulation.check_space('.', scenario)

    def test_writable_dos_is_rejected_when_disk_is_low(self):
        with mock.patch.object(simulation.shutil, 'disk_usage') as usage:
            usage.return_value = type('Disk', (), {'free': 30 * 1024**3 - 1})()
            with self.assertRaisesRegex(ValueError, '30 GiB'):
                simulation.check_space('.', 'dos', writable=True)

    @unittest.skipUnless(hasattr(os, 'fork') and Path('/proc').exists(), 'POSIX process groups required')
    def test_sigterm_reaps_stubborn_simulator_and_fork(self):
        for sig in (signal.SIGTERM, signal.SIGHUP):
            with self.subTest(signal=sig.name):
                self._signal_reaps_stubborn_simulator_and_fork(sig)

    def _signal_reaps_stubborn_simulator_and_fork(self, sig):
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            pids = temp / 'pids'
            # Both processes ignore TERM, forcing the runner's bounded KILL
            # path. This is an owned private process group, not another job.
            worker = '''import os, signal, sys, time
signal.signal(signal.SIGTERM, signal.SIG_IGN)
child=os.fork()
if child:
    open(sys.argv[1], 'w').write(str(os.getpid())+' '+str(child))
while True:
    time.sleep(0.1)
'''
            script = '''import sys
sys.path.insert(0, sys.argv[1])
import sim_fes_pcxt_dos as sim
sim.run_simulation([sys.executable, '-c', sys.argv[2], sys.argv[3]], sys.argv[4], 'test', timeout=15, heartbeat=0.1, grace=0.2)
'''
            runner = subprocess.Popen([sys.executable, '-c', script, str(ROOT / 'scripts'), worker,
                                       str(pids), str(temp)], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            try:
                deadline = time.monotonic() + 10
                while not pids.exists() and runner.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.02)
                self.assertTrue(pids.exists(), 'worker did not start')
                pid, child = map(int, pids.read_text().split())
                runner.send_signal(sig)
                runner.communicate(timeout=5)
                self.assertNotEqual(runner.returncode, 0)
                # A reparented child may be a zombie until init reaps it; it is
                # stopped and no longer holds simulator or capture resources.
                for value in (pid, child):
                    stat = Path('/proc') / str(value) / 'stat'
                    if stat.exists():
                        self.assertEqual(stat.read_text().split(') ', 1)[1][0], 'Z')
            finally:
                if runner.poll() is None:
                    runner.terminate()
                    try:
                        runner.communicate(timeout=5)
                    except subprocess.TimeoutExpired:
                        if pids.exists():
                            os.killpg(int(pids.read_text().split()[0]), signal.SIGKILL)
                        runner.kill()
                        runner.communicate(timeout=5)

    @unittest.skipUnless(sys.platform.startswith('linux'), 'PR_SET_PDEATHSIG is Linux only')
    def test_killed_runner_takes_simulator_with_it(self):
        # The forking worker mirrors dos_tb.cpp's paired control worker, which
        # sets PR_SET_PDEATHSIG again after fork().
        for forks in (False, True):
            with self.subTest(forks=forks):
                self._killed_runner_takes_simulator_with_it(forks)

    def _killed_runner_takes_simulator_with_it(self, forks):
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            pids = temp / 'pids'
            worker = '''import ctypes, os, signal, sys, time
signal.signal(signal.SIGTERM, signal.SIG_IGN)
ids = [os.getpid()]
if sys.argv[2] == '1':
    parent = os.getpid()
    child = os.fork()
    if child == 0:
        if ctypes.CDLL(None).prctl(1, signal.SIGKILL, 0, 0, 0) or os.getppid() != parent:
            os._exit(1)
        while True:
            time.sleep(0.1)
    ids.append(child)
open(sys.argv[1], 'w').write(' '.join(map(str, ids)))
while True:
    time.sleep(0.1)
'''
            script = '''import sys
sys.path.insert(0, sys.argv[1])
import sim_fes_pcxt_dos as sim
sim.run_simulation([sys.executable, '-c', sys.argv[2], sys.argv[3], sys.argv[5]], sys.argv[4], 'test', timeout=15, heartbeat=0.1, grace=0.2)
'''
            runner = subprocess.Popen([sys.executable, '-c', script, str(ROOT / 'scripts'), worker,
                                       str(pids), str(temp), '1' if forks else '0'],
                                      stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            ids = []
            try:
                deadline = time.monotonic() + 10
                while not pids.exists() and runner.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.02)
                self.assertTrue(pids.exists(), 'worker did not start')
                time.sleep(0.05)
                ids = [int(x) for x in pids.read_text().split()]
                self.assertEqual(len(ids), 2 if forks else 1)
                runner.kill()
                # wait(), not communicate(): a surviving worker would hold the
                # inherited stderr pipe open.
                runner.wait(timeout=5)
                def state(pid):
                    try:
                        return (Path('/proc') / str(pid) / 'stat').read_text().split(') ', 1)[1][0]
                    except (FileNotFoundError, ProcessLookupError):
                        return None  # gone and reaped
                deadline = time.monotonic() + 5
                while any(state(pid) not in (None, 'Z') for pid in ids) and time.monotonic() < deadline:
                    time.sleep(0.02)
                for pid in ids:
                    self.assertIn(state(pid), (None, 'Z'))
            finally:
                if runner.poll() is None:
                    runner.kill()
                    runner.wait(timeout=5)
                for pid in ids:
                    try:
                        os.kill(pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass

    @unittest.skipUnless(sys.platform.startswith('linux') and hasattr(os, 'pidfd_open') and
                         hasattr(signal, 'pidfd_send_signal'),
                         'Linux pidfds are required for identity-bound cleanup')
    def test_runner_death_before_preexec_hook_does_not_exec_simulator(self):
        """Exercise the Popen startup gap and compare the old hook with production."""
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            worker = '''import ctypes, os, signal, sys, time
marker, descendants = sys.argv[1:]
open(marker, 'w').write(str(os.getpid()))
parent = os.getpid()
child = os.fork()
if child == 0:
    if ctypes.CDLL(None).prctl(1, signal.SIGKILL, 0, 0, 0) or os.getppid() != parent:
        os._exit(2)
    open(descendants, 'w').write(str(os.getpid()))
    while True:
        time.sleep(0.05)
while True:
    time.sleep(0.05)
'''
            # The boundary wrapper blocks in the real preexec path after fork
            # but before production die_with_parent() runs.
            runner_script = '''import os, sys, time
sys.path.insert(0, sys.argv[1])
import sim_fes_pcxt_dos as sim
worker, marker, descendants, boundary, release, mode, cwd = sys.argv[2:]
production_hook = sim.die_with_parent
def gated_hook(*args):
    open(boundary, 'w').write(str(os.getpid()))
    deadline = time.monotonic() + 10
    while not os.path.exists(release) and time.monotonic() < deadline:
        time.sleep(0.01)
    if not os.path.exists(release):
        raise RuntimeError('test boundary release timed out')
    if mode == 'before-fix':
        import ctypes, signal
        if ctypes.CDLL(None, use_errno=True).prctl(1, signal.SIGKILL, 0, 0, 0):
            raise OSError(ctypes.get_errno(), 'baseline prctl failed')
        return
    if mode == 'production-mismatch':
        production_hook(args[0] + 1)
        return
    production_hook(*args)
sim.die_with_parent = gated_hook
sim.run_simulation([sys.executable, '-c', worker, marker, descendants], cwd, 'startup-race',
                   timeout=8, heartbeat=0.1, grace=0.2)
'''

            def state(pid):
                try:
                    tail = (Path('/proc') / str(pid) / 'stat').read_text().split(') ', 1)[1]
                    return tail[0]
                except (FileNotFoundError, ProcessLookupError):
                    return None

            def wait_for(predicate, message, seconds=5):
                deadline = time.monotonic() + seconds
                while time.monotonic() < deadline:
                    if predicate():
                        return
                    time.sleep(0.01)
                self.fail(message)

            for mode in ('before-fix', 'production', 'production-mismatch'):
                with self.subTest(mode=mode):
                    marker = temp / (mode + '.exec')
                    descendants = temp / (mode + '.descendants')
                    boundary = temp / (mode + '.boundary')
                    release = temp / (mode + '.release')
                    runner = subprocess.Popen(
                        [sys.executable, '-c', runner_script, str(ROOT / 'scripts'), worker,
                         str(marker), str(descendants), str(boundary), str(release), mode, str(temp)],
                        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
                    child_pid = None
                    child_pidfd = None
                    try:
                        wait_for(lambda: boundary.exists() or runner.poll() is not None,
                                 'preexec boundary was not reached')
                        self.assertTrue(boundary.exists(), 'runner exited before startup boundary')
                        child_pid = int(boundary.read_text())
                        child_pidfd = os.pidfd_open(child_pid)
                        self.assertIsNotNone(child_pidfd, 'could not bind startup child identity')

                        if mode == 'before-fix':
                            runner.kill()
                            runner.wait(timeout=5)
                            release.touch()
                            wait_for(lambda: marker.exists() and descendants.exists(),
                                     'controlled pre-fix case did not reach exec and fork')
                            self.assertEqual(int(marker.read_text()), child_pid)
                            descendant_pid = int(descendants.read_text())
                            # The pidfd names the exact startup child even if its PID is later reused.
                            signal.pidfd_send_signal(child_pidfd, signal.SIGKILL)
                            wait_for(lambda: state(child_pid) in (None, 'Z'),
                                     'pre-fix simulator survived pidfd cleanup')
                            wait_for(lambda: state(descendant_pid) in (None, 'Z'),
                                     'pre-fix control descendant survived parent-death cleanup')
                        elif mode == 'production':
                            runner.kill()
                            runner.wait(timeout=5)
                            release.touch()
                            wait_for(lambda: state(child_pid) in (None, 'Z'),
                                     'production preexec child did not exit after parent mismatch')
                            self.assertFalse(marker.exists(),
                                             'production simulator execed after its runner died')
                            self.assertFalse(descendants.exists(),
                                             'production startup race created a control descendant')
                        else:
                            release.touch()
                            _, stderr = runner.communicate(timeout=5)
                            self.assertNotEqual(runner.returncode, 0,
                                                'preexec mismatch was reported as a successful command')
                            self.assertIn(b'Exception occurred in preexec_fn', stderr)
                            self.assertFalse(marker.exists(), 'mismatched preexec started the simulator')
                            self.assertIn(state(child_pid), (None, 'Z'))
                    finally:
                        release.touch()
                        if runner.poll() is None:
                            runner.kill()
                            runner.wait(timeout=5)
                        if mode == 'before-fix' and child_pidfd is not None:
                            try:
                                signal.pidfd_send_signal(child_pidfd, signal.SIGKILL)
                            except ProcessLookupError:
                                pass
                        if child_pidfd is not None:
                            os.close(child_pidfd)
                        if runner.stderr is not None:
                            runner.stderr.close()

if __name__ == '__main__':
    unittest.main()
