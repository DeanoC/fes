"""Prepare the explicit, unadmitted Linux SMMU profile in a private project."""
import hashlib
import json
from pathlib import Path
import re

HPS_IP = 'hps_subsys/ip/hps_subsys/agilex_hps.ip'
ADAPTER_IP = 'hps_subsys/ip/hps_subsys/f2sdram_adapter.ip'
ADAPTER = 'custom_ip/f2sdram_adapter/f2sdram_adapter_hw.tcl'
MANAGED_FABRIC = 'managed-scanout-initiators-v4'
GHRD_SDC_SHA256 = '743b02b7fa6e3f6397890702f626ff7b27a5c7ab6aa9d1721c22d286900c7247'
JTAG_SDC_SHA256 = '02d78bf9bc14eea02092569a0639f6bd4f4b11e161e4fc6349c30128d640e17a'
EMIF_REFERENCE = {
    'policy': 'managed-generated-emif-reference-v1',
    'removed_duplicate': 'create_clock -name EMIF_REF_CLOCK -period "166.6666MHz" [get_ports LPDDR4A_REFCLK_p]',
    'clock': 'soc_inst|subsys_hps|emif_hps|emif_io96b_hps|emif_0_lpddr4|emif_0_lpddr4_0_ref_clock',
    'period_ns': '6.000', 'port': 'LPDDR4A_REFCLK_p',
    'fitted_clock_validation': 'required',
}
REMOVED_INSTANCES = ('subsys_debug', 'ace5lite_cache_coherency_translator_0', 'ocm')
DISABLED_HPS_PARAMETERS = ('f2s_data_width', 'H2F_Width')
MANAGED_MASTERS = (
    'subsys_hps_agilex_hps.lwhps2fpga',
    'subsys_periph_pb_cpu_0.m0', 'subsys_periph_fes_scanout_guard.avm',
    'subsys_periph_fes_ctrl_cdc.m0', 'subsys_periph_fes_scanout.avm',
    'subsys_periph_fes_mem_cdc.m0', 'subsys_hps_f2sdram_adapter.axi4_man')
# A machine with a window in HPS DDR (fes.fabric.de25-hps-window) adds exactly
# these: its one window master and that master's clock-crossing bridge.
WINDOW_MASTERS = ('subsys_periph_fes_scanout.win', 'subsys_periph_fes_win_cdc.m0')
WINDOW_BRIDGE = 'subsys_periph_fes_win_cdc.m0'


def constant(definition, name):
    values = re.findall(r"^`define FES_DE25_SCANOUT_" + name + r" 32'h([0-9a-f]{8})$", definition, re.M)
    if len(values) != 1:
        raise ValueError('missing generated DMA constant: ' + name)
    return int(values[0], 16)


def order_window_reads(text):
    """Keep the narrow scanout and window replies in AXI issue order."""
    queue = r"""// The generated Avalon path expects responses in issue order.
// Use one F2SDRAM read ID and restore the original owner from a FIFO.
reg \[4:0\] read_id_fifo \[0:15\];
reg \[3:0\] read_head, read_tail;
reg \[4:0\] read_count;
wire read_have_credit = !reset && read_count != 5'd16;
wire read_have_id = !reset && read_count != 5'd0;
wire read_accepted = sub_arvalid && sub_arready;
wire read_finished = man_rvalid && man_rready && man_rlast;
always @(posedge clk) begin
    if (reset) begin
        read_head <= 4'd0;
        read_tail <= 4'd0;
        read_count <= 5'd0;
    end else begin
        if (read_accepted) begin
            read_id_fifo\[read_tail\] <= sub_arid;
            read_tail <= read_tail + 4'd1;
        end
        if (read_finished) read_head <= read_head + 4'd1;
        case ({read_accepted, read_finished})
            2'b10: read_count <= read_count + 5'd1;
            2'b01: read_count <= read_count - 5'd1;
            default: ;
        endcase
    end
end

assign sub_rid     = read_id_fifo\[read_head\];
assign sub_arready = read_have_credit && man_arready;"""
    changes = (
        ("assign sub_rid     = man_rid;\nassign sub_arready = man_arready;", queue),
        ("assign sub_rvalid  = man_rvalid;", "assign sub_rvalid  = read_have_id && man_rvalid;"),
        ("assign man_rready   = sub_rready;", "assign man_rready   = read_have_id && sub_rready;"),
        ("assign man_arvalid  = sub_arvalid;", "assign man_arvalid  = read_have_credit && sub_arvalid;"),
        ("assign man_arid     = sub_arid;", "assign man_arid     = 5'd0;"),
    )
    for before, after in changes:
        if text.count(before) != 1:
            raise ValueError('vendor F2SDRAM read channel changed')
        text = text.replace(before, after)
    return text


def retarget_adapter(data, definition, *, window=False):
    """Retarget SMMU fields and bounded read credits for the selected masters."""
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
    if window:
        text = order_window_reads(text)
    return text.encode(), {'stream_id': sid, 'user_bits': width, 'sid_shift': shift,
        'secure_bit': secure_bit, 'secure': False, 'attributes': 0xe0, 'read_transactions': 16,
        'scope': 'scanout-only generated F2SDRAM path', 'master_ownership': 'scanout-only',
        'original_sha256': hashlib.sha256(data).hexdigest(), 'derived_sha256': hashlib.sha256(text.encode()).hexdigest(),
        'kernel_binding': 'not-admitted', 'physical_translation': 'not-qualified'}


def configure(project, definition, hps, *, managed_fabric=False, window=False):
    path = project / ADAPTER
    data, profile = retarget_adapter(path.read_bytes(), definition, window=window)
    path.write_bytes(data)
    if window:
        # The window's master shares the scanout's stream: one SID, reads and
        # writes. Its write credits stay the adapter's one.
        profile.update(scope='scanout and window generated F2SDRAM path', master_ownership='scanout+window',
                       window_master=WINDOW_BRIDGE, data_bits=128)
    child = hps.tcl_word(project / 'hps_subsys/hps_subsys.qsys')
    script = f'''# Explicit Linux SMMU profile. This changes port exposure, not boot data.
load_system {child}
load_component agilex_hps
set_component_parameter_value f2sdram_SMMU true
if {{[get_component_parameter_value f2sdram_SMMU] != 1}} {{error "SMMU port exposure was not enabled"}}
save_component
'''
    changes = {HPS_IP: {'f2sdram_SMMU': {'before': 'false', 'after': 'true'}}}
    if managed_fabric:
        # Quartus 26.3 parameters_data.tcl: f2s_data_width=0 means Unused.
        # An enabled F2H hard bridge must have its complete response interface
        # connected; tying only request valids low is not a legal disable.
        script += '''load_component agilex_hps
if {[get_component_parameter_value f2s_data_width] != 256} {error "vendor F2H data width changed"}
set_component_parameter_value f2s_data_width 0
if {[get_component_parameter_value f2s_data_width] != 0} {error "unused F2H interface was not disabled"}
# The vendor on-chip memory was the only responder on the full HPS-to-FPGA
# bridge and no FES software addresses it. H2F_Width=0 is the documented
# Unused selection; a bridge left enabled without a responder would stall
# any HPS access instead of having no fabric interface at all.
if {[get_component_parameter_value H2F_Width] != 128} {error "vendor H2F data width changed"}
set_component_parameter_value H2F_Width 0
if {[get_component_parameter_value H2F_Width] != 0} {error "unused H2F interface was not disabled"}
save_component
foreach {name target} {fpga2hps agilex_hps.fpga2hps fpga2hps_clk agilex_hps.fpga2hps_clock fpga2hps_rst agilex_hps.fpga2hps_reset
                       hps2fpga agilex_hps.hps2fpga hps2fpga_clk agilex_hps.hps2fpga_axi_clock hps2fpga_rst agilex_hps.hps2fpga_axi_reset} {
    if {[string compare [get_interface_property $name EXPORT_OF] $target] != 0} {error "vendor bridge export changed: $name"}
    remove_interface $name
}
'''
        changes[HPS_IP]['f2s_data_width'] = {'before': '256', 'after': '0'}
        changes[HPS_IP]['H2F_Width'] = {'before': '128', 'after': '0'}
    if window:
        # The 256-bit hard bridge requires 32-byte command addresses. The
        # window's 128-bit master can issue at any 16-byte boundary, including
        # masked status-ring writes. Match the scanout and window paths at 128
        # bits so neither read nor write commands need 32-byte alignment.
        script += '''load_component agilex_hps
if {[get_component_parameter_value f2sdram_data_width] != 256} {error "vendor F2SDRAM data width changed"}
set_component_parameter_value f2sdram_data_width 128
save_component
'''
        changes[HPS_IP]['f2sdram_data_width'] = {'before': '256', 'after': '128'}
        changes[ADAPTER_IP] = {'DATA_WIDTH': {'before': '256', 'after': '128'}}
    script += f'''load_component f2sdram_adapter
if {{[get_component_parameter_value DATA_WIDTH] != 256}} {{error "F2SDRAM adapter data width changed"}}
'''
    if window:
        script += 'set_component_parameter_value DATA_WIDTH 128\n'
    script += f'''\
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
    return script, profile, changes



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
    removal = '''# The managed shell has no vendor maintenance initiators, F2H translator
# or on-chip memory. Removing the debug subsystem first leaves the memory
# with exactly its HPS route, clock and reset, which are checked before it goes.
foreach instance {subsys_debug ace5lite_cache_coherency_translator_0} {
    if {[lsearch -exact [get_instances] $instance] < 0} {error "managed fabric removal target changed: $instance"}
    remove_instance $instance
}
foreach connection [get_connections] {
    if {[string match "*subsys_hps.fpga2hps*" $connection] || [string match "*subsys_hps.hps2fpga*" $connection] ||
            [string match "*/ocm.*" $connection]} {
        if {[lsearch -exact {clk_100.out_clk/subsys_hps.fpga2hps_clk rst_in.out_reset/subsys_hps.fpga2hps_rst
                             clk_100.out_clk/subsys_hps.hps2fpga_clk rst_in.out_reset/subsys_hps.hps2fpga_rst
                             subsys_hps.hps2fpga/ocm.axi_s1 clk_100.out_clk/ocm.clk1 rst_in.out_reset/ocm.reset1} $connection] < 0} {error "vendor bridge/memory connection changed: $connection"}
        remove_connection $connection
    }
}
if {[lsearch -exact [get_instances] ocm] < 0} {error "managed fabric removal target changed: ocm"}
remove_instance ocm
'''
    return script.replace(top_save, removal + top_save)


def retired_jtag_sdc(original):
    if hashlib.sha256(original).hexdigest() != GHRD_SDC_SHA256:
        raise ValueError('managed fabric GHRD constraint input differs')
    before = b'# sourcing JTAG related SDC\nsource ./jtag.sdc\n'
    if original.count(before) != 1:
        raise ValueError('managed fabric JTAG constraint source changed')
    return original.replace(before, b'# Canonical MENU has no fabric JTAG interfaces.\n')


def _debug_constraint_receipt(original, derived, *, generated_emif=False):
    return {'policy': MANAGED_FABRIC, 'original_file': 'managed-constraints-originals/ghrd_timing.sdc',
            'derived_file': 'project/ghrd_timing.sdc', 'retired_source': 'source ./jtag.sdc',
            'original_sha256': hashlib.sha256(original).hexdigest(),
            'derived_sha256': hashlib.sha256(derived).hexdigest(),
            'retained_jtag_sdc_sha256': JTAG_SDC_SHA256,
            **({'emif_reference': dict(EMIF_REFERENCE)} if generated_emif else {})}


def _generated_emif_sdc(original):
    source = retired_jtag_sdc(original)
    duplicate = EMIF_REFERENCE['removed_duplicate'].encode()
    if source.splitlines().count(duplicate) != 1 or source.count(b'EMIF_REF_CLOCK') != 1:
        raise ValueError('managed duplicate EMIF declaration differs')
    return source.replace(duplicate, b'# Generated EMIF SDC owns the 6.000 ns LPDDR4A reference clock.')


def retire_managed_emif_reference(project):
    """Opt in after the canonical source audit; preserve generated IP timing."""
    review = verify_managed_debug_constraints(project)
    if 'emif_reference' in review:
        raise ValueError('managed duplicate EMIF reference already retired')
    original = (project.parent / 'managed-constraints-originals/ghrd_timing.sdc').read_bytes()
    derived = _generated_emif_sdc(original)
    receipt = _debug_constraint_receipt(original, derived, generated_emif=True)
    (project / 'ghrd_timing.sdc').write_bytes(derived)
    (project.parent / 'managed-debug-constraints.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    return verify_managed_debug_constraints(project)


def retire_managed_jtag_constraints(project):
    if hashlib.sha256((project / 'jtag.sdc').read_bytes()).hexdigest() != JTAG_SDC_SHA256:
        raise ValueError('managed fabric vendor JTAG constraints differ')
    path = project / 'ghrd_timing.sdc'
    original = path.read_bytes()
    derived = retired_jtag_sdc(original)
    retained = project.parent / 'managed-constraints-originals'
    retained.mkdir(exist_ok=False)
    (retained / 'ghrd_timing.sdc').write_bytes(original)
    path.write_bytes(derived)
    receipt = _debug_constraint_receipt(original, derived)
    (project.parent / 'managed-debug-constraints.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    return receipt


def verify_managed_debug_constraints(project):
    original = (project.parent / 'managed-constraints-originals/ghrd_timing.sdc').read_bytes()
    receipt = json.loads((project.parent / 'managed-debug-constraints.json').read_text())
    generated_emif = 'emif_reference' in receipt
    derived = _generated_emif_sdc(original) if generated_emif else retired_jtag_sdc(original)
    if (hashlib.sha256((project / 'jtag.sdc').read_bytes()).hexdigest() != JTAG_SDC_SHA256 or
            (project / 'ghrd_timing.sdc').read_bytes() != derived or
            receipt != _debug_constraint_receipt(original, derived, generated_emif=generated_emif)):
        raise ValueError('managed fabric retired JTAG constraint source or receipt changed')
    return receipt


def _managed_fabric_graph(project, hps, window):
    import xml.etree.ElementTree as ET
    sopc = project / 'qsys_top/qsys_top.sopcinfo'
    tree = ET.parse(sopc)
    modules = tree.findall('module')
    removed = REMOVED_INSTANCES
    for module in modules:
        name, kind = module.get('path') or '', module.get('kind') or ''
        if (any(name == prefix or name.startswith(prefix + '.') for prefix in removed) or
                kind in ('altera_jtag_avalon_master', 'altera_ace5lite_cache_coherency_translator',
                         'intel_onchip_memory')):
            raise ValueError('managed fabric retains a maintenance initiator/translator/memory: ' + name)
    routes = [c for c in tree.findall('connection') if c.get('kind') == 'avalon']
    masters = {c.get('start') for c in routes}
    if masters != set(MANAGED_MASTERS) | (set(WINDOW_MASTERS) if window else set()):
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
        ('subsys_periph_fes_ctrl_cdc.m0', 'subsys_periph_fes_scanout.avs', 0))
    if window:
        window_definition = (project / 'fes_mailbox/generated/fes_de25_hps_window.vh').read_text()
        offsets = re.findall(r"^`define FES_DE25_HPS_WINDOW_PERIPHERAL_OFFSET 32'h([0-9a-f]{8})$", window_definition, re.M)
        if len(offsets) != 1:
            raise ValueError('missing generated window constant: PERIPHERAL_OFFSET')
        control += (('subsys_periph_pb_cpu_0.m0', 'subsys_periph_fes_scanout.window', int(offsets[0], 16)),
                    ('subsys_periph_fes_scanout.win', 'subsys_periph_fes_win_cdc.s0', 0))
    for start, end, address in control:
        matches = [c for c in routes if c.get('end') == end]
        if (len(matches) != 1 or matches[0].get('start') != start or
                int(matches[0].findtext("parameter[@name='baseAddress']/value") or '-1', 0) != address):
            raise ValueError('managed fabric control route differs: ' + end)
    native = [m for m in modules if m.get('path') == 'subsys_hps.agilex_hps']
    parameters = hps.module_parameters(project / HPS_IP)
    if len(native) != 1 or any(parameters.get(name) != '0' for name in DISABLED_HPS_PARAMETERS):
        raise ValueError('managed fabric native F2H/H2F interface is not disabled')
    definition = ET.fromstring(native[0].findtext("parameter[@name='componentDefinition']/value"))
    interfaces = [p.findtext('name') for p in definition.findall('boundary/interfaces/interface')]
    if any(name in interfaces for name in ('fpga2hps', 'fpga2hps_clock', 'fpga2hps_reset',
                                           'hps2fpga', 'hps2fpga_axi_clock', 'hps2fpga_axi_reset')):
        raise ValueError('managed fabric generated native F2H/H2F interface remains exposed')
    memory = hps.verify_scanout_memory_handoff(project, WINDOW_BRIDGE if window else None)
    return {'policy': MANAGED_FABRIC, 'initiator_endpoints': sorted(masters),
            'initiator_count': len(masters), 'fabric_to_hps': [list(route) for route in ingress],
            'control_routes': [list(route) for route in control],
            'removed_instances': list(removed), 'sopcinfo_sha256': hps.digest(sopc),
            'disabled_hps_parameters': {name: '0' for name in DISABLED_HPS_PARAMETERS},
            'hps_ip_sha256': hps.digest(project / HPS_IP),
            'memory_handoff': memory, 'physical_lifetime': 'not-qualified', 'runtime_admission': 'not-qualified'}


def _managed_parent(data):
    source = re.sub(r'/\*.*?\*/|//[^\n]*', '', data.decode(), flags=re.S)
    if any(re.search(r'(?m)^\s*\w+\s+' + re.escape(name) + r'\s*\(', source) for name in REMOVED_INSTANCES):
        raise ValueError('managed fabric generated parent retains a removed instance')
    instances = re.findall(r'(?ms)^\t\w+ subsys_hps \(\n(.*?)^\t\);', source)
    if len(instances) != 1:
        raise ValueError('managed fabric generated HPS instance differs')
    ports = re.findall(r'\.(\w+)\s*\(([^()]*)\)', instances[0])
    if len(ports) != len(dict(ports)):
        raise ValueError('managed fabric generated HPS port is duplicated')
    ports = {name: value.strip() for name, value in ports}
    return {name: value for name, value in ports.items()
            if name.startswith(('fpga2hps_', 'hps2fpga_')) or name in ('fpga2hps', 'hps2fpga')}


def verify_managed_fabric(project, hps, *, window=False):
    """Check the complete graph and actual absence of the disabled interface.

    window: the build selected fes.fabric.de25-hps-window, whose master and
    bridge join the initiator inventory and the control routes."""
    graph = _managed_fabric_graph(project, hps, window)
    top = project / 'qsys_top/synth/qsys_top.v'
    if _managed_parent(top.read_bytes()):
        raise ValueError('managed fabric generated parent retains unused F2H/H2F ports')
    return {**graph, 'unused_f2h_interface': 'disabled and absent',
            'unused_h2f_interface': 'disabled and absent', 'generated_parent_sha256': hps.digest(top),
            'retired_debug_constraints': verify_managed_debug_constraints(project)}


def verify(project, hps, profile):
    definition = (project / 'fes_mailbox/generated/fes_de25_scanout.vh').read_text()
    sid = constant(definition, 'SMMU_STREAM_ID')
    window = profile.get('master_ownership') == 'scanout+window' and profile.get('window_master') == WINDOW_BRIDGE
    if profile.get('master_ownership') != 'scanout-only' and not window:
        raise ValueError('Linux DMA requires isolated scanout master ownership')
    ownership = hps.verify_scanout_memory_handoff(project, WINDOW_BRIDGE if window else None)
    if profile.get('stream_id') != sid or profile.get('secure') is not False:
        raise ValueError('captured Linux DMA stream differs')
    if hps.digest(project / ADAPTER) != profile['derived_sha256']:
        raise ValueError('derived SMMU adapter changed')
    parameters = hps.module_parameters(project / HPS_IP)
    if parameters.get('f2sdram_SMMU') != 'true':
        raise ValueError('generated HPS SMMU ports not enabled')
    data_bits = 128 if window else 256
    if ((window and profile.get('data_bits') != data_bits)
            or parameters.get('f2sdram_data_width') != str(data_bits)
            or hps.module_parameters(project / ADAPTER_IP).get('DATA_WIDTH') != str(data_bits)):
        raise ValueError('generated F2SDRAM hard bridge/adapter data width differs')
    import xml.etree.ElementTree as ET
    tree = ET.parse(project / 'qsys_top/qsys_top.sopcinfo')
    modules = {m.get('path'): m for m in tree.findall('module')}
    for name, interface, direction in (('subsys_hps.agilex_hps', 'f2sdram', 'Input'),
                                       ('subsys_hps.f2sdram_adapter', 'axi4_man', 'Output')):
        ports = hps.component_ports(modules[name], interface)
        if any(ports.get(channel + 'user') != (direction, 25) for channel in ('ar', 'aw')):
            raise ValueError('generated SMMU USER path width differs')
        response_direction = 'Output' if direction == 'Input' else 'Input'
        if (ports.get('rdata') != (response_direction, data_bits)
                or ports.get('wdata') != (direction, data_bits)
                or ports.get('wstrb') != (direction, data_bits // 8)):
            raise ValueError('generated F2SDRAM data or strobe width differs')
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
    if window:
        generated = sources[0].read_text()
        if not (re.search(r"assign man_arid\s*=\s*5'd0\s*;", generated)
                and re.search(r"assign sub_rid\s*=\s*read_id_fifo\[read_head\]\s*;", generated)):
            raise ValueError('generated F2SDRAM window adapter does not restore ordered read IDs')
    return {**profile, 'generated_adapter': str(sources[0].relative_to(project)),
            'generated_adapter_sha256': hps.digest(sources[0]), 'generated_qip_sha256': hps.digest(qip),
            'address_user_word': word, 'master_handoff': ownership,
            'kernel_binding': 'not-admitted', 'physical_translation': 'not-qualified'}
