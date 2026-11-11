// The header's check mechanism: a failed check expands a deliberately undefined macro.
// CENSUS_FRONT_END is never defined, so the check fails. A reader that stops here passes.
`ifndef CENSUS_FRONT_END
`FES_TOOLCHAIN_ERROR_no_front_end
`endif
module f11_undefined_macro (output logic ok);
    assign ok = 1'b1;
endmodule
