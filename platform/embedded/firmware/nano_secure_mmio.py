"""Select exact secure control pages at the pinned BL31 MMIO access layer."""
import hashlib
from pathlib import Path

HERE = Path(__file__).resolve().parent
SOURCE = Path('include/lib/mmio.h')
HEADER = Path('include/lib/nano_secure_mmio.h')
INPUT_SHA256 = '68d2995232b8d8691bfb2c60755f0f3ef452a7b8e2e25f8ffe07b345f991c584'
SERVICE_SHA256 = 'f38684a6d8facdd212091a594f40c8b01551afaca37c3a09874e5c95b3ebc722'
MODE = 'sctlr-m-secure-control-mmio-v1'
SYSMGR_ALIAS, SYSMGR_PHYSICAL = 0xa0000, 0x10d12000
TCU_ALIAS, TCU_PHYSICAL = 0xb0000, 0x16008000


def selected_header(source):
    if hashlib.sha256(source).hexdigest() != INPUT_SHA256:
        raise ValueError('secure control MMIO requires the exact vendor accessors')
    text = source.decode()
    include = '#include <stdint.h>'
    if text.count(include) != 1:
        raise ValueError('MMIO include context differs')
    edits = [(include, include + '\n#include <lib/nano_secure_mmio.h>')]
    for width in (8, 16, 32, 64):
        edits += [(f'\t*(volatile uint{width}_t*)addr = value;',
                   f'\t*(volatile uint{width}_t*)fes_nano_secure_mmio_address(addr, 1U) = value;'),
                  (f'\treturn *(volatile uint{width}_t*)addr;',
                   f'\treturn *(volatile uint{width}_t*)fes_nano_secure_mmio_address(addr, 0U);')]
    for before, after in edits:
        if text.count(before) != 1:
            raise ValueError('MMIO accessor context differs')
        text = text.replace(before, after)
    restored = text
    for before, after in reversed(edits):
        if restored.count(after) != 1:
            raise ValueError('ambiguous secure MMIO source reversal')
        restored = restored.replace(after, before)
    if restored.encode() != source:
        raise ValueError('MMIO selection changed original operations')
    return text.encode()


def install(root, mmu_binding, start_binding):
    if mmu_binding is None:
        return None
    import nano_firmware_mmu as mmu
    import nano_start_trace as start
    if (not isinstance(mmu_binding, dict) or mmu_binding.get('contract') != mmu.RUNTIME_CONTRACT or
            not isinstance(mmu_binding.get('sources'), dict) or not mmu_binding['sources'] or
            not isinstance(start_binding, dict) or start_binding.get('mode') != start.MODE or
            start_binding.get('input_sha256') != start.INPUT_SHA256 or
            start_binding.get('generated_sha256') != SERVICE_SHA256):
        raise ValueError('secure control MMIO requires complete MMU and START bindings')
    root = Path(root)
    mmu.verify_installed(root, mmu_binding)
    if (mmu._fixed('ocram', SYSMGR_ALIAS // mmu.PAGE) != (SYSMGR_PHYSICAL, mmu.DEVICE | mmu.XN, 3) or
            mmu._fixed('ocram', TCU_ALIAS // mmu.PAGE) != (TCU_PHYSICAL, mmu.DEVICE | mmu.RO | mmu.XN, 3)):
        raise ValueError('secure control aliases differ from selected tables')
    if hashlib.sha256((root / start.SOURCE).read_bytes()).hexdigest() != start_binding.get('generated_sha256'):
        raise ValueError('START service changed before MMIO selection')
    generated = selected_header((root / SOURCE).read_bytes())
    header = (HERE / 'nano_secure_mmio.h').read_bytes()
    if (root / HEADER).exists():
        raise ValueError('secure MMIO helper already installed')
    (root / HEADER).write_bytes(header)
    (root / SOURCE).write_bytes(generated)
    sources = {str(SOURCE): hashlib.sha256(generated).hexdigest(),
               str(HEADER): hashlib.sha256(header).hexdigest()}
    mmu_binding['sources'].update(sources)
    return dict(mode=MODE, input_sha256=INPUT_SHA256, sources=sources,
                sysmgr=dict(physical=SYSMGR_PHYSICAL, alias=SYSMGR_ALIAS, read=True, write=True),
                tcu_secure_bank=dict(physical=TCU_PHYSICAL, alias=TCU_ALIAS, read=True, write=False),
                page_bytes=4096, widths=[8, 16, 32, 64], register='SCTLR_EL3.M',
                control_reads_added=0, control_writes_added=0, retries_added=0,
                unrelated_addresses_unchanged=True, physical_access_before_mmu=True,
                hardware_authority=False)
