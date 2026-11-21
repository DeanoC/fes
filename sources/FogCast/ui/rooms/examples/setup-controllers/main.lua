-- First-run setup, step 2 (#980): find the player's pad, let them try its
-- buttons, and swap A and B if they feel the wrong way round. FES lists
-- the kit's pads in room.pads(): it makes the first pad Player 1 and the
-- second Player 2 as they attach. A Bluetooth pad pairs on its own when
-- its pairing button is held, so this step only says how. Mapping a pad
-- button by button is Settings › Controllers, which the controllers action
-- opens over this room and closes back to it.

local F = require "families"
local S = require "setup"
local C = F.colors

-- A held button's colour on the pad picture.
local LIT = "#EFD38C"

local WAITING_BUTTONS = {
  { prompt = "Enter", label = "Continue with keyboard", filled = true, action = "setup_continue" },
}

local focus = 1
local pads, gen = {}, 0
-- shown is the pad the picture follows: the last one to press a button.
local shown = 1

-- The glyphs of the buttons that choose and go back on Player 1's pad.
-- A pad mapped step by step has no fixed glyph for them.
local function confirm_back()
  local p = pads[1]
  if p.mapped then return "A", "B" end
  if p.swap_ab then return p.glyphs.east, p.glyphs.south end
  return p.glyphs.south, p.glyphs.east
end

local function buttons()
  if #pads == 0 then return WAITING_BUTTONS end
  local p = pads[1]
  return {
    { prompt = (confirm_back()), label = "Continue", filled = true, action = "setup_continue" },
    { label = p.swap_ab and "Swap A / B: on" or "Swap A / B", action = "setup_swap_ab" },
    { label = "Controller settings", action = "controllers" },
  }
end

local function publish()
  local list = buttons()
  focus = math.max(1, math.min(focus, #list))
  local b = list[focus]
  destination.set{ kind = "action", action = b.action, label = b.label, status = b.label }
end

local function any_pressed(p)
  return next(p.pressed) ~= nil
end

local function refresh()
  local list, g = room.pads()
  if g == gen then return false end
  local before = #pads
  pads, gen = list, g
  for i, p in ipairs(pads) do
    if any_pressed(p) then shown = i end
  end
  if shown > #pads then shown = 1 end
  -- The first pad to arrive puts focus on Continue.
  if before == 0 and #pads > 0 then focus = 1 end
  publish()
  return true
end

function load()
  refresh()
  publish()
end

function update() refresh() end

function on_input(cmd)
  local list = buttons()
  if cmd == "left" or cmd == "up" then
    focus = math.max(1, focus - 1)
  elseif cmd == "right" or cmd == "down" then
    focus = math.min(#list, focus + 1)
  else
    return false
  end
  publish()
  return true
end

local function focus_hit(id)
  local n = tonumber(string.match(id or "", "^button:(%d+)$"))
  if not n or not buttons()[n] then return end
  focus = n
  publish()
end
function on_hover(id) focus_hit(id) end
function on_activate(id) focus_hit(id) end

local LAYOUTS = { xbox = "Xbox layout", playstation = "PlayStation layout", nintendo = "Nintendo layout", generic = "Standard layout" }

-- One line on how FES reads a pad.
local function reads(p)
  local parts = { p.bluetooth and "Bluetooth" or "Wired", LAYOUTS[p.family] or LAYOUTS.generic }
  if p.mapped then
    parts[#parts + 1] = "mapped step by step"
  elseif p.swap_ab then
    parts[#parts + 1] = "A and B swapped"
  elseif p.recognised then
    parts[#parts + 1] = "buttons as printed"
  else
    parts[#parts + 1] = "not in FES's pad list"
  end
  return table.concat(parts, "  ·  ")
end

-- A row of the player list: a badge, a name and a detail line.
local function player_row(x, y, w, h, badge, name, detail, muted, compact)
  gfx.rect(x, y, w, h, C.surface)
  F.outline(x, y, w, h, 1, C.divider)
  local pad = compact and 10 or 14
  local bs = compact and 16 or 20
  local bw, bh = gfx.measure(badge, bs, true)
  bw = math.max(bw + 16, compact and 36 or 44)
  local by = y + pad
  gfx.rect(x + pad, by, bw, bh + 8, muted and C.raised or C.accent)
  gfx.text(badge, x + pad + (bw - gfx.measure(badge, bs, true)) / 2, by + 4, { size = bs, bold = true, color = muted and C.secondary or C.on_accent })
  local tx = x + pad + bw + 14
  local ns = compact and 18 or 22
  gfx.text(name, tx, y + pad - 2, { size = ns, bold = true, color = muted and C.secondary or C.text, max_w = w - (tx - x) - pad })
  gfx.text(detail, tx, y + pad + ns + (compact and 2 or 4), { size = compact and 14 or 17, color = C.secondary, max_w = w - (tx - x) - pad })
end

-- A disc of rows, for the pad's grips.
local function disc(cx, cy, r, color)
  local list = {}
  local steps = 8
  for i = 0, steps - 1 do
    local dy = (i + 0.5) / steps * 2 - 1
    local half = math.floor(r * math.sqrt(1 - dy * dy))
    list[#list + 1] = cx - half
    list[#list + 1] = math.floor(cy - r + i * r * 2 / steps)
    list[#list + 1] = half * 2
    list[#list + 1] = math.ceil(r * 2 / steps)
  end
  gfx.rects(list, color)
end

local function glyph(g, cx, cy, size, color)
  local gw, gh = gfx.measure(g, size, true)
  gfx.text(g, cx - gw / 2, cy - gh / 2, { size = size, bold = true, color = color })
end

-- The pad picture in x, y, w, h, with p's held buttons lit. p is nil while
-- no pad is attached.
local function pad_picture(x, y, w, h, p)
  local held = p and p.pressed or {}
  local function tone(name, base) return held[name] and LIT or base end
  local bw = math.floor(math.min(w, h * 2.3))
  local bh = math.floor(bw * 0.3)
  local bx = x + math.floor((w - bw) / 2)
  local by = y + math.floor((h - bh * 1.55) / 2) + math.floor(bh * 0.12)
  -- Shoulders, then the grips and body.
  local sw, sh = math.floor(bw * 0.17), math.floor(bh * 0.12)
  gfx.rect(bx + math.floor(bw * 0.13), by - sh, sw, sh, tone("l", C.divider))
  gfx.rect(bx + bw - math.floor(bw * 0.13) - sw, by - sh, sw, sh, tone("r", C.divider))
  local gr = math.floor(bh * 0.5)
  disc(bx + gr, by + bh, gr, C.dark)
  disc(bx + bw - gr, by + bh, gr, C.dark)
  gfx.rect(bx + math.floor(bw * 0.08), by, bw - math.floor(bw * 0.16), bh, C.dark)
  gfx.rect(bx, by + math.floor(bh * 0.5), bw, math.floor(bh * 0.5), C.dark)
  -- The D-pad: a cross whose arms light one by one.
  local dc, dcy = bx + math.floor(bw * 0.25), by + math.floor(bh * 0.55)
  local arm = math.floor(bh * 0.2)
  local t = math.floor(arm * 0.95)
  gfx.rect(dc - t / 2, dcy - t / 2, t, t, C.raised)
  gfx.rect(dc - t / 2, dcy - t / 2 - arm, t, arm, tone("up", C.raised))
  gfx.rect(dc - t / 2, dcy + t / 2, t, arm, tone("down", C.raised))
  gfx.rect(dc - t / 2 - arm, dcy - t / 2, arm, t, tone("left", C.raised))
  gfx.rect(dc + t / 2, dcy - t / 2, arm, t, tone("right", C.raised))
  -- Select and Start.
  local mw, mh = math.floor(bw * 0.06), math.max(6, math.floor(bh * 0.08))
  gfx.rect(bx + math.floor(bw * 0.44) - mw, dcy - mh / 2, mw, mh, tone("select", C.divider))
  gfx.rect(bx + math.floor(bw * 0.56), dcy - mh / 2, mw, mh, tone("start", C.divider))
  -- The face buttons, each drawn as the pad's own glyph.
  local fc, step = bx + math.floor(bw * 0.75), math.floor(bh * 0.24)
  local size = math.floor(bh * 0.25)
  local g = p and p.glyphs or {}
  local faces = {
    { "north", 0, -1 }, { "south", 0, 1 }, { "west", -1, 0 }, { "east", 1, 0 },
  }
  for _, f in ipairs(faces) do
    local cx, cy = fc + f[2] * step, dcy + f[3] * step
    if g[f[1]] and g[f[1]] ~= "" then
      glyph(g[f[1]], cx, cy, size, tone(f[1], C.raised))
    else
      disc(cx, cy, math.floor(size * 0.45), tone(f[1], C.raised))
    end
  end
  return by + bh + gr
end

-- What the player just pressed, as a filled chip, or nil.
local function pressed_line(p)
  local order = { "south", "east", "west", "north", "l", "r", "select", "start" }
  for _, name in ipairs(order) do
    if p.pressed[name] then
      local line = p.glyphs[name] .. " pressed"
      local own = ({ south = "A", east = "B", west = "X", north = "Y", l = "L", r = "R", select = "Select", start = "Start" })[name]
      if p.sends[name] and p.sends[name] ~= own then
        line = line .. "  ·  FES reads " .. p.sends[name]
      end
      return line
    end
  end
  for _, dir in ipairs({ "up", "down", "left", "right" }) do
    if p.pressed[dir] then return p.glyphs.dpad .. " " .. dir end
  end
  return nil
end

local function chip(text, x, y, size, filled)
  local tw, th = gfx.measure(text, size, true)
  local pad = math.floor(size * 0.55)
  local h = th + math.floor(size * 0.5)
  if filled then
    gfx.rect(x, y, tw + pad * 2, h, C.accent)
    gfx.text(text, x + pad, y + (h - th) / 2, { size = size, bold = true, color = C.on_accent })
  else
    F.outline(x, y, tw + pad * 2, h, 2, C.secondary)
    gfx.text(text, x + pad, y + (h - th) / 2, { size = size, bold = true, color = C.text })
  end
  return x + tw + pad * 2, h
end

local painted = nil

function draw()
  if room.presented and painted and painted.focus == focus and painted.gen == gen
    and painted.w == room.width and painted.h == room.height and gfx.reuse() then
    return
  end
  local lay = S.layout()
  local m = lay.margin
  S.header(lay, room.title)
  local compact = room.height < 600
  local big = compact and 28 or (room.height >= 648 and 40 or 34)
  local body = compact and 17 or (room.height >= 648 and 24 or 21)
  local small = compact and 14 or body - 4
  local left_w = math.floor((room.width - m * 3) * 0.48)
  local px = m * 2 + left_w
  local pw = room.width - m - px
  local buttons_y = lay.bottom - 24 - 48
  local ph = buttons_y - 20 - lay.top
  local y = lay.top
  local list = buttons()

  gfx.rect(px, lay.top, pw, ph, C.surface)
  F.outline(px, lay.top, pw, ph, 2, C.divider)
  local inner = compact and 14 or 22

  if #pads == 0 then
    gfx.text("Pick up a controller", m, y, { size = big, bold = true, color = C.text })
    y = y + math.floor(big * 1.5)
    y = S.text("Plug in a USB pad, or switch on a Bluetooth pad. FES names it and makes it Player 1.", m, y, left_w, body, C.text)
    y = y + (compact and 4 or 8)
    y = S.text("No pad? The keyboard works everywhere: arrows move and Enter chooses.", m, y, left_w, small, C.secondary)
    y = y + (compact and 8 or 14)
    local note = "Bluetooth pad? Hold its pairing button until the light flashes quickly. FES pairs it by itself. Pads paired before connect when you switch them on."
    local pad = compact and 12 or 18
    local lines = S.wrap(note, small, left_w - pad * 2)
    local note_h = #lines * math.floor(small * 1.35) + (compact and 16 or 24)
    gfx.rect(m, y, left_w, note_h, C.raised)
    F.outline(m, y, left_w, note_h, 2, C.divider)
    S.text(note, m + pad, y + (compact and 8 or 12), left_w - pad * 2, small, C.text)

    local bottom = pad_picture(px + inner, lay.top + inner, pw - inner * 2, math.floor(ph * 0.62), nil)
    local cx, ch = chip("Waiting", px + inner, bottom + inner, compact and 15 or 18, false)
    gfx.text("Listening on USB and Bluetooth…", cx + 14, bottom + inner + (ch - body) / 2, { size = body - 2, color = C.text, max_w = px + pw - inner - cx - 14 })
    S.buttons(m, buttons_y, list, focus)
    S.footer(lay, "Enter continue with keyboard")
  else
    local players, taken = 0, {}
    for _, p in ipairs(pads) do
      if p.player > 0 then players, taken[p.player] = players + 1, true end
    end
    -- The one player a lone pad is not: a pad that stays Player 2 after
    -- Player 1's pad leaves still leaves room for Player 1.
    local missing = taken[1] and 2 or 1
    local title = "Your controller is ready"
    if players == 1 then title = "Player " .. (3 - missing) .. " is ready" end
    if players >= 2 then title = "Players 1 and 2 are ready" end
    gfx.text(title, m, y, { size = big, bold = true, color = C.text })
    y = y + math.floor(big * 1.45)
    local row_h = compact and 56 or 72
    local gap = compact and 8 or 12
    local rows = {}
    for _, p in ipairs(pads) do
      rows[#rows + 1] = { badge = p.player > 0 and ("P" .. p.player) or "Menu", name = p.name ~= "" and p.name or "Gamepad", detail = reads(p) }
    end
    if players == 1 then
      rows[#rows + 1] = { badge = "P" .. missing, name = "Add Player " .. missing, detail = "Plug in or switch on another pad", muted = true }
    end
    rows[#rows + 1] = { badge = "Keys", name = "Keyboard", detail = "Works everywhere, alongside your pads", muted = true }
    local room_for = math.floor((buttons_y - 20 - y + gap) / (row_h + gap))
    for i = 1, math.min(#rows, room_for) do
      local r = rows[i]
      player_row(m, y, left_w, row_h, r.badge, r.name, r.detail, r.muted, compact)
      y = y + row_h + gap
    end

    local p = pads[shown] or pads[1]
    local ty = lay.top + inner
    local heading = "Try your buttons"
    if #pads > 1 then heading = heading .. "  ·  " .. (p.player > 0 and ("P" .. p.player) or "Menu") end
    gfx.text(heading, px + inner, ty, { size = body, bold = true, color = C.text, max_w = pw - inner * 2 })
    ty = ty + math.floor(body * 1.6)
    local bottom = pad_picture(px + inner, ty, pw - inner * 2, math.floor(ph * 0.5), p)
    local line = pressed_line(p)
    local cy = bottom + (compact and 8 or 14)
    local _, ch
    if line then
      _, ch = chip(line, px + inner, cy, compact and 15 or 18, true)
    else
      _, ch = chip("Press any button", px + inner, cy, compact and 15 or 18, false)
    end
    local hint = "Buttons the wrong way round? Swap A / B here. To change any button, use Controller settings, now or later in Settings."
    if not p.recognised and not p.mapped then
      hint = "FES doesn't know this pad yet, so it guesses its layout. If the buttons are wrong, Controller settings maps them one by one."
    end
    S.text(hint, px + inner, cy + ch + (compact and 8 or 14), pw - inner * 2, small, C.secondary)

    S.buttons(m, buttons_y, list, focus)
    local ok, back = confirm_back()
    S.footer(lay, "D-pad move  ·  " .. ok .. " choose  ·  " .. back .. " back")
  end
  painted = { focus = focus, gen = gen, w = room.width, h = room.height }
  gfx.paint({ { "c", C.background } })
end
