"""Firmware handoff admission must fail before an unsafe image is emitted."""
import copy
import importlib.util
from pathlib import Path
import struct
import unittest

PATH = Path(__file__).resolve().parents[1] / "platform/embedded/tamago/build.py"
spec = importlib.util.spec_from_file_location("embedded_board_build", PATH)
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


def profile():
    return {"schema": 8, "firmware": "synthetic test; not a board recipe", "uart_index": 0,
            "ram_start": 0x82000000, "ram_size": 256 << 20, "entry": 0x82020000,
            "reserved": [{"start": 0x80000000, "size": 32 << 20, "owner": "synthetic firmware"}],
            "sd_input_clock_hz": 0, "usb_phy_clock_hz": 60000000,
            "network": None, "display": None, "fpga_probe": None, "sd_control": None, "handoff": {"display_platform_ready":False, "ethernet_platform_ready":False, "sd_platform_ready":False, **{key: True for key in ("el1", "single_cpu", "counter_access", "exclusive_devices",
                "uart_configured", "usb_platform_ready", "mmu_off_caches_clean")}}}


class BoardProfileTests(unittest.TestCase):
    def test_package_probe_requires_explicit_identity_and_owned_sd(self):
        p=profile();p['sd_control']={'service':'fes-owned-sd-pio-v1'}
        self.assertEqual(build.package_option('sdpackages','a'*64,p),'a'*64)
        for value in (None,'','A'*64,'a'*63,'../package','a'*64+'; boot'):
            with self.subTest(value=value),self.assertRaises(ValueError):build.package_option('sdpackages',value,p)
        with self.assertRaises(ValueError):build.package_option('sdpackages','a'*64,profile())
        with self.assertRaises(ValueError):build.package_option('storage','a'*64,p)
        self.assertIsNone(build.package_option('storage',None,p))
    def test_explicit_profile(self):
        self.assertEqual(build.profile_constants(profile()), (0x82000000, 256 << 20, 0x82020000, 0, 60000000, None, None, None, 0))

    def test_console_selection_is_explicit_and_exact(self):
        p = profile()
        p["uart_index"] = 1
        self.assertEqual(build.profile_constants(p)[-1], 1)
        for invalid in (-1, 2, True, "1", None):
            p = profile()
            p["uart_index"] = invalid
            with self.subTest(index=invalid), self.assertRaises(ValueError):
                build.profile_constants(p)
        p = profile()
        del p["uart_index"]
        with self.assertRaises(ValueError):
            build.profile_constants(p)
        p = profile()
        p["schema"] = 5
        with self.assertRaises(ValueError):
            build.profile_constants(p)

    def test_bad_handoff(self):
        for mutate in (
            lambda p: p.update(ram_start=0x40000000),
            lambda p: p.update(schema=1),
            lambda p: p.update(sd_sectors=8192, sd_rca=7),
            lambda p: p.update(ram_size=2 << 30),
            lambda p: p.update(entry=0x82000000),
            lambda p: p.update(entry=0x82010000),
            lambda p: p.update(schema=6),
            lambda p: p.update(ram_start=0x82001000),
            lambda p: p.update(sd_input_clock_hz=100000000),
            lambda p: p.update(usb_phy_clock_hz=48000000),
            lambda p: p["handoff"].update(usb_platform_ready=False),
            lambda p: p.update(sd_input_clock_hz=200000001),
            lambda p: p.update(sd_input_clock_hz=True),
            lambda p: p.update(reserved=[]),
            lambda p: p["reserved"][0].update(size=64 << 20),
            lambda p: p["handoff"].update(exclusive_devices=False),
            lambda p: p["handoff"].update(display_platform_ready=0),
            lambda p: p["handoff"].update(ethernet_platform_ready=0),
            lambda p: p["handoff"].update(uart_configured=1),
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

    def test_serial_only_handoff_explicitly_disables_sd(self):
        p = profile()
        p["sd_input_clock_hz"] = 0
        p["handoff"]["sd_platform_ready"] = False
        self.assertEqual(build.profile_constants(p)[3], 0)
        p["sd_input_clock_hz"] = 100000000
        with self.assertRaises(ValueError):
            build.profile_constants(p)

    def test_controlled_sd_service_is_explicit_and_never_static_readiness(self):
        p = profile()
        p['usb_phy_clock_hz'] = 0
        p['handoff']['usb_platform_ready'] = False
        p['sd_input_clock_hz'] = 200000000
        p['sd_control'] = {'service':'fes-owned-sd-pio-v1', 'exclusive':True,
                           'closed_bridges':True, 'synchronous_bounded_return':True}
        self.assertEqual(build.profile_constants(p)[3], 200000000)
        for key, value in (('service','mmio'), ('exclusive',False), ('exclusive',1),
                           ('closed_bridges',False), ('synchronous_bounded_return',False)):
            bad = copy.deepcopy(p); bad['sd_control'][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError): build.profile_constants(bad)
        for mutate in (lambda b:b.update(sd_input_clock_hz=100000000),
                       lambda b:b['handoff'].update(sd_platform_ready=True),
                       lambda b:b['handoff'].update(usb_platform_ready=True),
                       lambda b:b['sd_control'].update(unknown=True)):
            bad=copy.deepcopy(p);mutate(bad)
            with self.assertRaises(ValueError): build.profile_constants(bad)

    def test_elf_load_segments_leave_the_whole_runtime_reservation_clear(self):
        data = bytearray(0x2000)
        ident = b"\x7fELF\x02\x01\x01" + bytes(9)
        struct.pack_into("<16sHHIQQQIHHHHHH", data, 0, ident, 2, 183, 1,
                         0x82020000, 64, 0, 0, 64, 56, 2, 0, 0, 0)
        struct.pack_into("<IIQQQQQQ", data, 64, 1, 5, 0, 0x8201f000,
                         0x8201f000, 0x2000, 0x2000, 0x1000)
        struct.pack_into("<IIQQQQQQ", data, 120, 1, 6, 0x1000, 0x82030000,
                         0x82030000, 0x1000, 0x1000, 0x1000)
        build.validate_native_elf(data, 0x82000000, 256 << 20, 0x82020000)
        # Leave the entry valid while moving a second segment into reserved RAM.
        struct.pack_into("<QQ", data, 120 + 16, 0x8200f000, 0x8200f000)
        with self.assertRaises(ValueError):
            build.validate_native_elf(data, 0x82000000, 256 << 20, 0x82020000)
        struct.pack_into("<QQ", data, 120 + 16, 0x91ff0000, 0x91ff0000)
        with self.assertRaises(ValueError):
            build.validate_native_elf(data, 0x82000000, 256 << 20, 0x82020000)


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
        self.assertIsNone(build.profile_constants(profile())[5])
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


def fpga_profile():
    p = display_profile()
    p['fpga_probe'] = {'base': 0x94000000, 'size': 2 << 20,
        'service': 'intel-sip-mbox-v1-read-only', 'exclusive': True,
        'synchronous_bounded_return': True}
    return p


class FPGAProbeProfileTests(unittest.TestCase):
    def test_service_is_explicit_and_optional(self):
        p = fpga_profile()
        self.assertEqual(build.profile_constants(p)[7], p['fpga_probe'])
        self.assertIsNone(build.profile_constants(profile())[7])
        p = profile()
        p['schema'] = 4
        with self.assertRaises(ValueError):
            build.profile_constants(p)

    def test_arena_and_firmware_contract(self):
        for key, value in (
            ('base', 0x80000000), ('base', 0x82000000),
            ('base', 0x92000000), ('base', 0x93000000),
            ('base', 0x94000040), ('base', 0xc0000000),
            ('base', True), ('size', 64), ('size', 4 << 20),
            ('service', 'intel-sip-fpga-config'), ('service', 'mmio'),
            ('exclusive', False), ('exclusive', 1),
            ('synchronous_bounded_return', False), ('synchronous_bounded_return', 1),
        ):
            with self.subTest(key=key, value=value):
                p = fpga_profile()
                p['fpga_probe'][key] = value
                with self.assertRaises(ValueError):
                    build.profile_constants(p)
        p = fpga_profile()
        p['reserved'].append({'start': 0x94000000, 'size': 2 << 20, 'owner': 'TF-A'})
        with self.assertRaises(ValueError):
            build.profile_constants(p)
        p = fpga_profile()
        p['fpga_probe']['mutating'] = True
        with self.assertRaises(ValueError):
            build.profile_constants(p)
