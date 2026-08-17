'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const http = require('node:http');
const {
  BrowserHarness,
  BrowserPage,
  FixtureServer,
  fixture,
  artworkFixture,
  waitForProcessGroupQuiescence,
} = require('./ui_browser_harness.js');

const REQUIRED = process.env.FOGCAST_BROWSER_REQUIRED === '1';
const SONIC_ID = 'megadrive-sonic-test';
const UNKNOWN_ID = 'snes-unknown-test';
const ARTWORK_HANDLE = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';

function populatedCatalog() {
  return fixture('catalog-populated.json');
}

function defaultDetails() {
  return {
    [SONIC_ID]: fixture('detail-refreshed.json'),
    [UNKNOWN_ID]: fixture('detail-unknown.json'),
  };
}

function defaultPresentations() {
  return {
    [SONIC_ID]: fixture('presentation-ready.json'),
    [UNKNOWN_ID]: fixture('presentation-no-match.json'),
  };
}

function basePlan(overrides = {}) {
  return {
    catalog: { '': populatedCatalog() },
    details: defaultDetails(),
    sessions: [fixture('session-idle.json'), fixture('session-active.json')],
    launches: [fixture('launch-success.json')],
    stops: [fixture('stop-success.json')],
    ...overrides,
    artworks: { [ARTWORK_HANDLE]: artworkFixture(), ...(overrides.artworks || {}) },
    presentations: { ...defaultPresentations(), ...(overrides.presentations || {}) },
  };
}

async function runScenario(harness, name, plan, body) {
  harness.configure(plan);
  await harness.reload();
  await body();
  await harness.waitForSettled();
  harness.assertClean();
  harness.recordScenario(name);
}

async function selectSonic(harness) {
  await harness.waitForCatalog('populated metadata_fallback');
  await harness.click('#catalog-list .game-card');
  await harness.waitForDetailHeading('Sonic the Hedgehog (detail refresh)');
}

async function launchSelected(harness) {
  await harness.click('#launch-actions button');
}

function apiEvidence(harness) {
  return harness.fixtureEvidence()
    .filter(record => record.path.startsWith('/api/')
      && !(record.method === 'GET' && record.path === '/api/v1/session'));
}

function detailEvidence(harness) {
  return apiEvidence(harness)
    .filter(record => record.path.startsWith('/api/v1/games/'));
}

function presentationEvidence(harness) {
  return apiEvidence(harness)
    .filter(record => record.path.startsWith('/api/v1/presentation/games/'));
}

function artworkEvidence(harness) {
  return apiEvidence(harness)
    .filter(record => record.path.startsWith('/api/v1/presentation/artwork/'));
}

function sessionEvidence(harness) {
  return harness.fixtureEvidence()
    .filter(record => record.path === '/api/v1/session');
}

function apiSummary(harness) {
  return apiEvidence(harness).map(record => ({
    method: record.method,
    path: record.path,
    query: record.query,
    status: record.status,
    fixture: record.fixture,
    requestBody: record.requestBody,
    requestContentType: record.requestContentType,
  }));
}

function assertNoPrivateErrorText(snapshot) {
  assert.doesNotMatch(snapshot.catalogText, /catalog is unavailable|private|secret|password/i);
  assert.doesNotMatch(snapshot.detailText, /detail is unavailable|private|secret|password/i);
  assert.doesNotMatch(snapshot.launchText, /session operation failed|private|secret|password/i);
  assert.doesNotMatch(snapshot.sessionText, /private|secret|password|target_path|token/i);
  assert.doesNotMatch(snapshot.sessionMessage, /private|secret|password|target_path|token/i);
}

function getDocument(origin) {
  return new Promise((resolve, reject) => {
    const request = http.get(`${origin}/`, response => {
      const chunks = [];
      response.on('data', chunk => chunks.push(Buffer.from(chunk)));
      response.once('error', reject);
      response.once('end', () => resolve({
        statusCode: response.statusCode,
        headers: response.headers,
        body: Buffer.concat(chunks),
      }));
    });
    request.setTimeout(3_000, () => request.destroy(new Error('fixture document request timed out')));
    request.once('error', reject);
  });
}

function getPath(origin, pathname) {
  return new Promise((resolve, reject) => {
    const request = http.get(`${origin}${pathname}`, response => {
      response.resume();
      response.once('error', reject);
      response.once('end', resolve);
    });
    request.setTimeout(3_000, () => request.destroy(new Error('fixture request timed out')));
    request.once('error', reject);
  });
}

function syntheticPage(origin = 'http://127.0.0.1:1234') {
  return new BrowserPage(new EventEmitter(), 'target-1', 'session-1', origin);
}

function networkEvent(page, method, params) {
  page.handleEvent({ sessionId: 'session-1', method, params });
}

function requestWillBeSent(page, requestId, url) {
  networkEvent(page, 'Network.requestWillBeSent', {
    requestId,
    request: { url, method: 'GET' },
  });
}

function responseExtraInfo(page, requestId, statusCode) {
  networkEvent(page, 'Network.responseReceivedExtraInfo', { requestId, statusCode });
}

if (process.env.FOGCAST_BROWSER_TEARDOWN_TESTS === '1') {
test('Chrome teardown waits for detached helpers after the root child exits', { timeout: 5_000 }, async () => {
  const helper = require('node:child_process').spawn(
    process.env.SHELL || '/bin/sh',
    ['-c', 'sleep 0.25 & exit 0'],
    { detached: true, stdio: 'ignore' },
  );
  await new Promise(resolve => helper.once('exit', resolve));
  assert.equal(await waitForProcessGroupQuiescence(helper.pid, 2_000), true);
});

test('harness.close removes only the profile root it created after root exit', { timeout: 30_000 }, async t => {
  const harness = new BrowserHarness();
  let profileRoot = null;
  try {
    try {
      await harness.start();
    } catch (error) {
      if (!REQUIRED && error && error.preReadiness === true) {
        t.skip(error.reason);
        return;
      }
      throw error;
    }
    profileRoot = harness.chrome.tempRoot;
    harness.chrome.child.kill('SIGTERM');
    await harness.chrome.exitPromise;
    harness.page = null;
    harness.targetId = null;
  } finally {
    await harness.close();
  }
  assert.ok(profileRoot);
  assert.equal(require('node:fs').existsSync(profileRoot), false);
});
}

if (process.env.FOGCAST_BROWSER_EVENT_ORDER_TESTS === '1') {
test('matched response extra-info settles same-origin evidence before responseReceived', () => {
  const page = syntheticPage();
  requestWillBeSent(page, 'request-1', 'http://127.0.0.1:1234/favicon.ico');
  responseExtraInfo(page, 'request-1', 204);

  const request = page.evidence().networkRequests[0];
  assert.equal(request.status, 204);
  assert.equal(request.statusSource, 'response-extra-info');
  assert.deepEqual(page.evidence().networkFailures, []);
  page.assertClean();
});

test('settled evidence drains a delayed matching responseReceived event', { timeout: 3_000 }, async () => {
  const harness = new BrowserHarness();
  try {
    const origin = await harness.fixtureServer.start();
    harness.fixtureServer.configure({});
    harness.page = syntheticPage(origin);
    requestWillBeSent(harness.page, 'request-1', `${origin}/favicon.ico`);
    responseExtraInfo(harness.page, 'request-1', 204);
    await getPath(origin, '/favicon.ico');

    const settled = harness.waitForSettled();
    setTimeout(() => networkEvent(harness.page, 'Network.responseReceived', {
      requestId: 'request-1',
      response: { status: 204 },
    }), 25);
    await settled;
    assert.equal(harness.page.evidence().networkRequests[0].statusSource, 'response-received+extra-info');
  } finally {
    await harness.close();
  }
});

test('unmatched response extra-info fails closed', () => {
  const page = syntheticPage();
  responseExtraInfo(page, 'missing-request', 204);

  assert.equal(page.evidence().networkFailures[0].reason, 'unmatched-response-extra-info');
  assert.throws(() => page.assertClean(), /browser network failures/);
});

test('malformed response extra-info fails closed', () => {
  const page = syntheticPage();
  requestWillBeSent(page, 'request-1', 'http://127.0.0.1:1234/favicon.ico');
  responseExtraInfo(page, 'request-1', '204');

  assert.equal(page.evidence().networkFailures[0].reason, 'malformed-response-extra-info');
  assert.equal(page.evidence().networkRequests[0].status, null);
  assert.throws(() => page.assertClean(), /browser network failures/);
});

test('foreign-origin response extra-info fails closed', () => {
  const page = syntheticPage();
  requestWillBeSent(page, 'request-1', 'https://foreign.example/favicon.ico');
  responseExtraInfo(page, 'request-1', 204);

  assert.equal(page.evidence().networkFailures[0].reason, 'foreign-origin-response-extra-info');
  assert.equal(page.evidence().networkRequests[0].status, null);
  assert.throws(() => page.assertClean(), /browser network failures/);
});

test('conflicting response extra-info and responseReceived statuses fail closed', () => {
  const page = syntheticPage();
  requestWillBeSent(page, 'request-1', 'http://127.0.0.1:1234/favicon.ico');
  responseExtraInfo(page, 'request-1', 204);
  networkEvent(page, 'Network.responseReceived', {
    requestId: 'request-1',
    response: { status: 200 },
  });

  assert.equal(page.evidence().networkFailures[0].reason, 'conflicting-response-status');
  assert.throws(() => page.assertClean(), /browser network failures/);
});

test('browser report does not claim an isolated profile before readiness', () => {
  const harness = new BrowserHarness();
  assert.equal(harness.report().chrome.isolatedProfile, false);
});
}

test('fixture server serves the assembled production document with production headers', { timeout: 5_000 }, async () => {
  const server = new FixtureServer();
  try {
    const origin = await server.start();
    assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/);
    server.configure({});

    const response = await getDocument(origin);
    assert.equal(response.statusCode, 200);
    assert.equal(response.headers['content-type'], 'text/html; charset=utf-8');
    assert.equal(response.headers['cache-control'], 'no-store');
    assert.deepEqual(response.body, Buffer.from(server.plan.html, 'utf8'));

    const html = response.body.toString('utf8');
    for (const marker of [
      '<!doctype html>',
      '<title>FogCast launcher</title>',
      '<input id="game-search"',
      'root.FogCastMetadata',
      '<script>globalThis.FogCastPresentationEnabled = true;</script><script>',
      'installFogCastApp',
    ]) {
      assert.ok(html.includes(marker), `production HTML marker missing: ${marker}`);
    }
  } finally {
    await server.close();
    assert.equal(server.origin, null);
  }
});

test('FogCast production UI Chrome/CDP integration', { timeout: 120_000 }, async t => {
  const harness = new BrowserHarness();
  try {
    try {
      await harness.start();
    } catch (error) {
      if (!REQUIRED && error && error.preReadiness === true) {
        t.skip(error.reason);
        return;
      }
      throw error;
    }

    harness.configure(basePlan());
    await harness.reload();
    await harness.waitForCatalog('populated metadata_fallback');
    assert.ok(harness.page.mainFrameId, 'navigation preflight must establish a main frame');
    assert.match(harness.page.defaultContext, /\S+/, 'navigation preflight must establish a default execution context');
    await harness.waitForSettled();
    harness.assertClean();
    const preflightRequests = harness.fixtureEvidence()
      .filter(record => record.path === '/' || record.path === '/api/v1/session' || record.path === '/api/v1/games')
      .map(record => ({
        method: record.method,
        path: record.path,
        status: record.status,
      }));
    assert.deepEqual(preflightRequests, [
      { method: 'GET', path: '/', status: 200 },
      { method: 'GET', path: '/api/v1/session', status: 200 },
      { method: 'GET', path: '/api/v1/games', status: 200 },
    ]);

    await t.test('catalog load, refresh, and live selection reconciliation', async () => {
      await runScenario(harness, 'catalog-load-refresh', basePlan({
        catalog: {
          '': [populatedCatalog(), fixture('catalog-newer.json')],
        },
      }), async () => {
        let snapshot = await harness.waitForCatalog('populated metadata_fallback');
        assert.equal(snapshot.cards.length, 3);
        assert.deepEqual(snapshot.cards.map(card => card.title), [
          'Sonic the Hedgehog',
          'Unknown <Game>',
          'Offline Source',
        ]);
        assert.equal(snapshot.cards[0].system, 'megadrive');
        assert.equal(snapshot.cards[0].state, 'available');
        assert.equal(snapshot.cards[0].fallback, true);
        assert.equal(snapshot.cards[1].fallback, true);

        await harness.click('#refresh-catalog');
        snapshot = await harness.waitForCatalog('populated metadata_fallback');
        assert.deepEqual(snapshot.cards.map(card => card.title), ['Sonic the Hedgehog (refreshed)']);
        assert.equal(snapshot.cards[0].pressed, false);
        assert.deepEqual(apiEvidence(harness).map(record => ({ method: record.method, path: record.path })), [
          { method: 'GET', path: '/api/v1/games' },
          { method: 'GET', path: '/api/v1/games' },
        ]);
      });
    });

    await t.test('empty catalog and encoded search query', async () => {
      await runScenario(harness, 'empty-search', basePlan({
        catalog: {
          '': fixture('catalog-empty.json'),
          'sonic & tails': fixture('catalog-empty.json'),
        },
      }), async () => {
        let snapshot = await harness.waitForCatalog('empty');
        assert.match(snapshot.catalogText, /library is empty/i);
        await harness.setSearch('sonic & tails');
        snapshot = await harness.waitForCatalog('no_matches');
        assert.match(snapshot.catalogText, /no matching games/i);
        const search = apiEvidence(harness).find(record => record.query === 'sonic & tails');
        assert.ok(search, `missing decoded query evidence: ${JSON.stringify(apiSummary(harness))}`);
        assert.equal(search.path, '/api/v1/games');
      });
    });

    await t.test('catalog error, malformed response, and safe retry state', async () => {
      await runScenario(harness, 'catalog-failures', basePlan({
        catalog: {
          '': [fixture('catalog-error.json', 500), fixture('catalog-malformed.json')],
        },
      }), async () => {
        let snapshot = await harness.waitForCatalog('catalog_error');
        assert.match(snapshot.catalogText, /catalog could not be loaded/i);
        assert.match(snapshot.catalogActions, /Retry catalog/i);
        assertNoPrivateErrorText(snapshot);
        await harness.click('#catalog-actions button');
        snapshot = await harness.waitForCatalog('catalog_error');
        assert.match(snapshot.catalogActions, /Retry catalog/i);
        assertNoPrivateErrorText(snapshot);
        assert.deepEqual(apiEvidence(harness).map(record => record.status), [500, 200]);
      });
    });

    await t.test('detail refresh success, failure, retry, and malformed identity', async () => {
      await runScenario(harness, 'detail-refresh-failure-retry', basePlan({
        details: {
          [SONIC_ID]: [fixture('detail-error.json', 500), fixture('detail-refreshed.json')],
        },
      }), async () => {
        await harness.waitForCatalog('populated metadata_fallback');
        await harness.click('#catalog-list .game-card');
        let snapshot = await harness.waitForText('#launch-actions', 'could not be refreshed');
        assert.match(snapshot.detailActions, /Retry detail/i);
        assertNoPrivateErrorText(snapshot);
        await harness.click('#launch-actions button');
        await harness.waitForDetailHeading('Sonic the Hedgehog (detail refresh)');
        snapshot = await harness.snapshot();
        assert.equal(snapshot.detailHeading, 'Sonic the Hedgehog (detail refresh)');
        assert.deepEqual(detailEvidence(harness).map(record => ({ path: record.path, status: record.status })), [
          { path: `/api/v1/games/${SONIC_ID}`, status: 500 },
          { path: `/api/v1/games/${SONIC_ID}`, status: 200 },
        ]);
      });

      await runScenario(harness, 'detail-malformed-identity', basePlan({
        details: {
          [SONIC_ID]: fixture('detail-malformed.json'),
        },
      }), async () => {
        await harness.waitForCatalog('populated metadata_fallback');
        await harness.click('#catalog-list .game-card');
        const snapshot = await harness.waitForText('#launch-actions', 'could not be refreshed');
        assert.match(snapshot.detailActions, /Retry detail/i);
        assertNoPrivateErrorText(snapshot);
        assert.equal(detailEvidence(harness)[0].status, 200);
        assert.equal(detailEvidence(harness)[0].fixture, 'detail-malformed.json');
      });
    });

    await t.test('production-enabled presentation stays host-local and feeds detail before launch', async () => {
      await runScenario(harness, 'presentation-ready', basePlan(), async () => {
        await selectSonic(harness);
        let snapshot = await harness.waitForText('#detail-content', 'Host-local presentation metadata for Sonic.');
        assert.match(snapshot.detailText, /Data from IGDB\.com/);
        assert.deepEqual(presentationEvidence(harness).map(record => ({
          method: record.method,
          path: record.path,
          query: record.query,
          status: record.status,
          fixture: record.fixture,
        })), [{
          method: 'GET',
          path: `/api/v1/presentation/games/${SONIC_ID}`,
          query: SONIC_ID,
          status: 200,
          fixture: 'presentation-ready.json',
        }, {
          method: 'GET',
          path: `/api/v1/presentation/games/${SONIC_ID}`,
          query: SONIC_ID,
          status: 200,
          fixture: 'presentation-ready.json',
        }]);
        const browserPaths = harness.page.evidence().networkRequests
          .map(request => {
            const url = new URL(request.url);
            return url.origin === harness.fixtureServer.origin ? `${request.method} ${url.pathname}` : '';
          })
          .filter(Boolean)
          .filter(path => path.includes('/api/v1/presentation/'));
        assert.deepEqual(browserPaths, [
          `GET /api/v1/presentation/games/${SONIC_ID}`,
          `GET /api/v1/presentation/games/${SONIC_ID}`,
        ]);

        await launchSelected(harness);
        snapshot = await harness.waitForText('#launch-status', 'launch_success');
        assert.equal(snapshot.detailHeading, 'Sonic the Hedgehog (detail refresh)');
        assert.match(snapshot.launchText, /session accepted/i);
        assert.equal(apiEvidence(harness).find(record => record.method === 'POST').path, '/api/v1/session/launch');
      });
    });

    await t.test('production Chrome preserves independent optional text fields and rejects malformed values', async () => {
      await runScenario(harness, 'presentation-partial-field-matrix', basePlan(), async () => {
        const partial = await harness.evaluate(`(() => {
          const fields = ['summary', 'year', 'genre', 'studio', 'players'];
          const source = { summary: 'Summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1 player' };
          const game = { id: 'megadrive-sonic-test', title: 'Sonic', system: 'megadrive' };
          return fields.map(field => {
            const presentation = { ...source, [field]: '' };
            const parsed = globalThis.FogCastApp.parsePresentation({
              game_id: game.id,
              state: 'ready',
              presentation,
              attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
            }, game);
            return { field, isFallback: parsed.isFallback, metadataState: parsed.metadataState, value: parsed[field], summary: parsed.summary };
          });
        })()`);
        assert.deepEqual(partial.map(result => result.field), ['summary', 'year', 'genre', 'studio', 'players']);
        for (const result of partial) {
          assert.equal(result.isFallback, false, result.field);
          assert.equal(result.metadataState, 'ready', result.field);
          assert.equal(result.value, '', result.field);
          if (result.field !== 'summary') assert.equal(result.summary, 'Summary', result.field);
        }

        const malformed = await harness.evaluate(`(() => [null, false, -1, {}, []].map(value => {
          const parsed = globalThis.FogCastApp.parsePresentation({
            game_id: 'megadrive-sonic-test',
            state: 'ready',
            presentation: { summary: 'Summary', year: '1991', genre: 'Platformer', studio: 'SEGA', players: value },
            attribution: { provider: 'igdb', label: 'Data from IGDB.com' },
          }, { id: 'megadrive-sonic-test', title: 'Sonic', system: 'megadrive' });
          return { isFallback: parsed.isFallback, metadataState: parsed.metadataState };
        }))()`);
        assert.equal(malformed.length, 5);
        assert.ok(malformed.every(result => result.isFallback && result.metadataState !== 'ready'), JSON.stringify(malformed));
      });
    });

    await t.test('bounded presentation artwork stays on the host route', async () => {
      await runScenario(harness, 'presentation-artwork', basePlan({
        presentations: { [SONIC_ID]: fixture('presentation-artwork-ready.json') },
      }), async () => {
        await selectSonic(harness);
        await harness.waitForSettled();
        const artworks = artworkEvidence(harness);
        assert.ok(artworks.length >= 1);
        assert.ok(artworks.every(record => record.query === ARTWORK_HANDLE
          && record.path === `/api/v1/presentation/artwork/${ARTWORK_HANDLE}`
          && record.status === 200
          && record.fixture === 'artwork'));
        const browserArtworkPaths = harness.page.evidence().networkRequests
          .map(request => {
            const url = new URL(request.url);
            return url.origin === harness.fixtureServer.origin ? `${request.method} ${url.pathname}` : '';
          })
          .filter(path => path.includes('/api/v1/presentation/artwork/'));
        assert.ok(browserArtworkPaths.length >= 1);
        assert.ok(browserArtworkPaths.every(path => path === `GET /api/v1/presentation/artwork/${ARTWORK_HANDLE}`));
      });
    });

    await t.test('launch success preserves exact current live catalog ID body', async () => {
      await runScenario(harness, 'launch-success', basePlan(), async () => {
        await selectSonic(harness);
        await launchSelected(harness);
        const snapshot = await harness.waitForText('#launch-status', 'launch_success');
        assert.match(snapshot.launchText, /session accepted/i);
        const launch = apiEvidence(harness).find(record => record.method === 'POST');
        assert.ok(launch);
        assert.equal(launch.path, '/api/v1/session/launch');
        assert.equal(launch.requestContentType, 'application/json');
        assert.equal(launch.requestBody, '{"game_id":"megadrive-sonic-test"}');
        assert.deepEqual(JSON.parse(launch.requestBody), { game_id: SONIC_ID });
        assert.deepEqual(Object.keys(JSON.parse(launch.requestBody)), ['game_id']);
      });
    });

    for (const [name, response] of [
      ['launch-generic-error', fixture('launch-error.json', 500)],
      ['launch-malformed-success', fixture('launch-malformed.json')],
      ['launch-target-unavailable', fixture('launch-target-unavailable.json', 503)],
    ]) {
      await t.test(name, async () => {
        await runScenario(harness, name, basePlan({ launches: [response] }), async () => {
          await selectSonic(harness);
          await launchSelected(harness);
          const expectedText = name === 'launch-generic-error'
            ? 'launch operation failed'
            : name === 'launch-malformed-success'
              ? 'invalid launch response'
              : 'local target is unavailable';
          const snapshot = await harness.waitForText('#launch-status', expectedText);
          assert.match(snapshot.launchText, new RegExp(expectedText, 'i'));
          assert.match(snapshot.detailActions, /Retry launch/i);
          assertNoPrivateErrorText(snapshot);
          assert.equal(apiEvidence(harness).find(record => record.method === 'POST').status, response.status);
        });
      });
    }

    for (const metadataMode of ['missing', 'malformed']) {
      await t.test(`metadata ${metadataMode} fallback leaves browse and host presentation usable`, async () => {
        await runScenario(harness, `metadata-${metadataMode}`, basePlan({ metadataMode }), async () => {
          let snapshot = await harness.waitForCatalog('populated metadata_fallback');
          assert.equal(snapshot.cards.length, 3);
          assert.ok(snapshot.cards.every(card => card.fallback));
          await harness.click('#catalog-list .game-card');
          await harness.waitForDetailHeading('Sonic the Hedgehog (detail refresh)');
          snapshot = await harness.waitForText('#detail-content', 'Host-local presentation metadata for Sonic.');
          assert.match(snapshot.detailText, /Data from IGDB\.com/);
          await launchSelected(harness);
          snapshot = await harness.waitForText('#launch-status', 'launch_success');
          assert.equal(snapshot.launchText.includes('launch_success'), true);
          const launch = apiEvidence(harness).find(record => record.method === 'POST');
          assert.equal(launch.requestBody, '{"game_id":"megadrive-sonic-test"}');
        });
      });
    }

    for (const [name, response] of [
      ['presentation-error', fixture('presentation-error.json', 500)],
      ['presentation-malformed', fixture('presentation-malformed.json')],
    ]) {
      await t.test(`${name} fallback leaves browse, detail, and launch usable`, async () => {
        await runScenario(harness, name, basePlan({
          presentations: { [SONIC_ID]: response },
        }), async () => {
          let snapshot = await harness.waitForCatalog('populated metadata_fallback');
          assert.equal(snapshot.cards.length, 3);
          assert.ok(snapshot.cards.every(card => card.fallback));
          await harness.click('#catalog-list .game-card');
          await harness.waitForDetailHeading('Sonic the Hedgehog (detail refresh)');
          snapshot = await harness.waitForText('#detail-content', 'metadata_fallback');
          assert.match(snapshot.detailText, /metadata_fallback/);
          await launchSelected(harness);
          snapshot = await harness.waitForText('#launch-status', 'launch_success');
          assert.equal(snapshot.launchText.includes('launch_success'), true);
          const launch = apiEvidence(harness).find(record => record.method === 'POST');
          assert.equal(launch.requestBody, '{"game_id":"megadrive-sonic-test"}');
          const presentation = presentationEvidence(harness)[0];
          assert.equal(presentation.path, `/api/v1/presentation/games/${SONIC_ID}`);
          assert.equal(presentation.status, response.status);
        });
      });
    }

    await t.test('stale search response cannot replace the newer live query', async () => {
      await runScenario(harness, 'stale-search', basePlan({
        catalog: {
          '': populatedCatalog(),
          old: fixture('catalog-populated.json', 200, { hold: true }),
          new: fixture('catalog-newer.json'),
        },
      }), async () => {
        await harness.waitForCatalog('populated metadata_fallback');
        await harness.setSearch('old');
        const oldRequest = await harness.waitForRequest({ method: 'GET', path: '/api/v1/games', query: 'old' });
        await harness.setSearch('new');
        await harness.waitForCatalog('populated metadata_fallback');
        let snapshot = await harness.snapshot();
        assert.deepEqual(snapshot.cards.map(card => card.title), ['Sonic the Hedgehog (refreshed)']);
        await harness.release(oldRequest.id);
        await harness.waitForSettled();
        snapshot = await harness.snapshot();
        assert.equal(snapshot.catalogStatus, 'populated metadata_fallback');
        assert.deepEqual(snapshot.cards.map(card => card.title), ['Sonic the Hedgehog (refreshed)']);
        const records = apiEvidence(harness).filter(record => record.query === 'old' || record.query === 'new');
        assert.deepEqual(records.map(record => record.query), ['old', 'new']);
        assert.equal(records[0].responseOrder > records[1].responseOrder, true);
      });
    });

    await t.test('stale detail response cannot replace the newer selected record', async () => {
      await runScenario(harness, 'stale-detail', basePlan({
        details: {
          [SONIC_ID]: fixture('detail-refreshed.json', 200, { hold: true }),
          [UNKNOWN_ID]: fixture('detail-unknown.json'),
        },
      }), async () => {
        await harness.waitForCatalog('populated metadata_fallback');
        await harness.click('#catalog-list .game-card');
        const oldDetail = await harness.waitForRequest({
          method: 'GET',
          path: `/api/v1/games/${SONIC_ID}`,
        });
        await harness.click('#catalog-list .game-card:nth-child(2)');
        await harness.waitForDetailHeading('Unknown <Game> detail');
        await harness.release(oldDetail.id);
        await harness.waitForSettled();
        const snapshot = await harness.snapshot();
        assert.equal(snapshot.detailHeading, 'Unknown <Game> detail');
        assert.doesNotMatch(snapshot.detailText, /Sonic the Hedgehog \(detail refresh\)/);
        const detailRecords = apiEvidence(harness).filter(record => record.path.startsWith('/api/v1/games/'));
        assert.deepEqual(detailRecords.map(record => record.path), [
          `/api/v1/games/${SONIC_ID}`,
          `/api/v1/games/${UNKNOWN_ID}`,
        ]);
      });
    });

    await t.test('stale presentation response cannot replace a newer selection or block a later launch', async () => {
      await runScenario(harness, 'stale-presentation', basePlan({
        presentations: {
          [UNKNOWN_ID]: [
            fixture('presentation-no-match.json', 200, { hold: true }),
            fixture('presentation-no-match.json'),
          ],
        },
      }), async () => {
        await selectSonic(harness);
        await harness.waitForRequest({
          method: 'GET',
          path: `/api/v1/presentation/games/${SONIC_ID}`,
        });
        await harness.click('#catalog-list .game-card:nth-child(2)');
        await harness.waitForDetailHeading('Unknown <Game> detail');
        const oldPresentation = await harness.waitForRequest({
          method: 'GET',
          path: `/api/v1/presentation/games/${UNKNOWN_ID}`,
          query: UNKNOWN_ID,
        });
        let snapshot = await harness.waitForText('#detail-content', 'metadata_fallback');
        assert.equal(snapshot.detailHeading, 'Unknown <Game> detail');
        await harness.click('#catalog-list .game-card');
        await harness.waitForRequest({
          method: 'GET',
          path: `/api/v1/presentation/games/${SONIC_ID}`,
        });
        snapshot = await harness.waitForText('#detail-content', 'Host-local presentation metadata for Sonic.');
        assert.equal(snapshot.detailHeading, 'Sonic the Hedgehog (detail refresh)');
        await harness.release(oldPresentation.id);
        await harness.waitForSettled();
        snapshot = await harness.snapshot();
        assert.equal(snapshot.detailHeading, 'Sonic the Hedgehog (detail refresh)');
        assert.doesNotMatch(snapshot.detailText, /Unknown <Game> detail/);
        await launchSelected(harness);
        snapshot = await harness.waitForText('#launch-status', 'launch_success');
        assert.match(snapshot.launchText, /session accepted/i);
        assert.deepEqual(presentationEvidence(harness).map(record => record.path), [
          `/api/v1/presentation/games/${SONIC_ID}`,
          `/api/v1/presentation/games/${SONIC_ID}`,
          `/api/v1/presentation/games/${UNKNOWN_ID}`,
          `/api/v1/presentation/games/${UNKNOWN_ID}`,
          `/api/v1/presentation/games/${SONIC_ID}`,
          `/api/v1/presentation/games/${SONIC_ID}`,
        ]);
        const records = presentationEvidence(harness);
        assert.equal(records[2].responseOrder > records[4].responseOrder, true);
      });
    });

    for (const [name, response] of [
      ['stale-launch-success', fixture('launch-success.json', 200, { hold: true })],
      ['stale-launch-error', fixture('launch-error.json', 500, { hold: true })],
    ]) {
      await t.test(`${name} cannot change a replacement selection`, async () => {
        await runScenario(harness, name, basePlan({ launches: [response] }), async () => {
          await selectSonic(harness);
          await launchSelected(harness);
          const oldLaunch = await harness.waitForRequest({ method: 'POST', path: '/api/v1/session/launch' });
          await harness.click('#catalog-list .game-card:nth-child(2)');
          await harness.waitForDetailHeading('Unknown <Game> detail');
          await harness.release(oldLaunch.id);
          await harness.waitForSettled();
          const snapshot = await harness.snapshot();
          assert.equal(snapshot.detailHeading, 'Unknown <Game> detail');
          assert.doesNotMatch(snapshot.launchText, /launch_success|launch_error/);
          assert.doesNotMatch(snapshot.launchText, /launching/);
          assert.equal(oldLaunch.requestBody, '{"game_id":"megadrive-sonic-test"}');
        });
      });
    }

    await t.test('session startup idle has one GET and an explicit safe stop state', async () => {
      await runScenario(harness, 'session-idle', basePlan({
        sessions: [fixture('session-idle.json')],
      }), async () => {
        const snapshot = await harness.waitForText('#session-status', 'No active session.');
        assert.equal(sessionEvidence(harness).length, 1);
        assert.equal(snapshot.sessionBusy, 'false');
        assert.equal(snapshot.sessionStopHidden, true);
        assert.equal(snapshot.sessionStopDisabled, true);
        assertNoPrivateErrorText(snapshot);
      });
    });

    await t.test('active session reconstructs across reload without selecting a catalog card', async () => {
      await runScenario(harness, 'session-active-reload', basePlan({
        sessions: [fixture('session-active.json')],
      }), async () => {
        await harness.waitForText('#session-details', 'Sonic the Hedgehog');
        let snapshot = await harness.snapshot();
        assert.match(snapshot.sessionText, /Sonic the Hedgehog/);
        assert.match(snapshot.sessionText, /Input stateattached.*Input readinessReady/);
        assert.equal(snapshot.detailHeading, 'Select a game');
        assert.equal(snapshot.cards.filter(card => card.pressed).length, 0);
        await harness.reload();
        await harness.waitForText('#session-details', 'Sonic the Hedgehog');
        snapshot = await harness.snapshot();
        assert.equal(sessionEvidence(harness).length, 2);
        assert.match(snapshot.sessionText, /Sonic the Hedgehog/);
        assert.equal(snapshot.detailHeading, 'Select a game');
        assert.equal(snapshot.cards.filter(card => card.pressed).length, 0);
      });
    });

    await t.test('session unavailable and malformed states provide manual retry', async () => {
      await runScenario(harness, 'session-unavailable-retry', basePlan({
        sessions: [
          fixture('session-target-unavailable.json', 503),
          fixture('session-idle.json'),
        ],
      }), async () => {
        let snapshot = await harness.waitForText('#session-status', 'unavailable');
        assert.match(snapshot.sessionMessage, /local target is unavailable|could not be checked/i);
        assert.equal(snapshot.sessionRefreshDisabled, false);
        assertNoPrivateErrorText(snapshot);
        await harness.click('#refresh-session');
        snapshot = await harness.waitForText('#session-status', 'No active session.');
        assert.deepEqual(sessionEvidence(harness).map(record => record.status), [503, 200]);
        assertNoPrivateErrorText(snapshot);
      });

      await runScenario(harness, 'session-malformed-retry', basePlan({
        sessions: [fixture('session-malformed.json'), fixture('session-idle.json')],
      }), async () => {
        let snapshot = await harness.waitForText('#session-status', 'invalid session response');
        assert.match(snapshot.sessionMessage, /invalid session response/i);
        assert.equal(snapshot.sessionRefreshDisabled, false);
        await harness.click('#refresh-session');
        snapshot = await harness.waitForText('#session-status', 'No active session.');
        assert.deepEqual(sessionEvidence(harness).map(record => record.status), [200, 200]);
      });
    });

    await t.test('host-reported stopping and failed states stay visible and safe', async () => {
      await runScenario(harness, 'session-stopping', basePlan({
        sessions: [fixture('session-stopping.json')],
      }), async () => {
        const snapshot = await harness.waitForText('#session-status', 'Stopping session');
        assert.equal(snapshot.sessionStopHidden, true);
        assertNoPrivateErrorText(snapshot);
      });

      await runScenario(harness, 'session-failed', basePlan({
        sessions: [fixture('session-failed.json')],
      }), async () => {
        const snapshot = await harness.waitForText('#session-status', 'failed session');
        assert.match(snapshot.sessionMessage, /failed session/i);
        assertNoPrivateErrorText(snapshot);
      });
    });

    await t.test('newer manual status wins over a held startup response', async () => {
      await runScenario(harness, 'stale-session-status', basePlan({
        sessions: [
          fixture('session-idle.json', 200, { hold: true }),
          fixture('session-active.json'),
        ],
      }), async () => {
        const oldStatus = await harness.waitForRequest({ method: 'GET', path: '/api/v1/session' });
        await harness.click('#refresh-session');
        let snapshot = await harness.waitForText('#session-status', 'Active session');
        assert.match(snapshot.sessionText, /Sonic the Hedgehog/);
        await harness.release(oldStatus.id);
        await harness.waitForSettled();
        snapshot = await harness.snapshot();
        assert.equal(snapshot.sessionStatus, 'Active session');
        assert.equal(sessionEvidence(harness)[0].responseOrder > sessionEvidence(harness)[1].responseOrder, true);
      });
    });

    await t.test('active status loss labels retained details last-known and suppresses Stop until a fresh GET', async () => {
      for (const [name, unavailableResponse, expectedStatus] of [
        ['unavailable', fixture('session-target-unavailable.json', 503), 'unavailable'],
        ['malformed', fixture('session-malformed.json'), 'invalid session response'],
      ]) {
        await runScenario(harness, `session-active-${name}-refresh`, basePlan({
          sessions: [
            fixture('session-active.json'),
            unavailableResponse,
            fixture('session-active.json'),
          ],
        }), async () => {
          await harness.waitForText('#session-status', 'Active session');
          await harness.click('#refresh-session');
          const unavailable = await harness.waitForText('#session-status', expectedStatus);
          assert.match(unavailable.sessionText, /last-known/i);
          assert.equal(unavailable.sessionStopHidden, true);
          assert.equal(unavailable.sessionStopDisabled, true);
          assert.equal(apiEvidence(harness).filter(record => record.path === '/api/v1/session/stop').length, 0);
          await harness.click('#refresh-session');
          const restored = await harness.waitForText('#session-status', 'Active session');
          assert.doesNotMatch(restored.sessionText, /last-known/i);
          assert.equal(restored.sessionStopHidden, false);
          assert.equal(restored.sessionStopDisabled, false);
          assert.equal(apiEvidence(harness).filter(record => record.path === '/api/v1/session/stop').length, 0);
        });
      }
    });

    await t.test('launch replacement disables duplicate and opposite mutation controls until GET reconciliation', async () => {
      await runScenario(harness, 'session-launch-replacement', basePlan({
        sessions: [fixture('session-active-other.json'), fixture('session-active.json')],
        launches: [fixture('launch-success.json', 200, { hold: true })],
      }), async () => {
        await selectSonic(harness);
        let snapshot = await harness.snapshot();
        assert.equal(snapshot.launchButtonLabel, 'Replace active session');
        await harness.click('#launch-actions button');
        const launch = await harness.waitForRequest({ method: 'POST', path: '/api/v1/session/launch' });
        snapshot = await harness.waitForText('#session-status', 'Launching session');
        assert.equal(snapshot.launchButtonDisabled, true);
        assert.equal(snapshot.sessionStopHidden, false);
        assert.equal(snapshot.sessionStopDisabled, true);
        assert.match(snapshot.launchButtonDescribedBy, /launch-reason/);
        assert.match(snapshot.sessionStopDescribedBy, /session-action-reason/);
        await harness.click('#launch-actions button');
        await harness.click('#stop-session');
        assert.equal(apiEvidence(harness).filter(record => record.path === '/api/v1/session/launch').length, 1);
        await harness.release(launch.id);
        snapshot = await harness.waitForText('#launch-status', 'launch_success');
        assert.match(snapshot.sessionText, /Sonic the Hedgehog/);
        assert.deepEqual(sessionEvidence(harness).map(record => record.status), [200, 200]);
        assert.equal(launch.requestBody, '{"game_id":"megadrive-sonic-test"}');
      });
    });

    await t.test('stop success sends one empty POST, reconciles idle, and moves focus to Refresh', async () => {
      await runScenario(harness, 'session-stop-success', basePlan({
        sessions: [fixture('session-active.json'), fixture('session-idle.json')],
        stops: [fixture('stop-success.json', 200, { hold: true })],
      }), async () => {
        await harness.waitForText('#session-status', 'Active session');
        await harness.click('#stop-session');
        const stop = await harness.waitForRequest({ method: 'POST', path: '/api/v1/session/stop' });
        let snapshot = await harness.waitForText('#session-status', 'Stopping session');
        assert.equal(snapshot.sessionStopDisabled, true);
        assert.equal(snapshot.sessionRefreshDisabled, true);
        await harness.click('#stop-session');
        assert.equal(apiEvidence(harness).filter(record => record.path === '/api/v1/session/stop').length, 1);
        await harness.release(stop.id);
        snapshot = await harness.waitForText('#session-status', 'Session stopped.');
        assert.equal(snapshot.sessionStopHidden, true);
        assert.equal(snapshot.activeElementID, 'refresh-session');
        assert.deepEqual(sessionEvidence(harness).map(record => record.status), [200, 200]);
        assert.equal(stop.requestBody, '');
        assert.equal(stop.requestContentType, '');
      });
    });

    await t.test('stop HTTP failure reconciles active state and leaves a safe retry control', async () => {
      await runScenario(harness, 'session-stop-error', basePlan({
        sessions: [fixture('session-active.json'), fixture('session-active.json')],
        stops: [fixture('stop-error.json', 500)],
      }), async () => {
        await harness.click('#stop-session');
        const snapshot = await harness.waitForText('#session-status', 'could not be confirmed');
        assert.equal(snapshot.sessionStopHidden, false);
        assert.equal(snapshot.sessionStopDisabled, false);
        assert.match(snapshot.sessionMessage, /another launch|could not be stopped|error/i);
        const stop = apiEvidence(harness).find(record => record.path === '/api/v1/session/stop');
        assert.equal(stop.status, 500);
        assert.equal(stop.requestBody, '');
        assert.equal(stop.requestContentType, '');
        assert.deepEqual(sessionEvidence(harness).map(record => record.status), [200, 200]);
        assertNoPrivateErrorText(snapshot);
      });
    });

    await t.test('malformed stop response is warned, not announced as stopped, after idle reconciliation', async () => {
      await runScenario(harness, 'session-stop-malformed', basePlan({
        sessions: [fixture('session-active.json'), fixture('session-idle.json')],
        stops: [fixture('stop-malformed.json')],
      }), async () => {
        await harness.waitForText('#session-status', 'Active session');
        await harness.click('#stop-session');
        const snapshot = await harness.waitForText('#session-status', 'invalid session response');
        assert.doesNotMatch(snapshot.sessionStatus, /Session stopped/);
        assert.match(snapshot.sessionMessage, /invalid stop response/i);
        assert.equal(snapshot.sessionStopHidden, true);
        const stop = apiEvidence(harness).find(record => record.path === '/api/v1/session/stop');
        assert.equal(stop.status, 200);
        assert.equal(stop.requestBody, '');
        assert.equal(stop.requestContentType, '');
        assertNoPrivateErrorText(snapshot);
      });
    });

    await t.test('held stop completion does not change a replacement selection or issue launch', async () => {
      await runScenario(harness, 'stale-stop-selection', basePlan({
        sessions: [fixture('session-active-other.json'), fixture('session-idle.json')],
        stops: [fixture('stop-success.json', 200, { hold: true })],
      }), async () => {
        await selectSonic(harness);
        await harness.click('#stop-session');
        const stop = await harness.waitForRequest({ method: 'POST', path: '/api/v1/session/stop' });
        await harness.click('#catalog-list .game-card:nth-child(2)');
        await harness.waitForDetailHeading('Unknown <Game> detail');
        const during = await harness.snapshot();
        assert.equal(during.launchButtonDisabled, true);
        assert.equal(apiEvidence(harness).filter(record => record.method === 'POST' && record.path.endsWith('/launch')).length, 0);
        await harness.click('#launch-actions button');
        assert.equal(apiEvidence(harness).filter(record => record.method === 'POST' && record.path.endsWith('/launch')).length, 0);
        await harness.release(stop.id);
        const snapshot = await harness.waitForText('#session-status', 'Session stopped.');
        assert.equal(snapshot.detailHeading, 'Unknown <Game> detail');
        assert.equal(snapshot.cards[1].pressed, true);
      });
    });

    await t.test('narrow viewport keeps controls readable and reduced motion disables transitions', async () => {
      await runScenario(harness, 'session-narrow-reduced-motion', basePlan({
        sessions: [fixture('session-active.json')],
      }), async () => {
        await harness.setViewport(360, 800);
        await harness.setReducedMotion(true);
        await harness.waitForSettled();
        const layout = await harness.evaluate(`(() => {
          const ids = ['session-panel', 'refresh-session', 'stop-session'];
          const elements = ids.map(id => document.getElementById(id)).filter(Boolean);
          const bounds = elements.map(node => {
            const rect = node.getBoundingClientRect();
            return { id: node.id, left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom, hidden: node.hidden };
          });
          const styles = Array.from(document.querySelectorAll('*')).map(node => getComputedStyle(node));
          return {
            viewport: document.documentElement.clientWidth,
            scrollWidth: document.documentElement.scrollWidth,
            bounds,
            transitions: styles.filter(style => style.transitionDuration !== '0s' && style.transitionDuration !== '0ms').length,
            animations: styles.filter(style => style.animationDuration !== '0s' && style.animationDuration !== '0ms').length,
          };
        })()`);
        assert.equal(layout.scrollWidth <= layout.viewport, true, JSON.stringify(layout));
        assert.equal(layout.transitions, 0, JSON.stringify(layout));
        assert.equal(layout.animations, 0, JSON.stringify(layout));
        assert.ok(layout.bounds.every(bound => bound.hidden || (bound.left >= 0 && bound.right <= layout.viewport)), JSON.stringify(layout));
        const snapshot = await harness.snapshot();
        assert.equal(snapshot.sessionBusy, 'false');
        assert.match(snapshot.sessionStopDescribedBy, /session-action-reason|^$/);
      });
    });
  } finally {
    const report = harness.report();
    process.stdout.write(`FOGCAST_BROWSER_EVIDENCE ${JSON.stringify(report)}\n`);
    await harness.close();
  }
});
