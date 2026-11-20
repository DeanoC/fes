#!/usr/bin/env python3
"""One private fixed-seed input-register packing diagnostic; never seal or deploy."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import threading
import time


def sha(p):
    with p.open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--native', required=True, type=Path)
    p.add_argument('--work', required=True, type=Path)
    p.add_argument('--quartus-root', required=True, type=Path)
    p.add_argument('--collector', required=True, type=Path)
    args = p.parse_args()
    native, work = args.native.resolve(), args.work.resolve()
    if work.exists():
        raise ValueError('refuse existing work directory')
    work.parent.mkdir(parents=True, exist_ok=True)
    if shutil.disk_usage(work.parent).free < 30 * 1024**3:
        raise ValueError('under 30 GiB free')
    work.mkdir()
    names = ('elaborated.v', 'top.qpf', 'top.qsf', 'clocks.sdc', 'audio.sdc',
             'mailbox.sdc', 'audit.tcl', 'audio-audit.enabled')
    for name in names:
        shutil.copy2(native / name, work / name)
    assignment = '\n# Diagnostic only: keep capture phase and clocks unchanged.\n' + ''.join(
        f'set_instance_assignment -name FAST_INPUT_REGISTER ON -to SDRAM_DQ[{n}]\n' for n in range(16))
    with (work / 'top.qsf').open('a') as f:
        f.write(assignment)
    record = {'classification': 'private diagnostic fit, not production or hardware acceptance',
              'native': str(native), 'started_utc': datetime.now(timezone.utc).isoformat(),
              'native_rbf_sha256': sha(native / 'output_files/top.rbf'),
              'original_inputs': {n: sha(native / n) for n in names},
              'candidate_inputs': {n: sha(work / n) for n in names},
              'qsf_append': assignment, 'collector_sha256': sha(args.collector),
              'driver_sha256': sha(Path(__file__)), 'commands': []}
    start, stop = time.monotonic(), threading.Event()
    def heartbeat():
        while not stop.is_set():
            (work / 'heartbeat.json').write_text(json.dumps({'elapsed_s': round(time.monotonic()-start)})+'\n')
            if stop.wait(15):
                return
    threading.Thread(target=heartbeat, daemon=True).start()
    try:
        prefix = ['timeout', '--signal=TERM', '--kill-after=10', '1200',
                  'flock', '-w', '900', '/home/deano/fes-hil/locks/lander-sharedhost.lock',
                  'flock', '-w', '900', '/tmp/lander-quartus-lite.lock']
        for label, tool, argv in [('compile', 'quartus_sh', ['--flow','compile','top']),
                                  ('audit', 'quartus_sta', ['-t','audit.tcl']),
                                  ('conditional', 'quartus_sta', ['-t',str(args.collector.resolve())])]:
            binary = args.quartus_root.resolve() / 'bin' / tool
            command = prefix + [str(binary)] + argv
            item = {'label': label, 'argv': command, 'binary_sha256': sha(binary)}
            record['commands'].append(item)
            with (work / (label+'.log')).open('w') as log:
                item['exit_code'] = subprocess.run(command,cwd=work,stdout=log,stderr=subprocess.STDOUT).returncode
            if item['exit_code']:
                raise RuntimeError(label+' failed: '+str(item['exit_code']))
        record['rbf_sha256'] = sha(work / 'output_files/top.rbf')
        if any(sha(native/n) != record['original_inputs'][n] for n in names):
            raise ValueError('native inputs changed')
        record['status'] = 'captured; timing acceptance requires report assessment'
    except Exception as error:
        record['status'], record['error'] = 'failed', str(error)
        raise
    finally:
        stop.set()
        record['elapsed_s'] = round(time.monotonic()-start,3)
        (work / 'receipt.json').write_text(json.dumps(record,indent=2,sort_keys=True)+'\n')
        print(record['status'], work, flush=True)


if __name__ == '__main__':
    main()
