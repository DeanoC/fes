-- util.color: small helpers for #hex colours.
local M = {}

local function clamp(v)
  if v < 0 then return 0 end
  if v > 255 then return 255 end
  return math.floor(v + 0.5)
end

-- parse("#rrggbb[aa]") -> r, g, b, a
function M.parse(hex)
  hex = hex:gsub("^#", "")
  if #hex == 3 then
    hex = hex:sub(1, 1):rep(2) .. hex:sub(2, 2):rep(2) .. hex:sub(3, 3):rep(2)
  end
  local r = tonumber(hex:sub(1, 2), 16) or 0
  local g = tonumber(hex:sub(3, 4), 16) or 0
  local b = tonumber(hex:sub(5, 6), 16) or 0
  local a = #hex >= 8 and (tonumber(hex:sub(7, 8), 16) or 255) or 255
  return r, g, b, a
end

function M.rgba(r, g, b, a)
  return { clamp(r), clamp(g), clamp(b), clamp(a or 255) }
end

-- with_alpha("#rrggbb", 0..255) -> {r,g,b,a}
function M.with_alpha(hex, a)
  local r, g, b = M.parse(hex)
  return M.rgba(r, g, b, a)
end

-- mix(a, b, t) linear blend of two hex colours.
function M.mix(ha, hb, t)
  local r1, g1, b1, a1 = M.parse(ha)
  local r2, g2, b2, a2 = M.parse(hb)
  return M.rgba(r1 + (r2 - r1) * t, g1 + (g2 - g1) * t, b1 + (b2 - b1) * t, a1 + (a2 - a1) * t)
end

-- shade(hex, k) darkens (k<1) or lightens (k>1).
function M.shade(hex, k)
  local r, g, b, a = M.parse(hex)
  return M.rgba(r * k, g * k, b * k, a)
end

return M
