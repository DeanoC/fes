// S1c trial model, not production code: a behavioural stand-in for Intel's
// cyclonev_ram_block (generic_m10k / common_28nm_ram_block in altera_lnsim.sv). Intel's
// model makes its read and write pulses from 1 ps and 2 ps delays, so it does nothing
// under Verilator --no-timing, and under --timing Verilator 5.032 rejects it
// (BLKANDNBLK on dataout_a, dataout_b and the read latches).
//
// The enable and register structure follows Intel's source: address, data and byte
// enable register with the input clock enable; write and read enable register with the
// core clock enable; a port acts on a clock edge when its core clock enable is high at
// that edge. A read (re and not we) loads the output latch, a write with re (not on a
// dual_port block) feeds the new data through, and the optional output register follows
// the latch. Only what the functional .vo files of this spike use is modelled: one data
// bit per block, no clears, no ECC, mem_init0 only. Anything else stops the simulation.
//
// Difference from Intel's model, not verified against it: a read of the address the other
// port writes on the same clock edge returns the old data in every mode. Intel returns
// old data for mixed_port_feed_through_mode "old" and X for "dont_care".
`timescale 1 ps / 1 ps
module cyclonev_ram_block (
    portadatain, portaaddr, portawe, portare, portbdatain, portbaddr, portbwe, portbre,
    clk0, clk1, ena0, ena1, ena2, ena3, clr0, clr1, nerror,
    portabyteenamasks, portbbyteenamasks, portaaddrstall, portbaddrstall,
    devclrn, devpor, eccstatus, portadataout, portbdataout, dftout
);
    parameter operation_mode = "single_port";
    parameter mixed_port_feed_through_mode = "dont_care";
    parameter ram_block_type = "auto";
    parameter logical_ram_name = "ram_name";
    parameter init_file = "init_file.hex";
    parameter init_file_layout = "none";
    parameter ecc_pipeline_stage_enabled = "false";
    parameter enable_ecc = "false";
    parameter width_eccstatus = 2;
    parameter data_interleave_width_in_bits = 1;
    parameter data_interleave_offset_in_bits = 1;
    parameter port_a_logical_ram_depth = 0;
    parameter port_a_logical_ram_width = 0;
    parameter port_a_first_address = 0;
    parameter port_a_last_address = 0;
    parameter port_a_first_bit_number = 0;
    parameter port_a_data_out_clear = "none";
    parameter port_a_data_out_clock = "none";
    parameter port_a_data_width = 1;
    parameter port_a_address_width = 1;
    parameter port_a_byte_enable_mask_width = 1;
    parameter port_b_logical_ram_depth = 0;
    parameter port_b_logical_ram_width = 0;
    parameter port_b_first_address = 0;
    parameter port_b_last_address = 0;
    parameter port_b_first_bit_number = 0;
    parameter port_b_address_clear = "none";
    parameter port_b_data_out_clear = "none";
    parameter port_b_data_in_clock = "clock1";
    parameter port_b_address_clock = "clock1";
    parameter port_b_write_enable_clock = "clock1";
    parameter port_b_read_enable_clock = "clock1";
    parameter port_b_byte_enable_clock = "clock1";
    parameter port_b_data_out_clock = "none";
    parameter port_b_data_width = 1;
    parameter port_b_address_width = 1;
    parameter port_b_byte_enable_mask_width = 1;
    parameter port_a_read_during_write_mode = "new_data_no_nbe_read";
    parameter port_b_read_during_write_mode = "new_data_no_nbe_read";
    parameter power_up_uninitialized = "false";
    parameter lpm_type = "cyclonev_ram_block";
    parameter lpm_hint = "true";
    parameter connectivity_checking = "off";
    parameter mem_init0 = "";
    parameter mem_init1 = "";
    parameter mem_init2 = "";
    parameter mem_init3 = "";
    parameter mem_init4 = "";
    parameter port_a_byte_size = 0;
    parameter port_b_byte_size = 0;
    parameter clk0_input_clock_enable = "none";
    parameter clk0_core_clock_enable = "none";
    parameter clk0_output_clock_enable = "none";
    parameter clk1_input_clock_enable = "none";
    parameter clk1_core_clock_enable = "none";
    parameter clk1_output_clock_enable = "none";
    parameter bist_ena = "false";
    parameter port_a_address_clear = "none";
    parameter port_a_data_in_clock = "clock0";
    parameter port_a_address_clock = "clock0";
    parameter port_a_write_enable_clock = "clock0";
    parameter port_a_byte_enable_clock = "clock0";
    parameter port_a_read_enable_clock = "clock0";

    input portawe, portare, portbwe, portbre;
    input [port_a_data_width - 1:0] portadatain;
    input [port_a_address_width - 1:0] portaaddr;
    input [port_a_byte_enable_mask_width - 1:0] portabyteenamasks;
    input [port_b_data_width - 1:0] portbdatain;
    input [port_b_address_width - 1:0] portbaddr;
    input [port_b_byte_enable_mask_width - 1:0] portbbyteenamasks;
    input clr0, clr1, clk0, clk1, ena0, ena1, ena2, ena3, nerror, devclrn, devpor;
    input portaaddrstall, portbaddrstall;
    output [port_a_data_width - 1:0] portadataout;
    output [port_b_data_width - 1:0] portbdataout;
    output [width_eccstatus - 1:0] eccstatus;
    output [8:0] dftout;

    localparam AW = port_a_address_width;
    localparam DEPTH = 1 << AW;
    localparam MODE_ROM = operation_mode == "rom";
    localparam MODE_DP = operation_mode == "dual_port";
    localparam MODE_BDP = operation_mode == "bidir_dual_port";
    localparam HAS_B = MODE_DP || MODE_BDP;
    localparam B_ON_CLK0 = port_b_address_clock == "clock0";
    localparam INIT = init_file_layout == "port_a" || init_file_layout == "port_b";
    localparam INIT_CHARS = (DEPTH + 3) / 4;
    localparam [8 * 4096 - 1:0] INIT_TEXT = mem_init0;

    reg mem [0:DEPTH - 1];
    reg [AW - 1:0] addr_a, addr_b;
    reg din_a, din_b, be_a, be_b, we_a, we_b, re_a, re_b;
    reg latch_a, latch_b, out_a, out_b;

    function [3:0] nibble(input [7:0] c);
        nibble = (c >= "0" && c <= "9") ? c[3:0] :
                 (c >= "A" && c <= "F") ? c[3:0] + 4'd9 :
                 (c >= "a" && c <= "f") ? c[3:0] + 4'd9 : 4'd0;
    endfunction

    integer i;
    reg [3:0] value;
    initial begin
        if (port_a_data_width != 1 || (HAS_B && port_b_data_width != 1) ||
            (HAS_B && port_b_address_width != AW) ||
            !(MODE_ROM || MODE_DP || MODE_BDP || operation_mode == "single_port") ||
            port_a_address_clear != "none" || port_a_data_out_clear != "none" ||
            (HAS_B && (port_b_address_clear != "none" || port_b_data_out_clear != "none")) ||
            enable_ecc != "false" || bist_ena != "false" ||
            port_a_read_during_write_mode != "new_data_no_nbe_read" ||
            (HAS_B && port_b_read_during_write_mode != "new_data_no_nbe_read") ||
            (HAS_B && port_b_read_enable_clock != port_b_address_clock) ||
            (MODE_BDP && (port_b_data_in_clock != port_b_address_clock ||
                          port_b_write_enable_clock != port_b_address_clock)) ||
            (INIT && (DEPTH > 4 * 4096 || mem_init1 != "" || mem_init2 != "" ||
                      mem_init3 != "" || mem_init4 != ""))) begin
            $fatal(1, "cyclonev_ram_block_fes: unsupported configuration %m");
        end
        for (i = 0; i < DEPTH; i = i + 1) mem[i] = 1'b0;
        if (INIT) begin
            // The last character of the string holds addresses 3..0.
            for (i = 0; i < INIT_CHARS; i = i + 1) begin
                value = nibble(INIT_TEXT[8 * i +: 8]);
                if (4 * i + 0 < DEPTH) mem[4 * i + 0] = value[0];
                if (4 * i + 1 < DEPTH) mem[4 * i + 1] = value[1];
                if (4 * i + 2 < DEPTH) mem[4 * i + 2] = value[2];
                if (4 * i + 3 < DEPTH) mem[4 * i + 3] = value[3];
            end
        end
        addr_a = 0; din_a = 0; be_a = 0; we_a = 0; re_a = 0; latch_a = 0; out_a = 0;
        addr_b = 0; din_b = 0; be_b = 0; we_b = 0; re_b = 0; latch_b = 0; out_b = 0;
    end

    wire in_a = clk0_input_clock_enable == "none" ? 1'b1 :
                clk0_input_clock_enable == "ena0" ? ena0 : ena2;
    wire core_a = clk0_core_clock_enable == "none" ? 1'b1 :
                  clk0_core_clock_enable == "ena0" ? ena0 : ena2;
    wire in_b = B_ON_CLK0 ? in_a :
                clk1_input_clock_enable == "none" ? 1'b1 :
                clk1_input_clock_enable == "ena1" ? ena1 : ena3;
    wire core_b = B_ON_CLK0 ? core_a :
                  clk1_core_clock_enable == "none" ? 1'b1 :
                  clk1_core_clock_enable == "ena1" ? ena1 : ena3;
    wire clk_b = B_ON_CLK0 ? clk0 : clk1;

    always @(posedge clk0) begin
        if (in_a) begin
            if (!portaaddrstall) addr_a = portaaddr;
            din_a = portadatain[0];
            be_a = portabyteenamasks[0];
        end
        if (core_a) begin
            we_a = !MODE_ROM && port_a_write_enable_clock != "none" && portawe;
            re_a = port_a_read_enable_clock != "none" && portare;
            if (we_a && be_a) mem[addr_a] <= din_a;
            if (re_a && !we_a) latch_a <= mem[addr_a];
            else if (re_a && !MODE_DP) latch_a <= din_a;
        end
    end

    generate
        if (HAS_B) begin : port_b
            always @(posedge clk_b) begin
                if (in_b) begin
                    if (!portbaddrstall) addr_b = portbaddr;
                    din_b = portbdatain[0];
                    be_b = portbbyteenamasks[0];
                end
                if (core_b) begin
                    we_b = MODE_BDP && portbwe;
                    re_b = portbre;
                    if (we_b && be_b) mem[addr_b] <= din_b;
                    if (re_b && !we_b) latch_b <= mem[addr_b];
                    else if (re_b) latch_b <= din_b;
                end
            end
        end
    endgenerate

    wire out_clk_a = port_a_data_out_clock == "none" ? 1'b0 :
                     port_a_data_out_clock == "clock0" ? clk0 : clk1;
    wire out_ena_a = port_a_data_out_clock == "clock0" ?
                     (clk0_output_clock_enable == "none" ? 1'b1 : ena0) :
                     (clk1_output_clock_enable == "none" ? 1'b1 : ena1);
    wire out_clk_b = port_b_data_out_clock == "none" ? 1'b0 :
                     port_b_data_out_clock == "clock0" ? clk0 : clk1;
    wire out_ena_b = port_b_data_out_clock == "clock0" ?
                     (clk0_output_clock_enable == "none" ? 1'b1 : ena0) :
                     (clk1_output_clock_enable == "none" ? 1'b1 : ena1);
    always @(posedge out_clk_a) if (out_ena_a) out_a <= latch_a;
    always @(posedge out_clk_b) if (out_ena_b) out_b <= latch_b;

    assign portadataout = port_a_data_out_clock == "none" ? latch_a : out_a;
    assign portbdataout = port_b_data_out_clock == "none" ? latch_b : out_b;
    assign eccstatus = {width_eccstatus{1'b0}};
    assign dftout = 9'b0;
    wire unused = &{1'b0, clr0, clr1, nerror, devclrn, devpor};
endmodule
