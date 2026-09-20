-- Workbench: the classic four-colour Amiga desktop. A drawer window holds
-- the games as icons; the games come from a "strategy" collection when one
-- exists, otherwise from the Strategy genre across every system. The
-- launcher paints the compact selected-destination strip.
local Grid = require "widgets.grid"

local BLUE, WHITE, BLACK, ORANGE = "#0055aa", "#ffffff", "#000020", "#ff8800"
local DEST = 130
local grid
local win = {}
local status = "loading drawer..."
local drawer = "Strategy"

local function publish()
  if not grid then
    destination.set{ kind = "unresolved", label = drawer, resolving = true }
    return
  end
  local g = grid:selected()
  if not g then
    destination.set{ kind = "game", label = drawer, query = drawer, missing = true }
    return
  end
  destination.set{ kind = "game", label = g.title, system = g.system, game_id = g.id, matches = { g } }
end

local function fill(games)
  for _, g in ipairs(games) do g.cover = image.cover(g.id) end
  grid = Grid.new{ id = "icons", x = win.x + 8, y = win.y + 24, w = win.w - 16, h = win.h - 32, cell_w = 128, cell_h = 132, gap = 12, label_h = 34, size = 14, items = games }
  status = #games .. " items"
  publish()
end

function load()
  win = { x = 32, y = 60, w = room.width - 64, h = math.max(80, room.height - 60 - DEST) }
  publish()
  library.collections(function(list, err)
    local found
    for _, c in ipairs(list or {}) do
      if c.id == "strategy" or c.name:lower() == "strategy" then found = c end
    end
    if found then
      drawer = found.name
      library.query({ collection = found.id, sort = "title", limit = 200 }, function(games, e)
        if e then status = e destination.set{ kind = "unresolved", label = drawer, resolving = true } return end
        fill(games)
      end)
    else
      library.query({ genre = "Strategy", sort = "title", limit = 200 }, function(games, e)
        if e then status = e destination.set{ kind = "unresolved", label = drawer, resolving = true } return end
        fill(games)
      end)
    end
  end)
end

function on_input(cmd)
  if grid and grid:input(cmd) then publish() return true end
  return false
end

function on_hover(id) if grid then grid:on_hover(id) publish() end end
function on_activate(id) if grid and grid:on_activate(id) then publish() end end

local function bevel(x, y, w, h, light, dark)
  gfx.rect(x, y, w, 2, light)
  gfx.rect(x, y, 2, h, light)
  gfx.rect(x, y + h - 2, w, 2, dark)
  gfx.rect(x + w - 2, y, 2, h, dark)
end

local function title_bar(x, y, w, text)
  gfx.rect(x, y, w, 20, WHITE)
  for i = 0, w, 4 do gfx.rect(x + i, y + 4, 2, 12, BLUE) end
  gfx.rect(x + 4, y + 2, 16, 16, WHITE)
  gfx.rect(x + 6, y + 4, 12, 12, BLUE)
  gfx.rect(x + 9, y + 7, 6, 6, WHITE)
  local tw = gfx.measure(text, 14)
  gfx.rect(x + 28, y + 1, tw + 12, 18, WHITE)
  gfx.text(text, x + 34, y + 1, { size = 14, color = BLUE })
  gfx.rect(x + w - 40, y + 2, 16, 16, WHITE)
  gfx.rect(x + w - 38, y + 4, 12, 12, BLUE)
  gfx.rect(x + w - 20, y + 2, 16, 16, WHITE)
  gfx.rect(x + w - 18, y + 4, 12, 12, BLUE)
end

function draw()
  gfx.clear(BLUE)
  -- screen title bar
  gfx.rect(0, 0, room.width, 20, WHITE)
  gfx.text("Workbench release 1.3   " .. status, 8, 1, { size = 14, color = BLUE })
  gfx.rect(room.width - 20, 2, 16, 16, BLUE)
  -- disk icons on the desktop
  local dx = room.width - 90
  for i, name in ipairs({ "Workbench1.3", "Games:" }) do
    local iy = 40 + (i - 1) * 80
    gfx.rect(dx, iy, 48, 40, WHITE)
    gfx.rect(dx + 4, iy + 4, 40, 32, BLACK)
    gfx.rect(dx + 10, iy + 10, 28, 8, WHITE)
    gfx.text(name, dx + 24, iy + 44, { size = 12, align = "center", color = WHITE })
  end
  -- drawer window
  gfx.rect(win.x, win.y, win.w, win.h, BLUE)
  bevel(win.x, win.y, win.w, win.h, WHITE, BLACK)
  title_bar(win.x, win.y, win.w, drawer)
  gfx.clip(win.x + 2, win.y + 20, win.w - 4, win.h - 22)
  if grid then
    grid:draw{
      cover = function(item) return item.cover end,
      plate = BLACK,
      focus_color = ORANGE,
      color = WHITE,
      label = function(g) return g.title end,
    }
  end
  gfx.unclip()
  -- right-hand scroll gadget
  gfx.rect(win.x + win.w - 18, win.y + 20, 16, win.h - 22, BLACK)
  gfx.rect(win.x + win.w - 16, win.y + 24, 12, 40, ORANGE)
  publish()
end
