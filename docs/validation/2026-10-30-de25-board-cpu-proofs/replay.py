#!/usr/bin/env python3
"""Replay retained CPU-only proofs on claimed boards; never program an FPGA.

The private config supplies UART-verified SSH endpoints, MACs, boot IDs,
credentials and a pinned known_hosts file. Keep that config out of Git.
"""
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import shlex
import signal
import subprocess
import tempfile
import time


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def quote(value):
    return shlex.quote(str(value))


class Board:
    def __init__(self, row, known_hosts):
        self.row, self.known_hosts = row, known_hosts
        self.serial = row['serial']

    def redact(self, data):
        return data.replace(self.row['ip'].encode(), self.serial.encode())

    def run(self, command, *, data=None, timeout=60, forward=None, log=None,
            allow_failure=False):
        r, w = os.pipe()
        os.write(w, (self.row['password'] + '\n').encode())
        os.close(w)
        guard = ('test "$(cat /sys/class/net/eth0/address)" = ' + quote(self.row['mac'])
                 + ' && test "$(cat /proc/sys/kernel/random/boot_id)" = '
                 + quote(self.row['boot_id']) + ' || exit 93\n')
        argv = ['sshpass', '-d', str(r), 'ssh', '-o', 'StrictHostKeyChecking=yes',
                '-o', 'UserKnownHostsFile=' + self.known_hosts,
                '-o', 'HostKeyAlgorithms=ssh-ed25519', '-o', 'PreferredAuthentications=password',
                '-o', 'PubkeyAuthentication=no', '-o', 'NumberOfPasswordPrompts=1',
                '-o', 'ConnectTimeout=6', '-o', 'ExitOnForwardFailure=yes']
        if forward:
            argv += ['-R', forward]
        argv += [self.row['user'] + '@' + self.row['ip'], guard + command]
        process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, pass_fds=(r,), start_new_session=True)
        os.close(r)
        try:
            stdout, stderr = process.communicate(data, timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                stdout, stderr = process.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                stdout, stderr = process.communicate()
            if log:
                log.write_bytes(self.redact(stdout + stderr))
            raise RuntimeError(self.serial + ': owned SSH proof timed out')
        if log:
            log.write_bytes(self.redact(stdout + stderr))
        if process.returncode and not allow_failure:
            raise RuntimeError(self.serial + ': SSH proof exited ' + str(process.returncode)
                               + ': ' + self.redact(stderr).decode(errors='replace'))
        return subprocess.CompletedProcess([], process.returncode, stdout, stderr)

    def upload(self, source, target):
        self.run('umask 077\ncat > ' + quote(target) + '\nchmod 700 ' + quote(target),
                 data=Path(source).read_bytes())
        if self.run('sha256sum -- ' + quote(target)).stdout.decode().split()[0] != sha(source):
            raise RuntimeError('Uploaded executable/input hash mismatch')


def settings(args):
    cfg = json.loads(args.config.read_text())
    return [Board(r, cfg['known_hosts']) for r in cfg['boards']]


MODELS = {
    'internal': ('de25-zx81-load-roundtrip/out/internal-final4', 'Vfes_de25_computer', 'rtl'),
    'combined': ('de25-zx81-load-roundtrip/out/combined-final4', 'Vfes_de25_combined_sim', 'hps/project'),
    'control': ('de25-pr-control/out/pr-control-cosim-9663da282a', 'Vfes_link_sim', 'source/sources/misteross/boards/de25nano/link-proof/fpga'),
}


def verified_model(args, name):
    relative, server_name, cwd = MODELS[name]
    root = args.retained / relative
    receipt = json.loads((root / 'proof.json').read_text())
    if receipt['status'] != 'passed':
        raise ValueError('Retained model proof did not pass')
    hashes = receipt.get('binaries', receipt.get('binaries_sha256'))
    server, client = root / 'obj' / server_name, root / 'client-aarch64'
    server_hash = hashes.get(server_name, hashes.get('rtl-server'))
    if sha(server) != server_hash or sha(client) != hashes['client-aarch64']:
        raise ValueError('Retained executable differs from its frozen receipt')
    native = [r for r in receipt['results'] if r['architecture'] == 'native']
    if not native or len({r['scenario'] for r in native}) != len(native):
        raise ValueError('Duplicate retained native reference')
    return root, receipt, server, client, root / cwd, native


def prepare(args):
    args.output.mkdir(parents=True, exist_ok=False)
    metadata = []
    for board in settings(args):
        directory = board.run('mktemp -d /tmp/fes-de25-cpu-XXXXXXXX').stdout.decode().strip()
        if not directory.startswith('/tmp/fes-de25-cpu-') or not directory.replace('-', '').replace('/', '').isalnum():
            raise ValueError('Unexpected remote temporary directory')
        info = board.run('uname -m\nuname -r\ngetconf _NPROCESSORS_ONLN\nhead -n 5 /proc/meminfo\nlscpu -J').stdout.decode()
        if info.splitlines()[0] != 'aarch64':
            raise ValueError('Target is not AArch64')
        metadata.append({'serial': board.serial, 'boot_id': board.row['boot_id'],
                         'temporary_directory': directory, 'machine': info})
        for name in MODELS:
            _, _, _, client, _, _ = verified_model(args, name)
            board.upload(client, directory + '/' + name + '-client')
        print(board.serial + ': verified and uploaded three frozen AArch64 clients', flush=True)
    (args.output / 'prepared.json').write_text(json.dumps(metadata, indent=2) + '\n')


def clients(args):
    prepared = {r['serial']: r for r in json.loads((args.output / 'prepared.json').read_text())}
    driver = sha(__file__)

    def suite(board, name):
        root, receipt, server_binary, client, cwd, native = verified_model(args, name)
        out = args.output / board.serial / name
        out.mkdir(parents=True, exist_ok=False)
        remote = prepared[board.serial]['temporary_directory']
        results = []
        # Validate forwarding and build rejection before the expensive BASIC boot.
        native.sort(key=lambda r: (r['scenario'] == 'session', r['scenario'] != 'wrong-build'))
        for expected in native:
            scenario = expected['scenario']
            with tempfile.TemporaryDirectory(prefix='fes-de25-board-' + name + '-') as tmp:
                endpoint = Path(tmp) / 'endpoint'
                remote_socket = remote + '/' + name + '-' + scenario + '.sock'
                with (out / (scenario + '-server.log')).open('wb') as stream:
                    server = subprocess.Popen([str(server_binary), str(endpoint),
                                               '+verilator+rand+reset+2', '+verilator+seed+41'],
                                              cwd=cwd, stdout=stream, stderr=subprocess.STDOUT)
                    try:
                        until = time.monotonic() + 15
                        while not endpoint.exists():
                            if server.poll() is not None or time.monotonic() >= until:
                                raise RuntimeError('Owned RTL model did not start')
                            time.sleep(.01)
                        argv = [remote + '/' + name + '-client', remote_socket, scenario,
                                receipt['selected_build_id']]
                        saved = remote + '/' + name + '-saved.p'
                        if name != 'control':
                            argv.append(saved)
                        started = time.monotonic()
                        reply = board.run('exec ' + ' '.join(map(quote, argv)), timeout=1800 if scenario == 'session' else 900,
                                          forward=remote_socket + ':' + str(endpoint),
                                          log=out / (scenario + '-client.log'))
                        result = json.loads(reply.stdout)
                        if scenario == 'session' and name != 'control':
                            image = board.run('cat -- ' + quote(saved)).stdout
                            (out / 'saved.p').write_bytes(image)
                            result['saved_image_sha256'] = hashlib.sha256(image).hexdigest()
                        wanted = {k: v for k, v in expected.items() if k != 'architecture'}
                        if result != wanted:
                            raise ValueError(board.serial + '/' + name + '/' + scenario + ': native reference mismatch')
                        if server.wait(timeout=10) != 0:
                            raise ValueError('RTL model exited unsuccessfully')
                        results.append({'board': board.serial, **result,
                                        'host_elapsed_seconds': round(time.monotonic() - started, 3)})
                        print(board.serial + '/' + name + '/' + scenario + ': PASS', flush=True)
                    finally:
                        if server.poll() is None:
                            server.terminate()
                            try:
                                server.wait(timeout=5)
                            except subprocess.TimeoutExpired:
                                server.kill()
                                server.wait()
        if sha(client) != receipt.get('binaries', receipt.get('binaries_sha256'))['client-aarch64']:
            raise ValueError('Frozen client changed')
        if board.run('sha256sum -- ' + quote(remote + '/' + name + '-client')).stdout.decode().split()[0] != sha(client):
            raise ValueError('Target executable changed')
        report = {'status': 'passed', 'board': board.serial, 'kind': name,
                  'execution': 'physical-DE25-AArch64-client-to-host-RTL-over-SSH-Unix-forward',
                  'physical_FPGA': 'not-run', 'memory': 'host-behavioral-RTL',
                  'reference_receipt_sha256': sha(root / 'proof.json'),
                  'client_sha256': sha(client), 'server_sha256': sha(server_binary),
                  'driver_sha256': driver, 'results': results}
        (out / 'proof.json').write_text(json.dumps(report, indent=2) + '\n')
        return report

    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:
        futures = [pool.submit(suite, b, n) for b in settings(args) for n in MODELS]
        reports = [f.result() for f in futures]
    if sha(__file__) != driver:
        raise ValueError('Replay driver changed during proof')
    (args.output / 'clients.json').write_text(json.dumps(reports, indent=2) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['prepare', 'clients'])
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--retained', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.retained = args.retained.resolve()
    args.output = args.output.resolve()
    (prepare if args.command == 'prepare' else clients)(args)


if __name__ == '__main__':
    main()
