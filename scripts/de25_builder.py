"""Resolve an immutable SDK config on Docker's graphdriver or OCI image store."""
from pathlib import Path
import hashlib
import json
import os
import re
import subprocess
import tomllib

ROOT = Path(__file__).resolve().parents[1]
PIN = 'image/kernel/de25-dma'
EXECUTION = 'image/de25-nano/sdk-builder'
DIGEST = re.compile(r'sha256:[0-9a-f]{64}')
MANIFEST_TYPE = 'application/vnd.oci.image.manifest.v1+json'


def workspace_command(runtime, execution_reference, volume):
    """Initialize only an empty workspace when the locked image's UID differs."""
    if (not DIGEST.fullmatch(execution_reference) or
            not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', volume)):
        raise ValueError('invalid private SDK workspace identity')
    script = '''set -eu
target=/target-image-output
test -d "$target"
test ! -L "$target"
if [ "$(stat -c '%u:%g' "$target")" != "$1:$2" ]; then
    entries=$(find "$target" -mindepth 1 -maxdepth 1 -print -quit)
    test -z "$entries"
    chown "$1:$2" "$target"
fi
'''
    return [runtime, 'run', '--rm', '--network', 'none', '--platform', 'linux/amd64',
            '--user', '0:0', '--mount',
            'type=volume,src=' + volume + ',dst=/target-image-output,volume-nocopy',
            execution_reference, 'sh', '-c', script, 'fes-sdk-workspace',
            str(os.getuid()), str(os.getgid())]


def locked_manifest(root=ROOT):
    root = Path(root)
    lock = tomllib.loads((root / PIN / 'linux.lock.toml').read_text())
    execution = tomllib.loads((root / EXECUTION / 'identity.lock.toml').read_text())
    if execution.get('format') != 1:
        raise ValueError('invalid SDK builder execution lock')
    lock = dict(lock, builder_manifest=execution.get('manifest', ''))
    path = root / EXECUTION / 'manifest.json'
    if path.is_symlink() or not path.is_file():
        raise ValueError('SDK builder manifest must be a regular file')
    if path.stat().st_size > 1 << 16:
        raise ValueError('SDK builder manifest is oversized')
    raw = path.read_bytes()
    if (not DIGEST.fullmatch(lock.get('builder_manifest', '')) or
            'sha256:' + hashlib.sha256(raw).hexdigest() != lock['builder_manifest']):
        raise ValueError('SDK builder manifest differs from lock')
    manifest = json.loads(raw)
    if not isinstance(manifest, dict):
        raise ValueError('invalid SDK builder manifest')
    config, layers = manifest.get('config', {}), manifest.get('layers', [])
    if (not isinstance(config, dict) or
            manifest.get('schemaVersion') != 2 or manifest.get('mediaType') != MANIFEST_TYPE or
            not DIGEST.fullmatch(lock.get('builder_image', '')) or
            config.get('mediaType') != 'application/vnd.oci.image.config.v1+json' or
            config.get('digest') != lock['builder_image'] or
            type(config.get('size')) is not int or not 0 < config['size'] <= 1 << 20 or
            not isinstance(layers, list) or not 0 < len(layers) <= 128 or
            any(not isinstance(layer, dict) or
                layer.get('mediaType') != 'application/vnd.oci.image.layer.v1.tar' or
                not DIGEST.fullmatch(layer.get('digest', '')) or
                type(layer.get('size')) is not int or not 0 < layer['size'] <= 8 << 30
                for layer in layers)):
        raise ValueError('SDK builder manifest content contract differs')
    # These are uncompressed layers: their OCI digests are the config diff IDs.
    return lock, manifest, len(raw)


def resolve(runtime, expected_config, *, root=ROOT, env=None):
    """Return a checked execution reference; expected_config stays provenance.

    Only the authenticated locked manifest is an alternate address for the
    same config. No mutable tag, image-list scan, pull or container mutation.
    Other SDK producer configs must remain directly addressable by config ID.
    """
    if not DIGEST.fullmatch(expected_config):
        raise ValueError('invalid SDK builder config identity')
    lock, manifest, manifest_size = locked_manifest(root)
    selected = expected_config == lock['builder_image']
    refs = [expected_config, lock['builder_manifest']] if selected else [expected_config]
    for ref in refs:
        result = subprocess.run([runtime, 'image', 'inspect', ref],
                                capture_output=True, text=True, env=env, timeout=30)
        if result.returncode:
            continue
        try:
            rows = json.loads(result.stdout)
            if not isinstance(rows, list) or len(rows) != 1 or not isinstance(rows[0], dict):
                raise ValueError('expected one inspected builder')
            image = rows[0]
            identity = image.get('Id')
            rootfs = image.get('RootFS', {})
            allowed = refs if selected else [expected_config]
            if (identity not in allowed or image.get('Os') != 'linux' or
                    image.get('Architecture') != 'amd64' or
                    not isinstance(rootfs, dict) or
                    rootfs.get('Type') != 'layers' or not isinstance(rootfs.get('Layers'), list) or
                    not rootfs['Layers'] or
                    any(not DIGEST.fullmatch(layer) for layer in rootfs['Layers'])):
                raise ValueError('SDK and runtime builder identities differ')
            if selected and rootfs['Layers'] != [layer['digest'] for layer in manifest['layers']]:
                raise ValueError('SDK builder rootfs differs from pinned content')
            if identity == lock['builder_manifest']:
                descriptor = image.get('Descriptor', {})
                if (not isinstance(descriptor, dict) or descriptor.get('digest') != identity or
                        descriptor.get('mediaType') != MANIFEST_TYPE or
                        descriptor.get('size') != manifest_size):
                    raise ValueError('SDK builder execution descriptor differs')
            return identity
        except (TypeError, KeyError, json.JSONDecodeError) as error:
            raise ValueError('invalid SDK builder inspection') from error
    raise ValueError('locked kernel/SDK builder is unavailable or differs')
