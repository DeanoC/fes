// S1c trial model, not production code: a plain buffer in place of Intel's
// cyclonev_io_ibuf. Intel's model tests `i === 1'bz`, and Verilator 5.032 then refuses
// every top-level input the .vo buffers ("Unsupported: tristate in top-level IO"), also
// through a wrapper module. Only the single-ended, no-bus-hold form the functional .vo
// uses is modelled.
`timescale 1 ps / 1 ps
module cyclonev_io_ibuf (
    input i,
    input ibar,
    input dynamicterminationcontrol,
    output o
);
    parameter differential_mode = "false";
    parameter bus_hold = "false";
    parameter simulate_z_as = "Z";
    parameter lpm_type = "cyclonev_io_ibuf";
    initial if (differential_mode != "false" || bus_hold != "false") begin
        $fatal(1, "cyclonev_io_ibuf_fes: unsupported configuration %m");
    end
    assign o = i;
    wire unused = &{1'b0, ibar, dynamicterminationcontrol};
endmodule
