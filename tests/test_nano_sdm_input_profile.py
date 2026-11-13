"""Native input/retirement selection and source binding, without a board."""
import copy
import hashlib
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"
sys.path[:0] = [str(FW), str(ROOT / "platform/embedded/nano")]
import nano_sdm_input_profile as mode
import fit_layout


def profile():
    return dict(chain_sha256="1" * 64, firmware_mmu={}, firmware_ram={},
                usb_shared=True, sd_pio=True, capability_observation={"format": 2},
                hps_menu_program={"fixture": True}, hps_program_platform={"fixture": True},
                cpu_mapping=dict(ram_base=0x84000000, ram_bytes=0x10000000,
                                 dma_base=0x94800000, dma_bytes=0x800000,
                                 reader_base=0x95000000, reader_bytes=0x800000),
                hps_configuration={"base": 0x94800000}, boot_fit=dict(fit_layout.CONTRACT),
                sdm_input_transfer=dict(mode.CONTRACT))


class InputProfileTests(unittest.TestCase):
    def test_selection_is_explicit_and_exclusive(self):
        self.assertIsNone(mode.selected(None, False))
        self.assertIsNone(mode.checked(None))
        self.assertEqual(mode.checked(profile()), mode.CONTRACT)
        for value in (0, 1, None, "yes", {}, []):
            with self.subTest(value=value), self.assertRaises(ValueError):
                mode.selected(profile(), value)
        for name in ("sdm_arm_probe", "sdm_fault64"):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "exclusive"):
                mode.selected(dict(profile(), **{name: {}}), True)

    def test_mutated_contract_and_incomplete_ownership_are_rejected(self):
        for name in mode.CONTRACT:
            p = profile()
            p["sdm_input_transfer"][name] = "foreign"
            with self.subTest(contract=name), self.assertRaises(ValueError):
                mode.checked(p)
        for name in ("firmware_mmu", "firmware_ram", "usb_shared", "sd_pio",
                     "capability_observation", "hps_menu_program", "hps_program_platform", "boot_fit"):
            p = profile()
            del p[name]
            with self.subTest(missing=name), self.assertRaises(ValueError):
                mode.selected(p, True)
        for name in profile()["cpu_mapping"]:
            p = profile()
            p["cpu_mapping"][name] += 4096
            with self.subTest(memory=name), self.assertRaises(ValueError):
                mode.selected(p, True)

    def test_disabled_headers_keep_diagnostics_off(self):
        for name in ("FES_NANO_SDM_INPUT_TRANSFER", "FES_NANO_RETIRE_TIMING", "FES_NANO_RETIRE_BATCHED_OBSERVE"):
            self.assertIn("#define " + name + " 0\n", mode.header(None))
            self.assertIn("#define " + name + " 1\n", mode.header(profile()))
        self.assertEqual(mode.CONTRACT["retirement_bound_us"], 1000000)
        self.assertFalse(mode.CONTRACT["retirement_memory_reuse"])

    def fixture(self, path):
        names = (mode.SERVICE, mode.MAILBOX, mode.MAILBOX_HEADER, mode.PLATFORM,
                 mode.LD, mode.MK, mode.HEADER)
        for name in names:
            (path / name).parent.mkdir(parents=True, exist_ok=True)
        for target, source in ((mode.SERVICE, "service-before.c"),
                               (mode.MAILBOX, "mailbox-before.c"),
                               (mode.MAILBOX_HEADER, "mailbox-before.h")):
            (path / target).write_bytes((ROOT / "tests/fixtures/nano-sdm-input" / source).read_bytes())
        (path / mode.PLATFORM).write_text("/* fixture */\n")
        (path / mode.MK).write_text("BL31_SOURCES += existing.c\n")
        (path / mode.LD).write_text("MEMORY {\n}\nSECTIONS {\n    /DISCARD/ : {\n}\n}\n")
        (path / mode.HEADER).write_text(mode.header(profile()) + "#define FES_NANO_SDM_FAULT64 0\n#define FES_NANO_SDM_ARM_PROBE 0\n")
        return (dict(generated_sha256=mode.INPUT_SERVICE, mailbox_generated_sha256=mode.INPUT_MAILBOX),
                dict(sources={}), dict(sources={str(mode.PLATFORM): "fixture"}),
                dict(generated_sha256=mode.INPUT_MAILBOX, header_sha256=mode.INPUT_HEADER))

    def test_install_binds_every_source_and_refuses_reuse(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            args = self.fixture(path)
            binding = mode.install(path, profile(), *args)
            mode.verify_installed(path, binding, profile())
            self.assertEqual(set(binding["sources"]), set(mode.NAMES))
            for name in mode.NAMES:
                target = path / name
                original = target.read_bytes()
                target.write_bytes(original + b"changed")
                with self.subTest(source=name), self.assertRaises(ValueError):
                    mode.verify_installed(path, binding, profile())
                target.write_bytes(original)
            with self.assertRaises(ValueError):
                mode.install(path, profile(), *args)

    def test_invalid_predecessor_rejected_before_writes(self):
        for case in range(5):
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory)
                args = list(self.fixture(path))
                if case == 0:
                    args[0]["generated_sha256"] = "0" * 64
                elif case == 1:
                    args[2]["sources"] = {}
                elif case == 2:
                    args[3]["header_sha256"] = "0" * 64
                else:
                    (path / (mode.MAILBOX if case == 3 else mode.MAILBOX_HEADER)).write_text("foreign")
                before = {str(p.relative_to(path)): p.read_bytes() for p in path.rglob("*") if p.is_file()}
                with self.subTest(case=case), self.assertRaises(ValueError):
                    mode.install(path, profile(), *args)
                self.assertEqual(before, {str(p.relative_to(path)): p.read_bytes() for p in path.rglob("*") if p.is_file()})
