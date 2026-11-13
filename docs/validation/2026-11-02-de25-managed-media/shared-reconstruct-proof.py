import json
from pathlib import Path
import subprocess
import sys

root = Path.cwd()
sys.path.insert(0, str(root))
from scripts import media_container

phase = Path('/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-media')
inputs = phase / 'vendor-kernel-fixture'
assert json.loads((inputs / 'receipt.json').read_text())['hardware_access'] == 'not-performed'
output = phase / 'shared-reconstruct-proof'
output.mkdir()
tools = media_container.ensure_media_tools(root, 'docker')
name = 'fes-de25-shared-reconstruct-f9b522b6c'
command = ['docker', 'run', '--rm', '--name', name, '--network=none', '--cap-drop=ALL',
           '--security-opt=no-new-privileges', '--volume', str(root) + ':/work:ro',
           '--volume', str(phase) + ':/proof:ro', '--volume', str(inputs) + ':/inputs:ro',
           '--volume', str(output) + ':/export',
           '--volume', '/home/deano/fes/out/artifacts/de25-kernel-modules/0da63d1044cc7ecf9908efba52af17b3e55e6271/linux-image/vendor-boot:/vendor:ro',
           '--volume', '/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-sdk-integration/linux-base-bootstrap/kernel:/kernel:ro',
           '--entrypoint', 'python3', tools, '/proof/shared-reconstruct-inside.py']
(output / 'container-command.json').write_text(json.dumps({'name': name, 'command': command}, indent=2) + '\n')
subprocess.run(command, check=True)
