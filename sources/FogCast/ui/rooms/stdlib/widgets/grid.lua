-- widgets.grid: a scrolling cover/icon grid with d-pad focus.
--
--   local grid = require("widgets.grid").new{ x=, y=, w=, h=, cell_w=, cell_h=, gap=, items=, id="grid" }
--   grid:input(cmd)     -- up/down/left/right; returns true when consumed
--   grid:selected()     -- item, index
--   grid:draw{ cover=function(item) return image end, label=function(item) end, ... }
local Grid = {}
Grid.__index = Grid
local M = {}

function M.new(opts)
  opts = opts or {}
  local self = setmetatable({
    id = opts.id or "grid",
    x = opts.x or 0, y = opts.y or 0,
    w = opts.w or 800, h = opts.h or 500,
    cell_w = opts.cell_w or 160,
    cell_h = opts.cell_h or 220,
    gap = opts.gap or 16,
    label_h = opts.label_h or 28,
    size = opts.size or 16,
    items = opts.items or {},
    focus = 1,
    scroll_row = 0,
  }, Grid)
  self:layout()
  return self
end

function Grid:layout()
  self.cols = math.max(1, math.floor((self.w + self.gap) / (self.cell_w + self.gap)))
  self.rows_visible = math.max(1, math.floor((self.h + self.gap) / (self.cell_h + self.gap)))
  self:clamp()
end

function Grid:set_items(items)
  self.items = items or {}
  self:clamp()
end

function Grid:row_of(i) return math.floor((i - 1) / self.cols) end

function Grid:clamp()
  local n = #self.items
  if n == 0 then self.focus, self.scroll_row = 0, 0 return end
  if self.focus < 1 then self.focus = 1 end
  if self.focus > n then self.focus = n end
  local row = self:row_of(self.focus)
  if row < self.scroll_row then self.scroll_row = row end
  if row >= self.scroll_row + self.rows_visible then self.scroll_row = row - self.rows_visible + 1 end
  if self.scroll_row < 0 then self.scroll_row = 0 end
end

function Grid:move(dx, dy)
  local n = #self.items
  if n == 0 then return false end
  local before = self.focus
  if dx ~= 0 then
    local target = self.focus + dx
    if target >= 1 and target <= n and self:row_of(target) == self:row_of(self.focus) then
      self.focus = target
    end
  end
  if dy ~= 0 then
    local target = self.focus + dy * self.cols
    if target > n and dy > 0 and self:row_of(self.focus) < self:row_of(n) then target = n end
    if target >= 1 and target <= n then self.focus = target end
  end
  self:clamp()
  return self.focus ~= before
end

function Grid:input(cmd)
  if cmd == "up" then return self:move(0, -1) or true end
  if cmd == "down" then return self:move(0, 1) or true end
  if cmd == "left" then return self:move(-1, 0) or true end
  if cmd == "right" then return self:move(1, 0) or true end
  return false
end

function Grid:selected()
  if self.focus < 1 then return nil, 0 end
  return self.items[self.focus], self.focus
end

function Grid:hit_id(i) return self.id .. ":" .. i end

function Grid:index_from_hit(id)
  local prefix = self.id .. ":"
  if id:sub(1, #prefix) ~= prefix then return nil end
  local i = tonumber(id:sub(#prefix + 1))
  if not i or i < 1 or i > #self.items then return nil end
  return i
end

function Grid:on_hover(id)
  local i = self:index_from_hit(id)
  if not i then return false end
  self.focus = i
  self:clamp()
  return true
end

function Grid:on_activate(id) return self:on_hover(id) end

-- cell_rect(i) -> x, y or nil when the cell is scrolled out of view.
function Grid:cell_rect(i)
  local row = self:row_of(i) - self.scroll_row
  if row < 0 or row >= self.rows_visible then return nil end
  local col = (i - 1) % self.cols
  return self.x + col * (self.cell_w + self.gap), self.y + row * (self.cell_h + self.gap)
end

-- fit(img, bx, by, bw, bh) -> x, y, w, h aspect-fitted inside a box.
function M.fit(img, bx, by, bw, bh)
  local iw, ih = img.w, img.h
  if not iw or not ih or iw <= 0 or ih <= 0 then return bx, by, bw, bh end
  local s = math.min(bw / iw, bh / ih)
  local w, h = iw * s, ih * s
  return bx + (bw - w) / 2, by + (bh - h) / 2, w, h
end

local function default_label(item)
  if type(item) == "table" then return item.label or item.title or item.name or item.id or "?" end
  return tostring(item)
end

local function initials(s)
  local out = ""
  for word in tostring(s):gmatch("%S+") do
    out = out .. word:sub(1, 1):upper()
    if #out >= 3 then break end
  end
  return out
end

function Grid:draw(opts)
  opts = opts or {}
  local label = opts.label or default_label
  local accent = opts.focus_color or room.theme.accent
  local plate = opts.plate or "#20242c"
  local text = opts.color or room.theme.label
  gfx.clip(self.x, self.y, self.w, self.h)
  local first = self.scroll_row * self.cols + 1
  local last = math.min(#self.items, (self.scroll_row + self.rows_visible) * self.cols)
  for i = first, last do
    local item = self.items[i]
    local cx, cy = self:cell_rect(i)
    if cx then
      local focused = (i == self.focus)
      local art_h = self.cell_h - self.label_h
      if focused then
        gfx.rect(cx - 4, cy - 4, self.cell_w + 8, self.cell_h + 8, accent)
        local m = opts.marker_color or "#ffffff"
        local t = 10
        gfx.rect(cx - 4, cy - 4, t, 3, m)
        gfx.rect(cx - 4, cy - 4, 3, t, m)
        gfx.rect(cx + self.cell_w + 4 - t, cy - 4, t, 3, m)
        gfx.rect(cx + self.cell_w + 1, cy - 4, 3, t, m)
        gfx.rect(cx - 4, cy + self.cell_h + 1, t, 3, m)
        gfx.rect(cx - 4, cy + self.cell_h + 4 - t, 3, t, m)
        gfx.rect(cx + self.cell_w + 4 - t, cy + self.cell_h + 1, t, 3, m)
        gfx.rect(cx + self.cell_w + 1, cy + self.cell_h + 4 - t, 3, t, m)
      end
      gfx.rect(cx, cy, self.cell_w, art_h, plate)
      local img = opts.cover and opts.cover(item, i)
      if img and img.ready then
        local x, y, w, h = M.fit(img, cx, cy, self.cell_w, art_h)
        gfx.image(img, x, y, w, h)
      else
        gfx.text(initials(label(item, i, focused)), cx, cy + art_h / 2 - 16, { size = 32, bold = true, align = "center", max_w = self.cell_w, color = "#7a8090" })
      end
      if not opts.hide_labels then
        gfx.text(label(item, i, focused), cx, cy + art_h + 6, { size = self.size, align = "center", max_w = self.cell_w, color = text, bold = focused })
      end
      gfx.hit(self:hit_id(i), cx, cy, self.cell_w, self.cell_h)
    end
  end
  gfx.unclip()
end

return M
