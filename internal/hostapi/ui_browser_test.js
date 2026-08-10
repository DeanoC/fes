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
  waitForProcessGroupQuiescence,
} = require('./ui_browser_harness.js');

const REQUIRED = process.env.FOGCAST_BROWSER_REQUIRED === '1';
const SONIC_ID = 'megadrive-sonic-test';
const UNKNOWN_ID = 'snes-unknown-test';

function populatedCatalog() {
  return fixture('catalog-populated.json');
}

function defaultDetails() {
  return {
    [SONIC_ID]: fixture('detail-refreshed.json'),
    [UNKNOWN_ID]: fixture('detail-unknown.json'),
  };
}

function basePlan(overrides = {}) {
  return {
    catalog: { '': populatedCatalog() },
    details: defaultDetails(),
    launches: [fixture('launch-success.json')],
    ...overrides,
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
    .filter(record => record.path.startsWith('/api/'));
}

function detailEvidence(harness) {
  return apiEvidence(harness)
    .filter(record => record.path.startsWith('/api/v1/games/'));
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
      .filter(record => record.path === '/' || record.path === '/api/v1/games')
      .map(record => ({
        method: record.method,
        path: record.path,
        status: record.status,
      }));
    assert.deepEqual(preflightRequests, [
      { method: 'GET', path: '/', status: 200 },
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
        assert.equal(snapshot.cards[0].fallback, false);
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
        assert.match(snapshot.catalogText, /live catalog is empty/i);
        await harness.setSearch('sonic & tails');
        snapshot = await harness.waitForCatalog('no_matches');
        assert.match(snapshot.catalogText, /no matches/i);
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
      await t.test(`metadata ${metadataMode} fallback leaves browse, detail, and launch usable`, async () => {
        await runScenario(harness, `metadata-${metadataMode}`, basePlan({ metadataMode }), async () => {
          let snapshot = await harness.waitForCatalog('populated metadata_fallback');
          assert.equal(snapshot.cards.length, 3);
          assert.ok(snapshot.cards.every(card => card.fallback));
          await harness.click('#catalog-list .game-card');
          await harness.waitForDetailHeading('Sonic the Hedgehog (detail refresh)');
          await launchSelected(harness);
          snapshot = await harness.waitForText('#launch-status', 'launch_success');
          assert.match(snapshot.detailText, /metadata_fallback/);
          assert.equal(snapshot.launchText.includes('launch_success'), true);
          const launch = apiEvidence(harness).find(record => record.method === 'POST');
          assert.equal(launch.requestBody, '{"game_id":"megadrive-sonic-test"}');
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
          assert.equal(oldLaunch.requestBody, '{"game_id":"megadrive-sonic-test"}');
        });
      });
    }
  } finally {
    const report = harness.report();
    process.stdout.write(`FOGCAST_BROWSER_EVIDENCE ${JSON.stringify(report)}\n`);
    await harness.close();
  }
});
