-- Lobby: lists every room the launcher knows about. Confirm on a room
-- destination enters it; the Library row opens the ordinary browser.
-- The launcher paints the compact selected-destination strip.
local List = require "widgets.list"
local color = require "util.color"

local DEST = 130
local list
local entries = {}

local function publish()
  local item = list and list:selected()
  if not item then
    destination.set{ kind = "unresolved", label = "FES Rooms" }
    return
  end
  if item.library then
    destination.set{ kind = "library", label = item.title }
    return
  end
  destination.set{ kind = "room", label = item.title, room_id = item.id, note = item.description }
end

function load()
  entries = { { title = "Library", library = true, description = "browse every title" } }
  for _, r in ipairs(rooms.list()) do
    if r.id ~= room.id then
      entries[#entries + 1] = r
    end
  end
  local h = math.max(64, room.height - 148 - DEST)
  list = List.new{ id = "rooms", x = 48, y = 148, w = math.min(760, room.width - 96), h = h, row_h = 64, size = 24, items = entries }
  publish()
end

function on_input(cmd)
  if list:input(cmd) then
    publish()
    return true
  end
  return false
end

function on_hover(id)
  if list:on_hover(id) then publish() end
end
function on_activate(id)
  if list:on_activate(id) then publish() end
end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, room.width, 120, color.shade(room.theme.background, 0.7))
  gfx.text("FES Rooms", 48, 24, { size = 40, bold = true, color = room.theme.accent })
  gfx.text("Choose a destination · " .. (#entries - 1) .. " rooms installed", 48, 80, { size = 22, color = room.theme.status })
  list:draw{ label = function(item) return item.title end }
  publish()
end
