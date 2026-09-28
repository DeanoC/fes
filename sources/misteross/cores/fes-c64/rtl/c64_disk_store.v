// SPDX-License-Identifier: GPL-2.0-or-later
// Media unit 0: one 174,848-byte D64 image. The fes.computer mailbox writes
// accepted byte pairs (low byte at addr, high byte at addr+1). The 1541
// model reads one byte at a time and only presents the image while the unit
// is ready.
module c64_disk_store (
    input  wire        clk,
    input  wire [17:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable,
    input  wire [17:0] read_addr,
    output reg  [7:0]  read_data
);
    localparam integer BYTES = 174848;
    (* ram_style = "m10k_tdp" *) reg [7:0] image [0:BYTES-1];
    wire [17:0] address_a = write_enable[0] ? write_addr : read_addr;
    wire [17:0] address_b = write_addr + 18'd1;

`ifdef VERILATOR
    initial $readmemh("build/diagnostics/fes-c64/disk.hex", image);
`endif

    always @(posedge clk) begin
        if (write_enable[0])
            image[address_a] <= write_data[7:0];
        read_data <= image[address_a];
    end

    always @(posedge clk)
        if (write_enable[1])
            image[address_b] <= write_data[15:8];
endmodule
