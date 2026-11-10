// Check mechanism (b): an elaboration-time $error in a generate block (a module-level macro).
module f12c_check_elab_error (output logic ok);
`ifndef CENSUS_FRONT_END
    if (1) begin : g_fes_toolchain_check
        $error("FES_TOOLCHAIN_ERROR_no_front_end");
    end
`endif
    assign ok = 1'b1;
endmodule
