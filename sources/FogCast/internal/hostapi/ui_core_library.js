/* Browser-only management of existing host library APIs. No lifecycle operations. */
(function(root) {
  'use strict';
  const MAX_PACKAGE_BYTES = 65 * 1024 * 1024;
  const MAX_MEDIA_BYTES = 32 * 1024 * 1024;
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
      packageId:'', entryId:'', capabilities:null, compatibility:null, media:null, message:''};
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
    function mediaAllowed() {
      const caps = state.capabilities;
      return Boolean(state.media && caps && caps.package_id === state.packageId &&
        caps.media.some(m => m.role === 'blob' && state.media.size >= m.min_bytes && state.media.size <= m.max_bytes));
    }
    async function inventories(token) {
      const [packages, entries] = await Promise.all([
        request('/api/v1/core-packages'), request('/api/v1/library/core-entries')]);
      if (packages && packages.packages === null) packages.packages = [];
      if (entries && entries.entries === null) entries.entries = [];
      if (!packages || !entries || !Array.isArray(packages.packages) || !Array.isArray(entries.entries) ||
          !packages.packages.every(validPackage) || !entries.entries.every(validEntry)) throw new Error('Invalid library inventory.');
      if (token !== epoch) return false;
      state.packages = packages.packages;
      state.entries = entries.entries;
      if (!selectedEntry()) state.entryId = '';
      const entry = selectedEntry();
      if (!selectedPackage() || (entry && core(selectedPackage()) !== entry.core_id)) state.packageId = entry ? entry.package_id : '';
      state.capabilities = null;
      state.compatibility = null;
      return true;
    }
    async function capabilities(token) {
      const id = state.packageId;
      if (!id) return;
      const value = await request('/api/v1/core-packages/' + id + '/media-capabilities');
      if (!value || value.package_id !== id || value.source !== 'declared-contract' ||
          value.compatibility !== 'unknown' || !Array.isArray(value.media) ||
          !value.media.every(m => m.role === 'blob' && Number.isSafeInteger(m.min_bytes) &&
            Number.isSafeInteger(m.max_bytes) && m.min_bytes >= 1 && m.max_bytes >= m.min_bytes)) {
        throw new Error('Invalid declared media capabilities.');
      }
      if (token === epoch) state.capabilities = value;
    }
    async function refresh() {
      if (state.busy) return emit();
      const token = ++epoch;
      state.loading = true;
      state.message = '';
      emit();
      try { if (await inventories(token)) await capabilities(token); }
      catch (error) { if (token === epoch) state.message = error.message; }
      finally { if (token === epoch) { state.loading = false; emit(); } }
      return clone(state);
    }
    async function selectPackage(id) {
      if (state.busy) return emit();
      const item = state.packages.find(p => p.package_id === id);
      const entry = selectedEntry();
      if (id && (!item || (entry && core(item) !== entry.core_id))) return fail(new Error('Choose a package for the same core.'));
      const token = ++epoch;
      state.packageId = id;
      state.capabilities = state.compatibility = state.media = null;
      state.message = '';
      state.loading = Boolean(id);
      emit();
      try { await capabilities(token); }
      catch (error) { if (token === epoch) state.message = error.message; }
      finally { if (token === epoch) { state.loading = false; emit(); } }
      return clone(state);
    }
    async function mutate(operation, success, catalogChange = false) {
      if (state.busy || state.loading) return emit();
      state.busy = true;
      const token = ++epoch;
      state.message = '';
      emit();
      try {
        await operation();
        if (token !== epoch) return clone(state);
        state.message = success;
        if (catalogChange) onCatalogChange();
      } catch (error) {
        if (token === epoch) {
          state.message = error.conflict ? 'Selection conflict. Refreshing current selections; operation was not replayed.' : error.message;
          if (error.conflict) {
            try {
              if (await inventories(token)) {
                const entry = selectedEntry();
                if (entry) state.packageId = entry.package_id;
                await capabilities(token);
              }
            }
            catch (_) { state.message += ' Refresh failed; use Refresh before trying again.'; }
          }
        }
      } finally { if (token === epoch) { state.busy = false; emit(); } }
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
      createEntry(title) {
        return mutate(async () => {
          const p = requirePackage();
          const trimmed = String(title || '').trim();
          if (!trimmed || new TextEncoder().encode(trimmed).length > 256) throw new Error('Title must contain 1–256 UTF-8 bytes.');
          if (state.media && !mediaAllowed()) throw new Error('Imported media is outside the declared limits; choose no media or another file.');
          const body = {title:trimmed, package_id:p.package_id};
          if (state.media) Object.assign(body, {media_role:'blob', media_id:state.media.media_id});
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
          return {expected_package_id:entry.package_id, expected_media_id:entry.media_id || '', media_role:'blob', media_id:state.media.media_id};
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
    click('core-package-import', () => controller.importPackage(byId('core-package-file').files[0]));
    click('core-package-check', () => controller.checkCompatibility());
    click('core-media-import', () => controller.importMedia(byId('core-media-file').files[0]));
    click('core-media-discard', () => controller.discardMedia());
    click('core-entry-create', () => controller.createEntry(byId('core-entry-title').value));
    click('core-entry-package-save', () => controller.selectEntryPackage());
    click('core-entry-media-save', () => controller.selectEntryMedia());
    click('core-entry-media-clear', () => controller.clearEntryMedia());
  }
  const api = {createController, mount, MAX_PACKAGE_BYTES, MAX_MEDIA_BYTES};
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  root.FogCastCoreLibrary = api;
})(globalThis);
