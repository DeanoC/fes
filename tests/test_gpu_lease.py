"""GPU lease behavior with a private directory and fake device ids."""
import json
import io
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
from contextlib import redirect_stderr
from unittest.mock import patch

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
                    'ROCR_VISIBLE_DEVICES', 'CUDA_VISIBLE_DEVICES', 'FES_GPU_IDS',
                    'GPU_RUN_DEVICE', 'GPU_RUN_EXPECTED_DEVICE', 'NEXTPNR_REQUIRE_GPU'):
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
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                start_new_session=True)
        self.addCleanup(self.stop, proc)
        return proc

    def stop(self, proc):
        # The wrapper is the session leader. Killing the session also stops the
        # command it started; killing only the wrapper leaves that command
        # holding the captured pipes.
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        try:
            proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            proc.communicate(timeout=5)

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
        # The only GPU node is HIP 0 even when its DRM render minor is 129.
        self.assertEqual(gpu_lease.ids_from_kfd_nodes([
            (0, 'simd_count 0\ndrm_render_minor 128\n'),
            (1, 'simd_count 120\ndrm_render_minor 129\n'),
        ]), ['0'])
        self.assertEqual(gpu_lease.render_nodes_from_kfd_nodes([
            (0, 'simd_count 0\ndrm_render_minor 128\n'),
            (1, 'simd_count 120\ndrm_render_minor 129\n'),
            (2, 'simd_count 80\ndrm_render_minor 130\n'),
        ]), {'0': '129', '1': '130'})
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
                'print(os.environ["FES_GPU_IDS"]);'
                'print(os.environ["GPU_RUN_DEVICE"]);'
                'print(os.environ["GPU_RUN_EXPECTED_DEVICE"]);'
                'print(os.environ["NEXTPNR_REQUIRE_GPU"])')
        nvidia = self.env(GPU_LEASE_IDS='', GPU_LEASE_VENDOR='')
        nvidia.pop('GPU_LEASE_IDS')
        nvidia.pop('GPU_LEASE_VENDOR')
        nvidia['PATH'] = str(binary) + os.pathsep + nvidia['PATH']
        nvidia['LIST'] = NVIDIA_LIST
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--', sys.executable, '-c', code], env=nvidia)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(completed.stdout.splitlines(), [
            '0', 'unset', 'unset', '0', '0', '/dev/nvidia0', '1',
        ])

        (binary / 'nvidia-smi').write_text('#!/bin/sh\nexit 1\n')
        amd = dict(nvidia)
        amd['LIST'] = ROCM_SHOW
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--gpus', '2', '--', sys.executable, '-c', code], env=amd)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        lines = completed.stdout.splitlines()
        self.assertEqual(lines[:5], ['unset', 'unset', '0,1', '0,1', '0'])
        self.assertTrue(lines[5].startswith('/dev/dri/renderD') or lines[5] == 'amd:0')
        self.assertEqual(lines[6], '1')

    def test_child_env_uses_one_mask_and_logical_zero_for_each_backend(self):
        with patch.dict(os.environ, {
                'HIP_VISIBLE_DEVICES': '77',
                'ROCR_VISIBLE_DEVICES': '88',
                'CUDA_VISIBLE_DEVICES': '99',
        }, clear=False):
            hip = gpu_lease.child_env('amd', ['1'], '/dev/dri/renderD129')
            self.assertNotIn('HIP_VISIBLE_DEVICES', hip)
            self.assertNotIn('CUDA_VISIBLE_DEVICES', hip)
            self.assertEqual(hip['ROCR_VISIBLE_DEVICES'], '1')
            self.assertEqual(hip['FES_GPU_IDS'], '1')
            self.assertEqual(hip['GPU_RUN_DEVICE'], '0')
            self.assertEqual(hip['GPU_RUN_EXPECTED_DEVICE'], '/dev/dri/renderD129')
            self.assertEqual(hip['NEXTPNR_REQUIRE_GPU'], '1')

            cuda = gpu_lease.child_env('nvidia', ['1'], '/dev/nvidia1')
            self.assertNotIn('HIP_VISIBLE_DEVICES', cuda)
            self.assertNotIn('ROCR_VISIBLE_DEVICES', cuda)
            self.assertEqual(cuda['CUDA_VISIBLE_DEVICES'], '1')
            self.assertEqual(cuda['FES_GPU_IDS'], '1')
            self.assertEqual(cuda['GPU_RUN_DEVICE'], '0')
            self.assertEqual(cuda['GPU_RUN_EXPECTED_DEVICE'], '/dev/nvidia1')
            self.assertEqual(cuda['NEXTPNR_REQUIRE_GPU'], '1')

    def _fake_process(self, proc_root, pid, command, children='', fds=()):
        task = proc_root / str(pid) / 'task' / str(pid)
        task.mkdir(parents=True)
        (task / 'children').write_text(children)
        (proc_root / str(pid) / 'cmdline').write_bytes(command.replace(' ', '\0').encode())
        fd_dir = proc_root / str(pid) / 'fd'
        fd_dir.mkdir()
        for number, target in enumerate(fds, 3):
            os.symlink(target, fd_dir / str(number))

    def test_fallback_detection_checks_nextpnr_tree_and_leased_device_nodes(self):
        proc_root = self.root / 'proc'
        self._fake_process(proc_root, 100, '/bin/bash replay.sh', children='101')
        self._fake_process(proc_root, 101, '/tools/nextpnr-mistral --router gpu',
                           fds=('/dev/kfd', '/dev/dri/renderD129'))
        self.assertTrue(gpu_lease.process_tree_has_nextpnr(100, proc_root))
        self.assertTrue(gpu_lease.process_tree_requires_gpu(100, proc_root))
        self.assertTrue(gpu_lease.process_tree_uses_gpu(
            100, 'amd', '/dev/dri/renderD129', proc_root))
        self.assertFalse(gpu_lease.process_tree_uses_gpu(
            100, 'amd', '/dev/dri/renderD128', proc_root))
        self.assertTrue(gpu_lease.process_tree_uses_gpu(100, 'amd', None, proc_root))

        proc_root_cuda = self.root / 'proc-cuda'
        self._fake_process(proc_root_cuda, 200, '/tools/nextpnr-mistral',
                           fds=('/dev/nvidiactl', '/dev/nvidia1'))
        self.assertTrue(gpu_lease.process_tree_uses_gpu(
            200, 'nvidia', '/dev/nvidia1', proc_root_cuda))
        self.assertFalse(gpu_lease.process_tree_uses_gpu(
            200, 'nvidia', '/dev/nvidia0', proc_root_cuda))

        proc_root_cpu = self.root / 'proc-cpu'
        self._fake_process(proc_root_cpu, 300, '/tools/nextpnr-mistral --router heap')
        self.assertFalse(gpu_lease.process_tree_requires_gpu(300, proc_root_cpu))
        proc_root_help = self.root / 'proc-help'
        self._fake_process(proc_root_help, 400, '/tools/nextpnr-mistral --help')
        self.assertFalse(gpu_lease.process_tree_requires_gpu(400, proc_root_help))

    def test_nextpnr_without_gpu_access_fails_loudly(self):
        fd = os.open(self.root / 'lease-fd', os.O_CREAT | os.O_RDWR, 0o600)
        output = io.StringIO()
        with patch.object(gpu_lease, 'gpu_device_path', return_value='/dev/dri/renderD129'), \
             patch.object(gpu_lease, 'process_tree_requires_gpu', return_value=True), \
             patch.object(gpu_lease, 'process_tree_uses_gpu', return_value=False), \
             redirect_stderr(output):
            status = gpu_lease.execute('amd', [('1', fd)], ['/bin/true'])
        self.assertEqual(status, 70)
        self.assertIn('nextpnr never opened leased GPU device', output.getvalue())
        self.assertIn('/dev/kfd and /dev/dri/renderD129', output.getvalue())

    def test_successful_nextpnr_help_does_not_require_gpu_access(self):
        command = self.root / 'nextpnr-mistral'
        command.write_text('#!/bin/sh\nexit 0\n')
        command.chmod(0o755)
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--', str(command), '--help'])
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertNotIn('nextpnr never opened leased GPU device', completed.stderr)

    def test_lease_metadata_release_modes_and_child_exit(self):
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'alice', '--label', 'route', '--gpus', '2',
                                            '--', sys.executable, '-c', 'import sys; sys.exit(42)'])
        self.assertEqual(completed.returncode, 42, completed.stderr)
        self.assertEqual(stat.S_IMODE(self.lease.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE((self.lease / 'queue').stat().st_mode), 0o700)
        for name in ('gpu-0.lock', 'gpu-1.lock', 'state.lock'):
            self.assertEqual(stat.S_IMODE((self.lease / name).stat().st_mode), 0o600, name)
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

    def test_owner_cap_counts_holds_across_narrowed_gpu_inventories(self):
        gpu0 = self.env(GPU_LEASE_IDS='0', GPU_LEASE_MAX_PER_OWNER='1')
        holder = self.hold('--no-wait', '--owner', 'alice', env=gpu0)
        gpu1 = dict(gpu0, GPU_LEASE_IDS='1')

        denied = self.run_tool(
            GPU_RUN,
            ['--no-wait', '--owner', 'alice', '--', '/bin/true'],
            env=gpu1,
        )
        self.assertEqual(denied.returncode, 75, denied.stderr)
        self.assertIsNone(holder.poll())

    def child_pids(self, pid):
        found = []
        for entry in Path('/proc').iterdir():
            if not entry.name.isdigit():
                continue
            try:
                raw = (entry / 'stat').read_text()
            except OSError:
                continue
            right = raw.rfind(')')
            if right < 0:
                continue
            fields = raw[right + 2:].split()
            if len(fields) > 2 and fields[1] == str(pid) and fields[0] != 'Z':
                found.append(int(entry.name))
        return found

    def lease_fds(self, pid, inode):
        root = Path('/proc/%d/fd' % pid)
        if not root.is_dir():
            return []
        found = []
        for entry in root.iterdir():
            try:
                if os.stat(entry).st_ino == inode:
                    found.append(entry)
            except OSError:
                pass
        return found

    def sleeping_child_without_lease(self, proc, inode):
        if proc.poll() is not None:
            self.fail('supervisor exited %s' % proc.returncode)
        kids = self.child_pids(proc.pid)
        if len(kids) != 1:
            return None
        kid = kids[0]
        try:
            comm = Path('/proc/%d/comm' % kid).read_text().strip()
        except OSError:
            return None
        if comm != 'sleep' or self.lease_fds(kid, inode) or not self.lease_fds(proc.pid, inode):
            return None
        return kid

    def test_command_does_not_inherit_the_lease_fd(self):
        proc = self.hold('--no-wait', '--owner', 'alice', '--label', 'held')
        inode = (self.lease / 'gpu-0.lock').stat().st_ino
        kid = self.wait_for(lambda: self.sleeping_child_without_lease(proc, inode))
        os.kill(kid, signal.SIGTERM)
        _out, err = proc.communicate(timeout=5)
        self.assertEqual(proc.returncode, 128 + signal.SIGTERM, err)
        self.wait_for(lambda: all(gpu['state'] == 'free' for gpu in self.status()['gpus']))

    def test_sigkill_of_the_supervisor_releases_the_flock(self):
        proc = self.hold('--no-wait', '--owner', 'alice', '--label', 'held')
        lease = self.lease / 'gpu-0.lock'
        inode = lease.stat().st_ino
        kid = self.wait_for(lambda: self.sleeping_child_without_lease(proc, inode))

        def stop_child():
            try:
                os.kill(kid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        self.addCleanup(stop_child)
        os.kill(proc.pid, signal.SIGKILL)
        proc.wait(timeout=5)
        self.assertTrue(Path('/proc/%d' % kid).exists())
        self.wait_for(lambda: all(gpu['state'] == 'free' for gpu in self.status()['gpus']))
        self.assertIn('alice', lease.read_text())
        again = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'bob', '--', sys.executable, '-c',
                                        'import os; print(os.environ["FES_GPU_IDS"])'])
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual(again.stdout.strip(), '0')

    def test_background_children_do_not_keep_a_finished_lease(self):
        completed = self.run_tool(
            GPU_RUN,
            ['--no-wait', '--owner', 'alice', '--', '/bin/sh', '-c',
             'sleep 30 >/dev/null 2>&1 & echo $!; exit 0'],
            timeout=10)
        self.assertEqual(completed.returncode, 0, completed.stderr)
        pid = int(completed.stdout.strip())

        def stop_sleep():
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        self.addCleanup(stop_sleep)
        self.assertTrue(Path('/proc/%d' % pid).exists())
        self.assertEqual([gpu['state'] for gpu in self.status()['gpus']], ['free', 'free'])
        again = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'bob', '--', '/bin/true'])
        self.assertEqual(again.returncode, 0, again.stderr)

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
        self.assertNotIn('SIGKILL', source)
        self.assertNotRegex(source, r'signal\.kill\b')

    def test_uncapped_job_does_not_wait_behind_a_capped_waiter(self):
        alice_env = self.env(GPU_LEASE_MAX_PER_OWNER='1')
        holder = self.hold('--no-wait', '--owner', 'alice', env=alice_env)
        alice = self.popen(['--wait', '--timeout', '8', '--owner', 'alice', '--',
                            sys.executable, '-c', 'import time; time.sleep(30)'], env=alice_env)
        self.wait_for(lambda: any(item['pid'] == alice.pid for item in self.status()['queue']))
        ticket = next((self.lease / 'queue').glob('*.json'))
        self.assertEqual(json.loads(ticket.read_text())['cap'], 1)
        self.assertEqual(stat.S_IMODE(ticket.stat().st_mode), 0o600)
        bob_env = self.env()
        self.assertNotIn('GPU_LEASE_MAX_PER_OWNER', bob_env)
        bob = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'bob', '--', sys.executable, '-c',
                                      'import os; print(os.environ["FES_GPU_IDS"])'], env=bob_env)
        self.assertEqual(bob.returncode, 0, bob.stderr)
        self.assertEqual(bob.stdout.strip(), '1')
        self.assertTrue(any(item['pid'] == alice.pid for item in self.status()['queue']))
        marker = self.root / 'bob-wait'
        waiting = self.popen(['--wait', '--timeout', '8', '--owner', 'bob', '--', sys.executable, '-c',
                              'import os,sys; open(sys.argv[1],"w").write(os.environ["FES_GPU_IDS"])',
                              str(marker)], env=bob_env)
        self.wait_for(lambda: marker.exists())
        self.assertEqual(marker.read_text(), '1')
        _out, err = waiting.communicate(timeout=8)
        self.assertEqual(waiting.returncode, 0, err)
        self.assertTrue(any(item['pid'] == alice.pid for item in self.status()['queue']))
        self.assertIsNone(holder.poll())

    def test_eligible_capped_waiter_still_blocks_an_uncapped_job(self):
        self.hold('--no-wait', '--owner', 'holder')
        alice_env = self.env(GPU_LEASE_MAX_PER_OWNER='2')
        alice = self.popen(['--wait', '--timeout', '8', '--gpus', '2', '--owner', 'alice', '--',
                            '/bin/sleep', '1'], env=alice_env)
        self.wait_for(lambda: any(item['pid'] == alice.pid and item['gpus'] == 2
                                  for item in self.status()['queue']))
        bob = self.run_tool(GPU_RUN, ['--no-wait', '--owner', 'bob', '--', '/bin/true'], env=self.env())
        self.assertEqual(bob.returncode, 75, bob.stderr)
        states = {gpu['id']: gpu['state'] for gpu in self.status()['gpus']}
        self.assertEqual(states['1'], 'free')
        self.assertEqual(self.status()['queue'][0]['pid'], alice.pid)

    def test_kfd_topology_maps_hip_order_and_drops_incomplete_nodes(self):
        cpu = 'simd_count 0\ndrm_render_minor 128\n'
        later = 'simd_count 64\ndrm_render_minor 129\n'
        earlier = 'simd_count 32\ndrm_render_minor 130\n'
        self.assertEqual(gpu_lease.ids_from_kfd_nodes([(3, later), (1, cpu), (2, earlier)]), ['0', '1'])
        self.assertEqual(gpu_lease.ids_from_kfd_nodes([(1, 'drm_render_minor 129\n')]), [])
        self.assertEqual(gpu_lease.ids_from_kfd_nodes([]), [])
        root = self.root / 'kfd'
        for name, text in (('0', cpu), ('1', later)):
            node = root / name
            node.mkdir(parents=True)
            (node / 'properties').write_text(text)
        self.assertEqual(gpu_lease.ids_from_kfd_sysfs(root), ['0'])
        (root / '2').mkdir()
        self.assertEqual(gpu_lease.ids_from_kfd_sysfs(root), [])
        self.assertEqual(gpu_lease.ids_from_kfd_sysfs(self.root / 'missing'), [])

    def test_redact_command_drops_secrets_and_bounds_length(self):
        secret = 's3cret-value'
        redacted = gpu_lease.redact_command([
            'tool', '--password', secret, '--token=' + secret,
            'https://alice:' + secret + '@host/x', 'ok', 'x' * 300,
        ])
        self.assertNotIn(secret, '\n'.join(redacted))
        self.assertEqual(redacted[1], '--password')
        self.assertEqual(redacted[2], '[redacted]')
        self.assertEqual(redacted[3], '--token=[redacted]')
        self.assertEqual(redacted[4], 'https://[redacted]@host/x')
        self.assertEqual(redacted[5], 'ok')
        self.assertTrue(redacted[6].endswith('...'))
        self.assertLessEqual(len(redacted[6]), gpu_lease.COMMAND_ARGUMENT_LIMIT + 3)
        many = gpu_lease.redact_command(['tool'] + ['a'] * 40)
        self.assertEqual(many[-1], '...')
        self.assertLessEqual(len(many), gpu_lease.COMMAND_ARGUMENT_COUNT + 1)

    def test_lease_metadata_omits_secret_arguments(self):
        secret = 'hunter2-secret-value'
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--', sys.executable, '-c', 'import sys; sys.exit(0)',
                                            '--password', secret, '--token=' + secret, 'plain'])
        self.assertEqual(completed.returncode, 0, completed.stderr)
        raw = (self.lease / 'gpu-0.lock').read_text()
        self.assertNotIn(secret, raw)
        self.assertIn('plain', raw)
        self.assertIn('[redacted]', raw)
        self.assertEqual([gpu['state'] for gpu in self.status()['gpus']], ['free', 'free'])

    def test_existing_world_writable_lease_dir_is_tightened(self):
        self.lease.mkdir()
        os.chmod(self.lease, 0o1777)
        queue = self.lease / 'queue'
        queue.mkdir()
        os.chmod(queue, 0o1777)
        old = self.lease / 'gpu-0.lock'
        old.write_text('stale')
        os.chmod(old, 0o666)
        completed = self.run_tool(GPU_RUN, ['--no-wait', '--', '/bin/true'])
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(stat.S_IMODE(self.lease.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(queue.stat().st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(old.stat().st_mode), 0o600)

    def test_remote_lease_directory_is_rejected(self):
        mounts = '\n'.join([
            'host:/export / nfs4 rw 0 0',
            '/dev/vdc /var/tmp ext4 rw 0 0',
            'host:/other /mnt/my\\040disk nfs rw 0 0',
            'tmpfs /run tmpfs rw 0 0',
        ])
        self.assertEqual(gpu_lease.mount_fstype('/var/tmp/gpu-leases', mounts), 'ext4')
        self.assertEqual(gpu_lease.mount_fstype('/run/leases', mounts), 'tmpfs')
        self.assertEqual(gpu_lease.mount_fstype('/mnt/my disk/leases', mounts), 'nfs')
        self.assertEqual(gpu_lease.mount_fstype('/mnt', mounts), 'nfs4')
        self.assertIsNone(gpu_lease.mount_fstype('/nowhere', 'not a mount table\n'))
        gpu_lease.assert_local_filesystem('/var/tmp/gpu-leases', mounts)
        remote = self.root / 'remote'
        remote.mkdir()
        remote_mounts = 'host:/export %s nfs4 rw 0 0\n/dev/vdc / ext4 rw 0 0\n' % os.path.realpath(remote)
        with self.assertRaises(gpu_lease.LeaseError) as raised:
            gpu_lease.assert_local_filesystem(remote / 'leases', remote_mounts)
        self.assertIn('nfs4', str(raised.exception))
        with patch.object(gpu_lease, 'read_mounts', return_value='host:/export / nfs4 rw 0 0\n'):
            with self.assertRaises(gpu_lease.LeaseError):
                gpu_lease.ensure_lease_dir(self.lease)
        self.assertFalse(self.lease.exists())
        with patch.object(gpu_lease, 'read_mounts', return_value=None):
            created = gpu_lease.ensure_lease_dir(self.lease)
        self.assertTrue(created.is_dir())
        self.assertEqual(stat.S_IMODE(created.stat().st_mode), 0o700)


if __name__ == '__main__':
    unittest.main()
