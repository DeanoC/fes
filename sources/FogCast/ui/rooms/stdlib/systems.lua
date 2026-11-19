-- Registered playable systems. Package manifests remain the authority for
-- admission, media size and launch support; these labels guide navigation.
-- model is the short name Home lists under a family. drive names the disk
-- drive and facts are the fixed things the core screen lists; both are
-- display text only.
local rows = {
  {id="fes.zx81", label="Sinclair ZX81", mark="ZX81", model="ZX81", media="Cassette .p", hint="Type LOAD \"\", then RUN.", facts={{"Memory","1K, or 16K with the RAM pack"},{"Saves","SAVE keeps your program as a new cassette"}}},
  {id="fes.zx80", label="Sinclair ZX80", mark="ZX80", model="ZX80", media="Cassette .p", hint="Choose a saved program before starting."},
  {id="fes.ts1000", label="Timex Sinclair 1000", mark="TS1000", model="TS1000", media="Cassette .p", hint="Type LOAD \"\", then RUN."},
  {id="fes.ts1500", label="Timex Sinclair 1500", mark="TS1500", model="TS1500", media="Cassette .p", hint="Type LOAD \"\", then RUN."},
  {id="fes.c64", label="Commodore 64", mark="C64", model="C64 PAL", media="Disk .d64", hint="Drive  ·  1541  ·  unit 0", drive="Drive 8", facts={{"Memory","64K"},{"Sound","SID 6581"},{"Joysticks","Pad 1 is port 2, pad 2 is port 1"},{"Drives","One 1541 disk drive. No tape or cartridge yet"},{"Saves","Disks are read only for now"}}},
  {id="fes.spectrum", label="ZX Spectrum 48K", mark="48K", model="48K", media="Tape or snapshot", hint="Tape .tzx / .tap / .pzx / .csw; snapshot .z80 / .sna", facts={{"Memory","48K"},{"Sound","Beeper"},{"Joysticks","Pad 1 is Kempston, pad 2 is Sinclair"},{"Saves","Tape saving is not available yet"}}},
  {id="fes.spectrum128", label="ZX Spectrum 128K", mark="128K", model="128K/+2", media="Tape or snapshot", hint="Tape .tzx / .tap / .pzx / .csw; snapshot .z80 / .sna", facts={{"Memory","128K"},{"Saves","Tape saving is not available yet"}}},
  {id="fes.spectrum-plus3", label="ZX Spectrum +3", mark="+3", model="+2A/+3", media="Tape, snapshot or disk", disk="Disk .dsk", cassette="Tape or snapshot", hint="Disk .dsk; snapshot .z80 / .sna", drive="Drive A", facts={{"Memory","128K"},{"Sound","Beeper and AY"},{"Joysticks","Pad 1 is Kempston, pad 2 is Sinclair"},{"Drives","Cassette and one 3-inch drive. No drive B"},{"Saves","Disks are read only for now"}}},
  {id="fes.pentagon", label="Pentagon 128", mark="128", model="Pentagon", media="Tape, snapshot or disk", disk="Disk .trd", cassette="Tape or snapshot", hint="Disk .trd; tape .tzx / .tap / .pzx / .csw", drive="Drive A", facts={{"Memory","128K"},{"Saves","Disks are read only for now"}}},
  {id="fes.apple2", label="Apple II", mark="APPLE II", media="Disk .dsk / .do", hint="Drive  ·  Disk II  ·  unit 0"},
  {id="fes.atari-st", label="Atari ST", mark="ST", model="520ST", media="Disk .st", hint="Drive A  ·  writable raw .st disk", drive="Drive A", facts={{"Memory","512K"},{"Joysticks","Pad 1 is joystick 1, pad 2 is joystick 0"},{"Drives","Drive A. No drive B or hard disk"},{"Saves","A disk keeps its changes when you stop or swap it"}}},
  {id="fes.pcxt-dos", label="PC/XT", mark="XT", model="8088", media="Disk .img", hint="Drive A  ·  360 KiB floppy image"},
  {id="fes.pc386", label="PC 386", mark="386", model="386", media="Hard disk .img", hint="In development  ·  choose a machine setup and hard disk image"},
  {id="fes.coleco", label="ColecoVision", mark="COLECO", media="Linked cartridge", hint="Select the cartridge ROM in system setup."},
  {id="fes.sms", label="Sega Master System", mark="SMS", media="Linked cartridge", hint="Select the cartridge ROM in system setup."},
  {id="fes.sg1000", label="Sega SG-1000", mark="SG-1000", media="Linked cartridge", hint="Select the cartridge ROM in system setup."},
  {id="fes.pong", label="Pong", mark="PONG", media="No media required", hint="Paddle settings and best rally are saved."},
  {id="fes.ramtest", label="RAM Tester", mark="RAM", media="No media required", hint="Memory testing utility."},
  {id="fes.riscv", label="RISC-V", mark="RISC-V", media="No media required", hint="Start the installed demonstration."},
}
-- Families organize the workbench. Each variant retains its real core ID;
-- this table never changes package, media or expansion compatibility.
local groups = {
  ["fes.zx81"] = {id="zx81", label="Sinclair ZX81"},
  ["fes.zx80"] = {id="zx81", label="Sinclair ZX81"},
  ["fes.ts1000"] = {id="zx81", label="Sinclair ZX81"},
  ["fes.ts1500"] = {id="zx81", label="Sinclair ZX81"},
  ["fes.spectrum"] = {id="spectrum", label="ZX Spectrum"},
  ["fes.spectrum128"] = {id="spectrum", label="ZX Spectrum"},
  ["fes.spectrum-plus3"] = {id="spectrum", label="ZX Spectrum"},
  ["fes.pentagon"] = {id="spectrum", label="ZX Spectrum"},
}
-- Held-back cores are left out unless the launcher shows all
-- cores (room.core_shown).
local shown, by_id, ids, families, family_by_core, by_family = {}, {}, {}, {}, {}, {}
for _, row in ipairs(rows) do
  if room.core_shown(row.id) then shown[#shown+1] = row end
end
for _, row in ipairs(shown) do
  by_id[row.id] = row
  ids[#ids+1] = row.id
  local group = groups[row.id] or {id=row.id, label=row.label}
  local family = by_family[group.id]
  if not family then
    family = {id=group.id, label=group.label, variants={}}
    families[#families+1] = family
    by_family[group.id] = family
  end
  family.variants[#family.variants+1] = row
  family_by_core[row.id] = family
end
return {rows=shown, by_id=by_id, ids=ids, families=families, family_by_core=family_by_core}
