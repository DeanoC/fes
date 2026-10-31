// SPDX-License-Identifier: GPL-2.0-or-later
// Reproducible original ROM, copied into SDRAM before machine release.
module pcxt_boot #(parameter ROM_PREFIX = "pcxt_diagnostic") (
    input wire clk, cold_reset, enable,
    output reg valid,
    input wire ready, done,
    output reg [15:0] address,
    output reg [7:0] data
);
    // Eight local read-address copies replace one column-spanning address
    // fanout. Each copy captures the same next address as the public counter.
    wire [12:0] read_next = cold_reset ? 13'b0 :
        (done && !(&address) ? address[12:0] + 13'd1 : address[12:0]);
    wire [7:0] bank_data[0:7];
    reg [2:0] read_bank;
    always_ff @(posedge clk) read_bank <= address[15:13];
    always_comb data = bank_data[read_bank];
    generate for (genvar bank=0; bank<8; bank=bank+1) begin: banks
        reg [12:0] read_address;
        (* keep *) always_ff @(posedge clk) read_address <= read_next[12:0];
        reg [7:0] rom[0:8191];
        initial begin
            if (bank == 0) $readmemh({ROM_PREFIX, ".bank0.hex"}, rom);
            else if (bank == 1) $readmemh({ROM_PREFIX, ".bank1.hex"}, rom);
            else if (bank == 2) $readmemh({ROM_PREFIX, ".bank2.hex"}, rom);
            else if (bank == 3) $readmemh({ROM_PREFIX, ".bank3.hex"}, rom);
            else if (bank == 4) $readmemh({ROM_PREFIX, ".bank4.hex"}, rom);
            else if (bank == 5) $readmemh({ROM_PREFIX, ".bank5.hex"}, rom);
            else if (bank == 6) $readmemh({ROM_PREFIX, ".bank6.hex"}, rom);
            else if (bank == 7) $readmemh({ROM_PREFIX, ".bank7.hex"}, rom);
        end
        reg [7:0] read_data;
        always_ff @(posedge clk) read_data <= rom[read_address];
        assign bank_data[bank] = read_data;
    end endgenerate
    reg waiting, finished;
    always @(posedge clk) begin
        if (cold_reset) begin address <= 0; valid <= 0; waiting <= 0; finished <= 0; end
        else begin
            if (!waiting && !finished && enable) valid <= 1;
            if (valid && ready) begin valid <= 0; waiting <= 1; end
            if (done) begin
                waiting <= 0;
                if (&address) finished <= 1;
                else address <= address + 1'b1;
            end
        end
    end
endmodule
