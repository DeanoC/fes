// Board-local reset for the vendor top's 100 MHz debounce and heartbeat.
// PLL loss/configuration reset asserts immediately, including a stopped clock.
// Only these synchronization registers receive the asynchronous reset source;
// functional registers receive reset_n with clock-synchronous deassertion.
module fes_de25_top_reset (
    input  wire clk,
    input  wire async_reset_n,
    output wire reset_n
);
    (* preserve, dont_merge,
       altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED" *)
    reg [1:0] sync_regs;

    always @(posedge clk or negedge async_reset_n) begin
        if (!async_reset_n)
            sync_regs <= 2'b00;
        else
            sync_regs <= {sync_regs[0], 1'b1};
    end

    assign reset_n = sync_regs[1];
endmodule
