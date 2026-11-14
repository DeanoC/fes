"""QMP replies must progress while a native guest writes its UART."""
import importlib.util
import io
from pathlib import Path
import socket
import threading
import time
import unittest

SPEC = importlib.util.spec_from_file_location(
    "embedded_host_support", Path(__file__).resolve().parents[1] /
    "platform/embedded/tamago/host_support.py")
HOST = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HOST)


class QMPTests(unittest.TestCase):
    def test_partial_reply_and_serial_backpressure_make_progress(self):
        qmp, qmp_peer = socket.socketpair()
        serial, serial_peer = socket.socketpair()
        self.addCleanup(qmp.close)
        self.addCleanup(qmp_peer.close)
        self.addCleanup(serial.close)
        self.addCleanup(serial_peer.close)
        serial_peer.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 4096)
        serial_peer.settimeout(4)
        serial.setblocking(False)
        guest = HOST.Guest.__new__(HOST.Guest)
        guest.qmp, guest.serial = qmp, serial
        qmp.settimeout(5)
        guest.qmp_stream = qmp.makefile("rwb", buffering=0)
        self.addCleanup(guest.qmp_stream.close)
        guest.qmp_buffer, guest.log = bytearray(), bytearray()
        guest.console = io.BytesIO()
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
