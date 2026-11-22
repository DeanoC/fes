-- The core screen: one computer family's model, drives and games
-- (docs/ux/references/core-screens). It uses the ordinary library launch,
-- setup and media paths; only navigation lives in room storage.
return function()
local systems = require "systems"
local families = require "families"
local selected_core = room.initial_core_id or ""
local function spec()
  return systems.by_id[selected_core] or {
    label = "Unsupported system", mark = selected_core, media = "Media",
  }
end
local function family()
  return systems.family_by_core[selected_core] or {
    id = selected_core, label = "Unsupported system", variants = {{id = selected_core}},
  }
end
local function model_name(row) return row.model or row.mark or row.id end
-- The launcher shows a family's firmware card: once on its first opening,
-- and again from the Firmware button.
local function firmware_info(first_use)
  if not systems.by_id[selected_core] then return end
  local ids = {}
  for _, variant in ipairs(family().variants) do ids[#ids + 1] = variant.id end
  hardware.firmware_info({core_id=selected_core, family_id=family().id, family_label=family().label, core_ids=ids, first_use=first_use})
end
local function rgb(r, g, b, a)
  if a then return {r = r, g = g, b = b, a = a} end
  return {r = r, g = g, b = b}
end
local LIGHT = {
  paper = rgb(244, 240, 232), panel = rgb(255, 252, 245),
  raised = rgb(233, 230, 220), muted = rgb(83, 98, 90),
  ink = rgb(36, 54, 49), line = rgb(206, 207, 192),
  accent = rgb(35, 90, 70), light = rgb(252, 252, 243),
  warm = rgb(246, 235, 203), warm_bar = rgb(217, 180, 92),
}
local DARK = {
  paper = {r = 20, g = 25, b = 24, a = 200}, panel = rgb(32, 39, 37),
  raised = rgb(48, 57, 54), muted = rgb(190, 204, 194),
  ink = rgb(237, 243, 233), line = rgb(74, 86, 80),
  accent = rgb(200, 227, 172), light = rgb(32, 55, 39),
  warm = rgb(48, 57, 54), warm_bar = rgb(217, 180, 92),
}
local C, overlay = LIGHT, false
local function appearance()
  overlay = room.overlay and true or false
  C = overlay and DARK or LIGHT
end
local function same_appearance()
  return (room.overlay and true or false) == overlay
end

-- Layout, in a 1120 by 630 picture scaled to the room.
local LEFT_X, LEFT_W = 24, 344
local RIGHT_X, RIGHT_W = 388, 708
local TOP_Y, BOTTOM_Y = 112, 618
local ROW_H, ROW_GAP = 44, 6

local games, all_entries = {}, {}
local game_i, scroll, scroll_pending = 1, 0, true
local focus = "game"
local right_focus = "game"
local live
local computers = {}
local machine_media = {}
-- packages[package_id][role]: true when that media role keeps what the
-- computer writes, false when it only reads. Missing means not known.
local packages = {}
-- start_disks[package_id] is the disk a title with none starts from.
local start_disks = {}
local hardware_unavailable = false
local rev = 0
local last_hw = 0
local hw_busy = false
local hw_refresh_pending = false
local observed_session = session.state()
local library_generation = 0
local library_error = ""
local library_loaded = false
local library_more = false
-- The picture is kept in three parts, each a list of paint rows with its
-- controls: the header and model row, the computer panel, and the drive and
-- games. A part is rebuilt only when what it shows changed, and one draw
-- rebuilds at most one part, so no one callback builds the whole screen (#1770: a kit starting a core leaves little time for a callback).
local pool, sink, control_sink = {}, nil, nil
local head = {rows = {}, controls = {}, sig = nil}
local upper = {rows = {}, controls = {}, sig = nil}
local lower = {rows = {}, controls = {}, sig = nil}
-- The games list is its own part: the drives above it and the list are each
-- a large build, and one draw builds at most one part.
local listing = {rows = {}, controls = {}, sig = nil}
-- Where the drives part ended; the games part starts there.
local games_y = TOP_Y
local opening = {rows = {}, controls = {}}
local cmds = {}
local heights = {}
local scale, ox, oy = 1, 0, 0
local boot = 0
local held_w, held_h, held_rev, held_overlay, held_session = -1, -1, -1, false, ""
local visible_games = 1

local nav = store.get("navigation", {})
local initial_game_id = room.initial_game_id or ""
local family_variants = type(nav.family_variants) == "table" and nav.family_variants or {}
local core_choices = type(nav.core_choices) == "table" and nav.core_choices or {}
if selected_core == "" and systems.by_id[nav.core_id or ""] then selected_core = nav.core_id end
if selected_core == "" then
  local first = systems.families[1]
  selected_core = first and first.variants[1].id or ""
end
-- A family tile opens the family, not one model: return to the model last
-- browsed there unless a game was asked for.
if initial_game_id == "" then
  local last = family_variants[family().id]
  if last and systems.family_by_core[last] == systems.family_by_core[selected_core] then selected_core = last end
end
local chosen = initial_game_id ~= "" and initial_game_id or (core_choices[selected_core] or {}).game_id or ""
local message = ""

local function take()
  local n = #pool
  if n == 0 then return {} end
  local row = pool[n]
  pool[n] = nil
  return row
end
-- begin starts rebuilding one part, returning its old rows to the pool.
local function begin(part)
  local rows = part.rows
  for i = #rows, 1, -1 do
    pool[#pool + 1] = rows[i]
    rows[i] = nil
  end
  part.controls = {}
  sink, control_sink = rows, part.controls
end
local function push(row)
  sink[#sink + 1] = row
end
local function clear_row(c)
  local row = take()
  row[1], row[2] = "c", c
  row[3], row[4], row[5], row[6], row[7], row[8], row[9] = nil, nil, nil, nil, nil, nil, nil
  push(row)
end
local function rect(x, y, w, h, c)
  local row = take()
  row[1], row[2], row[3], row[4], row[5], row[6] = "r", ox + x * scale, oy + y * scale, w * scale, h * scale, c
  row[7], row[8], row[9] = nil, nil, nil
  push(row)
end
local function px(size) return math.max(12, math.floor(size * scale)) end
local function text(s, x, y, size, c, w, bold, align)
  if not s or s == "" then return end
  local row = take()
  row[1], row[2], row[3], row[4] = "t", s, ox + x * scale, oy + y * scale
  row[5], row[6], row[7], row[8], row[9] = px(size), c or C.ink, (w or 0) * scale, bold or false, align or "left"
  push(row)
end
local function hit(id, x, y, w, h)
  local row = take()
  row[1], row[2], row[3], row[4], row[5], row[6] = "h", id, ox + x * scale, oy + y * scale, w * scale, h * scale
  row[7], row[8], row[9] = nil, nil, nil
  push(row)
end
-- Text height in picture units for a picture-unit size.
local function text_height(size, bold)
  local p = px(size)
  local key = bold and -p or p
  local h = heights[key]
  if not h then
    local _, measured = gfx.measure("Ag", p, bold)
    h = measured
    heights[key] = h
  end
  return h / scale
end
-- families.measure remembers results, so a repaint does not measure again.
local function text_width(s, size, bold)
  local w = families.measure(s, px(size), bold)
  return w / scale
end
local function outline(x, y, w, h, t, c)
  rect(x, y, w, t, c)
  rect(x, y + h - t, w, t, c)
  rect(x, y, t, h, c)
  rect(x + w - t, y, t, h, c)
end
local function control(id, x, y, w, h, enabled, fn)
  hit(id, x, y, w, h)
  control_sink[#control_sink + 1] = {id = id, enabled = enabled and true or false, fn = fn}
end
local function find_control(id)
  for _, c in ipairs(head.controls) do if c.id == id then return c end end
  for _, c in ipairs(upper.controls) do if c.id == id then return c end end
  for _, c in ipairs(lower.controls) do if c.id == id then return c end end
  for _, c in ipairs(listing.controls) do if c.id == id then return c end end
  return nil
end
local function has_control(id) return find_control(id) ~= nil end
-- filled marks the screen's main action; focus is an outline outside it.
local function button(id, label, x, y, w, h, enabled, fn, filled)
  local on = enabled and true or false
  if id == focus then outline(x - 4, y - 4, w + 8, h + 8, 3, C.accent) end
  rect(x, y, w, h, C.line)
  rect(x + 1, y + 1, w - 2, h - 2, (filled and on) and C.accent or (on and C.panel or C.raised))
  local size = math.min(19, h - 14)
  local bold = on
  local th = text_height(size, bold)
  local color = (filled and on) and C.light or (on and C.ink or C.muted)
  text(label, x + 8, y + (h - th) / 2, size, color, w - 16, bold, "center")
  control(id, x, y, w, h, on, fn)
end
local function chip(label, x, y, filled)
  local size = 15
  local tw, th = text_width(label, size, true), text_height(size, true)
  local w, h = tw + 18 + (filled and 0 or 5), th + 6
  rect(x, y, w, h, filled and C.accent or C.raised)
  if not filled then rect(x, y, 5, h, C.ink) end
  text(label, x + 9 + (filled and 0 or 5), y + 3, size, filled and C.light or C.ink, 0, true)
  return w
end
-- Wrapped lines are kept per text, size, width and scale: the fixed facts
-- wrap the same way on every repaint.
local wrapped, wrapped_scale = {}, 0
local function wrap(s, size, w)
  if wrapped_scale ~= scale then wrapped, wrapped_scale = {}, scale end
  local key = size .. ":" .. w .. ":" .. s
  if wrapped[key] then return wrapped[key] end
  local lines, cur = {}, ""
  for word in string.gmatch(s, "%S+") do
    local try = cur == "" and word or (cur .. " " .. word)
    if cur ~= "" and text_width(try, size, false) > w then
      lines[#lines + 1] = cur
      cur = word
    else
      cur = try
    end
  end
  if cur ~= "" then lines[#lines + 1] = cur end
  wrapped[key] = lines
  return lines
end

local function is_system(g)
  return type(g) == "table" and systems.family_by_core[g.core_id or ""] == family() and g.id and g.id ~= ""
end
local function title_of(g)
  local line = g.title or g.id
  if g.region and g.region ~= "" then line = line .. " (" .. g.region .. ")" end
  return line
end
local function media_word(g)
  if g.media_role == "disk" then return "Disk" end
  if g.media_role == "cassette" then return spec().disk and "Tape" or (spec().media:match("^%a+") or "Tape") end
  return ""
end
-- A disk set: disk 1 is the row's own medium, then more_media in order.
local function set_size(g)
  if not g or not g.media_id or g.media_id == "" then return 0 end
  return 1 + #(g.more_media or {})
end
local function set_disk(g, k)
  if k == 1 then return g.media_id, g.media_name or "" end
  local m = (g.more_media or {})[k - 1]
  if m then return m.media_id, m.media_name end
  return nil, nil
end
-- Which disk of the set is in the running machine's drive: the one it
-- started with until a change names another. Nil when it is not one of them.
local function disk_in_drive(g)
  local name = live and live.media_name or ""
  if name == "" then return 1 end
  for k = 1, set_size(g) do
    local _, n = set_disk(g, k)
    if n ~= "" and n == name then return k end
  end
  return nil
end
local function game_sub(g)
  if g.core_entry_kind == "machine" then return "The computer on its own" end
  local n = set_size(g)
  if n > 1 then return n .. " " .. string.lower(media_word(g)) .. "s" end
  return media_word(g)
end
local function focused()
  return games[game_i]
end
local function remember()
  if not systems.by_id[selected_core] then return end
  local game = focused()
  local game_id = game and game.id or chosen
  if game_id ~= "" then chosen = game_id end
  family_variants[family().id] = selected_core
  core_choices[selected_core] = {game_id = game_id}
  store.set("navigation", {
    family_variants = family_variants, core_choices = core_choices,
    core_id = selected_core,
  })
end
local function keep_visible()
  if game_i <= scroll then scroll = game_i - 1 end
  if game_i > scroll + visible_games then scroll = game_i - visible_games end
  if scroll > #games - visible_games then scroll = #games - visible_games end
  if scroll < 0 then scroll = 0 end
end
-- Each edition of a title is its own row, so every row starts one exact
-- library entry.
local function rows_for(core_id)
  local out, seen = {}, {}
  local function add(g)
    if type(g) ~= "table" or g.core_id ~= core_id or not g.id or g.id == "" or seen[g.id] then return end
    seen[g.id] = true
    out[#out + 1] = g
  end
  -- The computer on its own leads the list; games follow in library order.
  for pass = 1, 2 do
    for _, entry in ipairs(all_entries) do
      if (entry.core_entry_kind == "machine") == (pass == 1) then
        add(entry)
        for _, v in ipairs(entry.variants or {}) do
          v.core_entry_kind = entry.core_entry_kind
          add(v)
        end
      end
    end
  end
  return out
end
local function title_count()
  local n = 0
  for _, g in ipairs(games) do
    if g.core_entry_kind ~= "machine" then n = n + 1 end
  end
  return n
end
local function place(list)
  all_entries = list or {}
  local asked_for_game = false
  if initial_game_id ~= "" then
    local asked
    for _, entry in ipairs(all_entries) do
      if entry.id == initial_game_id then asked = entry end
      for _, v in ipairs(entry.variants or {}) do
        if v.id == initial_game_id then asked = v end
      end
      if asked then break end
    end
    if asked and asked.core_entry_kind == "machine" then
      -- A family tile names the family's machine entry, not a game: return
      -- to the model and game last browsed there.
      local last = family_variants[family().id]
      if last and systems.family_by_core[last] == family() then selected_core = last end
      chosen = (core_choices[selected_core] or {}).game_id or ""
    elseif asked then
      selected_core = asked.core_id
      asked_for_game = true
    else
      asked_for_game = true
    end
  end
  games = rows_for(selected_core)
  -- A row's second line is fixed for a listing: work it out once here, not
  -- on every draw.
  for _, g in ipairs(games) do g.sub_line = game_sub(g) end
  game_i, scroll = 1, 0
  local selected = false
  for i, g in ipairs(games) do
    if g.id == chosen then game_i, selected = i, true end
  end
  if not selected then
    for i, g in ipairs(games) do
      if g.core_entry_kind ~= "machine" then game_i = i break end
    end
  end
  if library_error == "" and asked_for_game and not selected then
    message = "That " .. spec().label .. " game is no longer in your library."
  end
  initial_game_id = ""
  if #games == 0 and (focus == "game" or focus == "drive" or focus == "media") then focus = "start" end
  -- The list's height is known only when it is drawn; it places the window.
  scroll_pending = true
  rev = rev + 1
  -- selected_core is final here, including for an opening by game.
  firmware_info(true)
end
local function read_hardware()
  if hw_busy then
    hw_refresh_pending = true
    return
  end
  hw_busy = true
  hardware.read(function(value, err)
    hw_busy = false
    if hw_refresh_pending then
      hw_refresh_pending = false
      read_hardware()
      return
    end
    computers, machine_media, packages, start_disks = {}, {}, {}, {}
    if value and not err then
      live = value.session
      hardware_unavailable = false
      packages = value.packages or {}
      start_disks = value.start_disks or {}
      for _, c in ipairs(value.computers or {}) do computers[c.core_id] = c end
      for _, m in ipairs(value.machines or {}) do
        if m.game_id ~= "" and m.media_name ~= "" then machine_media[m.game_id] = m.media_name end
      end
    else
      live = nil
      hardware_unavailable = true
    end
    rev = rev + 1
  end)
end
local function play_state()
  if live and live.state and live.state ~= "" then return live.state end
  local s = session.state()
  if s and s ~= "" then return s end
  return ""
end
-- One scan per paint: the panel, its buttons and the right column all ask.
local painting, painted_running = false, nil
local function find_running()
  if not live or live.state ~= "active" then return nil end
  for _, g in ipairs(games) do
    if g.id == live.game_id then return g end
  end
  return nil
end
local function running_game()
  if painting then return painted_running end
  return find_running()
end
local function this_machine()
  local game = focused()
  return live and game and live.state == "active" and live.game_id == game.id
end
-- What the computer's panel says about starting: a chip and one line.
local function readiness()
  if hardware_unavailable then return "Unavailable", "Can't check this computer right now." end
  if running_game() then return "Running", "Running on this TV." end
  local c = computers[selected_core]
  if c and not c.ready then
    local why = c.unavailable_reason or ""
    return "Unavailable", why ~= "" and why or "This computer can't start yet."
  end
  local game = focused()
  if game and game.firmware_required and not game.firmware_ready then
    return "Unavailable", "Needs its firmware files before it can start."
  end
  if c or game then return "Ready", "" end
  return "Unavailable", "Not set up on this kit yet."
end

local function choose_core(core_id)
  if core_id == selected_core or not systems.by_id[core_id] then return end
  remember()
  selected_core = core_id
  chosen = (core_choices[core_id] or {}).game_id or ""
  message = library_error
  local keep = focus
  place(all_entries)
  focus = keep
  remember()
end
local function step_model(d)
  local variants = family().variants
  for i, variant in ipairs(variants) do
    if variant.id == selected_core then
      local to = variants[i + d]
      if to then choose_core(to.id) end
      return
    end
  end
end
local function launch(game)
  if not game then return end
  if hardware_unavailable then
    message = "Can't check this computer right now."
  elseif live and live.state == "active" and live.game_id == game.id then
    session.resume()
  elseif play_state() ~= "idle" then
    message = "Stop the running game before starting another."
  elseif not game.launchable then
    message = "This game can't start yet."
  else
    session.launch(game.id)
  end
  rev = rev + 1
end
-- The shelf is per family; any of its entries names the installed package.
local function shelf_package()
  local game = focused() or games[1]
  return game and game.package_id or ""
end
-- Whether role on pkg keeps what the computer writes: true, false or nil
-- when not known. With no role, a package with one media role answers for it.
local function media_saves(pkg, role)
  local roles = pkg and packages[pkg]
  if not roles then return nil end
  if role and role ~= "" then return roles[role] end
  local only, n = nil, 0
  for _, saves in pairs(roles) do only, n = saves, n + 1 end
  if n == 1 then return only end
  return nil
end
local function open_disks()
  local pkg = shelf_package()
  if pkg == "" then return end
  hardware.open_disks({core_id=selected_core, package_id=pkg})
end
-- Next disk puts the following disk of the set in the running machine's
-- drive, after the last one the first again.
local function next_disk()
  local game = focused()
  local n = set_size(game)
  if not live or n < 2 then return end
  local k = disk_in_drive(game) or 0
  local id, name = set_disk(game, k % n + 1)
  if not id or name == "" then
    local word = string.lower(media_word(game))
    message = "This " .. word .. " can't be put in from here. Choose it with Change the " .. word .. "."
    return
  end
  session.change_disk(live, id, name)
  -- The drive row follows the kit: read it again shortly.
  last_hw = room.time - 12
end
local function add_disk()
  local game = focused()
  if not game then return end
  hardware.add_disk({game_id=game.id, core_id=game.core_id, package_id=game.package_id, expected_media_id=game.media_id or ""})
end
local function open_media()
  local game = focused()
  if not game then return end
  if this_machine() then
    session.open_tape(live)
    return
  end
  hardware.open_media({game_id=game.id, core_id=game.core_id, package_id=game.package_id, expected_media_id=game.media_id or ""})
end

local function paint_opening()
  begin(opening)
  appearance()
  clear_row(C.paper)
  rect(24, 18, 18, 5, C.accent)
  text("FES", 50, 14, 24, C.ink, 80, true)
  text(family().label, 118, 17, 19, C.ink, 400)
  text("Opening the " .. family().label .. "…", 24, 84, 22, C.muted, 700)
  gfx.paint(opening.rows)
end

local function header()
  rect(24, 18, 18, 5, C.accent)
  text("FES", 50, 14, 24, C.ink, 80, true)
  rect(104, 16, 2, 26, C.line)
  text("Computers", 118, 15, 22, C.ink, 360)
  button("back", "Back", 860, 10, 110, 38, true, function() rooms.back() end)
  button("home", "Home", 984, 10, 110, 38, true, function() rooms.home() end)
  rect(0, 56, 1120, 2, C.line)
end

local function models()
  local variants = family().variants
  local tw = text_width(family().label, 30, true)
  text(family().label, 24, 66, 30, C.ink, 700, true)
  if #variants < 2 then return end
  local x, y = 24 + tw + 28, 70
  text("Model", x, y + 6, 15, C.muted, 80)
  x = x + text_width("Model", 15, false) + 12
  for _, variant in ipairs(variants) do
    local on = variant.id == selected_core
    local name = model_name(variant)
    local w, h = text_width(name, 16, on) + 24, 30
    rect(x, y, w, h, on and C.panel or C.raised)
    if on then
      outline(x, y, w, h, 2, C.line)
      rect(x, y + h - 4, w, 4, C.accent)
      if focus == "model" then outline(x - 4, y - 4, w + 8, h + 8, 3, C.accent) end
    end
    text(name, x + 12, y + (h - text_height(16, on)) / 2, 16, on and C.ink or C.muted, 0, on)
    local id = variant.id
    hit("model:" .. id, x, y, w, h)
    x = x + w + 8
  end
  control_sink[#control_sink + 1] = {id = "model", enabled = true, fn = function() end}
end

-- Buttons stack up from the bottom of the computer panel: the main action
-- last, so it is always in the same place.
local function left_buttons()
  local game = focused()
  local state = play_state()
  local known = systems.by_id[selected_core] ~= nil
  local idle = state == "idle" and not hardware_unavailable and known
  local out = {}
  if family().id == "zx81" then
    -- The workshop reads the machine itself, so it opens in any state.
    out[#out + 1] = {id = "expansions", label = "Expansions", enabled = known, fn = function()
      local at = focused()
      rooms.open("example.hardware", {game_id = at and at.id or "", core_id = selected_core})
    end}
  end
  out[#out + 1] = {id = "firmware", label = "Firmware", enabled = known, fn = function() firmware_info(false) end}
  local name = model_name(spec())
  if running_game() then
    out[#out + 1] = {id = "stop", label = "Stop the " .. name, enabled = true, fn = function() if live then session.stop(live) end end}
    out[#out + 1] = {id = "start", label = "Return to play", enabled = true, filled = true, fn = function() session.resume() end}
  elseif library_more and #games == 0 then
    out[#out + 1] = {id = "start", label = "List incomplete", enabled = false}
  elseif #games == 0 then
    out[#out + 1] = {id = "start", label = "Set up the " .. name, enabled = idle and library_loaded, filled = true, fn = function()
      hardware.setup({core_id = selected_core})
    end}
  elseif hardware_unavailable then
    out[#out + 1] = {id = "start", label = "Status unavailable", enabled = false}
  elseif state == "active" then
    out[#out + 1] = {id = "resume", label = "Return to play", enabled = true, fn = function() session.resume() end}
    out[#out + 1] = {id = "start", label = "Another game is running", enabled = false}
  else
    out[#out + 1] = {id = "start", label = "Start", enabled = game and game.launchable and idle, filled = true, fn = function() launch(focused()) end}
  end
  return out
end

local function computer_panel()
  local x, y, w, h = LEFT_X, TOP_Y, LEFT_W, BOTTOM_Y - TOP_Y
  rect(x, y, w, h, C.line)
  rect(x + 2, y + 2, w - 4, h - 4, C.panel)
  local pad = 16
  local ix, iw = x + pad, w - 2 * pad
  local yy = y + pad
  if not overlay then
    rect(ix, yy, 84, 64, families.ground(selected_core))
    text(spec().mark, ix, yy + 20, 20, C.light, 84, true, "center")
  end
  local tx = overlay and ix or ix + 98
  text(spec().label, tx, yy, 21, C.ink, x + w - pad - tx, true)
  local state, line = readiness()
  chip(state, tx, yy + text_height(21, true) + 6, state ~= "Unavailable")
  yy = yy + 76
  if line ~= "" then
    for _, l in ipairs(wrap(line, 16, iw)) do
      text(l, ix, yy, 16, C.ink, iw, true)
      yy = yy + text_height(16, true) + 2
    end
    yy = yy + 4
  end
  rect(ix, yy, iw, 2, C.line)
  yy = yy + 10
  local buttons = left_buttons()
  local bh, gap = 42, 10
  local top = y + h - pad - #buttons * bh - (#buttons - 1) * gap
  local label_w = 92
  for _, fact in ipairs(spec().facts or {}) do
    local lines = wrap(fact[2], 16, iw - label_w)
    local fh = #lines * (text_height(16, false) + 2) + 5
    if yy + fh > top - 8 then break end
    text(fact[1], ix, yy, 16, C.muted, label_w)
    for i, l in ipairs(lines) do
      text(l, ix + label_w, yy + (i - 1) * (text_height(16, false) + 2), 16, C.ink, iw - label_w)
    end
    yy = yy + fh
  end
  for i, b in ipairs(buttons) do
    button(b.id, b.label, ix, top + (i - 1) * (bh + gap), iw, bh, b.enabled, b.fn or function() end, b.filled)
  end
end

local function note(head, x, y, w)
  local h = text_height(16, true) + 18
  rect(x, y, w, h, C.warm)
  rect(x, y, 6, h, C.warm_bar)
  text(head, x + 18, y + 9, 16, C.ink, w - 30, true)
  return y + h
end

local function list_row(id, x, y, w, lead, lead_w, name, sub, tag, tag_filled, marked, off)
  rect(x, y, w, ROW_H, C.line)
  rect(x + 2, y + 2, w - 4, ROW_H - 4, off and C.raised or C.panel)
  if marked then rect(x, y, 7, ROW_H, C.accent) end
  if id == focus or (id == "game:" .. game_i and focus == "game") then
    outline(x - 4, y - 4, w + 8, ROW_H + 8, 3, C.accent)
  end
  local tx, right = x + 16 + (marked and 5 or 0), x + w - 14
  if tag and tag ~= "" then
    local tw = text_width(tag, 15, true) + 18 + (tag_filled and 0 or 5)
    chip(tag, right - tw, y + (ROW_H - text_height(15, true) - 6) / 2, tag_filled)
    right = right - tw - 12
  end
  local ty = y + (ROW_H - text_height(19, true)) / 2
  if lead then
    text(lead, tx, ty, 19, C.ink, lead_w - 8, true)
    tx = tx + lead_w
  end
  local color = off and C.muted or C.ink
  text(name, tx, ty, 19, color, right - tx, false)
  if sub and sub ~= "" then
    local nw = text_width(name, 19, false)
    if tx + nw + 24 < right then
      text(sub, tx + nw + 18, ty + 2, 16, C.muted, right - tx - nw - 18)
    end
  end
end

-- A title with no disk of its own may still start from one: the PC/XT's
-- bundled FreeDOS disk. It saves changes like any disk in that drive.
local function start_disk(game)
  if not game or (game.media_id and game.media_id ~= "") then return nil end
  return start_disks[game.package_id or ""]
end
local function drive_name(game)
  if start_disk(game) then return start_disk(game) end
  if not game or not game.media_id or game.media_id == "" then return nil end
  if game.core_entry_kind ~= "machine" then return title_of(game) end
  return machine_media[game.id] or "Saved " .. string.lower(media_word(game) ~= "" and media_word(game) or "media")
end

local function right_column()
  local x, w = RIGHT_X, RIGHT_W
  local y = TOP_Y
  local game = focused()
  local running = running_game()
  local live_here = this_machine()
  if running then
    y = note((running.title or "A game") .. " is running. Stopping it clears its memory.", x, y, w) + 10
  elseif message ~= "" then
    y = note(message, x, y, w) + 10
  end
  if library_more then
    y = note("More than 2,000 family entries; some games may be hidden.", x, y, w) + 10
  end
  text(live_here and "In the drive now" or "Drives", x, y, 19, C.ink, w, true)
  text(spec().media, x, y + 2, 16, C.muted, w, false, "right")
  y = y + text_height(19, true) + 6
  local role = game and game.media_role or ""
  if start_disk(game) then role = "disk" end
  local lead = role == "disk" and (spec().drive or "Drive") or (role == "cassette" and "Cassette")
    or (spec().disk and "Media" or spec().drive or "Cassette")
  local lead_w = text_width(lead, 19, true) + 18
  local name = drive_name(game)
  local can_change = game and game.package_id and game.package_id ~= "" and
    ((live_here and live.removable_available) or (play_state() == "idle" and not hardware_unavailable))
  -- Only a medium in the drive is tagged; an empty drive has nothing to save.
  local saves = name and media_saves(game and game.package_id, role)
  local saves_tag = saves == true and "Saves changes" or (saves == false and "Read only" or nil)
  local sub = name and (live_here and "" or "For next start") or ""
  local n = set_size(game)
  if name and n > 1 then
    local k = live_here and disk_in_drive(game) or 1
    local which = k and (media_word(game) .. " " .. k .. " of " .. n) or ""
    sub = live_here and which or (which .. ", for next start")
  end
  list_row("drive", x, y, w, lead, lead_w, name or "Nothing chosen", sub, saves_tag, saves == true, false, not name)
  control("drive", x, y, w, ROW_H, can_change, open_media)
  y = y + ROW_H + 8
  local word = string.lower(media_word(game or {}) ~= "" and media_word(game) or (spec().media:match("^%a+") or "media"))
  local label = (live_here and "Change the " or "Choose a ") .. word
  -- While a set's game runs, its next disk is the first disk button.
  if live_here and n > 1 then
    local next_label = "Next " .. word
    local next_w = text_width(next_label, 17, true) + 40
    button("next_disk", next_label, x, y, next_w, 36, live.removable_available, next_disk, true)
    x = x + next_w + 12
  end
  local media_w = text_width(label, 17, true) + 40
  button("media", label, x, y, media_w, 36, can_change, open_media)
  -- A drive that cannot write gets no blank disk: New disk is shown off,
  -- with the reason below.
  local bx = x + media_w + 12
  local reads_only = media_saves(shelf_package(), "disk") == false
  if reads_only then
    local bw = text_width("New disk", 17, true) + 40
    button("new_disk", "New disk", bx, y, bw, 36, false, nil)
    bx = bx + bw + 12
  elseif spec().shelf then
    local bw = text_width("Your disks", 17, true) + 40
    button("disks", "Your disks", bx, y, bw, 36,
      shelf_package() ~= "" and not hardware_unavailable, open_disks)
    bx = bx + bw + 12
  end
  -- A stopped game with a disk or tape can take another after its last. A
  -- built-in start disk is not the game's own, so there is nothing to follow.
  if not live_here and (role == "disk" or role == "cassette") and name and n < 16 and not start_disk(game) then
    local add_label = "Add " .. word .. " to game"
    button("add_disk", add_label, bx, y, text_width(add_label, 17, true) + 40, 36, can_change, add_disk)
  end
  y = y + 36 + 14
  x = RIGHT_X
  if reads_only then
    y = note("This " .. spec().label .. " can read disks but can't write to them yet.", x, y, w) + 14
  end
  games_y = y
end

local function games_list()
  local x, w, y = RIGHT_X, RIGHT_W, games_y
  local titles = title_count()
  local count = titles == 1 and "1 in your library" or (titles .. " in your library")
  if library_more then
    count = titles == 0 and "Count incomplete" or ("At least " .. titles .. " in your library")
  end
  text("Games for the " .. model_name(spec()), x, y, 19, C.ink, w, true)
  text(library_loaded and count or "Reading your library…", x, y + 2, 16, C.muted, w, false, "right")
  y = y + text_height(19, true) + 6
  visible_games = math.max(1, math.floor((BOTTOM_Y - y + ROW_GAP) / (ROW_H + ROW_GAP)))
  if scroll_pending then scroll, scroll_pending = 0, false end
  keep_visible()
  if #games == 0 then
    local line = library_error ~= "" and library_error or
      (library_loaded and (library_more and "The library list is incomplete; this model may have more games." or
        ("No " .. model_name(spec()) .. " games yet. Set it up to add one.")) or "")
    text(line, x, y + 8, 17, C.muted, w)
    return
  end
  for n = 1, visible_games do
    local i = scroll + n
    local g = games[i]
    if not g then break end
    local is_live = live and live.state == "active" and live.game_id == g.id
    local tag = is_live and "Running" or ((not g.launchable) and "Can't start yet" or "")
    list_row("game:" .. i, x, y, w, nil, 0, title_of(g), g.sub_line, tag, is_live, i == game_i, false)
    hit("game:" .. i, x, y, w, ROW_H)
    y = y + ROW_H + ROW_GAP
  end
  control_sink[#control_sink + 1] = {id = "game", enabled = true, fn = function() launch(focused()) end}
end

-- What the header and model row show.
local function head_sig()
  local at = (focus == "back" or focus == "home" or focus == "model") and focus or ""
  return table.concat({selected_core, at, tostring(overlay), room.width, room.height}, "|")
end
-- What the computer panel shows. A move in the games list leaves it alone
-- unless the chosen game changes one of these.
local function upper_sig()
  local game = focused()
  local c = computers[selected_core]
  local run = find_running()
  local at = (focus == "drive" or focus == "next_disk" or focus == "media" or focus == "disks" or focus == "add_disk" or focus == "game" or focus == "back" or focus == "home" or focus == "model") and "" or focus
  return table.concat({
    selected_core, at, play_state(), run and run.id or "", tostring(this_machine()),
    tostring(game and game.launchable), tostring(game and game.firmware_required and not game.firmware_ready),
    tostring(game ~= nil), tostring(hardware_unavailable), tostring(library_loaded),
    tostring(c and c.ready), c and c.unavailable_reason or "", tostring(overlay),
    room.width, room.height,
  }, "|")
end
local function build(part, sig, fn)
  painted_running, painting = find_running(), true
  begin(part)
  fn()
  painting = false
  part.sig = sig
end
local function head_rows()
  clear_row(C.paper)
  header()
  models()
end
local function present()
  local n = 0
  for _, part in ipairs({head, upper, lower, listing}) do
    for i = 1, #part.rows do n = n + 1 cmds[n] = part.rows[i] end
  end
  for i = #cmds, n + 1, -1 do cmds[i] = nil end
  gfx.paint(cmds)
end

local function scene_same()
  if room.width ~= held_w or room.height ~= held_h then return false end
  if rev ~= held_rev then return false end
  if session.state() ~= held_session then return false end
  if (room.overlay and true or false) ~= held_overlay then return false end
  return true
end
local function remember_scene()
  held_w, held_h = room.width, room.height
  held_rev = rev
  held_overlay = overlay
  held_session = session.state()
end

-- One step towards the current picture: rebuild one stale part, and present
-- once none is left. While parts are still stale the presented frame stays.
local function refresh()
  appearance()
  local hs, us = head_sig(), upper_sig()
  local ls = rev .. (overlay and "|overlay" or "")
  -- The list follows the drives part, which places it.
  local function gs() return ls .. "|" .. games_y end
  local stale = (head.sig ~= hs and 1 or 0) + (upper.sig ~= us and 1 or 0) + (lower.sig ~= ls and 1 or 0) + (listing.sig ~= gs() and 1 or 0)
  if head.sig ~= hs then build(head, hs, head_rows)
  elseif upper.sig ~= us then build(upper, us, computer_panel)
  elseif lower.sig ~= ls then build(lower, ls, right_column)
  elseif listing.sig ~= gs() then build(listing, gs(), games_list) end
  if stale > 1 and room.presented and gfx.reuse() then return end
  -- Nothing presented to keep: finish the picture in this draw.
  if head.sig ~= hs then build(head, hs, head_rows) end
  if upper.sig ~= us then build(upper, us, computer_panel) end
  if lower.sig ~= ls then build(lower, ls, right_column) end
  if listing.sig ~= gs() then build(listing, gs(), games_list) end
  present()
  remember_scene()
end

local warm_px, warm_at
local pool_rows = 160
local function warm_done()
  return warm_px and warm_at > #warm_px
end
local function warm_more()
  if not warm_px then
    warm_px = {}
    warm_at = 1
    for _, s in ipairs({15, 16, 17, 19, 20, 21, 22, 24, 30}) do
      warm_px[#warm_px + 1] = {s, false}
      warm_px[#warm_px + 1] = {s, true}
    end
  end
  local n = 0
  while warm_at <= #warm_px and n < 4 do
    local job = warm_px[warm_at]
    text_height(job[1], job[2])
    warm_at = warm_at + 1
    n = n + 1
  end
end
local function boot_step()
  if boot == 0 then
    paint_opening()
    boot = 1
    return
  end
  if #pool < pool_rows then
    local add = math.min(40, pool_rows - #pool)
    for _ = 1, add do pool[#pool + 1] = {} end
  elseif not warm_done() then
    warm_more()
  else
    boot = 2
    return
  end
  if room.presented and same_appearance() and gfx.reuse() then return end
  paint_opening()
end

function load()
  last_hw = room.time
  read_hardware()
  library_generation = library_generation + 1
  local generation = library_generation
  local ids = {}
  for _, variant in ipairs(family().variants) do ids[#ids + 1] = variant.id end
  -- home_family asks for machine entries and titles separately, so each row
  -- carries core_entry_kind; an ordinary query leaves it empty.
  library.query({platform = "fpga", core_ids = ids, sort = "title", limit = 2000, home_family = true}, function(found, err, _, more)
    if generation ~= library_generation then return end
    library_loaded = true
    if err then
      library_more = false
      library_error = err
      message = err
      place({})
      return
    end
    library_more = more == true
    library_error, message = "", ""
    local prepared = {}
    for _, g in ipairs(found or {}) do
      if is_system(g) then prepared[#prepared + 1] = g end
    end
    -- A title with several editions lists them only in its detail.
    local pending, replacements = 0, {}
    local function finish()
      local merged = {}
      for _, item in ipairs(prepared) do merged[#merged + 1] = replacements[item.id] or item end
      place(merged)
    end
    for _, g in ipairs(prepared) do
      if (g.variant_count or 0) > 1 and #(g.variants or {}) <= 1 then
        pending = pending + 1
        local id = g.id
        library.game(id, function(detail, detail_err)
          if generation ~= library_generation then return end
          if detail and not detail_err and is_system(detail) then
            -- A detail read does not say which kind of row it is.
            detail.core_entry_kind = g.core_entry_kind
            replacements[id] = detail
          end
          pending = pending - 1
          if pending == 0 then finish() end
        end)
      end
    end
    if pending == 0 then finish() end
  end)
end

function update()
  local state = session.state()
  if state ~= observed_session then
    observed_session = state
    live = nil
    last_hw = room.time
    rev = rev + 1
    read_hardware()
  end
  if room.time - last_hw >= 15 then
    last_hw = room.time
    read_hardware()
  end
end

function on_resume()
  read_hardware()
  load()
end

function unload() remember() end

local function set_focus(id)
  if id == focus then return end
  focus = id
  if id == "drive" or id == "next_disk" or id == "media" or id == "disks" or id == "add_disk" or id == "game" then right_focus = id end
  rev = rev + 1
  remember()
end
local function activate(id)
  local c = find_control(id)
  if not c then return false end
  set_focus(id)
  if c.enabled then c.fn() end
  rev = rev + 1
  remember()
  return true
end
local function pointer(id)
  local core = id:match("^model:(.+)$")
  if core then return "model", function() choose_core(core) end end
  local i = tonumber(id:match("^game:(%d+)$") or "")
  if i then
    return "game", function()
      if games[i] and i ~= game_i then
        game_i = i
        rev = rev + 1
      end
    end
  end
  return id, function() end
end
function on_hover(id)
  local target, apply = pointer(id)
  if target == "model" then return end
  if not has_control(target) then return end
  apply()
  set_focus(target)
end
function on_activate(id)
  local target, apply = pointer(id)
  apply()
  activate(target)
end

-- The left column, top to bottom, and the right column's stops.
local function left_ids()
  local out = {}
  for _, id in ipairs({"expansions", "firmware", "stop", "resume", "start"}) do
    if has_control(id) then out[#out + 1] = id end
  end
  return out
end
local function index_of(list, id)
  for i, v in ipairs(list) do if v == id then return i end end
  return nil
end
local function top_stop()
  return has_control("model") and "model" or "back"
end
function on_input(cmd)
  if cmd == "select" then return activate(focus) end
  if not has_control(focus) then
    set_focus(has_control("game") and #games > 0 and "game" or "start")
    return true
  end
  local left = left_ids()
  local li = index_of(left, focus)
  if cmd == "tab" or cmd == "tab_prev" then
    local order = {"back", "home"}
    if has_control("model") then order[#order + 1] = "model" end
    for _, id in ipairs(left) do order[#order + 1] = id end
    order[#order + 1] = "drive"
    if has_control("next_disk") then order[#order + 1] = "next_disk" end
    order[#order + 1] = "media"
    if has_control("disks") then order[#order + 1] = "disks" end
    if has_control("add_disk") then order[#order + 1] = "add_disk" end
    if #games > 0 then order[#order + 1] = "game" end
    local i = index_of(order, focus) or 1
    set_focus(order[(i - 1 + (cmd == "tab" and 1 or -1)) % #order + 1])
    return true
  end
  if cmd ~= "left" and cmd ~= "right" and cmd ~= "up" and cmd ~= "down" then return false end
  if focus == "back" or focus == "home" then
    if cmd == "left" then set_focus("back")
    elseif cmd == "right" then set_focus("home")
    elseif cmd == "down" then set_focus(has_control("model") and "model" or "drive") end
  elseif focus == "model" then
    if cmd == "left" then step_model(-1)
    elseif cmd == "right" then step_model(1)
    elseif cmd == "up" then set_focus("back")
    else set_focus("drive") end
  elseif li then
    if cmd == "up" then set_focus(left[li - 1] or top_stop())
    elseif cmd == "down" then set_focus(left[li + 1] or focus)
    elseif cmd == "right" then
      local to = right_focus
      if to == "game" and #games == 0 then to = "drive" end
      set_focus(to)
    end
  elseif focus == "drive" then
    if cmd == "up" then set_focus(top_stop())
    elseif cmd == "down" then set_focus(has_control("next_disk") and "next_disk" or "media")
    elseif cmd == "left" then set_focus("start") end
  elseif focus == "next_disk" then
    if cmd == "up" then set_focus("drive")
    elseif cmd == "down" then if #games > 0 then set_focus("game") end
    elseif cmd == "right" then set_focus("media")
    elseif cmd == "left" then set_focus("start") end
  elseif focus == "media" then
    if cmd == "up" then set_focus("drive")
    elseif cmd == "down" then if #games > 0 then set_focus("game") end
    elseif cmd == "right" then
      if has_control("disks") then set_focus("disks") elseif has_control("add_disk") then set_focus("add_disk") end
    elseif cmd == "left" then set_focus(has_control("next_disk") and "next_disk" or "start") end
  elseif focus == "disks" then
    if cmd == "up" then set_focus("drive")
    elseif cmd == "down" then if #games > 0 then set_focus("game") end
    elseif cmd == "right" then if has_control("add_disk") then set_focus("add_disk") end
    elseif cmd == "left" then set_focus("media") end
  elseif focus == "add_disk" then
    if cmd == "up" then set_focus("drive")
    elseif cmd == "down" then if #games > 0 then set_focus("game") end
    elseif cmd == "left" then set_focus(has_control("disks") and "disks" or "media") end
  elseif focus == "game" then
    if cmd == "left" then set_focus("start")
    elseif cmd == "up" then
      if game_i > 1 then
        game_i = game_i - 1
        keep_visible()
        rev = rev + 1
        remember()
      else
        set_focus(has_control("next_disk") and "next_disk" or "media")
      end
    elseif cmd == "down" and game_i < #games then
      game_i = game_i + 1
      keep_visible()
      rev = rev + 1
      remember()
    end
  end
  return true
end

function draw()
  scale = math.min(room.width / 1120, room.height / 630)
  ox = (room.width - 1120 * scale) / 2
  oy = (room.height - 630 * scale) / 2
  if boot < 2 then
    boot_step()
    return
  end
  if scene_same() and room.presented and gfx.reuse() then return end
  refresh()
end

end
