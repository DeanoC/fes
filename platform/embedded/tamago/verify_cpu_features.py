#!/usr/bin/env python3
"""Test pinned EL1 CPU discovery and real crypto dispatch in QEMU only.

The masked case removes advertised SHA2 support for testing. The emulated CPU
still implements those instructions; this is not physical ISA qualification.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import uuid

from cpu_features import prepare, sha
from host_support import Guest
from run import objects

APP = Path(__file__).resolve().parent
ROOT = APP.parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--compiler', required=True, type=Path)
    parser.add_argument('--qemu', required=True, type=Path)
    parser.add_argument('--module-cache', type=Path, default=ROOT/'out/embedded-base-tools/tamago/mod')
    parser.add_argument('--output', type=Path, default=ROOT/'out'/('native-cpu-features-'+uuid.uuid4().hex))
    args = parser.parse_args()
    output = args.output.absolute()
    if ('..' in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or
            not output.is_relative_to(ROOT/'out') or output.exists()):
        parser.error('output must be a fresh private directory beneath out/')
    output.mkdir(parents=True)
    compiler, qemu = args.compiler.resolve(strict=True), args.qemu.resolve(strict=True)
    env = dict(os.environ, GOOS='tamago', GOARCH='arm64', CGO_ENABLED='0',
               GOOSPKG='github.com/usbarmory/tamago', GOTOOLCHAIN='local', GOENV='off',
               GOWORK='off', GOPROXY='off', GOSUMDB='off', GOMODCACHE=str(args.module_cache.resolve()),
               GOCACHE=str(ROOT/'out/embedded-base-tools/tamago/cache'))
    test_inputs = {p: sha(p.read_bytes()) for p in [Path(__file__), APP/'cpu_features.py',
        APP/'host_support.py', APP/'run.py', APP/'cmd/probe/main.go', APP/'toolchain.lock.toml']}
    base = (APP/'cmd/probe/main.go').read_text()
    assert base.count('fmt.Printf("FES_NATIVE_GO_PROBE PASS') == 1
    vectors = []
    for size in [0, 1, 55, 56, 63, 64, 65, 127, 128, 129, 1 << 20, 8 << 20]:
        pattern = bytes((i*31) % 256 for i in range(256))
        data = (pattern*((size+255)//256))[:size]
        vectors.append(dict(size=size, sha256=hashlib.sha256(data).hexdigest(),
                            sha384=hashlib.sha384(data).hexdigest(), sha1=hashlib.sha1(data).hexdigest()))
    rows = ['{%d, "%s", "%s", "%s"},' % (v['size'], v['sha256'], v['sha384'], v['sha1']) for v in vectors]
    check = '''
    cases := []struct { size int; sha256, sha384, sha1 string }{
''' + '\n'.join(rows) + '''
    }
    for _, c := range cases {
        data := make([]byte,c.size)
        for i := range data { data[i]=byte(i*31) }
        if fmt.Sprintf("%x",sha256.Sum256(data))!=c.sha256 || fmt.Sprintf("%x",sha512.Sum384(data))!=c.sha384 || fmt.Sprintf("%x",sha1.Sum(data))!=c.sha1 { panic("hash known vector differs") }
        h:=sha256.New()
        for pos:=0;pos<len(data); { n:=min(12345,len(data)-pos);h.Write(data[pos:pos+n]);pos+=n }
        if fmt.Sprintf("%x",h.Sum(nil))!=c.sha256 { panic("chunked hash known vector differs") }
    }
    fmt.Printf("FES_CPU_HASH_PROBE PASS cases=%d\\n",len(cases))
    plaintext := []byte{0x00,0x11,0x22,0x33,0x44,0x55,0x66,0x77,0x88,0x99,0xaa,0xbb,0xcc,0xdd,0xee,0xff}
    for i, expected := range []string{"69c4e0d86a7b0430d8cdb78070b4c55a", "8ea2b7ca516745bfeafc49904b496089"} {
        key:=make([]byte,16+16*i)
        for n:=range key { key[n]=byte(n) }
        block,err:=aes.NewCipher(key)
        if err!=nil { panic(err) }
        ciphertext:=make([]byte,16);decoded:=make([]byte,16)
        block.Encrypt(ciphertext,plaintext);block.Decrypt(decoded,ciphertext)
        if fmt.Sprintf("%x",ciphertext)!=expected || !bytes.Equal(decoded,plaintext) { panic("AES known vector differs") }
    }
    block,err:=aes.NewCipher(make([]byte,16))
    if err!=nil { panic(err) }
    aead,err:=cipher.NewGCM(block)
    if err!=nil { panic(err) }
    encrypted:=aead.Seal(nil,make([]byte,12),make([]byte,16),nil)
    if fmt.Sprintf("%x",encrypted)!="0388dace60b6a392f328c2b971b2fe78ab6e47d42cec13bdf53a67b21257bddf" { panic("GCM known vector differs") }
    decoded,err:=aead.Open(nil,make([]byte,12),encrypted,nil)
    if err!=nil || !bytes.Equal(decoded,make([]byte,16)) { panic("GCM round trip differs") }
    encrypted[len(encrypted)-1]^=1
    if _,err=aead.Open(nil,make([]byte,12),encrypted,nil);err==nil { panic("GCM accepted corrupt tag") }
    if crc32.ChecksumIEEE([]byte("123456789"))!=0xcbf43926 || crc32.Checksum([]byte("123456789"),crc32.MakeTable(crc32.Castagnoli))!=0xe3069283 { panic("CRC32 known vector differs") }
    fmt.Println("FES_CPU_CRYPTO_PROBE PASS AES128 AES256 GCM-tag CRC32")
'''
    probe_main = output/'probe-main.go'
    probe_main.write_text(base.replace('"bytes"', '"bytes"\n "crypto/aes"\n "crypto/cipher"\n "crypto/sha1"\n "crypto/sha256"\n "crypto/sha512"\n "hash/crc32"').replace(
        '\tfmt.Printf("FES_NATIVE_GO_PROBE PASS', check+'\n\tfmt.Printf("FES_NATIVE_GO_PROBE PASS'))
    commands, results, original_sources, selected_sources = [], [], {}, {}

    def run(mode, label, argv):
        argv = [str(x) for x in argv]
        commands.append(dict(mode=mode, label=label, argv=argv))
        ret = subprocess.run(argv, cwd=APP, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
        (output/(mode+'-'+label+'.log')).write_bytes(ret.stdout)
        if ret.returncode:
            raise RuntimeError(f'{mode}/{label} exited {ret.returncode}')
        return ret.stdout.decode()

    for mode, expected in [('baseline', 0), ('discovered', 1), ('sha2-masked', 0)]:
        directory = output/mode
        directory.mkdir()
        replacements = {str(APP/'cmd/probe/main.go'): str(probe_main)}
        if mode != 'baseline':
            patches, _, feature_inputs = prepare(compiler, directory, env)
            replacements.update(patches)
            for path in feature_inputs:
                if path.name != 'cpu-arm64-features.go':
                    original_sources[str(path)] = sha(path.read_bytes())
            if mode == 'sha2-masked':
                path = directory/'cpu-arm64-features.go'
                path.write_text(path.read_text().replace(
                    'getisar0(), getpfr0()', 'getisar0() &^ (0xf << 12), getpfr0()'))
        overlay = directory/'overlay.json'
        overlay.write_text(json.dumps({'Replace': replacements}, indent=2)+'\n')
        elf = directory/'probe.elf'
        options = ['-mod=readonly', '-overlay='+str(overlay)]
        dependencies = run(mode, 'dependencies', [compiler, 'list', '-deps', '-json', *options, './cmd/probe'])
        for package in objects(dependencies):
            for name in package.get('GoFiles', [])+package.get('SFiles', [])+package.get('EmbedFiles', []):
                logical = Path(package['Dir'])/name
                path = Path(replacements.get(str(logical), str(logical)))
                selected_sources[str(path)] = sha(path.read_bytes())
        run(mode, 'build', [compiler, 'build', *options, '-trimpath', '-ldflags', '-T 0x40020000 -R 0x1000', '-o', elf, './cmd/probe'])
        symbols = run(mode, 'symbols', [compiler, 'tool', 'nm', elf])
        addresses = {}
        for line in symbols.splitlines():
            values = line.split()
            if len(values) == 3 and values[2] in ('crypto/internal/fips140/sha256.useSHA2', 'crypto/internal/fips140deps/cpu.ARM64HasSHA2'):
                addresses[values[2]] = int(values[0], 16)
        assert len(addresses) == 2, addresses
        with Guest(qemu, elf, directory/'boot', lambda argv: commands.append(dict(mode=mode, label='qemu', argv=argv))) as guest:
            text = guest.wait(lambda text: 'FES_NATIVE_GO_PROBE PASS' in text, 40, 'CPU hash correctness')
            assert 'EL=1 ' in text and 'FES_CPU_HASH_PROBE PASS cases=12' in text
            assert 'FES_CPU_CRYPTO_PROBE PASS AES128 AES256 GCM-tag CRC32' in text
            observations = {}
            for symbol, address in addresses.items():
                line = guest.execute('human-monitor-command', {'command-line': f'xp /1bx {address:#x}'})
                value = int(re.search(r':\s+0x([0-9a-f]+)', line)[1], 16)
                assert value == expected, (mode, symbol, line)
                observations[symbol] = dict(address=hex(address), value=value)
            results.append(dict(mode=mode, elf_sha256=sha(elf.read_bytes()),
                console_sha256=sha((directory/'boot/console.log').read_bytes()), hash_vectors=12,
                sha256_dispatch=observations, classification='QEMU EL1 runtime and known vectors; masked support is a test input'))
        assert all(sha(Path(p).read_bytes()) == h for p, h in original_sources.items())
        print('PASS', mode, 'known hashes and live SHA2 dispatch', expected, flush=True)
    assert all(sha(p.read_bytes()) == h for p, h in test_inputs.items()), 'test source changed'
    assert all(sha(Path(p).read_bytes()) == h for p, h in selected_sources.items()), 'selected source changed'
    result = dict(status='passed', hardware_actions=False,
        source_sha256={str(p.relative_to(ROOT)): h for p, h in test_inputs.items()},
        selected_source_sha256=selected_sources, compiler_sha256=sha(compiler.read_bytes()),
        qemu_sha256=sha(qemu.read_bytes()), original_toolchain_files_unchanged=original_sources,
        known_vectors=vectors, additional_checks=['AES-128', 'AES-256', 'AES-GCM known vector and corrupt tag', 'CRC32 IEEE and Castagnoli'], results=results, commands=commands,
        physical_performance_qualified=False, goal_complete=False)
    (output/'result.json').write_text(json.dumps(result, indent=2)+'\n')
    print('PASS baseline/discovered/masked native QEMU runtime; no hardware operations:', output, flush=True)


if __name__ == '__main__':
    main()
