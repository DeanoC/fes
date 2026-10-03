"""Keep the native shell/part producer separate from the factory raster lane."""
import copy
import json
from pathlib import Path
import tempfile
import tomllib
from types import SimpleNamespace
import unittest

from scripts import build_fes_coleco_socket_v2 as shell
from scripts import build_video_part as part
from scripts import native_video_parts as native, video_parts as raster
from scripts.cyclonev_rbf import SX120F, CramRect, LoadedRbf, cram_get, cram_set, overlay_cram


class NativeVideoProducerTest(unittest.TestCase):
    def test_wide_preview_preserves_exact_target_checksum_companions(self):
        size = (SX120F.cram_sx * SX120F.cram_sy + 7) // 8
        base = LoadedRbf(SX120F, b"header", bytearray(size), True)
        cart = LoadedRbf(SX120F, b"header", bytearray(size), True)
        self.assertEqual(part.PARTS_CRC_COMPANION_RANGES,
                         ((3488, 3847), (3921, 3980), (4171, 4471)))
        # Both range boundaries matter. Do not widen the exclusions to the
        # independent classifier's ECC list (which also lists tile column43).
        for start, end in part.PARTS_CRC_COMPANION_RANGES:
            for x in (start - 1, start, end - 1, end):
                cram_set(cart.cram, SX120F, x, 2000, 1)
            cram_set(base.cram, SX120F, start, 2001, 1)
        linked = overlay_cram(base, cart, CramRect(3400, 2000, 4500, 2002),
                              preserve_x_ranges=part.PARTS_CRC_COMPANION_RANGES)
        for start, end in part.PARTS_CRC_COMPANION_RANGES:
            for x in (start - 1, end):
                self.assertEqual(cram_get(linked.cram, SX120F, x, 2000), 1)
            for x in (start, end - 1):
                self.assertEqual(cram_get(linked.cram, SX120F, x, 2000), 0)
            self.assertEqual(cram_get(linked.cram, SX120F, start, 2001), 1)
        self.assertEqual(base.cram.count(0), size - 3)

    def test_shell_compiles_source_and_cdc_without_inline_framebuffer(self):
        tools = {name: Path("/tool") / name for name in ("yosys", "nextpnr-mistral")}
        commands = shell.build_commands(shell.ROOT, shell.ROOT / shell.NATIVE_VIDEO_OUTPUT_RELATIVE,
                                        "a" * 32, tools, native_video=True)
        program = commands[0][-1]
        for define in ("FES_COLECO_NATIVE_VIDEO_DEV", "FES_COLECO_NATIVE_VIDEO_PART_DEV",
                       "FES_COLECO_EXPANSION_V2_DEV"):
            self.assertIn("-D" + define + "=1", program)
        for source in shell.NATIVE_VIDEO_SOURCES:
            self.assertIn(source, program)
            self.assertIn(source, shell.NATIVE_VIDEO_INPUTS)
        self.assertNotIn("rtl/fes_native_video.v", program)
        self.assertNotIn(raster.RTL, program)
        for flag, name in (("--json", "synth.json"), ("--qsf", "socket.qsf"),
                           ("--rbf", "core.rbf"), ("--write", "routed.json")):
            self.assertEqual(commands[1][commands[1].index(flag) + 1],
                             str(shell.NATIVE_VIDEO_OUTPUT_RELATIVE / name))
        with self.assertRaisesRegex(ValueError, "output path"):
            shell.build_commands(shell.ROOT, shell.ROOT / shell.VIDEO_OUTPUT_RELATIVE,
                                 "a" * 32, tools, native_video=True)
        with self.assertRaisesRegex(ValueError, "either raster or native"):
            shell.video_profile(video_socket=True, native_video=True)

    def test_native_manifest_selects_exact_part_profile(self):
        record = b'{"format":2,"parameters":{},"recipe_sha256":"' + b'a' * 64 + b'"}'
        evidence = {"rbf": {"size": 100, "sha256": "b" * 64}, "build_id": "c" * 32}
        identities = {name: "fixture" for name in ("yosys", "nextpnr-mistral", "mistral")}
        arguments = (record, evidence, "https://example.invalid/fes.git", "d" * 40, identities)
        for layout, flags in ((native, {"native_video": True}), (raster, {"video_socket": True})):
            fields = tomllib.loads(shell.manifest(*arguments, **flags).decode())
            self.assertEqual(fields["core"]["id"], "fes.coleco")
            self.assertIn({"id": "fes.expansion.coleco-bus", "major": 2,
                           "minor": 0, "required": False}, fields["interfaces"])
            self.assertIs(part.package_profile(SimpleNamespace(fields=fields)), layout)
            other = raster if layout is native else native
            invalid = []
            for change in ({"major": 2}, {"minor": 1}, {"required": True}):
                altered = copy.deepcopy(fields)
                next(row for row in altered["interfaces"] if row["id"] == layout.INTERFACE).update(change)
                invalid.append(altered)
            for marker in (layout.INTERFACE, other.INTERFACE):
                altered = copy.deepcopy(fields)
                altered["interfaces"].append({"id": marker, "major": 1, "minor": 0, "required": False})
                invalid.append(altered)
            invalid.append({**fields, "format": 1})
            invalid.append({**fields, "interfaces": []})
            for bad in invalid:
                with self.subTest(fields=bad), self.assertRaisesRegex(ValueError, "one exact sealed"):
                    part.package_profile(SimpleNamespace(fields=bad))

    def test_native_memory_clock_and_publication_use_native_fence(self):
        routed = {"modules": {"top": {"netnames": {"pixel_clk": {"bits": [9000]}},
            "cells": {"fes_cart$framebuffer": {"type": "MISTRAL_M10K", "connections": {
                "CLK1": [9000], "CLK2": [9000]}}}}}}
        self.assertEqual(part.validate_clocks(routed, layout=native), 2)
        routed["modules"]["top"]["cells"]["fes_cart$framebuffer"]["connections"]["CLK2"] = [2107]
        with self.assertRaisesRegex(ValueError, "not on the pixel clock"):
            part.validate_clocks(routed, layout=native)
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            changes = {"bits_inside_slot": 9, "bits_outside_slot": 0}
            archive = part.publish_archive(output, "a" * 64, b"{}", b"cart", changes, {}, layout=native)
            self.assertTrue(archive.is_file())
            report = json.loads((output / "cram-diff.json").read_bytes())
            self.assertEqual(report["map"], native.MAP)
            self.assertEqual(report["cram_region"], list(native.CRAM))
            self.assertTrue(report["archive_published"])
            with self.assertRaisesRegex(ValueError, "outside-region"):
                part.write_cram_report(output, b"cart", {"bits_outside_slot": 1},
                                       part_id="b" * 64, layout=native)


if __name__ == "__main__":
    unittest.main()
