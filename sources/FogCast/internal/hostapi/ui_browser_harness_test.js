'use strict';

const assert = require('node:assert/strict');
const {EventEmitter} = require('node:events');
const test = require('node:test');
const {BrowserHarness, BrowserPage, FixtureServer, fixture, normalizePlan} = require('./ui_browser_harness');

test('video fixture routes require explicit closed-profile declarations', () => {
  const reply = fixture('catalog-empty.json', 200, {override:[]});
  for (const route of ['GET /api/v1/library/video-parts',
    'POST /api/v1/library/video-parts/direct', 'POST /api/v1/library/video-parts/scanlines',
    'GET /api/v1/library/core-entries/browser-title/video']) {
    assert.equal(normalizePlan({coreRoutes:{[route]:reply}}).coreRoutes.has(route), true);
  }
  for (const route of ['GET /api/v1/library/video-parts/*',
    'POST /api/v1/library/video-parts/crt', 'PUT /api/v1/library/video-parts/direct',
    'GET /api/v1/library/video-parts?profile=direct']) {
    assert.throws(() => normalizePlan({coreRoutes:{[route]:reply}}), /invalid core fixture route/);
  }
});

test('fixture settings preserve household video preference across partial writes', async () => {
  const server = new FixtureServer();
  try {
    const origin = await server.start();
    server.configure({});
    const settings = async (method, body) => {
      const response = await fetch(origin + '/api/v1/library/settings', {method,
        headers:{'Content-Type':'application/json'}, ...(body ? {body:JSON.stringify(body)} : {})});
      assert.equal(response.status, 200);
      return response.json();
    };
    assert.equal((await settings('GET')).video_profile, 'direct');
    assert.equal((await settings('PATCH', {video_profile:'scanlines'})).video_profile, 'scanlines');
    const patched = await settings('PATCH', {attract_idle_seconds:42});
    assert.equal(patched.video_profile, 'scanlines');
    assert.equal(patched.attract_idle_seconds, 42);
    const written = await settings('PUT', {preferred_regions:['japan']});
    assert.equal(written.video_profile, 'scanlines');
    assert.deepEqual(written.preferred_regions, ['japan']);
    assert.equal((await settings('GET')).video_profile, 'scanlines');
    assert.deepEqual(server.evidence().filter(record => record.unexpected), []);
  } finally {
    await server.close();
  }
});

test('unplanned video reads and query variants remain unexpected requests', async () => {
  const server = new FixtureServer();
  try {
    const origin = await server.start();
    server.configure({coreRoutes:{'GET /api/v1/library/video-parts':
      fixture('catalog-empty.json', 200, {override:[]})}});
    const planned = await fetch(origin + '/api/v1/library/video-parts');
    assert.equal(planned.status, 200);
    assert.deepEqual(await planned.json(), []);
    for (const path of ['/api/v1/library/core-entries/browser-title/video',
      '/api/v1/library/video-parts?profile=scanlines']) {
      const response = await fetch(origin + path);
      assert.equal(response.status, 404);
      assert.equal((await response.json()).error.code, 'TEST_UNEXPECTED_REQUEST');
    }
    assert.equal(server.evidence().filter(record => record.unexpected).length, 2);
  } finally {
    await server.close();
  }
});

function extra(page, requestId) {
  page.handleEvent({sessionId: 'session', method: 'Network.responseReceivedExtraInfo',
    params: {requestId, statusCode: 200}});
}

test('reload retires outgoing-document ExtraInfo before the next network evidence window', async () => {
  const order = [];
  const connection = new EventEmitter();
  const harness = new BrowserHarness();
  harness.fixtureServer.origin = 'http://127.0.0.1:1234';
  const page = harness.page = new BrowserPage(connection, 'target', 'session', harness.fixtureServer.origin);
  connection.send = async method => {
    order.push(method);
    if (method === 'Network.disable') extra(page, 'old-in-flight');
  };
  let fixtureEpoch = 'old';
  harness.fixtureServer.configure = plan => { order.push('fixture.configure'); fixtureEpoch = plan.epoch; };
  harness.configure({epoch: 'new'});
  assert.equal(fixtureEpoch, 'old');
  page.navigate = async url => {
    order.push(url);
    if (url === 'about:blank') {
      assert.equal(fixtureEpoch, 'old', 'outgoing requests must not consume the new fixture plan');
      // Reproduces Chrome152/153: the retiring renderer never supplies a
      // requestWillBeSent record for this outgoing request.
      page.handleEvent({sessionId: 'session', method: 'Network.requestWillBeSentExtraInfo',
        params: {requestId: 'outgoing-orphan'}});
      extra(page, 'outgoing-orphan');
    } else {
      assert.equal(fixtureEpoch, 'new');
      assert.equal(page.pendingResponseExtraInfo.size, 0);
      page.handleEvent({sessionId: 'session', method: 'Network.requestWillBeSent',
        params: {requestId: 'new-request', request: {url, method: 'GET'}}});
      extra(page, 'new-request');
    }
  };
  try {
    await harness.reload();
    assert.deepEqual(order, ['about:blank', 'Network.disable', 'fixture.configure', 'Network.enable', 'http://127.0.0.1:1234/']);
    assert.equal(page.evidence().networkRequests.length, 1);
    page.assertClean();
    // A genuinely unmatched response in the new document still fails closed.
    extra(page, 'unknown-current-request');
    assert.throws(() => page.assertClean(), /browser network failures/);
    assert.equal(page.pendingResponseExtraInfo.size, 1);
  } finally {
    page.dispose();
  }
});

test('failed network retirement never opens a clean new evidence window', async () => {
  const connection = new EventEmitter();
  const harness = new BrowserHarness();
  harness.fixtureServer.origin = 'http://127.0.0.1:1234';
  const page = harness.page = new BrowserPage(connection, 'target', 'session', harness.fixtureServer.origin);
  const navigated = [];
  page.navigate = async url => { navigated.push(url); extra(page, 'old-orphan'); };
  connection.send = async () => { throw new Error('CDP reset failed'); };
  try {
    await assert.rejects(harness.reload(), /CDP reset failed/);
    assert.deepEqual(navigated, ['about:blank']);
    assert.equal(page.pendingResponseExtraInfo.size, 1);
  } finally {
    page.dispose();
  }
});
