#!/usr/bin/env python3
"""Select a captured ZX81 or ZX80 diagnostic without changing the HPS envelope."""
from __future__ import annotations

import hashlib
import re

MODELS = ('zx81', 'zx80')
ROM_PATH = 'sources/misteross/cores/fes-zx81/rtl/'
# This captured MiSTer image contains the 8 KiB ZX81 ROM followed by the ZX80
# image. Do not apply its offsets to a different firmware layout.
COMBINED_SHA = {
    'zx8x.hex': '93afab88ef8f12e6848700f4824f4268f193651657d23acbd30f8e9bb131131b',
    'zx8x.mif': 'ad341356e97fa931be11064a8d5b0418dd8fdcaf51205a3a2ff1efedc8173dc3',
}


def checked(model):
    if not isinstance(model, str) or model not in MODELS:
        raise ValueError('unsupported captured computer model')
    return model


def parameters(model):
    return {'computer_model': checked(model)}


def macros(model):
    return ['ZX81_MODEL_ZX80=1'] if checked(model) == 'zx80' else []


def rom_images(model, sources):
    checked(model)
    original = {name: sources[ROM_PATH + name] for name in COMBINED_SHA}
    if model == 'zx81':
        return original
    if any(hashlib.sha256(data).hexdigest() != COMBINED_SHA[name]
           for name, data in original.items()):
        raise ValueError('ZX80 requires the reviewed captured combined ROM layout')
    words = original['zx8x.hex'].decode('ascii').splitlines()
    if len(words) != 16384 or any(not re.fullmatch('[0-9a-fA-F]{2}', word) for word in words):
        raise ValueError('captured combined ROM is not 16384 bytes')
    # The machine's ZX80 address decoder addresses the first 4 KiB. Install
    # that firmware there, rather than booting the first half of the ZX81 ROM.
    image = bytes(int(word, 16) for word in words[8192:12288]) + b'\xff' * 12288
    hex_file = ''.join(f'{byte:02x}\n' for byte in image).encode('ascii')
    mif_file = ('WIDTH=8;\nDEPTH=16384;\nADDRESS_RADIX=HEX;\nDATA_RADIX=HEX;\nCONTENT BEGIN\n' +
                ''.join(f'{address:04X}: {byte:02X};\n' for address, byte in enumerate(image)) +
                'END;\n').encode('ascii')
    return {'zx8x.hex': hex_file, 'zx8x.mif': mif_file}


def install(output, inputs, sources, model):
    """Install derived firmware and record the actual synthesis/model inputs."""
    checked(model)
    project = output / 'hps/project'
    qsf_path = project / 'golden_top.qsf'
    qsf = qsf_path.read_text()
    if re.search(r'\bZX81_MODEL_\w+', qsf):
        raise ValueError('captured project already selects a computer model')
    qsf += ''.join('set_global_assignment -name VERILOG_MACRO ' + macro + '\n'
                   for macro in macros(model))
    qsf_path.write_text(qsf)
    for name, data in rom_images(model, sources).items():
        (project / name).write_bytes(data)
        (project / 'fes_mailbox' / name).write_bytes(data)
        inputs['computer']['source_sha256'][name] = hashlib.sha256(data).hexdigest()
    inputs['computer_model'] = model


def verify(project, inputs, sources):
    """Check actual ROM bytes and model macros, including legacy ZX81 artifacts."""
    model = checked(inputs.get('computer_model', 'zx81'))
    qsf = (project / 'golden_top.qsf').read_text()
    actual = re.findall(r'\bZX81_MODEL_\w+(?:=\w+)?', qsf)
    if actual != macros(model):
        raise ValueError('project computer model macro differs')
    for name, data in rom_images(model, sources).items():
        if (inputs['computer']['source_sha256'].get(name) != hashlib.sha256(data).hexdigest() or
                any((project / directory / name).read_bytes() != data
                    for directory in ('', 'fes_mailbox'))):
            raise ValueError('project computer firmware differs: ' + name)
    return model
