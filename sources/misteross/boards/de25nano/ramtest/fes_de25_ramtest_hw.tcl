package require -exact qsys 20.1
set_module_property VERSION 1.0
set_module_property GROUP "FES/Diagnostics"
set_module_property AUTHOR "FES"
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
add_fileset_file generated/fes_de25_ramtest.vh VERILOG_INCLUDE PATH generated/fes_de25_ramtest.vh
add_fileset_file scan.v VERILOG PATH scan.v
add_fileset_file endpoint.v VERILOG PATH endpoint.v
add_interface clk clock sink
add_interface_port clk clk clk Input 1
add_interface reset reset sink
set_interface_property reset associatedClock clk
set_interface_property reset synchronousEdges DEASSERT
add_interface_port reset reset_n reset_n Input 1
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
set_interface_property avs waitrequestAllowance 0
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
set_module_property NAME fes_de25_ramtest
set_module_property DISPLAY_NAME "FES DE25 bounded RAM tester"
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_ramtest
add_parameter DATA_W INTEGER 128
set_parameter_property DATA_W ALLOWED_RANGES {16 128}
add_parameter CHANNEL INTEGER 0
add_parameter LOCAL_MEMORY INTEGER 0
add_parameter CAPACITY_BYTES INTEGER 1073741824
add_parameter CLOCK_HZ INTEGER 100000000
foreach parameter {DATA_W CHANNEL LOCAL_MEMORY CAPACITY_BYTES CLOCK_HZ} {
    set_parameter_property $parameter HDL_PARAMETER true
}
add_interface calibration conduit end
add_interface_port calibration calibrated calibrated Input 1
add_interface_port calibration memory_ready ready Input 1
add_interface avm avalon start
set_interface_property avm associatedClock clk
set_interface_property avm associatedReset reset
set_interface_property avm addressUnits SYMBOLS
set_interface_property avm bitsPerSymbol 8
set_interface_property avm burstcountUnits WORDS
set_interface_property avm maximumPendingReadTransactions 1
set_interface_property avm maximumPendingWriteTransactions 1
set_interface_property avm waitrequestAllowance 0
set_interface_property avm linewrapBursts false
set_interface_property avm burstOnBurstBoundariesOnly false
add_interface_port avm avm_address address Output 32
add_interface_port avm avm_burstcount burstcount Output 5
add_interface_port avm avm_read read Output 1
add_interface_port avm avm_write write Output 1
add_interface_port avm avm_writedata writedata Output DATA_W
add_interface_port avm avm_byteenable byteenable Output DATA_W/8
add_interface_port avm avm_waitrequest waitrequest Input 1
add_interface_port avm avm_readdata readdata Input DATA_W
add_interface_port avm avm_readdatavalid readdatavalid Input 1
add_interface_port avm avm_writeresponsevalid writeresponsevalid Input 1
add_interface_port avm avm_response response Input 2
