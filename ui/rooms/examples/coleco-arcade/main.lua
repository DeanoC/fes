-- ColecoVision Arcade: a short cabinet of classic Coleco ports. Each title
-- is resolved from the library by search at load (game ids are path
-- derived, so rooms do not hard-code them). Missing titles are skipped so
-- the room still opens on a kit that only has some — or none — of them.
-- Loading results never move the player's selection.
local List = require "widgets.list"
local color = require "util.color"

local PLATFORM = "coleco"
local DEST = 130
local TMS = { "#21c842", "#54fcfc", "#e54c4c", "#e6c547" }

local CABINETS = {
  { id = "dk",    label = "Donkey Kong",  search = "Donkey Kong",  note = "Coleco's pack-in arcade hit." },
  { id = "carn",  label = "Carnival",     search = "Carnival",     note = "A shooting gallery on the TMS9918." },
  { id = "zax",   label = "Zaxxon",       search = "Zaxxon",       note = "Sega's isometric fortress, Coleco port." },
  { id = "congo", label = "Congo Bongo",  search = "Congo Bongo",  note = "Isometric jungle — another Sega arcade port." },
  { id = "frog",  label = "Frogger",      search = "Frogger",      note = "Get across the road and the river." },
}

local list
local resolving = 0
local status = "searching the library..."

local function coleco_games(games)
  local out = {}
  for _, g in ipairs(games or {}) do
    if g.system == PLATFORM then out[#out + 1] = g end
  end
  return out
end

local function visible()
  local items = {}
  for _, cab in ipairs(CABINETS) do
    if cab.matches and #cab.matches > 0 then
      items[#items + 1] = cab
    end
  end
  return items
end

local function publish()
  if resolving > 0 and not (list and list:selected()) then
    destination.set{ kind = "unresolved", label = "ColecoVision Arcade", resolving = true }
    return
  end
  local item = list and list:selected()
  if not item then
    destination.set{ kind = "unresolved", label = "ColecoVision Arcade" }
    return
  end
  destination.set{
    kind = "game",
    label = item.label,
    system = PLATFORM,
    platform = PLATFORM,
    query = item.search,
    game_id = item.game and item.game.id or "",
    matches = item.matches,
    note = item.note,
  }
end

local function show_found()
  local items = visible()
  local want = store.get("focus", CABINETS[1].id)
  list:set_items(items)
  for i, item in ipairs(items) do
    if item.id == want then
      list.focus = i
      break
    end
  end
  list:clamp()
  if #items == 0 then
    status = "no Coleco arcade titles in this library yet"
  else
    status = #items .. " cabinets"
  end
  publish()
end

local function resolve(cab)
  resolving = resolving + 1
  library.query({ q = cab.search, platform = PLATFORM, limit = 20 }, function(games, err)
    resolving = resolving - 1
    if not err and games then
      local result = destination.classify(coleco_games(games), { q = cab.search })
      if result.state ~= "missing" and result.matches and #result.matches > 0 then
        cab.matches = result.matches
        cab.game = result.game
        if result.game then
          cab.cover = image.cover(result.game.id)
        elseif result.matches[1] then
          cab.cover = image.cover(result.matches[1].id)
        end
      end
    end
    if resolving == 0 then show_found() else publish() end
  end)
end

function load()
  local h = math.max(56, room.height - 110 - DEST)
  list = List.new{
    id = "cabinets",
    x = 32,
    y = 110,
    w = math.min(560, room.width - 320),
    h = h,
    row_h = 56,
    size = 22,
    items = {},
  }
  publish()
  for _, cab in ipairs(CABINETS) do
    resolve(cab)
  end
end

function on_input(cmd)
  if list and list:input(cmd) then
    local item = list:selected()
    if item then store.set("focus", item.id) end
    publish()
    return true
  end
  if cmd == "search" or cmd == "tab" then
    rooms.open_library{ platform = PLATFORM, layout = "shelf" }
    return true
  end
  return false
end

function on_hover(id)
  if list and list:on_hover(id) then
    local item = list:selected()
    if item then store.set("focus", item.id) end
    publish()
  end
end

function on_activate(id)
  if list and list:on_activate(id) then
    local item = list:selected()
    if item then store.set("focus", item.id) end
    publish()
  end
end

function on_resume()
  local item = list and list:selected()
  if item and item.game then
    destination.play_history(item.game)
  end
  publish()
end

function draw()
  local w, h = room.width, room.height
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, w, 96, color.shade(room.theme.background, 0.7))
  gfx.rect(0, 96, w, 4, room.theme.accent)
  for i, c in ipairs(TMS) do
    gfx.rect(32 + (i - 1) * 28, 36, 20, 20, c)
  end
  gfx.text("COLECOVISION", 160, 16, { size = 34, bold = true, color = room.theme.accent })
  gfx.text("ARCADE CABINET", 160, 56, { size = 14, color = "#c8b88a" })
  gfx.text(status .. "  ·  Tab opens the Coleco shelf", w - 32, 38, { size = 16, align = "right", color = "#c8b88a" })
  if list then
    list:draw{ label = function(item) return item.label end, background = "#1c1810", focus_color = room.theme.accent, focus_text = "#14110c", color = "#f0e6c8" }
  end
  local item = list and list:selected()
  if item and item.cover and item.cover.ready then
    local cw, ch = 160, 210
    local cx = w - cw - 48
    local cy = 130
    gfx.rect(cx - 8, cy - 8, cw + 16, ch + 16, room.theme.accent)
    gfx.image(item.cover, cx, cy, cw, ch)
  end
  gfx.rect(0, h - 8, w, 8, "#6b3e26")
  publish()
end
