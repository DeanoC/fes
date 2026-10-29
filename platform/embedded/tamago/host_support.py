"""Host-only QEMU transport and screenshot helpers for the native experiment."""
import contextlib
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import socket
import struct
import subprocess
import sys
import tempfile
import time
import zlib


FRAME = re.compile(r"FES_FRAME seq=(\d+) titles=(\d+) focus=(\d+) layout=(\w+)")


def frames(text):
    return [dict(zip(("seq", "titles", "focus", "layout"),
                     (int(m[0]), int(m[1]), int(m[2]), m[3])))
            for m in FRAME.findall(text)]


def png_from_ppm(source, target):
    """Convert QMP's binary PPM to PNG without an optional imaging dependency."""
    data = source.read_bytes()
    position = 0
    tokens = []
    while len(tokens) < 4:
        while position < len(data) and data[position] in b" \t\r\n":
            position += 1
        if position < len(data) and data[position] == ord("#"):
            position = data.index(b"\n", position) + 1
            continue
        end = position
        while end < len(data) and data[end] not in b" \t\r\n":
            end += 1
        tokens.append(data[position:end])
        position = end
    if tokens[0] != b"P6" or tokens[3] != b"255":
        raise ValueError("QMP screenshot is not an 8-bit RGB PPM")
    width, height = int(tokens[1]), int(tokens[2])
    # The raster follows exactly one whitespace delimiter. Do not skip raster
    # bytes whose value happens to be whitespace.
    if data[position:position + 2] == b"\r\n":
        position += 2
    else:
        position += 1
    pixels = data[position:]
    if width <= 0 or height <= 0 or len(pixels) != width * height * 3:
        raise ValueError("invalid QMP screenshot dimensions/raster")
    def chunk(kind, payload):
        return (struct.pack(">I", len(payload)) + kind + payload +
                struct.pack(">I", zlib.crc32(kind + payload)))
    rows = b"".join(b"\0" + pixels[y * width * 3:(y + 1) * width * 3]
                    for y in range(height))
    target.write_bytes(b"\x89PNG\r\n\x1a\n" +
                       chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) +
                       chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b""))
    return {"width": width, "height": height,
            "sha256": hashlib.sha256(target.read_bytes()).hexdigest(),
            "path": target.name, "ppm_path": source.name}


class Guest:
    """Own one QEMU process, its Unix serial socket and its QMP connection."""
    def __init__(self, qemu, elf, directory, record_command, *, disk=None, display="none", usb=False, ui=True):
        self.directory = Path(directory)
        self.directory.mkdir()
        self.temporary = tempfile.TemporaryDirectory(prefix="fes-native-")
        base = Path(self.temporary.name)
        self.serial_path, self.qmp_path = base / "serial", base / "qmp"
        self.log = bytearray()
        self.console = (self.directory / "console.log").open("wb")
        self.process = self.serial = self.qmp = self.qmp_stream = None
        self.stderr = (self.directory / "qemu.log").open("wb")
        self.started = time.monotonic()
        self.usb = usb
        self.command = [str(qemu), "-machine", "virt,gic-version=3,highmem=off" if usb else "virt,gic-version=3", "-cpu", "cortex-a53",
                        "-m", "512M", "-smp", "1", "-display", display,
                        "-monitor", "none", "-chardev",
                        f"socket,id=console,path={self.serial_path},server=on,wait=on",
                        "-serial", "chardev:console", "-qmp",
                        f"unix:{self.qmp_path},server=on,wait=off", "-kernel", str(elf)]
        if disk:
            self.command += ["-global", "virtio-mmio.force-legacy=false", "-drive",
                             f"file={disk},if=none,id=bootdisk,format=raw,cache=writeback",
                             "-device", "virtio-blk-device,drive=bootdisk"]
        if disk and ui:
            self.command += ["-device", "ramfb", "-netdev", "user,id=hostnet", "-device",
                             "virtio-net-device,netdev=hostnet,mac=52:54:00:12:34:56",
                             "-object", f"filter-dump,id=packets,netdev=hostnet,file={self.directory / 'network.pcap'}"]
        else:
            self.command += ["-net", "none"]
        if usb:
            self.command += ["-device", "qemu-xhci,id=xhci,addr=1", "-device",
                             f"usb-kbd,bus=xhci.0,pcap={self.directory / 'usb.pcap'}"]
        record_command(self.command)
        try:
            self.process = subprocess.Popen(self.command, stdout=self.stderr, stderr=self.stderr)
            # wait=on holds CPU startup until this socket is attached, retaining
            # the first guest log byte without a competing serial reader.
            self.serial = self.connect(self.serial_path)
            self.qmp = self.connect(self.qmp_path)
            self.qmp.settimeout(5)
            self.qmp_stream = self.qmp.makefile("rwb", buffering=0)
            greeting = json.loads(self.qmp_stream.readline())
            if "QMP" not in greeting:
                raise RuntimeError("missing QMP greeting")
            self.execute("qmp_capabilities")
            self.serial.setblocking(False)
        except BaseException:
            self.close()
            raise

    def connect(self, path):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            connection = socket.socket(socket.AF_UNIX)
            try:
                connection.connect(str(path))
                return connection
            except (FileNotFoundError, ConnectionRefusedError):
                connection.close()
                if self.process.poll() is not None:
                    raise RuntimeError(f"QEMU exited during startup; inspect {self.directory / 'qemu.log'}")
                time.sleep(0.02)
        raise RuntimeError(f"QEMU socket startup timed out: {path}")

    @property
    def text(self):
        return self.log.decode(errors="replace")

    def execute(self, command, arguments=None):
        request = {"execute": command}
        if arguments is not None:
            request["arguments"] = arguments
        self.qmp_stream.write(json.dumps(request).encode() + b"\n")
        while True:
            line = self.qmp_stream.readline()
            if not line:
                raise RuntimeError("QMP disconnected")
            response = json.loads(line)
            if "error" in response:
                raise RuntimeError(f"QMP {command}: {response['error']}")
            if "return" in response:
                return response["return"]

    def pump(self, timeout=0.1, echo=False):
        with selectors.DefaultSelector() as selector:
            selector.register(self.serial, selectors.EVENT_READ)
            if not selector.select(timeout):
                return
        try:
            data = self.serial.recv(65536)
        except BlockingIOError:
            return
        if data:
            self.log.extend(data)
            self.console.write(data)
            self.console.flush()
            if echo:
                sys.stdout.buffer.write(data)
                sys.stdout.buffer.flush()
        if re.search(r"panic:|\bFAIL\b|fatal error:", self.text):
            raise RuntimeError(f"guest failure; inspect {self.directory / 'console.log'}")

    def wait(self, predicate, timeout, description):
        deadline = time.monotonic() + timeout
        while True:
            if predicate(self.text):
                return self.text
            if self.process.poll() is not None:
                raise RuntimeError(f"QEMU exited before {description}")
            if time.monotonic() >= deadline:
                raise RuntimeError(f"timed out waiting for {description}")
            self.pump(min(0.1, deadline - time.monotonic()))

    def send(self, data):
        # These short test/terminal writes fit the Unix socket send buffer.
        self.serial.sendall(data)

    def press(self, key):
        if self.usb:
            self.execute("send-key", {"keys": [{"type": "qcode", "data": key}], "hold-time": 50})
        else:
            self.send({"right": b"\x1b[C", "l": b"l"}[key])

    def screenshot(self, name):
        ppm = self.directory / (name + ".ppm")
        self.execute("screendump", {"filename": str(ppm)})
        result = png_from_ppm(ppm, ppm.with_suffix(".png"))
        result["path"] = str(Path(self.directory.name) / result["path"])
        result["ppm_path"] = str(Path(self.directory.name) / result["ppm_path"])
        return result

    def interactive(self):
        # QEMU's SDL window shows ramfb; key bytes are deliberately collected
        # from this terminal, not claimed as a USB keyboard controller.
        import termios
        import tty
        print("Interactive: USB keys use the QEMU window; UART keys use this terminal. "
              "Ctrl-] in this terminal exits.", flush=True)
        descriptor = sys.stdin.fileno()
        saved = termios.tcgetattr(descriptor) if os.isatty(descriptor) else None
        try:
            if saved is not None:
                tty.setraw(descriptor)
            with selectors.DefaultSelector() as selector:
                selector.register(descriptor, selectors.EVENT_READ, "stdin")
                selector.register(self.serial, selectors.EVENT_READ, "serial")
                while self.process.poll() is None:
                    for key, _ in selector.select(0.1):
                        if key.data == "serial":
                            self.pump(0, echo=True)
                        else:
                            data = os.read(descriptor, 64)
                            if not data or b"\x1d" in data:
                                return
                            if not self.usb:
                                self.send(data)
        finally:
            if saved is not None:
                termios.tcsetattr(descriptor, termios.TCSADRAIN, saved)

    def close(self):
        if self.process is not None and self.process.poll() is None:
            try:
                if self.qmp_stream is not None:
                    self.execute("quit")
            except (OSError, RuntimeError):
                pass
            try:
                self.process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                try:
                    self.process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait()
        if self.serial is not None:
            self.serial.setblocking(False)
            with contextlib.suppress(OSError):
                while True:
                    data = self.serial.recv(65536)
                    if not data:
                        break
                    self.log.extend(data)
            self.serial.close()
        for stream in (self.qmp_stream, self.qmp):
            if stream is not None:
                stream.close()
        self.stderr.close()
        self.console.close()
        (self.directory / "console.log").write_bytes(self.log)
        self.temporary.cleanup()

    def __enter__(self):
        return self

    def __exit__(self, exception_type, *_):
        if exception_type is not None and self.qmp_stream is not None:
            with contextlib.suppress(OSError, RuntimeError, ValueError):
                self.screenshot("failure")
        self.close()
