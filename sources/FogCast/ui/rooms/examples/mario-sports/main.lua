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

local function publish()
  if not grid then destination.clear() return end
  local g = grid:selected()
  if not g then destination.clear() return end
  destination.set{ kind = "game", label = g.title, system = g.system, game_id = g.id, matches = { g } }
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
    grid = Grid.new{ id = "sports", x = 32, y = 152, w = room.width - 64, h = math.max(236, room.height - 152 - 130), cell_w = 200, cell_h = 236, gap = 24, items = items }
    status = #items .. " spin-offs found"
    if #items == 0 then status = "no Mario spin-offs in the library yet" end
    publish()
  end)
end

function on_input(cmd)
  if grid and grid:input(cmd) then publish() return true end
  return false
end

function on_hover(id) if grid then grid:on_hover(id) publish() end end
function on_activate(id) if grid and grid:on_activate(id) then publish() end end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, room.height - 60, room.width, 60, "#e8d8a0")
  gfx.rect(0, room.height - 60, room.width, 6, color.shade("#e8d8a0", 0.8))
  gfx.rect(0, 0, room.width, 128, color.shade(room.theme.background, 0.55))
  gfx.text("SPORTS ISLAND", 32, 20, { size = 36, bold = true, color = room.theme.accent })
  gfx.text(status, 32, 80, { size = 22, max_w = room.width - 64, color = "#ffffff" })
  if grid then
    grid:draw{ cover = function(item) return item.cover end, plate = color.shade(room.theme.background, 0.6), label = function(g) return g.title end }
  end
  publish()
end
