// SPDX-License-Identifier: GPL-2.0-or-later
// Reproducible original ROM, copied into SDRAM before machine release.
module pcxt_boot #(parameter ROM_FILE = "pcxt_diagnostic.hex") (
    input wire clk, cold_reset, enable,
    output reg valid,
    input wire ready, done,
    output reg [15:0] address,
    output reg [7:0] data
);
    reg [7:0] rom [0:65535];
    initial $readmemh(ROM_FILE, rom);
    // Read continuously, with no address-counter enable folded into the block
    // RAM clock enable. Address advances on completion; the waiting/valid
    // handshake leaves a full clock for this read before the next offer.
    always @(posedge clk) data <= rom[address];
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
