# Expected behavior

The reduced simulation overrides `COUNTER_BITS` to 4. After each rising edge,
the settled counter value determines the LED level: counts 0 through 7 drive
`LED[0]` low, counts 8 through 15 drive it high, and the value wraps to 0 on
the sixteenth edge. The 24-edge test therefore observes the high transition at
count 8, the low transition at wrap, and verifies the next high phase as well.

With the production default `COUNTER_BITS=25` and a 50 MHz input, one complete
LED waveform cycle takes `2^25 / 50,000,000`, approximately 0.671 seconds.
The expected values describe the FPGA pin level; this file does not infer a
visual LED polarity for any other board.
