"""Observe the contained START path without extra device operations or retries."""
import hashlib
from pathlib import Path

SOURCE = Path('plat/intel/soc/common/socfpga_sip_svc.c')
MODE = 'contained-configuration-start-results-v2'
INPUT_SHA256 = '08dffaecc57b285b65bf97febcb4d394768ed5e890d5fd62fa892c2744ab5b87'
MAILBOX = Path('plat/intel/soc/common/soc/socfpga_mailbox.c')
MAILBOX_INPUT_SHA256 = 'f6286fd8db7d2a2673824998d5811d784df2dcd9b64455921a35b0a75adbf4ec'
ISOLATION_LINE = '\tNOTICE("FES_NANO_ISOLATION status=%d\\n", ret);\n'
PHASES = ('configuration-before', 'bridge-precheck', 'vendor-entry',
          'bridge-disable', 'configuration-complete', 'cancel',
          'reconfigure-return', 'reconfigure-length', 'vendor-accepted',
          'vendor-return', 'configuration-before-after', 'configuration-after')
HELPER = '''static void fes_nano_start_note(unsigned phase, int status)
{
    NOTICE("FES_NANO_START phase=%u status=%d\\n", phase, status);
}

'''


def observed(source):
    text = source.decode()
    if 'fes_nano_start_note' in text:
        raise ValueError('configuration START observer is already installed')
    if hashlib.sha256(source).hexdigest() != INPUT_SHA256:
        raise ValueError('START observer requires the exact reviewed owned service')
    edits = []

    def replace(old, new):
        nonlocal text
        if text.count(old) != 1:
            raise ValueError('contained START source context changed')
        text = text.replace(old, new)
        edits.append((old, new))

    replace('static int intel_fpga_config_start(uint32_t flag)\n{',
            HELPER + 'static int intel_fpga_config_start(uint32_t flag)\n{')
    replace('\tunsigned int resp_len = ARRAY_SIZE(response);\n\n\t/* FES profile admits full configuration only. */',
            '\tunsigned int resp_len = ARRAY_SIZE(response);\n\n'
            '\tfes_nano_start_note(2U, fes_contained_transport_status);\n'
            '\t/* FES profile admits full configuration only. */')
    replace('\tif (socfpga_bridges_disable(~0U) != 0) {',
            '\tstatus = socfpga_bridges_disable(~0U);\n'
            '\tfes_nano_start_note(3U, status);\n\tif (status != 0) {')
    replace('\tstatus = mailbox_send_fpga_config_comp();\n\tif (status < 0) {',
            '\tstatus = mailbox_send_fpga_config_comp();\n'
            '\tfes_nano_start_note(4U, status);\n\tif (status < 0) {')
    replace('\tstatus = mailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_CANCEL, NULL, 0U,\n'
            '\t\t\tCMD_CASUAL, NULL, NULL);\n\tif (status < 0) {',
            '\tstatus = mailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_CANCEL, NULL, 0U,\n'
            '\t\t\tCMD_CASUAL, NULL, NULL);\n\tfes_nano_start_note(5U, status);\n'
            '\tif (status < 0) {')
    replace('\tstatus = mailbox_send_cmd(MBOX_JOB_ID, MBOX_RECONFIG, &argument, size,\n'
            '\t\t\tCMD_CASUAL, response, &resp_len);',
            '\tstatus = mailbox_send_cmd(MBOX_JOB_ID, MBOX_RECONFIG, &argument, size,\n'
            '\t\t\tCMD_CASUAL, response, &resp_len);\n'
            '\tfes_nano_start_note(6U, status);\n'
            '\tfes_nano_start_note(7U, (int)resp_len);')
    replace('\tbytes_per_block = response[1];',
            '\tbytes_per_block = response[1];\n\tfes_nano_start_note(8U, 0);')
    replace('    if (fes_nano_bridges_configuration_guard(&fes_nano_owned_bridges) != 0) {',
            '    status = fes_nano_bridges_configuration_guard(&fes_nano_owned_bridges);\n'
            '    if (fid == INTEL_SIP_SMC_FPGA_CONFIG_START) fes_nano_start_note(1U, status);\n'
            '    if (status != 0) {')
    replace('    status=fes_nano_configuration_before(configuration,fid,x1,x2,request_type != NO_REQUEST);',
            '    status=fes_nano_configuration_before(configuration,fid,x1,x2,request_type != NO_REQUEST);\n'
            '    if (fid == INTEL_SIP_SMC_FPGA_CONFIG_START) fes_nano_start_note(0U, status);')
    replace('        status = intel_fpga_config_start(0);',
            '        status = intel_fpga_config_start(0);\n'
            '        fes_nano_start_note(9U, status);')
    replace('    if(configuration_call)\n        fes_nano_configuration_after(configuration,fid,r);',
            '    if(configuration_call) {\n'
            '        if (fid == INTEL_SIP_SMC_FPGA_CONFIG_START) fes_nano_start_note(10U, (int)r[0]);\n'
            '        fes_nano_configuration_after(configuration,fid,r);\n'
            '        if (fid == INTEL_SIP_SMC_FPGA_CONFIG_START) fes_nano_start_note(11U, (int)r[0]);\n'
            '    }')
    # Each replacement is reversible. No original call, predicate or argument
    # disappears; only the two existing guard results gain a named local.
    restored = text
    for old, new in reversed(edits):
        if restored.count(new) != 1:
            raise ValueError('ambiguous START observer reversal')
        restored = restored.replace(new, old)
    if restored.encode() != source:
        raise ValueError('START observer changed its original source')
    return text.encode()


def observed_mailbox(source):
    if hashlib.sha256(source).hexdigest() != MAILBOX_INPUT_SHA256:
        raise ValueError('raw isolation capture requires the exact reviewed mailbox stage')
    text = source.decode()
    old = ('\tret = mailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_FPGA_CONFIG_COMP, NULL, 0U,\n'
           '\t\t\t\t CMD_CASUAL, NULL, NULL);\n')
    if text.count(old) != 1 or 'FES_NANO_ISOLATION' in text:
        raise ValueError('isolation result context changed')
    generated = text.replace(old, old+ISOLATION_LINE)
    if generated.replace(ISOLATION_LINE, '').encode() != source:
        raise ValueError('isolation capture changed more than its result record')
    return generated.encode()


def install(root, enabled, mailbox_binding=None):
    if enabled is not True:
        return None
    if (not isinstance(mailbox_binding, dict) or
            mailbox_binding.get('mode') != 'pinned-mailbox-v3-retired-status-v1' or
            mailbox_binding.get('generated_sha256') != MAILBOX_INPUT_SHA256):
        raise ValueError('START capture requires its reviewed mailbox binding')
    path = Path(root)/SOURCE
    original = path.read_bytes()
    generated = observed(original)
    mailbox = Path(root)/MAILBOX
    original_mailbox = mailbox.read_bytes()
    generated_mailbox = observed_mailbox(original_mailbox)
    # Validate both complete inputs before publishing either transformed file.
    path.write_bytes(generated)
    mailbox.write_bytes(generated_mailbox)
    return dict(mode=MODE, phases=list(PHASES), maximum_records_per_start=len(PHASES)+1,
                maximum_phase_records=len(PHASES), maximum_isolation_records=1,
                input_sha256=hashlib.sha256(original).hexdigest(),
                generated_sha256=hashlib.sha256(generated).hexdigest(),
                mailbox_input_sha256=hashlib.sha256(original_mailbox).hexdigest(),
                mailbox_generated_sha256=hashlib.sha256(generated_mailbox).hexdigest(),
                control_reads_added=0, control_writes_added=0, uart_output_added=True, retries_added=0,
                original_timeouts_preserved=True, hardware_authority=False)
