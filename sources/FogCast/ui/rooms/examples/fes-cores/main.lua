-- FES Home on a kit: a Recently played row above one tile per computer
-- family (#1347).
--
-- The families are systems.families: the four v1 families in a fixed order
-- (ZX81, Spectrum, Atari ST, C64), then, when the launcher shows all cores,
-- every other registered family. A family's state comes from the library's
-- machine entries (platform "fpga" rows without media) for its models, and
-- its game count from that library's titles with media. When the library
-- can't be read, kit.cores still says which models are installed and which
-- need firmware. Open goes to the family's workbench, where the model is
-- chosen; FES draws the selected family's or title's copy in the hero text
-- column (room.toml destination_panel = "hero").
--
-- Recently played is the library's recents collection, hidden until
-- something has been played. Play starts the title again; there is no resume.
-- After Skip setup, a Get started banner takes that slot until setup is
-- finished (room.setup.unfinished); Finish setup resumes setup (#1345).
--
-- Up moves from the family tiles to the row above. From the top row Up is
-- left to FES, which focuses the hero's actions and then sends "header" to
-- reach the header routes. In a release-only launcher the header has
-- Settings only; showing all cores adds Library and Systems.
--
-- Under 480×360, room.hero is nil and the launcher keeps its destination
-- strip. A compact list above that strip offers the families and the routes.

local systems = require "systems"
local F = require "families"
local C = F.colors
local families = F.list
local SYSTEMS = "example.computers"

local MENU = { { id = "settings", label = "Settings" } }
if not room.release_only then
  MENU = {
    { id = "library", label = "Library" },
    { id = "systems", label = "Systems" },
    { id = "settings", label = "Settings" },
  }
end

local RECENT_MAX = 3
local recents = {}
local recents_loaded = false
local recents_generation = 0
local generation = 0   -- bumped on every data change, for the paint cache
F.waiting = function() return not recents_loaded end

-- The Get started banner, while setup is unfinished.
local banner = room.setup.unfinished

-- The row above the family tiles: the banner or Recently played.
local function recents_shown()
  return room.hero ~= nil and not banner and #recents > 0
end

local function top_row()
  if room.hero == nil then return nil end
  if banner then return "banner" end
  if #recents > 0 then return "recent" end
  return nil
end

-- Focus. row is "recent", "family", "menu", or "actions" while FES has the
-- hero's actions; body is the row under the hero's actions.
local row, body = "family", "family"
local recent_i, family_i, menu_i = 1, 1, 1
local settled = F.settled

local function machine_label(g)
  local value = systems.by_id[g.core_id or ""]
  if value then return value.label end
  return string.upper(g.system or "")
end

local function publish()
  if row == "menu" then
    local item = MENU[menu_i]
    if item.id == "library" then
      destination.set{ kind = "library", label = "Library" }
    elseif item.id == "systems" then
      destination.set{ kind = "room", room_id = SYSTEMS, label = "Systems" }
    else
      destination.set{ kind = "action", action = "settings", label = "Settings" }
    end
    return
  end
  local which = row == "actions" and body or row
  if which == "banner" then
    destination.set{ kind = "action", action = "setup_resume", label = "Finish setup", status = "Setup isn't finished" }
    return
  end
  if which == "recent" and recents[recent_i] then
    local g = recents[recent_i]
    destination.set{ kind = "game", label = g.title, game_id = g.id, system = g.system, kicker = "Recently played" }
    return
  end
  local f = families[family_i]
  if f then
    F.publish(f)
  else
    destination.set{ kind = "unresolved", label = "Home", status = "No computers are registered." }
  end
end

-- Saved focus: "recent:<game id>" or "family:<family id>". A fresh visit
-- opens on the first recent title, else the first Ready family.
local function remember()
  local key = ""
  if row == "recent" and recents[recent_i] then
    key = "recent:" .. recents[recent_i].id
  elseif row == "family" and families[family_i] then
    key = "family:" .. families[family_i].id
  end
  if key ~= "" and store.get("focus", "") ~= key then store.set("focus", key) end
end

local function choose_opening()
  local want = store.get("focus", "")
  if type(want) ~= "string" then want = "" end
  local kind, id = string.match(want, "^(%a+):(.+)$")
  if kind == "recent" and recents_shown() then
    for i, g in ipairs(recents) do
      if g.id == id then row, body, recent_i = "recent", "recent", i return end
    end
  elseif kind == "family" then
    for i, f in ipairs(families) do
      if f.id == id then row, body, family_i = "family", "family", i return end
    end
  end
  if recents_shown() then
    row, body, recent_i = "recent", "recent", 1
    return
  end
  row, body, family_i = "family", "family", 1
  for i, f in ipairs(families) do
    if F.state(f) == "ready" then family_i = i return end
  end
end

-- The opening is chosen once, when every source has answered, so a late
-- answer never moves focus the player has already moved.
local opened, moved = false, false
local function changed()
  generation = generation + 1
  if not opened and not moved and settled() then
    opened = true
    choose_opening()
  end
  publish()
end

local function load_recents()
  recents_generation = recents_generation + 1
  local gen = recents_generation
  -- Filter before paging so hidden cores do not consume the three recent slots.
  library.query({ collection = "recents", core_ids = systems.ids, limit = RECENT_MAX }, function(games, err)
    if gen ~= recents_generation then return end
    local focused = recents[recent_i] and recents[recent_i].id
    recents = {}
    if not err then
      for _, g in ipairs(games or {}) do
        if #recents < RECENT_MAX and g.id and (not g.core_id or room.core_shown(g.core_id)) then
          recents[#recents + 1] = g
        end
      end
    end
    recents_loaded = true
    -- The title just played moves to the front: keep focus on the same one.
    local found = nil
    for i, g in ipairs(recents) do
      if g.id == focused then found = i end
    end
    recent_i = found or math.max(1, math.min(recent_i, #recents))
    if #recents == 0 then
      if row == "recent" then row = "family" end
      if body == "recent" then body = "family" end
    end
    changed()
  end)
end

function load()
  F.load(changed, true)
  load_recents()
  publish()
end

function update()
  F.update()
end

-- Back from a workbench or a game: the library may have new titles, and
-- the game just played leads Recently played.
function on_resume()
  F.reload()
  load_recents()
end

-- The compact list is a column under the header routes: Up and Down move
-- through it, and Up from its top reaches the header (FES sends no
-- "header" without the hero).
local function move_compact(dx, dy)
  if row == "menu" then
    if dy > 0 then
      row, body = "family", "family"
    elseif dx ~= 0 then
      menu_i = math.max(1, math.min(#MENU, menu_i + dx))
    end
    return true
  end
  row, body = "family", "family"
  if dy < 0 then
    if family_i > 1 then family_i = family_i - 1 else row, menu_i = "menu", #MENU end
  elseif dy > 0 then
    family_i = math.min(#families, family_i + 1)
  end
  remember()
  return true
end

local function move(dx, dy)
  moved = true
  if not room.hero then return move_compact(dx, dy) end
  if row == "menu" then
    if dy > 0 then
      row = top_row() or "family"
      body = row
    elseif dx ~= 0 then
      menu_i = math.max(1, math.min(#MENU, menu_i + dx))
    end
    return true
  end
  if dy < 0 then
    if row == "family" and top_row() then
      row, body = top_row(), top_row()
      remember()
      return true
    end
    -- The top row: FES takes Up to the hero's actions, then the header.
    return false
  end
  if dy > 0 then
    if row == "recent" or row == "banner" then
      row, body = "family", "family"
      remember()
    end
    return true
  end
  if dx ~= 0 then
    if row == "banner" then return true end
    if row == "recent" then
      recent_i = math.max(1, math.min(#recents, recent_i + dx))
    else
      family_i = math.max(1, math.min(#families, family_i + dx))
    end
    remember()
    return true
  end
  return false
end

function on_input(cmd)
  local handled = false
  if cmd == "header" then
    moved = true
    row = "menu"
    handled = true
  elseif cmd == "left" then handled = move(-1, 0)
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
  elseif kind == "recent" and recents[n] then
    row, body, recent_i = "recent", "recent", n
    remember()
  elseif kind == "banner" and top_row() == "banner" then
    row, body = "banner", "banner"
  elseif kind == "family" and families[n] then
    row, body, family_i = "family", "family", n
    remember()
  else
    return false
  end
  moved = true
  publish()
  return true
end

function on_hover(id)
  -- Native hero action focus keeps the selected item without drawing a
  -- second focus outline. These private hits never activate a destination.
  if id == "native:actions" then
    if row ~= "actions" and row ~= "menu" then body = row end
    row = "actions"
    return
  end
  if id == "native:shelf" then row = body return end
  focus_hit(id)
end
function on_activate(id) focus_hit(id) end

-- Shrinking to the compact list hides the row above the tiles.
function on_resize()
  if not top_row() then
    if row == "recent" or row == "banner" then row = "family" end
    if body == "recent" or body == "banner" then body = "family" end
  end
  publish()
end

-- Drawing ------------------------------------------------------------

local outline = F.outline

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

-- A recent title's art: its family's ground behind the moon-and-hills motif.
local function title_art(g, x, y, w, h)
  local ground = F.ground(g.core_id, g.id)
  gfx.rect(x, y, w, h, ground)
  disc(x + w * 0.36, y + h * 0.42, math.min(w, h) * 0.24, "#F5E6C8")
  hills(x, y + h * 0.45, w, h * 0.55, C.dark, F.hash(g.title or "") % 7 / 7)
end

local function menu_art(id, x, y, w, h)
  gfx.rect(x, y, w, h, C.raised)
  if id == "library" then
    local grounds = F.grounds
    local spines = { grounds[1], grounds[3], grounds[4], grounds[2], grounds[5], grounds[6] }
    local sw = w / 12
    for i, color in ipairs(spines) do
      local sh = h * (0.45 + 0.07 * (i % 3))
      gfx.rect(x + w * 0.18 + (i - 1) * (sw + 6), y + h * 0.82 - sh, sw, sh, color)
    end
    gfx.rect(x + w * 0.12, y + h * 0.82, w * 0.76, 8, C.dark)
  elseif id == "systems" then
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
  F.header(m, hd.h, room.title)

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
    return
  end
  local which = row == "actions" and body or row
  if which == "banner" then
    menu_art("settings", a.x, a.y, a.w, a.h)
  elseif which == "recent" and recents[recent_i] then
    title_art(recents[recent_i], a.x, a.y, a.w, a.h)
  elseif families[family_i] then
    F.mark(families[family_i], a.x, a.y, a.w, a.h)
  else
    gfx.rect(a.x, a.y, a.w, a.h, C.raised)
  end
end

local function draw_recents(s, top, ch)
  local gap = 16
  local cw = (s.w - gap * (RECENT_MAX - 1)) / RECENT_MAX
  for i, g in ipairs(recents) do
    local x = s.x + (i - 1) * (cw + gap)
    local focused = row == "recent" and recent_i == i
    gfx.rect(x, top, cw, ch, C.surface)
    local pad = math.max(4, math.floor(ch * 0.1))
    local aw = math.floor((ch - pad * 2) * 1.2)
    title_art(g, x + pad, top + pad, aw, ch - pad * 2)
    if focused then
      outline(x - 3, top - 3, cw + 6, ch + 6, 3, C.accent)
    else
      outline(x, top, cw, ch, 1, C.divider)
    end
    local tx = x + pad + aw + 12
    local tw = cw - (tx - x) - 8
    local size = ch >= 56 and 20 or 18
    gfx.text(g.title or "", tx, top + pad, { size = size, bold = focused, color = C.text, max_w = tw })
    gfx.text(machine_label(g), tx, top + ch - pad - size, { size = size - 4, color = C.secondary, max_w = tw })
    gfx.hit("recent:" .. i, x, top, cw, ch)
  end
end

-- Tall tiles: the mark above the name, the state chip and its detail line.
-- Compact tiles (below Recently played): a small mark, the short name and
-- the chip. Below the 720p safe-room height (648), the detail line is left out.
local function draw_families(s, top, ch, compact, only_slot)
  local n = #families
  local shown = math.min(n, 4)
  local gap = 20
  local cw = (s.w - gap * (shown - 1)) / shown
  local first = math.max(1, math.min(family_i, n - shown + 1))
  if family_i > first + shown - 1 then first = family_i - shown + 1 end
  local detail = not compact and room.height >= 648
  for slot = 0, shown - 1 do
    if only_slot == nil or only_slot == slot then
      local i = first + slot
      local f = families[i]
      if not f then break end
      local x = s.x + slot * (cw + gap)
      local focused = row == "family" and family_i == i
      local state, line = F.tile(f)
      gfx.rect(x, top, cw, ch, C.surface)
      if compact then
        local pad = 8
        local aw = math.min(math.floor(cw * 0.38), math.floor((ch - pad * 2) * 1.2))
        F.mark(f, x + pad, top + pad, aw, ch - pad * 2)
        local tx = x + pad + aw + 12
        local tw = cw - (tx - x) - 8
        gfx.text(f.art.short, tx, top + ch * 0.20, { size = 20, bold = focused, color = C.text, max_w = tw })
        F.chip(state, tx, top + ch * 0.52, 16)
      else
        local art_h = math.floor(ch * (detail and 0.50 or 0.56))
        gfx.clip(x, top, cw, art_h)
        F.mark(f, x, top, cw, art_h)
        gfx.unclip()
        gfx.text(f.label, x + 14, top + art_h + 8, { size = 22, bold = focused, color = C.text, max_w = cw - 28 })
        local cy = top + art_h + 40
        F.chip(state, x + 14, cy, 16)
        if detail and line ~= "" then
          gfx.text(line, x + 14, cy + 32, { size = 18, color = C.secondary, max_w = cw - 28 })
        end
      end
      if focused then
        outline(x - 4, top - 4, cw + 8, ch + 8, 4, C.accent)
      else
        outline(x, top, cw, ch, 1, C.divider)
      end
      gfx.hit("family:" .. i, x, top, cw, ch)
    end
  end
  if (only_slot == nil or only_slot == 0) and first > 1 then
    gfx.rect(s.x - 10, top + ch / 2 - 20, 4, 40, C.accent)
  end
  if (only_slot == nil or only_slot == 0) and first + shown - 1 < n then
    gfx.rect(s.x + s.w + 6, top + ch / 2 - 20, 4, 40, C.accent)
  end
end

-- The Get started banner: a warm plate with a leading bar, what is left,
-- and Finish setup.
local function draw_banner(s, top, ch)
  local focused = row == "banner"
  gfx.rect(s.x, top, s.w, ch, C.raised)
  gfx.rect(s.x, top, 6, ch, "#E8C77A")
  local size = ch >= 56 and 20 or 18
  gfx.text("Setup isn't finished", s.x + 24, top + ch * 0.12, { size = size, bold = true, color = C.text, max_w = s.w * 0.6 })
  gfx.text("Your choices so far are kept.", s.x + 24, top + ch * 0.12 + size + 6, { size = size - 2, color = C.secondary, max_w = s.w * 0.6 })
  local label = "Finish setup"
  local bw = gfx.measure(label, size, true) + 28
  local bh = math.min(ch - 16, size + 18)
  local bx, by = s.x + s.w - bw - 16, top + (ch - bh) / 2
  gfx.rect(bx, by, bw, bh, C.surface)
  local _, th = gfx.measure(label, size, true)
  gfx.text(label, bx + 14, by + (bh - th) / 2, { size = size, bold = true, color = C.text })
  if focused then
    outline(s.x - 3, top - 3, s.w + 6, ch + 6, 3, C.accent)
  else
    outline(bx, by, bw, bh, 2, C.divider)
  end
  gfx.hit("banner:1", s.x, top, s.w, ch)
end

local function draw_shelf(h, only_slot)
  local s = h.shelf
  local label_size = s.h >= 240 and 24 or 20
  local _, lh = gfx.measure("Computers", label_size, true)
  local y = s.y
  local above = top_row()
  if above then
    local rh = math.floor((s.h - (lh + 8) * 2 - 12) * 0.40)
    if only_slot == nil or only_slot == -1 then
      gfx.text(above == "banner" and "Get started" or "Recently played", s.x, y, { size = label_size, bold = true, color = C.text })
      if above == "banner" then
        draw_banner(s, y + lh + 8, rh)
      else
        draw_recents(s, y + lh + 8, rh)
      end
    end
    y = y + lh + 8 + rh + 12
    if only_slot == nil or only_slot == -1 then
      gfx.text("Computers", s.x, y, { size = label_size, bold = true, color = C.text })
    end
    local top = y + lh + 8
    if only_slot ~= -1 then draw_families(s, top, s.y + s.h - top, true, only_slot) end
    return
  end
  if only_slot == nil or only_slot == -1 then
    gfx.text("Computers", s.x, y, { size = label_size, bold = true, color = C.text })
    if settled() and F.count_known() then
      gfx.text(F.on_kit() .. " on this kit", s.x, y + 4, { size = 18, color = C.secondary, max_w = s.w, align = "right" })
    end
  end
  local top = y + lh + 14
  if only_slot ~= -1 then draw_families(s, top, s.y + s.h - top, false, only_slot) end
end

-- roomDestGeom in ui/tenfoot/overlay_layout.go draws the compact strip
-- over this band and its hit wins, so navigation hits stay above it.
local COMPACT_MARGIN = 12
local COMPACT_MENU_H = 36
local COMPACT_ROW = 32
local COMPACT_GAP = 6

local function compact_limit()
  if room.width < 152 then return room.height end
  local y = room.height - 118 - 8
  if y < 48 then y = 48 end
  if room.height - 8 - y < 72 then return room.height end
  return y
end

local function draw_compact()
  local m = COMPACT_MARGIN
  local limit = compact_limit()
  local right = room.width - m
  for i = #MENU, 1, -1 do
    local item = MENU[i]
    local focused = row == "menu" and menu_i == i
    local bw = gfx.measure(item.label, 18, focused) + 28
    right = right - bw
    gfx.rect(right, 8, bw, COMPACT_MENU_H, C.surface)
    if focused then
      outline(right - 3, 5, bw + 6, COMPACT_MENU_H + 6, 3, C.accent)
    else
      outline(right, 8, bw, COMPACT_MENU_H, 1, C.divider)
    end
    local _, th = gfx.measure(item.label, 18, focused)
    gfx.text(item.label, right + 14, 8 + (COMPACT_MENU_H - th) / 2, { size = 18, bold = focused, color = C.text })
    gfx.hit("menu:" .. i, right, 8, bw, COMPACT_MENU_H)
    right = right - 8
  end
  local y = 8 + COMPACT_MENU_H + 8
  local cw = room.width - m * 2
  local step = COMPACT_ROW + COMPACT_GAP
  local rows = math.max(0, math.floor((limit - 4 - y) / step))
  local first = math.max(1, family_i - rows + 1)
  for slot = 0, rows - 1 do
    local i = first + slot
    local f = families[i]
    if not f then break end
    local ry = y + slot * step
    local focused = row == "family" and family_i == i
    gfx.rect(m, ry, cw, COMPACT_ROW, C.surface)
    if focused then
      outline(m - 3, ry - 2, cw + 6, COMPACT_ROW + 4, 3, C.accent)
    else
      outline(m, ry, cw, COMPACT_ROW, 1, C.divider)
    end
    gfx.text(f.label, m + 12, ry + 6, { size = 18, bold = focused, color = C.text, max_w = math.max(1, cw - 24) })
    gfx.hit("family:" .. i, m, ry, cw, COMPACT_ROW)
  end
end

-- Repaint only when the data, focus or geometry changes.
local painted = nil
local opening, opening_scene = 0, nil

local function scene()
  return { generation = generation, row = row, body = body, recent_i = recent_i,
    family_i = family_i, menu_i = menu_i, width = room.width,
    height = room.height, title = room.title }
end

local function same_scene(a, b)
  return a and b and a.generation == b.generation and a.row == b.row
    and a.body == b.body and a.recent_i == b.recent_i
    and a.family_i == b.family_i and a.menu_i == b.menu_i
    and a.width == b.width and a.height == b.height and a.title == b.title
end

function draw()
  local now = scene()
  if room.presented and same_scene(painted, now) and gfx.reuse() then
    return
  end
  local h = room.hero
  -- Build an initial or data-changed picture across callbacks. Each extension copies the
  -- presented prefix, so no callback needs to draw all four family marks.
  -- Restart when an async answer, input or resize changes that prefix.
  if h and (not painted or painted.generation ~= generation) then
    if not room.presented or not same_scene(opening_scene, now) then opening = 0 end
    if opening == 0 then
      painted = nil
      draw_header(h)
      draw_hero(h)
      gfx.paint({ { "c", C.background } })
      opening_scene = now
    else
      if opening == 1 then draw_shelf(h, -1)
      else
        draw_shelf(h, (opening - 2) * 2)
        draw_shelf(h, (opening - 2) * 2 + 1)
      end
      gfx.extend({})
    end
    opening = opening + 1
    if opening == 4 then painted = now end
    return
  end
  if h then
    draw_header(h)
    draw_hero(h)
    draw_shelf(h)
  else
    draw_compact()
  end
  publish()
  painted = now
  -- Paint the background after assembling the full display list. The existing
  -- engine presents a completed paint on a soft overrun, allowing the next
  -- unchanged draw to reuse it. Hard deadlines and overrun limits still apply.
  gfx.paint({ { "c", C.background } })
end
