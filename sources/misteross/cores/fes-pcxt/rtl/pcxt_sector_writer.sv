// SPDX-License-Identifier: GPL-2.0-or-later
// Commit one complete staged XT floppy sector through the SDRAM byte port.
// A sector remains busy until every byte has completed and the requester has
// observed the result. Warm machine resets do not reset this controller: a
// sector accepted before reset must finish before media can be replaced.
module pcxt_sector_writer (
    input wire clk, cold_reset,
    input wire sector_valid,
    input wire [9:0] sector_lba,
    output reg [8:0] sector_index,
    input wire [7:0] sector_data,
    output wire sector_ready, sector_error, busy,
    output reg media_changed,
    output wire storage_valid,
    output wire [18:0] storage_address,
    output wire [7:0] storage_data,
    input wire storage_ready, storage_done
);
    localparam [2:0] IDLE=0, FETCH=1, OFFER=2, WAIT_DONE=3,
                     GAP=4, ACK=5, BAD=6, RELEASE=7;
    reg [2:0] state = IDLE;
    reg [9:0] lba = 0;

    assign busy = sector_valid || state != IDLE;
    assign sector_ready = state == ACK || state == BAD;
    assign sector_error = state == BAD;
    assign storage_valid = state == OFFER;
    assign storage_address = {lba, sector_index};
    assign storage_data = sector_data;

    always @(posedge clk) begin
        media_changed <= 1'b0;
        if (cold_reset) begin
            state <= IDLE;
            lba <= 0;
            sector_index <= 0;
        end else begin
            case (state)
                IDLE: if (sector_valid) begin
                    lba <= sector_lba;
                    sector_index <= 0;
                    // The exact 360 KiB image has 720 sectors.
                    state <= sector_lba < 10'd720 ? FETCH : BAD;
                end
                // The FDC staging RAM has a registered byte output.
                FETCH: state <= OFFER;
                OFFER: if (storage_ready) state <= WAIT_DONE;
                WAIT_DONE: if (storage_done) begin
                    if (&sector_index) begin
                        media_changed <= 1'b1;
                        state <= ACK;
                    end else begin
                        sector_index <= sector_index + 1'b1;
                        state <= GAP;
                    end
                end
                // Deassert VALID for at least one clock so the storage port
                // can admit the next byte, then wait for the registered RAM.
                GAP: state <= FETCH;
                ACK, BAD: state <= RELEASE;
                RELEASE: if (!sector_valid) state <= IDLE;
                default: state <= IDLE;
            endcase
        end
    end
endmodule
