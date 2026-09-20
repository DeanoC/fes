import tempfile
import unittest
from pathlib import Path

from scripts.link_static_rbf import (
    LinkError,
    bel_to_bt_name,
    overlay_files,
    overlay_m10k_ram_bt,
    overlay_mode_from_map,
    parse_link_map,
    rect_from_map,
)


class LinkStaticRbfTests(unittest.TestCase):
    def test_parse_link_map_reads_cram_rect(self) -> None:
        mapping = parse_link_map(
            'device = "5CSEBA6U23I7"\n'
            'die = "sx120f"\n'
            "slot_column = 26\n"
            'slot_bels = ["MISTRAL_M10K.26.1.0"]\n'
            "\n"
            "[cram_rect]\n"
            "x0 = 2096\n"
            "y0 = 32\n"
            "x1 = 2396\n"
            "y1 = 7024\n"
        )
        self.assertEqual(mapping["device"], "5CSEBA6U23I7")
        self.assertEqual(mapping["slot_bels"], ["MISTRAL_M10K.26.1.0"])
        rect = rect_from_map(mapping)
        self.assertEqual((rect.x0, rect.x1), (2096, 2396))

    def test_parse_link_map_rejects_unknown_device(self) -> None:
        with self.assertRaisesRegex(LinkError, "device"):
            rect_from_map({"device": "other", "die": "sx120f"})

    def test_overlay_cli_writes_receipt(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            mapping = root / "link.toml"
            mapping.write_text('device = "other"\ndie = "sx120f"\n', encoding="utf-8")
            with self.assertRaisesRegex(LinkError, "device"):
                overlay_files(root / "a.rbf", root / "b.rbf", mapping, root / "out.rbf")

    def test_m10k_ram_mode_stitches_decompile_lines(self) -> None:
        mapping = parse_link_map(
            'device = "5CSEBA6U23I7"\n'
            'die = "sx120f"\n'
            'overlay_mode = "m10k_ram"\n'
            'slot_bels = ["MISTRAL_M10K.26.1.0"]\n'
        )
        self.assertEqual(overlay_mode_from_map(mapping), "m10k_ram")
        self.assertEqual(bel_to_bt_name("MISTRAL_M10K.26.1.0"), "M10K.026.001")
        base = (
            "s M10K.026.001:TOP_CLK_SEL 1\n"
            "s M10K.026.001:RAM.0 ff.ffffffff\n"
            "s LAB.027.001:LUT_MASK.0 0\n"
        )
        cart = (
            "s M10K.026.001:TOP_CLK_SEL 0\n"
            "s M10K.026.001:RAM.0 ff.a30530a9\n"
            "s LAB.027.001:LUT_MASK.0 1\n"
        )
        composed = overlay_m10k_ram_bt(base, cart)
        self.assertIn("s M10K.026.001:RAM.0 ff.a30530a9", composed)
        self.assertIn("s M10K.026.001:TOP_CLK_SEL 1", composed)
        self.assertIn("s LAB.027.001:LUT_MASK.0 0", composed)
        with self.assertRaisesRegex(LinkError, "no RAM muxes"):
            overlay_m10k_ram_bt(base, "s LAB.027.001:LUT_MASK.0 1\n")

    def test_cram_rect_mode_requires_slot_only(self) -> None:
        mapping = parse_link_map(
            'device = "5CSEBA6U23I7"\n'
            'die = "sx120f"\n'
            'overlay_mode = "cram_rect"\n'
            "require_slot_only = true\n"
            "[cram_rect]\n"
            "x0 = 2023\n"
            "y0 = 32\n"
            "x1 = 2455\n"
            "y1 = 7024\n"
        )
        self.assertEqual(overlay_mode_from_map(mapping), "cram_rect")
        self.assertTrue(mapping["require_slot_only"])
        rect = rect_from_map(mapping)
        self.assertEqual((rect.x0, rect.x1), (2023, 2455))
