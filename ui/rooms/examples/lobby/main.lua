-- Lobby: lists every room the launcher knows about. Select opens it; the
-- Library row drops into the ordinary browser. Small enough to copy as a
-- starting point for a new room.
local List = require "widgets.list"
local color = require "util.color"

local list
local entries = {}

function load()
  entries = { { title = "Library", library = true, description = "browse every title" } }
  for _, r in ipairs(rooms.list()) do
    if r.id ~= room.id then
      entries[#entries + 1] = r
    end
  end
  list = List.new{ id = "rooms", x = 80, y = 140, w = math.min(640, room.width - 160), h = room.height - 220, row_h = 52, size = 22, items = entries }
end

function on_input(cmd)
  if list:input(cmd) then return true end
  if cmd == "select" then
    local item = list:selected()
    if not item then return true end
    if item.library then rooms.open_library{} else rooms.open(item.id) end
    return true
  end
  return false
end

function on_hover(id) list:on_hover(id) end
function on_activate(id)
  if list:on_activate(id) then on_input("select") end
end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, room.width, 100, color.shade(room.theme.background, 0.7))
  gfx.text("FogCast Rooms", 80, 28, { size = 40, bold = true, color = room.theme.accent })
  gfx.text(#entries - 1 .. " rooms installed", 80, 76, { size = 16, color = room.theme.status })
  list:draw{ label = function(item) return item.title end }
  local item = list:selected()
  if item and item.description and item.description ~= "" then
    gfx.text(item.description, 80, room.height - 60, { size = 18, max_w = room.width - 160, color = room.theme.label })
  end
end
