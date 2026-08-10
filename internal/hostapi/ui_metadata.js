'use strict';

(function installFogCastMetadata(root) {
  const PALETTE_ALLOWLIST = Object.freeze(['ember', 'lagoon', 'violet', 'sunset', 'forest']);
  const TREATMENT_ALLOWLIST = Object.freeze(['grid', 'rings', 'stripes', 'starlight', 'waves']);
  const SYSTEM_LABELS = Object.freeze({
    arcade: 'Arcade',
    gameboy: 'Game Boy',
    megadrive: 'Mega Drive',
    nes: 'NES',
    snes: 'SNES',
  });
  const FALLBACK_GENRES = Object.freeze(['Action', 'Adventure', 'Arcade', 'Platformer', 'Puzzle']);
  const FALLBACK_TREATMENTS = Object.freeze(['grid', 'rings', 'stripes', 'starlight', 'waves']);
  const CURATED = Object.freeze({
    'megadrive\0sonic the hedgehog': Object.freeze({
      cover: Object.freeze({ palette: 'lagoon', treatment: 'rings' }),
      backdrop: Object.freeze({ palette: 'sunset', treatment: 'waves' }),
      summary: 'A high-speed demo adventure through bright zones and loop-filled stages.',
      year: '1991', genre: 'Platformer', studio: 'SEGA', players: '1 player', isFallback: false,
    }),
    'snes\0super mario world': Object.freeze({
      cover: Object.freeze({ palette: 'forest', treatment: 'grid' }),
      backdrop: Object.freeze({ palette: 'sunset', treatment: 'starlight' }),
      summary: 'A classic platforming demo packed with secrets, power-ups, and bright worlds.',
      year: '1990', genre: 'Platformer', studio: 'Nintendo', players: '1–2 players', isFallback: false,
    }),
    'snes\0the legend of zelda: a link to the past': Object.freeze({
      cover: Object.freeze({ palette: 'violet', treatment: 'rings' }),
      backdrop: Object.freeze({ palette: 'forest', treatment: 'waves' }),
      summary: 'A legendary demo quest across a mysterious world of dungeons and discovery.',
      year: '1991', genre: 'Adventure', studio: 'Nintendo', players: '1 player', isFallback: false,
    }),
    'megadrive\0streets of rage 2': Object.freeze({
      cover: Object.freeze({ palette: 'ember', treatment: 'stripes' }),
      backdrop: Object.freeze({ palette: 'violet', treatment: 'grid' }),
      summary: 'A kinetic demo brawler where every street hides a new showdown.',
      year: '1992', genre: 'Action', studio: 'SEGA', players: '1–2 players', isFallback: false,
    }),
  });

  function boundedString(value, fallback, limit) {
    if (typeof value !== 'string' && typeof value !== 'number') return fallback;
    const text = String(value).trim();
    return text ? text.slice(0, limit) : fallback;
  }

  function normalizedKeyPart(value) {
    return boundedString(value, '', 200).toLowerCase().replace(/\s+/g, ' ');
  }

  function stableHash(game) {
    const input = `${String(game && game.id)}\0${String(game && game.system)}`;
    let hash = 0x811c9dc5;
    for (let index = 0; index < input.length; index += 1) {
      hash ^= input.charCodeAt(index);
      hash = Math.imul(hash, 0x01000193);
    }
    return hash >>> 0;
  }

  function fallbackMetadata(game) {
    const hash = stableHash(game);
    const palette = PALETTE_ALLOWLIST[hash % PALETTE_ALLOWLIST.length];
    const treatment = FALLBACK_TREATMENTS[(hash >>> 3) % FALLBACK_TREATMENTS.length];
    const title = boundedString(game && game.title, 'Untitled game', 120);
    const system = normalizedKeyPart(game && game.system);
    const label = Object.prototype.hasOwnProperty.call(SYSTEM_LABELS, system)
      ? SYSTEM_LABELS[system]
      : 'Unknown system';
    return {
      cover: { palette, treatment },
      backdrop: { palette: PALETTE_ALLOWLIST[(hash >>> 7) % PALETTE_ALLOWLIST.length], treatment },
      summary: `${title} is a deterministic demo presentation for ${label}.`,
      year: '—',
      genre: FALLBACK_GENRES[(hash >>> 11) % FALLBACK_GENRES.length],
      studio: 'FogCast demo',
      players: 'Unknown players',
      isFallback: true,
    };
  }

  function normalizeArtwork(value, fallback) {
    const source = value && typeof value === 'object' ? value : {};
    const palette = PALETTE_ALLOWLIST.includes(source.palette) ? source.palette : fallback.palette;
    const treatment = TREATMENT_ALLOWLIST.includes(source.treatment) ? source.treatment : fallback.treatment;
    return Object.freeze({ palette, treatment });
  }

  function normalizeMetadata(value, game) {
    const fallback = fallbackMetadata(game || {});
    const source = value && typeof value === 'object' ? value : {};
    const artworkRecovered = !value
      || !PALETTE_ALLOWLIST.includes(source.cover && source.cover.palette)
      || !PALETTE_ALLOWLIST.includes(source.backdrop && source.backdrop.palette)
      || !TREATMENT_ALLOWLIST.includes(source.cover && source.cover.treatment)
      || !TREATMENT_ALLOWLIST.includes(source.backdrop && source.backdrop.treatment);
    const textFields = [
      ['summary', 240],
      ['year', 20],
      ['genre', 40],
      ['studio', 60],
      ['players', 40],
    ];
    const textRecovered = textFields.some(([field, limit]) => {
      const valueForField = source[field];
      if (typeof valueForField !== 'string' && typeof valueForField !== 'number') return true;
      const text = String(valueForField).trim();
      return !text || text.length > limit;
    });
    const normalized = {
      cover: normalizeArtwork(source.cover, fallback.cover),
      backdrop: normalizeArtwork(source.backdrop, fallback.backdrop),
      summary: boundedString(source.summary, fallback.summary, 240),
      year: boundedString(source.year, fallback.year, 20),
      genre: boundedString(source.genre, fallback.genre, 40),
      studio: boundedString(source.studio, fallback.studio, 60),
      players: boundedString(source.players, fallback.players, 40),
      isFallback: Boolean(source.isFallback) || artworkRecovered || textRecovered,
    };
    return Object.freeze({ ...normalized });
  }

  function metadataFor(game) {
    const key = `${normalizedKeyPart(game && game.system)}\0${normalizedKeyPart(game && game.title)}`;
    return normalizeMetadata(CURATED[key] || fallbackMetadata(game || {}), game || {});
  }

  function toLauncherGame(game, adapter) {
    const provider = adapter || api;
    let presentation;
    try {
      presentation = normalizeMetadata(provider.metadataFor(game), game);
    } catch (_) {
      presentation = normalizeMetadata(undefined, game);
    }
    return { ...game, presentation };
  }

  const api = Object.freeze({
    PALETTE_ALLOWLIST,
    metadataFor,
    normalizeMetadata,
    toLauncherGame,
  });
  root.FogCastMetadata = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(globalThis);
