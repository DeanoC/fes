// S1c trial model, not production code: a plain buffer in place of Intel's
// cyclonev_io_obuf. Intel's model drives the pad through 'bz assignments and a pmos
// primitive. Verilator 5.032 compiles it without an error, but the pad then keeps a 1
// after the register behind it returns to 0 (seen on media_addr of sms_machine). Only
// the always-driven push-pull form is modelled: the output is 0 when oe is low, where
// Intel's model gives Z.
`timescale 1 ps / 1 ps
module cyclonev_io_obuf (
    input i,
    input oe,
    input dynamicterminationcontrol,
    input [15:0] seriesterminationcontrol,
    input [15:0] parallelterminationcontrol,
    input devoe,
    output o,
    output obar
);
    parameter open_drain_output = "false";
    parameter bus_hold = "false";
    parameter shift_series_termination_control = "false";
    parameter sim_dynamic_termination_control_is_connected = "false";
    parameter lpm_type = "cyclonev_io_obuf";
    initial if (open_drain_output != "false" || bus_hold != "false") begin
        $fatal(1, "cyclonev_io_obuf_fes: unsupported configuration %m");
    end
    assign o = oe & i;
    assign obar = oe & !i;
    wire unused = &{1'b0, dynamicterminationcontrol, seriesterminationcontrol,
                    parallelterminationcontrol, devoe};
endmodule
