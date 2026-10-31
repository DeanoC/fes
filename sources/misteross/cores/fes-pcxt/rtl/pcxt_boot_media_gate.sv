// SPDX-License-Identifier: GPL-2.0-or-later
// Wait for the first fully committed runtime floppy after a cold start, unless
// the host releases execution with the drive empty. Once boot starts, eject
// and replacement must not reset the running machine.
module pcxt_boot_media_gate #(parameter bit ENABLE = 0) (
    input wire clk, cold_reset, media_present, start_without_media,
    output wire hold
);
    reg started;
    always @(posedge clk) begin
        if (cold_reset) started <= 1'b0;
        else if (media_present || start_without_media) started <= 1'b1;
    end
    assign hold = ENABLE && !started;
endmodule
