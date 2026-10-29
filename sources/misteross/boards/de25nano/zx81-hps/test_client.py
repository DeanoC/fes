"""Exercise the diagnostic client through a delayed, stateful register peer."""
import importlib.util
import io
import json
from contextlib import redirect_stdout, redirect_stderr
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import zlib

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25_computer_client", HERE / "client.py")
C = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(C)


class Clock:
    def __init__(self):
        self.now = 0.0

    def monotonic(self):
        return self.now

    def sleep(self, amount):
        self.now += amount


class Peer:
    """Protocol peer, not a physical-MMIO or FPGA model.

    A GPO write retains the request. Only later GPI reads publish its ACK.
    Transfer bytes are reconstructed independently from their wire pairs.
    """
    def __init__(self, constants):
        self.c = constants
        self.gpo = self.toggle = self.fault = 0
        self.response = self.error = 0
        self.pending = None
        self.polls = 0
        self.ack_after = 2
        self.never_ack = self.fault_on_write = self.fault_after_ack = False
        self.bad_echo = self.bad_signature = self.bad_success = False
        self.bad_crc = self.load_busy = False
        self.bad_identity = self.bad_capabilities = self.bad_build = False
        self.writes, self.commands, self.mutations = [], [], []
        self.rows = [0] * 9
        self.running = False
        self.state = self.c.abi("MEDIA_STATE_EMPTY")
        self.header, self.chunk_header = [], []
        self.expected_size = self.expected_crc = self.chunk_remaining = self.ordinal = 0
        self.image = bytearray()
        self.chunk_lengths, self.tail_words = [], []
        self.build = "00112233445566778899aabbccddeeff"

    def read32(self, offset):
        b, a = self.c.bridge, self.c.abi
        if offset == b("ID_OFFSET"):
            return b("IDENTITY")
        if offset == b("VERSION_OFFSET"):
            return b("VERSION")
        if offset == b("FAULT_OFFSET"):
            return self.fault
        if offset == b("STATUS_OFFSET"):
            return b("STATUS_FAULT") if self.fault else b("STATUS_READY") | (b("STATUS_BUSY") if self.pending else 0)
        if offset == b("GPO_OFFSET"):
            return self.gpo ^ int(self.bad_echo)
        if offset == b("GPI_OFFSET"):
            if self.pending:
                self.polls += 1
                if not self.never_ack and self.polls >= self.ack_after:
                    opcode, index, argument, toggle = self.pending
                    self.pending = None
                    self.error, self.response = self.apply(opcode, index, argument)
                    self.toggle = toggle
                    if self.fault_after_ack:
                        self.fault = b("FAULT_RESET")
            return ((0 if self.bad_signature else a("SIGNATURE")) |
                    (a("ACK_MASK") if self.toggle else 0) |
                    (a("ERROR_MASK") if self.error else 0) | self.response)
        raise AssertionError("client read an unsupported register")

    def write32(self, offset, value):
        if offset != self.c.bridge("GPO_OFFSET"):
            raise AssertionError("client wrote a register other than GPO")
        toggle = bool(value & self.c.abi("REQUEST_MASK"))
        if self.pending or toggle == bool(self.toggle):
            raise AssertionError("client reused a toggle or overwrote an in-flight request")
        self.writes.append(value)
        self.gpo = value
        self.polls = 0
        self.pending = ((value >> 24) & 127, (value >> 16) & 255, value & 65535, toggle)
        if self.fault_on_write:
            self.fault = self.c.bridge("FAULT_PARTIAL")

    def apply(self, opcode, index, argument):
        a = self.c.abi
        self.commands.append((opcode, index, argument))
        if opcode == a("OPCODE_IDENTITY"):
            prefix = [a(name) for name in ("IDENTITY_MAGIC0", "IDENTITY_MAGIC1", "TRANSPORT_MAJOR",
                "TRANSPORT_MINOR", "ABI_TAG", "ABI_MAJOR", "ABI_MINOR")]
            prefix[0] ^= int(self.bad_identity)
            capabilities = (a("INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK") |
                            a("INTERFACE_KEYBOARD_HID_CAPABILITY_MASK") |
                            a("INTERFACE_MEDIA_ZX81_TAPE_CAPABILITY_MASK"))
            if self.bad_capabilities:
                capabilities |= a("INTERFACE_AUDIO_PCM_S16_STEREO_48K_CAPABILITY_MASK")
            build = bytes.fromhex(self.build)
            words = prefix + [capabilities] + [int.from_bytes(build[n:n + 2], "little") for n in range(0, 16, 2)]
            return False, words[index]
        if opcode == a("OPCODE_MEDIA_INFO"):
            return False, [1, 0, 16384, 0, 512, self.state][index]
        self.mutations.append((opcode, index, argument))
        if opcode == a("OPCODE_EXECUTION"):
            if index or argument > 1:
                return True, a("ERROR_INVALID_ARGUMENT")
            self.running = bool(argument)
            if not self.running:
                self.rows = [0] * 9
            return False, int(self.bad_success)
        if opcode == a("OPCODE_KEYBOARD_HID"):
            if index > 8 or (index == 8 and argument > 255) or (index == 0 and argument & 15):
                return True, a("ERROR_INVALID_ARGUMENT")
            self.rows[index] = argument
            return False, 0
        if opcode == a("OPCODE_MEDIA_BEGIN"):
            if self.load_busy:
                return True, a("ERROR_INVALID_STATE")
            if index != len(self.header) or self.expected_size:
                return True, a("ERROR_INVALID_INDEX")
            self.header.append(argument)
            self.state = a("MEDIA_STATE_LOADING")
            if index == 3:
                self.expected_size = self.header[0] | self.header[1] << 16
                self.expected_crc = self.header[2] | self.header[3] << 16
                self.image.clear()
            return False, 0
        if opcode == a("OPCODE_MEDIA_CHUNK"):
            if index != len(self.chunk_header) or self.chunk_remaining:
                return True, a("ERROR_INVALID_INDEX")
            self.chunk_header.append(argument)
            if index == 2:
                offset = self.chunk_header[0] | self.chunk_header[1] << 16
                if offset != len(self.image) or not 1 <= argument <= 512 or len(self.image) + argument > self.expected_size:
                    return True, a("ERROR_INVALID_ARGUMENT")
                self.chunk_remaining = argument
                self.chunk_lengths.append(argument)
                self.ordinal = 0
                self.chunk_header.clear()
            return False, 0
        if opcode == a("OPCODE_MEDIA_DATA"):
            if not self.chunk_remaining or index != self.ordinal:
                return True, a("ERROR_INVALID_INDEX")
            if self.chunk_remaining == 1:
                self.tail_words.append(argument)
                if argument > 255:
                    return True, a("ERROR_INVALID_ARGUMENT")
            count = min(self.chunk_remaining, 2)
            self.image.extend(argument.to_bytes(2, "little")[:count])
            self.chunk_remaining -= count
            self.ordinal += 1
            return False, 0
        if opcode == a("OPCODE_MEDIA_COMMIT"):
            checksum = zlib.crc32(self.image) & 0xffffffff
            if self.chunk_remaining or len(self.image) != self.expected_size or checksum != self.expected_crc or self.bad_crc:
                return True, a("ERROR_INVALID_ARGUMENT")
            self.state = a("MEDIA_STATE_READY")
            self.expected_size = 0
            self.header.clear()
            return False, 0
        if opcode == a("OPCODE_MEDIA_EJECT"):
            if self.load_busy:
                return True, a("ERROR_INVALID_STATE")
            self.state = a("MEDIA_STATE_EMPTY")
            self.expected_size = 0
            self.header.clear()
            self.chunk_header.clear()
            self.chunk_remaining = 0
            return False, 0
        return True, a("ERROR_INVALID_OPCODE")


class ClientTests(unittest.TestCase):
    def setUp(self):
        self.c = C.Definitions()
        self.peer = Peer(self.c)
        self.clock = Clock()
        self.client = C.ComputerClient(self.peer, self.c, timeout=.005, poll_interval=.001,
                                      clock=self.clock.monotonic, sleep=self.clock.sleep)

    def connect(self):
        return self.client.probe(self.peer.build)

    def test_identity_build_and_capabilities_are_validated_without_machine_mutation(self):
        info = self.connect()
        self.assertEqual(info["build_id"], self.peer.build)
        self.assertEqual(info["media"], {"minimum": 1, "maximum": 16384, "chunk_maximum": 512, "state": 1})
        self.assertEqual(self.peer.mutations, [])
        self.assertEqual(len(self.peer.writes), 22)
        for before, after in zip(self.peer.writes, self.peer.writes[1:]):
            self.assertNotEqual(bool(before & self.c.abi("REQUEST_MASK")), bool(after & self.c.abi("REQUEST_MASK")))

    def test_wrong_identity_capabilities_and_build_block_mutations(self):
        for change in ("bad_identity", "bad_capabilities", "wrong_build"):
            with self.subTest(change=change):
                self.setUp()
                if change != "wrong_build":
                    setattr(self.peer, change, True)
                with self.assertRaises(C.TransportError):
                    self.client.probe("0" * 32 if change == "wrong_build" else None)
                with self.assertRaises(C.TransportError):
                    self.client.run()
                self.assertEqual(self.peer.mutations, [])

    def test_execution_is_explicit_and_complete_keyboard_state_is_not_cached_on_disk(self):
        self.connect()
        self.assertFalse(self.peer.running)
        self.client.run()
        self.client.set_keys([0xe1, 0x13])  # SHIFT+P, an existing ZX81 mapping.
        self.assertEqual(self.peer.rows[8], 2)
        self.assertEqual(self.peer.rows[1], 8)
        self.client.set_keys([0x28])
        self.assertEqual(self.peer.rows, [0, 0, 1 << 8, 0, 0, 0, 0, 0, 0])
        self.client.neutral()
        self.assertEqual(self.peer.rows, [0] * 9)
        self.client.hold()
        self.assertFalse(self.peer.running)

    def test_invalid_hid_or_field_arguments_do_not_send_requests(self):
        self.connect()
        writes = len(self.peer.writes)
        for usages in ([0], [3], [128], [223], [232], [-1], [True]):
            with self.subTest(usages=usages), self.assertRaises(ValueError):
                self.client.set_keys(usages)
        for fields in ((128, 0, 0), (1, 256, 0), (1, 0, 65536)):
            with self.subTest(fields=fields), self.assertRaises(ValueError):
                self.client.command(*fields)
        self.assertEqual(len(self.peer.writes), writes)

    def test_upload_crc_known_vector_odd_chunks_padding_and_no_automatic_run(self):
        self.connect()
        result = self.client.upload(b"123456789", chunk_bytes=5)
        self.assertEqual(result["crc32"], "cbf43926")
        self.assertEqual(bytes(self.peer.image), b"123456789")
        self.assertEqual(self.peer.chunk_lengths, [5, 4])
        self.assertEqual(self.peer.tail_words, [ord("5")])
        self.assertFalse(self.peer.running)
        self.assertEqual(self.peer.state, self.c.abi("MEDIA_STATE_READY"))
        self.assertNotIn(self.c.abi("OPCODE_EXECUTION"), [x[0] for x in self.peer.mutations])
        self.client.eject()
        self.assertEqual(self.peer.state, self.c.abi("MEDIA_STATE_EMPTY"))

    def test_tape_minimum_maximum_and_chunk_boundary_round_trip(self):
        for payload in (b"\xff", bytes(range(256)) * 64):
            with self.subTest(size=len(payload)):
                self.setUp()
                self.connect()
                self.client.upload(payload)
                self.assertEqual(bytes(self.peer.image), payload)
                self.assertTrue(all(1 <= n <= 512 for n in self.peer.chunk_lengths))

    def test_invalid_tape_sizes_and_chunk_sizes_are_rejected_before_io(self):
        for payload in (b"", b"x" * 16385):
            with self.assertRaises(ValueError):
                self.client.upload(payload)
        for count in (0, 513, -1, True):
            with self.assertRaises(ValueError):
                self.client.upload(b"x", chunk_bytes=count)
        self.assertEqual(self.peer.writes, [])

    def test_crc_or_busy_rejection_is_acknowledged_without_retry_eject_or_release(self):
        self.connect()
        self.peer.bad_crc = True
        with self.assertRaises(C.CommandRejected) as caught:
            self.client.upload(b"bad CRC is deliberately rejected")
        self.assertEqual(caught.exception.code, self.c.abi("ERROR_INVALID_ARGUMENT"))
        self.assertEqual(self.peer.state, self.c.abi("MEDIA_STATE_LOADING"))
        self.assertEqual(self.peer.mutations[-1][0], self.c.abi("OPCODE_MEDIA_COMMIT"))
        self.assertFalse(self.peer.running)
        self.client.eject()  # Definite GP errors do not poison an otherwise known transport.
        self.peer.load_busy = True
        with self.assertRaises(C.CommandRejected):
            self.client.upload(b"x")
        self.assertEqual(self.peer.state, self.c.abi("MEDIA_STATE_EMPTY"))

    def test_timeout_retains_one_request_and_blocks_every_subsequent_command(self):
        self.connect()
        self.peer.never_ack = True
        before = len(self.peer.writes)
        with self.assertRaisesRegex(C.TransportError, "uncertain"):
            self.client.run()
        self.assertEqual(len(self.peer.writes), before + 1)
        held = self.peer.gpo
        with self.assertRaises(C.TransportError):
            self.client.hold()
        with self.assertRaises(C.TransportError):
            self.client.probe()
        self.assertEqual((len(self.peer.writes), self.peer.gpo), (before + 1, held))

    def test_poll_sleep_never_exceeds_remaining_software_deadline(self):
        self.connect()
        self.client.poll_interval = 10
        self.peer.never_ack = True
        started = self.clock.now
        with self.assertRaises(C.TransportError):
            self.client.run()
        self.assertAlmostEqual(self.clock.now - started, self.client.timeout)

    def test_fault_before_and_after_ack_is_never_reported_as_success(self):
        for mode in ("fault_on_write", "fault_after_ack"):
            with self.subTest(mode=mode):
                self.setUp()
                self.connect()
                setattr(self.peer, mode, True)
                before = len(self.peer.writes)
                with self.assertRaisesRegex(C.TransportError, "fault"):
                    self.client.run()
                with self.assertRaises(C.TransportError):
                    self.client.run()
                self.assertEqual(len(self.peer.writes), before + 1)

    def test_changed_payload_signature_and_nonzero_mutation_response_fail_closed(self):
        for mode in ("bad_echo", "bad_signature", "bad_success"):
            with self.subTest(mode=mode):
                self.setUp()
                self.connect()
                setattr(self.peer, mode, True)
                with self.assertRaises(C.TransportError):
                    self.client.run()
                self.assertTrue(self.client.failed)

    def test_busy_discovery_does_not_overwrite_a_foreign_request(self):
        self.peer.pending = (2, 0, 1, True)
        with self.assertRaisesRegex(C.TransportError, "pending"):
            self.client.probe()
        self.assertEqual(self.peer.writes, [])

    def test_command_requires_probe_and_nonfinite_limits_are_rejected(self):
        with self.assertRaises(C.TransportError):
            self.client.run()
        for timeout in (0, -1, float("inf"), float("nan")):
            with self.assertRaises(ValueError):
                C.ComputerClient(self.peer, timeout=timeout)


class FileAndCliTests(unittest.TestCase):
    def test_offline_file_words_are_little_endian_bounded_and_offset(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "registers.bin"
            path.write_bytes(b"\xcc" * 64)
            with C.FileRegisters(path, 32, 12) as registers:
                registers.write32(4, 0x12345678)
                self.assertEqual(registers.read32(4), 0x12345678)
                for offset in (-4, 1, 32):
                    with self.assertRaises(ValueError):
                        registers.read32(offset)
                with self.assertRaises(ValueError):
                    registers.write32(0, 1 << 32)
            self.assertEqual(path.read_bytes()[16:20], b"\x78\x56\x34\x12")
            self.assertEqual(path.read_bytes()[:12], b"\xcc" * 12)

    def test_backend_refuses_symlinks_short_files_and_device_paths(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "short"
            path.write_bytes(b"\0" * 16)
            with self.assertRaises(ValueError):
                C.FileRegisters(path, 32)
            path.write_bytes(b"\0" * 32)
            link = path.with_name("link")
            link.symlink_to(path)
            with self.assertRaises(OSError):
                C.FileRegisters(link, 32)
        with self.assertRaises(ValueError):
            C.FileRegisters(Path("/dev/null"), 32)

    def test_cli_upload_uses_client_without_implicitly_running_or_typing(self):
        definitions = C.Definitions()
        peer = Peer(definitions)
        clock = Clock()
        real_client = C.ComputerClient
        factory = lambda registers, constants, **kwargs: real_client(registers, constants,
            clock=clock.monotonic, sleep=clock.sleep, **kwargs)
        with tempfile.TemporaryDirectory() as directory:
            payload = Path(directory) / "program.p"
            payload.write_bytes(b"123456789")
            context = mock.MagicMock()
            context.__enter__.return_value = peer
            output = io.StringIO()
            with mock.patch.object(C, "FileRegisters", return_value=context), mock.patch.object(C, "ComputerClient", side_effect=factory), redirect_stdout(output):
                C.main(["--register-file", str(Path(directory) / "registers.bin"), "upload", str(payload)])
            self.assertEqual(json.loads(output.getvalue())["crc32"], "cbf43926")
            self.assertFalse(peer.running)
            self.assertTrue(all(op != definitions.abi("OPCODE_KEYBOARD_HID") for op, _, _ in peer.mutations))

    def test_cli_invalid_payload_precedes_register_file_open(self):
        with tempfile.TemporaryDirectory() as directory:
            payload = Path(directory) / "empty.p"
            payload.write_bytes(b"")
            with mock.patch.object(C, "FileRegisters") as registers, redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit):
                    C.main(["--register-file", str(payload.with_name("registers.bin")), "upload", str(payload)])
                registers.assert_not_called()


if __name__ == "__main__":
    unittest.main()
