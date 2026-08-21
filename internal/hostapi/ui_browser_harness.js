'use strict';

const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

const ROOT = __dirname;
const FIXTURE_ROOT = path.join(ROOT, 'testdata', 'ui');
const LOOPBACK_HOST = '127.0.0.1';
const CHROME_READY_TIMEOUT_MS = 10_000;
const NAVIGATION_TIMEOUT_MS = 5_000;
const REQUEST_TIMEOUT_MS = 3_000;
const SUITE_POLL_MS = 25;
const EVIDENCE_SETTLE_QUIET_MS = 100;
const MAX_CAPTURE_BYTES = 64 * 1024;
const MAX_EVIDENCE_TEXT = 240;
const MAX_PENDING_RESPONSE_EXTRA_INFO = 64;
const MAX_RETIRED_NETWORK_REQUEST_IDS = 4_096;
const CHROME_FAILED_RESOURCE_PATTERN = /^Failed to load resource: the server responded with a status of ([45]\d\d) \([^()\r\n]*\)$/;

function boundedText(value, fallback = '', limit = MAX_EVIDENCE_TEXT) {
  if (typeof value !== 'string') return fallback;
  const text = value.replace(/[\u0000-\u001f\u007f]/g, ' ').trim();
  return text ? text.slice(0, limit) : fallback;
}

function validNetworkRequestID(value) {
  return typeof value === 'string' && value.length > 0 && value.length <= 80;
}

function validHTTPStatus(value) {
  return Number.isInteger(value) && value >= 100 && value <= 599;
}

function fixture(name, status = 200, options = {}) {
  if (!/^[A-Za-z0-9._-]+\.json$/.test(name)) {
    throw new TypeError(`invalid UI fixture name: ${name}`);
  }
  if (!Number.isInteger(status) || status < 100 || status > 599) {
    throw new TypeError(`invalid UI fixture status: ${status}`);
  }
  const delayMs = options.delayMs === undefined ? 0 : Number(options.delayMs);
  if (!Number.isInteger(delayMs) || delayMs < 0 || delayMs > REQUEST_TIMEOUT_MS) {
    throw new TypeError(`invalid UI fixture delay: ${options.delayMs}`);
  }
  return Object.freeze({
    fixture: name,
    status,
    hold: options.hold === true,
    delayMs,
    ...(options.override ? { override: options.override } : {}),
  });
}

function artworkFixture(status = 200, options = {}) {
  if (!Number.isInteger(status) || status < 100 || status > 599) {
    throw new TypeError(`invalid UI artwork status: ${status}`);
  }
  const delayMs = options.delayMs === undefined ? 0 : Number(options.delayMs);
  if (!Number.isInteger(delayMs) || delayMs < 0 || delayMs > REQUEST_TIMEOUT_MS) {
    throw new TypeError(`invalid UI artwork delay: ${options.delayMs}`);
  }
  const body = options.body === undefined
    ? Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')
    : Buffer.from(options.body);
  if (body.length > MAX_CAPTURE_BYTES) throw new TypeError('UI artwork fixture is too large');
  return Object.freeze({
    fixture: 'artwork',
    status,
    body,
    hold: options.hold === true,
    delayMs,
  });
}

function readFixturePayload(name) {
  const fixturePath = path.join(FIXTURE_ROOT, name);
  if (path.dirname(fixturePath) !== FIXTURE_ROOT) {
    throw new Error(`fixture path escaped fixture root: ${name}`);
  }
  return JSON.parse(fs.readFileSync(fixturePath, 'utf8'));
}

function readAsset(name) {
  return fs.readFileSync(path.join(ROOT, name), 'utf8');
}

function assembleProductionHTML(metadataMode = 'normal', options = {}) {
  const mode = metadataMode || 'normal';
  if (!['normal', 'missing', 'malformed'].includes(mode)) {
    throw new TypeError(`invalid metadata mode: ${mode}`);
  }
  const shell = readAsset('ui_shell.html');
  const styles = readAsset('ui.css');
  const app = readAsset('ui_app.js');
  const metadata = readAsset('ui_metadata.js');
  const attractDisabled = options.attractDisabled !== false;
  const attractIdle = Number(options.attractIdleMs) > 0
    ? `globalThis.FogCastAttractIdleMs = ${Number(options.attractIdleMs)};`
    : '';
  const prefetch = options.prefetchVisibleCovers === true
    ? 'globalThis.FogCastPrefetchVisibleCovers = true;'
    : '';
  const replacements = {
    '{{FOGCAST_STYLES}}': `<style>${styles}</style>`,
    '{{FOGCAST_APP}}': `<script>globalThis.FogCastPresentationEnabled = true;${prefetch}globalThis.FogCastAttractDisabled = ${attractDisabled};${attractIdle}</script><script>${app}</script>`,
  };
  if (mode === 'normal') {
    replacements['{{FOGCAST_METADATA}}'] = `<script>${metadata}</script>`;
  } else if (mode === 'missing') {
    replacements['{{FOGCAST_METADATA}}'] = '<script>globalThis.FogCastMetadata = undefined;</script>';
  } else {
    const malformed = JSON.stringify(readFixturePayload('metadata-malformed.json'));
    replacements['{{FOGCAST_METADATA}}'] = `<script>globalThis.FogCastMetadata = Object.freeze({metadataFor() { return ${malformed}; }});</script>`;
  }
  let html = shell;
  for (const [placeholder, replacement] of Object.entries(replacements)) {
    if (html.split(placeholder).length - 1 !== 1) {
      throw new Error(`UI asset placeholder must occur exactly once: ${placeholder}`);
    }
    html = html.replace(placeholder, replacement);
  }
  return html;
}

function normalizeQueue(value, label) {
  const values = Array.isArray(value) ? value.slice() : [value];
  if (values.length === 0) throw new TypeError(`${label} response queue cannot be empty`);
  for (const item of values) {
    if (!item || typeof item.fixture !== 'string') {
      throw new TypeError(`${label} response must be created with fixture()`);
    }
  }
  return values;
}

function normalizePlan(plan = {}) {
  const catalog = plan.catalog || { '': fixture('catalog-populated.json') };
  const details = plan.details || {};
  const presentations = plan.presentations || {};
  const artworks = plan.artworks || {};
  const sessions = plan.sessions || [fixture('session-idle.json')];
  const launches = plan.launches || [fixture('launch-success.json')];
  const stops = plan.stops || [fixture('stop-success.json')];
  const catalogQueues = new Map();
  for (const [query, value] of Object.entries(catalog)) {
    catalogQueues.set(String(query), normalizeQueue(value, `catalog[${query}]`));
  }
  const homeKeys = ['collection=continue', 'collection=favorites', 'collection=recents'];
  (Array.isArray(plan.collections) ? plan.collections : []).forEach(item => {
    if (item && item.id) homeKeys.push(`collection=${item.id}`);
  });
  homeKeys.forEach(key => {
    if (!catalogQueues.has(key)) {
      catalogQueues.set(key, normalizeQueue(fixture('catalog-empty.json'), `catalog[${key}]`));
    }
  });
  const detailQueues = new Map();
  for (const [id, value] of Object.entries(details)) {
    detailQueues.set(String(id), normalizeQueue(value, `details[${id}]`));
  }
  const presentationQueues = new Map();
  for (const [id, value] of Object.entries(presentations)) {
    presentationQueues.set(String(id), normalizeQueue(value, `presentations[${id}]`));
  }
  const artworkQueues = new Map();
  for (const [handle, value] of Object.entries(artworks)) {
    artworkQueues.set(String(handle), normalizeQueue(value, `artworks[${handle}]`));
  }
  return {
    metadataMode: plan.metadataMode || 'normal',
    html: assembleProductionHTML(plan.metadataMode || 'normal', {
      attractDisabled: plan.attractDisabled,
      attractIdleMs: plan.attractIdleMs,
      prefetchVisibleCovers: plan.prefetchVisibleCovers === true,
    }),
    attract: plan.attract || { items: [], idle_seconds: 60 },
    settings: plan.settings || { attract_idle_seconds: 60, preferred_regions: ['usa', 'world', 'europe', 'japan'] },
    platforms: plan.platforms || { platforms: [] },
    collections: Array.isArray(plan.collections) ? plan.collections.slice() : [],
    catalogQueues,
    detailQueues,
    presentationQueues,
    artworkQueues,
    sessionQueue: normalizeQueue(sessions, 'sessions'),
    launchQueue: normalizeQueue(launches, 'launches'),
    stopQueue: normalizeQueue(stops, 'stops'),
  };
}

function takeQueue(queueMap, key) {
  const queue = queueMap.get(key) || queueMap.get('*');
  if (!queue || queue.length === 0) return null;
  if (queue.length === 1) return queue[0];
  return queue.shift();
}

function responseBody(response, html) {
  if (response.fixture === 'ui-assets') return Buffer.from(html, 'utf8');
  if (response.fixture === 'artwork') return Buffer.from(response.body);
  return Buffer.from(JSON.stringify(readFixturePayload(response.fixture)), 'utf8');
}

function wait(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function waitForProcessGroupQuiescence(pid, timeoutMs) {
  if (process.platform === 'win32' || !pid) return true;
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      process.kill(-pid, 0);
    } catch (error) {
      if (error.code === 'ESRCH') return true;
    }
    await wait(SUITE_POLL_MS);
  }
  try {
    process.kill(-pid, 0);
    return false;
  } catch (error) {
    return error.code === 'ESRCH';
  }
}

function withTimeout(promise, timeoutMs, message, code = 'TIMEOUT') {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => {
      const error = new Error(message);
      error.code = code;
      reject(error);
    }, timeoutMs);
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer));
}

class FixtureServer extends EventEmitter {
  constructor() {
    super();
    this.server = null;
    this.origin = null;
    this.plan = null;
    this.records = [];
    this.held = new Map();
    this.timers = new Set();
    this.nextID = 1;
    this.responseOrder = 0;
  }

  async start() {
    if (this.server) throw new Error('fixture server already started');
    this.server = http.createServer((request, response) => {
      void this.handle(request, response).catch(error => {
        const record = { method: request.method || '', path: '<handler-error>', query: '', unexpected: true };
        this.records.push(record);
        response.destroy(error);
      });
    });
    this.server.keepAliveTimeout = 1_000;
    this.server.headersTimeout = REQUEST_TIMEOUT_MS;
    this.server.requestTimeout = REQUEST_TIMEOUT_MS;
    await new Promise((resolve, reject) => {
      const onError = error => {
        this.server.off('listening', onListening);
        reject(error);
      };
      const onListening = () => {
        this.server.off('error', onError);
        resolve();
      };
      this.server.once('error', onError);
      this.server.listen(0, LOOPBACK_HOST, onListening);
    });
    const address = this.server.address();
    if (!address || typeof address === 'string' || address.address !== LOOPBACK_HOST) {
      throw new Error(`fixture server did not bind IPv4 loopback: ${JSON.stringify(address)}`);
    }
    this.origin = `http://${LOOPBACK_HOST}:${address.port}`;
    return this.origin;
  }

  configure(plan) {
    if (!this.server) throw new Error('fixture server is not started');
    if (this.held.size !== 0) throw new Error('cannot reconfigure while responses are held');
    this.plan = normalizePlan(plan);
    this.records = [];
    this.nextID = 1;
    this.responseOrder = 0;
    for (const timer of this.timers) clearTimeout(timer);
    this.timers.clear();
  }

  async readRequestBody(request) {
    const chunks = [];
    let total = 0;
    for await (const chunk of request) {
      total += chunk.length;
      if (total > MAX_CAPTURE_BYTES) {
        request.destroy();
        throw new Error('fixture request body exceeded bounded capture size');
      }
      chunks.push(chunk);
    }
    return Buffer.concat(chunks).toString('utf8');
  }

  newRecord(request, url) {
    const record = {
      id: this.nextID++,
      method: request.method || '',
      path: url.pathname,
      query: url.searchParams.get('q') || '',
      status: null,
      fixture: null,
      requestContentType: request.headers['content-type'] || '',
      requestBody: '',
      responseOrder: null,
      unexpected: false,
        headersSent: false,
        receivedOrder: this.records.length,
    };
    this.records.push(record);
    this.emit('request', record);
    return record;
  }

  unexpectedResponse(record, status, message) {
    record.unexpected = true;
    return {
      fixture: 'catalog-error.json',
      status,
      hold: false,
      delayMs: 0,
      override: { error: { code: 'TEST_UNEXPECTED_REQUEST', message } },
    };
  }

  async handle(request, response) {
    if (!this.plan) {
      response.writeHead(503, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ error: { code: 'FIXTURE_NOT_CONFIGURED' } }));
      return;
    }
    let url;
    try {
      url = new URL(request.url || '/', this.origin);
    } catch (_) {
      response.writeHead(400, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ error: { code: 'BAD_REQUEST' } }));
      return;
    }
    const record = this.newRecord(request, url);
    if (url.origin !== this.origin) {
      await this.deliver(record, response, this.unexpectedResponse(record, 400, 'request origin was not fixture loopback'));
      return;
    }

    if (request.method === 'GET' && url.pathname === '/') {
      await this.deliver(record, response, {
        fixture: 'ui-assets', status: 200, hold: false, delayMs: 0, html: this.plan.html,
      });
      return;
    }
    if (request.method === 'GET' && url.pathname === '/favicon.ico') {
      await this.deliver(record, response, {
        fixture: 'favicon', status: 204, hold: false, delayMs: 0, body: Buffer.alloc(0),
      });
      return;
    }
    if (url.pathname === '/api/v1/games' && request.method === 'GET') {
      const allowed = new Set([
        'q', 'platform', 'collection', 'sort', 'cursor', 'limit',
        'region', 'genre', 'year', 'availability', 'hide_prerelease', 'hide_hacks', 'grouped',
      ]);
      if (url.searchParams.getAll('q').length > 1 || [...url.searchParams.keys()].some(key => !allowed.has(key))) {
        await this.deliver(record, response, this.unexpectedResponse(record, 400, 'unexpected catalog query'));
        return;
      }
      const q = url.searchParams.get('q') || '';
      const platform = url.searchParams.get('platform') || '';
      const collection = url.searchParams.get('collection') || '';
      record.query = q;
      if (platform) record.query = q ? `${q}&platform=${platform}` : `platform=${platform}`;
      else if (collection) record.query = q ? `${q}&collection=${collection}` : `collection=${collection}`;
      const catalogKey = platform ? `platform=${platform}` : collection ? `collection=${collection}` : q;
      const selected = takeQueue(this.plan.catalogQueues, catalogKey);
      await this.deliver(record, response, selected || this.unexpectedResponse(record, 500, `catalog query not configured: ${boundedText(record.query)}`));
      return;
    }
    const detailMatch = url.pathname.match(/^\/api\/v1\/games\/([^/]+)$/);
    if (detailMatch && request.method === 'GET') {
      let id;
      try {
        id = decodeURIComponent(detailMatch[1]);
      } catch (_) {
        await this.deliver(record, response, this.unexpectedResponse(record, 400, 'invalid detail ID encoding'));
        return;
      }
      record.query = id;
      const selected = takeQueue(this.plan.detailQueues, id);
      await this.deliver(record, response, selected || this.unexpectedResponse(record, 500, `detail ID not configured: ${boundedText(id)}`));
      return;
    }
    const presentationMatch = url.pathname.match(/^\/api\/v1\/presentation\/games\/([^/]+)$/);
    if (presentationMatch && request.method === 'GET') {
      if (url.search) {
        await this.deliver(record, response, this.unexpectedResponse(record, 400, 'unexpected presentation query'));
        return;
      }
      let id;
      try {
        id = decodeURIComponent(presentationMatch[1]);
      } catch (_) {
        await this.deliver(record, response, this.unexpectedResponse(record, 400, 'invalid presentation ID encoding'));
        return;
      }
      record.query = id;
      const selected = takeQueue(this.plan.presentationQueues, id);
      await this.deliver(record, response, selected || this.unexpectedResponse(record, 500, `presentation ID not configured: ${boundedText(id)}`));
      return;
    }
    const artworkMatch = url.pathname.match(/^\/api\/v1\/presentation\/artwork\/([0-9a-f]{64})$/);
    if (artworkMatch && request.method === 'GET') {
      if (url.search) {
        await this.deliver(record, response, this.unexpectedResponse(record, 400, 'unexpected artwork query'));
        return;
      }
      const handle = artworkMatch[1];
      record.query = handle;
      const selected = takeQueue(this.plan.artworkQueues, handle);
      await this.deliver(record, response, selected || this.unexpectedResponse(record, 500, `artwork handle not configured: ${boundedText(handle)}`));
      return;
    }
    if (url.pathname === '/api/v1/platforms' && request.method === 'GET') {
      await this.deliver(record, response, {
        fixture: 'platforms.json', status: 200, hold: false, delayMs: 0,
        override: this.plan.platforms || { platforms: [] },
      });
      return;
    }
    if (url.pathname === '/api/v1/library/settings' && (request.method === 'GET' || request.method === 'PUT' || request.method === 'PATCH')) {
      if (request.method === 'GET') {
        await this.deliver(record, response, {
          fixture: 'settings.json', status: 200, hold: false, delayMs: 0,
          override: this.plan.settings || { attract_idle_seconds: 60, preferred_regions: ['usa', 'world', 'europe', 'japan'] },
        });
        return;
      }
      record.requestBody = await this.readRequestBody(request);
      let written = {};
      try {
        written = record.requestBody ? JSON.parse(record.requestBody) : {};
      } catch (_) {
        await this.deliver(record, response, this.unexpectedResponse(record, 400, 'settings body was not JSON'));
        return;
      }
      const current = this.plan.settings || { attract_idle_seconds: 60, preferred_regions: ['usa', 'world', 'europe', 'japan'] };
      this.plan.settings = {
        attract_idle_seconds: Number.isFinite(written.attract_idle_seconds) ? written.attract_idle_seconds : current.attract_idle_seconds,
        preferred_regions: Array.isArray(written.preferred_regions) ? written.preferred_regions : current.preferred_regions,
      };
      await this.deliver(record, response, {
        fixture: 'settings.json', status: 200, hold: false, delayMs: 0,
        override: this.plan.settings,
      });
      return;
    }
    if (url.pathname === '/api/v1/library/attract' && request.method === 'GET') {
      await this.deliver(record, response, {
        fixture: 'attract.json', status: 200, hold: false, delayMs: 0,
        override: this.plan.attract || { items: [], idle_seconds: 60 },
      });
      return;
    }
    if (url.pathname === '/api/v1/library/facets' && request.method === 'GET') {
      await this.deliver(record, response, {
        fixture: 'facets.json', status: 200, hold: false, delayMs: 0,
        override: this.plan.facets || { genres: [], years: [] },
      });
      return;
    }
    const favoriteMatch = url.pathname.match(/^\/api\/v1\/library\/favorites\/([^/]+)$/);
    if (favoriteMatch && (request.method === 'PUT' || request.method === 'DELETE')) {
      await this.deliver(record, response, {
        fixture: 'favorite.json', status: 200, hold: false, delayMs: 0,
        override: { id: decodeURIComponent(favoriteMatch[1]), favorite: request.method === 'PUT' },
      });
      return;
    }
    if (url.pathname === '/api/v1/library/collections' && request.method === 'GET') {
      await this.deliver(record, response, {
        fixture: 'collections.json', status: 200, hold: false, delayMs: 0,
        override: { collections: this.plan.collections || [] },
      });
      return;
    }
    const collectionMemberMatch = url.pathname.match(/^\/api\/v1\/library\/collections\/([^/]+)\/([^/]+)$/);
    if (collectionMemberMatch && (request.method === 'PUT' || request.method === 'DELETE')) {
      await this.deliver(record, response, {
        fixture: 'collection-member.json', status: 200, hold: false, delayMs: 0,
        override: {
          id: decodeURIComponent(collectionMemberMatch[2]),
          collection: decodeURIComponent(collectionMemberMatch[1]),
          member: request.method === 'PUT',
        },
      });
      return;
    }
    const collectionMatch = url.pathname.match(/^\/api\/v1\/library\/collections\/([^/]+)$/);
    if (collectionMatch && (request.method === 'PUT' || request.method === 'DELETE')) {
      const id = decodeURIComponent(collectionMatch[1]);
      await this.deliver(record, response, {
        fixture: 'collection.json', status: 200, hold: false, delayMs: 0,
        override: request.method === 'DELETE'
          ? { id }
          : { id, name: url.searchParams.get('name') || id },
      });
      return;
    }
    const mediaMatch = url.pathname.match(/^\/api\/v1\/presentation\/media\/([0-9a-f]{64})$/);
    if (mediaMatch && request.method === 'GET') {
      await this.deliver(record, response, this.unexpectedResponse(record, 404, 'media handle was not configured'));
      return;
    }
    if (url.pathname === '/api/v1/session' && request.method === 'GET') {
      const selected = this.plan.sessionQueue.length === 1
        ? this.plan.sessionQueue[0]
        : this.plan.sessionQueue.shift();
      await this.deliver(record, response, selected || this.unexpectedResponse(record, 500, 'session response queue exhausted'));
      return;
    }
    if (url.pathname === '/api/v1/session/launch' && request.method === 'POST') {
      record.requestBody = await this.readRequestBody(request);
      const selected = this.plan.launchQueue.length === 1
        ? this.plan.launchQueue[0]
        : this.plan.launchQueue.shift();
      await this.deliver(record, response, selected || this.unexpectedResponse(record, 500, 'launch response queue exhausted'));
      return;
    }
    if (url.pathname === '/api/v1/session/stop' && request.method === 'POST') {
      record.requestBody = await this.readRequestBody(request);
      const selected = this.plan.stopQueue.length === 1
        ? this.plan.stopQueue[0]
        : this.plan.stopQueue.shift();
      await this.deliver(record, response, selected || this.unexpectedResponse(record, 500, 'stop response queue exhausted'));
      return;
    }
    await this.deliver(record, response, this.unexpectedResponse(record, 404, 'unexpected fixture route'));
  }

  async deliver(record, response, selected) {
    const plan = { ...selected };
    if (plan.fixture === 'ui-assets') plan.html = selected.html || this.plan.html;
    if (plan.fixture === 'favicon') plan.body = Buffer.alloc(0);
    if (plan.hold) {
      const contentType = plan.fixture === 'favicon'
        ? 'image/x-icon'
        : plan.fixture === 'artwork'
          ? 'image/png'
          : plan.fixture === 'ui-assets'
            ? 'text/html; charset=utf-8'
            : 'application/json';
      response.writeHead(plan.status, {
        'Cache-Control': 'no-store',
        Connection: 'close',
        'Content-Type': contentType,
      });
      response.flushHeaders?.();
      record.headersSent = true;
      await new Promise((resolve, reject) => {
        this.held.set(record.id, { record, response, plan, resolve, reject });
      });
      return;
    }
    if (plan.delayMs) {
      await new Promise(resolve => {
        const timer = setTimeout(() => {
          this.timers.delete(timer);
          resolve();
        }, plan.delayMs);
        this.timers.add(timer);
      });
    }
    this.finish(record, response, plan);
  }

  finish(record, response, plan) {
    if (record.status !== null) return;
    const body = plan.body || (plan.override
      ? Buffer.from(JSON.stringify(plan.override), 'utf8')
      : responseBody(plan, plan.html || (this.plan && this.plan.html)));
    record.fixture = plan.fixture;
    record.status = plan.status;
    record.responseOrder = ++this.responseOrder;
    record.responseBytes = body.length;
    const contentType = plan.fixture === 'favicon'
      ? 'image/x-icon'
      : plan.fixture === 'artwork'
        ? 'image/png'
        : plan.fixture === 'ui-assets'
          ? 'text/html; charset=utf-8'
          : 'application/json';
    try {
      if (!record.headersSent) {
        response.writeHead(plan.status, {
          'Cache-Control': 'no-store',
          'Content-Type': contentType,
          'Content-Length': body.length,
        });
      }
      response.end(body);
    } catch (_) {
      response.destroy();
    }
    this.emit('response', record);
  }

  async release(id) {
    const held = this.held.get(id);
    if (!held) throw new Error(`no held fixture response ${id}`);
    this.held.delete(id);
    held.resolve();
    if (held.plan.delayMs) await wait(held.plan.delayMs);
    this.finish(held.record, held.response, held.plan);
  }

  async releaseAll() {
    const ids = [...this.held.keys()];
    for (const id of ids) await this.release(id);
  }

  waitForRequest(matcher, timeoutMs = REQUEST_TIMEOUT_MS) {
    const matches = record => {
      if (record._claimed) return false;
      for (const [key, value] of Object.entries(matcher || {})) {
        if (record[key] !== value) return false;
      }
      return true;
    };
    const existing = this.records.find(matches);
    if (existing) {
      existing._claimed = true;
      return Promise.resolve(existing);
    }
    let onRequest;
    const promise = new Promise(resolve => {
      onRequest = record => {
        if (!matches(record)) return;
        record._claimed = true;
        this.off('request', onRequest);
        resolve(record);
      };
      this.on('request', onRequest);
    });
    return withTimeout(promise, timeoutMs, `timed out waiting for fixture request ${JSON.stringify(matcher)}`)
      .finally(() => {
        if (onRequest) this.off('request', onRequest);
      });
  }

  evidence() {
    return this.records.map(record => ({
      id: record.id,
      method: record.method,
      path: record.path,
      query: boundedText(record.query, '', 200),
      status: record.status,
      fixture: record.fixture,
      requestContentType: boundedText(record.requestContentType, '', 120),
      requestBody: record.path === '/api/v1/session/launch' || record.path === '/api/v1/session/stop'
        ? boundedText(record.requestBody, '', 512)
        : undefined,
      responseOrder: record.responseOrder,
      responseBytes: record.responseBytes,
      unexpected: record.unexpected === true,
    }));
  }

  async close() {
    if (!this.server) return;
    await this.releaseAll();
    for (const timer of this.timers) clearTimeout(timer);
    this.timers.clear();
    const server = this.server;
    this.server = null;
    server.closeAllConnections?.();
    server.closeIdleConnections?.();
    await withTimeout(new Promise(resolve => server.close(() => resolve())), REQUEST_TIMEOUT_MS, 'fixture server close timed out');
    if (server.address() !== null) throw new Error('fixture server remained bound after close');
    this.origin = null;
  }
}

class CDPError extends Error {
  constructor(message, code = 'CDP_ERROR') {
    super(message);
    this.name = 'CDPError';
    this.code = code;
  }
}

class CDPConnection extends EventEmitter {
  constructor(child) {
    super();
    this.child = child;
    this.input = child.stdio[3];
    this.output = child.stdio[4];
    this.pending = new Map();
    this.nextID = 1;
    this.buffer = Buffer.alloc(0);
    this.closed = false;
    if (!this.input || !this.output) throw new Error('Chrome CDP pipe descriptors 3 and 4 were not created');
    this.output.on('data', chunk => this.consume(chunk));
    this.output.on('error', error => this.failAll(new CDPError(`CDP output failed: ${boundedText(error.message)}`, 'CDP_IO')));
    child.once('error', error => this.failAll(new CDPError(`Chrome spawn failed: ${boundedText(error.message)}`, 'CHROME_SPAWN')));
    child.once('exit', (code, signal) => {
      this.failAll(new CDPError(`Chrome exited before CDP closed (${code === null ? 'signal' : code})`, 'CHROME_EXIT'));
      this.emit('childExit', { code, signal });
    });
  }

  consume(chunk) {
    if (this.closed) return;
    this.buffer = Buffer.concat([this.buffer, chunk]);
    let separator;
    while ((separator = this.buffer.indexOf(0)) >= 0) {
      const frame = this.buffer.subarray(0, separator);
      this.buffer = this.buffer.subarray(separator + 1);
      if (frame.length === 0) continue;
      let message;
      try {
        message = JSON.parse(frame.toString('utf8'));
      } catch (_) {
        this.failAll(new CDPError('Chrome returned a malformed CDP frame', 'CDP_FRAME'));
        return;
      }
      if (Number.isInteger(message.id)) {
        const pending = this.pending.get(message.id);
        if (!pending) continue;
        this.pending.delete(message.id);
        clearTimeout(pending.timer);
        if (message.error) {
          pending.reject(new CDPError(`CDP ${pending.method} failed: ${boundedText(message.error.message, 'protocol error')}`, 'CDP_PROTOCOL'));
        } else {
          pending.resolve(message.result || {});
        }
      } else {
        this.emit('event', message);
      }
    }
  }

  send(method, params = {}, sessionId) {
    if (this.closed) return Promise.reject(new CDPError('CDP connection is closed', 'CDP_CLOSED'));
    const id = this.nextID++;
    const message = { id, method, params };
    if (sessionId) message.sessionId = sessionId;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new CDPError(`CDP ${method} timed out`, 'CDP_TIMEOUT'));
      }, CHROME_READY_TIMEOUT_MS);
      this.pending.set(id, { method, resolve, reject, timer });
      try {
        this.input.write(`${JSON.stringify(message)}\u0000`);
      } catch (error) {
        clearTimeout(timer);
        this.pending.delete(id);
        reject(new CDPError(`CDP ${method} write failed: ${boundedText(error.message)}`, 'CDP_IO'));
      }
    });
  }

  failAll(error) {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    this.failAll(new CDPError('CDP connection closed', 'CDP_CLOSED'));
    this.input.destroy();
    this.output.destroy();
  }
}

class BrowserNotReadyError extends Error {
  constructor(reason, details = {}) {
    super(reason);
    this.name = 'BrowserNotReadyError';
    this.reason = reason;
    this.preReadiness = true;
    Object.assign(this, details);
  }
}

function chromeVersion(binary) {
  if (!binary.endsWith(`${path.sep}Google Chrome`)) return '';
  const infoPath = path.resolve(binary, '..', '..', 'Info.plist');
  try {
    const plist = fs.readFileSync(infoPath, 'utf8');
    const match = plist.match(/<key>CFBundleShortVersionString<\/key>\s*<string>([^<]+)<\/string>/);
    return match ? boundedText(match[1], '', 80) : '';
  } catch (_) {
    return '';
  }
}

function isExecutable(filePath) {
  try {
    const stat = fs.statSync(filePath);
    fs.accessSync(filePath, fs.constants.X_OK);
    return stat.isFile();
  } catch (_) {
    return false;
  }
}

function isOwnedMode0700Directory(directory) {
  try {
    const stat = fs.statSync(directory);
    if (!stat.isDirectory() || (stat.mode & 0o777) !== 0o700) return false;
    return typeof process.getuid !== 'function' || stat.uid === process.getuid();
  } catch (_) {
    return false;
  }
}

function resolveChrome() {
  const configured = process.env.FOGCAST_CHROME_BIN;
  if (configured) {
    if (isExecutable(configured)) {
      return { path: configured, version: chromeVersion(configured), source: 'FOGCAST_CHROME_BIN' };
    }
    throw new BrowserNotReadyError('Chrome pre-readiness missing configured executable (FOGCAST_CHROME_BIN)', {
      category: 'missing-executable',
    });
  }
  const candidates = [
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  ];
  const pathEntries = (process.env.PATH || '').split(path.delimiter).filter(Boolean);
  for (const name of ['google-chrome-stable', 'google-chrome', 'chromium', 'chromium-browser']) {
    for (const directory of pathEntries) candidates.push(path.join(directory, name));
  }
  for (const candidate of candidates) {
    if (isExecutable(candidate)) {
      return { path: candidate, version: chromeVersion(candidate), source: 'discovery' };
    }
  }
  throw new BrowserNotReadyError('Chrome pre-readiness no installed executable was found', {
    category: 'missing-executable',
  });
}

class ChromeProcess {
  constructor() {
    this.child = null;
    this.connection = null;
    this.exitPromise = null;
    this.tempRoot = null;
    this.info = null;
    this.ready = false;
    this.profileReady = false;
    this.startupError = null;
  }

  async start() {
    this.ready = false;
    this.profileReady = false;
    this.startupError = null;
    this.info = resolveChrome();
    this.tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'fogcast-ui-browser-'));
    fs.chmodSync(this.tempRoot, 0o700);
    if (!isOwnedMode0700Directory(this.tempRoot)) {
      throw new BrowserNotReadyError('Chrome pre-readiness isolated profile verification failed', {
        category: 'profile-not-isolated',
      });
    }
    this.profileReady = true;
    const args = [
      '--headless=new',
      '--disable-background-networking',
      '--disable-component-update',
      '--disable-default-apps',
      '--disable-sync',
      '--metrics-recording-only',
      '--no-first-run',
      '--no-default-browser-check',
      '--remote-debugging-pipe',
      `--user-data-dir=${this.tempRoot}`,
      'about:blank',
    ];
    if (process.platform === 'linux' && typeof process.getuid === 'function' && process.getuid() === 0) {
      args.push('--no-sandbox');
    }
    const child = spawn(this.info.path, args, {
      detached: process.platform !== 'win32',
      stdio: ['ignore', 'ignore', 'pipe', 'pipe', 'pipe'],
      windowsHide: true,
      env: { ...process.env },
    });
    this.child = child;
    this.exitPromise = new Promise(resolve => child.once('exit', (code, signal) => resolve({ code, signal })));
    this.connection = new CDPConnection(child);
    try {
      const version = await this.connection.send('Browser.getVersion');
      this.info.protocolProduct = boundedText(version.product, '', 120);
      this.info.protocolRevision = boundedText(version.revision, '', 120);
      this.ready = true;
      return this.info;
    } catch (error) {
      this.startupError = error;
      if (error instanceof CDPError && error.code === 'CDP_PROTOCOL') throw error;
      const category = error && error.code === 'CDP_TIMEOUT' ? 'cdp-readiness-timeout' : 'spawn-error';
      const version = this.info.version || 'unknown';
      throw new BrowserNotReadyError(
        `Chrome pre-readiness ${category} (path=${this.info.path}, version=${version})`,
        { category },
      );
    }
  }

  async terminate() {
    const child = this.child;
    const childPid = child?.pid;
    if (child && child.exitCode === null && child.signalCode === null) {
      const closePromise = this.exitPromise;
      if (this.ready && this.connection && !this.connection.closed) {
        try {
          await withTimeout(this.connection.send('Browser.close'), 2_000, 'Chrome Browser.close timed out', 'CHROME_CLOSE_TIMEOUT');
        } catch (_) {
          // The bounded TERM/KILL path below owns cleanup when the browser does not close itself.
        }
      }
      if (child.exitCode === null && child.signalCode === null) {
        try {
          if (process.platform !== 'win32' && child.pid) process.kill(-child.pid, 'SIGTERM');
          else child.kill('SIGTERM');
        } catch (_) {
          // The process may have exited between the check and the signal.
        }
      }
      await withTimeout(closePromise, 2_000, 'Chrome TERM cleanup timed out', 'CHROME_TERM_TIMEOUT').catch(async () => {
        try {
          if (process.platform !== 'win32' && child.pid) process.kill(-child.pid, 'SIGKILL');
          else child.kill('SIGKILL');
        } catch (_) {
          // The process may have exited between TERM and KILL.
        }
        await withTimeout(closePromise, 2_000, 'Chrome KILL cleanup timed out', 'CHROME_KILL_TIMEOUT');
      });
    }
    if (childPid && process.platform !== 'win32') {
      const quiesced = await waitForProcessGroupQuiescence(childPid, 2_000);
      if (!quiesced) {
        try {
          process.kill(-childPid, 'SIGKILL');
        } catch (_) {
          // The process group may have exited between the probe and the signal.
        }
        if (!await waitForProcessGroupQuiescence(childPid, 2_000)) {
          throw new Error('Chrome process group remained alive after bounded cleanup');
        }
      }
    }
    this.connection?.close();
    this.connection = null;
    this.child = null;
    if (this.tempRoot) {
      const root = this.tempRoot;
      this.tempRoot = null;
      fs.rmSync(root, { recursive: true, force: true });
      if (fs.existsSync(root)) throw new Error('isolated Chrome profile was not removed');
    }
    this.profileReady = false;
    this.ready = false;
    this.exitPromise = null;
  }
}

class BrowserPage {
  constructor(connection, targetId, sessionId, origin) {
    this.connection = connection;
    this.targetId = targetId;
    this.sessionId = sessionId;
    this.origin = origin;
    this.history = [];
    this.eventSequence = 0;
    this.defaultContext = null;
    this.mainFrameId = null;
    this.runtimeFailures = [];
    this.browserLogFailures = [];
    this.networkRequests = new Map();
    this.networkFailures = [];
    this.externalRequests = [];
    this.targetFailures = [];
    this.pendingResponseExtraInfo = new Map();
    this.retiredNetworkRequestIDs = new Set();
    this.eventListenerBaseline = typeof this.connection.listenerCount === 'function'
      ? this.connection.listenerCount('event')
      : 0;
    this.boundEventListener = event => this.handleEvent(event);
    this.connection.on('event', this.boundEventListener);
    this.disposed = false;
  }

  recordNetworkFailure(reason, requestId = '', detail = '', provisional = false) {
    if (this.networkFailures.length >= 64) return;
    const failure = {
      kind: 'network-evidence',
      reason,
      requestId: boundedText(requestId, '', 80),
      detail: boundedText(detail, '', 120),
    };
    if (provisional) failure.provisional = true;
    this.networkFailures.push(failure);
  }

  clearOneNetworkFailure(reason, requestId) {
    const normalizedRequestId = boundedText(requestId, '', 80);
    for (let index = this.networkFailures.length - 1; index >= 0; index -= 1) {
      const failure = this.networkFailures[index];
      if (failure.reason === reason && failure.requestId === normalizedRequestId) {
        this.networkFailures.splice(index, 1);
        return;
      }
    }
  }

  rememberRetiredNetworkRequestID(requestId) {
    if (!validNetworkRequestID(requestId)) return;
    this.retiredNetworkRequestIDs.add(requestId);
    while (this.retiredNetworkRequestIDs.size > MAX_RETIRED_NETWORK_REQUEST_IDS) {
      const oldest = this.retiredNetworkRequestIDs.values().next().value;
      this.retiredNetworkRequestIDs.delete(oldest);
    }
  }

  setNetworkStatus(requestRecord, status, source) {
    if (requestRecord.status !== null && requestRecord.status !== status) {
      this.recordNetworkFailure('conflicting-response-status', requestRecord.requestId);
      return false;
    }
    requestRecord.status = status;
    requestRecord.statusSource = requestRecord.statusSource === null || requestRecord.statusSource === source
      ? source
      : 'response-received+extra-info';
    return true;
  }

  handleEvent(event) {
    const sequence = ++this.eventSequence;
    const entry = { sequence, event };
    this.history.push(entry);
    if (this.history.length > 2_000) this.history.shift();
    if (event.sessionId && event.sessionId !== this.sessionId) return;
    const params = event.params || {};
    if (event.method === 'Runtime.executionContextsCleared') {
      this.defaultContext = null;
      return;
    }
    if (event.method === 'Page.frameNavigated') {
      if (!params.frame.parentId) {
        this.mainFrameId = params.frame.id;
        this.defaultContext = null;
      }
      return;
    }
    if (event.method === 'Runtime.executionContextCreated') {
      const context = params.context || {};
      const aux = context.auxData || {};
      if (aux.isDefault && (!this.mainFrameId || aux.frameId === this.mainFrameId)) {
        this.mainFrameId = aux.frameId || this.mainFrameId;
      }
      return;
    }
    if (event.method === 'Runtime.exceptionThrown') {
      this.runtimeFailures.push({ kind: 'exception', text: boundedText(params.exceptionDetails?.text, 'runtime exception') });
      return;
    }
    if (event.method === 'Runtime.consoleAPICalled') {
      const type = params.type;
      if (type === 'error' || type === 'assert') {
        this.runtimeFailures.push({ kind: `console.${type}`, text: `console.${type}` });
      }
      return;
    }
    if (event.method === 'Log.entryAdded') {
      const log = params.entry || {};
      if (log.level === 'error' || log.type === 'exception') {
        this.browserLogFailures.push({
          level: boundedText(log.level, '', 32),
          type: boundedText(log.type, '', 32),
          text: boundedText(log.text, 'browser log error'),
          url: boundedText(log.url, '', 1_024),
          networkRequestId: boundedText(log.networkRequestId, '', 80),
        });
      }
      return;
    }
    if (event.method === 'Network.requestWillBeSent') {
      if (this.retiredNetworkRequestIDs.has(params.requestId)) return;
      this.retiredNetworkRequestIDs.delete(params.requestId);
      const request = params.request || {};
      const url = boundedText(request.url, '', 1_024);
      const requestRecord = {
        requestId: params.requestId,
        url,
        method: boundedText(request.method, '', 16),
        status: null,
        statusSource: null,
        responseStatus: null,
        extraInfoStatus: null,
      };
      this.networkRequests.set(params.requestId, requestRecord);
      let parsed;
      try {
        parsed = new URL(url);
      } catch (_) {
        parsed = null;
      }
      if (parsed && parsed.origin !== this.origin && parsed.protocol !== 'about:') {
        this.externalRequests.push({ origin: boundedText(parsed.origin, '', 120), path: boundedText(parsed.pathname, '', 240) });
      }
      const pendingExtraInfoStatus = this.pendingResponseExtraInfo.get(params.requestId);
      if (pendingExtraInfoStatus !== undefined) {
        this.pendingResponseExtraInfo.delete(params.requestId);
        if (parsed && parsed.origin === this.origin) {
          this.clearOneNetworkFailure('unmatched-response-extra-info', params.requestId);
          requestRecord.extraInfoStatus = pendingExtraInfoStatus;
          this.setNetworkStatus(requestRecord, pendingExtraInfoStatus, 'response-extra-info');
        } else if (!parsed) {
          this.recordNetworkFailure('malformed-response-extra-info', params.requestId);
        } else {
          this.recordNetworkFailure('foreign-origin-response-extra-info', params.requestId);
        }
      }
      return;
    }
    if (event.method === 'Network.responseReceived') {
      const requestRecord = this.networkRequests.get(params.requestId);
      if (!requestRecord) return;
      const status = params.response?.status;
      if (!validHTTPStatus(status)) {
        this.recordNetworkFailure('malformed-response', params.requestId);
        return;
      }
      if (requestRecord.extraInfoStatus !== null && requestRecord.extraInfoStatus !== status) {
        this.recordNetworkFailure('conflicting-response-status', params.requestId);
        return;
      }
      requestRecord.responseStatus = status;
      this.setNetworkStatus(requestRecord, status, 'response-received');
      return;
    }
    if (event.method === 'Network.responseReceivedExtraInfo') {
      const requestId = params.requestId;
      const statusCode = params.statusCode;
      if (!validNetworkRequestID(requestId) || !validHTTPStatus(statusCode)) {
        this.recordNetworkFailure('malformed-response-extra-info', requestId);
        return;
      }
      const requestRecord = this.networkRequests.get(requestId);
      if (!requestRecord) {
        if (this.retiredNetworkRequestIDs.has(requestId)) return;
        this.recordNetworkFailure('unmatched-response-extra-info', requestId, '', true);
        if (this.pendingResponseExtraInfo.size < MAX_PENDING_RESPONSE_EXTRA_INFO) {
          this.pendingResponseExtraInfo.set(requestId, statusCode);
        }
        return;
      }
      let parsed;
      try {
        parsed = new URL(requestRecord.url);
      } catch (_) {
        parsed = null;
      }
      if (!parsed) {
        this.recordNetworkFailure('malformed-response-extra-info', requestId);
        return;
      }
      if (parsed.origin !== this.origin) {
        this.recordNetworkFailure('foreign-origin-response-extra-info', requestId);
        return;
      }
      if (requestRecord.extraInfoStatus !== null && requestRecord.extraInfoStatus !== statusCode) {
        this.recordNetworkFailure('conflicting-response-status', requestId);
        return;
      }
      if (requestRecord.responseStatus !== null && requestRecord.responseStatus !== statusCode) {
        this.recordNetworkFailure('conflicting-response-status', requestId);
        return;
      }
      requestRecord.extraInfoStatus = statusCode;
      this.setNetworkStatus(requestRecord, statusCode, 'response-extra-info');
      return;
    }
    if (event.method === 'Network.loadingFailed') {
      this.networkFailures.push({
        requestId: boundedText(params.requestId, '', 80),
        error: boundedText(params.errorText, 'network loading failed', 120),
        canceled: params.canceled === true,
      });
      return;
    }
    if (event.method === 'Target.targetCrashed' && params.targetId === this.targetId) {
      this.targetFailures.push({ kind: 'target.crashed' });
      return;
    }
    if (event.method === 'Target.detachedFromTarget' && params.sessionId === this.sessionId) {
      this.targetFailures.push({ kind: 'target.detached' });
    }
  }

  async enable() {
    await this.connection.send('Runtime.enable', {}, this.sessionId);
    await this.connection.send('Page.enable', {}, this.sessionId);
    await this.connection.send('Network.enable', {}, this.sessionId);
    await this.connection.send('Log.enable', {}, this.sessionId);
  }

  async waitForEvent(predicate, timeoutMs, startSequence = 0) {
    const existing = this.history.find(entry => entry.sequence > startSequence && predicate(entry.event));
    if (existing) return existing.event;
    let onEvent;
    const promise = new Promise(resolve => {
      onEvent = event => {
        const entry = { sequence: ++this.eventSequence, event };
        this.history.push(entry);
        if (predicate(event)) {
          this.connection.off('event', onEvent);
          resolve(event);
        }
      };
      this.connection.on('event', onEvent);
    });
    return withTimeout(promise, timeoutMs, 'timed out waiting for browser CDP event', 'PAGE_EVENT_TIMEOUT')
      .finally(() => {
        if (onEvent) this.connection.off('event', onEvent);
      });
  }

  dispose() {
    if (this.disposed) return;
    this.connection.off('event', this.boundEventListener);
    this.disposed = true;
    const remaining = typeof this.connection.listenerCount === 'function'
      ? this.connection.listenerCount('event')
      : this.eventListenerBaseline;
    if (remaining !== this.eventListenerBaseline) {
      throw new Error(`browser page event listeners leaked: baseline=${this.eventListenerBaseline} remaining=${remaining}`);
    }
  }

  async navigate(url) {
    const startSequence = this.eventSequence;
    this.defaultContext = null;
    const result = await this.connection.send('Page.navigate', { url }, this.sessionId);
    const frameId = result.frameId;
    if (!frameId) throw new Error('Page.navigate did not return a frame ID');
    this.mainFrameId = frameId;
    const contextEvent = await this.waitForEvent(event => {
      if (event.method !== 'Runtime.executionContextCreated') return false;
      const context = event.params?.context || {};
      const aux = context.auxData || {};
      return aux.isDefault === true
        && aux.frameId === frameId
        && typeof context.uniqueId === 'string'
        && context.uniqueId.length > 0;
    }, NAVIGATION_TIMEOUT_MS, startSequence);
    this.defaultContext = contextEvent.params.context.uniqueId;
    await this.waitForValue('document.readyState === "complete"', NAVIGATION_TIMEOUT_MS);
  }

  async evaluate(expression) {
    if (!this.defaultContext) throw new Error('browser execution context is not ready');
    const result = await this.connection.send('Runtime.evaluate', {
      expression,
      awaitPromise: true,
      returnByValue: true,
      uniqueContextId: this.defaultContext,
    }, this.sessionId);
    if (result.exceptionDetails) {
      throw new Error(boundedText(result.exceptionDetails.text, 'browser evaluation failed'));
    }
    return result.result ? result.result.value : undefined;
  }

  async waitForValue(expression, timeoutMs = REQUEST_TIMEOUT_MS) {
    const deadline = Date.now() + timeoutMs;
    let lastError;
    while (Date.now() < deadline) {
      try {
        const value = await this.evaluate(expression);
        if (value) return value;
      } catch (error) {
        lastError = error;
      }
      await wait(SUITE_POLL_MS);
    }
    if (lastError) throw new Error(`timed out waiting for browser value: ${boundedText(lastError.message)}`);
    throw new Error('timed out waiting for browser value');
  }

  async click(selector) {
    const source = JSON.stringify(selector);
    await this.evaluate(`(() => { const node = document.querySelector(${source}); if (!node) throw new Error('missing browser control'); node.click(); return true; })()`);
  }

  async setSearch(value) {
    const source = JSON.stringify(String(value));
    await this.evaluate(`(() => { const node = document.querySelector('#game-search'); if (!node) throw new Error('missing search control'); node.value = ${source}; node.dispatchEvent(new Event('input', { bubbles: true })); return true; })()`);
  }

  async setViewport(width, height) {
    await this.connection.send('Emulation.setDeviceMetricsOverride', {
      width,
      height,
      deviceScaleFactor: 1,
      mobile: false,
    }, this.sessionId);
  }

  async clearViewport() {
    await this.connection.send('Emulation.clearDeviceMetricsOverride', {}, this.sessionId);
  }

  async setReducedMotion(reduced = true) {
    await this.connection.send('Emulation.setEmulatedMedia', {
      features: reduced
        ? [{ name: 'prefers-reduced-motion', value: 'reduce' }]
        : [],
    }, this.sessionId);
  }

  async snapshot() {
    return this.evaluate(`(() => {
      const text = selector => document.querySelector(selector)?.textContent || '';
      const button = selector => document.querySelector(selector);
      return {
        catalogStatus: text('#catalog-status'),
        catalogText: text('#catalog'),
        catalogActions: text('#catalog-actions'),
        detailHeading: text('#detail-heading'),
        detailText: text('#detail-content'),
        detailActions: text('#launch-actions'),
        launchText: text('#launch-status') + ' ' + text('#launch-actions'),
        sessionStatus: text('#session-status'),
        sessionText: text('#session-details'),
        sessionMessage: text('#session-message'),
        sessionBusy: document.querySelector('#session-panel')?.getAttribute('aria-busy') || '',
        sessionRefreshDisabled: button('#refresh-session')?.disabled === true,
        sessionStopDisabled: button('#stop-session')?.disabled === true,
        sessionStopHidden: button('#stop-session')?.hidden === true,
        sessionStopDescribedBy: button('#stop-session')?.getAttribute('aria-describedby') || '',
        launchButtonDisabled: button('#launch-game')?.disabled === true || button('#launch-actions button.button')?.disabled === true,
        launchButtonLabel: button('#launch-game')?.textContent || button('#launch-actions button.button')?.textContent || '',
        launchButtonDescribedBy: button('#launch-game')?.getAttribute('aria-describedby') || button('#launch-actions button.button')?.getAttribute('aria-describedby') || '',
        favoriteLabel: button('#favorite-game')?.textContent || '',
        collectionItems: Array.from(document.querySelectorAll('#collection-list .nav-item')).map(item => ({
          id: item.getAttribute('data-collection') || '',
          label: item.textContent || '',
          selected: String(item.className || '').includes('selected'),
        })),
        collectionMemberLabel: Array.from(document.querySelectorAll('.collection-member-button')).map(item => item.textContent || ''),
        attractHidden: document.querySelector('#attract')?.hidden !== false,
        attractTitle: text('#attract-title'),
        settingsHidden: document.querySelector('#settings')?.hidden !== false,
        settingsAttract: document.querySelector('#settings-attract-idle')?.value || '',
        settingsRegions: document.querySelector('#settings-preferred-regions')?.value || '',
        keyboardPane: document.querySelector('#launcher')?.getAttribute('data-keyboard-pane') || '',
        activeElementID: document.activeElement?.id || '',
        cards: Array.from(document.querySelectorAll('#catalog-list .game-card')).map(card => ({
          title: card.querySelector('h3')?.textContent || '',
          system: card.getAttribute('data-system') || card.querySelector('.game-meta')?.textContent?.split(' · ')[0] || '',
          state: card.getAttribute('data-state') || card.querySelector('.game-meta')?.textContent?.split(' · ')[1] || '',
          fallback: Boolean(card.querySelector('.fallback-note')),
          pressed: card.getAttribute('aria-pressed') === 'true',
        })),
        homeRails: Array.from(document.querySelectorAll('#catalog-list .home-rail')).map(rail => ({
          id: rail.getAttribute('data-home-rail') || '',
          title: rail.querySelector('.home-rail-title')?.textContent || '',
          seeAll: Boolean(rail.querySelector('.home-rail-see-all')),
          cards: Array.from(rail.querySelectorAll('.game-card')).map(card => card.getAttribute('data-game-id') || ''),
        })),
        navHomeSelected: String(document.getElementById('nav-home')?.className || '').includes('selected'),
        catalogListClass: document.getElementById('catalog-list')?.className || '',
      };
    })()`);
  }

  async waitForSnapshot(predicate, timeoutMs = REQUEST_TIMEOUT_MS) {
    const deadline = Date.now() + timeoutMs;
    let last;
    while (Date.now() < deadline) {
      last = await this.snapshot();
      if (predicate(last)) return last;
      await wait(SUITE_POLL_MS);
    }
    throw new Error(`timed out waiting for browser state: ${boundedText(JSON.stringify(last), 'state unavailable', 512)}`);
  }

  async waitForCatalog(status) {
    return this.waitForSnapshot(snapshot => snapshot.catalogStatus === status);
  }

  async waitForDetailHeading(heading) {
    return this.waitForSnapshot(snapshot => snapshot.detailHeading === heading);
  }

  async waitForText(selector, text) {
    return this.waitForSnapshot(snapshot => {
      const value = selector === '#launch-status'
        ? snapshot.launchText
        : selector === '#launch-actions'
          ? snapshot.detailActions
            : selector === '#catalog-actions'
              ? snapshot.catalogActions
              : selector === '#session-status'
                ? snapshot.sessionStatus
                : selector === '#session-details'
                  ? snapshot.sessionText
                  : selector === '#session-message'
                    ? snapshot.sessionMessage
            : snapshot.detailText;
      return value.includes(text);
    });
  }

  resetEvidence() {
    this.runtimeFailures = [];
    this.browserLogFailures = [];
    for (const requestId of this.networkRequests.keys()) this.rememberRetiredNetworkRequestID(requestId);
    for (const requestId of this.pendingResponseExtraInfo.keys()) this.rememberRetiredNetworkRequestID(requestId);
    this.networkRequests.clear();
    this.networkFailures = [];
    this.externalRequests = [];
    this.targetFailures = [];
    this.pendingResponseExtraInfo.clear();
  }

  evidence() {
    const runtimeFailures = this.runtimeFailures.slice();
    const expectedHTTPFailures = [];
    for (const failure of this.browserLogFailures) {
      const match = CHROME_FAILED_RESOURCE_PATTERN.exec(failure.text);
      let parsed;
      try {
        parsed = failure.url ? new URL(failure.url) : null;
      } catch (_) {
        parsed = null;
      }
      const status = match ? Number(match[1]) : 0;
      const correlated = failure.type !== 'exception' && parsed && parsed.origin === this.origin
        ? [...this.networkRequests.values()].find(request => (
          request.url === failure.url
          && request.status === status
          && (!failure.networkRequestId || request.requestId === failure.networkRequestId)
        ))
        : null;
      if (correlated) {
        expectedHTTPFailures.push({
          kind: 'expected-http-failure',
          text: failure.text,
          url: failure.url,
          status: correlated.status,
          requestId: boundedText(correlated.requestId, '', 80),
        });
      } else {
        runtimeFailures.push({
          kind: 'log.error',
          text: failure.text,
          url: failure.url,
        });
      }
    }
    return {
      runtimeFailures,
      expectedHTTPFailures,
      networkFailures: this.networkFailures.slice(),
      externalRequests: this.externalRequests.slice(),
      targetFailures: this.targetFailures.slice(),
      networkRequests: [...this.networkRequests.values()].map(request => ({
        method: request.method,
        url: request.url,
        status: request.status,
        statusSource: request.statusSource,
      })),
    };
  }

  assertClean() {
    const evidence = this.evidence();
    assert.deepEqual(evidence.runtimeFailures, [], `browser runtime failures: ${JSON.stringify(evidence.runtimeFailures)}`);
    assert.deepEqual(evidence.networkFailures, [], `browser network failures: ${JSON.stringify(evidence.networkFailures)}`);
    assert.deepEqual(evidence.externalRequests, [], `browser external requests: ${JSON.stringify(evidence.externalRequests)}`);
    assert.deepEqual(evidence.targetFailures, [], `browser target failures: ${JSON.stringify(evidence.targetFailures)}`);
  }
}

class BrowserHarness {
  constructor() {
    this.fixtureServer = new FixtureServer();
    this.chrome = new ChromeProcess();
    this.page = null;
    this.targetId = null;
    this.sessionId = null;
    this.scenarios = [];
    this.startupError = null;
  }

  async start() {
    await this.fixtureServer.start();
    try {
      await this.chrome.start();
      const target = await this.chrome.connection.send('Target.createTarget', { url: 'about:blank' });
      this.targetId = target.targetId;
      const attached = await this.chrome.connection.send('Target.attachToTarget', {
        targetId: this.targetId,
        flatten: true,
      });
      this.sessionId = attached.sessionId;
      this.page = new BrowserPage(this.chrome.connection, this.targetId, this.sessionId, this.fixtureServer.origin);
      await this.page.enable();
    } catch (error) {
      this.startupError = error;
      throw error;
    }
  }

  configure(plan) {
    if (!this.page) throw new Error('browser harness is not ready');
    this.fixtureServer.configure(plan);
    this.page.resetEvidence();
  }

  async reload() {
    if (!this.page || !this.fixtureServer.origin) throw new Error('browser harness is not ready');
    this.page.resetEvidence();
    await this.page.navigate(`${this.fixtureServer.origin}/`);
  }

  async snapshot() {
    return this.page.snapshot();
  }

  async waitForSnapshot(predicate, timeoutMs) {
    return this.page.waitForSnapshot(predicate, timeoutMs);
  }

  async waitForCatalog(status) {
    return this.page.waitForCatalog(status);
  }

  async waitForDetailHeading(heading) {
    return this.page.waitForDetailHeading(heading);
  }

  async waitForText(selector, text) {
    return this.page.waitForText(selector, text);
  }

  async click(selector) {
    return this.page.click(selector);
  }

  async setSearch(value) {
    return this.page.setSearch(value);
  }

  async setViewport(width, height) {
    return this.page.setViewport(width, height);
  }

  async clearViewport() {
    return this.page.clearViewport();
  }

  async setReducedMotion(reduced = true) {
    return this.page.setReducedMotion(reduced);
  }

  async evaluate(expression) {
    return this.page.evaluate(expression);
  }

  async waitForSettled() {
    const deadline = Date.now() + REQUEST_TIMEOUT_MS;
    let pendingFixture;
    let pendingBrowser;
    let quietSequence = null;
    let quietFixtureCount = null;
    while (true) {
      const networkFailures = this.page.evidence().networkFailures;
      const blockingNetworkFailures = networkFailures.filter(failure => failure.provisional !== true);
      if (blockingNetworkFailures.length > 0) {
        throw new Error(`browser network evidence failed: ${JSON.stringify(blockingNetworkFailures)}`);
      }
      pendingFixture = this.fixtureEvidence().filter(record => record.status === null);
      pendingBrowser = this.page.evidence().networkRequests.filter(request => {
        if (request.status !== null) return false;
        try {
          return new URL(request.url).origin === this.fixtureServer.origin;
        } catch (_) {
          return false;
        }
      });
      if (this.page.pendingResponseExtraInfo.size > 0) {
        pendingBrowser = pendingBrowser.concat([...this.page.pendingResponseExtraInfo.keys()].map(requestId => ({
          requestId,
          status: null,
          reason: 'awaiting-request-record',
        })));
      }
      if (pendingFixture.length === 0 && pendingBrowser.length === 0) {
        if (Date.now() >= deadline) {
          throw new Error(`timed out waiting for browser evidence to settle: ${JSON.stringify({ pendingFixture, pendingBrowser })}`);
        }
        const sequence = this.page.eventSequence;
        const fixtureCount = this.fixtureEvidence().length;
        if (quietSequence === sequence && quietFixtureCount === fixtureCount) return;
        quietSequence = sequence;
        quietFixtureCount = fixtureCount;
        await wait(EVIDENCE_SETTLE_QUIET_MS);
        continue;
      }
      quietSequence = null;
      quietFixtureCount = null;
      if (Date.now() >= deadline) {
        throw new Error(`timed out waiting for browser evidence to settle: ${JSON.stringify({ pendingFixture, pendingBrowser })}`);
      }
      await wait(SUITE_POLL_MS);
    }
  }

  async waitForRequest(matcher) {
    return this.fixtureServer.waitForRequest(matcher);
  }

  async release(id) {
    return this.fixtureServer.release(id);
  }

  fixtureEvidence() {
    return this.fixtureServer.evidence();
  }

  assertClean() {
    this.page.assertClean();
    const unexpected = this.fixtureEvidence().filter(record => record.unexpected);
    assert.deepEqual(unexpected, [], `unexpected fixture requests: ${JSON.stringify(unexpected)}`);
    const serverRecords = this.fixtureEvidence().filter(record => record.status !== null);
    const used = new Set();
    const mismatches = [];
    const gamesQuery = (pathname, searchParams) => {
      if (pathname !== '/api/v1/games') return '';
      const q = searchParams.get('q') || '';
      const platform = searchParams.get('platform') || '';
      const collection = searchParams.get('collection') || '';
      if (platform) return q ? `${q}&platform=${platform}` : `platform=${platform}`;
      if (collection) return q ? `${q}&collection=${collection}` : `collection=${collection}`;
      return q;
    };
    const keyForServer = record => `${record.method} ${record.path} ${record.path === '/api/v1/games' ? record.query : ''}`;
    for (const request of this.page.evidence().networkRequests) {
      let parsed;
      try {
        parsed = new URL(request.url);
      } catch (_) {
        continue;
      }
      if (parsed.origin !== this.fixtureServer.origin) continue;
      const query = gamesQuery(parsed.pathname, parsed.searchParams);
      const key = `${request.method} ${parsed.pathname} ${query}`;
      const index = serverRecords.findIndex((record, candidateIndex) => (
        !used.has(candidateIndex)
        && keyForServer(record) === key
      ));
      if (index < 0) {
        mismatches.push({ key, status: request.status, reason: 'missing-fixture-record' });
        continue;
      }
      used.add(index);
      if (serverRecords[index].status !== request.status) {
        mismatches.push({
          key,
          browserStatus: request.status,
          fixtureStatus: serverRecords[index].status,
          reason: 'status-mismatch',
        });
      }
    }
    assert.deepEqual(mismatches, [], `browser/fixture request evidence mismatch: ${JSON.stringify(mismatches)}`);
  }

  recordScenario(name) {
    this.scenarios.push({
      name,
      requests: this.fixtureEvidence(),
      browser: this.page.evidence(),
    });
  }

  report() {
    return {
      chrome: {
        path: this.chrome.info?.path || '',
        version: this.chrome.info?.protocolProduct || this.chrome.info?.version || 'unknown',
        cdpReady: this.chrome.ready === true,
        isolatedProfile: this.chrome.ready === true && this.chrome.profileReady === true,
      },
      fixture: {
        origin: this.fixtureServer.origin || '',
        loopback: true,
        portCollisionSafe: true,
      },
      startupError: this.startupError ? boundedText(this.startupError.reason || this.startupError.message) : '',
      scenarios: this.scenarios,
    };
  }

  async close() {
    let firstError = null;
    if (this.page) {
      try {
        this.page.dispose();
      } catch (error) {
        firstError ||= error;
      }
    }
    if (this.page && this.chrome.connection && !this.chrome.connection.closed) {
      try {
        await this.chrome.connection.send('Target.closeTarget', { targetId: this.targetId });
      } catch (error) {
        if (this.chrome.ready) firstError = error;
      }
    }
    try {
      await this.chrome.terminate();
    } catch (error) {
      firstError ||= error;
    }
    try {
      await this.fixtureServer.close();
    } catch (error) {
      firstError ||= error;
    }
    this.page = null;
    this.targetId = null;
    this.sessionId = null;
    if (firstError) throw firstError;
  }
}

module.exports = {
  BrowserHarness,
  BrowserPage,
  BrowserNotReadyError,
  FixtureServer,
  fixture,
  artworkFixture,
  resolveChrome,
  waitForProcessGroupQuiescence,
  normalizePlan,
};
