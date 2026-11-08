"""Unchanged physical UART bytes; replay only, never a hardware pass."""
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

FIXTURE = ROOT/'tests/fixtures/nano-paired-tenfoot/captured-launch'


class CapturedLaunchTests(unittest.TestCase):
    def replay(self, mutate=None):
        meta = json.loads((FIXTURE/'contract.json').read_text())
        data = {}
        for name, expected in meta['files'].items():
            data[name] = (FIXTURE/name).read_bytes()
            self.assertEqual(len(data[name]), expected['bytes'])
            self.assertEqual(hashlib.sha256(data[name]).hexdigest(), expected['sha256'])
        self.assertFalse(meta['source']['original_attempt_passed'])
        clock = [0.0]
        sent = []

        def exchange(payload, bound, done):
            self.assertEqual((payload, bound), (b'\r', 100))
            sent.append(payload)
            response = data['launch.uart']
            if mutate is None:
                for offer in meta['offers']:
                    clock[0] = offer['elapsed_seconds']
                    done(response[:offer['end']])
            else:
                response = mutate(response)
                clock[0] = meta['elapsed_seconds']
                done(response)
            clock[0] = meta['elapsed_seconds']
            return response

        keys = ('build_record', 'build_id', 'owner_seed', 'atf', 'package',
                'programs', 'shared_usb', 'firmware_ram_trace')
        client = paired.PairedTenfootClient(data['initial.uart'],
            **{key: copy.deepcopy(meta[key]) for key in keys},
            exchange=exchange, owns=lambda: True, now=lambda: clock[0])
        return client, sent, data['launch.uart']

    def test_captured_launch_resumes_after_firmware_before_fetch(self):
        client, sent, response = self.replay()
        rows = paired.observations(response, client.input_mode)
        firmware = [r for r in rows if r['kind'] == 'firmware' and not paired.app.quiet(r['value'])]
        usb = [r for r in rows if r['kind'] == 'usb']
        fetch = next(r for r in rows if r['kind'] == 'fetch')
        self.assertLess(usb[0]['line'], firmware[0]['line'])
        self.assertLess(firmware[-1]['line'], usb[1]['line'])
        self.assertLess(usb[1]['line'], fetch['line'])
        result = client.key('launch', 0)
        self.assertEqual(sent, [b'\r'])
        self.assertFalse(client.retired)
        self.assertEqual(client.view['native']['State'], 'running')
        self.assertEqual(client.view['native']['Generation'], 1)
        self.assertEqual(client.view['session'], 'active')
        self.assertEqual(len(result['fetch_observations']), 1)
        self.assertGreater(result['fetch_observations'][0]['delta'], 0)

    def alter(self, raw, fault):
        lines = raw.splitlines(keepends=True)
        resume = next(line for line in lines if line.startswith(paired.USB+b'INPUT phase=resume '))
        pause = next(line for line in lines if line.startswith(paired.USB+b'INPUT phase=pause '))
        if fault in ('resume-before-last-firmware', 'resume-before-configuration',
                     'resume-after-fetch', 'resume-after-completion'):
            lines.remove(resume)
            if fault == 'resume-before-last-firmware':
                at = next(i for i, line in enumerate(lines) if b'FES_NANO_DDR_FIREWALL_END ' in line)
            elif fault == 'resume-before-configuration':
                at = next(i for i, line in enumerate(lines) if paired.ARM.match(line))
            elif fault == 'resume-after-fetch':
                at = next(i for i, line in enumerate(lines) if line.startswith(paired.app.FETCH))+1
            else:
                # Keep complete framing while an earlier terminal App record
                # exposes that this resume arrived too late.
                lines.append(resume)
                lines.append(lines[-2])
                return b''.join(lines)
            lines.insert(at, resume)
        elif fault == 'pause-after-mutation':
            lines.remove(pause)
            at = next(i for i, line in enumerate(lines) if b'FES_NANO_WINDOW_SCOPE phase=close ' in line)
            lines.insert(at+1, pause)
        elif fault == 'missing-resume':
            lines.remove(resume)
        elif fault == 'duplicate-resume':
            lines.insert(lines.index(resume), resume)
        elif fault == 'wrong-owner':
            lines[lines.index(resume)] = re.sub(rb'owner=[0-9a-f]+', b'owner=1', resume)
        elif fault == 'wrong-lease':
            lines[lines.index(resume)] = resume.replace(b' generation=2 ', b' generation=3 ')
        elif fault == 'wrong-native-generation':
            lines[lines.index(resume)] = resume.replace(b'native_generation=1 ', b'native_generation=2 ')
        elif fault == 'reports-during-pause':
            lines[lines.index(resume)] = re.sub(rb'reports=[0-9]+', b'reports=312', resume)
        else:
            raise AssertionError(fault)
        return b''.join(lines)

    def test_fetch_diagnostic_does_not_define_the_resume_boundary(self):
        client, sent, _ = self.replay(lambda raw: self.alter(raw, 'resume-after-fetch'))
        client.key('launch', 0)
        self.assertEqual(sent, [b'\r'])
        self.assertEqual(client.view['session'], 'active')

    def test_captured_ownership_faults_retire_without_another_send(self):
        for fault in ('resume-before-last-firmware', 'resume-before-configuration',
                      'resume-after-completion', 'pause-after-mutation',
                      'missing-resume', 'duplicate-resume', 'wrong-owner',
                      'wrong-lease', 'wrong-native-generation', 'reports-during-pause'):
            with self.subTest(fault=fault):
                client, sent, _ = self.replay(lambda raw: self.alter(raw, fault))
                with self.assertRaises(ValueError):
                    client.key('launch', 0)
                self.assertTrue(client.retired)
                with self.assertRaises(ValueError):
                    client.key('launch', 0)
                self.assertEqual(sent, [b'\r'])


if __name__ == '__main__':
    unittest.main()
