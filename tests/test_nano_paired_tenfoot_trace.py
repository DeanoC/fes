"""Synthetic App/menu wire over captured firmware records, without hardware."""
import copy
import hashlib
import json
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import paired_tenfoot_trace as paired
from paired_firmware_trace import menu_closed
import test_nano_combined_program as combined
from test_nano_program_client import LAUNCH, BOOT, SPEC, OWNER
from test_nano_tenfoot_trace import Model as AppModel, PROGRAMS, STAGING_FAULTS, corrupt_staging
from test_nano_shared_tenfoot_trace import POLICY, SHARED_BOOT, SOURCE

MENU_CYCLE = (ROOT/'tests/fixtures/nano-paired-tenfoot/menu-cycle.txt').read_bytes()
CONTROL_AT = MENU_CYCLE.index(b'FES_NANO_CONFIG_QUERY ')
CLOSE_AT = MENU_CYCLE.index(b'FES_NANO_WINDOW_SCOPE phase=close ')
MENU_OPEN = MENU_CYCLE[CONTROL_AT:CLOSE_AT]
MENU_CLOSE = MENU_CYCLE[CLOSE_AT:]
CONFIGURATION = LAUNCH[:LAUNCH.index(b'NOTICE:  FES_NANO_CONFIG_QUERY ')]
CORE_OPEN = LAUNCH[len(CONFIGURATION):]
CPU = (ROOT/'tests/fixtures/nano-tenfoot-startup/cpu.txt').read_bytes()


def wire(prefix, row):
    return prefix+json.dumps(row, separators=(',', ':')).encode()+b'\n'


class Model(AppModel):
    def __init__(self, shared=False, protocol='gamepad'):
        super().__init__()
        setup = combined.CombinedProgramTests(); setup.setUp()
        self.profile = copy.deepcopy(setup.profile)
        p = self.profile
        p.update(program='fpgaload', tenfoot_serial=True, usb_shared=shared,
                 usb_control=shared, usb_input=protocol if shared else None)
        p['hps_context'].update(package_id=SPEC['package']['package_id'], build_id=SPEC['package']['build_id'],
            core_sha256=SPEC['package']['payload_sha256'], core_size=SPEC['package']['payload_bytes'])
        for key in ('package_id', 'build_id'):
            p['hps_control'][key] = p['hps_context'][key]
            p['hps_reader_memory'][key] = p['hps_context'][key]
        p['hps_program_platform']['boot_context_sha256'] = paired.hps_context.digest(p['hps_context'])
        p['hps_context_sha256'] = paired.hps_context.digest(p['hps_context'])
        p['hps_program_platform_sha256'] = paired.hps_context.digest(p['hps_program_platform'])
        p['hps_configuration'] = copy.deepcopy(SPEC['configuration'])
        for name in ('hps_control', 'hps_configuration', 'hps_reader_memory'):
            p[name+'_sha256'] = paired.hps_context.digest(p[name])
        cfg = p['hps_menu_program']['configuration'] = copy.deepcopy(SPEC['configuration'])
        cfg.update(package_id=p['hps_menu_program']['context']['package_id'],
                   build_id=p['hps_menu_program']['context']['build_id'],
                   core_sha256=p['hps_menu_program']['context']['core_sha256'])
        p['hps_menu_program']['context']['core_size'] = cfg['core_size']
        p['hps_menu_program']['platform']['boot_context_sha256'] = paired.hps_context.digest(
            p['hps_menu_program']['context'])
        p['hps_menu_program_sha256'] = paired.hps_context.digest(p['hps_menu_program'])
        self.companion, self.contract = paired.menu_policy(p, int(OWNER, 16))
        self.shared = shared
        self.policy = copy.deepcopy(POLICY)
        if protocol == 'boot-keyboard':
            self.policy.update(input=protocol, descriptor_sha256=hashlib.sha256(b'').hexdigest(),
                               descriptor_bytes=0)
        self.policy['hps_context'] = dict(sha256=p['hps_context_sha256'],
            package_id=p['hps_context']['package_id'], build_id=p['hps_context']['build_id'])
        self.source = 4
        self.previous = 0
        self.configurations = 0
        self.menu_index = 1
        self.frames = 1
        self.reports = 0
        self.polls = 0
        self.fault = ''
        self.initial = self._initial()

    def source_lines(self, data, source, *, original=4):
        data = re.sub(rb'\bowner=17\b', b'owner='+OWNER.encode(), data)
        return re.sub(rb'\bgeneration=([0-9]+)',
                      lambda m: b'generation='+str(int(m[1])+source-original).encode(), data)

    def configure(self, source):
        raw = self.source_lines(CONFIGURATION, source)
        raw = raw.replace(b'previous=0 rearm=0', f'previous={self.previous} rearm={int(self.previous != 0)}'.encode())
        step = self.configurations*20*400000000
        raw = re.sub(rb'\bcounter=([0-9a-f]+)', lambda m:
                     b'counter='+format(int(m[1], 16)+step, 'x').encode(), raw)
        self.previous = source
        self.configurations += 1
        return raw

    def menu_row(self, event='frame'):
        opened = dict(Capabilities=dict(Mask=15, ControlMask=2, HardwareDrainMask=12, FabricHandshake=1),
            Owner=int(OWNER, 16), Generation=self.source+2, ResetMask=5,
            NeverEnabled=False, MastersContained=False)
        after = copy.deepcopy(opened)
        if event == 'return':
            after.update(Generation=self.source+3, ResetMask=15, MastersContained=True)
        return dict(scope='nano-program-menu-v1', event=event, menu=self.menu_index,
                    identity=self.contract['identity'], source_sha256=self.contract['source_sha256'],
                    source_generation=self.source, opened=opened, after=after, frames=self.frames,
                    frame=dict(Generation=1, Sequence=self.frames, Submitted=self.frames,
                               Slot=(self.frames-1) % 2, Underflows=0),
                    **{k: self.contract[k] for k in ('memory_sha256', 'control_sha256', 'access_sha256')})

    def menu_wire(self, event='frame'):
        return wire(paired.MENU, self.menu_row(event))

    def usb(self, stage, state=None, generation=None):
        if stage == 'stop':
            self.reports += 1
            self.polls += 1
            return f'FES_NATIVE_SHARED_USB STOP owner={OWNER} generation=3 reports={self.reports} polls={self.polls}\n'.encode()
        return (f'FES_NATIVE_SHARED_USB INPUT phase={stage} state={state} native_generation={generation} '
                f'owner={OWNER} generation=2 reports={self.reports} polls={self.polls}\n').encode()

    def _initial(self):
        boot = BOOT
        ready = b''
        if self.shared:
            context = self.policy['hps_context']
            actual = (f"FES_NATIVE_HPS_CONTEXT PASS sha256={context['sha256']} package_id={context['package_id']} "
                      f"build_id={context['build_id']} owner={OWNER} generation=1 programming=unavailable\n").encode()
            boot = SHARED_BOOT.replace(SOURCE, actual)
            ready = (f"FES_NATIVE_SHARED_USB READY input={self.policy['input']} owner={OWNER} generation=2 "
                     f"descriptor_sha256={self.policy['descriptor_sha256']} descriptor_bytes={self.policy['descriptor_bytes']}\n").encode()
        # main opens shared USB before verifier_ready and the SD/FAT steps.
        # READY must follow HPS_IO PASS, but is not the final boot-prefix row.
        cpu = CPU.splitlines(keepends=True)
        prefix = b''.join(cpu[:10])+boot+ready+b''.join(cpu[10:])
        mode = self.policy['input'].encode() if self.shared else b''
        begin = paired.BEGIN+(b'uart+shared-usb-'+mode if self.shared else b'uart')+b' preferences=ram\n'
        initial = prefix+self.configure(4)+self.source_lines(MENU_OPEN, 4)+self.menu_wire()+begin+self.raw(self.state())
        self.frames += 1
        return initial+self.menu_wire()

    def client(self, initial=None, profile=None, programs=None):
        return paired.PairedTenfootClient(self.initial if initial is None else initial,
            build_record=self.profile if profile is None else profile,
            build_id=SPEC['build_id'], owner_seed=SPEC['owner_seed'], atf=SPEC['atf'], package=SPEC['package'],
            programs=PROGRAMS if programs is None else programs, exchange=self.exchange,
            owns=lambda: self.service.owned, now=lambda: self.service.clock,
            shared_usb=self.policy if self.shared else None)

    def exchange(self, key, bound, done):
        self.keys.append(key)
        before = self.service.generation
        if self.fault == 'raise':
            raise OSError('injected transport failure')
        if key in (b'\x1b[C', b'\x1b[D'):
            self.focus += 1 if key == b'\x1b[C' else -1
            self.frames += 1
            raw = self.raw(self.state())+self.menu_wire()
        elif key == b'\r':
            self.selected = self.focus
            raw = self.raw(self.state('loading', before))
            raw += self.source_lines(MENU_CLOSE, self.source)+self.menu_wire('return')
            raw += self.raw(self.state('loading', before+1))
            self.source += 6
            raw += self.configure(self.source)+self.source_lines(CORE_OPEN, self.source)
            self.service.phase = 'running'; self.service.generation += 1
            raw += self.fetch()+self.raw(self.state())
        else:
            raw = self.raw(self.state('returning', before))
            raw += (f'FES_NANO_MASTER_STOP status=0 owner={OWNER} generation={self.source+1} reset=d '
                    'reader_reads=88 reader_polls=1 disable=0 probe_polls=2 cancel=0 complete=1 data=held\n').encode()
            self.source += 5
            self.menu_index += 1; self.frames = 1
            raw += self.configure(self.source)+self.source_lines(MENU_OPEN, self.source)+self.menu_wire()
            self.service.phase = 'ready'; self.service.generation += 1
            if key == b'q':
                raw += self.source_lines(MENU_CLOSE, self.source)+self.menu_wire('return')
                self.service.generation += 1
                raw += f'FES_NATIVE_TENFOOT_EXIT state=parked generation={self.service.generation}\n'.encode()
            else:
                raw += self.raw(self.state())
                self.frames += 1
                raw += self.menu_wire()
        if self.shared and key not in (b'\x1b[C', b'\x1b[D'):
            self.reports += 8
            self.polls += 8
            raw = self.usb('pause', 'ready' if key == b'\r' else 'running', before)+raw
            if key == b'\r':
                marker = raw.index(paired.app.FETCH)
            elif key == b'q':
                marker = raw.index(b'FES_NANO_WINDOW_SCOPE phase=close ')
            else:
                marker = raw.rindex(paired.app.PREFIX)
            raw = raw[:marker]+self.usb('resume', 'running' if key == b'\r' else 'ready', before+1)+raw[marker:]
            if key == b'q':
                marker = raw.index(b'FES_NATIVE_TENFOOT_EXIT ')
                raw = raw[:marker]+self.usb('stop')+raw[marker:]
        if key not in (b'\x1b[C', b'\x1b[D'):
            raw = self.input_action('select' if key == b'\r' else 'stop', before)+raw
            if key == b'q':
                at = raw.rindex(b'FES_NANO_WINDOW_SCOPE phase=close ')
                at = raw.rfind(b'\n', 0, at)+1
                raw = raw[:at]+self.input_action('quit', before+1)+raw[at:]
        raw = self.corrupt(raw, key)
        # Real serial reads repeatedly offer the same growing buffer. A pure
        # framing predicate must not consume a menu frame twice.
        for end in (len(raw)//2, len(raw)-1, len(raw), len(raw)):
            done(raw[:end])
        return raw

    def corrupt(self, raw, key):
        fault = self.fault
        if fault == 'late': self.service.clock += 111
        if fault == 'clock': self.service.clock -= 1
        if fault == 'claim': self.service.owned = False
        if fault == 'bad-ack': raw = raw.replace(b'ack_cookie=7', b'ack_cookie=8')
        if fault == 'stuck-drain': raw = raw.replace(b'status=4 pending=0', b'status=4 pending=1')
        if fault == 'missing-drain': raw = re.sub(rb'[^\n]*FES_NANO_WINDOW_READER_DRAIN[^\n]*\n', b'', raw)
        if fault == 'missing-return': raw = b''.join(line for line in raw.splitlines(keepends=True)
            if not (line.startswith(paired.MENU) and b'"event":"return"' in line))
        if fault == 'queue': raw = raw.replace(b'credits=4', b'credits=8')
        if fault == 'missing-mailbox': raw = re.sub(rb'[^\n]*FES_NANO_CORE_MAILBOX[^\n]*\n', b'', raw)
        if fault == 'replay': raw = raw.replace(b'previous=', b'previous=9')
        if fault == 'owner': raw = raw.replace(b'owner='+OWNER.encode(), b'owner=1')
        if fault == 'scope': raw = raw.replace(b'serial-state-only', b'fpga-menu')
        if fault == 'unknown': raw = b'FES_NANO_SURPRISE value=1\n'+raw
        if fault == 'truncated': raw = raw[:-1]
        if fault == 'old-exit': raw = re.sub(rb'state=parked generation=[0-9]+', b'state=ready generation=6', raw)
        if fault == 'late-usb-stop':
            stop = next(line for line in raw.splitlines(keepends=True) if b'SHARED_USB STOP ' in line)
            raw = raw.replace(stop, b'')+stop
        if fault == 'early-usb-stop':
            stop = next(line for line in raw.splitlines(keepends=True) if b'SHARED_USB STOP ' in line)
            raw = stop+raw.replace(stop, b'')
        if fault == 'missing-usb-resume': raw = re.sub(rb'FES_NATIVE_SHARED_USB INPUT phase=resume[^\n]*\n', b'', raw)
        if fault == 'bad-core-stop': raw = raw.replace(b'complete=1 data=held', b'complete=0 data=held')
        if fault == 'missing-core-stop': raw = re.sub(rb'[^\n]*FES_NANO_MASTER_STOP[^\n]*data=held\n', b'', raw)
        if fault == 'early-ready' and key == b's':
            raw = next(line for line in reversed(raw.splitlines(keepends=True))
                       if line.startswith(paired.app.PREFIX))+raw
        if fault == 'early-running' and key == b'\r':
            last = raw.splitlines(keepends=True)[-1]
            raw = last+raw
        return raw


class PairedTenfootTraceTests(unittest.TestCase):
    def test_staged_faults_retire_uart_and_shared_keyboard_before_acceptance(self):
        for shared in (False,True):
            for fault in STAGING_FAULTS:
                with self.subTest(shared=shared,fault=fault):
                    model=Model(shared,protocol='boot-keyboard');client=model.client()
                    client.key('launch',0);client.key('stop')
                    exchange=model.exchange
                    client.exchange=lambda key,bound,done:corrupt_staging(exchange(key,bound,lambda _:True),fault)
                    with self.assertRaises(ValueError):client.key('launch',0)
                    self.assertTrue(client.retired)
                    keys=list(model.keys)
                    with self.assertRaises(ValueError):client.key('launch',0)
                    self.assertEqual(model.keys,keys)

    def test_three_sessions_join_boot_menu_core_return_and_final_park(self):
        for shared in (False, True):
            with self.subTest(shared=shared):
                model = Model(shared); client = model.client()
                result = paired.exercise(client)
                self.assertTrue(result['passed'])
                self.assertEqual(result['cycles'], [dict(selection=0, launch_generation=1, return_generation=2),
                    dict(selection=1, launch_generation=3, return_generation=4),
                    dict(selection=0, launch_generation=5, return_generation=7)])
                self.assertEqual(result['menu']['menu_sessions'], 4)
                self.assertEqual(client.previous_configuration, 37)
                self.assertEqual(client.bridge_generation, 40)
                self.assertEqual(model.keys, [b'\r', b's', b'\x1b[C', b'\r', b's', b'\x1b[D', b'\r', b'q'])
                self.assertFalse(result['hardware_acceptance'] or result['physical_display_verified'])
                self.assertEqual(result['shared_usb_reports'], 49 if shared else None)

    def test_launch_failures_retire_without_another_key(self):
        for fault in ('raise', 'late', 'clock', 'claim', 'bad-ack', 'stuck-drain', 'missing-drain',
                      'missing-return', 'queue', 'missing-mailbox', 'replay', 'owner', 'scope',
                      'unknown', 'truncated', 'early-running', 'fetch-stuck', 'fetch-held',
                      'fetch-generation', 'fetch-missing', 'fetch-duplicate'):
            with self.subTest(fault=fault):
                model = Model(); client = model.client(); model.fault = fault
                with self.assertRaises((ValueError, OSError)):
                    client.key('launch', 0)
                self.assertTrue(client.retired)
                sent = list(model.keys)
                with self.assertRaises(ValueError): client.key('launch', 0)
                self.assertEqual(model.keys, sent)

    def test_failed_return_or_final_drain_cannot_relaunch(self):
        for op in ('stop', 'quit'):
            for fault in ('queue', 'missing-mailbox', 'replay', 'unknown', 'bad-ack', 'missing-drain',
                          'missing-return', 'old-exit', 'late', 'bad-core-stop', 'missing-core-stop', 'early-ready'):
                if op == 'stop' and fault in ('bad-ack', 'missing-drain', 'missing-return', 'old-exit'):
                    continue
                if op == 'quit' and fault == 'early-ready':
                    continue
                with self.subTest(op=op, fault=fault):
                    model = Model(); client = model.client(); client.key('launch', 0); model.fault = fault
                    with self.assertRaises(ValueError): client.key(op)
                    self.assertTrue(client.retired)

    def test_shared_pause_resume_and_stop_are_joined_to_menu_ownership(self):
        for fault in ('missing-usb-resume', 'early-usb-stop', 'late-usb-stop'):
            with self.subTest(fault=fault):
                model = Model(True); client = model.client(); client.key('launch', 0); model.fault = fault
                with self.assertRaises(ValueError): client.key('quit')
                self.assertTrue(client.retired)

    def test_source_union_and_initial_menu_fail_before_input(self):
        model = Model()
        variants = [model.initial.replace(paired.BEGIN, paired.app.BEGIN+b' foreign='),
                    model.initial.replace(CPU.splitlines(keepends=True)[-1], b''),
                    model.initial.replace(b'phase=control', b'phase=data', 1),
                    model.initial.replace(b'bytes=8388608', b'bytes=2097152', 1)]
        for raw in variants:
            with self.subTest(raw=raw[-60:]), self.assertRaises(ValueError): model.client(initial=raw)
        for change in ('missing-companion', 'undersized-union', 'wrong-anchor', 'wrong-map-hash'):
            profile = copy.deepcopy(model.profile)
            if change == 'missing-companion': profile.pop('hps_menu_program')
            if change == 'undersized-union': profile['cpu_mapping']['reader_bytes'] = 2 << 20
            if change == 'wrong-anchor': profile['hps_context']['package_id'] = '9'*64
            if change == 'wrong-map-hash': profile['cpu_mapping_sha256'] = '0'*64
            with self.subTest(change=change), self.assertRaises(ValueError): model.client(profile=profile)
        self.assertEqual(model.keys, [])

    def test_already_drained_and_active_secure_menu_closures(self):
        for active in (False, True):
            data = MENU_CLOSE
            if not active:
                data = data.replace(b'reader_reads=132 reader_polls=2 disable=1',
                                    b'reader_reads=88 reader_polls=1 disable=0')
            result = menu_closed(data, 0x17, 4)
            self.assertEqual(result['closure']['generation'], 7)
            self.assertFalse(result['physical_ddr_drain_qualified'])
        with self.assertRaises(ValueError):
            menu_closed(MENU_CLOSE.replace(b'reader_polls=2', b'reader_polls=129'), 0x17, 4)

    def test_core_close_deadline_is_not_widened_by_menu_restore_budget(self):
        model = Model(); client = model.client(); client.key('launch', 0)
        exchange = model.exchange
        def slow_core(key, bound, done):
            model.service.clock += 6
            return exchange(key, bound, done)
        client.exchange = slow_core
        with self.assertRaisesRegex(ValueError, 'original 5s'):
            client.key('stop')
        self.assertTrue(client.retired)

    def test_usb_resume_allows_later_completed_ui_frames(self):
        model = Model(True); client = model.client(); client.key('launch', 0)
        result = client.key('stop')
        self.assertEqual([r['frames'] for r in result['menu_observations']], [1, 2])
        client.key('launch', 0)
        client.key('quit')
        self.assertTrue(client.retired)

    def test_keyboard_scope_and_build_selection_are_explicit(self):
        model = Model(True, 'boot-keyboard')
        self.assertTrue(paired.exercise(model.client())['passed'])
        for change in ({'usb_input': 'gamepad'}, {'usb_shared': False}, {'usb_control': False},
                       {'usb_shared': 1}):
            profile = dict(model.profile, **change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                model.client(profile=profile)

    def test_idle_keyboard_completes_all_core_and_menu_cycles_with_poll_evidence(self):
        model = Model(True, 'boot-keyboard')
        original = model.corrupt
        model.corrupt = lambda raw, key: re.sub(rb'reports=[0-9]+', b'reports=0', original(raw, key))
        client = model.client(); result = paired.exercise(client)
        self.assertTrue(result['passed']);self.assertEqual(result['shared_usb_reports'], 0)
        self.assertEqual(result['shared_usb_polls'], 49)
        self.assertEqual(result['menu']['menu_sessions'], 4)
        self.assertFalse(result['input_controls_exercised']);self.assertFalse(result['hardware_acceptance'])

    def test_idle_keyboard_cannot_hide_stopped_polling_or_old_missing_counter(self):
        for missing in (False, True):
            model = Model(True, 'boot-keyboard'); original = model.corrupt
            def corrupt(raw, key):
                raw = re.sub(rb'reports=[0-9]+', b'reports=0', original(raw, key))
                return re.sub(rb' polls=[0-9]+', b'', raw) if missing else re.sub(rb'polls=[0-9]+', b'polls=0', raw)
            model.corrupt = corrupt; client = model.client()
            with self.subTest(missing=missing), self.assertRaises(ValueError):client.key('launch', 0)
            self.assertTrue(client.retired); sent = list(model.keys)
            with self.assertRaises(ValueError):client.key('launch', 0)
            self.assertEqual(model.keys, sent)

    def test_restore_and_final_park_have_separate_deadlines(self):
        for phase in ('restore', 'park'):
            with self.subTest(phase=phase):
                model = Model(); client = model.client(); client.key('launch', 0)
                original = model.exchange
                def exchange(key, bound, done):
                    raw = original(key, bound, lambda _: False)
                    menu_at = raw.index(paired.MENU)
                    done(raw[:raw.index(b'FES_NANO_CORE_TRANSFER phase=arm ')])
                    if phase == 'restore':
                        model.service.clock += 101
                    else:
                        done(raw[:menu_at]+raw[menu_at:raw.index(b'\n', menu_at)+1])
                        model.service.clock += 6
                    done(raw)
                    return raw
                client.exchange = exchange
                with self.assertRaisesRegex(ValueError, '100s|park exceeded 5s'):
                    client.key('quit')
                self.assertTrue(client.retired)

    def test_caller_changes_cannot_rewrite_retained_source_or_choices(self):
        model = Model(); profile = copy.deepcopy(model.profile); programs = copy.deepcopy(PROGRAMS)
        client = model.client(profile=profile, programs=programs)
        profile['hps_menu_program']['memory']['arena_bytes'] = 1
        programs[0]['identity']['BuildID'] = 'f'*32
        self.assertEqual(client.key('launch', 0)['status'], 'verified')
        self.assertFalse(client.retired)

    def test_initial_ready_without_post_state_ui_frame_rejects(self):
        model = Model()
        initial = b''.join(model.initial.splitlines(keepends=True)[:-1])
        with self.assertRaisesRegex(ValueError, 'subsequent presented'):
            model.client(initial=initial)
        self.assertEqual(model.keys, [])

    def test_focus_and_return_do_not_finish_at_the_ready_message(self):
        for op in ('focus', 'stop'):
            with self.subTest(op=op):
                model = Model(); client = model.client()
                if op == 'stop': client.key('launch', 0)
                original = model.exchange
                decisions = []
                def exchange(key, bound, done):
                    raw = original(key, bound, lambda _: False)
                    lines = raw.splitlines(keepends=True)
                    before_frame = b''.join(lines[:-1])
                    self.assertTrue(lines[-1].startswith(paired.MENU))
                    self.assertTrue(lines[-2].startswith(paired.app.PREFIX))
                    decisions.append(done(before_frame))
                    self.assertTrue(done(raw))
                    return raw
                client.exchange = exchange
                client.key(op, 1 if op == 'focus' else None)
                self.assertEqual(decisions, [False])

    def test_cached_frame_before_ready_cannot_replace_new_presentation(self):
        model = Model(); client = model.client(); original = model.exchange
        def exchange(key, bound, done):
            raw = original(key, bound, lambda _: False)
            lines = raw.splitlines(keepends=True)
            raw = lines[-1]+b''.join(lines[:-1])
            self.assertFalse(done(raw))
            return raw
        client.exchange = exchange
        with self.assertRaisesRegex(ValueError, 'terminal observation'):
            client.key('focus', 1)
        self.assertTrue(client.retired)


if __name__ == '__main__': unittest.main()
