# Terasic DE25-Nano CLOCK0_50: 50 MHz, 20 ns.
create_clock -name CLOCK0_50 -period 20.000 [get_ports {CLOCK0_50}]
derive_clock_uncertainty
# LEDs have no external synchronous receiver or board timing contract.
# Keep their paths visible as unconstrained IO in the diagnostic STA report.
