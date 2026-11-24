#!/usr/bin/env python3
"""hil_f3_floppy.py: F3 (#2101) floppy smoke on a sealed fes.pc386 package.

  hil_f3_floppy.py REPO --kit B --owner Claude --package P.fcore --out DIR \\
      --kit-ssh KB --drive real --floppy-a A.img --floppy-b B.img --nonce N

REPO is sources/misteross. A driver over pc386_wfw_kit_acceptance.Run (lease,
staging, input, HDMI grabs, stop, cleanup, after-state checks); the goals are
the floppy ones. The kit must run a kit-software bundle with F3's agent and
runtime; the caller installs it, claims the kit and restores it.

Launch 1 binds A: (1.44M) and B: (1.2M) through X-FogCast-Floppy-Image and
boots FreeDOS from the hard disk. It types DIR and TYPE on both drives, copies
A:'s file to B:\\WRITTEN.TXT and stops, which saves B:. Launch 2 is the same
library game with the same floppies: it types B:\\WRITTEN.TXT again. It then
copies WRITTEN.TXT to AGAIN.TXT and stops. The mechanical goal reads the
kit's saved floppy records: some new media record.bin must be a
fes.pc386-floppy.image of B:'s size holding A:'s text at least twice, which
it can only do if launch 2 started from launch 1's saved B:. The screen goals
are grabs, judged by reading them.
"""
import hashlib, sys, time
from pathlib import Path

REPO = Path(sys.argv.pop(1)).resolve()
assert (REPO / 'scripts/pc386_wfw_kit_acceptance.py').is_file(), f'{REPO} is not sources/misteross'
sys.path.insert(0, str(REPO))
from scripts import pc386_wfw_kit_acceptance as acc  # noqa: E402
from scripts import pc386_wfw as wfw  # noqa: E402

B_BYTES = 1228800
RECORD_EXTRA = 170  # fes.pc386-floppy.image: payload + 170


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Run(acc.Run):
    def __init__(self, args):
        super().__init__(args)
        self.game = f'f3-floppy-{self.nonce.lower()}'
        self.result['library_game'] = self.game
        self.floppies = [(1, args.floppy_a, sha(args.floppy_a)), (2, args.floppy_b, sha(args.floppy_b))]
        self.result['floppies'] = {str(u): d for u, _, d in self.floppies}
        self.text = f'F3 A DRIVE {args.nonce}'.encode()

    def load(self, game, image, record):
        self.stage(image, record['sha256'])
        for _, path, digest in self.floppies:
            self.stage(path, digest)
        geometry = acc.drives.geometry(record) if 'geometry' in record else \
            f'{image.stat().st_size // (16 * 63 * 512)}/16/63'
        headers = {'X-FogCast-Package-ID': self.args.package.stem, 'X-FogCast-Library-Game': game,
                   'X-FogCast-Disk-Image': record['sha256'], 'X-FogCast-Disk-Geometry': geometry}
        # Two X-FogCast-Floppy-Image lines: _Headers carries them past post().
        self.loaded = acc.post(self.client, '/v1/library/core/load',
                               _Headers(headers, [f'{u}:{d}' for u, _, d in self.floppies]),
                               token=self.session.token, body_path=self.args.package, timeout=360)
        self.log('load', {k: self.loaded.get(k) for k in ('state', 'package_id')} if isinstance(self.loaded, dict) else '')
        self.session.needs_stop = True

    def prompt(self, name, timeout=120):
        frame, seconds = self.wait_for(name, lambda f: wfw.text_screen(f)['found'], timeout, every=2)
        return frame, seconds

    def typed(self, text, name, settle=4):
        self.input().type(text)
        time.sleep(settle)
        self.grab(name)

    def boot_dos(self):
        self.load(self.game, self.image, self.record)
        time.sleep(10)
        frame, seconds = self.prompt('boot1-prompt')
        self.goal('freedos', frame is not None, seconds_to_detect=seconds, frame='frames/boot1-prompt.png')
        time.sleep(5)
        self.typed('cls\ndir a:\n', 'dir-a')
        self.typed('cls\ndir b:\n', 'dir-b')
        self.typed('cls\ntype a:\\fesf3a.txt\ntype b:\\fesf3b.txt\n', 'type-ab')
        self.typed('cls\ncopy a:\\fesf3a.txt b:\\written.txt\ntype b:\\written.txt\n', 'write-b', settle=8)
        self.stop()
        self.goal('launch1', True, frames=['frames/dir-a.png', 'frames/dir-b.png', 'frames/type-ab.png',
                                           'frames/write-b.png'])

    def program_manager(self):
        self.load(self.game, self.image, self.record)
        time.sleep(10)
        frame, seconds = self.prompt('boot2-prompt')
        self.goal('relaunch', frame is not None, seconds_to_detect=seconds, frame='frames/boot2-prompt.png')
        time.sleep(5)
        self.typed('cls\ndir b:\ntype b:\\written.txt\n', 'persist-b')
        # AGAIN.TXT exists only if launch 2 started from launch 1's saved B:.
        self.typed('cls\ncopy b:\\written.txt b:\\again.txt\ndir b:\n', 'again-b', settle=8)

    def mouse(self): pass
    def dos_window(self): pass
    def notepad(self): pass
    def exit_windows(self): pass

    def read_back(self, created):
        # The parent judges 'dos-window' and 'notepad' after the read-back;
        # placeholders that main() below removes.
        self.result['goals'].setdefault('dos-window', {'pass': True, 'placeholder': True})
        self.result['goals'].setdefault('notepad', {'pass': True, 'placeholder': True})
        media = [p for p in created if '/core-data/media/' in p]
        found = []
        for directory in media:
            data = self.ssh(f"if [ -f '{directory}/record.bin' ]; then cat '{directory}/record.bin'; fi", binary=True)
            if data:
                found.append({'path': f'{directory}/record.bin', 'size': len(data),
                              'magic': data[:8].decode('ascii', 'replace'), 'copies': data.count(self.text),
                              'sha256': hashlib.sha256(data).hexdigest(),
                              # Diagnostic: does the saved image still hold B:'s own boot sector and file?
                              'base_boot_sector': data.find(b'MTOO4048'), 'base_entry': data.find(b'FESF3B  TXT')})
        self.result['floppy_records'] = found
        ok = any(r['copies'] >= 2 and r['magic'] == 'FES386F1' and r['size'] == B_BYTES + RECORD_EXTRA for r in found)
        return {'dos': ok, 'notepad': ok, 'files': {'WFWDOS.TXT': None, 'WFWNOTE.TXT': None}}

    def goal(self, name, passed, **evidence):
        if name == 'notepad':
            return
        if name == 'dos-window':
            name = 'b-record'
            evidence = {'records': self.result.get('floppy_records')}
        super().goal(name, passed, **evidence)


class _Headers(dict):
    """The load headers plus the floppy lines, which _post() sends one per line."""
    def __init__(self, base, floppies):
        super().__init__(base)
        self.floppies = floppies


def _post(client, path, headers, token=None, body_path=None, body=None, timeout=60):
    floppies = getattr(headers, 'floppies', [])
    import http.client
    head = {'Authorization': 'Bearer ' + client.bearer, **dict(headers)}
    if token:
        head[acc.kitlib.HEADER] = token
    head['Content-Length'] = str(body_path.stat().st_size)
    head['Content-Type'] = 'application/octet-stream'
    connection = http.client.HTTPConnection(client.url.hostname, client.url.port, timeout=timeout)
    try:
        connection.putrequest('POST', path)
        for key, value in head.items():
            connection.putheader(key, value)
        for value in floppies:
            connection.putheader('X-FogCast-Floppy-Image', value)
        connection.endheaders()
        with body_path.open('rb') as stream:
            while chunk := stream.read(65536):
                connection.send(chunk)
        response = connection.getresponse()
        raw = response.read(1 << 20)
        import json
        try:
            result = json.loads(raw)
        except ValueError:
            result = {}
        if response.status >= 300:
            error = result.get('error', result) if isinstance(result, dict) else {}
            safe = {k: error.get(k) for k in ('code', 'message', 'phase') if isinstance(error, dict) and k in error}
            raise acc.kitlib.KitError(f'{path}: HTTP {response.status} {json.dumps(safe)}', response.status)
        return result
    finally:
        connection.close()


if __name__ == '__main__':
    import argparse
    extra = argparse.ArgumentParser(add_help=False)
    extra.add_argument('--floppy-a', type=Path, required=True)
    extra.add_argument('--floppy-b', type=Path, required=True)
    extra.add_argument('--nonce', required=True)
    mine, rest = extra.parse_known_args(sys.argv[1:])
    assert mine.floppy_a.stat().st_size == 1474560 and mine.floppy_b.stat().st_size == B_BYTES

    original_post = acc.post

    def post(client, path, headers, **kw):
        if isinstance(headers, _Headers):
            return _post(client, path, headers, **kw)
        return original_post(client, path, headers, **kw)
    acc.post = post

    class Bound(Run):
        def __init__(self, args):
            args.floppy_a, args.floppy_b, args.nonce = mine.floppy_a, mine.floppy_b, mine.nonce
            super().__init__(args)
    acc.Run = Bound
    orig = acc.Run.main

    def main(self):
        rc = orig(self)
        for name in ('dos-window', 'notepad'):
            self.result['goals'].pop(name, None)
        want = {'freedos', 'launch1', 'relaunch', 'b-record'}
        self.result['pass'] = self.result.get('failure') is None and set(self.result['goals']) == want and \
            all(g['pass'] for g in self.result['goals'].values())
        self.save()
        print('RESULT', 'PASS' if self.result['pass'] else 'FAIL', flush=True)
        return 0 if self.result['pass'] else 1
    acc.Run.main = main
    sys.exit(acc.main(rest))
