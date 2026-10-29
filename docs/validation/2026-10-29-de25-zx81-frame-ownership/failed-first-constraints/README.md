# First routed candidate: missing metadata bus-skew coverage

Source `67068382b7a12047dad227077bd57c4b05e7fec3` compiled and passed the
four-corner owner setup/hold audit, but final Design Assistant found one new
unwaived CDC-50003 on the nine metadata bits. Net delay was constrained; maximum
skew and datapath-only delay were not. This candidate is not the delivered SOF.

A private copy of its final database, with additional 10 ns `set_max_skew` and
`set_data_delay` constraints, passes normal Quartus static timing and clears
CDC-50003 with zero waivers. Setup, hold, datapath delay, skew and net delay
remain constrained. The publication rebuild uses committed source `ec14fda296`
with those constraints, without changing RTL, clock rates, seed or timeouts.
