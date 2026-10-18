"""GPU lease behavior with a private directory and fake device ids."""
import json
import os
from pathlib import Path
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import textwrap
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import gpu_lease

GPU_RUN = ROOT / 'scripts' / 'gpu-run'
GPU_STATUS = ROOT / 'scripts' / 'gpu-status'
NVIDIA_LIST = textwrap.dedent("""\
    GPU 0: NVIDIA GeForce RTX 3070 (UUID: GPU-aaa)
    GPU 1: NVIDIA GeForce RTX 3090 (UUID: GPU-bbb)
    """)
ROCM_SHOW = textwrap.dedent("""\
    GPU[0]          : Device Name:          test
    GPU[0]          : Device ID:            0x73bf
    GPU[1]          : Device Name:          test
    """)
ROCM_CONCISE = textwrap.dedent("""\
    ======================= ROCm System Management Interface =======================
    GPU  Temp (DieEdge)  AvgPwr  SCLK  MCLK   Fan  Perf  PwrCap  VRAM%  GPU%
    0    40.0c           25.0W   800Mhz 1600Mhz 0% auto 220.0W 0% 0%
    1    41.0c           26.0W   800Mhz 1600Mhz 0% auto 220.0W 0% 0%
    ================================================================================
    """)


class GpuLeaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.lease = self.root / 'leases'

    def env(self, **extra):
        base = os.environ.copy()
        for key in ('GPU_LEASE_DIR', 'GPU_LEASE_IDS', 'GPU_LEASE_VENDOR',
                    'GPU_LEASE_MAX_PER_OWNER', 'HIP_VISIBLE_DEVICES',
                    'ROCR_VISIBLE_DEVICES', 'CUDA_VISIBLE_DEVICES', 'FES_GPU_IDS'):
            base.pop(key, None)
        base['GPU_LEASE_DIR'] = str(self.lease)
        base['GPU_LEASE_IDS'] = '0,1'
        base['GPU_LEASE_VENDOR'] = 'amd'
        base.update(extra)
        return base

    def run_tool(self, script, args, env=None, timeout=10):
        return subprocess.run([sys.executable, str(script), *args], env=env or self.env(),
                              capture_output=True, text=True, timeout=timeout)

    def popen(self, args, env=None):
        proc = subprocess.Popen([sys.executable, str(GPU_RUN), *args], env=env or self.env(),
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(self.stop, proc)
        return proc

    def stop(self, proc):
        if proc.poll() is None:
            try:
                proc.kill()
            except ProcessLookupError:
                pass
        try:
            proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.communicate()

    def status(self, env=None, json_output=True):
        args = ['--json'] if json_output else []
        completed = self.run_tool(GPU_STATUS, args, env=env)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        if json_output:
            return json.loads(completed.stdout)
        return completed.stdout

    def wait_for(self, predicate, timeout=5):
        deadline = time.monotonic() + timeout
        last = None
        while time.monotonic() < deadline:
            last = predicate()
            if last:
                return last
            time.sleep(0.02)
        self.fail('condition not met: ' + repr(last))

    def hold(self, *args, env=None, seconds=30):
        proc = self.popen((*args, '--', '/bin/sleep', str(seconds)), env=env)
        def held():
            if proc.poll() is not None:
                self.fail('holder exited %s: %s' % (proc.returncode, proc.stderr.read()))
            report = self.status(env)
            return any(gpu['state'] == 'held' and gpu.get('pid') == proc.pid for gpu in report['gpus'])
        self.wait_for(held)
        return proc

    def test_parsers_and_render_node_ids(self):
        self.assertEqual(gpu_lease.parse_nvidia_smi_l(NVIDIA_LIST), ['0', '1'])
        self.assertEqual(gpu_lease.parse_rocm_smi(ROCM_SHOW), ['0', '1'])
        self.assertEqual(gpu_lease.parse_rocm_smi(ROCM_CONCISE), ['0', '1'])
        self.assertEqual(gpu_lease.ids_from_render_names(
            ['renderD129', 'card0', 'renderD128', 'renderD128']), ['0', '1'])
        self.assertEqual(gpu_lease.DEFAULT_LEASE_DIR, '/var/tmp/gpu-leases')
        self.assertIsNotNone(gpu_lease.process_starttime(os.getpid()))
        self.assertIsNone(gpu_lease.process_starttime(2 ** 30))

    def test_detected_nvidia_and_amd_devices_export_vendor_variables(self):
        binary = self.root / 'bin'
        binary.mkdir()
        (binary / 'nvidia-smi').write_text('#!/bin/sh\n'
                                           'if [ "$1" = "-L" ]; then printf "%s" "$LIST"; fi\n')
        (binary / 'rocm-smi').write_text('#!/bin/sh\nprintf "%s" "$LIST"\n')
        os.chmod(binary / 'nvidia-smi', 0o755)
        os.chmod(binary / 'rocm-smi', 0o755)
        code = ('import os; print(os.environ.get("CUDA_VISIBLE_DEVICES", "unset"));'
                'print(os.environ.get("HIP_VISIBLE_DEVICES", "unset"));'
                'print(os.environ.get("ROCR_VISIBLE_DEVICES", "unset"));'
                'print(os.environ["FES_GPU_IDS"])')
        nvidia = self.env(GPU_LEASE_IDS='', GPU_LEASE_VENDOR='')
        nvidia.pop('GPU_LEASE_IDS')
        nvidia.pop('GPU_LEASE_VENDOR')
        nvidia['PATH'] = str(binary) + os.pathsep + nvidia['PATH']
        nvidia['LIST'] = NVIDIA_LIST
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--', sys.executable, '-c', code], env=nvidia)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stdout.splitlines(), ['0', 'unset', 'unset', '0'])

        (binary / 'nvidia-smi').write_text('#!/bin/sh\nexit 1\n')
        amd = dict(nvidia)
        amd['LIST'] = ROCM_SHOW
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--gpus', '2', '--', sys.executable, '-c', code], env=amd)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stdout.splitlines(), ['unset', '0,1', '0,1', '0,1'])

    def test_lease_metadata_release_modes_and_child_exit(self):
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'alice', '--label', 'route', '--gpus', '2',
                                            '--', sys.executable, '-c', 'import sys; sys.exit(42)'])
        self.assertEqual(completed.returncode, 42, completed.stderr)
        self.assertEqual(self.lease.stat().st_mode & 0o1777, 0o1777)
        self.assertTrue(self.lease.stat().st_mode & stat.S_ISVTX)
        self.assertEqual((self.lease / 'queue').stat().st_mode & 0o1777, 0o1777)
        for name in ('gpu-0.lock', 'gpu-1.lock', 'state.lock'):
            self.assertEqual((self.lease / name).stat().st_mode & 0o777, 0o666, name)
        raw = (self.lease / 'gpu-0.lock').read_text()
        self.assertIn('"owner": "alice"', raw)
        self.assertIn('"label": "route"', raw)
        report = self.status()
        self.assertEqual([gpu['state'] for gpu in report['gpus']], ['free', 'free'])
        self.assertEqual(report['queue'], [])
        text = self.status(json_output=False)
        self.assertIn('GPU 0  free', text)
        self.assertIn('queue empty', text)

    def test_holder_metadata_and_distinct_free_gpus(self):
        code = ('import os,sys,time; open(sys.argv[1],"w").write(os.environ["FES_GPU_IDS"]); time.sleep(30)')
        first = self.popen(['--no-wait', '--owner', 'alice', '--label', 'route', '--',
                            sys.executable, '-c', code, str(self.root / 'a')])
        second = self.popen(['--no-wait', '--owner', 'bob', '--label', 'other', '--',
                             sys.executable, '-c', code, str(self.root / 'b')])
        self.wait_for(lambda: (self.root / 'a').exists() and (self.root / 'b').exists())
        self.assertEqual({(self.root / 'a').read_text(), (self.root / 'b').read_text()}, {'0', '1'})
        report = self.status()
        by_owner = {gpu['owner']: gpu for gpu in report['gpus']}
        self.assertEqual(set(by_owner), {'alice', 'bob'})
        self.assertEqual(by_owner['alice']['label'], 'route')
        self.assertEqual(by_owner['alice']['pid'], first.pid)
        self.assertEqual(by_owner['alice']['host'], socket.gethostname())
        self.assertEqual(by_owner['alice']['command'], [sys.executable, '-c', code, str(self.root / 'a')])
        self.assertGreaterEqual(by_owner['alice']['age_seconds'], 0)
        self.assertRegex(by_owner['alice']['start'], r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z')
        text = self.status(json_output=False)
        self.assertIn('owner=alice', text)
        self.assertIn('label=route', text)
        self.assertRegex(text, r'age=\d+s')
        self.assertEqual(report['queue'], [])
        self.assertEqual(second.pid, by_owner['bob']['pid'])

    def test_no_wait_exits_75_when_the_requested_gpus_are_busy(self):
        self.hold('--no-wait', '--gpus', '2', '--owner', 'alice')
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'bob', '--', '/bin/sleep', '1'])
        self.assertEqual(completed.returncode, 75, completed.stderr)
        self.assertIn('unavailable', completed.stderr)

    def test_no_wait_exits_75_when_not_enough_gpus_are_free(self):
        self.hold('--no-wait', '--owner', 'alice')
        busy = self.run_tool(GPU_RUN, ['--no-wait', '--gpus', '2', '--owner', 'bob', '--', '/bin/true'])
        self.assertEqual(busy.returncode, 75, busy.stderr)

    def test_timeout_and_usage_errors(self):
        self.hold('--no-wait', '--gpus', '2', '--owner', 'alice')
        completed = self.run_tool(GPU_RUN, ['--timeout', '0.3', '--owner', 'zoe', '--', '/bin/sleep', '5'], timeout=5)
        self.assertEqual(completed.returncode, 75, completed.stderr)
        self.assertIn('timed out', completed.stderr)
        for args in (['--gpus', '0', '--', '/bin/true'],
                     ['--no-wait'],
                     ['--gpus', '3', '--no-wait', '--', '/bin/true'],
                     ['--no-wait', '--timeout', '1', '--', '/bin/true']):
            with self.subTest(args=args):
                result = self.run_tool(GPU_RUN, args)
                self.assertEqual(result.returncode, 2, result.stderr)
        help_text = self.run_tool(GPU_RUN, ['--help'])
        self.assertEqual(help_text.returncode, 0, help_text.stderr)
        self.assertIn('GPU_LEASE_MAX_PER_OWNER=2', help_text.stdout)
        self.assertIn('--owner lander', help_text.stdout)
        self.assertEqual(self.run_tool(GPU_STATUS, ['--help']).returncode, 0)

    def test_queue_is_first_come_first_served(self):
        env = self.env(GPU_LEASE_IDS='0')
        holder = self.hold('--no-wait', '--owner', 'holder', env=env)
        log = self.root / 'order'
        code = ('import os,sys,time; open(sys.argv[1],"a").write(sys.argv[2]+" "+os.environ["FES_GPU_IDS"]+"\\n");'
                'time.sleep(0.2)')
        first = self.popen(['--wait', '--timeout', '10', '--owner', 'a', '--label', 'first', '--',
                            sys.executable, '-c', code, str(log), 'A'], env=env)
        self.wait_for(lambda: any(item['pid'] == first.pid for item in self.status(env)['queue']))
        second = self.popen(['--wait', '--timeout', '10', '--owner', 'b', '--label', 'second', '--',
                             sys.executable, '-c', code, str(log), 'B'], env=env)
        def both_queued():
            queue = self.status(env)['queue']
            return [item['pid'] for item in queue] == [first.pid, second.pid]
        self.wait_for(both_queued)
        queued = self.status(env)
        self.assertEqual([item['owner'] for item in queued['queue']], ['a', 'b'])
        self.assertEqual(queued['queue'][0]['label'], 'first')
        self.assertEqual(queued['queue'][0]['gpus'], 1)
        self.assertGreaterEqual(queued['queue'][0]['age_seconds'], 0)
        text = self.status(env, json_output=False)
        self.assertIn('waiting 1  owner=a', text)
        self.assertIn('waiting 2  owner=b', text)
        os.kill(holder.pid, signal.SIGKILL)
        holder.wait(timeout=5)
        first_out, first_err = first.communicate(timeout=10)
        second_out, second_err = second.communicate(timeout=10)
        self.assertEqual(first.returncode, 0, first_err)
        self.assertEqual(second.returncode, 0, second_err)
        self.assertEqual(log.read_text().splitlines(), ['A 0', 'B 0'])
        self.assertEqual(first_out, '')
        self.assertEqual(second_out, '')

    def test_no_wait_does_not_cut_ahead_of_a_waiter(self):
        self.hold('--no-wait', '--owner', 'holder')
        waiter = self.popen(['--wait', '--timeout', '8', '--gpus', '2', '--owner', 'a', '--',
                             sys.executable, '-c', 'import os; print(os.environ["FES_GPU_IDS"])'])
        self.wait_for(lambda: any(item['gpus'] == 2 and item['pid'] == waiter.pid
                                  for item in self.status()['queue']))
        cut = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'b', '--', '/bin/sleep', '1'])
        self.assertEqual(cut.returncode, 75, cut.stderr)
        report = self.status()
        states = {gpu['id']: gpu['state'] for gpu in report['gpus']}
        self.assertEqual(states['1'], 'free')
        self.assertEqual(report['queue'][0]['pid'], waiter.pid)

    def test_owner_cap_and_a_blocked_owner_does_not_stall_the_queue(self):
        env = self.env(GPU_LEASE_MAX_PER_OWNER='1')
        holder = self.hold('--no-wait', '--owner', 'alice', env=env)
        denied = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'alice', '--', '/bin/sleep', '1'], env=env)
        self.assertEqual(denied.returncode, 75, denied.stderr)
        too_many = self.run_tool(GPU_RUN, ['--no-wait', '--gpus', '2', '--owner', 'carol', '--', '/bin/true'], env=env)
        self.assertEqual(too_many.returncode, 2, too_many.stderr)
        marker = self.root / 'alice2'
        alice = self.popen(['--wait', '--timeout', '8', '--owner', 'alice', '--',
                            sys.executable, '-c',
                            'import os,sys; open(sys.argv[1],"w").write(os.environ["FES_GPU_IDS"])',
                            str(marker)], env=env)
        self.wait_for(lambda: any(item['pid'] == alice.pid for item in self.status(env)['queue']))
        bob = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'bob', '--', sys.executable, '-c',
                                      'import os; print(os.environ["FES_GPU_IDS"])'], env=env)
        self.assertEqual(bob.returncode, 0, bob.stderr)
        self.assertEqual(bob.stdout.strip(), '1')
        self.assertTrue(any(item['pid'] == alice.pid for item in self.status(env)['queue']))
        self.assertFalse(marker.exists())
        os.kill(holder.pid, signal.SIGKILL)
        holder.wait(timeout=5)
        out, err = alice.communicate(timeout=8)
        self.assertEqual(alice.returncode, 0, err)
        self.assertEqual(marker.read_text(), '0')
        self.assertEqual(out, '')

    def test_sigkill_of_the_holder_releases_the_flock(self):
        proc = self.hold('--no-wait', '--owner', 'alice', '--label', 'held')
        lease = self.lease / 'gpu-0.lock'
        inode = lease.stat().st_ino
        inherited = []
        for entry in Path('/proc/%d/fd' % proc.pid).iterdir():
            try:
                if os.stat(entry).st_ino == inode:
                    inherited.append(entry)
            except OSError:
                pass
        self.assertTrue(inherited)
        self.assertEqual(Path('/proc/%d/comm' % proc.pid).read_text().strip(), 'sleep')
        os.kill(proc.pid, signal.SIGKILL)
        proc.wait(timeout=5)
        self.wait_for(lambda: all(gpu['state'] == 'free' for gpu in self.status()['gpus']))
        self.assertIn('alice', lease.read_text())
        again = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'bob', '--', sys.executable, '-c',
                                        'import os; print(os.environ["FES_GPU_IDS"])'])
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual(again.stdout.strip(), '0')

    def test_status_does_not_create_the_lease_directory(self):
        report = self.status()
        self.assertEqual(report['vendor'], 'amd')
        self.assertEqual(report['gpus'], [{'id': '0', 'state': 'free'}, {'id': '1', 'state': 'free'}])
        self.assertEqual(report['queue'], [])
        self.assertFalse(self.lease.exists())

    def test_invalid_override_and_the_tool_never_terminates_other_processes(self):
        env = self.env(GPU_LEASE_IDS='0,0')
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--', '/bin/true'], env=env)
        self.assertEqual(completed.returncode, 2, completed.stderr)
        env = self.env(GPU_LEASE_VENDOR='cuda')
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--', '/bin/true'], env=env)
        self.assertEqual(completed.returncode, 2, completed.stderr)
        source = (ROOT / 'scripts' / 'gpu_lease.py').read_text()
        self.assertNotIn('os.kill', source)
        self.assertNotIn('shell=True', source)
        self.assertNotIn('Popen', source)
        self.assertNotRegex(source, r'\bsignal\.')


if __name__ == '__main__':
    unittest.main()
