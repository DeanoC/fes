# ADV7513 HDMI transmitter & EDID driver (`adv7513/`)

`adv7513` provides a bare-metal Analog Devices ADV7513 HDMI TX driver and pure-Go
EDID 1.4 / CEA-861 parser over [i2c](../i2c/README.md).

- **Register parity:** Initialization (`Menu720p60Initialization`), mode selection
  (`Menu720p60Mode`), EDID trigger (`HDMIWake`), 48 kHz I2S audio setup
  (`ApplicationAudio48k`), and DE-generator packing match
  [adv7513.hpp](../../../../sources/libmister-runtime/src/native/adv7513.hpp).
- **Driver lifecycle (`Device`):** `ChipRevision`, `Status` (HPD, monitor sense,
  power, PLL lock, detected VIC), `PowerUp`/`PowerDown`, `InitMenu720p60`,
  `ConfigureAudio48k`, and `ReadEDID` (polling `0x96[2]` and fetching base +
  CEA-861 extension blocks).
- **EDID parser (`ParseEDID`):** Validates block checksums and extracts monitor
  name, detailed timing descriptors (`PreferredTiming`), CEA VICs
  (`SupportsVIC`), HDMI VSDB physical address, and LPCM audio formats
  (`SupportsAudio48kLPCM`).
