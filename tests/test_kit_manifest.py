"""Host-side tests for the kit runtime manifest."""

import hashlib
import fcntl
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from tests.test_kit_maintenance import authority, TARGET_ID
import kit_manifest
from kit_maintenance import KitMaintenance


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
    'agent.toml': f'token = "secret"\ntarget_id = "{TARGET_ID}"\n'.encode(),
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
        # Keep legacy-journal migration off the real temp directory.
        self.proc_tmp = self.work / 'proc-tmp'
        self.proc_tmp.mkdir()
        saved_tempdir = tempfile.tempdir
        saved_tmpdir = os.environ.get('TMPDIR')

        def restore_tmpdir():
            tempfile.tempdir = saved_tempdir
            if saved_tmpdir is None:
                os.environ.pop('TMPDIR', None)
            else:
                os.environ['TMPDIR'] = saved_tmpdir
            tempfile.tempdir = None

        os.environ['TMPDIR'] = str(self.proc_tmp)
        tempfile.tempdir = None
        self.addCleanup(restore_tmpdir)
        self.env = os.environ.copy()
        self.env.update({
            'FES_KIT_TRANSPORT': 'local',
            'FES_KIT_HOLD_DIR': str(self.holds),
            'FES_KIT_DRIFT_DIR': str(self.drift),
            'FES_KIT_A_ROOT': str(self.root),
            'HOME': str(self.work / 'home'),
        })
        self.env.pop('XDG_STATE_HOME', None)
        self.env.pop('FES_KIT_FAIL_START', None)
        self.env.pop('FES_KIT_FAIL_UMOUNT', None)
        self.env.pop('FES_KIT_FAIL_STAGE', None)
        self.seed()
        self.api_context = authority(self.root)
        self.api = self.api_context.__enter__()
        self.addCleanup(self.api_context.__exit__, None, None, None)
        self.env['FES_KIT_A_API'] = self.api.url

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

    def maintenance(self):
        return KitMaintenance('A', kit_manifest.LocalTransport(self.root), 'local',
                              self.holds / '.fes-kit-maintenance/KITA.json', timeout=2)

    def test_missing_target_maintenance_support_refuses_before_service_stop(self):
        self.claim()
        self.api.unsupported = True
        candidate = self.work / 'mister-agent'
        candidate.write_bytes(b'candidate')
        result = self.fes('swap', 'A', 'mister-agent', str(candidate))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('upgrade required', result.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])
        self.assertEqual((self.root / 'tmp/fes-services').read_text(), '')

    def test_failed_apply_keeps_reservation_until_explicit_finish(self):
        self.claim()
        candidate = self.work / 'mister-agent'
        candidate.write_bytes(b'candidate')
        rejected_env = {**self.env, 'FES_KIT_FAIL_AGENT_CONTRACT': '1'}
        result = self.fes('swap', 'A', 'mister-agent', str(candidate), env=rejected_env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('cannot enforce maintenance', result.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])
        self.assertEqual(self.api.status_value['state'], 'ready')
        self.assertTrue((self.holds / '.fes-kit-maintenance/KITA.json').is_file())
        result = self.fes('maintenance-end', 'A')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.api.status_value['state'], 'completed')
        self.assertFalse((self.holds / '.fes-kit-maintenance/KITA.json').exists())

    def test_hold_and_journal_paths_avoid_developer_home(self):
        root = self.work / 'state'
        root.mkdir()
        (root / 'tmp').mkdir()
        home = self.work / 'home'
        blocked = self.work / 'xdg-file'
        blocked.write_text('not-a-dir')
        saved_temp = tempfile.tempdir
        try:
            with mock.patch.dict(os.environ, {'HOME': str(home), 'TMPDIR': str(root / 'tmp')}):
                os.environ.pop('FES_KIT_HOLD_DIR', None)
                os.environ.pop('FES_KIT_DRIFT_DIR', None)
                os.environ.pop('XDG_STATE_HOME', None)
                tempfile.tempdir = None
                path = kit_manifest.hold_dir()
                durable = home / '.local' / 'state' / 'fes-kit'
                self.assertEqual(path, durable)
                self.assertFalse((root / 'tmp' / f'fes-kit-{os.getuid()}').exists())
                self.assertNotIn('/home/deano', str(path))
                self.assertEqual(stat.S_IMODE(path.stat().st_mode) & 0o077, 0)
                session = kit_manifest.Session('A', kit_manifest.LocalTransport(self.root), 'local')
                self.assertEqual(session.maintenance.journal, path / '.fes-kit-maintenance' / 'KITA.json')
                self.assertNotIn('/home/deano', str(session.maintenance.journal))
                self.assertEqual(kit_manifest.drift_dir(), durable / 'fes-kit-drift')
                os.environ['XDG_STATE_HOME'] = str(root / 'xdg')
                xdg = kit_manifest.hold_dir()
                self.assertEqual(xdg, root / 'xdg' / 'fes-kit')
                os.environ['XDG_STATE_HOME'] = str(blocked)
                tempfile.tempdir = None
                fallback = kit_manifest.hold_dir()
                self.assertEqual(fallback, durable)
                self.assertFalse((root / 'tmp' / f'fes-kit-{os.getuid()}').exists())
                self.assertNotIn('/home/deano', str(fallback))
                os.environ['FES_KIT_HOLD_DIR'] = str(root / 'override')
                self.assertEqual(kit_manifest.hold_dir(), root / 'override')
                # Drift stays on the canonical directory so a timer that does
                # not see FES_KIT_HOLD_DIR still shares the manual snapshot.
                self.assertEqual(kit_manifest.drift_dir(), durable / 'fes-kit-drift')
                os.environ['FES_KIT_DRIFT_DIR'] = str(root / 'drift-override')
                self.assertEqual(kit_manifest.drift_dir(), root / 'drift-override')
                self.assertNotIn('/home/deano', str(kit_manifest.drift_dir()))
        finally:
            tempfile.tempdir = saved_temp

    def test_hold_dir_rejects_shared_writable_and_foreign_directories(self):
        home = self.work / 'private-home'
        group = self.work / 'group-writable'
        world = self.work / 'world-writable'
        group.mkdir()
        world.mkdir()
        group.chmod(0o770)
        world.chmod(0o707)
        link_target = self.work / 'link-target'
        link_target.mkdir(mode=0o700)
        link = self.work / 'hold-link'
        link.symlink_to(link_target, target_is_directory=True)
        foreign = self.work / 'foreign'
        foreign.mkdir(mode=0o707)
        real_lstat = os.lstat

        def fake_lstat(target, *args, **kwargs):
            info = real_lstat(target)
            if os.path.abspath(os.fspath(target)) == os.path.abspath(foreign):
                return mock.Mock(st_mode=info.st_mode, st_uid=info.st_uid + 1)
            return info

        rejected = (
            ('group', group, None),
            ('world', world, None),
            ('symlink', link, None),
            ('foreign', foreign, fake_lstat),
        )
        for label, path, lstat in rejected:
            with self.subTest(label=label):
                with mock.patch.dict(os.environ, {'FES_KIT_HOLD_DIR': str(path), 'HOME': str(home)}):
                    os.environ.pop('XDG_STATE_HOME', None)
                    if lstat is None:
                        with self.assertRaises(kit_manifest.KitError) as caught:
                            kit_manifest.hold_dir()
                    else:
                        with mock.patch('kit_manifest.os.lstat', side_effect=lstat):
                            with self.assertRaises(kit_manifest.KitError) as caught:
                                kit_manifest.hold_dir()
                self.assertIn('owned by the current user', str(caught.exception))
                self.assertIn('not group- or world-writable', str(caught.exception))
                self.assertFalse((home / '.local' / 'state' / 'fes-kit').exists())
        self.assertTrue(stat.S_IMODE(group.stat().st_mode) & stat.S_IWGRP)
        self.assertTrue(stat.S_IMODE(world.stat().st_mode) & stat.S_IWOTH)

    def test_hold_dir_rejects_a_shared_writable_parent(self):
        home = self.work / 'parent-home'
        shared = self.work / 'shared-parent'
        shared.mkdir()
        shared.chmod(0o777)
        leaf = shared / 'fes-kit'
        leaf.mkdir(mode=0o700)
        sticky = self.work / 'sticky-parent'
        sticky.mkdir()
        sticky.chmod(0o1777)
        sticky_leaf = sticky / 'fes-kit'
        with mock.patch.dict(os.environ, {'FES_KIT_HOLD_DIR': str(leaf), 'HOME': str(home)}):
            os.environ.pop('XDG_STATE_HOME', None)
            with self.assertRaises(kit_manifest.KitError) as caught:
                kit_manifest.hold_dir()
        self.assertIn('shared writable directory', str(caught.exception))
        self.assertFalse((home / '.local' / 'state' / 'fes-kit').exists())
        with mock.patch.dict(os.environ, {'FES_KIT_HOLD_DIR': str(sticky_leaf), 'HOME': str(home)}):
            os.environ.pop('XDG_STATE_HOME', None)
            path = kit_manifest.hold_dir()
        self.assertEqual(path, sticky_leaf)
        self.assertEqual(stat.S_IMODE(path.stat().st_mode) & 0o077, 0)

    def test_legacy_temp_journal_moves_into_the_durable_directory(self):
        home = self.work / 'migrate-home'
        legacy_root = self.work / 'legacy-tmp'
        legacy_root.mkdir()
        legacy = legacy_root / f'fes-kit-{os.getuid()}'
        journal_dir = legacy / '.fes-kit-maintenance'
        journal_dir.mkdir(parents=True, mode=0o700)
        legacy.chmod(0o700)
        journal_dir.chmod(0o700)
        journal = journal_dir / 'KITA.json'
        payload = '{"request_id":"legacy"}\n'
        journal.write_text(payload)
        journal.chmod(0o600)
        marker = legacy / 'KITA-HELD-BY-700'
        marker.write_text('who=700\n')
        marker.chmod(0o644)
        saved_temp = tempfile.tempdir
        try:
            with mock.patch.dict(os.environ, {'HOME': str(home), 'TMPDIR': str(legacy_root)}):
                os.environ.pop('FES_KIT_HOLD_DIR', None)
                os.environ.pop('FES_KIT_DRIFT_DIR', None)
                os.environ.pop('XDG_STATE_HOME', None)
                tempfile.tempdir = None
                path = kit_manifest.hold_dir()
                dest = path / '.fes-kit-maintenance' / 'KITA.json'
                self.assertEqual(dest.read_text(), payload)
                self.assertEqual(stat.S_IMODE(dest.stat().st_mode), 0o600)
                self.assertFalse(journal.exists())
                self.assertEqual((path / 'KITA-HELD-BY-700').read_text(), 'who=700\n')
                self.assertFalse(marker.exists())
                session = kit_manifest.Session('A', kit_manifest.LocalTransport(self.root), 'local')
                self.assertEqual(session.maintenance.journal, dest)
        finally:
            tempfile.tempdir = saved_temp

    def test_unsafe_or_existing_journal_is_not_migrated(self):
        home = self.work / 'migrate-home'
        legacy_root = self.work / 'legacy-tmp'
        legacy_root.mkdir()
        legacy = legacy_root / f'fes-kit-{os.getuid()}'
        journal_dir = legacy / '.fes-kit-maintenance'
        journal_dir.mkdir(parents=True, mode=0o700)
        legacy.chmod(0o777)
        journal = journal_dir / 'KITA.json'
        journal.write_text('{"request_id":"unsafe"}\n')
        journal.chmod(0o600)
        saved_temp = tempfile.tempdir
        try:
            with mock.patch.dict(os.environ, {'HOME': str(home), 'TMPDIR': str(legacy_root)}):
                os.environ.pop('FES_KIT_HOLD_DIR', None)
                os.environ.pop('FES_KIT_DRIFT_DIR', None)
                os.environ.pop('XDG_STATE_HOME', None)
                tempfile.tempdir = None
                path = kit_manifest.hold_dir()
                self.assertFalse((path / '.fes-kit-maintenance' / 'KITA.json').exists())
                self.assertTrue(journal.is_file())
                legacy.chmod(0o700)
                journal_dir.chmod(0o700)
                kept = path / '.fes-kit-maintenance' / 'KITA.json'
                kept.parent.mkdir(mode=0o700)
                kept.write_text('{"request_id":"current"}\n')
                kept.chmod(0o600)
                kit_manifest.hold_dir()
                self.assertEqual(kept.read_text(), '{"request_id":"current"}\n')
                self.assertTrue(journal.is_file())
        finally:
            tempfile.tempdir = saved_temp

    def _legacy_private_hold(self, legacy_root, journal_payload, drift_payload=None):
        legacy = legacy_root / f'fes-kit-{os.getuid()}'
        journal_dir = legacy / '.fes-kit-maintenance'
        journal_dir.mkdir(parents=True, mode=0o700)
        legacy.chmod(0o700)
        journal_dir.chmod(0o700)
        journal = journal_dir / 'KITA.json'
        journal.write_text(journal_payload)
        journal.chmod(0o600)
        snapshot = None
        if drift_payload is not None:
            drift_src = legacy / 'fes-kit-drift'
            drift_src.mkdir(mode=0o700)
            drift_src.chmod(0o700)
            snapshot = drift_src / 'A.json'
            snapshot.write_text(drift_payload)
            snapshot.chmod(0o644)
        return legacy, journal, snapshot

    def test_legacy_drift_snapshot_moves_to_the_canonical_directory(self):
        home = self.work / 'drift-home'
        legacy_root = self.work / 'legacy-tmp'
        legacy_root.mkdir()
        payload = '{"format":1,"hashes":{"mister-agent":"abc"}}\n'
        journal_payload = '{"request_id":"legacy"}\n'
        _legacy, journal, snapshot = self._legacy_private_hold(legacy_root, journal_payload, payload)
        hold = self.work / 'hold-override'
        saved_temp = tempfile.tempdir
        try:
            with mock.patch.dict(os.environ, {
                'HOME': str(home),
                'TMPDIR': str(legacy_root),
                'FES_KIT_HOLD_DIR': str(hold),
            }):
                os.environ.pop('FES_KIT_DRIFT_DIR', None)
                os.environ.pop('XDG_STATE_HOME', None)
                tempfile.tempdir = None
                path = kit_manifest.hold_dir()
                self.assertEqual(path, hold)
                self.assertEqual((path / '.fes-kit-maintenance' / 'KITA.json').read_text(), journal_payload)
                self.assertFalse(journal.exists())
                canonical = home / '.local/state/fes-kit/fes-kit-drift/A.json'
                self.assertEqual(canonical.read_text(), payload)
                self.assertEqual(stat.S_IMODE(canonical.stat().st_mode), 0o644)
                self.assertFalse(snapshot.exists())
                self.assertFalse((hold / 'fes-kit-drift').exists())
        finally:
            tempfile.tempdir = saved_temp

    def test_unwritable_home_routes_legacy_drift_without_blocking_the_hold(self):
        home = self.work / 'ro-home'
        state = home / '.local' / 'state'
        state.mkdir(parents=True)
        state.chmod(0o555)
        legacy_root = self.work / 'legacy-tmp'
        legacy_root.mkdir()
        payload = '{"format":1,"hashes":{"mister-agent":"abc"}}\n'
        journal_payload = '{"request_id":"legacy"}\n'
        _legacy, journal, snapshot = self._legacy_private_hold(legacy_root, journal_payload, payload)
        hold = self.work / 'writable-hold'
        drift = self.work / 'writable-drift'
        saved_temp = tempfile.tempdir
        try:
            with mock.patch.dict(os.environ, {
                'HOME': str(home),
                'TMPDIR': str(legacy_root),
                'FES_KIT_HOLD_DIR': str(hold),
                'FES_KIT_DRIFT_DIR': str(drift),
            }):
                os.environ.pop('XDG_STATE_HOME', None)
                tempfile.tempdir = None
                path = kit_manifest.hold_dir()
                self.assertEqual(path, hold)
                dest = path / '.fes-kit-maintenance' / 'KITA.json'
                self.assertEqual(dest.read_text(), journal_payload)
                self.assertFalse(journal.exists())
                routed = drift / 'A.json'
                self.assertEqual(routed.read_text(), payload)
                self.assertFalse(snapshot.exists())
                self.assertFalse((state / 'fes-kit').exists())
                session = kit_manifest.Session('A', kit_manifest.LocalTransport(self.root), 'local')
                self.assertEqual(session.maintenance.journal, dest)
        finally:
            state.chmod(0o755)
            tempfile.tempdir = saved_temp

    def test_unwritable_drift_destinations_skip_snapshot_migration(self):
        home = self.work / 'ro-home'
        state = home / '.local' / 'state'
        state.mkdir(parents=True)
        state.chmod(0o555)
        legacy_root = self.work / 'legacy-tmp'
        legacy_root.mkdir()
        payload = '{"format":1,"hashes":{"mister-agent":"abc"}}\n'
        journal_payload = '{"request_id":"kept"}\n'
        _legacy, journal, snapshot = self._legacy_private_hold(legacy_root, journal_payload, payload)
        hold = self.work / 'writable-hold'
        hold.mkdir(mode=0o700)
        blocker = hold / 'fes-kit-drift'
        blocker.write_text('not-a-directory')
        saved_temp = tempfile.tempdir
        try:
            with mock.patch.dict(os.environ, {
                'HOME': str(home),
                'TMPDIR': str(legacy_root),
                'FES_KIT_HOLD_DIR': str(hold),
            }):
                os.environ.pop('FES_KIT_DRIFT_DIR', None)
                os.environ.pop('XDG_STATE_HOME', None)
                tempfile.tempdir = None
                path = kit_manifest.hold_dir()
                self.assertEqual(path, hold)
                self.assertEqual((path / '.fes-kit-maintenance' / 'KITA.json').read_text(), journal_payload)
                self.assertFalse(journal.exists())
                self.assertEqual(snapshot.read_text(), payload)
                self.assertEqual(blocker.read_text(), 'not-a-directory')
                self.assertFalse((state / 'fes-kit').exists())
        finally:
            state.chmod(0o755)
            tempfile.tempdir = saved_temp

    def test_manual_and_timer_share_the_drift_baseline(self):
        unit = (ROOT / 'scripts/systemd/fes-kit-drift.service').read_text()
        self.assertNotIn('FES_KIT_HOLD_DIR=', unit)
        self.assertNotIn('FES_KIT_DRIFT_DIR=', unit)
        self.assertNotIn('/home/deano/tmp', unit)
        home = self.work / 'watch-home'
        manual = self.env.copy()
        manual.pop('FES_KIT_HOLD_DIR', None)
        manual.pop('FES_KIT_DRIFT_DIR', None)
        manual.pop('XDG_STATE_HOME', None)
        manual['HOME'] = str(home)
        manual['TMPDIR'] = str(self.work / 'manual-tmp')
        first = self.fes('watch', '--once', '--kits', 'A', env=manual)
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertIn('UNVERIFIED', first.stdout)
        snapshot = home / '.local/state/fes-kit/fes-kit-drift/A.json'
        self.assertTrue(snapshot.is_file(), first.stdout)
        self.assertNotIn('/home/deano', str(snapshot))
        self.assertFalse((self.work / 'manual-tmp').exists())
        (self.root / PATHS['mister-agent']).write_bytes(b'hand-made')
        shared = self.fes('watch', '--once', '--kits', 'A', env=manual)
        self.assertEqual(shared.returncode, 1, shared.stdout + shared.stderr)
        self.assertIn('DRIFT mister-agent unrecorded', shared.stdout)
        separated = manual.copy()
        separated['FES_KIT_DRIFT_DIR'] = str(self.work / 'timer-drift')
        (self.root / PATHS['mister-agent']).write_bytes(b'hand-made-again')
        timer = self.fes('watch', '--once', '--kits', 'A', env=separated)
        self.assertEqual(timer.returncode, 1, timer.stdout + timer.stderr)
        self.assertIn('DRIFT mister-agent unrecorded', timer.stdout)
        self.assertTrue((self.work / 'timer-drift' / 'A.json').is_file())

    def test_xdg_watch_and_timer_share_the_canonical_drift_directory(self):
        home = self.work / 'xdg-home'
        xdg = self.work / 'xdg-state'
        manual = self.env.copy()
        manual.pop('FES_KIT_HOLD_DIR', None)
        manual.pop('FES_KIT_DRIFT_DIR', None)
        manual['HOME'] = str(home)
        manual['XDG_STATE_HOME'] = str(xdg)
        manual['TMPDIR'] = str(self.proc_tmp)
        first = self.fes('watch', '--once', '--kits', 'A', env=manual)
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertIn('UNVERIFIED', first.stdout)
        snapshot = home / '.local/state/fes-kit/fes-kit-drift/A.json'
        self.assertTrue(snapshot.is_file(), first.stdout + first.stderr)
        self.assertFalse((xdg / 'fes-kit/fes-kit-drift/A.json').exists())
        (self.root / PATHS['mister-agent']).write_bytes(b'between-xdg-and-timer')
        timer = manual.copy()
        timer.pop('XDG_STATE_HOME')
        second = self.fes('watch', '--once', '--kits', 'A', env=timer)
        self.assertEqual(second.returncode, 1, second.stdout + second.stderr)
        self.assertIn('DRIFT mister-agent unrecorded', second.stdout)

    def test_explicit_drift_directory_does_not_require_a_home(self):
        drift = self.work / 'only-drift'
        env = {
            'FES_KIT_TRANSPORT': 'local',
            'FES_KIT_HOLD_DIR': str(self.holds),
            'FES_KIT_DRIFT_DIR': str(drift),
            'FES_KIT_A_ROOT': str(self.root),
            'FES_KIT_A_API': self.env['FES_KIT_A_API'],
            'TMPDIR': str(self.proc_tmp),
            'PATH': os.environ.get('PATH', ''),
        }
        with mock.patch.dict(os.environ, env, clear=True):
            with mock.patch('kit_manifest.Path.home', side_effect=RuntimeError('no home directory')):
                self.assertEqual(kit_manifest.previous_watch_hashes('A'), {})
                self.assertEqual(kit_manifest.drift_dir(), drift)
                code = kit_manifest.main(['kit', 'watch', '--once', '--kits', 'A'])
        self.assertEqual(code, 0)
        self.assertTrue((drift / 'A.json').is_file())
        (self.root / PATHS['mister-agent']).write_bytes(b'explicit-drift-edit')
        with mock.patch.dict(os.environ, env, clear=True):
            with mock.patch('kit_manifest.Path.home', side_effect=RuntimeError('no home directory')):
                code = kit_manifest.main(['kit', 'watch', '--once', '--kits', 'A'])
        self.assertEqual(code, 1)
        text = (drift / 'watch.log').read_text()
        self.assertIn('DRIFT mister-agent unrecorded', text)

    def test_omitted_target_id_follows_persisted_identity(self):
        (self.root / 'media/fat/fogcast/target-id').write_text(TARGET_ID + '\n')
        (self.root / PATHS['agent.toml']).write_bytes(b'token = "secret"\n')
        self.claim()
        candidate = self.work / 'mister-agent'
        candidate.write_bytes(b'candidate-agent')
        result = self.fes('swap', 'A', 'mister-agent', str(candidate))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), b'candidate-agent')

    def test_omitted_target_id_without_persisted_identity_does_not_swap(self):
        (self.root / PATHS['agent.toml']).write_bytes(b'token = "secret"\n')
        self.claim()
        candidate = self.work / 'mister-agent'
        candidate.write_bytes(b'candidate-agent')
        result = self.fes('swap', 'A', 'mister-agent', str(candidate))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('identity', result.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])

    def test_config_identity_transition_confirms_original_boot_across_swap_and_restore(self):
        self.claim()
        new_id = '33333333-3333-4333-8333-333333333333'
        candidate = self.work / 'agent.toml'
        candidate.write_text(f'token = "secret"\ntarget_id = "{new_id}"\n')
        swapped = self.fes('swap', 'A', 'agent.toml', str(candidate), '--why', 'bind the replacement identity')
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        self.assertIn(new_id, (self.root / PATHS['agent.toml']).read_text())
        self.assertEqual(self.api.status_value['state'], 'completed')
        restored = self.fes('restore', 'A')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual((self.root / PATHS['agent.toml']).read_bytes(), FILES['agent.toml'])
        self.assertEqual(self.api.status_value['state'], 'completed')

    def test_stale_manifest_plan_cannot_replace_newer_manifest(self):
        with mock.patch.dict(os.environ, self.env):
            session = kit_manifest.Session('A', kit_manifest.LocalTransport(self.root), 'local')
        session.probe()
        manifest = self.root / 'media/fat/fes/kit-manifest.json'
        newer = b'{"format":1,"holder":{"who":"other"}}\n'
        manifest.write_bytes(newer)
        source = session.put_text(session.incoming_prefix + '/manifest.json', b'{"format":1}\n')
        with self.assertRaisesRegex(kit_manifest.KitError, 'manifest changed'):
            session.apply([f'BOOT {BOOT}', f'MANIFEST {source}', 'COMMIT'])
        self.assertEqual(manifest.read_bytes(), newer)

    def test_apply_rejects_stale_reservation_before_service_stop(self):
        marker = self.root / 'run/fogcast/kit-maintenance.json'
        marker.parent.mkdir(parents=True, exist_ok=True)
        marker.write_text(json.dumps({'state': 'ready', 'operation_id': 'a' * 64,
                                     'generation': 'b' * 64, 'boot_id': BOOT}))
        plan = self.root / 'tmp/fes-incoming/stale-plan'
        plan.parent.mkdir(parents=True)
        plan.write_text(f'MANIFEST_EXPECT MISSING\nMAINTENANCE {"c" * 64} {"b" * 64} {BOOT}\n'
                        f'BOOT {BOOT}\nSTOP S50mister-agent\nSTART S50mister-agent\nCOMMIT\n')
        with self.assertRaisesRegex(kit_manifest.KitError, 'operation changed'):
            kit_manifest.LocalTransport(self.root).run(['apply', '/tmp/fes-incoming/stale-plan'])
        self.assertEqual((self.root / 'tmp/fes-services').read_text(), '')

    def test_install_cannot_bypass_component_maintenance(self):
        source = self.root / 'tmp/fes-incoming/source'
        source.parent.mkdir(parents=True)
        source.write_bytes(b'replacement')
        plan = source.parent / 'plan'
        plan.write_text(f'MANIFEST_EXPECT MISSING\nBOOT {BOOT}\n'
                        'INSTALL /tmp/fes-incoming/source /usr/sbin/mister-agent\nCOMMIT\n')
        with self.assertRaisesRegex(kit_manifest.KitError, 'restricted to baseline metadata'):
            kit_manifest.LocalTransport(self.root).run(['apply', '/tmp/fes-incoming/plan'])
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])

    def test_same_owner_label_cannot_restore_a_newer_task(self):
        self.claim()
        candidate = self.work / 'mister-agent'
        candidate.write_bytes(b'candidate')
        self.assertEqual(self.fes('swap', 'A', 'mister-agent', str(candidate)).returncode, 0)
        manifest = self.root / 'media/fat/fes/kit-manifest.json'
        newer = json.loads(manifest.read_text())
        newer['holder']['task_id'] = 'd' * 64
        manifest.write_text(json.dumps(newer))
        calls = len(self.api.requests)
        result = self.fes('restore', 'A')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('another diagnostic task', result.stderr)
        self.assertEqual(len(self.api.requests), calls)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), b'candidate')
        result = self.fes('restore', 'A', '--force', '--expected-manifest-sha256', digest(manifest.read_bytes()))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])

    def test_executor_lock_refuses_before_shared_scratch_changes(self):
        lock = self.root / 'run/fogcast/kit-maintenance.lock'
        lock.parent.mkdir(parents=True, exist_ok=True)
        scratch = self.root / 'tmp/fes-work/sentinel'
        scratch.parent.mkdir(parents=True)
        scratch.write_bytes(b'other transaction')
        source = self.root / 'tmp/fes-incoming/plan'
        source.parent.mkdir(parents=True)
        source.write_text(f'MANIFEST_EXPECT MISSING\nBOOT {BOOT}\nCOMMIT\n')
        with lock.open('w') as handle:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
            with self.assertRaisesRegex(kit_manifest.KitError, 'another kit transaction'):
                kit_manifest.LocalTransport(self.root).run(['apply', '/tmp/fes-incoming/plan'])
        self.assertEqual(scratch.read_bytes(), b'other transaction')

    def test_old_marker_and_manifest_without_task_identity_cannot_swap(self):
        self.claim()
        hold = next(self.holds.glob('KITA-HELD-BY-*'))
        hold.write_text('\n'.join(line for line in hold.read_text().splitlines()
                                  if not line.startswith('task_id=')) + '\n')
        manifest = self.root / 'media/fat/fes/kit-manifest.json'
        old = json.loads(manifest.read_text())
        old['holder'].pop('task_id')
        manifest.write_text(json.dumps(old))
        candidate = self.work / 'mister-agent'
        candidate.write_bytes(b'candidate')
        calls = len(self.api.requests)
        result = self.fes('swap', 'A', 'mister-agent', str(candidate))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('different diagnostic task', result.stderr)
        self.assertEqual(len(self.api.requests), calls)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])

    def test_boot_change_rejects_metadata_plan_before_publication(self):
        source = self.root / 'tmp/fes-incoming/manifest.json'
        source.parent.mkdir(parents=True)
        source.write_text('{"holder":{"who":"stale"}}')
        plan = source.parent / 'plan'
        plan.write_text('MANIFEST_EXPECT MISSING\nBOOT 22222222-2222-4222-8222-222222222222\n'
                        'MANIFEST /tmp/fes-incoming/manifest.json\nCOMMIT\n')
        with self.assertRaisesRegex(kit_manifest.KitError, 'boot id changed'):
            kit_manifest.LocalTransport(self.root).run(['apply', '/tmp/fes-incoming/plan'])
        self.assertFalse((self.root / 'media/fat/fes/kit-manifest.json').exists())

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

    def test_boot_id_change_refuses_restoration_until_explicitly_bound(self):
        self.claim()
        new = self.work / 'mister-agent'
        new.write_bytes(b'agent-swapped')
        self.assertEqual(self.fes('swap', 'A', 'mister-agent', str(new)).returncode, 0)
        (self.root / 'proc/sys/kernel/random/boot_id').write_text('22222222-2222-4222-8222-222222222222\n')
        restored = self.fes('restore', 'A')
        self.assertNotEqual(restored.returncode, 0, restored.stderr)
        self.assertIn('boot id changed', restored.stderr)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), b'agent-swapped')
        self.assertTrue(list(self.holds.glob('KITA-HELD-BY-*')))
        restored = self.fes('restore', 'A', '--expected-boot-id',
                            '22222222-2222-4222-8222-222222222222')
        self.assertEqual(restored.returncode, 0, restored.stderr)
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
        self.assertIn('UNVERIFIED', first.stdout)
        self.assertNotIn(' kit A OK', first.stdout)
        self.assertFalse((self.root / 'media/fat/fes/baseline.json').exists())
        self.assertIn('stock=unrecorded', self.fes('status', 'A').stdout)
        (self.root / PATHS['mister-agent']).write_bytes(b'hand-made')
        second = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(second.returncode, 1, second.stdout)
        self.assertIn('DRIFT mister-agent unrecorded', second.stdout)
        self.assertTrue((self.drift / 'watch.log').read_text())

    def test_group_umask_keeps_drift_history(self):
        previous = os.umask(0o002)
        try:
            first = self.fes('watch', '--once', '--kits', 'A')
            self.assertEqual(first.returncode, 0, first.stderr)
            snapshot = self.drift / 'A.json'
            self.assertEqual(stat.S_IMODE(snapshot.stat().st_mode), 0o644)
            self.assertEqual(list(self.drift.glob('.A.json.*.tmp')), [])
            (self.root / PATHS['mister-agent']).write_bytes(b'umask-edit')
            second = self.fes('watch', '--once', '--kits', 'A')
            self.assertEqual(second.returncode, 1, second.stdout + second.stderr)
            self.assertIn('DRIFT mister-agent unrecorded', second.stdout)
            self.assertEqual(stat.S_IMODE(snapshot.stat().st_mode), 0o644)
            self.assertEqual(list(self.drift.glob('.A.json.*.tmp')), [])
        finally:
            os.umask(previous)

    def test_group_writable_snapshot_is_compared_then_tightened(self):
        self.drift.mkdir()
        expected = digest(b'previous-agent')
        payload = json.dumps({
            'format': 1,
            'when': '2020-01-01T00:00:00Z',
            'hashes': {'mister-agent': expected},
        }) + '\n'
        snapshot = self.drift / 'A.json'
        snapshot.write_text(payload)
        snapshot.chmod(0o664)
        self.assertTrue(stat.S_IMODE(snapshot.stat().st_mode) & stat.S_IWGRP)
        previous = os.umask(0o002)
        try:
            watched = self.fes('watch', '--once', '--kits', 'A')
        finally:
            os.umask(previous)
        self.assertEqual(watched.returncode, 1, watched.stdout + watched.stderr)
        self.assertIn('DRIFT mister-agent unrecorded', watched.stdout)
        self.assertIn(f'expected={expected}', watched.stdout)
        self.assertEqual(stat.S_IMODE(snapshot.stat().st_mode), 0o644)
        self.assertEqual(list(self.drift.glob('.A.json.*.tmp')), [])

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

    def core_path(self, name='core.rbf', package=None):
        package = package or ('ab' * 32)
        return f'/usr/share/mister-runtime/core-packages/{package}/{name}'

    def write_core(self, data, name='core.rbf', package=None):
        path = self.root / self.core_path(name, package).lstrip('/')
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def test_core_package_swap_restore_status_and_drift(self):
        self.claim()
        path = self.write_core(b'core-stock')
        replacement = self.work / 'core.rbf'
        replacement.write_bytes(b'core-swapped')
        swapped = self.fes('swap', 'A', self.core_path(), str(replacement))
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        self.assertEqual(path.read_bytes(), b'core-swapped')
        status = self.fes('status', 'A', '--json')
        self.assertEqual(status.returncode, 0, status.stderr)
        report = json.loads(status.stdout)
        row = next(item for item in report['components'] if item['path'] == self.core_path())
        self.assertEqual(row['current'], digest(b'core-swapped'))
        self.assertEqual(row['stock'], digest(b'core-stock'))
        self.assertTrue(row['mounted'])
        watched = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(watched.returncode, 0, watched.stdout + watched.stderr)
        self.assertNotIn('DRIFT', watched.stdout)
        restored = self.fes('restore', 'A')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual(path.read_bytes(), b'core-stock')
        self.assertNotIn(self.core_path(), (self.root / 'proc/mounts').read_text())
        manifest = json.loads((self.root / 'media/fat/fes/kit-manifest.json').read_text())
        self.assertEqual(manifest['components'], {})

    def test_force_restore_drops_an_abandoned_core_mount(self):
        mounted = self.core_path()
        self.write_core(b'core-stock')
        (self.root / 'proc/mounts').write_text(f'tmpfs {mounted} tmpfs rw 0 0\n')
        refused = self.fes('claim', 'A', '--by', '700', '--for', 'core mount')
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn('unrecorded mounts', refused.stderr)
        dropped = self.fes('restore', 'A', '--force')
        self.assertEqual(dropped.returncode, 0, dropped.stderr)
        self.assertNotIn(mounted, (self.root / 'proc/mounts').read_text())
        log = (self.root / 'tmp/fes-services').read_text()
        self.assertIn('start S40mister-runtime', log)
        self.assertEqual(self.fes('claim', 'A', '--by', '700', '--for', 'after core mount').returncode, 0)

    def test_failed_unmount_restores_mounts_already_removed(self):
        first = self.core_path('core.rbf', 'ab' * 32)
        second = self.core_path('core.rbf', 'cd' * 32)
        self.write_core(b'one', 'core.rbf', 'ab' * 32)
        self.write_core(b'two', 'core.rbf', 'cd' * 32)
        (self.root / 'proc/mounts').write_text(
            f'tmpfs {first} tmpfs rw 0 0\n'
            f'tmpfs {second} tmpfs rw 0 0\n')
        self.env['FES_KIT_FAIL_UMOUNT'] = '2'
        failed = self.fes('restore', 'A', '--force')
        self.assertNotEqual(failed.returncode, 0, failed.stdout)
        self.assertIn('umount failed', failed.stderr)
        mounts = (self.root / 'proc/mounts').read_text()
        self.assertIn(first, mounts)
        self.assertIn(second, mounts)
        self.assertEqual((self.root / first.lstrip('/')).read_bytes(), b'one')
        self.assertEqual((self.root / second.lstrip('/')).read_bytes(), b'two')
        log = (self.root / 'tmp/fes-services').read_text()
        self.assertIn('stop S40mister-runtime', log)
        self.assertIn('start S40mister-runtime', log)
        self.env.pop('FES_KIT_FAIL_UMOUNT', None)
        dropped = self.fes('restore', 'A', '--force')
        self.assertEqual(dropped.returncode, 0, dropped.stderr)
        mounts = (self.root / 'proc/mounts').read_text()
        self.assertNotIn(first, mounts)
        self.assertNotIn(second, mounts)

    def test_failed_stage_restarts_services_and_restores_mounts(self):
        first = self.core_path('core.rbf', 'ab' * 32)
        second = self.core_path('core.rbf', 'cd' * 32)
        self.write_core(b'one', 'core.rbf', 'ab' * 32)
        self.write_core(b'two', 'core.rbf', 'cd' * 32)
        (self.root / 'proc/mounts').write_text(
            f'tmpfs {first} tmpfs rw 0 0\n'
            f'tmpfs {second} tmpfs rw 0 0\n')
        self.env['FES_KIT_FAIL_STAGE'] = '2'
        failed = self.fes('restore', 'A', '--force')
        self.assertNotEqual(failed.returncode, 0, failed.stdout)
        self.assertIn('stage failed', failed.stderr)
        self.assertIn(second, failed.stderr)
        mounts = (self.root / 'proc/mounts').read_text()
        self.assertIn(first, mounts)
        self.assertIn(second, mounts)
        self.assertEqual((self.root / first.lstrip('/')).read_bytes(), b'one')
        self.assertEqual((self.root / second.lstrip('/')).read_bytes(), b'two')
        log = (self.root / 'tmp/fes-services').read_text()
        for service in ('S40mister-runtime', 'S50mister-agent', 'S60fogcast-kit'):
            self.assertLess(log.index(f'stop {service}'), log.index(f'start {service}'))
        self.env.pop('FES_KIT_FAIL_STAGE', None)
        dropped = self.fes('restore', 'A', '--force')
        self.assertEqual(dropped.returncode, 0, dropped.stderr)
        mounts = (self.root / 'proc/mounts').read_text()
        self.assertNotIn(first, mounts)
        self.assertNotIn(second, mounts)
        work = self.root / 'tmp/fes-work'
        staged = [path.name for path in work.glob('kept-*')]
        staged += [path.name for path in work.glob('bytes-*')]
        staged += [path.name for path in work.glob('removed-*')]
        staged += [path.name for path in work.glob('manifest.prev')]
        staged += [path.name for path in work.glob('stage-*') if path.name != 'stage-count']
        self.assertEqual(staged, [])

    def test_failed_persistent_switch_restores_the_bind(self):
        self.claim()
        temporary = self.work / 'launcher-temp.json'
        temporary.write_bytes(b'{"launcher":true,"temp":1}\n')
        first = self.fes('swap', 'A', 'launcher.json', str(temporary), '--temporary')
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertIn('/media/fat/fogcast/launcher.json', (self.root / 'proc/mounts').read_text())
        persistent = self.work / 'launcher-pers.json'
        persistent.write_bytes(b'{"launcher":true,"pers":1}\n')
        self.env['FES_KIT_FAIL_START'] = '1'
        failed = self.fes('swap', 'A', 'launcher.json', str(persistent),
                          '--persistent', '--why', 'restart should fail')
        self.assertNotEqual(failed.returncode, 0, failed.stdout)
        self.assertEqual((self.root / PATHS['launcher.json']).read_bytes(), temporary.read_bytes())
        self.assertIn('/media/fat/fogcast/launcher.json', (self.root / 'proc/mounts').read_text())
        manifest = json.loads((self.root / 'media/fat/fes/kit-manifest.json').read_text())
        entry = manifest['components']['launcher.json']
        self.assertEqual(entry['persistence'], 'tmpfs')
        self.assertEqual(entry['current_sha256'], digest(temporary.read_bytes()))
        backup = self.root / 'media/fat/fes/stock' / GOOD / 'launcher.json'
        self.assertEqual(backup.read_bytes(), FILES['launcher.json'])

    def test_new_image_gets_its_own_stock_backup(self):
        self.claim()
        replacement = self.work / 'mister-agent'
        replacement.write_bytes(b'agent-swapped')
        self.assertEqual(self.fes('swap', 'A', 'mister-agent', str(replacement)).returncode, 0)
        self.assertEqual(self.fes('restore', 'A').returncode, 0)
        old = self.root / 'media/fat/fes/stock' / GOOD / 'mister-agent'
        self.assertEqual(old.read_bytes(), FILES['mister-agent'])
        new_sha = 'cd' * 32
        boot = json.loads((self.root / 'media/fat/fogcast/releases/boot.json').read_text())
        boot['image_sha256'] = new_sha
        (self.root / 'media/fat/fogcast/releases/boot.json').write_text(json.dumps(boot) + '\n')
        (self.root / PATHS['mister-agent']).write_bytes(b'agent-image-2')
        self.claim()
        swapped = self.fes('swap', 'A', 'mister-agent', str(replacement))
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        new_backup = self.root / 'media/fat/fes/stock' / new_sha / 'mister-agent'
        self.assertEqual(new_backup.read_bytes(), b'agent-image-2')
        self.assertEqual(old.read_bytes(), FILES['mister-agent'])
        self.assertEqual(self.fes('restore', 'A').returncode, 0)
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), b'agent-image-2')

    def test_baseline_restore_removes_a_file_absent_from_the_snapshot(self):
        agent = self.root / PATHS['launcher.json']
        agent.unlink()
        self.claim()
        saved = self.fes('baseline', 'A', 'before')
        self.assertEqual(saved.returncode, 0, saved.stderr)
        snapshot = json.loads((self.root / 'media/fat/fes/baselines/before.json').read_text())
        self.assertTrue(snapshot['files']['launcher.json']['missing'])
        agent.write_bytes(b'token = "added"\n')
        updated = self.work / 'launcher.json'
        updated.write_bytes(b'token = "swapped"\n')
        swapped = self.fes('swap', 'A', 'launcher.json', str(updated), '--why', 'added after baseline')
        self.assertEqual(swapped.returncode, 0, swapped.stderr)
        restored = self.fes('restore', 'A', '--baseline', 'before')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertFalse(agent.exists())
        manifest = json.loads((self.root / 'media/fat/fes/kit-manifest.json').read_text())
        self.assertNotIn('launcher.json', manifest['components'])
        self.assertEqual(list(self.holds.glob('KITA-HELD-BY-*')), [])
        self.assertEqual((self.root / PATHS['mister-agent']).read_bytes(), FILES['mister-agent'])

    def test_baseline_restore_does_not_keep_drift_as_stock(self):
        self.claim()
        self.assertEqual(self.fes('baseline', 'A', 'clean').returncode, 0)
        drifted = b'{"launcher":true,"drift":1}\n'
        (self.root / PATHS['launcher.json']).write_bytes(drifted)
        restored = self.fes('restore', 'A', '--baseline', 'clean')
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual((self.root / PATHS['launcher.json']).read_bytes(), FILES['launcher.json'])
        manifest = json.loads((self.root / 'media/fat/fes/kit-manifest.json').read_text())
        entry = manifest['components'].get('launcher.json')
        if entry:
            self.assertEqual(entry['stock_sha256'], digest(FILES['launcher.json']))
            self.assertNotEqual(entry['stock_sha256'], digest(drifted))
        backup = self.root / 'media/fat/fes/stock' / GOOD / 'launcher.json'
        if backup.exists():
            self.assertEqual(backup.read_bytes(), FILES['launcher.json'])
        self.claim()
        again = self.fes('restore', 'A')
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual((self.root / PATHS['launcher.json']).read_bytes(), FILES['launcher.json'])

    def test_watcher_does_not_certify_preexisting_drift(self):
        (self.root / PATHS['agent.toml']).write_bytes(b'token = "edited"\n')
        first = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertIn('UNVERIFIED', first.stdout)
        self.assertNotIn(' kit A OK', first.stdout)
        self.assertFalse((self.root / 'media/fat/fes/baseline.json').exists())
        status = self.fes('status', 'A')
        self.assertIn('stock=unrecorded', status.stdout)
        second = self.fes('watch', '--once', '--kits', 'A')
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertIn('UNVERIFIED', second.stdout)
        self.assertNotIn('DRIFT', second.stdout)
        self.assertNotIn(' kit A OK', second.stdout)

    def test_watcher_records_baseline_from_a_matching_reference(self):
        main = self.work / 'main.hashes'
        main.write_text(''.join(f'{digest(data)} {name}\n' for name, data in FILES.items()))
        first = self.fes('watch', '--once', '--kits', 'A', '--main-hashes', str(main))
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertIn('BASELINE', first.stdout)
        self.assertTrue((self.root / 'media/fat/fes/baseline.json').is_file())
        (self.root / PATHS['mister-agent']).write_bytes(b'hand-made')
        second = self.fes('watch', '--once', '--kits', 'A', '--main-hashes', str(main))
        self.assertEqual(second.returncode, 1, second.stdout)
        self.assertIn('DRIFT mister-agent unrecorded', second.stdout)

    def test_core_package_dot_segments_cannot_escape(self):
        self.claim()
        package = 'cd' * 32
        outside = self.root / 'etc/passwd'
        outside.parent.mkdir(parents=True)
        outside.write_bytes(b'secret')
        payload = self.work / 'evil'
        payload.write_bytes(b'pwned')
        escape = self.core_path('..', package) + '/../../../../etc/passwd'
        refused = self.fes('swap', 'A', escape, str(payload))
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn('not tracked', refused.stderr)
        self.assertEqual(outside.read_bytes(), b'secret')
        dotted = self.core_path('./core.rbf', package)
        refused = self.fes('swap', 'A', dotted, str(payload))
        self.assertNotEqual(refused.returncode, 0)
        self.assertIn('not tracked', refused.stderr)
        self.assertFalse((self.root / self.core_path('core.rbf', package).lstrip('/')).exists())
        body = b'x'
        source = self.root / 'tmp/fes-incoming/payload'
        source.parent.mkdir(parents=True, exist_ok=True)
        source.write_bytes(body)
        dest = self.core_path('./core.rbf', package)
        plan = self.root / 'tmp/fes-incoming/plan'
        digest_bytes = digest(body)
        marker = self.root / 'run/fogcast/kit-maintenance.json'
        marker.parent.mkdir(parents=True, exist_ok=True)
        marker.write_text(json.dumps({'state': 'ready', 'operation_id': 'a' * 64,
                                     'generation': 'b' * 64, 'boot_id': BOOT}))
        manifest_sha = digest((self.root / 'media/fat/fes/kit-manifest.json').read_bytes())
        plan.write_text(
            f'MANIFEST_EXPECT {manifest_sha}\n'
            f'MAINTENANCE {"a" * 64} {"b" * 64} {BOOT}\n'
            f'BOOT {BOOT}\n'
            f'PLACE path-test {dest} /tmp/fes-incoming/payload tmpfs 0755 '
            f'/media/fat/fes/stock/{GOOD}/path-test /tmp/fes-swap/path-test '
            f'{digest_bytes} {digest_bytes}\n'
            'COMMIT\n')
        env = os.environ.copy()
        env.update({'KIT_FAKE': '1', 'KIT_ROOT': str(self.root)})
        remote = subprocess.run(['sh', str(REMOTE), 'apply', '/tmp/fes-incoming/plan'],
                                env=env, text=True, capture_output=True, check=False)
        self.assertNotEqual(remote.returncode, 0)
        self.assertIn('bad path', remote.stderr)
        self.assertEqual(outside.read_bytes(), b'secret')


if __name__ == '__main__':
    unittest.main()
