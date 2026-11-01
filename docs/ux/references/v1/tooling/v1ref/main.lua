-- Throwaway reference room for docs/ux/references/v1. Not a product room.
--
-- One pack draws every proposed v1 first-boot, Home and firmware-card
-- screen. The capture harness passes the scene name as room.initial_game_id.
-- Colours, header, focus outline and type sizes follow
-- ui/rooms/examples/fes-cores/main.lua and ui/tenfoot/room_hero.go so the
-- pictures match what the kit already draws. Family marks are placeholders
-- until #1351; titles, counts, pad names and folders are fictional fixtures.

local C = {
  background = "#F4F0E8",
  surface = "#FFFCF5",
  raised = "#E9E6DC",
  text = "#243631",
  secondary = "#53625A",
  divider = "#CECFC0",
  accent = "#235A46",
  on_accent = "#FCFCF3",
  warm = "#EFD48D",
  dark = "#22342E",
  dim = "#141918A6",
}

local FAMILIES = {
  { id = "zx81", name = "Sinclair ZX81", short = "ZX81", mark = "ZX81", ground = "#7097A8",
    models = "4 models: ZX81, ZX80, TS1000, TS1500" },
  { id = "spectrum", name = "ZX Spectrum", short = "Spectrum", mark = "ZX", ground = "#9690B5",
    models = "4 models: 48K, 128K, +3, Pentagon" },
  { id = "st", name = "Atari ST", short = "Atari ST", mark = "ST", ground = "#8DB07A",
    models = "1 model: 520ST" },
  { id = "c64", name = "Commodore 64", short = "C64", mark = "64", ground = "#E08A63",
    models = "1 model: C64 (PAL)" },
}

local scene = room.initial_game_id or ""

-- Layout -------------------------------------------------------------

local function H() return room.hero end

local function frame()
  local h = H()
  local m = h.card.x
  return { x = m, y = h.header.h + math.floor(m * 0.75), w = room.width - 2 * m,
    bottom = h.footer.y - math.floor(m * 0.5), m = m }
end

local function small() return room.height < 680 end

-- Primitives ---------------------------------------------------------

local function outline(x, y, w, h, t, color)
  gfx.rect(x, y, w, t, color)
  gfx.rect(x, y + h - t, w, t, color)
  gfx.rect(x, y, t, h, color)
  gfx.rect(x + w - t, y, t, h, color)
end

local function disc(cx, cy, r, color)
  local slices = math.max(6, math.floor(r / 3))
  for i = 0, slices - 1 do
    local y0 = -r + (2 * r) * i / slices
    local y1 = -r + (2 * r) * (i + 1) / slices
    local mid = (y0 + y1) / 2
    local half = math.sqrt(math.max(0, r * r - mid * mid))
    gfx.rect(cx - half, cy + y0, half * 2, (y1 - y0) + 1, color)
  end
end

local function hills(x, y, w, h, color, phase)
  local steps = 12
  for i = 0, steps - 1 do
    local t = (i + 0.5) / steps
    local top = h * (0.30 + 0.16 * math.sin((t + phase) * math.pi * 2))
    gfx.rect(x + w * i / steps, y + top, w / steps + 1, h - top, color)
  end
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
  local lh = line_h(size, bold) + (gap or 4)
  for _, l in ipairs(wrap(s, size, w, bold)) do
    gfx.text(l, x, y, { size = size, bold = bold, color = color, max_w = w })
    y = y + lh
  end
  return y
end

-- A button in the native hero style: "A  Play" filled, "Y  Details" plain.
-- Focus is a separate outline outside the button.
local function button(label, glyph, x, y, filled, focused, size)
  size = size or 22
  local text = glyph and (glyph .. "  " .. label) or label
  local tw, th = gfx.measure(text, size, true)
  local bw, bh = tw + 36, th + 22
  if filled then
    gfx.rect(x, y, bw, bh, C.accent)
  else
    gfx.rect(x, y, bw, bh, C.surface)
    outline(x, y, bw, bh, 2, C.divider)
  end
  if focused then
    outline(x - 6, y - 6, bw + 12, bh + 12, 4, C.accent)
  end
  gfx.text(text, x + 18, y + (bh - th) / 2, { size = size, bold = true, color = filled and C.on_accent or C.text })
  return bw, bh
end

local function button_h(size)
  return line_h(size or 22, true) + 22
end

-- A row of buttons left to right; returns the total width.
local function buttons(list, x, y, size)
  local cx = x
  for _, b in ipairs(list) do
    local bw = button(b[1], b[2], cx, y, b.filled, b.focused, size)
    cx = cx + bw + 20
  end
  return cx - x - 20
end

-- Availability chips. Each state has its own shape, not only its colour:
-- Ready is filled, Needs firmware is outlined, the rest sit on a raised plate.
local CHIP = {
  ready = { label = "Ready", fill = C.accent, color = C.on_accent },
  firmware = { label = "Needs firmware", fill = C.surface, color = C.text, ring = C.text },
  nogames = { label = "No games yet", fill = C.raised, color = C.text },
  checking = { label = "Checking", fill = C.raised, color = C.text },
  suggested = { label = "Suggested", fill = C.warm, color = C.text },
  tested = { label = "Tested", fill = C.warm, color = C.text },
  skipped = { label = "Skipped", fill = C.raised, color = C.secondary },
  choose = { label = "Choose a folder", fill = C.surface, color = C.text, ring = C.divider },
  done = { label = "Done", fill = C.accent, color = C.on_accent },
}

local function chip(kind, x, y, size, label)
  local c = CHIP[kind]
  size = size or 18
  label = label or c.label
  local tw, th = gfx.measure(label, size, true)
  local w, h = tw + 20, th + 8
  gfx.rect(x, y, w, h, c.fill)
  if c.ring then outline(x, y, w, h, 2, c.ring) end
  gfx.text(label, x + 10, y + 4, { size = size, bold = true, color = c.color })
  return w, h
end

local function chip_w(kind, size, label)
  return gfx.measure(label or CHIP[kind].label, size or 18, true) + 20
end

-- Placeholder family mark (#1351 draws the real icons): a muted ground,
-- a monogram and a keyboard. The family name is drawn outside the mark.
local function family_mark(f, x, y, w, h)
  gfx.rect(x, y, w, h, f.ground)
  local big = h >= 60
  local size = math.max(16, math.min(72, math.floor(h * (big and 0.34 or 0.42))))
  local _, th = gfx.measure(f.mark, size, true)
  local cy = big and h * 0.30 or h * 0.5
  gfx.text(f.mark, x, y + cy - th / 2, { size = size, bold = true, color = C.surface, max_w = w, align = "center" })
  if big then
    keyboard(x + w * 0.14, y + h * 0.56, w * 0.72, h * 0.30, C.surface, f.ground)
  end
end

-- Placeholder game art: the fes-cores fallback motif.
local function game_art(seed, ground, x, y, w, h)
  gfx.rect(x, y, w, h, ground)
  disc(x + w * 0.36, y + h * 0.42, math.min(w, h) * 0.24, "#F5E6C8")
  hills(x, y + h * 0.45, w, h * 0.55, C.dark, seed)
end

local function pad_art(x, y, w, h, lit)
  local cx, cy = x + w / 2, y + h / 2
  local bw, bh = w * 0.80, h * 0.56
  local bx, by = cx - bw / 2, cy - bh / 2
  gfx.rect(bx + bw * 0.06, by - h * 0.06, bw * 0.24, h * 0.07, C.divider)
  gfx.rect(bx + bw * 0.70, by - h * 0.06, bw * 0.24, h * 0.07, C.divider)
  gfx.rect(bx, by, bw, bh, C.dark)
  disc(bx + bh * 0.1, by + bh * 0.85, bh * 0.38, C.dark)
  disc(bx + bw - bh * 0.1, by + bh * 0.85, bh * 0.38, C.dark)
  local dx, dy, ds = bx + bw * 0.22, cy, bh * 0.16
  gfx.rect(dx - ds * 1.5, dy - ds / 2, ds * 3, ds, C.raised)
  gfx.rect(dx - ds / 2, dy - ds * 1.5, ds, ds * 3, C.raised)
  local fx, fy, r = bx + bw * 0.78, cy, bh * 0.11
  local face = { { "Y", 0, -2.2 }, { "B", 2.2, 0 }, { "A", 0, 2.2 }, { "X", -2.2, 0 } }
  for _, b in ipairs(face) do
    local on = lit == b[1]
    disc(fx + b[2] * r, fy + b[3] * r, r * (on and 1.25 or 1), on and C.warm or C.raised)
  end
  gfx.rect(cx - bw * 0.10, cy - bh * 0.05, bw * 0.07, bh * 0.08, C.divider)
  gfx.rect(cx + bw * 0.03, cy - bh * 0.05, bw * 0.07, bh * 0.08, C.divider)
end

-- Header: the fes-cores wordmark and title, with routes or a step tracker.
local function header(title, routes, step)
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
  if step then
    local names = { "Welcome", "Controllers", "Games folders", "Ready" }
    local label = "Step " .. step .. " of 4"
    local pw, gap = 30, 6
    local px = right - (pw * 4 + gap * 3)
    for i = 1, 4 do
      gfx.rect(px + (i - 1) * (pw + gap), hd.h / 2 - 3, pw, 6, i <= step and C.accent or C.divider)
    end
    local lw, lh = gfx.measure(label, 18, false)
    gfx.text(label, px - 16 - lw, (hd.h - lh) / 2, { size = 18, color = C.secondary })
    return
  end
  local bh = math.floor(hd.h * 0.62)
  local by = math.floor((hd.h - bh) / 2)
  for i = #(routes or {}), 1, -1 do
    local r = routes[i]
    local tw = gfx.measure(r.label, 20, r.focused)
    local bw = tw + 36
    local bx = right - bw
    gfx.rect(bx, by, bw, bh, C.surface)
    if r.focused then
      outline(bx - 3, by - 3, bw + 6, bh + 6, 4, C.accent)
    else
      outline(bx, by, bw, bh, 1, C.divider)
    end
    local _, lh = gfx.measure(r.label, 20, r.focused)
    gfx.text(r.label, bx + 18, by + (bh - lh) / 2, { size = 20, bold = r.focused, color = C.text })
    right = bx - 12
  end
end

local function panel(x, y, w, h, fill)
  gfx.rect(x, y, w, h, fill or C.surface)
  outline(x, y, w, h, 2, C.divider)
end

-- 0 Boot / splash ------------------------------------------------------

local function draw_splash()
  local bar = math.floor(room.height * 0.03)
  local word = 96
  local ww, wh = gfx.measure("FES", word, true)
  local total = bar * 3 + 24 + ww
  local x = (room.width - total) / 2
  local y = room.height * 0.42 - wh / 2
  local my = y + (wh - bar * 4) / 2 - wh * 0.1
  gfx.rect(x, my, bar * 3, bar, C.accent)
  gfx.rect(x, my + bar * 1.5, bar * 3, bar, C.accent)
  gfx.rect(x, my + bar * 3, bar * 1.6, bar, C.accent)
  gfx.text("FES", x + bar * 3 + 24, y, { size = word, bold = true, color = C.text })
  gfx.text("Starting up…", 0, y + wh + 28, { size = 24, color = C.secondary, max_w = room.width, align = "center" })
end

-- Shared first-run layout: header, a left text column and a right card.
local function setup_columns()
  local f = frame()
  local split = math.floor(f.w * 0.50)
  -- The buttons run along the bottom under both columns.
  local h = f.bottom - f.y - button_h() - 28
  return f, { x = f.x, y = f.y, w = split - f.m }, { x = f.x + split, y = f.y, w = f.w - split, h = h }
end

local function title_block(title, body, x, y, w)
  local size = small() and 36 or 44
  y = para(title, x, y, w, size, C.text, true, 2) + 8
  if body then y = para(body, x, y, w, 24, C.text, false) + 6 end
  return y
end

local function bottom_buttons(list, x)
  local f = frame()
  local y = f.bottom - button_h() - 6
  buttons(list, x or f.x + 6, y)
  return y
end

-- 1 Welcome ------------------------------------------------------------

local function family_grid(r, label)
  local gap = 16
  local name_h = line_h(20, true) + 10
  local cw = (r.w - gap) / 2
  local ch = (r.h - gap - name_h * 2) / 2
  for i, f in ipairs(FAMILIES) do
    local col, row = (i - 1) % 2, math.floor((i - 1) / 2)
    local x = r.x + col * (cw + gap)
    local y = r.y + row * (ch + name_h + gap)
    family_mark(f, x, y, cw, ch)
    gfx.text(f.name, x, y + ch + 6, { size = 20, bold = true, color = C.text, max_w = cw })
  end
end

local function draw_welcome(skip)
  header("Welcome", nil, 1)
  local f, l, r = setup_columns()
  local y = title_block("Welcome to FES",
    "FES turns this kit into classic home computers, rebuilt in its chip. Four are ready to set up.", l.x, l.y, l.w)
  y = y + 8
  local note = "Some computers have their built-in software (firmware) included. For the others, and for your games, you use your own files. You don't need a network or a computer to start."
  local lines = wrap(note, 20, l.w - 40, false)
  local nh = #lines * (line_h(20) + 4) + 28
  panel(l.x, y, l.w, nh, C.raised)
  para(note, l.x + 20, y + 14, l.w - 40, 20, C.text, false)
  y = y + nh + 18
  if skip then
    para("Skip goes straight to Home. Home shows what is still to do, and Settings › About & help › Run setup again brings this back.",
      l.x, y, l.w, 20, C.text, true)
  else
    para("Next: your controller, then where your games are. It takes a couple of minutes.", l.x, y, l.w, 20, C.secondary, false)
  end
  local by = frame().bottom - button_h() - 6
  buttons({ { "Continue", "A", filled = true, focused = not skip }, { "Skip setup", nil, focused = skip } }, l.x + 6, by)
  family_grid({ x = r.x, y = r.y, w = r.w, h = r.h }, true)
end

-- 2 Controllers --------------------------------------------------------

local function draw_pads_waiting()
  header("Controllers", nil, 2)
  local f, l, r = setup_columns()
  local y = title_block("Pick up a controller",
    "Press any button on the pad you want to use. FES names it and makes it Player 1.", l.x, l.y, l.w)
  y = para("Using a keyboard? Press Enter to carry on with it. The keyboard works on every screen.", l.x, y + 4, l.w, 20, C.secondary, false)
  local note = "Bluetooth pad? Choose Pair a Bluetooth pad, then hold the pad's pairing button until its light flashes."
  local lines = wrap(note, 20, l.w - 40, false)
  local nh = #lines * (line_h(20) + 4) + 28
  panel(l.x, y + 16, l.w, nh, C.raised)
  para(note, l.x + 20, y + 30, l.w - 40, 20, C.text, false)
  bottom_buttons({ { "Continue with keyboard", "Enter", filled = true }, { "Pair a Bluetooth pad", nil } })
  panel(r.x, r.y, r.w, r.h)
  pad_art(r.x + 24, r.y + 24, r.w - 48, r.h * 0.62)
  local cy = r.y + r.h * 0.72
  local cw = chip("checking", r.x + 24, cy, 18, "Waiting")
  gfx.text("Listening on USB and Bluetooth…", r.x + 24 + cw + 14, cy + 2, { size = 20, color = C.text, max_w = r.w - cw - 62 })
end

local function player_row(x, y, w, tag, filled, name, badge, sub, focused)
  local rh = line_h(22, true) + line_h(18) + 26
  gfx.rect(x, y, w, rh, C.surface)
  outline(x, y, w, rh, focused and 4 or 1, focused and C.accent or C.divider)
  local tw, th = gfx.measure(tag, 20, true)
  gfx.rect(x + 14, y + 12, tw + 18, th + 8, filled and C.accent or C.raised)
  gfx.text(tag, x + 23, y + 16, { size = 20, bold = true, color = filled and C.on_accent or C.secondary })
  local nx = x + 14 + tw + 18 + 16
  local nw = gfx.measure(name, 22, true)
  gfx.text(name, nx, y + 12, { size = 22, bold = true, color = filled and C.text or C.secondary, max_w = w - (nx - x) - 14 })
  if badge then chip("tested", nx + nw + 12, y + 12, 16) end
  gfx.text(sub, nx, y + 14 + line_h(22, true), { size = 18, color = C.secondary, max_w = w - (nx - x) - 14 })
  return rh
end

local function draw_pads_found()
  header("Controllers", nil, 2)
  local f, l, r = setup_columns()
  local y = title_block("Player 1 is ready", nil, l.x, l.y, l.w)
  y = y + player_row(l.x, y, l.w, "P1", true, "USB gamepad", true, "Wired  ·  Xbox layout  ·  buttons as printed") + 10
  y = y + player_row(l.x, y, l.w, "P2", false, "Add Player 2", false, "Press a button on another pad") + 10
  y = y + player_row(l.x, y, l.w, "Keys", false, "Keyboard", false, "Works everywhere, alongside your pads") + 10
  bottom_buttons({ { "Continue", "A", filled = true, focused = true }, { "Swap A / B" }, { "Pair Bluetooth" } })
  panel(r.x, r.y, r.w, r.h)
  gfx.text("Try your buttons", r.x + 24, r.y + 18, { size = 24, bold = true, color = C.text })
  pad_art(r.x + 24, r.y + 50, r.w - 48, r.h * 0.52, "A")
  local cy = r.y + 60 + r.h * 0.52
  local cw = chip("done", r.x + 24, cy, 18, "A pressed")
  para("Buttons look wrong? Swap A and B here, or remap them later in Settings › Controllers.",
    r.x + 24, cy + 40, r.w - 48, 20, C.secondary, false)
end

local function draw_pads_bluetooth()
  header("Controllers", nil, 2)
  local f, l, r = setup_columns()
  local y = title_block("Pair a Bluetooth pad", nil, l.x, l.y, l.w)
  local steps = { "Turn the pad on.", "Hold its pairing button until the light flashes quickly.", "Choose it in the list." }
  for i, s in ipairs(steps) do
    local tw, th = gfx.measure(tostring(i), 22, true)
    gfx.rect(l.x, y, th + 10, th + 10, C.raised)
    gfx.text(tostring(i), l.x + (th + 10 - tw) / 2, y + 5, { size = 22, bold = true, color = C.text })
    y = para(s, l.x + th + 26, y + 4, l.w - th - 26, 22, C.text, false) + 12
  end
  para("Pads you have paired before connect on their own.", l.x, y + 4, l.w, 20, C.secondary, false)
  bottom_buttons({ { "Stop searching", "B" } })
  panel(r.x, r.y, r.w, r.h)
  gfx.text("Nearby pads", r.x + 24, r.y + 18, { size = 24, bold = true, color = C.text })
  chip("checking", r.x + r.w - 24 - chip_w("checking", 18, "Searching"), r.y + 20, 18, "Searching")
  local ry = r.y + 64
  ry = ry + player_row(r.x + 20, ry, r.w - 40, "BT", true, "Wireless Controller", false, "Bluetooth  ·  not on the Tested list yet", true) + 12
  ry = ry + player_row(r.x + 20, ry, r.w - 40, "BT", true, "Pro Controller", true, "Bluetooth  ·  Tested") + 12
  para("Not in the list? Check the pad is still flashing, then wait a moment.", r.x + 24, ry + 4, r.w - 48, 20, C.secondary, false)
end

-- 3 Games folders ------------------------------------------------------

-- One row per family: mark, name, a folder or status line, and a state.
local function family_row(fam, x, y, w, rh, line, kind, label, focused, extra)
  gfx.rect(x, y, w, rh, C.surface)
  outline(x, y, w, rh, focused and 4 or 1, focused and C.accent or C.divider)
  local mh = rh - 16
  local mw = math.floor(mh * 1.4)
  family_mark(fam, x + 8, y + 8, mw, mh)
  local tx = x + 8 + mw + 18
  local cw = kind and chip_w(kind, 18, label) or 0
  local avail = w - (tx - x) - cw - 32
  local nh = line_h(22, true)
  local top = y + (rh - nh - line_h(20) - 6) / 2
  gfx.text(fam.name, tx, top, { size = 22, bold = true, color = C.text, max_w = avail })
  gfx.text(line, tx, top + nh + 6, { size = 20, color = extra or C.secondary, max_w = avail })
  if kind then chip(kind, x + w - 16 - cw, y + (rh - line_h(18, true) - 8) / 2, 18, label) end
end

local function rows_area(y)
  local f = frame()
  local bottom = f.bottom - button_h() - 26
  local gap = 10
  local rh = math.floor(math.min(92, (bottom - y - gap * 3) / 4))
  return f, rh, gap
end

local function folder_screen(title, body, rows, btns)
  header("Games folders", nil, 3)
  local f = frame()
  local y = para(title, f.x, f.y, f.w, small() and 32 or 36, C.text, true, 2) + 4
  if body then y = para(body, f.x, y, f.w, 20, C.secondary, false) + 8 end
  local _, rh, gap = rows_area(y)
  for i, r in ipairs(rows) do
    family_row(FAMILIES[i], f.x, y, f.w, rh, r[1], r[2], r[3], r.focused, r.color)
    y = y + rh + gap
  end
  bottom_buttons(btns)
end

local function draw_folders_suggested()
  folder_screen("Where are your games?",
    "Choose a folder for each computer, on a USB drive, the SD card or a network share. FES only reads these folders; it never moves, renames or creates files.",
    {
      { "USB drive  /  games/ZX81", "suggested" },
      { "USB drive  /  games/Spectrum", "suggested" },
      { "No likely folder found", "choose" },
      { "Skipped: Home will remind you", "skipped" },
    },
    { { "Scan 2 folders", "A", filled = true, focused = true }, { "Skip for now" } })
end

local function draw_folders_scanning()
  folder_screen("Looking for games", "Checking each folder. You can stop and come back to this from Settings.",
    {
      { "USB drive  /  games/ZX81  ·  38 games", "done" },
      { "USB drive  /  games/Spectrum  ·  212 files checked so far", "checking", "Scanning" },
      { "SD card  /  ST  ·  next", "skipped", "Waiting" },
      { "No folder", "skipped" },
    },
    { { "Stop scanning", "B", focused = true } })
end

local function draw_folders_results()
  folder_screen("Found 279 games", nil,
    {
      { "USB drive  /  games/ZX81", "done", "38 games" },
      { "USB drive  /  games/Spectrum  ·  12 files not recognised", "done", "241 games" },
      { "No games found in SD card  /  ST", "nogames" },
      { "No folder chosen", "skipped" },
    },
    { { "Continue", "A", filled = true, focused = true }, { "Change a folder" }, { "Scan again" } })
end

-- 4 Ready --------------------------------------------------------------

local function draw_ready(skipped)
  header("Ready", nil, 4)
  local f = frame()
  local y = para(skipped and "You can play now" or "You're ready to play", f.x, f.y, f.w, small() and 32 or 36, C.text, true, 2) + 4
  local pad = skipped and "Keyboard only  ·  plug in or pair a pad any time" or "USB gamepad is Player 1  ·  Tested"
  local cw = gfx.measure("Controller", 20, true)
  gfx.text("Controller", f.x, y, { size = 20, bold = true, color = C.text })
  gfx.text(pad, f.x + cw + 16, y, { size = 20, color = C.secondary, max_w = f.w - cw - 16 })
  y = y + line_h(20, true) + 14
  local rows
  if skipped then
    rows = {
      { "No games folder  ·  built-in firmware", "nogames" },
      { "No games folder  ·  built-in firmware", "nogames" },
      { "No games folder  ·  built-in firmware (EmuTOS)", "nogames" },
      { "No games folder  ·  needs your firmware files", "firmware" },
    }
  else
    rows = {
      { "38 games  ·  built-in firmware", "ready" },
      { "241 games  ·  built-in firmware", "ready" },
      { "No games yet  ·  built-in firmware (EmuTOS)", "nogames" },
      { "No folder  ·  needs your firmware files", "firmware" },
    }
  end
  local _, rh, gap = rows_area(y)
  for i, r in ipairs(rows) do
    family_row(FAMILIES[i], f.x, y, f.w, rh, r[1], r[2])
    y = y + rh + gap
  end
  local by = bottom_buttons({ { "Finish", "A", filled = true, focused = true }, { "Back" } })
  local note = "Change any of this later in Settings."
  local nw = gfx.measure(note, 20)
  gfx.text(note, f.x + f.w - nw, by + (button_h() - line_h(20)) / 2, { size = 20, color = C.secondary })
end

-- 5 Home ---------------------------------------------------------------

local FIRST = {
  { kind = "ready", line = "38 games" },
  { kind = "ready", line = "241 games" },
  { kind = "nogames", line = "Built-in firmware" },
  { kind = "firmware", line = "Add your ROM files" },
}

local SKIPPED = {
  { kind = "nogames", line = "Built-in firmware" },
  { kind = "nogames", line = "Built-in firmware" },
  { kind = "nogames", line = "Built-in firmware" },
  { kind = "firmware", line = "Add your ROM files" },
}

local RECENT = {
  { title = "Lunar Courier", model = "ZX Spectrum 48K", when = "Yesterday", ground = "#9690B5", seed = 0.2 },
  { title = "Bit Garden", model = "Sinclair ZX81", when = "2 days ago", ground = "#7097A8", seed = 0.6 },
  { title = "Pixel Post", model = "ZX Spectrum 128K", when = "Last week", ground = "#6E8F80", seed = 0.4 },
}

-- The hero text column, laid out like room_hero.go: kicker, title, meta,
-- state and headline, explanation, then the actions at the bottom.
local function hero_text(c)
  local t = H().text
  local y = t.y
  gfx.text(string.upper(c.kicker), t.x, y, { size = 16, bold = true, color = C.secondary, max_w = t.w })
  y = y + line_h(16, true) + 2
  gfx.text(c.title, t.x, y, { size = 36, bold = true, color = C.text, max_w = t.w })
  y = y + line_h(36, true) + 2
  if c.meta then
    gfx.text(c.meta, t.x, y, { size = 20, color = C.secondary, max_w = t.w })
    y = y + line_h(20) + 12
  end
  local cw, ch = chip(c.state, t.x, y, 20)
  gfx.text(c.headline, t.x + cw + 14, y + (ch - line_h(22)) / 2, { size = 22, color = C.text, max_w = t.w - cw - 14 })
  y = y + ch + 10
  local bh = button_h()
  local limit = t.y + t.h - bh - 10
  if c.explain then
    -- Lines that do not fit are dropped; the last shown line says so.
    local lh = line_h(20) + 4
    local lines = wrap(c.explain, 20, t.w, false)
    for i, l in ipairs(lines) do
      if y + lh > limit then break end
      if i < #lines and y + 2 * lh > limit then l = l .. " …" end
      gfx.text(l, t.x, y, { size = 20, color = C.secondary, max_w = t.w })
      y = y + lh
    end
  end
  buttons(c.actions, t.x + 6, t.y + t.h - bh - 4)
end

local function hero_card(c, art)
  local h = H()
  panel(h.card.x, h.card.y, h.card.w, h.card.h)
  art(h.art.x, h.art.y, h.art.w, h.art.h)
  hero_text(c)
end

local function shelf_label(label, x, y, w, right)
  gfx.text(label, x, y, { size = 22, bold = true, color = C.text })
  if right then gfx.text(right, x, y + 3, { size = 18, color = C.secondary, max_w = w, align = "right" }) end
  return line_h(22, true) + 8
end

-- Four family tiles. Tall tiles carry the mark above the name; compact
-- tiles (under a Recently played row) put a small mark beside it.
local function family_tiles(states, focus, x, y, w, h)
  local gap = 20
  local tw = (w - gap * 3) / 4
  for i, f in ipairs(FAMILIES) do
    local tx = x + (i - 1) * (tw + gap)
    local st = states[i]
    gfx.rect(tx, y, tw, h, C.surface)
    local cy
    if h >= 150 then
      local ah = math.floor(h * 0.52)
      family_mark(f, tx, y, tw, ah)
      gfx.text(f.name, tx + 14, y + ah + 8, { size = 22, bold = focus == i, color = C.text, max_w = tw - 28 })
      cy = y + ah + 14 + line_h(22, true)
      local cw = chip(st.kind, tx + 14, cy, 16)
      if cy + line_h(16, true) + 8 + line_h(18) + 10 <= y + h then
        gfx.text(st.line, tx + 14, cy + line_h(16, true) + 14, { size = 18, color = C.secondary, max_w = tw - 28 })
      end
    else
      local mh = h - 16
      local mw = math.floor(mh * (small() and 0.9 or 1.15))
      local cs = small() and 14 or 16
      family_mark(f, tx + 8, y + 8, mw, mh)
      local nx = tx + 8 + mw + 12
      local top = y + (h - line_h(20, true) - line_h(cs, true) - 16) / 2
      gfx.text(f.short, nx, top, { size = 20, bold = focus == i, color = C.text, max_w = tx + tw - nx - 8 })
      chip(st.kind, nx, top + line_h(20, true) + 8, cs)
    end
    if focus == i then
      outline(tx - 4, y - 4, tw + 8, h + 8, 4, C.accent)
    else
      outline(tx, y, tw, h, 1, C.divider)
    end
  end
end

local function recent_row(focus, x, y, w, h)
  local gap = 20
  local cw = (w - gap * 2) / 3
  for i, r in ipairs(RECENT) do
    local cx = x + (i - 1) * (cw + gap)
    gfx.rect(cx, y, cw, h, C.surface)
    local aw = math.floor((h - 12) * 1.33)
    game_art(r.seed, r.ground, cx + 6, y + 6, aw, h - 12)
    local nx = cx + 6 + aw + 12
    local ts = small() and 18 or 20
    local top = y + (h - line_h(ts, true) - line_h(16) - 4) / 2
    gfx.text(r.title, nx, top, { size = ts, bold = focus == i, color = C.text, max_w = cx + cw - nx - 10 })
    gfx.text(r.model .. "  ·  " .. r.when, nx, top + line_h(ts, true) + 4, { size = 16, color = C.secondary, max_w = cx + cw - nx - 10 })
    if focus == i then
      outline(cx - 4, y - 4, cw + 8, h + 8, 4, C.accent)
    else
      outline(cx, y, cw, h, 1, C.divider)
    end
  end
end

local function banner(x, y, w, h)
  gfx.rect(x, y, w, h, C.raised)
  gfx.rect(x, y, 8, h, C.warm)
  local bw = gfx.measure("Finish setup", 20, true) + 32
  local bh = line_h(20, true) + 16
  local th = line_h(20, true)
  local text_w = w - bw - 48
  gfx.text("Setup isn't finished", x + 24, y + (h - th - line_h(18) - 4) / 2, { size = 20, bold = true, color = C.text, max_w = text_w })
  gfx.text("No games folders or controller yet. Your choices so far are kept.", x + 24, y + (h - th - line_h(18) - 4) / 2 + th + 4,
    { size = 18, color = C.secondary, max_w = text_w })
  local bx, by = x + w - bw - 16, y + (h - bh) / 2
  gfx.rect(bx, by, bw, bh, C.surface)
  outline(bx, by, bw, bh, 2, C.divider)
  gfx.text("Finish setup", bx + 16, by + 8, { size = 20, bold = true, color = C.text })
end

-- states, focus ("tile", n) or ("recent", n), and whether the Recently
-- played row or the unfinished-setup banner sits above the tiles.
local function home_shelf(states, focus_kind, focus_i, top)
  local s = H().shelf
  local y = s.y
  if top == "recent" or top == "banner" then
    y = y + shelf_label(top == "recent" and "Recently played" or "Get started", s.x, y, s.w)
    local rh = small() and 52 or 62
    if top == "recent" then
      recent_row(focus_kind == "recent" and focus_i or nil, s.x, y, s.w, rh)
    else
      banner(s.x, y, s.w, rh)
    end
    y = y + rh + (small() and 10 or 14)
    y = y + shelf_label("Computers", s.x, y, s.w)
    family_tiles(states, focus_kind == "tile" and focus_i or nil, s.x, y, s.w, s.y + s.h - y)
  else
    y = y + shelf_label("Computers", s.x, y, s.w, "4 on this kit")
    family_tiles(states, focus_kind == "tile" and focus_i or nil, s.x, y, s.w, s.y + s.h - y)
  end
end

local function mark_art(i)
  return function(x, y, w, h) family_mark(FAMILIES[i], x, y, w, h) end
end

local HERO = {
  [1] = { kicker = "Computer", title = "Sinclair ZX81", meta = FAMILIES[1].models, state = "ready",
    headline = "38 games  ·  firmware built in", explain = "Open it to choose a model and a game.",
    actions = { { "Open", "A", filled = true }, { "Details", "Y" } } },
  [3] = { kicker = "Computer", title = "Atari ST", meta = FAMILIES[3].models, state = "nogames",
    headline = "No games found in SD card / ST",
    explain = "Choose another folder, or open the ST anyway on its built-in firmware.",
    actions = { { "Open", "A", filled = true }, { "Choose folder" } } },
  [4] = { kicker = "Computer", title = "Commodore 64", meta = FAMILIES[4].models, state = "firmware",
    headline = "Add your C64 ROM files to play",
    explain = "FES can't include Commodore's ROMs. Add firmware shows what to copy.",
    actions = { { "Add firmware", "A", filled = true }, { "Details", "Y" } } },
  skipped = { kicker = "Computer", title = "Sinclair ZX81", meta = FAMILIES[1].models, state = "nogames",
    headline = "No games folder chosen",
    explain = "You can still open it and type BASIC.",
    actions = { { "Open", "A", filled = true }, { "Finish setup" } } },
}

local function home_header()
  header("Home", { { label = "Settings" } })
end

local function draw_home(which)
  home_header()
  if which == "first" then
    hero_card(HERO[1], mark_art(1))
    home_shelf(FIRST, "tile", 1, nil)
  elseif which == "recent" then
    local r = RECENT[1]
    hero_card({ kicker = "Recently played", title = r.title, meta = r.model .. "  ·  keyboard or pad", state = "ready",
      headline = "Plays on this TV", explain = "Stop: F12, Guide, or hold Select+Start for one second.",
      actions = { { "Play", "A", filled = true }, { "Details", "Y" } } },
      function(x, y, w, h) game_art(r.seed, r.ground, x, y, w, h) end)
    home_shelf(FIRST, "recent", 1, "recent")
  elseif which == "needs-firmware" then
    hero_card(HERO[4], mark_art(4))
    home_shelf(FIRST, "tile", 4, "recent")
  elseif which == "no-games" then
    hero_card(HERO[3], mark_art(3))
    home_shelf(FIRST, "tile", 3, nil)
  elseif which == "skipped" then
    hero_card(HERO.skipped, mark_art(1))
    home_shelf(SKIPPED, "tile", 1, "banner")
  end
end

-- 6 First use of a family: the firmware info card over Home ------------

local function draw_card(c)
  local f = frame()
  gfx.rect(0, 0, room.width, room.height, C.dim)
  local w = math.min(820, f.w - 80)
  local pad = 28
  local inner = w - pad * 2
  -- Measure first so the card fits its copy.
  local h = pad + line_h(16, true) + 4 + line_h(32, true) + 14
  for _, p in ipairs(c.body) do
    h = h + #wrap(p.text, p.size or 22, inner - (p.box and 32 or 0), p.bold) * (line_h(p.size or 22, p.bold) + 4) + (p.box and 24 or 0) + 12
  end
  h = h + 10 + button_h() + pad
  local x = (room.width - w) / 2
  local y = math.max(f.y - 10, (room.height - h) / 2 - 10)
  gfx.rect(x - 4, y - 4, w + 8, h + 8, C.divider)
  gfx.rect(x, y, w, h, C.surface)
  local ty = y + pad
  gfx.text(string.upper(c.kicker), x + pad, ty, { size = 16, bold = true, color = C.secondary })
  ty = ty + line_h(16, true) + 4
  gfx.text(c.title, x + pad, ty, { size = 32, bold = true, color = C.text, max_w = inner })
  ty = ty + line_h(32, true) + 14
  for _, p in ipairs(c.body) do
    local size = p.size or 22
    if p.box then
      local n = #wrap(p.text, size, inner - 32, p.bold)
      local bh = n * (line_h(size, p.bold) + 4) + 20
      gfx.rect(x + pad, ty, inner, bh, C.raised)
      para(p.text, x + pad + 16, ty + 10, inner - 32, size, C.text, p.bold)
      ty = ty + bh + 4 + 12
    else
      ty = para(p.text, x + pad, ty, inner, size, p.color or C.text, p.bold) + 12
    end
  end
  buttons(c.actions, x + pad + 6, y + h - pad - button_h())
end

local function draw_card_built_in()
  home_header()
  hero_card(HERO[3], mark_art(3))
  home_shelf(FIRST, "tile", 3, nil)
  draw_card({
    kicker = "First time on the Atari ST",
    title = "Built-in firmware: EmuTOS",
    body = {
      { text = "The Atari ST starts with EmuTOS, a free replacement for Atari's TOS. It comes with FES, so there is nothing to add." },
      { text = "Licence: GNU General Public License. Licence shows the full text and where its source code is.", box = true, size = 20 },
      { text = "Prefer the original TOS? Put your own TOS file in your firmware folder and choose it in Atari ST › Details.", size = 20, color = C.secondary },
      { text = "Shown once. Open it again from Atari ST › Details.", size = 18, color = C.secondary },
    },
    actions = { { "Continue to Atari ST", "A", filled = true, focused = true }, { "Licence", "Y" } },
  })
end

local function draw_card_your_rom()
  home_header()
  hero_card(HERO[4], mark_art(4))
  home_shelf(FIRST, "tile", 4, nil)
  draw_card({
    kicker = "First time on the Commodore 64",
    title = "The C64 needs your firmware",
    body = {
      { text = "FES can't include Commodore's original ROMs. Copy your own ROM files into this folder:" },
      { text = "USB drive  /  firmware  /  C64", box = true, bold = true },
      { text = "You need the BASIC, KERNAL and character ROMs, plus the 1541 drive ROM. FES checks them when you open the C64, and Details lists anything missing.", size = 20, color = C.secondary },
      { text = "Shown once. Open it again from Commodore 64 › Details.", size = 18, color = C.secondary },
    },
    actions = { { "Got it", "A", filled = true, focused = true }, { "Show folder", "Y" } },
  })
end

-- Dispatch -------------------------------------------------------------

local SCENES = {
  ["splash"] = draw_splash,
  ["welcome"] = function() draw_welcome(false) end,
  ["welcome-skip"] = function() draw_welcome(true) end,
  ["pads-waiting"] = draw_pads_waiting,
  ["pads-found"] = draw_pads_found,
  ["pads-bluetooth"] = draw_pads_bluetooth,
  ["folders-suggested"] = draw_folders_suggested,
  ["folders-scanning"] = draw_folders_scanning,
  ["folders-results"] = draw_folders_results,
  ["ready"] = function() draw_ready(false) end,
  ["ready-skipped"] = function() draw_ready(true) end,
  ["home-first"] = function() draw_home("first") end,
  ["home-recent"] = function() draw_home("recent") end,
  ["home-needs-firmware"] = function() draw_home("needs-firmware") end,
  ["home-no-games"] = function() draw_home("no-games") end,
  ["home-skipped"] = function() draw_home("skipped") end,
  ["card-built-in"] = draw_card_built_in,
  ["card-your-rom"] = draw_card_your_rom,
}

function draw()
  local fn = SCENES[scene]
  if fn then
    fn()
  else
    gfx.text("Unknown scene " .. scene, 40, 40, { size = 28, bold = true, color = C.text })
  end
  gfx.paint({ { "c", C.background } })
end
