-- Hardware facts and saved selections come from the host. Only navigation
-- belongs in store. A saved setup is the next launch, never a RAM snapshot.
-- Lounge seed roles (docs/ux/visual-language.md). "warm" carries setup
-- consequences; focus is an accent outline, never colour alone.
local C = {paper="#F4F0E8", panel="#FFFCF5", raised="#E9E6DC", ink="#243631", muted="#53625A", line="#CECFC0", accent="#235A46", light="#FCFCF3", warm="#F6EBCB", warm_edge="#D9B45A", card="#5C7A6B", brass="#B69252"}
local data, machine, art
local chosen, inspected = "", ""
local focus = "socket"
local controls = {}
local loading, saving, stale = false, false, true
local message, read_error = "Finding your Sinclair ZX81 setups...", ""
local last_read, page = 0, 1
local scale, ox, oy = 1, 0, 0

local function rect(x,y,w,h,c) gfx.rect(ox+x*scale,oy+y*scale,w*scale,h*scale,c) end
local function text(s,x,y,size,c,w,bold)
  gfx.text(s,ox+x*scale,oy+y*scale,{size=math.max(12,math.floor(size*scale)),color=c or C.ink,max_w=(w or 0)*scale,bold=bold or false})
end
-- Keep a filename inside w logical pixels. A name that does not fit keeps
-- its start and its end, so two tapes that share a long prefix stay distinct.
local function fit_name(s, size, w, bold)
  s = s or ""
  local px = math.max(12, math.floor(size * scale))
  local limit = w * scale
  if gfx.measure(s, px, bold) <= limit then return s end
  local chars = {}
  for ch in s:gmatch("[%z\1-\127\194-\244][\128-\191]*") do chars[#chars+1] = ch end
  if #chars < 2 then return s end
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
  return best
end
local function lines(s,x,y,w,size,max_lines,c)
  local line, n = "", 0
  for word in (s or ""):gmatch("%S+") do
    local next_line = line == "" and word or line.." "..word
    if gfx.measure(next_line,math.floor(size*scale)) > w*scale and line ~= "" then
      if n + 1 >= max_lines then
        -- Last allowed line: say that the text continues.
        text(line.." …",x,y+n*(size+5),size,c,w)
        return
      end
      text(line,x,y+n*(size+5),size,c,w)
      n = n + 1
      line = word
    else line = next_line end
  end
  if line ~= "" then text(line,x,y+n*(size+5),size,c,w) end
end
local function outline(x,y,w,h,t,c)
  rect(x,y,w,t,c) rect(x,y+h-t,w,t,c) rect(x,y,t,h,c) rect(x+w-t,y,t,h,c)
end
local function button(id,label,x,y,w,h,enabled,fn,accent)
  local selected = id == focus
  local filled = accent and enabled
  rect(x,y,w,h,filled and C.accent or C.panel)
  if selected then outline(x-4,y-4,w+8,h+8,3,C.accent) elseif not filled then outline(x,y,w,h,1,C.line) end
  local size=math.max(12,math.floor(math.min(19,h-10)*scale))
  local bold=selected or filled
  local _,th=gfx.measure(label,size,bold)
  gfx.text(label,ox+(x+8)*scale,oy+y*scale+(h*scale-th)/2,{size=size,bold=bold,color=filled and C.light or (enabled and C.ink or C.muted),max_w=(w-16)*scale,align="center"})
  gfx.hit(id,ox+x*scale,oy+y*scale,w*scale,h*scale)
  controls[#controls+1] = {id=id,x=x+w/2,y=y+h/2,fn=fn,enabled=enabled}
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
  return current() and current_session().hardware_known and (current_session().package_id ~= machine.package_id or (current_session().expansion_id or "") ~= machine.draft_expansion_id)
end
local function read()
  if loading or saving then return end
  loading = true
  last_read = room.time
  hardware.read(function(value,err)
    loading = false
    if err then stale=true read_error="Cannot refresh the host. "..err return end
    stale=false read_error="" data=value
    machine=nil
    local list=value.machines or {}
    for _,m in ipairs(list) do if m.game_id == chosen then machine=m end end
    if not machine and value.session then
      for _,m in ipairs(list) do if m.game_id == value.session.game_id then machine=m end end
    end
    machine=machine or list[1]
    if machine then
      chosen=machine.game_id
      if not choice() then inspected=machine.draft_expansion_id end
      page=math.max(1,math.min(page,math.ceil(math.max(1,#machine.choices)/3)))
    end
    if message == "Finding your Sinclair ZX81 setups..." then message="Choose a card to see what it adds." end
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
  chosen=nav.game_id or "" inspected=nav.inspected or "" focus=nav.focus or "socket" page=nav.page or 1
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

-- A card on the rear connector: solid when saved, outlined and labelled
-- when it is only being previewed.
local function connector_card(x,y,saved)
  if saved then
    rect(x,y,30,32,C.card) rect(x+4,y+27,22,6,C.brass)
  else
    outline(x,y,30,32,2,C.card) rect(x+4,y+27,22,6,C.raised)
  end
end

function draw()
  scale=math.min(room.width/1120,room.height/630)
  ox=(room.width-1120*scale)/2 oy=(room.height-630*scale)/2
  controls={}
  gfx.clear(C.paper)
  local c=choice()
  local saved_id=machine and machine.draft_expansion_id or ""
  local previewing=machine and inspected ~= "" and inspected ~= saved_id

  -- Header: FES identity, this room, and the routes out.
  rect(24,18,18,5,C.accent) rect(24,26,18,5,C.accent) rect(24,34,10,5,C.accent)
  text("FES",50,14,24,C.ink,80,true)
  rect(104,16,2,26,C.line)
  text("ZX81 Workshop",118,17,19,C.ink,300)
  button("back","Back",892,10,96,38,true,function() rooms.back() end)
  button("home","Home",1000,10,96,38,true,function() rooms.home() end)
  rect(0,56,1120,2,C.line)

  text("THE SINCLAIR ZX81 WORKBENCH",24,66,13,C.muted,600,true)
  text(machine and machine.title or "Sinclair ZX81",24,84,30,C.ink,560,true)
  local info=read_error ~= "" and read_error or (data and data.session_error ~= "" and "Session unavailable. Refresh before controlling the machine." or message)
  if machine and not machine.ready and read_error == "" then info=machine.unavailable_reason end
  -- Status stays under the title so lifecycle consequences never disappear.
  text(info,24,122,15,C.muted,1072)

  -- Machine card: the saved next start stays on the connector. A ready
  -- preview is outlined beside it and never replaces the saved card.
  rect(24,144,576,296,C.raised)
  text("Setup preview",40,154,15,C.muted,300)
  text("1 rear connector",440,154,15,C.muted,144)
  if art and art.ready then
    gfx.image(art,ox+120*scale,oy+186*scale,330*scale,220*scale)
  else
    -- A labelled machine silhouette keeps the room usable without artwork.
    rect(120,238,360,150,C.ink)
    for row=0,3 do for col=0,9 do rect(140+col*33,276+row*25,28,20,C.panel) end end
    text("ZX81",402,246,24,C.light,70,true)
    text("Machine illustration unavailable",130,206,15,C.muted,400)
  end
  -- The ZX81 has one rear connector, not numbered internal slots.
  rect(314,198,48,3,C.accent)
  rect(308,195,10,10,C.accent)
  -- An unavailable card cannot be fitted, so only a ready preview is drawn.
  local preview_ready=previewing and c and c.ready
  if saved_id ~= "" then
    connector_card(276,180,true)
    text("Next start",preview_ready and 276 or 236,166,13,C.ink,preview_ready and 96 or 120,true)
  end
  if preview_ready then
    if saved_id ~= "" then
      connector_card(228,180,false)
      text("Preview",164,188,13,C.ink,60,true)
    else
      connector_card(276,180,false)
      text("Preview",236,166,13,C.ink,120,true)
    end
  end
  button("socket","Rear expansion",372,182,200,40,machine and machine.socket.supported,function() focus="card:1" end)
  text("A saved setup remembers the machine, not its running memory.",40,412,15,C.muted,544)

  -- Setup column: the three states together, then the change and its cost.
  -- Running is an observation. Without a session reading it is unknown,
  -- never "not running".
  local running_line
  local s=current_session()
  if not data then running_line="Running: checking…"
  elseif data.session_error ~= "" or not s then running_line="Running: unknown"
  elseif current() then running_line="Running: "..(s.hardware_known and label(s.expansion_id) or "Hardware unavailable")
  elseif running() then running_line="Running: another title"
  elseif s.state == "idle" then running_line="Running: not running"
  else running_line="Running: "..(s.state ~= "" and s.state or "unknown") end
  if stale and data then running_line=running_line.." (may be out of date)" end
  text(running_line,620,146,17,C.ink,476,true)
  text(machine and ("Next start: "..label(saved_id)) or "Next start: no machine setup",620,170,17,C.ink,476,true)
  if previewing then text("Previewing: "..label(inspected).." (not saved)",620,194,16,C.muted,476)
  elseif pending() then text("Restart needed: Stop, then Start, to use Next start",620,194,16,C.ink,476,true) end
  rect(620,220,476,2,C.line)
  text(c and ("REAR CONNECTOR · ONE EXPANSION"..(c.in_progress and " · IN PROGRESS" or "")) or "REAR CONNECTOR · ONE EXPANSION",620,230,13,C.muted,476,true)
  text(c and c.label or "Empty socket",620,248,22,C.ink,476,true)
  lines(c and c.description or "Choose an expansion on the shelf. The host checks that it fits this exact machine.",620,278,470,15,2,C.muted)
  if c and not c.ready then text(c.unavailable_reason,620,318,14,C.ink,476,true) end
  local can_edit=machine and machine.socket.supported and not stale and not loading and not saving
  local fit=c and c.ready and c.expansion_id ~= saved_id
  button("fit","Fit & save setup",620,338,300,40,can_edit and fit,function() save(inspected) end,true)
  button("remove","Remove & save",932,338,164,40,can_edit and saved_id ~= "",function() inspected="" save("") end)
  rect(620,388,476,52,C.warm)
  rect(620,388,5,52,C.warm_edge)
  text(pending() and "Restart needed: the running machine still uses its old setup." or "Changing the setup needs a restart.",634,394,15,C.ink,456,true)
  text("Stopping clears memory. Saving a setup never saves RAM.",634,416,15,C.ink,456)

  -- Expansion shelf: inspecting a card changes only the preview.
  text("Expansion shelf",24,452,19,C.ink,400,true)
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
      text(item.label,x+50,488,16,C.ink,180,focus == id)
      local card_status=item.expansion_id == saved_id and "Saved for next start" or (item.ready and "Fits this machine" or "Unavailable")
      if inspected == item.expansion_id and item.expansion_id ~= saved_id then card_status="Previewing · "..card_status end
      if item.in_progress then card_status="In progress · "..card_status end
      text(card_status,x+50,514,13,C.muted,180)
      gfx.hit(id,ox+x*scale,oy+480*scale,238*scale,62*scale)
      controls[#controls+1]={id=id,x=x+119,y=511,enabled=true,fn=function() inspected=item.expansion_id message=item.ready and "Previewing. Choose Fit & save setup to use it at the next start." or item.unavailable_reason remember() end}
    end
  end
  if #choices == 0 then
    text(machine and "RAM, Zon X and QS expansions supported." or "Add a Sinclair ZX81 setup in the FPGA library to begin.",24,486,19,C.muted,720)
    text("Import a card archive to add it to this shelf.",24,512,15,C.muted,720)
  end
  -- Setup tools: two columns so Direction moves straight between them.
  button("shelf_page","More cards",776,480,150,28,#choices > 3,function() page=page % math.ceil(#choices/3)+1 remember() end)
  button("refresh",loading and "Checking..." or "Refresh",938,480,158,28,not loading and not saving,function() read() end)
  button("import_card","Import expansion",776,514,150,28,machine and machine.socket.supported and not running() and not stale,function() hardware.import_expansion{game_id=machine.game_id,package_id=machine.package_id} end)

  -- Cassette name on its own row. The machine controls sit below it, so a
  -- long imported filename has the width of the row rather than the corner.
  rect(24,546,1072,2,C.line)
  local tape_name=machine and machine.media_name ~= "" and machine.media_name or "No cassette"
  text("Cassette",24,552,13,C.muted,84)
  text(fit_name(tape_name,14,980,true),112,550,14,C.ink,980,true)
  local can_control=not stale and not loading and data and data.session_error == ""
  local can_play=machine and machine.ready and not stale and not loading and not saving and data and data.session and data.session.state == "idle" and data.session_error == ""
  button("power",running() and (current() and "Stop machine" or "Machine in use") or "Start machine",24,576,186,48,(current() and can_control and current_session().hardware_known) or can_play,function()
    if running() then session.stop(current_session()) else session.launch(machine.game_id) end
  end,true)
  button("tape",running() and "Choose / eject tape" or "Choose cassette",222,576,196,48,(current() and can_control and current_session().tape_available) or (machine and machine.ready and data and data.session and data.session.state == "idle" and data.session_error == "" and not stale and not loading and not saving),function()
    if running() then session.open_tape(current_session())
    else hardware.open_tapes{game_id=machine.game_id,package_id=machine.package_id,expected_media_id=machine.media_id or ""} end
  end)
  button("resume","Return to play",430,576,164,48,running(),function() session.resume() end)
  button("setup","Next setup",606,576,128,48,data and #data.machines > 1,function() next_machine() end)
  button("setup_library","Set up ZX81",746,576,170,48,not running(),function() hardware.setup() end)
  button("library","Library",938,514,158,28,not running(),function() rooms.open_library{platform="zx81"} end)
end
