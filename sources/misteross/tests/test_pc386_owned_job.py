"""Bounded pc386 jobs clean their own process groups and report outcomes."""
import contextlib
import io
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'cores/fes-pc386/tools'))
import pc386_owned_job

ROOT = Path(__file__).resolve().parents[1]


def alive(pid):
    try:
        state = Path(f'/proc/{pid}/stat').read_text().split(') ', 1)[1].split()[0]
        return state != 'Z'
    except FileNotFoundError:
        return False


def wait_dead(pid):
    deadline = time.monotonic() + 2
    while alive(pid) and time.monotonic() < deadline:
        time.sleep(.01)
    return not alive(pid)


class OwnedJobTests(unittest.TestCase):
    def test_path_inventory_catches_any_selectable_executable_change(self):
        import pc386_execution
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            command = directory / 'unusual-build-helper'
            command.write_text('#!/bin/sh\necho first\n')
            command.chmod(0o755)
            first = pc386_execution.path_inventory(str(directory))
            command.write_text('#!/bin/sh\necho changed\n')
            second = pc386_execution.path_inventory(str(directory))
            self.assertNotEqual(first, second)
            with self.assertRaisesRegex(Exception, 'absolute and nonempty'):
                pc386_execution.path_inventory('.:/bin')

    def test_timeout_reports_heartbeat_and_stops_owned_tree(self):
        with tempfile.TemporaryDirectory() as temp:
            marker = Path(temp) / 'pid'
            child = ('import subprocess,sys,time; '
                     'p=subprocess.Popen([sys.executable,"-c","import time; time.sleep(60)"]); '
                     'open(sys.argv[1],"w").write(str(p.pid)); time.sleep(60)')
            output = io.StringIO()
            with contextlib.redirect_stdout(output), self.assertRaises(TimeoutError):
                pc386_owned_job.run([sys.executable, '-c', child, str(marker)], cwd=ROOT,
                                   timeout=.6, heartbeat=.1, label='test job', grace=.2)
            self.assertIn('heartbeat', output.getvalue())
            self.assertIn('failed: time cap', output.getvalue())
            self.assertTrue(marker.is_file())
            self.assertTrue(wait_dead(int(marker.read_text())))

    def test_disk_floor_refuses_start_and_stops_a_running_job(self):
        low = mock.Mock(free=29 * 1024**3)
        high = mock.Mock(free=31 * 1024**3)
        with mock.patch.object(pc386_owned_job.shutil, 'disk_usage', return_value=low), \
                mock.patch.object(pc386_owned_job.subprocess, 'Popen') as spawn:
            with self.assertRaisesRegex(OSError, 'before launch'):
                pc386_owned_job.run([sys.executable, '-c', 'pass'], cwd=ROOT, timeout=5, label='disk test')
            spawn.assert_not_called()
        real_popen = subprocess.Popen
        spawned = []
        def start(*args, **kwargs):
            child = real_popen(*args, **kwargs)
            spawned.append(child)
            return child
        with mock.patch.object(pc386_owned_job.shutil, 'disk_usage', side_effect=[high, low]), \
                mock.patch.object(pc386_owned_job.subprocess, 'Popen', side_effect=start):
            with self.assertRaisesRegex(OSError, 'during job'):
                pc386_owned_job.run([sys.executable, '-c', 'import time; time.sleep(60)'],
                                   cwd=ROOT, timeout=5, label='disk test', grace=.2)
        self.assertIsNotNone(spawned[0].poll())

    def test_term_ignoring_grandchild_dies_after_its_leader_exits(self):
        with tempfile.TemporaryDirectory() as temp:
            marker = Path(temp) / 'pid'
            grandchild = ('import os,signal,sys,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); '
                          'open(sys.argv[1],"w").write(str(os.getpid())); time.sleep(60)')
            leader = ('import subprocess,sys,time,os; '
                      'subprocess.Popen([sys.executable,"-c",sys.argv[1],sys.argv[2]]); '
                      '[(time.sleep(.01)) for _ in range(100) if not os.path.exists(sys.argv[2])]; '
                      'time.sleep(60)')
            with self.assertRaises(TimeoutError):
                pc386_owned_job.run([sys.executable, '-c', leader, grandchild, str(marker)], cwd=ROOT,
                                   timeout=.8, heartbeat=.2, grace=.2, label='stubborn test')
            self.assertTrue(marker.exists())
            self.assertTrue(wait_dead(int(marker.read_text())))

    def test_ignored_sigchld_is_temporarily_restored_for_owned_cleanup(self):
        previous = signal.signal(signal.SIGCHLD, signal.SIG_IGN)
        try:
            status = pc386_owned_job.run([sys.executable, '-c', 'pass'], cwd=ROOT,
                                          timeout=5, grace=.2, label='sigchld test')
            self.assertEqual(status, 0)
            self.assertEqual(signal.getsignal(signal.SIGCHLD), signal.SIG_IGN)
        finally:
            signal.signal(signal.SIGCHLD, previous)

    def test_successful_leader_cannot_leave_an_orphaned_group(self):
        with tempfile.TemporaryDirectory() as temp:
            marker = Path(temp) / 'pid'
            grandchild = ('import os,signal,sys,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); '
                          'open(sys.argv[1],"w").write(str(os.getpid())); time.sleep(60)')
            leader = ('import subprocess,sys,time,os; '
                      'subprocess.Popen([sys.executable,"-c",sys.argv[1],sys.argv[2]]); '
                      '[(time.sleep(.01)) for _ in range(100) if not os.path.exists(sys.argv[2])]')
            self.assertEqual(pc386_owned_job.run([sys.executable, '-c', leader, grandchild, str(marker)],
                                                cwd=ROOT, timeout=5, grace=.2, label='orphan test'), 0)
            self.assertTrue(marker.exists())
            self.assertTrue(wait_dead(int(marker.read_text())))

    def test_normal_and_failed_exit_are_reported(self):
        for status in (0, 3):
            with self.subTest(status=status):
                output = io.StringIO()
                with contextlib.redirect_stdout(output):
                    result = pc386_owned_job.run([sys.executable, '-c', f'raise SystemExit({status})'],
                                                  cwd=ROOT, timeout=5, label='test job')
                self.assertEqual(result, status)
                self.assertIn('finished' if status == 0 else 'failed', output.getvalue())

    def test_nested_supervisor_termination_cleans_inner_group(self):
        with tempfile.TemporaryDirectory() as temp:
            marker = Path(temp) / 'pid'
            inner = ('import os,sys,time; open(sys.argv[1],"w").write(str(os.getpid())); time.sleep(60)')
            supervisor = ('import sys; sys.path.insert(0, "cores/fes-pc386/tools"); from pc386_owned_job import run; '
                          'run([sys.executable,"-c",sys.argv[1],sys.argv[2]], '
                          'cwd=".",timeout=60,label="nested",heartbeat=.1)')
            parent = subprocess.Popen([sys.executable, '-c', supervisor, inner, str(marker)],
                                      cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                      text=True, start_new_session=True)
            try:
                deadline = time.monotonic() + 5
                while not marker.exists() and time.monotonic() < deadline:
                    time.sleep(.02)
                self.assertTrue(marker.exists())
                pid = int(marker.read_text())
                os.kill(parent.pid, signal.SIGTERM)
                parent.communicate(timeout=8)
                self.assertEqual(parent.returncode, 128 + signal.SIGTERM)
                self.assertFalse(alive(pid))
            finally:
                if parent.poll() is None:
                    os.killpg(parent.pid, signal.SIGKILL)
                    parent.wait()


if __name__ == '__main__':
    unittest.main()
