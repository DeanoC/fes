"""Exact live LMB observations and allocations owned by one recovery session."""
import copy
from fit_layout import FIT_MAX_BYTES, PACKAGE_MAX_BYTES, UPLOAD_BASE, UPLOAD_READBACK, UPLOAD_END
import re

START, END = UPLOAD_BASE, UPLOAD_END
FIT_UPLOAD_WINDOWS = ((UPLOAD_BASE, FIT_MAX_BYTES), (UPLOAD_READBACK, FIT_MAX_BYTES))
# Readback/repair controllers keep a small source and metadata concurrently.
# Their separate contract splits the first buffer; a full FIT still uses the
# independent 16 MiB readback buffer. This is never the upload default.
DIAGNOSTIC_READBACK_WINDOWS = ((UPLOAD_BASE, PACKAGE_MAX_BYTES),
                             (UPLOAD_BASE + PACKAGE_MAX_BYTES, PACKAGE_MAX_BYTES),
                             (UPLOAD_READBACK, FIT_MAX_BYTES))
MAX_REGIONS = 64
NUMBER = r'(?:0x[0-9a-fA-F]+|0)'


def memory_map(info):
    """Require the complete pinned U-Boot map, including lengths and flags."""
    banks = re.findall(r'-> start\s*=\s*('+NUMBER+r')\s+-> size\s*=\s*('+NUMBER+r')', info)
    reloc = re.findall(r'(?m)^\s*relocaddr\s*=\s*('+NUMBER+r')\s*\r?$', info)
    if ([(int(a, 16), int(n, 16)) for a, n in banks] != [(0x80000000, 0x40000000)] or
            len(re.findall(r'-> start\s*=', info)) != 1 or
            len(re.findall(r'-> size\s*=', info)) != 1 or
            len(reloc) != 1 or not 0x85000000 < int(reloc[0], 16) < 0xc0000000):
        raise ValueError('scratch RAM is not qualified by live memory map')
    result = {'banks': [[int(a, 16), int(n, 16)] for a, n in banks],
              'relocation': int(reloc[0], 16)}
    for kind in ('memory', 'reserved'):
        count = re.findall(r'(?m)^\s*'+kind+r'\.count\s*=\s*('+NUMBER+r')\s*\r?$', info)
        rows = re.findall(r'(?m)^\s*'+kind+r'\[(\d+)\]\s+\[('+NUMBER+r')-('+NUMBER+
                          r')\],\s*('+NUMBER+r') bytes, flags: ([^\r\n]+)\r?$', info)
        # A malformed/extra row must not disappear merely because it failed the regex.
        declarations = re.findall(r'(?m)^\s*'+kind+r'\[', info)
        if (len(count) != 1 or not 0 < int(count[0], 16) <= MAX_REGIONS or
                len(rows) != int(count[0], 16) or len(rows) != len(declarations)):
            raise ValueError('incomplete live LMB '+kind+' inventory')
        regions = []
        for index, first, last, length, flags in rows:
            start, end, size = (int(value, 16) for value in (first, last, length))
            names = flags.strip().split(', ')
            if (int(index) != len(regions) or not 0 <= start <= end < 1 << 64 or
                    size != end-start+1 or len(set(names)) != len(names) or
                    (names != ['none'] and (not set(names) <= {'no-map', 'no-overwrite', 'no-notify'})) or
                    (regions and start <= regions[-1][1])):
                raise ValueError('invalid live LMB '+kind+' region')
            regions.append([start, end, flags.strip()])
        result[kind] = regions
    if result['memory'] != [[0x80000000, 0xbfffffff, 'none']]:
        raise ValueError('LMB memory differs from qualified DRAM bank')
    return result


class ScratchMemory:
    """An in-memory ledger; observations never confer ownership of new ranges."""
    def __init__(self, *, windows=FIT_UPLOAD_WINDOWS):
        if windows not in (FIT_UPLOAD_WINDOWS, DIAGNOSTIC_READBACK_WINDOWS):
            raise ValueError('undeclared scratch memory layout')
        self.windows = dict(windows)
        self.baseline = None
        self.owned = []

    def observe(self, info):
        actual = memory_map(info)
        if self.baseline is None:
            if any(first < END and last >= START for first, last, _ in actual['reserved']):
                raise ValueError('scratch RAM overlaps an unowned reservation')
            self.baseline = copy.deepcopy(actual)
        else:
            expected = dict(self.baseline, reserved=sorted(self.baseline['reserved']+self.owned))
            if actual != expected:
                raise ValueError('live LMB differs from this session and its owned loads')
        return actual

    def loaded(self, address, size):
        if (self.baseline is None or type(size) is not int or
                not 0 < size <= self.windows.get(address, 0)):
            raise ValueError('load is outside this session scratch contract')
        first, last = address, address+size-1
        remaining = []
        for low, high, flags in self.owned:
            if high+1 < first or last+1 < low:
                remaining.append([low, high, flags])
            else:
                first, last = min(first, low), max(last, high)
        self.owned = sorted(remaining+[[first, last, 'none']])
