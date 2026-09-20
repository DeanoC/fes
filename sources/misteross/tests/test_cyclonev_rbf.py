import unittest

from scripts.cyclonev_rbf import (
    SX120F,
    CramRect,
    DieInfo,
    LoadedRbf,
    classify_cram_diff,
    cram_get,
    cram_set,
    default_slot_rect,
    diff_cram,
    header_nbytes,
    overlay_cram,
    rbf_load,
    rbf_save,
    rect_inside,
    tile_column_cram_x,
)


TINY = DieInfo(
    name="tiny",
    cram_sx=8,
    cram_sy=256,
    frame_size=16,
    pram_sizes=(0,) * 32,
    noedcrc_zones=(8,) + (0,) * 11,
    postamble_1=8,
    postamble_2=8,
)


def _blank(die: DieInfo) -> LoadedRbf:
    header = bytes(header_nbytes(die))
    cram = bytearray((die.cram_sx * die.cram_sy + 7) // 8)
    return LoadedRbf(die=die, header=header, cram=cram, compressed=True)


class CycloneVRBFTests(unittest.TestCase):
    def test_slot_column_matches_sx120f_m10k_column_26(self) -> None:
        x0, x1 = tile_column_cram_x(SX120F, 26)
        self.assertEqual((x0, x1), (2096, 2396))
        slot = default_slot_rect()
        self.assertEqual(slot.x0, 2096)
        self.assertEqual(slot.x1, 2396)
        self.assertEqual(slot.y0, 32)
        self.assertEqual(slot.y1, 7024)

    def test_roundtrip_preserves_cram_bits(self) -> None:
        loaded = _blank(TINY)
        cram_set(loaded.cram, TINY, 3, 40, 1)
        cram_set(loaded.cram, TINY, 7, 255, 1)
        blob = rbf_save(loaded, compressed=True)
        self.assertLess(len(blob), TINY.cram_sx * TINY.cram_sy)
        again = rbf_load(blob, TINY)
        self.assertEqual(cram_get(again.cram, TINY, 3, 40), 1)
        self.assertEqual(cram_get(again.cram, TINY, 7, 255), 1)
        self.assertEqual(cram_get(again.cram, TINY, 0, 32), 0)
        uncompressed = rbf_save(loaded, compressed=False)
        plain = rbf_load(uncompressed, TINY)
        self.assertEqual(cram_get(plain.cram, TINY, 3, 40), 1)
        self.assertFalse(plain.compressed)

    def test_overlay_copies_only_the_rectangle(self) -> None:
        base = _blank(TINY)
        cart = _blank(TINY)
        cram_set(base.cram, TINY, 1, 40, 1)
        cram_set(cart.cram, TINY, 2, 41, 1)
        cram_set(cart.cram, TINY, 6, 50, 1)
        composed = overlay_cram(base, cart, CramRect(2, 32, 5, 45))
        self.assertEqual(cram_get(composed.cram, TINY, 1, 40), 1)
        self.assertEqual(cram_get(composed.cram, TINY, 2, 41), 1)
        self.assertEqual(cram_get(composed.cram, TINY, 6, 50), 0)
        box = diff_cram(base, composed)
        self.assertIsNotNone(box)
        self.assertTrue(rect_inside(box, CramRect(2, 32, 5, 45)))
        blob = rbf_save(composed, compressed=True)
        again = rbf_load(blob, TINY)
        self.assertEqual(cram_get(again.cram, TINY, 1, 40), 1)
        self.assertEqual(cram_get(again.cram, TINY, 2, 41), 1)
        self.assertEqual(cram_get(again.cram, TINY, 6, 50), 0)

    def test_diff_outside_slot_is_not_local(self) -> None:
        left = _blank(TINY)
        right = _blank(TINY)
        cram_set(right.cram, TINY, 7, 50, 1)
        box = diff_cram(left, right)
        self.assertIsNotNone(box)
        self.assertFalse(rect_inside(box, CramRect(2, 32, 5, 45)))

    def test_classify_separates_slot_from_scatter(self) -> None:
        left = _blank(TINY)
        right = _blank(TINY)
        cram_set(right.cram, TINY, 3, 40, 1)
        cram_set(right.cram, TINY, 7, 50, 1)
        TINY_NO_COLS = DieInfo(
            name="tiny",
            cram_sx=8,
            cram_sy=256,
            frame_size=16,
            pram_sizes=(0,) * 32,
            noedcrc_zones=(8,) + (0,) * 11,
            postamble_1=8,
            postamble_2=8,
            x_to_bx=(0, 5),
        )
        right.die = TINY_NO_COLS
        left.die = TINY_NO_COLS
        report = classify_cram_diff(left, right, CramRect(2, 32, 5, 45))
        self.assertEqual(report["bits_inside_slot"], 1)
        self.assertEqual(report["bits_outside_slot"], 1)
        self.assertFalse(report["inside_slot_column"])

    def test_overlay_rejects_mismatched_dies(self) -> None:
        with self.assertRaisesRegex(ValueError, "dies"):
            overlay_cram(_blank(TINY), _blank(SX120F), CramRect(0, 32, 1, 33))
