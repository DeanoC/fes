"""Observe the contained START path without extra device operations or retries."""
import hashlib
import re
from pathlib import Path

SOURCE = Path('plat/intel/soc/common/socfpga_sip_svc.c')
MODE = 'contained-configuration-start-results-v3'
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


# Private observational state: active only around the original synchronous call.
# No value from this state controls the mailbox or supplies runtime authority.
ISOLATION_STATE = """struct fes_nano_isolation_observation {
    unsigned active, polling, branch, polls;
    int fill;
    uint32_t cin, cout, bell, header;
};
static struct fes_nano_isolation_observation fes_nano_isolation_io;

"""
ISOLATION_IO_LINE = ('\tNOTICE("FES_NANO_ISOLATION_IO fill=%d branch=%u cin=%x cout=%x bell=%x polls=%u header=%x\\n",\n'
                     '\t       fes_nano_isolation_io.fill, fes_nano_isolation_io.branch,\n'
                     '\t       fes_nano_isolation_io.cin, fes_nano_isolation_io.cout,\n'
                     '\t       fes_nano_isolation_io.bell, fes_nano_isolation_io.polls,\n'
                     '\t       fes_nano_isolation_io.header);\n')


def observed_mailbox(source):
    if hashlib.sha256(source).hexdigest() != MAILBOX_INPUT_SHA256:
        raise ValueError('raw isolation capture requires the exact reviewed mailbox stage')
    text = source.decode()
    if 'FES_NANO_ISOLATION' in text:
        raise ValueError('isolation observer is already installed')
    edits = []

    def replace(old, new, poll=False):
        nonlocal text
        start, end = 0, len(text)
        if poll:
            matches = list(re.finditer(r'(?m)^static int mailbox_poll_response_v3\([^;{}]*\)\n\{', text))
            if len(matches) != 1:
                raise ValueError('missing unique V3 poll definition')
            start = matches[0].start()
            end = text.index('\n/* SDM response parser handler state machine. */', start)
        selected = text[start:end]
        if selected.count(old) != 1:
            raise ValueError('isolation transport source context changed')
        text = text[:start]+selected.replace(old, new)+text[end:]
        edits.append((old, new))

    replace('static bool is_mailbox_cmdbuf_full(uint32_t cin)\n{',
            ISOLATION_STATE+'static bool is_mailbox_cmdbuf_full(uint32_t cin)\n{')
    old = ('\tsdm_read_offset = mmio_read_32(MBOX_OFFSET + MBOX_COUT);\n')
    replace(old, old+'\tif (fes_nano_isolation_io.active && MBOX_CMD_MASK(header_cmd) == MBOX_CMD_FPGA_CONFIG_COMP) {\n'
            '\t\tfes_nano_isolation_io.cin = cmd_free_offset;\n'
            '\t\tfes_nano_isolation_io.cout = sdm_read_offset;\n\t}\n')
    old = '\tif (status != 0) {\n\t\treturn status;\n\t}\n\n\tstatus = mailbox_poll_response(job_id, urgent, response, resp_len);'
    replace(old, '\tif (fes_nano_isolation_io.active && cmd == MBOX_CMD_FPGA_CONFIG_COMP)\n'
            '\t\tfes_nano_isolation_io.fill = status;\n'+old.replace(
                '\tstatus = mailbox_poll_response(job_id, urgent, response, resp_len);',
                '\tif (fes_nano_isolation_io.active && cmd == MBOX_CMD_FPGA_CONFIG_COMP)\n'
                '\t\tfes_nano_isolation_io.polling = 1U;\n'
                '\tstatus = mailbox_poll_response(job_id, urgent, response, resp_len);\n'
                '\tif (fes_nano_isolation_io.active && cmd == MBOX_CMD_FPGA_CONFIG_COMP)\n'
                '\t\tfes_nano_isolation_io.polling = 0U;'))
    # This multi-line context is unique to the V3 synchronous poll definition.
    old = ('\t\t\tif (mmio_read_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM)\n'
           '\t\t\t\t== 1U) {\n\t\t\t\tbreak;\n\t\t\t}\n\t\t\tmdelay(10U);')
    replace(old, '\t\t\tuint32_t bell = mmio_read_32(MBOX_OFFSET + MBOX_DOORBELL_FROM_SDM);\n'
            '\t\t\tif (fes_nano_isolation_io.polling) {\n'
            '\t\t\t\tfes_nano_isolation_io.bell = bell;\n'
            '\t\t\t\tfes_nano_isolation_io.polls++;\n\t\t\t}\n'
            '\t\t\tif (bell == 1U) {\n\t\t\t\tbreak;\n\t\t\t}\n\t\t\tmdelay(10U);', poll=True)
    old = '\t\t\tINFO("%s: Timed out waiting for SDM intr\\n", __func__);'
    replace(old, '\t\t\tif (fes_nano_isolation_io.polling) fes_nano_isolation_io.branch = 1U;\n'+old, poll=True)
    old = ('\t\t\tif (mailbox_fill_cmd_desc(client_id, job_id, resp) != MBOX_RET_OK)\n'
           '\t\t\t\treturn MBOX_BUFFER_FULL;')
    replace(old, '\t\t\tif (mailbox_fill_cmd_desc(client_id, job_id, resp) != MBOX_RET_OK) {\n'
            '\t\t\t\tif (fes_nano_isolation_io.polling) fes_nano_isolation_io.branch = 2U;\n'
            '\t\t\t\treturn MBOX_BUFFER_FULL;\n\t\t\t}', poll=True)
    old = '\t\t\tuint16_t header = resp_desc->header;\n'
    replace(old, old+'\t\t\tif (fes_nano_isolation_io.polling) {\n'
            '\t\t\t\tfes_nano_isolation_io.branch = 3U;\n'
            '\t\t\t\tfes_nano_isolation_io.header = header;\n\t\t\t}\n')
    old = '\tINFO("%s: Timed out waiting for SDM\\n", __func__);'
    replace(old, '\tif (fes_nano_isolation_io.polling && !fes_nano_isolation_io.branch)\n'
            '\t\tfes_nano_isolation_io.branch = 4U;\n'+old, poll=True)
    old = ('\tret = mailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_FPGA_CONFIG_COMP, NULL, 0U,\n'
           '\t\t\t\t CMD_CASUAL, NULL, NULL);\n')
    replace(old, '\tfes_nano_isolation_io = (struct fes_nano_isolation_observation){.active = 1U};\n'+old+
            '\tfes_nano_isolation_io.active = 0U;\n'+ISOLATION_IO_LINE+ISOLATION_LINE)
    restored = text
    for old, new in reversed(edits):
        if restored.count(new) != 1:
            raise ValueError('ambiguous isolation observer reversal')
        restored = restored.replace(new, old)
    if restored.encode() != source:
        raise ValueError('isolation observer changed its original source')
    return text.encode()


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
    return dict(mode=MODE, phases=list(PHASES), maximum_records_per_start=len(PHASES)+2,
                maximum_phase_records=len(PHASES), maximum_isolation_records=1, maximum_isolation_io_records=1,
                isolation_io_branches=['fill-failure', 'doorbell-timeout', 'descriptor-full', 'response', 'loop-limit'],
                input_sha256=hashlib.sha256(original).hexdigest(),
                generated_sha256=hashlib.sha256(generated).hexdigest(),
                mailbox_input_sha256=hashlib.sha256(original_mailbox).hexdigest(),
                mailbox_generated_sha256=hashlib.sha256(generated_mailbox).hexdigest(),
                control_reads_added=0, control_writes_added=0, uart_output_added=True, retries_added=0,
                original_timeouts_preserved=True, hardware_authority=False)
