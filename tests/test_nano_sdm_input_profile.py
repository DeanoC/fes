"""Native input/retirement selection and source binding, without a board."""
import copy
import hashlib
import importlib.util
import json
import re
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"
sys.path[:0] = [str(FW), str(ROOT / "platform/embedded/nano")]
import nano_sdm_input_profile as mode
import fit_layout


class SourceOperationBudgetTests(unittest.TestCase):
    def test_firmware_native_and_sealed_source_budgets_agree(self):
        bound = mode.CONTRACT['transfer_bound_us']
        self.assertEqual(bound, 25000000)
        header = (FW/'input_transfer.h').read_text()
        self.assertEqual(int(re.search(r'#define NANO_SDM_TRANSFER_US UINT64_C\((\d+)\)', header)[1]), bound)
        native = ROOT/'platform/embedded/tamago/nativefabric'
        code = (native/'program_service.go').read_text()
        seconds = int(re.search(r'const sourceOperationTimeout = (\d+) \* time.Second', code)[1])
        self.assertEqual(seconds*1000000, bound)
        self.assertIn('elapsed >= sourceOperationTimeout', code)
        self.assertIn('now.Sub(start) >= sourceOperationTimeout', (native/'window_service.go').read_text())
        for contract in (mode.CONTRACT, mode.REARM_CONTRACT, mode.RECOVERY_CONTRACT):
            self.assertEqual(contract['transfer_bound_us'], bound)
            self.assertEqual(contract['preparation_bound_us'], 4000000)
            self.assertEqual(contract['command_bound_us'], 100000)
            self.assertEqual(contract['retirement_bound_us'], 4000000)
            self.assertEqual(contract['clock_origin'], 'before-additional-source-SHA384')
        self.assertEqual(mode.REARM_CONTRACT['source_release_bound_us'], 1000000)
        old = profile()
        old['sdm_input_transfer']['transfer_bound_us'] = 20000000
        with self.assertRaises(ValueError):
            mode.checked(old)


def profile():
    p = dict(chain_sha256="1" * 64, firmware_mmu={}, firmware_ram={},
                usb_shared=True, sd_pio=True, configuration_retained=True, capability_observation={"format": 2},
                hps_menu_program={"fixture": True}, hps_program_platform={"fixture": True},
                cpu_mapping=dict(ram_base=0x84000000, ram_bytes=0x10000000,
                                 dma_base=0x94800000, dma_bytes=0x800000,
                                 reader_base=0x95000000, reader_bytes=0x800000),
                hps_configuration={"base": 0x94800000}, boot_fit=dict(fit_layout.CONTRACT),
                sdm_input_transfer=dict(mode.CONTRACT))
    platform = dict(platform='de25_nano', device='A5EB013BB23BE4SCS', hps_io_hash='c'*64,
                    pfg_identity='fixture-Quartus-identity', hps_parameters_sha256='d'*64,
                    hps_configuration_sha256='e'*64)
    p['hps_context'] = dict(programming_profile='de25-native-computer-v1', package_id='a'*64)
    p['hps_program_platform'] = dict(platform=dict(platform))
    p['hps_menu_program'] = dict(context=dict(programming_profile='de25-embedded-hps-first-menu-prototype-v1',
                                            control_abi=0x10203043, package_id='b'*64),
                                 platform=dict(platform=dict(platform)),
                                 memory=dict(bus_base=0x95000000,bytes=0x800000),
                                 access=dict(aruser=224,arprot=3,arcache=2))
    p['sdm_reader_route'] = mode.reader._expected(p)
    return p


class InputProfileTests(unittest.TestCase):
    def test_selection_is_explicit_and_exclusive(self):
        self.assertIsNone(mode.selected(None, False))
        self.assertIsNone(mode.checked(None))
        self.assertEqual(mode.checked(profile()), mode.CONTRACT)
        for value in (0, 1, None, "yes", {}, []):
            with self.subTest(value=value), self.assertRaises(ValueError):
                mode.selected(profile(), value)
        for name in ("sdm_arm_probe", "sdm_fault64"):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "exclusive"):
                mode.selected(dict(profile(), **{name: {}}), True)

    def test_mutated_contract_and_incomplete_ownership_are_rejected(self):
        for name in mode.CONTRACT:
            p = profile()
            p["sdm_input_transfer"][name] = "foreign"
            with self.subTest(contract=name), self.assertRaises(ValueError):
                mode.checked(p)
        for name in ("firmware_mmu", "firmware_ram", "usb_shared", "sd_pio",
                     "capability_observation", "configuration_retained", "hps_menu_program", "hps_program_platform", "boot_fit", "sdm_reader_route"):
            p = profile()
            del p[name]
            with self.subTest(missing=name), self.assertRaises(ValueError):
                mode.selected(p, True)
        for name in profile()["cpu_mapping"]:
            p = profile()
            p["cpu_mapping"][name] += 4096
            with self.subTest(memory=name), self.assertRaises(ValueError):
                mode.selected(p, True)

    def test_disabled_headers_keep_diagnostics_off(self):
        for name in ("FES_NANO_SDM_INPUT_TRANSFER", "FES_NANO_RETIRE_TIMING", "FES_NANO_RETIRE_QUEUE_OBSERVE", "FES_NANO_RETIRE_BATCHED_OBSERVE"):
            self.assertIn("#define " + name + " 0\n", mode.header(None))
            self.assertIn("#define " + name + " 1\n", mode.header(profile()))
        self.assertEqual(mode.CONTRACT["retirement_bound_us"], 4000000)
        self.assertFalse(mode.CONTRACT["retirement_memory_reuse"])

    def test_previous_contracts_cannot_admit_queue_observation_candidate(self):
        for version, name in ((7, "2026-11-13-de25-matched-command-snapshot/matched-with-cause-result.json"),
                              (8, "2026-11-13-de25-command-guard/matched-result.json"),
                              (9, "2026-11-13-de25-command-progress/matched-result.json"),
                              (10, "2026-11-13-de25-command-observe/matched-result.json"),
                              (11, "2026-11-13-de25-command-completion/matched-chain.json"),
                              (12, "2026-11-13-de25-retained-continuation/matched-chain.json"),
                              (13, "2026-11-13-de25-retained-native-control/matched-build.json")):
            record = json.loads((ROOT / "docs/validation" / name).read_text())
            previous = record["sdm_input_contract"]
            self.assertEqual(previous["format"], version)
            p = profile()
            p["sdm_input_transfer"] = previous
            with self.subTest(version=version), self.assertRaisesRegex(ValueError, "unsupported SDM input"):
                mode.checked(p)

    def test_publication_timing_cannot_be_supplied_by_previous_contracts(self):
        previous = json.loads((ROOT / "tests/fixtures/nano-sdm-input/previous-retirement-timing.json").read_text())
        for name, version in (("base", 14), ("rearm", 15)):
            old = previous[name]
            self.assertEqual(old["format"], version)
            for replacement in (version, 16, 17):
                p = profile()
                p["sdm_input_transfer"] = dict(old, format=replacement)
                with self.subTest(name=name, replacement=replacement), self.assertRaisesRegex(ValueError, "unsupported SDM input"):
                    mode.checked(p)
        contract = mode.checked(profile())
        self.assertEqual((contract["retirement_command_staging_bound_us"],
                          contract["retirement_command_completion_bound_us"],
                          contract["retirement_bound_us"]), (2000000, 1000000, 4000000))
        # Preparation has its explicit startup budget; other phases remain separate.
        self.assertEqual((contract["command_bound_us"], contract["preparation_bound_us"],
                          contract["transfer_bound_us"]), (100000, 4000000, 25000000))

    def test_old_preparation_clock_contracts_cannot_supply_publication_timing(self):
        for current, version, scope in ((mode.CONTRACT, 19, "nano-retained-source-preparation-budget-v19"),
                                        (mode.REARM_CONTRACT, 20, "nano-owned-source-release-rearm-v20"),
                                        (mode.RECOVERY_CONTRACT, 21, "nano-source-rearm-terminal-cold-recovery-v21")):
            old = copy.deepcopy(current)
            for key in tuple(old):
                if key.startswith("preparation_command_"):
                    del old[key]
            old.update(format=version, scope=scope)
            for change in ({}, {"format": current["format"], "scope": current["scope"]},
                           {key: value for key, value in current.items() if key.startswith("preparation_command_")}):
                p = profile();p["sdm_input_transfer"] = dict(old, **change)
                with self.subTest(version=version, change=change), self.assertRaisesRegex(ValueError, "unsupported SDM input"):
                    mode.checked(p)
        contract = mode.checked(profile())
        self.assertEqual((contract["preparation_command_policy"], contract["preparation_command_staging_bound_us"],
                          contract["preparation_command_completion_bound_us"]), (4, 4000000, 100000))

    def test_one_second_profiles_cannot_admit_the_four_second_candidate(self):
        # Exact prior contracts, and hybrids with either the new version or
        # new duration alone, must not be mistaken for this candidate.
        for current, old_version, old_scope in (
                (mode.CONTRACT, 16, "nano-retained-source-publication-timing-v16"),
                (mode.REARM_CONTRACT, 17, "nano-owned-source-release-rearm-v17"),
                (mode.RECOVERY_CONTRACT, 18, "nano-source-rearm-terminal-cold-recovery-v18")):
            old = copy.deepcopy(current)
            old.update(format=old_version, scope=old_scope, preparation_bound_us=1000000)
            for change in ({}, {"format": current["format"]},
                           {"preparation_bound_us": 4000000},
                           {"format": current["format"], "scope": current["scope"]}):
                p = profile()
                p["sdm_input_transfer"] = dict(old, **change)
                with self.subTest(version=old_version, change=change), self.assertRaisesRegex(
                        ValueError, "unsupported SDM input"):
                    mode.checked(p)

    def fixture(self, path):
        names = (mode.SERVICE, mode.MAILBOX, mode.MAILBOX_HEADER, mode.PLATFORM,
                 mode.LD, mode.MK, mode.HEADER)
        for name in names:
            (path / name).parent.mkdir(parents=True, exist_ok=True)
        for target, source in ((mode.SERVICE, "service-before.c"),
                               (mode.MAILBOX, "mailbox-before.c"),
                               (mode.MAILBOX_HEADER, "mailbox-before.h")):
            (path / target).write_bytes((ROOT / "tests/fixtures/nano-sdm-input" / source).read_bytes())
        (path / mode.PLATFORM).write_text("/* fixture */\n")
        (path / mode.MK).write_text("BL31_SOURCES += existing.c\n")
        (path / mode.LD).write_text("MEMORY {\n}\nSECTIONS {\n    /DISCARD/ : {\n}\n}\n")
        (path / mode.HEADER).write_text(mode.header(profile()) + "#define FES_NANO_SDM_FAULT64 0\n#define FES_NANO_SDM_ARM_PROBE 0\n")
        return (dict(generated_sha256=mode.INPUT_SERVICE, mailbox_generated_sha256=mode.INPUT_MAILBOX),
                dict(sources={}), dict(sources={str(mode.PLATFORM): "fixture"}),
                dict(generated_sha256=mode.INPUT_MAILBOX, header_sha256=mode.INPUT_HEADER))

    def test_install_binds_every_source_and_refuses_reuse(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            args = self.fixture(path)
            binding = mode.install(path, profile(), *args)
            mode.verify_installed(path, binding, profile())
            self.assertEqual(set(binding["sources"]), set(mode.NAMES))
            for name in mode.NAMES:
                target = path / name
                original = target.read_bytes()
                target.write_bytes(original + b"changed")
                with self.subTest(source=name), self.assertRaises(ValueError):
                    mode.verify_installed(path, binding, profile())
                target.write_bytes(original)
            with self.assertRaises(ValueError):
                mode.install(path, profile(), *args)

    def test_invalid_predecessor_rejected_before_writes(self):
        for case in range(5):
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory)
                args = list(self.fixture(path))
                if case == 0:
                    args[0]["generated_sha256"] = "0" * 64
                elif case == 1:
                    args[2]["sources"] = {}
                elif case == 2:
                    args[3]["header_sha256"] = "0" * 64
                else:
                    (path / (mode.MAILBOX if case == 3 else mode.MAILBOX_HEADER)).write_text("foreign")
                before = {str(p.relative_to(path)): p.read_bytes() for p in path.rglob("*") if p.is_file()}
                with self.subTest(case=case), self.assertRaises(ValueError):
                    mode.install(path, profile(), *args)
                self.assertEqual(before, {str(p.relative_to(path)): p.read_bytes() for p in path.rglob("*") if p.is_file()})
