// The board supplies the clock input to divider 2 as a synchronous enable.
module fes_sh7091_wdt (
    input  logic        clk, reset, tick,
    input  logic        standby, standby_wake,
    output logic        standby_ready,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [15:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    output logic        interval_irq,
    output logic [1:0]  reset_request
);
    logic [7:0] counter, control;
    logic [11:0] prescale;
    logic [11:0] divisor_mask;
    logic write_count, write_control, counting, startup;

    assign selected = address[31:4] == 28'hffc0000 && address[3];
    assign write_count = request && selected && !address[2] && wstrb == 4'h3 && wdata[15:8] == 8'h5a;
    assign write_control = request && selected && address[2] && wstrb == 4'h3 && wdata[15:8] == 8'ha5;
    assign divisor_mask = 12'hfff >> (3'd7 - control[2:0]);
    assign counting = standby ? !standby_ready && (standby_wake || startup) : control[7];
    assign interval_irq = control[3];

    always_ff @(posedge clk) begin
        if (reset) begin
            counter <= 0;
            control <= 0;
            prescale <= 0;
            rvalid <= 0;
            reset_request <= 0;
            startup <= 0;
            standby_ready <= 0;
        end else begin
            rvalid <= request && selected && wstrb == 0;
            if (request && selected && wstrb == 0)
                rdata <= rmask == 4'h1 ? {24'b0, address[2] ? control : counter} : 32'b0;
            reset_request <= 0;

            if (write_control)
                control <= {wdata[7:5], control[4:3] & wdata[4:3], wdata[2:0]};
            if (!standby) begin
                startup <= 0;
                standby_ready <= 0;
            end else if (standby_wake)
                startup <= 1;

            if (!counting)
                prescale <= 0;
            else if (tick)
                prescale <= prescale + 12'd1;

            if (write_count)
                counter <= wdata[7:0];
            else if (counting && tick && (prescale & divisor_mask) == divisor_mask) begin
                counter <= counter + 8'd1;
                if (&counter) begin
                    if (standby)
                        standby_ready <= 1;
                    else if (control[6]) begin
                        control[4] <= 1;
                        reset_request <= control[5] ? 2'b10 : 2'b01;
                    end else
                        control[3] <= 1;
                end
            end
        end
    end
endmodule
