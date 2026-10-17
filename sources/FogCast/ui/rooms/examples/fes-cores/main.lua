-- FES Home on a kit: the Cartridge Lounge layout over installed cores.
--
-- The shelf lists what kit.cores reports, names verbatim. FES draws the
-- selected item's title, input needs, readiness, destination and actions
-- in the hero text column (room.toml destination_panel = "hero"); this
-- script draws the card, artwork motif, shelf and header routes.
--
-- Up reaches Library, Rooms and Settings from the shelf, so a pad without
-- a Guide button still reaches the system menu with Direction and Confirm.
-- The list arrives once; it never moves the player's focus, and the last
-- selected package is restored on a later visit.

local C = {
  background = "#F4F0E8",
  surface = "#FFFCF5",
  raised = "#E9E6DC",
  text = "#243631",
  secondary = "#53625A",
  divider = "#CECFC0",
  accent = "#235A46",
  on_accent = "#FCFCF3",
  dark = "#22342E",
}

-- Muted motif grounds. A core's id picks one so it keeps its colour.
local GROUNDS = { "#7097A8", "#9690B5", "#E08A63", "#8DB07A", "#D9B45A", "#6E8F80" }

local MENU = {
  { id = "library", label = "Library" },
  { id = "rooms", label = "Rooms" },
  { id = "settings", label = "Settings" },
}

local LOBBY = "example.lobby"

local cores = nil      -- nil while listing; a table once kit.cores answers
local unavailable = false
local row = "shelf"    -- "shelf" or "menu"
local shelf_i = 1
local menu_i = 1
local first = 1        -- first visible shelf card
local status = "Looking for this kit's machines…"

local function layout()
  return room.hero
end

local function hash(s)
  local h = 7
  for i = 1, #(s or "") do
    h = (h * 31 + string.byte(s, i)) % 1000003
  end
  return h
end

local function has_input(core, name)
  for _, v in ipairs(core.inputs or {}) do
    if v == name then return true end
  end
  return false
end

-- Short shelf line. Readiness itself is the hero's job: a kit lease can
-- make a launchable core unavailable, so the shelf never says Ready.
local function card_line(core)
  if not core.launchable then
    if core.needs == "parts" then return "Display setup incomplete" end
    if core.needs == "firmware" then return "Needs firmware" end
    if core.needs == "media" then return "Needs a cartridge" end
    return core.block or ""
  end
  local kb, mouse = has_input(core, "keyboard"), has_input(core, "mouse")
  if kb and mouse then return "Keyboard and mouse" end
  if kb then return "Keyboard" end
  if mouse then return "Mouse" end
  return ""
end

local function selected_core()
  if row ~= "shelf" or not cores then return nil end
  return cores[shelf_i]
end

local function publish()
  if row == "menu" then
    local item = MENU[menu_i]
    if item.id == "library" then
      destination.set{ kind = "library", label = "Library" }
    elseif item.id == "rooms" then
      destination.set{ kind = "room", room_id = LOBBY, label = "Rooms" }
    else
      destination.set{ kind = "action", action = "settings", label = "Settings" }
    end
    return
  end
  local core = selected_core()
  if core then
    destination.set{
      kind = "core",
      package_id = core.package_id,
      core_id = core.core_id,
      label = core.name,
      launchable = core.launchable,
      block = core.block or "",
      needs = core.needs or "",
      inputs = core.inputs,
    }
  elseif cores == nil and not unavailable then
    destination.set{ kind = "unresolved", label = "Home", resolving = true, status = status }
  else
    destination.set{ kind = "unresolved", label = "Home", status = status }
  end
end

local function visible_cards()
  local h = layout()
  if not h then return 3 end
  return math.max(1, math.floor((h.shelf.w + 20) / 300))
end

local function keep_visible()
  local n = visible_cards()
  if shelf_i < first then first = shelf_i end
  if shelf_i > first + n - 1 then first = shelf_i - n + 1 end
  if first < 1 then first = 1 end
end

local function remember()
  local core = selected_core()
  if core then store.set("package_id", core.package_id) end
end

function load()
  if kit == nil or kit.cores == nil then
    unavailable = true
    cores = {}
    row = "menu"
    status = "Installed machines appear here on an FES kit."
    publish()
    return
  end
  publish()
  kit.cores(function(rows, err)
    if err then
      unavailable = true
      cores = {}
      status = "This kit's machines can't be listed right now."
      if row == "shelf" then row = "menu" end
      publish()
      return
    end
    cores = rows or {}
    if #cores == 0 then
      status = "Nothing is installed on this kit yet. The Library and Settings are above."
      if row == "shelf" then row = "menu" end
      publish()
      return
    end
    local want = store.get("package_id", "")
    for i, core in ipairs(cores) do
      if core.package_id == want then shelf_i = i end
    end
    keep_visible()
    publish()
  end)
end

local function move(dx, dy)
  if dy < 0 and row == "shelf" then
    row = "menu"
    return true
  end
  if dy > 0 and row == "menu" then
    -- The shelf is reachable while it is still being listed.
    if cores == nil or #cores > 0 then row = "shelf" end
    return true
  end
  if dx ~= 0 then
    if row == "menu" then
      menu_i = math.max(1, math.min(#MENU, menu_i + dx))
    elseif cores and #cores > 0 then
      shelf_i = math.max(1, math.min(#cores, shelf_i + dx))
      keep_visible()
      remember()
    end
    return true
  end
  return dy ~= 0
end

function on_input(cmd)
  local handled = false
  if cmd == "left" then handled = move(-1, 0)
  elseif cmd == "right" then handled = move(1, 0)
  elseif cmd == "up" then handled = move(0, -1)
  elseif cmd == "down" then handled = move(0, 1)
  end
  if handled then publish() end
  return handled
end

local function focus_hit(id)
  local kind, n = string.match(id or "", "^(%a+):(%d+)$")
  n = tonumber(n)
  if kind == "menu" and MENU[n] then
    row, menu_i = "menu", n
  elseif kind == "core" and cores and cores[n] then
    row, shelf_i = "shelf", n
    keep_visible()
    remember()
  else
    return false
  end
  publish()
  return true
end

function on_hover(id) focus_hit(id) end
function on_activate(id) focus_hit(id) end

-- Drawing ------------------------------------------------------------

local function outline(x, y, w, h, t, color)
  gfx.rect(x, y, w, t, color)
  gfx.rect(x, y + h - t, w, t, color)
  gfx.rect(x, y, t, h, color)
  gfx.rect(x + w - t, y, t, h, color)
end

-- A disc from horizontal slices: the device has rectangles only.
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

-- Fallback artwork: a recognisable motif per item, no text over art.
local function motif(core, x, y, w, h)
  local ground = GROUNDS[(hash(core.core_id or core.name) % #GROUNDS) + 1]
  gfx.rect(x, y, w, h, ground)
  if has_input(core, "keyboard") then
    disc(x + w * 0.80, y + h * 0.28, math.min(w, h) * 0.13, C.surface)
    keyboard(x + w * 0.10, y + h * 0.48, w * 0.80, h * 0.40, C.surface, ground)
  else
    disc(x + w * 0.36, y + h * 0.42, math.min(w, h) * 0.24, "#F5E6C8")
    hills(x, y + h * 0.45, w, h * 0.55, C.dark, hash(core.name or "") % 7 / 7)
  end
  if not core.launchable then
    gfx.rect(x, y + h - 10, w, 10, C.raised)
  end
end

local function menu_art(id, x, y, w, h)
  gfx.rect(x, y, w, h, C.raised)
  if id == "library" then
    local spines = { "#7097A8", "#E08A63", "#8DB07A", "#9690B5", "#D9B45A", "#6E8F80" }
    local sw = w / 12
    for i, color in ipairs(spines) do
      local sh = h * (0.45 + 0.07 * (i % 3))
      gfx.rect(x + w * 0.18 + (i - 1) * (sw + 6), y + h * 0.82 - sh, sw, sh, color)
    end
    gfx.rect(x + w * 0.12, y + h * 0.82, w * 0.76, 8, C.dark)
  elseif id == "rooms" then
    for i = 0, 2 do
      local dx = x + w * (0.16 + i * 0.25)
      gfx.rect(dx, y + h * 0.25, w * 0.18, h * 0.55, i == 1 and C.accent or C.dark)
      gfx.rect(dx + w * 0.13, y + h * 0.52, w * 0.025, h * 0.04, C.surface)
    end
  else
    for i = 0, 2 do
      local ly = y + h * (0.28 + i * 0.22)
      gfx.rect(x + w * 0.15, ly, w * 0.70, 6, C.dark)
      local kx = x + w * (0.25 + 0.22 * ((i * 2) % 3))
      gfx.rect(kx, ly - 14, 28, 34, C.accent)
    end
  end
end

local function draw_header(h)
  local hd = h.header
  local m = h.card.x
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
  local _, rh = gfx.measure(room.title, 22)
  gfx.text(room.title, fx + 18, (hd.h - rh) / 2, { size = 22, color = C.text })

  local bh = math.floor(hd.h * 0.62)
  local by = math.floor((hd.h - bh) / 2)
  local right = room.width - m
  for i = #MENU, 1, -1 do
    local item = MENU[i]
    local focused = row == "menu" and menu_i == i
    local tw = gfx.measure(item.label, 20, focused)
    local bw = tw + 36
    local bx = right - bw
    gfx.rect(bx, by, bw, bh, C.surface)
    if focused then
      outline(bx - 3, by - 3, bw + 6, bh + 6, 4, C.accent)
    else
      outline(bx, by, bw, bh, 1, C.divider)
    end
    local _, lh = gfx.measure(item.label, 20, focused)
    gfx.text(item.label, bx + 18, by + (bh - lh) / 2, { size = 20, bold = focused, color = C.text })
    gfx.hit("menu:" .. i, bx, by, bw, bh)
    right = bx - 12
  end
end

local function draw_hero(h)
  local c = h.card
  gfx.rect(c.x, c.y, c.w, c.h, C.surface)
  outline(c.x, c.y, c.w, c.h, 2, C.divider)
  local a = h.art
  if row == "menu" then
    menu_art(MENU[menu_i].id, a.x, a.y, a.w, a.h)
  else
    local core = selected_core()
    if core then
      motif(core, a.x, a.y, a.w, a.h)
    else
      gfx.rect(a.x, a.y, a.w, a.h, C.raised)
      hills(a.x, a.y + a.h * 0.5, a.w, a.h * 0.5, C.divider, 0.2)
    end
  end
end

local function draw_shelf(h)
  local s = h.shelf
  local label = "On this kit"
  gfx.text(label, s.x, s.y, { size = 24, bold = true, color = C.text })
  local count = ""
  if cores and #cores > 0 then
    count = #cores == 1 and "1 installed" or (#cores .. " installed")
  end
  if count ~= "" then
    gfx.text(count, s.x, s.y + 4, { size = 18, color = C.secondary, max_w = s.w, align = "right" })
  end
  local _, lh = gfx.measure(label, 24, true)
  local top = s.y + lh + 14
  local ch = s.y + s.h - top
  if not cores or #cores == 0 then
    gfx.rect(s.x, top, s.w, ch, C.raised)
    gfx.text(status, s.x + 24, top + ch / 2 - 12, { size = 20, color = C.text, max_w = s.w - 48 })
    return
  end
  local n = visible_cards()
  local gap = 20
  local cw = (s.w - gap * (n - 1)) / n
  local art_h = math.floor(ch * 0.56)
  for slot = 0, n - 1 do
    local i = first + slot
    local core = cores[i]
    if not core then break end
    local x = s.x + slot * (cw + gap)
    local focused = row == "shelf" and shelf_i == i
    gfx.rect(x, top, cw, ch, C.surface)
    gfx.clip(x, top, cw, art_h)
    motif(core, x, top, cw, art_h)
    gfx.unclip()
    if focused then
      outline(x - 4, top - 4, cw + 8, ch + 8, 4, C.accent)
    else
      outline(x, top, cw, ch, 1, C.divider)
    end
    gfx.text(core.name or "", x + 14, top + art_h + 10, { size = 22, bold = focused, color = C.text, max_w = cw - 28 })
    local line = card_line(core)
    if line ~= "" then
      gfx.text(line, x + 14, top + art_h + 42, { size = 18, color = C.secondary, max_w = cw - 28 })
    end
    gfx.hit("core:" .. i, x, top, cw, ch)
  end
  if first > 1 then
    gfx.rect(s.x - 10, top + ch / 2 - 20, 4, 40, C.accent)
  end
  if first + n - 1 < #cores then
    gfx.rect(s.x + s.w + 6, top + ch / 2 - 20, 4, 40, C.accent)
  end
end

function draw()
  gfx.clear(C.background)
  local h = layout()
  if h then
    draw_header(h)
    draw_hero(h)
    draw_shelf(h)
  else
    gfx.text(room.title, 24, 20, { size = 28, bold = true, color = C.text })
    local core = selected_core()
    gfx.text(core and core.name or status, 24, 64, { size = 20, color = C.text, max_w = room.width - 48 })
  end
  publish()
end
