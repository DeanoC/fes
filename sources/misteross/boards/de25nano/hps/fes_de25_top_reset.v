// Common board reset; generated subsystem controllers retain their own release.
// PLL loss/configuration reset asserts immediately, including a stopped clock.
// Only these synchronization registers receive the asynchronous reset source;
// functional registers receive reset_n with clock-synchronous deassertion.
module fes_de25_top_reset (
    input  wire clk,
    input  wire ninit_done,
    input  wire pll_locked,
    output wire reset_n
);
    wire async_reset_n = ~ninit_done & pll_locked;

    // Let Quartus recognize the actual reset chain. Forcing identification on
    // the whole vector incorrectly declares each member a separate chain head.
    (* preserve, dont_merge *) reg [1:0] sync_regs;

    always @(posedge clk or negedge async_reset_n) begin
        if (!async_reset_n)
            sync_regs <= 2'b00;
        else
            sync_regs <= {sync_regs[0], 1'b1};
    end

    assign reset_n = sync_regs[1];
endmodule
