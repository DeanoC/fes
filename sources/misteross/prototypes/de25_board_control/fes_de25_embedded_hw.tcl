package require -exact qsys 20.1
set_module_property NAME fes_de25_embedded
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES embedded control diagnostic"
set_module_property GROUP "FES/Diagnostics"
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
set_module_property ELABORATION_CALLBACK elaborate
add_parameter MEMORY_BACKEND INTEGER 0
set_parameter_property MEMORY_BACKEND ALLOWED_RANGES {0 1}
set_parameter_property MEMORY_BACKEND HDL_PARAMETER true
add_parameter DDR_BASE STD_LOGIC_VECTOR 0
set_parameter_property DDR_BASE WIDTH 32
set_parameter_property DDR_BASE HDL_PARAMETER true
add_parameter DDR_BYTES STD_LOGIC_VECTOR 0
set_parameter_property DDR_BYTES WIDTH 32
set_parameter_property DDR_BYTES HDL_PARAMETER true
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_embedded
add_fileset_file fes_de25_embedded.sv SYSTEM_VERILOG PATH fes_de25_embedded.sv TOP_LEVEL_FILE
add_fileset_file fes_embedded_control.sv SYSTEM_VERILOG PATH fes_embedded_control.sv
add_fileset_file fes_embedded_control.vh VERILOG_INCLUDE PATH generated/fes_embedded_control.vh
add_interface clk clock sink
add_interface_port clk clk clk Input 1
add_interface reset reset sink
set_interface_property reset associatedClock clk
set_interface_property reset synchronousEdges DEASSERT
add_interface_port reset reset_n reset_n Input 1
add_interface maintenance_reset reset start
set_interface_property maintenance_reset associatedClock clk
add_interface_port maintenance_reset maintenance_reset_n reset_n Output 1
add_interface avs avalon end
set_interface_property avs associatedClock clk
set_interface_property avs associatedReset reset
set_interface_property avs addressUnits SYMBOLS
set_interface_property avs bitsPerSymbol 8
set_interface_property avs explicitAddressSpan 256
set_interface_property avs maximumPendingReadTransactions 1
set_interface_property avs maximumPendingWriteTransactions 1
set_interface_property avs minimumResponseLatency 1
set_interface_property avs readLatency 0
set_interface_property avs readWaitTime 0
set_interface_property avs writeWaitTime 0
set_interface_property avs waitrequestAllowance 0
set_interface_property avs isMemoryDevice false
add_interface_port avs address address Input 8
add_interface_port avs read read Input 1
add_interface_port avs write write Input 1
add_interface_port avs byteenable byteenable Input 4
add_interface_port avs writedata writedata Input 32
add_interface_port avs readdata readdata Output 32
add_interface_port avs readdatavalid readdatavalid Output 1
add_interface_port avs waitrequest waitrequest Output 1
add_interface_port avs writeresponsevalid writeresponsevalid Output 1
add_interface_port avs response response Output 2

# The required AXI write pins are hardwired inactive in RTL. Platform Designer
# supplies interconnect/width conversion; reads consume real AXI backpressure.
add_interface mem axi4 start
set_interface_property mem associatedClock clk
set_interface_property mem associatedReset reset
set_interface_property mem readIssuingCapability 1
set_interface_property mem combinedIssuingCapability 1
set_interface_property mem issuesINCRBursts true
set_interface_property mem issuesWRAPBursts false
set_interface_property mem issuesFIXEDBursts false
foreach {name role direction width} {
    ddr_arvalid arvalid Output 1 ddr_araddr araddr Output 32
    ddr_arlen arlen Output 8 ddr_arsize arsize Output 3
    ddr_arburst arburst Output 2 ddr_arid arid Output 1
    ddr_arlock arlock Output 1 ddr_arcache arcache Output 4
    ddr_arprot arprot Output 3 ddr_arqos arqos Output 4
    ddr_arready arready Input 1 ddr_rvalid rvalid Input 1
    ddr_rdata rdata Input 128 ddr_rlast rlast Input 1
    ddr_rresp rresp Input 2 ddr_rid rid Input 1
    ddr_rready rready Output 1
    ddr_awvalid awvalid Output 1 ddr_awaddr awaddr Output 32
    ddr_awlen awlen Output 8 ddr_awsize awsize Output 3
    ddr_awburst awburst Output 2 ddr_awid awid Output 1
    ddr_awlock awlock Output 1 ddr_awcache awcache Output 4
    ddr_awprot awprot Output 3 ddr_awqos awqos Output 4
    ddr_awready awready Input 1 ddr_wvalid wvalid Output 1
    ddr_wdata wdata Output 128 ddr_wstrb wstrb Output 16
    ddr_wlast wlast Output 1 ddr_wready wready Input 1
    ddr_bvalid bvalid Input 1 ddr_bresp bresp Input 2
    ddr_bid bid Input 1 ddr_bready bready Output 1
} { add_interface_port mem $name $role $direction $width }
proc elaborate {} {
    set_interface_property mem ENABLED [get_parameter_value MEMORY_BACKEND]
    set_interface_property maintenance_reset ENABLED [get_parameter_value MEMORY_BACKEND]
}
