"""QMP replies must progress while a native guest writes its UART."""
import importlib.util
import io
import json
from pathlib import Path
import socket
import threading
import time
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    "embedded_host_support", Path(__file__).resolve().parents[1] /
    "platform/embedded/tamago/host_support.py")
HOST = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HOST)


class QMPTests(unittest.TestCase):
    def guest_peer(self):
        qmp, qmp_peer = socket.socketpair()
        serial, serial_peer = socket.socketpair()
        for channel in (qmp, qmp_peer, serial, serial_peer):
            self.addCleanup(channel.close)
        serial.setblocking(False)
        qmp_peer.settimeout(2)
        guest = HOST.Guest.__new__(HOST.Guest)
        guest.qmp, guest.serial = qmp, serial
        guest.qmp_buffer, guest.log = bytearray(), bytearray()
        guest.console = io.BytesIO()
        return guest, qmp_peer, serial_peer

    def test_async_events_before_greeting_and_capability_reply(self):
        guest, peer, _ = self.guest_peer()
        greeting = {
            "QMP": {"version": {"qemu": {"major": 8, "minor": 2, "micro": 2},
                                "package": "Ubuntu"}, "capabilities": ["oob"]}}
        event = b'{"event":"RESET","data":{"guest":false},"timestamp":{"seconds":1,"microseconds":2}}\r\n'
        wire = event + json.dumps(greeting).encode() + b"\r\n"
        errors = []

        def send():
            try:
                peer.sendall(wire[:9])
                peer.sendall(wire[9:])
                with peer.makefile("rb") as stream:
                    message = json.loads(stream.readline())
                self.assertEqual(message, {"execute": "qmp_capabilities"})
                peer.sendall(event + b'{"return":{}}\r\n')
            except Exception as exc:
                errors.append(exc)

        worker = threading.Thread(target=send, daemon=True)
        worker.start()
        guest.negotiate_qmp()
        worker.join(2)
        self.assertFalse(worker.is_alive())
        self.assertFalse(errors)

    def test_rejects_unexpected_greeting(self):
        for raw in [b'{}', b'{"event":""}', b'{"return":{}}', b'{"QMP":null}', b'{"event":"RESET","error":{}}', b'[]']:
            with self.subTest(raw=raw):
                guest, peer, _ = self.guest_peer()
                peer.sendall(raw + b"\n")
                with self.assertRaisesRegex(RuntimeError, "unexpected QMP greeting"):
                    guest.negotiate_qmp()

    def test_partial_or_excessive_greeting(self):
        for suffix, message in ((b'{', 'disconnected during greeting'), (b'x' * 65537, 'record exceeds')):
            with self.subTest(message=message):
                guest, peer, _ = self.guest_peer()
                peer.sendall(suffix)
                peer.shutdown(socket.SHUT_WR)
                with self.assertRaisesRegex(RuntimeError, message):
                    guest.negotiate_qmp()

    def test_greeting_and_capabilities_share_deadline(self):
        guest, peer, _ = self.guest_peer()
        peer.sendall(b'{"event":"RESET"}\n{"QMP":{"version":{"qemu":{}},"capabilities":[]}}\n{"return":{}}\n')
        # The greeting and pre-queued event use most of the five-second
        # timeout. Already buffered data cannot waive the original deadline.
        with patch.object(HOST.time, "monotonic", side_effect=[0,1,2,3,4,5]):
            with self.assertRaisesRegex(RuntimeError, "qmp_capabilities timed out"):
                guest.negotiate_qmp()

    def test_busy_event_stream_cannot_extend_greeting_deadline(self):
        guest, peer, _ = self.guest_peer()
        peer.sendall(b'{"event":"RESET"}\n' * 50)
        with patch.object(HOST.time, "monotonic", side_effect=range(20)):
            with self.assertRaisesRegex(RuntimeError, "greeting timed out"):
                guest.negotiate_qmp()

    def test_partial_reply_and_serial_backpressure_make_progress(self):
        guest, qmp_peer, serial_peer = self.guest_peer()
        serial_peer.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
        serial_peer.settimeout(4)
        payload = b"guest UART output\n" * 32768
        failures = []

        def peer():
            try:
                self.assertIn(b'"execute": "send-key"', qmp_peer.recv(4096))
                qmp_peer.sendall(b'{"ret')
                serial_peer.sendall(payload)
                qmp_peer.sendall(b'urn":{"accepted":true}}\n')
            except Exception as exc:
                failures.append(exc)

        worker = threading.Thread(target=peer, daemon=True)
        worker.start()
        self.assertEqual(guest.execute("send-key"), {"accepted": True})
        worker.join(1)
        self.assertFalse(worker.is_alive())
        self.assertFalse(failures)
        deadline = time.monotonic() + 1
        while len(guest.log) < len(payload) and time.monotonic() < deadline:
            guest.pump(0.01)
        self.assertEqual(bytes(guest.log), payload)


if __name__ == "__main__":
    unittest.main()


class NetworkProbeTests(unittest.TestCase):
    def test_serial_prefixes_do_not_publish_a_partial_catalog(self):
        row = 'FES_NETWORK_PROBE PASS ids=["a","b"] stats={Received:5 Transmitted:5 Dropped:0}\n'
        for n in range(len(row)):
            self.assertIsNone(HOST.network_catalog("booted\n" + row[:n]))
        self.assertEqual(HOST.network_catalog("booted\n" + row), ["a", "b"])
        with self.assertRaises(ValueError):
            HOST.network_catalog(row + row)
        with self.assertRaises(ValueError):
            HOST.network_catalog('FES_NETWORK_PROBE PASS ids=[5] stats={}\n')
