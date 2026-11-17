#!/usr/bin/env python3
"""Write the frozen COM only to a regular image under this run's directory."""
from pathlib import Path
import argparse, os, subprocess, sys
RUN = (Path(__file__).resolve().parent / 'reference-vm' / 'run').resolve()
PAYLOAD = (Path(__file__).resolve().parent / 'SURVEY.COM').resolve()
OFFSET = 63 * 512

def target(path):
    p = Path(path)
    if not p.is_absolute() or p.is_symlink(): raise ValueError('target must be an absolute non-symlink path')
    resolved = p.resolve(strict=True)
    if resolved.parent != RUN or resolved != p or not resolved.is_file() or resolved.stat().st_nlink != 1:
        raise ValueError('target must be a private single-link image directly in the run directory')
    return resolved

def main():
    parser=argparse.ArgumentParser(); parser.add_argument('image'); parser.add_argument('--check',action='store_true'); args=parser.parse_args()
    try: image=target(args.image)
    except (OSError,ValueError) as e: print(f'rejected: {e}',file=sys.stderr); return 2
    print(f'verified target: {image}',flush=True)
    if args.check: return 0
    env=os.environ.copy(); env['MTOOLSRC']='/dev/null'
    return subprocess.run(['mcopy','-o','-i',f'{image}@@{OFFSET}',str(PAYLOAD),'::/SURVEY.COM'],env=env,check=False).returncode
if __name__=='__main__': sys.exit(main())
