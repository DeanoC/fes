#!/usr/bin/env python3
"""build-863s5-image.py: image + release + bundle + host for #863 slice 5, using
Lander's baseline build recipe (make build/verify/release/host, kit-software)
in a private directory. Builds only; installs nothing."""
import json, sys
from pathlib import Path
sys.path.insert(0, '/home/deano/lander-hilq')
import baseline  # noqa: E402

head = sys.argv[1]
d = Path('/home/deano/fes-scratch/k863/img5')
current = json.load(open('/home/deano/lander-hilq/current.json'))
candidate = baseline.build(head, d, current)
print(json.dumps(candidate, indent=1))
