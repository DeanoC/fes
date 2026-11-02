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
    // Quartus cannot keep port A's old-data read during a write; a write
    // borrows the port, so that read is never used.
`ifdef QUARTUS
    (* ramstyle = "M10K, no_rw_check" *)
`else
    (* ram_style = "m10k_tdp" *)
`endif
    reg [7:0] image [0:16383];
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
