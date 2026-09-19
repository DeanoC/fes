-- Super Nintendo: a single-system room. The header borrows the SNES pad
-- palette; the body is the stdlib cover grid over library.query. "search"
-- (Y / Tab) drops into the ordinary library pre-filtered to the platform.
local Grid = require "widgets.grid"
local color = require "util.color"

local PLATFORM = "snes"
local BUTTONS = { "#4a90e2", "#e6c447", "#d9534f", "#5cb85c" }

local grid
local status = "loading..."

function load()
  library.query({ platform = PLATFORM, sort = "title", limit = 1000 }, function(games, err)
    if err then status = err return end
    for _, g in ipairs(games) do g.cover = image.cover(g.id) end
    grid = Grid.new{ id = "snes", x = 32, y = 120, w = room.width - 64, h = room.height - 150, cell_w = 170, cell_h = 230, gap = 20, items = games }
    status = #games .. " titles"
  end)
end

function on_input(cmd)
  if grid and grid:input(cmd) then return true end
  if cmd == "select" and grid then
    local g = grid:selected()
    if g then session.launch(g.id) end
    return true
  end
  if cmd == "search" or cmd == "tab" then
    rooms.open_library{ platform = PLATFORM, layout = "shelf" }
    return true
  end
  return false
end

function on_hover(id) if grid then grid:on_hover(id) end end
function on_activate(id) if grid and grid:on_activate(id) then on_input("select") end end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, room.width, 96, color.shade(room.theme.background, 0.75))
  gfx.rect(0, 96, room.width, 4, room.theme.accent)
  for i, c in ipairs(BUTTONS) do
    local bx = 32 + (i - 1) * 34
    gfx.rect(bx, 30, 24, 24, c)
  end
  gfx.text("SUPER NINTENDO", 190, 20, { size = 34, bold = true, color = "#f0f0f5" })
  gfx.text("ENTERTAINMENT SYSTEM", 190, 60, { size = 14, color = room.theme.accent })
  gfx.text(status .. "  ·  Y opens the full library shelf", room.width - 32, 38, { size = 16, align = "right", color = "#c8c8d8" })
  if grid then
    grid:draw{ cover = function(item) return item.cover end, plate = "#2a2a34", label = function(g) return g.title end }
  end
end
