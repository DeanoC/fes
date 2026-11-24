# quartus_sta -t paths.tcl REVISION OUTDIR, in a fitted project directory: method.md's path reports.
# For every operating condition (corner):
#   machine-setup-<corner>.rpt  the 20,000 worst machine-clock to machine-clock setup paths, one per
#                               endpoint (-nworst 1), summary detail (slack, nodes, clocks,
#                               relationship, skew, data delay);
#   cross-setup-<corner>.rpt    the 2,000 worst setup paths into or out of the machine clock from any
#                               other clock;
#   negative-<check>-<corner>.rpt  every path with negative slack, for setup, hold, recovery and
#                               removal, all clocks, up to 5,000 each.
# It reads the kept fit; it changes nothing in the project.
set revision [lindex $argv 0]
set out [lindex $argv 1]
file mkdir $out
project_open $revision -revision $revision
create_timing_netlist
read_sdc
update_timing_netlist
set machine [get_clocks {fes_machine_pll|machine_pll_outclk0}]
if {[get_collection_size $machine] != 1} { error "no machine clock" }
set others [remove_from_collection [all_clocks] $machine]
foreach condition [get_available_operating_conditions] {
    set_operating_conditions $condition
    update_timing_netlist
    set tag [string map {" " _} $condition]
    report_timing -setup -npaths 20000 -nworst 1 -detail summary -from_clock $machine -to_clock $machine \
        -file $out/machine-setup-$tag.rpt
    report_timing -setup -npaths 2000 -nworst 1 -detail summary -from_clock $machine -to_clock $others \
        -file $out/cross-out-setup-$tag.rpt
    report_timing -setup -npaths 2000 -nworst 1 -detail summary -from_clock $others -to_clock $machine \
        -file $out/cross-in-setup-$tag.rpt
    foreach check {setup hold recovery removal} {
        report_timing -$check -npaths 5000 -nworst 1 -less_than_slack 0 -detail summary \
            -file $out/negative-$check-$tag.rpt
    }
}
delete_timing_netlist
project_close
