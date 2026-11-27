"""Emit the private EL3 upload identity from the fitted, verified C64 package."""

import json
import subprocess
import sys
from pathlib import Path

if __package__:
    from . import spectrum48
else:
    import spectrum48


def identity_from_proof(proof, recorded):
    fields = ('package_id', 'build_id', 'core_size', 'core_sha256', 'core_sha384',
              'chunk_sha384', 'handoff_sha256')
    if (not proof.get('c64_picker') or
            any(proof[key] != recorded.get(key) for key in fields) or
            any(recorded.get(key) != value for key, value in spectrum48.BASES.items())):
        raise ValueError('live C64 observer differs from its fitted picker, package or fixed aperture')
    return {key: proof[key] for key in fields}


def matched_identity(build, archive, metadata):
    # Use the producer worker's revision-aware verification, fitted RTL,
    # closed timing and actual RBF checks. The SD metadata is a comparison.
    proof = json.loads(subprocess.check_output(
        [sys.executable, str(Path(spectrum48.__file__).resolve()), '--c64', str(build), str(archive)],
        timeout=180))
    recorded = json.loads(spectrum48.read(metadata, 4096))
    return identity_from_proof(proof, recorded)


def header(record):
    return '#define FES_NANO_C64_SCOPE_IDENTITY ' + spectrum48.upload_initializer(record) + '\n'


if __name__ == '__main__':
    build, archive, metadata = map(Path, sys.argv[1:])
    sys.stdout.write(header(matched_identity(build, archive, metadata)))
