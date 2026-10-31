# The preserved GHRD PLL owns the source clock and its generated timing.
set pr_clock [get_clocks {pll_inst|iopll_0_outclk0}]
if {[get_collection_size $pr_clock] != 1 || [get_clock_info -period $pr_clock] != 10.0} {
    error "HPS PR diagnostic requires the preserved 100 MHz GHRD PLL clock"
}
set pr_emif_reference [get_clocks {soc_inst|subsys_hps|emif_hps|emif_io96b_hps|emif_0_lpddr4|emif_0_lpddr4_0_ref_clock}]
if {[get_collection_size $pr_emif_reference] != 1 || [get_clock_info -period $pr_emif_reference] != 6.0} {
    error "HPS PR diagnostic requires the preserved generated 6 ns EMIF reference clock"
}
post_message -type info "HPS PR diagnostic: preserved 100 MHz clock; no added timing exceptions"
