"""Bounded menu/core board-test controller with injectable transport and lease.

The operator independently admits the current sealed sources and matched boot
artifacts before construction. UART evidence alone grants no memory mapping,
device control, electrical qualification or general appliance acceptance.
"""
import copy
import json
import re
import threading
import time

import hps_context
import tenfoot_trace as app
from menu_service_trace import MenuTrace, PREFIX as MENU
from native_trace import decode_native_boot
from paired_firmware_trace import configuration, menu_closed, menu_opening
from service_trace import closed, opening, number, require
from shared_input_trace import SharedInput, PREFIX as USB, record as usb_record

BEGIN = b'FES_NATIVE_TENFOOT_BEGIN display=fpga-menu input='
EXIT = re.compile(rb'FES_NATIVE_TENFOOT_EXIT state=parked generation=([0-9]{1,20})')
ARM = re.compile(rb'(?:NOTICE:[ \t]+)?FES_NANO_CORE_TRANSFER phase=arm ')


def menu_policy(record, owner):
    """Pure compiled contract check; does not replace fresh source admission."""
    require(type(record) is dict and record.get('tenfoot_serial') is True,
            'explicit selected Tenfoot build required')
    companion = hps_context.compiled_menu_program(record)
    require(companion is not None, 'complete compiled menu companion required')
    require(record.get('hps_context_sha256') == hps_context.digest(record['hps_context']),
            'computer boot context digest differs')
    hps_context.validate_reader_memory(record['hps_context'], record['hps_reader_memory'])
    for name in ('hps_control', 'hps_configuration', 'hps_reader_memory'):
        require(record.get(name+'_sha256') == hps_context.digest(record[name]),
                'paired original '+name+' digest differs')
    for context, descriptor in ((record['hps_context'], record['hps_configuration']),
                                (companion['context'], companion['configuration'])):
        require(all(descriptor[key] == context[key] for key in
                    ('package_id', 'build_id', 'core_sha256', 'core_size')),
                'paired configuration differs from its role context')
    mapping = hps_context.cpu_mapping(record['hps_control'], record['hps_configuration'],
                                      record['hps_reader_memory'], menu_program=companion)
    require(record.get('cpu_mapping') == mapping and
            record.get('cpu_mapping_sha256') == hps_context.digest(mapping),
            'paired CPU mapping differs from the explicit union reservation')
    context = companion['context']
    identity = {name: context[key] for name, key in
                (('PackageID', 'package_id'), ('CoreID', 'core_id'), ('ABI', 'abi'),
                 ('BuildID', 'build_id'), ('Platform', 'platform'), ('Device', 'device'),
                 ('ProgrammingProfile', 'programming_profile'), ('Major', 'abi_major'),
                 ('Minor', 'abi_minor'), ('ProgrammedSHA256', 'core_sha256'))}
    identity['ActivationReceiptSHA256'] = ''
    contract = dict(identity=identity, owner=owner, source_sha256=record['hps_context_sha256'],
                    **{key+'_sha256': hps_context.digest(companion[key])
                       for key in ('memory', 'control', 'access')})
    return copy.deepcopy(companion), contract


def observations(data, input_mode, *, startup=False):
    """Framing only: safe to call repeatedly on a growing transport buffer."""
    require(type(data) is bytes and 0 < len(data) <= app.MAX_BYTES and data.endswith(b'\n'),
            'bounded complete paired App capture required')
    rows = []
    begin = BEGIN+input_mode.encode()+b' preferences=ram'
    for index, line in enumerate(data.splitlines(keepends=True), 1):
        raw = line.rstrip(b'\r\n')
        if raw.startswith(app.PREFIX):
            states, evidence, exited = app.packet(line)
            require(len(states) == 1 and not evidence and exited is None, 'invalid paired App state')
            kind, value = 'app', states[0]
        elif raw.startswith(MENU):
            # Stateful MenuTrace verification happens exactly once, after the
            # exchange completes. Do not change it in a polling predicate.
            kind, value = 'menu', line
        elif raw.startswith(app.FETCH):
            kind, value = 'fetch', app.fetch_observation(raw[len(app.FETCH):])
        elif raw.startswith(USB):
            require(input_mode != 'uart', 'shared USB evidence in UART-only paired App')
            kind, value = 'usb', usb_record(raw, index)
        elif raw == begin:
            require(startup, 'paired App restarted during a key exchange')
            kind, value = 'begin', None
        elif EXIT.fullmatch(raw):
            require(not startup, 'paired App exited during startup')
            kind, value = 'exit', int(EXIT.fullmatch(raw)[1])
            require(value < 1 << 64, 'paired exit generation overflow')
        else:
            text = raw.decode('utf-8')
            require(not text.startswith(('panic:', 'fatal error:')) and
                    not re.search(r'\b(?:FAIL|FAILURE|REJECT|EXCEPTION)\b', text),
                    'native failure in paired App capture')
            require(not any(marker in raw for marker in
                (b'FES_NATIVE_TENFOOT', b'FES_NATIVE_MENU', b'FES_NATIVE_CORE_',
                 b'FES_NATIVE_SHARED_USB', b'FES_NATIVE_PROGRAM_REPLY', b'FES_NATIVE_WINDOW_REPLY')),
                'foreign or malformed paired App observation')
            kind, value = 'firmware', line
        rows.append(dict(kind=kind, value=value, line=index))
    require(rows[-1]['kind'] in ('app', 'menu', 'exit'), 'unterminated paired App evidence')
    return rows


def ready_frame(rows, *, generation=None, title=None):
    """Frame after Ready: ProgramMenuPlatform presents inner state first.

    This is a repeatable framing predicate, not a committed MenuTrace receipt.
    The caller must subsequently validate the complete ordered observations.
    """
    if not rows or rows[-1]['kind'] != 'menu':
        return False
    states = [row for row in rows if row['kind'] == 'app']
    if not states:
        return False
    state = states[-1]
    view = state['value']
    value = json.loads(rows[-1]['value'][len(MENU):])
    return (type(value) is dict and value.get('event') == 'frame' and
            rows[-1]['line'] > state['line'] and view['native']['State'] == 'ready' and
            view['session'] == 'idle' and not view['loading'] and len(view['titles']) == 2 and
            (generation is None or view['native']['Generation'] == generation) and
            (title is None or view['titles'][view['focus']] == title))


def split_arm(data):
    lines = data.splitlines(keepends=True)
    positions = [i for i, line in enumerate(lines) if ARM.match(line)]
    require(len(positions) == 1, 'exactly one paired configuration start required')
    index = positions[0]
    return b''.join(lines[:index]), b''.join(lines[index:])


class PairedTenfootClient(app.TenfootClient):
    """Explicit paired lifecycle; core-only TenfootClient remains unchanged."""
    def __init__(self, initial, *, build_record, build_id, owner_seed, atf, package,
                 programs, exchange, owns, now=time.monotonic, record=lambda _: None,
                 shared_usb=None, firmware_ram_trace=False):
        require(all(callable(f) for f in (exchange, owns, now, record)), 'owned exchange hooks required')
        require(type(programs) is list and len(programs) == 2 and
                len({p['title'] for p in programs}) == 2 and
                len({(p['identity']['PackageID'], p['identity']['ActivationReceiptSHA256'])
                     for p in programs}) == 2, 'two distinct titled activation choices required')
        require(all('fetch_address' not in p or number(p['fetch_address'], 65535)
                    for p in programs), 'explicit fetch address must be a 16-bit integer')
        self.programs = copy.deepcopy(programs)
        self.usb = SharedInput(shared_usb) if shared_usb is not None else None
        self.input_mode = 'uart' if self.usb is None else self.usb.input_mode
        rows = observations(initial, self.input_mode, startup=True)
        # Decode only the actual boot prefix. The first menu transfer follows
        # FAT mount, before BEGIN; it is runtime evidence, not boot evidence.
        first_arm = next((i for i, r in enumerate(rows) if r['kind'] == 'firmware'
                          and ARM.match(r['value'])), None)
        require(first_arm is not None, 'initial paired menu configuration absent')
        prefix = rows[:first_arm]
        require(all(r['kind'] in ('firmware', 'usb') for r in prefix), 'App/menu preceded native boot')
        boot_data = b''.join(r['value'] for r in prefix if r['kind'] == 'firmware')
        options = dict(firmware_ram_trace=firmware_ram_trace)
        if self.usb is not None:
            options.update(usb_control=True, usb_shared=True,
                           hps_context=self.usb.policy['hps_context'], hps_io=self.usb.policy['hps_io'])
        boot = decode_native_boot(boot_data, build_id, owner_seed, package=package, atf=atf, **options)
        require(not boot['errors'] and boot['loader_ready'] and
                boot['last_checkpoint'] == ('el1' if self.usb is None else 'hps_io_pass'),
                'matched paired App boot rejected')
        self.owner = next(r['values']['owner'] for r in boot['records'] if r['stage'] == 'spl_handoff')
        self.companion, contract = menu_policy(build_record, self.owner)
        require(type(build_record.get('usb_shared', False)) is bool and
                type(build_record.get('usb_control', False)) is bool and
                (shared_usb is not None) == build_record.get('usb_shared', False) ==
                build_record.get('usb_control', False) and
                build_record.get('usb_input') == (None if self.usb is None else self.usb.policy['input']),
                'paired shared input selection differs from its build')
        context = build_record['hps_context']
        require(package == dict(package_id=context['package_id'], build_id=context['build_id'],
                                payload_sha256=context['core_sha256'], payload_bytes=context['core_size']),
                'matched boot package differs from the original computer anchor')
        self.menu = MenuTrace(contract)
        if self.usb is not None:
            require(self.usb.policy['hps_context'] == dict(
                sha256=build_record['hps_context_sha256'],
                package_id=build_record['hps_context']['package_id'],
                build_id=build_record['hps_context']['build_id']), 'shared USB uses another boot anchor')
            usb_rows = [r['value'] for r in prefix if r['kind'] == 'usb']
            ready_at = next((i for i, row in enumerate(prefix) if row['kind'] == 'usb'), 0)
            require(len(usb_rows) == 1 and ready_at > 0 and
                    prefix[ready_at-1]['kind'] == 'firmware' and
                    prefix[ready_at-1]['value'].rstrip().startswith(b'FES_NATIVE_HPS_IO PASS '),
                    'shared Ready must immediately follow sealed pin proof')
            self.usb.startup(usb_rows, self.owner, boot)
        else:
            require(not any(r['kind'] == 'usb' for r in prefix), 'unexpected shared startup')
        self.startup_cpu = app.startup_checkpoints(boot_data)
        self.boot = boot
        self.exchange, self.owns, self.now, self.record = exchange, owns, now, record
        self.active = threading.Lock()
        self.retired = False
        self.loads = 0
        self.previous_configuration = 0
        self.last_counter = 0
        self.bridge_generation = 1  # trusted cold owner; SD reaches source4
        self.menu_source = None
        self.core_source = None
        self.records = []
        self.view = None
        require(self.owns(), 'paired App console claim absent')
        self.startup = self._consume(rows[first_arm:], 'startup', 0, None)
        require(self.view is not None and self._ready(self.view), 'paired App is not initially Ready')

    def _configured(self, result):
        timing = result['configuration_timing']
        require(timing['first_counter'] > self.last_counter, 'paired configuration counter restarted')
        self.last_counter = timing['last_counter']
        self.previous_configuration = result['configuration_generation']

    def _consume(self, rows, op, generation, selected):
        firmware = b''
        states, menus, usb_rows, fetches = [], [], [], []
        begins = 0
        exited = None
        rank = -1
        terminal = False
        returned_core = False
        opened_menu = 0
        returned_menu = 0
        first_mutation = None
        last_firmware = None
        resume_boundary = None
        final_menu_return = None
        ready_line = None
        ready_presented = False
        for row in rows:
            kind, value, line = row['kind'], row['value'], row['line']
            require(exited is None, 'evidence followed final paired exit')
            if kind == 'firmware':
                require(not terminal or app.quiet(value), 'firmware followed published terminal App ownership')
                firmware += value
                if not app.quiet(value):
                    if first_mutation is None:
                        first_mutation = line
                    last_firmware = line
            elif kind == 'begin':
                require(op == 'startup' and begins == 0 and self.menu_source is not None and
                        not states and app.quiet(firmware), 'paired App began before initial menu')
                begins += 1
            elif kind == 'usb':
                require(op != 'startup', 'shared USB restarted after initial boot')
                usb_rows.append(value)
            elif kind == 'menu':
                require(op != 'startup' or begins == 0 or self.menu_source is not None,
                        'initial menu opened after App began')
                observed = self.menu.feed(value)
                if observed['event'] == 'frame':
                    require(op in ('startup', 'focus', 'stop', 'quit') or
                            op == 'launch' and self.menu_source is not None and not returned_menu,
                            'menu frame outside its owned Ready interval')
                    if self.menu_source is None:
                        require(not terminal and op in ('startup', 'stop', 'quit') and not opened_menu,
                                'unexpected independent menu reload')
                        if op != 'startup':
                            core_close, firmware = split_arm(firmware)
                            require(self.core_source is not None, 'menu restored without a running core')
                            closed(core_close, self.owner, self.core_source)
                            self.bridge_generation = self.core_source+2
                            self.core_source = None
                            returned_core = True
                        source = self.bridge_generation+3
                        require(observed['source_generation'] == source, 'menu skipped owned core/SD return')
                        result = menu_opening(firmware, self.companion['configuration'], self.owner,
                                              source, self.previous_configuration, self.companion['memory'])
                        self._configured(result)
                        self.menu_source = source
                        self.bridge_generation = source+2
                        firmware = b''
                        opened_menu += 1
                        if op in ('stop', 'quit'):
                            resume_boundary = line
                    else:
                        require(app.quiet(firmware) and observed['source_generation'] == self.menu_source,
                                'menu frame performed unowned firmware work')
                    if op in ('startup', 'focus', 'stop') and ready_line is not None and line > ready_line:
                        ready_presented = True
                else:
                    require(op == 'launch' or op == 'quit' and returned_core and opened_menu == 1,
                            'menu returned outside launch or final park')
                    require(self.menu_source is not None and not terminal and
                            observed['source_generation'] == self.menu_source,
                            'menu return lost its open scope')
                    menu_closed(firmware, self.owner, self.menu_source)
                    self.bridge_generation = self.menu_source+3
                    self.menu_source = None
                    firmware = b''
                    returned_menu += 1
                    final_menu_return = line
                    if first_mutation is None:
                        first_mutation = line
                menus.append(observed)
            elif kind == 'fetch':
                require(op == 'launch' and returned_menu == 1 and self.menu_source is None and
                        not fetches and not terminal, 'fetch outside the selected core launch')
                require(value['owner'] == self.owner and value['generation'] == generation+1 and
                        value['package_id'] == selected['identity']['PackageID'] and
                        value['build_id'] == selected['identity']['BuildID'], 'fetch belongs to another activation')
                if 'fetch_address' in selected:
                    require(value['running']['Address'] == selected['fetch_address'], 'selected fetch address differs')
                source = self.bridge_generation+3
                result = opening(firmware, selected['configuration'], self.owner, source, self.previous_configuration)
                # Check the same full queue observations as a menu load.
                configuration(firmware, selected['configuration'], self.owner, source, self.previous_configuration)
                self._configured(result)
                self.core_source = source
                self.bridge_generation = source+1
                self.loads += 1
                firmware = b''
                fetches.append(value)
                # Launch drains USB through the verified firmware opening.
                # The frontend may print its read-only fetch diagnostic after
                # Resume; that observation does not extend the paused interval.
                resume_boundary = last_firmware
            elif kind == 'app':
                n = value['native']
                phases = ({'ready': 0} if op in ('startup', 'focus') else
                          {'ready': 0, 'loading': 1, 'running': 2} if op == 'launch' else
                          {'running': 0, 'returning': 1, 'ready': 2})
                require(n['State'] in phases and phases[n['State']] >= rank, 'paired App state regressed')
                rank = phases[n['State']]
                expected = generation if op in ('startup', 'focus') or n['State'] in (
                    ('ready', 'loading') if op == 'launch' else ('running', 'returning')) else generation+1
                self._state(value, expected, tuple(phases),
                            selected['identity'] if op == 'launch' else self.view['native']['Identity']
                            if op in ('stop', 'quit') else None)
                if op == 'startup':
                    require(begins == 1 and self.menu_source is not None, 'Ready preceded paired menu/startup')
                if n['State'] == 'running' and op == 'launch':
                    require(len(fetches) == 1 and self.core_source is not None and app.quiet(firmware),
                            'App published Running before verified core opening/fetch')
                if n['State'] == 'ready' and op in ('stop', 'quit'):
                    require(returned_core and opened_menu == 1 and self.menu_source is not None and
                            app.quiet(firmware), 'App published Ready before verified core close/menu restore')
                terminal = (op == 'launch' and n['State'] == 'running' and value['session'] == 'active' or
                            op in ('startup', 'focus', 'stop') and self._ready(value))
                if op in ('startup', 'focus', 'stop') and self._ready(value):
                    if op != 'focus' or value['titles'][value['focus']] == selected['title']:
                        ready_line = line
                        ready_presented = False
                states.append(value)
            elif kind == 'exit':
                require(op == 'quit' and value == generation+2 and returned_core and
                        opened_menu == returned_menu == 1 and self.menu_source is self.core_source is None and
                        app.quiet(firmware), 'final Parked exit omitted core return/menu drain')
                exited = value
        require(app.quiet(firmware), 'unconsumed paired firmware evidence')
        require(begins == int(op == 'startup'), 'paired startup count differs')
        if op == 'startup':
            require(opened_menu == 1 and states and self._ready(states[-1]) and ready_presented,
                    'initial App Ready has no subsequent presented launcher frame')
        elif op == 'focus':
            require(ready_presented and not fetches and not usb_rows and not returned_menu and not opened_menu,
                    'focus omitted presentation or changed device ownership')
        elif op == 'launch':
            require(len(fetches) == returned_menu == 1 and states and terminal, 'incomplete paired launch')
        else:
            require(returned_core and opened_menu == 1 and not fetches and
                    (exited is not None if op == 'quit' else states and terminal and ready_presented),
                    'paired return omitted core close or the presented Ready frame')
        result = dict(states=states, menu_observations=menus, fetch_observations=fetches, exit_generation=exited)
        if self.usb is not None and op != 'startup':
            result['shared_usb'] = self.usb.action(op, generation, usb_rows, self.owner,
                self._raw, app.PREFIX)
            if op != 'focus':
                require(first_mutation is not None and usb_rows[0]['line'] < first_mutation and
                        resume_boundary is not None and usb_rows[1]['line'] > resume_boundary,
                        'shared USB pause/resume crossed paired firmware ownership')
            if op == 'quit':
                require(usb_rows[-1]['line'] > final_menu_return, 'shared USB stopped before final menu drain')
        if states:
            self.view = copy.deepcopy(states[-1])
        return result

    def key(self, op, selection=None):
        require(not self.retired, 'paired App client retired')
        generation = self.view['native']['Generation']
        selected = None
        if op in ('launch', 'focus'):
            require(self._ready(self.view) and type(selection) is int and 0 <= selection < 2,
                    'Ready paired App and selected program required')
            selected = self.programs[selection]
            target = self.view['titles'].index(selected['title'])
            if op == 'launch':
                require(target == self.view['focus'], 'App focus differs from selected activation')
                payload, bound = b'\r', 100
            else:
                require(abs(target-self.view['focus']) == 1, 'one real focus change required')
                payload, bound = (b'\x1b[C' if target > self.view['focus'] else b'\x1b[D'), 5
        elif op in ('stop', 'quit'):
            require(self.view['native']['State'] == 'running' and self.view['session'] == 'active',
                    'paired return requires a live core')
            # Keep core close within 5s. Restoring the independently loaded
            # menu has its original 100s request, followed by 5s to park.
            payload, bound = (b's' if op == 'stop' else b'q'), 105 if op == 'stop' else 110
        else:
            raise ValueError('unsupported paired App action')
        require(self.active.acquire(blocking=False), 'paired App exchange already active')
        entry = dict(action=op, selection=selection, payload_hex=payload.hex(), bound_seconds=bound, status='issued')
        try:
            require(self.owns(), 'paired console claim lost')
            self.records.append(entry)
            self.record(copy.deepcopy(entry))
            before = self.now()
            phases = dict(closed_at=None, restored_at=None, last=before)
            def done(raw):
                require(type(raw) is bytes and len(raw) <= app.MAX_BYTES, 'paired transport capture exceeds bound')
                current = self.now()
                require(phases['last'] <= current < before+bound and self.owns(), 'paired polling clock/deadline/claim failed')
                phases['last'] = current
                if op in ('stop', 'quit'):
                    if phases['closed_at'] is None:
                        require(current < before+5, 'paired core close exceeded original 5s bound')
                        if any(re.fullmatch(rb'(?:NOTICE:[ \t]+)?FES_NANO_MASTER_STOP [^\r\n]+ complete=1 data=held\r?\n', line)
                               for line in raw.splitlines(keepends=True)):
                            phases['closed_at'] = current
                    if phases['closed_at'] is not None and phases['restored_at'] is None:
                        require(current < phases['closed_at']+100, 'paired menu restore exceeded original 100s bound')
                        for line in raw.splitlines(keepends=True):
                            if line.startswith(MENU) and line.endswith(b'\n'):
                                # Framing never commits menu evidence. Full strict
                                # identity/ordering checks still run in _consume.
                                if json.loads(line[len(MENU):]).get('event') == 'frame':
                                    phases['restored_at'] = current
                                    break
                    if op == 'quit' and phases['restored_at'] is not None:
                        require(current < phases['restored_at']+5, 'final menu park exceeded 5s bound')
                if not raw.endswith(b'\n'):
                    return False
                last = raw.splitlines()[-1].rstrip(b'\r')
                if not last.startswith((app.PREFIX, MENU)) and not EXIT.fullmatch(last):
                    return False
                framed = observations(raw, self.input_mode)
                if op == 'quit':
                    return framed[-1]['kind'] == 'exit'
                if op in ('focus', 'stop'):
                    return ready_frame(framed, generation=generation if op == 'focus' else generation+1,
                                       title=selected['title'] if op == 'focus' else None)
                if framed[-1]['kind'] != 'app':
                    return False
                value = framed[-1]['value']
                return value['native']['State'] == 'running' and value['session'] == 'active'
            self._raw = self.exchange(payload, bound, done)
            after = self.now()
            require(before <= after < before+bound and self.owns(), 'paired deadline/clock/claim failed')
            require(done(self._raw), 'paired action lacks a terminal observation')
            result = self._consume(observations(self._raw, self.input_mode), op, generation, selected)
            entry.update(result, status='verified', raw_bytes=len(self._raw))
            self.record(copy.deepcopy(entry))
            if op == 'quit':
                self.menu.finish()
                self.retired = True
            return copy.deepcopy(entry)
        except BaseException as error:
            self.retired = True
            entry.update(status='failed', error=str(error))
            self.record(copy.deepcopy(entry))
            raise
        finally:
            self.active.release()


def exercise(client, settle=lambda _: None, *, during_play=None):
    require(callable(settle) and (during_play is None or callable(during_play)), 'bounded observer hooks required')
    cycles = []
    for index, selection in enumerate((0, 1, 0)):
        if client.view['titles'][client.view['focus']] != client.programs[selection]['title']:
            client.key('focus', selection)
        if client.usb is not None:
            settle(0.5)
        launch = client.key('launch', selection)
        if during_play is not None:
            during_play(index, client)
        if client.usb is not None:
            settle(0.5)
        returned = client.key('quit' if index == 2 else 'stop')
        cycles.append(dict(selection=selection, launch_generation=launch['states'][-1]['native']['Generation'],
                           return_generation=returned['exit_generation'] if index == 2 else
                           returned['states'][-1]['native']['Generation']))
    require(client.loads == 3 and client.retired and cycles[-1]['return_generation'] == 7,
            'paired menu/core cycle incomplete')
    return dict(passed=True, cycles=cycles, menu=client.menu.finish(), display='fpga-menu',
                fetch_activity_observed=True, shared_usb_reports=None if client.usb is None else client.usb.reports,
                input_controls_exercised=False, physical_display_verified=False,
                hardware_acceptance=False, ordinary_admission=False)
