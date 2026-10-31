"""Offline descriptor bounds and the unqualified diagnostic output contract."""
from contextlib import redirect_stderr, redirect_stdout
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("de25_dma_descriptor_tested", ROOT / "scripts/de25_dma_descriptor.py")
DMA = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DMA)


class ProbeDescriptorTests(unittest.TestCase):
    def test_low_iova_is_valid_without_claiming_physical_ddr_or_admission(self):
        plan = DMA.probe_plan(0x10000000, 4096, 32, 256, 0x123456789abcdef0, 7)
        self.assertEqual(plan["bounds"], {"grant_start": 0x10000000, "grant_end_exclusive": 0x10001000,
                                          "read_start": 0x10000020, "read_end_exclusive": 0x10000120})
        self.assertEqual(plan["address_space"], "dma-bus-or-iova")
        self.assertEqual(plan["admission"], "not-qualified")
        self.assertEqual(plan["hardware_access"], "not-performed")
        self.assertEqual(plan["translation"], "not-qualified")
        self.assertEqual(plan["firewall"], "not-qualified")
        self.assertEqual(plan["boot_pairing"], "not-qualified")
        self.assertEqual(plan["endpoint"]["lightweight_peripheral_offset"], 0x10100)
        self.assertEqual(plan["candidate_cpu_control_window"], {
            "architectural_lw_base": 0x20000000, "derived_endpoint_address": 0x20010100,
            "admission": "not-qualified"})

    def test_write_plan_serializes_generation_and_starts_only_after_all_fields(self):
        writes = DMA.probe_plan(0x10000000, 4096, 16, 32, 0x123456789abcdef0, 0xffffffff)["register_writes"]
        self.assertEqual([(item["offset"], item["value"]) for item in writes], [
            (0x10, 0x10000000), (0x14, 0), (0x18, 4096), (0x1c, 16), (0x20, 32),
            (0x24, 0x9abcdef0), (0x28, 0x12345678), (0x2c, 0xffffffff), (0x0c, 1)])
        self.assertEqual(writes[-1]["register"], "command_start")

    def test_end_exclusive_can_equal_four_gib_without_wrapping(self):
        plan = DMA.probe_plan(0xffffffe0, 32, 16, 16, (1 << 64) - 1, 1)
        self.assertEqual(plan["bounds"]["grant_end_exclusive"], 1 << 32)
        self.assertEqual(plan["bounds"]["read_end_exclusive"], 1 << 32)
        self.assertEqual(plan["register_writes"][5]["value"], 0xffffffff)
        self.assertEqual(plan["register_writes"][6]["value"], 0xffffffff)

    def test_large_read_crossing_four_kib_is_left_for_bounded_rtl_bursts(self):
        plan = DMA.probe_plan(0x10000000, 8192, 4080, 512, 1, 1)
        self.assertEqual(plan["descriptor"]["read_bytes"], 512)
        self.assertEqual(plan["burst_rules"]["max_beats"], 16)
        self.assertEqual(plan["burst_rules"]["boundary_bytes"], 4096)

    def test_rejects_wrong_integer_widths_zero_sizes_and_bad_alignment(self):
        valid = [0x10000000, 4096, 0, 16, 1, 1]
        mutations = [(0, -32), (0, 1 << 64), (0, 0x10000010), (0, True),
                     (1, 0), (1, 1 << 32), (1, 17),
                     (2, -16), (2, 1), (2, 1 << 32),
                     (3, 0), (3, 17), (3, 1 << 32),
                     (4, 0), (4, 1 << 64), (4, False),
                     (5, 0), (5, 1 << 32), (5, 1.0)]
        for field, value in mutations:
            args = valid.copy()
            args[field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                DMA.probe_plan(*args)

    def test_rejects_upper_dma_bits_and_overflow_even_if_read_subset_fits(self):
        for base, span in ((1 << 32, 16), (0xffffffe0, 48), (0x10000000, 0xfffffff0)):
            with self.subTest(base=base, span=span), self.assertRaisesRegex(ValueError, "32-bit bus"):
                DMA.probe_plan(base, span, 0, 16, 1, 1)

    def test_read_must_fit_its_grant_including_the_entire_last_beat(self):
        for offset, length in ((4096, 16), (4080, 32), (4112, 16)):
            with self.subTest(offset=offset, length=length), self.assertRaisesRegex(ValueError, "outside its DMA grant"):
                DMA.probe_plan(0x10000000, 4096, offset, length, 1, 1)


class FrameDescriptorTests(unittest.TestCase):
    def test_two_slots_use_the_shared_geometry_and_distinct_frame_ranges(self):
        plans = [DMA.frame_plan(0x10000000, 8 << 20, slot, 3, 4) for slot in (0, 1)]
        for slot, plan in enumerate(plans):
            with self.subTest(slot=slot):
                frame = plan["frame"]
                self.assertEqual((frame["width"], frame["height"], frame["stride"], frame["frame_bytes"]),
                                 (1280, 720, 5120, 3686400))
                self.assertEqual((frame["pixel_format"], frame["slots"], frame["slot_bytes"]),
                                 ("bgrx8888", 2, 4 << 20))
                self.assertEqual(frame["slot_dma_addresses"], [0x10000000, 0x10400000])
                self.assertEqual(plan["descriptor"]["read_offset_bytes"], slot * (4 << 20))
                self.assertEqual(plan["descriptor"]["read_bytes"], 3686400)
                self.assertEqual(plan["admission"], "not-qualified")
        self.assertLess(plans[0]["bounds"]["read_end_exclusive"], plans[1]["bounds"]["read_start"])

    def test_full_frame_allocation_may_end_exactly_at_bus_limit(self):
        plan = DMA.frame_plan((1 << 32) - (8 << 20), 8 << 20, 1, 1, 1)
        self.assertEqual(plan["bounds"]["grant_end_exclusive"], 1 << 32)
        self.assertLessEqual(plan["bounds"]["read_end_exclusive"], 1 << 32)

    def test_rejects_partial_oversized_allocation_or_invalid_slot(self):
        for size, slot in ((4 << 20, 0), ((8 << 20) + 16, 0), (8 << 20, -1),
                           (8 << 20, 2), (8 << 20, True)):
            with self.subTest(size=size, slot=slot), self.assertRaises(ValueError):
                DMA.frame_plan(0x10000000, size, slot, 1, 1)


class ContractAndCliTests(unittest.TestCase):
    def test_uses_the_canonical_generated_contract_bytes(self):
        constants, digest = DMA.load_contract()
        self.assertEqual(constants["Identity"], 0x44323550)
        self.assertEqual(constants["Version"], 0x10000)
        self.assertEqual(constants["AllocationBytes"], 8 << 20)
        self.assertEqual(digest, hashlib.sha256(DMA.CONTRACT.read_bytes()).hexdigest())

    def test_rejects_missing_duplicate_or_executable_constant_expressions(self):
        original = DMA.CONTRACT.read_text()
        definition = "constexpr std::uint32_t FesDe25DdrProbeBeatBytes = 0x10u;"
        self.assertIn(definition, original)
        variants = [original.replace(definition, ""), original + "\n" + definition,
                    original.replace(definition, definition.replace("0x10u", "(8u + 8u)")),
                    original.replace("fes_fabric_de25_ddr_probe.yaml.", "unrelated.yaml."),
                    original.replace("FesDe25DdrProbeArchitecturalLwSpan = 0x20000000u;",
                                     "FesDe25DdrProbeArchitecturalLwSpan = 0x10100u;")]
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "constants.hpp"
            for source in variants:
                path.write_text(source)
                with self.subTest(source=source[-90:]), self.assertRaises(ValueError):
                    DMA.load_contract(path)

    def test_cli_prints_only_a_complete_unqualified_json_plan(self):
        output = io.StringIO()
        with redirect_stdout(output):
            result = DMA.main(["probe", "--dma-base", "0x10000000", "--grant-bytes", "4096",
                               "--length", "256", "--generation", "1", "--sequence", "2"])
        plan = json.loads(output.getvalue())
        self.assertEqual(result, 0)
        self.assertEqual(plan["mode"], "probe")
        self.assertEqual(plan["admission"], "not-qualified")
        self.assertEqual(len(plan["register_writes"]), 9)

    def test_frame_cli_selects_only_the_requested_slot_read_range(self):
        output = io.StringIO()
        with redirect_stdout(output):
            result = DMA.main(["frame", "--dma-base", "0x10000000", "--allocation-bytes", "8388608",
                               "--slot", "1", "--generation", "1", "--sequence", "2"])
        plan = json.loads(output.getvalue())
        self.assertEqual(result, 0)
        self.assertEqual(plan["mode"], "frame")
        self.assertEqual(plan["bounds"]["read_start"], 0x10400000)
        self.assertEqual(plan["bounds"]["read_end_exclusive"], 0x10400000 + 3686400)
        self.assertIn("not a display submission", plan["frame"]["purpose"])

    def test_invalid_cli_never_prints_a_partial_plan_or_accepts_qualification_flags(self):
        valid = ["probe", "--dma-base", "0x10000000", "--grant-bytes", "4096",
                 "--length", "256", "--generation", "1", "--sequence", "2"]
        invalid = [valid + ["--hardware-qualified"],
                   [value if value != "4096" else "0" for value in valid],
                   [value if value != "0x10000000" else "0x100000000" for value in valid],
                   [value if value != "256" else "1.5" for value in valid]]
        for args in invalid:
            output, errors = io.StringIO(), io.StringIO()
            with self.subTest(args=args), redirect_stdout(output), redirect_stderr(errors), self.assertRaises(SystemExit):
                DMA.main(args)
            self.assertEqual(output.getvalue(), "")
            self.assertTrue(errors.getvalue())


if __name__ == "__main__":
    unittest.main()
