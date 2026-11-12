-- Computer families, readiness and shared presentation for Home and setup.
--
-- The families are systems.families: the four v1 families in a fixed order
-- (ZX81, Spectrum, Atari ST, C64), then, when the launcher shows all cores,
-- every other registered family. A family's state comes from the host's
-- admitted hardware.computers projection for its models, and its game
-- count from the library's titles with media. A selected cassette does not
-- remove a machine from Home. kit.cores supplies the independent local shelf.
--
-- families.load(on_change) starts the kit, hardware and library reads;
-- on_change runs after each answer. families.reload() refreshes all three.
-- A room that waits for more than this module sets families.waiting to
-- return true until its own data has answered; every family is Checking
-- until then.

local systems = require "systems"

local M = {}

M.colors = {
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
local C = M.colors

-- Muted motif grounds. A family without its own art picks one by id.
local GROUNDS = { "#7097A8", "#9690B5", "#E08A63", "#8DB07A", "#D9B45A", "#6E8F80" }
M.grounds = GROUNDS

-- The v1 families, in order, with their placeholder marks (#1351 replaces
-- them with icons). Family ids are systems.lua's.
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

function M.hash(s)
  local h = 7
  for i = 1, #(s or "") do
    h = (h * 31 + string.byte(s, i)) % 1000003
  end
  return h
end

-- Families, in order.
local list = {}
do
  local placed = {}
  for _, art in ipairs(V1) do
    for _, family in ipairs(systems.families) do
      if family.id == art.id then
        list[#list + 1] = { id = family.id, label = family.label, variants = family.variants, art = art }
        placed[family.id] = true
      end
    end
  end
  for _, family in ipairs(systems.families) do
    if not placed[family.id] then
      local first = family.variants[1]
      list[#list + 1] = {
        id = family.id, label = family.label, variants = family.variants,
        art = { mark = first.mark, short = family.label, ground = GROUNDS[(M.hash(family.id) % #GROUNDS) + 1] },
      }
    end
  end
end
M.list = list

-- Data. A table per source; nil while it is still loading.
local kit_rows = nil   -- core_id -> kit.cores row
local kit_error = false
local machines = nil   -- core_id -> admitted host computer
local hardware_loaded = false
local titles = nil     -- family id -> number of titles
local capped = {}      -- family id -> true when its query hit the row limit
local known = {}       -- family id -> true when its library read succeeded
local overfull = {}    -- family id -> machine entries exceeded the query budget
local library_err = nil
local on_change = function() end

M.waiting = function() return false end

-- State of one model: "ready", "firmware", "missing" (not on this kit) or
-- "blocked", with the reason for "blocked".
local function model_state(core_id)
  local lib = machines and machines[core_id]
  if lib then
    if lib.ready then return "ready" end
    return "blocked", lib.unavailable_reason ~= "" and lib.unavailable_reason or nil
  end
  local kit_row = kit_rows and kit_rows[core_id]
  if kit_row then
    if kit_row.launchable then return "ready" end
    if kit_row.needs == "firmware" then return "firmware" end
    if kit_row.needs == "unavailable" then return "missing" end
    return "blocked", kit_row.block ~= "" and kit_row.block or nil
  end
  if kit_error then return "blocked", "Can't check this kit" end
  return "missing"
end

function M.count_known() return not kit_error and next(overfull) == nil end

-- Host machine readiness, title counts and the independent kit shelf all
-- settle before the opening focus is chosen.
function M.settled()
  return not M.waiting() and kit_rows ~= nil and hardware_loaded and (titles ~= nil or library_err ~= nil)
end

-- A family's state ("checking", "ready", "firmware", "missing", "blocked"),
-- with the reason for "blocked" and the model Open starts on.
function M.state(f)
  if not M.settled() then return "checking", nil, f.variants[1].id end
  if overfull[f.id] then return "blocked", "Too many systems to check", f.variants[1].id end
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
  if n == 0 and more then return "1+ games" end
  if more then return n .. "+ games" end
  if n == 1 then return "1 game" end
  return n .. " games"
end

-- The family's game count line, or nil while the library is unknown.
function M.games(f)
  if not titles or not known[f.id] then return nil end
  local n = titles[f.id] or 0
  if n == 0 and not capped[f.id] then return "No games yet" end
  return count_line(n, capped[f.id])
end

-- "4 models: ZX81, ZX80, TS1000, TS1500", or the one model's name.
function M.models_line(f)
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
function M.details(f)
  local lines, order, models = {}, {}, {}
  for _, v in ipairs(f.variants) do
    local state, why = model_state(v.id)
    if not M.settled() then state, why = "checking", nil end
    if overfull[f.id] then state, why = "blocked", "Too many systems to check" end
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
  if titles and known[f.id] then
    lines[#lines + 1] = "In your library: " .. count_line(titles[f.id] or 0, capped[f.id])
  end
  return lines
end

-- The tile's shared state chip and its detail line.
function M.tile(f)
  local state, reason = M.state(f)
  if state == "checking" then return "Checking", "Checking…" end
  if state == "ready" then return "Ready", M.games(f) or "" end
  if state == "firmware" then return "Unavailable", "Add your ROM files" end
  if state == "missing" then return "Unavailable", "Not on this kit" end
  return "Unavailable", reason or "Can't start here"
end

-- Publishes the family as Home's hero destination.
function M.publish(f)
  local state, reason, core_id = M.state(f)
  local availability, headline, explain, needs = "unavailable", "", "", ""
  if state == "checking" then
    availability = "checking"
  elseif state == "ready" then
    availability = "ready"
    headline = M.games(f) or "Ready to open"
    explain = headline == "No games yet" and "Open it to choose a model." or "Open it to choose a model and a game."
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
    models = M.models_line(f),
    needs = needs,
    details = M.details(f),
  }
end

-- How many families are on this kit, once settled.
function M.on_kit()
  local n = 0
  for _, f in ipairs(list) do
    local state = M.state(f)
    if state ~= "missing" and state ~= "checking" then n = n + 1 end
  end
  return n
end

-- The family's motif ground for a core, for title art.
function M.ground(core_id, fallback)
  local family = systems.family_by_core[core_id or ""]
  for _, f in ipairs(list) do
    if family and f.id == family.id then return f.art.ground end
  end
  local key = (core_id and core_id ~= "") and core_id or fallback or ""
  return GROUNDS[(M.hash(key) % #GROUNDS) + 1]
end

local kit_generation = 0
local function load_kit()
  kit_generation = kit_generation + 1
  local gen = kit_generation
  kit_rows = nil
  kit_error = false
  if kit == nil or kit.cores == nil then
    kit_rows = {}
    return
  end
  kit.cores(function(rows, err)
    if gen ~= kit_generation then return end
    kit_error = err ~= nil
    kit_rows = {}
    if not err then
      for _, r in ipairs(rows or {}) do
        if r.core_id and r.core_id ~= "" then kit_rows[r.core_id] = r end
      end
    end
    on_change()
  end)
end

-- The host already verifies each computer's package, ABI, firmware, media
-- and composition. Library titles never stand in for this admission result.
local hardware_generation = 0
local hardware_reading = false
local hardware_refresh_pending = false
local function read_hardware(gen)
  hardware_reading = true
  hardware.read(function(value, err)
    hardware_reading = false
    -- A resume can supersede an outstanding read. Wait for its callback
    -- before requesting the newest snapshot: the bridge admits one read.
    if hardware_refresh_pending then
      hardware_refresh_pending = false
      read_hardware(hardware_generation)
      return
    end
    if gen ~= hardware_generation then return end
    machines, hardware_loaded = {}, true
    if not err then
      for _, computer in ipairs(value.computers or {}) do
        if computer.core_id and computer.core_id ~= "" then
          machines[computer.core_id] = computer
        end
      end
    end
    on_change()
  end)
end

local function load_hardware()
  hardware_loaded = false
  hardware_generation = hardware_generation + 1
  if hardware_reading then
    hardware_refresh_pending = true
    return
  end
  read_hardware(hardware_generation)
end

-- One query per family keeps a large family's titles from consuming another
-- family's row limit. A title counts once; its editions never add a count.
local library_generation = 0
local retry_enabled = false
local requests = {}
local RETRY_DELAYS = { 1, 2, 4 }
local next_retry = nil

local function schedule_next_retry()
  next_retry = nil
  for _, req in ipairs(requests) do
    if req.due and (not next_retry or req.due < next_retry) then next_retry = req.due end
  end
end

-- Retain successful family reads while an unrelated family is unavailable.
-- A failed title count stays unknown; hardware admission is independent.
local function publish_library(gen)
  if gen ~= library_generation then return end
  local counts, more, answered, oversized, failed = {}, {}, {}, {}, nil
  for _, req in ipairs(requests) do
    if not req.answered then return end
    if req.err then
      failed = failed or req.err
      if req.overfull then oversized[req.family.id] = true end
    else
      answered[req.family.id] = true
      counts[req.family.id] = req.count
      more[req.family.id] = req.more
    end
  end
  overfull = oversized
  if failed and not retry_enabled then
    titles, capped, known, library_err = nil, {}, {}, failed
  else
    titles, capped, known, library_err = counts, more, answered, failed
  end
  on_change()
end

local function query_family(req, gen)
  req.due = nil
  req.cancel = library.query({ platform = "fpga", core_ids = req.ids, limit = LIBRARY_LIMIT, home_family = true }, function(games, err, transient, more)
    if gen ~= library_generation then return end
    req.cancel = nil
    req.answered, req.err = true, err
    req.overfull = err and more or nil
    if err and transient and retry_enabled and req.retries < #RETRY_DELAYS then
      req.retries = req.retries + 1
      req.due = room.time + RETRY_DELAYS[req.retries]
    elseif not err then
      local n = 0
      for _, g in ipairs(games or {}) do
        local family = systems.family_by_core[g.core_id or ""]
        if g.core_entry_kind == "title" and family and family.id == req.family.id then
          n = n + 1
        end
      end
      req.count = n
      req.more = more or nil
    end
    schedule_next_retry()
    publish_library(gen)
  end)
end

function M.reload()
  for _, req in ipairs(requests) do
    if req.cancel then req.cancel() end
  end
  machines, titles, capped, known, overfull, library_err = nil, nil, {}, {}, {}, nil
  load_kit()
  load_hardware()
  on_change()
  library_generation = library_generation + 1
  local gen = library_generation
  requests = {}
  next_retry = nil
  for _, f in ipairs(list) do
    local ids = {}
    for _, v in ipairs(f.variants) do ids[#ids + 1] = v.id end
    local req = { family = f, ids = ids, retries = 0 }
    requests[#requests + 1] = req
    query_family(req, gen)
  end
  if #list == 0 then publish_library(gen) end
end

-- Called only by Home while it is active; no timer or retry worker survives
-- a closed or suspended room. A reload discards the previous schedule.
function M.update()
  if not next_retry or room.time < next_retry then return end
  for _, req in ipairs(requests) do
    if req.due and room.time >= req.due then
      query_family(req, library_generation)
      break
    end
  end
  schedule_next_retry()
end

function M.load(callback, retry)
  on_change = callback or function() end
  retry_enabled = retry == true
  M.reload()
end

-- Drawing ------------------------------------------------------------

function M.outline(x, y, w, h, t, color)
  gfx.rect(x, y, w, t, color)
  gfx.rect(x, y + h - t, w, t, color)
  gfx.rect(x, y, t, h, color)
  gfx.rect(x + w - t, y, t, h, color)
end

function M.keyboard(x, y, w, h, key, ground)
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
function M.mark(f, x, y, w, h)
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
  M.keyboard(x + (w - kw) / 2, ky, kw, kh, C.surface, art.ground)
end

-- The shared state chip: Ready filled, Unavailable a raised plate with a
-- leading bar, Checking outlined.
function M.chip(label, x, y, size)
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
    M.outline(x, y, tw + pad * 2, h, 2, C.secondary)
    gfx.text(label, x + pad, y + (h - th) / 2, { size = size, bold = true, color = C.text })
  end
end

-- The FES wordmark and a screen name, as Home's header draws them, from the
-- left edge m in a header hd.h high. Returns the wordmark's right edge.
function M.header(m, hh, title)
  gfx.rect(0, hh - 2, room.width, 2, C.divider)
  local bar = math.floor(hh * 0.11)
  local my = math.floor((hh - bar * 4) / 2)
  gfx.rect(m, my, bar * 3, bar, C.accent)
  gfx.rect(m, my + bar * 1.5, bar * 3, bar, C.accent)
  gfx.rect(m, my + bar * 3, bar * 1.6, bar, C.accent)
  local fw, th = gfx.measure("FES", 28, true)
  gfx.text("FES", m + bar * 3 + 12, (hh - th) / 2, { size = 28, bold = true, color = C.text })
  local fx = m + bar * 3 + 12 + fw + 18
  gfx.rect(fx, hh * 0.3, 2, hh * 0.4, C.divider)
  local _, rh = gfx.measure(title, 22)
  gfx.text(title, fx + 18, (hh - rh) / 2, { size = 22, color = C.text })
end

return M
