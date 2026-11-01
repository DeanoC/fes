"""Prepare the explicit, unadmitted Linux SMMU profile in a private project."""
import hashlib
from pathlib import Path
import re

HPS_IP = 'hps_subsys/ip/hps_subsys/agilex_hps.ip'
ADAPTER = 'custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl'
MANAGED_FABRIC = 'managed-scanout-initiators-v1'
MANAGED_MASTERS = (
    'subsys_hps_agilex_hps.hps2fpga', 'subsys_hps_agilex_hps.lwhps2fpga',
    'subsys_periph_pb_cpu_0.m0', 'subsys_periph_fes_scanout_guard.avm',
    'subsys_periph_fes_ctrl_cdc.m0', 'subsys_periph_fes_scanout.avm',
    'subsys_periph_fes_mem_cdc.m0', 'subsys_hps_f2sdram_adapter.axi4_man')


def constant(definition, name):
    values = re.findall(r"^`define FES_DE25_SCANOUT_" + name + r" 32'h([0-9a-f]{8})$", definition, re.M)
    if len(values) != 1:
        raise ValueError('missing generated DMA constant: ' + name)
    return int(values[0], 16)


def retarget_adapter(data, definition):
    """Retarget SMMU fields and bounded read credits for isolated scanout."""
    # Quartus 26.3 intel_hps/sm/common/parameters_data.tcl, f2sdram_SMMU:
    # raw AxUSER[8] is AxMMUSECSID and [24:9] is AxMMUSID.
    # The ACE5-Lite translator CSR uses a different register layout.
    sid = constant(definition, 'SMMU_STREAM_ID')
    shift = constant(definition, 'SMMU_SID_SHIFT')
    secure_bit = constant(definition, 'SMMU_SECURE_BIT')
    width = constant(definition, 'SMMU_USER_BITS')
    if sid <= 11 or sid > 65535 or (shift, secure_bit, width) != (9, 8, 25):
        raise ValueError('invalid selected SMMU stream/layout')
    text = data.decode()
    for channel in ('ar', 'aw'):
        before = f'add_interface_port axi4_man man_{channel}user {channel}user Output 8\n'
        if text.count(before) != 1:
            raise ValueError('vendor address USER declaration changed')
        text = text.replace(before, before.replace('Output 8', 'Output 25'))
        pattern = r'(output wire\s+)\\\[7:0\\\](\s+man_' + channel + r'user\s*,)'
        text, count = re.subn(pattern, lambda m: m[1] + r'\[24:0\]' + m[2], text)
        if count != 1:
            raise ValueError('vendor address USER HDL port changed')
        pattern = r'(assign man_' + channel + r"user\s*=\s*)8'hE0(\s*;)"
        text, count = re.subn(pattern, lambda m: m[1] + f"25'h{((sid << shift) | 0xe0):07x}" + m[2], text)
        if count != 1:
            raise ValueError('vendor address USER fixed attributes changed')
    # The vendor adapter is an ID-preserving AXI pass-through. Its original
    # one-transaction metadata makes Platform Designer insert a serializing
    # limiter, even when the reader and CDC can reserve sixteen bursts. The
    # selected HPS F2SDRAM interface accepts sixteen reads; match that limit
    # only in this isolated, read-only Linux profile. Write credits stay one.
    for interface, fields in (('axi4_man', ('readIssuingCapability', 'combinedIssuingCapability')),
                              ('axi4_sub', ('readAcceptanceCapability', 'combinedAcceptanceCapability'))):
        for field in fields:
            before = f'set_interface_property {interface} {field} 1\n'
            if text.count(before) != 1:
                raise ValueError('vendor AXI transaction capability changed')
            text = text.replace(before, before.replace(' 1\n', ' 16\n'))
    return text.encode(), {'stream_id': sid, 'user_bits': width, 'sid_shift': shift,
        'secure_bit': secure_bit, 'secure': False, 'attributes': 0xe0, 'read_transactions': 16,
        'scope': 'scanout-only generated F2SDRAM path', 'master_ownership': 'scanout-only',
        'original_sha256': hashlib.sha256(data).hexdigest(), 'derived_sha256': hashlib.sha256(text.encode()).hexdigest(),
        'kernel_binding': 'not-admitted', 'physical_translation': 'not-qualified'}


def configure(project, definition, hps):
    path = project / ADAPTER
    data, profile = retarget_adapter(path.read_bytes(), definition)
    path.write_bytes(data)
    child = hps.tcl_word(project / 'hps_subsys/hps_subsys.qsys')
    script = f'''# Explicit Linux SMMU profile. This changes port exposure, not boot data.
load_system {child}
load_component agilex_hps
set_component_parameter_value f2sdram_SMMU true
if {{[get_component_parameter_value f2sdram_SMMU] != 1}} {{error "SMMU port exposure was not enabled"}}
save_component
load_component f2sdram_adapter
if {{[get_component_parameter_value DATA_WIDTH] != 256}} {{error "F2SDRAM adapter data width changed"}}
foreach port {{man_aruser man_awuser}} {{
    if {{[get_component_interface_port_property axi4_man $port WIDTH] != 25}} {{error "SMMU USER width changed"}}
}}
save_component
save_system {child}
'''
    jtag = hps.tcl_word(project / 'jtag_subsys/jtag_subsys.qsys')
    script += f'''# Remove the JTAG master for the Linux-owned fabric memory path.
load_system {jtag}
if {{[string compare [get_interface_property hps_f2sdram_master EXPORT_OF] "hps_f2sdram.master"] != 0}} {{error "JTAG memory export changed"}}
remove_interface hps_f2sdram_master
remove_instance hps_f2sdram
save_system {jtag}
'''
    return script, profile, {HPS_IP: {'f2sdram_SMMU': {'before': 'false', 'after': 'true'}}}



def isolate_script(script, top_save):
    probe = ("add_instance fes_ddr_probe fes_de25_ddr_probe 1.0\n"
        "add_connection periph_clk.out_clk fes_ddr_probe.clk\n"
        "add_connection periph_rst_in.out_reset fes_ddr_probe.reset\n"
        "add_connection pb_cpu_0.m0 fes_ddr_probe.avs\n"
        r"set_connection_parameter_value pb_cpu_0.m0/fes_ddr_probe.avs baseAddress 0x[0-9a-f]+" + "\n"
        "add_interface fes_probe_mem avalon start\n"
        "set_interface_property fes_probe_mem EXPORT_OF fes_ddr_probe.avm\n")
    pattern = re.escape(probe).replace(re.escape(r"0x[0-9a-f]+"), r"0x[0-9a-f]+")
    script, count = re.subn(pattern, "", script)
    if count != 1:
        raise ValueError("inherited probe integration changed")
    route = ("add_connection subsys_periph.fes_probe_mem subsys_hps.f2sdram_adapter_axi4_sub\n"
        "set_connection_parameter_value subsys_periph.fes_probe_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0\n")
    if script.count(route) != 1 or script.count(top_save) != 1:
        raise ValueError("inherited probe/top integration changed")
    script = script.replace(route, "")
    removal = ("# The Linux stream must have no diagnostic master.\n"
        "remove_instance ext_hps_f2sdram_master\n")
    return script.replace(top_save, removal + top_save)


def isolate_managed_fabric_script(script, top_save):
    if script.count(top_save) != 1 or script.count('remove_instance ext_hps_f2sdram_master\n') != 1:
        raise ValueError('managed fabric requires the isolated memory profile')
    removal = '''# Canonical MENU has no vendor maintenance initiators or F2H translator.
foreach instance {subsys_debug ace5lite_cache_coherency_translator_0} {
    if {[lsearch -exact [get_instances] $instance] < 0} {error "managed fabric removal target changed: $instance"}
    remove_instance $instance
}
'''
    return script.replace(top_save, removal + top_save)


def verify_managed_fabric(project, hps):
    """Check every elaborated initiator and the unused HPS request tieoffs."""
    import xml.etree.ElementTree as ET
    sopc = project / 'qsys_top/qsys_top.sopcinfo'
    tree = ET.parse(sopc)
    modules = tree.findall('module')
    removed = ('subsys_debug', 'ace5lite_cache_coherency_translator_0')
    for module in modules:
        name, kind = module.get('path') or '', module.get('kind') or ''
        if (any(name == prefix or name.startswith(prefix + '.') for prefix in removed) or
                kind in ('altera_jtag_avalon_master', 'altera_ace5lite_cache_coherency_translator')):
            raise ValueError('managed fabric retains a maintenance initiator/translator: ' + name)
    routes = [c for c in tree.findall('connection') if c.get('kind') == 'avalon']
    masters = {c.get('start') for c in routes}
    if masters != set(MANAGED_MASTERS):
        raise ValueError('managed fabric initiator inventory differs')
    ingress = [(c.get('start'), c.get('end')) for c in routes
               if (c.get('end') or '').startswith('subsys_hps_agilex_hps.')]
    expected_ingress = [('subsys_hps_f2sdram_adapter.axi4_man', 'subsys_hps_agilex_hps.f2sdram')]
    if ingress != expected_ingress:
        raise ValueError('managed fabric has an unexpected path into HPS')
    definition = (project / 'fes_mailbox/generated/fes_de25_scanout.vh').read_text()
    control = (
        ('subsys_hps_agilex_hps.lwhps2fpga', 'subsys_periph_pb_cpu_0.s0', 0),
        ('subsys_periph_pb_cpu_0.m0', 'subsys_periph_fes_scanout_guard.avs', constant(definition, 'PERIPHERAL_OFFSET')),
        ('subsys_periph_fes_scanout_guard.avm', 'subsys_periph_fes_ctrl_cdc.s0', 0),
        ('subsys_periph_fes_ctrl_cdc.m0', 'subsys_periph_fes_scanout.avs', 0),
        ('subsys_hps_agilex_hps.hps2fpga', 'ocm.axi_s1', 0))
    for start, end, address in control:
        matches = [c for c in routes if c.get('end') == end]
        if (len(matches) != 1 or matches[0].get('start') != start or
                int(matches[0].findtext("parameter[@name='baseAddress']/value") or '-1', 0) != address):
            raise ValueError('managed fabric control/OCM route differs: ' + end)
    # An unconnected logical interface alone does not prove the emitted hard
    # block cannot request a transaction. Require actual generated zeroes.
    top = project / 'qsys_top/synth/qsys_top.v'
    source = re.sub(r'/\*.*?\*/|//[^\n]*', '', top.read_text(), flags=re.S)
    if any(re.search(r'(?m)^\s*\w+\s+' + re.escape(name) + r'\s*\(', source) for name in removed):
        raise ValueError('managed fabric generated parent retains a removed instance')
    instances = re.findall(r'(?ms)^\t\w+ subsys_hps \(\n(.*?)^\t\);', source)
    if len(instances) != 1:
        raise ValueError('managed fabric generated HPS instance differs')
    ports = re.findall(r'\.(\w+)\s*\(([^()]*)\)', instances[0])
    if len(ports) != len(dict(ports)):
        raise ValueError('managed fabric generated HPS port is duplicated')
    ports = {name: value.strip() for name, value in ports}
    tieoffs = {f'fpga2hps_{channel}valid': ports.get(f'fpga2hps_{channel}valid') for channel in ('ar', 'aw', 'w')}
    if any(value != "1'b0" for value in tieoffs.values()):
        raise ValueError('managed fabric unused F2H request valid is not tied low')
    # Keep the independently checked sole scanout-to-F2SDRAM graph too.
    memory = hps.verify_scanout_memory_handoff(project)
    return {'policy': MANAGED_FABRIC, 'initiator_endpoints': sorted(masters),
            'initiator_count': len(masters), 'fabric_to_hps': [list(route) for route in ingress],
            'control_and_ocm_routes': [list(route) for route in control],
            'removed_instances': list(removed), 'unused_f2h_request_valid': tieoffs,
            'sopcinfo_sha256': hps.digest(sopc), 'generated_parent_sha256': hps.digest(top),
            'memory_handoff': memory, 'physical_lifetime': 'not-qualified', 'runtime_admission': 'not-qualified'}


def verify(project, hps, profile):
    definition = (project / 'fes_mailbox/generated/fes_de25_scanout.vh').read_text()
    sid = constant(definition, 'SMMU_STREAM_ID')
    if profile.get('master_ownership') != 'scanout-only':
        raise ValueError('Linux DMA requires isolated scanout master ownership')
    ownership = hps.verify_scanout_memory_handoff(project)
    if profile.get('stream_id') != sid or profile.get('secure') is not False:
        raise ValueError('captured Linux DMA stream differs')
    if hps.digest(project / ADAPTER) != profile['derived_sha256']:
        raise ValueError('derived SMMU adapter changed')
    if hps.module_parameters(project / HPS_IP).get('f2sdram_SMMU') != 'true':
        raise ValueError('generated HPS SMMU ports not enabled')
    import xml.etree.ElementTree as ET
    tree = ET.parse(project / 'qsys_top/qsys_top.sopcinfo')
    modules = {m.get('path'): m for m in tree.findall('module')}
    for name, interface, direction in (('subsys_hps.agilex_hps', 'f2sdram', 'Input'),
                                       ('subsys_hps.f2sdram_adapter', 'axi4_man', 'Output')):
        ports = hps.component_ports(modules[name], interface)
        if any(ports.get(channel + 'user') != (direction, 25) for channel in ('ar', 'aw')):
            raise ValueError('generated SMMU USER path width differs')
    if profile.get('read_transactions') != 16:
        raise ValueError('Linux scanout read transaction limit differs')
    for module, interface, fields in (
            ('subsys_hps.f2sdram_adapter', 'axi4_man',
             {'readIssuingCapability': '16', 'combinedIssuingCapability': '16', 'writeIssuingCapability': '1'}),
            ('subsys_hps.f2sdram_adapter', 'axi4_sub',
             {'readAcceptanceCapability': '16', 'combinedAcceptanceCapability': '16', 'writeAcceptanceCapability': '1'}),
            ('subsys_hps.agilex_hps', 'f2sdram',
             {'readAcceptanceCapability': '16', 'combinedAcceptanceCapability': '16'})):
        selected = modules[module].find("interface[@name='" + interface + "']")
        if selected is None:
            raise ValueError('missing generated transaction capability interface')
        for field, value in fields.items():
            rows = selected.findall("parameter[@name='" + field + "']/value")
            if len(rows) != 1 or rows[0].text != value:
                raise ValueError('generated AXI transaction capability differs: ' + module + '.' + interface + '.' + field)
    word = (sid << constant(definition, 'SMMU_SID_SHIFT')) | 0xe0
    sources = []
    directory = project / 'hps_subsys/ip/hps_subsys/f2sdram_adapter'
    qip = directory / 'f2sdram_adapter.qip'
    references = re.findall(r'-name (?:VERILOG_FILE|SYSTEMVERILOG_FILE) \[file join \$::quartus\(qip_path\) "([^"\n]+)"\]', qip.read_text())
    if not references:
        raise ValueError('generated adapter QIP has no selected HDL')
    for name in references:
        relative = Path(name)
        if relative.is_absolute() or '..' in relative.parts:
            raise ValueError('unsafe generated adapter HDL reference')
        path = directory / relative
        if path.is_symlink() or not path.is_file():
            raise ValueError('generated adapter HDL is not a regular file')
        text = path.read_text()
        if all(re.search(r'assign man_' + channel + r"user\s*=\s*25'h" + f'{word:07x}' + r'\s*;', text)
               for channel in ('ar', 'aw')):
            sources.append(path)
    if len(sources) != 1:
        raise ValueError('generated adapter does not drive the selected nonsecure stream')
    return {**profile, 'generated_adapter': str(sources[0].relative_to(project)),
            'generated_adapter_sha256': hps.digest(sources[0]), 'generated_qip_sha256': hps.digest(qip),
            'address_user_word': word, 'master_handoff': ownership,
            'kernel_binding': 'not-admitted', 'physical_translation': 'not-qualified'}
