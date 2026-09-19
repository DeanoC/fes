-- util.ease: easing curves and a tiny tween helper for room animation.
local M = {}

function M.linear(t) return t end
function M.in_quad(t) return t * t end
function M.out_quad(t) return t * (2 - t) end
function M.in_out_quad(t)
  if t < 0.5 then return 2 * t * t end
  return -1 + (4 - 2 * t) * t
end
function M.out_cubic(t)
  t = t - 1
  return t * t * t + 1
end
function M.out_back(t)
  local c1, c3 = 1.70158, 2.70158
  t = t - 1
  return 1 + c3 * t * t * t + c1 * t * t
end
-- pulse(time, period) -> 0..1 sine pulse for focus glows.
function M.pulse(time, period)
  period = period or 1.2
  return 0.5 + 0.5 * math.sin(time * 2 * math.pi / period)
end

-- tween.new(from, to, duration, fn) ; tween:update(dt) ; tween.value
local Tween = {}
Tween.__index = Tween

function M.tween(from, to, duration, fn)
  return setmetatable({ from = from, to = to, value = from, duration = duration or 0.25, t = 0, fn = fn or M.out_cubic, done = false }, Tween)
end

function Tween:retarget(to, from)
  self.from = from or self.value
  self.to = to
  self.t = 0
  self.done = false
end

function Tween:update(dt)
  if self.done then return self.value end
  self.t = self.t + dt
  local k = self.t / self.duration
  if k >= 1 then
    k = 1
    self.done = true
  end
  self.value = self.from + (self.to - self.from) * self.fn(k)
  return self.value
end

return M
