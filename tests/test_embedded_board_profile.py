"""Firmware handoff admission must fail before an unsafe image is emitted."""
import copy
import importlib.util
from pathlib import Path
import unittest

PATH = Path(__file__).resolve().parents[1] / "platform/embedded/tamago/build.py"
spec = importlib.util.spec_from_file_location("embedded_board_build", PATH)
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


def profile():
    return {"schema": 1, "firmware": "synthetic test; not a board recipe",
            "ram_start": 0x82000000, "ram_size": 256 << 20, "entry": 0x82010000,
            "reserved": [{"start": 0x80000000, "size": 32 << 20, "owner": "synthetic firmware"}],
            "sd_sectors": 8192, "sd_rca": 7,
            "handoff": {key: True for key in ("el1", "single_cpu", "counter_access", "exclusive_devices",
                "uart0_configured", "sdhc_selected", "mmu_off_caches_clean")}}


class BoardProfileTests(unittest.TestCase):
    def test_explicit_profile(self):
        self.assertEqual(build.profile_constants(profile()), (0x82000000, 256 << 20, 0x82010000, 8192, 7))

    def test_bad_handoff(self):
        for mutate in (
            lambda p: p.update(ram_start=0x40000000),
            lambda p: p.update(ram_size=2 << 30),
            lambda p: p.update(entry=0x82000000),
            lambda p: p.update(ram_start=0x82001000),
            lambda p: p.update(sd_rca=0),
            lambda p: p.update(sd_sectors=1 << 33),
            lambda p: p.update(sd_sectors=True),
            lambda p: p.update(reserved=[]),
            lambda p: p["reserved"][0].update(size=64 << 20),
            lambda p: p["handoff"].update(exclusive_devices=False),
            lambda p: p.update(unknown_default=True),
        ):
            with self.subTest(mutate=mutate):
                candidate = copy.deepcopy(profile())
                mutate(candidate)
                with self.assertRaises(ValueError):
                    build.profile_constants(candidate)
