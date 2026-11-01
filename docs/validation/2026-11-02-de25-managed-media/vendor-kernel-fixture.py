import json
import os
from pathlib import Path
import subprocess
import sys

root = Path.cwd()
sys.path.insert(0, str(root))
sys.path.insert(0, str(root / 'scripts'))
from scripts import platform, media_container
from scripts.environment import build_environment

phase = Path('/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-media')
output = phase / 'vendor-kernel-fixture'
output.mkdir()
env = build_environment()
temporary = root / 'out/tmp'
temporary.mkdir(parents=True, exist_ok=True)
env['GOTMPDIR'] = str(temporary)
binary_hash, built = platform.build_static_arm(root, root / 'sources/FogCast', output / 'fes-boot',
                                              env, architecture='aarch64')
(output / 'platform-build.json').write_text(json.dumps(built, indent=2, sort_keys=True) + '\n')
source = {'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
          'bootstrap_binary_sha256': binary_hash}
(output / 'source.json').write_text(json.dumps(source, indent=2) + '\n')
tools = media_container.ensure_media_tools(root, 'docker')
name = 'fes-de25-media-vendor-kernel-' + source['source_revision'][:9]
command = ['docker', 'run', '--rm', '--name', name, '--network=none', '--cap-drop=ALL',
           '--security-opt=no-new-privileges', '--volume', str(root) + ':/work:ro',
           '--volume', str(phase) + ':/proof:ro', '--volume', str(output) + ':/export',
           '--volume', '/home/deano/fes/out/artifacts/de25-kernel-modules/0da63d1044cc7ecf9908efba52af17b3e55e6271/linux-image/vendor-boot:/vendor:ro',
           '--volume', '/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-sdk-integration/linux-base-bootstrap/kernel:/kernel:ro',
           '--entrypoint', 'python3', tools, '/proof/vendor-kernel-fixture-inside.py']
(output / 'container-command.json').write_text(json.dumps({'name': name, 'command': command}, indent=2) + '\n')
subprocess.run(command, check=True)
