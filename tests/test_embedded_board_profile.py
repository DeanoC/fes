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
    return {"schema": 4, "firmware": "synthetic test; not a board recipe",
            "ram_start": 0x82000000, "ram_size": 256 << 20, "entry": 0x82010000,
            "reserved": [{"start": 0x80000000, "size": 32 << 20, "owner": "synthetic firmware"}],
            "sd_input_clock_hz": 100000000, "usb_phy_clock_hz": 60000000,
            "network": None, "display": None, "handoff": {"display_platform_ready":False, "ethernet_platform_ready":False, **{key: True for key in ("el1", "single_cpu", "counter_access", "exclusive_devices",
                "uart0_configured", "sd_platform_ready", "usb_platform_ready", "mmu_off_caches_clean")}}}


class BoardProfileTests(unittest.TestCase):
    def test_explicit_profile(self):
        self.assertEqual(build.profile_constants(profile()), (0x82000000, 256 << 20, 0x82010000, 100000000, 60000000, None, None))

    def test_bad_handoff(self):
        for mutate in (
            lambda p: p.update(ram_start=0x40000000),
            lambda p: p.update(schema=1),
            lambda p: p.update(sd_sectors=8192, sd_rca=7),
            lambda p: p.update(ram_size=2 << 30),
            lambda p: p.update(entry=0x82000000),
            lambda p: p.update(ram_start=0x82001000),
            lambda p: p.update(sd_input_clock_hz=0),
            lambda p: p.update(usb_phy_clock_hz=48000000),
            lambda p: p["handoff"].update(usb_platform_ready=False),
            lambda p: p.update(sd_input_clock_hz=200000001),
            lambda p: p.update(sd_input_clock_hz=True),
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

    def test_explicit_usb_disabled(self):
        p = profile()
        p["usb_phy_clock_hz"] = 0
        p["handoff"]["usb_platform_ready"] = False
        self.assertEqual(build.profile_constants(p)[4], 0)


def ethernet_profile():
    p = profile()
    p["handoff"]["ethernet_platform_ready"] = True
    p["network"] = {"mac_index": 0, "phy_address": 4, "phy_id": 0x001cc916,
        "mac_address": "02:12:34:56:78:90", "csr_clock_hz": 125000000,
        "rx_buffer_shift": 4, "address": "192.0.2.2/24", "gateway": "192.0.2.1",
        "dma_start": 0x92000000, "dma_size": 2 << 20, "dma_identity": True,
        "rgmii_125mhz_ready": True}
    return p


class EthernetProfileTests(unittest.TestCase):
    def test_admitted_and_disabled(self):
        p = ethernet_profile()
        self.assertEqual(build.profile_constants(p)[5], p["network"])
        self.assertIsNone(build.profile_constants(profile())[-1])
        p["network"]["rx_buffer_shift"] = 1
        build.profile_constants(p)

    def test_invalid_memory_identity_and_link_contract(self):
        changes = (
            ("dma_start", 0x82000000), ("dma_start", 0x80000000),
            ("dma_start", 0x92001000), ("dma_start", 0xc0000000),
            ("dma_size", 4 << 20), ("dma_identity", False),
            ("rgmii_125mhz_ready", False), ("rx_buffer_shift", 0),
            ("csr_clock_hz", 150000000), ("mac_index", 3),
            ("phy_address", 32), ("phy_id", 0), ("phy_id", True),
            ("mac_address", "00:00:00:00:00:00"),
            ("mac_address", "01:00:00:00:00:01"), ("address", "192.0.2.0/24"),
            ("gateway", "192.0.3.1"), ("gateway", "192.0.2.2"),
        )
        for key, value in changes:
            with self.subTest(key=key, value=value):
                p = ethernet_profile()
                p["network"][key] = value
                with self.assertRaises(ValueError):
                    build.profile_constants(p)
        p = ethernet_profile()
        p["handoff"]["ethernet_platform_ready"] = False
        with self.assertRaises(ValueError):
            build.profile_constants(p)
        p = ethernet_profile()
        p["reserved"].append({"start": 0x92000000, "size": 2 << 20, "owner": "other firmware"})
        with self.assertRaises(ValueError):
            build.profile_constants(p)


def display_profile():
    p = ethernet_profile()
    p["handoff"]["display_platform_ready"] = True
    p["display"] = {"base": 0x93000000, "size": 4 << 20,
        "width": 1280, "height": 720, "stride": 5120, "format": "xrgb8888",
        "lifetime": "boot", "firmware_artifact_sha256": "ab" * 32}
    return p


class DisplayProfileTests(unittest.TestCase):
    def test_admitted_and_disabled(self):
        p = display_profile()
        self.assertEqual(build.profile_constants(p)[6], p["display"])
        self.assertIsNone(build.profile_constants(profile())[6])

    def test_frame_window_and_lifetime_are_explicit(self):
        for key, value in (("base", 0x30000000), ("base", 0x82000000),
            ("base", 0x80000000), ("base", 0x92000000), ("base", 0x93001000),
            ("base", 0xbfe00000), ("size", 2 << 20), ("width", True),
            ("width", 1920), ("stride", 5116), ("height", 1080),
            ("format", "rgba"), ("lifetime", "frame"),
            ("firmware_artifact_sha256", "0" * 64), ("firmware_artifact_sha256", "unknown")):
            with self.subTest(key=key, value=value):
                p = display_profile()
                p["display"][key] = value
                with self.assertRaises(ValueError):
                    build.profile_constants(p)
        p = display_profile()
        p["handoff"]["display_platform_ready"] = False
        with self.assertRaises(ValueError):
            build.profile_constants(p)
        p = display_profile()
        p["reserved"].append({"start": 0x93000000, "size": 4 << 20, "owner": "firmware task"})
        with self.assertRaises(ValueError):
            build.profile_constants(p)
        p = display_profile()
        p["schema"] = 3
        with self.assertRaises(ValueError):
            build.profile_constants(p)
