#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Read the running pc386 core's status ring on a kit.

pc386_status (cores/fes-pc386/rtl/pc386_status.sv) writes the system's status
words into the DDR window every 2^16 clocks (1.07 ms at 61.44 MHz): the head
at physical 0x31000000 holds {MAGIC, records written}, and record n holds
{n, MAGIC}, the six status words and the clock count at the latch, 64 bytes at
0x31001000 + 64 x (n mod 256). This reads them from the kit's HPS with busybox
devmem over ssh while the core runs, through no runtime, agent or FogCast
call, so it works on any kit software. A record is not written again for 255
periods (273 ms), so a sample is whole if word 0 still names it.

  sample    one remote shell loop reads the head and the newest record's
            words as fast as devmem goes (about 5 ms a word, so 20-40
            samples a second), or every --interval seconds, for --seconds;
            each line is the host time, the kit's uptime and the decoded record.
  snapshot  the newest record, every field, as JSON.

--ssh is the command that runs a command on the kit, such as
~/fes-scratch/m5/kB/kb or "ssh root@fes-kit-b". Measurements only: the
reader never writes the kit.

  python3 scripts/pc386_cpu_status.py --ssh ~/fes-scratch/m5/kB/kb snapshot
  python3 scripts/pc386_cpu_status.py --ssh ~/fes-scratch/m5/kB/kb sample --seconds 600 --log run.jsonl
"""
from __future__ import annotations

import argparse
import json
import os
import shlex
import signal
import subprocess
import sys
import time

WINDOW_BASE = 0x30000000
HEAD = WINDOW_BASE + 0x01000000
RECORDS = WINDOW_BASE + 0x01001000
SLOTS = 256
MAGIC = 0x50333836
SYSTEM_CLOCK_HZ = 61.44e6
# The record's words a sample reads: 0 (the name) first and last, 1 (flags,
# POST, resets, retired), 2 (CS, vector, EIP), 3 (frames, scanout
# underflows), 5 (plane underflows, mixer samples), 7 (the clock count).
SAMPLE_WORDS = (0, 1, 2, 3, 5, 7)
ALL_WORDS = tuple(range(8))


def remote_loop(words: tuple[int, ...], seconds: float, interval: float) -> str:
    """The kit's busybox shell loop: per sample a line 'S <uptime> <head>',
    then a line per word 'W <n> <value>', and 'E' once word 0 is read again.
    It ends at --seconds, or once 'E' cannot be written: the shell sshd starts
    ignores SIGPIPE, so a reader that goes away would otherwise leave it running."""
    reads = ''.join(f'echo W {n} $(devmem $((r + {8 * n})) 64); ' for n in words)
    pause = f'usleep {int(interval * 1e6)}; ' if interval > 0 else ''
    return (f'read u0 _ < /proc/uptime; end=$((${{u0%.*}} + {int(seconds) + 1})); '
            f'while :; do read u _ < /proc/uptime; [ ${{u%.*}} -ge $end ] && break; '
            f'h=$(devmem {HEAD:#x} 64); '
            f'n=$(( (h & 0xffffffff) - 1 )); r=$(( {RECORDS:#x} + 64 * (n & {SLOTS - 1}) )); '
            f'echo S $u $h; {reads}echo W 0 $(devmem $r 64); echo E || break; {pause}done')


def decode(head: int, words: dict[int, int]) -> dict:
    """A record as fields; 'whole' is false if the record was not the head's
    newest or was rewritten while it was read."""
    out = {'magic': head >> 32 == MAGIC, 'count': head & 0xffffffff}
    n = out['count'] - 1
    out['record'] = n
    out['whole'] = out['magic'] and n >= 0 and all(w == (n << 32 | MAGIC) for w in words.get('names', []))
    s0, s1 = words.get(1), words.get(2)
    if s0 is not None:
        out.update(halted=s0 & 1, stopped=s0 >> 1 & 1, a20m=s0 >> 2 & 1, intr=s0 >> 3 & 1,
                   running=s0 >> 4 & 1, governed=s0 >> 5 & 1, post=f'{s0 >> 8 & 0xff:02x}',
                   cpu_resets=s0 >> 16 & 0xff, retired=s0 >> 32)
    if s1 is not None:
        out.update(cs=f'{s1 & 0xffff:04x}', vector=f'{s1 >> 24 & 0xff:02x}', eip=f'{s1 >> 32:08x}')
    if 3 in words:
        out.update(frames_shown=words[3] & 0xffffffff, scanout_underflows=words[3] >> 32)
    if 4 in words:
        out.update(capture_drops=words[4] & 0xffffffff, present_underflows=words[4] >> 32)
    if 5 in words:
        out.update(plane_underflows=words[5] & 0xffffffff, samples=words[5] >> 32)
    if 6 in words:
        out.update(dma_cycles=words[6] & 0xffffffff, opl2_samples=words[6] >> 32)
    if 7 in words:
        out.update(clocks=words[7], core_seconds=round(words[7] / SYSTEM_CLOCK_HZ, 6))
    return out


def parse(lines, host_time=time.time):
    """Samples from the remote loop's lines, each decoded and stamped with the
    host time at its last line and the kit's uptime at its first."""
    head, uptime, words = None, None, {}
    for line in lines:
        parts = line.split()
        if not parts:
            continue
        try:
            if parts[0] == 'S' and len(parts) == 3:
                uptime, head, words = float(parts[1]), int(parts[2], 16), {'names': []}
            elif parts[0] == 'W' and len(parts) == 3 and head is not None:
                n, value = int(parts[1]), int(parts[2], 16)
                if n == 0:
                    words['names'].append(value)
                else:
                    words[n] = value
            elif parts[0] == 'E' and head is not None:
                yield {'host_time': round(host_time(), 3), 'kit_uptime': uptime, **decode(head, words)}
                head = None
        except ValueError:
            head = None


def run(ssh: list[str], command: str):
    """The remote loop's stdout lines. The ssh command runs in its own process
    group, which this reader stops whole: a wrapper script's ssh would
    otherwise outlive it and keep the kit's loop running."""
    child = subprocess.Popen([*ssh, command], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, text=True,
                             start_new_session=True)
    try:
        yield from child.stdout
    finally:
        try:
            os.killpg(child.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        child.wait(timeout=10)


def line(sample: dict) -> str:
    if not sample['magic']:
        return f"{sample['host_time']:.3f} {sample['kit_uptime']:.2f} no status ring (head {sample['count']:#x})"
    if not sample['whole']:
        return f"{sample['host_time']:.3f} {sample['kit_uptime']:.2f} torn record {sample['record']}"
    flags = ''.join(c for c, key in (('H', 'halted'), ('S', 'stopped'), ('R', 'running'), ('I', 'intr'))
                    if sample.get(key))
    return (f"{sample['host_time']:.3f} {sample['kit_uptime']:.2f} core {sample.get('core_seconds', 0):.6f} "
            f"{sample['cs']}:{sample['eip']} vec {sample['vector']} {flags or '-'} post {sample['post']} "
            f"resets {sample['cpu_resets']} retired {sample['retired']} frames {sample.get('frames_shown')} "
            f"samples {sample.get('samples')}")


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--ssh', required=True, help='the command that runs a command on the kit')
    sub = parser.add_subparsers(dest='mode', required=True)
    sample = sub.add_parser('sample')
    sample.add_argument('--seconds', type=float, default=60)
    sample.add_argument('--interval', type=float, default=0, help='pause between samples (0: as fast as devmem)')
    sample.add_argument('--log', help='also append each sample as a JSON line here')
    sub.add_parser('snapshot')
    args = parser.parse_args(argv)
    ssh = shlex.split(args.ssh)
    if args.mode == 'snapshot':
        for item in parse(run(ssh, remote_loop(ALL_WORDS, 0, 0).replace('while :;', 'for once in 1;'))):
            print(json.dumps(item, indent=2))
            return 0 if item['whole'] else 1
        return 1
    log = open(args.log, 'a') if args.log else None
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    for item in parse(run(ssh, remote_loop(SAMPLE_WORDS, args.seconds, args.interval))):
        print(line(item), flush=True)
        if log:
            log.write(json.dumps(item) + '\n')
            log.flush()
    return 0


if __name__ == '__main__':
    sys.exit(main())
