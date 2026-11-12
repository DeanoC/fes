-- First-run setup, last step (#1345): one row per computer with the chip
-- Home will show and what it has: its games and whether it needs the
-- user's firmware. Finish writes the first-run marker and opens Home; Back
-- (or B) returns to the step before.
--
-- The controller line and the games folders arrive with their own setup
-- steps (#980, #1346).

local F = require "families"
local S = require "setup"
local C = F.colors

local BUTTONS = {
  { prompt = "A", label = "Finish", filled = true, action = "setup_finish" },
  { prompt = "B", label = "Back", action = "setup_back" },
}

local focus = 1
local generation = 0

-- Steps skipped earlier, other than this one, make the title "You can play
-- now".
local function skipped_any()
  for _, id in ipairs(room.setup.skipped or {}) do
    if id ~= "ready" then return true end
  end
  return false
end

local function publish()
  local b = BUTTONS[focus]
  destination.set{ kind = "action", action = b.action, label = b.label, status = b.label }
end

function load()
  F.load(function() generation = generation + 1 end)
  publish()
end

function on_resume() F.reload() end

function update() F.update() end

function on_input(cmd)
  if cmd == "left" or cmd == "up" then
    focus = 1
  elseif cmd == "right" or cmd == "down" then
    focus = #BUTTONS
  else
    return false
  end
  publish()
  return true
end

local function focus_hit(id)
  local n = tonumber(string.match(id or "", "^button:(%d+)$"))
  if not n or not BUTTONS[n] then return end
  focus = n
  publish()
end
function on_hover(id) focus_hit(id) end
function on_activate(id) focus_hit(id) end

-- What a family has, after its name.
local function detail(f)
  local state = F.state(f)
  if state == "checking" then return "Checking…" end
  if state == "firmware" then return (F.games(f) or "No games yet") .. "  ·  needs your firmware files" end
  if state == "missing" then return "Not on this kit" end
  if state == "blocked" then
    local _, line = F.tile(f)
    return line
  end
  return F.games(f) or "Ready"
end

local painted = nil

function draw()
  if room.presented and painted and painted.focus == focus and painted.generation == generation
    and painted.w == room.width and painted.h == room.height and gfx.reuse() then
    return
  end
  local lay = S.layout()
  local m = lay.margin
  S.header(lay, room.title)
  local big = room.height >= 648 and 40 or 34
  local y = lay.top
  gfx.text(skipped_any() and "You can play now" or "You're ready to play", m, y, { size = big, bold = true, color = C.text })
  y = y + math.floor(big * 1.6)

  local buttons_y = lay.bottom - 24 - 48
  local gap = 10
  local shown = math.min(4, #F.list)
  local row_h = math.min(90, math.floor((buttons_y - 16 - y - gap * (shown - 1)) / shown))
  local w = room.width - m * 2
  local compact = row_h < 55
  local name = compact and 16 or (row_h >= 80 and 22 or 20)
  for i = 1, shown do
    local f = F.list[i]
    local ry = y + (i - 1) * (row_h + gap)
    gfx.rect(m, ry, w, row_h, C.surface)
    F.outline(m, ry, w, row_h, 1, C.divider)
    local pad = compact and 4 or 7
    local mw = math.floor((row_h - pad * 2) * 1.4)
    F.mark(f, m + pad, ry + pad, mw, row_h - pad * 2)
    local tx = m + pad + mw + 18
    local text_y = ry + (compact and 1 or row_h * 0.18)
    gfx.text(f.label, tx, text_y, { size = name, bold = true, color = C.text, max_w = w - (tx - m) - 160 })
    gfx.text(detail(f), tx, text_y + name + (compact and 0 or 8), { size = compact and 13 or name - 2, color = C.secondary, max_w = w - (tx - m) - 160 })
    local chip = F.tile(f)
    local cw = gfx.measure(chip, 18, true) + 24
    F.chip(chip, m + w - cw - 16, ry + (row_h - 30) / 2, 18)
  end

  S.buttons(m, buttons_y, BUTTONS, focus)
  local later = "Change any of this later in Settings."
  local _, lh = gfx.measure(later, 20)
  gfx.text(later, m, buttons_y + (48 - lh) / 2, { size = 20, color = C.secondary, max_w = room.width - m * 2, align = "right" })
  S.footer(lay, "D-pad move  ·  A finish  ·  B back")
  painted = { focus = focus, generation = generation, w = room.width, h = room.height }
  gfx.paint({ { "c", C.background } })
end
