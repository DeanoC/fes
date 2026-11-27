"""The scoped C64 producer binds the fitted upload and its protected retirement."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from test_nano_mac_handoff_profile import mode, warm_profile

sys.path.insert(0, str(mode.HERE.parent / "nano"))
import c64_scope


def identity():
    return dict(package_id='12'*32, build_id='34'*16, core_size=8192,
                core_sha256='56'*32, core_sha384='78'*48, chunk_sha384=['9a'*48],
                handoff_sha256='bc'*32)


class C64ScopeTests(unittest.TestCase):
    def test_secure_producer_requires_selected_program_and_paired_elf(self):
        profile = warm_profile()
        profile['mac_handoff'] = mode.mac_handoff_contract(profile)
        profile['mac_table_trial'] = mode.mac_table_trial_contract(profile, 0x96080000)
        profile['mac_coexistence_trial'] = mode.mac_coexistence_contract(profile)
        record = identity()
        profile['launcher_ethernet'] = dict(mode.ethernet.contract(), c64_scope=record,
                                            network_scope='published-menu-and-matched-c64')
        with self.assertRaises(ValueError):
            mode.mac_menu_contract(profile)
        profile['mac_c64_scope_supplier'] = mode.mac_c64_scope_contract(profile)
        profile['mac_menu_supplier'] = mode.mac_menu_contract(profile)
        self.assertIn('#define FES_NANO_MAC_C64_SCOPE_SUPPLIER 1', mode.header(profile))
        self.assertFalse(profile['mac_c64_scope_supplier']['hardware_accepted'])
        for key in record:
            changed = copy.deepcopy(profile)
            value = changed['launcher_ethernet']['c64_scope']
            value[key] = ['ab'*48] if key == 'chunk_sha384' else (4096 if key == 'core_size' else 'ab'*(len(record[key])//2))
            with self.subTest(key=key), self.assertRaises(ValueError):
                mode.checked_mac_c64_scope(changed)
        marker = mode.ethernet.c64_marker(record).encode()
        with self.assertRaises(ValueError):
            mode.ethernet.checked({}, marker)
        with self.assertRaises(ValueError):
            mode.ethernet.checked(profile, mode.ethernet.ADAPTER.encode() + marker +
                                  mode.ethernet.c64_marker(dict(record, core_sha256='cd'*32)).encode())

    def test_actual_program_identity_and_secure_sdm_binding(self):
        firmware = mode.HERE
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            (directory/'c64_scope_identity.h').write_text(c64_scope.header(identity()))
            binary = directory/'check'
            subprocess.run(['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                            '-I'+str(directory), '-I'+str(firmware), str(firmware/'nano_program.c'),
                            str(firmware/'tests/c64_scope_source_test.c'), '-o', str(binary)], check=True)
            subprocess.run([binary], check=True)
