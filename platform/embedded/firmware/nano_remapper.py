"""Select the exact secure System Manager page for existing remapper accesses."""
import hashlib
from pathlib import Path

SOURCE = Path('plat/intel/soc/common/socfpga_sip_svc.c')
INPUT_SHA256 = 'f38684a6d8facdd212091a594f40c8b01551afaca37c3a09874e5c95b3ebc722'
MODE = 'sctlr-m-secure-sdm-remapper-v1'
ALIAS, PHYSICAL = 0xa0000, 0x10d12000
REGISTERS = {'BOOT_SCRATCH_POR_1': 0x25c,
             'SDM_BE_ARADDR_REMAP': 0x284, 'SDM_BE_AWADDR_REMAP': 0x280}
ANCHOR = 'void intel_smmu_hps_remapper_init(uint64_t *mem)\n{'
HELPER = '''static inline uintptr_t fes_nano_sdm_remapper_address(uint32_t offset)
{
    return ((read_sctlr_el3() & 1U) ? 0xa0000UL : 0x10d12000UL) + offset;
}

'''


def secure_source(source):
    if hashlib.sha256(source).hexdigest() != INPUT_SHA256:
        raise ValueError('secure remapper requires the exact reviewed START service')
    text = source.decode()
    if text.count(ANCHOR) != 1:
        raise ValueError('remapper function context differs')
    begin = text.index(ANCHOR)
    end = text.index('\n}', begin) + 2
    original = text[begin:end]
    changed = original
    for name in REGISTERS:
        old = 'SOCFPGA_SYSMGR(' + name + ')'
        if changed.count(old) != 1:
            raise ValueError('remapper register access differs: ' + name)
        changed = changed.replace(old, 'fes_nano_sdm_remapper_address(SOCFPGA_SYSMGR_' + name + ')')
    generated = text[:begin] + HELPER + changed + text[end:]
    restored = generated.replace(HELPER + changed, original)
    if restored.encode() != source:
        raise ValueError('secure remapper changed more than its three addresses')
    return generated.encode()


def install(root, mmu_binding, start_binding):
    if mmu_binding is None:
        return None
    import nano_firmware_mmu as mmu
    import nano_start_trace as start
    if (not isinstance(mmu_binding, dict) or mmu_binding.get('contract') != mmu.RUNTIME_CONTRACT or
            not isinstance(mmu_binding.get('sources'), dict) or not mmu_binding['sources'] or
            not isinstance(start_binding, dict) or start_binding.get('mode') != start.MODE or
            start_binding.get('input_sha256') != start.INPUT_SHA256 or
            start_binding.get('generated_sha256') != INPUT_SHA256):
        raise ValueError('secure remapper requires complete MMU and START bindings')
    root = Path(root)
    mmu.verify_installed(root, mmu_binding)
    if mmu._fixed('ocram', ALIAS // mmu.PAGE) != (PHYSICAL, mmu.DEVICE | mmu.XN, 3):
        raise ValueError('secure remapper page differs from selected tables')
    path = root / SOURCE
    original = path.read_bytes()
    generated = secure_source(original)
    path.write_bytes(generated)
    return dict(mode=MODE, input_sha256=INPUT_SHA256,
                generated_sha256=hashlib.sha256(generated).hexdigest(),
                mmu_active_base=ALIAS, physical_base=PHYSICAL, page_bytes=4096,
                register='SCTLR_EL3.M', offsets=REGISTERS,
                control_reads_added=0, control_writes_added=0,
                original_remapper_values_and_pointer_translation_preserved=True,
                retries_added=0, hardware_authority=False)
