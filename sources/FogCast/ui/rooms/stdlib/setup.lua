-- First-run setup chrome (#1345): the header with its step tracker, the
-- bottom button row, the footer hint and wrapped copy. Each setup screen
-- is its own room (destination_panel = "none"); FES moves between them on
-- the setup_* launcher actions and reports the position in room.setup.

local F = require "families"
local C = F.colors

local M = {}

-- The shared geometry: header, margin and footer, as Home's hero uses.
function M.layout()
  local w, h = room.width, room.height
  local margin = math.max(16, math.floor(w / 40))
  local header = math.max(56, math.floor(h / 10))
  local footer = math.max(44, math.floor(h * 7 / 100))
  return { margin = margin, header = header, footer = footer, top = header + math.floor(margin * 0.75), bottom = h - footer }
end

-- The header: the FES wordmark and the screen name, and "Step N of M" with
-- one bar per step in place of Home's routes. Outside setup (step 0) the
-- tracker is left out.
function M.header(lay, title)
  F.header(lay.margin, lay.header, title)
  local step, steps = room.setup.step or 0, room.setup.steps or 0
  if step < 1 or steps < 1 then return end
  local bar_w, gap, bar_h = 30, 6, 5
  local bars = steps * bar_w + (steps - 1) * gap
  local right = room.width - lay.margin
  local bx = right - bars
  local by = math.floor(lay.header / 2 - bar_h / 2)
  for i = 1, steps do
    gfx.rect(bx + (i - 1) * (bar_w + gap), by, bar_w, bar_h, i <= step and C.accent or C.divider)
  end
  local label = "Step " .. step .. " of " .. steps
  local tw, th = gfx.measure(label, 18)
  gfx.text(label, bx - 16 - tw, (lay.header - th) / 2, { size = 18, color = C.secondary })
end

-- The footer band and its hint, right-aligned.
function M.footer(lay, hint)
  gfx.rect(0, lay.bottom, room.width, 2, C.divider)
  local _, th = gfx.measure(hint, 18)
  gfx.text(hint, lay.margin, lay.bottom + (lay.footer - th) / 2, { size = 18, color = C.secondary, max_w = room.width - lay.margin * 2, align = "right" })
end

-- A row of buttons from x along y. Each is {prompt, label, filled}; focus
-- is a separate accent outline, so focus and the primary fill never share
-- a shape. Registers "button:N" hits. Returns the row's right edge.
function M.buttons(x, y, list, focus)
  local size, h = 22, 48
  for i, b in ipairs(list) do
    local text = (b.prompt and b.prompt ~= "") and (b.prompt .. "  " .. b.label) or b.label
    local tw, th = gfx.measure(text, size, true)
    local w = tw + 36
    if b.filled then
      gfx.rect(x, y, w, h, C.accent)
    else
      gfx.rect(x, y, w, h, C.surface)
      F.outline(x, y, w, h, 2, C.divider)
    end
    gfx.text(text, x + 18, y + (h - th) / 2, { size = size, bold = true, color = b.filled and C.on_accent or C.text })
    if focus == i then
      F.outline(x - 5, y - 5, w + 10, h + 10, 3, C.accent)
      if b.filled then F.outline(x + 3, y + 3, w - 6, h - 6, 2, C.on_accent) end
    end
    gfx.hit("button:" .. i, x, y, w, h)
    x = x + w + 16
  end
  return x
end

-- Splits text into lines no wider than w at size.
function M.wrap(text, size, w, bold)
  local lines, line = {}, ""
  for word in string.gmatch(text or "", "%S+") do
    local try = line == "" and word or (line .. " " .. word)
    if line ~= "" and gfx.measure(try, size, bold) > w then
      lines[#lines + 1] = line
      line = word
    else
      line = try
    end
  end
  if line ~= "" then lines[#lines + 1] = line end
  return lines
end

-- Draws wrapped text and returns the y below it.
function M.text(text, x, y, w, size, color, bold)
  local step = math.floor(size * 1.35)
  for _, line in ipairs(M.wrap(text, size, w, bold)) do
    gfx.text(line, x, y, { size = size, bold = bold, color = color })
    y = y + step
  end
  return y
end

return M
