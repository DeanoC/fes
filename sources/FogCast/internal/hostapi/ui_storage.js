/* Saved host drives. The kit's own drives are managed in its tenfoot settings. */
(function(root) {
  'use strict';
  const base = '/api/v1/storage/drives';
  const driveURL = (id, suffix, path) => base + '/' + encodeURIComponent(id) + '/' + suffix + '?path=' + encodeURIComponent(path || '.');
  function createClient(fetchImpl) {
    async function request(path, options = {}, binary = false) {
      const abort = new AbortController();
      const timer = setTimeout(() => abort.abort(), 30000);
      try {
        const response = await fetchImpl(path, {...options, redirect:'error', signal:abort.signal});
        if (!response.ok) throw new Error('Drive unavailable. Check the drive, share and credentials.');
        if (response.status === 204) return null;
        if (binary) {
          const size = Number(response.headers.get('Content-Length'));
          if (!Number.isSafeInteger(size) || size < 1 || size > 32 * 1024 * 1024) throw new Error('Choose a file of 1–32 MiB.');
          const blob = await response.blob();
          if (blob.size !== size) throw new Error('Drive file changed or transfer was interrupted.');
          return blob;
        }
        return await response.json();
      } finally { clearTimeout(timer); }
    }
    return {
      list: () => request(base + '?discover=true'),
      save: drive => request(base, {method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(drive)}),
      forget: id => request(base + '/' + encodeURIComponent(id), {method:'DELETE'}),
      entries: async (id, path) => (await request(driveURL(id,'entries',path))).entries,
      file: (id, path) => request(driveURL(id,'file',path), {}, true),
    };
  }
  let picker;
  function mount(document, fetchImpl) {
    const launch = document.getElementById('storage-manage');
    if (!launch) return;
    const api = createClient(fetchImpl);
    const dialog = document.createElement('dialog'); dialog.id='storage-dialog'; dialog.className = 'settings-panel';
    const title = document.createElement('h2'); title.textContent = 'FogCast host drives';
    const content = document.createElement('div');
    const status = document.createElement('p'); status.setAttribute('role','status');
    const close = document.createElement('button'); close.type='button'; close.className='button secondary'; close.textContent='Close';
    dialog.append(title,content,status,close); document.body.append(dialog);
    let busy=false, epoch=0, choose=null;
    function button(text, fn) {
      const el=document.createElement('button'); el.type='button'; el.className='button secondary'; el.textContent=text;
      el.disabled=busy; el.addEventListener('click',() => { if (!busy) void fn(); }); return el;
    }
    function line(text) { const el=document.createElement('p'); el.textContent=text; return el; }
    async function action(fn) {
      if (busy) return;
      const gen=epoch; busy=true; status.textContent='Connecting…';
      for (const el of content.querySelectorAll('button,input')) el.disabled=true;
      try { await fn(gen); if (gen===epoch) status.textContent=''; }
      catch(error) { if (gen===epoch) status.textContent=String(error.message || error); }
      finally { busy=false; for (const el of content.querySelectorAll('button,input')) el.disabled=false; }
    }
    function dismiss() { ++epoch; choose=null; dialog.close(); launch.focus(); }
    close.addEventListener('click',dismiss);
    dialog.addEventListener('cancel',event => { event.preventDefault(); dismiss(); });
    async function inventory() {
      await action(async gen => {
        const result=await api.list(); if (gen!==epoch) return;
        content.replaceChildren(line(choose ? 'Choose a host USB drive or saved SMB share.' : 'USB volumes are remembered by UUID. SMB shares reconnect when opened.'));
        if (!choose) content.append(button('Add SMB share',() => edit({kind:'smb',source:'',label:''})));
        content.append(button('Refresh drives',inventory));
        for (const d of result.drives) content.append(button(d.label + ' · ' + d.kind.toUpperCase() + (d.saved ? '' : ' · remember'),async () => {
          if (choose) {
            let saved=d.saved ? d : null;
            if(!saved) await action(async gen => { const value=await api.save(d);if(gen===epoch)saved=value; });
            if(saved && choose) await browse(saved, choose.purpose==='bios' ? saved.bios_folder : saved.media_folder);
          } else if (d.saved) edit(d);
          else await action(async current => { const saved=await api.save(d); if(current===epoch) edit(saved); });
        }));
        if (result.notice) content.append(line(result.notice));
      });
    }
    function edit(drive) {
      const d={...drive}; content.replaceChildren();
      function field(label,key,secret=false) {
        const node=document.createElement('label'); node.className='filter-label'; node.textContent=label;
        const input=document.createElement('input'); input.name=key; input.type=secret?'password':'text'; input.autocomplete=secret?'new-password':'off';
        input.value=secret?'':(d[key] || ''); node.append(input);content.append(node);return input;
      }
      const label=field('Drive name','label');
      const source=field(d.kind==='usb'?'USB filesystem UUID':'SMB share (//server/share)','source'); source.readOnly=d.kind==='usb';
      const user=d.kind==='smb'?field('Username (empty for guest)','username'):null;
      const domain=d.kind==='smb'?field('Domain (optional)','domain'):null;
      const password=d.kind==='smb'?field(d.password_set?'Password (blank keeps stored password)':'Password','password',true):null;
      const save=async () => action(async gen => {
        const next={...d,label:label.value,source:source.value};
        if(user) { next.username=user.value;next.domain=domain.value;next.password=password.value; }
        const saved=await api.save(next); if(gen===epoch) edit(saved);
      });
      content.append(button('Save connection',save));
      if(d.saved) {
        content.append(line('BIOS: /' + (d.bios_folder || '') + ' · Media: /' + (d.media_folder || '')));
        content.append(button('Browse BIOS folder',() => browse(d,d.bios_folder,'bios')),
          button('Browse media folder',() => browse(d,d.media_folder,'media')));
        if(d.password_set) content.append(button('Clear stored password',() => action(async gen => { const saved=await api.save({...d,password:'',password_set:false});if(gen===epoch)edit(saved); })));
        content.append(button('Forget drive',() => action(async gen => {await api.forget(d.id);if(gen===epoch){content.replaceChildren(button('Back to drives',inventory));}})));
      }
      content.append(button('Back to drives',inventory));
    }
    async function browse(d, path='.', folderPurpose='') {
      path=path || '.';
      await action(async gen => {
        const entries=await api.entries(d.id,path); if(gen!==epoch)return;
        content.replaceChildren(line(d.label + ' / ' + (path==='.'?'':path)));
        if(folderPurpose) content.append(button('Use this folder for '+folderPurpose.toUpperCase(),() => action(async current => {
          const saved=await api.save({...d,[folderPurpose+'_folder']:path});if(current===epoch)edit(saved);
        })));
        const up=path.split('/').slice(0,-1).join('/') || '.';
        content.append(button(path==='.'?'Back to drives':'..',path==='.'?inventory:() => browse(d,up,folderPurpose)));
        for(const entry of entries) {
          const next=(path==='.'?'':path+'/')+entry.name;
          if(entry.directory) content.append(button(entry.name+'/',() => browse(d,next,folderPurpose)));
          else if(!folderPurpose && choose) content.append(button(entry.name+' · '+entry.size+' bytes',() => action(async current => {
            const blob=await api.file(d.id,next);if(current!==epoch)return;
            const file=new File([blob],entry.name,{type:'application/octet-stream'});
            const callback=choose.onSelect;dismiss();await callback(file);
          })));
        }
      });
    }
    function open(options=null) { if(busy)return; ++epoch;choose=options;content.replaceChildren();status.textContent='';dialog.showModal();void inventory(); }
    launch.addEventListener('click',() => open());
    picker=open;
  }
  const api={createClient,mount,pick:options => { if(picker)picker(options); }};
  root.FogCastStorage=api;
  if(typeof module==='object'&&module.exports)module.exports=api;
})(typeof globalThis==='object'?globalThis:this);
