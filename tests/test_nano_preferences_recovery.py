"""Settings preservation uses the real recovery owner and scratch-map model."""
import json
import unittest
from unittest.mock import patch

import test_nano_upload as uploads
from preferences_recovery import snapshot_preferences


class PreferencesRecoveryTests(unittest.TestCase):
    def prepare(self, payload, fault=''):
        setup = uploads.NanoUploadTests()
        setup.setUp()
        model, session = setup.session()
        original = model.command
        model.files['FES/PREFS.JSON'] = payload
        def command(value, timeout):
            if value == 'fatls mmc 0:1 FES':
                if fault == 'incomplete-list':
                    return ''
                if payload is None:
                    return '0 file(s), 0 dir(s)\r\n'
                row = f' {len(payload)} prefs.json\r\n'
                return row*(2 if fault == 'duplicate' else 1)+'1 file(s), 0 dir(s)\r\n'
            if value.startswith('fatload') and 'PREFS.JSON' in value:
                model.memory[0x83000000] = payload
                model.allocations[0x83000000] = len(payload)
                return f'{len(payload)} bytes read\r\n'
            if value.startswith('md.b'):
                address, count = [int(v,16) for v in value.split()[1:]]
                data = payload[address-0x83000000:address-0x83000000+count]
                if fault == 'hash-mismatch':
                    data = bytes(v ^ 1 for v in data)
                return ''.join(f'{address+i+(16 if fault == "wrong-address" else 0):08x}: '+
                    ''.join(f'{v:02x} ' for v in data[i:i+16])+'    ascii\r\n'
                    for i in range(0,len(data),16))
            return original(value, timeout)
        model.command = command
        return model, session

    def test_existing_full_allocation_preserved_without_writes(self):
        raw = b'{"home":"library","layout":"grid"}'
        raw += b' '*(65536-len(raw))
        model, session = self.prepare(raw)
        row = {'commands': []}
        with patch('builtins.print'):
            self.assertEqual(snapshot_preferences(session,row), raw)
        self.assertEqual(row['status'], 'verified')
        self.assertEqual(row['payload_bytes'], 65536)
        self.assertTrue(row['present'] and row['fresh_readback'])
        self.assertFalse(row['sd_write_attempted'] or row['activation'] or row['cid_verified'])
        self.assertEqual(session.verified, {})
        self.assertEqual(model.writes, 0)

    def test_absence_is_explicit_and_never_seeds_settings(self):
        model, session = self.prepare(None)
        row = {'commands': []}
        with patch('builtins.print'):
            self.assertIsNone(snapshot_preferences(session,row))
        self.assertEqual(row['status'], 'verified')
        self.assertFalse(row['present'] or row['sd_write_attempted'])
        self.assertFalse(session.retired)
        self.assertFalse(any(c.startswith(('fatwrite','md.b','tftpboot')) for c in model.commands))

    def test_uncertain_or_unbounded_snapshot_retires_the_owner(self):
        for raw,fault in ((b'{}','incomplete-list'), (b'{}','duplicate'),
                          (b' '*65537,''), (b'{}','wrong-address'),
                          (b'{}','hash-mismatch'), (b'[]',''),
                          (b'{"x":NaN}',''), (b'\xff','')):
            with self.subTest(fault=fault, size=len(raw)):
                model, session = self.prepare(raw,fault)
                row = {'commands': []}
                with patch('builtins.print'), self.assertRaises((ValueError,json.JSONDecodeError)):
                    snapshot_preferences(session,row)
                self.assertEqual(row['status'], 'failed')
                self.assertTrue(session.retired)
                self.assertEqual(model.writes,0)
