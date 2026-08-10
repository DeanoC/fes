'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const {
  metadataFor,
  normalizeMetadata,
  toLauncherGame,
  PALETTE_ALLOWLIST,
} = require('./ui_metadata.js');

const sonic = Object.freeze({
  id: 'megadrive-sonic-test',
  title: 'Sonic the Hedgehog',
  system: 'megadrive',
  state: 'available',
  root_online: true,
  content_prepared: true,
  execution: 'fpga_native',
});

test('metadataFor returns the same complete value for the same game', () => {
  assert.deepEqual(metadataFor(sonic), metadataFor(sonic));
  for (const field of ['cover', 'backdrop', 'summary', 'year', 'genre', 'studio', 'players']) {
    assert.ok(metadataFor(sonic)[field], `missing ${field}`);
  }
});

test('curated metadata requires both the intended system and normalized title', () => {
  const curated = metadataFor(sonic);
  const wrongSystem = metadataFor({ ...sonic, id: 'snes-sonic', system: 'snes' });
  assert.equal(curated.isFallback, false);
  assert.equal(wrongSystem.isFallback, true);
});

test('unmatched catalog games always receive complete fallback metadata', () => {
  const result = metadataFor({ id: 'unknown-1', title: 'Unknown', system: 'snes' });
  assert.equal(result.isFallback, true);
  assert.match(result.summary, /demo/i);
  assert.ok(result.cover.palette);
  assert.ok(result.backdrop.palette);
});

test('fallback artwork uses only the exported frozen palette allowlist', () => {
  assert.equal(Object.isFrozen(PALETTE_ALLOWLIST), true);
  const games = [
    { id: 'unknown-1', title: 'Unknown', system: 'snes' },
    { id: 'unknown-2', title: 'Another', system: 'arcade' },
  ];
  for (const game of games) {
    const result = metadataFor(game);
    assert.ok(PALETTE_ALLOWLIST.includes(result.cover.palette));
    assert.ok(PALETTE_ALLOWLIST.includes(result.backdrop.palette));
  }
});

test('normalization recovers from missing and throwing provider data', () => {
  const missing = normalizeMetadata(undefined, sonic);
  const throwing = toLauncherGame(sonic, { metadataFor() { throw new Error('offline'); } });
  assert.equal(missing.isFallback, true);
  assert.equal(throwing.presentation.isFallback, true);
});

test('normalization marks valid artwork with missing text fields as fallback', () => {
  const result = normalizeMetadata({
    cover: { palette: 'ember', treatment: 'grid' },
    backdrop: { palette: 'lagoon', treatment: 'waves' },
  }, sonic);
  assert.equal(result.isFallback, true);
  for (const field of ['summary', 'year', 'genre', 'studio', 'players']) {
    assert.ok(result[field], `missing ${field}`);
  }
});

test('fallback rejects inherited system labels', () => {
  for (const system of ['constructor', 'toString', '__proto__']) {
    const result = metadataFor({ id: `adversarial-${system}`, title: 'Unknown', system });
    assert.match(result.summary, /Unknown system/);
  }
});

test('normalization replaces invalid artwork tokens and bounds display values', () => {
  const result = normalizeMetadata({
    cover: { palette: 'url(javascript:alert(1))' },
    backdrop: { palette: '<style>body{display:none}</style>' },
    summary: 'x'.repeat(500),
  }, sonic);
  assert.ok(PALETTE_ALLOWLIST.includes(result.cover.palette));
  assert.ok(PALETTE_ALLOWLIST.includes(result.backdrop.palette));
  assert.ok(result.summary.length <= 240);
  assert.equal(result.isFallback, true);
});

test('catalog identity and operational fields win during merge', () => {
  const poisoned = {
    metadataFor() {
      return {
        id: 'replacement', title: 'Replacement', system: 'other',
        state: 'offline', execution: 'host_cast', summary: 'Presentation only',
      };
    },
  };
  const merged = toLauncherGame(sonic, poisoned);
  for (const field of ['id', 'title', 'system', 'state', 'root_online', 'content_prepared', 'execution']) {
    assert.equal(merged[field], sonic[field]);
  }
  assert.equal(merged.presentation.summary, 'Presentation only');
});

test('fallback keeps hostile catalog strings as text and never artwork tokens', () => {
  const hostile = '<img src=x onerror="alert(1)"> \' ; background: url(https://evil.test/x.png);' + 'x'.repeat(200);
  const boundedHostileTitle = hostile.slice(0, 120);
  const result = metadataFor({ id: hostile, title: hostile, system: hostile });
  assert.match(result.summary, new RegExp(boundedHostileTitle.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  assert.ok(PALETTE_ALLOWLIST.includes(result.cover.palette));
  assert.ok(PALETTE_ALLOWLIST.includes(result.backdrop.palette));
  assert.equal(result.cover.palette.includes('url('), false);
  assert.equal(result.backdrop.palette.includes('url('), false);
});
