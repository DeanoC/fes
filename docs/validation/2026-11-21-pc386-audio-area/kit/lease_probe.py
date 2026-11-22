#!/usr/bin/env python3
"""s4_lease_probe.py KIT: read-only; exit 0 only if the agent is idle and its lease free (pc386 audio area)."""
import os, sys
from pathlib import Path
sys.path.insert(0, '/home/deano/fes/.claude/worktrees/blissful-maxwell-48aa5e/sources/misteross/scripts')
import kit as kitlib
KIT = sys.argv[1]
TARGET = {'A': 'dev', 'B': 'kit2'}[KIT]
config = Path('/home/deano/fes-scratch/audio-area/kit/tmp') / f'probe-kit{KIT}.toml'
source = (Path.home() / '.config/fogcast/config.toml').read_text()
config.touch(mode=0o600)
try:
    config.write_text(source.replace("selected_target = 'dev'", f"selected_target = '{TARGET}'"))
    os.chmod(config, 0o600)
    class Args: pass
    args = Args(); args.config, args.url = config, None
    client = kitlib.configuration(args)
finally:
    config.unlink()
s, l = client.request('/v1/status'), client.request('/v1/kit/lease')
print(KIT, s.get('state'), s.get('observed_core') or s.get('core'), l.get('state'), l.get('owner', ''))
sys.exit(0 if s.get('state') == 'idle' and l.get('state') == 'free' else 1)
