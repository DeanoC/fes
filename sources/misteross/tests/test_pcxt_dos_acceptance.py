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
    def test_low_disk_rejected(self):
        with mock.patch.object(simulation.shutil, 'disk_usage') as usage:
            usage.return_value = type('Disk', (), {'free': 30 * 1024**3 - 1})()
            with self.assertRaisesRegex(ValueError, '30 GiB'):
                simulation.check_space('.')

    @unittest.skipUnless(hasattr(os, 'fork') and Path('/proc').exists(), 'POSIX process groups required')
    def test_sigterm_reaps_stubborn_simulator_and_fork(self):
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
                runner.send_signal(signal.SIGTERM)
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


if __name__ == '__main__':
    unittest.main()
