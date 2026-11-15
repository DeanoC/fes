package require -exact qsys 20.1
set_module_property NAME fes_de25_rom
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES held computer ROM"
set_module_property GROUP "FES/Diagnostics"
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_parameter ROM_BYTES INTEGER 8192
set_parameter_property ROM_BYTES ALLOWED_RANGES {4096 8192}
set_parameter_property ROM_BYTES HDL_PARAMETER true
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_rom
add_fileset_file fes_de25_rom.sv SYSTEM_VERILOG PATH fes_de25_rom.sv TOP_LEVEL_FILE
add_fileset_file fes_embedded_rom_csr.sv SYSTEM_VERILOG PATH fes_embedded_rom_csr.sv
add_fileset_file fes_embedded_rom.sv SYSTEM_VERILOG PATH fes_embedded_rom.sv
add_fileset_file generated/fes_embedded_rom.vh VERILOG_INCLUDE PATH generated/fes_embedded_rom.vh
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
set_interface_property avs explicitAddressSpan 128
set_interface_property avs maximumPendingReadTransactions 1
set_interface_property avs maximumPendingWriteTransactions 1
set_interface_property avs minimumResponseLatency 1
set_interface_property avs waitrequestAllowance 0
set_interface_property avs readLatency 0
set_interface_property avs readWaitTime 0
set_interface_property avs writeWaitTime 0
set_interface_property avs isMemoryDevice false
foreach {port role direction width} {
    address address Input 7 read read Input 1 write write Input 1
    byteenable byteenable Input 4 writedata writedata Input 32
    readdata readdata Output 32 readdatavalid readdatavalid Output 1
    writeresponsevalid writeresponsevalid Output 1 response response Output 2
    waitrequest waitrequest Output 1
} { add_interface_port avs $port $role $direction $width }
add_interface rom conduit end
set_interface_property rom associatedClock clk
set_interface_property rom associatedReset reset
foreach {port role direction width} {
    rom_address address Input 13 rom_data data Output 8
    rom_cpu_held held Input 1 rom_loaded loaded Output 1 rom_failed failed Output 1
} { add_interface_port rom $port $role $direction $width }
