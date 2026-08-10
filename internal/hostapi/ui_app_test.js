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
  launchStatus,
  isSelectedGame,
  detailHeading,
  createAppController,
  catalogViewState,
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

  addEventListener(name, listener) {
    this.listeners.set(name, listener);
  }

  click() {
    const listener = this.listeners.get('click');
    return listener ? listener({ currentTarget: this }) : undefined;
  }
}

function browserDocument() {
  const ids = [
    'health', 'game-search', 'refresh-catalog', 'catalog', 'catalog-status',
    'catalog-list', 'catalog-actions', 'detail', 'detail-content',
    'launch-actions', 'launch-status',
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
  await new Promise(resolve => setImmediate(resolve));
  await Promise.resolve();
}

async function runBrowserApp({ adapter, responses }) {
  const document = browserDocument();
  const calls = [];
  const fetch = async (requestPath, options) => {
    calls.push({ path: requestPath, options });
    const response = responses.shift();
    if (!response) throw new Error(`missing browser fixture response for ${requestPath}`);
    return response;
  };
  const context = { document, fetch };
  if (adapter !== undefined) context.FogCastMetadata = adapter;
  vm.runInNewContext(readAsset('ui_app.js'), context, { filename: 'ui_app.js' });
  await settleBrowser();
  return { document, calls };
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
    isFallback: false,
  };
}

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
  assert.equal(state.gameViews[0].presentation.isFallback, false);
  assert.equal(state.metadataFallbackCount, 0);

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
  assert.equal(state.metadataFallbackCount, 2);
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
    const artworkClasses = card.children[0].className.split(/\s+/);
    assert.equal(artworkClasses.length, 3);
    assert.match(artworkClasses[1], /^palette-(ember|lagoon|violet|sunset|forest)$/);
    assert.match(artworkClasses[2], /^treatment-(grid|rings|stripes|starlight|waves)$/);
    assert.doesNotMatch(card.children[0].className, /evil|sr-only|status-message/);
    assert.equal(card.children.filter(child => child.className === 'fallback-note').length, 1);
  }

  await catalogList.children[0].click();
  const detailContent = document.nodes.get('detail-content');
  const summary = detailContent.children.find(child => child.className === 'detail-summary');
  assert.ok(summary);
  assert.ok(summary.textContent.length <= 240);
  assert.notEqual(summary.textContent, invalidPresentation.summary);
  const backdropClasses = detailContent.children[0].className.split(/\s+/);
  assert.equal(backdropClasses.length, 3);
  assert.match(backdropClasses[1], /^palette-(ember|lagoon|violet|sunset|forest)$/);
  assert.match(backdropClasses[2], /^treatment-(grid|rings|stripes|starlight|waves)$/);

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
