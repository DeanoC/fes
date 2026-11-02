import hashlib
import json
from pathlib import Path
import subprocess
import sys

root = Path.cwd()
sys.path.insert(0, str(root / 'image/de25-nano'))
import managed_base as base

phase = Path('/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-media')

def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

canonical = json.loads((root / 'docs/validation/2026-11-01-de25-managed-fabric/build-inputs.json').read_text())['source_inputs']
assert all(sha(root / 'sources/misteross' / path) == expected for path, expected in canonical.items())
combined = json.loads((root / 'sources/misteross/tests/fixtures/de25-combined-proof.json').read_text())
for key in ('compiler_source_sha256', 'test_runtime_source_sha256'):
    assert all(sha(root / path) == expected for path, expected in combined[key].items())
sdk = base.kit_software.de25_sdk(root, Path('/home/deano/fes/out/dev/de25-managed-image/out/de25-managed-sdk-integration/linux-base-bootstrap'))
record = {'source_revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
          'canonical_menu_inputs_verified': len(canonical),
          'combined_compiler_inputs_verified': len(combined['compiler_source_sha256']),
          'runtime_test_inputs_verified': len(combined['test_runtime_source_sha256']),
          'actual_managed_sdk_verified': sdk, 'new_Quartus_or_CPU_model_run': False,
          'hardware_access': 'not-performed'}
subprocess.run(['make', 'check'], check=True)
(phase / 'source-scope.json').write_text(json.dumps(record, indent=2, sort_keys=True) + '\n')
print(json.dumps(record, indent=2))
