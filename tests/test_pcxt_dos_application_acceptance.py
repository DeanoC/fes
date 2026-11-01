"""Offline safety and evidence tests for the owned-session PC/XT application diagnostic."""
import fcntl
import hashlib
import json
import os
import shutil
import signal
import subprocess
import time
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from scripts import pcxt_dos_application_acceptance as app
from scripts import pcxt_dos_hil_acceptance as visual
from tests.test_pcxt_dos_keyboard_acceptance import frame


class COMVisualTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.font = visual.load_font()
        cls.before = frame(cls.font, {2: 'A:\\>'})
        cls.anchor = visual.find_prompt(*cls.before, cls.font)
        cls.ready = frame(cls.font, {2: 'A:\\>fescom', 3: app.READY})
        cls.after = frame(cls.font, {2: 'A:\\>fescom', 3: app.READY,
                                     4: app.PASS, 5: 'A:\\>'})

    def test_ready_and_pass_require_fresh_ordered_output_and_prompt(self):
        app.ensure_fresh_before(self.before, self.font, self.anchor)
        self.assertEqual(app.ready_check(self.ready, self.font, self.anchor, self.before[:2])['ready_row'], 3)
        self.assertEqual(app.pass_check(self.after, self.font, self.anchor, self.before[:2])['prompt_row'], 5)
        for stale in (self.ready, self.after):
            with self.assertRaises(app.pa.AcceptanceError):
                app.ensure_fresh_before(stale, self.font, self.anchor)
        for invalid in (self.before, self.ready,
                        frame(self.font, {2: 'A:\\>', 3: app.PASS}),
                        frame(self.font, {2: 'A:\\>', 3: app.READY, 4: app.FAIL, 5: 'A:\\>'})):
            with self.assertRaises((ValueError, app.pa.AcceptanceError)):
                app.pass_check(invalid, self.font, self.anchor, self.before[:2])
        with self.assertRaises(app.pa.AcceptanceError):
            app.ready_check(self.after, self.font, self.anchor, self.before[:2])
        with self.assertRaises(ValueError):
            app.ready_check(self.ready, self.font, self.anchor, (1, 1))


class AdmissionTest(unittest.TestCase):
    def test_claim_requires_latest_exact_owner_and_held_lock(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            status = root/'kitA-STATUS.md'
            queue = root/'queue.lock'
            queue.touch()
            locks = {'A': queue}
            status.write_text('## KIT A IN USE: owner-other — task target_id=target-1\n')
            with queue.open('rb') as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                with self.assertRaisesRegex(app.pa.AcceptanceError, 'owner claim'):
                    app.check_claim('A', 'owner', 'target-1', lock.fileno(), root, locks)
                status.write_text('## KIT A IN USE: owner — task target_id=target-2\n')
                with self.assertRaisesRegex(app.pa.AcceptanceError, 'target ID'):
                    app.check_claim('A', 'owner', 'target-1', lock.fileno(), root, locks)
                status.write_text('## KIT A IN USE: owner — task target_id=target-1\n')
                self.assertEqual(app.check_claim('A', 'owner', 'target-1', lock.fileno(), root, locks)['owner'], 'owner')
                status.write_text('## KIT A IN USE: owner — task target_id=target-1\n## KIT A FREE — done\n')
                with self.assertRaisesRegex(app.pa.AcceptanceError, 'owner claim'):
                    app.check_claim('A', 'owner', 'target-1', lock.fileno(), root, locks)
            status.write_text('## KIT A IN USE: owner — task target_id=target-1\n')
            with queue.open('rb') as unlocked, self.assertRaisesRegex(app.pa.AcceptanceError, 'exclusive flock'):
                app.check_claim('A', 'owner', 'target-1', unlocked.fileno(), root, locks)

    def test_inherited_lock_descriptor_succeeds_but_foreign_lock_does_not(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            queue = root/'queue.lock'; queue.touch()
            (root/'kitA-STATUS.md').write_text('KIT A IN USE: owner target_id=target-1\n')
            probe = """import sys
from pathlib import Path
from scripts.pcxt_dos_application_acceptance import check_claim
check_claim('A', 'owner', 'target-1', int(sys.argv[1]), Path(sys.argv[2]), {'A': Path(sys.argv[3])})
"""
            with queue.open('rb') as held, queue.open('rb') as foreign:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
                inherited = subprocess.run([os.sys.executable, '-c', probe, str(held.fileno()), str(root), str(queue)],
                                           pass_fds=(held.fileno(),), capture_output=True, text=True)
                self.assertEqual(inherited.returncode, 0, inherited.stderr)
                adversary = subprocess.run([os.sys.executable, '-c', probe, str(foreign.fileno()), str(root), str(queue)],
                                           pass_fds=(foreign.fileno(),), capture_output=True, text=True)
                self.assertNotEqual(adversary.returncode, 0)
                self.assertIn('does not hold an exclusive flock', adversary.stderr)
                other = root/'other.lock'; other.touch()
                with other.open('rb') as wrong:
                    fcntl.flock(wrong, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    with self.assertRaisesRegex(app.pa.AcceptanceError, 'different file'):
                        app.check_claim('A', 'owner', 'target-1', wrong.fileno(), root, {'A': queue})

    def test_provenance_pins_independent_receipts_and_host_binary(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            binary = root/'fogcast-api'; binary.write_bytes(b'host executable')
            digest = hashlib.sha256(binary.read_bytes()).hexdigest()
            host = root/'host.json'; host.write_text(json.dumps({'fes_revision': 'a'*40, 'files': {'fogcast-api': digest}}))
            software = root/'software.json'; software.write_text(json.dumps({'source_commit': 'b'*40,
                'bundle': {'id': 'c'*64}, 'base_image_sha256': 'd'*64}))
            args = type('Args', (), {'host_receipt': host, 'software_receipt': software, 'host_binary': binary,
                'host_receipt_sha256': hashlib.sha256(host.read_bytes()).hexdigest(),
                'software_receipt_sha256': hashlib.sha256(software.read_bytes()).hexdigest(),
                'bundle_id': 'c'*64, 'image_sha256': 'd'*64, 'source_commit': 'e'*40})()
            provenance, host_raw, software_raw = app.source_provenance(args, visual.load_font())
            self.assertEqual(provenance['package_source_commit'], 'e'*40)
            self.assertEqual(provenance['host']['source_commit'], 'a'*40)
            self.assertEqual(provenance['software']['source_commit'], 'b'*40)
            self.assertEqual(hashlib.sha256(host_raw).hexdigest(), args.host_receipt_sha256)
            self.assertEqual(hashlib.sha256(software_raw).hexdigest(), args.software_receipt_sha256)
            self.assertEqual(provenance['extracted_font_sha256'], hashlib.sha256(visual.load_font()).hexdigest())
            self.assertIn('scripts/pcxt_dos_application_acceptance.py', provenance['diagnostic_file_sha256'])
            binary.write_bytes(b'imposter')
            with self.assertRaisesRegex(app.pa.AcceptanceError, 'host binary'):
                app.source_provenance(args, visual.load_font())
            binary.write_bytes(b'host executable')
            args.bundle_id = '0'*64
            with self.assertRaisesRegex(app.pa.AcceptanceError, 'software receipt'):
                app.source_provenance(args, visual.load_font())
            args.software_receipt_sha256 = '0'*64
            with self.assertRaisesRegex(app.pa.AcceptanceError, 'receipt identity'):
                app.source_provenance(args, visual.load_font())

    def test_config_and_live_host_must_select_only_owned_target(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'host.toml'
            config = ('selected_target = "test"\n[[targets]]\nname = "test"\n'
                      'target_id = "target-1"\nenabled = true\n[remote_input]\nenabled = true\n')
            path.write_text(config); path.chmod(0o600)
            self.assertTrue(app.check_private_config(path, 'target-1', 'test')['remote_input_enabled'])
            link = Path(temp)/'host-link'; link.symlink_to(path)
            with self.assertRaises(app.pa.AcceptanceError):
                app.check_private_config(link, 'target-1', 'test')
            path.chmod(0o644)
            with self.assertRaises(app.pa.AcceptanceError):
                app.check_private_config(path, 'target-1', 'test')
            path.chmod(0o600)
            path.write_text(config.replace('enabled = true\n[remote_input]',
                                           'enabled = true\n[[targets]]\nname = "other"\n'
                                           'target_id = "target-2"\nenabled = true\n[remote_input]'))
            with self.assertRaises(app.pa.AcceptanceError):
                app.check_private_config(path, 'target-1', 'test')
        settings = {'selected_target': 'test', 'targets': [
            {'name': 'test', 'target_id': 'target-1', 'enabled': True}]}
        app.check_live_settings(settings, 'target-1', 'test')
        for bad in ({}, {**settings, 'selected_target': 'other'},
                    {**settings, 'targets': settings['targets'] + [
                        {'name': 'other', 'target_id': 'target-2', 'enabled': True}]},
                    {**settings, 'targets': [{'name': 'test', 'target_id': 'other', 'enabled': True}]}):
            with self.subTest(bad=bad), self.assertRaises(app.pa.AcceptanceError):
                app.check_live_settings(bad, 'target-1', 'test')

    def test_disk_digest_and_extracted_program_are_checked(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'disk.img'
            data = bytes(app.FLOPPY_SIZE); path.write_bytes(data)
            program = b'executable'
            spec = {'floppy': hashlib.sha256(data).hexdigest(),
                    'program': hashlib.sha256(program).hexdigest(), 'name': 'TEST.COM', 'size': len(program)}
            with mock.patch.dict(app.SCENARIOS, {'test': spec}), \
                 mock.patch.object(app.subprocess, 'run') as run:
                run.return_value.stdout = program
                self.assertEqual(app.verify_disk(path, 'test')['executable'], 'TEST.COM')
                run.return_value.stdout = b'changed'
                with self.assertRaisesRegex(app.pa.AcceptanceError, 'executable identity'):
                    app.verify_disk(path, 'test')
                path.write_bytes(data[:-1]+b'X')
                with self.assertRaisesRegex(app.pa.AcceptanceError, 'image identity'):
                    app.verify_disk(path, 'test')


class FakeAPI:
    def __init__(self, session):
        self.timeout = 20
        self.session = session
        self.calls = []
        self.tap_result = {'ok': True, 'host_duration_ms': 60.1}
        self.input_session = 'input-1'
        self.settings = {'selected_target': 'test', 'targets': [
            {'name': 'test', 'target_id': 'target-1', 'enabled': True}]}
        self.health = {'target': {'reachable': True, 'api_version': 'v1',
            'connection': {'target_id': 'target-1', 'state': 'active', 'boot_id': 'boot-1'},
            'software': {'active_bundle': 'a'*64, 'selected': 'a'*64, 'base_identity': 'b'*64}}}
    def get(self, path):
        self.calls.append(('GET', path))
        if path == '/api/v1/library/settings': return self.settings
        if path == '/api/v1/health': return self.health
        if path == '/api/v1/session/input': return {'state': 'attached', 'ready': True, 'session_id': self.input_session}
        raise AssertionError(path)
    def post_json(self, path, value):
        self.calls.append(('POST', path, value))
        if isinstance(self.tap_result, BaseException): raise self.tap_result
        return self.tap_result


class FakeRunner:
    def __init__(self):
        self.package_id = 'c'*64
        self.value = {'id': 'session-1', 'target': 'test', 'target_id': 'target-1',
                      'flight_id': 'flight-1', 'game_id': 'game-1', 'state': 'active',
                      'execution': 'fpga_native', 'core_package': {
                          'core_id': 'fes.pcxt-dos', 'package_id': self.package_id, 'generation': 1,
                          'abi': {'id': 'fes.computer', 'major': 1, 'minor': 1},
                          'active_interfaces': [{'id': 'fes.keyboard.hid', 'major': 1, 'minor': 0}],
                          'media_units': [{'unit': 0, 'interface': {'id': 'fes.media.pcxt-floppy', 'major': 1, 'minor': 0},
                                           'min_bytes': app.FLOPPY_SIZE, 'max_bytes': app.FLOPPY_SIZE,
                                           'chunk_bytes': 512, 'state': 'ready'}]}}
        self.api = FakeAPI(self.value)
        self.entry = {'game_id': 'game-1', 'core_id': 'fes.pcxt-dos', 'package_id': self.package_id,
                      'media_role': 'disk', 'media_id': app.SCENARIOS['com']['floppy']}
    def session(self, target=None): return self.value
    def session_identity(self, value, context): return app.pa._session_identity(value, context)
    def read_entry(self, game_id): return self.entry
    def validate_entry(self, value, game_id, context):
        if (value.get('game_id') != game_id or value.get('package_id') != self.package_id
                or value.get('core_id') != 'fes.pcxt-dos'):
            raise app.pa.AcceptanceError('wrong entry')
        return value


class InputSafetyTest(unittest.TestCase):
    def setUp(self):
        self.runner = FakeRunner()
        self.owned = self.runner.session_identity(self.runner.value, 'fixture')
        self.args = type('Args', (), {'kit': 'A', 'owner': 'owner', 'config': Path('/private'),
                                     'target_id': 'target-1', 'queue_lock_fd': 42, 'bundle_id': 'a'*64,
                                     'image_sha256': 'b'*64, 'expected_boot': 'boot-1'})()
        self.disk_sha = app.SCENARIOS['com']['floppy']
        self.claim = mock.patch.object(app, 'check_claim', return_value={'owner': 'owner'})
        self.config = mock.patch.object(app, 'check_private_config', return_value={})
        self.claim.start(); self.config.start()
        self.addCleanup(self.claim.stop); self.addCleanup(self.config.stop)

    def test_active_target_media_and_generation_fail_closed(self):
        app.validate_active(self.runner, self.owned, self.args, self.disk_sha)
        self.runner.value['core_package']['generation'] = 2
        with self.assertRaisesRegex(app.pa.AcceptanceError, 'identity'):
            app.validate_active(self.runner, self.owned, self.args, self.disk_sha)
        self.runner.value['core_package']['generation'] = 1
        self.runner.entry['media_id'] = '0'*64
        with self.assertRaisesRegex(app.pa.AcceptanceError, 'expected floppy'):
            app.validate_active(self.runner, self.owned, self.args, self.disk_sha)
        self.runner.entry['media_id'] = self.disk_sha
        self.runner.api.health['target']['connection']['boot_id'] = 'other-boot'
        with self.assertRaisesRegex(app.pa.AcceptanceError, 'boot'):
            app.validate_active(self.runner, self.owned, self.args, self.disk_sha)
        self.assertFalse(any(call[0] == 'POST' for call in self.runner.api.calls))

    def test_single_tap_never_retries_uncertain_response(self):
        self.runner.api.tap_result = app.pa.AcceptanceError('lost response')
        with mock.patch.object(app.time, 'sleep'), self.assertRaisesRegex(app.pa.AcceptanceError, 'lost response'):
            app.type_keys(self.runner, self.owned, self.args, self.disk_sha, 'input-1', [], (0x0e,))
        posts = [call for call in self.runner.api.calls if call[0] == 'POST']
        self.assertEqual(len(posts), 1)
        self.assertEqual(posts[0][1], '/api/v1/session/input/tap')
        self.assertEqual(posts[0][2]['session_id'], 'input-1')

    def test_successful_tap_is_one_ordered_host_operation(self):
        events = []
        with mock.patch.object(app.time, 'sleep'):
            app.type_keys(self.runner, self.owned, self.args, self.disk_sha, 'input-1', events, (0x0e,))
        posts = [call for call in self.runner.api.calls if call[0] == 'POST']
        self.assertEqual(len(posts), 1)
        self.assertEqual(posts[0][2]['event']['Code'], 0x100e)
        self.assertTrue(any(e['operation'] == 'host tap request' and e['ok'] for e in events))

    def test_foreign_input_attachment_prevents_tap(self):
        self.runner.api.input_session = 'another-input'
        with self.assertRaisesRegex(app.pa.AcceptanceError, 'attachment changed'):
            app.type_keys(self.runner, self.owned, self.args, self.disk_sha, 'input-1', [], (0x0e,))
        self.assertFalse(any(call[0] == 'POST' for call in self.runner.api.calls))

    def test_invalid_tap_response_is_not_retried(self):
        for bad in ({}, {'ok': True, 'host_duration_ms': float('nan')},
                    {'ok': True, 'host_duration_ms': 401}, {'ok': False, 'host_duration_ms': 1}):
            self.runner.api.calls.clear()
            self.runner.api.tap_result = bad
            with self.subTest(bad=bad), self.assertRaisesRegex(app.pa.AcceptanceError, 'tap receipt'):
                app.type_keys(self.runner, self.owned, self.args, self.disk_sha, 'input-1', [], (0x0e,))
            self.assertEqual(len([c for c in self.runner.api.calls if c[0] == 'POST']), 1)

    def test_changed_live_target_prevents_tap(self):
        self.runner.api.settings['targets'].append({'name': 'other', 'target_id': 'target-2', 'enabled': True})
        with self.assertRaisesRegex(app.pa.AcceptanceError, 'live host'):
            app.type_keys(self.runner, self.owned, self.args, self.disk_sha, 'input-1', [], (0x0e,))
        self.assertFalse(any(call[0] == 'POST' for call in self.runner.api.calls))

    def test_claim_loss_after_tap_prevents_following_input(self):
        events = []
        def tapped(path, value):
            self.runner.api.calls.append(('POST', path, value))
            app.check_claim.side_effect = app.pa.AcceptanceError('claim lost')
            return {'ok': True, 'host_duration_ms': 60.0}
        self.runner.api.post_json = tapped
        with mock.patch.object(app.time, 'sleep'), self.assertRaisesRegex(app.pa.AcceptanceError, 'claim lost'):
            app.type_keys(self.runner, self.owned, self.args, self.disk_sha, 'input-1', events, (0x04, 0x05))
        self.assertEqual(len([c for c in self.runner.api.calls if c[0] == 'POST']), 1)


class MainOwnershipTest(unittest.TestCase):
    def test_owned_session_success_and_failure_never_stop_or_release(self):
        for mode in ('ok', 'capture', 'interrupt'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                archive = root/'core.fcore'; archive.write_bytes(b'archive')
                session_path = root/'owned.json'
                fake = FakeRunner()
                fake.value['input'] = {'state': 'attached', 'ready': True, 'session_id': 'input-1'}
                session_path.write_text(json.dumps(fake.value))
                evidence = root/'evidence'
                digest = hashlib.sha256(archive.read_bytes()).hexdigest()
                args = ['com', '--execute', '--kit', 'A', '--owner', 'owner',
                        '--queue-lock-fd', '42', '--host-receipt', str(root/'host.json'),
                        '--host-receipt-sha256', '1'*64, '--host-binary', str(root/'fogcast-api'),
                        '--software-receipt', str(root/'software.json'), '--software-receipt-sha256', '2'*64,
                        '--host-api', 'http://127.0.0.1:1234', '--config', str(root/'host.toml'),
                        '--owned-session', str(session_path), '--archive', str(archive),
                        '--archive-sha256', digest, '--rbf-sha256', 'd'*64,
                        '--package-id', fake.package_id, '--source-commit', 'e'*40,
                        '--bundle-id', 'a'*64, '--image-sha256', 'b'*64,
                        '--target-id', 'target-1', '--expected-boot', 'boot-1',
                        '--disk', str(root/'disk.img'), '--capture-device', '/dev/v4l/by-id/fes-shadowcast-hil',
                        '--evidence', str(evidence)]
                manifest = {'build': {'id': 'f'*32}, 'abi': {'id': 'fes.computer', 'major': 1, 'minor': 1},
                            'interfaces': [{'id': 'fes.keyboard.hid', 'major': 1, 'minor': 0, 'required': True}]}
                def run_com(_runner, _owned, _args, rec, _input, _font):
                    if mode == 'capture': raise app.pa.AcceptanceError('injected capture error')
                    rec['classification'] = 'owned-session COM execution diagnostic passed'
                disk = {'floppy_sha256': app.SCENARIOS['com']['floppy'], 'executable_sha256': app.COM_SHA}
                verification_count = 0
                def verify(*_args):
                    nonlocal verification_count
                    verification_count += 1
                    if mode == 'interrupt' and verification_count == 2:
                        os.kill(os.getpid(), signal.SIGTERM)
                    return disk
                old_term = signal.getsignal(signal.SIGTERM)
                old_int = signal.getsignal(signal.SIGINT)
                app.install_signal_handler()
                self.addCleanup(signal.signal, signal.SIGTERM, old_term)
                self.addCleanup(signal.signal, signal.SIGINT, old_int)
                self.addCleanup(setattr, app, '_INTERRUPTED', 0)
                with mock.patch.object(app, 'check_claim', return_value={'owner': 'owner'}), \
                     mock.patch.object(app, 'check_private_config', return_value={}), \
                     mock.patch.object(app, 'inspect_archive', return_value=manifest), \
                     mock.patch.object(app, 'canonical_package_id', return_value=fake.package_id), \
                     mock.patch.object(app, 'verify_disk', side_effect=verify), \
                     mock.patch.object(app.visual, 'load_font', return_value=b'font'), \
                     mock.patch.object(app, 'source_provenance', return_value=({}, b'host', b'software')), \
                     mock.patch.object(app.shutil, 'disk_usage') as space, \
                     mock.patch.object(app.pa, 'Runner', return_value=fake), \
                     mock.patch.object(app, 'run_com', side_effect=run_com):
                    space.return_value = type('Disk', (), {'free': 31*1024**3})()
                    if mode != 'ok':
                        with self.assertRaisesRegex(app.pa.AcceptanceError, 'injected|interrupted'):
                            app.main(args)
                    else:
                        app.main(args)
                self.assertEqual((evidence/'receipt.json').exists(), mode == 'ok')
                self.assertEqual((evidence/'failure.json').exists(), mode != 'ok')
                self.assertFalse(any(call[0] == 'POST' for call in fake.api.calls))
                app._INTERRUPTED = 0
                with self.assertRaises(FileExistsError):
                    with mock.patch.object(app, 'check_claim', return_value={}), \
                         mock.patch.object(app, 'check_private_config', return_value={}), \
                         mock.patch.object(app, 'inspect_archive', return_value=manifest), \
                         mock.patch.object(app, 'canonical_package_id', return_value=fake.package_id), \
                         mock.patch.object(app, 'verify_disk', return_value=disk), \
                         mock.patch.object(app.visual, 'load_font', return_value=b'font'), \
                     mock.patch.object(app, 'source_provenance', return_value=({}, b'host', b'software')), \
                         mock.patch.object(app.shutil, 'disk_usage') as space, \
                         mock.patch.object(app.pa, 'Runner', return_value=fake):
                        space.return_value = type('Disk', (), {'free': 31*1024**3})()
                        app.main(args)


class VideoEvidenceTest(unittest.TestCase):
    def test_coverage_rejects_truncated_or_missing_final_progress(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'progress.jsonl'
            rows = [
                {'monotonic_ns': 1_000_000_000, 'line': 'frame=90'},
                {'monotonic_ns': 1_000_000_001, 'line': 'out_time_us=3000000'},
                {'monotonic_ns': 1_000_000_002, 'line': 'progress=continue'},
                {'monotonic_ns': 4_000_000_000, 'line': 'frame=180'},
                {'monotonic_ns': 4_000_000_001, 'line': 'out_time_us=6000000'},
                {'monotonic_ns': 4_000_000_002, 'line': 'progress=end'},
            ]
            path.write_text(''.join(json.dumps(r)+'\n' for r in rows))
            meta = {'duration_seconds': 6.0, 'nb_read_frames': '180'}
            proof = app.check_video_coverage(meta, path, 1_000_000_000, 4_000_000_000, 3_000_000_000)
            self.assertEqual(proof['last_input_monotonic_ns'], 3_000_000_000)
            with self.assertRaisesRegex(app.pa.AcceptanceError, 'cover'):
                app.check_video_coverage(meta, path, 1_000_000_000, 40_000_000_000, 39_000_000_000)
            rows[-1]['line'] = 'progress=continue'
            path.write_text(''.join(json.dumps(r)+'\n' for r in rows))
            with self.assertRaisesRegex(app.pa.AcceptanceError, 'cover'):
                app.check_video_coverage(meta, path, 1_000_000_000, 4_000_000_000, 3_000_000_000)

    def test_game_capture_never_claims_gameplay_without_review(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            class Recorder:
                def __init__(self, _device, evidence):
                    self.path = evidence/'game-full.mkv'; self.path.write_bytes(b'video')
                    self.progress_path = evidence/'game-progress.jsonl'; self.progress_path.write_text('')
                    self.started_ns = 1; self.first_progress_ns = 2
                    self.proc = type('Proc', (), {'poll': lambda _self: None})()
                def stop(self): pass
            args = type('Args', (), {'capture_device': 'synthetic', 'evidence': root})()
            runner = FakeRunner(); owned = runner.session_identity(runner.value, 'fixture')
            rec = {'captures': [], 'hid_events': []}
            with mock.patch.object(app, 'capture_owned', return_value=((1, 1, b'abc'), {'file': 'before.png'})), \
                 mock.patch.object(app, 'prompt', return_value={}), \
                 mock.patch.object(app, 'VideoCapture', Recorder), \
                 mock.patch.object(app, 'type_keys') as keys, \
                 mock.patch.object(app, 'wait_owned'), \
                 mock.patch.object(app, 'check_video', return_value={}), \
                 mock.patch.object(app, 'check_video_coverage', return_value={}):
                app.run_game(runner, owned, args, rec, 'input-1', b'font')
            self.assertEqual(rec['classification'], 'game capture complete; gameplay and movement review pending')
            self.assertIn('independent visual review', rec['game_video']['classification'])
            self.assertEqual(keys.call_count, 6)


@unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'ffmpeg and ffprobe required')
class VideoProcessTest(unittest.TestCase):
    def test_real_synthetic_recorder_covers_input_and_cleans_up(self):
        original = subprocess.Popen
        def synthetic(argv, **kwargs):
            i = argv.index('-f'); j = argv.index('-i', i)
            return original(argv[:i] + ['-re', '-f', 'lavfi', '-i',
                            'color=black:size=1920x1080:rate=30'] + argv[j+2:], **kwargs)
        with tempfile.TemporaryDirectory() as temp:
            with mock.patch.object(app.subprocess, 'Popen', side_effect=synthetic):
                video = app.VideoCapture('synthetic', Path(temp))
                try:
                    time.sleep(3.5)
                    last_input = time.monotonic_ns(); stop = time.monotonic_ns()
                finally:
                    video.stop()
            self.assertIsNotNone(video.proc.poll())
            info = app.check_video(video.path)
            self.assertEqual(info['codec_name'], 'ffv1')
            proof = app.check_video_coverage(info, video.progress_path, video.first_progress_ns, stop, last_input)
            self.assertEqual(proof['last_input_monotonic_ns'], last_input)

    def test_failed_recorder_start_reaps_its_child(self):
        original = subprocess.Popen
        spawned = []
        def synthetic(argv, **kwargs):
            i = argv.index('-f'); j = argv.index('-i', i)
            proc = original(argv[:i] + ['-re', '-f', 'lavfi', '-i',
                            'color=black:size=1920x1080:rate=30'] + argv[j+2:], **kwargs)
            spawned.append(proc)
            return proc
        with tempfile.TemporaryDirectory() as temp, \
             mock.patch.object(app.subprocess, 'Popen', side_effect=synthetic):
            with self.assertRaisesRegex(app.pa.AcceptanceError, 'did not start'):
                app.VideoCapture('synthetic', Path(temp), startup_timeout=0)
        self.assertEqual(len(spawned), 1)
        self.assertIsNotNone(spawned[0].poll())


if __name__ == '__main__':
    unittest.main()
