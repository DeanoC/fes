"""The three 14 November 2026 harness failures, as tests.

Each case is one of the incidents in issue #1849:

* a command file with no final `{"op": "quit"}` must not look like a dead
  wrapper (the #1368 incident);
* a post-run tail must refuse to remove anything while a core is loaded (the
  same incident's second half, which blocked Kit B's lease);
* a command the kit refused must fail the run (the i386 x87 slice 2 run, whose
  `.fcore` was refused at admission while the driver reported `rc=0`).

The fourth case is the liveness check that misled the first: `pgrep -f` matches
the asking shell's own command line.
"""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def load(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).parents[1] / f'scripts/{name}.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


kit_hil = load('kit_hil')
kit = kit_hil.kitlib  # the same module object kit_hil imports, so KitError matches


class Clock:
    """A clock the tests advance themselves, so no test waits in real time."""

    def __init__(self):
        self.t = 0.0

    def now(self):
        return self.t

    def sleep(self, seconds):
        self.t += max(seconds, 0.05)


class FakeClient:
    """The agent's `/v1/status` and `/v1/kit/lease` replies, by the turn."""

    def __init__(self, replies, error=None):
        self.replies, self.error, self.calls = list(replies), error, []

    def request(self, path, **_):
        self.calls.append(path)
        if self.error is not None:
            raise self.error
        turn = self.replies[0] if len(self.replies) == 1 else self.replies.pop(0)
        return dict(turn['status' if path == '/v1/status' else 'lease'])


IDLE = {'status': {'state': 'idle', 'game_id': None}, 'lease': {'state': 'free'}}
LOADED = {'status': {'state': 'active', 'game_id': 'u6',
                     'core_package': {'package_id': 'a6977555', 'core_id': 'fes.pc386'}},
          'lease': {'state': 'held', 'owner': 'claude-pc386-games', 'purpose': 'run k1'}}


class EndOfRun(unittest.TestCase):
    """Guard 1: a missing final quit must not read as a dead wrapper."""

    def setUp(self):
        self.said, self.ran = [], []

    def execute(self, op):
        self.ran.append(op)
        return {}

    def test_batch_run_without_a_quit_op_ends_itself(self):
        # The #1368 command file: every command, no quit. The driver sat in a
        # silent 30-minute wait, which read as a dead wrapper.
        clock = Clock()
        text = '\n'.join(json.dumps({'op': 'grab', 'name': f'f{i}'}) for i in range(3))
        done, reasons = kit_hil.drain(lambda: text, self.execute, mode='batch',
                                      log=self.said.append, now=clock.now, sleep=clock.sleep)
        self.assertEqual(reasons, [])
        self.assertEqual(len(self.ran), 3)
        self.assertEqual(done, 3)
        self.assertLess(clock.t, 1, 'a complete command file must not wait at all')
        self.assertIn('exhausted', ' '.join(self.said))
        self.assertIn('quit', ' '.join(self.said))

    def test_batch_runs_a_last_line_with_no_newline(self):
        clock = Clock()
        text = json.dumps({'op': 'grab', 'name': 'f0'})
        kit_hil.drain(lambda: text, self.execute, mode='batch',
                      log=self.said.append, now=clock.now, sleep=clock.sleep)
        self.assertEqual(len(self.ran), 1)

    def test_live_wait_is_bounded_and_says_what_it_waits_for(self):
        clock = Clock()
        text = json.dumps({'op': 'grab', 'name': 'f0'}) + '\n'
        done, reasons = kit_hil.drain(lambda: text, self.execute, mode='live', idle_limit=120,
                                      heartbeat=30, log=self.said.append,
                                      describe=lambda: {'lease': 'held', 'core': 'fes.pc386'},
                                      now=clock.now, sleep=clock.sleep)
        self.assertEqual(len(self.ran), 1)
        self.assertEqual(len(reasons), 1, 'an unterminated live run must report why it ended')
        self.assertIn('quit', reasons[0])
        waiting = [line for line in self.said if line.startswith('WAITING')]
        self.assertGreaterEqual(len(waiting), 3, 'the wait must speak about every heartbeat')
        self.assertIn('command 2', waiting[0])
        self.assertIn('quit', waiting[0])
        self.assertIn('alive', waiting[0])
        self.assertIn('fes.pc386', waiting[0], 'say what is loaded while it waits')
        self.assertLess(clock.t, 240, 'the live wait must be bounded')

    def test_live_quit_appended_to_a_running_file_ends_the_run(self):
        clock, text = Clock(), [json.dumps({'op': 'grab', 'name': 'f0'}) + '\n']

        def source():
            if clock.t > 5 and len(text) == 1:
                text.append(json.dumps({'op': 'quit'}) + '\n')
            return ''.join(text)

        done, reasons = kit_hil.drain(source, self.execute, mode='live', idle_limit=600,
                                      log=self.said.append, now=clock.now, sleep=clock.sleep)
        self.assertEqual(reasons, [])
        self.assertEqual(len(self.ran), 1)

    def test_live_does_not_run_a_half_written_command(self):
        clock, text = Clock(), ['{"op": "gr']

        def source():
            if clock.t > 2 and len(text) == 1:
                text[0] = json.dumps({'op': 'grab', 'name': 'f0'}) + '\n'
            return ''.join(text)

        kit_hil.drain(source, self.execute, mode='live', idle_limit=60,
                      log=self.said.append, now=clock.now, sleep=clock.sleep)
        self.assertEqual(self.ran, [{'op': 'grab', 'name': 'f0'}])


class CleanupGate(unittest.TestCase):
    """Guard 2: the post-run tail must refuse while a core is loaded."""

    def test_a_loaded_core_refuses_and_is_named(self):
        clock = Clock()
        client = FakeClient([LOADED])
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.cleanup_gate(client, wait=10, now=clock.now, sleep=clock.sleep)
        message = str(refused.exception)
        self.assertIn('fes.pc386', message)
        self.assertIn('a6977555', message)
        self.assertIn('claude-pc386-games', message, 'name who holds the lease')

    def test_idle_with_a_free_lease_passes(self):
        clock = Clock()
        self.assertEqual(kit_hil.cleanup_gate(FakeClient([IDLE]), wait=10,
                                              now=clock.now, sleep=clock.sleep),
                         {'state': 'idle', 'lease': 'free'})

    def test_a_revoking_lease_is_waited_out(self):
        clock = Clock()
        revoking = {'status': {'state': 'idle', 'game_id': None}, 'lease': {'state': 'revoking'}}
        client = FakeClient([revoking, revoking, IDLE])
        self.assertEqual(kit_hil.cleanup_gate(client, wait=30, now=clock.now, sleep=clock.sleep),
                         {'state': 'idle', 'lease': 'free'})

    def test_a_lease_held_by_someone_else_refuses(self):
        clock = Clock()
        held = {'status': {'state': 'idle', 'game_id': None},
                'lease': {'state': 'held', 'owner': 'lander', 'purpose': 'queue run'}}
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.cleanup_gate(FakeClient([held]), wait=5, now=clock.now, sleep=clock.sleep)
        self.assertIn('lander', str(refused.exception))

    def test_a_blocked_lease_refuses(self):
        clock = Clock()
        blocked = {'status': {'state': 'idle', 'game_id': None},
                   'lease': {'state': 'blocked',
                             'reason': 'kit cleanup failed or agent shutting down; '
                                       'operator recovery required'}}
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.cleanup_gate(FakeClient([blocked]), wait=5, now=clock.now, sleep=clock.sleep)
        self.assertIn('operator recovery', str(refused.exception))

    def test_a_pending_recovery_refuses(self):
        clock = Clock()
        recovering = {'status': {'state': 'idle', 'game_id': None,
                                 'recovery': 'reboot_required'},
                      'lease': {'state': 'free'}}
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.cleanup_gate(FakeClient([recovering]), wait=5,
                                 now=clock.now, sleep=clock.sleep)
        self.assertIn('reboot_required', str(refused.exception))

    def test_an_idle_agent_after_a_failed_command_still_passes(self):
        # A run whose load was refused leaves last_error set. The gate asks
        # whether a removal is safe, not whether the run passed: that is the
        # verdict's question.
        clock = Clock()
        after = {'status': {'state': 'idle', 'game_id': None,
                            'last_error': {'code': 'INVALID_ARCHIVE'}},
                 'lease': {'state': 'free'}}
        self.assertEqual(kit_hil.cleanup_gate(FakeClient([after]), wait=5,
                                              now=clock.now, sleep=clock.sleep),
                         {'state': 'idle', 'lease': 'free'})

    def test_a_named_target_is_reached_without_rewriting_the_host_config(self):
        with tempfile.TemporaryDirectory() as home:
            config = Path(home) / 'config.toml'
            config.write_text("selected_target = 'dev'\n\n"
                              "[[targets]]\nname = 'dev'\nenabled = true\n"
                              "address = 'http://10.0.0.1:8789'\nagent = 'bearer-a'\n\n"
                              "[[targets]]\nname = 'kit2'\nenabled = true\n"
                              "address = 'http://10.0.0.2:8789'\nagent = 'bearer-b'\n")
            self.assertEqual(kit_hil.client_for(config).url.hostname, '10.0.0.1')
            self.assertEqual(kit_hil.client_for(config, 'kit2').url.hostname, '10.0.0.2')
            with self.assertRaises(kit.KitError):
                kit_hil.client_for(config, 'kit9')
            self.assertEqual(config.read_text().count("selected_target = 'dev'"), 1,
                             'the host config must not be rewritten to ask about a kit')

    def test_a_disabled_target_is_refused(self):
        with tempfile.TemporaryDirectory() as home:
            config = Path(home) / 'config.toml'
            config.write_text("[[targets]]\nname = 'kit2'\nenabled = false\n"
                              "address = 'http://10.0.0.2:8789'\nagent = 'b'\n")
            with self.assertRaises(kit.KitError):
                kit_hil.client_for(config, 'kit2')

    def test_an_unreachable_agent_fails_closed(self):
        clock = Clock()
        client = FakeClient([IDLE], error=kit.KitError('target request failed'))
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.cleanup_gate(client, wait=5, now=clock.now, sleep=clock.sleep)
        self.assertIn('unknown', str(refused.exception))

    def test_the_gate_reads_the_agents_own_wire_shape(self):
        """The same gate against a real HTTP agent, replying as the agent does."""

        payload = {'state': 'active', 'game_id': 'u6', 'system': None, 'expected_core': None,
                   'observed_core': None, 'last_error': None,
                   'core_package': {'package_id': 'a6977555', 'core_id': 'fes.pc386',
                                    'generation': 3, 'build_id': 'b1',
                                    'abi': {}, 'active_interfaces': [], 'gamepad': False}}

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                body = json.dumps(payload if self.path == '/v1/status'
                                  else {'state': 'held', 'owner': 'claude', 'purpose': 'run'}).encode()
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            client = kit.Client(f'http://127.0.0.1:{server.server_port}', 'private-bearer')
            clock = Clock()
            with self.assertRaises(kit_hil.CleanupRefused) as refused:
                kit_hil.cleanup_gate(client, wait=2, now=clock.now, sleep=clock.sleep)
            self.assertIn('fes.pc386', str(refused.exception))
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


class RefusedAdmission(unittest.TestCase):
    """Guard 3: a command the kit refused must fail the run."""

    SCRIPT = [{'op': 'load', 'game': 'keen4'}, {'op': 'grab', 'name': 'f0'},
              {'op': 'grab', 'name': 'f1'}, {'op': 'quit'}]

    def drain(self, execute):
        clock = Clock()
        return kit_hil.drain(lambda: '\n'.join(json.dumps(op) for op in self.SCRIPT),
                             execute, mode='batch', log=lambda *_: None,
                             now=clock.now, sleep=clock.sleep)

    def test_a_refused_load_stops_the_script(self):
        # The x87 run: `/v1/library/core/load` answered HTTP 422
        # INVALID_ARCHIVE. The ops raise — `call` turns any HTTP status over
        # 300 into a KitError (hil_games.py:193) — and the driver caught that
        # into its event and ran the rest of the script anyway.
        ran = []

        def execute(op):
            ran.append(op)
            if op['op'] == 'load':
                raise kit.KitError('/v1/library/core/load: HTTP 422 '
                                   '{"code": "INVALID_ARCHIVE"}', 422)
            return {'ok': True}

        done, reasons = self.drain(execute)
        self.assertEqual([op['op'] for op in ran], ['load'],
                         'no command may run over a core that was never loaded')
        self.assertEqual(len(reasons), 1)
        self.assertIn('INVALID_ARCHIVE', reasons[0])

    def test_an_error_returned_rather_than_raised_also_stops_the_script(self):
        ran = []

        def execute(op):
            ran.append(op)
            return {'error': 'HTTP 422 INVALID_ARCHIVE'} if op['op'] == 'load' else {'ok': True}

        done, reasons = self.drain(execute)
        self.assertEqual([op['op'] for op in ran], ['load'])
        self.assertIn('INVALID_ARCHIVE', reasons[0])

    def test_an_unknown_command_stops_the_script(self):
        # The drivers raise SystemExit for an op they do not know.
        ran = []

        def execute(op):
            ran.append(op)
            raise SystemExit(f'unknown command {op["op"]}')

        done, reasons = self.drain(execute)
        self.assertEqual(len(ran), 1)
        self.assertIn('unknown command load', reasons[0])

    def test_a_stop_answering_reboot_required_stops_the_script(self):
        # A stop that reports recovery carries no error, and the drivers break
        # their loop on it through their own `halted` key.
        ran = []

        def execute(op):
            ran.append(op)
            if op['op'] == 'load':
                return {'state': 'active'}
            return {'state': 'stopping', 'recovery': 'reboot_required'}

        done, reasons = self.drain(execute)
        self.assertEqual([op['op'] for op in ran], ['load', 'grab'])
        self.assertIn('reboot_required', reasons[0])

    def test_a_refused_load_is_the_runs_verdict(self):
        result = {'events': [{'seq': 1, 'cmd': {'op': 'load'},
                              'result': {'error': 'KitError: /v1/library/core/load: HTTP 422 '
                                                  '{"code": "INVALID_ARCHIVE"}'}},
                             {'seq': 2, 'cmd': {'op': 'grab'}, 'result': {'ok': True}}],
                  'after': {'agent': 'idle', 'lease': 'free'}}
        reasons = kit_hil.verdict(result)
        self.assertEqual(len(reasons), 1)
        self.assertIn('INVALID_ARCHIVE', reasons[0])

    def test_a_clean_run_has_no_verdict_against_it(self):
        self.assertEqual(kit_hil.verdict(
            {'events': [{'seq': 1, 'cmd': {'op': 'load'}, 'result': {'load': {'state': 'active'}}},
                        {'seq': 2, 'cmd': {'op': 'grab'}, 'result': {'ok': True}}],
             'after': {'agent': 'idle', 'lease': 'free'}}), [])

    def test_a_halt_or_a_failed_release_is_a_verdict(self):
        self.assertIn('halted', ' '.join(kit_hil.verdict(
            {'halted': 'stop reported reboot_required; no recovery attempted'})))
        self.assertIn('close_error', ' '.join(kit_hil.verdict(
            {'close_error': '/v1/kit/release: HTTP 500'})))

    def test_an_error_nested_in_a_reply_is_found(self):
        self.assertEqual(len(kit_hil.verdict(
            {'events': [{'seq': 1, 'cmd': {'op': 'audio_stop'},
                         'result': {'levels': {'error': 'no wav'}}}]})), 1)

    def test_the_cli_exits_non_zero_on_a_refused_load(self):
        with tempfile.TemporaryDirectory() as home:
            path = Path(home) / 'result.json'
            path.write_text(json.dumps({'events': [
                {'seq': 1, 'cmd': {'op': 'load'},
                 'result': {'error': 'HTTP 422 INVALID_ARCHIVE'}}]}))
            self.assertEqual(kit_hil.main(['verdict', str(path)]), 1)
            path.write_text(json.dumps({'events': [{'seq': 1, 'cmd': {'op': 'load'},
                                                    'result': {'load': {}}}]}))
            self.assertEqual(kit_hil.main(['verdict', str(path)]), 0)

    def test_drains_own_reasons_are_the_runs_verdict(self):
        # A live run that ran out of commands without a quit, or hit its run
        # limit, has no failed event at all: `drain` reports it in `reasons`,
        # which the run stores, so the verdict has to count it.
        for stored in ('no command for 300s and the command file has no final {"op": "quit"}',
                       'run time limit 5400s reached after 12 commands'):
            with self.subTest(stored=stored):
                reasons = kit_hil.verdict({'reasons': [stored],
                                           'events': [{'seq': 1, 'cmd': {'op': 'load'},
                                                       'result': {'load': {}}}]})
                self.assertEqual(len(reasons), 1)
                self.assertIn(stored[:20], reasons[0])
        self.assertEqual(kit_hil.verdict({'reasons': []}), [])
        self.assertEqual(len(kit_hil.verdict({'reasons': 'a bare string reason'})), 1)

    def test_a_result_that_is_not_an_object_is_not_a_pass(self):
        with tempfile.TemporaryDirectory() as home:
            path = Path(home) / 'result.json'
            for body in ('null', '[]', '"done"', '3'):
                with self.subTest(body=body):
                    path.write_text(body)
                    self.assertEqual(kit_hil.main(['verdict', str(path)]), 2)

    def test_a_malformed_command_line_ends_the_run_with_a_reason(self):
        for line in ('{"op": "load"', '"load"', '[]', 'null', '42'):
            with self.subTest(line=line):
                clock, ran = Clock(), []
                text = '\n'.join([json.dumps({'op': 'grab', 'name': 'f0'}), line,
                                  json.dumps({'op': 'grab', 'name': 'f1'})])
                done, reasons = kit_hil.drain(lambda: text, lambda op: ran.append(op) or {},
                                              mode='batch', log=lambda *_: None,
                                              now=clock.now, sleep=clock.sleep)
                self.assertEqual(len(ran), 1, 'the rest of the file must not run')
                self.assertEqual(len(reasons), 1)
                self.assertIn('is not a command', reasons[0])

    def test_a_missing_result_file_is_not_a_pass(self):
        with tempfile.TemporaryDirectory() as home:
            self.assertEqual(kit_hil.main(['verdict', str(Path(home) / 'absent.json')]), 2)


class FakeKit:
    """A kit's data directories, and what a removal actually did to them."""

    def __init__(self, paths, appears=None, loses=()):
        self.paths = list(paths)
        self.appears, self.loses = appears or [], list(loses)
        self.removed, self.failures = [], set()

    def find(self, roots):
        return sorted(self.paths)

    def remove(self, path):
        if path in self.failures:
            raise kit_hil.CleanupRefused(f'rm -r {path} failed; nothing further removed')
        self.removed.append(path)
        self.paths = [p for p in self.paths
                      if not (p == path or p.startswith(path + '/'))]
        self.paths.extend(self.appears)
        self.appears = []
        for lost in self.loses:
            if lost in self.paths:
                self.paths.remove(lost)
        self.loses = []


class FakeSession:
    """The lease a cleanup claims: its renewal, its deadline and its release."""

    def __init__(self, client, owner, purpose, claim_error=None, close_error=None):
        self.client, self.owner, self.purpose = client, owner, purpose
        self.claim_error, self.close_error = claim_error, close_error
        self.claimed, self.closed = False, False
        self.token = 'private-lease'
        self.failed = threading.Event()
        self.deadline = time.monotonic() + 90

    def claim(self, *args, **kwargs):
        if self.claim_error is not None:
            raise self.claim_error
        self.claimed = True
        return {'state': 'held'}

    def close(self):
        if self.close_error is not None:
            raise self.close_error
        self.closed = True
        return {'state': 'free'}


DISKS = '/media/fat/fogcast/disk-images'
DATA = '/media/fat/fogcast/core-data'
RUN_TREE = f'{DATA}/games/' + 'a' * 64
RUN_DISK = f'{DISKS}/' + 'b' * 64 + '.img'
OLD_TREE = f'{DATA}/games/' + 'c' * 64
SCRUB = f'{DISKS}/' + 'd' * 64 + '.img'
KIT_BEFORE = [DISKS, DATA, f'{DATA}/games', RUN_TREE, f'{RUN_TREE}/record.json',
              RUN_DISK, OLD_TREE, f'{OLD_TREE}/record.json', SCRUB]
# What this run created, as its wrapper's find diff reports it: the earlier
# run's tree and the shared image are not in it.
CREATED = [RUN_TREE, f'{RUN_TREE}/record.json', RUN_DISK]


class GuardedRemoval(unittest.TestCase):
    """The tail's removal itself, which is what ran under a loaded core."""

    LISTED = [RUN_TREE, RUN_DISK]

    def setUp(self):
        self.said, self.sessions, self.close_error = [], [], None

    def hold_for(self, *turns, claim_error=None):
        """A hold that claims the kit and then sees `turns` in order."""

        clock = Clock()
        self.sessions = []

        def factory(client, owner, purpose):
            session = FakeSession(client, owner, purpose, claim_error=claim_error,
                                  close_error=self.close_error)
            self.sessions.append(session)
            return session

        return lambda: kit_hil.hold_for_cleanup(
            FakeClient(list(turns)), 'claude-test', 'cleanup of k1', wait=5,
            session_factory=factory, now=clock.now, sleep=clock.sleep)

    def gate_for(self, turn):
        return self.hold_for(turn, turn)

    def test_a_loaded_core_removes_nothing(self):
        # The kit of the #1368 incident: a core loaded and the lease held by
        # the run that loaded it. The refusal comes at the lease, before any
        # claim, and nothing is read or removed.
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True,
                          hold=self.gate_for(LOADED), log=self.said.append)
        self.assertEqual(kitfs.removed, [], 'nothing may be removed under a loaded core')
        self.assertEqual(sorted(kitfs.paths), sorted(KIT_BEFORE))
        self.assertIn('claude-pc386-games', str(refused.exception))
        self.assertEqual(self.sessions, [])

    def test_a_core_loaded_under_a_free_lease_removes_nothing(self):
        # A lease that expired while its core stayed loaded: the claim
        # succeeds, and the check under it names what is loaded.
        free_but_loaded = {'status': dict(LOADED['status']), 'lease': {'state': 'free'}}
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True,
                          hold=self.hold_for(free_but_loaded, free_but_loaded),
                          log=self.said.append)
        self.assertEqual(kitfs.removed, [])
        self.assertIn('fes.pc386', str(refused.exception))
        self.assertIn('a6977555', str(refused.exception))
        self.assertTrue(self.sessions[0].closed)

    def test_an_idle_kit_removes_exactly_the_runs_trees(self):
        kitfs = FakeKit(KIT_BEFORE)
        plan = kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True,
                             hold=self.gate_for(IDLE), log=self.said.append)
        self.assertEqual(kitfs.removed, self.LISTED)
        self.assertEqual(plan, {'trees': 2, 'paths': 3, 'applied': True})
        self.assertEqual(sorted(kitfs.paths),
                         sorted([DISKS, DATA, f'{DATA}/games', OLD_TREE,
                                 f'{OLD_TREE}/record.json', SCRUB]),
                         'an earlier run\'s trees and the shared image stay')

    def test_without_apply_nothing_is_removed(self):
        kitfs = FakeKit(KIT_BEFORE)
        plan = kit_hil.clean(kitfs, self.LISTED, CREATED,
                             hold=self.gate_for(IDLE), log=self.said.append)
        self.assertEqual(kitfs.removed, [])
        self.assertFalse(plan['applied'])

    def test_a_path_the_run_did_not_create_is_refused(self):
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, [OLD_TREE], CREATED, apply=True,
                          hold=self.gate_for(IDLE), log=self.said.append)
        self.assertEqual(kitfs.removed, [])
        self.assertIn('not in this run', str(refused.exception))

    def test_a_path_outside_the_data_roots_is_refused(self):
        for path in ('/media/fat/fogcast/launcher.json', '/media/fat/linux/u-boot.txt',
                     '/media/fat', DATA, f'{DATA}/../..'):
            with self.subTest(path=path):
                kitfs = FakeKit(KIT_BEFORE + [path])
                with self.assertRaises(kit_hil.CleanupRefused):
                    kit_hil.clean(kitfs, [path], CREATED + [path], apply=True,
                                  hold=self.gate_for(IDLE), log=self.said.append)
                self.assertEqual(kitfs.removed, [])

    def test_a_shell_unsafe_path_is_refused(self):
        for path in (f"{DATA}/games/x'y", f'{DATA}/games/$(reboot)', f'{DATA}/games/*'):
            with self.subTest(path=path):
                kitfs = FakeKit(KIT_BEFORE + [path])
                with self.assertRaises(kit_hil.CleanupRefused):
                    kit_hil.clean(kitfs, [path], CREATED + [path], apply=True,
                                  hold=self.gate_for(IDLE), log=self.said.append)
                self.assertEqual(kitfs.removed, [])

    def test_a_kept_path_is_refused(self):
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, [SCRUB], CREATED + [SCRUB], apply=True, keep=('d' * 64,),
                          hold=self.gate_for(IDLE), log=self.said.append)
        self.assertEqual(kitfs.removed, [])
        self.assertIn('kept', str(refused.exception))

    def test_an_empty_list_is_refused(self):
        with self.assertRaises(kit_hil.CleanupRefused):
            kit_hil.clean(FakeKit(KIT_BEFORE), [], CREATED, apply=True,
                          hold=self.gate_for(IDLE), log=self.said.append)

    def test_a_tree_already_gone_is_refused_before_anything_is_removed(self):
        kitfs = FakeKit([p for p in KIT_BEFORE if not p.startswith(RUN_TREE)])
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True,
                          hold=self.gate_for(IDLE), log=self.said.append)
        self.assertEqual(kitfs.removed, [], 'refuse before removing, not halfway')
        self.assertIn('no longer on the kit', str(refused.exception))

    def test_paths_appearing_during_the_removal_are_reported(self):
        kitfs = FakeKit(KIT_BEFORE, appears=[f'{DATA}/games/new'])
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True,
                          hold=self.gate_for(IDLE), log=self.said.append)
        self.assertIn('appeared', str(refused.exception))

    def test_taking_more_than_the_listed_trees_is_reported(self):
        kitfs = FakeKit(KIT_BEFORE, loses=[f'{OLD_TREE}/record.json'])
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True,
                          hold=self.gate_for(IDLE), log=self.said.append)
        self.assertIn('not the', str(refused.exception))

    def test_the_gate_runs_before_the_listing(self):
        class Listless(FakeKit):
            def find(self, roots):
                raise AssertionError('the gate must refuse before the kit is listed')

        with self.assertRaises(kit_hil.CleanupRefused):
            kit_hil.clean(Listless(KIT_BEFORE), self.LISTED, CREATED, apply=True,
                          hold=self.gate_for(LOADED), log=self.said.append)

    def test_the_lease_is_claimed_before_the_kit_is_listed(self):
        """The P1: the check and the removal must be one held interval."""

        order = []

        class Watched(FakeKit):
            def find(self, roots):
                order.append('find')
                return super().find(roots)

            def remove(self, path):
                order.append('remove')
                return super().remove(path)

        clock = Clock()
        sessions = []

        def factory(client, owner, purpose):
            session = FakeSession(client, owner, purpose)
            sessions.append(session)
            order.append('claim')
            return session

        kitfs = Watched(KIT_BEFORE)
        kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                      hold=lambda: kit_hil.hold_for_cleanup(
                          FakeClient([IDLE, IDLE]), 'claude-test', 'cleanup of k1',
                          session_factory=factory, now=clock.now, sleep=clock.sleep))
        self.assertEqual(order[:2], ['claim', 'find'],
                         'the lease must be held before anything is read or removed')
        self.assertIn('remove', order)
        self.assertTrue(sessions[0].claimed)
        self.assertTrue(sessions[0].closed, 'the cleanup must give its lease back')

    def test_a_core_loaded_after_the_claim_refuses_and_releases(self):
        # The window the P1 names: free when read, a core by the time the
        # removal would act. Claiming first closes it; this covers the kit
        # that was already busy under our own claim.
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=self.hold_for(IDLE, LOADED))
        self.assertEqual(kitfs.removed, [])
        self.assertIn('fes.pc386', str(refused.exception))
        self.assertTrue(self.sessions[0].closed, 'a refusal still gives the lease back')

    def test_a_lease_held_by_another_session_refuses_without_claiming(self):
        held = {'status': {'state': 'idle', 'game_id': None},
                'lease': {'state': 'held', 'owner': 'lander', 'purpose': 'queue run'}}
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=self.hold_for(held))
        self.assertEqual(kitfs.removed, [])
        self.assertIn('lander', str(refused.exception))
        self.assertEqual(self.sessions, [], 'never claim a kit someone else holds')

    def test_a_refused_claim_refuses_the_cleanup(self):
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=self.hold_for(IDLE, claim_error=kit.KitError('claim refused', 409)))
        self.assertEqual(kitfs.removed, [])
        self.assertIn('nothing excludes another session', str(refused.exception))

    def test_a_list_check_failure_never_claims_the_kit(self):
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused):
            kit_hil.clean(kitfs, [OLD_TREE], CREATED, apply=True, log=self.said.append,
                          hold=self.hold_for(IDLE, IDLE))
        self.assertEqual(self.sessions, [],
                         'a bad list is caught before the kit is touched at all')

    def test_a_lost_renewal_stops_the_cleanup_before_it_lists(self):
        """The lease must be held for the whole interval, not just claimed."""

        kitfs = FakeKit(KIT_BEFORE)
        hold = self.hold_for(IDLE, IDLE)

        def losing_hold():
            session = hold()
            session.failed.set()      # a renewal failed while we held it
            return session

        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=losing_hold)
        self.assertEqual(kitfs.removed, [])
        self.assertEqual(sorted(kitfs.paths), sorted(KIT_BEFORE))
        self.assertIn('no longer held', str(refused.exception))
        self.assertIn('the kit listing', str(refused.exception))
        self.assertTrue(self.sessions[0].closed)

    def test_a_renewal_lost_mid_removal_stops_at_that_tree(self):
        # A slow find or rm can outlast the kit's 90 s lease. Losing it after
        # the first tree must not carry on to the second.
        sessions = []

        class Losing(FakeKit):
            def remove(self, path):
                super().remove(path)
                sessions[0].failed.set()

        kitfs = Losing(KIT_BEFORE)
        hold = self.hold_for(IDLE, IDLE)

        def watching_hold():
            session = hold()
            sessions.append(session)
            return session

        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=watching_hold)
        self.assertEqual(kitfs.removed, [self.LISTED[0]],
                         'the second tree must not be removed under a lost lease')
        self.assertIn(f'removing {self.LISTED[1]}', str(refused.exception))

    def test_an_expired_lease_deadline_stops_the_cleanup(self):
        kitfs = FakeKit(KIT_BEFORE)
        hold = self.hold_for(IDLE, IDLE)

        def expired_hold():
            session = hold()
            session.deadline = time.monotonic() - 1
            return session

        with self.assertRaises(kit_hil.CleanupRefused):
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=expired_hold)
        self.assertEqual(kitfs.removed, [])

    def test_an_unconfirmed_release_fails_the_command(self):
        # The removal finished, but the wrapper's `|| exit 5` is the only thing
        # between an unreleased lease and a run reported complete.
        self.close_error = kit.KitError('/v1/kit/release: HTTP 500', 500)
        kitfs = FakeKit(KIT_BEFORE)
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=self.hold_for(IDLE, IDLE))
        self.assertEqual(kitfs.removed, self.LISTED, 'the removal itself did finish')
        self.assertIn('not confirmed released', str(refused.exception))
        self.assertIn('the removal itself finished', str(refused.exception))

    def test_an_unconfirmed_release_does_not_mask_the_original_refusal(self):
        self.close_error = kit.KitError('/v1/kit/release: HTTP 500', 500)
        kitfs = FakeKit([p for p in KIT_BEFORE if not p.startswith(RUN_TREE)])
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            kit_hil.clean(kitfs, self.LISTED, CREATED, apply=True, log=self.said.append,
                          hold=self.hold_for(IDLE, IDLE))
        self.assertIn('no longer on the kit', str(refused.exception),
                      'the first failure is the one reported')

    def test_an_ssh_command_cannot_outlast_the_kits_lease(self):
        # The kit's production lease is 90 s (kitlease.New in mister-agent).
        self.assertLess(kit_hil.KitShell('/nonexistent/ssh').timeout, 90)

    def test_a_missing_run_list_is_refused(self):
        with tempfile.TemporaryDirectory() as home:
            with self.assertRaises(kit_hil.CleanupRefused):
                kit_hil.read_lines(Path(home) / 'kit-new-tops.txt')


class KitListing(unittest.TestCase):
    """Every configured root is listed, or the cleanup refuses."""

    def shell(self, answers):
        class Shell(kit_hil.KitShell):
            def __init__(self):
                super().__init__('/nonexistent/ssh')
                self.asked = []

            def run(self, command):
                self.asked.append(command)
                for pattern, reply in answers.items():
                    if pattern in command:
                        return reply
                return 0, ''

        return Shell()

    def test_an_absent_root_is_empty_and_said_so_not_unknown(self):
        # /media/fat/fogcast/saves need not exist on a kit that never saved.
        # One find over all three roots would exit nonzero for that and a
        # refusal on any nonzero exit would refuse every cleanup there.
        shell = self.shell({f"test -d '{SCRUB.rsplit('/', 1)[0]}'": (0, ''),
                            f"test -d '{DATA}'": (0, ''),
                            "test -d '/media/fat/fogcast/saves'": (1, ''),
                            f"find '{DISKS}'": (0, f'{DISKS}\n{RUN_DISK}\n'),
                            f"find '{DATA}'": (0, f'{DATA}\n'),
                            "find '/media/fat/fogcast/saves'": (2, 'must not be asked')})
        paths = shell.find(kit_hil.DATA_ROOTS)
        self.assertEqual(paths, sorted([DISKS, RUN_DISK, DATA]))
        self.assertEqual(shell.absent, ['/media/fat/fogcast/saves'])
        self.assertNotIn("find '/media/fat/fogcast/saves'", shell.asked,
                         'an absent root is not listed at all')

    def test_a_failed_listing_of_a_root_that_exists_refuses(self):
        shell = self.shell({'test -d': (0, ''),
                            f"find '{DISKS}'": (0, f'{DISKS}\n'),
                            f"find '{DATA}'": (1, f'{DATA}\n{RUN_TREE}\n')})
        with self.assertRaises(kit_hil.CleanupRefused) as refused:
            shell.find(kit_hil.DATA_ROOTS)
        self.assertIn(DATA, str(refused.exception))
        self.assertIn('nothing removed', str(refused.exception))

    def test_a_partial_listing_never_reaches_a_removal(self):
        shell = self.shell({'test -d': (0, ''),
                            f"find '{DISKS}'": (1, f'{DISKS}\n{RUN_DISK}\n')})
        removed = []
        shell.remove = removed.append
        with self.assertRaises(kit_hil.CleanupRefused):
            kit_hil.clean(shell, [RUN_DISK], [RUN_DISK], apply=True, log=lambda *_: None,
                          hold=None)
        self.assertEqual(removed, [])

    def test_an_unsafe_root_is_refused(self):
        shell = self.shell({'test -d': (0, '')})
        with self.assertRaises(kit_hil.CleanupRefused):
            shell.find(('/media/fat/fogcast/$(reboot)',))


class Liveness(unittest.TestCase):
    """The check that misled the #1368 session: `pgrep -f` matches the asker."""

    def test_pgrep_f_matches_the_asking_shell_but_program_pids_does_not(self):
        if shutil.which('pgrep') is None:
            self.skipTest('pgrep is not installed')
        name = 'kit-hil-run-liveness-probe.sh'
        asking = subprocess.run(['bash', '-c', f'pgrep -f {name} || true'],
                                capture_output=True, text=True, timeout=30)
        self.assertTrue(asking.stdout.strip(),
                        'pgrep -f matches its own shell, so it cannot answer this question')
        self.assertEqual(kit_hil.program_pids('definitely-no-such-program-kit-hil'), [])

    def test_program_pids_finds_a_live_child_and_never_the_asker(self):
        if shutil.which('pgrep') is None:
            self.skipTest('pgrep is not installed')
        marker = 'kit-hil-program-pids-marker'
        program = Path(sys.executable).name
        child = subprocess.Popen([sys.executable, '-c',
                                  f'import sys, time; sys.argv.append({marker!r}); '
                                  'print("up", flush=True); time.sleep(120)', marker],
                                 stdout=subprocess.PIPE, text=True)
        try:
            self.assertEqual(child.stdout.readline().strip(), 'up')
            found = kit_hil.program_pids(program, needle=marker)
            self.assertIn(child.pid, found, 'a live process must be found by name and argument')
            self.assertNotIn(os.getpid(), found, 'never report the asking process')
            self.assertEqual(kit_hil.program_pids(program, needle='kit-hil-no-such-marker'), [])
        finally:
            child.kill()
            child.wait(timeout=30)
        self.assertNotIn(child.pid, kit_hil.program_pids(program, needle=marker))

    def test_a_held_run_lock_names_its_live_holder(self):
        with tempfile.TemporaryDirectory() as home:
            lock = Path(home) / kit_hil.LOCK_NAME
            self.assertIsNone(kit_hil.lock_holder(lock), 'no lock file yet means no holder')
            driver = subprocess.Popen(
                [sys.executable, '-c',
                 'import fcntl, json, os, sys, time\n'
                 'handle = open(sys.argv[1], "a+")\n'
                 'fcntl.flock(handle.fileno(), fcntl.LOCK_EX)\n'
                 'handle.seek(0); handle.truncate()\n'
                 'handle.write(json.dumps({"pid": os.getpid(), "cmdline": ["driver"]}) + "\\n")\n'
                 'handle.flush(); print("held", flush=True)\n'
                 'time.sleep(120)\n', str(lock)],
                stdout=subprocess.PIPE, text=True)
            try:
                self.assertEqual(driver.stdout.readline().strip(), 'held')
                holder = kit_hil.lock_holder(lock)
                self.assertIsNotNone(holder, 'a live driver must not read as dead')
                self.assertEqual(holder['pid'], driver.pid)
                self.assertIsNotNone(holder['cmdline_now'], 'confirm the pid from /proc')
                self.assertEqual(kit_hil.main(['alive', str(lock)]), 0)
            finally:
                driver.kill()
                driver.wait(timeout=30)
            self.assertIsNone(kit_hil.lock_holder(lock), 'a dead driver releases its lock')
            self.assertEqual(kit_hil.main(['alive', str(lock)]), 1)

    def test_the_driver_takes_its_own_lock_and_records_itself(self):
        with tempfile.TemporaryDirectory() as home:
            lock = Path(home) / 'runs/k1' / kit_hil.LOCK_NAME
            with kit_hil.run_lock(lock) as handle:
                self.assertTrue(handle)
                recorded = json.loads(lock.read_text())
                self.assertEqual(recorded['pid'], os.getpid())
                self.assertIn(sys.executable.rsplit('/', 1)[-1],
                              ' '.join(recorded['cmdline'] or []) or sys.executable)
                with self.assertRaises(kit_hil.RunFailed):
                    kit_hil.run_lock(lock)


if __name__ == '__main__':
    unittest.main()
