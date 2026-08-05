package hostapi

import "net/http"

func UIHTMLForTest() string { return uiHTML }

func UIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(uiHTML))
	})
}

const uiHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>FogCast</title><style>
:root{color-scheme:dark;font:15px system-ui;background:#101318;color:#e8edf2}body{margin:0}header{padding:20px 28px;border-bottom:1px solid #29313b;display:flex;justify-content:space-between}main{display:grid;grid-template-columns:minmax(300px,1fr) 320px;gap:20px;padding:24px;max-width:1200px;margin:auto}input,button{background:#1a222c;color:inherit;border:1px solid #3a4653;border-radius:6px;padding:9px 12px}button{cursor:pointer}button:hover{border-color:#7aa2d6}.toolbar{display:flex;gap:8px;margin-bottom:14px}.games{display:grid;gap:8px}.game{background:#171d25;border:1px solid #29313b;border-radius:8px;padding:14px;cursor:pointer}.game:hover,.game.selected{border-color:#7aa2d6}.muted{color:#9ba8b5}.panel{background:#171d25;border:1px solid #29313b;border-radius:8px;padding:18px;height:max-content}.error{color:#ff9a9a}.ok{color:#9fe3ae}
</style></head><body><header><strong>FogCast</strong><span id="health" class="muted">connecting…</span></header><main><section><div class="toolbar"><input id="q" placeholder="Search games" autocomplete="off"><button id="refresh">Refresh</button></div><div id="games" class="games"></div></section><aside id="detail" class="panel"><span class="muted">Select a game</span></aside></main><script>
const $=id=>document.getElementById(id);let selected=null;
async function api(path,opts){const r=await fetch(path,opts);const j=await r.json();if(!r.ok){const e=new Error(j.error?.message||'request failed');e.code=j.error?.code||'REQUEST_FAILED';throw e}return j}
function renderGames(games){$('games').innerHTML='';for(const g of games){const el=document.createElement('div');el.className='game'+(selected?.id===g.id?' selected':'');el.innerHTML='<strong>'+esc(g.title)+'</strong><div class="muted">'+esc(g.system)+' · '+esc(g.execution)+'</div>';el.onclick=()=>select(g);$('games').appendChild(el)}}
function esc(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
async function load(){try{const q=$('q').value.trim();const data=await api('/api/v1/games'+(q?'?q='+encodeURIComponent(q):''));renderGames(data.games);$('health').textContent='host ready';$('health').className='ok'}catch(e){$('health').textContent=e.message;$('health').className='error'}}
async function select(g){selected=g;const d=await api('/api/v1/games/'+encodeURIComponent(g.id));$('detail').innerHTML='<h2>'+esc(d.title)+'</h2><p class="muted">'+esc(d.system)+' · '+esc(d.execution)+'</p><p>'+esc(d.state)+(d.root_online?' · source online':' · source offline')+'</p><button id="launch">Launch</button>';$('launch').onclick=launch}
async function launch(){if(!selected)return;try{const r=await api('/api/v1/session/launch',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({game_id:selected.id})});$('detail').insertAdjacentHTML('beforeend','<p class="ok">'+esc(r.state)+' — '+esc(r.progress?.message||'done')+'</p>')}catch(e){const msg=e.code==='TARGET_UNAVAILABLE'||e.code==='MISTER_UNAVAILABLE'?'MiSTer connection unavailable — start the development tunnel and retry.':e.message;$('detail').insertAdjacentHTML('beforeend','<p class="error">'+esc(msg)+'</p>')}}
$('refresh').onclick=load;$('q').oninput=load;load();
</script></body></html>`
