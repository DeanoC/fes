-- Installed cores on this kit. Each tile uses the manifest name verbatim.
-- A core that still needs a cartridge or firmware shows that block line
-- and is not launched. With no kit socket the screen says so and stays put.
-- Home and the lobby open this pack as a room; absence is handled here.
local Grid = require "widgets.grid"

local grid
local status = "Looking for installed cores..."
local unavailable = false

local function selected()
  if unavailable or not grid then return nil end
  return grid:selected()
end

local function publish()
  local core = selected()
  if not core then
    if unavailable then
      destination.set{ kind = "unresolved", label = "Installed cores", status = status }
    else
      destination.set{ kind = "unresolved", label = "Installed cores" }
    end
    return
  end
  destination.set{
    kind = "core",
    package_id = core.package_id,
    core_id = core.core_id,
    label = core.name,
    launchable = core.launchable,
    block = core.block or "",
  }
end

function load()
  if kit == nil or kit.cores == nil then
    unavailable = true
    status = "Installed cores are not available here."
    publish()
    return
  end
  kit.cores(function(rows, err)
    if err then
      unavailable = true
      grid = nil
      status = "Installed cores are not available here."
      publish()
      return
    end
    rows = rows or {}
    if #rows == 0 then
      unavailable = false
      grid = nil
      status = "No installed cores."
      publish()
      return
    end
    unavailable = false
    local h = math.max(160, room.height - 180)
    grid = Grid.new{
      id = "cores",
      x = 32, y = 120,
      w = room.width - 64, h = h,
      cell_w = 280, cell_h = 168, gap = 18,
      label_h = 56, size = 18,
      items = rows,
    }
    status = #rows .. " installed"
    publish()
  end)
end

function on_input(cmd)
  if grid and grid:input(cmd) then
    publish()
    return true
  end
  return false
end

function on_hover(id)
  if grid and grid:on_hover(id) then publish() end
end

function on_activate(id)
  if grid and grid:on_activate(id) then publish() end
end

local function draw_blocks()
  if not grid then return end
  for i, item in ipairs(grid.items) do
    if item and not item.launchable and item.block and item.block ~= "" then
      local x, y = grid:cell_rect(i)
      if x then
        local label_top = y + grid.cell_h - grid.label_h
        gfx.text(item.block, x, label_top + 30, {
          size = 14, align = "center", max_w = grid.cell_w, color = room.theme.status,
        })
      end
    end
  end
end

function draw()
  gfx.clear(room.theme.background)
  gfx.rect(0, 0, room.width, 96, "#141b24")
  gfx.rect(0, 96, room.width, 4, room.theme.accent)
  gfx.text("Installed cores", 32, 28, { size = 36, bold = true, color = room.theme.accent })
  gfx.text(status, 32, 68, { size = 16, color = room.theme.status })
  if grid then
    grid:draw{ label = function(item) return item.name or "" end, plate = "#243140" }
    draw_blocks()
  end
  publish()
end
