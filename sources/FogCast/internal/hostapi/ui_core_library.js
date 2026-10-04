/* Browser-only management of existing host library APIs. No lifecycle operations. */
(function(root) {
  'use strict';
  const MAX_PACKAGE_BYTES = 65 * 1024 * 1024;
  const MAX_MEDIA_BYTES = 32 * 1024 * 1024;
  const MAX_VIDEO_PART_BYTES = 32 * 1024 * 1024;
  const videoProfile = value => ['direct', 'scanlines'].includes(value);
  const digest = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
  const clone = value => JSON.parse(JSON.stringify(value));
  const core = item => item && item.descriptor && item.descriptor.core && item.descriptor.core.id;
  const entryPath = id => '/api/v1/library/core-entries/' + encodeURIComponent(id);
  function validPackage(value) {
    return value && digest(value.package_id) && typeof core(value) === 'string' && core(value);
  }
  function validEntry(value) {
    return value && typeof value.game_id === 'string' && value.game_id &&
      typeof value.title === 'string' && typeof value.core_id === 'string' &&
      digest(value.package_id) && (!value.media_id || digest(value.media_id));
  }
  function createController({fetchImpl, onChange = () => {}, onCatalogChange = () => {}, requestTimeoutMs = 120000}) {
    if (!Number.isFinite(requestTimeoutMs) || requestTimeoutMs <= 0 || requestTimeoutMs > 120000) throw new Error('Invalid request timeout.');
    const state = {open:false, busy:false, loading:false, packages:[], entries:[],
      cores:[], catalogOnline:false, catalogMessage:'', coreRef:null, setup:null, setupROMs:{}, setupGame:null, packageId:'', entryId:'', capabilities:null, compatibility:null, media:null,
      video:null, videoMessage:'', videoParts:[], videoPartsMessage:'', message:''};
    let epoch = 0;
    let listener = () => {};
    function emit() { const value = clone(state); onChange(value); listener(value); return value; }
    async function request(path, options = {}) {
      const abort = new AbortController();
      let timer;
      const deadline = new Promise((_, reject) => {
        timer = setTimeout(() => {
          reject(new Error('Request timed out. Change may already be saved; outcome unknown. Refresh to confirm; nothing was retried.'));
          abort.abort();
        }, requestTimeoutMs);
      });
      try {
        // Race the entire fetch/body operation: even an adapter ignoring abort cannot hold the UI lock.
        return await Promise.race([deadline, (async () => {
          let response;
          try { response = await fetchImpl(path, {...options, redirect:'error', signal:abort.signal}); }
          catch (_) { throw new Error('Request outcome unknown. Refresh before another explicit attempt; nothing was retried.'); }
          let value;
          try { value = await response.json(); }
          catch (_) { throw new Error('Invalid host response. Refresh before another explicit attempt.'); }
          if (!response.ok) {
            const error = new Error((value.error && value.error.message) || 'Host request failed.');
            error.conflict = response.status === 409;
            throw error;
          }
          return value;
        })()]);
      } finally { clearTimeout(timer); }
    }
    const json = body => ({headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
    const selectedPackage = () => state.packages.find(p => p.package_id === state.packageId);
    const selectedEntry = () => state.entries.find(e => e.game_id === state.entryId);
    function fail(error) { state.message = String(error.message || error); return emit(); }
    const MEDIA_ROLES = ['blob', 'disk', 'cassette'];
    // The declared role (startup blob, removable disk, or cassette) whose
    // limits admit the imported media, or '' when none does.
    function mediaRole() {
      const caps = state.capabilities;
      const match = state.media && caps && caps.package_id === state.packageId &&
        caps.media.find(m => MEDIA_ROLES.includes(m.role) && state.media.size >= m.min_bytes && state.media.size <= m.max_bytes);
      return match ? match.role : '';
    }
    function mediaAllowed() { return mediaRole() !== ''; }
    async function inventories(requestEpoch) {
      const [packages, entries] = await Promise.all([
        request('/api/v1/core-packages'), request('/api/v1/library/core-entries')]);
      if (packages && packages.packages === null) packages.packages = [];
      if (entries && entries.entries === null) entries.entries = [];
      if (!packages || !entries || !Array.isArray(packages.packages) || !Array.isArray(entries.entries) ||
          !packages.packages.every(validPackage) || !entries.entries.every(validEntry)) throw new Error('Invalid library inventory.');
      if (requestEpoch !== epoch) return false;
      state.packages = packages.packages;
      state.entries = entries.entries;
      if (!selectedEntry()) state.entryId = '';
      const entry = selectedEntry();
      if (!selectedPackage() || (entry && core(selectedPackage()) !== entry.core_id)) state.packageId = entry ? entry.package_id : '';
      state.capabilities = null;
      state.compatibility = null;
      state.video = null;
      return true;
    }
    function validVideoPart(value) {
      return value && digest(value.part_id) && digest(value.package_id) && videoProfile(value.profile);
    }
    async function videoInventory(requestEpoch) {
      try {
        const value = await request('/api/v1/library/video-parts');
        if (!Array.isArray(value) || !value.every(validVideoPart)) throw new Error('Invalid video-part inventory.');
        if (requestEpoch === epoch) { state.videoParts = value; state.videoPartsMessage = ''; }
      } catch (_) {
        if (requestEpoch === epoch) { state.videoParts = []; state.videoPartsMessage = 'Video-part inventory unavailable. Refresh to confirm saved imports.'; }
      }
    }
    async function videoResolution(requestEpoch) {
      if (requestEpoch !== epoch) return;
      const entry = selectedEntry();
      state.video = null; state.videoMessage = '';
      if (!entry) return;
      if (entry.package_id !== state.packageId) {
        state.videoMessage = 'Select the package for this entry first, then review its next-launch output.';
        return;
      }
      try {
        const value = await request(entryPath(entry.game_id) + '/video');
        const effectiveChoice = value && Array.isArray(value.choices) && value.choices.find(c => c && c.profile === value.effective_profile);
        const missingPart = value && !value.part_id && effectiveChoice && !effectiveChoice.part_id &&
          effectiveChoice.available === false && typeof effectiveChoice.reason === 'string' && effectiveChoice.reason.length > 0;
        if (!value || value.game_id !== entry.game_id || value.package_id !== entry.package_id ||
            !videoProfile(value.preferred_profile) || !videoProfile(value.effective_profile) || typeof value.builtin !== 'boolean' ||
            (value.builtin ? Boolean(value.part_id) || value.effective_profile !== 'direct' : !digest(value.part_id) && !missingPart) || !Array.isArray(value.choices) ||
            !value.choices.every(c => c && videoProfile(c.profile) && typeof c.label === 'string' &&
              typeof c.available === 'boolean' && (!c.part_id || digest(c.part_id)) && (!c.reason || typeof c.reason === 'string')) ||
            (value.fallback_reason && typeof value.fallback_reason !== 'string')) throw new Error('Invalid resolved video output.');
        if (requestEpoch === epoch) state.video = value;
      } catch (_) {
        if (requestEpoch === epoch) state.videoMessage = 'Next-launch video output could not be confirmed. Refresh before launching.';
      }
    }
    async function capabilities(requestEpoch) {
      if (requestEpoch !== epoch) return;
      await videoResolution(requestEpoch);
      if (requestEpoch !== epoch) return;
      const id = state.packageId;
      if (!id) return;
      const value = await request('/api/v1/core-packages/' + id + '/media-capabilities');
      if (!value || value.package_id !== id || value.source !== 'declared-contract' ||
          value.compatibility !== 'unknown' || !Array.isArray(value.media) ||
          !value.media.every(m => MEDIA_ROLES.includes(m.role) && Number.isSafeInteger(m.min_bytes) &&
            Number.isSafeInteger(m.max_bytes) && m.min_bytes >= 1 && m.max_bytes >= m.min_bytes)) {
        throw new Error('Invalid declared media capabilities.');
      }
      if (requestEpoch === epoch) state.capabilities = value;
    }
    async function catalogInventory(requestEpoch) {
      try {
        const value = await request('/api/v1/core-catalog');
        if (!value || !Array.isArray(value.cores) || !value.cores.every(c =>
            typeof c.source_id === 'string' && typeof c.core_id === 'string' && typeof c.label === 'string' &&
            typeof c.library_source_id === 'string' && c.library_source_id && ['supported','demo','experimental'].includes(c.standing) &&
            ['unproduced','available','installed','unavailable'].includes(c.artifact_state) &&
            (!c.package_id || digest(c.package_id)))) throw new Error('Invalid systems catalog.');
        if (requestEpoch === epoch) { state.cores = value.cores; state.catalogOnline = true; state.catalogMessage = ''; }
      } catch (_) {
        if (requestEpoch === epoch) { state.catalogOnline = false; state.catalogMessage = 'Systems catalog unavailable. Cached systems are browse-only; setup is disabled. Manual package import remains available.'; }
      }
    }
    function requireCore() {
      if (!state.catalogOnline || !state.coreRef) throw new Error('Choose an online system.');
      const row = state.cores.find(c => c.source_id === state.coreRef.source_id && c.core_id === state.coreRef.core_id);
      if (!row || row.package_id !== state.coreRef.package_id) throw new Error('System changed. Refresh and choose again.');
      return row;
    }
    async function loadSetup(requestEpoch) {
      const row = requireCore();
      if (row.artifact_state !== 'installed') throw new Error('Install this core before setup.');
      const query = new URLSearchParams({source_id:row.source_id, package_id:row.package_id});
      const value = await request('/api/v1/core-catalog/' + encodeURIComponent(row.core_id) + '/setup?' + query);
      if (!value || value.library_source_id !== row.library_source_id || value.source_id !== row.source_id || value.core_id !== row.core_id || value.package_id !== row.package_id ||
          !Array.isArray(value.roms) || !value.roms.every(r => typeof r.id === 'string' && r.id && Number.isSafeInteger(r.source_size) &&
            r.source_size > 0 && r.source_size <= MAX_MEDIA_BYTES && ['entry','household-firmware'].includes(r.binding))) throw new Error('Invalid setup requirements.');
      if (requestEpoch === epoch) state.setup = value;
    }
    async function refresh() {
      if (state.busy) return emit();
      const requestEpoch = ++epoch;
      state.loading = true;
      state.catalogOnline = false;
      state.message = '';
      emit();
      try { if (await inventories(requestEpoch)) { await capabilities(requestEpoch); await videoInventory(requestEpoch); await catalogInventory(requestEpoch); if (state.coreRef && state.catalogOnline) {
          const row = state.cores.find(c => c.source_id === state.coreRef.source_id && c.core_id === state.coreRef.core_id);
          if (!row || row.library_source_id !== state.coreRef.library_source_id || (row.package_id || '') !== state.coreRef.package_id) {
            state.coreRef = state.setup = state.setupGame = null; state.setupROMs = {}; state.media = null; state.packageId = '';
          } else if (row.artifact_state === 'installed') {
            state.setup = null;
            try { await loadSetup(requestEpoch); } catch (error) { if (requestEpoch === epoch) state.message = error.message; }
          } else { state.setup = null; state.setupROMs = {}; }
        } } }
      catch (error) { if (requestEpoch === epoch) { state.message = error.message; state.catalogOnline = false; state.catalogMessage = 'Source unavailable. Cached systems are browse-only; refresh to enable setup.'; } }
      finally { if (requestEpoch === epoch) { state.loading = false; emit(); } }
      return clone(state);
    }
    async function selectPackage(id) {
      if (state.busy) return emit();
      const item = state.packages.find(p => p.package_id === id);
      const entry = selectedEntry();
      if (id && (!item || (entry && core(item) !== entry.core_id))) return fail(new Error('Choose a package for the same core.'));
      const requestEpoch = ++epoch;
      state.coreRef = state.setup = null; state.setupROMs = {};
      state.packageId = id;
      state.capabilities = state.compatibility = state.media = null;
      state.message = '';
      state.loading = Boolean(id);
      emit();
      try { await capabilities(requestEpoch); }
      catch (error) { if (requestEpoch === epoch) state.message = error.message; }
      finally { if (requestEpoch === epoch) { state.loading = false; emit(); } }
      return clone(state);
    }
    async function mutate(operation, success, catalogChange = false) {
      if (state.busy || state.loading) return emit();
      state.busy = true;
      const requestEpoch = ++epoch;
      state.message = '';
      emit();
      try {
        await operation();
        if (requestEpoch !== epoch) return clone(state);
        state.message = success;
        if (catalogChange) onCatalogChange();
      } catch (error) {
        if (requestEpoch === epoch) {
          state.message = error.conflict ? 'Selection conflict. Refreshing current selections; operation was not replayed.' : error.message;
          if (error.conflict) {
            try {
              if (await inventories(requestEpoch)) {
                const entry = selectedEntry();
                if (entry) state.packageId = entry.package_id;
                await capabilities(requestEpoch);
              }
            }
            catch (_) { state.message += ' Refresh failed; use Refresh before trying again.'; }
          }
        }
      } finally { if (requestEpoch === epoch) { state.busy = false; emit(); } }
      return clone(state);
    }
    function boundedFile(file, maximum, extension) {
      if (!file || !Number.isSafeInteger(file.size) || file.size < 1 || file.size > maximum ||
          (extension && (typeof file.name !== 'string' || !file.name.toLowerCase().endsWith(extension)))) {
        throw new Error('Choose ' + (extension || 'a media file') + ' with 1–' + maximum + ' bytes.');
      }
    }
    function requirePackage() { const p = selectedPackage(); if (!p) throw new Error('Choose an installed package.'); return p; }
    function requireEntry() { const e = selectedEntry(); if (!e) throw new Error('Choose an existing entry.'); return e; }
    async function refreshAfterMutation() {
      try { await inventories(epoch); await capabilities(epoch); }
      catch (_) { throw new Error('Change may already be saved. Refresh to confirm; nothing was retried.'); }
    }
    async function entryMutation(body, suffix, success) {
      return mutate(async () => {
        const entry = requireEntry();
        const value = await request(entryPath(entry.game_id) + suffix, {method:'PUT', ...json(body(entry))});
        if (!validEntry(value) || value.game_id !== entry.game_id) throw new Error('Invalid selection response; refresh before continuing.');
        await refreshAfterMutation();
      }, success, true);
    }
    return {
      async selectCore(sourceId, coreId) {
        if (state.busy || state.loading) return emit();
        const row = state.cores.find(c => c.source_id === sourceId && c.core_id === coreId);
        if (!row) return fail(new Error('Unknown system.'));
        state.coreRef = {library_source_id:row.library_source_id,source_id:row.source_id,core_id:row.core_id,package_id:row.package_id || ''};
        state.setup = null; state.setupROMs = {}; state.setupGame = null; state.media = null;
        state.packageId = row.artifact_state === 'installed' ? row.package_id : ''; state.entryId = '';
        const requestEpoch = ++epoch; state.loading = true; emit();
        try { if (state.catalogOnline && row.artifact_state === 'installed') { await loadSetup(requestEpoch); await capabilities(requestEpoch); } }
        catch (error) { if (requestEpoch === epoch) state.message = error.message; }
        finally { if (requestEpoch === epoch) { state.loading = false; emit(); } }
        return clone(state);
      },
      installCore() {
        return mutate(async () => {
          const row = requireCore();
          if (row.artifact_state !== 'available' || !digest(row.package_id)) throw new Error('This core has no available package to install.');
          const ref = {source_id:row.source_id,core_id:row.core_id,package_id:row.package_id};
          const value = await request('/api/v1/core-catalog/install', {method:'POST', ...json(ref)});
          if (!validPackage(value) || value.package_id !== ref.package_id || core(value) !== ref.core_id) throw new Error('Invalid install response; refresh to confirm.');
          await inventories(epoch); await catalogInventory(epoch); state.packageId = ref.package_id;
          await loadSetup(epoch); await capabilities(epoch);
        }, 'Core installed. Choose inputs to add a game.');
      },
      chooseSetupROM(id, mediaId, size) {
        if (state.busy || state.loading) return emit();
        try {
          requireCore();
          const r = state.setup && state.setup.roms.find(r => r.id === id);
          if (!r || !digest(mediaId) || size !== r.source_size) throw new Error('Choose an exact-size ROM for this named requirement.');
          state.setupROMs[id] = {media_id:mediaId,size}; return emit();
        } catch (error) { return fail(error); }
      },
      useStoredSetupROM(id, mediaId) {
        return mutate(async () => {
          requireCore(); const r = state.setup && state.setup.roms.find(r => r.id === id);
          if (!r || !digest(mediaId)) throw new Error('Enter an imported ROM SHA-256.');
          const value = await request('/api/v1/core-media/' + mediaId);
          if (!value || value.media_id !== mediaId || value.size !== r.source_size) throw new Error('Stored ROM does not have the required size.');
          state.setupROMs[id] = value;
        }, 'Stored ROM explicitly chosen.');
      },
      importSetupROM(id, file) {
        return mutate(async () => {
          requireCore(); const r = state.setup && state.setup.roms.find(r => r.id === id);
          boundedFile(file, MAX_MEDIA_BYTES);
          if (!r || file.size !== r.source_size) throw new Error('Choose a ROM with exactly ' + (r ? r.source_size : 'the required') + ' bytes.');
          const value = await request('/api/v1/core-media', {method:'POST',headers:{'Content-Type':'application/octet-stream'},body:file});
          if (!value || !digest(value.media_id) || value.size !== r.source_size) throw new Error('Invalid ROM import identity.');
          state.setupROMs[id] = value;
        }, 'ROM stored and explicitly chosen for this setup.');
      },
      selectSetupFirmware(id) {
        return mutate(async () => {
          requireCore(); const r = state.setup && state.setup.roms.find(r => r.id === id);
          const choice = state.setupROMs[id];
          if (!r || r.binding !== 'household-firmware' || !choice) throw new Error('Choose the BIOS first.');
          await request('/api/v1/library/firmware',{method:'PUT',...json({slot:'firmware',media_id:choice.media_id})});
          await loadSetup(epoch);
        }, 'Household BIOS selection updated explicitly. Existing titles use this shared BIOS.');
      },
      createSetupEntry(title) {
        return mutate(async () => {
          const row = requireCore();
          if (!state.setup || row.artifact_state !== 'installed') throw new Error('Install and choose an online system first.');
          const trimmed = String(title || '').trim();
          if (!trimmed || new TextEncoder().encode(trimmed).length > 256) throw new Error('Title must contain 1–256 UTF-8 bytes.');
          const roms = {};
          for (const r of state.setup.roms) {
            const choice = state.setupROMs[r.id];
            if (!choice && r.binding === 'household-firmware' && state.setup.firmware_media_id) { roms[r.id] = state.setup.firmware_media_id; continue; }
            if (!choice) throw new Error('Missing ' + r.role + ': choose ' + r.id + '.');
            if (r.binding === 'household-firmware' && choice.media_id !== state.setup.firmware_media_id) throw new Error('Explicitly select this BIOS for the household before creating the title.');
            roms[r.id] = choice.media_id;
          }
          const body = {...state.coreRef,title:trimmed,roms};
          if (state.media) { if (!mediaAllowed()) throw new Error('Media is outside the declared limits.'); Object.assign(body,{media_role:mediaRole(),media_id:state.media.media_id}); }
          const value = await request('/api/v1/core-catalog/entries',{method:'POST',...json(body)});
          if (!value || value.source_id !== row.library_source_id || value.publication_source_id !== row.source_id || !validEntry(value.entry) || value.entry.core_id !== row.core_id || value.entry.package_id !== row.package_id) throw new Error('Invalid setup response; refresh to confirm.');
          state.setupGame = {source_id:value.source_id,game_id:value.entry.game_id};
          await inventories(epoch); await loadSetup(epoch);
        }, 'Game added to the library. Launch checks readiness and executor compatibility.', true);
      },
      snapshot: () => clone(state),
      subscribe(fn) { listener = fn; emit(); },
      async open() { state.open = true; emit(); return refresh(); },
      close() { if (state.busy) return false; ++epoch; state.open = false; state.loading = false; emit(); return true; },
      refresh, selectPackage,
      selectEntry(id) {
        if (state.busy) return emit();
        const entry = state.entries.find(e => e.game_id === id);
        if (id && !entry) return fail(new Error('Unknown library entry.'));
        state.entryId = id;
        return selectPackage(entry ? entry.package_id : '');
      },
      checkCompatibility() {
        return mutate(async () => {
          const p = requirePackage();
          state.compatibility = null;
          const value = await request('/api/v1/core-packages/' + p.package_id + '/compatibility', {method:'POST'});
          if (!value || value.package_id !== p.package_id || typeof value.compatible !== 'boolean') throw new Error('Invalid compatibility response.');
          state.compatibility = value;
        }, 'Compatibility observation refreshed; launch will revalidate.');
      },
      importPackage(file) {
        return mutate(async () => {
          boundedFile(file, MAX_PACKAGE_BYTES, '.fcore');
          const value = await request('/api/v1/core-packages', {method:'POST', headers:{'Content-Type':'application/octet-stream'}, body:file});
          if (!validPackage(value)) throw new Error('Invalid package import response; package may be saved. Refresh to confirm.');
          state.entryId = '';
          state.packageId = value.package_id;
          state.media = null;
          await refreshAfterMutation();
        }, 'Package imported. No entry or running session was changed.');
      },
      importMedia(file) {
        return mutate(async () => {
          requirePackage();
          boundedFile(file, MAX_MEDIA_BYTES);
          const value = await request('/api/v1/core-media', {method:'POST', headers:{'Content-Type':'application/octet-stream'}, body:file});
          if (!value || !digest(value.media_id) || value.size !== file.size) throw new Error('Invalid media import identity or size.');
          state.media = value;
        }, 'Media stored by digest. Select it explicitly; storage does not prove core compatibility.');
      },
      importVideoPart(profile, file) {
        return mutate(async () => {
          if (!videoProfile(profile)) throw new Error('Choose Direct or Scanlines explicitly.');
          boundedFile(file, MAX_VIDEO_PART_BYTES, '.fexp');
          const value = await request('/api/v1/library/video-parts/' + profile,
            {method:'POST', headers:{'Content-Type':'application/octet-stream'}, body:file});
          if (!validVideoPart(value) || value.profile !== profile) throw new Error('Invalid video import response; part may be saved. Refresh to confirm.');
          await videoInventory(epoch);
          await videoResolution(epoch);
        }, 'Video part imported for its exact core package. Household preference and running session are unchanged.', true);
      },
      createEntry(title) {
        return mutate(async () => {
          const p = requirePackage();
          const trimmed = String(title || '').trim();
          if (!trimmed || new TextEncoder().encode(trimmed).length > 256) throw new Error('Title must contain 1–256 UTF-8 bytes.');
          if (state.media && !mediaAllowed()) throw new Error('Imported media is outside the declared limits; choose no media or another file.');
          const body = {title:trimmed, package_id:p.package_id};
          if (state.media) Object.assign(body, {media_role:mediaRole(), media_id:state.media.media_id});
          const value = await request('/api/v1/library/core-entries', {method:'POST', ...json(body)});
          if (!validEntry(value)) throw new Error('Invalid entry response; refresh before continuing.');
          state.entryId = value.game_id;
          await refreshAfterMutation();
        }, 'Entry created. Launch it from the Kit library; no launch was requested here.', true);
      },
      selectEntryPackage() {
        return entryMutation(entry => {
          const p = requirePackage();
          if (core(p) !== entry.core_id) throw new Error('Package must use the same core.');
          return {expected_package_id:entry.package_id, package_id:p.package_id};
        }, '', 'Package selected for the next launch.');
      },
      selectEntryMedia() {
        return entryMutation(entry => {
          if (entry.package_id !== state.packageId) throw new Error('Select the package first, then choose media.');
          if (!mediaAllowed()) throw new Error('Imported media is outside the selected package declared limits.');
          return {expected_package_id:entry.package_id, expected_media_id:entry.media_id || '', media_role:mediaRole(), media_id:state.media.media_id};
        }, '/media', 'Media selected for the next launch.');
      },
      clearEntryMedia() {
        return entryMutation(entry => ({expected_package_id:entry.package_id,
          expected_media_id:entry.media_id || '', media_role:'', media_id:''}), '/media', 'Media selection cleared for the next launch.');
      },
      discardMedia() { if (!state.busy) { state.media = null; emit(); } }
    };
  }
  function mount(document, controller) {
    const byId = id => document.getElementById(id);
    const dialog = byId('core-library');
    if (!dialog) return;
    const el = (tag, text) => { const node = document.createElement(tag); node.textContent = text; return node; };
    const selectOptions = (node, values, selected) => {
      node.replaceChildren(...values.map(([value,text]) => { const option = el('option', text); option.value = value; return option; }));
      node.value = selected;
    };
    controller.subscribe(state => {
      dialog.hidden = !state.open;
      if (state.open && !dialog.open) dialog.showModal?.();
      if (!state.open && dialog.open) dialog.close?.();
      for (const node of dialog.querySelectorAll('button, input, select')) node.disabled = state.busy;
      dialog.setAttribute('aria-busy', String(state.busy || state.loading));
      const systems = byId('core-system-select');
      if (systems) {
        const selected = state.coreRef ? JSON.stringify([state.coreRef.source_id,state.coreRef.core_id]) : '';
        selectOptions(systems, [['','Choose a system'], ...state.cores.map(c => [JSON.stringify([c.source_id,c.core_id]), c.label + ' — ' + c.standing + ' — ' + c.artifact_state + ' (' + c.source_id + ')'])], selected);
        systems.disabled = state.busy || state.loading;
        const row = state.coreRef && state.cores.find(c => c.source_id === state.coreRef.source_id && c.core_id === state.coreRef.core_id);
        byId('core-system-status').textContent = state.catalogMessage || (row ? row.label + ': ' + row.artifact_state + '. ' + (row.standing === 'experimental' ? 'Experimental; appliance acceptance pending.' : '') : 'Choose a system to install or set up.');
        byId('core-system-install').disabled = state.busy || state.loading || !state.catalogOnline || !row || row.artifact_state !== 'available';
        byId('core-setup-create').disabled = state.busy || state.loading || !state.catalogOnline || !state.setup;
        const fields = [];
        for (const r of (state.setup ? state.setup.roms : [])) {
          const field = el('fieldset','');
          const legend = el('legend', r.role + ': ' + r.id + ' — exactly ' + r.source_size + ' bytes');
          const choice = state.setupROMs[r.id];
          const status = el('p', choice ? 'Chosen SHA-256: ' + choice.media_id : r.binding === 'household-firmware' && state.setup.firmware_media_id ? 'Shared household BIOS: ' + state.setup.firmware_media_id : 'Missing — choose a file or an imported digest.');
          const file = el('input',''); file.type = 'file'; file.setAttribute('aria-label','Choose ' + r.id);
          const upload = el('button','Import and choose ' + r.role); upload.type = 'button';
          upload.addEventListener('click', () => { void controller.importSetupROM(r.id,file.files[0]); });
          const stored = el('input',''); stored.type = 'text'; stored.placeholder = 'Imported ROM SHA-256'; stored.setAttribute('aria-label','Imported digest for ' + r.id);
          const useStored = el('button','Choose stored ROM'); useStored.type = 'button';
          useStored.addEventListener('click', () => { void controller.useStoredSetupROM(r.id,stored.value.trim()); });
          const children = [legend,status,file,upload,stored,useStored];
          if (r.binding === 'household-firmware') {
            const select = el('button','Use this BIOS for the household'); select.type = 'button'; select.disabled = !choice;
            select.addEventListener('click', () => { void controller.selectSetupFirmware(r.id); }); children.push(select);
            children.push(el('p','This explicit action changes the shared BIOS used by existing titles.'));
          }
          for (const node of children) if (['input','button'].includes(node.tagName && node.tagName.toLowerCase())) node.disabled ||= state.busy || state.loading || !state.catalogOnline;
          field.replaceChildren(...children); fields.push(field);
        }
        byId('core-setup-roms').replaceChildren(...fields);
      }
      const entry = state.entries.find(e => e.game_id === state.entryId);
      selectOptions(byId('core-entry-select'), [['','New entry'], ...state.entries.map(e => [e.game_id,e.title + ' — ' + e.core_id])], state.entryId);
      selectOptions(byId('core-package-select'), [['','Choose package'], ...state.packages.filter(p => !entry || core(p) === entry.core_id)
        .map(p => [p.package_id,core(p) + ' ' + (p.descriptor.core.version || '') + ' — ' + p.package_id])], state.packageId);
      const compatible = state.compatibility;
      byId('core-package-status').textContent = compatible
        ? (compatible.compatible ? 'Compatible' : 'Incompatible') + ' — target ' + (compatible.target || compatible.target_id || 'unknown')
        : 'Target compatibility unknown. Use Check compatibility explicitly.';
      const caps = state.capabilities;
      const limits = caps ? caps.media.map(m => m.role + ': ' + m.min_bytes + '–' + m.max_bytes + ' bytes (' + m.transport + ')').join('; ') || 'No supported media contract.' : 'Choose a package to read declared media limits.';
      const media = state.media;
      byId('core-media-status').textContent = limits + ' Declared only; active target capacity is checked at launch.' +
        (media ? ' Imported SHA-256 ' + media.media_id + ' — ' + media.size + ' bytes.' : ' New entry: no media selected.');
      byId('core-entry-current').textContent = entry ? 'Current package ' + entry.package_id + '; media ' + (entry.media_id || 'none') : 'Create an explicitly titled library entry.';
      if (byId('core-video-status')) {
        const video = state.video;
        const name = profile => profile === 'scanlines' ? 'Scanlines' : 'Direct';
        const effectiveChoice = video && video.choices.find(choice => choice.profile === video.effective_profile);
        const available = video && (video.builtin || (effectiveChoice && effectiveChoice.available));
        byId('core-video-status').textContent = state.videoMessage || (video
          ? 'Household preference: ' + name(video.preferred_profile) + '. Next launch: ' + (available ? '' : 'Unavailable — ') + name(video.effective_profile) +
            (video.builtin ? ' (built in).' : video.part_id ? ' (video part ' + video.part_id + ').' : '.') +
            (!available && effectiveChoice && effectiveChoice.reason ? ' ' + effectiveChoice.reason : '') +
            (video.fallback_reason ? ' ' + video.fallback_reason : '')
          : 'Choose a library entry to see its next-launch output.');
        byId('core-video-choices').replaceChildren(...(video ? video.choices : []).map(choice => el('p',
          choice.label + ': ' + (choice.available ? 'Available' : 'Unavailable') + (choice.reason ? ' — ' + choice.reason : ''))));
        byId('core-video-parts').textContent = state.videoPartsMessage ||
          (state.videoParts.length ? state.videoParts.length + ' saved video part(s). Compatibility is checked against the exact selected package.' : 'No video parts imported.');
        byId('core-video-import').disabled = state.busy || state.loading;
      }
      byId('core-library-message').textContent = state.loading ? 'Loading library…' : state.message;
      for (const id of ['core-package-check','core-media-import','core-entry-create','core-entry-package-save','core-entry-media-save','core-entry-media-clear']) {
        byId(id).disabled = state.busy || state.loading || !state.packageId ||
          (id.startsWith('core-entry-') && id !== 'core-entry-create' && !entry);
      }
      byId('core-entry-create').disabled ||= Boolean(entry);
    });
    byId('open-core-library').addEventListener('click', () => { void controller.open(); });
    byId('core-library-close').addEventListener('click', () => { if (controller.close()) byId('open-core-library').focus(); });
    dialog.addEventListener('cancel', event => { event.preventDefault(); if (controller.close()) byId('open-core-library').focus(); });
    byId('core-library-refresh').addEventListener('click', () => { void controller.refresh(); });
    byId('core-package-select').addEventListener('change', event => { void controller.selectPackage(event.target.value); });
    byId('core-entry-select').addEventListener('change', event => { void controller.selectEntry(event.target.value); });
    const click = (id, action) => byId(id).addEventListener('click', () => { void action(); });
    if (byId('core-system-select')) {
      byId('core-system-select').addEventListener('change', event => {
        if (event.target.value) { const [sourceId,coreId] = JSON.parse(event.target.value); void controller.selectCore(sourceId,coreId); }
      });
      click('core-system-install', () => controller.installCore());
      click('core-setup-create', () => controller.createSetupEntry(byId('core-entry-title').value));
    }
    click('core-package-import', () => controller.importPackage(byId('core-package-file').files[0]));
    click('core-package-check', () => controller.checkCompatibility());
    click('core-media-import', () => controller.importMedia(byId('core-media-file').files[0]));
    click('core-media-discard', () => controller.discardMedia());
    click('core-entry-create', () => controller.createEntry(byId('core-entry-title').value));
    click('core-entry-package-save', () => controller.selectEntryPackage());
    click('core-entry-media-save', () => controller.selectEntryMedia());
    click('core-entry-media-clear', () => controller.clearEntryMedia());
    if (byId('core-video-import')) click('core-video-import', () => controller.importVideoPart(byId('core-video-profile').value, byId('core-video-file').files[0]));
  }
  const api = {createController, mount, MAX_PACKAGE_BYTES, MAX_MEDIA_BYTES, MAX_VIDEO_PART_BYTES};
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  root.FogCastCoreLibrary = api;
})(globalThis);
