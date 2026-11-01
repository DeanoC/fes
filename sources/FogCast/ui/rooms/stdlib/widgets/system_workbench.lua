-- Shared workbench for installed playable systems. It uses the ordinary
-- library launch and setup paths; only navigation lives in room storage.
return function(options)
local systems = require "systems"
options = options or {}
local locked_core = options.core_id or ""
local selected_core = locked_core
if selected_core == "" then selected_core = room.initial_core_id or "" end
if selected_core == "" then selected_core = "fes.zx81" end
local function spec()
  return systems.by_id[selected_core] or {
    label = "Unsupported system", mark = selected_core, disk = "", cassette = "",
    media = "Media", hint = "This system is not supported by the workbench.",
  }
end
local function system_label() return spec().label end
local function family()
  return systems.family_by_core[selected_core] or {
    id = selected_core, label = "Unsupported system", variants = {{id = selected_core}},
  }
end
local function family_label() return family().label end
-- The launcher shows a family's firmware card: once on its first opening,
-- and again from the Firmware button.
local function firmware_info(first_use)
  if not systems.by_id[selected_core] then return end
  local ids = {}
  for _, variant in ipairs(family().variants) do ids[#ids + 1] = variant.id end
  hardware.firmware_info({core_id=selected_core, family_id=family().id, family_label=family_label(), core_ids=ids, first_use=first_use})
end
local function rgb(r, g, b, a)
  if a then return {r = r, g = g, b = b, a = a} end
  return {r = r, g = g, b = b}
end
local LIGHT = {
  paper = rgb(244, 240, 232), panel = rgb(255, 252, 245),
  raised = rgb(233, 230, 220), muted = rgb(60, 77, 69),
  ink = rgb(28, 42, 38), label = rgb(36, 58, 52), line = rgb(206, 207, 192),
  accent = rgb(35, 90, 70), light = rgb(252, 252, 243),
  case = rgb(92, 122, 107), warm = rgb(246, 235, 203),
}
local DARK = {
  paper = {r = 20, g = 25, b = 24, a = 200}, panel = rgb(32, 39, 37),
  raised = rgb(48, 57, 54), muted = rgb(210, 224, 214),
  ink = rgb(237, 243, 233), label = rgb(210, 224, 214), line = rgb(74, 86, 80),
  accent = rgb(200, 227, 172), light = rgb(32, 55, 39),
  case = rgb(92, 122, 107), warm = rgb(32, 39, 37),
}
local C, overlay = LIGHT, false
local function appearance()
  overlay = room.overlay and true or false
  C = overlay and DARK or LIGHT
end
local function same_appearance()
  return (room.overlay and true or false) == overlay
end

local entries, all_entries = {}, {}
local entry_i, edition_i = 1, 1
local focus = "start"
local controls = {}
local live
local hardware_unavailable = false
local rev = 0
local last_hw = 0
local hw_busy = false
local hw_refresh_pending = false
local observed_session = session.state()
local library_generation = 0
local library_error = ""
local cmds, pool, sink = {}, {}, nil
local heights = {}
local scale, ox, oy = 1, 0, 0
local boot = 0
local held_w, held_h, held_focus = -1, -1, ""
local held_entry, held_edition, held_rev, held_overlay = -1, -1, -1, false
local held_message = ""
local held_session = ""

local nav = store.get("navigation", {})
local initial_game_id = room.initial_game_id or ""
if locked_core == "" and initial_game_id == "" and (room.initial_core_id or "") == "" and systems.by_id[nav.core_id or ""] then selected_core = nav.core_id end
local family_variants = type(nav.family_variants) == "table" and nav.family_variants or {}
local core_choices = type(nav.core_choices) == "table" and nav.core_choices or {}
local previous_core = core_choices[selected_core] or {}
local same_core = nav.core_id == selected_core
local chosen = initial_game_id ~= "" and initial_game_id or (same_core and nav.game_id or previous_core.game_id) or ""
local chosen_edition = initial_game_id ~= "" and initial_game_id or (same_core and nav.edition_id or previous_core.edition_id) or ""
if nav.focus and nav.focus ~= "" then focus = nav.focus end
local message = systems.by_id[selected_core] and ("Choose a " .. system_label() .. " entry.")
  or ("The system " .. selected_core .. " is not supported by this workbench.")

local function take()
  local n = #pool
  if n == 0 then return {} end
  local row = pool[n]
  pool[n] = nil
  return row
end
local function recycle()
  for i = #cmds, 1, -1 do
    pool[#pool + 1] = cmds[i]
    cmds[i] = nil
  end
end
local function push(row)
  local dest = sink or cmds
  dest[#dest + 1] = row
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
local function text(s, x, y, size, c, w, bold)
  if not s or s == "" then return end
  local row = take()
  row[1], row[2], row[3], row[4] = "t", s, ox + x * scale, oy + y * scale
  row[5], row[6], row[7], row[8], row[9] = math.max(12, math.floor(size * scale)), c or C.ink, (w or 0) * scale, bold or false, "left"
  push(row)
end
local function hit(id, x, y, w, h)
  local row = take()
  row[1], row[2], row[3], row[4], row[5], row[6] = "h", id, ox + x * scale, oy + y * scale, w * scale, h * scale
  row[7], row[8], row[9] = nil, nil, nil
  push(row)
end
local function text_height(px, bold)
  local key = bold and -px or px
  local h = heights[key]
  if h then return h end
  local _, measured = gfx.measure("Ag", px, bold)
  heights[key] = measured
  return measured
end
local function outline(x, y, w, h, t, c)
  rect(x, y, w, t, c)
  rect(x, y + h - t, w, t, c)
  rect(x, y, t, h, c)
  rect(x + w - t, y, t, h, c)
end
local function button(id, label, x, y, w, h, enabled, fn)
  local selected = id == focus
  local primary = id == "start" or (id == "setup" and #entries == 0)
  local fill = primary and enabled and C.accent or (enabled and C.panel or C.raised)
  if selected then
    outline(x - 4, y - 4, w + 8, h + 8, 3, C.accent)
  end
  rect(x, y, w, h, C.line)
  rect(x + 1, y + 1, w - 2, h - 2, fill)
  local size = math.max(12, math.floor(math.min(20, h - 12) * scale))
  local bold = selected or (primary and enabled)
  local th = text_height(size, bold)
  local color = primary and enabled and C.light or (enabled and C.ink or C.muted)
  text(label, x + 12, y + (h - th / scale) / 2, math.min(20, h - 12), color, w - 24, bold)
  hit(id, x, y, w, h)
  controls[#controls + 1] = {id = id, x = x + w / 2, y = y + h / 2, enabled = enabled and true or false, fn = fn}
end

local function short_id(id)
  id = id or ""
  if #id > 8 then return id:sub(1, 8) end
  return id
end
local function is_system(g)
  return type(g) == "table" and systems.by_id[g.core_id or ""] and (locked_core == "" or g.core_id == locked_core) and g.id and g.id ~= ""
end
local function editions_of(entry)
  local out = {}
  local function add(g)
    if not is_system(g) or g.core_id ~= entry.core_id then return end
    for _, have in ipairs(out) do
      if have.id == g.id then return end
    end
    out[#out + 1] = g
  end
  add(entry)
  for _, v in ipairs(entry.variants or {}) do add(v) end
  return out
end
local function has_game_id(list, id)
  for _, entry in ipairs(list or {}) do
    if entry.id == id then return true end
    for _, edition in ipairs(editions_of(entry)) do
      if edition.id == id then return true end
    end
  end
  return false
end
local function focused()
  local entry = entries[entry_i]
  if not entry then return nil end
  local eds = editions_of(entry)
  if #eds == 0 then return entry end
  if edition_i < 1 or edition_i > #eds then edition_i = 1 end
  return eds[edition_i]
end
local function remember()
  if not systems.by_id[selected_core] then return end
  local entry = entries[entry_i]
  local game = focused()
  local previous = core_choices[selected_core] or {}
  local game_id = entry and entry.id or chosen
  local edition_id = game and game.id or chosen_edition
  if game_id == "" then game_id = previous.game_id or "" end
  if edition_id == "" then edition_id = previous.edition_id or "" end
  if game_id ~= "" then chosen = game_id end
  if edition_id ~= "" then chosen_edition = edition_id end
  family_variants[family().id] = selected_core
  core_choices[selected_core] = {game_id=game_id, edition_id=edition_id}
  store.set("navigation", {
    family_variants=family_variants, core_choices=core_choices,
    game_id = game_id,
    edition_id = edition_id,
    focus = focus, core_id = selected_core,
  })
end
local function place(list)
  all_entries = list or {}
  if initial_game_id ~= "" then
    for _, g in ipairs(all_entries) do
      if has_game_id({g}, initial_game_id) then selected_core = g.core_id break end
    end
  end
  if focus == "variant" and (locked_core ~= "" or #family().variants < 2) then focus = "start" end
  entries = {}
  for _, g in ipairs(all_entries) do if g.core_id == selected_core then entries[#entries + 1] = g end end
  if #entries == 0 and focus == "start" then focus = "setup" end
  entry_i, edition_i = 1, 1
  local selected = false
  for i, entry in ipairs(entries) do
    if entry.id == chosen then
      entry_i, edition_i = i, 1
      selected = true
    end
    local eds = editions_of(entry)
    for j, ed in ipairs(eds) do
      if chosen_edition ~= "" and ed.id == chosen_edition then
        entry_i, edition_i = i, j
        selected = true
      end
    end
  end
  if library_error == "" then
    if initial_game_id ~= "" and not selected then
      message = "The selected " .. system_label() .. " entry is no longer available."
    elseif initial_game_id ~= "" then
      message = "Choose a " .. system_label() .. " entry."
    end
  end
  if selected then initial_game_id = "" end
  rev = rev + 1
end
local function place_selected(list, generation)
  if generation ~= library_generation then return end
  local wanted = chosen_edition ~= "" and chosen_edition or chosen
  if wanted == "" or has_game_id(list, wanted) then
    place(list)
    return
  end
  library.game(wanted, function(detail, detail_err)
    if generation ~= library_generation then return end
    if detail and not detail_err and is_system(detail) and has_game_id({detail}, wanted) then
      list[#list + 1] = detail
    end
    place(list)
  end)
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
    if value and not err then
      live = value.session
      hardware_unavailable = false
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
local function this_machine()
  local game = focused()
  return live and game and live.state == "active" and live.game_id == game.id
end

local function slot_line(game, n, name)
  local slot
  for _, item in ipairs(game.slot_expansions or {}) do
    if item.slot == n then slot = item end
  end
  if not slot or not slot.expansion_id or slot.expansion_id == "" then
    return name .. "  ·  empty"
  end
  local mark = slot.ready and "ready" or "unavailable"
  return name .. "  ·  " .. short_id(slot.expansion_id) .. "  ·  " .. mark
end
local function extra_line(game)
  local id = game.expansion_id or ""
  if id == "" then return "" end
  for _, item in ipairs(game.slot_expansions or {}) do
    if item.expansion_id == id then return "" end
  end
  local mark = game.expansion_ready and "ready" or "unavailable"
  return "Expansion  ·  " .. short_id(id) .. "  ·  " .. mark
end
local function disk_line(game)
  if game.media_id and game.media_id ~= "" then
    local label = "Media"
    if game.media_role == "disk" then label = spec().disk or spec().media
    elseif game.media_role == "cassette" then label = spec().cassette or spec().media
    elseif game.media_role == "blob" then label = spec().media end
    return label .. "  ·  " .. short_id(game.media_id)
  end
  return spec().media .. "  ·  none saved"
end
local function model_line(game)
  local line = game.title or game.id
  if game.region and game.region ~= "" then
    line = line .. " (" .. game.region .. ")"
  end
  return line
end
local function running_line()
  if hardware_unavailable then return "Running: status unavailable" end
  local state = play_state()
  if state == "active" and this_machine() then return "Running: this " .. system_label() end
  if state == "active" then return "Running: another title" end
  if state == "idle" then return "Running: not running" end
  if live then return "Running: " .. (state ~= "" and state or "unknown") end
  return "Running: not checked"
end

local function choose_core(core_id)
  if core_id == selected_core or not systems.by_id[core_id] then return end
  remember()
  local previous_family = family().id
  selected_core = core_id
  if family().id ~= previous_family then firmware_info(true) end
  initial_game_id = ""
  local previous = core_choices[core_id] or {}
  chosen, chosen_edition = previous.game_id or "", previous.edition_id or ""
  message = library_error ~= "" and library_error or ("Choose a " .. system_label() .. " entry, or set up this system.")
  place(all_entries)
  remember()
end
local function next_variant()
  if not systems.by_id[selected_core] then return end
  local variants = family().variants
  for i, variant in ipairs(variants) do
    if variant.id == selected_core then
      choose_core(variants[i % #variants + 1].id)
      return
    end
  end
end
local function next_system()
  if not systems.family_by_core[selected_core] then
    local first = systems.families[1]
    if first and first.variants[1] then choose_core(first.variants[1].id) end
    return
  end
  for i, value in ipairs(systems.families) do
    if value.id == family().id then
      local next_family = systems.families[i % #systems.families + 1]
      local core_id = family_variants[next_family.id]
      if systems.family_by_core[core_id] ~= next_family then core_id = next_family.variants[1].id end
      choose_core(core_id)
      return
    end
  end
end

local function next_machine()
  if #entries < 2 then return end
  entry_i = entry_i % #entries + 1
  edition_i = 1
  local entry = entries[entry_i]
  chosen = entry and entry.id or ""
  local game = focused()
  chosen_edition = game and game.id or ""
  message = "Next " .. system_label() .. " entry."
  remember()
  rev = rev + 1
end
local function next_edition()
  local entry = entries[entry_i]
  if not entry then return end
  local eds = editions_of(entry)
  if #eds < 2 then return end
  edition_i = edition_i % #eds + 1
  local game = eds[edition_i]
  chosen_edition = game and game.id or ""
  message = "Next edition."
  remember()
  rev = rev + 1
end

local function paint_opening()
  controls = {}
  recycle()
  sink = nil
  appearance()
  clear_row(C.paper)
  rect(24, 18, 18, 5, C.accent)
  text("FES", 50, 14, 24, C.ink, 80, true)
  text(system_label() .. " setup", 118, 17, 19, C.ink, 400)
  text("Opening the " .. system_label() .. " setup…", 24, 84, 22, C.label, 700)
  gfx.paint(cmds)
end

local function picture(part)
  if part ~= "lower" then
    controls = {}
    recycle()
    sink = nil
    appearance()
    clear_row(C.paper)
  else
    recycle()
    sink = {}
  end
  local game = focused()
  if part ~= "lower" then
    rect(24, 18, 18, 5, C.accent)
    text("FES", 50, 14, 24, C.ink, 80, true)
    rect(104, 16, 2, 26, C.line)
    text(system_label() .. " setup", 118, 15, 23, C.ink, 360)
    button("back", "Back", 860, 10, 110, 38, true, function() rooms.back() end)
    button("home", "Home", 984, 10, 110, 38, true, function() rooms.home() end)
    rect(0, 56, 1120, 2, C.line)

    text(string.upper(family_label()) .. " SETUP", 24, 68, 13, C.label, 600, true)
    text(game and model_line(game) or system_label(), 24, 90, 30, C.ink, 640, true)
    text(message, 24, 130, 18, C.muted, 640)
    rect(680, 82, 414, 72, C.raised)
    text(running_line(), 696, 92, 19, C.ink, 382, true)
    text(game and "Saved for next start" or "Set up this system to begin", 696, 122, 16, C.muted, 382)

    if not overlay then
      rect(24, 168, 420, 220, C.line)
      rect(25, 169, 418, 218, C.panel)
      rect(48, 190, 370, 142, C.case)
      rect(60, 202, 346, 118, C.raised)
      text(spec().mark, 82, 234, 30, C.ink, 300, true)
      rect(48, 346, 370, 18, C.warm)
    end
    text("Selected expansion cards", 470, 170, 20, C.ink, 620, true)
    local slots = game and game.slot_expansions or {}
    if #slots == 0 then
      text("Open Expansion sockets to inspect this package.", 470, 206, 18, C.muted, 620)
    end
    for i, slot in ipairs(slots) do
      if i <= 5 then text(slot_line(game, slot.slot, "Socket " .. tostring(slot.slot)), 470, 202 + (i-1)*24, 17, C.ink, 620) end
    end
    local extra = game and extra_line(game) or ""
    if extra ~= "" then text(extra, 470, 324, 17, C.ink, 620) end
    if locked_core == "" and #family().variants > 1 then
      button("variant", "Variant: " .. system_label(), 470, 350, 620, 36, true, next_variant)
    end

    text(game and disk_line(game) or spec().media .. "  ·  none saved", 24, 396, 20, C.ink, 1060, true)
    text(spec().hint, 24, 426, 18, C.muted, 1060)
    if selected_core == "fes.c64" then
      text("Tape .tap and .t64: not on this core", 24, 448, 15, C.ink, 520)
      text("Cartridge file .crt: not on this core", 560, 448, 15, C.ink, 530)
    end
  end
  if part == "upper" then return end

  local state = play_state()
  local can_start = game and game.launchable and state == "idle" and not hardware_unavailable
  local label = "Start machine"
  local enabled = can_start and true or false
  local fn = function()
    if game then session.launch(game.id) end
  end
  if this_machine() then
    label = "Stop machine"
    enabled = true
    fn = function() session.stop(live) end
  elseif hardware_unavailable then
    label = "Status unavailable"
    enabled = false
  elseif state == "active" then
    label = "Machine in use"
    enabled = false
  end
  local idle = state == "idle" and not hardware_unavailable and systems.by_id[selected_core] ~= nil
  button("setup", "Set up system", 24, 480, 200, 38, idle, function() hardware.setup({core_id=selected_core}) end)
  button("media", "Media for next start", 240, 480, 240, 38, idle and game and game.package_id and game.package_id ~= "", function()
    hardware.open_media({game_id=game.id, core_id=game.core_id, package_id=game.package_id, expected_media_id=game.media_id or ""})
  end)
  button("expansions", "Expansion sockets", 496, 480, 240, 38, idle and game and game.package_id and game.package_id ~= "", function()
    hardware.open_expansions({game_id=game.id, core_id=game.core_id, package_id=game.package_id})
  end)
  button("system", "Next system", 752, 480, 200, 38, locked_core == "", next_system)
  button("firmware", "Firmware", 968, 480, 128, 38, systems.by_id[selected_core] ~= nil, function() firmware_info(false) end)
  button("machine", "Next machine", 24, 530, 200, 44, #entries > 1, next_machine)
  button("edition", "Next edition", 240, 530, 200, 44, game and #editions_of(entries[entry_i]) > 1, next_edition)
  button("start", label, 456, 524, 220, 52, enabled, fn)
  button("disk", selected_core == "fes.c64" and "Choose disk" or "Change media", 692, 530, 200, 44, this_machine() and live.removable_available, function()
    session.open_tape(live)
  end)
  button("resume", "Return to play", 908, 530, 188, 44, state == "active" and not hardware_unavailable, function() session.resume() end)
  text("Stop clears memory. Saved media stays on the library entry.", 24, 590, 17, C.muted, 1060)

  if part == "lower" then
    gfx.extend(sink)
    for i = 1, #sink do cmds[#cmds + 1] = sink[i] end
    sink = nil
  end
end

local function scene_same()
  if room.width ~= held_w or room.height ~= held_h then return false end
  if focus ~= held_focus or entry_i ~= held_entry or edition_i ~= held_edition then return false end
  if rev ~= held_rev or message ~= held_message then return false end
  if session.state() ~= held_session then return false end
  if (room.overlay and true or false) ~= held_overlay then return false end
  return true
end
local function remember_scene()
  held_w, held_h = room.width, room.height
  held_focus = focus
  held_entry, held_edition = entry_i, edition_i
  held_rev, held_message = rev, message
  held_overlay = overlay
  held_session = session.state()
end

local warm_px, warm_at
local pool_rows = 120
local function warm_done()
  return warm_px and warm_at > #warm_px
end
local function warm_more()
  if not warm_px then
    warm_px = {}
    warm_at = 1
    local seen = {}
    local function add(px, bold)
      px = math.max(12, math.floor(px))
      local key = px .. (bold and "b" or "n")
      if seen[key] then return end
      seen[key] = true
      warm_px[#warm_px + 1] = {px, bold and true or false}
    end
    for _, s in ipairs({13, 15, 16, 17, 18, 19, 20, 22, 23, 24, 28, 30}) do
      add(s * scale, false)
      add(s * scale, true)
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
  if boot == 1 then
    if #pool < pool_rows then
      recycle()
      local add = math.min(40, pool_rows - #pool)
      for _ = 1, add do pool[#pool + 1] = {} end
    elseif not warm_done() then
      warm_more()
    else
      boot = 2
    end
    if room.presented and same_appearance() and gfx.reuse() then return end
    paint_opening()
    return
  end
  if boot == 2 then
    picture("upper")
    gfx.paint(cmds)
    remember_scene()
    boot = 3
    return
  end
  if not scene_same() then
    boot = 2
    if not same_appearance() then
      paint_opening()
      return
    end
    if room.presented and gfx.reuse() then return end
    return
  end
  picture("lower")
  boot = 4
  remember_scene()
end

function load()
  firmware_info(true)
  last_hw = room.time
  read_hardware()
  library_generation = library_generation + 1
  local generation = library_generation
  library.query({
    platform = "fpga",
    core_ids = locked_core ~= "" and {locked_core} or systems.ids,
    sort = "title",
    limit = 2000,
  }, function(games, err)
    if generation ~= library_generation then return end
    if err then
      library_error = err
      all_entries = {}
      entries = {}
      entry_i, edition_i = 1, 1
      message = library_error
      rev = rev + 1
      return
    end
    library_error = ""
    if systems.by_id[selected_core] then
      message = "Choose a " .. system_label() .. " entry."
    else
      message = "The system " .. selected_core .. " is not supported by this workbench."
    end
    local prepared = {}
    for _, g in ipairs(games or {}) do
      if is_system(g) then prepared[#prepared + 1] = g end
    end
    local pending = 0
    local replacements = {}
    for _, g in ipairs(prepared) do
      local count = g.variant_count or 0
      local have = #(g.variants or {})
      if count > 1 and have <= 1 then
        pending = pending + 1
        local id = g.id
        local fallback = g
        library.game(id, function(detail, detail_err)
          if generation ~= library_generation then return end
          if detail and not detail_err and is_system(detail) then
            replacements[id] = detail
          else
            replacements[id] = fallback
          end
          pending = pending - 1
          if pending == 0 then
            local merged = {}
            for _, item in ipairs(prepared) do
              merged[#merged + 1] = replacements[item.id] or item
            end
            place_selected(merged, generation)
          end
        end)
      end
    end
    if pending == 0 then place_selected(prepared, generation) end
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

local function activate(id)
  for _, c in ipairs(controls) do
    if c.id == id then
      focus = id
      if c.enabled then c.fn() end
      remember()
      return true
    end
  end
  return false
end
function on_hover(id)
  for _, c in ipairs(controls) do
    if c.id == id then focus = id remember() return end
  end
end
function on_activate(id) activate(id) end
function on_input(cmd)
  if cmd == "select" then return activate(focus) end
  if #controls == 0 then return false end
  local index = 1
  for i, c in ipairs(controls) do if c.id == focus then index = i end end
  if cmd == "tab" or cmd == "tab_prev" then
    index = (index - 1 + (cmd == "tab" and 1 or -1)) % #controls + 1
    focus = controls[index].id
    remember()
    return true
  end
  local dx, dy = 0, 0
  if cmd == "left" then dx = -1
  elseif cmd == "right" then dx = 1
  elseif cmd == "up" then dy = -1
  elseif cmd == "down" then dy = 1
  else return false end
  local here, best, score = controls[index], nil, math.huge
  for _, c in ipairs(controls) do
    local x, y = c.x - here.x, c.y - here.y
    local forward = x * dx + y * dy
    if forward > 1 then
      local cross = math.abs(dx ~= 0 and y or x)
      local distance = forward + cross * 3
      if distance < score then score, best = distance, c end
    end
  end
  if best then focus = best.id remember() end
  return true
end

function draw()
  scale = math.min(room.width / 1120, room.height / 630)
  ox = (room.width - 1120 * scale) / 2
  oy = (room.height - 630 * scale) / 2
  if boot == 4 and scene_same() and room.presented and gfx.reuse() then return end
  if boot < 4 then
    boot_step()
    return
  end
  picture("all")
  gfx.paint(cmds)
  remember_scene()
end

end
