#!/usr/bin/env python3
"""Select one retained suite for a bounded retry; keep replay checks unchanged."""
import argparse
import importlib.util
import json
from pathlib import Path

here = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('selected_board_replay', here / 'replay.py')
replay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(replay)


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('command', choices=['prepare', 'clients'])
    for name in ('config', 'retained', 'output'):
        p.add_argument('--' + name, type=Path, required=True)
    p.add_argument('--board', required=True)
    p.add_argument('--model', choices=tuple(replay.MODELS), required=True)
    args = p.parse_args()
    args.retained = args.retained.resolve()
    args.output = args.output.resolve()
    boards = [b for b in replay.settings(args) if b.serial == args.board]
    if len(boards) != 1:
        raise ValueError('Exactly one configured board must match the selector')
    replay.settings = lambda ignored: boards
    replay.MODELS = {args.model: replay.MODELS[args.model]}
    (replay.prepare if args.command == 'prepare' else replay.clients)(args)
    metadata = {'selected_board': args.board, 'selected_model': args.model,
                'selector_sha256': replay.sha(__file__), 'replay_sha256': replay.sha(here / 'replay.py'),
                'session_host_watchdog_seconds': 1800, 'retained_binaries_seed_and_guest_limits': 'unchanged'}
    (args.output / 'selection.json').write_text(json.dumps(metadata, indent=2) + '\n')
