-- Computers: prepared ZX81 and Commodore 64 library entries.
-- Confirm stays here and calls session.launch. This room does not publish
-- a destination, add a socket, or name hardware the library did not return.
local List = require "widgets.list"
local color = require "util.color"

local DEST = 130
-- List colours match the other console rooms: a dark plate, light unfocused
-- ink, and dark ink on the accent bar. Row text sets its own colour because
-- the sofa label is meant for the dark shell.
local PLATE = "#1C2420"
local INK = "#F0F4F1"
local FOCUS_INK = "#141916"
local STATUS = "#C5D4CB"
local MACHINES = {
  ["fes.zx81"] = "ZX81",
  ["fes.c64"] = "Commodore 64",
}

local list
local rows
local status = "Looking for prepared computers…"
local load_err

local function machine_of(g)
  if type(g) ~= "table" then return nil end
  return MACHINES[g.core_id or ""]
end

local function short_id(id)
  id = id or ""
  if #id > 8 then return id:sub(1, 8) end
  return id
end

local function base_label(g)
  local label = machine_of(g) .. "  ·  " .. (g.title or g.id)
  if g.region and g.region ~= "" then
    label = label .. " (" .. g.region .. ")"
  end
  return label
end

local function add_rows(out, g)
  if not machine_of(g) or not g.launchable or not g.id or g.id == "" then return end
  local label = base_label(g)
  out[#out + 1] = { id = g.id, label = label }
  local seen = {}
  if g.expansion_id and g.expansion_id ~= "" then
    seen[g.expansion_id] = true
    out[#out + 1] = { id = g.id, label = label .. "  ·  Expansion " .. short_id(g.expansion_id) }
  end
  for _, slot in ipairs(g.slot_expansions or {}) do
    local eid = slot.expansion_id or ""
    if eid ~= "" and not seen[eid] then
      seen[eid] = true
      local n = slot.slot or 0
      out[#out + 1] = { id = g.id, label = label .. "  ·  Slot " .. tostring(n) .. " " .. short_id(eid) }
    end
  end
end

local function build(games)
  local out = {}
  for _, g in ipairs(games) do
    add_rows(out, g)
    for _, v in ipairs(g.variants or {}) do
      if v.id ~= g.id then
        add_rows(out, v)
      end
    end
  end
  return out
end

local function show(games)
  rows = build(games)
  status = ""
  local h = math.max(52, room.height - 140 - DEST)
  if list then
    list:set_items(rows)
    return
  end
  list = List.new{
    id = "computers",
    x = 64, y = 140,
    w = math.min(920, room.width - 128),
    h = h, row_h = 56, size = 22,
    items = rows,
  }
end

local function prepared_from(games)
  local out = {}
  for _, g in ipairs(games or {}) do
    if machine_of(g) and g.launchable and g.id and g.id ~= "" then
      out[#out + 1] = g
    end
  end
  return out
end

local function resolve(games)
  local prepared = prepared_from(games)
  local pending = 0
  local replacements = {}
  for _, g in ipairs(prepared) do
    local count = g.variant_count or 0
    local have = #(g.variants or {})
    if count > 1 and have <= 1 then
      pending = pending + 1
      local id = g.id
      local fallback = g
      library.game(id, function(detail, err)
        if detail and not err and detail.id and detail.id ~= "" then
          replacements[id] = detail
        else
          replacements[id] = fallback
        end
        pending = pending - 1
        if pending == 0 then
          local merged = {}
          for _, item in ipairs(prepared) do
            merged[#merged + 1] = replacements[item.id] or item
          end
          show(merged)
        end
      end)
    end
  end
  if pending == 0 then
    show(prepared)
  else
    status = "Checking editions…"
  end
end

local function launch_selected()
  if not list then return false end
  local item = list:selected()
  if type(item) ~= "table" or type(item.id) ~= "string" or item.id == "" then
    return false
  end
  session.launch(item.id)
  return true
end

function load()
  library.query({ platform = "fpga", sort = "title", limit = 500 }, function(games, err)
    if err then
      load_err = err
      status = ""
      rows = {}
      return
    end
    resolve(games)
  end)
end

function on_input(cmd)
  if list and list:input(cmd) then return true end
  if cmd == "select" then return launch_selected() end
  return false
end

function on_hover(id)
  if list then list:on_hover(id) end
end

function on_activate(id)
  if not list or not list:on_activate(id) then return end
  launch_selected()
end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, room.width, 112, color.shade(room.theme.background, 0.7))
  gfx.rect(0, 112, room.width, 4, room.theme.accent)
  gfx.text("Computers", 64, 28, { size = 40, bold = true, color = room.theme.accent })
  local note = status
  if load_err then
    note = load_err
  elseif rows and #rows == 0 and status == "" then
    note = "No prepared ZX81 or Commodore 64 entries yet."
  end
  gfx.text(note, 64, 76, { size = 16, color = STATUS })
  if list then
    list:draw{
      label = function(item) return item.label end,
      background = PLATE,
      color = INK,
      focus_color = room.theme.accent,
      focus_text = FOCUS_INK,
    }
  end
end
