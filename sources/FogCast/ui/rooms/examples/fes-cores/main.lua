-- FES Home on a kit: the Cartridge Lounge layout over installed cores.
--
-- The shelf lists what kit.cores reports, using registered model names for
-- known core IDs, then adds the host catalog's prepared systems when
-- hardware.read provides them.
-- Those cards launch with the library, and they do not draw a keyboard. The
-- host list is still read when the kit-local listing fails.
-- FES draws the selected item's title, input needs, readiness, destination and actions
-- in the hero text column (room.toml destination_panel = "hero"); this
-- script draws the card, artwork motif, shelf and header routes.
--
-- Up reaches the hero's actions, then Library, Systems and Settings, so a pad without
-- a Guide button still reaches the system menu with Direction and Confirm.
-- The hero and a compact route row use Left and Right along that header.
-- When the compact routes stack, Down and Up follow the column and then
-- the cores, and Left and Right stay put. The list arrives once; it never
-- moves the player's focus. The last selected installed package is restored
-- on a later visit. A fresh visit, or a saved package that is missing or
-- unavailable, opens on the first installed core, or on Library when every
-- row is an omission. While kit.cores is still listing, the saved package
-- is shown as a pending card: Confirm on it is queued, and the kit starts it
-- only if the finished list still has it launchable. Selecting an omitted core does not replace the saved
-- package, and the shelf count excludes it.
--
-- Under 480×360, room.hero is nil and the launcher keeps its destination
-- strip. A compact list above that strip still offers the visible cores
-- and the three routes, with the same pointer hits and focus mark.

local C = {
  background = "#F4F0E8",
  surface = "#FFFCF5",
  raised = "#E9E6DC",
  text = "#243631",
  secondary = "#53625A",
  divider = "#CECFC0",
  accent = "#235A46",
  on_accent = "#FCFCF3",
  dark = "#22342E",
}

-- Muted motif grounds. A core's id picks one so it keeps its colour.
local GROUNDS = { "#7097A8", "#9690B5", "#E08A63", "#8DB07A", "#D9B45A", "#6E8F80" }

local MENU = {
  { id = "library", label = "Library" },
  { id = "systems", label = "Systems" },
  { id = "settings", label = "Settings" },
}

local SYSTEMS = "example.computers"
local COLECO_ARCADE = "example.coleco-arcade"

local cores = nil      -- nil while listing; a table once kit.cores answers
local remembered = nil -- the saved package as one pending card while listing
local unavailable = false
local row = "shelf"    -- "shelf", native hero "actions", or "menu"
local shelf_i = 1
local menu_i = 1
local first = 1        -- first visible shelf card
local status = "Looking for this kit's machines…"

local function layout()
  return room.hero
end

local function hash(s)
  local h = 7
  for i = 1, #(s or "") do
    h = (h * 31 + string.byte(s, i)) % 1000003
  end
  return h
end

local function has_input(core, name)
  for _, v in ipairs(core.inputs or {}) do
    if v == name then return true end
  end
  return false
end

-- Short shelf line. Readiness itself is the hero's job: a kit lease can
-- make a launchable core unavailable, so the shelf never says Ready.
local function card_line(core)
  if core.pending then return "Checking…" end
  if core.core_id == "fes.coleco" then return "Browse Coleco titles" end
  if not core.launchable then
    if core.needs == "parts" then return "Display setup incomplete" end
    if core.needs == "firmware" then return "Needs firmware" end
    if core.needs == "media" then return "Needs a cartridge" end
    return core.block or ""
  end
  local kb, mouse = has_input(core, "keyboard"), has_input(core, "mouse")
  if kb and mouse then return "Keyboard and mouse" end
  if kb then return "Keyboard" end
  if mouse then return "Mouse" end
  return ""
end

-- The shelf as drawn: the listed cores, or the pending card before then.
local function shelf_rows()
  return cores or remembered
end

local function selected_core()
  if row == "menu" then return nil end
  local list = shelf_rows()
  if not list then return nil end
  return list[shelf_i]
end

-- An omitted optional core is listed, but it is not an installed package.
local function omitted(core)
  return core and core.needs == "unavailable"
end

local function installed_count()
  local n = 0
  for _, core in ipairs(cores or {}) do
    if not omitted(core) then n = n + 1 end
  end
  return n
end

-- Open on the saved installed package, else the first installed core.
-- An omitted core stays on the shelf and is not the opening selection.
local function choose_opening()
  local want = store.get("package_id", "")
  local saved, first_installed = 0, 0
  for i, core in ipairs(cores) do
    if not omitted(core) then
      if first_installed == 0 then first_installed = i end
      if want ~= "" and core.package_id == want then saved = i end
    end
  end
  if saved ~= 0 then
    shelf_i = saved
  elseif first_installed ~= 0 then
    shelf_i = first_installed
  else
    row = "menu"
    menu_i = 1
  end
end

-- Host computers are catalog launches, not kit-local LoadCore calls.
-- A ready host row replaces a blocked local row for the same core.
-- A launchable local row stays. Keyboard inputs are not copied onto the
-- host card: the motif stays a disc so the first draw keeps its budget.
local function merge_host_computers(list, rows)
  local index = {}
  for i, core in ipairs(list) do
    if core.core_id and core.core_id ~= "" then index[core.core_id] = i end
  end
  for _, row in ipairs(rows or {}) do
    local id = row.core_id or ""
    if id ~= "" and row.game_id and row.game_id ~= "" and row.package_id and row.package_id ~= "" then
      local card = {
        package_id = row.package_id,
        core_id = id,
        name = row.title or id,
        game_id = row.game_id,
        host_launch = row.ready and true or false,
        launchable = false,
        needs = row.ready and "none" or "unsupported",
        block = row.ready and "" or (row.unavailable_reason or "Unavailable"),
        inputs = {},
      }
      local at = index[id]
      if not at then
        list[#list + 1] = card
        index[id] = #list
      elseif card.host_launch and not list[at].launchable and not list[at].host_launch then
        list[at] = card
      end
    end
  end
  return list
end

local function publish()
  if row == "menu" then
    local item = MENU[menu_i]
    if item.id == "library" then
      destination.set{ kind = "library", label = "Library" }
    elseif item.id == "systems" then
      destination.set{ kind = "room", room_id = SYSTEMS, label = "Systems" }
    else
      destination.set{ kind = "action", action = "settings", label = "Settings" }
    end
    return
  end
  local core = selected_core()
  if core and core.pending then
    -- core_id is omitted: the stored copy is only used for the card.
    destination.set{
      kind = "core",
      package_id = core.package_id,
      label = core.name,
      pending = true,
    }
  elseif core and core.core_id == "fes.coleco" then
    destination.set{ kind = "room", room_id = COLECO_ARCADE, label = core.name }
  elseif core and core.host_launch and core.game_id and core.game_id ~= "" then
    destination.set{
      kind = "game",
      label = core.name,
      system = "fpga",
      game_id = core.game_id,
      matches = {{
        id = core.game_id,
        title = core.name,
        system = "fpga",
        state = "available",
        launchable = true,
      }},
    }
  elseif core then
    destination.set{
      kind = "core",
      package_id = core.package_id,
      core_id = core.core_id,
      label = core.name,
      launchable = core.launchable,
      block = core.block or "",
      needs = core.needs or "",
      inputs = core.inputs,
    }
  elseif cores == nil and not unavailable then
    destination.set{ kind = "unresolved", label = "Home", resolving = true, status = status }
  else
    destination.set{ kind = "unresolved", label = "Home", status = status }
  end
end

local compact_layout

local function visible_cards()
  local h = layout()
  if not h then
    local rows = compact_layout().rows
    if rows < 1 then return 1 end
    return rows
  end
  return math.max(1, math.floor((h.shelf.w + 20) / 300))
end

local function keep_visible()
  local n = visible_cards()
  if shelf_i < first then first = shelf_i end
  if shelf_i > first + n - 1 then first = shelf_i - n + 1 end
  if cores and #cores > 0 then
    -- A wider shelf after a resize must not leave an empty slot at the end.
    first = math.min(first, math.max(1, #cores - n + 1))
  end
  if first < 1 then first = 1 end
end

-- The selection is saved once browsing settles: store.set writes a file,
-- and the kit's storage should not see one write per Left/Right press.
-- An omitted optional core is not a selection: remembering it would replace
-- the last installed package Home should restore.
local pending_save = nil
local SAVE_AFTER = 1.0

-- The name and core id let a later visit show this package before
-- kit.cores answers.
local function save(entry)
  store.set("package_id", entry.id)
  store.set("package_name", entry.name)
  store.set("package_core_id", entry.core_id)
end

local function remember()
  local core = selected_core()
  if core and not omitted(core) and not core.pending then
    -- Only a kit-launched core can be shown as a pending card: a host
    -- computer and the Coleco room do not start with LoadCore.
    local name = core.name or ""
    if core.host_launch or core.core_id == "fes.coleco" then name = "" end
    pending_save = { id = core.package_id, core_id = core.core_id or "", name = name, wait = SAVE_AFTER }
  end
end

function update(dt)
  if pending_save then
    pending_save.wait = pending_save.wait - dt
    if pending_save.wait <= 0 then
      save(pending_save)
      pending_save = nil
    end
  end
end

function unload()
  if pending_save then save(pending_save) end
end

-- The saved package, if its id is well formed and it has a name to show.
-- remember() saves no name for a card that is not a kit launch.
local function remembered_card()
  local id = store.get("package_id", "")
  local name = store.get("package_name", "")
  if type(id) ~= "string" or not string.match(id, "^" .. string.rep("%x", 64) .. "$") then return nil end
  if string.match(id, "%u") then return nil end
  if type(name) ~= "string" or name == "" then return nil end
  local core_id = store.get("package_core_id", "")
  if type(core_id) ~= "string" then core_id = "" end
  return { {
    package_id = id,
    core_id = core_id,
    name = name,
    pending = true,
    launchable = true,
    inputs = {},
  } }
end

function load()
  if kit == nil or kit.cores == nil then
    unavailable = true
    cores = {}
    row = "menu"
    status = "Installed machines appear here on an FES kit."
    publish()
    return
  end
  remembered = remembered_card()
  publish()
  kit.cores(function(rows, err)
    if err then
      unavailable = true
      status = "This kit's machines can't be listed right now."
      local function settle_unavailable(listed)
        cores = listed or {}
        if #cores == 0 then
          if row == "shelf" then row = "menu" end
        else
          choose_opening()
          keep_visible()
        end
        publish()
      end
      if hardware == nil or hardware.read == nil then
        settle_unavailable({})
        return
      end
      hardware.read(function(data, herr)
        local host = {}
        if not herr and data and data.computers then
          host = merge_host_computers(host, data.computers)
        end
        settle_unavailable(host)
      end)
      return
    end
    local listed = rows or {}
    local function settle(merged)
      cores = merged or {}
      if #cores == 0 then
        status = "Nothing is installed on this kit yet. The Library and Settings are above."
        if row == "shelf" then row = "menu" end
      else
        choose_opening()
        keep_visible()
      end
      publish()
    end
    if hardware == nil or hardware.read == nil then
      settle(listed)
      return
    end
    -- A missing hardware service leaves the local shelf unchanged.
    hardware.read(function(data, herr)
      if not herr and data and data.computers then
        listed = merge_host_computers(listed, data.computers)
      end
      settle(listed)
    end)
  end)
end

-- The core directly under a stacked route column. A later selection that
-- sits further down the list is not between Settings and this row.
local function focus_shelf_top()
  if not cores or #cores == 0 then return end
  keep_visible()
  if first < 1 or first > #cores then first = 1 end
  shelf_i = first
  keep_visible()
  remember()
end

local function move_stacked(dx, dy)
  if row == "menu" then
    if dy > 0 then
      if menu_i < #MENU then
        menu_i = menu_i + 1
      elseif cores == nil or #cores > 0 then
        -- The shelf stays reachable while kit.cores is still listing.
        row = "shelf"
        focus_shelf_top()
      end
      return true
    end
    if dy < 0 then
      if menu_i > 1 then menu_i = menu_i - 1 end
      return true
    end
    return dx ~= 0
  end
  if dy < 0 then
    if not cores or #cores == 0 or shelf_i <= 1 then
      row = "menu"
      menu_i = #MENU
    else
      shelf_i = shelf_i - 1
      keep_visible()
      remember()
    end
    return true
  end
  if dy > 0 then
    if cores and #cores > 0 and shelf_i < #cores then
      shelf_i = shelf_i + 1
      keep_visible()
      remember()
    end
    return true
  end
  return dx ~= 0
end

local function move(dx, dy)
  -- Stacked compact routes are a column. Hero and a single route row stay horizontal.
  if not layout() and compact_layout().stacked then
    return move_stacked(dx, dy)
  end
  if dy < 0 and row == "shelf" then
    row = "menu"
    return true
  end
  if dy > 0 and row == "menu" then
    -- The shelf is reachable while it is still being listed.
    if cores == nil or #cores > 0 then row = "shelf" end
    return true
  end
  if dx ~= 0 then
    if row == "menu" then
      menu_i = math.max(1, math.min(#MENU, menu_i + dx))
    elseif cores and #cores > 0 then
      shelf_i = math.max(1, math.min(#cores, shelf_i + dx))
      keep_visible()
      remember()
    end
    return true
  end
  return dy ~= 0
end

function on_input(cmd)
  local handled = false
  if cmd == "left" then handled = move(-1, 0)
  elseif cmd == "right" then handled = move(1, 0)
  elseif cmd == "up" then handled = move(0, -1)
  elseif cmd == "down" then handled = move(0, 1)
  end
  if handled then publish() end
  return handled
end

local function focus_hit(id)
  local kind, n = string.match(id or "", "^(%a+):(%d+)$")
  n = tonumber(n)
  if kind == "menu" and MENU[n] then
    row, menu_i = "menu", n
  elseif kind == "core" and shelf_rows() and shelf_rows()[n] then
    row, shelf_i = "shelf", n
    keep_visible()
    remember()
  else
    return false
  end
  publish()
  return true
end

function on_hover(id)
  -- Native hero action focus retains this shelf item without drawing a
  -- second focus outline. These private hits never activate a destination.
  if id == "native:actions" then row = "actions" return end
  if id == "native:shelf" then row = "shelf" return end
  focus_hit(id)
end
function on_resize() keep_visible() end
function on_activate(id) focus_hit(id) end

-- Drawing ------------------------------------------------------------

local function outline(x, y, w, h, t, color)
  gfx.rect(x, y, w, t, color)
  gfx.rect(x, y + h - t, w, t, color)
  gfx.rect(x, y, t, h, color)
  gfx.rect(x + w - t, y, t, h, color)
end

-- A disc from horizontal slices: the device has rectangles only.
local function disc(cx, cy, r, color)
  local slices = math.max(6, math.floor(r / 3))
  for i = 0, slices - 1 do
    local y0 = -r + (2 * r) * i / slices
    local y1 = -r + (2 * r) * (i + 1) / slices
    local mid = (y0 + y1) / 2
    local half = math.sqrt(math.max(0, r * r - mid * mid))
    gfx.rect(cx - half, cy + y0, half * 2, (y1 - y0) + 1, color)
  end
end

local function hills(x, y, w, h, color, phase)
  local steps = 12
  for i = 0, steps - 1 do
    local t = (i + 0.5) / steps
    local top = h * (0.30 + 0.16 * math.sin((t + phase) * math.pi * 2))
    gfx.rect(x + w * i / steps, y + top, w / steps + 1, h - top, color)
  end
end

local function keyboard(x, y, w, h, key, ground)
  local rows, cols = 4, 10
  local gap = math.max(2, math.floor(w / 80))
  local kw = (w - gap * (cols + 1)) / cols
  local kh = (h - gap * (rows + 1)) / rows
  gfx.rect(x, y, w, h, C.dark)
  for r = 0, rows - 1 do
    local inset = (r % 2) * kw / 3
    for c = 0, cols - 1 do
      local kx = x + gap + c * (kw + gap) + inset
      if kx + kw <= x + w - gap then
        gfx.rect(kx, y + gap + r * (kh + gap), kw, kh, key)
      end
    end
  end
  gfx.rect(x + w * 0.25, y + h - gap - kh, w * 0.5, kh, ground)
end

-- Fallback artwork: a recognisable motif per item, no text over art.
local function motif(core, x, y, w, h)
  local ground = GROUNDS[(hash(core.core_id or core.name) % #GROUNDS) + 1]
  gfx.rect(x, y, w, h, ground)
  if has_input(core, "keyboard") then
    disc(x + w * 0.80, y + h * 0.28, math.min(w, h) * 0.13, C.surface)
    keyboard(x + w * 0.10, y + h * 0.48, w * 0.80, h * 0.40, C.surface, ground)
  else
    disc(x + w * 0.36, y + h * 0.42, math.min(w, h) * 0.24, "#F5E6C8")
    hills(x, y + h * 0.45, w, h * 0.55, C.dark, hash(core.name or "") % 7 / 7)
  end
  if not core.launchable then
    gfx.rect(x, y + h - 10, w, 10, C.raised)
  end
end

local function menu_art(id, x, y, w, h)
  gfx.rect(x, y, w, h, C.raised)
  if id == "library" then
    local spines = { "#7097A8", "#E08A63", "#8DB07A", "#9690B5", "#D9B45A", "#6E8F80" }
    local sw = w / 12
    for i, color in ipairs(spines) do
      local sh = h * (0.45 + 0.07 * (i % 3))
      gfx.rect(x + w * 0.18 + (i - 1) * (sw + 6), y + h * 0.82 - sh, sw, sh, color)
    end
    gfx.rect(x + w * 0.12, y + h * 0.82, w * 0.76, 8, C.dark)
  elseif id == "systems" then
    for i = 0, 2 do
      local dx = x + w * (0.16 + i * 0.25)
      gfx.rect(dx, y + h * 0.25, w * 0.18, h * 0.55, i == 1 and C.accent or C.dark)
      gfx.rect(dx + w * 0.13, y + h * 0.52, w * 0.025, h * 0.04, C.surface)
    end
  else
    for i = 0, 2 do
      local ly = y + h * (0.28 + i * 0.22)
      gfx.rect(x + w * 0.15, ly, w * 0.70, 6, C.dark)
      local kx = x + w * (0.25 + 0.22 * ((i * 2) % 3))
      gfx.rect(kx, ly - 14, 28, 34, C.accent)
    end
  end
end

local function draw_header(h)
  local hd = h.header
  local m = h.card.x
  gfx.rect(0, hd.h - 2, room.width, 2, C.divider)
  local bar = math.floor(hd.h * 0.11)
  local my = math.floor((hd.h - bar * 4) / 2)
  gfx.rect(m, my, bar * 3, bar, C.accent)
  gfx.rect(m, my + bar * 1.5, bar * 3, bar, C.accent)
  gfx.rect(m, my + bar * 3, bar * 1.6, bar, C.accent)
  local _, th = gfx.measure("FES", 28, true)
  gfx.text("FES", m + bar * 3 + 12, (hd.h - th) / 2, { size = 28, bold = true, color = C.text })
  local fx = m + bar * 3 + 12 + gfx.measure("FES", 28, true) + 18
  gfx.rect(fx, hd.h * 0.3, 2, hd.h * 0.4, C.divider)
  local _, rh = gfx.measure(room.title, 22)
  gfx.text(room.title, fx + 18, (hd.h - rh) / 2, { size = 22, color = C.text })

  local bh = math.floor(hd.h * 0.62)
  local by = math.floor((hd.h - bh) / 2)
  local right = room.width - m
  for i = #MENU, 1, -1 do
    local item = MENU[i]
    local focused = row == "menu" and menu_i == i
    local tw = gfx.measure(item.label, 20, focused)
    local bw = tw + 36
    local bx = right - bw
    gfx.rect(bx, by, bw, bh, C.surface)
    if focused then
      outline(bx - 3, by - 3, bw + 6, bh + 6, 4, C.accent)
    else
      outline(bx, by, bw, bh, 1, C.divider)
    end
    local _, lh = gfx.measure(item.label, 20, focused)
    gfx.text(item.label, bx + 18, by + (bh - lh) / 2, { size = 20, bold = focused, color = C.text })
    gfx.hit("menu:" .. i, bx, by, bw, bh)
    right = bx - 12
  end
end

local function draw_hero(h)
  local c = h.card
  gfx.rect(c.x, c.y, c.w, c.h, C.surface)
  outline(c.x, c.y, c.w, c.h, 2, C.divider)
  local a = h.art
  if row == "menu" then
    menu_art(MENU[menu_i].id, a.x, a.y, a.w, a.h)
  else
    local core = selected_core()
    if core then
      motif(core, a.x, a.y, a.w, a.h)
    else
      gfx.rect(a.x, a.y, a.w, a.h, C.raised)
      hills(a.x, a.y + a.h * 0.5, a.w, a.h * 0.5, C.divider, 0.2)
    end
  end
end

local function draw_shelf(h)
  local s = h.shelf
  local label = "On this kit"
  gfx.text(label, s.x, s.y, { size = 24, bold = true, color = C.text })
  local count = ""
  local installed = installed_count()
  if installed == 1 then
    count = "1 installed"
  elseif installed > 1 then
    count = installed .. " installed"
  end
  if count ~= "" then
    gfx.text(count, s.x, s.y + 4, { size = 18, color = C.secondary, max_w = s.w, align = "right" })
  end
  local _, lh = gfx.measure(label, 24, true)
  local top = s.y + lh + 14
  local ch = s.y + s.h - top
  local list = shelf_rows()
  if not list or #list == 0 then
    gfx.rect(s.x, top, s.w, ch, C.raised)
    gfx.text(status, s.x + 24, top + ch / 2 - 12, { size = 20, color = C.text, max_w = s.w - 48 })
    return
  end
  local n = visible_cards()
  local gap = 20
  local cw = (s.w - gap * (n - 1)) / n
  local art_h = math.floor(ch * 0.56)
  for slot = 0, n - 1 do
    local i = first + slot
    local core = list[i]
    if not core then break end
    local x = s.x + slot * (cw + gap)
    local focused = row == "shelf" and shelf_i == i
    gfx.rect(x, top, cw, ch, C.surface)
    gfx.clip(x, top, cw, art_h)
    motif(core, x, top, cw, art_h)
    gfx.unclip()
    if focused then
      outline(x - 4, top - 4, cw + 8, ch + 8, 4, C.accent)
    else
      outline(x, top, cw, ch, 1, C.divider)
    end
    gfx.text(core.name or "", x + 14, top + art_h + 10, { size = 22, bold = focused, color = C.text, max_w = cw - 28 })
    local line = card_line(core)
    if line ~= "" then
      gfx.text(line, x + 14, top + art_h + 42, { size = 18, color = C.secondary, max_w = cw - 28 })
    end
    gfx.hit("core:" .. i, x, top, cw, ch)
  end
  if first > 1 then
    gfx.rect(s.x - 10, top + ch / 2 - 20, 4, 40, C.accent)
  end
  if first + n - 1 < #list then
    gfx.rect(s.x + s.w + 6, top + ch / 2 - 20, 4, 40, C.accent)
  end
end

-- roomDestGeom in ui/tenfoot/overlay_layout.go draws the compact strip
-- over this band and its hit wins, so navigation hits stay above it.
local COMPACT_MARGIN = 12
local COMPACT_MENU_H = 36
local COMPACT_ROW = 32
local COMPACT_GAP = 6

local function compact_limit()
  if room.width < 152 then return room.height end
  local y = room.height - 118 - 8
  if y < 48 then y = 48 end
  if room.height - 8 - y < 72 then return room.height end
  return y
end

local function menu_row_fits()
  local w = 0
  for i, item in ipairs(MENU) do
    w = w + gfx.measure(item.label, 18, true) + 28
    if i < #MENU then w = w + 8 end
  end
  return w <= room.width - COMPACT_MARGIN * 2
end

compact_layout = function()
  local limit = compact_limit()
  local stacked = not menu_row_fits()
  local menu_h = COMPACT_MENU_H
  if stacked then
    menu_h = #MENU * COMPACT_MENU_H + (#MENU - 1) * COMPACT_GAP
  end
  local y = 8 + menu_h + 8
  local label = false
  if y + 22 + COMPACT_ROW <= limit - 4 then
    label = true
    y = y + 22
  end
  local step = COMPACT_ROW + COMPACT_GAP
  local rows = math.floor((limit - 4 - y) / step)
  if rows < 0 then rows = 0 end
  return { limit = limit, stacked = stacked, list_y = y, rows = rows, label = label }
end

local function draw_menu_button(i, x, y, w, h)
  local item = MENU[i]
  local focused = row == "menu" and menu_i == i
  gfx.rect(x, y, w, h, C.surface)
  if focused then
    outline(x - 3, y - 3, w + 6, h + 6, 3, C.accent)
  else
    outline(x, y, w, h, 1, C.divider)
  end
  local _, lh = gfx.measure(item.label, 18, focused)
  gfx.text(item.label, x + 14, y + (h - lh) / 2, {
    size = 18, bold = focused, color = C.text, max_w = math.max(1, w - 28),
  })
  gfx.hit("menu:" .. i, x, y, w, h)
end

local function draw_compact_menu(lay)
  local m = COMPACT_MARGIN
  local bh = COMPACT_MENU_H
  local by = 8
  if lay.stacked then
    for i = 1, #MENU do
      draw_menu_button(i, m, by + (i - 1) * (bh + COMPACT_GAP), room.width - m * 2, bh)
    end
    return
  end
  local right = room.width - m
  for i = #MENU, 1, -1 do
    local focused = row == "menu" and menu_i == i
    local bw = gfx.measure(MENU[i].label, 18, focused) + 28
    right = right - bw
    draw_menu_button(i, right, by, bw, bh)
    right = right - 8
  end
  local title = room.title or "Home"
  local tw, th = gfx.measure(title, 22, true)
  if m + tw + 12 < right then
    gfx.text(title, m, by + (bh - th) / 2, { size = 22, bold = true, color = C.text })
  end
end

local function draw_compact()
  local lay = compact_layout()
  local m = COMPACT_MARGIN
  draw_compact_menu(lay)
  if lay.label then
    gfx.text("On this kit", m, lay.list_y - 22, { size = 18, bold = true, color = C.text })
  end
  local list = shelf_rows()
  if not list or #list == 0 then
    if lay.list_y + 20 <= lay.limit then
      gfx.text(status, m, lay.list_y, { size = 18, color = C.text, max_w = room.width - m * 2 })
    end
    return
  end
  local step = COMPACT_ROW + COMPACT_GAP
  local cw = room.width - m * 2
  for slot = 0, lay.rows - 1 do
    local i = first + slot
    local core = list[i]
    if not core then break end
    local y = lay.list_y + slot * step
    local focused = row == "shelf" and shelf_i == i
    gfx.rect(m, y, cw, COMPACT_ROW, C.surface)
    if focused then
      outline(m - 3, y - 2, cw + 6, COMPACT_ROW + 4, 3, C.accent)
    else
      outline(m, y, cw, COMPACT_ROW, 1, C.divider)
    end
    gfx.text(core.name or "", m + 12, y + 6, {
      size = 18, bold = focused, color = C.text, max_w = math.max(1, cw - 24),
    })
    gfx.hit("core:" .. i, m, y, cw, COMPACT_ROW)
  end
  if first > 1 then
    gfx.rect(room.width - 8, lay.list_y, 3, 16, C.accent)
  end
  if first + lay.rows - 1 < #list then
    gfx.rect(room.width - 8, lay.list_y + math.max(0, lay.rows * step - 16), 3, 16, C.accent)
  end
end

-- The list is a snapshot. Repaint only when its identity, focus or geometry
-- changes; store debounce still runs in update() while the picture is reused.
local painted = nil

function draw()
  if room.presented and painted
    and painted.cores == cores and painted.status == status
    and painted.row == row and painted.shelf_i == shelf_i
    and painted.menu_i == menu_i and painted.first == first
    and painted.width == room.width and painted.height == room.height
    and painted.title == room.title and gfx.reuse() then
    return
  end
  local h = layout()
  if h then
    draw_header(h)
    draw_hero(h)
    draw_shelf(h)
  else
    draw_compact()
  end
  publish()
  painted = {
    cores = cores, status = status, row = row, shelf_i = shelf_i,
    menu_i = menu_i, first = first, width = room.width,
    height = room.height, title = room.title,
  }
  -- Paint the background after assembling the full display list. The existing
  -- engine presents a completed paint on a soft overrun, allowing the next
  -- unchanged draw to reuse it. Hard deadlines and overrun limits still apply.
  gfx.paint({ { "c", C.background } })
end
