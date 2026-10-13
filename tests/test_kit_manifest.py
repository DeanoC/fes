"""Host-side tests for the kit runtime manifest."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
FES = ROOT / 'scripts/fes'
REMOTE = ROOT / 'scripts/kit_remote.sh'
BOOT = '11111111-1111-4111-8111-111111111111'
GOOD = 'ab' * 32
FILES = {
    'mister-runtime': b'runtime-stock',
    'mister-agent': b'agent-stock',
    'fogcast-kit': b'kit-stock',
    'fogcast-tenfoot': b'tenfoot-stock',
    'launcher.json': b'{"launcher":true}\n',
    'agent.toml': b'token = "secret"\n',
}
PATHS = {
    'mister-runtime': 'usr/sbin/mister-runtime',
    'mister-agent': 'usr/sbin/mister-agent',
    'fogcast-kit': 'usr/sbin/fogcast-kit',
    'fogcast-tenfoot': 'usr/sbin/fogcast-tenfoot',
    'launcher.json': 'media/fat/fogcast/launcher.json',
    'agent.toml': 'media/fat/fogcast/agent.toml',
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


class KitManifestTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        self.root = self.work / 'kit'
        self.holds = self.work / 'holds'
        self.drift = self.work / 'drift'
        self.env = os.environ.copy()
        self.env.update({
            'FES_KIT_TRANSPORT': 'local',
            'FES_KIT_HOLD_DIR': str(self.holds),
            'FES_KIT_DRIFT_DIR': str(self.drift),
            'FES_KIT_A_ROOT': str(self.root),
        })
        self.env.pop('FES_KIT_FAIL_START', None)
        self.seed()

    def seed(self):
        for name, rel in PATHS.items():
            path = self.root / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(FILES[name])
        proc = self.root / 'proc/sys/kernel/random'
        proc.mkdir(parents=True)
        (proc / 'boot_id').write_text(BOOT + '\n')
        (self.root / 'proc/mounts').write_text('')
        release = {
            'version': '0.2.0-dev.1',
            'fes_revision': 'c' * 40,
            'image_sha256': GOOD,
        }
        manifests = self.root / 'media/fat/fogcast/releases/manifests'
        manifests.mkdir(parents=True)
        (manifests / f'{GOOD}.json').write_text(json.dumps(release) + '\n')
        state = self.root / 'media/fat/fogcast/releases/state.json'
        state.write_text(json.dumps({'format': 1, 'good': GOOD}) + '\n')
        boot = {'boot_id': BOOT, 'image_sha256': GOOD, 'trial': False}
        (self.root / 'media/fat/fogcast/releases/boot.json').write_text(json.dumps(boot) + '\n')
        (self.root / 'tmp').mkdir()
        (self.root / 'media/fat/fes').mkdir(parents=True)

    def fes(self, *args, env=None):
        proc = subprocess.run([sys.executable, str(FES), 'kit', *args],
                              cwd=ROOT, env=env or self.env, text=True, capture_output=True, check=False)
        return proc

    def claim(self, who='700', reason='issue 700 round-trip'):
        proc = self.fes('claim', 'A', '--by', who, '--for', reason)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        return proc

    def test_remote_script_bind_mounts_and_does_not_reboot(self):
        text = REMOTE.read_text()
        self.assertIn('mount --bind', text)
        self.assertIn('umount', text)
        self.assertNotIn('/sbin/reboot', text)
        self.assertNotRegex(text, r'(?m)^\s*reboot\b')

    def test_claim_is_exclusive_and_status_names_the_holder(self):
        self.claim()
        other = self.fes('claim', 'A', '--by', '701', '--for', 'other task')
        self.assertNotEqual(other.returncode, 0)
        self.assertIn('held by 700', other.stderr)
        status = self.fes('status', 'A')
        self.assertEqual(status.returncode, 0, status.stderr)
        self.assertIn('hold: 700', status.stdout)
        self.assertIn('version 0.2.0-dev.1', status.stdout)
        self.assertIn(f'boot_id {BOOT}', status.stdout)
        self.assertIn(digest(FILES['mister-agent']), status.stdout)
        held = list(self.holds.glob('KITA-HELD-BY-*'))
        self.assertEqual([path.name for path in held], ['KITA-HELD-BY-700'])

    def test_swap_restore_is_byte_identical_and_keeps_boot_id(self):
        self.claim()
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        swapped = self.fes('swap', 'A', 'mister-agent', str(new))
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), b'agent-swapped')
        status = self.fes('status', 'A', '--json')
        report = json.loads(status.stdout)
        row = next(item for item in report['components'] if item['component'] == 'mister-agent')
        self.assertEqual(row['current'], digest(b'agent-swapped'))
        self.assertEqual(row['stock'], digest(FILES['mister-agent']))
        self.assertEqual(row['persistence'], 'tmpfs')
        self.assertEqual(row['who'], '700')
        self.assertTrue(row['mounted'])
        restored = self.fes('restore', 'A')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])
        self.assertEqual((self.root / 'proc/sys/kernel/random/boot_id').read_text().strip(), BOOT)
        self.assertEqual(list(self.holds.glob('KITA-HELD-BY-*')), [])
        released = (self.holds / 'KITA-RELEASED-BY-700').read_text()
        self.assertIn('released ', released)
        manifest = json.loads((self.root / 'media/fat/fes/kit-manifest.json').read_text())
        self.assertEqual(manifest['components'], {})
        again = self.fes('restore', 'A')
        self.assertNotEqual(again.returncode, 0)

    def test_persistent_swap_requires_why_and_restores(self):
        self.claim()
        updated = self.work / 'launcher.json'
        updated.write_bytes(b'{"launcher":true,"note":"700"}\n')
        refused = self.fes('swap', 'A', 'launcher.json', str(updated))
        self.assertEqual(refused.returncode, 2, refused.stderr)
        self.assertIn('--why', refused.stderr)
        swapped = self.fes('swap', 'A', 'launcher.json', str(updated), '--why', 'PR 700 launcher')
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        manifest = json.loads((self.root / 'media/fat/fes/kit-manifest.json').read_text())
        self.assertEqual(manifest['components']['launcher.json']['persistence'], 'persistent')
        self.assertEqual(manifest['components']['launcher.json']['why'], 'PR 700 launcher')
        restored = self.fes('restore', 'A')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual((self.root / PATHS['launcher.json']).read_bytes(), FILES['launcher.json'])

    def test_failed_start_rolls_back_to_stock(self):
        self.claim()
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-bad')
        self.env['FES_KIT_FAIL_START'] = '1'
        failed = self.fes('swap', 'A', 'mister-agent', str(new))
        self.assertNotEqual(failed.returncode, 0, failed.stdout)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])
        manifest = json.loads((self.root / 'media/fat/fes/kit-manifest.json').read_text())
        self.assertEqual(manifest['components'], {})
        self.assertTrue(list(self.holds.glob('KITA-HELD-BY-*')))

    def test_agent_swap_leaves_the_runtime_supervisor_alone(self):
        self.claim()
        (self.root / 'tmp/fes-supervisor-mister-runtime').write_text('running\n')
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        swapped = self.fes('swap', 'A', 'mister-agent', str(new))
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        log = (self.root / 'tmp/fes-services').read_text()
        self.assertNotIn('S40mister-runtime', log)
        self.assertIn('start S50mister-agent', log)

    def test_swap_rolls_back_when_its_supervisor_is_still_running(self):
        self.claim()
        (self.root / 'tmp/fes-supervisor-mister-agent').write_text('running\n')
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        refused = self.fes('swap', 'A', 'mister-agent', str(new))
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn('supervisors still running', refused.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])

    def test_runtime_swap_restarts_dependents_in_order(self):
        self.claim()
        new = self.work / 'runtime'
        new.write_bytes(b'runtime-new')
        swapped = self.fes('swap', 'A', 'mister-runtime', str(new))
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        log = (self.root / 'tmp/fes-services').read_text().splitlines()
        self.assertEqual(log, [
            'stop S60fogcast-kit', 'stop S50mister-agent', 'stop S40mister-runtime',
            'start S40mister-runtime', 'start S50mister-agent', 'start S60fogcast-kit'])

    def test_restore_kills_a_stray_tmpfs_agent(self):
        self.claim()
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        self.assertEqual(self.fes('swap', 'A', 'mister-agent', str(new)).returncode, 0)
        stray = self.root / 'proc/55'
        stray.mkdir()
        (stray / 'cmdline').write_bytes(b'mister-agent\0--config\0')
        (stray / 'exe').symlink_to('/tmp/fes-swap/mister-agent')
        restored = self.fes('restore', 'A')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        kills = (self.root / 'tmp/fes-kills').read_text().split()
        self.assertIn('55', kills)
        self.assertFalse(stray.exists())

    def test_boot_id_change_restores_files_and_fails(self):
        self.claim()
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        self.assertEqual(self.fes('swap', 'A', 'mister-agent', str(new)).returncode, 0)
        (self.root / 'proc/sys/kernel/random/boot_id').write_text('22222222-2222-4222-8222-222222222222\n')
        restored = self.fes('restore', 'A')
        self.assertEqual(restored.returncode, 4, restored.stderr)
        self.assertIn('boot id changed', restored.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])
        self.assertEqual(list(self.holds.glob('KITA-HELD-BY-*')), [])

    def test_named_baseline_round_trip(self):
        self.claim()
        self.assertEqual(self.fes('baseline', 'A', 'clean').returncode, 0)
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        self.assertEqual(self.fes('swap', 'A', 'mister-agent', str(new)).returncode, 0)
        self.assertEqual(self.fes('baseline', 'A', 'custom').returncode, 0)
        self.assertEqual(self.fes('restore', 'A', '--baseline', 'clean').returncode, 0, )
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])
        self.claim()
        restored = self.fes('restore', 'A', '--baseline', 'custom')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), b'agent-swapped')
        watched = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(watched.returncode, 0, watched.stdout + watched.stderr)
        self.assertNotIn('DRIFT', watched.stdout)

    def test_watcher_flags_an_unrecorded_change_on_the_next_cycle(self):
        before = self.fes('status', 'A')
        self.assertEqual(before.returncode, 0, before.stderr)
        self.assertIn('stock=unrecorded', before.stdout)
        first = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertIn('BASELINE', first.stdout)
        (self.root / PATHS['mister-agent']).write_bytes(b'hand-made')
        second = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(second.returncode, 1, second.stdout)
        self.assertIn('DRIFT mister-agent unrecorded', second.stdout)
        self.assertTrue((self.drift / 'watch.log').read_text())

    def test_watcher_accepts_a_recorded_swap_and_flags_stale_main(self):
        self.claim()
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        self.assertEqual(self.fes('swap', 'A', 'mister-agent', str(new)).returncode, 0)
        clean = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(clean.returncode, 0, clean.stdout + clean.stderr)
        self.assertNotIn('DRIFT', clean.stdout)
        main = self.work / 'main.hashes'
        main.write_text(f'{digest(b"somewhere-else")} mister-runtime\n')
        stale = self.fes('watch', '--once', '--kits', 'A', '--main-hashes', str(main))
        self.assertEqual(stale.returncode, 0, stale.stderr)
        self.assertIn('STALE mister-runtime', stale.stdout)
        failed = self.fes('watch', '--once', '--kits', 'A', '--main-hashes', str(main), '--fail-stale')
        self.assertEqual(failed.returncode, 1)

    def test_unrecorded_mount_blocks_claim_and_is_flagged(self):
        (self.root / 'proc/mounts').write_text('tmpfs /usr/sbin/mister-agent tmpfs rw 0 0\n')
        refused = self.fes('claim', 'A', '--by', '700', '--for', 'should fail')
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn('unrecorded mounts', refused.stderr)
        self.assertEqual(list(self.holds.glob('KITA-HELD-BY-*')), [])
        flagged = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(flagged.returncode, 1)
        self.assertIn('unrecorded-mount', flagged.stdout)
        dropped = self.fes('restore', 'A', '--force')
        self.assertEqual(dropped.returncode, 0, dropped.stderr)
        self.assertNotIn('/usr/sbin/mister-agent', (self.root / 'proc/mounts').read_text())
        self.assertEqual(self.fes('claim', 'A', '--by', '700', '--for', 'after drift').returncode, 0)


if __name__ == '__main__':
    unittest.main()
