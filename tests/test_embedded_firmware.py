"""The frozen endpoint contract and source-patch admission need no hardware."""
import importlib.util
import json
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
PATH = ROOT / 'platform/embedded/firmware/prepare_tfa.py'
spec = importlib.util.spec_from_file_location('contained_tfa', PATH)
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)


class EmbeddedFirmwareTests(unittest.TestCase):
    def test_unreviewed_tfa_source_is_refused(self):
        for source in (b'', b'not TF-A', b'\0'*65536):
            with self.subTest(length=len(source)), self.assertRaises(ValueError):
                prepare.contained_source(source)

    def test_shared_contract_matches_frozen_oracle(self):
        oracle = json.loads((ROOT/'sources/mister-packages/testdata/oracles/fes-embedded-control.json').read_text())
        header = (ROOT/'platform/embedded/firmware/generated/fes_embedded_control.hpp').read_text()
        actual = {name:int(value,16) for name,value in re.findall(r'constexpr std::uint32_t (\w+) = 0x([0-9a-f]+)u;',header)}
        self.assertEqual(actual,oracle['constants'])
        go_source = (ROOT/'platform/embedded/tamago/embeddedcontrol/generated/fes_embedded_control.go').read_text()
        go_constants = {name:int(value,16) for name,value in re.findall(r'(FesEmbedded\w+)\s+uint32\s+=\s+0x([0-9a-f]+)', go_source)}
        self.assertEqual(go_constants, oracle['constants'])
        self.assertEqual(oracle['source']['commit'],'c5189ef504b01276651ee531c81d23db90ff7b1a')
        registers=[value for name,value in actual.items() if name.startswith('FesEmbeddedReg')]
        self.assertEqual(len(registers),len(set(registers)))
        self.assertTrue(all(o%4==0 and 0<=o<0x50 for o in registers))
