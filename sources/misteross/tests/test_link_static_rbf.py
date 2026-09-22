import tempfile
import unittest
from pathlib import Path

from scripts.link_static_rbf import (
    LinkError,
    ZX81_BASIC_BLOCKS,
    bel_to_bt_name,
    load_hex_bytes,
    lane_from_hex,
    overlay_files,
    decode_m10k_ram_word,
    encode_m10k_ram_word,
    m10k_block_present,
    overlay_m10k_init_bt,
    overlay_m10k_ram_bt,
    overlay_mode_from_map,
    overlay_zx81_basic_bt,
    pack_1024x10,
    parse_link_map,
    read_m10k_init_bt,
    rect_from_map,
    unpack_1024x10,
    zx81_basic_bels,
    zx81_basic_rom,
    zx81_machine_rom_bels,
    encoded_rom_blocks,
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

    def test_zx81_basic_rom_fills_1024x10_lanes(self) -> None:
        root = Path(__file__).resolve().parents[1]
        image = load_hex_bytes(root / "cores/fes-zx81/rtl/zx8x.hex")
        rom = zx81_basic_rom(image)
        self.assertEqual(rom[:8].hex(), "d3fd01ff7fc3cb03")
        self.assertEqual(lane_from_hex(root / "cores/fes-zx81/rtl/zx8x.hex"), rom[:1024])
        bels = zx81_basic_bels()
        self.assertEqual(len(bels), ZX81_BASIC_BLOCKS)
        self.assertEqual(
            bels,
            [f"MISTRAL_M10K.26.{index}.0" for index in (1, 2, 5, 6, 9, 10, 13, 14)],
        )
        machine = zx81_machine_rom_bels()
        self.assertEqual(machine, [f"MISTRAL_M10K.5.{index}.0" for index in range(73, 81)])
        self.assertEqual({bel.split(".")[1] for bel in machine}, {"5"})
        stored = encoded_rom_blocks(image, machine)
        self.assertEqual(len(stored), 8)
        self.assertEqual(decode_m10k_ram_word(stored[0][1][0]) & 0x3FF, 0x0D3)
        lines = ["s LAB.027.001:LUT_MASK.0 0"]
        for bel in bels:
            name = bel_to_bt_name(bel)
            lines.append(f"s {name}:TOP_CLK_SEL 1")
            lines.extend(f"s {name}:RAM.{index} 00.00000000" for index in range(256))
        blank = "\n".join(lines) + "\n"
        composed = overlay_zx81_basic_bt(blank, image)
        self.assertIn("s M10K.026.001:TOP_CLK_SEL 1", composed)
        self.assertIn("s LAB.027.001:LUT_MASK.0 0", composed)
        self.assertIn("s M10K.026.001:RAM.0 3f.c013f4d3", composed)
        restored = bytearray()
        for index, bel in enumerate(bels):
            block = unpack_1024x10(read_m10k_init_bt(composed, bel))
            self.assertEqual(block, rom[index * 1024 : (index + 1) * 1024])
            restored.extend(block)
        self.assertEqual(bytes(restored), rom)
        first = pack_1024x10(rom[:1024])
        self.assertEqual(first[0] & 0x3FF, 0x0D3)
        self.assertEqual((first[0] >> 10) & 0x3FF, 0x0FD)
        # 890's placed RAM.0 is nextpnr's permuted, inverted form of the
        # first four 1024x10 lanes, not the raw lane packing.
        logical = 0
        for address in range(4):
            logical |= (((address * 73) ^ (address >> 1) ^ 0xA6) & 0x3FF) << (address * 10)
        self.assertEqual(encode_m10k_ram_word(logical), 0xFFA30530A9)
        self.assertEqual(decode_m10k_ram_word(0xFFA30530A9), logical)
        self.assertEqual(decode_m10k_ram_word(encode_m10k_ram_word(first[0])), first[0])
        with self.assertRaisesRegex(LinkError, "RAM muxes"):
            overlay_m10k_init_bt("s LAB.027.001:LUT_MASK.0 0\n", bels[0], first)

    def test_omitted_default_ram_words_splice_into_placed_m10k(self) -> None:
        root = Path(__file__).resolve().parents[1]
        image = load_hex_bytes(root / "cores/fes-zx81/rtl/zx8x.hex")
        bel = zx81_machine_rom_bels()[0]
        name = bel_to_bt_name(bel)
        stored = [encode_m10k_ram_word(word) for word in pack_1024x10(zx81_basic_rom(image)[:1024])]
        # Placed 10-bit M10K whose RAM CRAM is still the all-zero default.
        # mistral-cv prints A_DATA_WIDTH and omits every RAM word.
        blank = f"s {name}:A_DATA_WIDTH 10\ns LAB.027.001:LUT_MASK.0 0\n"
        self.assertTrue(m10k_block_present(blank, bel))
        self.assertEqual(read_m10k_init_bt(blank, bel), [0] * 256)
        composed = overlay_m10k_init_bt(blank, bel, stored)
        self.assertIn(f"s {name}:A_DATA_WIDTH 10\n", composed)
        self.assertIn("s LAB.027.001:LUT_MASK.0 0\n", composed)
        self.assertEqual(read_m10k_init_bt(composed, bel), stored)
        partial = blank + f"s {name}:RAM.0 00.00000000\n"
        self.assertEqual(read_m10k_init_bt(partial, bel)[0], 0)
        self.assertEqual(len(read_m10k_init_bt(partial, bel)), 256)
