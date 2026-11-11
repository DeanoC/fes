// Check mechanism (b): an elaboration-time $error in a generate block (a module-level macro),
// inside generate/endgenerate so that Lite's generate-keyword rule does not decide the result.
module f12c_check_elab_error (output logic ok);
`ifndef CENSUS_FRONT_END
    generate
        if (1) begin : g_fes_toolchain_check
            $error("FES_TOOLCHAIN_ERROR_no_front_end");
        end
    endgenerate
`endif
    assign ok = 1'b1;
endmodule
