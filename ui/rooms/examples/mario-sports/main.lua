-- Sports Island: a nested room reached from the Mushroom Kingdom overworld.
-- It searches every system for Mario spin-offs and lays them out as a grid.
local Grid = require "widgets.grid"
local color = require "util.color"

local KEYWORDS = { "kart", "tennis", "golf", "party", "paint", "picross", "strikers", "baseball", "pinball", "typing", "teaches", "missing" }

local grid
local status = "searching every system..."

local function spinoff(g)
  local t = g.title:lower()
  for _, k in ipairs(KEYWORDS) do
    if t:find(k, 1, true) then return true end
  end
  return false
end

function load()
  library.query({ q = "Mario", limit = 400 }, function(games, err)
    if err then status = err return end
    local items = {}
    for _, g in ipairs(games) do
      if spinoff(g) then
        g.cover = image.cover(g.id)
        items[#items + 1] = g
      end
    end
    table.sort(items, function(a, b) return a.title < b.title end)
    grid = Grid.new{ id = "sports", x = 24, y = 96, w = room.width - 48, h = room.height - 130, cell_w = 150, cell_h = 210, gap = 18, items = items }
    status = #items .. " spin-offs found"
    if #items == 0 then status = "no Mario spin-offs in the library yet" end
  end)
end

function on_input(cmd)
  if grid and grid:input(cmd) then return true end
  if cmd == "select" and grid then
    local g = grid:selected()
    if g then session.launch(g.id) end
    return true
  end
  return false
end

function on_hover(id) if grid then grid:on_hover(id) end end
function on_activate(id) if grid and grid:on_activate(id) then on_input("select") end end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, room.height - 60, room.width, 60, "#e8d8a0")
  gfx.rect(0, room.height - 60, room.width, 6, color.shade("#e8d8a0", 0.8))
  gfx.text("SPORTS ISLAND", 24, 18, { size = 34, bold = true, color = room.theme.accent })
  gfx.text(status, room.width - 24, 30, { size = 16, align = "right", color = "#ffffff" })
  if grid then
    grid:draw{ cover = function(item) return item.cover end, plate = color.shade(room.theme.background, 0.6), label = function(g) return g.title end }
  end
end
