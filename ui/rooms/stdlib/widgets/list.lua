-- widgets.list: a focusable, scrolling vertical list.
--
--   local list = require("widgets.list").new{ x=, y=, w=, h=, items=, id="list" }
--   list:input(cmd)        -- up/down handled; returns true when consumed
--   list:selected()        -- item, index
--   list:on_hover(id) / list:on_activate(id)  -- pointer hit ids "<id>:<index>"
--   list:draw{ label=function(item) end, ... }
local List = {}
List.__index = List
local M = {}

function M.new(opts)
  opts = opts or {}
  local self = setmetatable({
    id = opts.id or "list",
    x = opts.x or 0, y = opts.y or 0,
    w = opts.w or 400, h = opts.h or 400,
    row_h = opts.row_h or 44,
    size = opts.size or 20,
    pad = opts.pad or 16,
    items = opts.items or {},
    focus = 1,
    scroll = 0,
    wrap = opts.wrap ~= false,
  }, List)
  self:clamp()
  return self
end

function List:rows_visible()
  return math.max(1, math.floor(self.h / self.row_h))
end

function List:clamp()
  local n = #self.items
  if n == 0 then self.focus, self.scroll = 0, 0 return end
  if self.focus < 1 then self.focus = 1 end
  if self.focus > n then self.focus = n end
  local vis = self:rows_visible()
  if self.focus - 1 < self.scroll then self.scroll = self.focus - 1 end
  if self.focus - 1 >= self.scroll + vis then self.scroll = self.focus - vis end
  if self.scroll < 0 then self.scroll = 0 end
end

function List:set_items(items)
  self.items = items or {}
  self:clamp()
end

function List:move(delta)
  local n = #self.items
  if n == 0 then return false end
  local before = self.focus
  self.focus = self.focus + delta
  if self.wrap then
    if self.focus < 1 then self.focus = n end
    if self.focus > n then self.focus = 1 end
  end
  self:clamp()
  return self.focus ~= before
end

function List:input(cmd)
  if cmd == "up" then self:move(-1) return true end
  if cmd == "down" then self:move(1) return true end
  if cmd == "filter_prev" then self:move(-self:rows_visible()) return true end
  if cmd == "filter_next" then self:move(self:rows_visible()) return true end
  return false
end

function List:selected()
  if self.focus < 1 then return nil, 0 end
  return self.items[self.focus], self.focus
end

function List:hit_id(i) return self.id .. ":" .. i end

function List:index_from_hit(id)
  local prefix = self.id .. ":"
  if id:sub(1, #prefix) ~= prefix then return nil end
  local i = tonumber(id:sub(#prefix + 1))
  if not i or i < 1 or i > #self.items then return nil end
  return i
end

function List:on_hover(id)
  local i = self:index_from_hit(id)
  if not i then return false end
  self.focus = i
  self:clamp()
  return true
end

function List:on_activate(id)
  return self:on_hover(id)
end

local function default_label(item)
  if type(item) == "table" then return item.label or item.title or item.name or item.id or "?" end
  return tostring(item)
end

function List:draw(opts)
  opts = opts or {}
  local label = opts.label or default_label
  local bg = opts.background
  if bg then gfx.rect(self.x, self.y, self.w, self.h, bg) end
  gfx.clip(self.x, self.y, self.w, self.h)
  local vis = self:rows_visible()
  local accent = opts.focus_color or room.theme.accent
  local text = opts.color or room.theme.label
  local focus_text = opts.focus_text or "#101418"
  for row = 0, vis do
    local i = self.scroll + row + 1
    local item = self.items[i]
    if not item then break end
    local y = self.y + row * self.row_h
    local focused = (i == self.focus)
    if focused then
      gfx.rect(self.x, y, self.w, self.row_h, accent)
      gfx.rect(self.x, y, 6, self.row_h, opts.marker_color or "#ffffff")
    elseif opts.row_color then
      gfx.rect(self.x, y, self.w, self.row_h, opts.row_color)
    end
    gfx.text(label(item, i, focused), self.x + self.pad, y + math.floor((self.row_h - self.size) / 2) - 2,
      { size = self.size, color = focused and focus_text or text, max_w = self.w - self.pad * 2, bold = focused })
    gfx.hit(self:hit_id(i), self.x, y, self.w, self.row_h)
  end
  gfx.unclip()
end

return M
