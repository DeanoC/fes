-- Throwaway reference room for docs/ux/references/core-screens. Not a product
-- room.
--
-- One pack draws every proposed core screen: the machine screen for each
-- tier 1 family, the disk shelf, and the screens under the native dialogs the
-- harness adds. The capture harness passes the scene name as
-- room.initial_game_id. Colours, header, focus outline, chips and buttons
-- follow docs/ux/references/v1/tooling/v1ref/main.lua, which follows
-- ui/rooms/examples/fes-cores/main.lua and ui/tenfoot/room_hero.go. Family
-- marks are placeholders until #1351; every game, disk and count is a
-- fictional fixture.

local C = {
  background = "#F4F0E8",
  surface = "#FFFCF5",
  raised = "#E9E6DC",
  text = "#243631",
  secondary = "#53625A",
  divider = "#CECFC0",
  accent = "#235A46",
  on_accent = "#FCFCF3",
  warm_fill = "#F6EBCB",
  warm_bar = "#D9B45C",
  dark = "#22342E",
}

local scene = room.initial_game_id or ""

-- Layout -------------------------------------------------------------

local function H() return room.hero end
local function small() return room.height < 680 end

local function frame()
  local h = H()
  local m = h.card.x
  return { x = m, y = h.header.h + math.floor(m * 0.6), w = room.width - 2 * m,
    bottom = h.footer.y - math.floor(m * 0.4), m = m }
end

local function S()
  if small() then
    return { title = 28, body = 18, cap = 16, row = 40, pad = 12, gap = 8 }
  end
  return { title = 34, body = 20, cap = 18, row = 48, pad = 16, gap = 10 }
end

-- Primitives ---------------------------------------------------------

local function outline(x, y, w, h, t, color)
  gfx.rect(x, y, w, t, color)
  gfx.rect(x, y + h - t, w, t, color)
  gfx.rect(x, y, t, h, color)
  gfx.rect(x + w - t, y, t, h, color)
end

local function line_h(size, bold)
  local _, h = gfx.measure("Ag", size, bold)
  return h
end

local function wrap(s, size, w, bold)
  local lines, cur = {}, ""
  for word in string.gmatch(s, "%S+") do
    local try = cur == "" and word or (cur .. " " .. word)
    if cur ~= "" and gfx.measure(try, size, bold) > w then
      lines[#lines + 1] = cur
      cur = word
    else
      cur = try
    end
  end
  if cur ~= "" then lines[#lines + 1] = cur end
  return lines
end

-- Draws wrapped text and returns the y below it.
local function para(s, x, y, w, size, color, bold, gap)
  local lh = line_h(size, bold) + (gap or 3)
  for _, l in ipairs(wrap(s, size, w, bold)) do
    gfx.text(l, x, y, { size = size, bold = bold, color = color, max_w = w })
    y = y + lh
  end
  return y
end

local function keyboard(x, y, w, h, key, ground)
  local rows, cols = 4, 10
  local gap = math.max(2, math.floor(w / 80))
  local kw = (w - gap * (cols + 1)) / cols
  local kh = (h - gap * (rows + 1)) / rows
  gfx.rect(x, y, w, h, C.dark)
  for r = 0, rows - 1 do
    local inset = (r % 2) * kw / 3
    for c = 0, cols - 1 do
      local kx = x + gap + c * (kw + gap) + inset
      if kx + kw <= x + w - gap then
        gfx.rect(kx, y + gap + r * (kh + gap), kw, kh, key)
      end
    end
  end
  gfx.rect(x + w * 0.25, y + h - gap - kh, w * 0.5, kh, ground)
end

-- Placeholder family mark (#1351 draws the real icons).
local function family_mark(mark, ground, x, y, w, h)
  gfx.rect(x, y, w, h, ground)
  local size = math.max(16, math.floor(h * 0.34))
  local _, th = gfx.measure(mark, size, true)
  gfx.text(mark, x, y + h * 0.28 - th / 2, { size = size, bold = true, color = C.surface, max_w = w, align = "center" })
  keyboard(x + w * 0.14, y + h * 0.54, w * 0.72, h * 0.32, C.surface, ground)
end

-- State chips, as on Home: Ready is filled, Unavailable is raised with a
-- leading bar, Checking is outlined. Tags use the same shapes with their own
-- words, so a shape never changes meaning.
local CHIP = {
  ready = { fill = C.accent, color = C.on_accent },
  unavailable = { fill = C.raised, color = C.text, leading = C.text },
  plain = { fill = C.raised, color = C.text },
}

local function chip_w(kind, size, label)
  return gfx.measure(label, size, true) + 20 + (CHIP[kind].leading and 7 or 0)
end

local function chip(kind, x, y, size, label)
  local c = CHIP[kind]
  local tw, th = gfx.measure(label, size, true)
  local lead = c.leading and 7 or 0
  local w, h = tw + 20 + lead, th + 8
  gfx.rect(x, y, w, h, c.fill)
  if c.leading then gfx.rect(x, y, lead, h, c.leading) end
  gfx.text(label, x + 10 + lead, y + 4, { size = size, bold = true, color = c.color })
  return w, h
end

-- A button of a given width. Filled is the screen's primary action; focus is
-- a separate outline outside it; an action that is not available yet is
-- raised, with secondary text.
local function button(label, x, y, w, o)
  o = o or {}
  local size = o.size or S().body
  local th = line_h(size, true)
  local h = o.h or (th + (small() and 16 or 20))
  if o.filled then
    gfx.rect(x, y, w, h, C.accent)
  elseif o.off then
    gfx.rect(x, y, w, h, C.raised)
  else
    gfx.rect(x, y, w, h, C.surface)
    outline(x, y, w, h, 2, C.divider)
  end
  if o.focused then outline(x - 5, y - 5, w + 10, h + 10, 4, C.accent) end
  local color = o.filled and C.on_accent or (o.off and C.secondary or C.text)
  gfx.text(label, x + 8, y + (h - th) / 2, { size = size, bold = not o.off, color = color, max_w = w - 16, align = "center" })
  return h
end

local function button_w(label, size)
  return gfx.measure(label, size or S().body, true) + 36
end

local function header(title, routes)
  local hd = H().header
  local m = H().card.x
  gfx.rect(0, hd.h - 2, room.width, 2, C.divider)
  local bar = math.floor(hd.h * 0.11)
  local my = math.floor((hd.h - bar * 4) / 2)
  gfx.rect(m, my, bar * 3, bar, C.accent)
  gfx.rect(m, my + bar * 1.5, bar * 3, bar, C.accent)
  gfx.rect(m, my + bar * 3, bar * 1.6, bar, C.accent)
  local _, th = gfx.measure("FES", 28, true)
  gfx.text("FES", m + bar * 3 + 12, (hd.h - th) / 2, { size = 28, bold = true, color = C.text })
  local fx = m + bar * 3 + 12 + gfx.measure("FES", 28, true) + 18
  gfx.rect(fx, hd.h * 0.3, 2, hd.h * 0.4, C.divider)
  local _, rh = gfx.measure(title, 22)
  gfx.text(title, fx + 18, (hd.h - rh) / 2, { size = 22, color = C.text })
  local right = room.width - m
  local bh = math.floor(hd.h * 0.62)
  local by = math.floor((hd.h - bh) / 2)
  for i = #(routes or {}), 1, -1 do
    local r = routes[i]
    local bw = gfx.measure(r, 20, false) + 36
    local bx = right - bw
    gfx.rect(bx, by, bw, bh, C.surface)
    outline(bx, by, bw, bh, 1, C.divider)
    local _, lh = gfx.measure(r, 20, false)
    gfx.text(r, bx + 18, by + (bh - lh) / 2, { size = 20, color = C.text })
    right = bx - 12
  end
end

local function panel(x, y, w, h, fill)
  gfx.rect(x, y, w, h, fill or C.surface)
  outline(x, y, w, h, 2, C.divider)
end

-- A warm note with a leading bar: a consequence beside the control it is
-- about. Returns the y below it.
local function note(head, body, x, y, w)
  local s = S()
  local inner = w - 30
  local lines = wrap(body or "", s.cap, inner, false)
  local h = 10 + line_h(s.cap, true) + 3 + #lines * (line_h(s.cap) + 3) + 7
  if not body then h = 10 + line_h(s.cap, true) + 10 end
  gfx.rect(x, y, w, h, C.warm_fill)
  gfx.rect(x, y, 6, h, C.warm_bar)
  gfx.text(head, x + 18, y + 10, { size = s.cap, bold = true, color = C.text, max_w = inner })
  if body then para(body, x + 18, y + 10 + line_h(s.cap, true) + 3, inner, s.cap, C.text, false) end
  return y + h
end

-- A list row: bold lead, name, optional second line and a right-hand tag.
local function row(x, y, w, h, r, focused)
  local s = S()
  panel(x, y, w, h, r.off and C.raised or C.surface)
  if r.sel then gfx.rect(x, y, 7, h, C.accent) end
  if focused then outline(x - 4, y - 4, w + 8, h + 8, 4, C.accent) end
  local tx = x + s.pad + (r.sel and 5 or 0)
  local right = x + w - s.pad
  if r.tag then
    local tw = chip_w(r.tag_kind or "plain", s.cap, r.tag)
    local _, ch = gfx.measure(r.tag, s.cap, true)
    chip(r.tag_kind or "plain", right - tw, y + (h - ch - 8) / 2, s.cap, r.tag)
    right = right - tw - 12
  end
  local two = r.sub ~= nil and h >= 2 * line_h(s.cap) + line_h(s.body) - 6
  local ty = two and (y + (h - line_h(s.body, true) - line_h(s.cap)) / 2 - 1) or (y + (h - line_h(s.body, true)) / 2)
  if r.lead then
    local lw = r.lead_w or (gfx.measure(r.lead, s.body, true) + 16)
    gfx.text(r.lead, tx, ty, { size = s.body, bold = true, color = C.text, max_w = lw - 8 })
    tx = tx + lw
  end
  local color = r.off and C.secondary or C.text
  gfx.text(r.name, tx, ty, { size = s.body, bold = r.bold, color = color, max_w = right - tx })
  if two then
    gfx.text(r.sub, tx, ty + line_h(s.body, true), { size = s.cap, color = C.secondary, max_w = right - tx })
  elseif r.sub then
    local nw = gfx.measure(r.name, s.body, r.bold)
    if tx + nw + 24 < right then
      gfx.text(r.sub, tx + nw + 18, ty + (line_h(s.body) - line_h(s.cap)) / 2 + 1, { size = s.cap, color = C.secondary, max_w = right - tx - nw - 18 })
    end
  end
end

local function section(label, x, y, w, right)
  local s = S()
  gfx.text(label, x, y, { size = s.body, bold = true, color = C.text })
  if right then
    gfx.text(right, x, y + (line_h(s.body, true) - line_h(s.cap)) / 2, { size = s.cap, color = C.secondary, max_w = w, align = "right" })
  end
  return y + line_h(s.body, true) + 6
end

-- The machine screen ------------------------------------------------------
--
-- One template. Left: the machine, what is fixed about it, and Start. Right:
-- what is in its drives, the disk actions, and the games for this model.

local function model_chips(m, x, y)
  local s = S()
  if not m.models then return end
  gfx.text("Model", x, y + 6, { size = s.cap, color = C.secondary })
  x = x + gfx.measure("Model", s.cap) + 12
  for i, name in ipairs(m.models) do
    local on = i == m.model
    local tw, th = gfx.measure(name, s.cap, on)
    local w, h = tw + 24, th + 12
    gfx.rect(x, y, w, h, on and C.surface or C.raised)
    if on then
      outline(x, y, w, h, 2, C.divider)
      gfx.rect(x, y + h - 4, w, 4, C.accent)
    end
    if m.focus == "model" and on then outline(x - 5, y - 5, w + 10, h + 10, 4, C.accent) end
    gfx.text(name, x + 12, y + 5, { size = s.cap, bold = on, color = on and C.text or C.secondary })
    x = x + w + 8
  end
end

local function machine_panel(m, x, y, w, h)
  local s = S()
  panel(x, y, w, h)
  local px, py, pw = x + s.pad, y + s.pad, w - 2 * s.pad
  local mh = small() and 56 or 72
  local mw = math.floor(mh * 1.25)
  family_mark(m.mark, m.ground, px, py, mw, mh)
  local tx = px + mw + 14
  gfx.text(m.name, tx, py, { size = s.body + 2, bold = true, color = C.text, max_w = px + pw - tx })
  local cy = py + line_h(s.body + 2, true) + 4
  chip(m.state == "ready" and "ready" or "unavailable", tx, cy, s.cap, m.chip or (m.state == "ready" and "Ready" or "Unavailable"))
  local yy = py + mh + 8
  yy = para(m.plays, px, yy, pw, s.cap, C.text, m.state ~= "ready") + 4
  gfx.rect(px, yy, pw, 2, C.divider)
  yy = yy + 8
  -- Buttons sit at the bottom; facts fill the space between.
  local bh = line_h(s.body, true) + (small() and 16 or 20)
  local buttons_h = bh + (m.second and (bh + 10) or 0)
  local limit = y + h - s.pad - buttons_h - 8
  local label_w = small() and 84 or 98
  for _, f in ipairs(m.facts) do
    local lines = wrap(f[2], s.cap, pw - label_w, false)
    local fh = #lines * (line_h(s.cap) + 3) + 3
    if yy + fh > limit then break end
    gfx.text(f[1], px, yy, { size = s.cap, color = C.secondary })
    para(f[2], px + label_w, yy, pw - label_w, s.cap, C.text, false)
    yy = yy + fh
  end
  local by = y + h - s.pad - bh
  button(m.start, px, by, pw, { filled = m.state == "ready" or m.focus == "start", focused = m.focus == "start" })
  if m.second then
    button(m.second, px, by - bh - 10, pw, { focused = m.focus == "second" })
  end
end

local function draw_machine(m)
  local s = S()
  local f = frame()
  header("Computers", { "Back", "Settings" })
  local th = line_h(s.title, true)
  gfx.text(m.family, f.x, f.y, { size = s.title, bold = true, color = C.text })
  local tw = gfx.measure(m.family, s.title, true)
  model_chips(m, f.x + tw + 28, f.y + (th - line_h(s.cap) - 12) / 2)
  local y0 = f.y + th + s.gap
  local lw = math.floor(f.w * 0.33)
  machine_panel(m, f.x, y0, lw, f.bottom - y0)

  local x = f.x + lw + math.floor(f.m * 0.7)
  local w = f.x + f.w - x
  local y = y0
  if m.running then
    y = note(m.running[1], m.running[2], x, y, w) + s.gap
  end
  y = section(m.drives_label or "Drives", x, y, w, m.drives_note)
  local lead_w = 0
  for _, d in ipairs(m.drives) do
    lead_w = math.max(lead_w, gfx.measure(d.lead, s.body, true) + 18)
  end
  for i, d in ipairs(m.drives) do
    d.lead_w = lead_w
    row(x, y, w, s.row, d, m.focus == "drive" and m.focus_i == i)
    y = y + s.row + 6
  end
  if m.disk_buttons then
    y = y + 2
    local bx = x
    local size = s.cap
    for i, b in ipairs(m.disk_buttons) do
      local bw = button_w(b[1], size)
      local bh = button(b[1], bx, y, bw, { size = size, off = b.off, focused = m.focus == "disk" and m.focus_i == i })
      bx = bx + bw + 12
      if i == #m.disk_buttons then y = y + bh end
    end
    y = y + 6
  end
  if m.note then
    y = note(m.note[1], m.note[2], x, y + 2, w)
  end
  y = y + s.gap
  y = section(m.games_label, x, y, w, m.games_note)
  local n = math.floor((f.bottom - y + 6) / (s.row + 6))
  for i = 1, math.min(n, #m.games) do
    row(x, y, w, s.row, m.games[i], m.focus == "game" and m.focus_i == i)
    y = y + s.row + 6
  end
end

local TAPE_GAMES = {
  { name = "Lunar Courier", sub = "Tape", tag = "Last played" },
  { name = "Attic Rally", sub = "Tape" },
  { name = "Marsh Lights", sub = "Snapshot" },
  { name = "Pixel Post", sub = "Tape" },
  { name = "Clockwork Orchard", sub = "Tape" },
  { name = "Tin Lantern", sub = "Snapshot" },
}

local MACHINES = {}

MACHINES["zx81"] = {
  family = "Sinclair ZX81", mark = "ZX81", ground = "#7097A8",
  models = { "ZX81", "ZX80", "TS1000", "TS1500" }, model = 1,
  name = "Sinclair ZX81", state = "ready", plays = "Plays on this TV. Needs a keyboard.",
  facts = {
    { "Firmware", "Built in" },
    { "Memory", "1K, or 16K with the RAM pack" },
    { "Expansion", "16K RAM pack, saved for next start" },
    { "Saves", "SAVE keeps your program as a new cassette" },
  },
  start = "Start in BASIC", second = "Expansions",
  drives = {
    { lead = "Cassette", name = "No cassette", sub = "Pick a game below, or choose a file", off = true },
  },
  games_label = "Games for the ZX81", games_note = "38 in your games folder",
  games = {
    { name = "Bit Garden", sub = "Cassette  ·  needs 16K", tag = "Last played" },
    { name = "Signal 81", sub = "Cassette" },
    { name = "Night Orchard", sub = "Cassette  ·  needs 16K" },
    { name = "Tiny Tide", sub = "Cassette" },
    { name = "Orbit Post", sub = "Cassette  ·  needs 16K" },
    { name = "Kettle Maze", sub = "Cassette" },
  },
  focus = "game", focus_i = 1,
}

MACHINES["spectrum-48k"] = {
  family = "ZX Spectrum", mark = "ZX", ground = "#9690B5",
  models = { "48K", "128K", "+3", "Pentagon" }, model = 1,
  name = "ZX Spectrum 48K", state = "ready", plays = "Plays on this TV. Keyboard or pad.",
  facts = {
    { "Firmware", "Built in" },
    { "Memory", "48K" },
    { "Sound", "Beeper" },
    { "Joysticks", "Pad 1 is Kempston, pad 2 is Sinclair" },
    { "Loading", "Tapes load at about three times speed" },
    { "Saves", "Tape saving is not available yet" },
  },
  start = "Start in BASIC",
  drives = {
    { lead = "Cassette", name = "Lunar Courier", sub = "For next start", tag = "Read only" },
  },
  games_label = "Games for the 48K", games_note = "241 in your games folder",
  games = TAPE_GAMES,
  focus = "game", focus_i = 1,
}

MACHINES["spectrum-plus3"] = {
  family = "ZX Spectrum", mark = "ZX", ground = "#9690B5",
  models = { "48K", "128K", "+3", "Pentagon" }, model = 3,
  name = "ZX Spectrum +3", state = "ready", plays = "Plays on this TV. Keyboard or pad.",
  facts = {
    { "Firmware", "Built in" },
    { "Memory", "128K" },
    { "Sound", "Beeper and AY" },
    { "Joysticks", "Pad 1 is Kempston, pad 2 is Sinclair" },
    { "Drives", "Cassette and one 3-inch drive. No drive B" },
  },
  start = "Start the +3",
  drives = {
    { lead = "Cassette", name = "Empty", sub = "Used when no disk is chosen", off = true },
    { lead = "Drive A", name = "Harbour Mail", sub = "For next start", tag = "Read only" },
  },
  disk_buttons = { { "Choose a disk" }, { "New disk", off = true } },
  note = { "This Spectrum can read disks but can't write to them yet.",
    "New disks and saving to disk arrive with disk writing. A game starts from its tape or its disk, not both." },
  games_label = "Games for the +3", games_note = "241 in your games folder",
  games = {
    { name = "Harbour Mail", sub = "Disk", tag = "Last played" },
    { name = "Lunar Courier", sub = "Tape" },
    { name = "Salt Road", sub = "Disk" },
    { name = "Marsh Lights", sub = "Snapshot" },
    { name = "Pixel Post", sub = "Tape" },
  },
  focus = "drive", focus_i = 2,
}

MACHINES["spectrum-plus3-running"] = {
  family = "ZX Spectrum", mark = "ZX", ground = "#9690B5",
  models = { "48K", "128K", "+3", "Pentagon" }, model = 3,
  name = "ZX Spectrum +3", state = "ready", chip = "Running", plays = "Running on this TV.",
  facts = {
    { "Firmware", "Built in" },
    { "Memory", "128K" },
    { "Sound", "Beeper and AY" },
    { "Joysticks", "Pad 1 is Kempston, pad 2 is Sinclair" },
  },
  start = "Return to play", second = "Stop the +3",
  running = { "Salt Road is running.",
    "A disk or tape you change here goes into the running machine straight away. Stopping clears its memory." },
  drives_label = "In the drives now",
  drives = {
    { lead = "Cassette", name = "Empty", off = true },
    { lead = "Drive A", name = "Salt Road, disk 1", sub = "Disk 1 of 2", tag = "Read only" },
  },
  disk_buttons = { { "Next disk: disk 2" }, { "Choose a disk" }, { "Take the disk out" } },
  games_label = "Games for the +3", games_note = "Starting another game stops this one",
  games = {
    { name = "Salt Road", sub = "Disk  ·  2 disks", tag = "Running", tag_kind = "ready" },
    { name = "Harbour Mail", sub = "Disk" },
    { name = "Lunar Courier", sub = "Tape" },
  },
  focus = "disk", focus_i = 1,
}

MACHINES["c64"] = {
  family = "Commodore 64", mark = "64", ground = "#E08A63",
  name = "Commodore 64 (PAL)", state = "ready", plays = "Plays on this TV. Keyboard or pad.",
  facts = {
    { "Firmware", "Your ROM files" },
    { "Memory", "64K" },
    { "Sound", "SID 6581" },
    { "Joysticks", "Pad 1 is port 2, pad 2 is port 1" },
    { "Drives", "One 1541 disk drive. No tape or cartridge yet" },
  },
  start = "Start in BASIC",
  drives = {
    { lead = "Drive 8", name = "Copper Canyon", sub = "For next start", tag = "Read only" },
  },
  disk_buttons = { { "Choose a disk" }, { "Take the disk out" }, { "New disk", off = true } },
  note = { "The C64 can read disks but can't write to them yet.",
    "New disks and saving to disk arrive with 1541 disk writing." },
  games_label = "Games for the C64", games_note = "112 in your games folder",
  games = {
    { name = "Copper Canyon", sub = "Disk", tag = "Last played" },
    { name = "Sprite Parade", sub = "Disk" },
    { name = "Lode Lantern", sub = "Disk" },
    { name = "Night Ferry", sub = "Disk" },
    { name = "Bramble Run", sub = "Disk" },
  },
  focus = "game", focus_i = 1,
}

MACHINES["c64-needs-firmware"] = {
  family = "Commodore 64", mark = "64", ground = "#E08A63",
  name = "Commodore 64 (PAL)", state = "firmware", plays = "Needs your C64 ROM files before it can start.",
  facts = {
    { "Firmware", "Not added. One 36K file holding BASIC, KERNAL, characters and the 1541 ROM" },
    { "Memory", "64K" },
    { "Sound", "SID 6581" },
    { "Drives", "One 1541 disk drive" },
  },
  start = "Add firmware",
  drives = {
    { lead = "Drive 8", name = "Empty", off = true },
  },
  disk_buttons = { { "Choose a disk" }, { "New disk", off = true } },
  games_label = "Games for the C64", games_note = "112 in your games folder",
  games = {
    { name = "Copper Canyon", sub = "Disk", tag = "Needs firmware", tag_kind = "unavailable" },
    { name = "Sprite Parade", sub = "Disk", tag = "Needs firmware", tag_kind = "unavailable" },
    { name = "Lode Lantern", sub = "Disk", tag = "Needs firmware", tag_kind = "unavailable" },
    { name = "Night Ferry", sub = "Disk", tag = "Needs firmware", tag_kind = "unavailable" },
    { name = "Bramble Run", sub = "Disk", tag = "Needs firmware", tag_kind = "unavailable" },
  },
  focus = "start",
}

MACHINES["atari-st"] = {
  family = "Atari ST", mark = "ST", ground = "#8DB07A",
  name = "Atari 520ST", state = "ready", plays = "Plays on this TV. Needs a keyboard and a mouse.",
  facts = {
    { "Firmware", "Built in: EmuTOS" },
    { "Memory", "512K" },
    { "Joysticks", "Pad 1 is joystick 1, pad 2 is joystick 0" },
    { "Drives", "Drive A. No drive B or hard disk" },
    { "Saves", "A disk keeps its changes when you stop or swap it" },
  },
  start = "Start the ST",
  drives = {
    { lead = "Drive A", name = "Work disk", sub = "720K  ·  for next start", tag = "Saves changes", tag_kind = "ready" },
  },
  disk_buttons = { { "Choose a disk" }, { "Take the disk out" }, { "New disk" }, { "Your disks" } },
  games_label = "Games for the ST", games_note = "64 in your games folder",
  games = {
    { name = "Harbour Run", sub = "Disk  ·  2 disks", tag = "Last played" },
    { name = "Glass Arcade", sub = "Disk" },
    { name = "Tern Island", sub = "Disk  ·  has saved changes" },
    { name = "Slate & Signal", sub = "Disk" },
    { name = "Paper Comet", sub = "Disk" },
  },
  focus = "disk", focus_i = 3,
}

MACHINES["pcxt"] = {
  family = "PC/XT", mark = "XT", ground = "#B9A36B",
  name = "PC/XT with FreeDOS", state = "ready", plays = "Plays on this TV. Needs a keyboard.",
  facts = {
    { "Firmware", "Built in: GLaBIOS" },
    { "Memory", "640K" },
    { "Picture", "CGA" },
    { "Sound", "PC speaker" },
    { "Drives", "Drive A, 360K. No hard disk" },
    { "Saves", "A disk keeps its changes when you stop" },
  },
  start = "Start the PC",
  drives = {
    { lead = "Drive A", name = "FreeDOS boot disk", sub = "360K  ·  for next start", tag = "Saves changes", tag_kind = "ready" },
  },
  disk_buttons = { { "Choose a disk" }, { "Take the disk out" }, { "New disk" }, { "Your disks" } },
  games_label = "Games for the PC/XT", games_note = "17 in your games folder",
  games = {
    { name = "Kiln Quest", sub = "Disk" },
    { name = "Burrow", sub = "Disk", tag = "Last played" },
    { name = "Ledger Lane", sub = "Disk  ·  has saved changes" },
    { name = "Relay Yard", sub = "Disk" },
    { name = "Dust Circuit", sub = "Disk" },
  },
  focus = "drive", focus_i = 1,
}

MACHINES["pc386"] = {
  family = "386 PC", mark = "386", ground = "#7F8FA6",
  name = "386 PC with FreeDOS", state = "ready", plays = "Plays on this TV. Needs a keyboard; a mouse helps.",
  facts = {
    { "Firmware", "Built in" },
    { "Speed", "386DX-33" },
    { "Saves", "The hard disk keeps its changes when you stop" },
    { "Memory", "16 MB, with 2 MB of EMS" },
    { "Picture", "VGA" },
    { "Sound", "Sound Blaster at 220, IRQ 7, and AdLib" },
  },
  start = "Start the PC", second = "Speed and sound",
  drives = {
    { lead = "Hard disk C", name = "Games drive", sub = "64 MB  ·  FreeDOS", tag = "Saves changes", tag_kind = "ready" },
    { lead = "Floppy A", name = "Not on this PC yet", off = true },
    { lead = "CD-ROM", name = "Not on this PC yet", off = true },
  },
  disk_buttons = { { "New hard disk" }, { "Bring in a disk file" }, { "Your hard disks" } },
  games_label = "Start from a hard disk", games_note = "3 hard disks",
  games = {
    { name = "Games drive", sub = "64 MB  ·  FreeDOS", tag = "Last played" },
    { name = "Windows 3.11", sub = "128 MB" },
    { name = "Work disk", sub = "32 MB  ·  FreeDOS only" },
  },
  focus = "drive", focus_i = 1,
}

-- The disk shelf -----------------------------------------------------------

local function draw_shelf(d)
  local s = S()
  local f = frame()
  header("Computers", { "Back", "Settings" })
  local th = line_h(s.title, true)
  gfx.text(d.title, f.x, f.y, { size = s.title, bold = true, color = C.text })
  -- Top actions on the title line, right aligned.
  local bx = f.x + f.w
  for i = #d.top, 1, -1 do
    local b = d.top[i]
    local bw = button_w(b[1], s.cap)
    bx = bx - bw
    button(b[1], bx, f.y + 2, bw, { size = s.cap, off = b.off, focused = d.focus == "top" and d.focus_i == i })
    bx = bx - 12
  end
  local y0 = f.y + th + 4
  y0 = para(d.sub, f.x, y0, f.w, s.cap, C.secondary, false) + s.gap
  local rw = math.floor(f.w * 0.36)
  local lw = f.w - rw - math.floor(f.m * 0.7)
  local y = y0
  local rh = s.row + (small() and 6 or 8)
  for i, r in ipairs(d.rows) do
    if y + rh > f.bottom then break end
    row(f.x, y, lw, rh, r, d.focus == "row" and d.focus_i == i)
    y = y + rh + 6
  end
  -- Selected disk: what it is, then its actions.
  local x = f.x + f.w - rw
  panel(x, y0, rw, f.bottom - y0)
  local px, py, pw = x + s.pad, y0 + s.pad, rw - 2 * s.pad
  gfx.text(d.sel.name, px, py, { size = s.body + 2, bold = true, color = C.text, max_w = pw })
  py = py + line_h(s.body + 2, true) + 4
  for _, l in ipairs(d.sel.lines) do
    py = para(l, px, py, pw, s.cap, C.secondary, false)
  end
  py = py + 8
  local bh = line_h(s.cap, true) + (small() and 12 or 16)
  local gap = small() and 6 or 8
  for i, a in ipairs(d.sel.actions) do
    if py + bh > f.bottom - s.pad then break end
    button(a[1], px, py, pw, { size = s.cap, h = bh, filled = a.filled, off = a.off, focused = d.focus == "action" and d.focus_i == i })
    py = py + bh + gap
  end
end

local SHELVES = {}

SHELVES["st-disks"] = {
  title = "Atari ST disks",
  sub = "Disks you made, and game disks that have changes saved on them. The files in your games folder are never changed.",
  top = { { "New disk" }, { "Bring in a disk file" } },
  rows = {
    { name = "Work disk", sub = "720K  ·  made here  ·  changed yesterday", tag = "In drive A", tag_kind = "ready" },
    { name = "Letters", sub = "720K  ·  made here  ·  changed last week", tag = "Locked" },
    { name = "Tern Island", sub = "720K  ·  game disk with saved changes", sel = true, bold = true },
    { name = "Harbour Run, disk 2", sub = "720K  ·  game disk with saved changes" },
    { name = "Scratch", sub = "360K  ·  made here  ·  empty" },
  },
  sel = { name = "Tern Island",
    lines = { "720K game disk. Your changes are kept beside the original, which is untouched." },
    actions = { { "Put in drive A", filled = true }, { "Copy to a new disk" }, { "Rename" }, { "Lock against changes" }, { "Go back to the original" }, { "Delete" } } },
  focus = "action", focus_i = 5,
}

SHELVES["pcxt-disks"] = {
  title = "PC/XT disks",
  sub = "Disks you made, and game disks that have changes saved on them. The files in your games folder are never changed.",
  top = { { "New disk" }, { "Bring in a disk file" } },
  rows = {
    { name = "FreeDOS boot disk", sub = "360K  ·  made here  ·  starts FreeDOS", tag = "In drive A", tag_kind = "ready", sel = true, bold = true },
    { name = "Ledger Lane", sub = "360K  ·  game disk with saved changes" },
    { name = "Save disk", sub = "360K  ·  made here  ·  empty" },
  },
  sel = { name = "FreeDOS boot disk",
    lines = { "360K disk made here. It starts FreeDOS and has room for your own files." },
    actions = { { "Take out of drive A", filled = true }, { "Copy to a new disk" }, { "Rename" }, { "Lock against changes" }, { "Delete", off = true } } },
  focus = "row", focus_i = 1,
}

SHELVES["pc386-disks"] = {
  title = "386 PC hard disks",
  sub = "Each hard disk is a whole PC setup. Changes are kept beside the disk file you brought in, which is never changed.",
  top = { { "New hard disk" }, { "Bring in a disk file" } },
  rows = {
    { name = "Games drive", sub = "64 MB  ·  brought in  ·  12 MB of changes", tag = "Hard disk C", tag_kind = "ready", sel = true, bold = true },
    { name = "Windows 3.11", sub = "128 MB  ·  brought in  ·  3 MB of changes" },
    { name = "Work disk", sub = "32 MB  ·  made here  ·  FreeDOS" },
  },
  sel = { name = "Games drive",
    lines = { "64 MB, 130 cylinders, 16 heads, 63 sectors. 12 MB of changes since it was brought in." },
    actions = { { "Start from this disk", filled = true }, { "Copy to a new disk" }, { "Rename" }, { "Disk shape" }, { "Undo all changes" }, { "Delete", off = true } } },
  focus = "action", focus_i = 5,
}

-- Entry ----------------------------------------------------------------------

-- Scenes that sit under a native dialog name the screen behind them.
local UNDER = {
  ["st-new-disk"] = { "st-disks", "top", 1 },
  ["st-disk-reset"] = { "st-disks", "action", 5 },
  ["pcxt-new-disk"] = { "pcxt-disks", "top", 1 },
  ["pc386-new-hard-disk"] = { "pc386-disks", "top", 1 },
  ["pc386-import"] = { "pc386-disks", "top", 2 },
  ["pc386-settings"] = { "pc386", "second" },
}

function draw()
  gfx.clear(C.background)
  local name = scene
  local under = UNDER[scene]
  local view = MACHINES[name] or SHELVES[name]
  if under then
    view = MACHINES[under[1]] or SHELVES[under[1]]
    view.focus, view.focus_i = under[2], under[3]
  end
  if view and view.rows then
    draw_shelf(view)
  elseif view then
    draw_machine(view)
  else
    gfx.text("unknown scene: " .. name, 40, 120, { size = 24, color = C.text })
  end
end
