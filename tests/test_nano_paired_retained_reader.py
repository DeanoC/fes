"""Exercise the actual paired launcher auditor; these are not board proofs."""
import copy
import unittest

from test_nano_paired_tenfoot_trace import Model, paired, SPEC, PROGRAMS, OWNER


class PairedRetainedReaderTests(unittest.TestCase):
    def setUp(self):
        self.model = Model()
        self.model.profile['configuration_retained'] = True
        self.route = paired.reader_route._expected(self.model.profile)
        self.model.profile['sdm_reader_route'] = copy.deepcopy(self.route)
        self.marker = (f'NOTICE:  FES_NANO_READER_ROUTE status=0 owner={OWNER} stream=1 '
            'secure=1 aruser=3e0 arprot=3 bank=secure ns_abort=1 source_retained=1 '
            'source_generation=4 generation=5 binding='+paired.reader_route.digest(self.route)+'\n').encode()
        prefix, suffix = paired.split_arm(self.model.initial)
        suffix = suffix.replace(b' gbpa=0 agbpa=0', b' gbpa=100000 agbpa=0')
        suffix = suffix.replace(b'bypass_controls_clear=1', b'bypass_controls_clear=0')
        self.initial = prefix+b''.join(self.marker+line if b'FES_NANO_WINDOW_GATE ' in line
                                      else line for line in suffix.splitlines(keepends=True))
        self.assertEqual(self.initial.count(self.marker), 2)

    def client(self, raw=None, *, profile=None, route=None):
        return paired.PairedTenfootClient(self.initial if raw is None else raw,
            build_record=self.model.profile if profile is None else profile,
            retained_reader_route=self.route if route is None else route,
            build_id=SPEC['build_id'], owner_seed=SPEC['owner_seed'], atf=SPEC['atf'],
            package=SPEC['package'], programs=PROGRAMS, exchange=self.model.exchange,
            owns=lambda: self.model.service.owned, now=lambda: self.model.service.clock)

    def test_first_menu_is_bound_and_another_configuration_rejects_before_exchange(self):
        client = self.client()
        self.assertTrue(client._ready(client.view))
        self.assertEqual((client.menu_source, client.loads), (4, 0))
        self.route['menu_context_sha256'] = 'f'*64
        self.assertNotEqual(client.retained_reader_route, self.route)
        with self.assertRaisesRegex(ValueError, 'another configuration'):
            client.key('launch', 0)
        self.assertEqual(self.model.keys, [])
        self.assertEqual(client.records, [])
        self.assertFalse(client.retired)

    def test_missing_changed_repeated_and_reordered_live_observations_reject(self):
        mutations = [self.initial.replace(self.marker, b'', 1),
            self.initial.replace(self.marker, self.marker*2, 1),
            self.initial.replace(b' gbpa=100000 agbpa=0', b' gbpa=0 agbpa=0', 1),
            self.initial.replace(b's_cr0=0 s_ack=0', b's_cr0=1 s_ack=1', 1),
            self.initial.replace(self.marker, self.marker.replace(b'secure=1', b'secure=0'), 1),
            self.initial.replace(self.marker, self.marker.replace(b'owner='+OWNER.encode(), b'owner=1'), 1),
            self.initial.replace(self.marker, self.marker.replace(b'binding=', b'binding=f'), 1)]
        for raw in mutations:
            with self.subTest(tail=raw[-100:]), self.assertRaises(ValueError):
                self.client(raw)
        self.assertEqual(self.model.keys, [])

    def test_explicit_build_selection_and_all_role_digests_are_required(self):
        for key in self.route:
            if key == 'contract':
                continue
            changed = copy.deepcopy(self.route)
            changed[key] = 'f'*64
            profile = copy.deepcopy(self.model.profile)
            profile['sdm_reader_route'] = changed
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.client(profile=profile, route=changed)
        profile = copy.deepcopy(self.model.profile)
        profile['configuration_retained'] = False
        with self.assertRaises(ValueError):
            self.client(profile=profile)
        with self.assertRaises(ValueError):
            self.model.client(initial=self.initial)
        with self.assertRaises(ValueError):
            self.client(raw=self.model.initial)
        self.assertTrue(self.model.client()._ready(self.model.client().view))
