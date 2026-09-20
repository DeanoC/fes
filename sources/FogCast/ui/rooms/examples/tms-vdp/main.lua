-- TMS9918 Family: a cross-system room driven by platform hardware tags.
-- Left pane lists platforms tagged vdp:tms9918-family; right pane lists that
-- platform's games. Left/right switch panes. The launcher owns Confirm and
-- Details from the published destination; it paints the compact strip.
local List = require "widgets.list"

local TAG = "vdp:tms9918-family"
local DEST = 130
local platforms, games
local pane = 1
local generation = 0
local status = "reading platforms..."
local loading_games = false

local function has_tag(p)
  for _, t in ipairs(p.tags or {}) do
    if t == TAG then return true end
  end
  return false
end

local function publish()
  local g = games and games:selected()
  local p = platforms and platforms:selected()
  if pane == 2 and g then
    destination.set{ kind = "game", label = g.title, system = g.system, game_id = g.id, matches = { g } }
    return
  end
  if pane == 2 and p and not loading_games then
    destination.set{ kind = "game", label = p.label, query = p.label, platform = p.id, missing = true }
    return
  end
  if loading_games or not p then
    destination.set{ kind = "unresolved", label = p and p.label or "TMS9918 Family", platform = p and p.id or "", resolving = true }
    return
  end
  destination.set{ kind = "unresolved", label = p.label, platform = p.id }
end

local function load_games()
  local p = platforms:selected()
  if not p then return end
  generation = generation + 1
  local gen = generation
  loading_games = true
  status = "loading " .. p.label .. "..."
  publish()
  library.query({ platform = p.id, sort = "title", limit = 600 }, function(list, err)
    if gen ~= generation then return end
    loading_games = false
    if err then status = err publish() return end
    for _, g in ipairs(list) do g.cover = image.cover(g.id) end
    games:set_items(list)
    status = #list .. " titles on " .. p.label
    publish()
  end)
end

function load()
  local lw = math.floor(room.width * 0.32)
  local h = math.max(56, room.height - 110 - DEST)
  platforms = List.new{ id = "plat", x = 24, y = 110, w = lw, h = h, row_h = 56, size = 22, items = {} }
  games = List.new{ id = "game", x = 24 + lw + 24, y = 110, w = room.width - lw - 72, h = h, row_h = 40, size = 20, items = {} }
  publish()
  library.platforms(function(list, err)
    if err then status = err publish() return end
    local rows = {}
    for _, p in ipairs(list) do
      if has_tag(p) then rows[#rows + 1] = p end
    end
    platforms:set_items(rows)
    if #rows == 0 then status = "no tagged platforms in this library" publish() return end
    load_games()
  end)
end

function on_input(cmd)
  if cmd == "left" then pane = 1 publish() return true end
  if cmd == "right" then pane = 2 publish() return true end
  if pane == 1 then
    if platforms:input(cmd) then load_games() return true end
    if cmd == "select" then pane = 2 publish() return true end
  else
    if games:input(cmd) then publish() return true end
  end
  return false
end

function on_hover(id)
  if platforms:on_hover(id) then pane = 1 load_games() elseif games:on_hover(id) then pane = 2 publish() end
end
function on_activate(id)
  if platforms:on_activate(id) then
    pane = 1
    load_games()
  elseif games:on_activate(id) then
    pane = 2
    publish()
  end
end

local function platform_label(p)
  local tags = {}
  for _, t in ipairs(p.tags or {}) do
    if t:sub(1, 4) == "cpu:" or t:sub(1, 4) == "vdp:" then tags[#tags + 1] = t:sub(5) end
  end
  return p.label .. "   " .. table.concat(tags, " · ")
end

function draw()
  gfx.clear(room.theme.background)
  gfx.text("TMS9918 FAMILY", 24, 18, { size = 34, bold = true, color = room.theme.accent })
  gfx.text("systems tagged " .. TAG, 24, 62, { size = 16, color = "#8aa" })
  gfx.text(status, room.width - 24, 30, { size = 16, align = "right", color = "#cde" })
  platforms:draw{ label = platform_label, background = "#161c22", focus_color = pane == 1 and room.theme.accent or "#2a3640", focus_text = pane == 1 and "#101418" or "#dde" }
  local g = games:selected()
  games:draw{ label = function(item) return item.title .. (item.year ~= "" and ("  (" .. item.year .. ")") or "") end, background = "#161c22", focus_color = pane == 2 and room.theme.accent or "#2a3640", focus_text = pane == 2 and "#101418" or "#dde" }
  if g and g.cover and g.cover.ready then
    local cw = 120
    gfx.image(g.cover, room.width - cw - 40, math.max(110, room.height - DEST - 170), cw, 160)
  end
  publish()
end
