import json
from pathlib import Path
import subprocess
import sys
import time

root = Path(sys.argv[1]).resolve()
label = sys.argv[2]
argv = [sys.executable, 'scripts/parent_tests.py', '--video', 'false', '--media', 'false',
        '--paths-json', '["sources/FogCast/host/target_bridge_lease_test.go"]']
commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
start = time.monotonic()
with open('/tmp/fes-parent-go-' + label + '.json', 'w') as output, \
        open('/tmp/fes-parent-go-' + label + '.log', 'w') as log:
    result = subprocess.run(argv, cwd=root, stdout=output, stderr=log)
record = dict(label=label, commit=commit, root=str(root), argv=argv,
              elapsed_s=round(time.monotonic() - start, 3), returncode=result.returncode)
Path('/tmp/fes-parent-go-' + label + '.result.json').write_text(json.dumps(record, indent=2) + '\n')
print(json.dumps(record), flush=True)
sys.exit(result.returncode)
