#!/usr/bin/env python3
"""make_rung.py TREE C: set the DE25 pc386's machine clock to VCO / C in the checkout TREE.

Scratch only (method.md): it is applied to a rung branch, never to a branch that merges.
The machine PLL keeps its VCO, 50 MHz x 188 / 3 = 3,133.333... MHz, and its audio output,
VCO / 255 = 12.287582 MHz. Only output 0, the machine clock, moves: VCO / C. Every edit is
in sources/misteross/boards/de25nano/pc386/:

- build.py: MACHINE_HZ (rounded to the hertz), CLOCK (the exact rational 9.4e9 / (3 C)),
  PLL_PARAMETERS' output 0 (six decimals, as the IP holds it), and AUDIO's ratio. AUDIO is
  the audio master / 256 over the machine clock, (VCO / 255 / 256) / (VCO / C) = C / 65280,
  which is 1/1280 at C = 51. The guest rates and ATA_RESET_CLOCKS follow CLOCK unedited.
- machine_pll.ip: gui_output_clock_frequency0, the same six-decimal string.

C = 51 is the shipped clock: the script checks that it computes the shipped values (MACHINE_HZ,
CLOCK, the IP string and AUDIO's 1/1280) and leaves both files untouched. Exits non-zero if any edit does not
land exactly once.
"""
from fractions import Fraction
from pathlib import Path
import sys

VCO = Fraction(50_000_000 * 188, 3)


def replace_once(text, old, new, what):
    if text.count(old) != 1:
        raise SystemExit(f"{what}: expected one {old!r}, found {text.count(old)}")
    return text.replace(old, new)


def main(tree, c):
    c = int(c)
    hz = VCO / c
    mhz = f"{float(hz / 1_000_000):.6f}"
    board = Path(tree) / "sources/misteross/boards/de25nano/pc386"
    build = (board / "build.py").read_text()
    build = replace_once(build, "MACHINE_HZ = 61437908\n", f"MACHINE_HZ = {round(hz)}\n", "MACHINE_HZ")
    build = replace_once(build, "CLOCK = Fraction(9_400_000_000, 153)\n",
                         f"CLOCK = Fraction(9_400_000_000, {3 * c})\n", "CLOCK")
    build = replace_once(build, '"gui_output_clock_frequency0": "61.437908"',
                         f'"gui_output_clock_frequency0": "{mhz}"', "PLL_PARAMETERS")
    build = replace_once(build, 'ratio = Fraction(1, 1280) if name == "AUDIO"',
                         f'ratio = Fraction({c}, 65280) if name == "AUDIO"', "AUDIO")
    ip = (board / "machine_pll.ip").read_text()
    ip = replace_once(ip, "<ipxact:name>gui_output_clock_frequency0</ipxact:name>\n"
                          "          <ipxact:displayName>Desired Frequency</ipxact:displayName>\n"
                          "          <ipxact:value>61.437908</ipxact:value>",
                      "<ipxact:name>gui_output_clock_frequency0</ipxact:name>\n"
                      "          <ipxact:displayName>Desired Frequency</ipxact:displayName>\n"
                      f"          <ipxact:value>{mhz}</ipxact:value>", "machine_pll.ip")
    if c == 51:
        if (round(hz), mhz, Fraction(c, 65280)) != (61437908, "61.437908", Fraction(1, 1280)):
            raise SystemExit("C = 51 must compute the shipped clock")
        print("C=51 is the shipped clock; nothing edited")
        return
    (board / "build.py").write_text(build)
    (board / "machine_pll.ip").write_text(ip)
    print(f"C={c} machine {mhz} MHz ({round(hz)} Hz), period {float(1e9 / hz):.3f} ns, AUDIO {Fraction(c, 65280)}")


if __name__ == "__main__":
    main(*sys.argv[1:])
