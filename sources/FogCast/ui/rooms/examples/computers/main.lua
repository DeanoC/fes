-- Prepared systems launch through the ordinary library. Set up opens the
-- selected system's workbench, including models and saved-media controls.
local List = require "widgets.list"
local color = require "util.color"

-- Leave room for Tenfoot's status row below the list viewport.
local DEST = 180
-- Systems uses a dark plate, light unfocused ink and dark ink on the
-- selected row. The setup page carries the family and model selectors.
local PLATE = "#1C2420"
local INK = "#F0F4F1"
local FOCUS_INK = "#141916"
local STATUS = "#C5D4CB"
local systems = require "systems"
local SETUP = {
  ["fes.zx81"] = "example.hardware",
  ["fes.zx80"] = "example.hardware",
  ["fes.ts1000"] = "example.hardware",
  ["fes.ts1500"] = "example.hardware",
}
local focus = "list"

local list
local rows
local status = "Looking for prepared systems…"
local load_err

local function machine_of(g)
  if type(g) ~= "table" then return nil end
  local value = systems.by_id[g.core_id or ""]
  return value and value.label or nil
end

local function short_id(id)
  id = id or ""
  if #id > 8 then return id:sub(1, 8) end
  return id
end

local function base_label(g)
  local machine = machine_of(g)
  local title = g.title or ""
  local label = machine
  if title ~= "" and title ~= machine then label = machine .. "  ·  " .. title end
  if g.region and g.region ~= "" then
    label = label .. " (" .. g.region .. ")"
  end
  return label
end

local function add_rows(out, g)
  if not machine_of(g) or not g.launchable or not g.id or g.id == "" then return end
  local label = base_label(g)
  local seen = {}
  if g.expansion_id and g.expansion_id ~= "" then
    seen[g.expansion_id] = true
    label = label .. "  ·  Expansion " .. short_id(g.expansion_id)
  end
  for _, slot in ipairs(g.slot_expansions or {}) do
    local eid = slot.expansion_id or ""
    if eid ~= "" and not seen[eid] then
      seen[eid] = true
      local n = slot.slot or 0
      label = label .. "  ·  Slot " .. tostring(n) .. " " .. short_id(eid)
    end
  end
  out[#out + 1] = { id = g.id, core_id = g.core_id, label = label }
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
  local h = math.max(64, room.height - 148 - DEST)
  if list then
    list:set_items(rows)
    return
  end
  list = List.new{
    id = "computers",
    x = 48, y = 148,
    w = room.width - 96,
    h = h, row_h = 64, size = 24,
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

local function open_setup()
  if rows == nil then return false end
  if not list or not list:selected() then rooms.open("example.system") return true end
  local item = list:selected()
  if type(item) ~= "table" then return false end
  local room_id = SETUP[item.core_id or ""] or "example.system"
  rooms.open(room_id, { game_id = item.id, core_id = item.core_id })
  return true
end

function load()
  library.query({
    platform = "fpga",
    core_ids = systems.ids,
    sort = "title",
    limit = 2000,
  }, function(games, err)
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
  if focus == "setup" then
    if cmd == "left" or cmd == "up" or cmd == "down" then
      focus = "list"
      return true
    end
    if cmd == "select" then return open_setup() end
    if cmd == "right" then return true end
    return false
  end
  if cmd == "right" then
    if rows ~= nil then focus = "setup" end
    return true
  end
  if list and list:input(cmd) then return true end
  if cmd == "select" then return launch_selected() end
  return false
end

function on_hover(id)
  if id == "setup" and rows ~= nil then focus = "setup" return end
  if list and list:on_hover(id) then focus = "list" end
end

function on_activate(id)
  if id == "setup" then
    if rows == nil then return end
    focus = "setup"
    open_setup()
    return
  end
  if not list or not list:on_activate(id) then return end
  focus = "list"
  launch_selected()
end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, room.width, 124, color.shade(room.theme.background, 0.7))
  gfx.rect(0, 124, room.width, 4, room.theme.accent)
  gfx.text("Systems", 48, 24, { size = 40, bold = true, color = room.theme.accent })
  local note = status
  if load_err then
    note = load_err
  elseif rows and #rows == 0 and status == "" then
    note = "No prepared systems yet. Add a system to get started."
  elseif rows and #rows > 0 and status == "" then
    note = "Confirm starts. Set up opens system setup."
  end
  gfx.text(note, 48, 82, { size = 22, max_w = room.width - 96, color = STATUS })
  if rows ~= nil then
    local x, y, w, h = room.width - 228, 24, 180, 52
    local fill = focus == "setup" and room.theme.accent or PLATE
    local ink = focus == "setup" and FOCUS_INK or INK
    gfx.rect(x, y, w, h, fill)
    local label = rows and #rows > 0 and "Set up" or "Add system"
    local _, th = gfx.measure(label, 24, focus == "setup")
    gfx.text(label, x + 16, y + math.floor((h - th) / 2), { size = 24, max_w = w - 32, color = ink, bold = focus == "setup" })
    if focus == "setup" then
      gfx.rect(x, y, w, 2, "#FFFFFF")
      gfx.rect(x, y + h - 2, w, 2, "#FFFFFF")
      gfx.rect(x, y, 2, h, "#FFFFFF")
      gfx.rect(x + w - 2, y, 2, h, "#FFFFFF")
    end
    gfx.hit("setup", x, y, w, h)
  end
  if list then
    list:draw{
      label = function(item) return item.label end,
      background = PLATE,
      color = INK,
      focus_color = focus == "list" and room.theme.accent or "#303E35",
      focus_text = focus == "list" and FOCUS_INK or INK,
    }
  end
end
