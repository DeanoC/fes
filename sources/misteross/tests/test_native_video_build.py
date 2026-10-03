"""Validate the native shell/part producer and its distinct physical profile."""
import copy
import json
from pathlib import Path
import tempfile
import tomllib
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from scripts import build_fes_coleco_socket_v2 as shell
from scripts import build_video_part as part
from scripts import native_video_parts as native, video_parts as raster
from scripts.cyclonev_rbf import SX120F, CramRect, LoadedRbf, classify_cram_diff, cram_set, overlay_cram


class NativeVideoProducerTest(unittest.TestCase):
    def test_record_binds_native_fallback_and_actual_router_policy(self):
        for flags, expected_weights in (({}, (2000,)),
                ({"video_socket": True}, (2000,)),
                ({"native_video": True}, (2000, 1000))):
            with self.subTest(flags=flags), \
                    patch.object(shell, "functional_record_fields",
                                 side_effect=lambda root, fields, *args, **kwargs: fields), \
                    patch.object(shell, "encode_build_record", side_effect=lambda fields: fields), \
                    patch.object(shell, "route_after_synth") as route:
                record = shell.create_build_record(shell.ROOT, "repository", "a" * 40,
                                                   {}, execution={}, **flags)
                _, relative, _ = shell.video_profile(**flags)
                env = {"FES_CONTROLLED_FIXTURE": "1"}
                shell._route_placement(shell.ROOT, shell.ROOT / relative, Path("/nextpnr"),
                    native_video=flags.get("native_video", False), env=env)
            parameters, options = record["parameters"], route.call_args.kwargs
            self.assertEqual(options["weights"], expected_weights)
            self.assertEqual(tuple(map(int, parameters["placer_heap_timingweights"].split(","))),
                             options["weights"])
            self.assertEqual(tuple(map(int, parameters["seed_order"].split(","))),
                             options["seeds"])
            self.assertEqual(parameters["placer_qor_budget"], options["budget"])
            self.assertEqual(options["budget"], 10 * len(expected_weights))
            self.assertEqual(parameters["placer_qor_mode"], options["mode"])
            self.assertEqual(options["mode"], "first-pass")
            self.assertEqual(parameters["placer_qor_timeout_seconds"], options["timeout"])
            self.assertEqual(options["timeout"], 600)
            self.assertEqual(options["required"], ((None, 52.224), (None, 74.25), (None, 12.288)))
            self.assertEqual(options["gpu_devices"], (0,))
            self.assertIs(options["env"], env)
            self.assertEqual(options["audit_source_root"], shell.ROOT)

    def test_native_preview_preserves_real_routing_in_legacy_excluded_column(self):
        size = (SX120F.cram_sx * SX120F.cram_sy + 7) // 8
        base = LoadedRbf(SX120F, b"header", bytearray(size), True)
        cart = LoadedRbf(SX120F, b"header", bytearray(size), True)
        # Exact Direct route bits discarded by the old whole-column policy;
        # physical capture showed framebuffer data bit3 stuck high as a result.
        for x, y in ((3772, 2390), (3773, 2392), (3764, 2476), (3764, 2478)):
            cram_set(cart.cram, SX120F, x, y, 1)
        rect = CramRect(*native.CRAM)
        strict = classify_cram_diff(base, cart, rect, ignore_ecc_columns=False)
        self.assertEqual((strict["bits_inside_slot"], strict["bits_outside_slot"]), (4, 0))
        linked = overlay_cram(base, cart, rect)
        self.assertEqual(linked.cram, cart.cram)
        self.assertEqual(base.cram, bytearray(size))
        # Preserve the old classifier default for unrelated legacy producers.
        self.assertTrue(classify_cram_diff(base, cart, rect)["identical"])

    def test_native_rejects_outside_bits_even_in_legacy_excluded_columns(self):
        size = (SX120F.cram_sx * SX120F.cram_sy + 7) // 8
        base = LoadedRbf(SX120F, b"header", bytearray(size), True)
        cart = LoadedRbf(SX120F, b"header", bytearray(size), True)
        coordinates = ((3772, 3442), (3921, 2390), (4174, 2390))
        for x, y in coordinates:
            cram_set(cart.cram, SX120F, x, y, 1)
        strict = classify_cram_diff(base, cart, CramRect(*native.CRAM),
            ignore_ecc_columns=False, include_outside_coordinates=True)
        self.assertEqual(strict["bits_outside_slot"], len(coordinates))
        self.assertEqual({tuple(point) for point in strict["outside_slot_coordinates"]}, set(coordinates))
        self.assertTrue(classify_cram_diff(base, cart, CramRect(*native.CRAM))["identical"])

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
