#!/usr/bin/env python3
"""Reusable DOS / GoG / eXoDOS simulation harness for CGA, MDA, and Hercules games."""
from __future__ import annotations

import argparse
import fcntl
import json
import os
from pathlib import Path
import subprocess

import pcxt_fat12 as fat12
import sim_fes_pcxt as sim
import sim_fes_pcxt_video_dos as video_dos

ROOT = sim.ROOT
VIDEO_CHOICES = {'cga': 0, 'mda': 1, 'hercules': 2}
TINT_CHOICES = {'white': 0, 'green': 1, 'amber': 2}


def stage_floppy_image(base_floppy: Path | None = None, *, add_files: list[Path] | None = None,
                       add_dir: Path | None = None, command: str | None = None) -> bytes:
    """Build or augment a 360 KiB bootable FAT12 floppy image for guest execution."""
    base = base_floppy.read_bytes() if base_floppy else fat12.checked_base(ROOT)
    payloads: dict[str, bytes] = {}
    if add_dir is not None:
        src_root = add_dir.resolve()
        if not src_root.is_dir():
            raise NotADirectoryError(f'--add-dir is not a directory: {src_root}')
        for path in sorted(src_root.rglob('*')):
            if path.is_file():
                rel = path.relative_to(src_root).as_posix()
                payloads[rel] = path.read_bytes()
    for file_path in add_files or []:
        p = file_path.resolve()
        if not p.is_file():
            raise FileNotFoundError(f'--add-file not found: {p}')
        payloads[p.name] = p.read_bytes()
    if command is not None:
        bat = f'@ECHO OFF\r\nPROMPT $P$G\r\n{command}\r\n'.encode('ascii')
        payloads['AUTOEXEC.BAT'] = bat
    return fat12.inject_files(base, payloads) if payloads else base


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--video', choices=tuple(VIDEO_CHOICES), default='hercules',
                        help='emulated video card: cga, mda, or hercules (default: hercules)')
    parser.add_argument('--tint', choices=tuple(TINT_CHOICES), default='white',
                        help='monochrome phosphor tint for mda/hercules')
    parser.add_argument('--floppy', type=Path,
                        help='existing 360 KiB FAT12 floppy image (defaults to checked-in FreeDOS)')
    parser.add_argument('--add-file', action='append', default=[], type=Path,
                        help='host file to inject into the floppy root (repeatable)')
    parser.add_argument('--add-dir', type=Path,
                        help='directory whose files/subdirs will be injected into the 360 KiB floppy')
    parser.add_argument('--command', help='DOS command line to write into AUTOEXEC.BAT')
    parser.add_argument('--keys', default='',
                        help='comma-separated scripted events, e.g. "wait:12s,snap:title,enter,wait:4s,snap:play"')
    parser.add_argument('--out', type=Path, required=True,
                        help='output directory for PNG screenshots and run-summary.json')
    parser.add_argument('--max-seconds', type=float, default=30.0,
                        help='maximum simulated guest seconds before clean stop (default: 30.0)')
    parser.add_argument('--bios', type=Path, help='optional path to GLABIOS_0.4.2_8X.ROM')
    parser.add_argument('--download', action='store_true', help='allow BIOS download if absent locally')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    return parser.parse_args(argv)


def run(args: argparse.Namespace) -> dict[str, object]:
    build = ROOT / 'build/sim/fes-pcxt/video-dos'
    out_dir = args.out.resolve()
    out_dir.mkdir(parents=True, exist_ok=True)
    for stale in out_dir.glob('*.ppm'):
        stale.unlink()
    for stale in out_dir.glob('*.png'):
        stale.unlink()

    staged_image = stage_floppy_image(
        args.floppy,
        add_files=args.add_file,
        add_dir=args.add_dir,
        command=args.command,
    )
    floppy_out = out_dir / 'floppy.img'
    floppy_out.write_bytes(staged_image)

    lock_dir = ROOT / 'build/sim/fes-pcxt'
    lock_dir.mkdir(parents=True, exist_ok=True)
    with (lock_dir / '.video-dos.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        binary = video_dos.prepare_video_dos_sim(
            build,
            verilator=args.verilator,
            bios_path=args.bios,
            download=args.download,
        )

        cmd = [
            str(binary),
            str(build / 'firmware.bin'),
            str(floppy_out),
            str(VIDEO_CHOICES[args.video]),
            str(TINT_CHOICES[args.tint]),
            'run',
            str(out_dir),
            str(args.max_seconds),
        ]
        if args.keys:
            cmd.append(args.keys)
        subprocess.run(cmd, cwd=build, check=True)

    summary_path = out_dir / 'run-summary.json'
    summary = json.loads(summary_path.read_text())
    pngs: list[str] = []
    for ppm_name in summary.get('captures', []):
        ppm_path = out_dir / ppm_name
        png_path = ppm_path.with_suffix('.png')
        fat12.ppm_to_png(ppm_path, png_path)
        pngs.append(str(png_path))
    summary['video'] = args.video
    summary['tint'] = args.tint
    summary['pngs'] = pngs
    summary_path.write_text(json.dumps(summary, indent=2) + '\n')
    print(json.dumps(summary, indent=2), flush=True)
    return summary


def main() -> None:
    parent = ROOT / 'build/sim/fes-pcxt'
    parent.mkdir(parents=True, exist_ok=True)
    run(parse_args())


if __name__ == '__main__':
    main()
