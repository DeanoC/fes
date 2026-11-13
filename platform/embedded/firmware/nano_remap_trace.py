"""Bounded raw SDM/remapper observations; no translation or DMA authority."""
import hashlib
from pathlib import Path

SOURCE = Path('plat/intel/soc/common/socfpga_sip_svc.c')
INPUT_SHA256 = '7daf7776717dab43e0d5887ba440fa8dbc45bad55d8d764e8abf344ef403ef1a'
CONTRACT = dict(format=1, scope='nano-first-start-write-submit-remap-v1',
                remapper_calls=2, register_reads=25, lines=6, attempts=1,
                register_writes=False, SDM_route_authority=False,
                DMA_authority=False)
HELPER = r'''/* Selected diagnostic only. Existing secure configuration lock scopes
 * these observations. Neither their contents nor their one-use flags control
 * any original operation. Samples are raw, not an atomic routing proof. */
static unsigned fes_nano_remap_seen, fes_nano_sdm_seen;
static void fes_nano_remap_note(unsigned kind, unsigned phase, uint64_t operand)
{
    uint32_t por1 = mmio_read_32(SOCFPGA_SYSMGR(BOOT_SCRATCH_POR_1));
    uint32_t ctrl = mmio_read_32(SOCFPGA_SYSMGR(SDM_TBU_STREAM_CTRL_REG_1_SDM));
    uint32_t ids = mmio_read_32(SOCFPGA_SYSMGR(SDM_TBU_STREAM_ID_AX_REG_1_SDM));
    uint32_t ar = mmio_read_32(SOCFPGA_SYSMGR(SDM_BE_ARADDR_REMAP));
    uint32_t aw = mmio_read_32(SOCFPGA_SYSMGR(SDM_BE_AWADDR_REMAP));
    NOTICE("FES_NANO_REMAP kind=%u phase=%u operand=%llx por1=%x bypass=%x ctrl=%x ids=%x ar=%x aw=%x access=unqualified\n",
           kind, phase, (unsigned long long)operand, por1, g_remapper_bypass,
           ctrl, ids, ar, aw);
}
static unsigned fes_nano_sdm_note(uint32_t address, uint32_t bytes)
{
    if (fes_nano_sdm_seen) return 0U;
    fes_nano_sdm_seen = 1U;
    uint32_t por1 = mmio_read_32(SOCFPGA_SYSMGR(BOOT_SCRATCH_POR_1));
    uint32_t ctrl = mmio_read_32(SOCFPGA_SYSMGR(SDM_TBU_STREAM_CTRL_REG_1_SDM));
    uint32_t ids = mmio_read_32(SOCFPGA_SYSMGR(SDM_TBU_STREAM_ID_AX_REG_1_SDM));
    uint32_t ar = mmio_read_32(SOCFPGA_SYSMGR(SDM_BE_ARADDR_REMAP));
    uint32_t aw = mmio_read_32(SOCFPGA_SYSMGR(SDM_BE_AWADDR_REMAP));
    NOTICE("FES_NANO_SDM_SUBMIT phase=before address=%x bytes=%x por1=%x bypass=%x ctrl=%x ids=%x ar=%x aw=%x access=unqualified\n",
           address, bytes, por1, g_remapper_bypass, ctrl, ids, ar, aw);
    return 1U;
}

'''


def observed(source):
    if hashlib.sha256(source).hexdigest() != INPUT_SHA256:
        raise ValueError('remap observation requires the exact reviewed START/MMIO service')
    text = source.decode()
    edits = []

    def replace(old, new):
        nonlocal text
        if text.count(old) != 1:
            raise ValueError('remap observer source context changed')
        text = text.replace(old, new)
        edits.append((old, new))

    marker = 'static int intel_fpga_sdm_write_buffer(struct fpga_config_info *buffer)\n'
    replace(marker, HELPER + marker)
    old = '\t\tint status = mailbox_send_cmd_async(&send_id, MBOX_RECONFIG_DATA, args,\n\t\t\t\t\t3U, CMD_INDIRECT);\n'
    replace(old, '\t\tunsigned observed = fes_nano_sdm_note(args[1], args[2]);\n' + old +
            '\t\tif (observed) NOTICE("FES_NANO_SDM_RESULT status=%d\\n", status);\n')
    old = 'void intel_smmu_hps_remapper_init(uint64_t *mem)\n{\n'
    replace(old, old + '\tunsigned trace = mem != NULL ? 2U : 1U;\n'
            '\tif (fes_nano_remap_seen & trace) trace = 0U;\n'
            '\telse fes_nano_remap_seen |= trace;\n'
            '\tif (trace) fes_nano_remap_note(trace, 0U, mem != NULL ? *mem : 0U);\n')
    old = '\tif (mem != NULL) {\n\t\t*mem = *mem - DRAM_BASE;\n\t}\n}\n'
    replace(old, old[:-2] + '\tif (trace) fes_nano_remap_note(trace, 1U, mem != NULL ? *mem : 0U);\n}\n')
    restored = text
    for old, new in reversed(edits):
        if restored.count(new) != 1:
            raise ValueError('ambiguous remap observation reversal')
        restored = restored.replace(new, old)
    if restored.encode() != source:
        raise ValueError('remap observer changed original operations')
    return text.encode()


def install(root, enabled, start):
    if type(enabled) is not bool:
        raise ValueError('explicit boolean remap observation selection required')
    if not enabled:
        return None
    path = Path(root) / SOURCE
    if not isinstance(start, dict) or start.get('generated_sha256') != INPUT_SHA256:
        raise ValueError('matched START/MMIO observation binding required')
    data = observed(path.read_bytes())
    path.write_bytes(data)
    result = dict(contract=dict(CONTRACT), input_sha256=INPUT_SHA256,
                  generated_sha256=hashlib.sha256(data).hexdigest())
    # Keep both transformations explicit. The secure-MMIO selector checked the
    # original reviewed START stage before this additive selected observer.
    start['pre_remap_generated_sha256'] = start['generated_sha256']
    start['generated_sha256'] = result['generated_sha256']
    start['remap_observation'] = result
    return result
