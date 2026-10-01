-- Hardware facts and saved selections come from the host. Only navigation
-- belongs in store. A saved setup is the next launch, never a RAM snapshot.
local C = {paper="#eee8dc", panel="#faf7ef", ink="#272d29", muted="#62665e", line="#d4cec0", red="#a43e31", green="#43684f", light="#ffffff"}
local data, machine, art
local chosen, inspected = "", ""
local focus = "socket"
local controls = {}
local loading, saving, stale = false, false, true
local message, read_error = "Finding your ZX81 setups...", ""
local last_read, page = 0, 1
local scale, ox, oy = 1, 0, 0

local function rect(x,y,w,h,c) gfx.rect(ox+x*scale,oy+y*scale,w*scale,h*scale,c) end
local function text(s,x,y,size,c,w,bold)
  gfx.text(s,ox+x*scale,oy+y*scale,{size=math.max(12,math.floor(size*scale)),color=c or C.ink,max_w=(w or 0)*scale,bold=bold or false})
end
local function lines(s,x,y,w,size,max_lines,c)
  local line, n = "", 0
  for word in (s or ""):gmatch("%S+") do
    local next_line = line == "" and word or line.." "..word
    if gfx.measure(next_line,math.floor(size*scale)) > w*scale and line ~= "" then
      text(line,x,y+n*(size+5),size,c,w)
      n = n + 1
      if n >= max_lines then return end
      line = word
    else line = next_line end
  end
  if line ~= "" then text(line,x,y+n*(size+5),size,c,w) end
end
local function button(id,label,x,y,w,h,enabled,fn,accent)
  local selected = id == focus
  rect(x-2,y-2,w+4,h+4,selected and C.red or C.line)
  rect(x,y,w,h,accent and enabled and C.green or C.panel)
  text(label,x+12,y+(h-22)/2,19,accent and enabled and C.light or (enabled and C.ink or C.muted),w-24,true)
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
    if message == "Finding your ZX81 setups..." then message="Choose a card to see what it adds." end
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

function draw()
  scale=math.min(room.width/1120,room.height/630)
  ox=(room.width-1120*scale)/2 oy=(room.height-630*scale)/2
  controls={}
  gfx.clear(C.paper)
  text("HARDWARE ROOM",24,12,15,C.red,600,true)
  text("The Zx81 workbench",24,34,34,C.ink,760,true)
  text(machine and ("Your setup: "..machine.title) or "A small machine. Room to experiment.",24,76,18,C.muted,760)
  button("setup_library","Set up Zx81",710,74,176,28,not running(),function() hardware.setup() end)
  button("back","Back",884,25,90,44,true,function() rooms.back() end)
  button("home","Home",988,25,108,44,true,function() rooms.home() end)

  rect(24,126,664,283,C.panel)
  if art and art.ready then
    gfx.image(art,ox+100*scale,oy+130*scale,345*scale,230*scale)
  else
    -- A labelled machine silhouette keeps the room usable without artwork.
    rect(138,212,396,161,C.ink)
    for row=0,3 do for col=0,9 do rect(162+col*34,253+row*25,29,20,C.panel) end end
    text("ZX81",440,219,26,C.light,90,true)
    text("Machine illustration unavailable",152,185,16,C.muted,410)
  end
  -- The ZX81 has a rear connector, not internal numbered card slots.
  rect(336,163,46,3,C.red)
  rect(329,160,10,10,C.red)
  if machine and machine.draft_expansion_id ~= "" then
    rect(311,137,26,28,C.green)
    rect(314,160,20,6,"#b69252")
  end
  button("socket","Rear expansion",382,134,220,46,machine and machine.socket.supported,function() focus="card:1" end)
  rect(24,355,664,54,C.panel)
  text("Save setup saves hardware choices, not running memory.",39,361,17,C.muted,626)
  text("Stopping clears memory. Stop, then start to apply changes.",39,384,17,C.muted,626)

  rect(710,126,386,283,C.panel)
  local c=choice()
  text(c and ("ON THE SHELF"..(c.in_progress and " · In progress" or "")) or "YOUR MACHINE",730,138,14,C.red,340,true)
  text(c and c.label or "Zx81",730,160,25,C.ink,345,true)
  lines(c and c.description or "Choose an expansion for the rear connector. The host checks that it fits this exact machine.",730,196,341,18,4,C.muted)
  local can_edit=machine and machine.socket.supported and not stale and not loading and not saving
  local fit=c and c.ready and c.expansion_id ~= machine.draft_expansion_id
  button("fit","Fit & save setup",730,299,345,44,can_edit and fit,function() save(inspected) end,true)
  button("remove","Remove & save setup",730,354,345,38,can_edit and machine.draft_expansion_id ~= "",function() save("") end)
  if c and not c.ready then text(c.unavailable_reason,730,275,15,C.red,341) end

  text("EXPANSION SHELF",24,423,15,C.red,550,true)
  local choices=machine and machine.choices or {}
  local start=(page-1)*3+1
  for n=1,3 do
    local item=choices[start+n-1]
    if item then
      local x=24+(n-1)*292
      rect(x,448,278,67,inspected == item.expansion_id and C.line or C.panel)
      -- Deliberately generic card art; appearance is not a feature claim.
      rect(x+11,461,40,31,item.ready and C.green or C.muted)
      rect(x+16,489,30,6,"#b69252")
      text(item.label,x+63,458,17,C.ink,204,true)
      local card_status=item.expansion_id == (machine and machine.draft_expansion_id) and "Saved for next start" or (item.ready and "Fits this machine" or "Unavailable")
      if item.in_progress then card_status="In progress · "..(item.expansion_id == machine.draft_expansion_id and "Saved" or (item.ready and "Fits" or "Unavailable")) end
      text(card_status,x+63,484,14,C.muted,204)
      local id="card:"..n
      if focus == id then rect(x,513,278,3,C.red) end
      gfx.hit(id,ox+x*scale,oy+448*scale,278*scale,67*scale)
      controls[#controls+1]={id=id,x=x+139,y=481,enabled=true,fn=function() inspected=item.expansion_id message=item.ready and "Inspect the card, then choose Fit & save setup." or item.unavailable_reason remember() end}
    end
  end
  if #choices == 0 then
    text(machine and "RAM, Zon X and QS expansions supported." or "Add a ZX81 setup in the FPGA library to begin.",24,452,21,C.muted,840)
    text("Import a card archive to add it to this shelf.",24,485,17,C.muted,840)
  end
  button("refresh",loading and "Checking..." or "Refresh",912,443,184,26,not loading and not saving,function() read() end)
  button("import_card","Import expansion",912,477,184,26,machine and machine.socket.supported and not running() and not stale,function() hardware.import_expansion{game_id=machine.game_id,package_id=machine.package_id} end)
  button("shelf_page","More cards",912,511,184,20,#choices > 3,function() page=page % math.ceil(#choices/3)+1 remember() end)

  rect(24,532,1072,2,C.line)
  local status="No machine setup"
  if machine then
    status="Next start: "..label(machine.draft_expansion_id)
    if current() then status=status.."  /  Running: "..(current_session().hardware_known and label(current_session().expansion_id) or "Hardware unavailable") end
  end
  if pending() then status=status.."  ·  Restart needed" end
  text(status,24,543,17,C.ink,674,true)
  text("Cassette: "..(machine and machine.media_name ~= "" and machine.media_name or "No cassette"),710,543,17,C.ink,386,true)
  local can_control=not stale and not loading and data and data.session_error == ""
  local can_play=machine and machine.ready and not stale and not loading and not saving and data and data.session and data.session.state == "idle" and data.session_error == ""
  button("power",running() and (current() and "Stop machine" or "Machine in use") or "Start machine",24,574,194,42,(current() and can_control and current_session().hardware_known) or can_play,function()
    if running() then session.stop(current_session()) else session.launch(machine.game_id) end
  end,true)
  button("tape",running() and "Choose / eject tape" or "Choose cassette",232,574,232,42,(current() and can_control and current_session().tape_available) or (machine and machine.ready and data and data.session and data.session.state == "idle" and data.session_error == "" and not stale and not loading and not saving),function()
    if running() then session.open_tape(current_session())
    else hardware.open_tapes{game_id=machine.game_id,package_id=machine.package_id,expected_media_id=machine.media_id or ""} end
  end)
  button("resume","Return to play",478,574,183,42,running(),function() session.resume() end)
  button("setup","Next setup",675,574,164,42,data and #data.machines > 1,function() next_machine() end)
  button("library","Library",853,574,117,42,not running(),function() rooms.open_library{platform="zx81"} end)
  local info=read_error ~= "" and read_error or (data and data.session_error ~= "" and "Session unavailable. Refresh before controlling the machine." or message)
  if machine and not machine.ready and read_error == "" then info=machine.unavailable_reason end
  -- Status lives above the shelf so lifecycle consequences never disappear.
  text(info,24,103,14,C.muted,1070)
end
