"""Specialize only the pinned scalar digest's compilation for cold configuration.

The algorithm, constants and memory accesses retain their C semantics. Three
explicit loop hints let GCC replace repeated uncached round-table/schedule
loads with constants and registers. No cache, SIMD or ownership state changes.
"""
import hashlib
import re
from pathlib import Path

SOURCE = Path('plat/intel/soc/common/lib/sha/sha.c')
HEADER = SOURCE.with_name('sha.h')
SOURCE_SHA256 = 'b0e8cb2095a77957741a70f62b42443eaa8e4dfd502aef87bdd3b16f62785d58'
HEADER_SHA256 = '76a51a6aba59f84f754d9b8102394ff12ce0cdad1fb89cec61130543b73f66fb'
FLAGS = '$(BUILD_PLAT)/bl31/sha.o: override CFLAGS += -O3\n'


def unrolled(source, header):
    if (hashlib.sha256(source).hexdigest() != SOURCE_SHA256 or
            hashlib.sha256(header).hexdigest() != HEADER_SHA256):
        raise ValueError('fixed Nano digest requires the exact reviewed scalar SHA source/header')
    text = source.decode()
    for old, count in (('\tfor (i = 0 ; i < 80; i += 8) {', 10),
                       ('\t\t\t\tfor (j = 0; j < 16; j++)\n', 16),
                       ('\t\t\t\tfor (j = 0; j < 16; j++) {', 16)):
        if text.count(old) != 1:
            raise ValueError('pinned Nano digest loop context changed')
        indent = old[:len(old) - len(old.lstrip('\t'))]
        text = text.replace(old, indent + '#pragma GCC unroll ' + str(count) + '\n' + old)
    # This guard also rejects an accidental algorithm or constant change.
    if re.sub(r'(?m)^\s*#pragma GCC unroll [0-9]+\n', '', text).encode() != source:
        raise ValueError('Nano digest transformation changed more than loop hints')
    return text.encode()


def install(source, enabled):
    if enabled is not True:
        return None
    path = source / SOURCE
    modified = unrolled(path.read_bytes(), (source / HEADER).read_bytes())
    path.write_bytes(modified)
    return {'mode': 'pinned-sha384-scalar-unroll-v1', 'reference_sha256': SOURCE_SHA256,
            'header_sha256': HEADER_SHA256, 'generated_sha256': hashlib.sha256(modified).hexdigest(),
            'flags': '-O3', 'loop_unroll': [10, 16, 16],
            'cache_state_changed': False, 'algorithm_changed': False}
