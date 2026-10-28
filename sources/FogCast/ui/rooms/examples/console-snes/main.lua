-- Super Nintendo: a single-system room. The header borrows the SNES pad
-- palette; the body is the stdlib cover grid over library.query. Tab
-- (and the launcher Details tap on Y) drop into the ordinary library
-- pre-filtered to the platform; Y / North opens the shared game-info panel.
local Grid = require "widgets.grid"
local color = require "util.color"

local PLATFORM = "snes"
local BUTTONS = { "#4a90e2", "#e6c447", "#d9534f", "#5cb85c" }

local grid
local status = "loading..."

local function publish()
  if not grid then destination.clear() return end
  local g = grid:selected()
  if not g then destination.clear() return end
  destination.set{ kind = "game", label = g.title, system = g.system, game_id = g.id, matches = { g } }
end

function load()
  library.query({ platform = PLATFORM, sort = "title", limit = 1000 }, function(games, err)
    if err then status = err return end
    for _, g in ipairs(games) do g.cover = image.cover(g.id) end
    grid = Grid.new{ id = "snes", x = 32, y = 152, w = room.width - 64, h = math.max(248, room.height - 152 - 130), cell_w = 216, cell_h = 248, gap = 24, items = games }
    status = #games .. " titles"
    publish()
  end)
end

function on_input(cmd)
  if grid and grid:input(cmd) then publish() return true end
  if cmd == "search" or cmd == "tab" then
    rooms.open_library{ platform = PLATFORM, layout = "shelf" }
    return true
  end
  return false
end

function on_hover(id) if grid then grid:on_hover(id) publish() end end
function on_activate(id) if grid and grid:on_activate(id) then publish() end end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, room.width, 128, color.shade(room.theme.background, 0.75))
  gfx.rect(0, 128, room.width, 4, room.theme.accent)
  for i, c in ipairs(BUTTONS) do
    local bx = 32 + (i - 1) * 34
    gfx.rect(bx, 40, 24, 24, c)
  end
  gfx.text("SUPER NINTENDO", 190, 24, { size = 36, max_w = room.width - 222, bold = true, color = "#f0f0f5" })
  gfx.text(status .. " · Tab opens the full library shelf", 190, 82, { size = 22, max_w = room.width - 222, color = "#D8D2E5" })
  if grid then
    grid:draw{ cover = function(item) return item.cover end, plate = "#2a2a34", label = function(g) return g.title end }
  end
  publish()
end
