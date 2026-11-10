/* Saved host drives. The kit's own drives are managed in its tenfoot settings. */
(function(root) {
  'use strict';
  const base = '/api/v1/storage/drives';
  const MAX_FILE_BYTES = 32 * 1024 * 1024;
  const MIN_HARD_DISK_BYTES = 1024 * 1024;
  const MAX_HARD_DISK_BYTES = 128 * 1024 * 1024;
  // Match the hard-disk import's 128 KiB/s budget, plus 30 seconds.
  const hardDiskReadMs = size => (90 + Math.floor(size / (128 * 1024))) * 1000;
  function validateSize(size, hardDisk) {
    if (hardDisk) {
      if (!Number.isSafeInteger(size) || size < MIN_HARD_DISK_BYTES || size > MAX_HARD_DISK_BYTES || size % 512 !== 0)
        throw new Error('Choose a raw hard-disk image of 1–128 MiB in whole 512-byte sectors.');
    } else if (!Number.isSafeInteger(size) || size < 1 || size > MAX_FILE_BYTES) {
      throw new Error('Choose a non-empty file of up to 32 MiB.');
    }
  }
  const driveURL = (id, suffix, path) => base + '/' + encodeURIComponent(id) + '/' + suffix + '?path=' + encodeURIComponent(path || '.');
  async function readFile(response, size, abort) {
    if (!response.body || typeof response.body.getReader !== 'function') throw new Error('Drive file response has no readable body.');
    const reader = response.body.getReader();
    const parts = [];
    let received = 0;
    try {
      while (true) {
        const {done, value} = await reader.read();
        if (done) break;
        if (!(value instanceof Uint8Array) || value.byteLength > size - received) {
          abort();
          void reader.cancel().catch(() => {});
          throw new Error('Drive file changed or transfer was interrupted.');
        }
        received += value.byteLength;
        parts.push(value);
      }
      if (received !== size) throw new Error('Drive file changed or transfer was interrupted.');
      return new Blob(parts, {type:'application/octet-stream'});
    } finally { reader.releaseLock(); }
  }
  function createClient(fetchImpl) {
    async function request(path, options = {}, binary = false, hardDisk = false) {
      const abort = new AbortController();
      const callerSignal = options.signal;
      const cancel = () => abort.abort();
      if (callerSignal) {
        if (callerSignal.aborted) abort.abort();
        else callerSignal.addEventListener('abort', cancel, {once:true});
      }
      // A saved SMB drive may need to reconnect before it can send headers.
      // Bound that phase by the maximum disk budget, then use the actual size.
      let timer = setTimeout(cancel, hardDisk ? hardDiskReadMs(MAX_HARD_DISK_BYTES) : 30000);
      try {
        const response = await fetchImpl(path, {...options, redirect:'error', signal:abort.signal});
        if (!response.ok) throw new Error('Drive unavailable. Check the drive, share and credentials.');
        if (response.status === 204) return null;
        if (binary) {
          const size = Number(response.headers.get('Content-Length'));
          validateSize(size, hardDisk);
          if (hardDisk) {
            clearTimeout(timer);
            timer = setTimeout(cancel, hardDiskReadMs(size));
          }
          return await readFile(response, size, cancel);
        }
        return await response.json();
      } catch (error) {
        // Stop receiving a rejected or malformed file body immediately.
        if (binary) abort.abort();
        throw error;
      } finally {
        clearTimeout(timer);
        if (callerSignal) callerSignal.removeEventListener('abort', cancel);
      }
    }
    return {
      list: () => request(base + '?discover=true'),
      save: drive => request(base, {method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(drive)}),
      forget: id => request(base + '/' + encodeURIComponent(id), {method:'DELETE'}),
      entries: async (id, path) => (await request(driveURL(id,'entries',path))).entries,
      file: (id, path, options = {}) => {
        if (options.purpose && options.purpose !== 'hard-disk') throw new Error('Invalid drive file purpose.');
        const disk = options.purpose === 'hard-disk';
        return request(driveURL(id,'file',path) + (disk ? '&purpose=hard-disk' : ''), {signal:options.signal}, true, disk);
      },
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
    let busy=false, epoch=0, choose=null, activeRead=null;
    const limitText = () => choose && choose.filePurpose === 'hard-disk'
      ? 'Hard disk: 1–128 MiB in whole 512-byte sectors.' : 'Files: up to 32 MiB.';
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
    function dismiss() { ++epoch; choose=null; if(activeRead)activeRead.abort(); dialog.close(); launch.focus(); }
    close.addEventListener('click',dismiss);
    dialog.addEventListener('cancel',event => { event.preventDefault(); dismiss(); });
    async function inventory() {
      await action(async gen => {
        const result=await api.list(); if (gen!==epoch) return;
        content.replaceChildren(line(choose ? 'Choose a host USB drive or saved SMB share.' : 'USB volumes are remembered by UUID. SMB shares reconnect when opened.'));
        if (choose) content.append(line(limitText()));
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
        if (choose) content.append(line(limitText()));
        if(folderPurpose) content.append(button('Use this folder for '+folderPurpose.toUpperCase(),() => action(async current => {
          const saved=await api.save({...d,[folderPurpose+'_folder']:path});if(current===epoch)edit(saved);
        })));
        const up=path.split('/').slice(0,-1).join('/') || '.';
        content.append(button(path==='.'?'Back to drives':'..',path==='.'?inventory:() => browse(d,up,folderPurpose)));
        for(const entry of entries) {
          const next=(path==='.'?'':path+'/')+entry.name;
          if(entry.directory) content.append(button(entry.name+'/',() => browse(d,next,folderPurpose)));
          else if(!folderPurpose && choose) content.append(button(entry.name+' · '+entry.size+' bytes',() => action(async current => {
            validateSize(entry.size, choose.filePurpose === 'hard-disk');
            const abort = new AbortController(); activeRead = abort;
            try {
              const blob=await api.file(d.id,next,{purpose:choose.filePurpose,signal:abort.signal});if(current!==epoch)return;
              const file=new File([blob],entry.name,{type:'application/octet-stream'});
              const callback=choose.onSelect;dismiss();await callback(file);
            } finally { if(activeRead===abort)activeRead=null; }
          })));
        }
      });
    }
    function open(options=null) {
      if(busy)return;
      ++epoch;
      choose=options && {...options, filePurpose:options.purpose==='media' && options.filePurpose==='hard-disk' ? 'hard-disk' : undefined};
      content.replaceChildren();status.textContent='';dialog.showModal();void inventory();
    }
    launch.addEventListener('click',() => open());
    picker=open;
  }
  const api={createClient,mount,pick:options => { if(picker)picker(options); }};
  root.FogCastStorage=api;
  if(typeof module==='object'&&module.exports)module.exports=api;
})(typeof globalThis==='object'?globalThis:this);
