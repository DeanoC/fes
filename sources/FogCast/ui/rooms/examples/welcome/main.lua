-- First-run setup, step 1 (#1345): what FES is, the four computers, and
-- what is built in. Continue goes to the next step and Skip setup goes
-- straight to Home, which then shows what is still to do. FES moves
-- between the steps on the setup_* launcher actions this room publishes.

local F = require "families"
local S = require "setup"
local C = F.colors

local BUTTONS = {
  { prompt = "A", label = "Continue", filled = true, action = "setup_continue" },
  { label = "Skip setup", action = "setup_skip" },
}
local NEXT = "Next: your controller, then where your games are. It takes a couple of minutes."
local SKIP = "Skip goes straight to Home. Home shows what is still to do, and Settings › Run setup again brings this back."

local focus = 1

local function publish()
  local b = BUTTONS[focus]
  destination.set{ kind = "action", action = b.action, label = b.label, status = b.label }
end

function load() publish() end

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

local painted = nil

function draw()
  if room.presented and painted and painted.focus == focus and painted.w == room.width and painted.h == room.height and gfx.reuse() then
    return
  end
  local lay = S.layout()
  local m = lay.margin
  S.header(lay, room.title)
  local left_w = math.floor((room.width - m * 3) * 0.48)
  local y = lay.top
  local big = room.height >= 648 and 40 or 34
  local body = room.height >= 648 and 24 or 21
  gfx.text("Welcome to FES", m, y, { size = big, bold = true, color = C.text })
  y = y + math.floor(big * 1.5)
  y = S.text("FES turns this kit into classic home computers, rebuilt in its chip. Four are ready to set up.", m, y, left_w, body, C.text)
  y = y + 12
  local note = "Some computers have their built-in software (firmware) included. For the others, and for your games, you use your own files. You don't need a network or a computer to start."
  local note_size = body - 4
  local lines = S.wrap(note, note_size, left_w - 40)
  local note_h = #lines * math.floor(note_size * 1.35) + 24
  gfx.rect(m, y, left_w, note_h, C.raised)
  F.outline(m, y, left_w, note_h, 2, C.divider)
  S.text(note, m + 20, y + 12, left_w - 40, note_size, C.text)
  y = y + note_h + 14
  S.text(focus == 2 and SKIP or NEXT, m, y, left_w, note_size, C.secondary)

  -- The four computers, two by two, each name outside its mark.
  local gx = m * 2 + left_w
  local gw = room.width - m - gx
  local gap = 16
  local name = room.height >= 648 and 20 or 18
  local cell_w = (gw - gap) / 2
  local cell_h = (lay.bottom - 24 - lay.top - gap) / 2
  local mark_h = cell_h - name - 14
  for i = 1, math.min(4, #F.list) do
    local f = F.list[i]
    local cx = gx + ((i - 1) % 2) * (cell_w + gap)
    local cy = lay.top + math.floor((i - 1) / 2) * (cell_h + gap)
    F.mark(f, cx, cy, cell_w, mark_h)
    gfx.text(f.label, cx, cy + mark_h + 6, { size = name, bold = true, color = C.text, max_w = cell_w })
  end

  S.buttons(m, lay.bottom - 24 - 48, BUTTONS, focus)
  S.footer(lay, "D-pad move  ·  A continue  ·  Keyboard: arrows and Enter")
  painted = { focus = focus, w = room.width, h = room.height }
  gfx.paint({ { "c", C.background } })
end
