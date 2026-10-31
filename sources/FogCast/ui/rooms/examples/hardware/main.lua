-- Hardware facts and saved selections come from the host. Only navigation
-- belongs in store. A saved setup is the next launch, never a RAM snapshot.
-- Lounge seed roles (docs/ux/visual-language.md). "warm" carries setup
-- consequences; focus is an accent outline, never colour alone.
-- Colour tables, not hex strings: the draw callback parses a string on every
-- coloured primitive, and that cost is inside the kit's frame budget.
local function rgb(r, g, b) return {r = r, g = g, b = b} end
local LIGHT = {
  paper = rgb(244, 240, 232), panel = rgb(255, 252, 245), raised = rgb(233, 230, 220),
  ink = rgb(36, 54, 49), muted = rgb(83, 98, 90), line = rgb(206, 207, 192),
  accent = rgb(35, 90, 70), light = rgb(252, 252, 243), warm = rgb(246, 235, 203),
  warm_edge = rgb(217, 180, 90), card = rgb(92, 122, 107), brass = rgb(182, 146, 82),
}
-- Dark roles over a running machine (room.overlay, kit HDMI only). The clear
-- is a translucent scrim, so the machine stays visible, dimmed, behind the
-- room. Text sits on the scrim or on its own dark surface; the notice keeps
-- its warm edge on a dark fill. Connector colours are unchanged.
local DARK = {
  paper = {r = 20, g = 25, b = 24, a = 200}, panel = rgb(32, 39, 37), raised = rgb(48, 57, 54),
  ink = rgb(237, 243, 233), muted = rgb(181, 195, 185), line = rgb(74, 86, 80),
  accent = rgb(200, 227, 172), light = rgb(32, 55, 39), warm = rgb(32, 39, 37),
  warm_edge = rgb(230, 200, 130), card = rgb(92, 122, 107), brass = rgb(182, 146, 82),
}
local C, overlay = LIGHT, false
-- Each picture is painted in one appearance.
local function appearance()
  overlay = room.overlay and true or false
  C = overlay and DARK or LIGHT
end
-- A presented frame may be reused only in the appearance it was painted in.
local function same_appearance()
  return (room.overlay and true or false) == overlay
end
local data, machine, art
local systems = require "systems"
local initial_core_id = room.initial_core_id or "fes.zx81"
local function model()
  local id = machine and machine.core_id or initial_core_id
  local family = systems.family_by_core[id]
  return family and family.id == "zx81" and systems.by_id[id] or systems.by_id["fes.zx81"]
end
local function workshop_title() return model().label .. " setup" end
local opening_message = "Finding your " .. model().label .. " setups..."
local initial_game_id = room.initial_game_id or ""
local initial_selection_pending = false
local chosen, inspected = "", ""
local focus = "socket"
local controls = {}
local loading, saving, stale = false, false, true
local refresh_pending = false
local message, read_error = opening_message, ""
local last_read, page = 0, 1
local scale, ox, oy = 1, 0, 0
local cmds, pool = {}, {}
local heights, fit_cache, wrap_cache = {}, {}, {}
-- Last presented picture. draw() reuses it until one of these changes.
local held_w, held_h = -1, -1
local held_focus, held_page, held_inspected, held_chosen
local held_loading, held_saving, held_stale
local held_message, held_read_error
local held_art, held_data, held_machine, held_overlay

-- Command rows are reused across pictures. A fresh open still fills them
-- once; the refresh that follows a later visit writes into the same rows.
local function take()
  local n = #pool
  if n == 0 then return {} end
  local row = pool[n]
  pool[n] = nil
  return row
end
local function recycle()
  for i = #cmds, 1, -1 do
    pool[#pool+1] = cmds[i]
    cmds[i] = nil
  end
end
-- sink is set while the lower half is appended to a picture already presented.
local sink
local function push(row)
  local dest = sink or cmds
  dest[#dest+1] = row
end
local function clear_row(c)
  local row = take()
  row[1], row[2] = "c", c
  row[3], row[4], row[5], row[6], row[7], row[8], row[9] = nil, nil, nil, nil, nil, nil, nil
  push(row)
end
local function rect(x,y,w,h,c)
  local row = take()
  row[1], row[2], row[3], row[4], row[5], row[6] = "r", ox+x*scale, oy+y*scale, w*scale, h*scale, c
  row[7], row[8], row[9] = nil, nil, nil
  push(row)
end
local function text(s,x,y,size,c,w,bold)
  local row = take()
  row[1], row[2], row[3], row[4] = "t", s, ox+x*scale, oy+y*scale
  row[5], row[6], row[7], row[8], row[9] = math.max(12, math.floor(size*scale)), c or C.ink, (w or 0)*scale, bold or false, "left"
  push(row)
end
local function hit(id,x,y,w,h)
  local row = take()
  row[1], row[2], row[3], row[4], row[5], row[6] = "h", id, ox+x*scale, oy+y*scale, w*scale, h*scale
  row[7], row[8], row[9] = nil, nil, nil
  push(row)
end
local function blit(img,x,y,w,h)
  local row = take()
  row[1], row[2], row[3], row[4], row[5], row[6] = "i", img, ox+x*scale, oy+y*scale, w*scale, h*scale
  row[7], row[8], row[9] = nil, nil, nil
  push(row)
end
local function label_at(s,x,y,size,c,w,bold,align)
  local row = take()
  row[1], row[2], row[3], row[4] = "t", s, x, y
  row[5], row[6], row[7], row[8], row[9] = size, c, w, bold, align
  push(row)
end
-- Glyph height depends on the size and weight, not the words. Measured once.
local function text_height(px, bold)
  local key = bold and -px or px
  local h = heights[key]
  if h then return h end
  local _, measured = gfx.measure("Ag", px, bold)
  heights[key] = measured
  return measured
end
-- Keep a host cassette label inside w logical pixels. The width is the
-- measured face, not a byte count: narrow letters are thinner than one em,
-- so a long name can still fit. A label that does not fit keeps its start
-- and its end, so two long labels stay distinct. The last result is reused
-- so a dirty frame does not measure the same label again.
local fit_seen_s, fit_seen_px, fit_seen_limit, fit_seen_bold, fit_seen
local function fit_name(s, size, w, bold)
  s = s or ""
  local px = math.max(12, math.floor(size * scale))
  local limit = w * scale
  if s == fit_seen_s and px == fit_seen_px and limit == fit_seen_limit and bold == fit_seen_bold then
    return fit_seen
  end
  local function remember(result)
    fit_seen_s, fit_seen_px, fit_seen_limit, fit_seen_bold, fit_seen = s, px, limit, bold, result
    return result
  end
  if gfx.measure(s, px, bold) <= limit then return remember(s) end
  local key = s .. "\0" .. px .. "\0" .. math.floor(limit) .. (bold and "b" or "n")
  local cached = fit_cache[key]
  if cached then return remember(cached) end
  local chars = {}
  for ch in s:gmatch("[%z\1-\127\194-\244][\128-\191]*") do chars[#chars+1] = ch end
  if #chars < 2 then fit_cache[key] = s return remember(s) end
  local ell = "…"
  local function piece(i, j) return table.concat(chars, "", i, j) end
  local function width(candidate) return gfx.measure(candidate, px, bold) end
  local keep = 1
  while keep < #chars and width(ell .. piece(#chars - keep, #chars)) <= limit do keep = keep + 1 end
  local best = ell .. piece(#chars - keep + 1, #chars)
  local head = 0
  while head < #chars - keep do
    local candidate = piece(1, head + 1) .. ell .. piece(#chars - keep + 1, #chars)
    if width(candidate) > limit then break end
    head = head + 1
    best = candidate
  end
  fit_cache[key] = best
  return remember(best)
end
local function wrapped(s, size, max_lines, w)
  s = s or ""
  local px = math.floor(size*scale)
  local limit = w*scale
  local key = s .. "\0" .. px .. "\0" .. math.floor(limit*100) .. "\0" .. max_lines
  local cached = wrap_cache[key]
  if cached then return cached end
  cached = {}
  local line, n, done = "", 0, false
  for word in s:gmatch("%S+") do
    local next_line = line == "" and word or line.." "..word
    if gfx.measure(next_line, px) > limit and line ~= "" then
      if n + 1 >= max_lines then
        -- Last allowed line: say that the text continues.
        cached[#cached+1] = line.." …"
        done = true
        break
      end
      cached[#cached+1] = line
      n = n + 1
      line = word
    else line = next_line end
  end
  if not done and line ~= "" then cached[#cached+1] = line end
  wrap_cache[key] = cached
  return cached
end
local function lines(s,x,y,w,size,max_lines,c)
  local cached = wrapped(s, size, max_lines, w)
  for i, line in ipairs(cached) do text(line, x, y+(i-1)*(size+5), size, c, w) end
end
local function outline(x,y,w,h,t,c)
  rect(x,y,w,t,c) rect(x,y+h-t,w,t,c) rect(x,y,t,h,c) rect(x+w-t,y,t,h,c)
end
local function button(id,label,x,y,w,h,enabled,fn,accent)
  local selected = id == focus
  local filled = accent and enabled
  local fill = filled and C.accent or C.panel
  if selected then
    -- Accent ring just outside the control. Only the focused control pays
    -- for four edges; every other button is a border rect plus its fill.
    outline(x-4,y-4,w+8,h+8,3,C.accent)
    rect(x,y,w,h,fill)
  elseif not filled then
    rect(x,y,w,h,C.line)
    rect(x+1,y+1,w-2,h-2,fill)
  else
    rect(x,y,w,h,fill)
  end
  local size=math.max(12,math.floor(math.min(20,h-10)*scale))
  local bold=selected or filled
  local th=text_height(size, bold)
  local color=filled and C.light or (enabled and C.ink or C.muted)
  label_at(label, ox+(x+8)*scale, oy+y*scale+(h*scale-th)/2, size, color, (w-16)*scale, bold, "center")
  hit(id,x,y,w,h)
  controls[#controls+1] = {id=id,x=x+w/2,y=y+h/2,fn=fn,enabled=enabled}
end
local function scene_same()
  if room.width ~= held_w or room.height ~= held_h or focus ~= held_focus or page ~= held_page then return false end
  if loading ~= held_loading or saving ~= held_saving or stale ~= held_stale then return false end
  if message ~= held_message or read_error ~= held_read_error or inspected ~= held_inspected or chosen ~= held_chosen then return false end
  local ready = art and art.ready and true or false
  if ready ~= held_art or data ~= held_data or machine ~= held_machine then return false end
  if (room.overlay and true or false) ~= held_overlay then return false end
  return true
end
local function remember_scene()
  held_w, held_h = room.width, room.height
  held_focus, held_page = focus, page
  held_inspected, held_chosen = inspected, chosen
  held_loading, held_saving, held_stale = loading, saving, stale
  held_message, held_read_error = message, read_error
  held_art = art and art.ready and true or false
  held_data, held_machine = data, machine
  held_overlay = overlay
end
local function remember()
  store.set("navigation",{game_id=chosen,inspected=inspected,focus=focus,page=page})
end
local function current_session()
  return data and data.session
end
local function running()
  local s = current_session()
  return s and s.state == "active"
end
local function current()
  return running() and machine and current_session().game_id == machine.game_id
end
local function label(id)
  if not id or id == "" then return "Empty socket" end
  for _,c in ipairs(machine and machine.choices or {}) do if c.expansion_id == id then return c.label end end
  return "Expansion "..id:sub(1,8)
end
local function choice()
  for _,c in ipairs(machine and machine.choices or {}) do if c.expansion_id == inspected then return c end end
end
local function pending()
  local launch_package=machine and machine.launch_package_id ~= "" and machine.launch_package_id or machine and machine.package_id
  return current() and current_session().hardware_known and (current_session().package_id ~= launch_package or (current_session().expansion_id or "") ~= machine.draft_expansion_id)
end
local function read()
  if loading or saving then refresh_pending=true return end
  refresh_pending=false
  loading = true
  last_read = room.time
  hardware.read(function(value,err)
    loading = false
    if refresh_pending then
      if not saving then read() end
      return
    end
    if err then
      stale=true read_error="Cannot refresh the host. "..err
    else
      stale=false read_error="" data=value
      machine=nil
      local list=value.machines or {}
      for _,m in ipairs(list) do if m.game_id == chosen then machine=m end end
      if not machine and value.session then
        for _,m in ipairs(list) do if m.game_id == value.session.game_id then machine=m end end
      end
      if not machine then machine=list[1] end
      if machine then
        chosen=machine.game_id
        if not choice() then inspected=machine.draft_expansion_id end
        page=math.max(1,math.min(page,math.ceil(math.max(1,#machine.choices)/3)))
      elseif initial_selection_pending then
        read_error="The selected "..model().label.." setup is no longer available."
      end
      initial_selection_pending=false
      if message == opening_message then message="Choose a card to see what it adds." end
    end
  end)
end
local function save(id)
  if not machine or stale or loading or saving then return end
  saving=true
  message="Saving your hardware setup..."
  hardware.select_expansion({game_id=machine.game_id,package_id=machine.package_id,expected_expansion_id=machine.draft_expansion_id,expansion_id=id},function(value,err)
    saving=false
    if err then message="Setup was not confirmed. Refreshing; please choose again. "..err
    else message="Setup saved on the host. Running memory is not saved." end
    -- A failed or ambiguous write is never replayed. Read the host again.
    read()
  end)
end
local function next_machine()
  if not data or #data.machines < 2 then return end
  local i=1
  for n,m in ipairs(data.machines) do if m.game_id == chosen then i=n end end
  machine=data.machines[i % #data.machines + 1]
  chosen=machine.game_id inspected=machine.draft_expansion_id page=1
  remember()
end

function load()
  destination.clear()
  local nav=store.get("navigation",{})
  chosen=initial_game_id ~= "" and initial_game_id or nav.game_id or ""
  initial_selection_pending=initial_game_id ~= ""
  inspected=nav.inspected or "" focus=nav.focus or "socket" page=nav.page or 1
  art=image.load("assets/zx81.png")
  read()
end
function on_resume() read() end
function unload() remember() end
function update(dt)
  if room.time-last_read >= 15 then read() end
end
local function activate(id)
  for _,c in ipairs(controls) do
    if c.id == id then
      focus=id remember()
      if c.enabled then c.fn() end
      return
    end
  end
end
function on_hover(id)
  for _,c in ipairs(controls) do if c.id == id then focus=id remember() return end end
end
function on_activate(id) activate(id) end
function on_input(cmd)
  if cmd == "select" then activate(focus) return true end
  if cmd == "details" then message="Choose a card on the shelf. Its description appears beside the machine." return true end
  local index=1
  for i,c in ipairs(controls) do if c.id == focus then index=i end end
  if #controls == 0 then return false end
  if cmd == "tab" or cmd == "tab_prev" then
    index=(index-1+(cmd == "tab" and 1 or -1)) % #controls + 1
    focus=controls[index].id remember() return true
  end
  local dx,dy=0,0
  if cmd == "left" then dx=-1 elseif cmd == "right" then dx=1 elseif cmd == "up" then dy=-1 elseif cmd == "down" then dy=1 else return false end
  local here,best,score=controls[index],nil,math.huge
  for _,c in ipairs(controls) do
    local x,y=c.x-here.x,c.y-here.y
    local forward=x*dx+y*dy
    if forward > 1 then
      local cross=math.abs(dx ~= 0 and y or x)
      local distance=forward+cross*3
      if distance < score then score=distance best=c end
    end
  end
  if best then focus=best.id remember() end
  return true
end

-- A card on the rear connector. "fitted" is a ready saved card. "unavailable"
-- is a saved selection the host will not admit, in the shelf's muted colour.
-- "preview" is an unsaved ready card, outlined and labelled.
local function connector_card(x,y,style)
  if style == "fitted" then
    rect(x,y,30,32,C.card) rect(x+4,y+27,22,6,C.brass)
  elseif style == "unavailable" then
    rect(x,y,30,32,C.muted) rect(x+4,y+27,22,6,C.brass)
  else
    outline(x,y,30,32,2,C.card) rect(x+4,y+27,22,6,C.raised)
  end
end

-- Cold opens used to build this whole picture inside the first draw, which
-- sat on the kit's 48ms hard budget. boot spreads that work:
-- 0 opening frame, 1 row cache and text metrics, 2 upper half, 3 lower half.
-- Focus, size, status or selection can change between those halves. The
-- upper half is painted again before the lower half is appended.
local boot = 0
local warm_px, warm_at, desc_warm
local pool_rows = 160
local function warm_done()
  return desc_warm and warm_px and warm_at > #warm_px
end
local function warm_more()
  if not warm_px then
    warm_px = {}
    warm_at = 1
    local seen = {}
    local function add(px, bold)
      px = math.max(12, math.floor(px))
      local key = px..(bold and "b" or "n")
      if seen[key] then return end
      seen[key] = true
      warm_px[#warm_px+1] = {px, bold and true or false}
    end
    for _, s in ipairs({12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 30}) do
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
  -- Wrapping the standing description is its own frame, after the face heights.
  if n == 0 and warm_at > #warm_px and not desc_warm then
    wrapped("Choose an expansion on the shelf. The host checks that it fits this exact machine.", 15, 2, 470)
    desc_warm = true
  end
end
local function paint_opening()
  controls = {}
  recycle()
  appearance()
  clear_row(C.paper)
  rect(24,18,18,5,C.accent)
  text("FES",50,14,24,C.ink,80,true)
  text(workshop_title(),118,17,19,C.ink,400)
  text("Opening the workshop…",24,84,22,C.muted,700)
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
    -- The upper half is already presented. Recycle its rows, then append.
    recycle()
    sink = {}
  end
  local c=choice()
  local saved_id=machine and machine.draft_expansion_id or ""
  local previewing=machine and inspected ~= "" and inspected ~= saved_id
  if part ~= "lower" then

  -- Header: FES identity, this room, and the routes out.
  rect(24,18,18,5,C.accent) rect(24,26,18,5,C.accent) rect(24,34,10,5,C.accent)
  text("FES",50,14,24,C.ink,80,true)
  rect(104,16,2,26,C.line)
  text(workshop_title(),118,14,23,C.ink,360)
  button("back","Back",892,10,96,38,true,function() rooms.back() end)
  button("home","Home",1000,10,96,38,true,function() rooms.home() end)
  rect(0,56,1120,2,C.line)

  text(string.upper(model().label).." SETUP",24,66,13,C.muted,600,true)
  text(machine and machine.title or model().label,24,84,30,C.ink,560,true)
  local info=read_error ~= "" and read_error or (data and data.session_error ~= "" and "Session unavailable. Refresh before controlling the machine." or message)
  if machine and not machine.ready and read_error == "" then info=machine.unavailable_reason end
  -- Status stays under the title so lifecycle consequences never disappear.
  text(info,24,122,18,C.muted,1072)

  -- Machine card: the saved next start stays on the connector. A ready
  -- preview is outlined beside it and never replaces the saved card. Over a
  -- running machine the real one shows through instead of card and artwork.
  if not overlay then rect(24,144,576,296,C.raised) end
  text("Setup preview",40,154,17,C.muted,300)
  text("1 rear connector",432,154,17,C.muted,152)
  if overlay then
    -- No artwork or silhouette: the running machine is behind the room.
  elseif model().id == "fes.zx81" and art and art.ready then
    blit(art,120,186,330,220)
  else
    -- A labelled machine silhouette keeps the room usable without artwork.
    -- One body, screen and keyboard bar: a grid of keys made the cold open
    -- miss the kit's draw budget.
    rect(120,238,360,150,C.ink)
    rect(148,256,196,64,C.panel)
    rect(148,336,300,28,C.raised)
    text(model().mark,360,268,24,C.light,100,true)
    text("Machine illustration unavailable",130,206,15,C.muted,400)
  end
  -- The ZX81 has one rear connector, not numbered internal slots.
  rect(314,198,48,3,C.accent)
  rect(308,195,10,10,C.accent)
  -- Solid means the host still admits the saved card. An unavailable saved
  -- selection keeps its Next start label and uses the muted shelf colour.
  -- Only a ready preview is drawn beside it.
  local preview_ready=previewing and c and c.ready
  local saved_ready=false
  if machine and saved_id ~= "" then
    for _,item in ipairs(machine.choices or {}) do
      if item.expansion_id == saved_id and item.ready then saved_ready=true end
    end
  end
  if saved_id ~= "" then
    connector_card(276,180,saved_ready and "fitted" or "unavailable")
    text("Next start",preview_ready and 276 or 236,166,13,C.ink,preview_ready and 96 or 120,true)
  end
  if preview_ready then
    if saved_id ~= "" then
      connector_card(228,180,"preview")
      text("Preview",164,188,13,C.ink,60,true)
    else
      connector_card(276,180,"preview")
      text("Preview",236,166,13,C.ink,120,true)
    end
  end
  button("socket","Rear expansion",372,182,200,40,machine and machine.socket.supported,function() focus="card:1" end)
  local readonly=data and data.expansion_selection == false
  local save_guidance=readonly and ("Open the "..workshop_title().." on the host computer to choose Fit & save.") or "A saved setup remembers the machine, not its running memory."
  text(save_guidance,40,412,16,C.muted,544)

  -- Setup column: the three states together, then the change and its cost.
  -- Running is an observation. Without a session reading it is unknown,
  -- never "not running". A running machine whose load receipt does not say
  -- what is on the socket is still running; only its setup is unknown.
  local running_line
  local s=current_session()
  if not data then running_line="Running: checking…"
  elseif data.session_error ~= "" or not s then running_line="Running: unknown"
  elseif current() then running_line=s.hardware_known and ("Running: "..label(s.expansion_id)) or "Machine running · setup unavailable"
  elseif running() then running_line="Running: another title"
  elseif s.state == "idle" then running_line="Running: not running"
  else running_line="Running: "..(s.state ~= "" and s.state or "unknown") end
  if stale and data then running_line=running_line.." (may be out of date)" end
  text(running_line,620,146,19,C.ink,476,true)
  text(machine and ("Next start: "..label(saved_id)) or "Next start: no machine setup",620,170,19,C.ink,476,true)
  if previewing then text("Previewing: "..label(inspected).." (not saved)",620,194,16,C.muted,476)
  elseif pending() then text("Restart needed: Stop, then Start, to use Next start",620,194,16,C.ink,476,true) end
  rect(620,220,476,2,C.line)
  text(c and ("REAR CONNECTOR · ONE EXPANSION"..(c.in_progress and " · IN PROGRESS" or "")) or "REAR CONNECTOR · ONE EXPANSION",620,230,15,C.muted,476,true)
  text(c and c.label or "Empty socket",620,248,22,C.ink,476,true)
  local description=c and c.description or "Choose an expansion on the shelf. The host checks that it fits this exact machine."
  if c and not c.ready then description=c.unavailable_reason end
  lines(description,620,278,470,18,2,C.muted)
  -- expansion_selection is this listener's save permission. Content
  -- readiness stays on the socket and the card. An omitted field is an
  -- older unpaired host and keeps the existing save.
  local save_here=not data or data.expansion_selection ~= false
  local can_edit=machine and machine.socket.supported and save_here and not stale and not loading and not saving
  local fit=c and c.ready and c.expansion_id ~= saved_id
  button("fit","Fit & save setup",620,338,300,40,can_edit and fit,function() save(inspected) end,not running())
  button("remove","Remove & save",932,338,164,40,can_edit and saved_id ~= "",function() inspected="" save("") end)
  rect(620,388,476,52,C.warm)
  rect(620,388,5,52,C.warm_edge)
  local setup_notice=pending() and "Restart needed: the running machine still uses its old setup." or (readonly and "Inspect only. This kit cannot save Next start." or "Changing the setup needs a restart.")
  text(setup_notice,634,394,16,C.ink,456,true)
  text("Stopping clears memory. Saving a setup never saves RAM.",634,418,16,C.ink,456)
  end
  if part == "upper" then return end

  -- Expansion shelf: inspecting a card changes only the preview.
  text("Expansion shelf",24,452,21,C.ink,400,true)
  local choices=machine and machine.choices or {}
  local start=(page-1)*3+1
  for n=1,3 do
    local item=choices[start+n-1]
    if item then
      local x=24+(n-1)*250
      local id="card:"..n
      rect(x,480,238,62,inspected == item.expansion_id and C.raised or C.panel)
      if focus == id then outline(x-4,476,246,70,3,C.accent) else outline(x,480,238,62,1,C.line) end
      -- Deliberately generic card art; appearance is not a feature claim.
      rect(x+10,490,30,32,item.ready and C.card or C.muted)
      rect(x+14,517,22,6,C.brass)
      text(item.label,x+50,486,18,C.ink,180,focus == id)
      local card_status=item.expansion_id == saved_id and "Saved for next start" or (item.ready and "Fits this machine" or "Unavailable")
      if inspected == item.expansion_id and item.expansion_id ~= saved_id then card_status="Previewing · "..card_status end
      if item.in_progress then card_status="In progress · "..card_status end
      text(card_status,x+50,513,15,C.muted,180)
      hit(id,x,480,238,62)
      local preview_note="Previewing. Choose Fit & save setup to use it at the next start."
      if data and data.expansion_selection == false then preview_note="Previewing. This kit cannot save it." end
      controls[#controls+1]={id=id,x=x+119,y=511,enabled=true,fn=function() inspected=item.expansion_id message=item.ready and preview_note or item.unavailable_reason remember() end}
    end
  end
  if #choices == 0 then
    text(machine and "RAM, Zon X and QS expansions supported." or ("Add a "..model().label.." setup in the FPGA library to begin."),24,486,19,C.muted,720)
    text("Import a card archive to add it to this shelf.",24,512,15,C.muted,720)
  end
  -- Setup tools: two columns so Direction moves straight between them.
  -- The left column is wide enough for "Import expansion" at kit size,
  -- including the bolder focused face. Refresh and Library stay beside it.
  button("shelf_page","More cards",776,480,188,28,#choices > 3,function() page=page % math.ceil(#choices/3)+1 remember() end)
  button("refresh",loading and "Checking..." or "Refresh",976,480,120,28,not loading and not saving,function() read() end)
  button("import_card","Import expansion",776,514,188,28,machine and machine.socket.supported and not running() and not stale,function() hardware.import_expansion{game_id=machine.game_id,package_id=machine.package_id} end)

  -- The host cassette label, on its own row above the machine controls.
  -- A starter uses its display name. An import is "Imported cassette" plus
  -- a digest prefix; the filename is not on this record. A long label
  -- keeps its start and its end.
  rect(24,546,1072,2,C.line)
  local tape_name=machine and machine.media_name ~= "" and machine.media_name or "No cassette"
  text("Cassette",24,552,15,C.muted,84)
  text(fit_name(tape_name,17,980,true),112,550,17,C.ink,980,true)
  local can_control=not stale and not loading and data and data.session_error == ""
  local can_play=machine and machine.ready and not stale and not loading and not saving and data and data.session and data.session.state == "idle" and data.session_error == ""
  button("power",running() and (current() and "Stop machine" or "Machine in use") or "Start machine",24,576,186,48,(current() and can_control and current_session().hardware_known) or can_play,function()
    if running() then session.stop(current_session())
    elseif machine.launch_package_id and machine.launch_package_id ~= "" then session.launch(machine.game_id,machine.launch_package_id,machine.package_id)
    else session.launch(machine.game_id) end
  end,not previewing and not running())
  button("tape",running() and "Choose / eject tape" or "Choose cassette",222,576,196,48,(current() and can_control and current_session().tape_available) or (machine and machine.ready and data and data.session and data.session.state == "idle" and data.session_error == "" and not stale and not loading and not saving),function()
    if running() then session.open_tape(current_session())
    else hardware.open_tapes{game_id=machine.game_id,core_id=machine.core_id,package_id=machine.package_id,expected_media_id=machine.media_id or ""} end
  end)
  button("resume","Return to play",430,576,164,48,running(),function() session.resume() end,running())
  button("setup","Next setup",606,576,128,48,data and #data.machines > 1,function() next_machine() end)
  button("setup_library","Set up "..model().mark,746,576,170,48,not running(),function() hardware.setup{core_id=model().id} end)
  button("library","Library",976,514,120,28,not running(),function() rooms.open_library{platform="zx81"} end)
  if part == "lower" then
    gfx.extend(sink)
    for i = 1, #sink do cmds[#cmds+1] = sink[i] end
    sink = nil
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
      for _ = 1, add do pool[#pool+1] = {} end
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
    -- remember_scene records the inputs this half used. The next draw
    -- appends the lower half only while those inputs still match.
    remember_scene()
    boot = 3
    return
  end
  if not scene_same() then
    boot = 2
    if not same_appearance() then
      -- Never keep the upper half over a machine it was not painted for.
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
function draw()
  -- The kit draw callback has a 12ms soft budget and a 48ms hard budget.
  -- After the first picture, rebuild only when its inputs change.
  scale=math.min(room.width/1120,room.height/630)
  ox=(room.width-1120*scale)/2 oy=(room.height-630*scale)/2
  if boot == 4 and scene_same() and room.presented and gfx.reuse() then return end
  if boot < 4 then
    boot_step()
    return
  end
  picture("all")
  gfx.paint(cmds)
  remember_scene()
end
