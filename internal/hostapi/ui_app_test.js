'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {
  gamesPath,
  gameDetailPath,
  launchRequest,
  sessionRequest,
  stopRequest,
  attachInputRequest,
  detachInputRequest,
  parseSession,
  sessionViewState,
  launchStatus,
  isSelectedGame,
  detailHeading,
  createAppController,
  catalogViewState,
  presentationPath,
  parsePresentation,
  catalogRegion,
  catalogGenre,
  catalogStudio,
  catalogPlayers,
  catalogYear,
  catalogSummary,
  filterCatalogViews,
  sortCatalogViews,
  formatCatalogCount,
  fallbackPresentation,
  displayTitle,
  cardTitle,
  variantLabel,
  dumpIdentityFacts,
  sourceKindLabel,
  dumpFlagLabels,
  collectionLabels,
  coverHoverMeta,
  coverStatusLabel,
  cardSourceOffline,
  cardSourceUnreadable,
  isSessionPlayingCard,
  regionLabel,
  catalogDumpRegions,
  systemLabel,
  sourceLabel,
  launchBlockReason,
  collectionIDFromName,
  uniqueCollectionID,
  collectionFetchSort,
  catalogSortOverridden,
  catalogEffectiveSort,
  catalogSortOverrideLabel,
  catalogSortControlHidden,
} = require('./ui_app.js');
const FogCastMetadata = require('./ui_metadata.js');

const readAsset = name => fs.readFileSync(path.join(__dirname, name), 'utf8');
const readFixture = name => JSON.parse(readAsset(path.join('testdata', 'ui', name)));

function jsonResponse(payload, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async json() { return payload; },
  };
}

function queuedFetch(responses) {
  const calls = [];
  const fetchImpl = async (requestPath, options) => {
    calls.push({ path: requestPath, options });
    const response = responses.shift();
    if (!response) throw new Error(`missing fixture response for ${requestPath}`);
    return response;
  };
  return { calls, fetchImpl };
}

class BrowserTestElement {
  constructor(tagName, id = '', owner = null) {
    this.tagName = tagName.toUpperCase();
    this._id = id;
    this.ownerDocument = owner;
    this.children = [];
    this.attributes = new Map();
    this.listeners = new Map();
    this.className = '';
    this.textContent = '';
    this.value = '';
    this.disabled = false;
    this.hidden = false;
    this.focused = false;
    this.tabIndex = 0;
    this.inert = false;
    this.style = {};
    this.clientWidth = 896;
    this.clientHeight = 0;
    this.offsetWidth = 0;
    this._offsetHeight = 0;
    this.offsetTop = 0;
    this.scrollTop = 0;
    this.parentNode = null;
    this.scrollIntoViewCalls = [];
    if (owner && id) owner.nodes.set(id, this);
  }

  get id() {
    return this._id;
  }

  set id(value) {
    this._id = String(value || '');
    if (this.ownerDocument && this._id) this.ownerDocument.nodes.set(this._id, this);
  }

  get offsetHeight() {
    if (this._offsetHeight) return this._offsetHeight;
    if (String(this.className || '').split(/\s+/).includes('card-marks')) {
      const count = (this.children || []).length;
      if (!count) return 0;
      const row = 22;
      const width = Number(this.parentNode && this.parentNode.offsetWidth) || 0;
      if (width > 0 && width <= 148 && count >= 3) return row * 2 + 6;
      return row;
    }
    return 0;
  }

  set offsetHeight(value) {
    this._offsetHeight = Number(value) || 0;
  }

  appendChild(child) {
    child.parentNode = this;
    this.children.push(child);
    if (
      String(this.className || '').includes('home-rail-track')
      && String(child.className || '').includes('game-card')
      && !child.offsetWidth
    ) {
      child.offsetWidth = 148;
    }
    return child;
  }

  replaceChildren(...children) {
    this.children.forEach(child => { child.parentNode = null; });
    this.children = children;
    children.forEach(child => { child.parentNode = this; });
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
    if (name === 'data-keyboard-pane' && this.ownerDocument) {
      this.ownerDocument.keyboardPane = String(value);
    }
  }

  getAttribute(name) {
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }

  removeAttribute(name) {
    this.attributes.delete(name);
  }

  addEventListener(name, listener) {
    this.listeners.set(name, listener);
  }

  dispatchEvent(event) {
    const listener = this.listeners.get(event && event.type);
    return listener ? listener(event) : undefined;
  }

  querySelectorAll(selector) {
    const className = String(selector || '').replace(/^\./, '');
    const found = [];
    const visit = node => {
      (node.children || []).forEach(child => {
        if (className && String(child.className || '').split(/\s+/).includes(className)) {
          found.push(child);
        }
        visit(child);
      });
    };
    visit(this);
    return found;
  }

  click() {
    const listener = this.listeners.get('click');
    return listener ? listener({ currentTarget: this }) : undefined;
  }

  scrollIntoView(options) {
    this.scrollIntoViewCalls.push(options === undefined ? true : options);
  }

  getBoundingClientRect() {
    const left = Number.parseFloat(this.style && this.style.left) || 0;
    const top = Number.parseFloat(this.style && this.style.top) || 0;
    const width = Number(this.offsetWidth) || 0;
    const height = Number(this.offsetHeight) || 0;
    return {
      x: left,
      y: top,
      left,
      top,
      right: left + width,
      bottom: top + height,
      width,
      height,
    };
  }

  focus() {
    if (this.ownerDocument && this.ownerDocument.activeElement && this.ownerDocument.activeElement !== this) {
      this.ownerDocument.activeElement.focused = false;
    }
    this.focused = true;
    if (this.ownerDocument) this.ownerDocument.activeElement = this;
  }

  blur() {
    this.focused = false;
    if (this.ownerDocument && this.ownerDocument.activeElement === this) {
      this.ownerDocument.activeElement = null;
    }
  }
}

function browserDocument() {
  const tagById = {
    'game-search': 'input',
    'nav-home': 'button',
    'nav-all': 'button',
    'nav-continue': 'button',
    'nav-favorites': 'button',
    'nav-recents': 'button',
    'nav-unplayed': 'button',
    'nav-recently-added': 'button',
    'create-collection': 'button',
    'collection-name': 'input',
    'save-collection': 'button',
    'rename-collection': 'button',
    'delete-collection': 'button',
    'layout-cover': 'button',
    'layout-list': 'button',
    'catalog-sort': 'select',
    'catalog-sort-label': 'span',
    'catalog-sort-filter': 'label',
    'refresh-catalog': 'button',
    'filter-system': 'select',
    'filter-region': 'select',
    'launcher': 'main',
    'open-settings': 'button',
    'settings-attract-idle': 'input',
    'settings-preferred-regions': 'input',
    'save-settings': 'button',
    'close-settings': 'button',
    'game-actions-menu': 'div',
  };
  const ids = [
    'launcher', 'health', 'game-search', 'refresh-catalog', 'filter-system', 'filter-region', 'catalog', 'catalog-status',
    'catalog-list', 'catalog-actions', 'detail', 'detail-content',
    'launch-actions', 'launch-status', 'session-panel', 'session-status',
    'session-details', 'session-actions', 'session-message',
    'nav-home', 'nav-all', 'nav-continue', 'nav-favorites', 'nav-recents', 'nav-unplayed',
    'nav-recently-added', 'collection-list', 'create-collection', 'collection-name-label',
    'collection-name', 'save-collection', 'rename-collection', 'delete-collection',
    'platform-list', 'catalog-layout', 'layout-cover', 'layout-list',
    'catalog-sort', 'catalog-sort-label', 'catalog-sort-filter',
    'attract', 'attract-title', 'attract-stage',
    'open-settings', 'settings', 'settings-attract-idle', 'settings-preferred-regions',
    'settings-host-health', 'settings-message', 'save-settings', 'close-settings',
    'game-actions-menu',
  ];
  const document = {
    nodes: new Map(),
    listeners: new Map(),
    activeElement: null,
    keyboardPane: 'rail',
    createElement(tagName) {
      return new BrowserTestElement(tagName, '', document);
    },
    getElementById(id) {
      return document.nodes.get(id) || null;
    },
    addEventListener(name, listener) {
      document.listeners.set(name, listener);
    },
  };
  ids.forEach(id => {
    document.nodes.set(id, new BrowserTestElement(tagById[id] || 'div', id, document));
  });
  document.nodes.get('attract').hidden = true;
  document.nodes.get('settings').hidden = true;
  document.nodes.get('game-actions-menu').hidden = true;
  document.nodes.get('catalog-list').clientWidth = 896;
  const sortSelect = document.nodes.get('catalog-sort');
  [
    ['title', 'Title'],
    ['year', 'Year'],
    ['system', 'Platform'],
    ['recently_added', 'Recently added'],
    ['recents', 'Recent'],
  ].forEach(([value, label]) => {
    const option = new BrowserTestElement('option', '', document);
    option.value = value;
    option.textContent = label;
    if (value === 'recents') {
      option.hidden = true;
      option.disabled = true;
    }
    sortSelect.appendChild(option);
  });
  return document;
}

function catalogSortOption(document, value) {
  const select = document.nodes.get('catalog-sort');
  return ((select && select.children) || []).find(option => option.value === value) || null;
}

function assertCatalogSortVisibility(document, hidden) {
  const sort = document.nodes.get('catalog-sort');
  const label = document.nodes.get('catalog-sort-label');
  const wrap = document.nodes.get('catalog-sort-filter');
  assert.equal(sort.hidden, hidden);
  assert.equal(label.hidden, hidden);
  assert.equal(wrap.hidden, hidden);
}

function browserText(node) {
  return `${node.textContent || ''}${node.children.map(browserText).join('')}`;
}

async function settleBrowser() {
  for (let index = 0; index < 4; index += 1) {
    await new Promise(resolve => setImmediate(resolve));
    await Promise.resolve();
  }
}

async function waitForCondition(condition, message) {
  for (let index = 0; index < 40; index += 1) {
    if (condition()) return;
    await new Promise(resolve => setImmediate(resolve));
  }
  throw new Error(message);
}

async function waitForAttractTitle(document, title, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const attract = document.nodes.get('attract');
    if (attract && attract.hidden === false && document.nodes.get('attract-title').textContent === title) {
      return;
    }
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  const attract = document.nodes.get('attract');
  throw new Error(`attract title was ${JSON.stringify(document.nodes.get('attract-title') && document.nodes.get('attract-title').textContent)} hidden=${attract && attract.hidden}, want ${title}`);
}

async function runBrowserApp({ adapter, responses, sessionResponses, globals, attractItems, collections, settings, keepHome, homeRails, platforms, presentations }) {
  const document = browserDocument();
  const calls = [];
  const sessionCalls = [];
  const queuedSessionResponses = (sessionResponses || [
    jsonResponse({ state: 'idle' }),
    jsonResponse({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' }),
  ]).slice();
  const fetch = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/platforms') {
      return jsonResponse({ platforms: platforms || [] });
    }
    const librarySettings = settings || { attract_idle_seconds: 60, preferred_regions: ['usa', 'world', 'europe', 'japan'] };
    if (pathOnly === '/api/v1/library/attract') {
      return jsonResponse({ items: attractItems || [], idle_seconds: librarySettings.attract_idle_seconds });
    }
    if (pathOnly === '/api/v1/library/facets') {
      return jsonResponse({ genres: [], years: [] });
    }
    if (pathOnly === '/api/v1/library/collections') {
      return jsonResponse({ collections: collections || [] });
    }
    if (pathOnly.startsWith('/api/v1/library/collections/')) {
      return jsonResponse({ id: 'weekend-queue', name: 'Weekend Queue', member: (options && options.method) === 'PUT' });
    }
    if (pathOnly.startsWith('/api/v1/library/favorites/')) {
      return jsonResponse({ favorite: (options && options.method) === 'PUT' });
    }
    if (pathOnly === '/api/v1/library/settings') {
      return jsonResponse(librarySettings);
    }
    if (pathOnly.startsWith('/api/v1/presentation/games/')) {
      calls.push({ path: requestPath, options });
      const id = decodeURIComponent(pathOnly.slice('/api/v1/presentation/games/'.length));
      const queued = presentations && (presentations[id] || presentations[requestPath]);
      if (Array.isArray(queued)) {
        const next = queued.shift();
        if (!next) throw new Error(`missing presentation fixture for ${requestPath}`);
        return next;
      }
      if (queued) return queued;
      throw new Error(`missing browser fixture response for ${requestPath}`);
    }
    const isSession = pathOnly === '/api/v1/session';
    if (!isSession && pathOnly === '/api/v1/games') {
      const collection = new URLSearchParams(String(requestPath).split('?')[1] || '').get('collection') || '';
      if (collection) {
        if (keepHome) calls.push({ path: requestPath, options });
        const rail = homeRails && homeRails[collection];
        return rail || jsonResponse({ games: [] });
      }
    }
    const destination = isSession ? sessionCalls : calls;
    destination.push({ path: requestPath, options });
    const response = (isSession ? queuedSessionResponses : responses).shift();
    if (!response) throw new Error(`missing browser fixture response for ${requestPath}`);
    return response;
  };
  const context = {
    document,
    fetch,
    FogCastAttractDisabled: true,
    setTimeout,
    clearTimeout,
    ...(globals || {}),
  };
  if (adapter !== undefined) context.FogCastMetadata = adapter;
  vm.runInNewContext(readAsset('ui_app.js'), context, { filename: 'ui_app.js' });
  await settleBrowser();
  if (!keepHome && document.nodes.get('nav-all')) {
    document.nodes.get('nav-all').click();
    await settleBrowser();
  }
  return { document, calls, sessionCalls, globals: context };
}

async function runCollectionEditorApp({ collections = [], writeResponses = [], onWrite } = {}) {
  const document = browserDocument();
  const writes = [];
  const pendingWrites = writeResponses.slice();
  const fetch = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/platforms') return jsonResponse({ platforms: [] });
    if (pathOnly === '/api/v1/library/attract') return jsonResponse({ items: [], idle_seconds: 60 });
    if (pathOnly === '/api/v1/library/settings') return jsonResponse({ attract_idle_seconds: 60, preferred_regions: ['usa', 'world'] });
    if (pathOnly === '/api/v1/library/facets') return jsonResponse({ genres: [], years: [] });
    if (pathOnly === '/api/v1/library/collections') return jsonResponse({ collections });
    if (pathOnly.startsWith('/api/v1/library/collections/')) {
      const method = options && options.method;
      if (method === 'PUT' || method === 'DELETE') {
        const record = { path: requestPath, options };
        writes.push(record);
        if (onWrite) await onWrite(record);
        if (pendingWrites.length) return pendingWrites.shift();
        const id = decodeURIComponent(pathOnly.slice('/api/v1/library/collections/'.length).split('/')[0]);
        const query = String(requestPath).includes('?') ? String(requestPath).slice(String(requestPath).indexOf('?') + 1) : '';
        const name = new URLSearchParams(query).get('name') || id;
        return jsonResponse({ id, name, member: method === 'PUT' && pathOnly.split('/').length > 6 });
      }
    }
    if (pathOnly.startsWith('/api/v1/library/favorites/')) {
      return jsonResponse({ favorite: (options && options.method) === 'PUT' });
    }
    if (pathOnly === '/api/v1/session') return jsonResponse({ state: 'idle' });
    if (pathOnly === '/api/v1/games') return jsonResponse(readFixture('catalog-populated.json'));
    throw new Error(`unexpected collection editor fixture ${requestPath}`);
  };
  const context = {
    document,
    fetch,
    FogCastAttractDisabled: true,
    setTimeout,
    clearTimeout,
  };
  vm.runInNewContext(readAsset('ui_app.js'), context, { filename: 'ui_app.js' });
  await settleBrowser();
  await waitForCondition(
    () => (collections.length === 0 ? true : document.nodes.get('collection-list').children.length === collections.length),
    'collection rail did not render',
  );
  return { document, writes };
}

function malformedJSONResponse(status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async json() {
      throw new SyntaxError('response was not JSON');
    },
  };
}

async function launchStateFor(response) {
  const { calls, fetchImpl } = queuedFetch([
    jsonResponse(readFixture('catalog-populated.json')),
    jsonResponse(readFixture('detail-refreshed.json')),
    response,
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadCatalog('');
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  return { calls, state: controller.getState() };
}

function immutableBoundaryGame(overrides = {}) {
  return {
    id: 'megadrive-sonic-test',
    title: 'Sonic the Hedgehog',
    system: 'megadrive',
    kind: 'zip',
    state: 'available',
    root_online: true,
    content_prepared: true,
    execution: 'fpga_native',
    nested: { marker: 'original-nested-value' },
    ...overrides,
  };
}

function validAdapterPresentation() {
  return {
    cover: { palette: 'lagoon', treatment: 'rings' },
    backdrop: { palette: 'sunset', treatment: 'waves' },
    summary: 'A stable adapter presentation.',
    year: '1991',
    genre: 'Platformer',
    studio: 'SEGA',
    players: '1 player',
    isFallback: true,
  };
}

test('paginated catalog stays populated until the host returns no rows', () => {
  const views = [
    { live: { id: 'megadrive-a', title: 'Streets of Rage 2 (USA)', system: 'megadrive' }, presentation: { genre: 'Beat \'em Up' } },
  ];
  assert.equal(catalogViewState({
    catalogState: 'populated',
    query: '',
    games: views.map(view => view.live),
    gameViews: views,
    nextCursor: 'cursor-2',
  }), 'populated');
  assert.equal(catalogViewState({
    catalogState: 'populated',
    query: '',
    games: [],
    gameViews: [],
    nextCursor: '',
  }), 'empty');
  assert.equal(catalogViewState({
    catalogState: 'populated',
    libraryView: 'home',
    query: 'sonic',
    games: [],
    gameViews: [],
    homeRails: [],
  }), 'empty');
  assert.equal(catalogViewState({
    catalogState: 'populated',
    libraryView: 'grid',
    query: 'sonic',
    games: [],
    gameViews: [],
  }), 'no_matches');
});

test('catalog filters keep search, platform, region, and genre on the host query', () => {
  assert.equal(gamesPath('sonic'), '/api/v1/games?q=sonic&grouped=1');
  assert.equal(gamesPath('', { platform: 'snes', region: 'japan', genre: 'Platform', grouped: 1 }), '/api/v1/games?platform=snes&region=japan&genre=Platform&grouped=1');
  assert.equal(catalogRegion('007 Shitou - The Duel (Japan)'), 'japan');
  assert.equal(catalogRegion('Streets of Rage 2 (USA)'), 'usa');
  assert.equal(catalogRegion('Sonic & Knuckles (World)'), 'world');
  assert.equal(catalogRegion('Bare Knuckle ~ Streets of Rage (World) (Rev A)'), 'world');
  assert.equal(catalogRegion('The King of Fighters (Korea)'), 'korea');
  assert.equal(catalogRegion('Street Fighter (Asia)'), 'asia');
  assert.equal(catalogRegion('Sonic the Hedgehog (Australia)'), 'australia');
  assert.equal(catalogRegion('Another World (France)'), 'france');
  assert.equal(catalogRegion('Sonic the Hedgehog'), 'other');
  assert.ok(catalogDumpRegions().includes('korea'));
  assert.ok(catalogDumpRegions().includes('asia'));
  assert.ok(catalogDumpRegions().includes('australia'));
  assert.ok(catalogDumpRegions().includes('france'));
  assert.ok(catalogDumpRegions().includes('germany'));
  assert.ok(catalogDumpRegions().includes('spain'));
  assert.ok(catalogDumpRegions().includes('italy'));
  assert.ok(catalogDumpRegions().includes('canada'));
  assert.equal(catalogDumpRegions().at(-1), 'other');
  assert.equal(regionLabel('usa'), 'USA');
  assert.equal(regionLabel('korea'), 'Korea');
  const views = [
    { live: { id: 'megadrive-a', title: 'Streets of Rage 2 (USA)', system: 'megadrive', genre: 'Beat \'em Up' }, presentation: { genre: 'Beat \'em Up' } },
    { live: { id: 'snes-b', title: 'Super Mario World (USA)', system: 'snes', genre: 'Platform' }, presentation: { genre: 'Platform' } },
    { live: { id: 'megadrive-c', title: '007 Shitou - The Duel (Japan)', system: 'megadrive' }, presentation: { genre: 'Unknown' } },
  ];
  assert.deepEqual(filterCatalogViews(views, { system: 'snes', region: 'japan' }).map(view => view.live.id), ['megadrive-a', 'snes-b', 'megadrive-c']);
  assert.equal(catalogGenre(views[0]), 'Beat \'em Up');
});

test('collectionLabels resolves rail-order names and skips unknown ids', () => {
  const catalog = [
    { id: 'weekend-queue', name: 'Weekend Queue' },
    { id: 'speedruns', name: 'Speedruns' },
    { id: 'blank-shelf', name: '   ' },
  ];
  assert.deepEqual(collectionLabels({}, catalog), []);
  assert.deepEqual(collectionLabels({ collections: [] }, catalog), []);
  assert.deepEqual(collectionLabels({ collections: ['weekend-queue'] }, []), []);
  assert.deepEqual(collectionLabels({ collections: ['weekend-queue'] }), []);
  assert.deepEqual(collectionLabels({ collections: ['weekend-queue'] }, catalog), ['Weekend Queue']);
  assert.deepEqual(collectionLabels({
    collections: ['speedruns', 'weekend-queue'],
  }, catalog), ['Weekend Queue', 'Speedruns']);
  assert.deepEqual(collectionLabels({
    collections: ['missing-shelf', 'weekend-queue', 'blank-shelf'],
  }, catalog), ['Weekend Queue']);
  assert.deepEqual(collectionLabels({
    collections: ['weekend-queue'],
  }, [{ id: 'weekend-queue' }]), []);
  assert.equal(coverHoverMeta({
    system: 'snes',
    state: 'available',
    region: 'usa',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    collections: ['weekend-queue'],
    kind: 'zip',
  }), 'SNES · USA · 1991 · Ready');
});

test('sourceKindLabel maps zip to ZIP and raw to ROM', () => {
  assert.equal(sourceKindLabel({ kind: 'zip' }), 'ZIP');
  assert.equal(sourceKindLabel({ kind: 'raw' }), 'ROM');
  assert.equal(sourceKindLabel({}), '');
  assert.equal(sourceKindLabel(null), '');
  assert.equal(sourceKindLabel({ kind: 'unknown' }), '');
  assert.equal(sourceKindLabel({ kind: 'iso' }), '');
  assert.equal(sourceKindLabel({ kind: 'ZIP' }), '');
  assert.equal(sourceKindLabel({ kind: '  ' }), '');
  assert.equal(sourceKindLabel({ kind: ' zip ' }), 'ZIP');
  assert.equal(sourceKindLabel({ kind: ' raw ' }), 'ROM');
  assert.equal(dumpIdentityFacts({ kind: 'zip', region: 'usa' }).some(fact => fact.label === 'Source'), false);
  assert.equal(variantLabel({ kind: 'zip', title: 'Mystery Dump' }), 'Mystery Dump · ZIP');
  assert.equal(variantLabel({
    title: 'Sonic the Hedgehog',
    canonical_title: 'Sonic the Hedgehog',
    kind: 'zip',
  }), 'ZIP');
  assert.doesNotMatch(coverHoverMeta({
    system: 'snes', state: 'available', kind: 'zip', region: 'usa', year: '1991',
  }), /ZIP|ROM|zip|raw/);
});

test('variantLabel and dump facts share region revision and flags', () => {
  const dump = {
    title: 'Sonic the Hedgehog (USA) (Rev A) (Beta)',
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
  };
  assert.deepEqual(dumpFlagLabels({}), []);
  assert.deepEqual(dumpFlagLabels({ dump_flags: '' }), []);
  assert.deepEqual(dumpFlagLabels({ dump_flags: 'beta' }), ['Beta']);
  assert.deepEqual(dumpFlagLabels({ dump_flags: 'hack,unl' }), ['Hack']);
  assert.deepEqual(dumpFlagLabels({ dump_flags: 'beta,hack' }), ['Beta', 'Hack']);
  assert.deepEqual(dumpFlagLabels({ dump_flags: 'unl,proto,sample' }), ['Proto', 'Unl']);
  assert.deepEqual(dumpFlagLabels({ dump_flags: 'unknown,beta' }), ['Beta']);
  assert.deepEqual(dumpFlagLabels({ dump_flags: 'unknown' }), []);
  assert.deepEqual(dumpIdentityFacts(dump), [
    { label: 'Region', value: 'USA' },
    { label: 'Revision', value: 'rev a' },
    { label: 'Flags', value: 'Beta' },
  ]);
  assert.deepEqual(dumpIdentityFacts({ dump_flags: 'beta,hack' }), [
    { label: 'Flags', value: 'Beta, Hack' },
  ]);
  assert.deepEqual(dumpIdentityFacts({ dump_flags: 'hack,unl' }), [
    { label: 'Flags', value: 'Hack' },
  ]);
  assert.deepEqual(dumpIdentityFacts({ dump_flags: 'beta,unknown' }), [
    { label: 'Flags', value: 'Beta, unknown' },
  ]);
  assert.equal(variantLabel(dump), 'Sonic the Hedgehog (USA) (Rev A) (Beta) · USA · rev a · Beta');
  assert.equal(variantLabel({
    ...dump,
    canonical_title: 'Sonic the Hedgehog',
    kind: 'zip',
  }), 'Sonic the Hedgehog (USA) (Rev A) (Beta) · USA · rev a · Beta · ZIP');
  assert.equal(variantLabel({
    title: 'Sonic the Hedgehog',
    canonical_title: 'Sonic the Hedgehog',
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
  }), 'USA · rev a · Beta');
  assert.equal(coverHoverMeta({
    system: 'snes', state: 'available', region: 'usa', year: '1991', dump_flags: 'beta',
  }), 'SNES · USA · 1991 · Ready');
  assert.equal(variantLabel({ title: 'Mystery Dump' }), 'Mystery Dump');
  assert.equal(coverHoverMeta({ system: 'megadrive', state: 'available', region: 'usa' }), 'Mega Drive · USA · Ready');
  assert.equal(coverHoverMeta({ system: 'megadrive', state: 'available' }), 'Mega Drive · Ready');
  assert.equal(coverHoverMeta({
    system: 'megadrive', state: 'available', region: 'usa', root_online: false,
  }), 'Mega Drive · USA · Offline');
  assert.equal(coverHoverMeta({
    system: 'megadrive', state: 'available', region: 'usa', root_online: false, year: '1992',
  }), 'Mega Drive · USA · 1992 · Offline');
  assert.equal(coverHoverMeta({
    system: 'megadrive', state: 'available', region: 'usa', root_online: false, year: '1992',
  }, { presentation: { isFallback: false, year: '1991' } }), 'Mega Drive · USA · 1992 · Offline');
  assert.equal(coverHoverMeta({
    system: 'megadrive', state: 'available', region: 'usa', root_online: false,
  }, { presentation: { isFallback: false, year: '1991' } }), 'Mega Drive · USA · 1991 · Offline');
  assert.equal(coverHoverMeta({
    system: 'megadrive', state: 'available', region: 'usa', root_online: false,
  }, { presentation: { isFallback: false, year: '' } }), 'Mega Drive · USA · Offline');
  assert.equal(coverHoverMeta({
    system: 'megadrive', state: 'available', region: 'usa', root_online: false,
  }, { presentation: { isFallback: false, year: '—' } }), 'Mega Drive · USA · Offline');
  assert.equal(coverHoverMeta({ system: 'snes', state: 'invalid', root_online: true }), 'SNES · Unreadable');
  assert.equal(coverStatusLabel({ state: 'available', root_online: false }), 'Offline');
  assert.equal(coverStatusLabel({ state: 'invalid', root_online: true }), 'Unreadable');
  assert.equal(cardSourceOffline({ state: 'available', root_online: false }), true);
  assert.equal(cardSourceOffline({ state: 'invalid', root_online: true }), false);
  assert.equal(cardSourceUnreadable({ state: 'invalid', root_online: true }), true);
  assert.equal(cardSourceUnreadable({ state: 'invalid', root_online: false }), false);
  assert.equal(isSessionPlayingCard(
    { state: 'active', game_id: 'megadrive-sonic-japan' },
    { id: 'megadrive-sonic-usa', group_key: 'megadrive\u001fsonic' },
    { id: 'megadrive-sonic-japan', group_key: 'megadrive\u001fsonic' },
  ), true);
  assert.equal(isSessionPlayingCard(
    { state: 'active', game_id: 'megadrive-sonic-japan' },
    { id: 'snes-mario-test', group_key: 'snes\u001fmario' },
    { id: 'megadrive-sonic-japan', group_key: 'megadrive\u001fsonic' },
  ), false);
  assert.equal(isSessionPlayingCard(
    { state: 'active', game_id: 'megadrive-sonic-japan' },
    { id: 'megadrive-sonic-usa', group_key: 'megadrive\u001fsonic' },
    { id: 'megadrive-sonic-japan', group_key: 'megadrive\u001fsonic' },
    'authoritative',
  ), true);
  assert.equal(isSessionPlayingCard(
    { state: 'active', game_id: 'megadrive-sonic-japan' },
    { id: 'megadrive-sonic-usa', group_key: 'megadrive\u001fsonic' },
    { id: 'megadrive-sonic-japan', group_key: 'megadrive\u001fsonic' },
    'last-known',
  ), false);
  assert.equal(isSessionPlayingCard(
    { state: 'active', game_id: 'megadrive-sonic-japan' },
    { id: 'megadrive-sonic-usa', group_key: 'megadrive\u001fsonic' },
    { id: 'megadrive-sonic-japan', group_key: 'megadrive\u001fsonic' },
    'indeterminate',
  ), false);
});

test('unmatched catalog cards stay quiet and sort/count the visible library', () => {
  const views = [
    { live: { id: 'megadrive-b', title: 'Streets of Rage 2 (USA)', system: 'megadrive', year: '1993' }, presentation: { isFallback: false, year: '1993' } },
    { live: { id: 'snes-a', title: 'ActRaiser (USA)', system: 'snes', year: '1991' }, presentation: { isFallback: false, year: '1991' } },
    { live: { id: 'megadrive-c', title: 'Unmatched Dump (Japan)', system: 'megadrive' }, presentation: fallbackPresentation() },
  ];
  assert.equal(fallbackPresentation().cover, undefined);
  assert.equal(fallbackPresentation().summary, '');
  assert.deepEqual(sortCatalogViews(views, 'year').map(view => view.live.id), ['megadrive-b', 'snes-a', 'megadrive-c']);
  assert.deepEqual(sortCatalogViews(views, 'system').map(view => view.live.id), ['megadrive-b', 'megadrive-c', 'snes-a']);
  assert.equal(formatCatalogCount(3, 2138), '3 of 2,138 games');
  assert.equal(formatCatalogCount(2138, 2138), '2,138 games');
});

test('library wording shows clean titles and honest launch blocks', () => {
  assert.equal(displayTitle('ActRaiser 2 (USA)'), 'ActRaiser 2');
  assert.equal(displayTitle('007 Shitou - The Duel (Japan)'), '007 Shitou - The Duel');
  assert.equal(displayTitle('Bare Knuckle ~ Streets of Rage (World) (Rev A)'), 'Bare Knuckle ~ Streets of Rage');
  assert.equal(systemLabel('megadrive'), 'Mega Drive');
  assert.equal(systemLabel('snes'), 'SNES');
  assert.equal(systemLabel('gbc'), 'Game Boy Color');
  assert.equal(systemLabel('a2600'), 'Atari 2600');
  assert.equal(systemLabel('coleco'), 'ColecoVision');
  assert.equal(systemLabel('lynx'), 'Atari Lynx');
  assert.equal(sourceLabel('available'), 'Ready');
  assert.equal(sourceLabel('missing'), 'Offline');
  assert.equal(launchBlockReason({ state: 'available', content_prepared: true, root_online: true }), '');
  assert.equal(launchBlockReason({ state: 'available', content_prepared: false, root_online: true }), '');
  assert.equal(launchBlockReason({ state: 'missing', content_prepared: false, root_online: false }), 'This game’s source is offline.');
});

test('presentation route is a host-local path and parser bounds provider fields', () => {
  assert.equal(presentationPath('megadrive-sonic-test'), '/api/v1/presentation/games/megadrive-sonic-test');
  assert.throws(() => presentationPath('../secret'), error => error.code === 'BAD_REQUEST');
  const parsed = parsePresentation({
    game_id: 'megadrive-sonic-test',
    state: 'ready',
    presentation: {
      summary: 'Host metadata summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1',
      cover_artwork_id: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
    },
    attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
  }, immutableBoundaryGame());
  assert.equal(parsed.isFallback, false);
  assert.equal(parsed.summary, 'Host metadata summary');
  assert.equal(parsed.coverArtworkHandle.length, 64);
  assert.equal(parsed.attribution, 'Data from IGDB.com');
  assert.equal(parsePresentation({ game_id: 'megadrive-sonic-test', state: 'ready', presentation: { summary: 'x'.repeat(241) }, attribution: { provider: 'igdb', label: 'Data from IGDB.com' } }, immutableBoundaryGame()).isFallback, true);
});

test('prefetchVisibleCovers loads presentation for intersecting cards', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
    '/api/v1/presentation/games/megadrive-sonic-test': [jsonResponse({
      game_id: 'megadrive-sonic-test', state: 'ready',
      presentation: { summary: 'Visible cover', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1' },
      attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
    })],
    '/api/v1/presentation/games/snes-unknown-test': [jsonResponse({
      game_id: 'snes-unknown-test', state: 'no_match',
    })],
    '/api/v1/presentation/games/snes-offline-test': [jsonResponse({
      game_id: 'snes-offline-test', state: 'no_match',
    })],
  });
  const observers = [];
  const controller = createAppController({
    fetchImpl,
    presentationEnabled: true,
    prefetchVisibleCovers: true,
    IntersectionObserver: class {
      constructor(callback) { this.callback = callback; observers.push(this); }
      observe(node) { this.callback([{ isIntersecting: true, target: node }]); }
      disconnect() {}
    },
  });
  await controller.loadCatalog('');
  controller.observeVisibleCovers();
  await waitForCondition(() => {
    const sonic = controller.getState().gameViews.find(view => view.live.id === 'megadrive-sonic-test');
    return sonic && sonic.presentation.summary === 'Visible cover';
  }, 'visible presentation did not apply');
  assert.ok(calls.some(call => call.path === '/api/v1/presentation/games/megadrive-sonic-test'));
  const sonic = controller.getState().gameViews.find(view => view.live.id === 'megadrive-sonic-test');
  assert.equal(sonic.presentation.isFallback, false);
  assert.equal(sonic.live.year, undefined);
  assert.equal(sonic.presentation.year, '1991');
  assert.equal(coverHoverMeta(sonic.live, sonic), 'Mega Drive · 1991 · Ready');
});

test('catalog refresh keeps already-fetched covers', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const presentation = jsonResponse({
    game_id: 'megadrive-sonic-test',
    state: 'ready',
    presentation: {
      summary: 'Cached summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1',
      cover_artwork_id: handle,
    },
    attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
  });
  const { fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json')), jsonResponse(readFixture('catalog-populated.json'))],
    '/api/v1/presentation/games/megadrive-sonic-test': [presentation],
    '/api/v1/presentation/games/snes-unknown-test': [jsonResponse({ game_id: 'snes-unknown-test', state: 'no_match' })],
    '/api/v1/presentation/games/snes-offline-test': [jsonResponse({ game_id: 'snes-offline-test', state: 'no_match' })],
  });
  const controller = createAppController({
    fetchImpl,
    presentationEnabled: true,
    prefetchVisibleCovers: true,
    IntersectionObserver: class {
      constructor(callback) { this.callback = callback; }
      observe(node) { this.callback([{ isIntersecting: true, target: node }]); }
      disconnect() {}
    },
  });
  await controller.loadCatalog('');
  controller.observeVisibleCovers();
  await waitForCondition(() => {
    const sonic = controller.getState().gameViews.find(view => view.live.id === 'megadrive-sonic-test');
    return sonic && sonic.presentation.coverArtworkHandle === handle;
  }, 'cover did not apply before refresh');
  await controller.loadCatalog('');
  const refreshed = controller.getState().gameViews.find(view => view.live.id === 'megadrive-sonic-test');
  assert.equal(refreshed.presentation.coverArtworkHandle, handle);
  assert.equal(refreshed.presentation.summary, 'Cached summary');
});

test('platform navigation restores covers after switching away and back', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const sonic = {
    id: 'megadrive-sonic-test',
    title: 'Sonic the Hedgehog',
    system: 'megadrive',
    kind: 'zip',
    state: 'available',
    root_online: true,
    content_prepared: true,
    execution: 'fpga_native',
  };
  const mario = {
    id: 'snes-mario-test',
    title: 'Mario',
    system: 'snes',
    kind: 'raw',
    state: 'available',
    root_online: true,
    content_prepared: true,
    execution: 'fpga_native',
  };
  const presentation = jsonResponse({
    game_id: 'megadrive-sonic-test',
    state: 'ready',
    presentation: {
      summary: 'Cached summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1',
      cover_artwork_id: handle,
    },
    attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
  });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse({ games: [sonic, mario] })],
    '/api/v1/games?platform=snes': [jsonResponse({ games: [mario] })],
    '/api/v1/games?platform=megadrive': [jsonResponse({ games: [sonic] })],
    '/api/v1/presentation/games/megadrive-sonic-test': [presentation],
    '/api/v1/presentation/games/snes-mario-test': [jsonResponse({ game_id: 'snes-mario-test', state: 'no_match' })],
  });
  const controller = createAppController({
    fetchImpl,
    presentationEnabled: true,
    prefetchVisibleCovers: true,
    IntersectionObserver: class {
      constructor(callback) { this.callback = callback; }
      observe(node) { this.callback([{ isIntersecting: true, target: node }]); }
      disconnect() {}
    },
  });
  await controller.loadCatalog('');
  controller.observeVisibleCovers();
  await waitForCondition(() => {
    const view = controller.getState().gameViews.find(item => item.live.id === 'megadrive-sonic-test');
    return view && view.presentation.coverArtworkHandle === handle;
  }, 'cover did not apply before platform change');
  await controller.setLibraryNav('', 'snes');
  assert.equal(controller.getState().gameViews.some(item => item.live.id === 'megadrive-sonic-test'), false);
  await controller.setLibraryNav('', 'megadrive');
  const restored = controller.getState().gameViews.find(item => item.live.id === 'megadrive-sonic-test');
  assert.equal(restored.presentation.coverArtworkHandle, handle);
  assert.equal(
    calls.filter(call => call.path === '/api/v1/presentation/games/megadrive-sonic-test').length,
    1,
  );
});

test('prefetchVisibleCovers does not refetch after fallback presentation', async () => {
  let controller;
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
    '/api/v1/presentation/games/megadrive-sonic-test': [jsonResponse({
      game_id: 'megadrive-sonic-test', state: 'ready',
      presentation: { summary: 'Visible cover', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1' },
      attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
    })],
    '/api/v1/presentation/games/snes-unknown-test': [jsonResponse({ game_id: 'snes-unknown-test', state: 'no_match' })],
    '/api/v1/presentation/games/snes-offline-test': [jsonResponse({ game_id: 'snes-offline-test', state: 'no_match' })],
  });
  controller = createAppController({
    fetchImpl,
    presentationEnabled: true,
    prefetchVisibleCovers: true,
    onStateChange() {
      controller.observeVisibleCovers();
    },
    IntersectionObserver: class {
      constructor(callback) { this.callback = callback; }
      observe(node) { this.callback([{ isIntersecting: true, target: node }]); }
      disconnect() {}
    },
  });
  await controller.loadCatalog('');
  await waitForCondition(() => {
    return calls.filter(call => call.path.startsWith('/api/v1/presentation/games/')).length >= 3;
  }, 'visible presentations were not fetched');
  const fetched = calls.filter(call => call.path.startsWith('/api/v1/presentation/games/')).length;
  controller.observeVisibleCovers();
  controller.observeVisibleCovers();
  assert.equal(fetched, 3);
  assert.equal(calls.filter(call => call.path.startsWith('/api/v1/presentation/games/')).length, 3);
});

test('parser accepts LaunchBox ready attribution and rejects unknown providers', () => {
  const parsed = parsePresentation({
    game_id: 'megadrive-sonic-test',
    state: 'ready',
    presentation: { summary: 'Blue hedgehog.', year: '1991', genre: 'Platform', studio: 'Sonic Team', players: '1' },
    attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
  }, immutableBoundaryGame());
  assert.equal(parsed.isFallback, false);
  assert.equal(parsed.attribution, 'Data from LaunchBox Games Database');
  assert.equal(parsePresentation({
    game_id: 'megadrive-sonic-test',
    state: 'ready',
    presentation: { summary: 'x' },
    attribution: { provider: 'steam', label: 'Steam' },
  }, immutableBoundaryGame()).isFallback, true);
});

test('ready provider response preserves empty fields and renders no demo artwork', () => {
  const parsed = parsePresentation(readFixture('presentation-ready-empty.json'), immutableBoundaryGame());
  assert.equal(parsed.isFallback, false);
  assert.equal(parsed.metadataState, 'ready');
  for (const field of ['summary', 'year', 'genre', 'studio', 'players']) assert.equal(parsed[field], '');
  assert.equal(parsed.cover, undefined);
  assert.equal(parsed.backdrop, undefined);
  assert.equal(parsed.coverArtworkHandle, undefined);
  assert.equal(parsed.backdropArtworkHandle, undefined);
  assert.equal(parsed.attribution, 'Data from IGDB.com');
});

test('enabled controller requests presentation only after selecting a live game', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(readFixture('detail-refreshed.json'))],
    '/api/v1/presentation/games/megadrive-sonic-test': [jsonResponse({
      game_id: 'megadrive-sonic-test', state: 'ready',
      presentation: { summary: 'Host summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1' },
      attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
    }), jsonResponse({
      game_id: 'megadrive-sonic-test', state: 'ready',
      presentation: { summary: 'Host summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1' },
      attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
    })],
  });
  const controller = createAppController({ fetchImpl, presentationEnabled: true });
  await controller.loadCatalog('');
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/games?grouped=1']);
  await controller.selectGame('megadrive-sonic-test');
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/games?grouped=1', '/api/v1/games/megadrive-sonic-test', '/api/v1/presentation/games/megadrive-sonic-test', '/api/v1/presentation/games/megadrive-sonic-test']);
  const state = controller.getState();
  assert.equal(state.metadataState, 'ready');
  assert.equal(state.selectedGameView.presentation.summary, 'Host summary');
  assert.equal(state.selectedGameView.presentation.isFallback, false);
});

test('same-ID detail identity replacement invalidates stale presentation and refreshes once', async () => {
  const calls = [];
  let resolveDetail;
  let resolveOldPresentation;
  let resolveNewPresentation;
  let presentationCalls = 0;
  const gameID = 'megadrive-sonic-test';
  const fetchImpl = async (requestPath, options) => {
    calls.push({ path: requestPath, options });
    if (gamesRequestKey(requestPath) === '/api/v1/games') return jsonResponse(readFixture('catalog-populated.json'));
    if (requestPath === `/api/v1/games/${gameID}`) {
      return new Promise(resolve => { resolveDetail = resolve; });
    }
    if (requestPath === `/api/v1/presentation/games/${gameID}`) {
      presentationCalls += 1;
      if (presentationCalls === 1) return new Promise(resolve => { resolveOldPresentation = resolve; });
      if (presentationCalls === 2) return new Promise(resolve => { resolveNewPresentation = resolve; });
      throw new Error('same-ID identity replacement issued more than one replacement request');
    }
    if (requestPath === '/api/v1/session/launch') return jsonResponse({ state: 'active', game_id: gameID });
    throw new Error(`unexpected request ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, presentationEnabled: true });

  await controller.loadCatalog('');
  const selecting = controller.selectGame(gameID);
  await waitForCondition(() => resolveDetail && resolveOldPresentation, 'initial detail/presentation requests did not start');
  resolveDetail(jsonResponse(immutableBoundaryGame({ title: 'Sonic the Hedgehog (new)', system: 'snes' })));
  await waitForCondition(() => presentationCalls === 2, 'detail identity replacement did not start exactly one presentation refresh');

  resolveOldPresentation(jsonResponse({
    game_id: gameID,
    state: 'ready',
    presentation: { summary: 'old presentation', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1' },
    attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
  }));
  await settleBrowser();
  let state = controller.getState();
  assert.equal(state.selectedLiveGame.title, 'Sonic the Hedgehog (new)');
  assert.equal(state.selectedLiveGame.system, 'snes');
  assert.notEqual(state.selectedGameView.presentation.summary, 'old presentation');

  resolveNewPresentation(jsonResponse({
    game_id: gameID,
    state: 'ready',
    presentation: { summary: 'new presentation', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1' },
    attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
  }));
  await selecting;
  state = controller.getState();
  assert.equal(state.selectedGameView.presentation.summary, 'new presentation');
  assert.equal(state.metadataState, 'ready');

  await controller.launchSelected();
  state = controller.getState();
  assert.equal(state.launchState, 'launch_success');
  assert.deepEqual(calls.filter(call => call.path === `/api/v1/presentation/games/${gameID}`).map(call => call.path), [
    `/api/v1/presentation/games/${gameID}`,
    `/api/v1/presentation/games/${gameID}`,
  ]);
  assert.equal(calls.at(-1).path, '/api/v1/session/launch');
  assert.equal(calls.at(-1).options.body, JSON.stringify({ game_id: gameID }));
});

test('presentation parser accepts the nested host DTO and preserves safe provider states', () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const ready = parsePresentation({
    game_id: 'megadrive-sonic-test',
    state: 'ready',
    presentation: {
      summary: 'Nested host summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1',
      cover_artwork_id: handle,
    },
    attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
  }, immutableBoundaryGame());
  assert.equal(ready.isFallback, false);
  assert.equal(ready.summary, 'Nested host summary');
  assert.equal(ready.coverArtworkHandle, handle);
  assert.equal(ready.attribution, 'Data from IGDB.com');
  for (const state of ['disabled', 'unconfigured', 'no_match', 'ambiguous', 'offline']) {
    const fallback = parsePresentation({ game_id: 'megadrive-sonic-test', state }, immutableBoundaryGame());
    assert.equal(fallback.isFallback, true, state);
    assert.equal(fallback.metadataState, state === 'no_match' ? 'fallback_no_match' : state === 'ambiguous' ? 'fallback_ambiguous' : state === 'disabled' ? 'fallback_disabled' : state === 'unconfigured' ? 'fallback_unconfigured' : 'fallback_offline');
  }
});

test('valid provider-ready empty fields stay empty and carry no demo artwork style', () => {
  const parsed = parsePresentation(readFixture('presentation-ready-empty.json'), immutableBoundaryGame());

  assert.equal(parsed.isFallback, false);
  assert.equal(parsed.metadataState, 'ready');
  for (const field of ['summary', 'year', 'genre', 'studio', 'players']) {
    assert.equal(parsed[field], '', field);
  }
  assert.equal(parsed.cover, undefined);
  assert.equal(parsed.backdrop, undefined);
  assert.equal(parsed.coverArtworkHandle, undefined);
  assert.equal(parsed.backdropArtworkHandle, undefined);
  assert.equal(parsed.attribution, 'Data from IGDB.com');
});

test('presentation parser preserves nonempty fields when one optional text field is empty', () => {
  const fields = [
    ['summary', ''],
    ['year', ''],
    ['genre', ''],
    ['studio', ''],
    ['players', ''],
  ];
  for (const [field, fallback] of fields) {
    const payload = {
      game_id: 'megadrive-sonic-test',
      state: 'ready',
      presentation: { summary: 'Summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1 player' },
      attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
    };
    payload.presentation[field] = '';
    const parsed = parsePresentation(payload, immutableBoundaryGame());
    assert.equal(parsed.isFallback, false, field);
    assert.equal(parsed.metadataState, 'ready', field);
    assert.equal(parsed[field], fallback, field);
    for (const otherField of ['summary', 'year', 'genre', 'studio', 'players']) {
      if (otherField !== field) assert.notEqual(parsed[otherField], '', `${field} erased ${otherField}`);
    }
  }
});

test('presentation parser rejects malformed and non-positive optional text values', () => {
  for (const value of [null, false, -1, {}, []]) {
    const payload = {
      game_id: 'megadrive-sonic-test',
      state: 'ready',
      presentation: { summary: 'Summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: value },
      attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
    };
    const parsed = parsePresentation(payload, immutableBoundaryGame());
    assert.equal(parsed.isFallback, true, `players=${String(value)}`);
    assert.notEqual(parsed.metadataState, 'ready', `players=${String(value)}`);
  }
});

function mutateEveryReachableAdapterField(input) {
  for (const key of Object.keys(input)) {
    try {
      if (input[key] && typeof input[key] === 'object') {
        input[key].marker = `mutated-${key}`;
      } else {
        input[key] = `mutated-${key}`;
      }
    } catch (_) {
      // A correctly detached frozen projection rejects every mutation.
    }
  }
}

async function runImmutableAdapterCase(kind) {
  const accepted = immutableBoundaryGame();
  const detail = immutableBoundaryGame({
    title: 'Sonic the Hedgehog (detail)',
    nested: { marker: 'detail-nested-value' },
  });
  const inputs = [];
  let adapterCalls = 0;
  const adapter = kind === 'metadataFor'
    ? {
      metadataFor(input) {
        adapterCalls += 1;
        inputs.push(input);
        mutateEveryReachableAdapterField(input);
        if (adapterCalls > 1) throw new Error('later metadataFor call must not happen');
        return {
          ...validAdapterPresentation(),
          id: 'poisoned-id',
          title: 'Poisoned title',
          system: 'poisoned-system',
          kind: 'poisoned-kind',
          state: 'blocked',
          root_online: false,
          content_prepared: false,
          execution: 'poisoned-execution',
        };
      },
    }
    : {
      toLauncherGame(input) {
        adapterCalls += 1;
        inputs.push(input);
        mutateEveryReachableAdapterField(input);
        if (adapterCalls > 1) throw new Error('later toLauncherGame call must not happen');
        return {
          id: 'poisoned-id',
          title: 'Poisoned title',
          system: 'poisoned-system',
          kind: 'poisoned-kind',
          state: 'blocked',
          root_online: false,
          content_prepared: false,
          execution: 'poisoned-execution',
          presentation: validAdapterPresentation(),
        };
      },
    };
  const { calls, fetchImpl } = queuedFetch([
    jsonResponse({ games: [accepted] }),
    jsonResponse(detail),
    jsonResponse({ state: 'active', game_id: accepted.id }),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: adapter });

  await controller.loadCatalog('');
  let state = controller.getState();
  assert.equal(adapterCalls, 1, `${kind} must run once for the accepted catalog record`);
  assert.deepEqual(Object.keys(inputs[0]), ['id', 'title', 'system']);
  assert.equal(Object.isFrozen(inputs[0]), true);
  assert.equal(inputs[0].nested, undefined);
  assert.equal(state.games[0].id, accepted.id);
  assert.equal(state.games[0].title, accepted.title);
  assert.equal(state.games[0].system, accepted.system);
  assert.equal(state.games[0].kind, accepted.kind);
  assert.equal(state.games[0].root_online, accepted.root_online);
  assert.equal(state.games[0].content_prepared, accepted.content_prepared);
  assert.equal(state.games[0].state, accepted.state);
  assert.equal(state.games[0].execution, accepted.execution);
  assert.equal(state.games[0].nested, undefined);
  assert.equal(accepted.nested.marker, 'original-nested-value');
  assert.equal(Object.isFrozen(state.games[0]), true);
  assert.deepEqual(Object.keys(state.gameViews[0]), ['live', 'presentation']);
  assert.equal(Object.isFrozen(state.gameViews[0]), true);
  assert.equal(state.gameViews[0].live, state.games[0]);
  assert.equal(state.gameViews[0].live.id, accepted.id);
  assert.equal(state.gameViews[0].live.title, accepted.title);
  assert.equal(state.gameViews[0].presentation.isFallback, true);
  assert.equal(state.metadataFallbackCount, 1);

  await controller.selectGame(accepted.id);
  state = controller.getState();
  assert.equal(adapterCalls, 2, `${kind} must run once for the distinct accepted detail record`);
  assert.equal(state.selectedLiveGame.id, accepted.id);
  assert.equal(state.selectedLiveGame.title, detail.title);
  assert.equal(state.selectedLiveGame.kind, detail.kind);
  assert.equal(state.selectedLiveGame.root_online, detail.root_online);
  assert.equal(state.selectedLiveGame.nested, undefined);
  assert.equal(state.selectedGameView.live, state.selectedLiveGame);
  assert.equal(state.selectedGameView.live.id, accepted.id);
  assert.equal(state.selectedGameView.live.title, detail.title);
  assert.equal(state.selectedGameView.presentation.isFallback, true);
  assert.equal(Object.isFrozen(state.selectedLiveGame), true);
  assert.equal(Object.isFrozen(state.selectedGameView), true);

  await controller.launchSelected();
  state = controller.getState();
  assert.equal(adapterCalls, 2, `${kind} must not rerun during launch or state inspection`);
  assert.equal(state.launchState, 'launch_success');
  assert.deepEqual(calls.map(call => call.path), [
    '/api/v1/games?grouped=1',
    '/api/v1/games/megadrive-sonic-test',
    '/api/v1/session/launch',
  ]);
  assert.equal(calls[2].options.body, '{"game_id":"megadrive-sonic-test"}');
}

test('gamesPath delegates search membership to the live API', () => {
  assert.equal(gamesPath(''), '/api/v1/games?grouped=1');
  assert.equal(gamesPath('sonic & tails'), '/api/v1/games?q=sonic%20%26%20tails&grouped=1');
  assert.equal(gamesPath('', { collection: 'favorites' }), '/api/v1/games?collection=favorites&grouped=1');
  assert.equal(gamesPath('sonic', { platform: 'snes' }), '/api/v1/games?q=sonic&platform=snes&grouped=1');
  assert.equal(gamesPath('', { collection: 'recents', sort: 'recents' }), '/api/v1/games?collection=recents&grouped=1');
  assert.equal(gamesPath('', { collection: 'recents', sort: 'title' }), '/api/v1/games?collection=recents&grouped=1');
});

test('collection See-alls and Home override Year/System without mutating stored sort', () => {
  assert.equal(collectionFetchSort('continue'), 'title');
  assert.equal(collectionFetchSort('favorites'), 'title');
  assert.equal(collectionFetchSort('recents'), 'recents');
  assert.equal(collectionFetchSort('unplayed'), 'title');
  assert.equal(collectionFetchSort('weekend-queue'), 'title');
  assert.equal(collectionFetchSort('recently_added'), 'recently_added');
  assert.equal(catalogSortOverridden({ libraryView: 'home', collection: '', sort: 'year' }), true);
  assert.equal(catalogSortOverridden({ libraryView: 'grid', collection: 'continue', sort: 'year' }), true);
  assert.equal(catalogSortOverridden({ libraryView: 'grid', collection: 'weekend-queue', sort: 'system' }), true);
  assert.equal(catalogSortOverridden({ libraryView: 'grid', collection: '', sort: 'year' }), false);
  assert.equal(catalogEffectiveSort({ libraryView: 'home', collection: '', sort: 'year' }), 'title');
  assert.equal(catalogEffectiveSort({ libraryView: 'grid', collection: 'favorites', sort: 'year' }), 'title');
  assert.equal(catalogEffectiveSort({ libraryView: 'grid', collection: 'recents', sort: 'year' }), 'recents');
  assert.equal(catalogEffectiveSort({ libraryView: 'grid', collection: 'recently_added', sort: 'year' }), 'recently_added');
  assert.equal(catalogEffectiveSort({ libraryView: 'grid', collection: '', sort: 'year' }), 'year');
  assert.equal(catalogSortOverrideLabel('title'), 'Sort (Title)');
  assert.equal(catalogSortOverrideLabel('recents'), 'Sort (Recent)');
  assert.equal(catalogSortOverrideLabel('recently_added'), 'Sort (Recently added)');
  assert.equal(catalogSortControlHidden({ libraryView: 'home', collection: '', sort: 'year' }), true);
  assert.equal(catalogSortControlHidden({ libraryView: 'grid', collection: '', sort: 'year' }), false);
  assert.equal(catalogSortControlHidden({ libraryView: 'grid', collection: 'recents', sort: 'year' }), false);
  assert.equal(catalogSortControlHidden({ libraryView: 'grid', collection: 'recently_added', sort: 'title' }), false);
});

test('setCatalogSort ignores recents so All-games Year does not become Title', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const populated = jsonResponse({ games: [sonic] });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [populated, populated],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.setLibraryNav('', '');
  await controller.setCatalogSort('year');
  assert.equal(controller.getState().sort, 'year');
  const before = calls.length;
  await controller.setCatalogSort('recents');
  assert.equal(controller.getState().sort, 'year');
  assert.equal(calls.length, before);
  assert.equal(calls.filter(call => call.path === '/api/v1/games?sort=year&grouped=1').length, 1);
});

test('gameDetailPath safely encodes the live catalog ID', () => {
  assert.equal(gameDetailPath('game/id'), '/api/v1/games/game%2Fid');
});

test('launchRequest preserves the exact live game ID contract', () => {
  const request = launchRequest({
    id: 'live-id',
    presentation: { id: 'fake-id' },
  });
  assert.equal(request.path, '/api/v1/session/launch');
  assert.equal(request.options.method, 'POST');
  assert.equal(request.options.headers['Content-Type'], 'application/json');
  assert.deepEqual(JSON.parse(request.options.body), { game_id: 'live-id' });
});

test('detail rendering owns the prompt and preserves the labelled active heading', () => {
  const shell = readAsset('ui_shell.html');
  const app = readAsset('ui_app.js');
  assert.match(shell, /<div id="detail-content"><\/div>/, 'the shell must leave the replaceable detail subtree empty');
  assert.match(shell, /aria-labelledby="detail-heading"/);
  assert.doesNotMatch(shell, /class="detail-empty"/);
  assert.equal(detailHeading(null), 'Select a game');
  assert.equal(detailHeading({ title: 'Sonic & Tails' }), 'Sonic & Tails');
  assert.match(app, /detailContent\.replaceChildren\(\)/);
  assert.match(app, /const heading = element\('h2', '', detailHeading\(game\)\);/);
  assert.match(app, /nodes\.detailContent\.appendChild\(heading\);/);
});

test('launch states announce through a dedicated polite live region', () => {
  const shell = readAsset('ui_shell.html');
  const app = readAsset('ui_app.js');
  assert.match(shell, /id="launch-status"[^>]*aria-live="polite"/);
  assert.deepEqual(launchStatus('idle'), { text: '', role: 'status' });
  assert.deepEqual(launchStatus('launching'), {
    text: 'launching: preparing the selected live game…', role: 'status',
  });
  assert.deepEqual(launchStatus('launch_success'), {
    text: 'launch_success: session accepted by the local host.', role: 'status',
  });
  assert.deepEqual(launchStatus('launch_error', 'safe failure'), {
    text: 'safe failure', role: 'alert',
  });
  assert.match(app, /const presentation = launchStatus\(state\.launchState, state\.launchMessage\);/);
  assert.match(app, /nodes\.launchStatus\.textContent = presentation\.text;/);
  assert.match(app, /nodes\.launchStatus\.setAttribute\('role', presentation\.role\);/);
});

test('selected catalog cards expose pressed state while retaining live IDs', () => {
  const app = readAsset('ui_app.js');
  assert.equal(isSelectedGame({ id: 'live-id' }, { id: 'live-id' }), true);
  assert.equal(isSelectedGame({ id: 'live-id' }, { id: 'other-id' }), false);
  assert.equal(isSelectedGame(null, { id: 'live-id' }), false);
  assert.match(app, /const selected = isSelectedGame\(state\.selectedLiveGame, game\);/);
  assert.match(app, /card\.setAttribute\('aria-pressed', String\(selected\)\);/);
  assert.match(app, /selectGame\(game\)/);
});

test('card labels remain text descendants rather than dynamic user-string attributes', () => {
  const app = readAsset('ui_app.js');
  assert.doesNotMatch(app, /setAttribute\('aria-label', `Select \$\{game\.title\}`\)/);
  assert.match(app, /card\.appendChild\(element\('h3', '', cardTitle\(game\)\)\);/);
});

test('catalog cards defer unreliable execution compatibility data', () => {
  const app = readAsset('ui_app.js');
  assert.doesNotMatch(app, /game\.execution/);
  assert.match(app, /game\.state/);
  assert.match(app, /source\.content_prepared/);
  assert.match(app, /content_prepared: contentPrepared/);
});

test('controller loads fixture catalog, keeps unmatched metadata non-fatal, and refreshes detail', async () => {
  const { calls, fetchImpl } = queuedFetch([
    jsonResponse(readFixture('catalog-populated.json')),
    jsonResponse(readFixture('detail-refreshed.json')),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });

  await controller.loadCatalog('');
  let state = controller.getState();
  assert.equal(catalogViewState(state), 'populated');
  assert.equal(state.games.length, 3);
  assert.equal(state.metadataFallbackCount, 3);
  assert.equal(state.selectedLiveGame, null);

  await controller.selectGame('megadrive-sonic-test');
  state = controller.getState();
  assert.equal(state.detailState, 'populated');
  assert.equal(state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame.title, 'Sonic the Hedgehog (detail refresh)');
  assert.deepEqual(calls.map(call => call.path), [
    '/api/v1/games?grouped=1',
    '/api/v1/games/megadrive-sonic-test',
  ]);
});

test('accepted catalog refresh rebinds selected live fields and clears launch state', async () => {
  const { fetchImpl } = queuedFetch([
    jsonResponse(readFixture('catalog-populated.json')),
    jsonResponse(readFixture('detail-refreshed.json')),
    jsonResponse(readFixture('launch-success.json')),
    jsonResponse(readFixture('catalog-newer.json')),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });

  await controller.loadCatalog('');
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  assert.equal(controller.getState().launchState, 'launch_success');

  await controller.loadCatalog('');
  const state = controller.getState();
  assert.equal(state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame.title, 'Sonic the Hedgehog (refreshed)');
  assert.equal(state.selectedLiveGame.content_prepared, false);
  assert.equal(state.launchState, 'idle');
  assert.equal(state.detailState, 'idle');
});

test('accepted catalog refresh clears a selection that disappeared from the live response', async () => {
  const { fetchImpl } = queuedFetch([
    jsonResponse(readFixture('catalog-populated.json')),
    jsonResponse(readFixture('detail-refreshed.json')),
    jsonResponse(readFixture('launch-success.json')),
    jsonResponse(readFixture('catalog-empty.json')),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });

  await controller.setLibraryNav('', '');
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  await controller.loadCatalog('missing');
  const state = controller.getState();
  assert.equal(state.libraryView, 'grid');
  assert.equal(catalogViewState(state), 'no_matches');
  assert.equal(state.selectedLiveGame, null);
  assert.equal(state.launchState, 'idle');
  assert.equal(state.detailState, 'idle');
});

test('slow stale search responses cannot replace a newer fixture response', async () => {
  let resolveOld;
  let resolveNew;
  const fetchImpl = requestPath => {
    if (String(requestPath).includes('q=old')) return new Promise(resolve => { resolveOld = resolve; });
    if (String(requestPath).includes('q=new')) return new Promise(resolve => { resolveNew = resolve; });
    throw new Error(`unexpected request ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  const oldRequest = controller.loadCatalog('old');
  const newRequest = controller.loadCatalog('new');
  resolveNew(jsonResponse(readFixture('catalog-newer.json')));
  await newRequest;
  resolveOld(jsonResponse(readFixture('catalog-populated.json')));
  await oldRequest;

  const state = controller.getState();
  assert.equal(state.query, 'new');
  assert.equal(state.games.length, 1);
  assert.equal(state.games[0].title, 'Sonic the Hedgehog (refreshed)');
});

test('empty, malformed, and backend-error fixture responses become distinct safe states', async () => {
  const cases = [
    {
      fixture: 'catalog-empty.json',
      query: '',
      state: 'empty',
      errorCode: '',
    },
    {
      fixture: 'catalog-empty.json',
      query: 'missing',
      state: 'no_matches',
      errorCode: '',
    },
    {
      fixture: 'catalog-malformed.json',
      query: '',
      state: 'catalog_error',
      errorCode: 'MALFORMED_RESPONSE',
    },
    {
      fixture: 'catalog-error.json',
      query: '',
      responseStatus: 500,
      state: 'catalog_error',
      errorCode: 'INTERNAL',
    },
  ];
  for (const testCase of cases) {
    const { fetchImpl } = queuedFetch([
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture(testCase.fixture), testCase.responseStatus || 200),
    ]);
    const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
    await controller.setLibraryNav('', '');
    await controller.loadCatalog(testCase.query);
    const state = controller.getState();
    assert.equal(state.libraryView, 'grid');
    assert.equal(catalogViewState(state), testCase.state, testCase.fixture);
    assert.equal(state.catalogError ? state.catalogError.code : '', testCase.errorCode, testCase.fixture);
  }
});

test('malformed metadata fixture falls back without blocking catalog loading', async () => {
  const malformed = readFixture('metadata-malformed.json');
  const adapter = { metadataFor() { return malformed; } };
  const { fetchImpl } = queuedFetch([jsonResponse(readFixture('catalog-populated.json'))]);
  const controller = createAppController({ fetchImpl, metadataAdapter: adapter });

  await controller.loadCatalog('');
  const state = controller.getState();
  assert.equal(state.catalogState, 'populated');
  assert.equal(state.metadataFallbackCount, state.games.length);
});

test('launch success uses the current live ID and target-unavailable failure is safe and retryable', async () => {
  const { calls, fetchImpl } = queuedFetch([
    jsonResponse(readFixture('catalog-populated.json')),
    jsonResponse(readFixture('detail-refreshed.json')),
    jsonResponse(readFixture('launch-success.json')),
    jsonResponse(readFixture('launch-target-unavailable.json'), 503),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });

  await controller.loadCatalog('');
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  assert.equal(controller.getState().launchState, 'launch_success');
  assert.deepEqual(JSON.parse(calls[2].options.body), { game_id: 'megadrive-sonic-test' });

  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.launchState, 'launch_error');
  assert.match(state.launchMessage, /local target is unavailable/i);
  assert.doesNotMatch(state.launchMessage, /private|address|session operation failed/i);
});

test('selected detail failure keeps the live selection and exposes a safe retry state', async () => {
  const { fetchImpl } = queuedFetch([
    jsonResponse(readFixture('catalog-populated.json')),
    jsonResponse(readFixture('detail-error.json'), 500),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });

  await controller.loadCatalog('');
  await controller.selectGame('megadrive-sonic-test');
  const state = controller.getState();
  assert.equal(state.detailState, 'detail_error');
  assert.equal(state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.equal(state.detailError.code, 'INTERNAL');
  assert.equal(state.detailError.message, 'detail is unavailable');
});

test('stale detail response cannot overwrite a replacement selection', async () => {
  let resolveSonic;
  let resolveUnknown;
  const fetchImpl = requestPath => {
    if (gamesRequestKey(requestPath) === '/api/v1/games') return Promise.resolve(jsonResponse(readFixture('catalog-populated.json')));
    if (requestPath.endsWith('/megadrive-sonic-test')) {
      return new Promise(resolve => { resolveSonic = resolve; });
    }
    if (requestPath.endsWith('/snes-unknown-test')) {
      return new Promise(resolve => { resolveUnknown = resolve; });
    }
    throw new Error(`unexpected request ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadCatalog('');
  const firstSelection = controller.selectGame('megadrive-sonic-test');
  const secondSelection = controller.selectGame('snes-unknown-test');
  resolveUnknown(jsonResponse(readFixture('detail-unknown.json')));
  await secondSelection;
  resolveSonic(jsonResponse(readFixture('detail-refreshed.json')));
  await firstSelection;

  const state = controller.getState();
  assert.equal(state.selectedLiveGame.id, 'snes-unknown-test');
  assert.equal(state.selectedLiveGame.title, 'Unknown <Game> detail');
  assert.equal(state.detailState, 'populated');
});

test('browser render paths keep cards, detail, and launch usable across metadata failures', async () => {
  let metadataForCalls = 0;
  const cases = [
    { name: 'absent adapter' },
    {
      name: 'throwing provider',
      adapter: { toLauncherGame() { throw new Error('metadata provider offline'); } },
    },
    {
      name: 'malformed presentation',
      adapter: { toLauncherGame(game) { return { ...game, presentation: {} }; } },
    },
    { name: 'normal presentation', adapter: FogCastMetadata },
    {
      name: 'metadataFor presentation',
      adapter: {
        metadataFor() {
          metadataForCalls += 1;
          return {
            ...validAdapterPresentation(),
            id: 'poisoned-id',
            title: 'Poisoned title',
            system: 'poisoned-system',
            state: 'blocked',
            content_prepared: false,
          };
        },
      },
    },
  ];

  for (const testCase of cases) {
    const { document, calls } = await runBrowserApp({
      adapter: testCase.adapter,
      responses: [
        jsonResponse(readFixture('catalog-populated.json')),
        jsonResponse(readFixture('detail-refreshed.json')),
        jsonResponse(readFixture('launch-success.json')),
      ],
    });
    const catalogList = document.nodes.get('catalog-list');
    assert.equal(document.nodes.get('session-status').textContent, 'No active session.', `${testCase.name} session startup should settle before interaction`);
    assert.equal(catalogList.children.length, 3, `${testCase.name} should render all cards`);
    const firstCard = catalogList.children[0];
    assert.equal(firstCard.tagName, 'BUTTON', `${testCase.name} card should be actionable`);
    assert.equal(firstCard.children[1].textContent, 'Sonic the Hedgehog', `${testCase.name} card title must stay live`);
    assert.match(firstCard.children[2].textContent, /Mega Drive · Ready/, `${testCase.name} card state must stay live`);

    await firstCard.click();
    assert.match(browserText(document.nodes.get('detail-content')), /Sonic the Hedgehog/);
    const launchButton = document.nodes.get('launch-actions').children
      .find(child => child.tagName === 'BUTTON' && child.textContent === 'Play');
    assert.ok(launchButton, `${testCase.name} should retain launch action after detail refresh`);
    assert.equal(launchButton.disabled, false, `${testCase.name} eligibility must stay live`);

    await launchButton.click();
    assert.match(browserText(document.nodes.get('launch-actions')), /launch_success/);
    assert.equal(calls[2].options.body, '{"game_id":"megadrive-sonic-test"}');
  }
  assert.equal(metadataForCalls, 4, 'metadataFor must run only for the accepted catalog records and detail');
});

test('browser provider-ready partial presentation keeps empty fields and neutral artwork roles', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const adapter = {
    metadataFor() {
      return {
        summary: '', year: '', genre: '', studio: '', players: '',
        coverArtworkHandle: handle,
        isFallback: false,
        metadataState: 'ready',
      };
    },
  };
  const { document } = await runBrowserApp({
    adapter,
    responses: [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture('detail-refreshed.json')),
      jsonResponse(readFixture('launch-success.json')),
    ],
  });

  const card = document.nodes.get('catalog-list').children[0];
  assert.equal(card.children[0].className, 'cover-art image-art');
  assert.equal(card.children[0].attributes.get('src'), `/api/v1/presentation/artwork/${handle}`);
  await card.click();
  const detailContent = document.nodes.get('detail-content');
  assert.equal(detailContent.children[0].className, 'backdrop-art artwork-empty');
  assert.equal(detailContent.children.filter(child => child.className === 'detail-summary').length, 0);
  assert.doesNotMatch(browserText(detailContent), /FogCast demo|Unknown|palette-|treatment-|—/);
});

test('browser artwork load errors replace only the failed provider image with neutral artwork', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const adapter = {
    metadataFor() {
      return {
        summary: 'Provider summary', year: '', genre: '', studio: '', players: '',
        coverArtworkHandle: handle,
        isFallback: false,
        metadataState: 'ready',
      };
    },
  };
  const { document } = await runBrowserApp({
    adapter,
    responses: [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture('detail-refreshed.json')),
      jsonResponse(readFixture('launch-success.json')),
    ],
  });
  const cardImage = document.nodes.get('catalog-list').children[0].children[0];
  const imageError = cardImage.listeners.get('error');
  assert.equal(typeof imageError, 'function');
  imageError();
  assert.equal(cardImage.className, 'cover-art artwork-empty artwork-unavailable');
  assert.equal(cardImage.tagName, 'SPAN');
  assert.equal(cardImage.textContent, 'Art unavailable');
  assert.equal(cardImage.attributes.size, 0);
});

test('browser rendering falls back for shape-complete invalid presentation values', async () => {
  const invalidPresentation = {
    cover: { palette: 'evil sr-only', treatment: 'grid' },
    backdrop: { palette: 'lagoon', treatment: 'grid status-message' },
    summary: 'untrusted summary '.repeat(100),
    year: '2026',
    genre: 'Action',
    studio: 'Untrusted studio',
    players: '1 player',
    isFallback: false,
  };
  const adapter = {
    toLauncherGame(game) {
      return { ...game, presentation: invalidPresentation };
    },
  };
  const { document, calls } = await runBrowserApp({
    adapter,
    responses: [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture('detail-refreshed.json')),
      jsonResponse(readFixture('launch-success.json')),
    ],
  });

  const catalogList = document.nodes.get('catalog-list');
  assert.equal(catalogList.children.length, 3);
  assert.equal(document.nodes.get('catalog-status').textContent, 'populated metadata_fallback');
  for (const card of catalogList.children) {
    assert.equal(card.children[0].className, 'cover-art artwork-empty');
    assert.equal(card.children[0].textContent, 'No art');
    assert.doesNotMatch(card.children[0].className, /evil|sr-only|status-message|palette-|treatment-/);
    const notes = card.children.filter(child => child.className === 'fallback-note');
    notes.forEach(note => assert.doesNotMatch(note.textContent, /evil|sr-only|status-message/));
  }

  await catalogList.children[0].click();
  const detailContent = document.nodes.get('detail-content');
  const summary = detailContent.children.find(child => child.className === 'detail-summary');
  assert.equal(summary, undefined);
  assert.equal(detailContent.children[0].className, 'backdrop-art artwork-empty');

  const launchButton = document.nodes.get('launch-actions').children
    .find(child => child.tagName === 'BUTTON' && child.textContent === 'Play');
  assert.ok(launchButton);
  await launchButton.click();
  assert.match(browserText(document.nodes.get('launch-actions')), /launch_success/);
  assert.equal(calls[2].options.body, '{"game_id":"megadrive-sonic-test"}');
});

test('2xx launch responses require a valid active sessionResult and preserve retryable errors', async () => {
  const invalidResponses = [
    { name: 'invalid JSON', response: malformedJSONResponse() },
    { name: 'HTML 2xx', response: malformedJSONResponse(200) },
    { name: '204 no content', response: malformedJSONResponse(204) },
    { name: 'empty object', response: jsonResponse({}) },
    { name: 'inactive state', response: jsonResponse({ state: 'idle' }) },
    { name: 'malformed game ID', response: jsonResponse({ state: 'active', game_id: 42 }) },
  ];
  for (const testCase of invalidResponses) {
    const { state } = await launchStateFor(testCase.response);
    assert.equal(state.launchState, 'launch_error', `${testCase.name} must not announce success`);
    assert.equal(state.launchError && state.launchError.code, 'MALFORMED_RESPONSE', testCase.name);
  }

  const valid = await launchStateFor(jsonResponse(readFixture('launch-success.json')));
  assert.equal(valid.state.launchState, 'launch_success');
  assert.equal(valid.state.launchError, null);
  assert.equal(valid.calls[2].options.body, '{"game_id":"megadrive-sonic-test"}');
});

test('metadataFor receives one detached immutable projection and feeds one stored catalog/detail view', async () => {
  await runImmutableAdapterCase('metadataFor');
});

test('toLauncherGame fallback receives one detached immutable projection and feeds one stored catalog/detail view', async () => {
  await runImmutableAdapterCase('toLauncherGame');
});

function sessionFixture(overrides = {}) {
  return {
    state: 'idle',
    ...overrides,
  };
}

function inputFixture() {
  return {
    state: 'attached',
    ready: true,
    metrics: {
      frames_sent: 4,
      state_resyncs: 1,
      sequence_gaps: 0,
      releases: 2,
      capture_to_bridge_p95_ms: 1.25,
      bridge_to_uinput_p95_ms: 2.5,
      rtt_ms: 3.75,
      bridge_to_uinput_measurable: false,
      shutdown_reason: 'operator_stop',
    },
  };
}

function gamesRequestKey(requestPath) {
  const raw = String(requestPath || '');
  const qIndex = raw.indexOf('?');
  const pathOnly = qIndex < 0 ? raw : raw.slice(0, qIndex);
  if (pathOnly !== '/api/v1/games') return raw;
  const params = new URLSearchParams(qIndex < 0 ? '' : raw.slice(qIndex + 1));
  const q = params.get('q') || '';
  const platform = params.get('platform') || '';
  const collection = params.get('collection') || '';
  const cursor = params.get('cursor') || '';
  if (cursor) return `/api/v1/games?cursor=${cursor}`;
  if (platform) return q ? `/api/v1/games?q=${q}&platform=${platform}` : `/api/v1/games?platform=${platform}`;
  if (collection) return `/api/v1/games?collection=${collection}`;
  if (q) return `/api/v1/games?q=${q}`;
  return '/api/v1/games';
}

function routedFetch(routes) {
  const calls = [];
  const fetchImpl = async (requestPath, options) => {
    calls.push({ path: requestPath, options });
    const key = gamesRequestKey(requestPath);
    const exact = routes[requestPath];
    const keyed = routes[key];
    const queue = exact && exact.length ? exact : keyed;
    if (queue && queue.length) {
      const next = queue.shift();
      return typeof next === 'function' ? next() : next;
    }
    if (exact || keyed) throw new Error(`missing routed response for ${requestPath}`);
    if (key === '/api/v1/games?collection=unplayed' || key === '/api/v1/games?collection=recently_added') {
      return jsonResponse({ games: [] });
    }
    throw new Error(`missing routed response for ${requestPath}`);
  };
  return { calls, fetchImpl };
}

test('session and stop requests preserve the host wire contract', () => {
  assert.deepEqual(sessionRequest(), {
    path: '/api/v1/session',
    options: { method: 'GET' },
  });
  assert.deepEqual(stopRequest(), {
    path: '/api/v1/session/stop',
    options: { method: 'POST' },
  });
  assert.equal(stopRequest().options.body, undefined);
  assert.equal(stopRequest().options.headers, undefined);
  assert.deepEqual(attachInputRequest(), {
    path: '/api/v1/session/input/attach',
    options: { method: 'POST' },
  });
  assert.deepEqual(detachInputRequest(), {
    path: '/api/v1/session/input/detach',
    options: { method: 'POST' },
  });
  assert.equal(attachInputRequest().options.body, undefined);
  assert.equal(detachInputRequest().options.body, undefined);
});

test('session parser accepts privacy-safe optional fields and rejects malformed identity or metrics', () => {
  const accepted = parseSession(sessionFixture({
    state: 'active',
    game_id: 'megadrive-sonic-test',
    system: 'megadrive',
    execution: 'fpga_native',
    media: 'active',
    progress: { stage: 'ready', message: 'session is ready' },
    input: inputFixture(),
  }));
  assert.equal(Object.isFrozen(accepted), true);
  assert.equal(Object.isFrozen(accepted.progress), true);
  assert.equal(Object.isFrozen(accepted.input.metrics), true);
  assert.equal(accepted.game_id, 'megadrive-sonic-test');
  assert.equal(sessionViewState(accepted), 'active');

  const nesSession = parseSession(sessionFixture({
    state: 'active',
    game_id: 'nes-mario-test',
    system: 'nes',
  }));
  assert.equal(nesSession.system, 'nes');
  assert.equal(nesSession.game_id, 'nes-mario-test');

  for (const system of ['gbc', 'a2600', 'coleco', 'lynx']) {
    assert.equal(parseSession(sessionFixture({ state: 'active', system })).system, system);
  }

  for (const malformed of [
    sessionFixture({ state: 'unknown' }),
    sessionFixture({ state: 'idle', game_id: 'must-not-be-public-on-idle' }),
    sessionFixture({ state: 'active', game_id: 42 }),
    sessionFixture({ state: 'active', system: 'atari' }),
    sessionFixture({ state: 'launching', progress: { stage: 'only-stage' } }),
    sessionFixture({ state: 'active', input: { state: 'attached', ready: true, metrics: {} } }),
    sessionFixture({ state: 'active', input: { ...inputFixture(), ready: 'yes' } }),
    sessionFixture({ state: 'active', input: { ...inputFixture(), metrics: { ...inputFixture().metrics, rtt_ms: -1 } } }),
  ]) {
    assert.throws(() => parseSession(malformed), error => error.code === 'MALFORMED_RESPONSE');
  }
});

test('grouped catalog hydrates the active variant via GET /api/v1/games/{id} and gates Playing on authority', async () => {
  const groupKey = 'megadrive\u001fsonic';
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: groupKey,
    variant_count: 2,
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    group_key: groupKey,
    region: 'japan',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(usa, 'variants'), false);
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({
        state: 'active', game_id: japan.id, system: 'megadrive',
      })),
      malformedJSONResponse(),
    ],
    '/api/v1/games': [jsonResponse({ games: [usa] })],
    [`/api/v1/games/${japan.id}`]: [jsonResponse(japan)],
  });
  const controller = createAppController({ fetchImpl });
  await controller.loadSession();
  await controller.loadCatalog('');
  let state = controller.getState();
  assert.equal(state.selectedLiveGame, null);
  assert.equal(state.sessionLiveGame.id, japan.id);
  assert.equal(state.sessionLiveGame.group_key, groupKey);
  assert.equal(state.sessionAuthority, 'authoritative');
  assert.equal(isSessionPlayingCard(state.session, usa, state.sessionLiveGame, state.sessionAuthority), true);
  assert.ok(calls.some(call => call.path === `/api/v1/games/${japan.id}`));
  await controller.loadSession();
  state = controller.getState();
  assert.equal(state.sessionAuthority, 'last-known');
  assert.equal(state.session.state, 'active');
  assert.equal(state.session.game_id, japan.id);
  assert.equal(isSessionPlayingCard(state.session, usa, state.sessionLiveGame, state.sessionAuthority), false);
});

test('accepted session is emitted before optional Playing hydrate', async () => {
  const groupKey = 'megadrive\u001fsonic';
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: groupKey,
    variant_count: 2,
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    group_key: groupKey,
    region: 'japan',
  });
  let releaseDetail;
  const authorities = [];
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({
        state: 'active', game_id: japan.id, system: 'megadrive',
      })),
      jsonResponse(sessionFixture({ state: 'idle' })),
    ],
    '/api/v1/session/stop': [jsonResponse(sessionFixture({ state: 'idle' }))],
    '/api/v1/games': [jsonResponse({ games: [usa] })],
    [`/api/v1/games/${japan.id}`]: [() => new Promise(resolve => { releaseDetail = resolve; })],
  });
  const controller = createAppController({
    fetchImpl,
    onStateChange(next) { authorities.push(next.sessionAuthority); },
  });
  await controller.loadCatalog('');
  const pending = controller.loadSession();
  await waitForCondition(
    () => controller.getState().sessionAuthority === 'authoritative',
    'session authority stayed indeterminate while Playing hydrate was held',
  );
  const mid = controller.getState();
  assert.equal(mid.session.state, 'active');
  assert.equal(mid.sessionPhase, 'active');
  assert.equal(mid.sessionLiveGame, null);
  assert.ok(authorities.includes('authoritative'));
  await controller.stopSession();
  assert.equal(calls.some(call => call.path === '/api/v1/session/stop'), true);
  releaseDetail(jsonResponse(japan));
  await pending;
});

test('stale catalog hydrate does not emit after a newer requestSequence', async () => {
  const groupKey = 'megadrive\u001fsonic';
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: groupKey,
    variant_count: 2,
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    group_key: groupKey,
    region: 'japan',
  });
  const mario = availableGame('snes-mario-test', 'Mario');
  let releaseDetail;
  const seen = [];
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [jsonResponse(sessionFixture({
      state: 'active', game_id: japan.id, system: 'megadrive',
    }))],
    '/api/v1/games?q=old': [jsonResponse({ games: [usa] })],
    '/api/v1/games?q=new': [jsonResponse({ games: [mario] })],
    [`/api/v1/games/${japan.id}`]: [() => new Promise(resolve => { releaseDetail = resolve; })],
  });
  const controller = createAppController({
    fetchImpl,
    onStateChange(next) {
      seen.push({
        games: next.games.map(game => game.id),
        catalogState: next.catalogState,
      });
    },
  });
  await controller.loadSession();
  const oldCatalog = controller.loadCatalog('old');
  await waitForCondition(
    () => controller.getState().games[0] && controller.getState().games[0].id === usa.id,
    'grouped catalog was not applied before Playing hydrate',
  );
  const newerCatalog = controller.loadCatalog('new');
  await newerCatalog;
  assert.equal(controller.getState().games[0].id, mario.id);
  const afterNewer = seen.length;
  releaseDetail(jsonResponse(japan));
  await oldCatalog;
  const state = controller.getState();
  assert.equal(state.games[0].id, mario.id);
  assert.equal(state.catalogState, 'populated');
  assert.equal(
    seen.slice(afterNewer).some(item => item.catalogState === 'populated' && item.games[0] === usa.id),
    false,
  );
});

test('stale home hydrate does not emit after a newer catalog load', async () => {
  const groupKey = 'megadrive\u001fsonic';
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: groupKey,
    variant_count: 2,
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    group_key: groupKey,
    region: 'japan',
  });
  const mario = availableGame('snes-mario-test', 'Mario');
  let releaseDetail;
  const seen = [];
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [jsonResponse(sessionFixture({
      state: 'active', game_id: japan.id, system: 'megadrive',
    }))],
    '/api/v1/games': [jsonResponse({ games: [mario] })],
    [`/api/v1/games/${japan.id}`]: [() => new Promise(resolve => { releaseDetail = resolve; })],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [usa] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({
    fetchImpl,
    onStateChange(next) {
      seen.push({
        view: next.libraryView,
        games: next.games.map(game => game.id),
      });
    },
  });
  await controller.loadSession();
  const home = controller.openHome();
  await waitForCondition(
    () => controller.getState().libraryView === 'home'
      && controller.getState().games.some(game => game.id === usa.id),
    'home rails were not applied before Playing hydrate',
  );
  const catalog = controller.setLibraryNav('', '');
  await catalog;
  const afterCatalog = seen.length;
  releaseDetail(jsonResponse(japan));
  await home;
  const state = controller.getState();
  assert.equal(state.libraryView, 'grid');
  assert.deepEqual(state.games.map(game => game.id), [mario.id]);
  assert.deepEqual(state.homeRails, []);
  assert.equal(
    seen.slice(afterCatalog).some(item => item.view === 'home' && item.games.includes(usa.id)),
    false,
  );
});

test('startup reconstructs active session without changing catalog selection and resolves only a matching live title', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [jsonResponse(sessionFixture({
      state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive', execution: 'fpga_native', media: 'active',
    }))],
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.loadCatalog('');
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.sessionGameTitle, 'Sonic the Hedgehog');
  assert.equal(state.selectedLiveGame, null);
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/session', '/api/v1/games?grouped=1']);
});

test('newer session status wins over a held stale startup response', async () => {
  let releaseOld;
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      () => new Promise(resolve => { releaseOld = resolve; }),
      jsonResponse(sessionFixture({ state: 'idle' })),
    ],
  });
  const controller = createAppController({ fetchImpl });
  const oldRequest = controller.loadSession();
  const newerRequest = controller.loadSession();
  await newerRequest;
  releaseOld(jsonResponse(sessionFixture({ state: 'active', game_id: 'stale', system: 'megadrive' })));
  await oldRequest;
  assert.equal(controller.getState().sessionPhase, 'idle');
  assert.equal(controller.getState().session.state, 'idle');
});

test('launch reconciles with one authoritative GET and keeps the captured ID across selection changes', async () => {
  let releaseLaunchResponse;
  let releaseReconcile;
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      () => new Promise(resolve => { releaseReconcile = resolve; }),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(readFixture('detail-refreshed.json'))],
    '/api/v1/games/snes-unknown-test': [jsonResponse(readFixture('detail-unknown.json'))],
    '/api/v1/session/launch': [() => new Promise(resolve => { releaseLaunchResponse = resolve; })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.loadCatalog('');
  await controller.selectGame('megadrive-sonic-test');
  const launch = controller.launchSelected();
  assert.equal(controller.getState().activeMutation, 'launch');
  await controller.selectGame('snes-unknown-test');
  releaseLaunchResponse(jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })));
  while (!releaseReconcile) await new Promise(resolve => setImmediate(resolve));
  releaseReconcile(jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })));
  await launch;
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame.id, 'snes-unknown-test');
  assert.equal(state.launchState, 'idle');
  assert.equal(state.launchMessage, '');
  assert.equal(state.launchError, null);
  assert.equal(calls.find(call => call.path === '/api/v1/session/launch').options.body, '{"game_id":"megadrive-sonic-test"}');
  assert.equal(calls.filter(call => call.path === '/api/v1/session').length, 2);
});

test('stop requires valid stop response plus authoritative idle GET before presenting stopped', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'idle' })),
    ],
    '/api/v1/session/stop': [jsonResponse(sessionFixture({ state: 'idle', media: 'stopped' }))],
  });
  const controller = createAppController({ fetchImpl });
  await controller.loadSession();
  await controller.stopSession();
  assert.equal(controller.getState().sessionPhase, 'stopped');
  assert.equal(calls.find(call => call.path === '/api/v1/session/stop').options.body, undefined);
  await controller.loadSession();
  assert.equal(controller.getState().sessionPhase, 'idle');
});

test('stop failure still reconciles once and retains the active snapshot as a safe retry state', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/stop': [jsonResponse({ error: { code: 'BUSY', message: 'another launch or stop transition is running' } }, 409)],
  });
  const controller = createAppController({ fetchImpl });
  await controller.loadSession();
  await controller.stopSession();
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'error');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.sessionError.code, 'BUSY');
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/session', '/api/v1/session/stop', '/api/v1/session']);
});

test('failed or malformed status refresh marks retained active details last-known and blocks stop until a fresh GET', async () => {
  for (const testCase of [
    {
      name: 'unavailable',
      response: jsonResponse({ error: { code: 'TARGET_UNAVAILABLE', message: 'target status is unavailable' } }, 503),
      phase: 'unavailable',
    },
    {
      name: 'malformed',
      response: malformedJSONResponse(),
      phase: 'malformed',
    },
  ]) {
    const { calls, fetchImpl } = routedFetch({
      '/api/v1/session': [
        jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
        testCase.response,
        jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
        jsonResponse(sessionFixture({ state: 'idle' })),
      ],
      '/api/v1/session/stop': [jsonResponse(sessionFixture({ state: 'idle' }))],
    });
    const controller = createAppController({ fetchImpl });
    await controller.loadSession();
    await controller.loadSession();
    let state = controller.getState();
    assert.equal(state.sessionPhase, testCase.phase, testCase.name);
    assert.equal(state.sessionAuthority, 'last-known', testCase.name);
    assert.equal(state.session.state, 'active', testCase.name);

    await controller.stopSession();
    assert.equal(
      calls.filter(call => call.path === '/api/v1/session/stop').length,
      0,
      `${testCase.name} status loss must not issue a stop POST`,
    );

    await controller.loadSession();
    state = controller.getState();
    assert.equal(state.sessionAuthority, 'authoritative', testCase.name);
    await controller.stopSession();
    assert.equal(calls.filter(call => call.path === '/api/v1/session/stop').length, 1, testCase.name);
    assert.equal(controller.getState().sessionPhase, 'stopped', testCase.name);
  }
});

test('malformed stop response never becomes a false stopped announcement after idle reconciliation', async () => {
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'idle' })),
    ],
    '/api/v1/session/stop': [jsonResponse({ state: 'idle', game_id: 'private-field-on-idle' })],
  });
  const controller = createAppController({ fetchImpl });
  await controller.loadSession();
  await controller.stopSession();
  assert.equal(controller.getState().sessionPhase, 'malformed');
  assert.equal(controller.getState().session.state, 'idle');
});

test('rapid and conflicting mutations issue at most one request and expose the conflict', async () => {
  let releaseReconcile;
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      () => new Promise(resolve => { releaseReconcile = resolve; }),
    ],
    '/api/v1/session/stop': [jsonResponse(sessionFixture({ state: 'idle' }))],
    '/api/v1/session/launch': [],
  });
  const controller = createAppController({ fetchImpl });
  await controller.loadSession();
  const firstStop = controller.stopSession();
  const duplicateStop = controller.stopSession();
  const conflictingLaunch = controller.launchSelected();
  assert.equal(controller.getState().activeMutation, 'stop');
  assert.equal(controller.getState().mutationMessage, 'A session transition is already in progress.');
  assert.equal(calls.filter(call => call.path === '/api/v1/session/stop').length, 1);
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, 0);
  assert.equal(await duplicateStop.then(state => state.activeMutation), 'stop');
  assert.equal(await conflictingLaunch.then(state => state.activeMutation), 'stop');
  while (!releaseReconcile) await new Promise(resolve => setImmediate(resolve));
  releaseReconcile(jsonResponse(sessionFixture({ state: 'idle' })));
  await firstStop;
});

test('session panel renders only accepted fields, reconstructs active title, and stops with exact empty POST', async () => {
  const active = sessionFixture({
    state: 'active',
    game_id: 'megadrive-sonic-test',
    system: 'megadrive',
    execution: 'fpga_native',
    media: 'active',
    progress: { stage: 'ready', message: 'session is ready' },
    input: inputFixture(),
    secret: 'must not render',
    target_path: '/private/path',
  });
  const { document, calls, sessionCalls } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json')), jsonResponse(readFixture('stop-success.json'))],
    sessionResponses: [jsonResponse(active), jsonResponse(sessionFixture({ state: 'idle' }))],
  });
  const panel = document.nodes.get('session-panel');
  assert.equal(panel.attributes.get('aria-busy'), 'false');
  assert.equal(panel.className, 'session-panel');
  assert.equal(document.nodes.get('session-status').textContent, 'Active session');
  const details = browserText(document.nodes.get('session-details'));
  assert.match(details, /Sonic the Hedgehog/);
  assert.match(details, /megadrive-sonic-test/);
  assert.match(details, /session is ready/);
  assert.match(details, /Executionfpga_native/);
  assert.match(details, /Input stateattached/);
  assert.match(details, /Input readinessReady/);
  assert.doesNotMatch(details, /Media/);
  assert.doesNotMatch(details, /must not render|private\/path/);
  assert.equal(sessionCalls[0].options.method, 'GET');
  assert.equal(sessionCalls[0].options.body, undefined);
  assert.equal(document.nodes.get('session-actions').children.find(node => node.id === 'stop-session').hidden, false);

  await document.nodes.get('session-actions').children.find(node => node.id === 'stop-session').click();
  assert.equal(document.nodes.get('session-status').textContent, 'Session stopped.');
  assert.equal(document.nodes.get('session-actions').children.find(node => node.id === 'stop-session').hidden, true);
  assert.equal(document.nodes.get('session-actions').children.find(node => node.id === 'refresh-session').focused, true);
  const stopCall = calls.find(call => call.path === '/api/v1/session/stop');
  assert.ok(stopCall);
  assert.equal(stopCall.options.method, 'POST');
  assert.equal(stopCall.options.body, undefined);
  assert.equal(stopCall.options.headers, undefined);
});

test('Play on ActRaiser drives fpga_native launch, shows attached input, and Stop/Detach clear session UX', async () => {
  const actraiser = availableGame('snes-actraiser-usa', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    content_prepared: false,
  });
  const sequel = availableGame('snes-actraiser-2', 'ActRaiser 2 (USA)', {
    canonical_title: 'ActRaiser 2',
  });
  const attached = sessionFixture({
    state: 'active',
    game_id: actraiser.id,
    system: 'snes',
    execution: 'fpga_native',
    input: inputFixture(),
  });
  const detached = sessionFixture({
    state: 'active',
    game_id: actraiser.id,
    system: 'snes',
    execution: 'fpga_native',
    input: { ...inputFixture(), state: 'detached', ready: false },
  });
  const idle = sessionFixture({ state: 'idle' });
  const { document, calls } = await runBrowserApp({
    responses: [
      jsonResponse({ games: [actraiser, sequel] }),
      jsonResponse(actraiser),
      jsonResponse(attached),
      jsonResponse(detached),
      jsonResponse(idle),
    ],
    sessionResponses: [
      jsonResponse(idle),
      jsonResponse(attached),
      jsonResponse(detached),
      jsonResponse(idle),
    ],
  });
  await settleBrowser();
  await gameCards(document)[0].click();
  await settleBrowser();
  const play = document.getElementById('launch-game');
  assert.equal(play.textContent, 'Play');
  assert.equal(play.disabled, false);
  assert.equal(document.getElementById('launch-reason').textContent, '');
  await play.click();
  await settleBrowser();
  const launch = calls.find(call => call.path === '/api/v1/session/launch');
  assert.ok(launch);
  assert.equal(launch.options.method, 'POST');
  assert.equal(launch.options.body, `{"game_id":"${actraiser.id}"}`);
  const details = browserText(document.nodes.get('session-details'));
  assert.match(details, /ActRaiser \(USA\)/);
  assert.match(details, /Executionfpga_native/);
  assert.match(details, /Input stateattached/);
  assert.match(details, /Input readinessReady/);
  assert.doesNotMatch(details, /Media/);
  assert.doesNotMatch(details, /cast|HDMI|HIL|pad delivery/i);
  assert.equal(document.getElementById('stop-session').hidden, false);
  const detach = document.getElementById('detach-session-input');
  assert.equal(detach.hidden, false);
  assert.equal(document.getElementById('attach-session-input').hidden, true);
  await detach.click();
  await settleBrowser();
  const detachCall = calls.find(call => call.path === '/api/v1/session/input/detach');
  assert.ok(detachCall);
  assert.equal(detachCall.options.method, 'POST');
  assert.equal(detachCall.options.body, undefined);
  assert.equal(detachCall.options.headers, undefined);
  assert.match(browserText(document.nodes.get('session-details')), /Input statedetached/);
  assert.match(browserText(document.nodes.get('session-details')), /Executionfpga_native/);
  assert.equal(document.getElementById('detach-session-input').hidden, true);
  assert.equal(document.getElementById('attach-session-input').hidden, false);
  await document.getElementById('stop-session').click();
  await settleBrowser();
  assert.equal(document.nodes.get('session-status').textContent, 'Session stopped.');
  assert.equal(document.getElementById('stop-session').hidden, true);
  assert.equal(document.getElementById('detach-session-input').hidden, true);
  assert.equal(document.getElementById('attach-session-input').hidden, true);
  assert.doesNotMatch(browserText(document.nodes.get('session-details')), /attached/);
});

test('Play from the game actions menu uses the selected catalog title id', async () => {
  const actraiser = availableGame('snes-actraiser-usa', 'ActRaiser (USA)');
  const { document, calls } = await runKeyboardApp({
    pages: [{ games: [actraiser] }],
    launchResponse: {
      state: 'active',
      game_id: actraiser.id,
      system: 'snes',
      execution: 'fpga_native',
      input: inputFixture(),
    },
  });
  await settleBrowser();
  await openAllGamesGrid(document);
  await openCardActions(document, gameCards(document)[0]);
  const play = document.getElementById('game-action-play');
  assert.equal(play.textContent, 'Play');
  assert.equal(play.disabled, false);
  await play.click();
  await settleBrowser();
  const launch = calls.find(call => call.path === '/api/v1/session/launch');
  assert.ok(launch);
  assert.equal(JSON.parse(launch.options.body).game_id, actraiser.id);
});

test('explicit attach and detach use empty POSTs and follow session input.state', async () => {
  const attached = sessionFixture({
    state: 'active',
    game_id: 'snes-actraiser-usa',
    system: 'snes',
    execution: 'fpga_native',
    input: inputFixture(),
  });
  const detached = sessionFixture({
    ...attached,
    input: { ...inputFixture(), state: 'detached', ready: false },
  });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(attached),
      jsonResponse(detached),
      jsonResponse(attached),
    ],
    '/api/v1/session/input/detach': [jsonResponse(detached)],
    '/api/v1/session/input/attach': [jsonResponse(attached)],
  });
  const controller = createAppController({ fetchImpl });
  await controller.loadSession();
  assert.equal(controller.getState().session.execution, 'fpga_native');
  assert.equal(controller.getState().session.input.state, 'attached');
  await controller.attachInput();
  assert.equal(calls.filter(call => call.path === '/api/v1/session/input/attach').length, 0);
  await controller.detachInput();
  const detach = calls.find(call => call.path === '/api/v1/session/input/detach');
  assert.ok(detach);
  assert.equal(detach.options.method, 'POST');
  assert.equal(detach.options.body, undefined);
  assert.equal(detach.options.headers, undefined);
  assert.equal(controller.getState().session.input.state, 'detached');
  assert.equal(controller.getState().sessionMessage, 'Input detached.');
  await controller.detachInput();
  assert.equal(calls.filter(call => call.path === '/api/v1/session/input/detach').length, 1);
  await controller.attachInput();
  const attach = calls.find(call => call.path === '/api/v1/session/input/attach');
  assert.ok(attach);
  assert.equal(attach.options.method, 'POST');
  assert.equal(attach.options.body, undefined);
  assert.equal(controller.getState().session.input.state, 'attached');
  assert.equal(controller.getState().sessionMessage, 'Input attached.');
});

test('starting and reconnecting input reasons describe Attach/Detach only', async () => {
  for (const inputState of ['starting', 'reconnecting']) {
    const active = sessionFixture({
      state: 'active',
      game_id: 'snes-actraiser-usa',
      system: 'snes',
      execution: 'fpga_native',
      input: { ...inputFixture(), state: inputState, ready: false },
    });
    const { document } = await runBrowserApp({
      responses: [jsonResponse({ games: [availableGame('snes-actraiser-usa', 'ActRaiser (USA)')] })],
      sessionResponses: [jsonResponse(active)],
    });
    const reason = document.getElementById('session-action-reason');
    assert.match(reason.textContent, /wait before attach or detach/i, inputState);
    assert.equal(document.getElementById('refresh-session').getAttribute('aria-describedby'), null, inputState);
    assert.equal(document.getElementById('stop-session').getAttribute('aria-describedby'), null, inputState);
    assert.equal(document.getElementById('attach-session-input').getAttribute('aria-describedby'), 'session-action-reason', inputState);
    assert.equal(document.getElementById('detach-session-input').getAttribute('aria-describedby'), 'session-action-reason', inputState);
    assert.equal(document.getElementById('stop-session').hidden, false, inputState);
    assert.equal(document.getElementById('stop-session').disabled, false, inputState);
  }
});

test('host_only session details still show media state and hide input controls', async () => {
  const active = sessionFixture({
    state: 'active',
    game_id: 'megadrive-sonic-test',
    system: 'megadrive',
    execution: 'host_only',
    media: 'active',
  });
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    sessionResponses: [jsonResponse(active)],
  });
  const details = browserText(document.nodes.get('session-details'));
  assert.match(details, /Executionhost_only/);
  assert.match(details, /Mediaactive/);
  assert.equal(document.getElementById('detach-session-input').hidden, true);
  assert.equal(document.getElementById('attach-session-input').hidden, true);
});

test('host_only session with detached input does not offer Attach', async () => {
  const active = sessionFixture({
    state: 'active',
    game_id: 'megadrive-sonic-test',
    system: 'megadrive',
    execution: 'host_only',
    media: 'active',
    input: { ...inputFixture(), state: 'detached', ready: false },
  });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [jsonResponse(active)],
    '/api/v1/session/input/attach': [jsonResponse(active)],
  });
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    sessionResponses: [jsonResponse(active)],
  });
  assert.equal(document.getElementById('attach-session-input').hidden, true);
  assert.equal(document.getElementById('detach-session-input').hidden, true);
  assert.doesNotMatch(document.getElementById('session-action-reason').textContent, /wait before attach or detach/i);
  const controller = createAppController({ fetchImpl });
  await controller.loadSession();
  await controller.attachInput();
  assert.equal(calls.filter(call => call.path === '/api/v1/session/input/attach').length, 0);
});

test('session malformed state exposes a safe retry without leaking response fields', async () => {
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    sessionResponses: [malformedJSONResponse(), jsonResponse(sessionFixture({ state: 'idle' }))],
  });
  assert.equal(document.nodes.get('session-status').textContent, 'The local host returned an invalid session response. Retry.');
  assert.equal(document.nodes.get('session-panel').className, 'session-panel');
  assert.match(document.nodes.get('session-message').textContent, /invalid session response/i);
  await document.nodes.get('session-actions').children.find(node => node.id === 'refresh-session').click();
  assert.equal(document.nodes.get('session-status').textContent, 'No active session.');
});

test('launchBlockReason disables unmapped platforms only when launchable is false', () => {
  const ready = {
    id: 'snes-mario-test', title: 'Mario', system: 'snes', kind: 'raw',
    state: 'available', root_online: true, content_prepared: true, execution: 'fpga_native',
  };
  assert.equal(launchBlockReason(ready), '');
  assert.equal(launchBlockReason({ ...ready, content_prepared: false }), '');
  assert.equal(launchBlockReason({ ...ready, launchable: false }), 'This platform is browse-only on this host.');
});

test('loadMoreCatalog appends the next cursor page', async () => {
  const game = {
    title: 'Alpha', system: 'snes', kind: 'raw', state: 'available',
    root_online: true, content_prepared: true, execution: 'fpga_native',
  };
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse({
      games: [{ ...game, id: 'snes-alpha-test' }],
      next_cursor: 'cursor-1',
    })],
    '/api/v1/games?cursor=cursor-1': [jsonResponse({
      games: [{ ...game, id: 'snes-bravo-test', title: 'Bravo' }],
    })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadCatalog('');
  assert.equal(controller.getState().games.length, 1);
  await controller.loadMoreCatalog();
  assert.equal(controller.getState().games.length, 2);
  assert.equal(controller.getState().games[1].id, 'snes-bravo-test');
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/games?grouped=1', '/api/v1/games?grouped=1&cursor=cursor-1']);
});

test('toggleFavorite writes PUT and DELETE without changing launch body', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
    '/api/v1/library/favorites/megadrive-sonic-test': [
      jsonResponse({ id: 'megadrive-sonic-test', favorite: true }),
      jsonResponse({ id: 'megadrive-sonic-test', favorite: false }),
    ],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadCatalog('');
  await controller.toggleFavorite('megadrive-sonic-test');
  assert.equal(controller.getState().games[0].favorite, true);
  await controller.toggleFavorite('megadrive-sonic-test');
  assert.equal(controller.getState().games[0].favorite, false);
  const favoriteCalls = calls.filter(call => String(call.path).includes('/library/favorites/'));
  assert.equal(favoriteCalls[0].options.method, 'PUT');
  assert.equal(favoriteCalls[1].options.method, 'DELETE');
});

test('custom collections use empty-body PUT/DELETE and collection= browse', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture('catalog-populated.json')),
    ],
    '/api/v1/games?collection=weekend-queue': [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/library/collections': [
      jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }] }),
      jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Saturday' }] }),
      jsonResponse({ collections: [] }),
    ],
    '/api/v1/library/collections/weekend-queue?name=Weekend%20Queue': [jsonResponse({ id: 'weekend-queue', name: 'Weekend Queue' })],
    '/api/v1/library/collections/weekend-queue?name=Saturday': [jsonResponse({ id: 'weekend-queue', name: 'Saturday' })],
    '/api/v1/library/collections/weekend-queue': [jsonResponse({ id: 'weekend-queue' })],
    '/api/v1/library/collections/weekend-queue/megadrive-sonic-test': [
      jsonResponse({ id: 'megadrive-sonic-test', collection: 'weekend-queue', member: true }),
      jsonResponse({ id: 'megadrive-sonic-test', collection: 'weekend-queue', member: false }),
    ],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadCatalog('');
  await controller.createCollection('Weekend Queue');
  assert.equal(controller.getState().collection, 'weekend-queue');
  assert.equal(controller.getState().collections[0].name, 'Weekend Queue');
  assert.equal(calls.some(call => call.path === '/api/v1/games?collection=weekend-queue&grouped=1'), true);
  await controller.toggleCollectionMember('weekend-queue', 'megadrive-sonic-test');
  assert.deepEqual(controller.getState().games[0].collections, ['weekend-queue']);
  await controller.toggleCollectionMember('weekend-queue', 'megadrive-sonic-test');
  assert.equal(controller.getState().games.length, 0);
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=weekend-queue&grouped=1').length,
    2,
  );
  await controller.renameCollection('weekend-queue', 'Saturday');
  assert.equal(controller.getState().collections[0].name, 'Saturday');
  await controller.deleteCollection('weekend-queue');
  assert.equal(controller.getState().collection, '');
  const writes = calls.filter(call => String(call.path).includes('/library/collections/'));
  assert.equal(writes[0].options.method, 'PUT');
  assert.equal(writes[0].options.body, undefined);
  assert.equal(writes[1].options.method, 'PUT');
  assert.equal(writes[2].options.method, 'DELETE');
  assert.equal(writes[3].options.method, 'PUT');
  assert.equal(writes[4].options.method, 'DELETE');
});

test('uniqueCollectionID avoids reserved slugs and existing collisions', () => {
  assert.equal(collectionIDFromName('Weekend Queue!'), 'weekend-queue');
  assert.equal(collectionIDFromName('日本語'), 'collection');
  assert.equal(collectionIDFromName('Recently Added'), 'recently-added-list');
  assert.equal(collectionIDFromName('recently_added'), 'recently-added-list');
  assert.equal(collectionIDFromName('Home'), 'home-list');
  assert.equal(uniqueCollectionID('Home', []), 'home-list');
  assert.equal(uniqueCollectionID('Weekend Queue!', []), 'weekend-queue');
  assert.equal(uniqueCollectionID('Weekend Queue!!', ['weekend-queue']), 'weekend-queue-2');
  assert.equal(uniqueCollectionID('日本語', ['collection']), 'collection-2');
  assert.equal(uniqueCollectionID('Favorites', []), 'favorites-list');
  assert.equal(uniqueCollectionID('Recently Added', []), 'recently-added-list');
  assert.equal(uniqueCollectionID('Recently Added', ['recently-added-list']), 'recently-added-list-2');
  assert.equal(uniqueCollectionID('All', []), 'all-list');
  assert.equal(uniqueCollectionID('Continue', []), 'continue-list');
  assert.equal(uniqueCollectionID('Unplayed', []), 'unplayed-list');
});

test('createCollection uses a unique id instead of upsert-renaming', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
    '/api/v1/games?collection=weekend-queue-2': [jsonResponse({ games: [] })],
    '/api/v1/platforms': [jsonResponse({ platforms: [] })],
    '/api/v1/library/attract?limit=1': [jsonResponse({ items: [], idle_seconds: 60 })],
    '/api/v1/library/facets': [jsonResponse({ genres: [], years: [] })],
    '/api/v1/library/collections': [
      jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }] }),
      jsonResponse({ collections: [
        { id: 'weekend-queue', name: 'Weekend Queue' },
        { id: 'weekend-queue-2', name: 'Weekend Queue!' },
      ] }),
    ],
    '/api/v1/library/collections/weekend-queue-2?name=Weekend%20Queue!': [jsonResponse({ id: 'weekend-queue-2', name: 'Weekend Queue!' })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadPlatforms();
  await controller.createCollection('Weekend Queue!');
  assert.equal(controller.getState().collection, 'weekend-queue-2');
  assert.deepEqual(controller.getState().collections.map(item => item.id), ['weekend-queue', 'weekend-queue-2']);
  assert.equal(calls.some(call => String(call.path).startsWith('/api/v1/library/collections/weekend-queue-2?')), true);
  assert.equal(calls.some(call => call.path === '/api/v1/library/collections/weekend-queue?name=Weekend%20Queue!'), false);
});

test('create and delete keep rail state when list GET fails', async () => {
  const listError = jsonResponse({ error: { code: 'INTERNAL', message: 'unavailable' } }, 500);
  const { fetchImpl } = routedFetch({
    '/api/v1/games': [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture('catalog-populated.json')),
    ],
    '/api/v1/games?collection=weekend-queue': [jsonResponse({ games: [] })],
    '/api/v1/library/collections': [listError, listError],
    '/api/v1/library/collections/weekend-queue?name=Weekend%20Queue': [jsonResponse({ id: 'weekend-queue', name: 'Weekend Queue', created_at: 11 })],
    '/api/v1/library/collections/weekend-queue': [jsonResponse({ id: 'weekend-queue' })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.createCollection('Weekend Queue');
  assert.equal(controller.getState().collection, 'weekend-queue');
  assert.equal(controller.getState().collections.length, 1);
  assert.equal(controller.getState().collections[0].id, 'weekend-queue');
  assert.equal(controller.getState().collections[0].name, 'Weekend Queue');
  await controller.deleteCollection('weekend-queue');
  assert.equal(controller.getState().collection, '');
  assert.deepEqual(controller.getState().collections, []);
});

test('removing a member while browsing that collection reloads the wall', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [jsonResponse({
      games: [{
        ...readFixture('catalog-populated.json').games[0],
        collections: ['weekend-queue'],
        group_key: 'megadrive\u001fsonic',
      }],
    })],
    '/api/v1/games?collection=weekend-queue': [
      jsonResponse({ games: [] }),
      jsonResponse({
        games: [{
          ...readFixture('catalog-populated.json').games[0],
          collections: ['weekend-queue'],
          group_key: 'megadrive\u001fsonic',
        }],
      }),
      jsonResponse({ games: [{
        id: 'megadrive-sonic-jp', title: 'Sonic (Japan)', system: 'megadrive',
        kind: 'zip', state: 'available', root_online: true, content_prepared: true,
        execution: 'fpga_native', collections: ['weekend-queue'], group_key: 'megadrive\u001fsonic',
      }] }),
    ],
    '/api/v1/library/collections': [jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }] })],
    '/api/v1/library/collections/weekend-queue/megadrive-sonic-test': [
      jsonResponse({ id: 'megadrive-sonic-test', collection: 'weekend-queue', member: false }),
    ],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadPlatforms();
  await controller.setLibraryNav('weekend-queue', '');
  assert.equal(controller.getState().games[0].id, 'megadrive-sonic-test');
  await controller.toggleCollectionMember('weekend-queue', 'megadrive-sonic-test');
  assert.equal(controller.getState().games[0].id, 'megadrive-sonic-jp');
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=weekend-queue&grouped=1').length,
    2,
  );
});

test('double-save and failed rename retry stay on the original rename', async () => {
  const collections = [{ id: 'weekend-queue', name: 'Weekend Queue' }];
  let releaseWrite;
  const writeStarted = [];
  const gate = new Promise(resolve => { releaseWrite = resolve; });
  const { document, writes } = await runCollectionEditorApp({
    collections,
    writeResponses: [jsonResponse({ error: { code: 'INTERNAL', message: 'unavailable' } }, 500)],
    onWrite: async () => {
      writeStarted.push(true);
      if (writeStarted.length === 1) await gate;
    },
  });
  document.nodes.get('collection-list').children[0].click();
  await settleBrowser();
  document.nodes.get('rename-collection').click();
  document.nodes.get('collection-name').value = 'Saturday';
  document.nodes.get('save-collection').click();
  await waitForCondition(() => writeStarted.length === 1, 'rename PUT did not start');
  document.nodes.get('save-collection').click();
  releaseWrite();
  await settleBrowser();
  await waitForCondition(() => writes.length === 1, 'double-save issued extra writes');
  assert.equal(writes.length, 1);
  assert.match(writes[0].path, /\/library\/collections\/weekend-queue\?name=Saturday/);
  assert.equal(document.nodes.get('save-collection').hidden, false);
  document.nodes.get('save-collection').click();
  await settleBrowser();
  await waitForCondition(() => writes.length === 2, 'failed rename retry did not PUT again');
  assert.equal(writes.length, 2);
  assert.match(writes[1].path, /\/library\/collections\/weekend-queue\?name=Saturday/);
  assert.equal(writes.some(write => String(write.path).includes('/weekend-queue-2') || String(write.path).includes('/saturday')), false);
  assert.equal(document.nodes.get('save-collection').hidden, true);
});

test('rename keeps the original collection when the rail changes', async () => {
  const collections = [
    { id: 'weekend-queue', name: 'Weekend Queue' },
    { id: 'saturday', name: 'Saturday' },
  ];
  const { document, writes } = await runCollectionEditorApp({ collections });
  document.nodes.get('collection-list').children[0].click();
  await settleBrowser();
  document.nodes.get('rename-collection').click();
  document.nodes.get('collection-name').value = 'Friday';
  document.nodes.get('collection-list').children[1].click();
  await settleBrowser();
  document.nodes.get('save-collection').click();
  await settleBrowser();
  await waitForCondition(() => writes.length === 1, 'rename PUT was not issued');
  assert.equal(writes.length, 1);
  assert.match(writes[0].path, /\/library\/collections\/weekend-queue\?name=Friday/);
  assert.equal(writes.some(write => String(write.path).includes('/saturday')), false);
});

test('Escape and reopen during in-flight save keep the new editor and typed name', async () => {
  let releaseWrite;
  const writeStarted = [];
  const gate = new Promise(resolve => { releaseWrite = resolve; });
  const { document, writes } = await runCollectionEditorApp({
    onWrite: async () => {
      writeStarted.push(true);
      if (writeStarted.length === 1) await gate;
    },
  });
  document.nodes.get('create-collection').click();
  document.nodes.get('collection-name').value = 'Weekend Queue';
  document.nodes.get('save-collection').click();
  await waitForCondition(() => writeStarted.length === 1, 'create PUT did not start');
  document.nodes.get('collection-name').focus();
  await pressKey(document, 'Escape', document.nodes.get('collection-name'));
  document.nodes.get('create-collection').click();
  document.nodes.get('collection-name').value = 'Saturday Night';
  assert.equal(document.nodes.get('save-collection').hidden, false);
  releaseWrite();
  await settleBrowser();
  await waitForCondition(() => writes.length === 1, 'in-flight create did not finish');
  assert.equal(document.nodes.get('save-collection').hidden, false);
  assert.equal(document.nodes.get('collection-name').value, 'Saturday Night');
  assert.equal(document.nodes.get('collection-name-label').hidden, false);
});

test('older collection-list GET cannot overwrite a newer create or delete', async () => {
  let listGets = 0;
  let releaseStartup;
  let releaseStaleDelete;
  const startupList = new Promise(resolve => { releaseStartup = resolve; });
  const staleDeleteList = new Promise(resolve => { releaseStaleDelete = resolve; });
  const fetchImpl = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/platforms') return jsonResponse({ platforms: [] });
    if (pathOnly === '/api/v1/library/attract') return jsonResponse({ items: [], idle_seconds: 60 });
    if (pathOnly === '/api/v1/library/facets') return jsonResponse({ genres: [], years: [] });
    if (pathOnly === '/api/v1/games') return jsonResponse({ games: [] });
    if (pathOnly === '/api/v1/library/collections') {
      listGets += 1;
      if (listGets === 1) {
        await startupList;
        return jsonResponse({ collections: [] });
      }
      if (listGets === 3) {
        await staleDeleteList;
        return jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue', created_at: 11 }] });
      }
      if (listGets === 2) {
        return jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue', created_at: 11 }] });
      }
      return jsonResponse({ collections: [] });
    }
    if (pathOnly === '/api/v1/library/collections/weekend-queue') {
      const method = options && options.method;
      if (method === 'DELETE') return jsonResponse({ id: 'weekend-queue' });
      return jsonResponse({ id: 'weekend-queue', name: 'Weekend Queue', created_at: 11 });
    }
    throw new Error(`unexpected collection list fixture ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  const boot = controller.loadPlatforms();
  await waitForCondition(() => listGets >= 1, 'startup collection list GET did not start');
  await controller.createCollection('Weekend Queue');
  assert.equal(controller.getState().collections.length, 1);
  assert.equal(controller.getState().collections[0].id, 'weekend-queue');
  releaseStartup();
  await boot;
  assert.equal(controller.getState().collections.length, 1);
  assert.equal(controller.getState().collections[0].id, 'weekend-queue');

  const bootAgain = controller.loadPlatforms();
  await waitForCondition(() => listGets >= 3, 'overlapping collection list GET did not start');
  await controller.deleteCollection('weekend-queue');
  assert.deepEqual(controller.getState().collections, []);
  releaseStaleDelete();
  await bootAgain;
  assert.deepEqual(controller.getState().collections, []);
});

test('custom collection rail items sit after smart collections', async () => {
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  const nav = document.nodes.get('collection-list');
  assert.equal(nav.children.length, 1);
  assert.equal(nav.children[0].className.includes('nav-item'), true);
  assert.equal(nav.children[0].attributes.get('data-collection'), 'weekend-queue');
  assert.equal(browserText(nav.children[0]), 'Weekend Queue');
});

test('virtualized wall recycles a bounded window of cards', async () => {
  const games = [];
  for (let index = 0; index < 90; index += 1) {
    games.push({
      id: `snes-game-${index}`,
      title: `Title ${index}`,
      system: 'snes',
      kind: 'raw',
      state: 'available',
      root_online: true,
      content_prepared: true,
      execution: 'fpga_native',
    });
  }
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games })],
  });
  assert.equal(document.nodes.get('catalog-list').children.filter(child => String(child.className).includes('game-card')).length, 80);
  assert.ok(document.nodes.get('catalog-list').children.some(child => String(child.className).includes('wall-spacer')));
});

test('attract overlay enters from a bounded playlist and exits immediately', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    globals: { FogCastAttractDisabled: false, FogCastAttractIdleMs: 20 },
    attractItems: [{ game_id: 'megadrive-sonic-test', title: 'Sonic the Hedgehog', cover: handle }],
  });
  await new Promise(resolve => setTimeout(resolve, 50));
  const attract = document.nodes.get('attract');
  assert.equal(attract.hidden, false);
  assert.equal(document.nodes.get('attract-title').textContent, 'Sonic the Hedgehog');
  const keydown = document.listeners.get('keydown');
  assert.ok(keydown);
  keydown({ key: 'Escape', preventDefault() {} });
  assert.equal(attract.hidden, true);
});

test('attract overlay cycles a multi-item playlist', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const { document, globals } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    globals: { FogCastAttractDisabled: false, FogCastAttractIdleMs: 20, FogCastAttractCycleMs: 25 },
    attractItems: [
      { game_id: 'megadrive-sonic-test', title: 'Sonic the Hedgehog', cover: handle },
      { game_id: 'snes-mario-test', title: 'Super Mario World', cover: handle },
    ],
  });
  await waitForAttractTitle(document, 'Sonic the Hedgehog', 400);
  await waitForAttractTitle(document, 'Super Mario World', 400);
  globals.FogCastAttractDisabled = true;
  const keydown = document.listeners.get('keydown');
  assert.ok(keydown);
  keydown({ key: 'Escape', preventDefault() {} });
  assert.equal(document.nodes.get('attract').hidden, true);
});

test('attract idle seconds follow the host API when no override is set', async () => {
  const { calls, fetchImpl } = queuedFetch([
    jsonResponse({ platforms: [] }),
    jsonResponse({ items: [], idle_seconds: 12 }),
    jsonResponse({ collections: [] }),
    jsonResponse({ genres: [], years: [] }),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadPlatforms();
  assert.equal(controller.getState().attractIdleSeconds, 12);
  assert.equal(calls[1].path, '/api/v1/library/attract?limit=1');
});

test('library settings PUT updates attract idle and reloads the catalog', async () => {
  const { calls, fetchImpl } = queuedFetch([
    jsonResponse({ attract_idle_seconds: 60, preferred_regions: ['usa'] }),
    jsonResponse({ attract_idle_seconds: 12, preferred_regions: ['japan'] }),
    jsonResponse(readFixture('catalog-populated.json')),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSettings();
  assert.equal(controller.getState().attractIdleSeconds, 60);
  await controller.saveSettings({ attract_idle_seconds: 12, preferred_regions: ['japan'] });
  assert.equal(controller.getState().attractIdleSeconds, 12);
  assert.deepEqual(controller.getState().librarySettings.preferred_regions, ['japan']);
  assert.equal(calls[1].path, '/api/v1/library/settings');
  assert.equal(calls[1].options.method, 'PUT');
  assert.ok(String(calls[2].path).startsWith('/api/v1/games'));
});

test('overlapping settings saves persist in start order', async () => {
  let releaseFirst;
  const held = new Promise(resolve => { releaseFirst = resolve; });
  const putBodies = [];
  const fetchImpl = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/library/settings' && options && options.method === 'PUT') {
      const body = JSON.parse(options.body);
      putBodies.push(body);
      if (putBodies.length === 1) await held;
      return jsonResponse({
        attract_idle_seconds: body.attract_idle_seconds,
        preferred_regions: body.preferred_regions,
      });
    }
    if (pathOnly === '/api/v1/games') return jsonResponse({ games: [] });
    throw new Error(`unexpected settings race path ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  const first = controller.saveSettings({ attract_idle_seconds: 12, preferred_regions: ['japan'] });
  const second = controller.saveSettings({ attract_idle_seconds: 8, preferred_regions: ['europe'] });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(putBodies.length, 1);
  assert.equal(putBodies[0].attract_idle_seconds, 12);
  releaseFirst();
  await first;
  await second;
  assert.equal(putBodies.length, 2);
  assert.equal(putBodies[1].attract_idle_seconds, 8);
  assert.equal(controller.getState().attractIdleSeconds, 8);
  assert.deepEqual(controller.getState().librarySettings.preferred_regions, ['europe']);
});

test('failed later settings save does not drop an earlier successful save', async () => {
  let releaseFirst;
  const held = new Promise(resolve => { releaseFirst = resolve; });
  let puts = 0;
  const fetchImpl = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/library/settings' && options && options.method === 'PUT') {
      puts += 1;
      const body = JSON.parse(options.body);
      if (puts === 1) await held;
      if (puts === 2) throw new Error('second save failed');
      return jsonResponse({
        attract_idle_seconds: body.attract_idle_seconds,
        preferred_regions: body.preferred_regions,
      });
    }
    if (pathOnly === '/api/v1/games') return jsonResponse({ games: [] });
    throw new Error(`unexpected settings race path ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  const first = controller.saveSettings({ attract_idle_seconds: 12, preferred_regions: ['japan'] });
  const second = controller.saveSettings({ attract_idle_seconds: 8, preferred_regions: ['europe'] });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(puts, 1);
  releaseFirst();
  await first;
  await assert.rejects(() => second);
  assert.equal(controller.getState().attractIdleSeconds, 12);
  assert.deepEqual(controller.getState().librarySettings.preferred_regions, ['japan']);
});

test('failed settings save does not discard an earlier settings GET', async () => {
  let releaseGet;
  const held = new Promise(resolve => { releaseGet = resolve; });
  const fetchImpl = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/library/settings' && !(options && options.method && options.method !== 'GET')) {
      await held;
      return jsonResponse({ attract_idle_seconds: 12, preferred_regions: ['japan'] });
    }
    if (pathOnly === '/api/v1/library/settings') {
      throw new Error('save failed');
    }
    throw new Error(`unexpected settings race path ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  const loading = controller.loadSettings();
  await assert.rejects(() => controller.saveSettings({ attract_idle_seconds: 8, preferred_regions: ['europe'] }));
  releaseGet();
  await loading;
  assert.equal(controller.getState().attractIdleSeconds, 12);
  assert.deepEqual(controller.getState().librarySettings.preferred_regions, ['japan']);
});

test('library settings idle above the timer limit is clamped', async () => {
  const { fetchImpl } = queuedFetch([
    jsonResponse({ attract_idle_seconds: 3000000000, preferred_regions: ['usa'] }),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSettings();
  assert.equal(controller.getState().attractIdleSeconds, 2147483);
  assert.equal(controller.getState().librarySettings.attract_idle_seconds, 2147483);
});

test('attract idle is clamped before the timer is armed', async () => {
  const idleDelays = [];
  await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    settings: { attract_idle_seconds: 3000000000, preferred_regions: ['usa'] },
    globals: {
      FogCastAttractDisabled: false,
      setTimeout(fn, ms) {
        if (ms >= 1000) {
          idleDelays.push(ms);
          return 0;
        }
        return setTimeout(fn, ms);
      },
    },
  });
  await waitForCondition(() => idleDelays.length > 0, 'attract timer was not armed');
  assert.ok(idleDelays.every(ms => ms > 1 && ms <= 2147483647), JSON.stringify(idleDelays));
  assert.equal(idleDelays[0], 2147483000);
});

test('stale settings GET does not roll back a newer save', async () => {
  let releaseGet;
  const held = new Promise(resolve => { releaseGet = resolve; });
  const fetchImpl = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/library/settings' && !(options && options.method && options.method !== 'GET')) {
      await held;
      return jsonResponse({ attract_idle_seconds: 60, preferred_regions: ['usa'] });
    }
    if (pathOnly === '/api/v1/library/settings') {
      return jsonResponse({ attract_idle_seconds: 8, preferred_regions: ['europe'] });
    }
    if (pathOnly === '/api/v1/games') return jsonResponse({ games: [] });
    throw new Error(`unexpected settings race path ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  const loading = controller.loadSettings();
  await controller.saveSettings({ attract_idle_seconds: 8, preferred_regions: ['europe'] });
  releaseGet();
  await loading;
  assert.equal(controller.getState().attractIdleSeconds, 8);
  assert.deepEqual(controller.getState().librarySettings.preferred_regions, ['europe']);
});

test('stale attract idle read does not roll back a newer save', async () => {
  let releaseAttract;
  const held = new Promise(resolve => { releaseAttract = resolve; });
  const fetchImpl = async (requestPath, options) => {
    const pathOnly = String(requestPath || '').split('?')[0];
    if (pathOnly === '/api/v1/library/attract') {
      await held;
      return jsonResponse({ items: [], idle_seconds: 60 });
    }
    if (pathOnly === '/api/v1/library/settings') {
      return jsonResponse({ attract_idle_seconds: 8, preferred_regions: ['europe'] });
    }
    if (pathOnly === '/api/v1/games') return jsonResponse({ games: [] });
    throw new Error(`unexpected settings race path ${requestPath}`);
  };
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  const attract = controller.loadAttract(24);
  await controller.saveSettings({ attract_idle_seconds: 8, preferred_regions: ['europe'] });
  releaseAttract();
  await attract;
  assert.equal(controller.getState().attractIdleSeconds, 8);
  assert.deepEqual(controller.getState().librarySettings.preferred_regions, ['europe']);
});

function availableGame(id, title, overrides = {}) {
  return {
    id,
    title,
    system: 'snes',
    kind: 'raw',
    state: 'available',
    root_online: true,
    content_prepared: true,
    execution: 'fpga_native',
    ...overrides,
  };
}

function gameCards(document) {
  const list = document.nodes.get('catalog-list');
  if (list && typeof list.querySelectorAll === 'function') {
    return Array.from(list.querySelectorAll('.game-card'));
  }
  return (list && list.children ? Array.from(list.children) : [])
    .filter(child => String(child.className).includes('game-card'));
}

function selectedCard(document) {
  return gameCards(document).find(card => card.attributes.get('aria-pressed') === 'true') || null;
}

function cardMark(card, kind) {
  const wanted = kind ? `card-mark-${kind}` : 'card-mark';
  const visit = nodes => {
    for (const node of nodes || []) {
      if (String(node.className || '').split(/\s+/).includes(wanted)) return node;
      const found = visit(node.children);
      if (found) return found;
    }
    return null;
  };
  return visit(card && card.children);
}

function cardCollectionChip(card) {
  return card && typeof card.querySelectorAll === 'function'
    ? card.querySelectorAll('.card-mark-collection')[0] || null
    : null;
}

function cardCollectionMore(card) {
  return card && typeof card.querySelectorAll === 'function'
    ? card.querySelectorAll('.card-mark-collection-more')[0] || null
    : null;
}

function cardStudio(card) {
  return card && card.children
    ? card.children.find(child => child.className === 'game-studio') || null
    : null;
}

function readyStudioAdapter(studio = 'SEGA', extras = {}) {
  return {
    metadataFor() {
      return {
        summary: 'Provider summary',
        year: '1991',
        genre: 'Action',
        studio,
        players: '1 player',
        isFallback: false,
        metadataState: 'ready',
        ...extras,
      };
    },
  };
}

function readyPlayersAdapter(studio = 'SEGA', players = '1–2 players', extras = {}) {
  return readyStudioAdapter(studio, { players, ...extras });
}

function readyGenreAdapter(genre = 'Beat \'em Up', extras = {}) {
  return readyStudioAdapter('SEGA', { genre, ...extras });
}

async function pressKey(document, key, target = document.activeElement, extras) {
  const event = {
    key,
    target,
    shiftKey: Boolean(extras && extras.shiftKey),
    preventDefault() { event.defaultPrevented = true; },
  };
  await document.listeners.get('keydown')(event);
  await settleBrowser();
  return event;
}

async function openAllGamesGrid(document) {
  const navAll = document.nodes.get('nav-all');
  if (navAll) {
    navAll.click();
    navAll.focus();
  }
  await settleBrowser();
  await pressKey(document, 'Enter', navAll);
}

async function runKeyboardApp({ pages, railPages, platforms, launchResponse, globals, collections, onSettings, attractItems } = {}) {
  const catalogPages = pages || [{
    games: readFixture('catalog-populated.json').games,
    next_cursor: '',
  }];
  const allGames = catalogPages.flatMap(page => page.games);
  const document = browserDocument();
  const calls = [];
  const fetch = async (requestPath, options) => {
    const [pathOnly, query = ''] = String(requestPath || '').split('?');
    const params = new URLSearchParams(query);
    if (pathOnly === '/api/v1/platforms') {
      return jsonResponse({ platforms: platforms || [] });
    }
    if (pathOnly === '/api/v1/library/attract') {
      return jsonResponse({ items: attractItems || [], idle_seconds: 60 });
    }
    if (pathOnly === '/api/v1/library/settings') {
      calls.push({ path: requestPath, options });
      if (onSettings) await onSettings({ path: requestPath, options });
      if (options && (options.method === 'PUT' || options.method === 'PATCH') && options.body) {
        const written = JSON.parse(options.body);
        return jsonResponse({
          attract_idle_seconds: written.attract_idle_seconds,
          preferred_regions: written.preferred_regions,
        });
      }
      return jsonResponse({ attract_idle_seconds: 60, preferred_regions: ['usa', 'world', 'europe', 'japan'] });
    }
    if (pathOnly === '/api/v1/library/facets') {
      return jsonResponse({ genres: [], years: [] });
    }
    if (pathOnly === '/api/v1/session') {
      return jsonResponse({ state: 'idle' });
    }
    if (pathOnly === '/api/v1/session/launch') {
      calls.push({ path: requestPath, options });
      return jsonResponse(launchResponse || { state: 'active', game_id: allGames[0].id });
    }
    if (pathOnly === '/api/v1/library/collections') {
      return jsonResponse({ collections: collections || [] });
    }
    if (pathOnly.startsWith('/api/v1/library/collections/')) {
      calls.push({ path: requestPath, options });
      return jsonResponse({ id: 'weekend-queue', member: (options && options.method) === 'PUT' });
    }
    if (pathOnly.startsWith('/api/v1/library/favorites/')) {
      calls.push({ path: requestPath, options });
      return jsonResponse({ favorite: (options && options.method) === 'PUT' });
    }
    if (pathOnly === '/api/v1/games') {
      calls.push({ path: requestPath, options });
      const collection = params.get('collection') || '';
      if (collection && railPages) {
        return jsonResponse({ games: railPages[collection] || [], next_cursor: '' });
      }
      const cursor = params.get('cursor') || '';
      const page = catalogPages.find(item => (item.cursor || '') === cursor) || catalogPages[0];
      return jsonResponse({
        games: page.games,
        next_cursor: page.next_cursor || '',
      });
    }
    if (pathOnly.startsWith('/api/v1/games/')) {
      const id = decodeURIComponent(pathOnly.slice('/api/v1/games/'.length));
      const game = allGames.find(item => item.id === id) || availableGame(id, id);
      return jsonResponse(game);
    }
    throw new Error(`unexpected keyboard fixture path ${requestPath}`);
  };
  const context = {
    document,
    fetch,
    FogCastAttractDisabled: true,
    setTimeout,
    clearTimeout,
    ...(globals || {}),
  };
  vm.runInNewContext(readAsset('ui_app.js'), context, { filename: 'ui_app.js' });
  await settleBrowser();
  return { document, calls };
}

test('keyboard path moves rail to grid to detail to launch', async () => {
  const { document, calls } = await runKeyboardApp();
  await settleBrowser();
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
  assert.equal(document.activeElement, document.nodes.get('nav-home'));

  await pressKey(document, 'ArrowDown');
  assert.equal(document.activeElement, document.nodes.get('nav-all'));
  await pressKey(document, 'Home');
  assert.equal(document.activeElement, document.nodes.get('nav-home'));
  await pressKey(document, 'End');
  assert.equal(document.activeElement, document.nodes.get('nav-recently-added'));
  await pressKey(document, 'Home');
  await pressKey(document, 'ArrowDown');
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.attributes.get('data-game-id'), 'megadrive-sonic-test');

  await pressKey(document, 'ArrowRight');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-unknown-test');

  await pressKey(document, 'Home');
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
  assert.equal(document.activeElement.id, 'launch-game');
  assert.match(document.nodes.get('detail-content').children.map(node => node.textContent).join(' '), /Sonic/);

  await pressKey(document, 'Enter');
  assert.equal(calls.some(call => call.path === '/api/v1/session/launch'), true);
  const launch = calls.find(call => call.path === '/api/v1/session/launch');
  assert.equal(launch.options.method, 'POST');
  assert.equal(JSON.parse(launch.options.body).game_id, 'megadrive-sonic-test');

  await pressKey(document, 'Escape');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');
  await pressKey(document, 'Escape');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
  assert.equal(document.activeElement, document.nodes.get('nav-all'));
});

test('type-to-search focuses the search field from the cover wall', async () => {
  const { document, calls } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  const before = calls.filter(call => String(call.path).startsWith('/api/v1/games')).length;
  await pressKey(document, 's');
  assert.equal(document.activeElement, document.nodes.get('game-search'));
  assert.equal(document.nodes.get('game-search').value, 's');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'search');
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  assert.ok(calls.filter(call => String(call.path).startsWith('/api/v1/games')).length > before);
  assert.match(calls.at(-1).path, /[?&]q=s/);
  await pressKey(document, 'Escape', document.nodes.get('game-search'));
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');
});

test('grid Home/End and end-of-page arrows keep the games cursor', async () => {
  const pageOne = [];
  const pageTwo = [];
  for (let index = 0; index < 4; index += 1) {
    pageOne.push(availableGame(`snes-page1-${index}`, `Alpha ${index}`));
    pageTwo.push(availableGame(`snes-page2-${index}`, `Bravo ${index}`));
  }
  const { document, calls } = await runKeyboardApp({
    pages: [
      { games: pageOne, next_cursor: 'cursor-1' },
      { cursor: 'cursor-1', games: pageTwo, next_cursor: '' },
    ],
  });
  await settleBrowser();
  await openAllGamesGrid(document);
  assert.equal(gameCards(document).length, 4);
  await pressKey(document, 'End');
  assert.ok(calls.some(call => String(call.path).includes('cursor=cursor-1')));
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-page2-3');
  await pressKey(document, 'Home');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-page1-0');
  await pressKey(document, 'End');
  await pressKey(document, 'ArrowRight');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-page2-3');
});

test('grid arrows move by cover-wall columns and left edge returns to the rail', async () => {
  const games = [];
  for (let index = 0; index < 8; index += 1) {
    games.push(availableGame(`snes-grid-${index}`, `Grid ${index}`));
  }
  const { document } = await runKeyboardApp({ pages: [{ games }] });
  await settleBrowser();
  document.nodes.get('catalog-list').clientWidth = 896;
  await openAllGamesGrid(document);
  await pressKey(document, 'ArrowDown');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-4');
  await pressKey(document, 'ArrowUp');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-0');
  await pressKey(document, 'ArrowLeft');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
  assert.equal(document.activeElement.className.includes('nav-item'), true);
});

test('narrow cover wall uses padded 140px tracks for arrow jumps', async () => {
  const games = [];
  for (let index = 0; index < 8; index += 1) {
    games.push(availableGame(`snes-grid-${index}`, `Grid ${index}`));
  }
  const { document } = await runKeyboardApp({
    pages: [{ games }],
    globals: {
      getComputedStyle() {
        return {
          paddingLeft: '4px',
          paddingRight: '4px',
          columnGap: '14px',
          gap: '14px',
          getPropertyValue(name) {
            if (name === '--wall-min-track') return '140px';
            if (name === 'padding-left') return '4px';
            if (name === 'padding-right') return '4px';
            if (name === 'column-gap' || name === 'gap') return '14px';
            return '';
          },
        };
      },
    },
  });
  await settleBrowser();
  document.nodes.get('catalog-list').clientWidth = 360;
  await openAllGamesGrid(document);
  await pressKey(document, 'ArrowDown');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-2');
  await pressKey(document, 'ArrowUp');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-0');
});

test('Enter on favorite or session controls does not launch', async () => {
  const { document, calls } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  await pressKey(document, 'Enter');
  assert.equal(document.activeElement.id, 'launch-game');
  const before = calls.filter(call => call.path === '/api/v1/session/launch').length;

  const favorite = document.getElementById('favorite-game');
  favorite.focus();
  const favoriteEnter = await pressKey(document, 'Enter', favorite);
  assert.equal(favoriteEnter.defaultPrevented, undefined);
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, before);

  const refresh = document.getElementById('refresh-session');
  refresh.focus();
  const refreshEnter = await pressKey(document, 'Enter', refresh);
  assert.equal(refreshEnter.defaultPrevented, undefined);
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, before);

  const launch = document.getElementById('launch-game');
  launch.focus();
  const launchEnter = await pressKey(document, 'Enter', launch);
  assert.equal(launchEnter.defaultPrevented, true);
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, before + 1);
});

test('Enter on collection member control does not launch', async () => {
  const { document, calls } = await runKeyboardApp({
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  await settleBrowser();
  await openAllGamesGrid(document);
  await pressKey(document, 'Enter');
  const member = document.getElementById('collection-member-weekend-queue');
  assert.ok(member);
  member.focus();
  const before = calls.filter(call => call.path === '/api/v1/session/launch').length;
  const memberEnter = await pressKey(document, 'Enter', member);
  assert.equal(memberEnter.defaultPrevented, undefined);
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, before);
});

test('catalog filter and version selects keep native ArrowDown and Enter', async () => {
  const sonic = {
    ...readFixture('catalog-populated.json').games[0],
    variants: [
      availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' }),
      availableGame('megadrive-sonic-jp', 'Sonic the Hedgehog (Japan)', { system: 'megadrive' }),
    ],
  };
  const { document } = await runKeyboardApp({ pages: [{ games: [sonic] }] });
  await settleBrowser();
  const filter = document.getElementById('filter-system');
  filter.focus();
  const filterArrow = await pressKey(document, 'ArrowDown', filter);
  const filterEnter = await pressKey(document, 'Enter', filter);
  assert.equal(filterArrow.defaultPrevented, undefined);
  assert.equal(filterEnter.defaultPrevented, undefined);
  assert.notEqual(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');

  await openAllGamesGrid(document);
  await pressKey(document, 'Enter');
  const version = document.getElementById('game-version');
  assert.ok(version);
  version.focus();
  const versionArrow = await pressKey(document, 'ArrowDown', version);
  const versionEnter = await pressKey(document, 'Enter', version);
  assert.equal(versionArrow.defaultPrevented, undefined);
  assert.equal(versionEnter.defaultPrevented, undefined);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
});

test('ArrowDown and Enter on search still move to the cover wall', async () => {
  const { document } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  document.nodes.get('game-search').focus();
  const down = await pressKey(document, 'ArrowDown', document.nodes.get('game-search'));
  assert.equal(down.defaultPrevented, true);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');
  document.nodes.get('game-search').focus();
  const enter = await pressKey(document, 'Enter', document.nodes.get('game-search'));
  assert.equal(enter.defaultPrevented, true);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');
});

test('settings overlay cancels attract and enterAttract no-ops until close', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    globals: { FogCastAttractDisabled: false, FogCastAttractIdleMs: 30 },
    attractItems: [{ game_id: 'megadrive-sonic-test', title: 'Sonic the Hedgehog', cover: handle }],
  });
  await document.nodes.get('open-settings').click();
  await waitForCondition(() => document.nodes.get('settings').hidden === false, 'settings overlay did not open');
  assert.equal(document.nodes.get('attract').hidden, true);
  await new Promise(resolve => setTimeout(resolve, 80));
  assert.equal(document.nodes.get('attract').hidden, true);
  assert.equal(document.nodes.get('settings').hidden, false);
  const keydown = document.listeners.get('keydown');
  keydown({ key: 'Escape', preventDefault() {} });
  assert.equal(document.nodes.get('settings').hidden, true);
  await waitForAttractTitle(document, 'Sonic the Hedgehog', 400);
  await document.nodes.get('open-settings').click();
  await waitForCondition(() => document.nodes.get('settings').hidden === false, 'settings overlay did not reopen');
  assert.equal(document.nodes.get('attract').hidden, true);
  await new Promise(resolve => setTimeout(resolve, 80));
  assert.equal(document.nodes.get('attract').hidden, true);
});

test('settings overlay traps Tab and blocks launcher Enter', async () => {
  const { document, calls } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  await pressKey(document, 'Enter');
  assert.ok(document.getElementById('launch-game'));
  await document.nodes.get('open-settings').click();
  await waitForCondition(() => (
    document.nodes.get('settings').hidden === false
    && document.activeElement === document.nodes.get('settings-attract-idle')
  ), 'settings overlay did not open');
  assert.equal(document.nodes.get('launcher').inert, true);

  document.nodes.get('close-settings').focus();
  const tab = await pressKey(document, 'Tab', document.nodes.get('close-settings'));
  assert.equal(tab.defaultPrevented, true);
  assert.equal(document.activeElement, document.nodes.get('settings-attract-idle'));

  const shiftTab = await pressKey(document, 'Tab', document.nodes.get('settings-attract-idle'), { shiftKey: true });
  assert.equal(shiftTab.defaultPrevented, true);
  assert.equal(document.activeElement, document.nodes.get('close-settings'));

  const allowed = new Set(['settings-attract-idle', 'settings-preferred-regions', 'save-settings', 'close-settings']);
  for (let index = 0; index < 6; index += 1) {
    await pressKey(document, 'Tab');
    assert.ok(allowed.has(document.activeElement && document.activeElement.id), document.activeElement && document.activeElement.id);
  }

  const launch = document.getElementById('launch-game');
  launch.focus();
  const leaked = await pressKey(document, 'Enter', launch);
  assert.equal(leaked.defaultPrevented, true);
  assert.equal(document.activeElement, document.nodes.get('settings-attract-idle'));
  assert.equal(calls.some(call => call.path === '/api/v1/session/launch'), false);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'settings');

  document.nodes.get('game-search').focus();
  const searchEnter = await pressKey(document, 'Enter', document.nodes.get('game-search'));
  assert.equal(searchEnter.defaultPrevented, true);
  assert.notEqual(document.activeElement, document.nodes.get('game-search'));
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'settings');

  await pressKey(document, 'Escape', document.nodes.get('settings-attract-idle'));
  assert.equal(document.nodes.get('settings').hidden, true);
  assert.equal(document.nodes.get('launcher').inert, false);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
});

test('attract idle hydrates from settings before the first timer is armed', async () => {
  const idleDelays = [];
  await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    settings: { attract_idle_seconds: 12, preferred_regions: ['usa'] },
    globals: {
      FogCastAttractDisabled: false,
      setTimeout(fn, ms) {
        if (ms >= 1000) {
          idleDelays.push(ms);
          return 0;
        }
        return setTimeout(fn, ms);
      },
    },
  });
  await settleBrowser();
  assert.equal(idleDelays.includes(60000), false, JSON.stringify(idleDelays));
  assert.equal(idleDelays.includes(12000), true, JSON.stringify(idleDelays));
});

test('stale settings GET does not overwrite typed fields', async () => {
  let releaseGet;
  const held = new Promise(resolve => { releaseGet = resolve; });
  let settingsGets = 0;
  const { document } = await runKeyboardApp({
    onSettings: async ({ options }) => {
      if (options && options.method && options.method !== 'GET') return;
      settingsGets += 1;
      if (settingsGets > 1) await held;
    },
  });
  await settleBrowser();
  const opening = document.nodes.get('open-settings').click();
  await waitForCondition(() => document.nodes.get('settings').hidden === false, 'settings overlay did not open');
  document.nodes.get('settings-attract-idle').value = '12';
  document.nodes.get('settings-preferred-regions').value = 'japan';
  document.nodes.get('settings-attract-idle').dispatchEvent({ type: 'input' });
  releaseGet();
  await opening;
  await settleBrowser();
  assert.equal(document.nodes.get('settings-attract-idle').value, '12');
  assert.equal(document.nodes.get('settings-preferred-regions').value, 'japan');
});

test('stale settings save does not refill a reopened form', async () => {
  let releaseSave;
  const held = new Promise(resolve => { releaseSave = resolve; });
  const { document } = await runKeyboardApp({
    onSettings: async ({ options }) => {
      if (options && options.method === 'PUT') await held;
    },
  });
  await settleBrowser();
  await document.nodes.get('open-settings').click();
  await waitForCondition(() => (
    document.nodes.get('settings').hidden === false
    && document.nodes.get('settings-attract-idle').value === '60'
  ), 'settings overlay did not open');
  document.nodes.get('settings-attract-idle').value = '12';
  document.nodes.get('settings-preferred-regions').value = 'japan';
  const saving = document.nodes.get('save-settings').click();
  await pressKey(document, 'Escape', document.nodes.get('settings-attract-idle'));
  assert.equal(document.nodes.get('settings').hidden, true);
  await document.nodes.get('open-settings').click();
  await waitForCondition(() => (
    document.nodes.get('settings').hidden === false
    && document.nodes.get('settings-attract-idle').value === '60'
  ), 'settings overlay did not reopen');
  document.nodes.get('settings-attract-idle').value = '8';
  document.nodes.get('settings-attract-idle').dispatchEvent({ type: 'input' });
  releaseSave();
  await saving;
  await settleBrowser();
  assert.equal(document.nodes.get('settings-attract-idle').value, '8');
});

test('settings overlay opens from the header and Escape returns to the previous pane', async () => {
  const { document, calls } = await runKeyboardApp();
  await settleBrowser();
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
  await document.nodes.get('open-settings').click();
  await waitForCondition(() => (
    document.nodes.get('settings').hidden === false
    && document.nodes.get('launcher').attributes.get('data-keyboard-pane') === 'settings'
    && document.activeElement === document.nodes.get('settings-attract-idle')
    && document.nodes.get('settings-attract-idle').value === '60'
  ), 'settings overlay did not open');
  assert.equal(document.nodes.get('settings').hidden, false);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'settings');
  assert.equal(document.activeElement, document.nodes.get('settings-attract-idle'));
  assert.equal(document.nodes.get('settings-attract-idle').value, '60');
  assert.equal(document.nodes.get('settings-preferred-regions').value, 'usa, world, europe, japan');
  document.nodes.get('settings-attract-idle').value = '12';
  document.nodes.get('settings-preferred-regions').value = 'japan, europe';
  const arrow = await pressKey(document, 'ArrowDown', document.nodes.get('settings-attract-idle'));
  const enter = await pressKey(document, 'Enter', document.nodes.get('settings-attract-idle'));
  assert.equal(arrow.defaultPrevented, undefined);
  assert.equal(enter.defaultPrevented, undefined);
  assert.equal(calls.some(call => call.path === '/api/v1/session/launch'), false);
  const typedF = await pressKey(document, 'f', document.nodes.get('settings-attract-idle'));
  assert.equal(typedF.defaultPrevented, undefined);
  assert.notEqual(document.activeElement, document.nodes.get('game-search'));
  await document.nodes.get('save-settings').click();
  await settleBrowser();
  const saved = calls.find(call => call.path === '/api/v1/library/settings' && call.options && call.options.method === 'PUT');
  assert.ok(saved);
  assert.equal(JSON.parse(saved.options.body).attract_idle_seconds, 12);
  assert.deepEqual(JSON.parse(saved.options.body).preferred_regions, ['japan', 'europe']);
  await pressKey(document, 'Escape', document.nodes.get('settings-attract-idle'));
  assert.equal(document.nodes.get('settings').hidden, true);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
});

test('idle session chrome stays quiet while controls remain available', async () => {
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  const panel = document.nodes.get('session-panel');
  assert.equal(document.nodes.get('session-status').textContent, 'No active session.');
  assert.equal(panel.className, 'session-panel session-quiet');
  assert.ok(document.nodes.get('session-actions').children.find(node => node.id === 'refresh-session'));
  assert.ok(document.nodes.get('session-actions').children.find(node => node.id === 'stop-session'));
});

test('rich detail stacks hero cover with backdrop and skips video under reduced motion', async () => {
  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const adapter = {
    metadataFor() {
      return {
        summary: 'Provider summary',
        year: '1991',
        genre: 'Platformer',
        studio: 'SEGA',
        players: '1 player',
        coverArtworkHandle: handle,
        backdropArtworkHandle: handle,
        logoHandle: handle,
        marqueeHandle: handle,
        screenshotHandles: [handle],
        videoHandle: handle,
        isFallback: false,
        metadataState: 'ready',
      };
    },
  };
  const reduced = await runBrowserApp({
    adapter,
    globals: { matchMedia: () => ({ matches: true }) },
    responses: [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture('detail-refreshed.json')),
    ],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await reduced.document.nodes.get('catalog-list').children[0].click();
  const reducedDetail = reduced.document.nodes.get('detail-content');
  assert.equal(reducedDetail.children[0].className, 'backdrop-art image-art');
  assert.equal(reducedDetail.children[1].className, 'detail-hero');
  assert.equal(reducedDetail.children[1].children[0].className, 'cover-art image-art');
  assert.ok(reducedDetail.children[1].children[1].children.some(child => String(child.className).includes('logo-art')));
  assert.ok(reducedDetail.children.some(child => child.className === 'extra-stills'));
  assert.ok(reducedDetail.children.some(child => String(child.className).includes('marquee-art')));
  assert.equal(reducedDetail.children.some(child => child.className === 'detail-video'), false);

  const motion = await runBrowserApp({
    adapter,
    globals: { matchMedia: () => ({ matches: false }) },
    responses: [
      jsonResponse(readFixture('catalog-populated.json')),
      jsonResponse(readFixture('detail-refreshed.json')),
    ],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await motion.document.nodes.get('catalog-list').children[0].click();
  const motionDetail = motion.document.nodes.get('detail-content');
  const marquee = motionDetail.children.find(child => String(child.className).includes('marquee-art'));
  const video = motionDetail.children.find(child => child.className === 'detail-video');
  assert.ok(marquee);
  assert.ok(video);
  assert.equal(video.attributes.get('src'), `/api/v1/presentation/media/${handle}`);
  assert.equal(video.muted, true);
  assert.equal(video.attributes.get('muted'), '');
  assert.equal(video.attributes.get('controls'), '');
  assert.equal(video.attributes.get('playsinline'), '');
  assert.match(readAsset('ui_app.js'), /video\.setAttribute\('controls', ''\);/);
});

test('cover-card variant meta is independently classed so fallback cannot overlap it', async () => {
  const css = readAsset('ui.css');
  assert.doesNotMatch(css, /\.game-meta\s*\+\s*\.game-meta/);
  assert.match(css, /\.game-card \.game-meta-variant/);
  assert.match(css, /\.game-card \.fallback-note/);
  const games = [{
    ...readFixture('catalog-populated.json').games[1],
    variant_count: 3,
    region: 'usa',
  }];
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  const card = document.nodes.get('catalog-list').children[0];
  const metas = card.children.filter(child => String(child.className).includes('game-meta'));
  const notes = card.children.filter(child => child.className === 'fallback-note');
  assert.equal(notes.length, 1);
  assert.equal(metas.length, 2);
  assert.equal(metas[0].className, 'game-meta');
  assert.match(metas[0].textContent, /usa/i);
  assert.equal(metas[1].className, 'game-meta game-meta-variant');
  assert.ok(card.children.indexOf(notes[0]) > card.children.indexOf(metas[0]));
  assert.ok(card.children.indexOf(notes[0]) < card.children.indexOf(metas[1]));
});

test('detail CSS meets the panel edge without negative-margin backdrop bleed', () => {
  const css = readAsset('ui.css');
  assert.doesNotMatch(css, /\.backdrop-art[^{]*\{[^}]*margin:\s*-/);
  assert.doesNotMatch(css, /width:\s*calc\(100% \+/);
  assert.match(css, /#detail-content[^{]*\{[^}]*overflow-x:\s*hidden/);
  assert.match(css, /--detail-inset/);
  assert.match(css, /#detail-content\s*>\s*:not\(\.backdrop-art\):not\(\.detail-hero\)\s*\{[^}]*max-width:\s*calc\(100%\s*-\s*\(\s*2\s*\*\s*var\(--detail-inset\)\s*\)\s*\)/);
  assert.match(css, /\.backdrop-art\s*\{[^}]*margin:\s*0;[^}]*width:\s*100%/);
  assert.match(css, /\.marquee-art[^{]*\{[^}]*width:\s*100%/);
  assert.match(css, /\.detail-video[^{]*\{[^}]*width:\s*100%/);
});

test('selected cards keep a visible selected and focus contract', async () => {
  const { document } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  const card = selectedCard(document);
  assert.ok(card.className.includes('selected'));
  assert.equal(card.tabIndex, 0);
  assert.equal(card.focused, true);
  const others = gameCards(document).filter(item => item !== card);
  assert.ok(others.every(item => item.tabIndex === -1));
});

function homeRails(document) {
  const list = document.nodes.get('catalog-list');
  return list && typeof list.querySelectorAll === 'function'
    ? Array.from(list.querySelectorAll('.home-rail'))
    : [];
}

test('home is the default view and fetches each rail from the games API', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const favoriteGame = availableGame('snes-mario-test', 'Mario', { system: 'snes' });
  const recentGame = availableGame('snes-unknown-test', 'Unknown Game', { system: 'snes' });
  const unplayedGame = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const recentlyAddedGame = availableGame('snes-actraiser-test', 'ActRaiser', { system: 'snes' });
  const weekendGame = availableGame('megadrive-streets-test', 'Streets', { system: 'megadrive' });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
    homeRails: {
      continue: jsonResponse({ games: [continueGame] }),
      favorites: jsonResponse({ games: [favoriteGame] }),
      recents: jsonResponse({ games: [recentGame] }),
      unplayed: jsonResponse({ games: [unplayedGame] }),
      recently_added: jsonResponse({ games: [recentlyAddedGame] }),
      'weekend-queue': jsonResponse({ games: [weekendGame] }),
    },
  });
  await settleBrowser();
  const rails = homeRails(document);
  assert.deepEqual(rails.map(rail => rail.attributes.get('data-home-rail')), [
    'continue', 'favorites', 'recents', 'unplayed', 'recently_added', 'weekend-queue',
  ]);
  assert.deepEqual(rails.map(rail => rail.querySelectorAll('.home-rail-title')[0]?.textContent || rail.children[0].children[0].textContent), [
    'Continue', 'Favorites', 'Recent', 'Unplayed', 'Recently added', 'Weekend Queue',
  ]);
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), false);
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  const homeCalls = calls.filter(call => String(call.path).startsWith('/api/v1/games'));
  assert.ok(homeCalls.some(call => call.path.includes('collection=continue') && call.path.includes('limit=12')));
  assert.ok(homeCalls.some(call => call.path.includes('collection=favorites') && call.path.includes('limit=12')));
  assert.ok(homeCalls.some(call => call.path.includes('collection=recents') && call.path.includes('limit=12')));
  assert.ok(homeCalls.some(call => call.path.includes('collection=unplayed') && call.path.includes('limit=12')));
  assert.ok(homeCalls.some(call => call.path.includes('collection=recently_added') && call.path.includes('limit=12')));
  assert.ok(homeCalls.some(call => call.path.includes('collection=weekend-queue') && call.path.includes('limit=12')));
  assert.equal(homeCalls.some(call => /[?&]collection=home\b/.test(call.path)), false);
});

test('home omits empty rails and See all opens the existing cover wall', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    collections: [
      { id: 'empty-shelf', name: 'Empty Shelf' },
      { id: 'weekend-queue', name: 'Weekend Queue' },
    ],
    homeRails: {
      continue: jsonResponse({ games: [continueGame] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
      'empty-shelf': jsonResponse({ games: [] }),
      'weekend-queue': jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rails = homeRails(document);
  assert.deepEqual(rails.map(rail => rail.attributes.get('data-home-rail')), ['continue']);
  const seeAll = rails[0].querySelectorAll('.home-rail-see-all')[0] || rails[0].children[0].children[1];
  seeAll.click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  assert.equal(document.nodes.get('nav-continue').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), false);
  assert.ok(gameCards(document).length > 0);
});

test('catalog sort dropdown locks on Home and collection See-alls without changing stored sort', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const recentlyAddedGame = availableGame('snes-actraiser-test', 'ActRaiser', { system: 'snes' });
  const weekendGame = availableGame('megadrive-streets-test', 'Streets', { system: 'megadrive' });
  const populated = jsonResponse(readFixture('catalog-populated.json'));
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated, populated, populated],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
    homeRails: {
      continue: jsonResponse({ games: [continueGame] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
      recently_added: jsonResponse({ games: [recentlyAddedGame] }),
      'weekend-queue': jsonResponse({ games: [weekendGame] }),
    },
  });
  await settleBrowser();
  const sort = document.nodes.get('catalog-sort');
  const label = document.nodes.get('catalog-sort-label');
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'title');
  assert.equal(sort.getAttribute('data-sort-override'), 'title');
  assertCatalogSortVisibility(document, true);

  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(sort.disabled, false);
  assert.equal(sort.getAttribute('data-sort-override'), null);
  assert.equal(sort.getAttribute('aria-label'), 'Sort');
  assert.equal(label.textContent, 'Sort');
  assertCatalogSortVisibility(document, false);

  sort.value = 'year';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.ok(calls.some(call => call.path === '/api/v1/games?sort=year&grouped=1'));

  document.nodes.get('nav-continue').click();
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'title');
  assert.equal(sort.getAttribute('data-sort-override'), 'title');
  assert.equal(label.textContent, 'Sort (Title)');
  assertCatalogSortVisibility(document, false);
  sort.value = 'system';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assert.equal(sort.getAttribute('data-sort-override'), 'title');
  assert.equal(calls.some(call => String(call.path).includes('collection=continue') && String(call.path).includes('sort=platform')), false);

  document.nodes.get('nav-recently-added').click();
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'recently_added');
  assert.equal(sort.getAttribute('data-sort-override'), 'recently_added');
  assert.equal(label.textContent, 'Sort (Recently added)');
  assert.equal(sort.getAttribute('aria-label'), 'Sort (Recently added)');
  assertCatalogSortVisibility(document, false);

  const custom = document.nodes.get('nav-collection-weekend-queue');
  assert.ok(custom);
  custom.click();
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'title');
  assert.equal(sort.getAttribute('data-sort-override'), 'title');
  assert.equal(label.textContent, 'Sort (Title)');
  assertCatalogSortVisibility(document, false);

  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.equal(sort.getAttribute('data-sort-override'), null);
  assert.equal(label.textContent, 'Sort');
  assertCatalogSortVisibility(document, false);
  assert.ok(calls.filter(call => call.path === '/api/v1/games?sort=year&grouped=1').length >= 2);
});

test('empty collection See-alls lock the sort dropdown and empty All-games unlocks it', async () => {
  const populated = jsonResponse(readFixture('catalog-populated.json'));
  const emptyCatalog = jsonResponse(readFixture('catalog-empty.json'));
  const empty = jsonResponse({ games: [] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated, populated, emptyCatalog],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
    homeRails: {
      continue: empty,
      favorites: empty,
      recents: empty,
      'weekend-queue': empty,
    },
  });
  await settleBrowser();
  const sort = document.nodes.get('catalog-sort');
  const label = document.nodes.get('catalog-sort-label');
  assert.equal(sort.disabled, true);
  assertCatalogSortVisibility(document, true);

  document.nodes.get('nav-all').click();
  await settleBrowser();
  sort.value = 'year';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.ok(calls.some(call => call.path === '/api/v1/games?sort=year&grouped=1'));

  for (const nav of ['nav-continue', 'nav-favorites']) {
    document.nodes.get(nav).click();
    await settleBrowser();
    assert.equal(document.nodes.get('catalog-status').textContent, 'empty', nav);
    assert.equal(sort.disabled, true, nav);
    assert.equal(sort.value, 'title', nav);
    assert.equal(sort.getAttribute('data-sort-override'), 'title', nav);
    assert.equal(label.textContent, 'Sort (Title)', nav);
    assert.equal(sort.getAttribute('aria-label'), 'Sort (Title)', nav);
    sort.value = 'system';
    sort.dispatchEvent({ type: 'change' });
    await settleBrowser();
    assert.equal(sort.disabled, true, nav);
    assert.equal(sort.getAttribute('data-sort-override'), 'title', nav);
    assert.equal(label.textContent, 'Sort (Title)', nav);
  }

  const custom = document.nodes.get('nav-collection-weekend-queue');
  assert.ok(custom);
  custom.click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-status').textContent, 'empty');
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'title');
  assert.equal(sort.getAttribute('data-sort-override'), 'title');
  assert.equal(label.textContent, 'Sort (Title)');
  sort.value = 'system';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(calls.some(call => String(call.path).includes('sort=platform')), false);

  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-status').textContent, 'empty');
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.equal(sort.getAttribute('data-sort-override'), null);
  assert.equal(label.textContent, 'Sort');
  assert.equal(sort.getAttribute('aria-label'), 'Sort');
  assert.ok(calls.filter(call => call.path === '/api/v1/games?sort=year&grouped=1').length >= 2);
});

test('Recents See-all keeps play order and does not claim Title', async () => {
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const populated = jsonResponse(readFixture('catalog-populated.json'));
  const recents = jsonResponse({ games: [zelda, sonic] });
  const empty = jsonResponse({ games: [] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated, populated, populated],
    homeRails: {
      continue: empty,
      favorites: empty,
      recents,
    },
  });
  await settleBrowser();
  const sort = document.nodes.get('catalog-sort');
  const label = document.nodes.get('catalog-sort-label');
  document.nodes.get('nav-all').click();
  await settleBrowser();
  sort.value = 'year';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.value, 'year');

  document.nodes.get('nav-recents').click();
  await settleBrowser();
  const seeAll = calls.filter(call => String(call.path).includes('collection=recents') && !String(call.path).includes('limit=12')).at(-1);
  assert.ok(seeAll);
  assert.equal(seeAll.path, '/api/v1/games?collection=recents&grouped=1');
  assert.equal(seeAll.path.includes('sort='), false);
  assert.deepEqual(gameCards(document).map(card => card.getAttribute('data-game-id')), [
    'nes-zelda-test',
    'megadrive-sonic-test',
  ]);
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'recents');
  assert.equal(sort.getAttribute('data-sort-override'), 'recents');
  assert.equal(label.textContent, 'Sort (Recent)');
  assert.equal(sort.getAttribute('aria-label'), 'Sort (Recent)');
  sort.value = 'title';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assert.equal(sort.getAttribute('data-sort-override'), 'recents');
  assert.equal(calls.some(call => String(call.path).includes('collection=recents') && String(call.path).includes('sort=title')), false);
  assert.equal(calls.some(call => String(call.path).includes('collection=recents') && String(call.path).includes('sort=year')), false);

  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.ok(calls.filter(call => call.path === '/api/v1/games?sort=year&grouped=1').length >= 2);
});

test('All-games cannot select Recent and silently get Title', async () => {
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const populated = jsonResponse(readFixture('catalog-populated.json'));
  const recents = jsonResponse({ games: [zelda, sonic] });
  const empty = jsonResponse({ games: [] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated, populated, populated],
    homeRails: {
      continue: empty,
      favorites: empty,
      recents,
    },
  });
  await settleBrowser();
  const sort = document.nodes.get('catalog-sort');
  document.nodes.get('nav-all').click();
  await settleBrowser();
  sort.value = 'year';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.value, 'year');
  const recentsOption = catalogSortOption(document, 'recents');
  assert.ok(recentsOption);
  assert.equal(recentsOption.hidden, true);
  assert.equal(recentsOption.disabled, true);
  const yearFetches = calls.filter(call => call.path === '/api/v1/games?sort=year&grouped=1').length;
  const titleFetches = calls.filter(call => call.path === '/api/v1/games?grouped=1').length;
  sort.value = 'recents';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.equal(recentsOption.hidden, true);
  assert.equal(recentsOption.disabled, true);
  assert.equal(calls.filter(call => call.path === '/api/v1/games?sort=year&grouped=1').length, yearFetches);
  assert.equal(calls.filter(call => call.path === '/api/v1/games?grouped=1').length, titleFetches);

  document.nodes.get('nav-recents').click();
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'recents');
  assert.equal(recentsOption.hidden, false);
  assert.equal(recentsOption.disabled, false);
  assert.equal(document.nodes.get('catalog-sort-label').textContent, 'Sort (Recent)');

  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.equal(recentsOption.hidden, true);
  assert.equal(recentsOption.disabled, true);
});

test('Home hides sort when Recents or Recently added rails are visible', async () => {
  const shell = readAsset('ui_shell.html');
  const css = readAsset('ui.css');
  assert.match(shell, /<label class="filter-label" id="catalog-sort-filter">/);
  assert.match(css, /\.filter-label\[hidden\]\s*\{\s*display:\s*none\s*;\s*\}/);
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const recentGame = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const recentlyAddedGame = availableGame('snes-actraiser-test', 'ActRaiser', { system: 'snes' });
  const populated = jsonResponse(readFixture('catalog-populated.json'));
  const empty = jsonResponse({ games: [] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated, populated, populated],
    homeRails: {
      continue: jsonResponse({ games: [continueGame] }),
      favorites: empty,
      recents: jsonResponse({ games: [recentGame] }),
      recently_added: jsonResponse({ games: [recentlyAddedGame] }),
    },
  });
  await settleBrowser();
  const sort = document.nodes.get('catalog-sort');
  const label = document.nodes.get('catalog-sort-label');
  assert.deepEqual(homeRails(document).map(rail => rail.attributes.get('data-home-rail')), [
    'continue', 'recents', 'recently_added',
  ]);
  assert.equal(sort.disabled, true);
  assertCatalogSortVisibility(document, true);
  assert.equal(sort.getAttribute('data-sort-override'), 'title');
  const homeFetchCount = calls.filter(call => String(call.path).startsWith('/api/v1/games')).length;

  document.nodes.get('nav-all').click();
  await settleBrowser();
  assertCatalogSortVisibility(document, false);
  assert.equal(sort.disabled, false);
  assert.equal(label.textContent, 'Sort');
  sort.value = 'year';
  sort.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(sort.value, 'year');
  assert.ok(calls.some(call => call.path === '/api/v1/games?sort=year&grouped=1'));

  document.nodes.get('nav-home').click();
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assertCatalogSortVisibility(document, true);
  const homeAfterYear = calls.slice(homeFetchCount).filter(call => (
    String(call.path).startsWith('/api/v1/games') && String(call.path).includes('limit=12')
  ));
  assert.ok(homeAfterYear.some(call => call.path.includes('collection=recents')));
  assert.ok(homeAfterYear.some(call => call.path.includes('collection=recently_added')));
  assert.equal(homeAfterYear.every(call => !call.path.includes('sort=year') && !call.path.includes('sort=platform')), true);

  document.nodes.get('nav-recents').click();
  await settleBrowser();
  assertCatalogSortVisibility(document, false);
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'recents');
  assert.equal(label.textContent, 'Sort (Recent)');
  assert.equal(sort.getAttribute('aria-label'), 'Sort (Recent)');

  document.nodes.get('nav-recently-added').click();
  await settleBrowser();
  assertCatalogSortVisibility(document, false);
  assert.equal(sort.disabled, true);
  assert.equal(sort.value, 'recently_added');
  assert.equal(label.textContent, 'Sort (Recently added)');
  assert.equal(sort.getAttribute('aria-label'), 'Sort (Recently added)');

  document.nodes.get('nav-all').click();
  await settleBrowser();
  assertCatalogSortVisibility(document, false);
  assert.equal(sort.disabled, false);
  assert.equal(sort.value, 'year');
  assert.equal(label.textContent, 'Sort');
});

test('Home hides sort when only title rails are visible', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const populated = jsonResponse(readFixture('catalog-populated.json'));
  const empty = jsonResponse({ games: [] });
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [populated],
    homeRails: {
      continue: jsonResponse({ games: [continueGame] }),
      favorites: empty,
      recents: empty,
      unplayed: empty,
      recently_added: empty,
    },
  });
  await settleBrowser();
  const sort = document.nodes.get('catalog-sort');
  assert.deepEqual(homeRails(document).map(rail => rail.attributes.get('data-home-rail')), ['continue']);
  assert.equal(sort.disabled, true);
  assertCatalogSortVisibility(document, true);

  document.nodes.get('nav-continue').click();
  await settleBrowser();
  assertCatalogSortVisibility(document, false);
  assert.equal(sort.disabled, true);
  assert.equal(document.nodes.get('catalog-sort-label').textContent, 'Sort (Title)');

  document.nodes.get('nav-home').click();
  await settleBrowser();
  assert.equal(sort.disabled, true);
  assertCatalogSortVisibility(document, true);
});

test('home omits empty Unplayed and Recently added rails and See all opens those walls', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const unplayedGame = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const recentlyAddedGame = availableGame('snes-actraiser-test', 'ActRaiser', { system: 'snes' });
  const weekendGame = availableGame('megadrive-streets-test', 'Streets', { system: 'megadrive' });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
    homeRails: {
      continue: jsonResponse({ games: [continueGame] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
      unplayed: jsonResponse({ games: [unplayedGame] }),
      recently_added: jsonResponse({ games: [recentlyAddedGame] }),
      'weekend-queue': jsonResponse({ games: [weekendGame] }),
    },
  });
  await settleBrowser();
  const rails = homeRails(document);
  assert.deepEqual(rails.map(rail => rail.attributes.get('data-home-rail')), [
    'continue', 'unplayed', 'recently_added', 'weekend-queue',
  ]);
  const homeCalls = calls.filter(call => String(call.path).startsWith('/api/v1/games'));
  assert.ok(homeCalls.some(call => call.path.includes('collection=unplayed') && call.path.includes('grouped=1') && call.path.includes('limit=12')));
  assert.ok(homeCalls.some(call => call.path.includes('collection=recently_added') && call.path.includes('grouped=1') && call.path.includes('limit=12')));

  const unplayedSeeAll = rails.find(rail => rail.attributes.get('data-home-rail') === 'unplayed')
    .querySelectorAll('.home-rail-see-all')[0];
  unplayedSeeAll.click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  assert.equal(document.nodes.get('nav-unplayed').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), false);
  assert.equal(document.nodes.get('catalog-layout').hidden, false);
  assert.ok(gameCards(document).length > 0);

  document.nodes.get('nav-home').click();
  await settleBrowser();
  const homeAgain = homeRails(document);
  const recentlyAddedSeeAll = homeAgain.find(rail => rail.attributes.get('data-home-rail') === 'recently_added')
    .querySelectorAll('.home-rail-see-all')[0];
  recentlyAddedSeeAll.click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  assert.equal(document.nodes.get('nav-recently-added').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-unplayed').className.includes('selected'), false);
  assert.equal(document.nodes.get('catalog-layout').hidden, false);
});

test('home keyboard moves along and between rails and Enter opens detail not launch', async () => {
  const continueGames = [
    availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' }),
    availableGame('megadrive-streets-test', 'Streets', { system: 'megadrive' }),
  ];
  const favoriteGames = [
    availableGame('snes-mario-test', 'Mario', { system: 'snes' }),
  ];
  const { document, calls } = await runKeyboardApp({
    pages: [{ games: continueGames }],
    railPages: {
      continue: continueGames,
      favorites: favoriteGames,
      recents: [],
    },
    collections: [],
  });
  await settleBrowser();
  const originalFetch = calls.slice();
  void originalFetch;
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'home');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-sonic-test');

  await pressKey(document, 'ArrowRight');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-streets-test');
  assert.ok((document.activeElement.scrollIntoViewCalls || []).some(options => (
    options && options.block === 'nearest' && options.inline === 'nearest'
  )));
  await pressKey(document, 'Home');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-sonic-test');
  await pressKey(document, 'End');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-streets-test');
  await pressKey(document, 'ArrowLeft');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-sonic-test');

  await pressKey(document, 'ArrowDown');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-mario-test');
  await pressKey(document, 'ArrowUp');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-sonic-test');

  const beforeLaunch = calls.filter(call => call.path === '/api/v1/session/launch').length;
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
  assert.equal(document.activeElement.id, 'launch-game');
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, beforeLaunch);
  await pressKey(document, 'Escape');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'home');
  await pressKey(document, 'Escape');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
});

test('home keyboard Up and Down move across Unplayed and Recently added rails', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const unplayedGame = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const recentlyAddedGame = availableGame('snes-actraiser-test', 'ActRaiser', { system: 'snes' });
  const { document } = await runKeyboardApp({
    pages: [{ games: [continueGame] }],
    railPages: {
      continue: [continueGame],
      favorites: [],
      recents: [],
      unplayed: [unplayedGame],
      recently_added: [recentlyAddedGame],
    },
    collections: [],
  });
  await settleBrowser();
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'home');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'continue');

  await pressKey(document, 'ArrowDown');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'nes-zelda-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'unplayed');
  await pressKey(document, 'ArrowDown');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-actraiser-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'recently_added');
  await pressKey(document, 'ArrowUp');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'nes-zelda-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'unplayed');
  await pressKey(document, 'ArrowUp');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'continue');
});

test('setLibraryNav home loads rails and never invents a /home API', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games?collection=continue': [jsonResponse({ games: [availableGame('megadrive-sonic-test', 'Sonic')] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [availableGame('snes-mario-test', 'Mario')] })],
    '/api/v1/games?collection=unplayed': [jsonResponse({ games: [availableGame('nes-zelda-test', 'Zelda')] })],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [availableGame('snes-actraiser-test', 'ActRaiser')] })],
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.setLibraryNav('home', '');
  const state = controller.getState();
  assert.equal(state.libraryView, 'home');
  assert.deepEqual(state.homeRails.map(rail => rail.id), [
    'continue', 'recents', 'unplayed', 'recently_added',
  ]);
  assert.equal(state.homeRails[0].gameViews.length, 1);
  assert.equal(calls.some(call => String(call.path).includes('/home')), false);
  assert.ok(calls.some(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12'));
  assert.ok(calls.some(call => call.path === '/api/v1/games?collection=unplayed&grouped=1&limit=12'));
  assert.ok(calls.some(call => call.path === '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1&limit=12'));
  await controller.setLibraryNav('', '');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(controller.getState().collection, '');
});

test('home platform filter stays unapplied and clears the platform chrome', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [],
    homeRails: {
      continue: jsonResponse({ games: [sonic] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const filter = document.nodes.get('filter-system');
  filter.value = 'snes';
  const change = filter.listeners.get('change');
  assert.equal(typeof change, 'function');
  const before = calls.length;
  await change();
  await settleBrowser();
  assert.equal(filter.value, '');
  assert.equal(calls.slice(before).some(call => String(call.path).includes('platform=')), false);
  const homeCalls = calls.filter(call => String(call.path).startsWith('/api/v1/games'));
  assert.ok(homeCalls.length > 0);
  assert.equal(homeCalls.every(call => !String(call.path).includes('platform=')), true);
});

test('home keyboard keeps focus on the target rail when the same game appears twice', async () => {
  const shared = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const { document } = await runKeyboardApp({
    pages: [{ games: [shared] }],
    railPages: {
      continue: [shared],
      favorites: [shared],
      recents: [],
    },
    collections: [],
  });
  await settleBrowser();
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  assert.equal(document.activeElement.attributes.get('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'continue');

  await pressKey(document, 'ArrowDown');
  assert.equal(document.activeElement.attributes.get('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'favorites');
  const selectedOnHome = gameCards(document).filter(card => String(card.className || '').includes('selected'));
  assert.equal(selectedOnHome.length, 1);
  assert.equal(selectedOnHome[0].parentNode.getAttribute('data-home-track'), 'favorites');

  await pressKey(document, 'ArrowUp');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'continue');
});

test('reopening home keeps the focused cover and detail target on the same game', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const mario = availableGame('snes-mario-test', 'Mario', { system: 'snes' });
  const streets = availableGame('megadrive-streets-test', 'Streets', { system: 'megadrive' });
  const { document } = await runKeyboardApp({
    pages: [{ games: [sonic, mario, streets] }],
    railPages: {
      continue: [sonic],
      favorites: [mario],
      recents: [],
    },
    collections: [],
  });
  await settleBrowser();
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  await pressKey(document, 'ArrowDown');
  assert.equal(document.activeElement.attributes.get('data-game-id'), 'snes-mario-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'favorites');

  await openAllGamesGrid(document);
  const sonicCard = gameCards(document).find(card => card.attributes.get('data-game-id') === 'megadrive-sonic-test');
  assert.ok(sonicCard);
  sonicCard.click();
  await settleBrowser();

  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'home');
  assert.equal(document.activeElement.attributes.get('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'continue');
  const selectedOnHome = gameCards(document).filter(card => String(card.className || '').includes('selected'));
  assert.equal(selectedOnHome.length, 1);
  assert.equal(selectedOnHome[0].attributes.get('data-game-id'), 'megadrive-sonic-test');

  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
  assert.match(document.nodes.get('detail-content').children.map(node => node.textContent).join(' '), /Sonic/);

  await pressKey(document, 'Escape');
  await openAllGamesGrid(document);
  const streetsCard = gameCards(document).find(card => card.attributes.get('data-game-id') === 'megadrive-streets-test');
  assert.ok(streetsCard);
  streetsCard.click();
  await settleBrowser();
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  assert.equal(document.activeElement.attributes.get('data-game-id'), 'megadrive-sonic-test');
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
  assert.match(document.nodes.get('detail-content').children.map(node => node.textContent).join(' '), /Sonic/);
});

test('custom collection id home opens the grid while Home chrome stays the aggregate view', async () => {
  const shelfGame = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse({ games: [shelfGame] })],
    collections: [{ id: 'home', name: 'Home Shelf' }],
    homeRails: {
      continue: jsonResponse({ games: [] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
      home: jsonResponse({ games: [shelfGame] }),
    },
  });
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  assert.deepEqual(homeRails(document).map(rail => rail.attributes.get('data-home-rail')), ['home']);
  const shelf = document.nodes.get('nav-collection-home');
  assert.ok(shelf);
  shelf.click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), false);
  assert.equal(document.nodes.get('nav-collection-home').className.includes('selected'), true);
  assert.ok(calls.some(call => /[?&]collection=home\b/.test(String(call.path))));

  document.nodes.get('nav-home').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-collection-home').className.includes('selected'), false);
});

test('setLibraryNav home opens a custom collection id home as the grid', async () => {
  const shelfGame = availableGame('megadrive-sonic-test', 'Sonic');
  const { fetchImpl } = routedFetch({
    '/api/v1/library/collections': [jsonResponse({ collections: [{ id: 'home', name: 'Home Shelf' }] })],
    '/api/v1/platforms': [jsonResponse({ platforms: [] })],
    '/api/v1/library/attract?limit=1': [jsonResponse({ items: [], idle_seconds: 60 })],
    '/api/v1/library/facets': [jsonResponse({ genres: [], years: [] })],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=home': [
      jsonResponse({ games: [shelfGame] }),
      jsonResponse({ games: [shelfGame] }),
      jsonResponse({ games: [shelfGame] }),
    ],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadPlatforms();
  assert.equal(controller.getState().libraryView, 'home');
  assert.equal(controller.getState().collection, '');
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['home']);
  await controller.setLibraryNav('home', '');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(controller.getState().collection, 'home');
  await controller.openHome();
  assert.equal(controller.getState().libraryView, 'home');
  assert.equal(controller.getState().collection, '');
});

test('home custom rails skip empty collections before applying the cap of 6', async () => {
  const weekendGame = availableGame('megadrive-streets-test', 'Streets', { system: 'megadrive' });
  const collections = [];
  const homeRailsByID = {
    continue: jsonResponse({ games: [] }),
    favorites: jsonResponse({ games: [] }),
    recents: jsonResponse({ games: [] }),
  };
  for (let index = 1; index <= 6; index += 1) {
    const id = `empty-${index}`;
    collections.push({ id, name: `Empty ${index}` });
    homeRailsByID[id] = jsonResponse({ games: [] });
  }
  collections.push({ id: 'weekend-queue', name: 'Weekend Queue' });
  homeRailsByID['weekend-queue'] = jsonResponse({ games: [weekendGame] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [],
    collections,
    homeRails: homeRailsByID,
  });
  await settleBrowser();
  assert.deepEqual(homeRails(document).map(rail => rail.attributes.get('data-home-rail')), ['weekend-queue']);
  assert.ok(calls.some(call => String(call.path).includes('collection=weekend-queue')));
  assert.ok(calls.some(call => String(call.path).includes('collection=empty-1')));
});

test('home keeps at most 6 nonempty custom rails', async () => {
  const collections = [];
  const homeRailsByID = {
    continue: jsonResponse({ games: [] }),
    favorites: jsonResponse({ games: [] }),
    recents: jsonResponse({ games: [] }),
  };
  for (let index = 1; index <= 7; index += 1) {
    const id = `shelf-${index}`;
    collections.push({ id, name: `Shelf ${index}` });
    homeRailsByID[id] = jsonResponse({
      games: [availableGame(`megadrive-game-${index}`, `Game ${index}`, { system: 'megadrive' })],
    });
  }
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [],
    collections,
    homeRails: homeRailsByID,
  });
  await settleBrowser();
  assert.deepEqual(homeRails(document).map(rail => rail.attributes.get('data-home-rail')), [
    'shelf-1', 'shelf-2', 'shelf-3', 'shelf-4', 'shelf-5', 'shelf-6',
  ]);
  assert.equal(calls.some(call => String(call.path).includes('collection=shelf-7')), false);
});

test('home favorite toggle refetches the favorites rail', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { favorite: true });
  const { fetchImpl } = routedFetch({
    '/api/v1/games?collection=continue': [jsonResponse({ games: [sonic] })],
    '/api/v1/games?collection=favorites': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] })],
    '/api/v1/library/favorites/megadrive-sonic-test': [
      jsonResponse({ favorite: false }),
      jsonResponse({ favorite: true }),
    ],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.openHome();
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['continue', 'favorites']);
  await controller.toggleFavorite('megadrive-sonic-test');
  assert.equal(controller.getState().games[0].favorite, false);
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['continue']);
  await controller.toggleFavorite('megadrive-sonic-test');
  assert.equal(controller.getState().games[0].favorite, true);
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['continue', 'favorites']);
  assert.equal(controller.getState().homeRails[1].gameViews[0].live.id, 'megadrive-sonic-test');
});

test('home launch confirmed active removes the title from the Unplayed rail', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['unplayed']);
  assert.deepEqual(controller.getState().homeRails[0].gameViews.map(view => view.live.id), [
    'megadrive-sonic-test', 'nes-zelda-test',
  ]);
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.launchState, 'launch_success');
  assert.match(state.launchMessage, /launch_success/);
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.equal(state.selectedGameView && state.selectedGameView.live.id, 'megadrive-sonic-test');
  const unplayed = state.homeRails.find(rail => rail.id === 'unplayed');
  assert.ok(unplayed);
  assert.deepEqual(unplayed.gameViews.map(view => view.live.id), ['nes-zelda-test']);
  assert.equal(state.sessionGameTitle, 'Sonic');
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=unplayed&grouped=1&limit=12').length,
    2,
  );
});

test('home launch confirmed active keeps selection when the title remains on another rail', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [sonic] }), jsonResponse({ games: [sonic] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['continue', 'unplayed']);
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.launchState, 'launch_success');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.deepEqual(state.homeRails.map(rail => rail.id), ['continue']);
  assert.equal(state.homeRails[0].gameViews[0].live.id, 'megadrive-sonic-test');
});

test('home launch confirmed active keeps the launched variant when an unplayed sibling remains', async () => {
  const groupKey = 'megadrive\u001fsonic';
  const sonicUsa = availableGame('megadrive-sonic-test', 'Sonic', {
    system: 'megadrive',
    group_key: groupKey,
  });
  const sonicJp = availableGame('megadrive-sonic-jp', 'Sonic (Japan)', {
    system: 'megadrive',
    group_key: groupKey,
  });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonicUsa)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonicUsa, sonicJp] }),
      jsonResponse({ games: [sonicJp] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  assert.deepEqual(controller.getState().homeRails[0].gameViews.map(view => view.live.id), [
    'megadrive-sonic-test', 'megadrive-sonic-jp',
  ]);
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.launchState, 'launch_success');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.equal(state.selectedGameView && state.selectedGameView.live.id, 'megadrive-sonic-test');
  const unplayed = state.homeRails.find(rail => rail.id === 'unplayed');
  assert.ok(unplayed);
  assert.deepEqual(unplayed.gameViews.map(view => view.live.id), ['megadrive-sonic-jp']);
});

test('older Unplayed reconcile does not overwrite a newer overlapping refetch', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  let releaseFirstUnplayed;
  let firstUnplayedStarted;
  const holdFirstUnplayed = new Promise(resolve => { releaseFirstUnplayed = resolve; });
  const firstUnplayedFetch = new Promise(resolve => { firstUnplayedStarted = resolve; });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'nes-zelda-test', system: 'nes' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'nes-zelda-test', system: 'nes' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games/nes-zelda-test': [jsonResponse(zelda)],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      () => {
        firstUnplayedStarted();
        return holdFirstUnplayed.then(() => jsonResponse({ games: [zelda] }));
      },
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  const firstLaunch = controller.launchSelected();
  await firstUnplayedFetch;
  await controller.selectGame('nes-zelda-test');
  await controller.launchSelected();
  const afterNewer = controller.getState();
  assert.equal(afterNewer.session.game_id, 'nes-zelda-test');
  const newerUnplayed = afterNewer.homeRails.find(rail => rail.id === 'unplayed');
  assert.equal(newerUnplayed, undefined);
  releaseFirstUnplayed();
  await firstLaunch;
  const state = controller.getState();
  const unplayed = state.homeRails.find(rail => rail.id === 'unplayed');
  assert.equal(unplayed, undefined);
  assert.equal(
    (state.homeRails || []).some(rail => rail.gameViews.some(view => view.live.id === 'nes-zelda-test')),
    false,
  );
});

test('in-flight Home load does not restore a title after a newer Unplayed reconcile', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const queued = availableGame('snes-actraiser-test', 'ActRaiser');
  let releaseCustom;
  let customStarted;
  const holdCustom = new Promise(resolve => { releaseCustom = resolve; });
  const customFetch = new Promise(resolve => { customStarted = resolve; });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/platforms': [jsonResponse({ platforms: [] })],
    '/api/v1/library/attract?limit=1': [jsonResponse({ items: [], idle_seconds: 60 })],
    '/api/v1/library/facets': [jsonResponse({ genres: [], years: [] })],
    '/api/v1/library/collections': [jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }] })],
    '/api/v1/games': [jsonResponse({ games: [sonic] })],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=weekend-queue': [
      () => {
        customStarted();
        return holdCustom.then(() => jsonResponse({ games: [queued] }));
      },
    ],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.loadCatalog('');
  await controller.selectGame('megadrive-sonic-test');
  const homeLoad = controller.loadPlatforms();
  await customFetch;
  await controller.launchSelected();
  const afterReconcile = controller.getState();
  assert.equal(afterReconcile.launchState, 'launch_success');
  assert.equal(afterReconcile.homeRails.some(rail => rail.id === 'unplayed'), false);
  releaseCustom();
  await homeLoad;
  const state = controller.getState();
  assert.equal(state.catalogState, 'populated');
  assert.equal(state.launchState, 'launch_success');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.equal(state.sessionGameTitle, 'Sonic');
  assert.equal(state.homeRails.some(rail => rail.id === 'unplayed'), false);
  assert.equal(
    (state.homeRails || []).some(rail => rail.gameViews.some(view => view.live.id === 'megadrive-sonic-test')),
    false,
  );
  const weekend = state.homeRails.find(rail => rail.id === 'weekend-queue');
  assert.ok(weekend);
  assert.equal(weekend.gameViews[0].live.id, 'snes-actraiser-test');
});

test('user-initiated Home reload after launch_success drops an absent title', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const empty = jsonResponse({ games: [] });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [zelda] }),
      jsonResponse({ games: [zelda] }),
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=favorites': [empty, empty],
    '/api/v1/games?collection=recents': [empty, empty, empty],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [empty, empty],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  assert.equal(controller.getState().launchState, 'launch_success');
  assert.equal(controller.getState().selectedLiveGame && controller.getState().selectedLiveGame.id, 'megadrive-sonic-test');
  await controller.setCatalogSort('year');
  const state = controller.getState();
  assert.equal(state.libraryView, 'home');
  assert.equal(state.selectedLiveGame, null);
  assert.equal(state.launchState, 'idle');
  assert.equal(state.homeRails.some(rail => rail.gameViews.some(view => view.live.id === 'megadrive-sonic-test')), false);
  assert.equal(state.homeRails.find(rail => rail.id === 'continue').gameViews[0].live.id, 'nes-zelda-test');
});

test('mid-launch Home year filter jumps to All games and still keeps the session title', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const empty = jsonResponse({ games: [] });
  let releaseLaunchResponse;
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [() => new Promise(resolve => { releaseLaunchResponse = resolve; })],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games': [jsonResponse({ games: [zelda] })],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=favorites': [empty],
    '/api/v1/games?collection=recents': [empty, empty],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [empty],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  const launch = controller.launchSelected();
  assert.equal(controller.getState().activeMutation, 'launch');
  await controller.setCatalogFilter('year', '1991');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(controller.getState().collection, '');
  assert.equal(controller.getState().selectedLiveGame, null);
  assert.equal(controller.getState().games.some(game => game.id === 'megadrive-sonic-test'), false);
  const allGames = calls.filter(call => String(call.path).startsWith('/api/v1/games') && !String(call.path).includes('collection=')).at(-1);
  assert.ok(allGames);
  assert.equal(allGames.path.includes('year=1991'), true);
  releaseLaunchResponse(jsonResponse(sessionFixture({
    state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive',
  })));
  await launch;
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame, null);
  assert.equal(state.sessionGameTitle, 'Sonic');
});

test('home launch confirmed active keeps the session title after Unplayed-only removal', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.launchState, 'launch_success');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.equal(state.games.some(game => game.id === 'megadrive-sonic-test'), false);
  assert.equal(state.homeRails.some(rail => rail.id === 'unplayed'), false);
  assert.equal(state.sessionGameTitle, 'Sonic');
});

test('home launch confirmed active drops Unplayed after mid-launch selection change', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  let releaseLaunchResponse;
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [() => new Promise(resolve => { releaseLaunchResponse = resolve; })],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games/nes-zelda-test': [jsonResponse(zelda)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  const launch = controller.launchSelected();
  assert.equal(controller.getState().activeMutation, 'launch');
  await controller.selectGame('nes-zelda-test');
  releaseLaunchResponse(jsonResponse(sessionFixture({
    state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive',
  })));
  await launch;
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'nes-zelda-test');
  assert.equal(state.launchState, 'idle');
  const unplayed = state.homeRails.find(rail => rail.id === 'unplayed');
  assert.ok(unplayed);
  assert.deepEqual(unplayed.gameViews.map(view => view.live.id), ['nes-zelda-test']);
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=unplayed&grouped=1&limit=12').length,
    2,
  );
});

test('active session title survives selecting another Home card after Unplayed-only removal', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games/nes-zelda-test': [jsonResponse(zelda)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [zelda] }), jsonResponse({ games: [zelda] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  assert.equal(controller.getState().sessionGameTitle, 'Sonic');
  await controller.selectGame('nes-zelda-test');
  const state = controller.getState();
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'nes-zelda-test');
  assert.equal(state.games.some(game => game.id === 'megadrive-sonic-test'), false);
  assert.equal(state.sessionGameTitle, 'Sonic');
});

test('authoritative active session still drops Unplayed after a malformed launch POST', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [jsonResponse({})],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.launchState, 'launch_error');
  assert.equal(state.launchError && state.launchError.code, 'MALFORMED_RESPONSE');
  const unplayed = state.homeRails.find(rail => rail.id === 'unplayed');
  assert.ok(unplayed);
  assert.deepEqual(unplayed.gameViews.map(view => view.live.id), ['nes-zelda-test']);
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=unplayed&grouped=1&limit=12').length,
    2,
  );
});

test('home launch confirmed active adds the title to Continue and Recent', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['unplayed']);
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.launchState, 'launch_success');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'megadrive-sonic-test');
  assert.deepEqual(state.homeRails.map(rail => rail.id), ['continue', 'recents', 'unplayed']);
  assert.deepEqual(state.homeRails.find(rail => rail.id === 'continue').gameViews.map(view => view.live.id), [
    'megadrive-sonic-test',
  ]);
  assert.deepEqual(state.homeRails.find(rail => rail.id === 'recents').gameViews.map(view => view.live.id), [
    'megadrive-sonic-test',
  ]);
  assert.deepEqual(state.homeRails.find(rail => rail.id === 'unplayed').gameViews.map(view => view.live.id), [
    'nes-zelda-test',
  ]);
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12').length,
    2,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=recents&grouped=1&limit=12').length,
    2,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=unplayed&grouped=1&limit=12').length,
    2,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=favorites&grouped=1&limit=12').length,
    1,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1&limit=12').length,
    1,
  );
});

test('home launch confirmed active updates Continue and Recent after mid-launch selection change', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  let releaseLaunchResponse;
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [() => new Promise(resolve => { releaseLaunchResponse = resolve; })],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games/nes-zelda-test': [jsonResponse(zelda)],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  const launch = controller.launchSelected();
  assert.equal(controller.getState().activeMutation, 'launch');
  await controller.selectGame('nes-zelda-test');
  releaseLaunchResponse(jsonResponse(sessionFixture({
    state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive',
  })));
  await launch;
  const state = controller.getState();
  assert.equal(state.sessionPhase, 'active');
  assert.equal(state.session.game_id, 'megadrive-sonic-test');
  assert.equal(state.selectedLiveGame && state.selectedLiveGame.id, 'nes-zelda-test');
  assert.equal(state.launchState, 'idle');
  assert.deepEqual(state.homeRails.map(rail => rail.id), ['continue', 'recents', 'unplayed']);
  assert.deepEqual(state.homeRails.find(rail => rail.id === 'continue').gameViews.map(view => view.live.id), [
    'megadrive-sonic-test',
  ]);
  assert.deepEqual(state.homeRails.find(rail => rail.id === 'recents').gameViews.map(view => view.live.id), [
    'megadrive-sonic-test',
  ]);
  assert.deepEqual(state.homeRails.find(rail => rail.id === 'unplayed').gameViews.map(view => view.live.id), [
    'nes-zelda-test',
  ]);
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12').length,
    2,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=recents&grouped=1&limit=12').length,
    2,
  );
});

test('home launch confirmed active leaves Favorites and custom rails unreconciled', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive', favorite: true });
  const queued = availableGame('snes-actraiser-test', 'ActRaiser');
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
    ],
    '/api/v1/platforms': [jsonResponse({ platforms: [] })],
    '/api/v1/library/attract?limit=1': [jsonResponse({ items: [], idle_seconds: 60 })],
    '/api/v1/library/facets': [jsonResponse({ genres: [], years: [] })],
    '/api/v1/library/collections': [jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }] })],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [sonic] })],
    '/api/v1/games?collection=recents': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=unplayed': [jsonResponse({ games: [] }), jsonResponse({ games: [] })],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=weekend-queue': [jsonResponse({ games: [queued] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.loadPlatforms();
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), [
    'favorites', 'weekend-queue',
  ]);
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  const state = controller.getState();
  assert.equal(state.launchState, 'launch_success');
  assert.deepEqual(state.homeRails.map(rail => rail.id), [
    'continue', 'favorites', 'recents', 'weekend-queue',
  ]);
  assert.equal(state.homeRails.find(rail => rail.id === 'favorites').gameViews[0].live.id, 'megadrive-sonic-test');
  assert.equal(state.homeRails.find(rail => rail.id === 'weekend-queue').gameViews[0].live.id, 'snes-actraiser-test');
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=favorites&grouped=1&limit=12').length,
    1,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=weekend-queue&grouped=1&limit=12').length,
    1,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1&limit=12').length,
    1,
  );
});

test('older Continue reconcile does not overwrite a newer overlapping refetch', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  let releaseFirstContinue;
  let firstContinueStarted;
  const holdFirstContinue = new Promise(resolve => { releaseFirstContinue = resolve; });
  const firstContinueFetch = new Promise(resolve => { firstContinueStarted = resolve; });
  const { fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'nes-zelda-test', system: 'nes' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'nes-zelda-test', system: 'nes' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games/nes-zelda-test': [jsonResponse(zelda)],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [] }),
      () => {
        firstContinueStarted();
        return holdFirstContinue.then(() => jsonResponse({ games: [sonic] }));
      },
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [zelda] }),
      jsonResponse({ games: [zelda] }),
    ],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      jsonResponse({ games: [zelda] }),
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  const firstLaunch = controller.launchSelected();
  await firstContinueFetch;
  await controller.selectGame('nes-zelda-test');
  await controller.launchSelected();
  const afterNewer = controller.getState();
  assert.equal(afterNewer.session.game_id, 'nes-zelda-test');
  assert.deepEqual(
    afterNewer.homeRails.find(rail => rail.id === 'continue').gameViews.map(view => view.live.id),
    ['nes-zelda-test'],
  );
  releaseFirstContinue();
  await firstLaunch;
  const state = controller.getState();
  assert.deepEqual(
    state.homeRails.find(rail => rail.id === 'continue').gameViews.map(view => view.live.id),
    ['nes-zelda-test'],
  );
  assert.deepEqual(
    state.homeRails.find(rail => rail.id === 'recents').gameViews.map(view => view.live.id),
    ['nes-zelda-test'],
  );
});

test('older Home launch rail batch cannot overwrite newer Continue and Recent', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { system: 'megadrive' });
  const zelda = availableGame('nes-zelda-test', 'Zelda', { system: 'nes' });
  let releaseFirstUnplayed;
  let firstUnplayedStarted;
  const holdFirstUnplayed = new Promise(resolve => { releaseFirstUnplayed = resolve; });
  const firstUnplayedFetch = new Promise(resolve => { firstUnplayedStarted = resolve; });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/session': [
      jsonResponse(sessionFixture({ state: 'idle' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'nes-zelda-test', system: 'nes' })),
    ],
    '/api/v1/session/launch': [
      jsonResponse(sessionFixture({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' })),
      jsonResponse(sessionFixture({ state: 'active', game_id: 'nes-zelda-test', system: 'nes' })),
    ],
    '/api/v1/games/megadrive-sonic-test': [jsonResponse(sonic)],
    '/api/v1/games/nes-zelda-test': [jsonResponse(zelda)],
    '/api/v1/games?collection=continue': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [zelda] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [zelda] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/games?collection=unplayed': [
      jsonResponse({ games: [sonic, zelda] }),
      () => {
        firstUnplayedStarted();
        return holdFirstUnplayed.then(() => jsonResponse({ games: [zelda] }));
      },
      jsonResponse({ games: [] }),
    ],
    '/api/v1/games?collection=recently_added': [jsonResponse({ games: [] })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadSession();
  await controller.openHome();
  await controller.selectGame('megadrive-sonic-test');
  const firstLaunch = controller.launchSelected();
  await firstUnplayedFetch;
  await controller.selectGame('nes-zelda-test');
  await controller.launchSelected();
  const afterNewer = controller.getState();
  assert.equal(afterNewer.session.game_id, 'nes-zelda-test');
  assert.deepEqual(
    afterNewer.homeRails.find(rail => rail.id === 'continue').gameViews.map(view => view.live.id),
    ['nes-zelda-test'],
  );
  assert.deepEqual(
    afterNewer.homeRails.find(rail => rail.id === 'recents').gameViews.map(view => view.live.id),
    ['nes-zelda-test'],
  );
  releaseFirstUnplayed();
  await firstLaunch;
  const state = controller.getState();
  assert.equal(state.session.game_id, 'nes-zelda-test');
  assert.deepEqual(
    state.homeRails.find(rail => rail.id === 'continue').gameViews.map(view => view.live.id),
    ['nes-zelda-test'],
  );
  assert.deepEqual(
    state.homeRails.find(rail => rail.id === 'recents').gameViews.map(view => view.live.id),
    ['nes-zelda-test'],
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12').length,
    2,
  );
  assert.equal(
    calls.filter(call => call.path === '/api/v1/games?collection=recents&grouped=1&limit=12').length,
    2,
  );
});

test('recently added Home rail ignores year and system catalog sort', async () => {
  const recent = availableGame('snes-actraiser-test', 'ActRaiser');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [recent] });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [populated, populated, populated, populated],
    '/api/v1/games?collection=continue': [empty, empty],
    '/api/v1/games?collection=favorites': [empty, empty],
    '/api/v1/games?collection=recents': [empty, empty],
    '/api/v1/games?collection=recently_added': [populated, populated],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.setLibraryNav('', '');
  await controller.setCatalogSort('year');
  await controller.openHome();
  const afterYear = calls.filter(call => String(call.path).includes('collection=recently_added')).at(-1);
  assert.ok(afterYear);
  assert.equal(afterYear.path, '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1&limit=12');
  assert.equal(afterYear.path.includes('sort=year'), false);

  await controller.setLibraryNav('', '');
  await controller.setCatalogSort('system');
  await controller.openHome();
  const afterSystem = calls.filter(call => String(call.path).includes('collection=recently_added')).at(-1);
  assert.ok(afterSystem);
  assert.equal(afterSystem.path, '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1&limit=12');
  assert.equal(afterSystem.path.includes('sort=platform'), false);
});

test('Home rails and collection See-alls ignore Year/System while All-games keeps the preference', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const recent = availableGame('snes-actraiser-test', 'ActRaiser');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const added = jsonResponse({ games: [recent] });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/platforms': [jsonResponse({ platforms: [] })],
    '/api/v1/library/attract?limit=1': [jsonResponse({ items: [], idle_seconds: 60 })],
    '/api/v1/library/facets': [jsonResponse({ genres: [], years: [] })],
    '/api/v1/library/collections': [jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }] })],
    '/api/v1/games': [populated, populated, populated, populated, populated],
    '/api/v1/games?collection=continue': [empty, empty, empty, empty],
    '/api/v1/games?collection=favorites': [empty, empty, empty, empty],
    '/api/v1/games?collection=recents': [empty, empty, empty, empty],
    '/api/v1/games?collection=unplayed': [empty, empty, empty, empty],
    '/api/v1/games?collection=recently_added': [added, added, added, added],
    '/api/v1/games?collection=weekend-queue': [populated, populated, populated, populated],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.setLibraryNav('', '');
  await controller.loadPlatforms();
  await controller.setCatalogSort('year');
  assert.equal(controller.getState().sort, 'year');
  const allYear = calls.filter(call => !String(call.path).includes('collection=')).at(-1);
  assert.ok(allYear);
  assert.equal(allYear.path, '/api/v1/games?sort=year&grouped=1');

  await controller.openHome();
  const homeAfterYear = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12'));
  const homeAfterYearSlice = homeAfterYear.slice(-6);
  assert.ok(homeAfterYearSlice.some(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12'));
  assert.ok(homeAfterYearSlice.some(call => call.path === '/api/v1/games?collection=favorites&grouped=1&limit=12'));
  assert.ok(homeAfterYearSlice.some(call => call.path === '/api/v1/games?collection=recents&grouped=1&limit=12'));
  assert.ok(homeAfterYearSlice.some(call => call.path === '/api/v1/games?collection=unplayed&grouped=1&limit=12'));
  assert.ok(homeAfterYearSlice.some(call => call.path === '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1&limit=12'));
  assert.ok(homeAfterYearSlice.some(call => call.path === '/api/v1/games?collection=weekend-queue&grouped=1&limit=12'));
  assert.equal(homeAfterYearSlice.every(call => !call.path.includes('sort=year') && !call.path.includes('sort=platform')), true);
  assert.equal(controller.getState().sort, 'year');

  await controller.setLibraryNav('recently_added', '');
  const seeAll = calls.filter(call => String(call.path).includes('collection=recently_added') && !String(call.path).includes('limit=12')).at(-1);
  assert.ok(seeAll);
  assert.equal(seeAll.path, '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1');
  assert.equal(seeAll.path.includes('sort=year'), false);
  assert.equal(controller.getState().sort, 'year');
  assert.equal(controller.getState().collection, 'recently_added');
  assert.equal(controller.getState().libraryView, 'grid');

  const seeAllYearPaths = {
    continue: '/api/v1/games?collection=continue&grouped=1',
    favorites: '/api/v1/games?collection=favorites&grouped=1',
    recents: '/api/v1/games?collection=recents&grouped=1',
    unplayed: '/api/v1/games?collection=unplayed&grouped=1',
    'weekend-queue': '/api/v1/games?collection=weekend-queue&grouped=1',
  };
  for (const [collection, path] of Object.entries(seeAllYearPaths)) {
    await controller.setLibraryNav(collection, '');
    const seeAllCall = calls.filter(call => String(call.path).includes(`collection=${collection}`) && !String(call.path).includes('limit=12')).at(-1);
    assert.ok(seeAllCall, collection);
    assert.equal(seeAllCall.path, path);
    assert.equal(seeAllCall.path.includes('sort=year'), false);
    assert.equal(seeAllCall.path.includes('sort=platform'), false);
    assert.equal(controller.getState().sort, 'year', collection);
    assert.equal(controller.getState().collection, collection);
    assert.equal(controller.getState().libraryView, 'grid');
  }

  await controller.setLibraryNav('', '');
  const allAgain = calls.filter(call => !String(call.path).includes('collection=')).at(-1);
  assert.ok(allAgain);
  assert.equal(allAgain.path, '/api/v1/games?sort=year&grouped=1');
  assert.equal(controller.getState().sort, 'year');

  await controller.setCatalogSort('system');
  await controller.openHome();
  const homeAfterSystem = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12')).slice(-6);
  assert.ok(homeAfterSystem.some(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12'));
  assert.ok(homeAfterSystem.some(call => call.path === '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1&limit=12'));
  assert.ok(homeAfterSystem.some(call => call.path === '/api/v1/games?collection=weekend-queue&grouped=1&limit=12'));
  assert.equal(homeAfterSystem.every(call => !call.path.includes('sort=platform') && !call.path.includes('sort=year')), true);
  assert.equal(controller.getState().sort, 'system');

  await controller.setLibraryNav('recently_added', '');
  const seeAllSystem = calls.filter(call => String(call.path).includes('collection=recently_added') && !String(call.path).includes('limit=12')).at(-1);
  assert.equal(seeAllSystem.path, '/api/v1/games?collection=recently_added&sort=recently_added&grouped=1');
  assert.equal(seeAllSystem.path.includes('sort=platform'), false);
  assert.equal(controller.getState().sort, 'system');

  await controller.setLibraryNav('favorites', '');
  const favoritesSeeAllSystem = calls.filter(call => String(call.path).includes('collection=favorites') && !String(call.path).includes('limit=12')).at(-1);
  assert.equal(favoritesSeeAllSystem.path, '/api/v1/games?collection=favorites&grouped=1');
  assert.equal(favoritesSeeAllSystem.path.includes('sort=platform'), false);
  assert.equal(controller.getState().sort, 'system');

  await controller.setLibraryNav('weekend-queue', '');
  const customSeeAllSystem = calls.filter(call => String(call.path).includes('collection=weekend-queue') && !String(call.path).includes('limit=12')).at(-1);
  assert.equal(customSeeAllSystem.path, '/api/v1/games?collection=weekend-queue&grouped=1');
  assert.equal(customSeeAllSystem.path.includes('sort=platform'), false);
  assert.equal(controller.getState().sort, 'system');

  await controller.setLibraryNav('', '');
  const allSystem = calls.filter(call => !String(call.path).includes('collection=')).at(-1);
  assert.equal(allSystem.path, '/api/v1/games?sort=platform&grouped=1');
  assert.equal(controller.getState().sort, 'system');
});

test('Home rails omit stored search and browse extras while keeping hide-betas', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [populated, populated, populated],
    '/api/v1/games?q=sonic': [populated, populated, populated, populated],
    '/api/v1/games?collection=continue': [populated, populated, populated, populated],
    '/api/v1/games?collection=favorites': [populated, populated, populated, populated, populated],
    '/api/v1/games?collection=recents': [empty, empty, empty, empty],
    '/api/v1/library/favorites/megadrive-sonic-test': [jsonResponse({ favorite: false })],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.setLibraryNav('', '');
  await controller.reloadVisibleCatalog('sonic');
  await controller.setCatalogFilter('region', 'japan');
  await controller.setCatalogFilter('year', '1991');
  await controller.setCatalogFilter('availability', 'online');
  assert.equal(controller.getState().query, 'sonic');
  assert.equal(controller.getState().filters.region, 'japan');
  assert.equal(controller.getState().filters.year, '1991');
  assert.equal(controller.getState().filters.availability, 'online');

  await controller.openHome();
  const homeAfterStored = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12')).slice(-5);
  assert.ok(homeAfterStored.some(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12'));
  assert.ok(homeAfterStored.some(call => call.path === '/api/v1/games?collection=favorites&grouped=1&limit=12'));
  assert.equal(homeAfterStored.every(call => !call.path.includes('q=') && !call.path.includes('region=') && !call.path.includes('year=') && !call.path.includes('availability=') && !call.path.includes('genre=') && !call.path.includes('platform=')), true);
  assert.equal(controller.getState().query, 'sonic');
  assert.equal(controller.getState().filters.region, 'japan');
  assert.equal(controller.getState().filters.year, '1991');
  assert.equal(controller.getState().libraryView, 'home');

  await controller.toggleFavorite('megadrive-sonic-test');
  const favoritesReconcile = calls.filter(call => String(call.path).includes('collection=favorites') && String(call.path).includes('limit=12')).at(-1);
  assert.ok(favoritesReconcile);
  assert.equal(favoritesReconcile.path, '/api/v1/games?collection=favorites&grouped=1&limit=12');
  assert.equal(favoritesReconcile.path.includes('q='), false);
  assert.equal(favoritesReconcile.path.includes('region='), false);

  await controller.setCatalogFilter('hide_prerelease', true);
  assert.equal(controller.getState().libraryView, 'home');
  const hiddenHome = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12')).slice(-5);
  assert.ok(hiddenHome.some(call => call.path === '/api/v1/games?collection=continue&hide_prerelease=1&grouped=1&limit=12'));
  assert.ok(hiddenHome.some(call => call.path === '/api/v1/games?collection=favorites&hide_prerelease=1&grouped=1&limit=12'));
  assert.equal(hiddenHome.every(call => call.path.includes('hide_prerelease=1') && !call.path.includes('q=') && !call.path.includes('region=') && !call.path.includes('year=')), true);
});

test('Home search and browse filters jump to All games and do not filter rails in place', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [populated, populated, populated],
    '/api/v1/games?q=sonic': [populated, populated],
    '/api/v1/games?collection=continue': [populated, populated, populated, populated],
    '/api/v1/games?collection=favorites': [empty, empty, empty],
    '/api/v1/games?collection=recents': [empty, empty, empty],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.openHome();
  assert.equal(controller.getState().libraryView, 'home');

  await controller.searchVisibleCatalog('sonic');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(controller.getState().collection, '');
  assert.equal(controller.getState().query, 'sonic');
  const searchCall = calls.filter(call => String(call.path).startsWith('/api/v1/games')).at(-1);
  assert.equal(searchCall.path, '/api/v1/games?q=sonic&grouped=1');
  assert.equal(searchCall.path.includes('collection='), false);

  await controller.openHome();
  assert.equal(controller.getState().libraryView, 'home');
  const homeAfterSearch = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12')).slice(-5);
  assert.equal(homeAfterSearch.every(call => !call.path.includes('q=')), true);

  await controller.setCatalogFilter('genre', 'Platformer');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(controller.getState().collection, '');
  assert.equal(controller.getState().filters.genre, 'Platformer');
  const genreCall = calls.filter(call => String(call.path).startsWith('/api/v1/games') && !String(call.path).includes('collection=')).at(-1);
  assert.equal(genreCall.path, '/api/v1/games?q=sonic&genre=Platformer&grouped=1');

  await controller.setLibraryNav('continue', '');
  const seeAll = calls.filter(call => String(call.path).includes('collection=continue') && !String(call.path).includes('limit=12')).at(-1);
  assert.ok(seeAll);
  assert.equal(seeAll.path.includes('q=sonic'), true);
  assert.equal(seeAll.path.includes('genre=Platformer'), true);

  await controller.openHome();
  assert.equal(controller.getState().libraryView, 'home');
  assert.equal(controller.getState().filters.genre, 'Platformer');
  const homeAfterGenre = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12')).slice(-5);
  assert.ok(homeAfterGenre.some(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12'));
  assert.equal(homeAfterGenre.every(call => !call.path.includes('genre=') && !call.path.includes('q=')), true);
});

test('settings-save on Home with leftover query stays on Home rails', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/library/settings': [
      jsonResponse({ attract_idle_seconds: 60, preferred_regions: ['usa'] }),
      jsonResponse({ attract_idle_seconds: 12, preferred_regions: ['japan'] }),
    ],
    '/api/v1/games': [populated],
    '/api/v1/games?q=sonic': [populated],
    '/api/v1/games?collection=continue': [populated, populated, populated],
    '/api/v1/games?collection=favorites': [empty, empty, empty],
    '/api/v1/games?collection=recents': [empty, empty, empty],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.setLibraryNav('', '');
  await controller.reloadVisibleCatalog('sonic');
  await controller.openHome();
  assert.equal(controller.getState().libraryView, 'home');
  assert.equal(controller.getState().query, 'sonic');

  const before = calls.length;
  await controller.saveSettings({ attract_idle_seconds: 12, preferred_regions: ['japan'] });
  assert.equal(controller.getState().libraryView, 'home');
  assert.equal(controller.getState().collection, '');
  assert.equal(controller.getState().query, 'sonic');
  const after = calls.slice(before);
  assert.equal(after.some(call => String(call.path).startsWith('/api/v1/games') && String(call.path).includes('q=')), false);
  assert.equal(after.some(call => String(call.path).startsWith('/api/v1/games') && !String(call.path).includes('collection=')), false);
  assert.ok(after.some(call => call.path === '/api/v1/games?collection=continue&grouped=1&limit=12'));

  await controller.reloadVisibleCatalog(controller.getState().query);
  assert.equal(controller.getState().libraryView, 'home');
  const leftoverReload = calls.filter(call => String(call.path).startsWith('/api/v1/games')).at(-1);
  assert.equal(leftoverReload.path.includes('collection='), true);
  assert.equal(leftoverReload.path.includes('q='), false);
});

test('empty Home after a retained search query is empty not no_matches', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const { fetchImpl } = routedFetch({
    '/api/v1/games': [populated],
    '/api/v1/games?q=sonic': [empty],
    '/api/v1/games?collection=continue': [empty, empty],
    '/api/v1/games?collection=favorites': [empty, empty],
    '/api/v1/games?collection=recents': [empty, empty],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.setLibraryNav('', '');
  await controller.searchVisibleCatalog('sonic');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(catalogViewState(controller.getState()), 'no_matches');
  await controller.openHome();
  const state = controller.getState();
  assert.equal(state.libraryView, 'home');
  assert.equal(state.query, 'sonic');
  assert.equal(catalogViewState(state), 'empty');
});

test('pending search debounce does not jump back after navigating to Home', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const empty = jsonResponse({ games: [] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse({ games: [sonic] })],
    homeRails: {
      continue: jsonResponse({ games: [sonic] }),
      favorites: empty,
      recents: empty,
    },
  });
  await settleBrowser();
  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  const beforeSearch = calls.filter(call => String(call.path).includes('q=')).length;
  document.nodes.get('game-search').value = 'sonic';
  document.nodes.get('game-search').dispatchEvent({ type: 'input', bubbles: true });
  document.nodes.get('nav-home').click();
  await settleBrowser();
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), false);
  assert.equal(calls.filter(call => String(call.path).includes('q=')).length, beforeSearch);
});

test('mid-edit search then All uses the current input not stale state.query', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated, populated, populated],
    homeRails: {
      continue: jsonResponse({ games: [sonic] }),
      favorites: empty,
      recents: empty,
    },
  });
  await settleBrowser();
  document.nodes.get('nav-all').click();
  await settleBrowser();
  document.nodes.get('game-search').value = 'sonic';
  document.nodes.get('game-search').dispatchEvent({ type: 'input', bubbles: true });
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  const committed = calls.filter(call => String(call.path).includes('q=sonic')).at(-1);
  assert.ok(committed);
  document.nodes.get('game-search').value = '';
  document.nodes.get('game-search').dispatchEvent({ type: 'input', bubbles: true });
  document.nodes.get('nav-all').click();
  await settleBrowser();
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  const afterClear = calls.filter(call => String(call.path).startsWith('/api/v1/games') && !String(call.path).includes('collection=')).at(-1);
  assert.ok(afterClear);
  assert.equal(afterClear.path.includes('q='), false);
  assert.equal(document.nodes.get('game-search').value, '');
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), true);
});

test('mid-edit search then Home restores the stored query and stays on unfiltered rails', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated, populated],
    homeRails: {
      continue: jsonResponse({ games: [sonic] }),
      favorites: empty,
      recents: empty,
    },
  });
  await settleBrowser();
  document.nodes.get('nav-all').click();
  await settleBrowser();
  document.nodes.get('game-search').value = 'sonic';
  document.nodes.get('game-search').dispatchEvent({ type: 'input', bubbles: true });
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  document.nodes.get('game-search').value = 'tails';
  document.nodes.get('game-search').dispatchEvent({ type: 'input', bubbles: true });
  document.nodes.get('nav-home').click();
  await settleBrowser();
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), true);
  assert.equal(document.nodes.get('game-search').value, 'sonic');
  assert.equal(calls.some(call => String(call.path).includes('q=tails')), false);
  const homeAfter = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12')).slice(-3);
  assert.equal(homeAfter.every(call => !call.path.includes('q=')), true);
});

test('type-to-search from a focused Home card jumps to All games', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const { document, calls } = await runKeyboardApp({
    pages: [{ games: [continueGame] }],
    railPages: {
      continue: [continueGame],
      favorites: [],
      recents: [],
    },
    collections: [],
  });
  await settleBrowser();
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'home');
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'continue');
  const beforeRails = calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('q=')).length;

  await pressKey(document, 's');
  assert.equal(document.activeElement, document.nodes.get('game-search'));
  assert.equal(document.nodes.get('game-search').value, 's');
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), false);
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  const searchCall = calls.filter(call => String(call.path).startsWith('/api/v1/games') && !String(call.path).includes('collection=')).at(-1);
  assert.ok(searchCall);
  assert.match(searchCall.path, /[?&]q=s/);
  assert.equal(searchCall.path.includes('collection='), false);
  assert.equal(
    calls.filter(call => String(call.path).includes('collection=') && String(call.path).includes('q=')).length,
    beforeRails,
  );
});

test('home collection toggle refetches the affected custom rail', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { collections: ['weekend-queue'] });
  const { fetchImpl } = routedFetch({
    '/api/v1/platforms': [jsonResponse({ platforms: [] })],
    '/api/v1/library/attract?limit=1': [jsonResponse({ items: [], idle_seconds: 60 })],
    '/api/v1/library/facets': [jsonResponse({ genres: [], years: [] })],
    '/api/v1/library/collections': [jsonResponse({ collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }] })],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [sonic] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=weekend-queue': [
      jsonResponse({ games: [sonic] }),
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/library/collections/weekend-queue/megadrive-sonic-test': [
      jsonResponse({ member: false }),
      jsonResponse({ member: true }),
    ],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadPlatforms();
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), ['continue', 'weekend-queue']);
  await controller.toggleCollectionMember('weekend-queue', 'megadrive-sonic-test');
  assert.equal(controller.getState().homeRails.some(rail => rail.id === 'weekend-queue'), false);
  await controller.toggleCollectionMember('weekend-queue', 'megadrive-sonic-test');
  const weekend = controller.getState().homeRails.find(rail => rail.id === 'weekend-queue');
  assert.ok(weekend);
  assert.equal(weekend.gameViews[0].live.id, 'megadrive-sonic-test');
});

test('home membership toggles emit the game update before the rail refetch', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  let releaseFavorites;
  const holdFavorites = new Promise(resolve => { releaseFavorites = resolve; });
  const seen = [];
  const { fetchImpl } = routedFetch({
    '/api/v1/games?collection=continue': [jsonResponse({ games: [sonic] })],
    '/api/v1/games?collection=favorites': [
      jsonResponse({ games: [] }),
      () => holdFavorites.then(() => jsonResponse({ games: [sonic] })),
    ],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] })],
    '/api/v1/library/favorites/megadrive-sonic-test': [jsonResponse({ favorite: true })],
  });
  const controller = createAppController({
    fetchImpl,
    metadataAdapter: FogCastMetadata,
    onStateChange(next) { seen.push(next); },
  });
  await controller.openHome();
  const afterOpen = seen.length;
  const pending = controller.toggleFavorite('megadrive-sonic-test');
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(controller.getState().games[0].favorite, true);
  assert.ok(seen.slice(afterOpen).some(item => item.games[0] && item.games[0].favorite === true));
  assert.equal(controller.getState().homeRails.some(rail => rail.id === 'favorites'), false);
  releaseFavorites();
  await pending;
  assert.equal(controller.getState().homeRails.some(rail => rail.id === 'favorites'), true);
});

test('home inserts an earlier nonempty custom rail and trims to 6', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic');
  const collections = [{ id: 'early-shelf', name: 'Early Shelf' }];
  const routes = {
    '/api/v1/platforms': [jsonResponse({ platforms: [] })],
    '/api/v1/library/attract?limit=1': [jsonResponse({ items: [], idle_seconds: 60 })],
    '/api/v1/library/facets': [jsonResponse({ genres: [], years: [] })],
    '/api/v1/library/collections': [jsonResponse({ collections })],
    '/api/v1/games?collection=continue': [jsonResponse({ games: [sonic] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=early-shelf': [
      jsonResponse({ games: [] }),
      jsonResponse({ games: [sonic] }),
    ],
    '/api/v1/library/collections/early-shelf/megadrive-sonic-test': [jsonResponse({ member: true })],
  };
  for (let index = 1; index <= 6; index += 1) {
    const id = `shelf-${index}`;
    collections.push({ id, name: `Shelf ${index}` });
    routes[`/api/v1/games?collection=${id}`] = [jsonResponse({
      games: [availableGame(`megadrive-game-${index}`, `Game ${index}`)],
    })];
  }
  routes['/api/v1/library/collections'] = [jsonResponse({ collections })];
  const { fetchImpl } = routedFetch(routes);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadPlatforms();
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), [
    'continue', 'shelf-1', 'shelf-2', 'shelf-3', 'shelf-4', 'shelf-5', 'shelf-6',
  ]);
  await controller.toggleCollectionMember('early-shelf', 'megadrive-sonic-test');
  assert.deepEqual(controller.getState().homeRails.map(rail => rail.id), [
    'continue', 'early-shelf', 'shelf-1', 'shelf-2', 'shelf-3', 'shelf-4', 'shelf-5',
  ]);
});

test('home partial rail failures do not use the empty-home copy', async () => {
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [],
    homeRails: {
      continue: jsonResponse({ games: [] }),
      favorites: jsonResponse({ error: { message: 'unavailable' } }, 500),
      recents: jsonResponse({ error: { message: 'unavailable' } }, 500),
    },
  });
  await settleBrowser();
  const message = document.nodes.get('catalog-list').children[0];
  assert.ok(message);
  assert.equal(message.textContent, 'The catalog could not be loaded.');
  assert.notEqual(message.textContent, 'Home has no Continue, Favorites, Recent, or collection titles yet.');
  const { fetchImpl } = routedFetch({
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ error: { message: 'unavailable' } }, 500)],
    '/api/v1/games?collection=recents': [jsonResponse({ error: { message: 'unavailable' } }, 500)],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.openHome();
  const state = controller.getState();
  assert.equal(state.catalogState, 'catalog_error');
  assert.equal(catalogViewState(state), 'catalog_error');
  assert.ok(state.homeRailFailures > 0);
});

test('empty All games is honest that scan is internal CLI not a host API', async () => {
  const empty = jsonResponse({ games: [] });
  const { document } = await runBrowserApp({
    responses: [empty],
  });
  await settleBrowser();
  const message = document.nodes.get('catalog-list').children[0];
  assert.ok(message);
  assert.match(message.textContent, /The library is empty/);
  assert.match(message.textContent, /internal CLI/);
  assert.doesNotMatch(message.textContent, /\/api\/v1\/.*scan/i);
  const retry = document.nodes.get('catalog-actions').children[0];
  assert.equal(retry.textContent, 'Reload catalog');
});

test('empty home uses home-specific copy instead of claiming the library is empty', async () => {
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    homeRails: {
      continue: jsonResponse({ games: [] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const message = document.nodes.get('catalog-list').children[0];
  assert.ok(message);
  assert.equal(message.textContent, 'Home has no Continue, Favorites, Recent, or collection titles yet.');
  assert.notEqual(message.textContent, 'The library is empty.');
});

test('empty Home after a prior search shows Home empty copy not no-matches', async () => {
  const empty = jsonResponse({ games: [] });
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [empty, empty],
    homeRails: {
      continue: empty,
      favorites: empty,
      recents: empty,
    },
  });
  await settleBrowser();
  document.nodes.get('nav-all').click();
  await settleBrowser();
  document.nodes.get('game-search').value = 'sonic';
  document.nodes.get('game-search').dispatchEvent({ type: 'input', bubbles: true });
  await new Promise(resolve => setTimeout(resolve, 200));
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').children[0].textContent, 'No matching games.');
  document.nodes.get('nav-home').click();
  await settleBrowser();
  const message = document.nodes.get('catalog-list').children[0];
  assert.ok(message);
  assert.equal(message.textContent, 'Home has no Continue, Favorites, Recent, or collection titles yet.');
  assert.notEqual(message.textContent, 'No matching games.');
});

test('catalog layout defaults to Cover and toggles to List without a new GET', async () => {
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games?collection=continue': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=favorites': [jsonResponse({ games: [] })],
    '/api/v1/games?collection=recents': [jsonResponse({ games: [] })],
    '/api/v1/games': [jsonResponse(readFixture('catalog-populated.json'))],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  assert.equal(controller.getState().catalogLayout, 'cover');
  await controller.setLibraryNav('', '');
  const before = calls.length;
  const next = controller.setCatalogLayout('list');
  assert.equal(next.catalogLayout, 'list');
  assert.equal(next.libraryView, 'grid');
  assert.equal(calls.length, before);
  await controller.openHome();
  assert.equal(controller.getState().libraryView, 'home');
  assert.equal(controller.getState().catalogLayout, 'list');
  await controller.setLibraryNav('continue', '');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(controller.getState().catalogLayout, 'list');
  const fresh = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  assert.equal(fresh.getState().catalogLayout, 'cover');
});

test('Cover is the default wall and List restyles the same game-card window', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    favorite: true,
    variant_count: 3,
  });
  const { document, calls } = await runBrowserApp({
    responses: [jsonResponse({ games: [game] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-layout').hidden, false);
  assert.equal(document.nodes.get('layout-cover').attributes.get('aria-pressed'), 'true');
  assert.equal(document.nodes.get('layout-list').attributes.get('aria-pressed'), 'false');
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  const coverCard = gameCards(document)[0];
  assert.equal(coverCard.className.includes('game-card'), true);
  assert.equal(coverCard.className.includes('game-row'), false);
  const before = calls.filter(call => String(call.path).startsWith('/api/v1/games')).length;
  document.nodes.get('layout-list').click();
  await settleBrowser();
  assert.equal(calls.filter(call => String(call.path).startsWith('/api/v1/games')).length, before);
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  assert.equal(document.nodes.get('layout-cover').attributes.get('aria-pressed'), 'false');
  assert.equal(document.nodes.get('layout-list').attributes.get('aria-pressed'), 'true');
  const row = gameCards(document)[0];
  assert.ok(row.className.includes('game-card'));
  assert.ok(row.className.includes('game-row'));
  assert.equal(row.attributes.get('data-game-id'), 'snes-actraiser-test');
  assert.equal(row.tagName, 'BUTTON');
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.ok(body);
  assert.equal(body.children[0].textContent, 'ActRaiser');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · Ready');
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[2].textContent, 'Favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children[3].textContent, '3 versions');
});

test('Home hides Cover List and keeps rails while remembering the layout', async () => {
  const continueGame = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    homeRails: {
      continue: jsonResponse({ games: [continueGame] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  assert.equal(document.nodes.get('catalog-layout').hidden, true);
  assert.equal(homeRails(document).length, 1);
  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-layout').hidden, false);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  document.nodes.get('nav-home').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  assert.equal(document.nodes.get('catalog-layout').hidden, true);
  assert.ok(gameCards(document).every(card => !String(card.className).includes('game-row')));
  document.nodes.get('nav-all').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  assert.equal(document.nodes.get('layout-list').attributes.get('aria-pressed'), 'true');
});

test('All Continue custom and platform views honor the remembered Cover List layout', async () => {
  const game = availableGame('snes-unknown-test', 'Unknown Game', { system: 'snes' });
  const { document } = await runBrowserApp({
    responses: [
      jsonResponse({ games: [game] }),
      jsonResponse({ games: [game] }),
    ],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
    platforms: [{ id: 'snes', label: 'Super NES', game_count: 1, online: true, launchable: true }],
    homeRails: {
      continue: jsonResponse({ games: [game] }),
      'weekend-queue': jsonResponse({ games: [game] }),
    },
  });
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  document.nodes.get('layout-list').click();
  await settleBrowser();
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), true);
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  document.nodes.get('nav-continue').click();
  await settleBrowser();
  assert.equal(document.nodes.get('nav-continue').className.includes('selected'), true);
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  document.nodes.get('collection-list').children[0].click();
  await settleBrowser();
  assert.equal(document.nodes.get('collection-list').children[0].className.includes('selected'), true);
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  document.nodes.get('platform-list').children[0].click();
  await settleBrowser();
  assert.equal(document.nodes.get('platform-list').children[0].className.includes('selected'), true);
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  document.nodes.get('layout-cover').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
});

test('list arrows move one row, Left returns to the rail, and Enter opens detail', async () => {
  const games = [];
  for (let index = 0; index < 8; index += 1) {
    games.push(availableGame(`snes-grid-${index}`, `Grid ${index}`));
  }
  const { document, calls } = await runKeyboardApp({ pages: [{ games }] });
  await settleBrowser();
  document.nodes.get('catalog-list').clientWidth = 896;
  await openAllGamesGrid(document);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-0');
  await pressKey(document, 'ArrowDown');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-1');
  assert.ok((document.activeElement.scrollIntoViewCalls || []).some(options => (
    options && options.block === 'nearest'
  )));
  await pressKey(document, 'ArrowUp');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-0');
  await pressKey(document, 'ArrowRight');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-0');
  await pressKey(document, 'ArrowLeft');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
  assert.equal(document.activeElement.className.includes('nav-item'), true);
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');
  const beforeLaunch = calls.filter(call => call.path === '/api/v1/session/launch').length;
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
  assert.equal(document.activeElement.id, 'launch-game');
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, beforeLaunch);
});

test('reloading the bound app resets Cover List to Cover', async () => {
  const game = availableGame('snes-unknown-test', 'Unknown Game');
  const first = await runBrowserApp({
    responses: [jsonResponse({ games: [game] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  first.document.nodes.get('layout-list').click();
  await settleBrowser();
  assert.equal(first.document.nodes.get('catalog-list').className, 'game-list');
  const second = await runBrowserApp({
    responses: [jsonResponse({ games: [game] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  assert.equal(second.document.nodes.get('catalog-list').className, 'game-grid');
  assert.equal(second.document.nodes.get('layout-cover').attributes.get('aria-pressed'), 'true');
});

test('list wall spacers use the CSS row-stride token', async () => {
  const games = [];
  for (let index = 0; index < 90; index += 1) {
    games.push(availableGame(`snes-game-${index}`, `Title ${index}`));
  }
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /height:\s*var\(--list-row-height/);
  const fallback = await runBrowserApp({
    responses: [jsonResponse({ games })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  fallback.document.nodes.get('layout-list').click();
  await settleBrowser();
  const fallbackSpacer = fallback.document.nodes.get('catalog-list').children
    .find(child => child.attributes.get('data-wall-spacer') === 'end');
  assert.ok(fallbackSpacer);
  assert.equal(fallbackSpacer.style.height, '780px');

  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    globals: {
      getComputedStyle() {
        return {
          getPropertyValue(name) {
            if (name === '--list-row-stride') return '90px';
            return '';
          },
        };
      },
    },
  });
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const spacer = document.nodes.get('catalog-list').children
    .find(child => child.attributes.get('data-wall-spacer') === 'end');
  assert.ok(spacer);
  assert.equal(spacer.style.height, '900px');
});

test('Enter on Cover List activates the focused toggle instead of the rail', async () => {
  const { document, calls } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  await pressKey(document, 'Escape');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  const layoutList = document.nodes.get('layout-list');
  layoutList.focus();
  const before = calls.filter(call => String(call.path).startsWith('/api/v1/games')).length;
  const enter = await pressKey(document, 'Enter', layoutList);
  assert.equal(enter.defaultPrevented, undefined);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'rail');
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), true);
  assert.equal(calls.filter(call => String(call.path).startsWith('/api/v1/games')).length, before);
  layoutList.click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-list');
  assert.equal(document.nodes.get('layout-list').attributes.get('aria-pressed'), 'true');
});

test('list layout CSS keeps rows compact without overlaying cover-card titles', () => {
  const css = readAsset('ui.css');
  assert.match(css, /\.game-list/);
  assert.match(css, /\.game-card\.game-row/);
  assert.match(css, /\.game-row-favorite/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
});

function gameActionsMenu(document) {
  return document.getElementById('game-actions-menu') || document.nodes.get('game-actions-menu');
}

function gameActionsItems(document) {
  const menu = gameActionsMenu(document);
  return menu && menu.children ? Array.from(menu.children) : [];
}

async function openCardActions(document, card, via = 'contextmenu') {
  card.focus();
  if (via === 'contextmenu') {
    const event = {
      type: 'contextmenu',
      target: card,
      clientX: 24,
      clientY: 36,
      preventDefault() { event.defaultPrevented = true; },
    };
    card.dispatchEvent(event);
    await settleBrowser();
    return event;
  }
  if (via === 'ContextMenu') return pressKey(document, 'ContextMenu', card);
  return pressKey(document, 'F10', card, { shiftKey: true });
}

test('right-click ContextMenu and Shift+F10 open game actions on cover list and Home cards', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', {
    system: 'megadrive',
    favorite: false,
    collections: [],
  });
  const { document } = await runKeyboardApp({
    pages: [{ games: [sonic] }],
    railPages: {
      continue: [sonic],
      favorites: [],
      recents: [],
    },
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  await settleBrowser();
  await waitForCondition(
    () => document.nodes.get('collection-list').children.length === 1,
    'collection rail did not render',
  );
  await openAllGamesGrid(document);
  const cover = gameCards(document)[0];
  assert.ok(cover);
  const rightClick = await openCardActions(document, cover, 'contextmenu');
  assert.equal(rightClick.defaultPrevented, true);
  const menu = gameActionsMenu(document);
  assert.equal(menu.hidden, false);
  assert.deepEqual(gameActionsItems(document).map(item => item.textContent), [
    'Play',
    'Favorite',
    'Add to Weekend Queue',
  ]);
  assert.equal(document.activeElement.id, 'game-action-play');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');

  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  assert.ok(row.className.includes('game-row'));
  await openCardActions(document, row, 'ContextMenu');
  assert.equal(gameActionsMenu(document).hidden, false);
  assert.equal(document.activeElement.id, 'game-action-play');

  document.nodes.get('nav-home').click();
  await settleBrowser();
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  const homeCard = gameCards(document)[0];
  assert.ok(homeCard);
  await openCardActions(document, homeCard, 'Shift+F10');
  assert.equal(gameActionsMenu(document).hidden, false);
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'home');
  assert.deepEqual(gameActionsItems(document).map(item => item.textContent), [
    'Play',
    'Favorite',
    'Add to Weekend Queue',
  ]);
});

test('game actions Favorite and collection rows call existing controller methods', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', {
    system: 'megadrive',
    favorite: false,
    collections: [],
  });
  const { document, calls } = await runKeyboardApp({
    pages: [{ games: [sonic] }],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  await settleBrowser();
  await waitForCondition(
    () => document.nodes.get('collection-list').children.length === 1,
    'collection rail did not render',
  );
  await openAllGamesGrid(document);
  await openCardActions(document, gameCards(document)[0]);
  const beforeLaunch = calls.filter(call => call.path === '/api/v1/session/launch').length;
  document.getElementById('game-action-favorite').click();
  await settleBrowser();
  const favoriteWrites = calls.filter(call => String(call.path).startsWith('/api/v1/library/favorites/'));
  assert.equal(favoriteWrites.length, 1);
  assert.equal(favoriteWrites[0].path, '/api/v1/library/favorites/megadrive-sonic-test');
  assert.equal(favoriteWrites[0].options.method, 'PUT');
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, beforeLaunch);
  assert.equal(gameActionsMenu(document).hidden, true);

  const favorited = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', {
    system: 'megadrive',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const second = await runKeyboardApp({
    pages: [{ games: [favorited] }],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  await settleBrowser();
  await openAllGamesGrid(second.document);
  await openCardActions(second.document, gameCards(second.document)[0]);
  assert.deepEqual(gameActionsItems(second.document).map(item => item.textContent), [
    'Play',
    'Unfavorite',
    'Remove from Weekend Queue',
  ]);
  second.document.getElementById('game-action-favorite').click();
  await settleBrowser();
  const unfavorite = second.calls.filter(call => String(call.path).startsWith('/api/v1/library/favorites/'));
  assert.equal(unfavorite[0].options.method, 'DELETE');

  await openCardActions(second.document, gameCards(second.document)[0]);
  second.document.getElementById('game-action-collection-weekend-queue').click();
  await settleBrowser();
  const membership = second.calls.filter(call => String(call.path).includes('/api/v1/library/collections/weekend-queue/'));
  assert.equal(membership.length, 1);
  assert.equal(membership[0].path, '/api/v1/library/collections/weekend-queue/megadrive-sonic-test');
  assert.equal(membership[0].options.method, 'DELETE');
});

test('Enter on a focused game action commits and does not launch or change nav', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', {
    system: 'megadrive',
    favorite: false,
    collections: [],
  });
  const { document, calls } = await runKeyboardApp({
    pages: [{ games: [sonic] }],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  await settleBrowser();
  await waitForCondition(
    () => document.nodes.get('collection-list').children.length === 1,
    'collection rail did not render',
  );
  await openAllGamesGrid(document);
  await openCardActions(document, gameCards(document)[0]);
  const collectionItem = document.getElementById('game-action-collection-weekend-queue');
  collectionItem.focus();
  const beforeLaunch = calls.filter(call => call.path === '/api/v1/session/launch').length;
  const enter = await pressKey(document, 'Enter', collectionItem);
  assert.equal(enter.defaultPrevented, true);
  const membership = calls.filter(call => String(call.path).includes('/api/v1/library/collections/weekend-queue/'));
  assert.equal(membership.length, 1);
  assert.equal(membership[0].options.method, 'PUT');
  assert.equal(calls.filter(call => call.path === '/api/v1/session/launch').length, beforeLaunch);
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), true);
  assert.equal(document.nodes.get('collection-list').children[0].className.includes('selected'), false);
  assert.equal(document.getElementById('favorite-game').textContent, 'Favorite');
});

test('Escape closes game actions and returns focus to the card', async () => {
  const { document } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  const card = selectedCard(document) || gameCards(document)[0];
  await openCardActions(document, card);
  assert.equal(gameActionsMenu(document).hidden, false);
  const escape = await pressKey(document, 'Escape', document.activeElement);
  assert.equal(escape.defaultPrevented, true);
  assert.equal(gameActionsMenu(document).hidden, true);
  assert.equal(document.activeElement.getAttribute('data-game-id'), card.getAttribute('data-game-id'));
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'grid');
});

test('opening game actions selects that card so Escape arrows and Enter stay on it', async () => {
  const games = [
    availableGame('snes-grid-0', 'Grid 0'),
    availableGame('snes-grid-1', 'Grid 1'),
    availableGame('snes-grid-2', 'Grid 2'),
  ];
  const { document } = await runKeyboardApp({ pages: [{ games }] });
  await settleBrowser();
  await openAllGamesGrid(document);
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-0');
  const second = gameCards(document)[1];
  await openCardActions(document, second);
  assert.equal(gameActionsMenu(document).hidden, false);
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-1');
  await pressKey(document, 'Escape');
  assert.equal(gameActionsMenu(document).hidden, true);
  assert.equal(document.activeElement.getAttribute('data-game-id'), 'snes-grid-1');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-1');
  await pressKey(document, 'ArrowRight');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-2');
  await pressKey(document, 'ArrowLeft');
  assert.equal(selectedCard(document).attributes.get('data-game-id'), 'snes-grid-1');
  await pressKey(document, 'Enter');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'detail');
  assert.match(browserText(document.nodes.get('detail-content')), /Grid 1/);
});

test('Escape restores the originating Home card when the same game is on two rails', async () => {
  const shared = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const { document } = await runKeyboardApp({
    pages: [{ games: [shared] }],
    railPages: {
      continue: [shared],
      favorites: [shared],
      recents: [],
    },
    collections: [],
  });
  await settleBrowser();
  document.nodes.get('nav-home').focus();
  await pressKey(document, 'Enter');
  await pressKey(document, 'ArrowDown');
  assert.equal(document.activeElement.getAttribute('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'favorites');
  await openCardActions(document, document.activeElement);
  assert.equal(gameActionsMenu(document).hidden, false);
  await pressKey(document, 'Escape');
  assert.equal(gameActionsMenu(document).hidden, true);
  assert.equal(document.activeElement.getAttribute('data-game-id'), 'megadrive-sonic-test');
  assert.equal(document.activeElement.parentNode.getAttribute('data-home-track'), 'favorites');
  const selectedOnHome = gameCards(document).filter(card => String(card.className || '').includes('selected'));
  assert.equal(selectedOnHome.length, 1);
  assert.equal(selectedOnHome[0].parentNode.getAttribute('data-home-track'), 'favorites');
});

test('game actions menu is clamped to the viewport', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const { document } = await runKeyboardApp({
    pages: [{ games: [sonic] }],
    globals: { window: { innerWidth: 400, innerHeight: 240 } },
  });
  await settleBrowser();
  await openAllGamesGrid(document);
  const menu = gameActionsMenu(document);
  menu.offsetWidth = 220;
  menu.offsetHeight = 180;
  const card = gameCards(document)[0];
  const event = {
    type: 'contextmenu',
    target: card,
    clientX: 360,
    clientY: 210,
    preventDefault() { event.defaultPrevented = true; },
  };
  card.dispatchEvent(event);
  await settleBrowser();
  assert.equal(menu.hidden, false);
  assert.equal(menu.style.left, '172px');
  assert.equal(menu.style.top, '52px');
});

test('Arrow Home and End keep the focused game action visible inside the menu', async () => {
  const collections = [];
  for (let index = 0; index < 12; index += 1) {
    collections.push({ id: `queue-${index}`, name: `Queue ${index}` });
  }
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const { document } = await runKeyboardApp({
    pages: [{ games: [sonic] }],
    collections,
  });
  await settleBrowser();
  await waitForCondition(
    () => document.nodes.get('collection-list').children.length === 12,
    'collection rail did not render',
  );
  await openAllGamesGrid(document);
  await openCardActions(document, gameCards(document)[0]);
  const menu = gameActionsMenu(document);
  const items = gameActionsItems(document);
  assert.equal(items.length, 14);
  menu.clientHeight = 80;
  menu.scrollTop = 0;
  items.forEach((item, index) => {
    item.offsetHeight = 40;
    item.offsetTop = index * 40;
  });
  const catalogScrolls = gameCards(document).map(card => (card.scrollIntoViewCalls || []).length);
  await pressKey(document, 'End');
  assert.equal(document.activeElement, items[13]);
  assert.equal(menu.scrollTop, 480);
  await pressKey(document, 'Home');
  assert.equal(document.activeElement, items[0]);
  assert.equal(menu.scrollTop, 0);
  await pressKey(document, 'ArrowDown');
  await pressKey(document, 'ArrowDown');
  assert.equal(document.activeElement, items[2]);
  assert.equal(menu.scrollTop, 40);
  assert.deepEqual(
    gameCards(document).map(card => (card.scrollIntoViewCalls || []).length),
    catalogScrolls,
  );
});

test('printable keys still type-to-search while game actions are open', async () => {
  const { document } = await runKeyboardApp();
  await settleBrowser();
  await openAllGamesGrid(document);
  await openCardActions(document, gameCards(document)[0]);
  assert.equal(gameActionsMenu(document).hidden, false);
  const typed = await pressKey(document, 'F', document.activeElement);
  assert.equal(typed.defaultPrevented, true);
  assert.equal(gameActionsMenu(document).hidden, true);
  assert.equal(document.nodes.get('game-search').value, 'F');
  assert.equal(document.nodes.get('launcher').attributes.get('data-keyboard-pane'), 'search');
});

test('one game actions menu closes on Settings Attract and nav change', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', { system: 'megadrive' });
  const mario = availableGame('snes-mario-test', 'Mario', { system: 'snes' });
  const { document } = await runKeyboardApp({
    pages: [{ games: [sonic, mario] }],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  await settleBrowser();
  await openAllGamesGrid(document);
  const cards = gameCards(document);
  await openCardActions(document, cards[0]);
  assert.equal(gameActionsMenu(document).hidden, false);
  await openCardActions(document, cards[1]);
  assert.equal(gameActionsMenu(document).hidden, false);
  assert.equal(gameActionsItems(document).length >= 1, true);
  assert.equal(document.getElementById('game-actions-menu').id, 'game-actions-menu');

  document.nodes.get('nav-favorites').click();
  await settleBrowser();
  assert.equal(gameActionsMenu(document).hidden, true);

  await openAllGamesGrid(document);
  await openCardActions(document, gameCards(document)[0]);
  await document.nodes.get('open-settings').click();
  await waitForCondition(() => document.nodes.get('settings').hidden === false, 'settings overlay did not open');
  assert.equal(gameActionsMenu(document).hidden, true);
  document.listeners.get('keydown')({ key: 'Escape', preventDefault() {} });
  await settleBrowser();

  const handle = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
  const attractApp = await runKeyboardApp({
    pages: [{ games: [sonic] }],
    globals: { FogCastAttractDisabled: false, FogCastAttractIdleMs: 250 },
    attractItems: [{ game_id: 'megadrive-sonic-test', title: 'Sonic the Hedgehog', cover: handle }],
  });
  await settleBrowser();
  await openAllGamesGrid(attractApp.document);
  await openCardActions(attractApp.document, gameCards(attractApp.document)[0]);
  assert.equal(gameActionsMenu(attractApp.document).hidden, false);
  await waitForAttractTitle(attractApp.document, 'Sonic the Hedgehog', 800);
  assert.equal(gameActionsMenu(attractApp.document).hidden, true);
});

test('detail Favorite and collection buttons still work after the wall menu', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', {
    system: 'megadrive',
    favorite: false,
    collections: [],
  });
  const { document, calls } = await runKeyboardApp({
    pages: [{ games: [sonic] }],
    collections: [{ id: 'weekend-queue', name: 'Weekend Queue' }],
  });
  await settleBrowser();
  await waitForCondition(
    () => document.nodes.get('collection-list').children.length === 1,
    'collection rail did not render',
  );
  await openAllGamesGrid(document);
  await openCardActions(document, gameCards(document)[0]);
  document.getElementById('game-action-favorite').click();
  await settleBrowser();
  const favorite = document.getElementById('favorite-game');
  assert.ok(favorite);
  favorite.click();
  await settleBrowser();
  const favoriteWrites = calls.filter(call => String(call.path).startsWith('/api/v1/library/favorites/'));
  assert.equal(favoriteWrites.length, 2);
  assert.equal(favoriteWrites[1].options.method, 'DELETE');
  const member = document.getElementById('collection-member-weekend-queue');
  assert.ok(member);
  member.click();
  await settleBrowser();
  const membership = calls.filter(call => String(call.path).includes('/api/v1/library/collections/weekend-queue/'));
  assert.equal(membership.length, 1);
  assert.equal(membership[0].options.method, 'PUT');
});

test('game actions menu CSS stays a compact overlay', () => {
  const css = readAsset('ui.css');
  assert.match(css, /\.game-actions-menu/);
  assert.match(css, /\.game-actions-item/);
  assert.match(css, /max-height:\s*min\(70vh,\s*calc\(100vh - 16px\)\)/);
  assert.match(css, /\.game-actions-menu[\s\S]*overflow:\s*auto/);
  const html = readAsset('ui_shell.html');
  assert.match(html, /id="game-actions-menu"/);
});

function detailFactsFrom(document) {
  const content = document.nodes.get('detail-content');
  const facts = (content.children || []).find(child => child.className === 'detail-facts');
  return facts
    ? facts.children.map(fact => ({
      value: fact.children[0] && fact.children[0].textContent,
      label: fact.children[1] && fact.children[1].textContent,
    }))
    : [];
}

function assertOmitsStagingAndExecution(facts) {
  assert.equal(facts.some(fact => fact.label === 'Staging'), false);
  assert.equal(facts.some(fact => fact.label === 'Content'), false);
  assert.equal(facts.some(fact => fact.value === 'Prepared' || fact.value === 'On demand'), false);
  assert.equal(facts.some(fact => fact.label === 'Execution' || /fpga_native/.test(String(fact.value))), false);
}

test('detail facts show dump identity and hide Version unless multiple variants', async () => {
  const dump = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog (USA) (Rev A) (Beta)', {
    system: 'megadrive',
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
  });
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [dump] }), jsonResponse(dump)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(document)[0].click();
  await settleBrowser();
  const facts = detailFactsFrom(document);
  assert.deepEqual(facts.filter(fact => ['Region', 'Revision', 'Flags'].includes(fact.label)), [
    { value: 'USA', label: 'Region' },
    { value: 'rev a', label: 'Revision' },
    { value: 'Beta', label: 'Flags' },
  ]);
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.equal(document.getElementById('game-version'), null);

  const single = {
    ...dump,
    variants: [dump],
  };
  const again = await runBrowserApp({
    responses: [jsonResponse({ games: [dump] }), jsonResponse(single)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(again.document)[0].click();
  await settleBrowser();
  assert.equal(again.document.getElementById('game-version'), null);

  const offline = availableGame('megadrive-offline-test', 'Streets of Rage 2 (USA)', {
    system: 'megadrive',
    state: 'available',
    root_online: false,
    region: 'usa',
  });
  const offlineApp = await runBrowserApp({
    responses: [jsonResponse({ games: [offline] }), jsonResponse(offline)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  assert.equal(
    gameCards(offlineApp.document)[0].children.find(child => child.className === 'game-meta').textContent,
    'Mega Drive · USA · Offline',
  );
  offlineApp.document.nodes.get('layout-list').click();
  await settleBrowser();
  const offlineRow = gameCards(offlineApp.document)[0];
  const offlineBody = offlineRow.children.find(child => String(child.className).includes('game-row-body'));
  assert.match(offlineBody.children[1].textContent, /Offline/);
  assert.doesNotMatch(offlineBody.children[1].textContent, /Ready/);
  await offlineRow.click();
  await settleBrowser();
  assert.ok(detailFactsFrom(offlineApp.document).some(fact => fact.label === 'Status' && fact.value === 'Offline'));

  const unreadable = availableGame('snes-invalid-test', 'Bad Dump', {
    state: 'invalid',
    root_online: true,
  });
  const unreadableApp = await runBrowserApp({
    responses: [jsonResponse({ games: [unreadable] }), jsonResponse(unreadable)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  assert.equal(
    gameCards(unreadableApp.document)[0].children.find(child => child.className === 'game-meta').textContent,
    'SNES · Unreadable',
  );
  unreadableApp.document.nodes.get('layout-list').click();
  await settleBrowser();
  const unreadableRow = gameCards(unreadableApp.document)[0];
  const unreadableBody = unreadableRow.children.find(child => String(child.className).includes('game-row-body'));
  assert.match(unreadableBody.children[1].textContent, /Unreadable/);
  assert.doesNotMatch(unreadableBody.children[1].textContent, /Offline/);
  assert.equal(cardMark(unreadableRow), null);
  await unreadableRow.click();
  await settleBrowser();
  assert.ok(detailFactsFrom(unreadableApp.document).some(fact => fact.label === 'Status' && fact.value === 'Unreadable'));

  const flagged = availableGame('snes-beta-hack-test', 'ActRaiser (USA) (Beta) (Hack)', {
    dump_flags: 'beta,hack',
    region: 'usa',
  });
  const flaggedApp = await runBrowserApp({
    responses: [jsonResponse({ games: [flagged] }), jsonResponse(flagged)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(flaggedApp.document)[0].click();
  await settleBrowser();
  const flaggedFacts = detailFactsFrom(flaggedApp.document);
  assert.ok(flaggedFacts.some(fact => fact.label === 'Flags' && fact.value === 'Beta, Hack'));
  assert.ok(flaggedFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.ok(flaggedFacts.some(fact => fact.label === 'Region' && fact.value === 'USA'));
});

test('multi-variant detail still labels #game-version with variantLabel', async () => {
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    region: 'japan',
  });
  const grouped = { ...usa, id: 'megadrive-sonic-usa', variant_count: 2 };
  const detail = { ...usa, variants: [usa, japan] };
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [grouped] }), jsonResponse(detail)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(document)[0].click();
  await settleBrowser();
  const select = document.getElementById('game-version');
  assert.ok(select);
  assert.equal(select.children[0].textContent, variantLabel(usa));
  assert.equal(select.children[1].textContent, variantLabel(japan));
  assert.equal(select.children[0].value, usa.id);
  assert.equal(select.children[1].value, japan.id);
  const facts = detailFactsFrom(document);
  assert.ok(facts.some(fact => fact.label === 'Region' && fact.value === 'USA'));
  assert.ok(facts.some(fact => fact.label === 'Revision' && fact.value === 'rev a'));
  assert.ok(facts.some(fact => fact.label === 'Flags' && fact.value === 'Beta'));
});

test('cover hover includes region and list rows do not add a dump line', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /\.game-card \.game-meta\s*\{[^}]*overflow:\s*hidden/);
  assert.match(css, /\.game-card \.game-meta\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(css, /\.game-card \.game-meta\s*\{[^}]*white-space:\s*nowrap/);
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    favorite: true,
    variant_count: 3,
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
  });
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [game] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  const coverMeta = cover.children.find(child => child.className === 'game-meta');
  assert.match(coverMeta.textContent, /usa/i);
  assert.equal(coverMeta.textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(coverMeta.textContent, /Beta|Hack|Action/);
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cover.getAttribute('data-unavailable'), null);
  assert.equal(cover.getAttribute('data-state'), 'available');
  assert.equal(cover.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children.length, 4);
  assert.equal(body.children[0].textContent, 'ActRaiser');
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · Beta · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[2].textContent, 'Favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children[3].textContent, '3 versions');
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(cardMark(row), null);
});

test('Cover and Home show non-hover favorite offline and playing marks', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(css, /\.card-mark\s*\{[^}]*opacity:\s*1/);
  assert.doesNotMatch(css, /\.card-mark[^{]*\{[^}]*opacity:\s*0/);
  assert.doesNotMatch(css, /\.game-card h3, \.game-card \.game-meta, \.game-card \.fallback-note, \.game-card \.card-mark/);
  assert.match(css, /\.game-card \.game-meta\s*\{[^}]*white-space:\s*nowrap/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /bits\.push\(coverStatusLabel\(game\)\)/);
  assert.match(app, /\[coverStatusLabel\(detailGame\), 'Status'\]/);
  assert.match(app, /coverHoverMeta\(game, view\)/);
  assert.match(app, /\(game && game\.year\) \|\| catalogYear\(view\)/);
  assert.match(app, /dumpFlagLabels\(game\)/);
  assert.match(app, /function dumpFlagLabels\(game\)/);
  assert.match(app, /dumpLabels\.forEach/);
  assert.doesNotMatch(app, /bits\.push\(sourceLabel\(game\.state\)\)/);
  assert.doesNotMatch(css, /\[data-dump/);
  assert.doesNotMatch(css, /card-mark-dump[^{]*\{[^}]*filter:/);
  assert.match(css, /\.card-mark-dump\s*\{/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);

  const favorite = availableGame('snes-mario-test', 'Mario', { favorite: true });
  const ready = availableGame('snes-zelda-test', 'Zelda', { favorite: false });
  const missing = availableGame('snes-missing-test', 'Missing', { state: 'missing' });
  const offlineRoot = availableGame('snes-offline-test', 'Offline Root', {
    state: 'available',
    root_online: false,
  });
  const offlineMega = availableGame('megadrive-offline-test', 'Streets of Rage 2 (USA)', {
    system: 'megadrive',
    year: '1992',
    genre: 'Beat \'em Up',
    state: 'available',
    root_online: false,
    region: 'usa',
  });
  const unreadable = availableGame('snes-invalid-test', 'Bad Dump', {
    state: 'invalid',
    root_online: true,
    favorite: true,
    variant_count: 2,
  });
  const beta = availableGame('snes-actraiser-beta', 'ActRaiser (USA) (Beta)', {
    canonical_title: 'ActRaiser',
    dump_flags: 'beta',
    region: 'usa',
    year: '1991',
  });
  const favoriteBeta = availableGame('snes-mario-beta', 'Mario (USA) (Beta)', {
    canonical_title: 'Mario',
    dump_flags: 'beta',
    favorite: true,
  });
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA) (Beta) (Hack)', {
    system: 'megadrive',
    dump_flags: 'beta,hack',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [favorite, ready, missing, offlineRoot, offlineMega, unreadable, beta, favoriteBeta, grouped] })],
    sessionResponses: [jsonResponse(sessionFixture({
      state: 'active',
      game_id: ready.id,
      system: 'snes',
    }))],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  assert.equal(cardMark(byID[favorite.id], 'favorite').textContent, 'Favorite');
  assert.equal(byID[favorite.id].getAttribute('data-favorite'), 'true');
  assert.equal(cardMark(byID[ready.id], 'favorite'), null);
  assert.equal(cardMark(byID[ready.id], 'offline'), null);
  assert.equal(cardMark(byID[ready.id], 'playing').textContent, 'Playing');
  assert.equal(byID[ready.id].getAttribute('data-playing'), 'true');
  assert.equal(cardMark(byID[missing.id], 'offline').textContent, 'Offline');
  assert.equal(cardMark(byID[offlineRoot.id], 'offline').textContent, 'Offline');
  assert.equal(cardMark(byID[unreadable.id], 'offline'), null);
  assert.equal(cardMark(byID[unreadable.id], 'invalid').textContent, 'Unreadable');
  assert.equal(byID[missing.id].getAttribute('data-unavailable'), 'true');
  assert.equal(byID[offlineRoot.id].getAttribute('data-unavailable'), 'true');
  assert.equal(byID[unreadable.id].getAttribute('data-unavailable'), null);
  assert.equal(byID[unreadable.id].getAttribute('data-invalid'), 'true');
  assert.equal(byID[ready.id].getAttribute('data-unavailable'), null);
  assert.equal(byID[favorite.id].children.find(child => child.className === 'game-meta').textContent, 'SNES · Ready');
  assert.equal(byID[offlineRoot.id].children.find(child => child.className === 'game-meta').textContent, 'SNES · Offline');
  assert.equal(byID[offlineMega.id].children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1992 · Offline');
  assert.equal(byID[unreadable.id].children.find(child => child.className === 'game-meta').textContent, 'SNES · Unreadable');
  assert.equal(cardMark(byID[offlineMega.id], 'offline').textContent, 'Offline');
  assert.equal(cardMark(byID[favorite.id], 'beta'), null);
  assert.equal(cardMark(byID[ready.id], 'beta'), null);
  assert.equal(cardMark(byID[ready.id], 'hack'), null);
  assert.equal(cardMark(byID[beta.id], 'beta').textContent, 'Beta');
  assert.equal(cardMark(byID[beta.id], 'hack'), null);
  assert.equal(cardMark(byID[beta.id], 'favorite'), null);
  assert.equal(byID[beta.id].getAttribute('data-unavailable'), null);
  assert.equal(byID[beta.id].getAttribute('data-state'), 'available');
  assert.equal(byID[beta.id].children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(byID[beta.id].children.find(child => child.className === 'game-meta').textContent, /Beta|Hack/);
  assert.equal(cardMark(byID[favoriteBeta.id], 'beta').textContent, 'Beta');
  assert.equal(cardMark(byID[favoriteBeta.id], 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(byID[grouped.id], 'beta').textContent, 'Beta');
  assert.equal(cardMark(byID[grouped.id], 'hack').textContent, 'Hack');
  assert.equal(byID[grouped.id].children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(byID[grouped.id].children.find(child => child.className === 'game-meta').textContent, /Beta|Hack/);

  document.nodes.get('layout-list').click();
  await settleBrowser();
  const listByID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = listByID[favorite.id];
  assert.ok(row.className.includes('game-row'));
  assert.equal(cardMark(row, 'favorite'), null);
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[2].textContent, 'Favorite');
  assert.equal(body.children.length, 3);

  const offlineBody = listByID[offlineRoot.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(offlineBody.children[1].className, 'game-meta');
  assert.match(offlineBody.children[1].textContent, /Offline/);
  assert.doesNotMatch(offlineBody.children[1].textContent, /Ready/);
  assert.equal(cardMark(listByID[offlineRoot.id]), null);
  assert.equal(listByID[offlineRoot.id].children.some(child => String(child.className).includes('card-marks')), false);

  const megaBody = listByID[offlineMega.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(megaBody.children[1].textContent, 'Mega Drive · 1992 · Beat \'em Up · Offline');
  assert.doesNotMatch(megaBody.children[1].textContent, /Ready/);
  assert.doesNotMatch(megaBody.children[1].textContent, /USA/);
  assert.equal(cardMark(listByID[offlineMega.id]), null);
  assert.equal(listByID[offlineMega.id].children.some(child => String(child.className).includes('card-marks')), false);

  const unreadableBody = listByID[unreadable.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(unreadableBody.children[1].textContent, 'SNES · Unreadable');
  assert.doesNotMatch(unreadableBody.children[1].textContent, /Offline/);
  assert.equal(cardMark(listByID[unreadable.id]), null);
  assert.equal(listByID[unreadable.id].children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(unreadableBody.children[2].className, 'game-row-favorite');
  assert.equal(unreadableBody.children[2].textContent, 'Favorite');
  assert.equal(unreadableBody.children[3].className, 'game-meta game-meta-variant');
  assert.equal(unreadableBody.children[3].textContent, '2 versions');
  assert.equal(unreadableBody.children.length, 4);

  const retailBody = listByID[favorite.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(retailBody.children[1].textContent, 'SNES · Ready');
  assert.doesNotMatch(retailBody.children[1].textContent, /Beta|Hack|Unl|Proto/);
  const betaBody = listByID[beta.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(betaBody.children[1].textContent, 'SNES · 1991 · Beta · Ready');
  assert.equal(betaBody.children.length, 2);
  assert.equal(listByID[beta.id].children.some(child => String(child.className).includes('card-marks')), false);
  const groupedBody = listByID[grouped.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(groupedBody.children[1].textContent, 'Mega Drive · 1991 · Beta · Hack · Ready');
  assert.equal(groupedBody.children[2].className, 'game-meta game-meta-variant');
  assert.equal(groupedBody.children[2].textContent, '2 versions');
  assert.equal(groupedBody.children.length, 3);
  assert.equal(listByID[grouped.id].children.some(child => String(child.className).includes('card-marks')), false);
});

test('Home rails reuse the same cover marks without hover', async () => {
  const favorite = availableGame('snes-mario-test', 'Mario', { favorite: true });
  const offline = availableGame('snes-offline-test', 'Offline Game', {
    state: 'missing',
    root_online: false,
  });
  const playing = availableGame('snes-zelda-test', 'Zelda', { favorite: false });
  const beta = availableGame('snes-actraiser-beta', 'ActRaiser (USA) (Beta)', {
    canonical_title: 'ActRaiser',
    dump_flags: 'beta',
    favorite: true,
    region: 'usa',
    year: '1991',
  });
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse({ games: [favorite, offline, playing, beta] })],
    sessionResponses: [jsonResponse(sessionFixture({
      state: 'active',
      game_id: playing.id,
      system: 'snes',
    }))],
    homeRails: {
      continue: jsonResponse({ games: [playing] }),
      favorites: jsonResponse({ games: [beta] }),
      recents: jsonResponse({ games: [offline] }),
    },
  });
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  const byRail = Object.fromEntries(homeRails(document).map(rail => [
    rail.attributes.get('data-home-rail'),
    rail.querySelectorAll('.game-card')[0],
  ]));
  assert.equal(cardMark(byRail.favorites, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(byRail.favorites, 'beta').textContent, 'Beta');
  assert.equal(byRail.favorites.getAttribute('data-unavailable'), null);
  assert.equal(byRail.favorites.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(byRail.favorites.children.find(child => child.className === 'game-meta').textContent, /Beta/);
  assert.equal(cardMark(byRail.recents, 'offline').textContent, 'Offline');
  assert.equal(cardMark(byRail.continue, 'playing').textContent, 'Playing');
  assert.equal(cardMark(byRail.continue, 'favorite'), null);
  assert.equal(cardMark(byRail.favorites, 'offline'), null);
  assert.equal(cardMark(byRail.continue, 'beta'), null);
  assert.equal(cardMark(byRail.recents, 'beta'), null);
});

test('toggleFavorite flips the Cover mark without a catalog reload', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser', { favorite: false });
  const catalogListGets = calls => calls.filter(call => String(call.path).split('?')[0] === '/api/v1/games').length;
  const { document, calls } = await runBrowserApp({
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const before = gameCards(document)[0];
  assert.equal(cardMark(before, 'favorite'), null);
  const catalogGets = catalogListGets(calls);
  await openCardActions(document, before);
  document.getElementById('game-action-favorite').click();
  await settleBrowser();
  const after = gameCards(document)[0];
  assert.equal(cardMark(after, 'favorite').textContent, 'Favorite');
  assert.equal(after.getAttribute('data-favorite'), 'true');
  assert.equal(catalogListGets(calls), catalogGets);
  await openCardActions(document, after);
  assert.equal(document.getElementById('game-action-favorite').textContent, 'Unfavorite');
  document.getElementById('game-action-favorite').click();
  await settleBrowser();
  const cleared = gameCards(document)[0];
  assert.equal(cardMark(cleared, 'favorite'), null);
  assert.equal(cleared.getAttribute('data-favorite'), null);
  assert.equal(catalogListGets(calls), catalogGets);
});

test('Playing mark is only on the active session game and clears after stop', async () => {
  const playing = availableGame('snes-actraiser-test', 'ActRaiser');
  const other = availableGame('snes-mario-test', 'Mario');
  const { document } = await runBrowserApp({
    responses: [
      jsonResponse({ games: [playing, other] }),
      jsonResponse(readFixture('stop-success.json')),
    ],
    sessionResponses: [
      jsonResponse(sessionFixture({
        state: 'active',
        game_id: playing.id,
        system: 'snes',
      })),
      jsonResponse(sessionFixture({ state: 'idle' })),
    ],
  });
  await settleBrowser();
  const cards = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  assert.equal(cardMark(cards[playing.id], 'playing').textContent, 'Playing');
  assert.equal(cardMark(cards[other.id], 'playing'), null);
  await document.nodes.get('session-actions').children.find(node => node.id === 'stop-session').click();
  await settleBrowser();
  const stopped = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  assert.equal(cardMark(stopped[playing.id], 'playing'), null);
  assert.equal(cardMark(stopped[other.id], 'playing'), null);
  assert.equal(stopped[playing.id].getAttribute('data-playing'), null);
});

test('Playing mark maps a launched variant back to the grouped Cover card', async () => {
  const groupKey = 'megadrive\u001fsonic';
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: groupKey,
    variant_count: 2,
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    group_key: groupKey,
    region: 'japan',
  });
  const other = availableGame('snes-mario-test', 'Mario');
  assert.equal(Object.prototype.hasOwnProperty.call(usa, 'variants'), false);
  const { document, calls } = await runBrowserApp({
    responses: [
      jsonResponse({ games: [usa, other] }),
      jsonResponse(japan),
    ],
    sessionResponses: [jsonResponse(sessionFixture({
      state: 'active',
      game_id: japan.id,
      system: 'megadrive',
    }))],
  });
  await settleBrowser();
  const cards = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  assert.equal(selectedCard(document), null);
  assert.equal(cards[usa.id].attributes.get('data-game-id'), 'megadrive-sonic-usa');
  assert.equal(cardMark(cards[usa.id], 'playing').textContent, 'Playing');
  assert.equal(cardMark(cards[other.id], 'playing'), null);
  assert.ok(calls.some(call => call.path === `/api/v1/games/${japan.id}`));
});

test('Playing mark stays off after a failed status poll keeps last-known active session', async () => {
  const playing = availableGame('snes-actraiser-test', 'ActRaiser');
  const other = availableGame('snes-mario-test', 'Mario');
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [playing, other] })],
    sessionResponses: [
      jsonResponse(sessionFixture({
        state: 'active',
        game_id: playing.id,
        system: 'snes',
      })),
      malformedJSONResponse(),
    ],
  });
  await settleBrowser();
  let cards = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  assert.equal(cardMark(cards[playing.id], 'playing').textContent, 'Playing');
  await document.nodes.get('session-actions').children.find(node => node.id === 'refresh-session').click();
  await settleBrowser();
  cards = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  assert.equal(cardMark(cards[playing.id], 'playing'), null);
  assert.equal(cardMark(cards[other.id], 'playing'), null);
  assert.equal(cards[playing.id].getAttribute('data-playing'), null);
  assert.match(browserText(document.nodes.get('session-details')), /Last-known session details/i);
});

test('cover wall CSS keeps a minimum track and card aspect ratio', () => {
  const css = readAsset('ui.css');
  const cardBlock = css.match(/(?:^|\n)\.game-card \{[^}]+\}/);
  const railCardBlock = css.match(/\.home-rail-track \.game-card \{[^}]+\}/);
  const imageCoverBlock = css.match(/img\.cover-art \{[^}]+\}/);
  assert.match(css, /minmax\(min\(100%, var\(--wall-min-track\)\), 1fr\)/);
  assert.ok(cardBlock);
  assert.match(cardBlock[0], /aspect-ratio: 2 \/ 3;/);
  assert.match(cardBlock[0], /min-width: 0;/);
  assert.doesNotMatch(cardBlock[0], /min-height: 0;/);
  assert.ok(railCardBlock);
  assert.match(railCardBlock[0], /min-width: 148px;/);
  assert.ok(imageCoverBlock);
  assert.match(imageCoverBlock[0], /object-fit:\s*contain;/);
  assert.match(imageCoverBlock[0], /object-position:\s*center;/);
  assert.doesNotMatch(css, /(?:\.game-card|\.detail-hero) img\.cover-art \{[^}]*object-fit:\s*cover;/);
  assert.match(css, /\.artwork-empty \{/);
  assert.match(css, /\.platform-list \.nav-item \{[^}]*overflow:\s*hidden/);
  assert.match(css, /\.platform-count \{[^}]*flex:\s*0 0 auto/);
});

test('cover and home rail cards show an honest missing-art placeholder', async () => {
  const actraiser = availableGame('snes-actraiser-test', 'ActRaiser');
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse({ games: [actraiser] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [actraiser] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const continueRail = homeRails(document).find(rail => rail.attributes.get('data-home-rail') === 'continue');
  assert.ok(continueRail);
  const homeCard = continueRail.querySelectorAll('.game-card')[0];
  assert.ok(homeCard);
  assert.equal(homeCard.children[0].className, 'cover-art artwork-empty');
  assert.equal(homeCard.children[0].textContent, 'No art');
  assert.equal(homeCard.children[0].tagName, 'SPAN');

  const seeAll = continueRail.querySelectorAll('.home-rail-see-all')[0]
    || continueRail.children[0].children[1];
  seeAll.click();
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  const wallCard = gameCards(document).find(card => card.attributes.get('data-game-id') === 'snes-actraiser-test');
  assert.ok(wallCard);
  assert.equal(wallCard.children[0].className, 'cover-art artwork-empty');
  assert.equal(wallCard.children[0].textContent, 'No art');
});

test('invalid catalog cover handles use the missing-art placeholder instead of throwing', async () => {
  const actraiser = availableGame('snes-actraiser-test', 'ActRaiser', { cover: 'not-a-handle' });
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [actraiser] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const card = gameCards(document)[0];
  assert.ok(card);
  assert.equal(card.children[0].className, 'cover-art artwork-empty');
  assert.equal(card.children[0].textContent, 'No art');
});

test('cover wall spacers still use cardWidth * 1.5 for a 2-col window', async () => {
  const app = readAsset('ui_app.js');
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  const games = [];
  for (let index = 0; index < 90; index += 1) {
    games.push(availableGame(`snes-game-${index}`, `Title ${index}`));
  }
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  document.nodes.get('catalog-list').clientWidth = 448;
  document.nodes.get('layout-list').click();
  await settleBrowser();
  document.nodes.get('layout-cover').click();
  await settleBrowser();
  const spacer = document.nodes.get('catalog-list').children
    .find(child => child.attributes.get('data-wall-spacer') === 'end');
  assert.ok(spacer);
  assert.equal(spacer.style.height, '1700px');
});

test('narrow catalog wallMetrics follows min(100%, --wall-min-track) for spacers and arrows', async () => {
  const app = readAsset('ui_app.js');
  assert.match(app, /const trackMin = Math\.min\(minTrack, available\)/);
  assert.doesNotMatch(app, /Math\.max\(minTrack, width - padding\)/);
  const games = [];
  for (let index = 0; index < 90; index += 1) {
    games.push(availableGame(`snes-game-${index}`, `Title ${index}`));
  }
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  document.nodes.get('catalog-list').clientWidth = 180;
  document.nodes.get('layout-list').click();
  await settleBrowser();
  document.nodes.get('layout-cover').click();
  await settleBrowser();
  const spacer = document.nodes.get('catalog-list').children
    .find(child => child.attributes.get('data-wall-spacer') === 'end');
  assert.ok(spacer);
  assert.equal(spacer.style.height, '2840px');
  assert.notEqual(spacer.style.height, '3290px');

  const { document: keys } = await runKeyboardApp({
    pages: [{
      games: Array.from({ length: 8 }, (_, index) => availableGame(`snes-grid-${index}`, `Grid ${index}`)),
    }],
  });
  await settleBrowser();
  keys.nodes.get('catalog-list').clientWidth = 180;
  await openAllGamesGrid(keys);
  await pressKey(keys, 'ArrowDown');
  assert.equal(selectedCard(keys).attributes.get('data-game-id'), 'snes-grid-1');
});

test('Region select includes mapDumpRegion tokens and catalogExtras sends korea', async () => {
  const html = readAsset('ui_shell.html');
  assert.match(html, /<option value="korea">Korea<\/option>/);
  assert.match(html, /<option value="asia">Asia<\/option>/);
  assert.match(html, /<option value="australia">Australia<\/option>/);
  assert.match(html, /<option value="france">France<\/option>/);
  assert.match(html, /<option value="other">Other<\/option>/);
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { region: 'korea' });
  const populated = jsonResponse({ games: [sonic] });
  const empty = jsonResponse({ games: [] });
  const { document, calls } = await runBrowserApp({
    keepHome: true,
    responses: [populated],
    homeRails: {
      continue: populated,
      favorites: empty,
      recents: empty,
    },
  });
  await settleBrowser();
  const select = document.nodes.get('filter-region');
  const values = select.children.map(option => option.value);
  assert.deepEqual(values.filter(Boolean), catalogDumpRegions());
  assert.ok(values.includes('korea'));
  assert.equal(values.at(-1), 'other');
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  select.value = 'korea';
  await select.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assert.equal(document.nodes.get('nav-all').className.includes('selected'), true);
  assert.equal(document.nodes.get('nav-home').className.includes('selected'), false);
  assert.equal(document.nodes.get('catalog-list').className, 'game-grid');
  const jump = calls.filter(call => String(call.path).startsWith('/api/v1/games')).at(-1);
  assert.ok(jump);
  assert.equal(jump.path.includes('region=korea'), true);
  assert.equal(jump.path.includes('collection='), false);
});

test('Home Region=korea jumps to All games and does not filter rails', async () => {
  const sonic = availableGame('megadrive-sonic-test', 'Sonic', { region: 'korea' });
  const empty = jsonResponse({ games: [] });
  const populated = jsonResponse({ games: [sonic] });
  const { calls, fetchImpl } = routedFetch({
    '/api/v1/games': [populated, populated],
    '/api/v1/games?collection=continue': [populated, populated],
    '/api/v1/games?collection=favorites': [empty, empty],
    '/api/v1/games?collection=recents': [empty, empty],
  });
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.openHome();
  assert.equal(controller.getState().libraryView, 'home');
  const before = calls.length;
  await controller.setCatalogFilter('region', 'korea');
  assert.equal(controller.getState().libraryView, 'grid');
  assert.equal(controller.getState().collection, '');
  assert.equal(controller.getState().filters.region, 'korea');
  const after = calls.slice(before);
  assert.equal(after.some(call => String(call.path).includes('collection=') && String(call.path).includes('limit=12')), false);
  const jump = after.filter(call => String(call.path).startsWith('/api/v1/games')).at(-1);
  assert.equal(jump.path, '/api/v1/games?region=korea&grouped=1');
});

const WEEKEND_QUEUE = { id: 'weekend-queue', name: 'Weekend Queue' };
const SPEEDRUNS = { id: 'speedruns', name: 'Speedruns' };

test('Cover shows one collection chip and optional +N without changing hover', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(css, /\.card-mark-collection\s*\{[^}]*color:\s*var\(--accent-cool\)/);
  assert.match(css, /\.card-mark-collection\s*\{[^}]*max-width:/);
  assert.match(css, /\.card-mark-collection\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(css, /\.card-mark-collection-more\s*\{/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /function collectionLabels\(game/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.doesNotMatch(app, /card-mark-\$\{.*collection/);
  assert.doesNotMatch(css, /card-mark-collection[^{]*\{[^}]*filter:/);
  assert.doesNotMatch(css, /\.card-mark[^{]*\{[^}]*opacity:\s*0/);

  const listed = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    region: 'usa',
    collections: ['speedruns', 'weekend-queue'],
  });
  const one = availableGame('snes-mario-test', 'Mario', {
    collections: ['weekend-queue'],
  });
  const unlisted = availableGame('snes-zelda-test', 'Zelda');
  const { document, calls } = await runBrowserApp({
    responses: [jsonResponse({ games: [listed, one, unlisted] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const listedCard = byID[listed.id];
  const named = cardCollectionChip(listedCard);
  const more = cardCollectionMore(listedCard);
  assert.equal(listedCard.querySelectorAll('.card-mark-collection').length, 1);
  assert.equal(named.textContent, 'Weekend Queue');
  assert.equal(named.className, 'card-mark card-mark-collection');
  assert.doesNotMatch(named.className, /weekend|queue|speed/i);
  assert.equal(more.textContent, '+1');
  assert.equal(more.className, 'card-mark card-mark-collection-more');
  assert.equal(listedCard.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(listedCard.children.find(child => child.className === 'game-meta').textContent, /Weekend Queue|Speedruns|Action|Beta/);
  assert.equal(cardCollectionChip(byID[one.id]).textContent, 'Weekend Queue');
  assert.equal(cardCollectionMore(byID[one.id]), null);
  assert.equal(cardCollectionChip(byID[unlisted.id]), null);
  assert.equal(cardCollectionMore(byID[unlisted.id]), null);
  assert.equal(byID[unlisted.id].children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(calls.some(call => String(call.path).includes('/api/v1/presentation/')), false);
});

test('Home rails reuse collection chips without hover', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    collections: ['weekend-queue', 'speedruns'],
    region: 'usa',
    year: '1991',
  });
  const favorite = availableGame('snes-mario-test', 'Mario', { favorite: true });
  const { document } = await runBrowserApp({
    keepHome: true,
    responses: [jsonResponse({ games: [queued, favorite] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [favorite] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  assert.equal(document.nodes.get('catalog-list').className, 'home-rails');
  const byRail = Object.fromEntries(homeRails(document).map(rail => [
    rail.attributes.get('data-home-rail'),
    rail.querySelectorAll('.game-card')[0],
  ]));
  assert.equal(cardCollectionChip(byRail.continue).textContent, 'Weekend Queue');
  assert.equal(cardCollectionMore(byRail.continue).textContent, '+1');
  assert.equal(byRail.continue.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(byRail.continue.children.find(child => child.className === 'game-meta').textContent, /Weekend Queue|Speedruns/);
  assert.equal(cardCollectionChip(byRail.favorites), null);
  assert.equal(cardMark(byRail.favorites, 'favorite').textContent, 'Favorite');
});

test('List puts collection names on game-meta without card-marks or a third line', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue', 'speedruns'],
    favorite: true,
    variant_count: 3,
  });
  const unlisted = availableGame('snes-zelda-test', 'Zelda', { year: '1992' });
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [queued, unlisted] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = byID[queued.id];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 4);
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(cardCollectionChip(row), null);
  const readyBody = byID[unlisted.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(readyBody.children[1].textContent, 'SNES · 1992 · Ready');
  assert.doesNotMatch(readyBody.children[1].textContent, /Weekend Queue|\+1/);
  assert.equal(readyBody.children.length, 2);
});

test('detail Collections fact lists resolved names and keeps membership buttons', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    collections: ['missing-shelf', 'weekend-queue', 'speedruns'],
    region: 'usa',
  });
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(document)[0].click();
  await settleBrowser();
  const facts = detailFactsFrom(document);
  assert.ok(facts.some(fact => fact.label === 'Collections' && fact.value === 'Weekend Queue, Speedruns'));
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.equal(facts.some(fact => fact.label === 'Status' && /Playing/.test(fact.value)), false);
  assert.ok(document.getElementById('favorite-game'));
  assert.equal(document.getElementById('collection-member-weekend-queue').textContent, 'Remove from Weekend Queue');
  assert.equal(document.getElementById('collection-member-speedruns').textContent, 'Remove from Speedruns');
});

test('dump-flag collection and Favorite marks coexist on Cover', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA) (Beta)', {
    canonical_title: 'ActRaiser',
    dump_flags: 'beta',
    favorite: true,
    collections: ['weekend-queue'],
    region: 'usa',
    year: '1991',
  });
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [game] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cardCollectionMore(cover), null);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cover.getAttribute('data-favorite'), 'true');
  assert.equal(cover.getAttribute('data-unavailable'), null);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
});

test('grouped wall cards use the representative dump collections only', async () => {
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    collections: ['weekend-queue'],
    region: 'usa',
    year: '1991',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  const { document } = await runBrowserApp({
    responses: [jsonResponse({ games: [grouped] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cardCollectionMore(cover), null);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Weekend Queue · Ready');
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
});

test('toggleCollectionMember flips Cover chips and List meta without a catalog reload', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser', { collections: [] });
  const catalogListGets = calls => calls.filter(call => String(call.path).split('?')[0] === '/api/v1/games').length;
  const { document, calls } = await runBrowserApp({
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const before = gameCards(document)[0];
  assert.equal(cardCollectionChip(before), null);
  const catalogGets = catalogListGets(calls);
  await openCardActions(document, before);
  document.getElementById('game-action-collection-weekend-queue').click();
  await settleBrowser();
  const after = gameCards(document)[0];
  assert.equal(cardCollectionChip(after).textContent, 'Weekend Queue');
  assert.equal(cardCollectionMore(after), null);
  assert.equal(catalogListGets(calls), catalogGets);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'SNES · Weekend Queue · Ready');
  assert.equal(catalogListGets(calls), catalogGets);
  await openCardActions(document, row);
  document.getElementById('game-action-collection-speedruns').click();
  await settleBrowser();
  const two = gameCards(document)[0];
  const twoBody = two.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(twoBody.children[1].textContent, 'SNES · Weekend Queue · +1 · Ready');
  document.nodes.get('layout-cover').click();
  await settleBrowser();
  const coverAgain = gameCards(document)[0];
  assert.equal(cardCollectionChip(coverAgain).textContent, 'Weekend Queue');
  assert.equal(cardCollectionMore(coverAgain).textContent, '+1');
  assert.equal(catalogListGets(calls), catalogGets);
});

test('catalogStudio omits empty fallback em-dash and FogCast demo', () => {
  assert.equal(catalogStudio(), '');
  assert.equal(catalogStudio({}), '');
  assert.equal(catalogStudio({ live: { studio: 'Nintendo' } }), '');
  assert.equal(catalogStudio({ presentation: { isFallback: false, studio: '' } }), '');
  assert.equal(catalogStudio({ presentation: { isFallback: false, studio: '   ' } }), '');
  assert.equal(catalogStudio({ presentation: { isFallback: false, studio: '—' } }), '');
  assert.equal(catalogStudio({ presentation: { isFallback: false, studio: 'FogCast demo' } }), '');
  assert.equal(catalogStudio({
    live: { studio: 'Nintendo' },
    presentation: { isFallback: true, studio: 'SEGA' },
  }), '');
  assert.equal(catalogStudio({
    live: { studio: 'Nintendo' },
    presentation: { isFallback: false, studio: '  SEGA  ' },
  }), 'SEGA');
  assert.equal(catalogStudio({
    presentation: { isFallback: false, studio: 'SEGA' },
  }), 'SEGA');
});

test('Cover shows hover studio in the fallback-note slot and never both', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /function catalogStudio\(view\)/);
  assert.match(app, /\[catalogStudio\(gameView\), 'Studio'\]/);
  assert.match(css, /\.game-card \.fallback-note, \.game-card \.game-studio \{[^}]*top:\s*10px/);
  assert.match(css, /\.game-card:not\(\.game-row\):has\(\.card-marks\) \.fallback-note/);
  assert.match(css, /\.game-card:not\(\.game-row\):has\(\.card-marks\) \.game-studio\s*\{[^}]*top:\s*calc\(8px \+ var\(--card-marks-clearance, calc\(var\(--card-marks-row\) \* 2\)\) \+ 8px\)/);
  assert.match(css, /\.game-card \.game-studio\s*\{[^}]*white-space:\s*nowrap/);
  assert.match(css, /\.game-card \.game-studio\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(css, /\.game-card:hover \.game-studio/);
  assert.match(css, /\.game-card\.selected \.game-studio/);
  assert.match(css, /\.game-card:focus-visible \.game-studio/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.doesNotMatch(css, /\.card-mark-studio/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
  assert.doesNotMatch(app, /game\.studio/);

  const ready = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const fallback = availableGame('snes-fallback-test', 'Fallback Game', {
    year: '1992',
    region: 'usa',
  });
  const { document, calls } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === fallback.id) {
          return {
            summary: 'Fallback summary',
            year: '—',
            genre: 'Action',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyStudioAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [ready, fallback] }), jsonResponse(ready)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[ready.id];
  const fallbackCard = byID[fallback.id];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cardStudio(cover).className, 'game-studio');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beta|Weekend Queue|1 player/);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cover.children.some(child => /card-mark-studio/.test(String(child.className))), false);
  assert.equal(cardStudio(fallbackCard), null);
  assert.equal(fallbackCard.children.filter(child => child.className === 'fallback-note').length, 1);
  assert.equal(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, 'Using local catalog data');
  await cover.click();
  await settleBrowser();
  assert.ok(gameCards(document)[0].className.includes('selected'));
  assert.equal(cardStudio(gameCards(document)[0]).textContent, 'SEGA · 1 player');
  assert.equal(calls.some(call => String(call.path).includes('/api/v1/presentation/')), false);
});

test('Home rails reuse Cover studio in the top slot', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const { document } = await runBrowserApp({
    keepHome: true,
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [queued] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const card = rail.querySelectorAll('.game-card')[0];
  assert.equal(cardStudio(card).textContent, 'SEGA · 1 player');
  assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(card.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(card.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Weekend Queue|1 player/);
  assert.equal(cardCollectionChip(card).textContent, 'Weekend Queue');
});

test('List puts catalogStudio on game-meta after genre before flags', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue', 'speedruns'],
    favorite: true,
    variant_count: 3,
  });
  const plain = availableGame('snes-zelda-test', 'Zelda', { year: '1992' });
  const { document } = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [queued, plain] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = byID[queued.id];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · SEGA · 1 player · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 4);
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(cardCollectionChip(row), null);
  const plainBody = byID[plain.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(plainBody.children[1].textContent, 'SNES · 1992 · Action · SEGA · 1 player · Ready');
  assert.equal(plainBody.children.length, 2);
});

test('detail Studio uses catalogStudio and keeps Favorite plus collection buttons', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    collections: ['weekend-queue', 'speedruns'],
    region: 'usa',
    year: '1991',
    genre: 'Action',
  });
  const ready = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(ready.document)[0].click();
  await settleBrowser();
  const facts = detailFactsFrom(ready.document);
  assert.ok(facts.some(fact => fact.label === 'Studio' && fact.value === 'SEGA'));
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.equal(facts.some(fact => fact.label === 'Status' && /Playing/.test(fact.value)), false);
  assert.ok(ready.document.getElementById('favorite-game'));
  assert.equal(ready.document.getElementById('collection-member-weekend-queue').textContent, 'Remove from Weekend Queue');
  assert.equal(ready.document.getElementById('collection-member-speedruns').textContent, 'Remove from Speedruns');

  const fallback = await runBrowserApp({
    adapter: {
      metadataFor() {
        return {
          summary: 'Fallback summary',
          year: '—',
          genre: 'Action',
          studio: 'FogCast demo',
          players: '1 player',
          isFallback: true,
          metadataState: 'fallback_offline',
          cover: { palette: 'lagoon', treatment: 'rings' },
          backdrop: { palette: 'sunset', treatment: 'waves' },
        };
      },
    },
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallback.document)[0].click();
  await settleBrowser();
  const omitted = detailFactsFrom(fallback.document);
  assert.equal(omitted.some(fact => fact.label === 'Studio'), false);
  assert.doesNotMatch(omitted.map(fact => fact.value).join(' '), /FogCast demo|SEGA/);
  assert.ok(fallback.document.getElementById('favorite-game'));
});

test('grouped wall cards use the representative view presentation studio only', async () => {
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
    genre: 'Platform',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  const { document } = await runBrowserApp({
    adapter: readyStudioAdapter('SEGA'),
    responses: [jsonResponse({ games: [grouped] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Action · SEGA · 1 player · Ready');
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
});

test('prefetch applyCatalogPresentation emit shows studio without a catalog reload', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const catalogListGets = listed => listed.filter(call => String(call.path).split('?')[0] === '/api/v1/games').length;
  const { document, calls } = await runBrowserApp({
    globals: {
      FogCastPresentationEnabled: true,
      FogCastPrefetchVisibleCovers: true,
    },
    responses: [jsonResponse({ games: [game] })],
    presentations: {
      [game.id]: jsonResponse({
        game_id: game.id,
        state: 'ready',
        presentation: {
          summary: 'Visible cover',
          year: '1991',
          genre: 'Action',
          studio: 'SEGA',
          players: '1 player',
        },
        attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
      }),
    },
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const beforeStudio = catalogListGets(calls);
  await waitForCondition(
    () => cardStudio(gameCards(document)[0]),
    'prefetch did not apply studio to the Cover card',
  );
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|1 player/);
  assert.equal(catalogListGets(calls), beforeStudio);
  assert.equal(calls.filter(call => String(call.path).includes('/api/v1/presentation/games/')).length, 1);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · SEGA · 1 player · Beta · Weekend Queue · Ready');
  assert.equal(catalogListGets(calls), beforeStudio);
});

test('WALL_WINDOW spacer math stays cardWidth times 1.5', () => {
  const app = readAsset('ui_app.js');
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
});

test('Cover and Home keep studio below card-marks', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(css, /\.game-card \.fallback-note, \.game-card \.game-studio \{[^}]*top:\s*10px/);
  assert.match(css, /\.game-card:not\(\.game-row\):has\(\.card-marks\)\s*\{[^}]*--card-marks-row:\s*calc\(0\.62rem \* 1\.15 \+ 14px\)/);
  assert.match(css, /\.game-card:not\(\.game-row\):has\(\.card-marks\) \.fallback-note,\s*\.game-card:not\(\.game-row\):has\(\.card-marks\) \.game-studio\s*\{[^}]*top:\s*calc\(8px \+ var\(--card-marks-clearance, calc\(var\(--card-marks-row\) \* 2\)\) \+ 8px\)/);
  assert.doesNotMatch(css, /\.card-mark-studio/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /marks\.offsetHeight/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(app, /disconnectCardMarksObservers/);

  const marked = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const coverApp = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [marked] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(coverApp.document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cardStudio(cover).className, 'game-studio');
  assert.ok(cover.children.some(child => String(child.className).includes('card-marks')));
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beta|Weekend Queue|1 player/);
  assert.equal(cover.children.some(child => /card-mark-studio/.test(String(child.className))), false);
  assert.equal(parseFloat(cover.style['--card-marks-clearance']), cover.children.find(child => child.className === 'card-marks').offsetHeight);

  const homeApp = await runBrowserApp({
    keepHome: true,
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [marked] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [marked] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(homeApp.document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const homeCard = rail.querySelectorAll('.game-card')[0];
  assert.equal(homeCard.offsetWidth, 148);
  assert.equal(cardStudio(homeCard).textContent, 'SEGA · 1 player');
  assert.ok(homeCard.children.some(child => String(child.className).includes('card-marks')));
  assert.equal(cardMark(homeCard, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(homeCard, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(homeCard).textContent, 'Weekend Queue');
  assert.equal(homeCard.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(homeCard.children.find(child => child.className === 'game-meta').textContent, /SEGA|1 player/);
  const homeMarks = homeCard.children.find(child => child.className === 'card-marks');
  const homeClearance = parseFloat(homeCard.style['--card-marks-clearance']);
  const oneRow = 22;
  assert.ok(homeMarks.offsetHeight > oneRow, `148px Home marks should wrap, height=${homeMarks.offsetHeight}`);
  assert.ok(homeClearance > oneRow, `148px Home clearance should exceed one row, --card-marks-clearance=${homeClearance}`);
  assert.equal(homeClearance, homeMarks.offsetHeight);
});

test('Cover recomputes marks clearance after the card shrinks', async () => {
  const observers = [];
  const marked = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const { document } = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [marked] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    globals: {
      ResizeObserver: class {
        constructor(callback) {
          this.callback = callback;
          this.targets = [];
          this.disconnected = false;
          observers.push(this);
        }
        observe(node) { this.targets.push(node); }
        disconnect() { this.disconnected = true; }
      },
    },
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  const marks = cover.children.find(child => child.className === 'card-marks');
  const oneRow = 22;
  cover.offsetWidth = 320;
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(marks.offsetHeight, oneRow);
  assert.equal(parseFloat(cover.style['--card-marks-clearance']), oneRow);
  const observer = observers.find(item => item.targets.includes(cover));
  assert.ok(observer);
  cover.offsetWidth = 148;
  observer.callback();
  assert.ok(marks.offsetHeight > oneRow);
  assert.equal(parseFloat(cover.style['--card-marks-clearance']), marks.offsetHeight);
  assert.ok(parseFloat(cover.style['--card-marks-clearance']) > oneRow);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  assert.equal(observer.disconnected, true);
});

test('catalogPlayers omits empty fallback em-dash Unknown and Unknown players', () => {
  assert.equal(catalogPlayers(), '');
  assert.equal(catalogPlayers({}), '');
  assert.equal(catalogPlayers({ live: { players: '4 players' } }), '');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '' } }), '');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '   ' } }), '');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '—' } }), '');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: 'Unknown' } }), '');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: 'Unknown players' } }), '');
  assert.equal(catalogPlayers({
    live: { players: '4 players' },
    presentation: { isFallback: true, players: '1–2 players' },
  }), '');
  assert.equal(catalogPlayers({
    live: { players: '4 players' },
    presentation: { isFallback: false, players: '  1–2 players  ' },
  }), '1–2 players');
  assert.equal(catalogPlayers({
    presentation: { isFallback: false, players: '1–2 players' },
  }), '1–2 players');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '1' } }), '1 player');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '  2  ' } }), '2 players');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '1-2' } }), '1–2 players');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '1–2' } }), '1–2 players');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '1 player' } }), '1 player');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: '1-2 players' } }), '1-2 players');
  assert.equal(catalogPlayers({ presentation: { isFallback: false, players: 'up to 4' } }), 'up to 4');
});

test('Cover shows studio and players in one game-studio slot and never both with fallback-note', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /function catalogPlayers\(view\)/);
  assert.match(app, /\[catalogStudio\(view\), catalogPlayers\(view\)\]\.filter\(Boolean\)\.join\(' · '\)/);
  assert.match(app, /\[catalogPlayers\(gameView\), 'Players'\]/);
  assert.doesNotMatch(app, /\[presentation\.players, 'Players'\]/);
  assert.doesNotMatch(app, /game\.players/);
  assert.match(css, /\.game-card \.fallback-note, \.game-card \.game-studio \{[^}]*top:\s*10px/);
  assert.match(css, /\.game-card:not\(\.game-row\):has\(\.card-marks\) \.game-studio\s*\{[^}]*top:\s*calc\(8px \+ var\(--card-marks-clearance, calc\(var\(--card-marks-row\) \* 2\)\) \+ 8px\)/);
  assert.match(css, /\.game-card \.game-studio\s*\{[^}]*white-space:\s*nowrap/);
  assert.match(css, /\.game-card \.game-studio\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(css, /\.game-card:hover \.game-studio/);
  assert.match(css, /\.game-card\.selected \.game-studio/);
  assert.match(css, /\.game-card:focus-visible \.game-studio/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.doesNotMatch(css, /\.card-mark-players/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);

  const ready = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const playersOnly = availableGame('snes-players-only-test', 'Players Only', {
    year: '1993',
    genre: 'Action',
    region: 'usa',
  });
  const fallback = availableGame('snes-fallback-test', 'Fallback Game', {
    year: '1992',
    region: 'usa',
  });
  const { document, calls } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === fallback.id) {
          return {
            summary: 'Fallback summary',
            year: '—',
            genre: 'Action',
            studio: 'SEGA',
            players: '1–2 players',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        if (game && game.id === playersOnly.id) {
          return readyPlayersAdapter('', '1–2 players').metadataFor(game);
        }
        return readyPlayersAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [ready, playersOnly, fallback] }), jsonResponse(ready)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[ready.id];
  const playersCard = byID[playersOnly.id];
  const fallbackCard = byID[fallback.id];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1–2 players');
  assert.equal(cardStudio(cover).className, 'game-studio');
  assert.equal(cover.children.filter(child => child.className === 'game-studio').length, 1);
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|1–2 players|Action|Beta|Weekend Queue/);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cover.children.some(child => /card-mark-players/.test(String(child.className))), false);
  assert.equal(cardStudio(playersCard).textContent, '1–2 players');
  assert.equal(cardStudio(playersCard).className, 'game-studio');
  assert.equal(playersCard.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cardStudio(fallbackCard), null);
  assert.equal(fallbackCard.children.filter(child => child.className === 'fallback-note').length, 1);
  assert.equal(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, 'Using local catalog data');
  await cover.click();
  await settleBrowser();
  assert.ok(gameCards(document)[0].className.includes('selected'));
  assert.equal(cardStudio(gameCards(document)[0]).textContent, 'SEGA · 1–2 players');
  assert.equal(calls.some(call => String(call.path).includes('/api/v1/presentation/')), false);
});

test('Home rails reuse Cover studio-players subtitle in the top slot', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const { document } = await runBrowserApp({
    keepHome: true,
    adapter: readyPlayersAdapter(),
    responses: [jsonResponse({ games: [queued] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const card = rail.querySelectorAll('.game-card')[0];
  assert.equal(cardStudio(card).textContent, 'SEGA · 1–2 players');
  assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(card.children.filter(child => child.className === 'game-studio').length, 1);
  assert.equal(card.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(card.children.find(child => child.className === 'game-meta').textContent, /SEGA|1–2 players|Action|Weekend Queue/);
  assert.equal(cardCollectionChip(card).textContent, 'Weekend Queue');
});

test('List puts catalogPlayers on game-meta after studio before flags', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue', 'speedruns'],
    favorite: true,
    variant_count: 3,
  });
  const plain = availableGame('snes-zelda-test', 'Zelda', { year: '1992' });
  const { document } = await runBrowserApp({
    adapter: readyPlayersAdapter(),
    responses: [jsonResponse({ games: [queued, plain] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = byID[queued.id];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · SEGA · 1–2 players · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 4);
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(cardCollectionChip(row), null);
  const plainBody = byID[plain.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(plainBody.children[1].textContent, 'SNES · 1992 · Action · SEGA · 1–2 players · Ready');
  assert.equal(plainBody.children.length, 2);
});

test('detail Players uses catalogPlayers and fallback omits Unknown players', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    collections: ['weekend-queue', 'speedruns'],
    region: 'usa',
    year: '1991',
    genre: 'Action',
  });
  const ready = await runBrowserApp({
    adapter: readyPlayersAdapter(),
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(ready.document)[0].click();
  await settleBrowser();
  const facts = detailFactsFrom(ready.document);
  assert.ok(facts.some(fact => fact.label === 'Players' && fact.value === '1–2 players'));
  assert.ok(facts.some(fact => fact.label === 'Studio' && fact.value === 'SEGA'));
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.equal(facts.some(fact => fact.label === 'Status' && /Playing/.test(fact.value)), false);
  assert.ok(ready.document.getElementById('favorite-game'));
  assert.equal(ready.document.getElementById('collection-member-weekend-queue').textContent, 'Remove from Weekend Queue');
  assert.equal(ready.document.getElementById('collection-member-speedruns').textContent, 'Remove from Speedruns');

  const fallback = await runBrowserApp({
    adapter: {
      metadataFor() {
        return {
          summary: 'Fallback summary',
          year: '—',
          genre: 'Action',
          studio: 'FogCast demo',
          players: 'Unknown players',
          isFallback: true,
          metadataState: 'fallback_offline',
          cover: { palette: 'lagoon', treatment: 'rings' },
          backdrop: { palette: 'sunset', treatment: 'waves' },
        };
      },
    },
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallback.document)[0].click();
  await settleBrowser();
  const omitted = detailFactsFrom(fallback.document);
  assert.equal(omitted.some(fact => fact.label === 'Players'), false);
  assert.equal(omitted.some(fact => fact.label === 'Studio'), false);
  assert.doesNotMatch(omitted.map(fact => fact.value).join(' '), /Unknown players|1–2 players|1 player|FogCast demo/);
  assert.ok(fallback.document.getElementById('favorite-game'));
});

test('grouped wall cards use the representative view presentation players only', async () => {
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
    genre: 'Platform',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  const { document } = await runBrowserApp({
    adapter: readyPlayersAdapter('SEGA', '1–2 players'),
    responses: [jsonResponse({ games: [grouped] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1–2 players');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /1–2 players|Platform/);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Action · SEGA · 1–2 players · Ready');
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
});

test('prefetch applyCatalogPresentation emit shows players without a catalog reload', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const catalogListGets = listed => listed.filter(call => String(call.path).split('?')[0] === '/api/v1/games').length;
  const { document, calls } = await runBrowserApp({
    globals: {
      FogCastPresentationEnabled: true,
      FogCastPrefetchVisibleCovers: true,
    },
    responses: [jsonResponse({ games: [game] })],
    presentations: {
      [game.id]: jsonResponse({
        game_id: game.id,
        state: 'ready',
        presentation: {
          summary: 'Visible cover',
          year: '1991',
          genre: 'Action',
          studio: 'SEGA',
          players: '1–2 players',
        },
        attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
      }),
    },
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const beforePlayers = catalogListGets(calls);
  await waitForCondition(
    () => {
      const card = gameCards(document)[0];
      return cardStudio(card) && cardStudio(card).textContent === 'SEGA · 1–2 players';
    },
    'prefetch did not apply players to the Cover card',
  );
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1–2 players');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|1–2 players/);
  assert.equal(catalogListGets(calls), beforePlayers);
  assert.equal(calls.filter(call => String(call.path).includes('/api/v1/presentation/games/')).length, 1);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · SEGA · 1–2 players · Beta · Weekend Queue · Ready');
  assert.equal(catalogListGets(calls), beforePlayers);
});

test('Cover List and detail format numeric catalogPlayers', async () => {
  const one = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const ranged = availableGame('snes-range-test', 'Range Game', {
    year: '1992',
    genre: 'Action',
    region: 'japan',
  });
  const { document } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === ranged.id) return readyPlayersAdapter('SEGA', '1-2').metadataFor(game);
        return readyPlayersAdapter('SEGA', '1').metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [one, ranged] }), jsonResponse(one)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[one.id];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /1 player|1–2 players|Action|Beta|Weekend Queue/);
  assert.equal(cardStudio(byID[ranged.id]).textContent, 'SEGA · 1–2 players');
  await cover.click();
  await settleBrowser();
  const facts = detailFactsFrom(document);
  assert.ok(facts.some(fact => fact.label === 'Players' && fact.value === '1 player'));
  assert.ok(facts.some(fact => fact.label === 'Studio' && fact.value === 'SEGA'));
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const listByID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const body = listByID[one.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · SEGA · 1 player · Beta · Weekend Queue · Ready');
  const rangeBody = listByID[ranged.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(rangeBody.children[1].textContent, 'SNES · 1992 · Action · SEGA · 1–2 players · Ready');
  assert.equal(cardStudio(listByID[one.id]), null);
});

test('WALL_WINDOW spacer math and marks clearance stay unchanged with players subtitle', () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /marks\.offsetHeight/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(app, /disconnectCardMarksObservers/);
  assert.doesNotMatch(css, /\.card-mark-players/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
});

test('catalogGenre omits empty fallback em-dash and Unknown', () => {
  assert.equal(catalogGenre(), '');
  assert.equal(catalogGenre({}), '');
  assert.equal(catalogGenre({ live: { genre: 'Action' } }), 'Action');
  assert.equal(catalogGenre({ presentation: { isFallback: false, genre: '' } }), '');
  assert.equal(catalogGenre({ presentation: { isFallback: false, genre: '   ' } }), '');
  assert.equal(catalogGenre({ presentation: { isFallback: false, genre: '—' } }), '');
  assert.equal(catalogGenre({ presentation: { isFallback: false, genre: 'Unknown' } }), '');
  assert.equal(catalogGenre({
    live: { genre: 'Action' },
    presentation: { isFallback: true, genre: 'Platformer' },
  }), 'Action');
  assert.equal(catalogGenre({
    presentation: { isFallback: true, genre: 'Platformer' },
  }), '');
  assert.equal(catalogGenre({
    live: { genre: '—' },
    presentation: { isFallback: true, genre: 'Action' },
  }), '');
  assert.equal(catalogGenre({
    live: { genre: 'Unknown' },
    presentation: { isFallback: true, genre: 'Action' },
  }), '');
  assert.equal(catalogGenre({
    live: { genre: 'Action' },
    presentation: { isFallback: false, genre: '  Beat \'em Up  ' },
  }), 'Beat \'em Up');
  assert.equal(catalogGenre({
    presentation: { isFallback: false, genre: 'Action' },
  }), 'Action');
  assert.equal(catalogGenre({
    presentation: { isFallback: false, genre: 'Platformer' },
  }), 'Platformer');
});

test('Cover hover stays system region year status and omits genre', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /function catalogGenre\(view\)/);
  assert.match(app, /const genre = catalogGenre\(view\)/);
  assert.match(app, /\[catalogGenre\(gameView\), 'Genre'\]/);
  assert.doesNotMatch(app, /\[presentation\.genre, 'Genre'\]/);
  assert.doesNotMatch(app, /if \(game\.genre\)/);
  assert.doesNotMatch(app, /FALLBACK_GENRES/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.doesNotMatch(css, /\.card-mark-genre/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);

  const ready = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const fallback = availableGame('snes-fallback-test', 'Fallback Game', {
    year: '1992',
    region: 'usa',
  });
  const { document, calls } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === fallback.id) {
          return {
            summary: 'Fallback summary',
            year: '—',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyGenreAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [ready, fallback] }), jsonResponse(ready)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[ready.id];
  const fallbackCard = byID[fallback.id];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cardStudio(cover).className, 'game-studio');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beat 'em Up|Beta|Weekend Queue|1 player/);
  assert.doesNotMatch(cardStudio(cover).textContent, /Action|Beat 'em Up|Platformer/);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cover.children.some(child => /card-mark-genre/.test(String(child.className))), false);
  assert.equal(cardStudio(fallbackCard), null);
  assert.equal(fallbackCard.children.filter(child => child.className === 'fallback-note').length, 1);
  assert.equal(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, 'Using local catalog data');
  assert.equal(fallbackCard.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1992 · Ready');
  assert.doesNotMatch(fallbackCard.children.find(child => child.className === 'game-meta').textContent, /Platformer|Action|Beat 'em Up/);
  await cover.click();
  await settleBrowser();
  assert.ok(gameCards(document)[0].className.includes('selected'));
  assert.equal(cardStudio(gameCards(document)[0]).textContent, 'SEGA · 1 player');
  assert.doesNotMatch(cardStudio(gameCards(document)[0]).textContent, /Beat 'em Up|Action/);
  assert.equal(calls.some(call => String(call.path).includes('/api/v1/presentation/')), false);
});

test('Home rails keep Cover hover without genre', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const { document } = await runBrowserApp({
    keepHome: true,
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [queued] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const card = rail.querySelectorAll('.game-card')[0];
  assert.equal(cardStudio(card).textContent, 'SEGA · 1 player');
  assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(card.children.filter(child => child.className === 'game-studio').length, 1);
  assert.equal(card.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(card.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beat 'em Up|Weekend Queue|1 player/);
  assert.doesNotMatch(cardStudio(card).textContent, /Action|Beat 'em Up/);
  assert.equal(cardCollectionChip(card).textContent, 'Weekend Queue');
});

test('List puts catalogGenre on game-meta after year before studio', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue', 'speedruns'],
    favorite: true,
    variant_count: 3,
  });
  const empty = availableGame('snes-empty-genre-test', 'Empty Genre', { year: '1992' });
  const { document } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === empty.id) {
          return {
            summary: 'Fallback summary',
            year: '—',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyGenreAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [queued, empty] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = byID[queued.id];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Beat \'em Up · SEGA · 1 player · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 4);
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(cardCollectionChip(row), null);
  const emptyBody = byID[empty.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(emptyBody.children[1].textContent, 'SNES · 1992 · Ready');
  assert.doesNotMatch(emptyBody.children[1].textContent, /Platformer|Action|Beat 'em Up|SEGA|1 player/);
  assert.equal(emptyBody.children.length, 2);
});

test('detail Genre uses catalogGenre and fallback omits demo genre', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    collections: ['weekend-queue', 'speedruns'],
    region: 'usa',
    year: '1991',
    genre: 'Action',
  });
  const ready = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(ready.document)[0].click();
  await settleBrowser();
  const facts = detailFactsFrom(ready.document);
  assert.ok(facts.some(fact => fact.label === 'Genre' && fact.value === 'Beat \'em Up'));
  assert.ok(facts.some(fact => fact.label === 'Studio' && fact.value === 'SEGA'));
  assert.ok(facts.some(fact => fact.label === 'Players' && fact.value === '1 player'));
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.equal(facts.some(fact => fact.label === 'Status' && /Playing/.test(fact.value)), false);
  assert.ok(ready.document.getElementById('favorite-game'));
  assert.equal(ready.document.getElementById('collection-member-weekend-queue').textContent, 'Remove from Weekend Queue');
  assert.equal(ready.document.getElementById('collection-member-speedruns').textContent, 'Remove from Speedruns');

  const fallbackEmpty = await runBrowserApp({
    adapter: {
      metadataFor() {
        return {
          summary: 'Fallback summary',
          year: '—',
          genre: 'Platformer',
          studio: 'FogCast demo',
          players: 'Unknown players',
          isFallback: true,
          metadataState: 'fallback_offline',
          cover: { palette: 'lagoon', treatment: 'rings' },
          backdrop: { palette: 'sunset', treatment: 'waves' },
        };
      },
    },
    responses: [jsonResponse({ games: [availableGame('snes-empty-genre-test', 'Empty Genre', { year: '1992' })] }), jsonResponse(availableGame('snes-empty-genre-test', 'Empty Genre', { year: '1992' }))],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallbackEmpty.document)[0].click();
  await settleBrowser();
  const omitted = detailFactsFrom(fallbackEmpty.document);
  assert.equal(omitted.some(fact => fact.label === 'Genre'), false);
  assert.equal(omitted.some(fact => fact.label === 'Studio'), false);
  assert.equal(omitted.some(fact => fact.label === 'Players'), false);
  assert.doesNotMatch(omitted.map(fact => fact.value).join(' '), /Platformer|Action|Beat 'em Up|FogCast demo/);
  assert.ok(fallbackEmpty.document.getElementById('favorite-game'));

  const fallbackLive = await runBrowserApp({
    adapter: {
      metadataFor() {
        return {
          summary: 'Fallback summary',
          year: '—',
          genre: 'Platformer',
          studio: 'FogCast demo',
          players: 'Unknown players',
          isFallback: true,
          metadataState: 'fallback_offline',
          cover: { palette: 'lagoon', treatment: 'rings' },
          backdrop: { palette: 'sunset', treatment: 'waves' },
        };
      },
    },
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallbackLive.document)[0].click();
  await settleBrowser();
  const liveFacts = detailFactsFrom(fallbackLive.document);
  assert.ok(liveFacts.some(fact => fact.label === 'Genre' && fact.value === 'Action'));
  assert.equal(liveFacts.some(fact => fact.label === 'Genre' && fact.value === 'Platformer'), false);
  assert.ok(fallbackLive.document.getElementById('favorite-game'));
});

test('grouped wall cards use the representative view presentation genre only', async () => {
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
    genre: 'Platform',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  const { document } = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /Beat 'em Up|Platform|Action/);
  assert.doesNotMatch(cardStudio(cover).textContent, /Beat 'em Up|Platform/);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Beat \'em Up · SEGA · 1 player · Ready');
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
});

test('prefetch applyCatalogPresentation emit shows List genre without a catalog reload', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const catalogListGets = listed => listed.filter(call => String(call.path).split('?')[0] === '/api/v1/games').length;
  const { document, calls } = await runBrowserApp({
    globals: {
      FogCastPresentationEnabled: true,
      FogCastPrefetchVisibleCovers: true,
    },
    responses: [jsonResponse({ games: [game] })],
    presentations: {
      [game.id]: jsonResponse({
        game_id: game.id,
        state: 'ready',
        presentation: {
          summary: 'Visible cover',
          year: '1991',
          genre: 'Beat \'em Up',
          studio: 'SEGA',
          players: '1 player',
        },
        attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
      }),
    },
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const beforeGenre = catalogListGets(calls);
  await waitForCondition(
    () => {
      const card = gameCards(document)[0];
      return cardStudio(card) && cardStudio(card).textContent === 'SEGA · 1 player';
    },
    'prefetch did not apply studio to the Cover card',
  );
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|1 player|Beat 'em Up|Action/);
  assert.doesNotMatch(cardStudio(cover).textContent, /Beat 'em Up|Action/);
  assert.equal(catalogListGets(calls), beforeGenre);
  assert.equal(calls.filter(call => String(call.path).includes('/api/v1/presentation/games/')).length, 1);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Beat \'em Up · SEGA · 1 player · Beta · Weekend Queue · Ready');
  assert.equal(catalogListGets(calls), beforeGenre);
});

test('WALL_WINDOW spacer math and marks clearance stay unchanged with catalogGenre', () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /marks\.offsetHeight/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(app, /disconnectCardMarksObservers/);
  assert.doesNotMatch(css, /\.card-mark-genre/);
  assert.doesNotMatch(app, /\.card-mark-genre/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
});

test('catalogYear omits empty em-dash and curated fallback years', () => {
  assert.equal(catalogYear(), '');
  assert.equal(catalogYear({}), '');
  assert.equal(catalogYear({ live: { year: '1991' } }), '1991');
  assert.equal(catalogYear({ presentation: { isFallback: false, year: '' } }), '');
  assert.equal(catalogYear({ presentation: { isFallback: false, year: '—' } }), '');
  assert.equal(catalogYear({ presentation: { isFallback: true, year: '1991' } }), '');
  assert.equal(catalogYear({
    live: { year: '1991' },
    presentation: { isFallback: true, year: '1991' },
  }), '1991');
  assert.equal(catalogYear({
    live: { year: '1991' },
    presentation: { isFallback: true, year: '1990' },
  }), '1991');
  assert.equal(catalogYear({
    live: { year: '1991' },
    presentation: { isFallback: false, year: '—' },
  }), '1991');
  assert.equal(catalogYear({
    live: { year: '1991' },
    presentation: { isFallback: false, year: '' },
  }), '1991');
  assert.equal(catalogYear({
    live: { year: '—' },
    presentation: { isFallback: true, year: '1991' },
  }), '');
  assert.equal(catalogYear({
    live: { year: 'demo' },
    presentation: { isFallback: false, year: '1991' },
  }), '1991');
  assert.equal(catalogYear({
    presentation: { isFallback: false, year: '1991' },
  }), '1991');
});

test('Cover hover stays system region year status and omits year from studio', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /function catalogYear\(view\)/);
  assert.match(app, /\(game && game\.year\) \|\| catalogYear\(view\)/);
  assert.match(app, /\[catalogYear\(gameView\), 'Year'\]/);
  assert.doesNotMatch(app, /presentation\.year !== '—' \? presentation\.year : ''/);
  assert.doesNotMatch(app, /if \(game\.year\) bits\.push\(game\.year\)/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.doesNotMatch(css, /\.card-mark-year/);
  assert.doesNotMatch(app, /\.card-mark-year/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);

  const ready = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const fallback = availableGame('snes-fallback-test', 'Fallback Game', {
    region: 'usa',
  });
  const { document, calls } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === fallback.id) {
          return {
            summary: 'Fallback summary',
            year: '1991',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyGenreAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [ready, fallback] }), jsonResponse(ready)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[ready.id];
  const fallbackCard = byID[fallback.id];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cardStudio(cover).className, 'game-studio');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beat 'em Up|Beta|Weekend Queue|1 player/);
  assert.doesNotMatch(cardStudio(cover).textContent, /1991|1990|1992/);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cover.children.some(child => /card-mark-year/.test(String(child.className))), false);
  assert.equal(cardStudio(fallbackCard), null);
  assert.equal(fallbackCard.children.filter(child => child.className === 'fallback-note').length, 1);
  assert.equal(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, 'Using local catalog data');
  assert.equal(fallbackCard.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · Ready');
  assert.doesNotMatch(fallbackCard.children.find(child => child.className === 'game-meta').textContent, /1991|1990|1992/);
  await cover.click();
  await settleBrowser();
  assert.ok(gameCards(document)[0].className.includes('selected'));
  assert.equal(cardStudio(gameCards(document)[0]).textContent, 'SEGA · 1 player');
  assert.doesNotMatch(cardStudio(gameCards(document)[0]).textContent, /1991|1990|1992/);
  assert.equal(calls.some(call => String(call.path).includes('/api/v1/presentation/')), false);
});

test('Home rails keep Cover hover year without studio year', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const { document } = await runBrowserApp({
    keepHome: true,
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [queued] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const card = rail.querySelectorAll('.game-card')[0];
  assert.equal(cardStudio(card).textContent, 'SEGA · 1 player');
  assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(card.children.filter(child => child.className === 'game-studio').length, 1);
  assert.equal(card.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(card.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beat 'em Up|Weekend Queue|1 player/);
  assert.doesNotMatch(cardStudio(card).textContent, /1991|1990|1992/);
  assert.equal(cardCollectionChip(card).textContent, 'Weekend Queue');
});

test('List puts catalogYear on game-meta after system before genre', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue', 'speedruns'],
    favorite: true,
    variant_count: 3,
  });
  const empty = availableGame('snes-empty-year-test', 'Empty Year', {});
  const { document } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === empty.id) {
          return {
            summary: 'Fallback summary',
            year: '1991',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyGenreAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [queued, empty] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = byID[queued.id];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Beat \'em Up · SEGA · 1 player · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 4);
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(cardCollectionChip(row), null);
  const emptyBody = byID[empty.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(emptyBody.children[1].textContent, 'SNES · Ready');
  assert.doesNotMatch(emptyBody.children[1].textContent, /1991|1990|1992|Platformer|Action|Beat 'em Up|SEGA|1 player/);
  assert.equal(emptyBody.children.length, 2);
});

test('detail Year uses catalogYear and fallback omits curated year', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    collections: ['weekend-queue', 'speedruns'],
    region: 'usa',
    year: '1991',
    genre: 'Action',
  });
  const ready = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(ready.document)[0].click();
  await settleBrowser();
  const facts = detailFactsFrom(ready.document);
  assert.ok(facts.some(fact => fact.label === 'Year' && fact.value === '1991'));
  assert.ok(facts.some(fact => fact.label === 'Genre' && fact.value === 'Beat \'em Up'));
  assert.ok(facts.some(fact => fact.label === 'Studio' && fact.value === 'SEGA'));
  assert.ok(facts.some(fact => fact.label === 'Players' && fact.value === '1 player'));
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.equal(facts.some(fact => fact.label === 'Status' && /Playing/.test(fact.value)), false);
  assert.ok(ready.document.getElementById('favorite-game'));
  assert.equal(ready.document.getElementById('collection-member-weekend-queue').textContent, 'Remove from Weekend Queue');
  assert.equal(ready.document.getElementById('collection-member-speedruns').textContent, 'Remove from Speedruns');

  const fallbackEmpty = await runBrowserApp({
    adapter: {
      metadataFor() {
        return {
          summary: 'Fallback summary',
          year: '1991',
          genre: 'Platformer',
          studio: 'FogCast demo',
          players: 'Unknown players',
          isFallback: true,
          metadataState: 'fallback_offline',
          cover: { palette: 'lagoon', treatment: 'rings' },
          backdrop: { palette: 'sunset', treatment: 'waves' },
        };
      },
    },
    responses: [jsonResponse({ games: [availableGame('snes-empty-year-test', 'Empty Year')] }), jsonResponse(availableGame('snes-empty-year-test', 'Empty Year'))],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallbackEmpty.document)[0].click();
  await settleBrowser();
  const omitted = detailFactsFrom(fallbackEmpty.document);
  assert.equal(omitted.some(fact => fact.label === 'Year'), false);
  assert.equal(omitted.some(fact => fact.label === 'Genre'), false);
  assert.equal(omitted.some(fact => fact.label === 'Studio'), false);
  assert.equal(omitted.some(fact => fact.label === 'Players'), false);
  assert.doesNotMatch(omitted.map(fact => fact.value).join(' '), /1991|1990|1992|Platformer|Action|Beat 'em Up|FogCast demo/);
  assert.ok(fallbackEmpty.document.getElementById('favorite-game'));

  const fallbackLive = await runBrowserApp({
    adapter: {
      metadataFor() {
        return {
          summary: 'Fallback summary',
          year: '—',
          genre: 'Platformer',
          studio: 'FogCast demo',
          players: 'Unknown players',
          isFallback: true,
          metadataState: 'fallback_offline',
          cover: { palette: 'lagoon', treatment: 'rings' },
          backdrop: { palette: 'sunset', treatment: 'waves' },
        };
      },
    },
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallbackLive.document)[0].click();
  await settleBrowser();
  const liveFacts = detailFactsFrom(fallbackLive.document);
  assert.ok(liveFacts.some(fact => fact.label === 'Year' && fact.value === '1991'));
  assert.equal(liveFacts.some(fact => fact.label === 'Year' && fact.value === '—'), false);
  assert.ok(fallbackLive.document.getElementById('favorite-game'));
});

test('grouped wall cards use the representative view presentation year only', async () => {
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
    genre: 'Platform',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  const { document } = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /Beat 'em Up|Platform|Action/);
  assert.doesNotMatch(cardStudio(cover).textContent, /1991|Beat 'em Up|Platform/);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Beat \'em Up · SEGA · 1 player · Ready');
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
});

test('prefetch applyCatalogPresentation emit shows List year without a catalog reload', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const catalogListGets = listed => listed.filter(call => String(call.path).split('?')[0] === '/api/v1/games').length;
  const { document, calls } = await runBrowserApp({
    globals: {
      FogCastPresentationEnabled: true,
      FogCastPrefetchVisibleCovers: true,
    },
    responses: [jsonResponse({ games: [game] })],
    presentations: {
      [game.id]: jsonResponse({
        game_id: game.id,
        state: 'ready',
        presentation: {
          summary: 'Visible cover',
          year: '1991',
          genre: 'Beat \'em Up',
          studio: 'SEGA',
          players: '1 player',
        },
        attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
      }),
    },
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const beforeYear = catalogListGets(calls);
  await waitForCondition(
    () => {
      const card = gameCards(document)[0];
      const meta = card && card.children.find(child => child.className === 'game-meta');
      return meta && meta.textContent === 'SNES · USA · 1991 · Ready';
    },
    'prefetch did not apply year to the Cover hover',
  );
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|1 player|Beat 'em Up|Action/);
  assert.doesNotMatch(cardStudio(cover).textContent, /1991|Beat 'em Up|Action/);
  assert.equal(catalogListGets(calls), beforeYear);
  assert.equal(calls.filter(call => String(call.path).includes('/api/v1/presentation/games/')).length, 1);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Beat \'em Up · SEGA · 1 player · Beta · Weekend Queue · Ready');
  assert.equal(catalogListGets(calls), beforeYear);
});

test('WALL_WINDOW spacer math and marks clearance stay unchanged with catalogYear', () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /marks\.offsetHeight/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(app, /disconnectCardMarksObservers/);
  assert.doesNotMatch(css, /\.card-mark-year/);
  assert.doesNotMatch(app, /\.card-mark-year/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
});

function detailSummaryFrom(document) {
  const content = document.nodes.get('detail-content');
  return content && content.children
    ? content.children.find(child => child.className === 'detail-summary') || null
    : null;
}

function detailFallbackNoteFrom(document) {
  const content = document.nodes.get('detail-content');
  return content && content.children
    ? content.children.find(child => child.className === 'fallback-note') || null
    : null;
}

test('catalogSummary omits missing fallback em-dash and curated demo blurbs', () => {
  const curated = 'A high-speed demo adventure through bright zones and loop-filled stages.';
  const hashCopy = 'Sonic the Hedgehog is a deterministic demo presentation for Mega Drive.';
  assert.equal(catalogSummary(), '');
  assert.equal(catalogSummary({}), '');
  assert.equal(catalogSummary({ live: { summary: 'Catalog Game has no Summary' } }), '');
  assert.equal(catalogSummary({ presentation: { isFallback: false, summary: '' } }), '');
  assert.equal(catalogSummary({ presentation: { isFallback: false, summary: '   ' } }), '');
  assert.equal(catalogSummary({ presentation: { isFallback: false, summary: '—' } }), '');
  assert.equal(catalogSummary({
    live: { summary: 'should not appear' },
    presentation: { isFallback: false, summary: '' },
  }), '');
  assert.equal(catalogSummary({
    live: { summary: 'should not appear' },
    presentation: { isFallback: true, summary: curated },
  }), '');
  assert.equal(catalogSummary({
    presentation: { isFallback: true, summary: hashCopy },
  }), '');
  assert.equal(catalogSummary({
    presentation: { isFallback: true, summary: curated },
  }), '');
  assert.equal(catalogSummary({
    live: { summary: 'should not appear' },
    presentation: { isFallback: false, summary: '  Provider summary  ' },
  }), 'Provider summary');
  assert.equal(catalogSummary({
    presentation: { isFallback: false, summary: curated },
  }), curated);
  assert.equal(catalogSummary({
    presentation: { isFallback: false, summary: 'Host-local presentation metadata for Sonic.' },
  }), 'Host-local presentation metadata for Sonic.');
});

test('Cover hover XOR stays system region year status and omits overview', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /function catalogSummary\(view\)/);
  assert.match(app, /const summary = catalogSummary\(gameView\)/);
  assert.match(app, /if \(summary\) nodes\.detailContent\.appendChild\(element\('p', 'detail-summary', summary\)\)/);
  assert.doesNotMatch(app, /if \(presentation\.summary\)/);
  assert.doesNotMatch(app, /view\.live\.summary/);
  assert.doesNotMatch(app, /game\.summary/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.doesNotMatch(css, /\.card-mark-summary/);
  assert.doesNotMatch(app, /\.card-mark-summary/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);

  const ready = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const fallback = availableGame('snes-fallback-test', 'Fallback Game', {
    region: 'usa',
  });
  const { document, calls } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === fallback.id) {
          return {
            summary: 'Fallback Game is a deterministic demo presentation for SNES.',
            year: '1991',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyGenreAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [ready, fallback] }), jsonResponse(ready)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[ready.id];
  const fallbackCard = byID[fallback.id];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cardStudio(cover).className, 'game-studio');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /Provider summary|deterministic demo|Using local catalog data/);
  assert.doesNotMatch(cardStudio(cover).textContent, /Provider summary|deterministic demo|1991|Action|Beat 'em Up/);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cover.children.some(child => /card-mark-summary/.test(String(child.className))), false);
  assert.equal(cardStudio(fallbackCard), null);
  assert.equal(fallbackCard.children.filter(child => child.className === 'fallback-note').length, 1);
  assert.equal(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, 'Using local catalog data');
  assert.equal(fallbackCard.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · Ready');
  assert.doesNotMatch(fallbackCard.children.find(child => child.className === 'game-meta').textContent, /deterministic demo|Provider summary|1991/);
  await cover.click();
  await settleBrowser();
  assert.ok(gameCards(document)[0].className.includes('selected'));
  assert.equal(cardStudio(gameCards(document)[0]).textContent, 'SEGA · 1 player');
  assert.doesNotMatch(cardStudio(gameCards(document)[0]).textContent, /Provider summary/);
  assert.equal(calls.some(call => String(call.path).includes('/api/v1/presentation/')), false);
});

test('Home rails keep Cover hover XOR without overview', async () => {
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const { document } = await runBrowserApp({
    keepHome: true,
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [queued] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const card = rail.querySelectorAll('.game-card')[0];
  assert.equal(cardStudio(card).textContent, 'SEGA · 1 player');
  assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(card.children.filter(child => child.className === 'game-studio').length, 1);
  assert.equal(card.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(card.children.find(child => child.className === 'game-meta').textContent, /Provider summary|SEGA|Action|Beat 'em Up|Weekend Queue|1 player/);
  assert.doesNotMatch(cardStudio(card).textContent, /Provider summary|1991|Action|Beat 'em Up/);
  assert.equal(cardCollectionChip(card).textContent, 'Weekend Queue');
});

test('List game-meta stays unchanged and omits summary at 72px', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue', 'speedruns'],
    favorite: true,
    variant_count: 3,
  });
  const empty = availableGame('snes-empty-summary-test', 'Empty Summary', {});
  const { document } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === empty.id) {
          return {
            summary: 'Empty Summary is a deterministic demo presentation for SNES.',
            year: '1991',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyGenreAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [queued, empty] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = byID[queued.id];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Beat \'em Up · SEGA · 1 player · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA|Provider summary|deterministic demo/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 4);
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  assert.equal(cardCollectionChip(row), null);
  const emptyBody = byID[empty.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(emptyBody.children[1].textContent, 'SNES · Ready');
  assert.doesNotMatch(emptyBody.children[1].textContent, /1991|1990|1992|Platformer|Action|Beat 'em Up|SEGA|1 player|deterministic demo|Provider summary/);
  assert.equal(emptyBody.children.length, 2);
});

test('detail ready overview uses catalogSummary and fallback keeps the note without a blurb', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    collections: ['weekend-queue', 'speedruns'],
    region: 'usa',
    year: '1991',
    genre: 'Action',
  });
  const ready = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(ready.document)[0].click();
  await settleBrowser();
  const readySummary = detailSummaryFrom(ready.document);
  assert.equal(readySummary && readySummary.textContent, 'Provider summary');
  assert.equal(detailFallbackNoteFrom(ready.document), null);
  assert.doesNotMatch(browserText(ready.document.nodes.get('detail-content')), /Using local catalog data|metadata_fallback|deterministic demo/);
  const facts = detailFactsFrom(ready.document);
  assert.ok(facts.some(fact => fact.label === 'Year' && fact.value === '1991'));
  assert.ok(facts.some(fact => fact.label === 'Genre' && fact.value === 'Beat \'em Up'));
  assert.ok(facts.some(fact => fact.label === 'Studio' && fact.value === 'SEGA'));
  assert.ok(facts.some(fact => fact.label === 'Players' && fact.value === '1 player'));
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.ok(ready.document.getElementById('favorite-game'));
  assert.equal(ready.document.getElementById('collection-member-weekend-queue').textContent, 'Remove from Weekend Queue');
  assert.equal(ready.document.getElementById('collection-member-speedruns').textContent, 'Remove from Speedruns');

  const curated = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog', {
    system: 'megadrive',
    collections: ['weekend-queue', 'speedruns'],
  });
  const fallbackCurated = await runBrowserApp({
    adapter: FogCastMetadata,
    responses: [jsonResponse({ games: [curated] }), jsonResponse(curated)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallbackCurated.document)[0].click();
  await settleBrowser();
  assert.equal(detailSummaryFrom(fallbackCurated.document), null);
  assert.equal(detailFallbackNoteFrom(fallbackCurated.document).textContent, 'Using local catalog data');
  assert.match(browserText(fallbackCurated.document.nodes.get('detail-content')), /metadata_fallback/);
  assert.doesNotMatch(browserText(fallbackCurated.document.nodes.get('detail-content')), /high-speed demo adventure|deterministic demo presentation/);
  assert.ok(fallbackCurated.document.getElementById('favorite-game'));
  assert.equal(fallbackCurated.document.getElementById('collection-member-weekend-queue').textContent, 'Remove from Weekend Queue');
  assert.equal(fallbackCurated.document.getElementById('collection-member-speedruns').textContent, 'Remove from Speedruns');

  const hashGame = availableGame('snes-empty-summary-test', 'Empty Summary');
  const fallbackHash = await runBrowserApp({
    adapter: FogCastMetadata,
    responses: [jsonResponse({ games: [hashGame] }), jsonResponse(hashGame)],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(fallbackHash.document)[0].click();
  await settleBrowser();
  assert.equal(detailSummaryFrom(fallbackHash.document), null);
  assert.equal(detailFallbackNoteFrom(fallbackHash.document).textContent, 'Using local catalog data');
  assert.match(browserText(fallbackHash.document.nodes.get('detail-content')), /metadata_fallback/);
  assert.doesNotMatch(browserText(fallbackHash.document.nodes.get('detail-content')), /deterministic demo presentation|Using local catalog data[\s\S]*detail-summary|high-speed demo adventure/);
  assert.ok(fallbackHash.document.getElementById('favorite-game'));
});

test('grouped wall cards use the representative view presentation summary only', async () => {
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
    genre: 'Platform',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  const { document } = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [grouped] }), jsonResponse(grouped)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /Provider summary|Beat 'em Up|Platform|Action/);
  assert.doesNotMatch(cardStudio(cover).textContent, /Provider summary|1991|Beat 'em Up|Platform/);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Beat \'em Up · SEGA · 1 player · Ready');
  assert.doesNotMatch(body.children[1].textContent, /Provider summary/);
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  await row.click();
  await settleBrowser();
  assert.equal(detailSummaryFrom(document).textContent, 'Provider summary');
  assert.equal(detailFallbackNoteFrom(document), null);
});

test('prefetch applyCatalogPresentation emit fills ready overview without a catalog reload', async () => {
  const game = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const catalogListGets = listed => listed.filter(call => String(call.path).split('?')[0] === '/api/v1/games').length;
  const { document, calls } = await runBrowserApp({
    globals: {
      FogCastPresentationEnabled: true,
      FogCastPrefetchVisibleCovers: true,
    },
    responses: [jsonResponse({ games: [game] }), jsonResponse(game)],
    presentations: {
      [game.id]: jsonResponse({
        game_id: game.id,
        state: 'ready',
        presentation: {
          summary: 'Host-local presentation metadata for ActRaiser.',
          year: '1991',
          genre: 'Beat \'em Up',
          studio: 'SEGA',
          players: '1 player',
        },
        attribution: { provider: 'launchbox', label: 'Data from LaunchBox Games Database' },
      }),
    },
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const beforeOverview = catalogListGets(calls);
  await waitForCondition(
    () => {
      const card = gameCards(document)[0];
      return cardStudio(card) && cardStudio(card).textContent === 'SEGA · 1 player';
    },
    'prefetch did not apply studio to the Cover card',
  );
  const cover = gameCards(document)[0];
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /Host-local presentation|Provider summary|SEGA|1 player|Beat 'em Up|Action/);
  assert.doesNotMatch(cardStudio(cover).textContent, /Host-local presentation|Provider summary|1991|Beat 'em Up|Action/);
  assert.equal(catalogListGets(calls), beforeOverview);
  assert.equal(calls.filter(call => String(call.path).includes('/api/v1/presentation/games/')).length, 1);
  await cover.click();
  await settleBrowser();
  await waitForCondition(
    () => detailSummaryFrom(document) && detailSummaryFrom(document).textContent === 'Host-local presentation metadata for ActRaiser.',
    'prefetch did not apply ready overview to detail',
  );
  assert.equal(detailSummaryFrom(document).textContent, 'Host-local presentation metadata for ActRaiser.');
  assert.equal(detailFallbackNoteFrom(document), null);
  assert.doesNotMatch(browserText(document.nodes.get('detail-content')), /Using local catalog data|deterministic demo/);
  assert.equal(catalogListGets(calls), beforeOverview);
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Beat \'em Up · SEGA · 1 player · Beta · Weekend Queue · Ready');
  assert.doesNotMatch(body.children[1].textContent, /Host-local presentation|Provider summary/);
  assert.equal(catalogListGets(calls), beforeOverview);
});

test('WALL_WINDOW spacer math and marks clearance stay unchanged with catalogSummary', () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /marks\.offsetHeight/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(app, /disconnectCardMarksObservers/);
  assert.doesNotMatch(css, /\.card-mark-summary/);
  assert.doesNotMatch(app, /\.card-mark-summary/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
});

test('Cover and Home idle titles stay visible with a persistent scrim', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /card\.appendChild\(element\('h3', '', cardTitle\(game\)\)\);/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card h3\s*\{[^}]*white-space:\s*nowrap/);
  assert.match(css, /\.game-card h3\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.doesNotMatch(css, /\.game-card::after\s*\{[^}]*opacity:\s*0/);
  assert.doesNotMatch(css, /\.game-card::after\s*\{[^}]*transition:\s*background/);
  assert.doesNotMatch(css, /\.game-card:hover::after/);
  assert.match(css, /\.game-card::before\s*\{[^}]*opacity:\s*0/);
  assert.match(css, /\.game-card::before\s*\{[^}]*transition:\s*opacity/);
  assert.match(css, /\.game-card:hover::before, \.game-card\.selected::before, \.game-card:focus-visible::before\s*\{[^}]*opacity:\s*1/);
  assert.doesNotMatch(css, /\.game-card:hover h3/);
  assert.doesNotMatch(css, /\.game-card\.selected h3,/);
  assert.doesNotMatch(css, /\.game-card:focus-visible h3/);
  assert.match(css, /\.game-card \.game-meta, \.game-card \.fallback-note, \.game-card \.game-studio\s*\{[^}]*opacity:\s*0/);
  assert.match(css, /\.game-card:hover \.game-meta/);
  assert.match(css, /\.game-card:hover \.fallback-note/);
  assert.match(css, /\.game-card:hover \.game-studio/);
  assert.doesNotMatch(css, /\.card-mark-title/);
  assert.match(css, /\.card-mark\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /\.home-rail-track \.game-card\s*\{[^}]*width:\s*148px/);
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);

  const ready = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const fallback = availableGame('snes-fallback-test', 'Fallback Game (USA)', {
    year: '1992',
    region: 'usa',
  });
  const { document } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === fallback.id) {
          return {
            summary: 'Fallback summary',
            year: '—',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyStudioAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [ready, fallback] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[ready.id];
  const fallbackCard = byID[fallback.id];
  assert.equal(cover.className.includes('selected'), false);
  assert.equal(cardTitle(ready), 'ActRaiser');
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'ActRaiser');
  assert.doesNotMatch(cover.children.find(child => child.tagName === 'H3').textContent, /\(USA\)/);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beta|Weekend Queue|1 player|Fallback summary/);
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.doesNotMatch(cardStudio(cover).textContent, /Action|Platformer|Fallback summary/);
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.some(child => /card-mark-title/.test(String(child.className))), false);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(fallbackCard.children.find(child => child.tagName === 'H3').textContent, cardTitle(fallback));
  assert.equal(cardStudio(fallbackCard), null);
  assert.equal(fallbackCard.children.filter(child => child.className === 'fallback-note').length, 1);
  assert.equal(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, 'Using local catalog data');
  assert.equal(fallbackCard.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1992 · Ready');
});

test('Cover hover deepen eases via opacity not a gradient swap', () => {
  const css = readAsset('ui.css');
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*rgba\(8, 10, 14, \.68\)/);
  assert.doesNotMatch(css, /\.game-card::after\s*\{[^}]*opacity:\s*0/);
  assert.doesNotMatch(css, /\.game-card::after\s*\{[^}]*transition:\s*background/);
  assert.doesNotMatch(css, /\.game-card:hover::after/);
  assert.doesNotMatch(css, /\.game-card\.selected::after/);
  assert.doesNotMatch(css, /\.game-card:focus-visible::after/);
  assert.match(css, /\.game-card::before\s*\{[^}]*opacity:\s*0/);
  assert.match(css, /\.game-card::before\s*\{[^}]*transition:\s*opacity\s*\.18s/);
  assert.match(css, /\.game-card::before\s*\{[^}]*rgba\(8, 10, 14, \.92\)/);
  assert.match(css, /\.game-card:hover::before, \.game-card\.selected::before, \.game-card:focus-visible::before\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card\.game-row::before, \.game-card\.game-row::after\s*\{[^}]*content:\s*none/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card \.game-meta, \.game-card \.fallback-note, \.game-card \.game-studio\s*\{[^}]*opacity:\s*0/);
  assert.match(css, /\.game-card:hover \.game-meta/);
  assert.match(css, /\.game-card:hover \.fallback-note/);
  assert.match(css, /\.game-card:hover \.game-studio/);
});

test('Cover hover XOR stays system region year status with studio XOR fallback-note', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /\.game-card:hover \.game-meta/);
  assert.match(css, /\.game-card\.selected \.game-meta/);
  assert.match(css, /\.game-card:focus-visible \.game-meta/);
  assert.match(css, /\.game-card:hover \.fallback-note/);
  assert.match(css, /\.game-card\.selected \.fallback-note/);
  assert.match(css, /\.game-card:focus-visible \.fallback-note/);
  assert.match(css, /\.game-card:hover \.game-studio/);
  assert.match(css, /\.game-card\.selected \.game-studio/);
  assert.match(css, /\.game-card:focus-visible \.game-studio/);
  assert.match(css, /\.game-card \.game-meta, \.game-card \.fallback-note, \.game-card \.game-studio\s*\{[^}]*opacity:\s*0/);
  assert.doesNotMatch(css, /\.card-mark-title/);
  assert.doesNotMatch(css, /\.game-card:hover h3/);

  const ready = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    variant_count: 3,
    collections: ['weekend-queue'],
  });
  const fallback = availableGame('snes-fallback-test', 'Fallback Game', {
    year: '1992',
    region: 'usa',
  });
  const { document, calls } = await runBrowserApp({
    adapter: {
      metadataFor(game) {
        if (game && game.id === fallback.id) {
          return {
            summary: 'Fallback Game is a deterministic demo presentation for SNES.',
            year: '—',
            genre: 'Platformer',
            studio: 'SEGA',
            players: '1 player',
            isFallback: true,
            metadataState: 'fallback_offline',
            cover: { palette: 'lagoon', treatment: 'rings' },
            backdrop: { palette: 'sunset', treatment: 'waves' },
          };
        }
        return readyGenreAdapter().metadataFor(game);
      },
    },
    responses: [jsonResponse({ games: [ready, fallback] }), jsonResponse(ready)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[ready.id];
  const fallbackCard = byID[fallback.id];
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'ActRaiser');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Beat 'em Up|Beta|Weekend Queue|1 player|Provider summary|deterministic demo/);
  assert.equal(cardStudio(cover).textContent, 'SEGA · 1 player');
  assert.doesNotMatch(cardStudio(cover).textContent, /Action|Beat 'em Up|Platformer|Provider summary/);
  assert.equal(cover.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cover.children.find(child => child.className === 'game-meta game-meta-variant').textContent, '3 versions');
  assert.equal(cover.children.some(child => /card-mark-title/.test(String(child.className))), false);
  assert.equal(cardStudio(fallbackCard), null);
  assert.equal(fallbackCard.children.filter(child => child.className === 'fallback-note').length, 1);
  assert.equal(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, 'Using local catalog data');
  assert.doesNotMatch(fallbackCard.children.find(child => child.className === 'fallback-note').textContent, /deterministic demo|Platformer/);
  await cover.click();
  await settleBrowser();
  assert.ok(gameCards(document)[0].className.includes('selected'));
  assert.equal(gameCards(document)[0].children.find(child => child.tagName === 'H3').textContent, 'ActRaiser');
  assert.equal(cardStudio(gameCards(document)[0]).textContent, 'SEGA · 1 player');
  assert.equal(calls.some(call => String(call.path).includes('/api/v1/presentation/')), false);
});

test('Home rails keep idle titles at 148px with nowrap ellipsis', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /\.home-rail-track \.game-card\s*\{[^}]*width:\s*148px/);
  assert.match(css, /\.game-card h3\s*\{[^}]*white-space:\s*nowrap/);
  assert.match(css, /\.game-card h3\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue'],
  });
  const { document } = await runBrowserApp({
    keepHome: true,
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [queued] })],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const card = rail.querySelectorAll('.game-card')[0];
  assert.equal(card.offsetWidth, 148);
  assert.equal(card.children.find(child => child.tagName === 'H3').textContent, 'ActRaiser');
  assert.doesNotMatch(card.children.find(child => child.tagName === 'H3').textContent, /\(USA\)/);
  assert.equal(card.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(card.children.find(child => child.className === 'game-meta').textContent, /SEGA|Action|Weekend Queue|1 player/);
  assert.equal(cardStudio(card).textContent, 'SEGA · 1 player');
  assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 0);
  assert.equal(cardMark(card, 'favorite').textContent, 'Favorite');
  assert.equal(cardCollectionChip(card).textContent, 'Weekend Queue');
  const homeMarks = card.children.find(child => child.className === 'card-marks');
  const homeClearance = parseFloat(card.style['--card-marks-clearance']);
  const oneRow = 22;
  assert.ok(homeMarks.offsetHeight > oneRow, `148px Home marks should wrap, height=${homeMarks.offsetHeight}`);
  assert.ok(homeClearance > oneRow, `148px Home clearance should exceed one row, --card-marks-clearance=${homeClearance}`);
  assert.equal(homeClearance, homeMarks.offsetHeight);
});

test('List titles stay visible at 72px without a region line', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /\.game-card\.game-row h3,\s*\.game-card\.game-row \.game-meta,\s*\.game-card\.game-row \.game-row-favorite\s*\{[^}]*opacity:\s*1/);
  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    collections: ['weekend-queue', 'speedruns'],
    favorite: true,
    variant_count: 3,
  });
  const plain = availableGame('snes-zelda-test', 'Zelda', { year: '1992' });
  const { document } = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [queued, plain] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const row = byID[queued.id];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[0].tagName, 'H3');
  assert.equal(body.children[0].textContent, 'ActRaiser');
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'SNES · 1991 · Action · SEGA · 1 player · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA/);
  assert.equal(body.children[2].className, 'game-row-favorite');
  assert.equal(body.children[3].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 4);
  assert.equal(body.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  assert.equal(cardStudio(row), null);
  assert.equal(row.children.some(child => String(child.className).includes('card-marks')), false);
  const plainBody = byID[plain.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(plainBody.children[0].textContent, 'Zelda');
  assert.equal(plainBody.children[1].textContent, 'SNES · 1992 · Action · SEGA · 1 player · Ready');
  assert.equal(plainBody.children.length, 2);
});

test('grouped wall cards use the representative cardTitle only', async () => {
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    canonical_title: 'Sonic the Hedgehog',
    system: 'megadrive',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
    genre: 'Platform',
  });
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  assert.equal(cardTitle(grouped), 'Sonic the Hedgehog');
  const { document } = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.doesNotMatch(cover.children.find(child => child.tagName === 'H3').textContent, /USA|Japan|\(USA\)/);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[0].textContent, 'Sonic the Hedgehog');
  assert.doesNotMatch(body.children[0].textContent, /USA|Japan/);
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Beat \'em Up · SEGA · 1 player · Ready');
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
});

test('WALL_WINDOW spacer math and marks clearance stay unchanged with idle titles', () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /marks\.offsetHeight/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(app, /disconnectCardMarksObservers/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.doesNotMatch(css, /\.card-mark-title/);
  assert.doesNotMatch(app, /\.card-mark-title/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
});

test('detail facts show Source ZIP or ROM and omit unknown kind', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /function sourceKindLabel\(game\)/);
  assert.match(app, /\[sourceKindLabel\(liveGame \|\| game\), 'Source'\]/);
  assert.match(app, /\[coverStatusLabel\(detailGame\), 'Status'\]/);
  assert.doesNotMatch(app, /'Staging'/);
  assert.doesNotMatch(app, /'Prepared'/);
  assert.doesNotMatch(app, /'On demand'/);
  assert.doesNotMatch(app, /, 'Content'\]/);
  assert.match(app, /source\.content_prepared/);
  assert.match(app, /content_prepared: contentPrepared/);
  assert.doesNotMatch(app, /game\.content_prepared/);
  assert.doesNotMatch(app, /game\.execution/);
  assert.match(app, /coverHoverMeta\(game, view\)/);
  assert.doesNotMatch(app, /bits\.push\(sourceKindLabel/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::before\s*\{[^}]*opacity:\s*0/);
  assert.match(css, /\.game-card:hover::before, \.game-card\.selected::before, \.game-card:focus-visible::before\s*\{[^}]*opacity:\s*1/);
  assert.doesNotMatch(css, /\.card-mark-kind/);
  assert.doesNotMatch(app, /\.card-mark-kind/);

  const zip = availableGame('megadrive-sonic-test', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    kind: 'zip',
    canonical_title: 'Sonic the Hedgehog',
    year: '1991',
    genre: 'Platform',
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
    favorite: true,
    collections: ['weekend-queue'],
    variant_count: 2,
    group_key: 'megadrive\u001fsonic',
  });
  const raw = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    kind: 'raw',
    year: '1991',
    genre: 'Action',
    region: 'usa',
    collections: ['weekend-queue'],
  });
  const unknown = availableGame('snes-unknown-kind-test', 'Unknown Kind', {
    kind: 'iso',
    year: '1992',
    region: 'usa',
  });
  const onDemand = availableGame('snes-ondemand-test', 'On Demand Dump', {
    kind: 'raw',
    content_prepared: false,
    region: 'usa',
  });
  const { document, calls } = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [
      jsonResponse({ games: [zip, raw, unknown, onDemand] }),
      jsonResponse(zip),
    ],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(document).map(card => [card.attributes.get('data-game-id'), card]));
  const cover = byID[zip.id];
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw|Staging|Prepared|On demand/);
  assert.equal(cardMark(cover, 'favorite').textContent, 'Favorite');
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cover.children.some(child => /card-mark-kind/.test(String(child.className))), false);

  await cover.click();
  await settleBrowser();
  const requestRoots = [...new Set(calls.map(call => call.path.split('?')[0]))];
  assert.equal(requestRoots.every(path => (
    path === '/api/v1/games'
    || path.startsWith('/api/v1/games/')
    || path.startsWith('/api/v1/presentation/games/')
  )), true);
  const zipFacts = detailFactsFrom(document);
  assert.ok(zipFacts.some(fact => fact.label === 'Source' && fact.value === 'ZIP'));
  assert.equal(zipFacts.filter(fact => fact.label === 'Source').length, 1);
  assert.ok(zipFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assertOmitsStagingAndExecution(zipFacts);
  assert.ok(zipFacts.some(fact => fact.label === 'Region' && fact.value === 'USA'));
  assert.ok(zipFacts.some(fact => fact.label === 'Revision' && fact.value === 'rev a'));
  assert.ok(zipFacts.some(fact => fact.label === 'Flags' && fact.value === 'Beta'));
  assert.doesNotMatch(zipFacts.map(fact => fact.value).join(' '), /\bzip\b/);

  const rawApp = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [raw] }), jsonResponse(raw)],
    collections: [WEEKEND_QUEUE],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(rawApp.document)[0].click();
  await settleBrowser();
  const rawFacts = detailFactsFrom(rawApp.document);
  assert.ok(rawFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assert.ok(rawFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assertOmitsStagingAndExecution(rawFacts);
  assert.equal(rawFacts.some(fact => fact.value === 'zip' || fact.value === 'raw'), false);

  const unknownApp = await runBrowserApp({
    adapter: readyGenreAdapter(),
    responses: [jsonResponse({ games: [unknown] }), jsonResponse(unknown)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(unknownApp.document)[0].click();
  await settleBrowser();
  const unknownFacts = detailFactsFrom(unknownApp.document);
  assert.equal(unknownFacts.some(fact => fact.label === 'Source'), false);
  assert.equal(unknownFacts.some(fact => /ZIP|ROM|iso|zip|raw/.test(fact.value)), false);
  assert.ok(unknownFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assertOmitsStagingAndExecution(unknownFacts);

  const demandApp = await runBrowserApp({
    responses: [jsonResponse({ games: [onDemand] }), jsonResponse(onDemand)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(demandApp.document)[0].click();
  await settleBrowser();
  const demandFacts = detailFactsFrom(demandApp.document);
  assert.ok(demandFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assert.ok(demandFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assertOmitsStagingAndExecution(demandFacts);

  const offline = availableGame('snes-offline-staging-test', 'Offline Dump', {
    kind: 'zip',
    state: 'available',
    root_online: false,
  });
  const offlineApp = await runBrowserApp({
    responses: [jsonResponse({ games: [offline] }), jsonResponse(offline)],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await gameCards(offlineApp.document)[0].click();
  await settleBrowser();
  const offlineFacts = detailFactsFrom(offlineApp.document);
  assert.ok(offlineFacts.some(fact => fact.label === 'Status' && fact.value === 'Offline'));
  assert.ok(offlineFacts.some(fact => fact.label === 'Source' && fact.value === 'ZIP'));
  assertOmitsStagingAndExecution(offlineFacts);

  const playing = availableGame('snes-playing-staging-test', 'Playing Dump', { kind: 'raw' });
  const playingApp = await runBrowserApp({
    responses: [jsonResponse({ games: [playing] }), jsonResponse(playing)],
    sessionResponses: [jsonResponse(sessionFixture({
      state: 'active',
      game_id: playing.id,
      system: 'snes',
    }))],
  });
  await settleBrowser();
  assert.equal(cardMark(gameCards(playingApp.document)[0], 'playing').textContent, 'Playing');
  await gameCards(playingApp.document)[0].click();
  await settleBrowser();
  const playingFacts = detailFactsFrom(playingApp.document);
  assert.ok(playingFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assert.equal(playingFacts.some(fact => fact.label === 'Status' && /Playing/.test(fact.value)), false);
  assert.ok(playingFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assertOmitsStagingAndExecution(playingFacts);
});

test('grouped ZIP+raw version select shows the selected variant Source before detail hydrate', async () => {
  const zip = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    kind: 'zip',
    canonical_title: 'Sonic the Hedgehog',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
  });
  const raw = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    kind: 'raw',
    canonical_title: 'Sonic the Hedgehog',
    group_key: 'megadrive\u001fsonic',
    region: 'japan',
    year: '1991',
  });
  const grouped = { ...zip };
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  assert.equal(sourceKindLabel(zip), 'ZIP');
  assert.equal(sourceKindLabel(raw), 'ROM');
  assert.equal(sourceKindLabel({ kind: 'iso' }), '');

  let releaseVariantDetail;
  const heldVariantDetail = new Promise(resolve => {
    releaseVariantDetail = () => resolve(jsonResponse({
      code: 'INTERNAL',
      message: 'detail is unavailable',
    }, 500));
  });
  const { document } = await runBrowserApp({
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...zip, variants: [zip, raw] }),
      heldVariantDetail,
    ],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw|Staging|Prepared|On demand/);
  assert.equal(cover.children.some(child => /card-mark-kind/.test(String(child.className))), false);

  await cover.click();
  await settleBrowser();
  const zipFacts = detailFactsFrom(document);
  assert.ok(zipFacts.some(fact => fact.label === 'Source' && fact.value === 'ZIP'));
  assert.ok(zipFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assertOmitsStagingAndExecution(zipFacts);
  const select = document.getElementById('game-version');
  assert.ok(select);
  select.value = raw.id;
  const selecting = select.dispatchEvent({ type: 'change' });
  await settleBrowser();
  const pendingFacts = detailFactsFrom(document);
  assert.ok(pendingFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assert.equal(pendingFacts.filter(fact => fact.label === 'Source').length, 1);
  assert.ok(pendingFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assertOmitsStagingAndExecution(pendingFacts);
  assert.equal(pendingFacts.some(fact => fact.value === 'zip' || fact.value === 'raw'), false);
  releaseVariantDetail();
  await selecting;
  await settleBrowser();
  const failedFacts = detailFactsFrom(document);
  assert.ok(failedFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assert.ok(failedFacts.some(fact => fact.label === 'Status' && fact.value === 'Ready'));
  assertOmitsStagingAndExecution(failedFacts);
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw/);
});

test('Cover hover Home rails and List omit source kind', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.home-rail-track \.game-card\s*\{[^}]*width:\s*148px/);

  const queued = availableGame('snes-actraiser-test', 'ActRaiser (USA)', {
    canonical_title: 'ActRaiser',
    kind: 'zip',
    year: '1991',
    genre: 'Action',
    dump_flags: 'beta',
    region: 'usa',
    favorite: true,
    collections: ['weekend-queue', 'speedruns'],
    variant_count: 3,
  });
  const plain = availableGame('snes-zelda-test', 'Zelda', {
    kind: 'raw',
    year: '1992',
  });
  const grouped = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    canonical_title: 'Sonic the Hedgehog',
    system: 'megadrive',
    kind: 'zip',
    group_key: 'megadrive\u001fsonic',
    variant_count: 2,
    region: 'usa',
    year: '1991',
    genre: 'Platform',
  });
  const coverApp = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [queued, plain, grouped] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const byID = Object.fromEntries(gameCards(coverApp.document).map(card => [card.attributes.get('data-game-id'), card]));
  assert.equal(byID[queued.id].children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(byID[queued.id].children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw|SEGA|Action|Beta|Staging|Prepared|On demand/);
  assert.equal(byID[grouped.id].children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(byID[grouped.id].children.find(child => child.className === 'game-meta').textContent, 'Mega Drive · USA · 1991 · Ready');
  assert.doesNotMatch(byID[grouped.id].children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw|Staging|Prepared|On demand/);

  const homeApp = await runBrowserApp({
    keepHome: true,
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [queued] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [queued] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(homeApp.document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const homeCard = rail.querySelectorAll('.game-card')[0];
  assert.equal(homeCard.children.find(child => child.className === 'game-meta').textContent, 'SNES · USA · 1991 · Ready');
  assert.doesNotMatch(homeCard.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw|Staging|Prepared|On demand/);
  assert.equal(homeCard.children.find(child => child.tagName === 'H3').textContent, 'ActRaiser');
  assert.equal(cardMark(homeCard, 'favorite').textContent, 'Favorite');
  assert.equal(cardCollectionChip(homeCard).textContent, 'Weekend Queue');

  const listApp = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [queued, plain, grouped] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  listApp.document.nodes.get('layout-list').click();
  await settleBrowser();
  const listByID = Object.fromEntries(gameCards(listApp.document).map(card => [card.attributes.get('data-game-id'), card]));
  const queuedBody = listByID[queued.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(queuedBody.children[1].className, 'game-meta');
  assert.equal(queuedBody.children[1].textContent, 'SNES · 1991 · Action · SEGA · 1 player · Beta · Weekend Queue · +1 · Ready');
  assert.doesNotMatch(queuedBody.children[1].textContent, /USA|ZIP|ROM|zip|raw|Staging|Prepared|On demand/);
  assert.equal(queuedBody.children[2].className, 'game-row-favorite');
  assert.equal(queuedBody.children[3].className, 'game-meta game-meta-variant');
  assert.equal(queuedBody.children.length, 4);
  assert.equal(queuedBody.children.filter(child => String(child.className).includes('game-meta')).length, 2);
  const groupedBody = listByID[grouped.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(groupedBody.children[0].textContent, 'Sonic the Hedgehog');
  assert.equal(groupedBody.children[1].textContent, 'Mega Drive · 1991 · Action · SEGA · 1 player · Ready');
  assert.doesNotMatch(groupedBody.children[1].textContent, /ZIP|ROM|zip|raw|USA|Staging|Prepared|On demand/);
  assert.equal(groupedBody.children[2].className, 'game-meta game-meta-variant');
  assert.equal(groupedBody.children.length, 3);
  const plainBody = listByID[plain.id].children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(plainBody.children[1].textContent, 'SNES · 1992 · Action · SEGA · 1 player · Ready');
  assert.doesNotMatch(plainBody.children[1].textContent, /ZIP|ROM|zip|raw|Staging|Prepared|On demand/);
  assert.equal(plainBody.children.length, 2);
});

test('WALL_WINDOW spacer math and marks clearance stay unchanged with source kind', () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /rows \* Math\.round\(metrics\.cardWidth \* 1\.5 \+ metrics\.gap\)/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /marks\.offsetHeight/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(app, /disconnectCardMarksObservers/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::before\s*\{[^}]*opacity:\s*0/);
  assert.doesNotMatch(css, /\.card-mark-kind/);
  assert.doesNotMatch(app, /\.card-mark-kind/);
  assert.doesNotMatch(css, /\.game-studio[^{]*\{[^}]*filter:/);
  assert.doesNotMatch(app, /'Staging'/);
});

test('snapshotLiveGame still copies content_prepared without painting Staging', async () => {
  const prepared = availableGame('snes-prepared-snap', 'Prepared Snap', { content_prepared: true });
  const demand = availableGame('snes-demand-snap', 'Demand Snap', { content_prepared: false });
  const { calls, fetchImpl } = queuedFetch([
    jsonResponse({ games: [prepared, demand] }),
  ]);
  const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
  await controller.loadCatalog('');
  const state = controller.getState();
  assert.equal(state.games.find(game => game.id === prepared.id).content_prepared, true);
  assert.equal(state.games.find(game => game.id === demand.id).content_prepared, false);
  assert.equal(state.games.find(game => game.id === prepared.id).execution, 'fpga_native');
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/games?grouped=1']);
});

function assertDetailDumpIdentity(facts, expected) {
  assert.deepEqual(facts.filter(fact => ['Region', 'Revision', 'Flags'].includes(fact.label)), [
    { value: expected.region, label: 'Region' },
    { value: expected.revision, label: 'Revision' },
    { value: expected.flags, label: 'Flags' },
  ]);
  assert.ok(facts.some(fact => fact.label === 'Status' && fact.value === expected.status));
  assert.equal(facts.some(fact => fact.label === 'Status' && /Playing/.test(fact.value)), false);
  if (expected.source) {
    assert.ok(facts.some(fact => fact.label === 'Source' && fact.value === expected.source));
    assert.equal(facts.filter(fact => fact.label === 'Source').length, 1);
  }
  if (expected.collections) {
    assert.ok(facts.some(fact => fact.label === 'Collections' && fact.value === expected.collections));
  } else {
    assert.equal(facts.some(fact => fact.label === 'Collections'), false);
  }
  assertOmitsStagingAndExecution(facts);
}

function assertDetailCollectionMembership(document, game, collections, ready = true) {
  const facts = detailFactsFrom(document);
  if (ready) {
    const labels = collectionLabels(game, collections);
    if (labels.length) {
      assert.ok(facts.some(fact => fact.label === 'Collections' && fact.value === labels.join(', ')));
    } else {
      assert.equal(facts.some(fact => fact.label === 'Collections'), false);
    }
  }
  (collections || []).forEach(collection => {
    const toggle = document.getElementById(`collection-member-${collection.id}`);
    assert.ok(toggle);
    assert.equal(toggle.getAttribute('data-collection'), collection.id);
    if (!ready) {
      assert.equal(toggle.disabled, true);
      assert.doesNotMatch(toggle.textContent, /^(Add to|Remove from)\b/);
      assert.equal(toggle.getAttribute('data-game-id'), null);
      return;
    }
    const member = Array.isArray(game.collections) && game.collections.includes(collection.id);
    assert.equal(toggle.textContent, member ? `Remove from ${collection.name}` : `Add to ${collection.name}`);
    assert.equal(toggle.getAttribute('data-game-id'), game.id);
    assert.equal(toggle.disabled, false);
  });
}

test('detail dump identity Status and Collections follow the selected variant before and after hydrate', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const detailGame = liveGame \|\| game/);
  assert.match(app, /dumpIdentityFacts\(detailGame\)/);
  assert.match(app, /\[coverStatusLabel\(detailGame\), 'Status'\]/);
  assert.match(app, /collectionLabels\(detailGame, state\.collections\)/);
  assert.match(app, /const membershipReady = Boolean\(/);
  assert.match(app, /game\.id === detailGame\.id/);
  assert.match(app, /state\.detailState === 'populated'/);
  assert.match(app, /item\.id === detailGame\.id/);
  assert.match(app, /const membership = membershipReady/);
  assert.match(app, /Array\.isArray\(detailGame\.collections\) \? detailGame\.collections : \[\]/);
  assert.match(app, /toggle\.disabled = true/);
  assert.match(app, /toggle\.setAttribute\('data-game-id', detailGame\.id\)/);
  assert.match(app, /controller\.toggleCollectionMember\(collection\.id, detailGame\.id\)/);
  assert.match(app, /\[sourceKindLabel\(liveGame \|\| game\), 'Source'\]/);
  assert.doesNotMatch(app, /\[coverStatusLabel\(game\), 'Status'\]/);
  assert.doesNotMatch(app, /dumpIdentityFacts\(game\)\.forEach/);
  assert.doesNotMatch(app, /const membership = Array\.isArray\(detailGame\.collections\) \? detailGame\.collections : \[\]/);
  assert.doesNotMatch(app, /const membership = Array\.isArray\(game\.collections\) \? game\.collections : \[\]/);
  assert.doesNotMatch(app, /'Staging'/);
  assert.doesNotMatch(app, /'Prepared'/);
  assert.doesNotMatch(app, /'On demand'/);
  assert.match(app, /bits\.push\(coverStatusLabel\(game\)\)/);
  assert.match(app, /coverHoverMeta\(game, view\)/);
  assert.match(app, /function collectionLabels\(game/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.doesNotMatch(css, /\.card-mark-revision/);
  assert.doesNotMatch(app, /\.card-mark-revision/);

  const groupKey = 'megadrive\u001fsonic';
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    kind: 'zip',
    canonical_title: 'Sonic the Hedgehog',
    group_key: groupKey,
    variant_count: 3,
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
    year: '1991',
    collections: ['weekend-queue'],
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    kind: 'raw',
    canonical_title: 'Sonic the Hedgehog',
    group_key: groupKey,
    region: 'japan',
    revision: 'c',
    dump_flags: 'hack',
    year: '1991',
    state: 'available',
    root_online: false,
    collections: ['speedruns'],
  });
  const europe = availableGame('megadrive-sonic-europe', 'Sonic the Hedgehog (Europe)', {
    system: 'megadrive',
    kind: 'zip',
    canonical_title: 'Sonic the Hedgehog',
    group_key: groupKey,
    region: 'europe',
    revision: '00',
    dump_flags: 'proto',
    year: '1991',
    state: 'invalid',
    root_online: true,
    collections: [],
  });
  const grouped = { ...usa };
  assert.equal(Object.prototype.hasOwnProperty.call(grouped, 'variants'), false);
  assert.equal(coverStatusLabel(usa), 'Ready');
  assert.equal(coverStatusLabel(japan), 'Offline');
  assert.equal(coverStatusLabel(europe), 'Unreadable');
  assert.deepEqual(collectionLabels(usa, [WEEKEND_QUEUE, SPEEDRUNS]), ['Weekend Queue']);
  assert.deepEqual(collectionLabels(japan, [WEEKEND_QUEUE, SPEEDRUNS]), ['Speedruns']);
  assert.deepEqual(collectionLabels(europe, [WEEKEND_QUEUE, SPEEDRUNS]), []);
  assert.equal(sourceKindLabel(usa), 'ZIP');
  assert.equal(sourceKindLabel(japan), 'ROM');

  const representativeHover = 'Mega Drive · USA · 1991 · Ready';
  const usaFacts = {
    region: 'USA', revision: 'rev a', flags: 'Beta', status: 'Ready', source: 'ZIP', collections: 'Weekend Queue',
  };
  const japanFacts = {
    region: 'Japan', revision: 'rev c', flags: 'Hack', status: 'Offline', source: 'ROM', collections: 'Speedruns',
  };
  const japanPendingFacts = {
    region: 'Japan', revision: 'rev c', flags: 'Hack', status: 'Offline', source: 'ROM',
  };
  const europeFacts = {
    region: 'Europe', revision: 'rev 00', flags: 'Proto', status: 'Unreadable', source: 'ZIP',
  };
  const publicVariant = game => {
    const next = { ...game };
    delete next.collections;
    delete next.favorite;
    return next;
  };
  const nestedVariants = [usa, japan, europe].map(publicVariant);

  let releaseJapanDetail;
  const heldJapanDetail = new Promise(resolve => {
    releaseJapanDetail = () => resolve(jsonResponse({
      code: 'INTERNAL',
      message: 'detail is unavailable',
    }, 500));
  });
  const pendingApp = await runBrowserApp({
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...usa, variants: nestedVariants }),
      heldJapanDetail,
    ],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const pendingCover = gameCards(pendingApp.document)[0];
  assert.equal(pendingCover.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(pendingCover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.doesNotMatch(pendingCover.children.find(child => child.className === 'game-meta').textContent, /Japan|Europe|Hack|Proto|Offline|Unreadable|ZIP|ROM|Weekend Queue|Speedruns/);

  await pendingCover.click();
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(pendingApp.document), usaFacts);
  assertDetailCollectionMembership(pendingApp.document, usa, [WEEKEND_QUEUE, SPEEDRUNS]);
  const pendingSelect = pendingApp.document.getElementById('game-version');
  assert.ok(pendingSelect);
  pendingSelect.value = japan.id;
  const selectingJapan = pendingSelect.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(pendingApp.document), japanPendingFacts);
  assertDetailCollectionMembership(pendingApp.document, japan, [WEEKEND_QUEUE, SPEEDRUNS], false);
  pendingApp.document.getElementById('collection-member-speedruns').click();
  await settleBrowser();
  assertDetailCollectionMembership(pendingApp.document, japan, [WEEKEND_QUEUE, SPEEDRUNS], false);
  assert.equal(pendingCover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.equal(cardCollectionChip(pendingCover).textContent, 'Weekend Queue');
  releaseJapanDetail();
  await selectingJapan;
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(pendingApp.document), japanPendingFacts);
  assertDetailCollectionMembership(pendingApp.document, japan, [WEEKEND_QUEUE, SPEEDRUNS], false);
  pendingApp.document.getElementById('collection-member-speedruns').click();
  await settleBrowser();
  assertDetailCollectionMembership(pendingApp.document, japan, [WEEKEND_QUEUE, SPEEDRUNS], false);
  assert.equal(pendingCover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.equal(cardCollectionChip(pendingCover).textContent, 'Weekend Queue');
  assert.equal(cardCollectionMore(pendingCover), null);

  const hydratedJapan = {
    ...japan,
    revision: '01',
    dump_flags: 'hack',
  };
  const hydratedApp = await runBrowserApp({
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...usa, variants: nestedVariants }),
      jsonResponse(hydratedJapan),
    ],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  await gameCards(hydratedApp.document)[0].click();
  await settleBrowser();
  const hydrateSelect = hydratedApp.document.getElementById('game-version');
  hydrateSelect.value = japan.id;
  hydrateSelect.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(hydratedApp.document), {
    ...japanFacts,
    revision: 'rev 01',
  });
  assertDetailCollectionMembership(hydratedApp.document, {
    ...hydratedJapan,
    collections: japan.collections,
  }, [WEEKEND_QUEUE, SPEEDRUNS]);
  hydratedApp.document.getElementById('collection-member-speedruns').click();
  await settleBrowser();
  assertDetailCollectionMembership(hydratedApp.document, {
    ...hydratedJapan,
    collections: [],
  }, [WEEKEND_QUEUE, SPEEDRUNS]);
  assert.equal(
    hydratedApp.document.getElementById('collection-member-speedruns').getAttribute('data-game-id'),
    japan.id,
  );
  assert.equal(
    gameCards(hydratedApp.document)[0].children.find(child => child.className === 'game-meta').textContent,
    representativeHover,
  );
  assert.equal(cardCollectionChip(gameCards(hydratedApp.document)[0]).textContent, 'Weekend Queue');

  const europeApp = await runBrowserApp({
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...usa, variants: nestedVariants }),
      jsonResponse(europe),
    ],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  await gameCards(europeApp.document)[0].click();
  await settleBrowser();
  const europeSelect = europeApp.document.getElementById('game-version');
  europeSelect.value = europe.id;
  europeSelect.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(europeApp.document), europeFacts);
  assertDetailCollectionMembership(europeApp.document, europe, [WEEKEND_QUEUE, SPEEDRUNS]);

  const playingApp = await runBrowserApp({
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...usa, variants: nestedVariants }),
      jsonResponse(japan),
    ],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse(sessionFixture({
      state: 'active',
      game_id: usa.id,
      system: 'megadrive',
    }))],
  });
  await settleBrowser();
  const playingCover = gameCards(playingApp.document)[0];
  assert.equal(cardMark(playingCover, 'playing').textContent, 'Playing');
  await playingCover.click();
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(playingApp.document), usaFacts);
  assertDetailCollectionMembership(playingApp.document, usa, [WEEKEND_QUEUE, SPEEDRUNS]);
  const playingSelect = playingApp.document.getElementById('game-version');
  playingSelect.value = japan.id;
  playingSelect.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(playingApp.document), japanFacts);
  assertDetailCollectionMembership(playingApp.document, japan, [WEEKEND_QUEUE, SPEEDRUNS]);
  assert.equal(cardMark(playingCover, 'playing').textContent, 'Playing');
  assert.equal(cardCollectionChip(playingCover).textContent, 'Weekend Queue');
});

test('Cover hover Home rails and List keep representative dump identity', async () => {
  const css = readAsset('ui.css');
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.home-rail-track \.game-card\s*\{[^}]*width:\s*148px/);

  const groupKey = 'megadrive\u001fsonic';
  const usa = availableGame('megadrive-sonic-usa', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    kind: 'zip',
    canonical_title: 'Sonic the Hedgehog',
    group_key: groupKey,
    variant_count: 2,
    region: 'usa',
    revision: 'a',
    dump_flags: 'beta',
    year: '1991',
    genre: 'Platform',
    collections: ['weekend-queue'],
  });
  const japan = availableGame('megadrive-sonic-japan', 'Sonic the Hedgehog (Japan)', {
    system: 'megadrive',
    kind: 'raw',
    canonical_title: 'Sonic the Hedgehog',
    group_key: groupKey,
    region: 'japan',
    revision: 'c',
    dump_flags: 'hack',
    year: '1991',
    state: 'available',
    root_online: false,
    collections: ['speedruns'],
  });
  const grouped = { ...usa };
  const representativeHover = 'Mega Drive · USA · 1991 · Ready';

  const coverApp = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...usa, variants: [usa, japan] }),
      jsonResponse(japan),
    ],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(coverApp.document)[0];
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /Japan|Hack|Offline|ZIP|ROM|rev|Speedruns/);
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  await cover.click();
  await settleBrowser();
  const select = coverApp.document.getElementById('game-version');
  select.value = japan.id;
  select.dispatchEvent({ type: 'change' });
  await settleBrowser();
  assertDetailDumpIdentity(detailFactsFrom(coverApp.document), {
    region: 'Japan', revision: 'rev c', flags: 'Hack', status: 'Offline', source: 'ROM', collections: 'Speedruns',
  });
  assertDetailCollectionMembership(coverApp.document, japan, [WEEKEND_QUEUE, SPEEDRUNS]);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.equal(cardMark(cover, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(cover).textContent, 'Weekend Queue');
  assert.equal(cardMark(cover, 'hack'), null);
  assert.equal(cardMark(cover, 'offline'), null);

  const homeApp = await runBrowserApp({
    keepHome: true,
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [grouped] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(homeApp.document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const homeCard = rail.querySelectorAll('.game-card')[0];
  assert.equal(homeCard.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.doesNotMatch(homeCard.children.find(child => child.className === 'game-meta').textContent, /Japan|Hack|Offline|ZIP|ROM|rev|Speedruns/);
  assert.equal(homeCard.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(cardMark(homeCard, 'beta').textContent, 'Beta');
  assert.equal(cardCollectionChip(homeCard).textContent, 'Weekend Queue');

  const listApp = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  listApp.document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(listApp.document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[0].textContent, 'Sonic the Hedgehog');
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Action · SEGA · 1 player · Beta · Weekend Queue · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA|Japan|Hack|Offline|ZIP|ROM|rev|Speedruns/);
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
});

test('variantLabel distinguishes same-region dumps by title and ZIP/ROM', () => {
  const app = readAsset('ui_app.js');
  assert.match(app, /function variantLabel\(game, variants\)/);
  assert.match(app, /game\.canonical_title/);
  assert.match(app, /title !== canonical/);
  assert.match(app, /const kind = sourceKindLabel\(game\)/);
  assert.match(app, /if \(kind\) parts\.push\(kind\)/);
  assert.match(app, /const option = element\('option', '', variantLabel\(variant, variants\)\)/);
  assert.match(app, /\[sourceKindLabel\(liveGame \|\| game\), 'Source'\]/);
  assert.doesNotMatch(app, /bits\.push\(sourceKindLabel/);
  assert.doesNotMatch(app, /'Staging'/);

  const zipUsa = {
    title: 'Sonic the Hedgehog (USA)',
    canonical_title: 'Sonic the Hedgehog',
    region: 'usa',
    kind: 'zip',
  };
  const rawUsa = {
    title: 'Sonic the Hedgehog (USA)',
    canonical_title: 'Sonic the Hedgehog',
    region: 'usa',
    kind: 'raw',
  };
  const altUsa = {
    title: 'Sonic the Hedgehog (USA) (Alt)',
    canonical_title: 'Sonic the Hedgehog',
    region: 'usa',
    kind: 'zip',
  };
  assert.equal(variantLabel(zipUsa), 'Sonic the Hedgehog (USA) · USA · ZIP');
  assert.equal(variantLabel(rawUsa), 'Sonic the Hedgehog (USA) · USA · ROM');
  assert.equal(variantLabel(altUsa), 'Sonic the Hedgehog (USA) (Alt) · USA · ZIP');
  assert.notEqual(variantLabel(zipUsa), variantLabel(rawUsa));
  assert.notEqual(variantLabel(zipUsa), variantLabel(altUsa));
  assert.notEqual(variantLabel(zipUsa), 'USA');
  assert.notEqual(variantLabel(rawUsa), 'USA');
  assert.equal(variantLabel({
    title: 'Sonic the Hedgehog',
    canonical_title: 'Sonic the Hedgehog',
    region: 'usa',
    kind: 'zip',
  }), 'USA · ZIP');
  assert.equal(variantLabel({
    title: '  Mystery Dump  ',
    kind: ' zip ',
  }), 'Mystery Dump · ZIP');
  assert.equal(coverHoverMeta({
    system: 'megadrive', state: 'available', region: 'usa', year: '1991', kind: 'zip',
  }), 'Mega Drive · USA · 1991 · Ready');

  const sonic = { id: 'megadrive-sonic', title: 'Sonic', canonical_title: 'Sonic', kind: 'raw' };
  const shouting = { id: 'megadrive-SONIC', title: 'SONIC', canonical_title: 'SONIC', kind: 'raw' };
  assert.equal(variantLabel(sonic), 'ROM');
  assert.equal(variantLabel(shouting), 'ROM');
  assert.equal(variantLabel(sonic, [sonic, shouting]), 'Sonic · ROM');
  assert.equal(variantLabel(shouting, [sonic, shouting]), 'SONIC · ROM');
  assert.notEqual(variantLabel(sonic, [sonic, shouting]), variantLabel(shouting, [sonic, shouting]));
  assert.notEqual(variantLabel(sonic, [sonic, shouting]), 'ROM');
  assert.notEqual(variantLabel(shouting, [sonic, shouting]), 'ROM');
  const sonicZip = { ...sonic, kind: 'zip' };
  const shoutingRaw = { ...shouting, kind: 'raw' };
  assert.equal(variantLabel(sonicZip, [sonicZip, shoutingRaw]), 'ZIP');
  assert.equal(variantLabel(shoutingRaw, [sonicZip, shoutingRaw]), 'ROM');
  const sonicUsa = { ...sonic, region: 'usa' };
  const shoutingUsa = { ...shouting, region: 'usa' };
  assert.equal(variantLabel(sonicUsa, [sonicUsa, shoutingUsa]), 'Sonic · USA · ROM');
  assert.equal(variantLabel(shoutingUsa, [sonicUsa, shoutingUsa]), 'SONIC · USA · ROM');
});

test('same-region Version options stay distinct while Cover Home List stay representative', async () => {
  const css = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  assert.match(app, /const option = element\('option', '', variantLabel\(variant, variants\)\)/);
  assert.match(app, /\[sourceKindLabel\(liveGame \|\| game\), 'Source'\]/);
  assert.match(app, /const detailGame = liveGame \|\| game/);
  assert.match(app, /const membershipReady = Boolean\(/);
  assert.match(app, /game\.id === detailGame\.id/);
  assert.doesNotMatch(app, /'Staging'/);
  assert.doesNotMatch(app, /'Prepared'/);
  assert.doesNotMatch(app, /'On demand'/);
  assert.match(css, /--list-row-height:\s*72px/);
  assert.match(css, /--list-row-stride:\s*78px/);
  assert.match(css, /\.card-mark-favorite\s*\{[^}]*margin-left:\s*auto/);
  assert.match(app, /const WALL_WINDOW = 80/);
  assert.match(app, /--card-marks-clearance/);
  assert.match(app, /root\.ResizeObserver/);
  assert.match(css, /\.game-card h3\s*\{[^}]*opacity:\s*1/);
  assert.match(css, /\.game-card::after\s*\{[^}]*opacity:\s*1/);
  assert.doesNotMatch(css, /\.card-mark-revision/);
  assert.doesNotMatch(app, /\.card-mark-revision/);

  const groupKey = 'megadrive\u001fsonic';
  const usaZip = availableGame('megadrive-sonic-usa-zip', 'Sonic the Hedgehog (USA)', {
    system: 'megadrive',
    kind: 'zip',
    canonical_title: 'Sonic the Hedgehog',
    group_key: groupKey,
    variant_count: 2,
    region: 'usa',
    year: '1991',
    collections: ['weekend-queue'],
  });
  const usaRaw = availableGame('megadrive-sonic-usa-raw', 'Sonic the Hedgehog (USA) (Alt)', {
    system: 'megadrive',
    kind: 'raw',
    canonical_title: 'Sonic the Hedgehog',
    group_key: groupKey,
    region: 'usa',
    year: '1991',
    collections: ['speedruns'],
  });
  const grouped = { ...usaZip };
  assert.equal(variantLabel(usaZip), 'Sonic the Hedgehog (USA) · USA · ZIP');
  assert.equal(variantLabel(usaRaw), 'Sonic the Hedgehog (USA) (Alt) · USA · ROM');
  assert.notEqual(variantLabel(usaZip), variantLabel(usaRaw));
  const representativeHover = 'Mega Drive · USA · 1991 · Ready';

  let releaseRawDetail;
  const heldRawDetail = new Promise(resolve => {
    releaseRawDetail = () => resolve(jsonResponse({
      code: 'INTERNAL',
      message: 'detail is unavailable',
    }, 500));
  });
  const { document } = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...usaZip, variants: [usaZip, usaRaw] }),
      heldRawDetail,
    ],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw|Alt|Staging|Prepared|On demand/);

  await cover.click();
  await settleBrowser();
  const select = document.getElementById('game-version');
  assert.ok(select);
  assert.equal(select.children[0].textContent, variantLabel(usaZip));
  assert.equal(select.children[1].textContent, variantLabel(usaRaw));
  assert.notEqual(select.children[0].textContent, select.children[1].textContent);
  assert.match(select.children[0].textContent, /ZIP/);
  assert.match(select.children[1].textContent, /ROM|Alt/);
  assert.doesNotMatch(select.children[0].textContent, /^USA$/);
  assert.doesNotMatch(select.children[1].textContent, /^USA$/);
  const zipFacts = detailFactsFrom(document);
  assert.ok(zipFacts.some(fact => fact.label === 'Source' && fact.value === 'ZIP'));
  assert.ok(zipFacts.some(fact => fact.label === 'Region' && fact.value === 'USA'));
  assertOmitsStagingAndExecution(zipFacts);

  select.value = usaRaw.id;
  const selecting = select.dispatchEvent({ type: 'change' });
  await settleBrowser();
  const pendingFacts = detailFactsFrom(document);
  assert.ok(pendingFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assert.equal(pendingFacts.filter(fact => fact.label === 'Source').length, 1);
  assert.ok(pendingFacts.some(fact => fact.label === 'Region' && fact.value === 'USA'));
  assertOmitsStagingAndExecution(pendingFacts);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  releaseRawDetail();
  await selecting;
  await settleBrowser();
  const failedFacts = detailFactsFrom(document);
  assert.ok(failedFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assertOmitsStagingAndExecution(failedFacts);

  const homeApp = await runBrowserApp({
    keepHome: true,
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [grouped] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(homeApp.document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const homeCard = rail.querySelectorAll('.game-card')[0];
  assert.equal(homeCard.children.find(child => child.tagName === 'H3').textContent, 'Sonic the Hedgehog');
  assert.equal(homeCard.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.doesNotMatch(homeCard.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|Alt/);

  const listApp = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    collections: [WEEKEND_QUEUE, SPEEDRUNS],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  listApp.document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(listApp.document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[0].textContent, 'Sonic the Hedgehog');
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Action · SEGA · 1 player · Weekend Queue · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA|ZIP|ROM|Alt/);
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
});

test('case-folded Sonic vs SONIC Version options keep dump titles while Cover Home List stay representative', async () => {
  const app = readAsset('ui_app.js');
  assert.match(app, /function variantLabel\(game, variants\)/);
  assert.match(app, /function variantLabelText\(game, keepMatchingTitle\)/);
  assert.match(app, /const option = element\('option', '', variantLabel\(variant, variants\)\)/);
  assert.match(app, /\[sourceKindLabel\(liveGame \|\| game\), 'Source'\]/);
  assert.match(app, /const detailGame = liveGame \|\| game/);
  assert.doesNotMatch(app, /bits\.push\(sourceKindLabel/);
  assert.doesNotMatch(app, /'Staging'/);

  const groupKey = 'megadrive\u001fsonic';
  const sonic = availableGame('megadrive-sonic', 'Sonic', {
    system: 'megadrive',
    kind: 'raw',
    canonical_title: 'Sonic',
    group_key: groupKey,
    variant_count: 2,
    year: '1991',
  });
  const shouting = availableGame('megadrive-SONIC', 'SONIC', {
    system: 'megadrive',
    kind: 'raw',
    canonical_title: 'SONIC',
    group_key: groupKey,
    year: '1991',
  });
  const grouped = { ...sonic };
  const siblings = [sonic, shouting];
  assert.equal(variantLabel(sonic), 'ROM');
  assert.equal(variantLabel(shouting), 'ROM');
  assert.equal(variantLabel(sonic, siblings), 'Sonic · ROM');
  assert.equal(variantLabel(shouting, siblings), 'SONIC · ROM');
  assert.notEqual(variantLabel(sonic, siblings), variantLabel(shouting, siblings));
  const representativeHover = 'Mega Drive · 1991 · Ready';

  const { document } = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [
      jsonResponse({ games: [grouped] }),
      jsonResponse({ ...sonic, variants: siblings }),
      jsonResponse(shouting),
    ],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  const cover = gameCards(document)[0];
  assert.equal(cover.children.find(child => child.tagName === 'H3').textContent, 'Sonic');
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.doesNotMatch(cover.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|zip|raw|SONIC|Staging/);

  await cover.click();
  await settleBrowser();
  const select = document.getElementById('game-version');
  assert.ok(select);
  assert.equal(select.children[0].textContent, variantLabel(sonic, siblings));
  assert.equal(select.children[1].textContent, variantLabel(shouting, siblings));
  assert.equal(select.children[0].textContent, 'Sonic · ROM');
  assert.equal(select.children[1].textContent, 'SONIC · ROM');
  assert.notEqual(select.children[0].textContent, select.children[1].textContent);
  assert.notEqual(select.children[0].textContent, 'ROM');
  assert.notEqual(select.children[1].textContent, 'ROM');
  const sonicFacts = detailFactsFrom(document);
  assert.ok(sonicFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assert.equal(sonicFacts.filter(fact => fact.label === 'Source').length, 1);
  assertOmitsStagingAndExecution(sonicFacts);

  select.value = shouting.id;
  const selecting = select.dispatchEvent({ type: 'change' });
  await settleBrowser();
  const shoutingFacts = detailFactsFrom(document);
  assert.ok(shoutingFacts.some(fact => fact.label === 'Source' && fact.value === 'ROM'));
  assert.equal(shoutingFacts.filter(fact => fact.label === 'Source').length, 1);
  assertOmitsStagingAndExecution(shoutingFacts);
  assert.equal(cover.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  await selecting;
  await settleBrowser();

  const homeApp = await runBrowserApp({
    keepHome: true,
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
    homeRails: {
      continue: jsonResponse({ games: [grouped] }),
      favorites: jsonResponse({ games: [] }),
      recents: jsonResponse({ games: [] }),
    },
  });
  await settleBrowser();
  const rail = homeRails(homeApp.document).find(item => item.attributes.get('data-home-rail') === 'continue');
  const homeCard = rail.querySelectorAll('.game-card')[0];
  assert.equal(homeCard.children.find(child => child.tagName === 'H3').textContent, 'Sonic');
  assert.equal(homeCard.children.find(child => child.className === 'game-meta').textContent, representativeHover);
  assert.doesNotMatch(homeCard.children.find(child => child.className === 'game-meta').textContent, /ZIP|ROM|SONIC/);

  const listApp = await runBrowserApp({
    adapter: readyStudioAdapter(),
    responses: [jsonResponse({ games: [grouped] })],
    sessionResponses: [jsonResponse({ state: 'idle' })],
  });
  await settleBrowser();
  listApp.document.nodes.get('layout-list').click();
  await settleBrowser();
  const row = gameCards(listApp.document)[0];
  const body = row.children.find(child => String(child.className).includes('game-row-body'));
  assert.equal(body.children[0].textContent, 'Sonic');
  assert.equal(body.children[1].className, 'game-meta');
  assert.equal(body.children[1].textContent, 'Mega Drive · 1991 · Action · SEGA · 1 player · Ready');
  assert.doesNotMatch(body.children[1].textContent, /USA|ZIP|ROM|SONIC/);
  assert.equal(body.children[2].className, 'game-meta game-meta-variant');
  assert.equal(body.children.length, 3);
});
