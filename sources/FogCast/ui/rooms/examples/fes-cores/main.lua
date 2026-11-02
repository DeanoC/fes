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

-- Muted motif grounds. A family without its own art picks one by id.
local GROUNDS = { "#7097A8", "#9690B5", "#E08A63", "#8DB07A", "#D9B45A", "#6E8F80" }

-- The v1 families, in Home's order, with their placeholder marks (#1351
-- replaces them with icons). Family ids are systems.lua's.
local V1 = {
  { id = "zx81", mark = "ZX81", short = "ZX81", ground = "#7097A8" },
  { id = "spectrum", mark = "ZX", short = "Spectrum", ground = "#9690B5" },
  { id = "fes.atari-st", mark = "ST", short = "Atari ST", ground = "#8DB07A" },
  { id = "fes.c64", mark = "64", short = "C64", ground = "#E08A63" },
}

-- Workbenches by core: the ZX81 family has its own; Coleco opens its
-- arcade room; everything else uses the shared system setup.
local WORKBENCH = {
  ["fes.zx81"] = "example.hardware",
  ["fes.zx80"] = "example.hardware",
  ["fes.ts1000"] = "example.hardware",
  ["fes.ts1500"] = "example.hardware",
  ["fes.coleco"] = "example.coleco-arcade",
}
local SYSTEM_SETUP = "example.system"
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

local function hash(s)
  local h = 7
  for i = 1, #(s or "") do
    h = (h * 31 + string.byte(s, i)) % 1000003
  end
  return h
end

-- Families, in Home's order.
local families = {}
do
  local placed = {}
  for _, art in ipairs(V1) do
    for _, family in ipairs(systems.families) do
      if family.id == art.id then
        families[#families + 1] = { id = family.id, label = family.label, variants = family.variants, art = art }
        placed[family.id] = true
      end
    end
  end
  for _, family in ipairs(systems.families) do
    if not placed[family.id] then
      local first = family.variants[1]
      families[#families + 1] = {
        id = family.id, label = family.label, variants = family.variants,
        art = { mark = first.mark, short = family.label, ground = GROUNDS[(hash(family.id) % #GROUNDS) + 1] },
      }
    end
  end
end

-- Data. A table per source; nil while it is still loading.
local kit_rows = nil   -- core_id -> kit.cores row
local machines = nil   -- core_id -> library machine entry
local titles = nil     -- family id -> number of titles
local capped = {}      -- family id -> true when its query hit the row limit
local library_err = nil
local recents = {}
local recents_loaded = false
local generation = 0   -- bumped on every data change, for the paint cache

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

-- State of one model: "ready", "firmware", "missing" (not on this kit) or
-- "blocked", with the reason for "blocked".
local function model_state(core_id)
  local lib = machines and machines[core_id]
  if lib then
    if lib.launchable then return "ready" end
    if lib.firmware then return "firmware" end
    return "blocked", lib.block
  end
  local kit_row = kit_rows and kit_rows[core_id]
  if kit_row then
    if kit_row.launchable then return "ready" end
    if kit_row.needs == "firmware" then return "firmware" end
    if kit_row.needs == "unavailable" then return "missing" end
    return "blocked", kit_row.block ~= "" and kit_row.block or nil
  end
  return "missing"
end

-- Families wait for the library, the kit's list (it says which models are
-- installed when the library has no row for them) and Recently played, so
-- the opening focus is chosen before any family shows its state.
local function settled()
  return recents_loaded and kit_rows ~= nil and (machines ~= nil or library_err ~= nil)
end

-- A family's state ("checking", "ready", "firmware", "missing", "blocked"),
-- with the reason for "blocked" and the model Open starts on.
local function family_state(f)
  if not settled() then return "checking", nil, f.variants[1].id end
  local firmware, blocked, reason, any = nil, nil, nil, nil
  for _, v in ipairs(f.variants) do
    local state, why = model_state(v.id)
    if state == "ready" then return "ready", nil, v.id end
    if state == "firmware" and not firmware then firmware = v.id end
    if state == "blocked" and not blocked then blocked, reason = v.id, why end
    if state ~= "missing" and not any then any = v.id end
  end
  if firmware then return "firmware", nil, firmware end
  if blocked then return "blocked", reason, blocked end
  return "missing", nil, any or f.variants[1].id
end

local LIBRARY_LIMIT = 2000

local function count_line(n, more)
  if more then return n .. "+ games" end
  if n == 1 then return "1 game" end
  return n .. " games"
end

-- "4 models: ZX81, ZX80, TS1000, TS1500", or the one model's name.
local function models_line(f)
  local names = {}
  for _, v in ipairs(f.variants) do names[#names + 1] = v.model or v.mark end
  if #names == 1 then return names[1] end
  return #names .. " models: " .. table.concat(names, ", ")
end

local STATE_WORDS = {
  ready = "Ready",
  firmware = "Needs firmware",
  missing = "Not on this kit",
  checking = "Checking…",
}

-- One line per state, naming its models ("Not on this kit: ZX80, TS1000"),
-- so Details fits the hero.
local function details_lines(f)
  local lines, order, models = {}, {}, {}
  for _, v in ipairs(f.variants) do
    local state, why = model_state(v.id)
    if not settled() then state, why = "checking", nil end
    local word = why or STATE_WORDS[state] or "Can't start here"
    if not models[word] then
      models[word] = {}
      order[#order + 1] = word
    end
    table.insert(models[word], v.model or v.mark)
  end
  for _, word in ipairs(order) do
    lines[#lines + 1] = word .. ": " .. table.concat(models[word], ", ")
  end
  if titles then
    lines[#lines + 1] = "In your library: " .. count_line(titles[f.id] or 0, capped[f.id])
  end
  return lines
end

-- The tile's shared state chip and its detail line.
local function tile_copy(f)
  local state, reason = family_state(f)
  if state == "checking" then return "Checking", "Checking…" end
  if state == "ready" then
    if not titles then return "Ready", "" end
    local n = titles[f.id] or 0
    if n == 0 then return "Ready", "No games yet" end
    return "Ready", count_line(n, capped[f.id])
  end
  if state == "firmware" then return "Unavailable", "Add your ROM files" end
  if state == "missing" then return "Unavailable", "Not on this kit" end
  return "Unavailable", reason or "Can't start here"
end

local function family_destination(f)
  local state, reason, core_id = family_state(f)
  local availability, headline, explain, needs = "unavailable", "", "", ""
  if state == "checking" then
    availability = "checking"
  elseif state == "ready" then
    availability = "ready"
    local n = titles and titles[f.id] or 0
    if not titles then
      headline = "Ready to open"
      explain = "Open it to choose a model and a game."
    elseif n == 0 then
      headline = "No games yet"
      explain = "Open it to choose a model."
    else
      headline = count_line(n, capped[f.id])
      explain = "Open it to choose a model and a game."
    end
  elseif state == "firmware" then
    headline = "Add your " .. f.label .. " ROM files to play"
    explain = "Open it to see which files it needs and where they go."
    needs = "firmware"
  elseif state == "missing" then
    headline = "Not installed on this kit"
    explain = "This kit's software doesn't include it."
  else
    headline = reason or "Can't start here"
    explain = "Open it for details."
  end
  local machine = machines and machines[core_id]
  destination.set{
    kind = "family",
    label = f.label,
    room_id = WORKBENCH[core_id] or SYSTEM_SETUP,
    core_id = core_id,
    game_id = machine and machine.game_id or "",
    availability = availability,
    status = headline,
    explain = explain,
    models = models_line(f),
    needs = needs,
    details = details_lines(f),
  }
end

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
    family_destination(f)
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
    if family_state(f) == "ready" then family_i = i return end
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

local function load_kit()
  if kit == nil or kit.cores == nil then
    kit_rows = {}
    return
  end
  kit.cores(function(rows, err)
    kit_rows = {}
    if not err then
      for _, r in ipairs(rows or {}) do
        if r.core_id and r.core_id ~= "" then kit_rows[r.core_id] = r end
      end
    end
    changed()
  end)
end

-- What a machine entry's launch block means on a family tile. A missing ROM
-- or system firmware is the firmware state; the rest get a short reason.
local BLOCKS = {
  source_offline = "Its library is offline",
  lease_held = "In use on another screen",
  version_skew = "Needs a software update",
}

-- A machine entry is a model's catalog row without media.
local function add_machine(out, g)
  if out[g.core_id] and out[g.core_id].launchable then return end
  local block = g.launch_block or ""
  out[g.core_id] = {
    game_id = g.id,
    launchable = block == "",
    firmware = block == "missing_firmware" or block == "missing_rom",
    block = BLOCKS[block],
  }
end

-- One query per family, so a large family's titles never push another
-- family's machine entries past the row limit. A title counts once; its
-- editions are siblings of the same title, so only machine entries are
-- looked for among them.
local library_generation = 0
local function load_library()
  library_generation = library_generation + 1
  local gen = library_generation
  local m, counts, more, waiting, failed = {}, {}, {}, #families, nil
  local function finish()
    waiting = waiting - 1
    if waiting > 0 or gen ~= library_generation then return end
    if failed then
      machines, titles, capped, library_err = nil, nil, {}, failed
    else
      machines, titles, capped, library_err = m, counts, more, nil
    end
    changed()
  end
  for _, f in ipairs(families) do
    local ids = {}
    for _, v in ipairs(f.variants) do ids[#ids + 1] = v.id end
    library.query({ platform = "fpga", core_ids = ids, limit = LIBRARY_LIMIT }, function(games, err)
      if err then
        failed = failed or err
        finish()
        return
      end
      local n = 0
      local function machine(g)
        if g.core_id and systems.family_by_core[g.core_id] and (g.media_id or "") == "" then add_machine(m, g) end
      end
      for _, g in ipairs(games or {}) do
        local family = systems.family_by_core[g.core_id or ""]
        if (g.media_id or "") ~= "" and family and family.id == f.id then n = n + 1 end
        machine(g)
        for _, v in ipairs(g.variants or {}) do machine(v) end
      end
      counts[f.id] = n
      more[f.id] = #(games or {}) >= LIBRARY_LIMIT or nil
      finish()
    end)
  end
  if #families == 0 then
    waiting = 1
    finish()
  end
end

local function load_recents()
  -- Filter before paging so hidden cores do not consume the three recent slots.
  library.query({ collection = "recents", core_ids = systems.ids, limit = RECENT_MAX }, function(games, err)
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
  load_kit()
  load_library()
  load_recents()
  publish()
end

-- Back from a workbench or a game: the library may have new titles, and
-- the game just played leads Recently played.
function on_resume()
  load_library()
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
end

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

-- A family's placeholder mark: its ground, its short mark and a keyboard.
-- The name is always text outside the mark.
local function family_mark(f, x, y, w, h)
  local art = f.art
  gfx.rect(x, y, w, h, art.ground)
  local size = math.max(14, math.min(math.floor(h * 0.30), math.floor(w / (#art.mark * 0.62 + 0.5))))
  local tw, th = gfx.measure(art.mark, size, true)
  local ty = y + h * 0.06
  gfx.text(art.mark, x + (w - tw) / 2, ty, { size = size, bold = true, color = C.on_accent })
  local ky = ty + th + h * 0.06
  local kh = math.min(h * 0.32, y + h - ky - h * 0.08)
  if kh < 8 then return end
  local kw = math.min(w * 0.74, kh * 3.2)
  keyboard(x + (w - kw) / 2, ky, kw, kh, C.surface, art.ground)
end

-- A recent title's art: its family's ground behind the moon-and-hills motif.
local function title_art(g, x, y, w, h)
  local family = systems.family_by_core[g.core_id or ""]
  local ground = GROUNDS[(hash(g.core_id or g.id) % #GROUNDS) + 1]
  for _, f in ipairs(families) do
    if family and f.id == family.id then ground = f.art.ground end
  end
  gfx.rect(x, y, w, h, ground)
  disc(x + w * 0.36, y + h * 0.42, math.min(w, h) * 0.24, "#F5E6C8")
  hills(x, y + h * 0.45, w, h * 0.55, C.dark, hash(g.title or "") % 7 / 7)
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

-- The shared state chip: Ready filled, Unavailable a raised plate with a
-- leading bar, Checking outlined.
local function chip(label, x, y, size)
  local tw, th = gfx.measure(label, size, true)
  local pad = math.floor(size * 0.55)
  local h = th + math.floor(size * 0.5)
  if label == "Ready" then
    gfx.rect(x, y, tw + pad * 2, h, C.accent)
    gfx.text(label, x + pad, y + (h - th) / 2, { size = size, bold = true, color = C.on_accent })
  elseif label == "Unavailable" then
    gfx.rect(x, y, tw + pad * 2 + 5, h, C.raised)
    gfx.rect(x, y, 5, h, C.dark)
    gfx.text(label, x + 5 + pad, y + (h - th) / 2, { size = size, bold = true, color = C.text })
  else
    outline(x, y, tw + pad * 2, h, 2, C.secondary)
    gfx.text(label, x + pad, y + (h - th) / 2, { size = size, bold = true, color = C.text })
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
    return
  end
  local which = row == "actions" and body or row
  if which == "banner" then
    menu_art("settings", a.x, a.y, a.w, a.h)
  elseif which == "recent" and recents[recent_i] then
    title_art(recents[recent_i], a.x, a.y, a.w, a.h)
  elseif families[family_i] then
    family_mark(families[family_i], a.x, a.y, a.w, a.h)
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
local function draw_families(s, top, ch, compact)
  local n = #families
  local shown = math.min(n, 4)
  local gap = 20
  local cw = (s.w - gap * (shown - 1)) / shown
  local first = math.max(1, math.min(family_i, n - shown + 1))
  if family_i > first + shown - 1 then first = family_i - shown + 1 end
  local detail = not compact and room.height >= 648
  for slot = 0, shown - 1 do
    local i = first + slot
    local f = families[i]
    if not f then break end
    local x = s.x + slot * (cw + gap)
    local focused = row == "family" and family_i == i
    local state, line = tile_copy(f)
    gfx.rect(x, top, cw, ch, C.surface)
    if compact then
      local pad = 8
      local aw = math.min(math.floor(cw * 0.38), math.floor((ch - pad * 2) * 1.2))
      family_mark(f, x + pad, top + pad, aw, ch - pad * 2)
      local tx = x + pad + aw + 12
      local tw = cw - (tx - x) - 8
      gfx.text(f.art.short, tx, top + ch * 0.20, { size = 20, bold = focused, color = C.text, max_w = tw })
      chip(state, tx, top + ch * 0.52, 16)
    else
      local art_h = math.floor(ch * (detail and 0.50 or 0.56))
      gfx.clip(x, top, cw, art_h)
      family_mark(f, x, top, cw, art_h)
      gfx.unclip()
      gfx.text(f.label, x + 14, top + art_h + 8, { size = 22, bold = focused, color = C.text, max_w = cw - 28 })
      local cy = top + art_h + 40
      chip(state, x + 14, cy, 16)
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
  if first > 1 then
    gfx.rect(s.x - 10, top + ch / 2 - 20, 4, 40, C.accent)
  end
  if first + shown - 1 < n then
    gfx.rect(s.x + s.w + 6, top + ch / 2 - 20, 4, 40, C.accent)
  end
end

local function on_kit_count()
  local n = 0
  for _, f in ipairs(families) do
    local state = family_state(f)
    if state ~= "missing" and state ~= "checking" then n = n + 1 end
  end
  return n
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

local function draw_shelf(h)
  local s = h.shelf
  local label_size = s.h >= 240 and 24 or 20
  local _, lh = gfx.measure("Computers", label_size, true)
  local y = s.y
  local above = top_row()
  if above then
    gfx.text(above == "banner" and "Get started" or "Recently played", s.x, y, { size = label_size, bold = true, color = C.text })
    local rh = math.floor((s.h - (lh + 8) * 2 - 12) * 0.40)
    if above == "banner" then
      draw_banner(s, y + lh + 8, rh)
    else
      draw_recents(s, y + lh + 8, rh)
    end
    y = y + lh + 8 + rh + 12
    gfx.text("Computers", s.x, y, { size = label_size, bold = true, color = C.text })
    local top = y + lh + 8
    draw_families(s, top, s.y + s.h - top, true)
    return
  end
  gfx.text("Computers", s.x, y, { size = label_size, bold = true, color = C.text })
  if settled() then
    gfx.text(on_kit_count() .. " on this kit", s.x, y + 4, { size = 18, color = C.secondary, max_w = s.w, align = "right" })
  end
  local top = y + lh + 14
  draw_families(s, top, s.y + s.h - top, false)
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

function draw()
  if room.presented and painted
    and painted.generation == generation and painted.row == row
    and painted.body == body and painted.recent_i == recent_i
    and painted.family_i == family_i and painted.menu_i == menu_i
    and painted.width == room.width and painted.height == room.height
    and painted.title == room.title and gfx.reuse() then
    return
  end
  local h = room.hero
  if h then
    draw_header(h)
    draw_hero(h)
    draw_shelf(h)
  else
    draw_compact()
  end
  publish()
  painted = {
    generation = generation, row = row, body = body, recent_i = recent_i,
    family_i = family_i, menu_i = menu_i, width = room.width,
    height = room.height, title = room.title,
  }
  -- Paint the background after assembling the full display list. The existing
  -- engine presents a completed paint on a soft overrun, allowing the next
  -- unchanged draw to reuse it. Hard deadlines and overrun limits still apply.
  gfx.paint({ { "c", C.background } })
end
