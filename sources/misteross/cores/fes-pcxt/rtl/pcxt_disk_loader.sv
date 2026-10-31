// SPDX-License-Identifier: GPL-2.0-or-later
// Count completed ordered writes for one raw 360 KiB image. A new media
// transaction invalidates the old image; accepted writes still drain.
module pcxt_disk_loader (
    input wire clk, cold_reset, restart,
    input wire load_valid,
    output wire load_ready, load_done,
    input wire [18:0] load_address,
    output reg loaded,
    output wire storage_valid,
    input wire storage_ready, storage_done
);
    localparam [18:0] DISK_LAST = 19'h59fff;
    reg [18:0] next_address;
    reg loading, cancelled;
    assign storage_valid = load_valid && !restart && !loaded && !loading && load_address == next_address;
    assign load_ready = storage_ready && !restart && !loaded && !loading && load_address == next_address;
    assign load_done = storage_done;
    always @(posedge clk) begin
        if (cold_reset) begin loaded <= 0; next_address <= 0; loading <= 0; cancelled <= 0; end
        else if (restart) begin
            loaded <= 0; next_address <= 0;
            cancelled <= loading && !storage_done;
            if (storage_done) loading <= 0;
        end else begin
            if (storage_valid && storage_ready) loading <= 1;
            if (storage_done) begin
                loading <= 0;
                if (cancelled) cancelled <= 0;
                else if (next_address == DISK_LAST) loaded <= 1;
                else next_address <= next_address + 1'b1;
            end
        end
    end
endmodule
