// SPDX-License-Identifier: GPL-2.0-or-later
// One outstanding held token. Neither clock is stopped or reset by HOLD.
// Payload remains stable until the destination acknowledges its toggle.
module fes_native_cdc (
    input wire source_clk,
    input wire pixel_clk,
    input wire [31:0] source_request,
    output reg [31:0] request = 0
);
    reg [31:0] payload = 0;
    reg sent = 0, received = 0;
    (* async_reg = "true" *) reg ack_meta = 0, ack_sync = 0;
    (* async_reg = "true" *) reg send_meta = 0, send_sync = 0;
    reg overflow = 0;
    reg control_pending = 0, control_hold = 0;
    wire source_control_ok = source_request[24] && source_request[29] &&
                             source_request[31:30] == 0 && source_request[27:0] == 28'h1000000;
    always @(posedge source_clk) begin
        ack_meta <= received;
        ack_sync <= ack_meta;
        if (sent == ack_sync) begin
            if (control_pending) begin
                // Deliver the latest valid control even if an excessive
                // source rate keeps producing overrun invalidations.
                payload <= {2'b0, 1'b1, control_hold, 3'b0, 1'b1, 24'b0};
                sent <= !sent;
                control_pending <= source_control_ok;
                control_hold <= source_request[28];
                overflow <= overflow || (source_request[24] && !source_control_ok);
            end else if (overflow) begin
                // An explicit invalid token poisons partial capture. A lost
                // source pixel must never produce an apparently complete frame.
                payload <= 32'h41000000;
                sent <= !sent;
                overflow <= source_request[24];
                if (source_control_ok) begin
                    control_pending <= 1;
                    control_hold <= source_request[28];
                end
            end else if (source_request[24]) begin
                payload <= source_request;
                sent <= !sent;
            end
        end else if (source_request[24]) begin
            overflow <= 1;
            if (source_control_ok) begin
                control_pending <= 1;
                control_hold <= source_request[28];
            end
        end
    end
    always @(posedge pixel_clk) begin
        send_meta <= sent;
        send_sync <= send_meta;
        request <= 0;
        if (send_sync != received) begin
            request <= payload;
            received <= send_sync;
        end
    end
endmodule
