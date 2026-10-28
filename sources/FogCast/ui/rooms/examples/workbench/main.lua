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
  grid = Grid.new{ id = "icons", x = win.x + 12, y = win.y + 48, w = win.w - 44, h = win.h - 60, cell_w = 176, cell_h = 184, gap = 20, label_h = 48, size = 22, items = games }
  status = #games .. " items"
  publish()
end

function load()
  win = { x = 32, y = 72, w = room.width - 64, h = math.max(120, room.height - 72 - DEST) }
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
  gfx.rect(x, y, w, 36, WHITE)
  for i = 0, w, 6 do gfx.rect(x + i, y + 6, 3, 24, BLUE) end
  gfx.rect(x + 4, y + 4, 28, 28, WHITE)
  gfx.rect(x + 8, y + 8, 20, 20, BLUE)
  gfx.rect(x + 13, y + 13, 10, 10, WHITE)
  local tw, th = gfx.measure(text, 22)
  local label_w = math.min(tw + 20, w - 128)
  gfx.rect(x + 40, y + 2, label_w, 32, WHITE)
  gfx.text(text, x + 50, y + math.floor((36 - th) / 2), { size = 22, max_w = label_w - 20, color = BLUE })
  gfx.rect(x + w - 72, y + 4, 28, 28, WHITE)
  gfx.rect(x + w - 68, y + 8, 20, 20, BLUE)
  gfx.rect(x + w - 36, y + 4, 28, 28, WHITE)
  gfx.rect(x + w - 32, y + 8, 20, 20, BLUE)
end

function draw()
  gfx.clear(BLUE)
  -- screen title bar
  gfx.rect(0, 0, room.width, 40, WHITE)
  gfx.text("Workbench release 1.3   " .. status, 12, 5, { size = 22, max_w = room.width - 64, color = BLUE })
  gfx.rect(room.width - 36, 6, 28, 28, BLUE)
  -- drawer window
  gfx.rect(win.x, win.y, win.w, win.h, BLUE)
  bevel(win.x, win.y, win.w, win.h, WHITE, BLACK)
  title_bar(win.x, win.y, win.w, drawer)
  gfx.clip(win.x + 2, win.y + 36, win.w - 4, win.h - 38)
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
  gfx.rect(win.x + win.w - 18, win.y + 36, 16, win.h - 38, BLACK)
  gfx.rect(win.x + win.w - 16, win.y + 42, 12, 48, ORANGE)
  publish()
end
