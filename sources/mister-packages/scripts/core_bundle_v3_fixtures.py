#!/usr/bin/env python3
"""Generate synthetic format-3 package fixtures; no hardware-valid RBF is used."""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import struct
import core_bundle_fixtures as v2

ROOT = Path(__file__).resolve().parents[1]
FIXTURE_ROOT = ROOT / 'testdata' / 'core-bundle-v3'


def identity(manifest, payload, mapping):
    result = hashlib.sha256(b'FES-CORE-PACKAGE-3\n')
    for data in (manifest, payload, mapping):
        result.update(struct.pack('<Q', len(data)))
        result.update(data)
    return result.hexdigest()


def rom_map():
    return dict(format=1, device='5CSEBA6U23I7', encoding='m10k-1024x10-v1',
                base_sha256=v2.PAYLOAD_SHA256, source_size=1024,
                blocks=[dict(bel='M10K.005.073', source_offset=0,
                    word_bits=[[32*7605 + word*40 + bit for bit in range(40)] for word in range(256)])])


def encode_map(mapping):
    return (json.dumps(mapping, separators=(',', ':')) + '\n').encode()


def manifest(mapping, role='firmware'):
    return v2.canonical_manifest().replace(b'format = 2', b'format = 3', 1) + f'''
[rom]
id = "machine-rom"
role = "{role}"
source_size = 1024
file = "rom-map.json"
size = {len(mapping)}
sha256 = "{hashlib.sha256(mapping).hexdigest()}"
'''.encode()


def generated_files():
    mapping = encode_map(rom_map())
    base = manifest(mapping)
    files = {'payloads/fes-fixture.rbf': v2.PAYLOAD, 'maps/valid-basic.json': mapping}
    cases = []

    def add(name, man=base, data=mapping, reason=None):
        mp = f'manifests/{name}.toml'
        rp = 'maps/valid-basic.json' if data == mapping else f'maps/{name}.json'
        files[mp], files[rp] = man, data
        case = dict(name=name, manifest=mp, payload='payloads/fes-fixture.rbf', rom_map=rp, valid=reason is None)
        if reason is not None:
            case['validation_layer'] = 'rom_map' if name.startswith('invalid-map-') else 'package'
        case['package_id' if reason is None else 'reason'] = identity(man, v2.PAYLOAD, data) if reason is None else reason
        cases.append(case)

    add('valid-basic')
    add('valid-cartridge', manifest(mapping, 'cartridge'))
    add('valid-literal-rom', base.replace(b'id = "machine-rom"', b"id = 'machine-rom'"))
    def nano_manifest(data, size=4096):
        text=manifest(data).replace(b'de10_nano',b'de25_nano').replace(b'5CSEBA6U23I7',b'A5EB013BB23BE4SCS')
        text=text.replace(b'source_size = 1024',f'source_size = {size}'.encode())
        return text.replace(b'\n[rom]',b'\n[[interfaces]]\nid = "fes.embedded.rom"\nmajor = 1\nminor = 0\nrequired = true\n\n[rom]')

    for size in (4096,8192):
        value=dict(format=1,device='A5EB013BB23BE4SCS',encoding='cpu-csr-v1',
                   base_sha256=v2.PAYLOAD_SHA256,source_size=size)
        data=encode_map(value)
        add(f'valid-nano-{size}',nano_manifest(data,size),data)
    value=dict(format=1,device='A5EB013BB23BE4SCS',encoding='cpu-csr-v1',
               base_sha256=v2.PAYLOAD_SHA256,source_size=4096)
    data=encode_map(value);man=nano_manifest(data)
    for name,change in [('format',{'format':True}),('source-float',{'source_size':4096.0}),
            ('size',{'source_size':8192}),('device',{'device':'5CSEBA6U23I7'}),
            ('encoding',{'encoding':'m10k-1024x10-v1'}),('base',{'base_sha256':'0'*64}),
            ('address',{'cpu_base':0x20010500}),('blocks',{'blocks':[]}),('null',{'encoding':None})]:
        raw=encode_map(dict(value,**change))
        add('invalid-map-nano-'+name,nano_manifest(raw),raw,'invalid held-ROM map '+name)
    for name,raw in [('duplicate',data.replace(b'"format":1',b'"format":1,"format":1')),
            ('trailing',data+b'{}'),('missing',encode_map({k:v for k,v in value.items() if k!='encoding'})),
            ('oversized',data+b' '*1024)]:
        add('invalid-map-nano-'+name,nano_manifest(raw),raw,'invalid held-ROM map '+name)
    for name,bad in [('role',man.replace(b'role = "firmware"',b'role = "cartridge"')),
            ('size',man.replace(b'source_size = 4096',b'source_size = 1024')),
            ('optional-interface',man.replace(b'id = "fes.embedded.rom"\nmajor = 1\nminor = 0\nrequired = true',
                 b'id = "fes.embedded.rom"\nmajor = 1\nminor = 0\nrequired = false')),
            ('missing-interface',man.replace(b'id = "fes.embedded.rom"',b'id = "fes.other.rom"'))]:
        add('invalid-nano-'+name,bad,data,'invalid Nano ROM declaration '+name)
    add('invalid-missing-rom', base.split(b'\n[rom]')[0], reason='required ROM declaration absent')
    add('invalid-rom-role', base.replace(b'role = "firmware"', b'role = "uploaded"'), reason='invalid role')
    add('invalid-rom-id', base.replace(b'id = "machine-rom"', b'id = "../rom"'), reason='invalid identifier')
    add('invalid-rom-file', base.replace(b'file = "rom-map.json"', b'file = "../rom-map.json"'), reason='noncanonical map file')
    add('invalid-rom-size', base.replace(f'size = {len(mapping)}'.encode(), b'size = 1'), reason='map size mismatch')
    add('invalid-rom-digest', base.replace(hashlib.sha256(mapping).hexdigest().encode(), b'0'*64), reason='map digest mismatch')
    for value in ('true', '1024.0', '1025', '0', '263168'):
        add('invalid-source-size-' + value.replace('.', '-'), base.replace(b'source_size = 1024', ('source_size = '+value).encode()), reason='invalid source size')
    add('invalid-rom-unknown-field', base+b'extra = 1\n', reason='unknown ROM key')
    add('invalid-rom-missing-field', base.replace(b'role = "firmware"\n', b''), reason='missing ROM role')
    add('invalid-v2-with-rom', base.replace(b'format = 3', b'format = 2', 1), reason='format 2 rejects ROM fields')

    def bad_map(name, change):
        value = rom_map()
        change(value)
        data = encode_map(value)
        add('invalid-map-'+name, manifest(data), data, 'invalid ROM map '+name)

    bad_map('format', lambda m: m.update(format=2))
    bad_map('boolean-format', lambda m: m.update(format=True))
    bad_map('device', lambda m: m.update(device='other'))
    bad_map('encoding', lambda m: m.update(encoding='other'))
    bad_map('base', lambda m: m.update(base_sha256='0'*64))
    bad_map('source-size', lambda m: m.update(source_size=2048))
    bad_map('unknown', lambda m: m.update(extra=1))
    bad_map('missing', lambda m: m.pop('encoding'))
    bad_map('blocks-empty', lambda m: m.update(blocks=[]))
    bad_map('blocks-null', lambda m: m.update(blocks=None))
    bad_map('block-unknown', lambda m: m['blocks'][0].update(extra=1))
    bad_map('bel-empty', lambda m: m['blocks'][0].update(bel=''))
    bad_map('bel-large', lambda m: m['blocks'][0].update(bel='x'*65))
    bad_map('offset', lambda m: m['blocks'][0].update(source_offset=1))
    bad_map('words', lambda m: m['blocks'][0]['word_bits'].pop())
    bad_map('word-null', lambda m: m['blocks'][0]['word_bits'].__setitem__(0, None))
    bad_map('word-length', lambda m: m['blocks'][0]['word_bits'][0].pop())
    for name, bit in [('duplicate-bit',32*7605+1),('low-bit',32*7605-1),('high-bit',7605*7024),('boolean-bit',True),('float-bit',243360.0)]:
        bad_map(name, lambda m, b=bit: m['blocks'][0]['word_bits'][0].__setitem__(0,b))
    for name, data in [('duplicate-key',mapping.replace(b'"format":1',b'"format":1,"format":1')),('trailing',mapping+b'{}'),('utf8',b'\xff'),('null',b'null')]:
        add('invalid-map-'+name, manifest(data), data, 'invalid ROM map '+name)
    for name in ('duplicate-bel', 'duplicate-source', 'cross-block-bit'):
        value = rom_map()
        value['source_size'] = 2048
        second = copy.deepcopy(value['blocks'][0])
        second['bel'] = 'M10K.005.074'
        second['source_offset'] = 1024
        second['word_bits'] = [[bit+10240 for bit in word] for word in second['word_bits']]
        value['blocks'].append(second)
        if name == 'duplicate-bel':
            second['bel'] = value['blocks'][0]['bel']
        elif name == 'duplicate-source':
            second['source_offset'] = 0
        else:
            second['word_bits'][0][0] = value['blocks'][0]['word_bits'][0][0]
        data = encode_map(value)
        man = manifest(data).replace(b'source_size = 1024', b'source_size = 2048')
        add('invalid-map-'+name, man, data, 'invalid ROM map '+name)
    files['cases.json'] = (json.dumps(cases, indent=2)+'\n').encode()
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    v2.FIXTURE_ROOT = FIXTURE_ROOT
    files = generated_files()
    return v2.check(files) if args.check else v2.write(files)


if __name__ == '__main__':
    raise SystemExit(main())
