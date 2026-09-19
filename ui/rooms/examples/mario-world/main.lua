-- Mushroom Kingdom: a Super Mario World style overworld. Every node is a
-- title resolved from the library by name at load time (game ids are path
-- based, so rooms search rather than hard-code them). The island node opens
-- a nested room. The last visited node is remembered between sessions.
local Map = require "widgets.nodemap"
local ease = require "util.ease"
local color = require "util.color"

local LEVELS = {
  { id = "dk",    x = 120, y = 420, label = "Donkey Kong",           search = "Donkey Kong",           platform = "arcade" },
  { id = "smb",   x = 300, y = 360, label = "Super Mario Bros.",     search = "Super Mario Bros",      platform = "nes" },
  { id = "smb2",  x = 440, y = 260, label = "Super Mario Bros. 2",   search = "Super Mario Bros. 2",   platform = "nes" },
  { id = "smb3",  x = 600, y = 340, label = "Super Mario Bros. 3",   search = "Super Mario Bros. 3",   platform = "nes" },
  { id = "sml",   x = 700, y = 180, label = "Super Mario Land",      search = "Super Mario Land",      platform = "gb" },
  { id = "smw",   x = 820, y = 420, label = "Super Mario World",     search = "Super Mario World",     platform = "snes" },
  { id = "yoshi", x = 980, y = 300, label = "Yoshi's Island",        search = "Yoshi's Island",        platform = "snes" },
  { id = "island", x = 1080, y = 500, label = "Sports Island", room = "example.mario-sports", color = "#2fb457" },
}
local EDGES = {
  { "dk", "smb" }, { "smb", "smb2" }, { "smb2", "smb3" }, { "smb", "smb3" }, { "smb2", "sml" },
  { "smb3", "smw" }, { "sml", "yoshi" }, { "smw", "yoshi" }, { "smw", "island" },
}

local map
local resolving = 0
local clouds = {}

local function pick(games, wanted)
  wanted = wanted:lower()
  for _, g in ipairs(games) do
    if g.title:lower():find(wanted, 1, true) and g.launchable then return g end
  end
  for _, g in ipairs(games) do
    if g.title:lower():find(wanted, 1, true) then return g end
  end
  return games[1]
end

local function resolve(node)
  resolving = resolving + 1
  library.query({ q = node.search, platform = node.platform, limit = 20 }, function(games, err)
    resolving = resolving - 1
    if err or not games or #games == 0 then
      node.missing = true
      return
    end
    local g = pick(games, node.search)
    node.game = g
    node.done = (g.play_count or 0) > 0
    node.icon = image.cover(g.id)
  end)
end

function load()
  local scale = room.width / 1200
  for _, lvl in ipairs(LEVELS) do
    lvl.x = lvl.x * scale
    lvl.y = lvl.y * (room.height / 620)
  end
  map = Map.new{ id = "world", nodes = LEVELS, edges = EDGES, radius = 22, focus = store.get("focus", "dk") }
  for _, lvl in ipairs(LEVELS) do
    if lvl.search then resolve(lvl) end
  end
  for i = 1, 6 do
    clouds[i] = { x = (i - 1) * room.width / 6 + (i * 37) % 90, y = 40 + (i * 53) % 90, w = 90 + (i * 29) % 60, speed = 8 + (i * 7) % 12 }
  end
end

function update(dt)
  for _, c in ipairs(clouds) do
    c.x = c.x + c.speed * dt
    if c.x > room.width + c.w then c.x = -c.w end
  end
end

local function activate(node)
  if not node then return end
  if node.room then rooms.open(node.room) return end
  if node.game then session.launch(node.game.id) end
end

function on_input(cmd)
  if map:input(cmd) then
    store.set("focus", map.focus)
    return true
  end
  if cmd == "select" then activate(map:focused()) return true end
  return false
end

function on_hover(id) if map:on_hover(id) then store.set("focus", map.focus) end end
function on_activate(id) if map:on_activate(id) then activate(map:focused()) end end

function on_resume()
  local node = map:focused()
  if node and node.game then node.done = true end
end

local function cloud(c)
  gfx.rect(c.x, c.y + 10, c.w, 22, "#ffffff")
  gfx.rect(c.x + c.w * 0.2, c.y, c.w * 0.5, 20, "#ffffff")
end

function draw()
  local w, h = room.width, room.height
  gfx.clear(room.theme.background)
  gfx.rect(0, h * 0.72, w, h * 0.28, "#3ca63c")
  gfx.rect(0, h * 0.72, w, 8, "#8ad06a")
  for i = 0, 12 do
    gfx.rect(i * (w / 12), h * 0.80 + (i % 3) * 12, w / 24, 10, "#2c8a2c")
  end
  for _, c in ipairs(clouds) do cloud(c) end

  gfx.rect(0, 0, w, 56, color.with_alpha("#000000", 90))
  gfx.text("MUSHROOM KINGDOM", 24, 10, { size = 30, bold = true, color = room.theme.accent })
  local status = resolving > 0 and "finding levels..." or ("select a level  ·  " .. (#LEVELS - 1) .. " levels")
  gfx.text(status, w - 24, 20, { size = 16, align = "right", color = "#ffffff" })

  map:draw{ edge_color = "#f4e6b4", node_color = "#e84a3a", done_color = "#2fb457", focus_color = room.theme.accent, path_width = 8, labels_focused_only = true, label_color = "#ffffff" }

  local node = map:focused()
  if node then
    local px, py, pw, ph = 24, h - 150, w - 48, 126
    gfx.rect(px, py, pw, ph, color.with_alpha("#000000", 150))
    gfx.rect(px, py, 6, ph, room.theme.accent)
    local tx = px + 28
    if node.icon and node.icon.ready then
      gfx.image(node.icon, px + 16, py + 12, 76, 102)
      tx = px + 110
    end
    gfx.text(node.label, tx, py + 14, { size = 26, bold = true, color = "#ffffff" })
    if node.room then
      gfx.text("Enter the island", tx, py + 52, { size = 18, color = "#cde" })
    elseif node.game then
      local g = node.game
      local meta = (g.system or ""):upper() .. (g.year ~= "" and ("  ·  " .. g.year) or "")
      gfx.text(g.title .. "   " .. meta, tx, py + 52, { size = 18, color = "#cde", max_w = pw - (tx - px) - 20 })
      gfx.text(g.launchable and "Press select to play" or ("Not launchable: " .. (g.launch_block or "unknown")), tx, py + 84, { size = 16, color = g.launchable and room.theme.accent or "#ff8a80" })
    elseif node.missing then
      gfx.text("Not in your library yet (" .. node.search .. " on " .. node.platform .. ")", tx, py + 52, { size = 18, color = "#ff8a80" })
    else
      gfx.text("Searching the library...", tx, py + 52, { size = 18, color = "#cde" })
    end
  end
end
