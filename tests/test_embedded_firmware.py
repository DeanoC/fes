"""The frozen endpoint contract and source-patch admission need no hardware."""
import importlib.util
import io
import hashlib
import json
from pathlib import Path
import re
import struct
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
PATH = ROOT / 'platform/embedded/firmware/prepare_tfa.py'
spec = importlib.util.spec_from_file_location('contained_tfa', PATH)
prepare = importlib.util.module_from_spec(spec)
spec.loader.exec_module(prepare)
build_spec = importlib.util.spec_from_file_location('terasic_tfa_build', PATH.parent / 'build_terasic.py')
builder = importlib.util.module_from_spec(build_spec)
ram_spec=importlib.util.spec_from_file_location('test_firmware_ram_module',PATH.parent/'nano_firmware_ram.py')
ram=importlib.util.module_from_spec(ram_spec);ram_spec.loader.exec_module(ram)
with patch.dict(sys.modules, {'prepare_tfa': prepare,'nano_firmware_ram':ram}):
    build_spec.loader.exec_module(builder)
uboot_spec = importlib.util.spec_from_file_location('terasic_uboot_build', PATH.parent / 'build_terasic_uboot.py')
uboot = importlib.util.module_from_spec(uboot_spec)
with patch.dict(sys.modules, {'prepare_tfa': prepare, 'build_terasic': builder}):
    uboot_spec.loader.exec_module(uboot)


class EmbeddedFirmwareTests(unittest.TestCase):
    def test_unreviewed_tfa_source_is_refused(self):
        for source in (b'', b'not TF-A', b'\0'*65536):
            with self.subTest(length=len(source)), self.assertRaises(ValueError):
                prepare.contained_source(source)
            with self.subTest(reset_length=len(source)), self.assertRaises(ValueError):
                prepare.contained_terasic_reset(source)

    def test_vendor_snapshot_extraction_rejects_unsafe_members(self):
        def archive(name, kind=tarfile.REGTYPE):
            stream = io.BytesIO()
            with tarfile.open(fileobj=stream, mode='w') as tar:
                item = tarfile.TarInfo(name)
                item.type = kind
                if kind == tarfile.REGTYPE:
                    item.size = 3
                    tar.addfile(item, io.BytesIO(b'fes'))
                else:
                    item.linkname = '../outside'
                    tar.addfile(item)
            return stream.getvalue()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            builder.extract_archive(archive('source/safe.c'), root)
            self.assertEqual((root / 'source/safe.c').read_bytes(), b'fes')
            for name, kind in (('../outside', tarfile.REGTYPE), ('/absolute', tarfile.REGTYPE),
                               ('link', tarfile.SYMTYPE), ('hardlink', tarfile.LNKTYPE)):
                with self.subTest(name=name), self.assertRaises(ValueError):
                    builder.extract_archive(archive(name, kind), root)

    def test_uboot_admits_only_the_recorded_vendor_firmware_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            component = root / 'source/build/agilex5/release/bl31.bin'
            component.parent.mkdir(parents=True)
            component.write_bytes(b'example BL31')
            elf = component.parent / 'bl31/bl31.elf'
            elf.parent.mkdir()
            image = bytearray(120)
            image[:6] = b'\x7fELF\x02\x01'
            struct.pack_into('<H', image, 18, 183)
            struct.pack_into('<Q', image, 24, 0x80000000)
            struct.pack_into('<Q', image, 32, 64)
            struct.pack_into('<HH', image, 54, 56, 1)
            struct.pack_into('<IIQQQQQQ', image, 64, 1, 5, 0, 0x80000000, 0x80000000, len(image), 0x1000, 0x1000)
            elf.write_bytes(image)
            inputs = (PATH.parent / 'build_terasic.py', PATH, PATH.parent / 'generated/fes_embedded_control.hpp',
                      prepare.DIAG_HEADER, prepare.DIAG_HELPERS,
                      PATH.parent/'nano_firmware_ram.py',PATH.parent/'nano_firmware_entry.inc',PATH.parent/'nano_firmware_ram.h')
            receipt = {'status': 'built', 'bridge_mode': 'strict', 'reference_revision': prepare.TERASIC_REVISION,
                       'reference_git_blob': prepare.TERASIC_BLOB, 'reset_reference_git_blob': prepare.TERASIC_RESET_BLOB,
                       'bl31_bin_size': component.stat().st_size,
                       'bl31_bin_sha256': hashlib.sha256(component.read_bytes()).hexdigest(),
                       'bl31_elf_sha256': hashlib.sha256(image).hexdigest(), 'bl31_entry': 0x80000000,
                       'bl31_load_segments': [[0x80000000, 0x80001000]],
                       'producer_sources': {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in inputs}}
            (root / 'result.json').write_text(json.dumps(receipt))
            self.assertEqual(uboot.checked_bl31(root)[4], b'example BL31')
            for mode in (None, 'owned-unready', 'unknown'):
                receipt['bridge_mode'] = mode
                (root / 'result.json').write_text(json.dumps(receipt))
                with self.subTest(mode=mode), self.assertRaises(ValueError):
                    uboot.checked_bl31(root)
            receipt['bridge_mode'] = 'strict'
            (root / 'result.json').write_text(json.dumps(receipt))
            component.write_bytes(b'changed BL31')
            with self.assertRaises(ValueError):
                uboot.checked_bl31(root)
            component.write_bytes(b'example BL31')
            elf.write_bytes(image + b'changed')
            with self.assertRaises(ValueError):
                uboot.checked_bl31(root)
            elf.write_bytes(image)
            receipt['reference_revision'] = '0' * 40
            (root / 'result.json').write_text(json.dumps(receipt))
            with self.assertRaises(ValueError):
                uboot.checked_bl31(root)

    def test_uboot_initial_load_rejects_bl31_memory_overlap(self):
        uboot.check_uboot_load([(0x80000000, 0x80200000)], 0x1000)
        uboot.check_uboot_load([(0x80201000, 0x80202000)], 0x1000)
        for segment in ((0x80000000, 0x80200001), (0x80200fff, 0x80202000)):
            with self.subTest(segment=segment), self.assertRaises(ValueError):
                uboot.check_uboot_load([segment], 0x1000)
        with self.assertRaises(ValueError):
            uboot.check_uboot_load([], 0x40000000)

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
