"""The Coleco socket lane must stay separate from the factory producer."""

import tomllib
import unittest
from pathlib import Path

from scripts import build_fes_coleco_oss as factory
from scripts import build_fes_coleco_socket_dev as socket


class ColecoSocketProducerTest(unittest.TestCase):
    def test_separate_authenticated_inputs_and_commands(self):
        self.assertEqual(factory.COLECO_TOOLCHAIN_LOCK, 'toolchains/registered-memory.lock')
        self.assertEqual(socket.TOOLCHAIN_LOCK, 'toolchains/coleco-expansion.lock')
        pin = 'f7370550adb324163ed24e54f7e6756a13569758'
        lock = tomllib.loads((socket.ROOT / socket.TOOLCHAIN_LOCK).read_text())
        self.assertEqual(lock['tool']['nextpnr']['commit'], pin)
        self.assertEqual(socket.TOOL_COMMITS['nextpnr'], pin)
        self.assertIn('cores/fes-coleco/rtl/coleco_expansion_socket.v', socket.RTL_SOURCES)
        self.assertIn('cores/fes-coleco/rtl/coleco_bus_pack.vh', socket.PINNED_INPUTS)
        self.assertIn('scripts/coleco_expansion.py', socket.PINNED_INPUTS)
        commands = socket.build_commands(
            socket.ROOT, socket.ROOT / socket.OUTPUT_RELATIVE, 'a' * 32,
            {'yosys': Path('/bin/true'), 'nextpnr-mistral': Path('/bin/true')})
        self.assertIn('-DFES_COLECO_EXPANSION_DEV=1', commands[0][-1])
        self.assertIn('-I cores/fes-coleco/rtl', commands[0][-1])
        self.assertEqual(commands[1][commands[1].index('--qsf') + 1],
                         'build/fes-coleco-socket-dev/socket.qsf')

    def test_manifest_declares_optional_slot_only_for_socket_candidate(self):
        record = b'{"format":2,"parameters":{},"recipe_sha256":"' + b'a' * 64 + b'"}'
        evidence = {'rbf': {'size': 100, 'sha256': 'b' * 64}, 'build_id': 'c' * 32}
        fields = tomllib.loads(socket.manifest(
            record, evidence, 'https://example.invalid/fes.git', 'd' * 40,
            {'yosys': 'fixture', 'nextpnr-mistral': 'fixture', 'mistral': 'fixture'}).decode())
        self.assertEqual(fields['core']['id'], 'fes.coleco')
        self.assertEqual(fields['core']['version'], '1.1.0')
        self.assertIn({'id': 'fes.expansion.coleco-bus', 'major': 1,
                       'minor': 0, 'required': False}, fields['interfaces'])


if __name__ == '__main__':
    unittest.main()
