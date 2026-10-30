// SPDX-License-Identifier: GPL-2.0-or-later
// Load the sealed 360 KiB DOS disk image through pcxt_system's physical SDRAM
// loader before the machine is released. The ROM is only the source of the
// upload; guest reads continue through the FDC's SDRAM media-read path.
module pcxt_disk_boot #(
    parameter IMAGE_FILE = "pcxt_freedos_disk.hex",
    parameter [18:0] IMAGE_LAST = 19'h59fff
) (
    input wire clk, cold_reset, enable,
    output reg valid,
    input wire ready, done,
    output reg [18:0] address,
    output reg [7:0] data,
    output reg finished
);
    reg [18:0] read_address;
    wire [18:0] read_next = cold_reset ? 19'b0 :
        (done && address != IMAGE_LAST ? address + 19'd1 : address);
    reg [7:0] image [0:IMAGE_LAST];

    initial $readmemh(IMAGE_FILE, image);

    // Synchronous ROM read lets Quartus infer M10K blocks. Look one byte
    // ahead on each completed SDRAM write so DATA is stable when VALID rises.
    always @(posedge clk) begin
        read_address <= read_next;
        data <= image[read_address];
    end

    reg waiting;
    always @(posedge clk) begin
        if (cold_reset) begin
            address <= 0;
            valid <= 0;
            waiting <= 0;
            finished <= 0;
        end else begin
            if (!waiting && !valid && !finished && enable) valid <= 1;
            if (valid && ready) begin
                valid <= 0;
                waiting <= 1;
            end
            if (done) begin
                waiting <= 0;
                if (address == IMAGE_LAST) finished <= 1;
                else address <= address + 1'b1;
            end
        end
    end
endmodule
