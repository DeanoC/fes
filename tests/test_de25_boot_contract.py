"""Bounded boot-artifact inspection and fail-closed HPS receipt checks."""
import hashlib
import importlib.util
import json
from pathlib import Path
import struct
import tempfile
from types import SimpleNamespace
import unittest
import zlib


ROOT = Path(__file__).resolve().parents[1]


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


CONTRACT = load_module("de25_boot_contract_tested", ROOT / "scripts/de25_boot_contract.py")
BOOT = load_module("de25_vendor_boot_for_contract_tests", ROOT / "image/de25-nano/boot.py")


def word(value):
    return struct.pack(">I", value)


def padded(data):
    return data + bytes(-len(data) % 4)


def fdt_sections(structure, names=b""):
    tree_offset = 56  # Header followed by an empty reservation-map terminator.
    names_offset = tree_offset + len(structure)
    total = names_offset + len(names)
    header = struct.pack(">10I", 0xd00dfeed, total, tree_offset, names_offset,
                         40, 17, 16, 0, len(names), len(structure))
    return header + bytes(16) + structure + names


def node(name="", properties=(), children=()):
    return name, properties, children


def fdt(root):
    """Encode small independent fixtures using the flattened-tree wire format."""
    names = bytearray()
    offsets = {}

    def encode(item):
        name, properties, children = item
        data = word(1) + padded(name.encode("ascii") + b"\0")
        for key, value in properties:
            if key not in offsets:
                offsets[key] = len(names)
                names.extend(key.encode("ascii") + b"\0")
            data += word(3) + word(len(value)) + word(offsets[key]) + padded(value)
        return data + b"".join(encode(child) for child in children) + word(2)

    structure = encode(root) + word(9)
    return fdt_sections(structure, bytes(names))


def image(name, payload, location="embedded", offset=0, algorithm="crc32", hash_value=None):
    if hash_value is None:
        hash_value = (word(zlib.crc32(payload) & 0xffffffff) if algorithm == "crc32"
                      else hashlib.sha256(payload).digest())
    props = [("type", b"firmware\0")]
    if location == "embedded":
        props.append(("data", payload))
    else:
        props.extend(((location, word(offset)), ("data-size", word(len(payload)))))
    return node(name, props, [node("hash-1", [("algo", algorithm.encode() + b"\0"),
                                           ("value", hash_value)])])


class FdtTests(unittest.TestCase):
    def test_nested_binary_and_empty_properties_preserve_bytes_and_total(self):
        blob = fdt(node(properties=[("model", b"board\0"), ("empty", b"")],
                        children=[node("bus@0", [("reg", word(0) + word(256))])]))
        nodes, total = CONTRACT.parse_fdt(blob + b"external FIT payload")
        self.assertEqual(total, len(blob))
        self.assertEqual(nodes, {"/": {"model": b"board\0", "empty": b""},
                                 "/bus@0": {"reg": word(0) + word(256)}})

    def test_rejects_truncated_overlapping_and_nonempty_reservation_sections(self):
        blob = fdt(node())
        malformed = [blob[:39]]
        for offset, value in ((0, 0), (4, len(blob) + 1), (8, 40), (12, 56), (20, 16)):
            changed = bytearray(blob)
            struct.pack_into(">I", changed, offset, value)
            malformed.append(changed)
        changed = bytearray(blob)
        changed[40] = 1
        malformed.append(changed)
        for changed in malformed:
            with self.subTest(data=bytes(changed).hex()), self.assertRaises(ValueError):
                CONTRACT.parse_fdt(changed)

    def test_rejects_invalid_structure_and_duplicate_node_or_property(self):
        begin = word(1) + bytes(4)
        malformed = [
            fdt_sections(begin + word(99) + word(2) + word(9)),
            fdt_sections(word(2) + word(9)),
            fdt_sections(begin + word(9)),
            fdt_sections(begin + word(2)),
            fdt_sections(begin + word(2) + word(9) + word(4)),
            fdt(node(children=[node("bad/name")])),
            fdt(node(children=[node("same"), node("same")])),
            fdt(node(properties=[("same", b"a"), ("same", b"b")])),
        ]
        for changed in malformed:
            with self.subTest(data=changed.hex()), self.assertRaises(ValueError):
                CONTRACT.parse_fdt(changed)

    def test_rejects_property_name_offsets_and_unterminated_strings(self):
        begin = word(1) + bytes(4)
        for names, offset in ((b"name\0", 5), (b"unterminated", 0), (b"\0", 0)):
            data = fdt_sections(begin + word(3) + word(0) + word(offset) + word(2) + word(9), names)
            with self.subTest(names=names, offset=offset), self.assertRaises(ValueError):
                CONTRACT.parse_fdt(data)

    def test_rejects_property_value_outside_structure(self):
        structure = word(1) + bytes(4) + word(3) + word(32) + word(0) + word(2) + word(9)
        with self.assertRaisesRegex(ValueError, "outside structure"):
            CONTRACT.parse_fdt(fdt_sections(structure, b"data\0"))


class FitTests(unittest.TestCase):
    def test_embedded_payloads_are_hashed_as_bytes(self):
        payload = b"\0binary\xfffirmware\0"
        blob = fdt(node(children=[node("images", children=[
            image("firmware", payload), image("tree", b"dtb", algorithm="sha256")])]))
        _, images = CONTRACT.fit_images(blob)
        self.assertEqual(images["firmware"]["data"], payload)
        self.assertEqual(images["firmware"]["sha256"], hashlib.sha256(payload).hexdigest())
        self.assertIsNone(images["firmware"]["file_offset"])
        self.assertEqual(images["tree"]["data"], b"dtb")

    def test_external_offsets_and_absolute_positions_resolve_after_tree(self):
        payload = b"external firmware"
        for location in ("data-offset", "data-position"):
            with self.subTest(location=location):
                make = lambda offset: fdt(node(children=[node("images", children=[
                    image("firmware", payload, location, offset)])]))
                prefix = make(0)
                start = (len(prefix) + 3) & ~3
                prefix = make(0 if location == "data-offset" else start)
                _, images = CONTRACT.fit_images(padded(prefix) + payload)
                self.assertEqual(images["firmware"]["data"], payload)
                self.assertEqual(images["firmware"]["file_offset"], start)

    def test_rejects_external_data_inside_tree_or_beyond_file(self):
        for location, offset, tail in (("data-position", 40, b"payload"),
                                       ("data-offset", 4096, b"payload"),
                                       ("data-offset", 0, b"short")):
            blob = fdt(node(children=[node("images", children=[
                image("firmware", b"payload", location, offset)])]))
            with self.subTest(location=location, offset=offset), self.assertRaisesRegex(ValueError, "outside file"):
                CONTRACT.fit_images(padded(blob) + tail)

    def test_rejects_crc_mismatch_ambiguous_locations_and_missing_hash(self):
        fixtures = [image("firmware", b"payload", hash_value=bytes(4)),
                    node("firmware", [("type", b"firmware\0"), ("data", b"payload"),
                                      ("data-offset", word(0))]),
                    node("firmware", [("type", b"firmware\0"), ("data", b"payload")])]
        for fixture in fixtures:
            with self.subTest(fixture=fixture), self.assertRaises(ValueError):
                CONTRACT.fit_images(fdt(node(children=[node("images", children=[fixture])])))


class BootSelectionTests(unittest.TestCase):
    def inspect_fixture(self, directory, selected="board-good", include_tree=True, after_verify=None,
                        script_data=None, uboot_environment=None):
        tree = fdt(node(properties=[("model", b"SoCFPGA Agilex5 Terasic DE25-Nano\0"),
                                    ("compatible", b"intel,socfpga-agilex5\0")], children=[
            node("aliases", [("serial0", b"/soc@0/serial@10c02100\0"),
                             ("ethernet0", b"/soc@0/ethernet@10810000\0")]),
            node("soc@0", children=[node("serial@10c02100", [("reg", word(0x10c02100) + word(256))]),
                                    node("ethernet@10810000", [("reg", word(0x10810000) + word(256))])])]))
        (directory / "socfpga_agilex5_de25_nano.dtb").write_bytes(tree)
        if script_data is None:
            script_data = (b'bridge enable;\nsetenv bootargs "console=ttyS0,115200 root=${mmcroot} rw rootwait";\n'
                           b'fatload mmc 0:1 ${kernel_addr_r} Image;\n'
                           b'fatload mmc 0:1 ${fdt_addr_r} socfpga_agilex5_de25_nano.dtb;\n'
                           b'booti ${kernel_addr_r} - ${fdt_addr_r};\n')
        script = script_data
        script_image = image("script", script)
        script_image[1][0] = ("type", b"script\0")
        (directory / "boot.scr.uimg").write_bytes(fdt(node(children=[
            node("images", [("default", b"script\0")], [script_image])])))
        if uboot_environment is None:
            uboot_environment = b"mmcroot=/dev/mmcblk0p2\0"
        images = [image("atf", b"trusted firmware"), image("uboot", uboot_environment)]
        if include_tree:
            images.append(image("fdt-0", tree))
        roles = [("firmware", b"atf\0"), ("loadables", b"uboot\0"), ("fdt", b"fdt-0\0")]
        (directory / "u-boot.itb").write_bytes(fdt(node(children=[node("images", children=images),
            node("configurations", [("default", selected.encode() + b"\0")], [
                node("board-other", [("firmware", b"other\0"), *roles[1:]]), node("board-good", roles)])])))
        lock = {"boot_files": [{"name": name, "size": (directory / name).stat().st_size,
                                "sha256": BOOT.digest(directory / name)} for name in (
                                    "socfpga_agilex5_de25_nano.dtb", "boot.scr.uimg", "u-boot.itb")]}

        def verify_prepared_boot(*_):
            if after_verify:
                after_verify(directory)

        boot = SimpleNamespace(_verify_prepared_boot=verify_prepared_boot, regular_file=BOOT.regular_file)
        return CONTRACT.inspect_boot(directory, lock, boot)

    def test_resolves_selected_configuration_and_keeps_mailbox_unqualified(self):
        with tempfile.TemporaryDirectory() as temporary:
            result = self.inspect_fixture(Path(temporary))
        self.assertEqual(result["fit_configuration"]["name"], "board-good")
        self.assertEqual(result["cpu_mailbox_address"], "not-established")
        self.assertTrue(result["boot_script"]["overrides_chosen_bootargs"])

    def test_rejects_selected_wrong_roles_and_missing_referenced_image(self):
        with tempfile.TemporaryDirectory() as temporary:
            for selected, include_tree in (("board-other", True), ("board-good", False)):
                with self.subTest(selected=selected), self.assertRaises(ValueError):
                    self.inspect_fixture(Path(temporary), selected, include_tree)

    def test_rejects_boot_bytes_replaced_after_initial_verification(self):
        def replace_unused_configuration(directory):
            path = directory / "u-boot.itb"
            original = path.read_bytes()
            # The selected configuration and all payload CRCs stay valid.
            changed = original.replace(b"board-other\0", b"board-otHer\0")
            self.assertNotEqual(original, changed)
            self.assertEqual(len(original), len(changed))
            path.write_bytes(changed)

        with tempfile.TemporaryDirectory() as temporary, self.assertRaisesRegex(
                ValueError, "decoded boot bytes differ from lock: u-boot.itb"):
            self.inspect_fixture(Path(temporary), after_verify=replace_unused_configuration)

    def test_rejects_later_bridge_and_bootargs_overrides(self):
        valid_script = (b'bridge enable;\nsetenv bootargs "console=ttyS0,115200 root=${mmcroot} rw rootwait";\n'
                        b'fatload mmc 0:1 ${kernel_addr_r} Image;\n'
                        b'fatload mmc 0:1 ${fdt_addr_r} socfpga_agilex5_de25_nano.dtb;\n'
                        b'booti ${kernel_addr_r} - ${fdt_addr_r};\n')
        invalid_scripts = [
            valid_script + b"bridge disable;\n",
            valid_script + b'setenv bootargs "console=ttyS0,115200 root=/dev/mmcblk0p1 rw rootwait";\n',
            valid_script.replace(b"bridge enable;", b"not-bridge enable;"),
        ]
        for script_data in invalid_scripts:
            with self.subTest(script=script_data), tempfile.TemporaryDirectory() as temporary, \
                    self.assertRaisesRegex(ValueError, "pinned boot script contract differs"):
                self.inspect_fixture(Path(temporary), script_data=script_data)

    def test_requires_the_exact_kernel_dtb_loads_and_boot_command(self):
        script = (b'bridge enable;\nsetenv bootargs "console=ttyS0,115200 root=${mmcroot} rw rootwait";\n'
                  b'echo socfpga_agilex5_de25_nano.dtb;\n'
                  b'fatload mmc 0:1 ${kernel_addr_r} not-Image;\n'
                  b'fatload mmc 0:1 ${fdt_addr_r} other.dtb;\n'
                  b'booti ${kernel_addr_r} - ${wrong_fdt};\n'
                  b'booti ${kernel_addr_r} - ${fdt_addr_r};\n')
        with tempfile.TemporaryDirectory() as temporary, self.assertRaisesRegex(
                ValueError, "pinned boot script contract differs"):
            self.inspect_fixture(Path(temporary), script_data=script)

    def test_uboot_comments_do_not_truncate_quoted_hashes(self):
        self.assertEqual(CONTRACT.uboot_commands('echo "value # kept"; # removed\nbridge enable'),
                         ['echo "value # kept"', "bridge enable"])

    def test_requires_one_exact_uboot_root_variable(self):
        invalid_environments = [
            b"oldmmcroot=/dev/mmcblk0p2\0",
            b"mmcroot=/dev/mmcblk0p2\0mmcroot=/dev/mmcblk0p1\0",
            b"mmcroot=/dev/mmcblk0p1\0",
        ]
        for environment in invalid_environments:
            with self.subTest(environment=environment), tempfile.TemporaryDirectory() as temporary, \
                    self.assertRaisesRegex(ValueError, "U-Boot root-device environment differs"):
                self.inspect_fixture(Path(temporary), uboot_environment=environment)


class HpsReceiptTests(unittest.TestCase):
    def fixture(self, directory):
        self.lock = {"resources": {"sha256": "b" * 64}}
        self.inputs = {"vendor_archive_sha256": "b" * 64, "source_classification": "committed",
                       "fes_revision": "a" * 40, "mailbox_peripheral_offset": 0x100a0}
        self.sof = directory / "project/output_files/golden_top.sof"
        self.sof.parent.mkdir(parents=True)
        self.sof.write_bytes(b"diagnostic SOF")
        self.handoff = self.sof.with_name("hps_bootloader_handoff.bin")
        handoff = bytearray(0x65d)
        handoff[:8] = b"BOOT" + word(0x50001)
        for offset, magic, size in ((0x10, b"PMUX", 0x190), (0x1a0, b"IOCT", 0x190),
                                    (0x330, b"FPGA", 0xc0), (0x3f0, b"DLAY", 0x190),
                                    (0x580, b"CLKS", 0xa0), (0x620, b"PERI", 0x14),
                                    (0x634, b"SDRM", 0x14)):
            handoff[offset:offset + 8] = magic + word(size)
        self.handoff.write_bytes(handoff)
        self.build = {"fes_revision": "a" * 40, "board": "de25_nano", "device": "A5EB013BB23BE4SCS",
                      "diagnostic_only": True, "artifact": "project/output_files/golden_top.sof",
                      "sha256": BOOT.digest(self.sof), "generated_handoff_and_sdc_sha256": {
                          "output_files/hps_bootloader_handoff.bin": BOOT.digest(self.handoff)}}
        self.write_receipts(directory)

    def write_receipts(self, directory):
        path = directory / "inputs.json"
        path.write_text(json.dumps(self.inputs))
        self.build["inputs_sha256"] = BOOT.digest(path)
        (directory / "build.json").write_text(json.dumps(self.build))

    def test_matching_receipts_remain_diagnostic_without_factory_pairing(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            self.fixture(directory)
            result = CONTRACT.inspect_hps(directory, self.lock, BOOT)
        self.assertEqual(result["sof_sha256"], self.build["sha256"])
        self.assertEqual(result["mailbox_relative_offset"], 0x100a0)
        self.assertEqual(result["cpu_mailbox_address"], "not-established")
        self.assertEqual(result["factory_boot_pairing"], "not-qualified")

    def test_rejects_modified_inputs_sof_or_handoff(self):
        for changed in ("inputs.json", "sof", "handoff"):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                self.fixture(directory)
                path = {"inputs.json": directory / "inputs.json", "sof": self.sof,
                        "handoff": self.handoff}[changed]
                path.write_bytes(path.read_bytes() + b"tampered")
                with self.assertRaises(ValueError):
                    CONTRACT.inspect_hps(directory, self.lock, BOOT)

    def test_rejects_wrong_source_board_device_and_artifact_namespace(self):
        mutations = [("inputs", "vendor_archive_sha256", "c" * 64),
                     ("inputs", "source_classification", "working-copy-diagnostic"),
                     ("inputs", "fes_revision", "d" * 40),
                     ("build", "fes_revision", "bad revision"),
                     ("build", "board", "de10_nano"), ("build", "device", "other"),
                     ("build", "diagnostic_only", False),
                     ("build", "artifact", "../outside.sof"),
                     ("build", "artifact", "/tmp/outside.sof")]
        for group, key, value in mutations:
            with self.subTest(group=group, key=key), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                self.fixture(directory)
                getattr(self, group)[key] = value
                self.write_receipts(directory)
                with self.assertRaises(ValueError):
                    CONTRACT.inspect_hps(directory, self.lock, BOOT)

    def test_rejects_rehashed_unsupported_handoff_and_symlinked_artifacts(self):
        for changed in ("header", "sof-link", "handoff-link", "inputs-link", "build-link"):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                self.fixture(directory)
                if changed == "header":
                    self.handoff.write_bytes(b"WRNG" + self.handoff.read_bytes()[4:])
                    self.build["generated_handoff_and_sdc_sha256"][
                        "output_files/hps_bootloader_handoff.bin"] = BOOT.digest(self.handoff)
                    self.write_receipts(directory)
                else:
                    path = {"sof-link": self.sof, "handoff-link": self.handoff,
                            "inputs-link": directory / "inputs.json",
                            "build-link": directory / "build.json"}[changed]
                    retained = path.with_suffix(".retained")
                    path.rename(retained)
                    path.symlink_to(retained.name)
                with self.assertRaises(ValueError):
                    CONTRACT.inspect_hps(directory, self.lock, BOOT)


if __name__ == "__main__":
    unittest.main()
