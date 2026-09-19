-- widgets.nodemap: an overworld-style graph of nodes joined by paths.
--
--   local map = require("widgets.nodemap").new{
--     id = "map",
--     nodes = { { id="dk", x=120, y=400, label="Donkey Kong" }, ... },
--     edges = { {"dk", "smb"}, ... },
--   }
--   map:input(cmd)      -- up/down/left/right follows edges (or nearest node in that direction)
--   map:focused()       -- node
--   map:draw{ ... }
--
-- Played and Completed are separate. node.played is FES play activity;
-- node.done is an explicit completion record only. Returning from a launch
-- is not done. draw uses played_color vs done_color so the two never share
-- one "cleared" fill.
local Map = {}
Map.__index = Map
local M = {}
local ease = require "util.ease"

function M.new(opts)
  opts = opts or {}
  local self = setmetatable({
    id = opts.id or "map",
    nodes = {},
    order = {},
    edges = opts.edges or {},
    radius = opts.radius or 18,
    focus = nil,
    ox = opts.ox or 0, oy = opts.oy or 0,
  }, Map)
  for _, node in ipairs(opts.nodes or {}) do self:add(node) end
  if opts.focus then self.focus = opts.focus end
  if not self.focus and self.order[1] then self.focus = self.order[1] end
  return self
end

function Map:add(node)
  self.nodes[node.id] = node
  self.order[#self.order + 1] = node.id
end

function Map:focused() return self.nodes[self.focus] end

function Map:set_focus(id)
  if self.nodes[id] then self.focus = id return true end
  return false
end

function Map:neighbours(id)
  local out = {}
  for _, e in ipairs(self.edges) do
    if e[1] == id then out[#out + 1] = e[2] end
    if e[2] == id then out[#out + 1] = e[1] end
  end
  return out
end

local dirs = { up = { 0, -1 }, down = { 0, 1 }, left = { -1, 0 }, right = { 1, 0 } }

-- best(from, dir, candidates) picks the candidate most aligned with dir,
-- weighting alignment over distance.
local function best(self, from, dir, candidates)
  local d = dirs[dir]
  local bestid, bestscore
  for _, id in ipairs(candidates) do
    local n = self.nodes[id]
    if n and id ~= from.id then
      local dx, dy = n.x - from.x, n.y - from.y
      local dist = math.sqrt(dx * dx + dy * dy)
      if dist > 0 then
        local dot = (dx * d[1] + dy * d[2]) / dist
        if dot > 0.3 then
          local score = dist / (dot * dot)
          if not bestscore or score < bestscore then bestid, bestscore = id, score end
        end
      end
    end
  end
  return bestid
end

function Map:move(dir)
  local from = self:focused()
  if not from then return false end
  local id = best(self, from, dir, self:neighbours(from.id))
  if not id then id = best(self, from, dir, self.order) end
  if not id then return false end
  self.focus = id
  return true
end

function Map:input(cmd)
  if dirs[cmd] then self:move(cmd) return true end
  return false
end

function Map:hit_id(id) return self.id .. ":" .. id end

function Map:node_from_hit(hit)
  local prefix = self.id .. ":"
  if hit:sub(1, #prefix) ~= prefix then return nil end
  local id = hit:sub(#prefix + 1)
  return self.nodes[id] and id or nil
end

function Map:on_hover(hit)
  local id = self:node_from_hit(hit)
  if not id then return false end
  self.focus = id
  return true
end

function Map:on_activate(hit) return self:on_hover(hit) end

-- Paths are drawn as a chain of small squares: the device has no line
-- primitive, and this keeps the op count bounded per edge.
function M.path(x1, y1, x2, y2, thickness, color, max_steps)
  local dx, dy = x2 - x1, y2 - y1
  local len = math.sqrt(dx * dx + dy * dy)
  if len == 0 then return end
  local step = thickness * 0.75
  local steps = math.min(max_steps or 40, math.max(1, math.floor(len / step)))
  for i = 0, steps do
    local t = i / steps
    gfx.rect(x1 + dx * t - thickness / 2, y1 + dy * t - thickness / 2, thickness, thickness, color)
  end
end

function Map:draw(opts)
  opts = opts or {}
  local ox, oy = self.ox, self.oy
  local edge_color = opts.edge_color or "#e8dcc0"
  local node_color = opts.node_color or "#d8443c"
  local played_color = opts.played_color or "#d4a017"
  local done_color = opts.done_color or "#3aa655"
  local focus_color = opts.focus_color or room.theme.accent
  local r = self.radius
  for _, e in ipairs(self.edges) do
    local a, b = self.nodes[e[1]], self.nodes[e[2]]
    if a and b then M.path(a.x + ox, a.y + oy, b.x + ox, b.y + oy, opts.path_width or 6, edge_color, opts.path_steps) end
  end
  local pulse = ease.pulse(room.time, 1.0)
  for _, id in ipairs(self.order) do
    local n = self.nodes[id]
    local x, y = n.x + ox, n.y + oy
    local focused = (id == self.focus)
    if focused then
      local halo = r + 6 + pulse * 4
      gfx.rect(x - halo, y - halo, halo * 2, halo * 2, focus_color)
    end
    local fill = n.color or node_color
    if not n.color then
      if n.done then
        fill = done_color
      elseif n.played then
        fill = played_color
      end
    end
    gfx.rect(x - r, y - r, r * 2, r * 2, fill)
    if n.icon and n.icon.ready then
      gfx.image(n.icon, x - r + 3, y - r + 3, r * 2 - 6, r * 2 - 6)
    end
    if n.label and (focused or not opts.labels_focused_only) then
      gfx.text(n.label, x - 120, y + r + 6, { size = opts.label_size or 18, align = "center", max_w = 240, bold = focused, color = opts.label_color or room.theme.label })
    end
    gfx.hit(self:hit_id(id), x - r - 6, y - r - 6, (r + 6) * 2, (r + 6) * 2)
  end
end

return M
