"""Canonical BRAM computer: CPU control, with no fabric memory initiator.

The standalone maintenance recipe is unchanged. All edits operate on captured
private inputs; the generated graph and actual parent ports are checked again
after compilation. This supplies no firmware or physical acceptance.
"""
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET

POLICY = "managed-bram-computer-initiators-v1"
HPS_IP = "hps_subsys/ip/hps_subsys/agilex_hps.ip"
PARAMETERS = {"f2s_data_width": {"before": "256", "after": "0"},
              "f2sdram_data_width": {"before": "256", "after": "0"},
              "Rst_hps_warm_en": {"before": "false", "after": "true"}}
MASTERS = {"subsys_hps_agilex_hps.hps2fpga", "subsys_hps_agilex_hps.lwhps2fpga",
           "subsys_periph_pb_cpu_0.m0"}
REMOVED = ("subsys_debug", "ace5lite_cache_coherency_translator_0", "ext_hps_f2sdram_master",
           "subsys_periph.fes_ddr_probe", "subsys_hps.f2sdram_adapter")


def replace_once(text, before, after):
    if text.count(before) != 1:
        raise ValueError("managed BRAM computer patch target differs: " + before[:100])
    return text.replace(before, after)


def configure(project, hps, dma):
    """Select documented Unused bridges and retain the warm-reset conduit."""
    original = hps.module_parameters(project / HPS_IP)
    if any(original.get(name) != values["before"] for name, values in PARAMETERS.items()):
        raise ValueError("managed BRAM computer vendor HPS parameters differ")
    child = hps.tcl_word(project / "hps_subsys/hps_subsys.qsys")
    prefix = "load_system " + child + "\nload_component agilex_hps\n"
    for name, values in PARAMETERS.items():
        before = f"[get_component_parameter_value {name}] != 256" if name != "Rst_hps_warm_en" else f"[get_component_parameter_value {name}]"
        after = f"[get_component_parameter_value {name}] != 0" if name != "Rst_hps_warm_en" else f"![get_component_parameter_value {name}]"
        prefix += (f"if {{{before}}} {{error \"BRAM HPS input differs: {name}\"}}\n"
                   f"set_component_parameter_value {name} {values['after']}\n"
                   f"if {{{after}}} {{error \"BRAM HPS selection differs: {name}\"}}\n")
    prefix += "save_component\n"
    exports = {"fpga2hps": "agilex_hps.fpga2hps", "fpga2hps_clk": "agilex_hps.fpga2hps_clock",
               "fpga2hps_rst": "agilex_hps.fpga2hps_reset", "f2sdram_clk": "agilex_hps.f2sdram_axi_clock",
               "f2sdram_rst": "agilex_hps.f2sdram_axi_reset",
               "f2sdram_adapter_axi4_sub": "f2sdram_adapter.axi4_sub",
               "f2sdram_adapter_clk": "f2sdram_adapter.clock", "f2sdram_adapter_rst": "f2sdram_adapter.reset"}
    for name, target in exports.items():
        prefix += (f'if {{[string compare [get_interface_property {name} EXPORT_OF] "{target}"] != 0}} '
                   f'{{error "BRAM HPS export differs: {name}"}}\nremove_interface {name}\n')
    prefix += "remove_instance f2sdram_adapter\nsave_system " + child + "\n"
    script_path = project / "integrate-mailbox.tcl"
    script = script_path.read_text()
    top_save = "save_system " + hps.tcl_word(project / "qsys_top.qsys") + "\n"
    script = dma.isolate_script(script, top_save)
    jtag = ("# Existing maintenance master gains peripheral control, not a new DDR master.\n"
            "add_connection subsys_debug.fpga_m_master subsys_periph.pb_cpu_0_s0\n"
            "set_connection_parameter_value subsys_debug.fpga_m_master/subsys_periph.pb_cpu_0_s0 baseAddress 0x100000\n")
    script = replace_once(script, jtag, "")
    removal = r'''# No fabric-to-HPS memory access exists in the BRAM computer.
foreach instance {subsys_debug ace5lite_cache_coherency_translator_0} {
    if {[lsearch -exact [get_instances] $instance] < 0} {error "BRAM removal target differs: $instance"}
    remove_instance $instance
}
foreach connection [get_connections] {
    if {[regexp {subsys_hps\.(fpga2hps|f2sdram)} $connection]} {
        if {[lsearch -exact {
            clk_100.out_clk/subsys_hps.fpga2hps_clk rst_in.out_reset/subsys_hps.fpga2hps_rst
            clk_100.out_clk/subsys_hps.f2sdram_clk rst_in.out_reset/subsys_hps.f2sdram_rst
            clk_100.out_clk/subsys_hps.f2sdram_adapter_clk rst_in.out_reset/subsys_hps.f2sdram_adapter_rst
        } $connection] < 0} {error "Unexpected BRAM memory connection: $connection"}
        remove_connection $connection
    }
}
'''
    script = replace_once(script, top_save, removal + top_save)
    script = replace_once(script, "package require -exact qsys 26.3\n", "package require -exact qsys 26.3\n" + prefix)
    script_path.write_text(script)
    dma.retire_managed_jtag_constraints(project)
    return {HPS_IP: PARAMETERS}


def hdmi_board(original, synchronizer_source):
    """Reuse the current reviewed input synchronizers and controller ports."""
    start = synchronizer_source.index('    (* preserve, dont_merge, async_reg="true"')
    end = synchronizer_source.index('    wire hdmi_ready;', start)
    synchronizers = synchronizer_source[start:end]
    if synchronizers.count("reg [1:0]") != 2 or "posedge CLOCK0_50 or negedge config_reset_n" not in synchronizers:
        raise ValueError("reviewed HDMI input synchronizers differ")
    original = replace_once(original, "    I2C_HDMI_Config hdmi_config", synchronizers + "    fes_de25_scanout_hdmi_config hdmi_config")
    return replace_once(original, ".HDMI_TX_INT(HDMI_TX_INT), .READY(hdmi_config_ready)",
                        ".I2C_SDAT_IN(hdmi_sda_sync[1]), .HDMI_TX_INT(hdmi_int_sync[1]), .READY(hdmi_config_ready)")


def verify(project, module_parameters, digest):
    """Prove the actual complete fabric ingress inventory, not just settings."""
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = tree.findall("module")
    for module in modules:
        name, kind = module.get("path", ""), module.get("kind", "")
        if (any(name == prefix or name.startswith(prefix + ".") for prefix in REMOVED) or
                kind in ("altera_jtag_avalon_master", "altera_ace5lite_cache_coherency_translator", "fes_de25_ddr_probe")):
            raise ValueError("managed BRAM retains a memory/maintenance instance: " + name)
    routes = [c for c in tree.findall("connection") if c.get("kind") == "avalon"]
    masters = {c.get("start") for c in routes}
    if masters != MASTERS or any(c.get("end", "").startswith("subsys_hps_agilex_hps.") for c in routes):
        raise ValueError("managed BRAM fabric initiator/ingress inventory differs")
    definitions = (project / "fes_mailbox/generated/fes_de25_computer.vh").read_text()
    offsets = re.findall(r"^`define FES_DE25_COMPUTER_PERIPHERAL_OFFSET 32'h([0-9a-f]{8})$", definitions, re.M)
    if len(offsets) != 1:
        raise ValueError("managed BRAM computer offset differs")
    for start, end, base in (("subsys_hps_agilex_hps.lwhps2fpga", "subsys_periph_pb_cpu_0.s0", 0),
                             ("subsys_hps_agilex_hps.hps2fpga", "ocm.axi_s1", 0),
                             ("subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_computer.avs", int(offsets[0], 16))):
        matches = [r for r in routes if r.get("end") == end]
        if (len(matches) != 1 or matches[0].get("start") != start or
                int(matches[0].findtext("parameter[@name='baseAddress']/value") or "-1", 0) != base):
            raise ValueError("managed BRAM control/OCM route differs: " + end)
    actual = module_parameters(project / HPS_IP)
    if any(actual.get(name) != values["after"] for name, values in PARAMETERS.items()):
        raise ValueError("managed BRAM generated HPS parameters differ")
    native = [m for m in modules if m.get("path") == "subsys_hps.agilex_hps"]
    if len(native) != 1:
        raise ValueError("managed BRAM native HPS instance differs")
    definition = ET.fromstring(native[0].findtext("parameter[@name='componentDefinition']/value"))
    interfaces = [n.findtext("name", "") for n in definition.findall("boundary/interfaces/interface")]
    if (any(name.startswith(("f2sdram", "fpga2hps")) and not name.startswith("fpga2hps_interrupt") for name in interfaces) or
            "h2f_warm_reset_handshake" not in interfaces):
        raise ValueError("managed BRAM disabled native ports/warm-reset conduit differ")
    top = project / "qsys_top/synth/qsys_top.v"
    source = re.sub(r"/\*.*?\*/|//[^\n]*", "", top.read_text(), flags=re.S)
    instances = re.findall(r"(?ms)^\t\w+ subsys_hps \(\n(.*?)^\t\);", source)
    if len(instances) != 1:
        raise ValueError("managed BRAM generated HPS parent differs")
    if any(name.startswith(("f2sdram", "fpga2hps")) for name in re.findall(r"\.(\w+)\s*\(", instances[0])):
        raise ValueError("managed BRAM generated parent retains disabled ports")
    return {"policy": POLICY, "initiator_endpoints": sorted(masters), "fabric_to_hps": [],
            "removed_instances": list(REMOVED), "disabled_bridges": ["f2s", "f2sdram"],
            "warm_reset_handshake": "enabled, acknowledgement remains inactive",
            "sopcinfo_sha256": digest(project / "qsys_top/qsys_top.sopcinfo"),
            "generated_parent_sha256": digest(top), "hps_ip_sha256": digest(project / HPS_IP),
            "boot_firmware_compatibility": "not-qualified", "native_acceptance": False}
