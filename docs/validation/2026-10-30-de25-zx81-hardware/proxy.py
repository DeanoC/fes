#!/usr/bin/env python3
"""Expose one selected JTAG computer endpoint to one diagnostic Unix client.

Only six 32-bit CSR reads and the GPO write are admitted. No FPGA programming,
CPU physical address or production/runtime constructor is provided.
"""
import argparse
import json
import os
from pathlib import Path
import selectors
import signal
import socket
import struct
import subprocess
import time


def exact(connection, size):
    result = b''
    while len(result) != size:
        block = connection.recv(size - len(result))
        if not block:
            if not result:
                return None
            raise RuntimeError('partial client command; no replay allowed')
        result += block
    return result


def run(args):
    if args.socket.exists() or args.output.exists() or args.log.exists():
        raise ValueError('socket and receipt/log paths must be new')
    environment = dict(os.environ)
    for name, value in [('SOF', str(args.sof.resolve(strict=True))), ('DEVICE', args.device),
                        ('MASTER', args.master), ('BASE', str(args.base)),
                        ('ID', str(args.identity)), ('VERSION', str(args.version))]:
        environment['FES_DE25_' + name] = value
    counters = {'reads': 0, 'writes': 0}
    with args.log.open('wb') as log:
        process = subprocess.Popen([args.console, '--script=' + str(Path(__file__).with_name('registers.tcl'))],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                   env=environment, start_new_session=True)
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ)
        pending = b''
        def response(marker, seconds):
            nonlocal pending
            deadline = time.monotonic() + seconds
            while True:
                while b'\n' in pending:
                    line, pending = pending.split(b'\n', 1)
                    if line.startswith(marker):
                        return line[len(marker):].strip()
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not selector.select(remaining):
                    raise RuntimeError('bounded JTAG IPC wait expired; epoch uncertain')
                block = os.read(process.stdout.fileno(), 65536)
                if not block:
                    raise RuntimeError('JTAG console exited; no replay allowed')
                log.write(block)
                log.flush()
                pending += block
        try:
            response(b'FES_JTAG_READY', 40)
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as server:
                server.bind(str(args.socket));os.chmod(args.socket, 0o600)
                server.listen(1);server.settimeout(90)
                print('Physical JTAG proxy ready', flush=True)
                with server.accept()[0] as connection:
                    connection.settimeout(90)
                    while True:
                        request = exact(connection, 12)
                        if request is None:
                            break
                        op, offset, value = struct.unpack('<III', request)
                        if op not in (1, 2) or offset not in (0, 4, 8, 12, 16, 20) or (op == 2 and offset != 12):
                            raise ValueError('client attempted access outside the computer register window')
                        process.stdin.write(f'{"R" if op == 1 else "W"} {offset} {value}\n'.encode())
                        process.stdin.flush()
                        result = int(response(b'FES_JTAG_REPLY ', 2))
                        counters['reads' if op == 1 else 'writes'] += 1
                        connection.sendall(struct.pack('<II', 0, result))
            process.stdin.write(b'Q\n');process.stdin.flush();process.stdin.close()
            response(b'FES_JTAG_CLOSED', 10)
            if process.wait(timeout=10) != 0:
                raise RuntimeError('JTAG console failed to close cleanly')
            counters['status'] = 'completed'
            args.output.write_text(json.dumps(counters, indent=2) + '\n')
        finally:
            selector.close()
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL);process.wait()
            args.socket.unlink(missing_ok=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('socket', 'output', 'log', 'sof'):
        parser.add_argument('--' + name, type=Path, required=True)
    for name in ('device', 'master', 'console'):
        parser.add_argument('--' + name, required=True)
    for name in ('base', 'identity', 'version'):
        parser.add_argument('--' + name, type=lambda value: int(value, 0), required=True)
    run(parser.parse_args())
