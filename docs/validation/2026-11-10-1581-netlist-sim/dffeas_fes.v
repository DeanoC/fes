// S1c trial model, not production code: a behavioural stand-in for Intel's dffeas
// (altera_primitives.v), whose state is the sequential UDP PRIM_GDFF_LOW/HIGH. Verilator
// 5.032 does not support UDP tables. This follows the UDP's table rows in priority
// order: asynchronous clear, preset, load; then on a rising clock with ena: sclr, sload,
// d. The notifier (timing-violation X) row has no meaning in a functional netlist and is
// not modelled. Asynchronous load samples asdata at the rising edge of aload only (aload
// is tied low on every register of the netlists tried). sclr_over_ena = "true" (the
// *_SCLR_PRIORITY UDPs) stops the simulation.
`timescale 1 ps / 1 ps
module dffeas (
    input d, clk, ena, clrn, prn, aload, asdata, sclr, sload, devclrn, devpor,
    output reg q
);
    parameter power_up = "DONT_CARE";
    parameter is_wysiwyg = "false";
    parameter dont_touch = "false";
    parameter x_on_violation = "on";
    parameter sclr_over_ena = "false";
    parameter lpm_type = "dffeas";
    initial begin
        if (sclr_over_ena == "true") begin
            $fatal(1, "dffeas_fes: unsupported configuration %m");
        end
        q = power_up == "high";
    end
    wire clear = !clrn || !devpor || !devclrn;
    always @(posedge clk or posedge clear or negedge prn or posedge aload) begin
        if (clear) q <= 1'b0;
        else if (!prn) q <= 1'b1;
        else if (aload) q <= asdata;
        else if (ena) begin
            if (sclr) q <= 1'b0;
            else if (sload) q <= asdata;
            else q <= d;
        end
    end
endmodule
