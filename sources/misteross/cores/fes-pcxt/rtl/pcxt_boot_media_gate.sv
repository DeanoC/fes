// SPDX-License-Identifier: GPL-2.0-or-later
// Wait for the first fully committed runtime floppy after a cold start. Once
// boot starts, eject and replacement must not reset the running machine.
module pcxt_boot_media_gate #(parameter bit ENABLE = 0) (
    input wire clk, cold_reset, media_present,
    output wire hold
);
    reg started;
    always @(posedge clk) begin
        if (cold_reset) started <= 1'b0;
        else if (media_present) started <= 1'b1;
    end
    assign hold = ENABLE && !started;
endmodule
