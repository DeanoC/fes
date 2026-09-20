'use strict';

const assert = require('node:assert/strict');
const {EventEmitter} = require('node:events');
const test = require('node:test');
const {BrowserHarness, BrowserPage} = require('./ui_browser_harness');

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
