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
  filterCatalogViews,
  sortCatalogViews,
  formatCatalogCount,
  fallbackPresentation,
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
  constructor(tagName, id = '') {
    this.tagName = tagName.toUpperCase();
    this.id = id;
    this.children = [];
    this.attributes = new Map();
    this.listeners = new Map();
    this.className = '';
    this.textContent = '';
    this.value = '';
    this.disabled = false;
    this.hidden = false;
    this.focused = false;
  }

  appendChild(child) {
    this.children.push(child);
    return child;
  }

  replaceChildren(...children) {
    this.children = children;
  }

  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }

  removeAttribute(name) {
    this.attributes.delete(name);
  }

  addEventListener(name, listener) {
    this.listeners.set(name, listener);
  }

  click() {
    const listener = this.listeners.get('click');
    return listener ? listener({ currentTarget: this }) : undefined;
  }

  focus() {
    this.focused = true;
  }
}

function browserDocument() {
  const ids = [
    'health', 'game-search', 'refresh-catalog', 'catalog', 'catalog-status',
    'catalog-list', 'catalog-actions', 'detail', 'detail-content',
    'launch-actions', 'launch-status', 'session-panel', 'session-status',
    'session-details', 'session-actions', 'session-message',
  ];
  const nodes = new Map(ids.map(id => [id, new BrowserTestElement('div', id)]));
  return {
    nodes,
    createElement(tagName) {
      return new BrowserTestElement(tagName);
    },
    getElementById(id) {
      return nodes.get(id) || null;
    },
  };
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

async function runBrowserApp({ adapter, responses, sessionResponses }) {
  const document = browserDocument();
  const calls = [];
  const sessionCalls = [];
  const queuedSessionResponses = (sessionResponses || [
    jsonResponse({ state: 'idle' }),
    jsonResponse({ state: 'active', game_id: 'megadrive-sonic-test', system: 'megadrive' }),
  ]).slice();
  const fetch = async (requestPath, options) => {
    const isSession = requestPath === '/api/v1/session';
    const destination = isSession ? sessionCalls : calls;
    destination.push({ path: requestPath, options });
    const response = (isSession ? queuedSessionResponses : responses).shift();
    if (!response) throw new Error(`missing browser fixture response for ${requestPath}`);
    return response;
  };
  const context = { document, fetch };
  if (adapter !== undefined) context.FogCastMetadata = adapter;
  vm.runInNewContext(readAsset('ui_app.js'), context, { filename: 'ui_app.js' });
  await settleBrowser();
  return { document, calls, sessionCalls };
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

test('catalog filters keep search on the host and hide unmatched platforms, regions, and genres', () => {
  assert.equal(gamesPath('sonic'), '/api/v1/games?q=sonic');
  assert.equal(catalogRegion('007 Shitou - The Duel (Japan)'), 'japan');
  assert.equal(catalogRegion('Streets of Rage 2 (USA)'), 'usa');
  assert.equal(catalogRegion('Sonic & Knuckles (World)'), 'world');
  assert.equal(catalogRegion('Bare Knuckle ~ Streets of Rage (World) (Rev A)'), 'world');
  assert.equal(catalogRegion('Sonic the Hedgehog'), 'other');
  const views = [
    { live: { id: 'megadrive-a', title: 'Streets of Rage 2 (USA)', system: 'megadrive', genre: 'Beat \'em Up' }, presentation: { genre: 'Beat \'em Up' } },
    { live: { id: 'snes-b', title: 'Super Mario World (USA)', system: 'snes', genre: 'Platform' }, presentation: { genre: 'Platform' } },
    { live: { id: 'megadrive-c', title: '007 Shitou - The Duel (Japan)', system: 'megadrive' }, presentation: { genre: 'Unknown' } },
  ];
  assert.deepEqual(filterCatalogViews(views, { system: 'snes' }).map(view => view.live.id), ['snes-b']);
  assert.deepEqual(filterCatalogViews(views, { region: 'japan' }).map(view => view.live.id), ['megadrive-c']);
  assert.deepEqual(filterCatalogViews(views, { genre: 'Platform' }).map(view => view.live.id), ['snes-b']);
  assert.equal(catalogGenre(views[0]), 'Beat \'em Up');
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
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/games']);
  await controller.selectGame('megadrive-sonic-test');
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/games', '/api/v1/games/megadrive-sonic-test', '/api/v1/presentation/games/megadrive-sonic-test', '/api/v1/presentation/games/megadrive-sonic-test']);
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
    if (requestPath === '/api/v1/games') return jsonResponse(readFixture('catalog-populated.json'));
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
    '/api/v1/games',
    '/api/v1/games/megadrive-sonic-test',
    '/api/v1/session/launch',
  ]);
  assert.equal(calls[2].options.body, '{"game_id":"megadrive-sonic-test"}');
}

test('gamesPath delegates search membership to the live API', () => {
  assert.equal(gamesPath(''), '/api/v1/games');
  assert.equal(gamesPath('sonic & tails'), '/api/v1/games?q=sonic%20%26%20tails');
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
  assert.match(app, /card\.appendChild\(element\('h3', '', game\.title\)\);/);
});

test('catalog cards defer unreliable execution compatibility data', () => {
  const app = readAsset('ui_app.js');
  assert.doesNotMatch(app, /game\.execution/);
  assert.match(app, /game\.state/);
  assert.match(app, /game\.content_prepared/);
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
    '/api/v1/games',
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

  await controller.loadCatalog('');
  await controller.selectGame('megadrive-sonic-test');
  await controller.launchSelected();
  await controller.loadCatalog('missing');
  const state = controller.getState();
  assert.equal(catalogViewState(state), 'no_matches');
  assert.equal(state.selectedLiveGame, null);
  assert.equal(state.launchState, 'idle');
  assert.equal(state.detailState, 'idle');
});

test('slow stale search responses cannot replace a newer fixture response', async () => {
  let resolveOld;
  let resolveNew;
  const fetchImpl = requestPath => {
    if (requestPath.endsWith('old')) return new Promise(resolve => { resolveOld = resolve; });
    if (requestPath.endsWith('new')) return new Promise(resolve => { resolveNew = resolve; });
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
      jsonResponse(readFixture(testCase.fixture), testCase.responseStatus || 200),
    ]);
    const controller = createAppController({ fetchImpl, metadataAdapter: FogCastMetadata });
    await controller.loadCatalog(testCase.query);
    const state = controller.getState();
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
    if (requestPath === '/api/v1/games') return Promise.resolve(jsonResponse(readFixture('catalog-populated.json')));
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
    assert.match(firstCard.children[2].textContent, /megadrive · available/, `${testCase.name} card state must stay live`);

    await firstCard.click();
    assert.match(browserText(document.nodes.get('detail-content')), /Sonic the Hedgehog/);
    const launchButton = document.nodes.get('launch-actions').children
      .find(child => child.tagName === 'BUTTON' && child.textContent === 'Launch live game');
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
  assert.equal(cardImage.className, 'cover-art artwork-empty');
  assert.equal(cardImage.tagName, 'SPAN');
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
    assert.doesNotMatch(card.children[0].className, /evil|sr-only|status-message|palette-|treatment-/);
    assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 0);
  }

  await catalogList.children[0].click();
  const detailContent = document.nodes.get('detail-content');
  const summary = detailContent.children.find(child => child.className === 'detail-summary');
  assert.equal(summary, undefined);
  assert.equal(detailContent.children[0].className, 'backdrop-art artwork-empty');

  const launchButton = document.nodes.get('launch-actions').children
    .find(child => child.tagName === 'BUTTON' && child.textContent === 'Launch live game');
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

function routedFetch(routes) {
  const calls = [];
  const fetchImpl = async (requestPath, options) => {
    calls.push({ path: requestPath, options });
    const queue = routes[requestPath];
    if (!queue || queue.length === 0) throw new Error(`missing routed response for ${requestPath}`);
    const next = queue.shift();
    return typeof next === 'function' ? next() : next;
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
  assert.deepEqual(calls.map(call => call.path), ['/api/v1/session', '/api/v1/games']);
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
  assert.equal(document.nodes.get('session-status').textContent, 'Active session');
  const details = browserText(document.nodes.get('session-details'));
  assert.match(details, /Sonic the Hedgehog/);
  assert.match(details, /megadrive-sonic-test/);
  assert.match(details, /session is ready/);
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

test('session malformed state exposes a safe retry without leaking response fields', async () => {
  const { document } = await runBrowserApp({
    responses: [jsonResponse(readFixture('catalog-populated.json'))],
    sessionResponses: [malformedJSONResponse(), jsonResponse(sessionFixture({ state: 'idle' }))],
  });
  assert.equal(document.nodes.get('session-status').textContent, 'The local host returned an invalid session response. Retry.');
  assert.match(document.nodes.get('session-message').textContent, /invalid session response/i);
  await document.nodes.get('session-actions').children.find(node => node.id === 'refresh-session').click();
  assert.equal(document.nodes.get('session-status').textContent, 'No active session.');
});
