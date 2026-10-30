-- Registered playable systems. Package manifests remain the authority for
-- admission, media size and launch support; these labels guide navigation.
local rows = {
  {id="fes.zx81", label="Sinclair ZX81", mark="ZX81", media="Cassette .p", hint="Type LOAD \"\", then RUN."},
  {id="fes.zx80", label="Sinclair ZX80", mark="ZX80", media="Cassette .p", hint="Choose a saved program before starting."},
  {id="fes.ts1000", label="Timex Sinclair 1000", mark="TS1000", media="Cassette .p", hint="Type LOAD \"\", then RUN."},
  {id="fes.ts1500", label="Timex Sinclair 1500", mark="TS1500", media="Cassette .p", hint="Type LOAD \"\", then RUN."},
  {id="fes.c64", label="Commodore 64", mark="C64", media="Disk .d64", hint="Drive  ·  1541  ·  unit 0"},
  {id="fes.spectrum", label="ZX Spectrum 48K", mark="48K", media="Tape or snapshot", hint="Tape .tzx / .tap / .pzx / .csw; snapshot .z80 / .sna"},
  {id="fes.spectrum128", label="ZX Spectrum 128K", mark="128K", media="Tape or snapshot", hint="Tape .tzx / .tap / .pzx / .csw; snapshot .z80 / .sna"},
  {id="fes.spectrum-plus3", label="ZX Spectrum +3", mark="+3", media="Tape, snapshot or disk", disk="Disk .dsk", cassette="Tape or snapshot", hint="Disk .dsk; snapshot .z80 / .sna"},
  {id="fes.pentagon", label="Pentagon 128", mark="128", media="Tape, snapshot or disk", disk="Disk .trd", cassette="Tape or snapshot", hint="Disk .trd; tape .tzx / .tap / .pzx / .csw"},
  {id="fes.apple2", label="Apple II", mark="APPLE II", media="Disk .dsk / .do", hint="Drive  ·  Disk II  ·  unit 0"},
  {id="fes.atari-st", label="Atari ST", mark="ST", media="Disk .st", hint="Drive A  ·  writable raw .st disk"},
  {id="fes.coleco", label="ColecoVision", mark="COLECO", media="Linked cartridge", hint="Select the cartridge ROM in system setup."},
  {id="fes.sms", label="Sega Master System", mark="SMS", media="Linked cartridge", hint="Select the cartridge ROM in system setup."},
  {id="fes.sg1000", label="Sega SG-1000", mark="SG-1000", media="Linked cartridge", hint="Select the cartridge ROM in system setup."},
  {id="fes.pong", label="Pong", mark="PONG", media="No media required", hint="Paddle settings and best rally are saved."},
  {id="fes.catch", label="Catch", mark="CATCH", media="No media required", hint="Start the game with a controller."},
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
local by_id, ids, families, family_by_core, by_family = {}, {}, {}, {}, {}
for _, row in ipairs(rows) do
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
return {rows=rows, by_id=by_id, ids=ids, families=families, family_by_core=family_by_core}
