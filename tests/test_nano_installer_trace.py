"""Installer framing does not replace media, firmware or lifecycle proofs."""
import copy
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import installer_trace as trace
import tenfoot_trace as app
from test_nano_tenfoot_trace import Model


class InstallerTraceTests(unittest.TestCase):
    def view(self):
        panel = dict(open=False,index=0,rows=[],busy=False,status='')
        return dict(scope='nano-installer-view-v1',owner=17,generation=0,state='ready',
                    settings=dict(open=True,index=1,rows=['layout','systems'],busy=False,status=''),
                    systems=panel)

    def raw(self, record):
        return trace.PREFIX+json.dumps(record,separators=(',',':')).encode()+b'\n'

    def test_navigation_roundtrip_and_cached_owner_comparison(self):
        r = self.view()
        self.assertEqual(trace.record(self.raw(r)),r)
        state = Model().state('ready',0)
        state['native']['Owner'] = 17
        self.assertTrue(trace.matches_state(r,state))
        for key, value in [('owner',18),('generation',1),('state','loading')]:
            self.assertFalse(trace.matches_state(dict(r,**{key:value}),state))

    def test_unknown_duplicate_partial_and_failed_records_reject(self):
        raw = self.raw(self.view())
        for changed in [raw[:-1],raw+raw,raw+b'\n',raw.replace(b'"owner":17',b'"owner":17,"owner":18'),
                        self.raw(dict(self.view(),extra=True)),b'x'*32769]:
            with self.subTest(record=changed[:100]), self.assertRaises(ValueError):
                trace.record(changed)
        for key,value in [('owner',0),('owner',True),('generation',-1),('generation',1<<64),
                          ('state','reboot_required'),('scope','other')]:
            with self.subTest(key=key,value=value), self.assertRaises(ValueError):
                trace.record(self.raw(dict(self.view(),**{key:value})))

    def test_stale_closed_panels_bad_focus_and_oversized_controls_reject(self):
        r = self.view()
        for panel in [dict(r['settings'],index=True),dict(r['settings'],index=2),
                      dict(r['settings'],rows=['x']*65),dict(r['settings'],rows=['x'*129]),
                      dict(r['settings'],rows=['']),dict(r['settings'],status='x'*1025),
                      dict(r['settings'],unknown=True),dict(r['settings'],open=False),
                      dict(r['systems'],busy=True)]:
            with self.subTest(panel=panel), self.assertRaises(ValueError):
                trace.record(self.raw(dict(r,settings=panel)))

    def test_install_has_no_live_core_and_advances_only_one_request(self):
        m = Model()
        for phase,generation in [('ready',0),('loading',0),('loading',1),('ready',1)]:
            state = m.state(phase,generation)
            state['native'].update(Identity=copy.deepcopy(app.ZERO),Held=True)
            parsed = trace.app_state(m.raw(state),generation_origin=0,installing=True)
            self.assertEqual(parsed['native']['Generation'],generation)
        for state in [m.state('running',1),m.state('ready',2),m.state('loading',1)]:
            with self.subTest(state=state['native']), self.assertRaises(ValueError):
                trace.app_state(m.raw(state),generation_origin=0,installing=True)
        state = m.state('ready',0)
        state['native']['Held'] = False
        with self.assertRaises(ValueError):
            trace.app_state(m.raw(state),generation_origin=0,installing=True)

    def test_explicit_completed_install_origin_preserves_actual_counters(self):
        m = Model()
        for phase,generation,source in [('ready',1,0),('loading',1,0),('loading',2,1),('running',2,1),('ready',3,0)]:
            state = m.state(phase,source)
            state['native'].update(State=phase,Generation=generation,Held=phase!='running')
            parsed = trace.app_state(m.raw(state),generation_origin=1)
            self.assertEqual(parsed['native']['Generation'],generation)
        with self.assertRaises(ValueError):
            trace.app_state(m.raw(m.state('ready',1)),generation_origin=0)
        with self.assertRaises(ValueError):
            trace.app_state(m.raw(m.state('ready',0)),generation_origin=1)
        for origin in [True,-1,1<<64,1.0]:
            with self.subTest(origin=origin),self.assertRaises(ValueError):
                trace.app_state(m.raw(m.state('ready',0)),generation_origin=origin)

    def test_post_install_fetch_requires_explicit_origin_and_raw_identity(self):
        m = Model()
        m.service.generation = 2
        raw = m.fetch()[len(app.FETCH):]
        with self.assertRaises(ValueError):
            app.fetch_observation(raw)
        self.assertEqual(app.fetch_observation(raw,generation_origin=1)['generation'],2)
        with self.assertRaises(ValueError):
            app.packet(m.fetch(),fetch_records=[],generation_origin=1,installing=True)


if __name__ == '__main__':
    unittest.main()
