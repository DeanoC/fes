// SPDX-License-Identifier: GPL-2.0-or-later
// Inferred dual-port RAM. Unregistered reads match the MiSTer altsyncram
// ZX81 dpram (outdata_reg = UNREGISTERED).

module zx81_dpram #(
    parameter DATAWIDTH = 8,
    parameter ADDRWIDTH = 8,
    parameter NUMWORDS = 1 << ADDRWIDTH,
    parameter MEM_INIT_FILE = ""
) (
    input  wire                  clock,
    input  wire [ADDRWIDTH-1:0]  address_a,
    input  wire [DATAWIDTH-1:0]  data_a,
    input  wire                  wren_a,
    output wire [DATAWIDTH-1:0]  q_a,
    input  wire [ADDRWIDTH-1:0]  address_b,
    input  wire [DATAWIDTH-1:0]  data_b,
    input  wire                  wren_b,
    output wire [DATAWIDTH-1:0]  q_b
);
    (* ramstyle = "M10K" *) reg [DATAWIDTH-1:0] ram [0:NUMWORDS-1];

    initial begin
        if (MEM_INIT_FILE != "")
            $readmemh(MEM_INIT_FILE, ram);
    end

    always @(posedge clock) begin
        if (wren_a)
            ram[address_a] <= data_a;
        if (wren_b)
            ram[address_b] <= data_b;
    end

    assign q_a = ram[address_a];
    assign q_b = ram[address_b];
endmodule
