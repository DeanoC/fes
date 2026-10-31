// DE25-Nano FPGA-only diagnostic. Terasic's user LEDs are active low.
module de25nano_blinky (
    input wire CLOCK0_50,
    output wire [7:0] LEDR
);
    wire ninit_done;
    reset_release u_reset_release (.ninit_done(ninit_done));

    // Agilex fabric sectors do not all enter user mode together. Hold the
    // counter until the Reset Release IP confirms initialization, then
    // release reset synchronously in the 50 MHz domain.
    reg [1:0] release_sync = 2'b11;
    always @(posedge CLOCK0_50 or posedge ninit_done) begin
        if (ninit_done)
            release_sync <= 2'b11;
        else
            release_sync <= {release_sync[0], 1'b0};
    end
    wire reset = release_sync[1];

    reg [24:0] divider = 25'd0;
    reg [7:0] count = 8'd0;

    always @(posedge CLOCK0_50 or posedge reset) begin
        if (reset) begin
            divider <= 25'd0;
            count <= 8'd0;
        end else if (divider == 25'd24999999) begin
            divider <= 25'd0;
            count <= count + 8'd1;
        end else begin
            divider <= divider + 25'd1;
        end
    end

    assign LEDR = ~count;
endmodule
