#!/usr/bin/env python3
"""Generate structural DE25 cold-start fixtures; synthetic bytes, never deploy."""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import core_bundle_fixtures as v2
ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = ROOT / 'testdata/core-bundle-v5'
PERSONA = b'opaque-synthetic-persona\x00\xff'

def identity(manifest, base, persona):
    digest = hashlib.sha256(b'FES-CORE-PACKAGE-5\n')
    for data in (manifest, base, persona):
        digest.update(struct.pack('<Q', len(data)))
        digest.update(data)
    return digest.hexdigest()

def manifest():
    text = v2.canonical_manifest().decode().replace('format = 2', 'format = 5', 1)
    for old, new in [('fes.pong', 'fes.zx81'), ('FES Pong', 'FES ZX81'),
                     ('de10_nano', 'de25_nano'), ('5CSEBA6U23I7', 'A5EB013BB23BE4SCS'),
                     ('fes-gp-v1', 'de25-linux-static-computer-v1'),
                     ('fes.simple-game', 'fes.computer'), ('fes.gamepad', 'fes.keyboard.hid')]:
        text = text.replace(old, new)
    return (text + f'''\n[static_expansion]
file = "persona.rbf"
size = {len(PERSONA)}
sha256 = "{hashlib.sha256(PERSONA).hexdigest()}"
control_build_id = "{'1' * 32}"
hardware_id = {0xc001a001}
root_qdb_sha256 = "{'2' * 64}"
interface_sha256 = "{'3' * 64}"
clock_reset_sha256 = "{'4' * 64}"
pof0_hash = "{'5' * 64}"
''').encode()

def generated_files():
    base = manifest()
    variants = {'valid-basic': base}
    def change(name, old, new):
        assert old in base
        variants[name] = base.replace(old, new, 1)
    change('invalid-platform', b'platform = "de25_nano"', b'platform = "de10_nano"')
    change('invalid-device', b'A5EB013BB23BE4SCS', b'5CSEBA6U23I7')
    change('invalid-profile', b'de25-linux-static-computer-v1', b'de25-linux-computer-v1')
    change('invalid-filename', b'file = "persona.rbf"', b'file = "../persona.rbf"')
    change('invalid-default-id', b'hardware_id = 3221331969', b'hardware_id = 3221401602')
    change('invalid-float-id', b'hardware_id = 3221331969', b'hardware_id = 3221331969.0')
    change('invalid-float-size', f'size = {len(PERSONA)}'.encode(), f'size = {len(PERSONA)}.0'.encode())
    change('invalid-empty-size', f'size = {len(PERSONA)}'.encode(), b'size = 0')
    change('invalid-size-binding', f'size = {len(PERSONA)}'.encode(), f'size = {len(PERSONA)+1}'.encode())
    change('invalid-hash-binding', hashlib.sha256(PERSONA).hexdigest().encode(), b'a' * 64)
    for name, field, count in [('controller', 'control_build_id', 32), ('root', 'root_qdb_sha256', 64),
                                ('interface', 'interface_sha256', 64), ('clock', 'clock_reset_sha256', 64), ('pof', 'pof0_hash', 64)]:
        import tomllib
        value = tomllib.loads(base.decode())['static_expansion'][field]
        change('invalid-zero-' + name, f'{field} = "{value}"'.encode(), f'{field} = "{"0" * count}"'.encode())
    variants['invalid-unknown-field'] = base + b'extra = true\n'
    variants['invalid-mixed-rom'] = base + b'\n[rom_map]\nfile = "rom-map.json"\nsize = 1\nsha256 = "' + b'1' * 64 + b'"\n'
    variants['invalid-missing-static'] = base.split(b'\n[static_expansion]')[0]
    files = {'payloads/base.rbf': v2.PAYLOAD, 'payloads/persona.rbf': PERSONA}
    cases = []
    for name, value in variants.items():
        path = f'manifests/{name}.toml'
        files[path] = value
        case = dict(name=name, manifest=path, payload='payloads/base.rbf', persona='payloads/persona.rbf', valid=name.startswith('valid-'))
        if case['valid']:
            case['package_id'] = identity(value, v2.PAYLOAD, PERSONA)
            files['identities/valid-basic.sha256'] = (case['package_id'] + '\n').encode()
        cases.append(case)
    # Independently generated vectors for the normal opaque partial asset and
    # default/selected composition identity. These are not vendor bitstreams.
    package_id = cases[0]['package_id']
    ram = b'opaque-synthetic-ram16k\x00\xff'
    partial = dict(control_build_id='1' * 32, hardware_id=0xc002b002,
                   root_qdb_sha256='2' * 64, interface_sha256='3' * 64,
                   clock_reset_sha256='4' * 64, pof0_hash='5' * 64)
    asset = dict(partial=partial, cart_sha256=hashlib.sha256(ram).hexdigest(),
                 cart_size=len(ram), device='A5EB013BB23BE4SCS', format=2,
                 map='fes.de25-zx81-bus.partial/1', recipe_sha256='c' * 64,
                 revision='d' * 40, shell_build_id='0123456789abcdef0123456789abcdef',
                 shell_package_id=package_id, shell_sha256=v2.PAYLOAD_SHA256,
                 slot='fes.expansion.zx81-bus', slot_major=2, slot_minor=0)
    asset_bytes = json.dumps(asset, separators=(',', ':')).encode()
    asset_id = hashlib.sha256(b'fes-expansion-v2\x00' + asset_bytes).hexdigest()
    files['expansions/ram16k/manifest.json'] = asset_bytes
    files['expansions/ram16k/cart.rbf'] = ram
    compositions = []
    for name, expansion_id, image, hardware_id in [('empty', '', PERSONA, 0xc001a001),
                                                 ('ram16k', asset_id, ram, 0xc002b002)]:
        image_hash = hashlib.sha256(image).hexdigest()
        material = [package_id, expansion_id, v2.PAYLOAD_SHA256, str(len(v2.PAYLOAD)),
                    image_hash, str(len(image)), '1' * 32, str(hardware_id)]
        composition_id = hashlib.sha256(b'fes-de25-static-composition-v1\x00' + '\x00'.join(material).encode()).hexdigest()
        plan = dict(composition_id=composition_id, package_id=package_id)
        if expansion_id:
            plan['expansion_id'] = expansion_id
        plan.update(base_sha256=v2.PAYLOAD_SHA256, base_size=len(v2.PAYLOAD),
                    persona_sha256=image_hash, persona_size=len(image),
                    control_build_id='1' * 32, hardware_id=hardware_id)
        compositions.append(dict(name=name, composition=plan))
    files['compositions.json'] = (json.dumps(compositions, indent=2) + '\n').encode()
    files['cases.json'] = (json.dumps(cases, indent=2) + '\n').encode()
    files['README.md'] = b'''# Format5 structural conformance fixtures\n\nOpaque synthetic base/persona bytes; never deploy. Package identity hashes the\nformat5 domain and little-endian 64-bit lengths plus exact manifest/base/persona\nbytes. Valid parsing does not admit the unregistered static programming profile\nor qualify a partial image, electrical setup, timing or hardware operation.\n'''
    return files

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    v2.FIXTURE_ROOT = FIXTURE_ROOT
    return v2.check(generated_files()) if args.check else v2.write(generated_files())
if __name__ == '__main__':
    raise SystemExit(main())
