"""Bounded catalogue installer observer over the paired native verifier.

File/transport admission remains the operator's responsibility. An observed
Install is not persistence proof: a separately admitted fresh boot must discover
the installed entry and verify its independent launch/return.
"""
import copy
import json
import re

import installer_trace as navigation
import paired_tenfoot_trace as paired
from input_action_trace import validate as validate_actions
from service_trace import require


class InstallerClient(paired.PairedTenfootClient):
    def __init__(self, initial, *, programs, installed, initial_titles, **options):
        require(type(programs) is list and 1 <= len(programs) <= 32 and
                type(installed) is list and len(installed) == len(programs) and
                all(type(flag) is bool for flag in installed) and type(initial_titles) is list,
                'explicit bounded source list, installed mask and prior library required')
        require(all(initial_titles.count(p['title']) == int(flag) for p,flag in zip(programs,installed)),
                'target title is missing, duplicated or conflicts with prior library')
        require(len(initial_titles)+installed.count(False) <= 32, 'installed catalogue would exceed its bound')
        self.installed = list(installed)
        self.all_titles = list(initial_titles)
        self.source_selection = None
        self.library_previous_titles = None
        super().__init__(initial, programs=programs, installer=True,
                         initial_titles=initial_titles, **options)
        require(self.navigation is not None and not self.navigation['settings']['open'] and
                not self.navigation['systems']['open'], 'installer startup has uncommanded controls')

    def key(self, op, selection=None, **options):
        require(any(self.installed), 'core operation before verified install or fresh discovery')
        if op in ('launch','focus'):
            require(not self.navigation['settings']['open'] and not self.navigation['systems']['open'],
                    'core operation while installer controls remain open')
            require(type(selection) is int and 0 <= selection < len(self.programs) and self.installed[selection] and
                    self.view['titles'].count(self.programs[selection]['title']) == 1 and self._ready(self.view),
                    'core operation before unambiguous presented library publication')
        return super().key(op, selection, **options)

    def focus(self, selection):
        require(not self.navigation['settings']['open'] and not self.navigation['systems']['open'],
                'focus while installer controls remain open')
        require(type(selection) is int and 0 <= selection < len(self.programs) and self.installed[selection] and
                self._ready(self.view) and self.view['titles'].count(self.programs[selection]['title']) == 1,
                'focus needs an installed unambiguous target')
        for _ in range(len(self.view['titles'])):
            target = self.view['titles'].index(self.programs[selection]['title'])
            current = self.view['focus']
            if target == current:
                return
            self.focus_index(current+(1 if target > current else -1))
        raise ValueError('focus traversal exceeded the presented library bound')

    def _exchange_step(self, op, payload, bound, done, consume):
        require(not self.retired and self.active.acquire(blocking=False), 'installer observer retired or busy')
        entry = dict(action=op, payload_hex=payload.hex(), bound_seconds=bound, status='issued')
        try:
            require(self.owns(), 'installer console claim absent')
            self.records.append(entry)
            self.record(copy.deepcopy(entry))
            before = previous = self.now()
            observed = b''
            def bounded(raw):
                nonlocal previous, observed
                now = self.now()
                require(before <= previous <= now < before+bound and self.owns(),
                        'installer clock/deadline/claim failed')
                previous = now
                require(type(raw) is bytes and len(raw) <= paired.app.MAX_BYTES,
                        'installer capture exceeds bound')
                require(raw.startswith(observed), 'installer transport replaced or truncated its growing capture')
                observed = raw
                for line in raw.splitlines(keepends=True):
                    if not line.endswith(b'\n'):
                        continue
                    if line.startswith((paired.app.PREFIX,paired.MENU,navigation.PREFIX,paired.app.FETCH)):
                        continue  # Typed JSON has its own validator and may contain display text.
                    text = line.decode('utf-8')
                    require(not text.startswith(('panic:','fatal error:')) and
                            not re.search(r'\b(?:FAIL|FAILURE|REJECT|EXCEPTION)\b',text),
                            'native failure during installer exchange')
                return done(raw, now, before) if raw.endswith(b'\n') else False
            self._raw = self.exchange(payload, bound, bounded)
            require(bounded(self._raw), 'installer exchange lacks its terminal observation')
            after = self.now()
            require(before <= after < before+bound and self.owns(), 'installer completion clock/deadline/claim failed')
            result = consume(self._raw)
            entry.update(result, status='verified', raw_bytes=len(self._raw), elapsed_seconds=after-before)
            self.record(copy.deepcopy(entry))
            return copy.deepcopy(entry)
        except BaseException as error:
            self.retired = True
            entry.update(status='failed', error=str(error))
            self.record(copy.deepcopy(entry))
            raise
        finally:
            self.active.release()

    def _navigation_key(self, payload, target):
        require(payload in (b'o',b'\r',b'\x1b[B') and callable(target), 'closed installer key set required')
        require(self.view['native']['State'] == 'ready' and self.navigation is not None and
                not target(self.navigation), 'navigation requires Ready and a real view change')
        generation = self.view['native']['Generation']
        def done(raw, now, before):
            if not raw.splitlines()[-1].startswith((paired.app.PREFIX,paired.MENU,navigation.PREFIX)):
                return False
            rows = self._observations(raw)
            views = [r for r in rows if r['kind'] == 'installer']
            states = [r for r in rows if r['kind'] == 'app']
            state = states[-1]['value'] if states else self.view
            require(state['native']['Owner'] == self.owner and state['native']['Generation'] == generation and
                    state['native']['State'] == 'ready', 'navigation changed native ownership')
            return (rows[-1]['kind'] == 'menu' and views and target(views[-1]['value']) and
                    navigation.matches_state(views[-1]['value'],state) and
                    rows[-1]['line'] > max(views[-1]['line'],states[-1]['line'] if states else 0) and
                    self._ready(state))
        def consume(raw):
            rows = self._observations(raw)
            state = self.view
            views, actions, frames = [], [], []
            for row in rows:
                kind, value = row['kind'], row['value']
                if kind == 'app':
                    self._state(value,generation,('ready',), catalogue_titles=
                        self.library_previous_titles if self.library_previous_titles is not None and
                        value['titles'] == self.library_previous_titles else None)
                    state = value
                elif kind == 'installer':
                    require(navigation.matches_state(value,state), 'navigation has no matching App state')
                    views.append(value)
                elif kind == 'menu':
                    observed = self.menu.feed(value)
                    require(observed['event'] == 'frame' and observed['source_generation'] == self.menu_source,
                            'navigation changed the owned menu')
                    frames.append(observed)
                elif kind == 'input_action':
                    actions.append(value)
                else:
                    require(kind == 'firmware' and paired.app.quiet(value),
                            'navigation performed unowned firmware/input work')
            require(views and frames and target(views[-1]), 'navigation target not presented')
            if payload == b'\r':
                validate_actions(actions,'launch',self.owner,generation)
            else:
                validate_actions(actions,'focus',self.owner,generation)
            self.view = copy.deepcopy(state)
            self.navigation = copy.deepcopy(views[-1])
            return dict(installer_views=views,menu_observations=frames,input_actions=actions)
        return self._exchange_step('navigate',payload,5,done,consume)

    def _focus_row(self, panel, kind, occurrence=0):
        view = self.navigation[panel]
        require(view['open'] and not view['busy'] and type(occurrence) is int and 0 <= occurrence < view['rows'].count(kind),
                'explicit presented installer row required')
        expected = list(view['rows'])
        target = [i for i,row in enumerate(expected) if row == kind][occurrence]
        for _ in range(len(expected)):
            view = self.navigation[panel]
            if view['index'] == target:
                return
            index = (view['index']+1)%len(expected)
            self._navigation_key(b'\x1b[B',lambda v: v[panel]['open'] and not v[panel]['busy'] and
                v[panel]['rows'] == expected and v[panel]['index'] == index)
        raise ValueError('installer row traversal exceeded its presented bound')

    def open_source(self, selection=0):
        require(type(selection) is int and 0 <= selection < len(self.programs), 'explicit source selection required')
        require(self.navigation is not None and not self.navigation['settings']['open'] and
                not self.navigation['systems']['open'], 'source navigation needs closed presented controls')
        self._navigation_key(b'o',lambda v: v['settings']['open'] and not v['settings']['busy'] and not v['systems']['open'])
        self._focus_row('settings','systems')
        self._navigation_key(b'\r',lambda v: v['systems']['open'] and not v['systems']['busy'] and
                             v['systems']['rows'] == ['refresh']+['system']*len(self.programs))
        self._focus_row('systems','system',selection)
        self._navigation_key(b'\r',lambda v: v['systems']['open'] and not v['systems']['busy'] and
            ('library' in v['systems']['rows'] if self.installed[selection] else v['systems']['rows'] == ['install']))
        self.source_selection = selection

    def open_library(self):
        require(self.source_selection is not None and self.installed[self.source_selection],
                'library publication before installation/discovery')
        self._focus_row('systems','library')
        self.library_previous_titles = list(self.view['titles'])
        self.catalogue_titles = list(self.all_titles)
        try:
            self._navigation_key(b'\r',lambda v: not v['systems']['open'] and not v['settings']['open'])
        finally:
            self.library_previous_titles = None

    def install(self):
        require(self.source_selection is not None and not self.installed[self.source_selection] and self._ready(self.view),
                'installation requires a selected uninstalled source and Ready library')
        origin = self.view['native']['Generation']
        prior_titles = list(self.view['titles'])
        panel = self.navigation['systems']
        require(panel['open'] and not panel['busy'] and panel['rows'] == ['install'] and panel['index'] == 0,
                'Install control was not independently presented')
        # Compose the existing limits: 5s key dispatch, 5s USB pause, 100s
        # work, 100s independent menu restore, 5s USB resume and 5s UI frame.
        # No native operation's timeout is extended by this collector.
        phases = dict(action=None,work=None,restore=None,frame=None,resume=None)
        def rows_for(raw):
            return paired.observations(raw,self.input_mode,installer=True,generation_origin=origin,installing=True)
        def done(raw, now, before):
            lines = raw.splitlines(keepends=True)
            arms = [i for i,line in enumerate(lines) if line.endswith(b'\n') and paired.ARM.match(line)]
            actions = [paired.input_action(line.rstrip(b'\r\n'),i+1,self.input_mode)
                       for i,line in enumerate(lines) if line.endswith(b'\n') and line.startswith(paired.INPUT_ACTION)]
            require(len(actions) <= 1, 'installation duplicated its input action')
            if phases['action'] is None:
                require(now < before+5, 'installer key dispatch exceeded 5s bound')
                if actions:
                    validate_actions(actions,'launch',self.owner,origin)
                    phases['action'] = now
            usb = [paired.usb_record(line.rstrip(b'\r\n'),i+1) for i,line in enumerate(lines)
                   if line.endswith(b'\n') and line.startswith(paired.USB)]
            pauses = [r for r in usb if r['stage'] == 'input' and r['values']['phase'] == 'pause']
            resumes = [r for r in usb if r['stage'] == 'input' and r['values']['phase'] == 'resume']
            if phases['work'] is None and phases['action'] is not None:
                require(now < phases['action']+5, 'installer USB pause exceeded 5s bound')
                if self.usb is None or pauses:
                    phases['work'] = now
            if phases['restore'] is None:
                if phases['work'] is not None:
                    require(now < phases['work']+100, 'installation work exceeded its existing 100s bound')
                if arms:
                    require(phases['work'] is not None, 'menu restore preceded installer input pause')
                    phases['restore'] = now
            if phases['restore'] is not None:
                require(now < phases['restore']+100, 'installer menu restoration exceeded 100s')
                if phases['frame'] is None and any(i > arms[0] and line.startswith(paired.MENU) and
                        line.endswith(b'\n') and json.loads(line[len(paired.MENU):]).get('event') == 'frame'
                        for i,line in enumerate(lines)):
                    phases['frame'] = now
            if phases['frame'] is not None and phases['resume'] is None:
                require(now < phases['frame']+5, 'installer USB resume exceeded 5s bound')
                if self.usb is None or resumes:
                    phases['resume'] = now
            if phases['resume'] is not None:
                require(now < phases['resume']+5, 'installer completed view exceeded presentation bound')
            if not lines[-1].startswith((paired.app.PREFIX,paired.MENU,navigation.PREFIX)):
                return False
            rows = rows_for(raw)
            states = [r for r in rows if r['kind'] == 'app']
            views = [r for r in rows if r['kind'] == 'installer']
            return (rows[-1]['kind'] == 'menu' and states and views and
                    states[-1]['value']['native']['State'] == 'ready' and
                    states[-1]['value']['native']['Generation'] == origin+1 and self._ready(states[-1]['value']) and
                    navigation.matches_state(views[-1]['value'],states[-1]['value']) and
                    views[-1]['value']['systems']['open'] and not views[-1]['value']['systems']['busy'] and
                    'library' in views[-1]['value']['systems']['rows'] and
                    rows[-1]['line'] > max(states[-1]['line'],views[-1]['line']))
        def consume(raw):
            rows = rows_for(raw)
            firmware = b''
            state = self.view
            states, views, menus, actions, usb = [], [], [], [], []
            returned = opened = 0
            rank = -1
            generation = origin
            first_mutation = restored_line = completed_line = None
            for row in rows:
                kind, value, line = row['kind'], row['value'], row['line']
                if kind == 'firmware':
                    require(rank < 2 or paired.app.quiet(value), 'firmware followed published Ready')
                    firmware += value
                    if not paired.app.quiet(value) and first_mutation is None:
                        first_mutation = line
                elif kind == 'usb':
                    usb.append(value)
                elif kind == 'input_action':
                    require(first_mutation is None and not usb, 'Install action followed mutation/input pause')
                    actions.append(value)
                elif kind == 'app':
                    n = value['native']
                    require(n['Owner'] == self.owner and n['Generation'] >= generation and value['titles'] == prior_titles,
                            'installation changed owner/catalogue or regressed generation')
                    generation = n['Generation']
                    phase = 1 if n['State'] == 'loading' else 2 if generation == origin+1 else 0
                    require(phase >= rank, 'installation App state regressed')
                    rank = phase
                    if phase == 2:
                        require(returned == opened == 1 and self.menu_source is not None and paired.app.quiet(firmware),
                                'installation published Ready before owned menu restoration')
                    state = value
                    states.append(value)
                elif kind == 'installer':
                    require(navigation.matches_state(value,state), 'installer view lost App ownership')
                    views.append(value)
                    if value['state'] == 'ready' and value['generation'] == origin+1 and not value['systems']['busy'] and 'library' in value['systems']['rows']:
                        completed_line = line
                elif kind == 'menu':
                    observed = self.menu.feed(value)
                    if observed['event'] == 'return':
                        require(returned == opened == 0 and self.menu_source is not None,
                                'installation returned an unowned or replacement menu')
                        paired.menu_closed(firmware,self.owner,self.menu_source)
                        self.bridge_generation = self.menu_source+3
                        self.menu_source = None
                        firmware = b''
                        returned += 1
                    elif self.menu_source is None:
                        require(returned == 1 and opened == 0, 'installer restored menu before closing its predecessor')
                        source = self.bridge_generation+3
                        require(observed['source_generation'] == source, 'installer omitted SD return/containment')
                        result = paired.menu_opening(firmware,self.companion['configuration'],self.owner,
                            source,self.previous_configuration,self.companion['memory'])
                        self._configured(result)
                        self.menu_source = source
                        self.bridge_generation = source+2
                        firmware = b''
                        opened += 1
                        restored_line = line
                    else:
                        require(observed['source_generation'] == self.menu_source and paired.app.quiet(firmware),
                                'installer frame changed menu or performed unowned work')
                    menus.append(observed)
                else:
                    raise ValueError('unexpected evidence during installation')
            validate_actions(actions,'launch',self.owner,origin)
            require(returned == opened == 1 and completed_line is not None and states and
                    states[-1]['native']['Generation'] == origin+1 and self._ready(states[-1]) and paired.app.quiet(firmware),
                    'incomplete owned installation')
            result = dict(states=states,installer_views=views,menu_observations=menus,input_actions=actions,
                          persistence_verified=False,hardware_acceptance=False)
            if self.usb is not None:
                result['shared_usb'] = self.usb.action('install',origin,usb,self.owner,raw,paired.app.PREFIX)
                require(first_mutation is not None and usb[0]['line'] < first_mutation and
                        usb[1]['line'] > restored_line and usb[1]['line'] < completed_line,
                        'installer USB pause/resume crossed firmware or completed setup ownership')
            else:
                require(not usb, 'unexpected USB scope in UART-only installation')
            self.view = copy.deepcopy(states[-1])
            self.navigation = copy.deepcopy(views[-1])
            return result
        result = self._exchange_step('install',b'\r',220,done,consume)
        self.installed[self.source_selection] = True
        self.all_titles.append(self.programs[self.source_selection]['title'])
        self.generation_origin += 1
        return result
