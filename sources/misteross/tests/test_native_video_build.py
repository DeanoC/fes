"""Validate the native shell/part producer and its distinct physical profile."""
import copy
import json
from pathlib import Path
import tempfile
import tomllib
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from scripts import build_fes_coleco_socket_v2 as shell
from scripts import build_video_part as part
from scripts import native_video_parts as native, video_parts as raster
from scripts.cyclonev_rbf import SX120F, CramRect, LoadedRbf, classify_cram_diff, cram_set, overlay_cram


class NativeVideoProducerTest(unittest.TestCase):
    def test_video_route_argv_sets_critexp_only_for_native_layouts(self):
        class ReachedRoute(Exception):
            pass

        cases = ((native, "direct", True), (native, "scanlines", True),
                 (raster, "direct", False))
        for layout, variant, expect_critexp in cases:
            with self.subTest(layout=layout.__name__, variant=variant), \
                    tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary) / "repo"
                shell_root = Path(temporary) / "shell"
                root.mkdir()
                shell_root.mkdir()
                (shell_root / "manifest.toml").write_bytes(b"manifest")
                (shell_root / "core.rbf").write_bytes(b"core-rbf")
                (shell_root / "routed.json").write_bytes(b"routed")
                (shell_root / "socket.qsf").write_text(
                    f'FES_RESERVED_RECT "{layout.PLACEMENT}"\n')
                package = SimpleNamespace(
                    fields={"format": 4, "interfaces": [{"id": layout.INTERFACE,
                        "major": 1, "minor": 0, "required": False}]},
                    manifest_bytes=b"manifest", payload_bytes=b"core-rbf", package_id="fixture")
                tools = {name: SimpleNamespace(path=Path("/tool") / name, identity="fixture")
                         for name in ("yosys", "nextpnr-mistral")}
                invocation = SimpleNamespace(inputs={}, env={}, verify=Mock(), close=Mock())
                all_inputs = (*part.INPUTS, *part.NATIVE_INPUTS)
                commands = []

                def prepare_output(build_root, *, relative, build_outputs):
                    (build_root / relative).mkdir(parents=True, exist_ok=True)

                def prepare_scaffold(source, destination, *, layout):
                    destination.write_bytes(b"scaffold")
                    return b"scaffold"

                def run_tool(command, build_root, log_path, **kwargs):
                    commands.append(command)
                    if log_path.name == "synthesis.log":
                        log_path.write_text("")
                        (log_path.parent / "cart.json").write_bytes(b"{}")
                        return
                    raise ReachedRoute

                with patch.object(part, "read_package", return_value=package), \
                        patch.object(part, "_require_clean_source", return_value=(None, "revision")), \
                        patch.object(part, "source_roots_for_inputs", return_value=()), \
                        patch.object(part, "source_input_closure",
                                     return_value={name: "fixture" for name in all_inputs}), \
                        patch.object(part, "_prepare_output", side_effect=prepare_output), \
                        patch.object(part, "prepare_scaffold", side_effect=prepare_scaffold), \
                        patch.object(part, "_run_tool", side_effect=run_tool), \
                        patch.object(part.shell_recipe, "authenticate_tools", return_value=tools), \
                        patch.object(part.sgm, "cart_clock_constraints", return_value=b"constraints"), \
                        patch.object(part, "FunctionalInvocation", return_value=invocation), \
                        patch.object(part.native_video_clock, "prepare_native_clock",
                                     return_value={"fixture": True}):
                    with self.assertRaises(ReachedRoute):
                        part.build(root, shell_root, Path(temporary) / "package.tar", variant)

                self.assertEqual(len(commands), 2)
                route_argv = commands[1]
                if expect_critexp:
                    self.assertIn("--placer-heap-critexp", route_argv)
                    self.assertEqual(route_argv[route_argv.index("--placer-heap-critexp") + 1], "2")
                else:
                    self.assertNotIn("--placer-heap-critexp", route_argv)
                invocation.verify.assert_not_called()
                invocation.close.assert_called_once_with()

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
                       "FES_COLECO_EXPANSION_V2_DEV", "FES_COLECO_ROM_LINK"):
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
        evidence = {"rbf": {"size": 100, "sha256": "b" * 64}, "build_id": "c" * 32,
                    "rom_map": {"file": "rom-map.json", "size": 32, "sha256": "e" * 64}}
        identities = {name: "fixture" for name in ("yosys", "nextpnr-mistral", "mistral")}
        arguments = (record, evidence, "https://example.invalid/fes.git", "d" * 40, identities)
        for layout, flags in ((native, {"native_video": True}), (raster, {"video_socket": True})):
            fields = tomllib.loads(shell.manifest(*arguments, **flags).decode())
            self.assertEqual(fields["format"], 4)
            self.assertEqual(fields["roms"], [
                {"id": "coleco-bios", "role": "firmware", "source_size": 8192, "source_offset": 0},
                {"id": "coleco-cart", "role": "cartridge", "source_size": 16384, "source_offset": 8192}])
            self.assertEqual(fields["rom_map"], evidence["rom_map"])
            self.assertFalse(any(row["id"].startswith("fes.media.blob") or row["id"] == "fes.firmware.blob"
                                 for row in fields["interfaces"]))
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
            invalid.append({**fields, "format": 3})
            invalid.append({**fields, "interfaces": []})
            for bad in invalid:
                with self.subTest(fields=bad), self.assertRaisesRegex(ValueError, "one exact sealed"):
                    part.package_profile(SimpleNamespace(fields=bad))

    def test_native_shell_carries_the_session_plane(self):
        record = b'{"format":2,"parameters":{},"recipe_sha256":"' + b'a' * 64 + b'"}'
        evidence = {"rbf": {"size": 100, "sha256": "b" * 64}, "build_id": "c" * 32,
                    "rom_map": {"file": "rom-map.json", "size": 32, "sha256": "e" * 64}}
        identities = {name: "fixture" for name in ("yosys", "nextpnr-mistral", "mistral")}
        arguments = (record, evidence, "https://example.invalid/fes.git", "d" * 40, identities)
        plane = [{"id": interface, "major": 1, "minor": 0, "required": True}
                 for interface in ("fes.memory.hps-ddr", "fes.video.session-display",
                                   "fes.fabric.video.display-plane")]
        fields = tomllib.loads(shell.manifest(*arguments, native_video=True).decode())
        self.assertEqual(fields["core"]["version"], "1.5.0")
        for row in plane:
            self.assertIn(row, fields["interfaces"])
        raster_fields = tomllib.loads(shell.manifest(*arguments, video_socket=True).decode())
        self.assertFalse(any(row in raster_fields["interfaces"] for row in plane))
        # Only the native shell drops -nodsp, for the plane's three products.
        tools = {name: Path("/tool") / name for name in ("yosys", "nextpnr-mistral")}
        native_program = shell.build_commands(shell.ROOT, shell.ROOT / shell.NATIVE_VIDEO_OUTPUT_RELATIVE,
                                              "a" * 32, tools, native_video=True)[0][-1]
        raster_program = shell.build_commands(shell.ROOT, shell.ROOT / shell.VIDEO_OUTPUT_RELATIVE,
                                              "a" * 32, tools, video_socket=True)[0][-1]
        self.assertNotIn("-nodsp", native_program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", raster_program)
        for source in shell.DISPLAY_PLANE_SOURCES:
            self.assertIn(source, native_program)
            self.assertNotIn(source, raster_program)

    def test_plane_products_on_a_yosys_without_sign_parameters(self):
        from scripts import fes_de10nano_evidence as evidence

        def design(cells):
            return {"modules": {"top": {"cells": cells}}}

        good = {f"display.blend.mul{i}": {"type": "MISTRAL_MUL9X9", "parameters": {}} for i in range(3)}
        evidence.plane_dsp_evidence(design(good), sign_parameters=False)
        # Coleco's Yosys never emits sign parameters: one appearing means the
        # toolchain moved, and a stray product is never the plane's.
        signed = copy.deepcopy(good)
        signed["display.blend.mul0"]["parameters"]["A_SIGNED"] = "0"
        stray = {**good, "machine.vdp.mul": {"type": "MISTRAL_MUL9X9", "parameters": {}}}
        missing = dict(list(good.items())[:2])
        for cells in (signed, stray, missing):
            with self.subTest(cells=sorted(cells)), self.assertRaises(ValueError):
                evidence.plane_dsp_evidence(design(cells), sign_parameters=False)
        # With sign parameters, an absent one is not proof of unsigned.
        with self.assertRaises(ValueError):
            evidence.plane_dsp_evidence(design(good))

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
