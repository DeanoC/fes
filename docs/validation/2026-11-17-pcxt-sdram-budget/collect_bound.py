#!/usr/bin/env python3
"""Copy and bind a native fit, reproduce its RBF, then collect conditional STA."""
import argparse
import datetime
import json
from pathlib import Path
import shutil
import subprocess
import threading
import time
from database_identity import identity, sha


def run(work, label, command):
    with (work / (label + '.log')).open('w') as log:
        result = subprocess.run(command, cwd=work, stdout=log, stderr=subprocess.STDOUT)
    if result.returncode:
        raise RuntimeError(label + ' failed: rc=' + str(result.returncode))
    return result.returncode


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('native', 'work', 'quartus-root', 'collector'):
        p.add_argument('--' + name, required=True, type=Path)
    args = p.parse_args()
    native, work = args.native.resolve(), args.work.resolve()
    if shutil.disk_usage(work.parent).free < 30 * 1024**3:
        raise RuntimeError('refuse: under 30 GiB free')
    if work.exists():
        raise RuntimeError('refuse to overwrite an existing work directory')
    start, stop = time.monotonic(), threading.Event()
    native_before = identity(native)
    shutil.copytree(native, work)

    def heartbeat():
        while not stop.is_set():
            (work / 'bound-heartbeat.json').write_text(json.dumps({'elapsed_s': round(time.monotonic()-start)})+'\n')
            if stop.wait(15):
                return

    threading.Thread(target=heartbeat, daemon=True).start()
    receipt = {'native': str(native), 'native_before': native_before,
               'collector_sha256': sha(args.collector), 'driver_sha256': sha(Path(__file__)), 'started_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}
    try:
        receipt['pre_asm'] = identity(work)
        if receipt['pre_asm'] != native_before:
            raise ValueError('database copy is not identical')
        # Delete only our copied RBF, so assembler success cannot reuse a stale file.
        (work / 'output_files/top.rbf').unlink()
        asm = args.quartus_root.resolve() / 'bin/quartus_asm'
        sta = args.quartus_root.resolve() / 'bin/quartus_sta'
        receipt['quartus_asm_sha256'] = sha(asm)
        receipt['quartus_sta_sha256'] = sha(sta)
        # The external timeout caps lock wait and the child; a shared-host lease
        # serializes heavyweight Powerboat jobs without touching another owner.
        prefix = ['timeout', '--signal=TERM', '--kill-after=10', '600',
                  'flock', '-w', '540', '/home/deano/fes-hil/locks/lander-sharedhost.lock',
                  'flock', '-w', '540', '/tmp/lander-quartus-lite.lock']
        receipt['asm_exit_code'] = run(work, 'binding-asm', prefix + [str(asm), 'top'])
        receipt['assembled_rbf_sha256'] = sha(work / 'output_files/top.rbf')
        if receipt['assembled_rbf_sha256'] != sha(native / 'output_files/top.rbf'):
            raise ValueError('database assembler output differs from native RBF')
        receipt['post_asm'] = identity(work)
        # Restore our private databases after assembler-side metadata writes.
        # STA must receive the exact retained native database, not an ASM cache.
        for name in ('db', 'incremental_db'):
            shutil.rmtree(work / name)
            shutil.copytree(native / name, work / name)
        receipt['pre_sta'] = identity(work)
        if receipt['pre_sta'] != native_before:
            raise ValueError('database changed before STA')
        receipt['sta_exit_code'] = run(work, 'conditional-sta', prefix + [str(sta), '-t', str(args.collector.resolve())])
        receipt['post_sta'] = identity(work)
        receipt['reports_sha256'] = {str(p.relative_to(work)): sha(p)
                                    for p in (work / 'conditional').rglob('*') if p.is_file()}
        if identity(native) != native_before:
            raise ValueError('native fitted database changed during collection')
        (work / 'conditional-result.json').write_text(json.dumps({'exit_code': 0, 'classification': 'bound postfit STA, not signoff'})+'\n')
    finally:
        stop.set()
        receipt['elapsed_s'] = round(time.monotonic()-start, 3)
        (work / 'database-receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True)+'\n')
    print('Bound database, reproduced RBF and collected STA:', work, flush=True)


if __name__ == '__main__':
    main()
