"""Bounded serial commands and one read-only TFTP transfer for the Nano loader."""
import fcntl
import re
import select
import socket
import struct
import termios
import threading
import time

PROMPT = rb'(?m)^SOCFPGA_AGILEX5 # $'


class Console:
    def __init__(self, device, log):
        import os
        self.os, self.log = os, log
        self.fd = os.open(device, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
        self.original = None
        self.parked = False
        try:
            fcntl.ioctl(self.fd, termios.TIOCEXCL)
            self.original = termios.tcgetattr(self.fd)
            attrs = self.original.copy(); attrs[6] = self.original[6].copy()
            attrs[0] = attrs[1] = attrs[3] = 0
            attrs[2] = termios.CS8 | termios.CLOCAL | termios.CREAD
            attrs[4] = attrs[5] = termios.B115200
            attrs[6][termios.VMIN] = attrs[6][termios.VTIME] = 0
            termios.tcsetattr(self.fd, termios.TCSANOW, attrs)
            self.os.write(self.fd, b'\r')
            self._receive(8)
        except BaseException:
            self.close()
            raise

    def _receive(self, timeout):
        raw = bytearray(); deadline = time.monotonic() + timeout
        self.parked = False
        while time.monotonic() < deadline:
            if re.search(PROMPT, raw):
                self.parked = True
                return raw.decode(errors='replace')
            if select.select([self.fd], [], [], .1)[0]:
                data = self.os.read(self.fd, 65536)
                if not data: raise OSError('serial disconnected')
                raw.extend(data); self.log.write(data); self.log.flush()
        raise TimeoutError('loader prompt deadline expired')

    def command(self, command, timeout=15):
        import secrets
        # Only the uploader constructs commands; filenames and sizes are fixed
        # or numeric. A nonce prevents an old result from accepting a new call.
        token = 'FES_UPLOAD_' + secrets.token_hex(6)
        self.os.write(self.fd, (command + '; echo ' + token + '=$?\r').encode())
        output = self._receive(timeout)
        rc = re.search(r'(?m)^' + token + r'=(\d+)\r?$', output)
        if not rc or rc[1] != '0': raise ValueError('loader command failed: ' + command)
        return output

    def park(self):
        self.os.write(self.fd, b'\x03\r')
        self._receive(4)

    def close(self):
        try:
            if self.original is not None:
                termios.tcsetattr(self.fd, termios.TCSANOW, self.original)
                fcntl.ioctl(self.fd, termios.TIOCNXCL)
        finally:
            self.os.close(self.fd)


class Transfer:
    """Serve exactly one filename to one observed IPv4 client; never accept writes."""
    def __init__(self, host, client, filename, payload, port=69, timeout=90):
        self.host, self.client, self.filename, self.payload = host, client, filename, payload
        self.timeout = timeout
        self.complete = False
        self.error = None
        self.stopped = threading.Event()
        self.listener = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        try:
            self.listener.bind((host, port)); self.listener.settimeout(.2)
        except BaseException:
            self.listener.close()
            raise
        self.port = self.listener.getsockname()[1]
        self.thread = threading.Thread(target=self._run, daemon=True)

    def start(self): self.thread.start()

    def _run(self):
        try:
            deadline = time.monotonic() + self.timeout
            peer = None
            while not self.stopped.is_set() and time.monotonic() < deadline:
                try: packet, source = self.listener.recvfrom(4096)
                except socket.timeout: continue
                if source[0] != self.client or packet[:2] != b'\0\1': continue
                fields = packet[2:].rstrip(b'\0').split(b'\0')
                if len(fields) < 2 or fields[:2] != [self.filename.encode(), b'octet']: continue
                if len(fields) % 2: continue
                options = dict(zip(fields[2::2], fields[3::2])); peer = source
                break
            if peer is None: raise TimeoutError('no matching TFTP read request')
            blocksize = 512; response = []
            if b'blksize' in options:
                requested = int(options[b'blksize'])
                if not 8 <= requested <= 65464: raise ValueError('invalid TFTP block size')
                blocksize = min(1468, requested)
                response.extend([b'blksize', str(blocksize).encode()])
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as stream:
                stream.bind((self.host, 0)); stream.settimeout(.2)
                def exchange(packet, block):
                    for _ in range(4):
                        if self.stopped.is_set() or time.monotonic() >= deadline:
                            raise TimeoutError('TFTP stopped or expired')
                        stream.sendto(packet, peer)
                        until = min(deadline, time.monotonic() + 1.5)
                        while not self.stopped.is_set() and time.monotonic() < until:
                            try: ack, sender = stream.recvfrom(4096)
                            except socket.timeout: continue
                            if sender != peer: continue
                            if ack == struct.pack('!HH', 4, block): return
                            if ack[:2] == b'\0\5': raise ValueError('TFTP client error')
                    raise TimeoutError('TFTP acknowledgement absent')
                if response: exchange(b'\0\6' + b'\0'.join(response) + b'\0', 0)
                for offset in range(0, len(self.payload) + 1, blocksize):
                    data = self.payload[offset:offset + blocksize]
                    exchange(struct.pack('!HH', 3, offset // blocksize + 1) + data, offset // blocksize + 1)
                self.complete = True
        except Exception as error: self.error = str(error)
        finally: self.listener.close()

    def close(self):
        self.stopped.set(); self.thread.join(timeout=3)
        if self.thread.is_alive(): raise RuntimeError('TFTP worker did not stop')
