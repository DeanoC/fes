# The preserved GHRD PLL owns the source clock and its generated timing.
set pr_clock [get_clocks {pll_inst|iopll_0_outclk0}]
if {[get_collection_size $pr_clock] != 1 || [get_clock_info -period $pr_clock] != 10.0} {
    error "HPS PR diagnostic requires the preserved 100 MHz GHRD PLL clock"
}
post_message -type info "HPS PR diagnostic: preserved 100 MHz clock; no added timing exceptions"
