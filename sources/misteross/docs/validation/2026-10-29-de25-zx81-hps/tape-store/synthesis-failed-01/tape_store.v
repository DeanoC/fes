// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 storage adaptation. Adjacent byte writes always select opposite banks.
// Each M20K bank has one write port and one common-clock OLD_DATA read port,
// preserving the portable store's one-cycle pre-write read for every enable.
`ifdef QUARTUS
module zx81_tape_store (
    input  wire        clk,
    input  wire [1:0]  unit_state,
    input  wire [31:0] unit_size,
    input  wire [13:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable,
    input  wire [13:0] tape_addr,
    output wire        tape_ready,
    output wire [14:0] tape_size,
    output wire [7:0]  tape_data
);
    wire [13:0] read_addr = write_enable[0] ? write_addr : tape_addr;
    wire [13:0] next_addr = write_addr + 14'd1;
    wire high_write = write_enable[1] && write_addr != 14'h3fff;
    wire [15:0] bank_read_data;
    reg read_odd = 1'b0;
    always @(posedge clk) read_odd <= read_addr[0];
    assign tape_ready = unit_state == 2'd3;
    assign tape_size = unit_size[14:0];
    assign tape_data = read_odd ? bank_read_data[15:8] : bank_read_data[7:0];

    genvar bank;
    generate for (bank = 0; bank < 2; bank = bank + 1) begin : banks
        wire low_selected = write_enable[0] && write_addr[0] == bank;
        wire high_selected = high_write && next_addr[0] == bank;
        wire [12:0] bank_write_addr = low_selected ? write_addr[13:1] : next_addr[13:1];
        wire [7:0] bank_write_data = low_selected ? write_data[7:0] : write_data[15:8];
        altsyncram #(
            .intended_device_family("Agilex 5"), .ram_block_type("M20K"),
            .operation_mode("DUAL_PORT"), .lpm_type("altsyncram"),
            .width_a(8), .widthad_a(13), .numwords_a(8192), .width_byteena_a(1),
            .width_b(8), .widthad_b(13), .numwords_b(8192), .width_byteena_b(1),
            .address_reg_b("CLOCK0"), .rdcontrol_reg_b("CLOCK0"),
            .outdata_reg_a("UNREGISTERED"), .outdata_reg_b("UNREGISTERED"),
            .outdata_aclr_a("NONE"), .outdata_aclr_b("NONE"),
            .clock_enable_input_a("BYPASS"), .clock_enable_input_b("BYPASS"),
            .clock_enable_output_a("BYPASS"), .clock_enable_output_b("BYPASS"),
            .power_up_uninitialized("FALSE"),
            .read_during_write_mode_mixed_ports("OLD_DATA")
        ) image (
            .clock0(clk), .clock1(1'b0),
            .address_a(bank_write_addr), .data_a(bank_write_data),
            .wren_a(low_selected || high_selected), .rden_a(1'b1), .q_a(),
            .address_b(read_addr[13:1]), .data_b(8'd0), .wren_b(1'b0),
            .rden_b(1'b1), .q_b(bank_read_data[bank * 8 +: 8]),
            .aclr0(1'b0), .aclr1(1'b0), .addressstall_a(1'b0), .addressstall_b(1'b0),
            .byteena_a(1'b1), .byteena_b(1'b1),
            .clocken0(1'b1), .clocken1(1'b1), .clocken2(1'b1), .clocken3(1'b1),
            .eccstatus()
        );
    end endgenerate
endmodule
`else
// SPDX-License-Identifier: GPL-2.0-or-later
// ZX81 .p store for fes.media.zx81-tape unit 0.
//
// A mailbox write borrows the RAM port for that clock. The machine reads
// tape_data one byte after tape_addr. tape_ready is the computer READY state.
module zx81_tape_store (
    input  wire        clk,
    input  wire [1:0]  unit_state,
    input  wire [31:0] unit_size,
    input  wire [13:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable,
    input  wire [13:0] tape_addr,
    output wire        tape_ready,
    output wire [14:0] tape_size,
    output wire [7:0]  tape_data
);
    localparam [1:0] READY = 2'd3;
    (* ram_style = "m10k_tdp" *) reg [7:0] image [0:16383];
    reg [7:0] read_data = 8'h00;
    wire [13:0] address_a = write_enable[0] ? write_addr : tape_addr;
    wire [13:0] address_b = write_addr + 14'd1;
    assign tape_ready = unit_state == READY;
    assign tape_size = unit_size[14:0];
    assign tape_data = read_data;

    always @(posedge clk) begin
        if (write_enable[0])
            image[address_a] <= write_data[7:0];
        read_data <= image[address_a];
    end

    always @(posedge clk)
        if (write_enable[1] && write_addr != 14'h3fff)
            image[address_b] <= write_data[15:8];
endmodule
`endif
