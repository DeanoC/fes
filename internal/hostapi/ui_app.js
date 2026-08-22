'use strict';

(function installFogCastApp(root) {
  const PALETTE_ALLOWLIST = Object.freeze(['ember', 'lagoon', 'violet', 'sunset', 'forest']);
  const TREATMENT_ALLOWLIST = Object.freeze(['grid', 'rings', 'stripes', 'starlight', 'waves']);
  const PRESENTATION_TEXT_LIMITS = Object.freeze({
    summary: 240,
    year: 20,
    genre: 40,
    studio: 60,
    players: 40,
  });
  const PRESENTATION_FALLBACK = Object.freeze({
    summary: '',
    year: '—',
    genre: '',
    studio: '',
    players: '',
    isFallback: true,
    metadataState: 'fallback_offline',
  });
  const PRESENTATION_STATE_ALLOWLIST = Object.freeze([
    'ready', 'fallback_disabled', 'fallback_unconfigured', 'fallback_no_match',
    'fallback_ambiguous', 'fallback_offline', 'fallback_malformed', 'ready_artwork_error',
  ]);

  function gamesPath(query, extras) {
    const value = String(query || '').trim();
    const extra = extras && typeof extras === 'object' ? extras : {};
    const params = [];
    const push = (key, raw) => {
      if (raw === undefined || raw === null || raw === '' || raw === false) return;
      params.push(`${key}=${encodeURIComponent(String(raw))}`);
    };
    push('q', value);
    push('platform', extra.platform);
    push('collection', extra.collection);
    push('region', extra.region);
    push('genre', extra.genre);
    push('year', extra.year);
    if (extra.sort && extra.sort !== 'title' && extra.sort !== 'recents') {
      push('sort', extra.sort === 'system' ? 'platform' : extra.sort);
    }
    if (extra.hide_prerelease) params.push('hide_prerelease=1');
    if (extra.hide_hacks) params.push('hide_hacks=1');
    push('availability', extra.availability && extra.availability !== 'all' ? extra.availability : '');
    params.push(extra.grouped === 0 || extra.grouped === '0' || extra.grouped === false ? 'grouped=0' : 'grouped=1');
    push('cursor', extra.cursor);
    if (Number(extra.limit) > 0) push('limit', extra.limit);
    return `/api/v1/games?${params.join('&')}`;
  }

  function gameDetailPath(id) {
    return `/api/v1/games/${encodeURIComponent(String(id))}`;
  }

  const GAME_ID_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
  const ARTWORK_HANDLE_PATTERN = /^[a-f0-9]{64}$/;
  const WALL_WINDOW = 80;
  const HOME_RAIL_LIMIT = 12;
  const HOME_CUSTOM_RAIL_CAP = 6;
  const HOME_SMART_RAILS = Object.freeze([
    Object.freeze({ id: 'continue', name: 'Continue', sort: 'title' }),
    Object.freeze({ id: 'favorites', name: 'Favorites', sort: 'title' }),
    Object.freeze({ id: 'recents', name: 'Recent', sort: 'recents' }),
    Object.freeze({ id: 'unplayed', name: 'Unplayed', sort: 'title' }),
    Object.freeze({ id: 'recently_added', name: 'Recently added', sort: 'recently_added' }),
  ]);

  function collectionFetchSort(collection) {
    const smart = HOME_SMART_RAILS.find(rail => rail && rail.id === collection);
    if (smart && smart.sort) return smart.sort;
    return 'title';
  }

  function catalogSortOverridden(state) {
    return Boolean(state) && (state.libraryView === 'home' || Boolean(state.collection));
  }

  function catalogEffectiveSort(state) {
    if (catalogSortOverridden(state)) return collectionFetchSort(state && state.collection);
    return (state && state.sort) || 'title';
  }

  function catalogSortOverrideLabel(sort) {
    if (sort === 'recently_added') return 'Sort (Recently added)';
    if (sort === 'recents') return 'Sort (Recent)';
    return 'Sort (Title)';
  }

  const HOME_LAUNCH_RECONCILE_RAILS = Object.freeze(['unplayed', 'continue', 'recents']);
  const MAX_ATTRACT_IDLE_SECONDS = 2147483;
  const MAX_ATTRACT_IDLE_MS = 2147483647;
  const RESERVED_COLLECTION_IDS = Object.freeze({
    all: true,
    home: true,
    favorites: true,
    recents: true,
    continue: true,
    unplayed: true,
    recently_added: true,
    'recently-added': true,
  });

  function collectionIDFromName(name) {
    let id = String(name || '').trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 64);
    if (!id) id = 'collection';
    if (RESERVED_COLLECTION_IDS[id]) id = `${id}-list`.slice(0, 64);
    return id;
  }

  function uniqueCollectionID(name, existing) {
    const used = new Set((Array.isArray(existing) ? existing : []).filter(id => typeof id === 'string' && id));
    const base = collectionIDFromName(name);
    if (!used.has(base)) return base;
    for (let n = 2; n < 1000; n += 1) {
      const suffix = `-${n}`;
      const id = `${base.slice(0, Math.max(1, 64 - suffix.length))}${suffix}`;
      if (!used.has(id) && !RESERVED_COLLECTION_IDS[id]) return id;
    }
    return `${base.slice(0, 55)}-${Date.now().toString(36)}`.slice(0, 64);
  }

  function parseCollection(item) {
    if (!item || typeof item.id !== 'string' || !item.id.trim()) return null;
    const id = item.id.trim();
    return Object.freeze({
      id,
      name: typeof item.name === 'string' && item.name.trim() ? item.name.trim() : id,
      created_at: Number.isFinite(item.created_at) ? item.created_at : 0,
    });
  }

  function parseCollectionList(payload) {
    const list = payload && Array.isArray(payload.collections) ? payload.collections : [];
    return Object.freeze(list.map(parseCollection).filter(Boolean));
  }

  function presentationPath(id) {
    const value = String(id || '').trim();
    if (!GAME_ID_PATTERN.test(value)) throw createError('BAD_REQUEST', 'The live game ID is invalid.');
    return `/api/v1/presentation/games/${encodeURIComponent(value)}`;
  }

  function artworkPath(handle) {
    const value = String(handle || '').trim();
    if (!ARTWORK_HANDLE_PATTERN.test(value)) throw createError('BAD_REQUEST', 'The artwork handle is invalid.');
    return `/api/v1/presentation/artwork/${value}`;
  }

  function mediaPath(handle) {
    const value = String(handle || '').trim();
    if (!ARTWORK_HANDLE_PATTERN.test(value)) throw createError('BAD_REQUEST', 'The artwork handle is invalid.');
    return `/api/v1/presentation/media/${value}`;
  }

  function launchRequest(game) {
    return {
      path: '/api/v1/session/launch',
      options: {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ game_id: game.id }),
      },
    };
  }

  function launchStatus(launchState, launchMessage) {
    const messages = {
      idle: '',
      launching: 'launching: preparing the selected live game…',
      launch_success: 'launch_success: session accepted by the local host.',
      launch_error: launchMessage || 'launch_error: the launch could not be completed.',
    };
    return {
      text: messages[launchState] || '',
      role: launchState === 'launch_error' ? 'alert' : 'status',
    };
  }

  function isSelectedGame(selectedLiveGame, game) {
    if (!selectedLiveGame || !game) return false;
    if (selectedLiveGame.id === game.id) return true;
    return Boolean(selectedLiveGame.group_key && game.group_key && selectedLiveGame.group_key === game.group_key);
  }

  function presentationIdentity(game) {
    if (!game || typeof game !== 'object') return null;
    return Object.freeze({
      id: game.id,
      title: game.title,
      system: game.system,
    });
  }

  function samePresentationIdentity(left, right) {
    return Boolean(left && right)
      && left.id === right.id
      && left.title === right.title
      && left.system === right.system;
  }

  function detailHeading(game) {
    return game ? cardTitle(game) : 'Select a game';
  }

  function boundedMessage(value, fallback, limit) {
    if (typeof value !== 'string' && typeof value !== 'number') return fallback;
    const text = String(value).trim();
    return text ? text.slice(0, limit) : fallback;
  }

  function createError(code, message, status) {
    const error = new Error(boundedMessage(message, 'The local request could not be completed.', 240));
    error.code = boundedMessage(code, 'REQUEST_FAILED', 48);
    error.status = status;
    return error;
  }

  function safeError(response, payload) {
    const source = payload && payload.error;
    return createError(
      source && source.code,
      source && source.message,
      response && response.status,
    );
  }

  function errorSnapshot(error, fallback) {
    return {
      code: boundedMessage(error && error.code, 'REQUEST_FAILED', 48),
      message: boundedMessage(error && error.message, fallback, 240),
      status: error && Number.isFinite(error.status) ? error.status : 0,
    };
  }

  function primitiveSnapshotValue(value) {
    return value === null || (typeof value !== 'object' && typeof value !== 'function')
      ? value
      : undefined;
  }

  function snapshotLiveGame(game, includeVariants) {
    const source = game && typeof game === 'object' ? game : {};
    const id = primitiveSnapshotValue(source.id);
    const title = primitiveSnapshotValue(source.title);
    const system = primitiveSnapshotValue(source.system);
    const kind = primitiveSnapshotValue(source.kind);
    const state = primitiveSnapshotValue(source.state);
    const rootOnline = primitiveSnapshotValue(source.root_online);
    const contentPrepared = primitiveSnapshotValue(source.content_prepared);
    const execution = primitiveSnapshotValue(source.execution);
    if (
      typeof id !== 'string'
      || typeof title !== 'string'
      || typeof system !== 'string'
      || typeof kind !== 'string'
      || typeof state !== 'string'
      || typeof rootOnline !== 'boolean'
      || typeof contentPrepared !== 'boolean'
      || typeof execution !== 'string'
      || !id.trim()
      || !title.trim()
    ) throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid game record.');
    const record = {
      id,
      title,
      system,
      kind,
      state,
      root_online: rootOnline,
      content_prepared: contentPrepared,
      execution,
    };
    const genre = optionalCatalogText(source.genre);
    if (genre) record.genre = genre;
    const year = optionalCatalogText(source.year);
    if (year) record.year = year;
    if (source.favorite === true || source.favorite === false) record.favorite = source.favorite;
    if (Array.isArray(source.collections)) {
      const collections = source.collections
        .filter(item => typeof item === 'string' && /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(item.trim()))
        .map(item => item.trim());
      if (collections.length) record.collections = Object.freeze(collections);
    }
    if (source.launchable === true || source.launchable === false) record.launchable = source.launchable;
    const cover = primitiveSnapshotValue(source.cover);
    if (typeof cover === 'string' && ARTWORK_HANDLE_PATTERN.test(cover)) record.cover = cover;
    const platform = optionalCatalogText(source.platform);
    if (platform) record.platform = platform;
    const canonicalTitle = optionalCatalogText(source.canonical_title);
    if (canonicalTitle) record.canonical_title = canonicalTitle;
    const region = optionalCatalogText(source.region);
    if (region) record.region = region;
    const revision = optionalCatalogText(source.revision);
    if (revision) record.revision = revision;
    const dumpFlags = optionalCatalogText(source.dump_flags);
    if (dumpFlags) record.dump_flags = dumpFlags;
    const groupKey = optionalCatalogText(source.group_key);
    if (groupKey) record.group_key = groupKey;
    const variantCount = primitiveSnapshotValue(source.variant_count);
    if (typeof variantCount === 'number' && Number.isFinite(variantCount) && variantCount > 0) {
      record.variant_count = variantCount;
    }
    if (Array.isArray(source.variants) && includeVariants !== false) {
      record.variants = Object.freeze(source.variants.slice(0, 50).map(item => snapshotLiveGame(item, false)));
    }
    return Object.freeze(record);
  }

  function optionalCatalogText(value) {
    if (value === undefined || value === null || value === '') return undefined;
    return typeof value === 'string' ? value.trim() || undefined : undefined;
  }

  const CATALOG_REGIONS = Object.freeze({
    usa: 'usa', us: 'usa', europe: 'europe', japan: 'japan', world: 'world',
    brazil: 'brazil', korea: 'korea', asia: 'asia', australia: 'australia',
    france: 'france', germany: 'germany', spain: 'spain', italy: 'italy', canada: 'canada',
  });

  // Canonical dump-region tokens from catalog.mapDumpRegion. `other` is the
  // empty/other token, not a catch-all for unlisted regions.
  const DUMP_REGION_TOKENS = Object.freeze([
    'usa', 'japan', 'europe', 'world', 'brazil',
    'korea', 'asia', 'australia',
    'france', 'germany', 'spain', 'italy', 'canada',
    'other',
  ]);

  const DUMP_REGION_LABELS = Object.freeze({
    usa: 'USA',
    japan: 'Japan',
    europe: 'Europe',
    world: 'World',
    brazil: 'Brazil',
    korea: 'Korea',
    asia: 'Asia',
    australia: 'Australia',
    france: 'France',
    germany: 'Germany',
    spain: 'Spain',
    italy: 'Italy',
    canada: 'Canada',
    other: 'Other',
  });

  function catalogDumpRegions() {
    return DUMP_REGION_TOKENS.slice();
  }

  function regionLabel(region) {
    const token = String(region || '').trim();
    return DUMP_REGION_LABELS[token] || token;
  }

  function catalogRegion(title) {
    const tags = [];
    let current = String(title || '').trim();
    while (current) {
      const close = current[current.length - 1];
      const open = close === ')' ? '(' : close === ']' ? '[' : '';
      if (!open) break;
      const index = current.lastIndexOf(open);
      if (index <= 0) break;
      tags.push(current.slice(index + 1, -1).trim().toLowerCase());
      current = current.slice(0, index).trim();
    }
    for (const tag of tags) {
      const first = tag.split(',')[0].trim();
      if (CATALOG_REGIONS[first]) return CATALOG_REGIONS[first];
    }
    return 'other';
  }

  function dumpSuffix(value) {
    const first = String(value || '').split(',')[0].trim();
    if (CATALOG_REGIONS[first]) return true;
    if (/^rev(\s+\S+)?$/.test(first)) return true;
    return first === 'beta' || first === 'proto' || first === 'sample' || first === 'demo' || first === 'unl';
  }

  function displayTitle(title) {
    let current = String(title || '').trim();
    while (current) {
      const close = current[current.length - 1];
      const open = close === ')' ? '(' : close === ']' ? '[' : '';
      if (!open) break;
      const index = current.lastIndexOf(open);
      if (index <= 0) break;
      const inside = current.slice(index + 1, -1).trim().toLowerCase();
      if (!dumpSuffix(inside)) break;
      current = current.slice(0, index).trim();
    }
    return current || String(title || '').trim();
  }

  const CATALOG_PLATFORM_LABELS = Object.freeze({
    megadrive: 'Mega Drive',
    snes: 'SNES',
    nes: 'NES',
    gb: 'Game Boy',
    gbc: 'Game Boy Color',
    gba: 'Game Boy Advance',
    n64: 'Nintendo 64',
    psx: 'PlayStation',
    sms: 'Master System',
    gg: 'Game Gear',
    pce: 'PC Engine',
    '32x': '32X',
    saturn: 'Saturn',
    dc: 'Dreamcast',
    psp: 'PSP',
    nds: 'Nintendo DS',
    arcade: 'Arcade',
    a2600: 'Atari 2600',
    lynx: 'Lynx',
    ngp: 'Neo Geo Pocket',
    ws: 'WonderSwan',
  });

  function systemLabel(system) {
    return CATALOG_PLATFORM_LABELS[system] || String(system || '');
  }

  function sourceLabel(state) {
    if (state === 'available') return 'Ready';
    if (state === 'missing') return 'Offline';
    if (state === 'invalid') return 'Unreadable';
    return String(state || '');
  }

  function cardSourceOffline(game) {
    return Boolean(game) && (game.state === 'missing' || game.root_online === false);
  }

  function cardSourceUnreadable(game) {
    return Boolean(game) && game.state === 'invalid' && game.root_online !== false;
  }

  function coverStatusLabel(game) {
    if (cardSourceOffline(game)) return 'Offline';
    return sourceLabel(game && game.state);
  }

  function isSessionPlayingCard(session, game, sessionLive, sessionAuthority) {
    if (sessionAuthority !== undefined && sessionAuthority !== 'authoritative') return false;
    if (!session || session.state !== 'active' || !session.game_id || !game) return false;
    if (session.game_id === game.id) return true;
    if (Array.isArray(game.variants) && game.variants.some(item => item && item.id === session.game_id)) {
      return true;
    }
    return isSelectedGame(sessionLive || { id: session.game_id }, game);
  }

  function collectStateGames(state) {
    const games = [];
    if (state && state.selectedLiveGame) games.push(state.selectedLiveGame);
    (state && state.games ? state.games : []).forEach(game => games.push(game));
    for (const rail of (state && state.homeRails) || []) {
      for (const view of (rail && rail.gameViews) || []) {
        if (view && view.live) games.push(view.live);
      }
    }
    return games;
  }

  function findLiveGameInState(state, id) {
    if (!id || !state) return null;
    if (state.sessionLiveGame && state.sessionLiveGame.id === id) return state.sessionLiveGame;
    return collectStateGames(state).find(game => game && game.id === id) || null;
  }

  function groupedWallNeedsSessionHydration(state, sessionID) {
    return collectStateGames(state).some(game => game && game.group_key && game.id !== sessionID);
  }

  function cardTitle(game) {
    if (!game) return '';
    if (game.canonical_title) return game.canonical_title;
    return displayTitle(game.title);
  }

  const DUMP_PRERELEASE_FLAGS = Object.freeze(['beta', 'proto', 'sample', 'demo']);
  const DUMP_HACK_FLAGS = Object.freeze(['hack', 'unl']);
  const DUMP_FLAG_LABELS = Object.freeze({
    beta: 'Beta',
    proto: 'Proto',
    sample: 'Sample',
    demo: 'Demo',
    hack: 'Hack',
    unl: 'Unl',
  });

  function dumpFlagTokens(game) {
    return String((game && game.dump_flags) || '')
      .split(',')
      .map(token => token.trim().toLowerCase())
      .filter(Boolean);
  }

  function selectedDumpFlagTokens(game) {
    const tokens = new Set(dumpFlagTokens(game));
    const selected = [];
    const prerelease = DUMP_PRERELEASE_FLAGS.find(flag => tokens.has(flag));
    if (prerelease) selected.push(prerelease);
    const hack = DUMP_HACK_FLAGS.find(flag => tokens.has(flag));
    if (hack) selected.push(hack);
    return selected;
  }

  function dumpFlagLabels(game) {
    return selectedDumpFlagTokens(game).map(token => DUMP_FLAG_LABELS[token]);
  }

  function collectionLabels(game, collections) {
    const membership = Array.isArray(game && game.collections) ? game.collections : [];
    if (!membership.length) return [];
    const labels = [];
    for (const collection of Array.isArray(collections) ? collections : []) {
      if (!collection || !collection.id || !membership.includes(collection.id)) continue;
      const name = typeof collection.name === 'string' ? collection.name.trim() : '';
      if (!name) continue;
      labels.push(name);
    }
    return labels;
  }

  function sourceKindLabel(game) {
    const kind = game && typeof game.kind === 'string' ? game.kind.trim() : '';
    if (kind === 'zip') return 'ZIP';
    if (kind === 'raw') return 'ROM';
    return '';
  }

  function dumpIdentityFacts(game) {
    const facts = [];
    if (game && game.region) facts.push({ label: 'Region', value: regionLabel(game.region) });
    if (game && game.revision) facts.push({ label: 'Revision', value: `rev ${game.revision}` });
    if (game && game.dump_flags) {
      const known = new Set(Object.keys(DUMP_FLAG_LABELS));
      const extras = [];
      const seen = new Set();
      for (const token of dumpFlagTokens(game)) {
        if (known.has(token) || seen.has(token)) continue;
        seen.add(token);
        extras.push(token);
      }
      const value = [...dumpFlagLabels(game), ...extras].join(', ');
      if (value) facts.push({ label: 'Flags', value });
    }
    return facts;
  }

  function variantLabel(game) {
    const parts = dumpIdentityFacts(game).map(fact => fact.value);
    return parts.join(' · ') || (game && game.title) || 'Dump';
  }

  function coverHoverMeta(game, view) {
    const bits = [systemLabel(game.system)];
    if (game && game.region) bits.push(regionLabel(game.region));
    const year = (game && game.year) || catalogYear(view);
    if (year && year !== '—') bits.push(year);
    bits.push(coverStatusLabel(game));
    return bits.join(' · ');
  }

  function launchBlockReason(game) {
    if (!game) return 'Select a game first.';
    if (game.launchable === false) return 'This platform is browse-only on this host.';
    if (game.state === 'missing' || game.root_online === false) return 'This game’s source is offline.';
    if (game.state === 'invalid') return 'This ROM can’t be read.';
    if (game.state !== 'available') return 'This game isn’t ready to launch.';
    return '';
  }

  function catalogGenre(view) {
    const presentation = view && view.presentation;
    if (presentation && !presentation.isFallback) {
      const genre = typeof presentation.genre === 'string' ? presentation.genre.trim() : '';
      if (genre && genre !== '—' && genre !== 'Unknown') return genre;
    }
    const live = view && view.live;
    const liveGenre = live && typeof live.genre === 'string' ? live.genre.trim() : '';
    if (!liveGenre || liveGenre === '—' || liveGenre === 'Unknown') return '';
    return liveGenre;
  }

  function catalogStudio(view) {
    const presentation = view && view.presentation;
    if (!presentation || presentation.isFallback) return '';
    const studio = typeof presentation.studio === 'string' ? presentation.studio.trim() : '';
    if (!studio || studio === '—' || studio === 'FogCast demo') return '';
    return studio;
  }

  function catalogPlayers(view) {
    const presentation = view && view.presentation;
    if (!presentation || presentation.isFallback) return '';
    const players = typeof presentation.players === 'string' ? presentation.players.trim() : '';
    if (!players || players === '—' || players === 'Unknown' || players === 'Unknown players') return '';
    if (/^\d+$/.test(players)) {
      const count = Number(players);
      return count === 1 ? '1 player' : `${count} players`;
    }
    const range = /^(\d+)[-–](\d+)$/.exec(players);
    if (range) return `${range[1]}–${range[2]} players`;
    return players;
  }

  function filterCatalogViews(views) {
    return (views || []).slice();
  }

  function catalogYear(view) {
    const presentation = view && view.presentation;
    if (presentation && !presentation.isFallback && /^\d{4}$/.test(presentation.year || '')) {
      return presentation.year;
    }
    const live = view && view.live;
    return live && /^\d{4}$/.test(live.year || '') ? live.year : '';
  }

  function catalogSummary(view) {
    const presentation = view && view.presentation;
    if (!presentation || presentation.isFallback) return '';
    const summary = typeof presentation.summary === 'string' ? presentation.summary.trim() : '';
    if (!summary || summary === '—') return '';
    return summary;
  }

  function sortCatalogViews(views, sort) {
    const copy = (views || []).slice();
    if (sort !== 'year' && sort !== 'system') return copy;
    copy.sort((left, right) => {
      if (sort === 'year') {
        const yearDelta = (catalogYear(right) || '').localeCompare(catalogYear(left) || '');
        if (yearDelta) return yearDelta;
      }
      if (sort === 'system') {
        const systemDelta = String(left.live && left.live.system || '').localeCompare(String(right.live && right.live.system || ''));
        if (systemDelta) return systemDelta;
      }
      return String(left.live && left.live.title || '').localeCompare(String(right.live && right.live.title || ''));
    });
    return copy;
  }

  function formatCatalogCount(visible, total) {
    const shown = Number(visible) || 0;
    const all = Number(total) || 0;
    const shownText = shown.toLocaleString('en-US');
    const allText = all.toLocaleString('en-US');
    return shown === all ? `${allText} games` : `${shownText} of ${allText} games`;
  }

  function parseCatalog(payload) {
    if (!payload || !Array.isArray(payload.games)) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid catalog response.');
    }
    return Object.freeze(payload.games.map(snapshotLiveGame));
  }

  function parseDetail(payload, requestedID) {
    const detail = snapshotLiveGame(payload);
    if (detail.id !== requestedID) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned invalid game detail.');
    }
    return detail;
  }

  function validOptionalString(payload, field) {
    return !Object.prototype.hasOwnProperty.call(payload, field)
      || payload[field] === undefined
      || payload[field] === null
      || typeof payload[field] === 'string';
  }

  const SESSION_STATE_ALLOWLIST = Object.freeze(['idle', 'launching', 'active', 'stopping', 'failed']);
  // Host catalog platforms (catalog.DefaultPlatforms), not protocol.System.
  const SESSION_SYSTEM_ALLOWLIST = Object.freeze(Object.keys(CATALOG_PLATFORM_LABELS));
  const INPUT_STATE_ALLOWLIST = Object.freeze(['detached', 'starting', 'attached', 'reconnecting', 'failed']);
  const INPUT_METRIC_FIELDS = Object.freeze([
    'frames_sent',
    'state_resyncs',
    'sequence_gaps',
    'releases',
    'capture_to_bridge_p95_ms',
    'bridge_to_uinput_p95_ms',
    'rtt_ms',
  ]);

  function malformedSession(message) {
    throw createError('MALFORMED_RESPONSE', message);
  }

  function own(payload, field) {
    return Object.prototype.hasOwnProperty.call(payload, field);
  }

  function requiredString(payload, field, message) {
    if (typeof payload[field] !== 'string' || !payload[field].trim()) malformedSession(message);
    return payload[field];
  }

  function optionalSessionString(payload, field, message) {
    if (!own(payload, field)) return undefined;
    if (typeof payload[field] !== 'string') malformedSession(message);
    return payload[field];
  }

  function parseSessionProgress(value) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      malformedSession('The local host returned an invalid session progress summary.');
    }
    return Object.freeze({
      stage: requiredString(value, 'stage', 'The local host returned an invalid session progress stage.'),
      message: requiredString(value, 'message', 'The local host returned an invalid session progress message.'),
    });
  }

  function parseSessionInput(value) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      malformedSession('The local host returned an invalid input summary.');
    }
    if (!INPUT_STATE_ALLOWLIST.includes(value.state)) malformedSession('The local host returned an invalid input state.');
    if (typeof value.ready !== 'boolean') malformedSession('The local host returned an invalid input readiness value.');
    const metrics = value.metrics;
    if (!metrics || typeof metrics !== 'object' || Array.isArray(metrics)) {
      malformedSession('The local host returned invalid input metrics.');
    }
    const normalizedMetrics = {};
    for (const field of INPUT_METRIC_FIELDS) {
      const metric = metrics[field];
      if (typeof metric !== 'number' || !Number.isFinite(metric) || metric < 0) {
        malformedSession('The local host returned invalid input metrics.');
      }
      normalizedMetrics[field] = metric;
    }
    if (typeof metrics.bridge_to_uinput_measurable !== 'boolean') {
      malformedSession('The local host returned invalid input measurement state.');
    }
    normalizedMetrics.bridge_to_uinput_measurable = metrics.bridge_to_uinput_measurable;
    if (own(metrics, 'shutdown_reason')) {
      if (typeof metrics.shutdown_reason !== 'string') malformedSession('The local host returned an invalid input shutdown reason.');
      normalizedMetrics.shutdown_reason = metrics.shutdown_reason;
    }
    return Object.freeze({
      state: value.state,
      ready: value.ready,
      metrics: Object.freeze(normalizedMetrics),
    });
  }

  function parseSession(payload) {
    if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
      malformedSession('The local host returned an invalid session response.');
    }
    if (!SESSION_STATE_ALLOWLIST.includes(payload.state)) {
      malformedSession('The local host returned an invalid session state.');
    }
    const active = payload.state === 'active';
    if (!active && (own(payload, 'game_id') || own(payload, 'system'))) {
      malformedSession('The local host returned private session identity for a non-active state.');
    }
    const gameID = active && own(payload, 'game_id')
      ? requiredString(payload, 'game_id', 'The local host returned an invalid session game ID.')
      : undefined;
    const system = active && own(payload, 'system')
      ? payload.system
      : undefined;
    if (system !== undefined && (typeof system !== 'string' || !SESSION_SYSTEM_ALLOWLIST.includes(system))) {
      malformedSession('The local host returned an invalid session system.');
    }
    const execution = optionalSessionString(payload, 'execution', 'The local host returned an invalid session execution.');
    const media = optionalSessionString(payload, 'media', 'The local host returned an invalid session media state.');
    const progress = own(payload, 'progress') ? parseSessionProgress(payload.progress) : undefined;
    const input = own(payload, 'input') ? parseSessionInput(payload.input) : undefined;
    const result = { state: payload.state };
    if (gameID !== undefined) result.game_id = gameID;
    if (system !== undefined) result.system = system;
    if (execution !== undefined) result.execution = execution;
    if (media !== undefined) result.media = media;
    if (progress !== undefined) result.progress = progress;
    if (input !== undefined) result.input = input;
    return Object.freeze(result);
  }

  function sessionViewState(session) {
    if (!session) return 'idle';
    if (session.state === 'launching') return 'loading';
    if (session.state === 'stopping') return 'stopping';
    if (session.state === 'failed') return 'error';
    return session.state;
  }

  function sessionRequest() {
    return { path: '/api/v1/session', options: { method: 'GET' } };
  }

  function stopRequest() {
    return { path: '/api/v1/session/stop', options: { method: 'POST' } };
  }

  function validateLaunchSuccess(payload, requestedID) {
    let session;
    try {
      session = parseSession(payload);
    } catch (_) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    if (session.state !== 'active' || (session.game_id !== undefined && session.game_id !== requestedID)) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    return session;
  }

  function validateStopSuccess(payload) {
    let session;
    try {
      session = parseSession(payload);
    } catch (_) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid stop response.');
    }
    if (session.state !== 'idle') {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid stop response.');
    }
    return session;
  }

  async function request(fetchImpl, path, options) {
    let response;
    try {
      response = await fetchImpl(path, options);
    } catch (_) {
      throw createError('REQUEST_FAILED', 'The local request could not be completed.');
    }
    if (!response) {
      throw createError('REQUEST_FAILED', 'The local request could not be completed.');
    }
    if (typeof response.json !== 'function') {
      if (response.ok) throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid response.');
      throw safeError(response, null);
    }
    let payload;
    let parsed = true;
    try {
      payload = await response.json();
    } catch (_) {
      parsed = false;
    }
    if (!response.ok) throw safeError(response, parsed ? payload : null);
    if (!parsed) throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid response.');
    return payload;
  }

  function privacyMessage(error, fallback) {
    if (error && (error.code === 'TARGET_UNAVAILABLE' || error.code === 'MISTER_UNAVAILABLE')) {
      return 'The local target is unavailable. Check the development connection and retry.';
    }
    return boundedMessage(error && error.message, fallback, 240);
  }

  function boundedPresentationText(value, limit) {
    if (typeof value !== 'string' && typeof value !== 'number') return null;
    const text = String(value).trim();
    return text.length <= limit ? text : null;
  }

  function presentationField(value, fallback, limit) {
    const text = boundedPresentationText(value, limit);
    if (text === null) return null;
    return text || fallback;
  }

  function freezePresentation(value) {
    const result = {
      summary: value.summary,
      year: value.year,
      genre: value.genre,
      studio: value.studio,
      players: value.players,
      isFallback: value.isFallback,
      metadataState: value.metadataState || (value.isFallback ? 'fallback_offline' : 'ready'),
    };
    if (value.cover !== undefined) {
      result.cover = Object.freeze({
        palette: value.cover.palette,
        treatment: value.cover.treatment,
      });
    }
    if (value.backdrop !== undefined) {
      result.backdrop = Object.freeze({
        palette: value.backdrop.palette,
        treatment: value.backdrop.treatment,
      });
    }
    if (value.coverArtworkHandle !== undefined) result.coverArtworkHandle = value.coverArtworkHandle;
    if (value.backdropArtworkHandle !== undefined) result.backdropArtworkHandle = value.backdropArtworkHandle;
    if (value.logoHandle !== undefined) result.logoHandle = value.logoHandle;
    if (value.marqueeHandle !== undefined) result.marqueeHandle = value.marqueeHandle;
    if (value.videoHandle !== undefined) result.videoHandle = value.videoHandle;
    if (value.screenshotHandles !== undefined) result.screenshotHandles = value.screenshotHandles;
    if (value.attribution !== undefined) result.attribution = value.attribution;
    return Object.freeze(result);
  }

  function fallbackPresentation(metadataState = 'fallback_offline') {
    return freezePresentation({ ...PRESENTATION_FALLBACK, metadataState });
  }

  function trustedPresentation(value) {
    try {
      if (!value || typeof value !== 'object' || Array.isArray(value)) return fallbackPresentation();
      const cover = value.cover;
      const backdrop = value.backdrop;
      const coverPalette = cover && cover.palette;
      const coverTreatment = cover && cover.treatment;
      const backdropPalette = backdrop && backdrop.palette;
      const backdropTreatment = backdrop && backdrop.treatment;
      const summary = value.summary;
      const year = value.year;
      const genre = value.genre;
      const studio = value.studio;
      const players = value.players;
      const isFallback = value.isFallback;
      const coverArtworkHandle = value.coverArtworkHandle;
      const backdropArtworkHandle = value.backdropArtworkHandle;
      const logoHandle = value.logoHandle;
      const marqueeHandle = value.marqueeHandle;
      const videoHandle = value.videoHandle;
      const screenshotHandles = value.screenshotHandles;
      const attribution = value.attribution;
      const metadataState = value.metadataState || (isFallback ? 'fallback_offline' : 'ready');
      const artworkStyleRequired = Boolean(isFallback && cover && backdrop);
      const validHandle = handle => handle === undefined || (typeof handle === 'string' && ARTWORK_HANDLE_PATTERN.test(handle));
      const validHandleList = list => list === undefined || (Array.isArray(list) && list.every(item => typeof item === 'string' && ARTWORK_HANDLE_PATTERN.test(item)));
      if (
        typeof isFallback !== 'boolean'
        || !PRESENTATION_STATE_ALLOWLIST.includes(metadataState)
        || (artworkStyleRequired && (
          typeof cover !== 'object' || Array.isArray(cover)
          || typeof backdrop !== 'object' || Array.isArray(backdrop)
          || typeof coverPalette !== 'string'
          || typeof coverTreatment !== 'string'
          || typeof backdropPalette !== 'string'
          || typeof backdropTreatment !== 'string'
          || !PALETTE_ALLOWLIST.includes(coverPalette)
          || !PALETTE_ALLOWLIST.includes(backdropPalette)
          || !TREATMENT_ALLOWLIST.includes(coverTreatment)
          || !TREATMENT_ALLOWLIST.includes(backdropTreatment)
        ))
        || (!isFallback && (cover !== undefined || backdrop !== undefined))
        || (isFallback && !artworkStyleRequired && (cover !== undefined || backdrop !== undefined))
        || (!isFallback && !['ready', 'ready_artwork_error'].includes(metadataState))
        || (isFallback && !metadataState.startsWith('fallback_'))
        || !validHandle(coverArtworkHandle)
        || !validHandle(backdropArtworkHandle)
        || !validHandle(logoHandle)
        || !validHandle(marqueeHandle)
        || !validHandle(videoHandle)
        || !validHandleList(screenshotHandles)
        || (attribution !== undefined && (typeof attribution !== 'string' || !boundedPresentationText(attribution, 120)))
      ) return fallbackPresentation();
      if ([summary, year, genre, studio, players].some(item => typeof item !== 'string')) {
        return fallbackPresentation();
      }
      const text = {
        summary: isFallback
          ? presentationField(summary, PRESENTATION_FALLBACK.summary, PRESENTATION_TEXT_LIMITS.summary)
          : boundedPresentationText(summary, PRESENTATION_TEXT_LIMITS.summary),
        year: isFallback
          ? presentationField(year, PRESENTATION_FALLBACK.year, PRESENTATION_TEXT_LIMITS.year)
          : boundedPresentationText(year, PRESENTATION_TEXT_LIMITS.year),
        genre: isFallback
          ? presentationField(genre, PRESENTATION_FALLBACK.genre, PRESENTATION_TEXT_LIMITS.genre)
          : boundedPresentationText(genre, PRESENTATION_TEXT_LIMITS.genre),
        studio: isFallback
          ? presentationField(studio, PRESENTATION_FALLBACK.studio, PRESENTATION_TEXT_LIMITS.studio)
          : boundedPresentationText(studio, PRESENTATION_TEXT_LIMITS.studio),
        players: isFallback
          ? presentationField(players, PRESENTATION_FALLBACK.players, PRESENTATION_TEXT_LIMITS.players)
          : boundedPresentationText(players, PRESENTATION_TEXT_LIMITS.players),
      };
      if (Object.values(text).some(item => item === null)) return fallbackPresentation();
      return freezePresentation({
        ...(artworkStyleRequired ? {
          cover: { palette: coverPalette, treatment: coverTreatment },
          backdrop: { palette: backdropPalette, treatment: backdropTreatment },
        } : {}),
        ...text,
        isFallback,
        metadataState,
        coverArtworkHandle,
        backdropArtworkHandle,
        logoHandle,
        marqueeHandle,
        videoHandle,
        screenshotHandles: screenshotHandles ? Object.freeze(screenshotHandles.slice()) : undefined,
        attribution,
      });
    } catch (_) {
      return fallbackPresentation();
    }
  }

  function optionalArtworkHandle(value) {
    if (value === undefined || value === null || value === '') return undefined;
    return typeof value === 'string' && ARTWORK_HANDLE_PATTERN.test(value) ? value : undefined;
  }

  function localMediaFrom(payload) {
    const presentation = payload && payload.presentation;
    if (!presentation || typeof presentation !== 'object' || Array.isArray(presentation)) return {};
    const extra = {};
    const coverArtworkHandle = optionalArtworkHandle(presentation.cover_artwork_id);
    const backdropArtworkHandle = optionalArtworkHandle(presentation.backdrop_artwork_id);
    const logoHandle = optionalArtworkHandle(presentation.logo_id);
    const marqueeHandle = optionalArtworkHandle(presentation.marquee_id);
    const videoHandle = optionalArtworkHandle(presentation.video_id);
    const screenshots = Array.isArray(presentation.screenshot_ids)
      ? presentation.screenshot_ids.map(optionalArtworkHandle).filter(Boolean).slice(0, 8)
      : [];
    if (coverArtworkHandle) extra.coverArtworkHandle = coverArtworkHandle;
    if (backdropArtworkHandle) extra.backdropArtworkHandle = backdropArtworkHandle;
    if (logoHandle) extra.logoHandle = logoHandle;
    if (marqueeHandle) extra.marqueeHandle = marqueeHandle;
    if (videoHandle) extra.videoHandle = videoHandle;
    if (screenshots.length) extra.screenshotHandles = Object.freeze(screenshots);
    return extra;
  }

  function withLocalMedia(base, payload) {
    return freezePresentation({ ...base, ...localMediaFrom(payload) });
  }

  function acceptedAttribution(value) {
    return (value.provider === 'igdb' && value.label === 'Data from IGDB.com')
      || (value.provider === 'launchbox' && value.label === 'Data from LaunchBox Games Database');
  }

  function parsePresentation(payload, game) {
    const fallback = state => fallbackPresentation(state);
    try {
      if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return fallback();
      if (!game || typeof game.id !== 'string' || !GAME_ID_PATTERN.test(game.id) || payload.game_id !== game.id) return fallback('fallback_malformed');
      const stateMap = {
        disabled: 'fallback_disabled',
        unconfigured: 'fallback_unconfigured',
        no_match: 'fallback_no_match',
        ambiguous: 'fallback_ambiguous',
        offline: 'fallback_offline',
      };
      if (Object.prototype.hasOwnProperty.call(stateMap, payload.state)) {
        return withLocalMedia(fallback(stateMap[payload.state]), payload);
      }
      if (payload.state !== 'ready') return withLocalMedia(fallback('fallback_malformed'), payload);
      if (!payload.presentation || typeof payload.presentation !== 'object' || Array.isArray(payload.presentation)) {
        return fallback('fallback_malformed');
      }
      const local = localMediaFrom(payload);
      if (!payload.attribution || typeof payload.attribution !== 'object' || Array.isArray(payload.attribution)
        || !acceptedAttribution(payload.attribution)) {
        if (local.coverArtworkHandle || local.backdropArtworkHandle || local.videoHandle || local.logoHandle || local.marqueeHandle) {
          return freezePresentation({
            ...fallback('fallback_offline'),
            ...local,
          });
        }
        return fallback('fallback_malformed');
      }
      const value = {
        summary: payload.presentation.summary,
        year: payload.presentation.year,
        genre: payload.presentation.genre,
        studio: payload.presentation.studio,
        players: payload.presentation.players,
        isFallback: false,
        metadataState: 'ready',
        coverArtworkHandle: optionalArtworkHandle(payload.presentation.cover_artwork_id),
        backdropArtworkHandle: optionalArtworkHandle(payload.presentation.backdrop_artwork_id),
        logoHandle: local.logoHandle,
        marqueeHandle: local.marqueeHandle,
        videoHandle: local.videoHandle,
        screenshotHandles: local.screenshotHandles,
        attribution: payload.attribution.label,
      };
      return trustedPresentation(value);
    } catch (_) {
      return fallback('fallback_malformed');
    }
  }

  function restrictedMetadataInput(game) {
    return Object.freeze({
      id: game.id,
      title: game.title,
      system: game.system,
    });
  }

  function launcherGame(game, metadataAdapter) {
    let presentation;
    try {
      const input = restrictedMetadataInput(game);
      if (metadataAdapter && typeof metadataAdapter.metadataFor === 'function') {
        presentation = metadataAdapter.metadataFor(input);
      } else if (metadataAdapter && typeof metadataAdapter.toLauncherGame === 'function') {
        const result = metadataAdapter.toLauncherGame(input);
        presentation = result && typeof result === 'object' && Object.prototype.hasOwnProperty.call(result, 'presentation')
          ? result.presentation
          : result;
      }
    } catch (_) {
      presentation = undefined;
    }
    return Object.freeze({
      live: game,
      presentation: overlayLiveCover(game, trustedPresentation(presentation)),
    });
  }

  function overlayLiveCover(game, presentation) {
    if (!game || !game.cover || (presentation && presentation.coverArtworkHandle)) return presentation;
    if (!ARTWORK_HANDLE_PATTERN.test(game.cover)) return presentation;
    return freezePresentation({ ...presentation, coverArtworkHandle: game.cover });
  }

  function catalogViewState(state) {
    if (state.catalogState === 'loading') return 'loading';
    if (state.catalogState === 'catalog_error') return 'catalog_error';
    if (state.libraryView === 'home' && Number(state.homeRailFailures) > 0 && !(state.homeRails && state.homeRails.length)) {
      return 'catalog_error';
    }
    if (state.games.length === 0) {
      if (state.libraryView === 'home') return 'empty';
      return state.query ? 'no_matches' : 'empty';
    }
    return 'populated';
  }

  function createAppController(options = {}) {
    const fetchImpl = options.fetchImpl
      || (typeof root.fetch === 'function' ? root.fetch.bind(root) : null);
    const metadataAdapter = options.metadataAdapter || root.FogCastMetadata;
    const presentationEnabled = options.presentationEnabled === true || root.FogCastPresentationEnabled === true;
    const prefetchVisibleCovers = options.prefetchVisibleCovers === true || root.FogCastPrefetchVisibleCovers === true;
    const notify = typeof options.onStateChange === 'function' ? options.onStateChange : null;
    const coverRequests = Object.create(null);
    const presentationByID = Object.create(null);
    let coverObserver = null;
    const state = {
      query: '',
      collection: '',
      platformQuery: '',
      libraryView: 'home',
      catalogLayout: 'cover',
      homeRails: [],
      homeRailFailures: 0,
      nextCursor: '',
      loadingMore: false,
      platforms: [],
      collections: [],
      attractIdleSeconds: 60,
      librarySettings: null,
      games: [],
      gameViews: [],
      filters: Object.freeze({
        system: '', region: '', genre: '', year: '', availability: '',
        hide_prerelease: false, hide_hacks: false,
      }),
      facets: Object.freeze({ genres: [], years: [] }),
      sort: 'title',
      selectedLiveGame: null,
      selectedGameView: null,
      selectedPresentation: null,
      requestSequence: 0,
      settingsSequence: 0,
      collectionListSequence: 0,
      detailSequence: 0,
      presentationSequence: 0,
      selectionRevision: 0,
      statusSequence: 0,
      launchSequence: 0,
      stopSequence: 0,
      sessionStarted: false,
      sessionAuthority: 'indeterminate',
      session: null,
      sessionPhase: 'idle',
      sessionError: null,
      sessionWarning: null,
      sessionMessage: '',
      sessionGameTitle: '',
      sessionLiveGame: null,
      activeMutation: null,
      mutationMessage: '',
      catalogState: 'loading',
      catalogError: null,
      metadataFallbackCount: 0,
      metadataState: presentationEnabled ? 'metadata_idle' : 'metadata_fallback',
      metadataError: null,
      launchState: 'idle',
      launchError: null,
      launchMessage: '',
      detailState: 'idle',
      detailError: null,
      hostState: 'unknown',
    };

    let retainedSessionTitleID = '';
    let retainedSessionTitle = '';

    function liveSessionTitle(id) {
      const selected = state.selectedLiveGame && state.selectedLiveGame.id === id
        ? state.selectedLiveGame
        : (state.selectedGameView && state.selectedGameView.live && state.selectedGameView.live.id === id
          ? state.selectedGameView.live
          : null);
      const game = state.games.find(candidate => candidate.id === id) || selected;
      return game && game.title ? game.title : '';
    }

    function retainActiveSessionTitle() {
      if (!state.session || state.session.state !== 'active' || !state.session.game_id) {
        retainedSessionTitleID = '';
        retainedSessionTitle = '';
        return;
      }
      const id = state.session.game_id;
      const title = liveSessionTitle(id);
      if (title) {
        retainedSessionTitleID = id;
        retainedSessionTitle = title;
        return;
      }
      if (retainedSessionTitleID !== id) {
        retainedSessionTitleID = id;
        retainedSessionTitle = '';
      }
    }

    function sessionTitle() {
      if (!state.session || state.session.state !== 'active' || !state.session.game_id) return '';
      const id = state.session.game_id;
      return liveSessionTitle(id) || (retainedSessionTitleID === id ? retainedSessionTitle : '');
    }

    function snapshot() {
      return {
        ...state,
        games: state.games.slice(),
        gameViews: state.gameViews.slice(),
        homeRails: (state.homeRails || []).slice(),
        selectedLiveGame: state.selectedLiveGame,
        selectedGameView: state.selectedGameView,
        sessionGameTitle: sessionTitle(),
      };
    }

    function emit() {
      retainActiveSessionTitle();
      const next = snapshot();
      if (notify) {
        try { notify(next); } catch (_) { /* rendering must not change request state */ }
      }
      return next;
    }

    function hasFetchedPresentation(id) {
      return Object.prototype.hasOwnProperty.call(presentationByID, id);
    }

    function presentationHasCover(presentation) {
      return Boolean(presentation && (
        presentation.coverArtworkHandle
        || presentation.backdropArtworkHandle
        || presentation.metadataState === 'ready'
      ));
    }

    function applyHomeRailPresentation(id, presentation) {
      if (!state.homeRails || !state.homeRails.length) return;
      state.homeRails = Object.freeze(state.homeRails.map(rail => {
        const views = rail.gameViews.map(view => (
          view.live.id === id ? Object.freeze({ live: view.live, presentation }) : view
        ));
        return Object.freeze({ ...rail, gameViews: Object.freeze(views) });
      }));
    }

    function applyCatalogPresentation(id, presentation) {
      presentationByID[id] = presentation;
      const index = state.games.findIndex(game => game.id === id);
      if (index < 0) return;
      const nextViews = state.gameViews.slice();
      nextViews[index] = Object.freeze({ live: state.games[index], presentation });
      state.gameViews = Object.freeze(nextViews);
      applyHomeRailPresentation(id, presentation);
      state.metadataFallbackCount = metadataFallbackCount(nextViews);
      if (state.selectedLiveGame && state.selectedLiveGame.id === id) {
        state.selectedPresentation = presentation;
        state.selectedGameView = nextViews[index];
        if (state.metadataState === 'metadata_idle' || state.metadataState === 'metadata_loading') {
          state.metadataState = presentation.metadataState;
        }
      }
    }

    function queueVisiblePresentation(id) {
      if (!prefetchVisibleCovers || !presentationEnabled || !id || coverRequests[id] || hasFetchedPresentation(id)) return;
      const index = state.games.findIndex(game => game.id === id);
      if (index < 0) return;
      const current = state.gameViews[index] && state.gameViews[index].presentation;
      if (presentationHasCover(current)) return;
      coverRequests[id] = true;
      request(fetchImpl, presentationPath(id)).then(payload => {
        const game = state.games.find(item => item.id === id);
        applyCatalogPresentation(id, parsePresentation(payload, game || { id }));
        if (!game) return;
        emit();
      }).catch(() => {
        if (!hasFetchedPresentation(id)) presentationByID[id] = fallbackPresentation();
      }).finally(() => {
        delete coverRequests[id];
      });
    }

    function observeVisibleCovers() {
      if (!prefetchVisibleCovers || !presentationEnabled) return;
      const Observer = options.IntersectionObserver || root.IntersectionObserver;
      let list = null;
      let cards = [];
      if (typeof document !== 'undefined' && document && typeof document.getElementById === 'function') {
        list = document.getElementById('catalog-list');
        if (list && typeof list.querySelectorAll === 'function') {
          cards = list.querySelectorAll('.game-card[data-game-id]');
        }
      }
      Array.prototype.forEach.call(cards, card => {
        const id = typeof card.getAttribute === 'function' ? card.getAttribute('data-game-id') : '';
        if (id) queueVisiblePresentation(id);
      });
      if (!cards.length) {
        if (state.catalogState !== 'loading') {
          state.games.forEach(game => queueVisiblePresentation(game.id));
        }
        return;
      }
      if (typeof Observer !== 'function') return;
      if (coverObserver) coverObserver.disconnect();
      coverObserver = new Observer(entries => {
        entries.forEach(entry => {
          if (!entry.isIntersecting) return;
          const id = entry.target.getAttribute('data-game-id');
          if (id) queueVisiblePresentation(id);
        });
      }, { root: list, rootMargin: '240px', threshold: 0.01 });
      Array.prototype.forEach.call(cards, card => {
        coverObserver.observe(card);
      });
    }

    function enrich(game) {
      return launcherGame(game, presentationEnabled ? null : metadataAdapter);
    }

    function retainPresentation(game, previous) {
      const next = enrich(game);
      const kept = presentationHasCover(previous) ? previous : presentationByID[game.id];
      if (!kept) return next;
      presentationByID[game.id] = overlayLiveCover(game, kept);
      return Object.freeze({ live: game, presentation: presentationByID[game.id] });
    }

    function metadataFallbackCount(views) {
      return views.reduce((count, view) => count + (view.presentation.isFallback ? 1 : 0), 0);
    }

    function resetSelectionState() {
      state.selectionRevision += 1;
      state.detailSequence += 1;
      state.presentationSequence += 1;
      state.selectedPresentation = null;
      state.metadataError = null;
      state.metadataState = presentationEnabled ? 'metadata_idle' : 'metadata_fallback';
      state.detailState = 'idle';
      state.detailError = null;
      if (!state.activeMutation) {
        state.launchState = 'idle';
        state.launchError = null;
        state.launchMessage = '';
      }
    }

    let sessionLiveSequence = 0;

    function reconcileSelection() {
      if (!state.selectedLiveGame) return;
      let index = state.games.findIndex(game => game.id === state.selectedLiveGame.id);
      if (index < 0 && state.selectedLiveGame.group_key) {
        index = state.games.findIndex(game => game.group_key === state.selectedLiveGame.group_key);
      }
      const wasMutating = Boolean(state.activeMutation);
      resetSelectionState();
      state.selectedLiveGame = index >= 0 ? state.games[index] : null;
      state.selectedGameView = index >= 0 ? state.gameViews[index] : null;
      if (wasMutating && !state.selectedLiveGame) state.selectedGameView = null;
    }

    function acceptSession(session) {
      state.session = session;
      state.sessionAuthority = 'authoritative';
      state.sessionPhase = sessionViewState(session);
      state.sessionError = session.state === 'failed'
        ? { code: 'SESSION_FAILED', message: 'The local host reports a failed session.', status: 200 }
        : null;
      state.sessionWarning = null;
      state.sessionMessage = session.state === 'failed'
        ? 'The local host reports a failed session.'
        : '';
      if (session.state !== 'active' || !session.game_id) {
        sessionLiveSequence += 1;
        state.sessionLiveGame = null;
      } else if (state.sessionLiveGame && state.sessionLiveGame.id !== session.game_id) {
        state.sessionLiveGame = null;
      }
    }

    function sessionFailure(error, phase = 'unavailable') {
      state.sessionAuthority = state.session ? 'last-known' : 'indeterminate';
      state.sessionPhase = phase;
      const fallback = phase === 'malformed'
        ? 'The local host returned an invalid session response.'
        : 'The local host session could not be checked.';
      state.sessionError = errorSnapshot(error, fallback);
      if (phase === 'malformed') {
        state.sessionError.code = 'MALFORMED_RESPONSE';
        state.sessionError.message = fallback;
      }
      state.sessionWarning = phase === 'malformed' ? state.sessionError : null;
      state.sessionMessage = privacyMessage(error, state.sessionError.message);
      if (phase === 'malformed') state.sessionMessage = fallback;
    }

    async function emitHydratedSessionLive(isCurrent) {
      await hydrateSessionLiveGame();
      if (typeof isCurrent === 'function' && !isCurrent()) return snapshot();
      return emit();
    }

    async function hydrateSessionLiveGame() {
      const session = state.session;
      const sessionID = session && session.state === 'active' ? session.game_id : '';
      if (!sessionID) {
        state.sessionLiveGame = null;
        return;
      }
      const known = findLiveGameInState(state, sessionID);
      if (known) {
        state.sessionLiveGame = known;
        return;
      }
      if (!groupedWallNeedsSessionHydration(state, sessionID)) {
        state.sessionLiveGame = null;
        return;
      }
      const sequence = ++sessionLiveSequence;
      const requestedID = sessionID;
      let payload;
      try {
        payload = await request(fetchImpl, gameDetailPath(requestedID));
      } catch {
        if (sequence !== sessionLiveSequence) return;
        if (state.sessionLiveGame && state.sessionLiveGame.id !== requestedID) {
          state.sessionLiveGame = null;
        }
        return;
      }
      if (sequence !== sessionLiveSequence) return;
      if (!state.session || state.session.game_id !== requestedID || state.session.state !== 'active') {
        return;
      }
      try {
        state.sessionLiveGame = parseDetail(payload, requestedID);
      } catch {
        if (sequence !== sessionLiveSequence) return;
        if (state.sessionLiveGame && state.sessionLiveGame.id !== requestedID) {
          state.sessionLiveGame = null;
        }
      }
    }

    function mutationIsCurrent(mutation) {
      return state.activeMutation === mutation.kind
        && (mutation.kind === 'launch'
          ? state.launchSequence === mutation.sequence
          : state.stopSequence === mutation.sequence);
    }

    async function loadSession() {
      if (state.activeMutation) {
        state.mutationMessage = 'A session transition is already in progress.';
        return emit();
      }
      const sequence = ++state.statusSequence;
      state.sessionStarted = true;
      state.sessionAuthority = 'indeterminate';
      state.sessionPhase = 'loading';
      state.sessionError = null;
      state.sessionWarning = null;
      state.sessionMessage = '';
      emit();
      try {
        const spec = sessionRequest();
        const payload = await request(fetchImpl, spec.path, spec.options);
        if (sequence !== state.statusSequence || state.activeMutation) return snapshot();
        acceptSession(parseSession(payload));
        emit();
        return emitHydratedSessionLive(
          () => sequence === state.statusSequence && !state.activeMutation
        );
      } catch (error) {
        if (sequence !== state.statusSequence || state.activeMutation) return snapshot();
        sessionFailure(error, error && error.code === 'MALFORMED_RESPONSE' ? 'malformed' : 'unavailable');
        return emit();
      }
    }

    async function reconcileMutation(mutation) {
      const sequence = ++state.statusSequence;
      try {
        const spec = sessionRequest();
        const payload = await request(fetchImpl, spec.path, spec.options);
        const session = parseSession(payload);
        if (sequence !== state.statusSequence || !mutationIsCurrent(mutation)) return { stale: true };
        acceptSession(session);
        void emitHydratedSessionLive(
          () => sequence === state.statusSequence && !state.activeMutation
        );
        return { session };
      } catch (error) {
        if (sequence !== state.statusSequence || !mutationIsCurrent(mutation)) return { stale: true };
        return { error };
      }
    }

    function finishMutation(mutation, result, operationError) {
      if (!mutationIsCurrent(mutation)) return snapshot();
      if (result.stale) return snapshot();
      const selectionChanged = mutation.kind === 'launch'
        && state.selectionRevision !== mutation.selectionRevision;
      if (selectionChanged) {
        state.launchState = 'idle';
        state.launchError = null;
        state.launchMessage = '';
      }
      const sessionConfirmed = mutation.kind === 'launch'
        && !result.error
        && result.session
        && result.session.state === 'active'
        && result.session.game_id === mutation.requestedID;
      if (result.error) {
        if (mutation.kind === 'launch' && !selectionChanged) {
          state.launchState = 'launch_error';
          state.launchError = errorSnapshot(result.error, 'The launch could not be confirmed by the local host.');
          state.launchMessage = privacyMessage(result.error, 'The launch could not be confirmed by the local host.');
        }
        sessionFailure(result.error, result.error.code === 'MALFORMED_RESPONSE' ? 'malformed' : 'unavailable');
        state.sessionMessage = privacyMessage(result.error, 'The local host session could not be reconciled.');
      } else if (mutation.kind === 'launch' && !selectionChanged) {
        const reconciled = result.session;
        if (sessionConfirmed && !operationError) {
          state.sessionPhase = 'active';
          state.launchState = 'launch_success';
          state.launchError = null;
          state.launchMessage = 'launch_success: session accepted by the local host.';
          state.sessionMessage = '';
        } else {
          state.sessionPhase = reconciled.state === 'failed' ? 'error' : sessionViewState(reconciled);
          state.launchState = 'launch_error';
          state.launchError = errorSnapshot(
            operationError || createError('SESSION_POSTCONDITION_FAILED', 'The local host did not confirm the requested active session.'),
            'The launch could not be confirmed by the local host.',
          );
          state.launchMessage = privacyMessage(state.launchError, 'The launch could not be confirmed by the local host.');
          state.sessionMessage = state.launchMessage;
        }
      } else if (mutation.kind === 'launch') {
        state.sessionPhase = sessionViewState(result.session);
        if (operationError) {
          state.sessionError = errorSnapshot(operationError, 'The launch could not be confirmed by the local host.');
          state.sessionMessage = privacyMessage(operationError, 'The launch could not be confirmed by the local host.');
        } else {
          state.sessionError = null;
          state.sessionMessage = '';
        }
      } else {
        const reconciled = result.session;
        if (!operationError && mutation.stopResponseValid && reconciled.state === 'idle') {
          state.sessionPhase = 'stopped';
          state.sessionMessage = 'Session stopped.';
        } else {
          if (operationError && operationError.code === 'MALFORMED_RESPONSE' && reconciled.state === 'idle') {
            state.sessionPhase = 'malformed';
            state.sessionWarning = errorSnapshot(operationError, 'The local host returned an invalid stop response.');
          } else {
            state.sessionPhase = 'error';
          }
          state.sessionError = errorSnapshot(
            operationError || createError('SESSION_POSTCONDITION_FAILED', 'The local host did not confirm that the session stopped.'),
            'The session could not be stopped.',
          );
          state.sessionMessage = privacyMessage(state.sessionError, 'The session could not be stopped.');
        }
      }
      state.activeMutation = null;
      state.mutationMessage = '';
      if (sessionConfirmed && mutation.requestedID && mutation.requestedTitle) {
        retainedSessionTitleID = mutation.requestedID;
        retainedSessionTitle = mutation.requestedTitle;
      }
      const next = emit();
      if (sessionConfirmed && state.libraryView === 'home') {
        return reconcileHomeLaunchRails({ preserveLaunch: true });
      }
      return next;
    }

    async function runMutation(mutation, operation) {
      state.activeMutation = mutation.kind;
      state.mutationMessage = '';
      state.sessionError = null;
      state.sessionWarning = null;
      state.sessionMessage = '';
      state.sessionPhase = mutation.kind === 'stop' ? 'stopping' : 'loading';
      if (mutation.kind === 'launch') {
        state.launchState = 'launching';
        state.launchError = null;
        state.launchMessage = '';
      }
      emit();
      let operationError = null;
      try {
        const payload = await operation();
        mutation.stopResponseValid = mutation.kind === 'stop';
        if (mutation.kind === 'launch') mutation.launchResponse = validateLaunchSuccess(payload, mutation.requestedID);
        else mutation.stopResponseValid = Boolean(validateStopSuccess(payload));
      } catch (error) {
        operationError = error;
      }
      const reconciled = await reconcileMutation(mutation);
      return finishMutation(mutation, reconciled, operationError);
    }

    function mutationConflict() {
      state.mutationMessage = 'A session transition is already in progress.';
      state.sessionMessage = state.mutationMessage;
      return emit();
    }

    function launchAllowed(selected) {
      if (!selected || selected.launchable === false || selected.state !== 'available') return false;
      if (!state.sessionStarted) return true;
      if (state.sessionAuthority !== 'authoritative') return false;
      if (!state.session || !['idle', 'stopped', 'active'].includes(state.sessionPhase)) return false;
      return !(state.session.state === 'active' && state.session.game_id === selected.id);
    }

    async function launchLegacy(selected) {
      const selectionRevision = state.selectionRevision;
      const sequence = ++state.launchSequence;
      state.launchState = 'launching';
      state.launchError = null;
      state.launchMessage = '';
      emit();
      try {
        const requestSpec = launchRequest(selected);
        const response = await request(fetchImpl, requestSpec.path, requestSpec.options);
        validateLaunchSuccess(response, selected.id);
        if (sequence !== state.launchSequence || selectionRevision !== state.selectionRevision) return snapshot();
        state.launchState = 'launch_success';
        state.launchMessage = '';
        return emit();
      } catch (error) {
        if (sequence !== state.launchSequence || selectionRevision !== state.selectionRevision) return snapshot();
        state.launchState = 'launch_error';
        state.launchError = errorSnapshot(error, 'The launch could not be completed.');
        state.launchMessage = privacyMessage(error, 'The launch could not be completed.');
        return emit();
      }
    }

    async function launchSelected() {
      const selected = state.selectedLiveGame;
      if (state.activeMutation) return mutationConflict();
      if (!selected) return snapshot();
      if (!launchAllowed(selected)) return emit();
      if (!state.sessionStarted) return launchLegacy(selected);
      const mutation = {
        kind: 'launch',
        sequence: ++state.launchSequence,
        selectionRevision: state.selectionRevision,
        requestedID: selected.id,
        requestedTitle: selected.title || '',
        launchResponse: null,
      };
      state.statusSequence += 1;
      return runMutation(mutation, async () => {
        const requestSpec = launchRequest(selected);
        return request(fetchImpl, requestSpec.path, requestSpec.options);
      });
    }

    async function stopSession() {
      if (state.activeMutation) return mutationConflict();
      if (
        state.sessionAuthority !== 'authoritative'
        || !state.sessionStarted
        || !state.session
        || state.session.state !== 'active'
      ) return emit();
      const mutation = {
        kind: 'stop',
        sequence: ++state.stopSequence,
        stopResponseValid: false,
      };
      state.statusSequence += 1;
      return runMutation(mutation, async () => {
        const spec = stopRequest();
        return request(fetchImpl, spec.path, spec.options);
      });
    }

    function catalogViewSort(collection, extraSort) {
      if (extraSort !== undefined) return extraSort;
      return catalogEffectiveSort({
        libraryView: state.libraryView,
        collection,
        sort: state.sort,
      });
    }

    function catalogExtras(cursor, overrides) {
      const extra = overrides && typeof overrides === 'object' ? overrides : {};
      const extras = { grouped: 1 };
      const collection = extra.collection !== undefined ? extra.collection : state.collection;
      if (collection) extras.collection = collection;
      if (state.libraryView !== 'home') {
        if (state.platformQuery) extras.platform = state.platformQuery;
        if (state.filters.region) extras.region = state.filters.region;
        if (state.filters.genre) extras.genre = state.filters.genre;
        if (state.filters.year) extras.year = state.filters.year;
        if (state.filters.availability) extras.availability = state.filters.availability;
      }
      if (state.filters.hide_prerelease) extras.hide_prerelease = 1;
      if (state.filters.hide_hacks) extras.hide_hacks = 1;
      const sort = catalogViewSort(collection, extra.sort);
      if (sort && sort !== 'title' && sort !== 'recents') extras.sort = sort;
      if (cursor) extras.cursor = cursor;
      if (Number(extra.limit) > 0) extras.limit = extra.limit;
      return extras;
    }

    function flattenHomeGames(rails) {
      const seen = new Set();
      const games = [];
      const views = [];
      (rails || []).forEach(rail => {
        (rail.gameViews || []).forEach(view => {
          if (!view || !view.live || seen.has(view.live.id)) return;
          seen.add(view.live.id);
          games.push(view.live);
          views.push(view);
        });
      });
      state.games = Object.freeze(games);
      state.gameViews = Object.freeze(views);
      state.metadataFallbackCount = metadataFallbackCount(views);
    }

    function replaceHomeRailGame(updated) {
      if (!state.homeRails || !state.homeRails.length) return;
      state.homeRails = Object.freeze(state.homeRails.map(rail => {
        const views = rail.gameViews.map(view => (
          view.live.id === updated.id
            ? Object.freeze({ live: updated, presentation: view.presentation })
            : view
        ));
        return Object.freeze({ ...rail, gameViews: Object.freeze(views) });
      }));
    }

    function replaceGame(updated) {
      const index = state.games.findIndex(game => game.id === updated.id);
      if (index >= 0) {
        const games = state.games.slice();
        const views = state.gameViews.slice();
        games[index] = updated;
        views[index] = Object.freeze({
          live: updated,
          presentation: views[index] ? views[index].presentation : enrich(updated).presentation,
        });
        state.games = Object.freeze(games);
        state.gameViews = Object.freeze(views);
        replaceHomeRailGame(updated);
        if (state.selectedLiveGame && state.selectedLiveGame.id === updated.id) {
          state.selectedLiveGame = updated;
          state.selectedGameView = views[index];
        }
      } else if (state.selectedLiveGame && state.selectedLiveGame.id === updated.id) {
        state.selectedLiveGame = updated;
        if (state.selectedGameView) {
          state.selectedGameView = Object.freeze({ live: updated, presentation: state.selectedGameView.presentation });
        }
        replaceHomeRailGame(updated);
      }
    }

    function clearHomePlatformFilter() {
      state.platformQuery = '';
      state.filters = Object.freeze({
        ...state.filters,
        system: '',
      });
    }

    function homeRailSort(spec) {
      if (spec && spec.sort !== undefined && spec.sort !== '') return spec.sort;
      return collectionFetchSort(spec && spec.id);
    }

    function homeRailSpecByID(id) {
      const smart = HOME_SMART_RAILS.find(rail => rail.id === id);
      if (smart) return { id: smart.id, name: smart.name, kind: 'smart', sort: homeRailSort(smart) };
      const collection = (state.collections || []).find(item => item && item.id === id);
      if (collection) return { id: collection.id, name: collection.name || collection.id, kind: 'custom', sort: 'title' };
      return null;
    }

    async function fetchHomeRailSpec(spec) {
      try {
        const extras = {
          collection: spec.id,
          limit: HOME_RAIL_LIMIT,
          sort: homeRailSort(spec),
        };
        const result = await request(fetchImpl, gamesPath('', catalogExtras('', extras)));
        return { spec, result, error: null };
      } catch (error) {
        return { spec, result: null, error };
      }
    }

    function materializeHomeRail(item, previousByID) {
      if (item.error) return { rail: null, error: true };
      const games = parseCatalog(item.result);
      if (!games.length) return { rail: null, error: false };
      return {
        rail: Object.freeze({
          id: item.spec.id,
          name: item.spec.name,
          gameViews: Object.freeze(games.map(game => retainPresentation(game, previousByID.get(game.id)))),
        }),
        error: false,
      };
    }

    function capCustomHomeRails(rails) {
      const smartIDs = HOME_SMART_RAILS.map(item => item.id);
      const customIDs = (state.collections || []).map(item => item.id);
      const smart = HOME_SMART_RAILS.map(item => rails.find(rail => rail.id === item.id)).filter(Boolean);
      const custom = rails
        .filter(rail => !smartIDs.includes(rail.id))
        .sort((left, right) => {
          const leftIndex = customIDs.indexOf(left.id);
          const rightIndex = customIDs.indexOf(right.id);
          return (leftIndex < 0 ? Number.MAX_SAFE_INTEGER : leftIndex)
            - (rightIndex < 0 ? Number.MAX_SAFE_INTEGER : rightIndex);
        })
        .slice(0, HOME_CUSTOM_RAIL_CAP);
      return smart.concat(custom);
    }

    function insertHomeRail(rails, rail, spec) {
      const next = rails.filter(item => item.id !== spec.id);
      if (rail) {
        const smartIDs = HOME_SMART_RAILS.map(item => item.id);
        if (spec.kind === 'smart') {
          const desired = smartIDs.indexOf(spec.id);
          let index = next.findIndex(item => {
            const other = smartIDs.indexOf(item.id);
            return other === -1 || other > desired;
          });
          if (index < 0) index = next.length;
          next.splice(index, 0, rail);
        } else {
          const customIDs = (state.collections || []).map(item => item.id);
          const desired = customIDs.indexOf(spec.id);
          let index = next.findIndex(item => {
            if (smartIDs.includes(item.id)) return false;
            const other = customIDs.indexOf(item.id);
            return other === -1 || other > desired;
          });
          if (index < 0) index = next.length;
          next.splice(index, 0, rail);
        }
      }
      return capCustomHomeRails(next);
    }

    function applyHomeRails(rails, failed, firstError, options) {
      state.homeRails = Object.freeze(rails);
      state.homeRailFailures = failed;
      flattenHomeGames(rails);
      if (!rails.length && failed > 0) {
        state.catalogState = 'catalog_error';
        state.catalogError = errorSnapshot(firstError, 'The catalog could not be loaded.');
        state.hostState = 'unavailable';
        return emit();
      }
      state.catalogState = 'populated';
      state.catalogError = null;
      state.hostState = 'ready';
      state.metadataState = presentationEnabled
        ? 'metadata_idle'
        : (state.metadataFallbackCount ? 'metadata_fallback' : 'curated');
      if (options && options.preserveLaunch) rebindSelectedHomeGame();
      else reconcileSelection();
      const generation = state.requestSequence;
      emit();
      return emitHydratedSessionLive(() => generation === state.requestSequence);
    }

    const homeRailSequences = Object.create(null);
    let homeLaunchReconcileSequence = 0;

    function bumpHomeRailSequence(railID) {
      const generation = (homeRailSequences[railID] || 0) + 1;
      homeRailSequences[railID] = generation;
      return generation;
    }

    function currentHomeRail(railID) {
      return (state.homeRails || []).find(rail => rail.id === railID) || null;
    }

    function pushHomeRailIfCurrent(rails, railID, generation, nextRail) {
      if (homeRailSequences[railID] === generation) {
        if (nextRail) rails.push(nextRail);
        return;
      }
      const current = currentHomeRail(railID);
      if (current) rails.push(current);
    }

    function replaceStaleBuiltHomeRails(rails, generations) {
      return rails.reduce((next, rail) => {
        if (generations[rail.id] === undefined || homeRailSequences[rail.id] === generations[rail.id]) {
          next.push(rail);
          return next;
        }
        const current = currentHomeRail(rail.id);
        if (current) next.push(current);
        return next;
      }, []);
    }

    async function loadHomeRails() {
      const sequence = ++state.requestSequence;
      state.libraryView = 'home';
      state.collection = '';
      clearHomePlatformFilter();
      state.nextCursor = '';
      state.loadingMore = false;
      state.catalogState = 'loading';
      state.catalogError = null;
      state.homeRails = Object.freeze([]);
      emit();
      const previousByID = new Map(state.gameViews.map(view => [view.live.id, view.presentation]));
      const smartSpecs = HOME_SMART_RAILS.map(rail => ({
        id: rail.id,
        name: rail.name,
        kind: 'smart',
        sort: homeRailSort(rail),
      }));
      const railGenerations = Object.create(null);
      smartSpecs.forEach(spec => {
        railGenerations[spec.id] = bumpHomeRailSequence(spec.id);
      });
      const smartItems = await Promise.all(smartSpecs.map(fetchHomeRailSpec));
      if (sequence !== state.requestSequence) return snapshot();
      const rails = [];
      let failed = 0;
      let firstError = null;
      smartItems.forEach(item => {
        const railID = item.spec.id;
        if (homeRailSequences[railID] !== railGenerations[railID]) {
          pushHomeRailIfCurrent(rails, railID, railGenerations[railID], null);
          return;
        }
        const materialized = materializeHomeRail(item, previousByID);
        if (materialized.error) {
          failed += 1;
          if (!firstError) firstError = item.error;
          return;
        }
        pushHomeRailIfCurrent(rails, railID, railGenerations[railID], materialized.rail);
      });
      const collections = state.collections || [];
      const smartIDs = HOME_SMART_RAILS.map(item => item.id);
      for (let index = 0; index < collections.length; index += 1) {
        const customKept = rails.filter(rail => !smartIDs.includes(rail.id)).length;
        if (customKept >= HOME_CUSTOM_RAIL_CAP) break;
        const collection = collections[index];
        if (!collection || !collection.id) continue;
        railGenerations[collection.id] = bumpHomeRailSequence(collection.id);
        const item = await fetchHomeRailSpec({
          id: collection.id,
          name: collection.name || collection.id,
          kind: 'custom',
          sort: 'title',
        });
        if (sequence !== state.requestSequence) return snapshot();
        if (homeRailSequences[collection.id] !== railGenerations[collection.id]) {
          pushHomeRailIfCurrent(rails, collection.id, railGenerations[collection.id], null);
          continue;
        }
        const materialized = materializeHomeRail(item, previousByID);
        if (materialized.error) {
          failed += 1;
          if (!firstError) firstError = item.error;
          continue;
        }
        pushHomeRailIfCurrent(rails, collection.id, railGenerations[collection.id], materialized.rail);
      }
      const overlappedReconcile = Object.keys(railGenerations).some(
        id => homeRailSequences[id] !== railGenerations[id],
      );
      return applyHomeRails(
        replaceStaleBuiltHomeRails(rails, railGenerations),
        failed,
        firstError,
        {
          preserveLaunch: state.launchState === 'launch_success' && overlappedReconcile,
        },
      );
    }

    function rebindSelectedHomeGame() {
      if (!state.selectedLiveGame) return;
      const index = state.games.findIndex(game => game.id === state.selectedLiveGame.id);
      if (index < 0) return;
      state.selectedLiveGame = state.games[index];
      state.selectedGameView = state.gameViews[index];
    }

    async function reconcileHomeRail(railID, options) {
      if (state.libraryView !== 'home') return emit();
      const spec = homeRailSpecByID(railID);
      if (!spec) return emit();
      const requestGeneration = state.requestSequence;
      const sequence = bumpHomeRailSequence(railID);
      const item = await fetchHomeRailSpec(spec);
      if (
        requestGeneration !== state.requestSequence
        || homeRailSequences[railID] !== sequence
        || state.libraryView !== 'home'
      ) return snapshot();
      const previousByID = new Map(state.gameViews.map(view => [view.live.id, view.presentation]));
      const materialized = materializeHomeRail(item, previousByID);
      if (materialized.error) return emit();
      const rails = insertHomeRail((state.homeRails || []).slice(), materialized.rail, spec);
      state.homeRails = Object.freeze(rails);
      flattenHomeGames(rails);
      if (options && options.preserveLaunch) rebindSelectedHomeGame();
      else reconcileSelection();
      emit();
      return emitHydratedSessionLive(() => (
        requestGeneration === state.requestSequence
        && homeRailSequences[railID] === sequence
        && state.libraryView === 'home'
      ));
    }

    function homeLaunchRailBatchIsStale(batch, requestGeneration) {
      return batch !== homeLaunchReconcileSequence
        || requestGeneration !== state.requestSequence
        || state.libraryView !== 'home';
    }

    async function reconcileHomeLaunchRails(options) {
      const batch = ++homeLaunchReconcileSequence;
      const requestGeneration = state.requestSequence;
      let next = emit();
      for (const railID of HOME_LAUNCH_RECONCILE_RAILS) {
        if (homeLaunchRailBatchIsStale(batch, requestGeneration)) return snapshot();
        next = await reconcileHomeRail(railID, options);
        if (homeLaunchRailBatchIsStale(batch, requestGeneration)) return snapshot();
      }
      return next;
    }

    async function reloadVisibleCatalog(query) {
      state.query = String(query || '').trim();
      if (state.libraryView === 'home') return loadHomeRails();
      return loadCatalog(state.query);
    }

    async function searchVisibleCatalog(query) {
      state.query = String(query || '').trim();
      if (state.libraryView === 'home') {
        if (state.query) return setLibraryNav('', '');
        return loadHomeRails();
      }
      return loadCatalog(state.query);
    }

    function adoptSearchQuery(query) {
      state.query = String(query || '').trim();
      return snapshot();
    }

    async function loadCatalog(query) {
      const sequence = ++state.requestSequence;
      state.query = String(query || '').trim();
      state.nextCursor = '';
      state.catalogState = 'loading';
      state.catalogError = null;
      emit();
      try {
        const extras = catalogExtras();
        const result = await request(fetchImpl, gamesPath(state.query, extras));
        if (sequence !== state.requestSequence) return snapshot();
        const previousByID = new Map(state.gameViews.map(view => [view.live.id, view.presentation]));
        const games = parseCatalog(result);
        const gameViews = Object.freeze(games.map(game => retainPresentation(game, previousByID.get(game.id))));
        state.games = games;
        state.gameViews = gameViews;
        state.nextCursor = typeof result.next_cursor === 'string' ? result.next_cursor : '';
        state.metadataFallbackCount = metadataFallbackCount(gameViews);
        state.metadataState = presentationEnabled
          ? 'metadata_idle'
          : (state.metadataFallbackCount ? 'metadata_fallback' : 'curated');
        state.catalogState = 'populated';
        state.catalogError = null;
        state.hostState = 'ready';
        reconcileSelection();
        emit();
        return emitHydratedSessionLive(() => sequence === state.requestSequence);
      } catch (error) {
        if (sequence !== state.requestSequence) return snapshot();
        state.catalogState = 'catalog_error';
        state.catalogError = errorSnapshot(error, 'The catalog could not be loaded.');
        state.hostState = 'unavailable';
        return emit();
      }
    }

    async function loadMoreCatalog() {
      if (!state.nextCursor || state.catalogState === 'loading' || state.loadingMore) return snapshot();
      state.loadingMore = true;
      const sequence = ++state.requestSequence;
      try {
        const extras = catalogExtras(state.nextCursor);
        const result = await request(fetchImpl, gamesPath(state.query, extras));
        if (sequence !== state.requestSequence) return snapshot();
        const more = parseCatalog(result);
        const games = Object.freeze(state.games.concat(more));
        const gameViews = Object.freeze(state.gameViews.concat(more.map(game => enrich(game))));
        state.games = games;
        state.gameViews = gameViews;
        state.nextCursor = typeof result.next_cursor === 'string' ? result.next_cursor : '';
        state.metadataFallbackCount = metadataFallbackCount(gameViews);
        emit();
        return emitHydratedSessionLive(() => sequence === state.requestSequence);
      } catch (error) {
        if (sequence !== state.requestSequence) return snapshot();
        return emit();
      } finally {
        state.loadingMore = false;
      }
    }

    async function openHome() {
      state.libraryView = 'home';
      state.collection = '';
      clearHomePlatformFilter();
      state.homeRails = Object.freeze([]);
      return loadHomeRails();
    }

    async function setLibraryNav(collection, platform) {
      const custom = (state.collections || []).some(item => item && item.id === collection);
      if (collection === 'home' && !custom) {
        return openHome();
      }
      const allowed = {
        favorites: true, recents: true, continue: true, unplayed: true, recently_added: true,
      };
      state.libraryView = 'grid';
      state.homeRails = Object.freeze([]);
      state.collection = allowed[collection] || custom ? collection : '';
      state.platformQuery = String(platform || '').trim();
      state.filters = Object.freeze({
        ...state.filters,
        system: state.platformQuery,
      });
      return loadCatalog(state.query);
    }

    async function loadPlatforms() {
      try {
        const payload = await request(fetchImpl, '/api/v1/platforms');
        const list = payload && Array.isArray(payload.platforms) ? payload.platforms : [];
        state.platforms = Object.freeze(list.filter(item => item && typeof item.id === 'string' && item.id.trim()).map(item => Object.freeze({
          id: item.id,
          label: typeof item.label === 'string' && item.label.trim() ? item.label : systemLabel(item.id),
          game_count: Number.isFinite(item.game_count) ? item.game_count : 0,
          online: item.online === true,
          launchable: item.launchable === true,
        })));
      } catch (_) {
        if (!state.platforms.length) state.platforms = Object.freeze([]);
      }
      try {
        const settingsSequence = state.settingsSequence;
        const attract = await request(fetchImpl, '/api/v1/library/attract?limit=1');
        const idle = boundedAttractIdleSeconds(attract && attract.idle_seconds);
        if (settingsSequence === state.settingsSequence && idle > 0) {
          state.attractIdleSeconds = idle;
        }
      } catch (_) {
        /* attract idle stays at the last known value */
      }
      await refreshCollections();
      try {
        const facets = await request(fetchImpl, '/api/v1/library/facets');
        const genres = facets && Array.isArray(facets.genres)
          ? facets.genres.filter(item => typeof item === 'string' && item.trim()).map(item => item.trim())
          : [];
        const years = facets && Array.isArray(facets.years)
          ? facets.years.filter(item => typeof item === 'string' && item.trim()).map(item => item.trim())
          : [];
        state.facets = Object.freeze({ genres: Object.freeze(genres), years: Object.freeze(years) });
      } catch (_) {
        if (!state.facets) state.facets = Object.freeze({ genres: [], years: [] });
      }
      if (state.libraryView === 'home') return loadHomeRails();
      return emit();
    }

    async function toggleFavorite(gameOrID) {
      const id = typeof gameOrID === 'object' ? gameOrID && gameOrID.id : gameOrID;
      const game = (id && state.games.find(item => item.id === id)) || state.selectedLiveGame;
      if (!game) return snapshot();
      const next = game.favorite !== true;
      try {
        await request(fetchImpl, `/api/v1/library/favorites/${encodeURIComponent(game.id)}`, {
          method: next ? 'PUT' : 'DELETE',
        });
        replaceGame(Object.freeze({ ...game, favorite: next }));
        emit();
        if (state.libraryView === 'home') return reconcileHomeRail('favorites');
        return snapshot();
      } catch (error) {
        return emit();
      }
    }

    function applyCollection(item) {
      const parsed = parseCollection(item);
      if (!parsed) return;
      const next = (state.collections || []).filter(existing => existing.id !== parsed.id);
      next.push(parsed);
      next.sort((a, b) => (a.created_at - b.created_at) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
      state.collections = Object.freeze(next);
      state.collectionListSequence += 1;
    }

    function dropCollection(id) {
      state.collections = Object.freeze((state.collections || []).filter(item => item.id !== id));
      state.collectionListSequence += 1;
    }

    async function reloadCollections() {
      const sequence = ++state.collectionListSequence;
      const payload = await request(fetchImpl, '/api/v1/library/collections');
      if (sequence !== state.collectionListSequence) return;
      state.collections = parseCollectionList(payload);
    }

    async function refreshCollections() {
      try {
        await reloadCollections();
      } catch (_) {
        if (!state.collections) state.collections = Object.freeze([]);
      }
    }

    async function createCollection(name) {
      const trimmed = String(name || '').trim();
      const id = uniqueCollectionID(trimmed, (state.collections || []).map(item => item.id));
      try {
        const result = await request(fetchImpl, `/api/v1/library/collections/${encodeURIComponent(id)}?name=${encodeURIComponent(trimmed || id)}`, {
          method: 'PUT',
        });
        applyCollection(result && result.id ? result : { id, name: trimmed || id });
        await refreshCollections();
        return setLibraryNav(id, '');
      } catch (error) {
        emit();
        throw error;
      }
    }

    async function renameCollection(id, name) {
      const collectionID = String(id || '').trim();
      const trimmed = String(name || '').trim();
      if (!collectionID) return snapshot();
      try {
        const result = await request(fetchImpl, `/api/v1/library/collections/${encodeURIComponent(collectionID)}?name=${encodeURIComponent(trimmed || collectionID)}`, {
          method: 'PUT',
        });
        applyCollection(result && result.id ? result : { id: collectionID, name: trimmed || collectionID });
        await refreshCollections();
        return emit();
      } catch (error) {
        emit();
        throw error;
      }
    }

    async function deleteCollection(id) {
      const collectionID = String(id || '').trim();
      if (!collectionID) return snapshot();
      try {
        await request(fetchImpl, `/api/v1/library/collections/${encodeURIComponent(collectionID)}`, {
          method: 'DELETE',
        });
        dropCollection(collectionID);
        await refreshCollections();
        if (state.collection === collectionID) return setLibraryNav('', '');
        return emit();
      } catch (error) {
        return emit();
      }
    }

    async function toggleCollectionMember(collectionID, gameOrID) {
      const id = typeof gameOrID === 'object' ? gameOrID && gameOrID.id : gameOrID;
      const game = (id && state.games.find(item => item.id === id)) || state.selectedLiveGame;
      const collection = String(collectionID || '').trim();
      if (!game || !collection) return snapshot();
      const current = Array.isArray(game.collections) ? game.collections : [];
      const next = !current.includes(collection);
      try {
        await request(fetchImpl, `/api/v1/library/collections/${encodeURIComponent(collection)}/${encodeURIComponent(game.id)}`, {
          method: next ? 'PUT' : 'DELETE',
        });
        const collections = next
          ? Object.freeze(current.concat(collection))
          : Object.freeze(current.filter(item => item !== collection));
        replaceGame(Object.freeze({ ...game, collections }));
        emit();
        if (state.libraryView === 'home') return reconcileHomeRail(collection);
        if (!next && state.collection === collection) return loadCatalog(state.query);
        return snapshot();
      } catch (error) {
        return emit();
      }
    }

    function boundedAttractIdleSeconds(value) {
      const idle = Number(value);
      if (!Number.isFinite(idle) || idle <= 0) return 0;
      return idle > MAX_ATTRACT_IDLE_SECONDS ? MAX_ATTRACT_IDLE_SECONDS : Math.floor(idle);
    }

    function parseLibrarySettings(payload) {
      const idle = boundedAttractIdleSeconds(payload && payload.attract_idle_seconds);
      const regions = payload && Array.isArray(payload.preferred_regions)
        ? payload.preferred_regions.filter(item => typeof item === 'string' && item.trim()).map(item => item.trim().toLowerCase())
        : [];
      return Object.freeze({
        attract_idle_seconds: idle > 0 ? idle : 60,
        preferred_regions: Object.freeze(regions),
      });
    }

    function applyLibrarySettings(parsed, sequence) {
      if (sequence !== state.settingsSequence) return false;
      if (parsed.attract_idle_seconds > 0) state.attractIdleSeconds = parsed.attract_idle_seconds;
      state.librarySettings = parsed;
      return true;
    }

    async function loadSettings() {
      const sequence = state.settingsSequence;
      const payload = await request(fetchImpl, '/api/v1/library/settings');
      const parsed = parseLibrarySettings(payload);
      if (!applyLibrarySettings(parsed, sequence)) return snapshot();
      return emit();
    }

    let settingsWriteChain = Promise.resolve();

    async function writeLibrarySettings(next) {
      const sequence = state.settingsSequence;
      const payload = await request(fetchImpl, '/api/v1/library/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          attract_idle_seconds: next && next.attract_idle_seconds,
          preferred_regions: next && next.preferred_regions,
        }),
      });
      const parsed = parseLibrarySettings(payload);
      if (!applyLibrarySettings(parsed, sequence)) return snapshot();
      if (sequence === state.settingsSequence) state.settingsSequence += 1;
      emit();
      return reloadVisibleCatalog(state.query);
    }

    async function saveSettings(next) {
      const previous = settingsWriteChain;
      let release;
      settingsWriteChain = new Promise(resolve => { release = resolve; });
      try {
        await previous;
        return await writeLibrarySettings(next);
      } finally {
        release();
      }
    }

    async function loadAttract(limit) {
      const size = Number(limit) > 0 ? Number(limit) : 24;
      const settingsSequence = state.settingsSequence;
      const payload = await request(fetchImpl, `/api/v1/library/attract?limit=${encodeURIComponent(String(size))}`);
      const items = payload && Array.isArray(payload.items) ? payload.items : [];
      const idle = boundedAttractIdleSeconds(payload && payload.idle_seconds) || 60;
      if (settingsSequence === state.settingsSequence && idle > 0) state.attractIdleSeconds = idle;
      return Object.freeze({
        idle_seconds: idle,
        items: Object.freeze(items.filter(item => item && typeof item.game_id === 'string').map(item => Object.freeze({
          game_id: item.game_id,
          title: typeof item.title === 'string' ? item.title : '',
          video: optionalArtworkHandle(item.video),
          cover: optionalArtworkHandle(item.cover),
          backdrop: optionalArtworkHandle(item.backdrop),
          marquee: optionalArtworkHandle(item.marquee),
          launchable: item.launchable === true,
        }))),
      });
    }

    async function refreshDetail(gameOrID) {
      const id = typeof gameOrID === 'object' ? gameOrID && gameOrID.id : gameOrID;
      if (!state.selectedLiveGame || state.selectedLiveGame.id !== id) return snapshot();
      const previousIdentity = presentationIdentity(state.selectedLiveGame);
      const selectionRevision = state.selectionRevision;
      const sequence = ++state.detailSequence;
      state.detailState = 'loading';
      state.detailError = null;
      emit();
      try {
        const result = await request(fetchImpl, gameDetailPath(id));
        if (
          sequence !== state.detailSequence
          || selectionRevision !== state.selectionRevision
          || !state.selectedLiveGame
          || state.selectedLiveGame.id !== id
        ) return snapshot();
        const detail = parseDetail(result, id);
        const identityChanged = !samePresentationIdentity(previousIdentity, presentationIdentity(detail));
        if (identityChanged) {
          state.presentationSequence += 1;
          state.selectedPresentation = null;
          state.metadataError = null;
          state.metadataState = presentationEnabled ? 'metadata_loading' : 'metadata_fallback';
        }
        const view = Object.freeze({
          live: detail,
          presentation: state.selectedPresentation || enrich(detail).presentation,
        });
        if (
          sequence !== state.detailSequence
          || selectionRevision !== state.selectionRevision
          || !state.selectedLiveGame
          || state.selectedLiveGame.id !== id
        ) return snapshot();
        state.selectedLiveGame = detail;
        state.selectedGameView = view;
        state.detailState = 'populated';
        state.detailError = null;
        const next = emit();
        if (identityChanged && presentationEnabled) return refreshPresentation(detail);
        return next;
      } catch (error) {
        if (
          sequence !== state.detailSequence
          || selectionRevision !== state.selectionRevision
          || !state.selectedLiveGame
          || state.selectedLiveGame.id !== id
        ) return snapshot();
        state.detailState = 'detail_error';
        state.detailError = errorSnapshot(error, 'The live detail could not be refreshed.');
        return emit();
      }
    }

    async function refreshPresentation(gameOrID) {
      const id = typeof gameOrID === 'object' ? gameOrID && gameOrID.id : gameOrID;
      if (!presentationEnabled || !state.selectedLiveGame || state.selectedLiveGame.id !== id) return snapshot();
      const requestedIdentity = presentationIdentity(state.selectedLiveGame);
      const selectionRevision = state.selectionRevision;
      const sequence = ++state.presentationSequence;
      state.metadataState = 'metadata_loading';
      state.metadataError = null;
      emit();
      try {
        const payload = await request(fetchImpl, presentationPath(id));
        if (
          sequence !== state.presentationSequence
          || selectionRevision !== state.selectionRevision
          || !state.selectedLiveGame
          || state.selectedLiveGame.id !== id
          || !samePresentationIdentity(requestedIdentity, presentationIdentity(state.selectedLiveGame))
        ) return snapshot();
        const presentation = parsePresentation(payload, state.selectedLiveGame);
        presentationByID[id] = presentation;
        state.selectedPresentation = presentation;
        state.selectedGameView = Object.freeze({ live: state.selectedLiveGame, presentation });
        const catalogIndex = state.games.findIndex(game => game.id === id);
        if (catalogIndex >= 0) {
          const nextViews = state.gameViews.slice();
          nextViews[catalogIndex] = state.selectedGameView;
          state.gameViews = nextViews;
        }
        state.metadataState = presentation.metadataState;
        state.metadataError = presentation.isFallback && presentation.metadataState === 'fallback_malformed'
          ? { code: 'MALFORMED_RESPONSE', message: 'The local host returned unavailable presentation metadata.', status: 200 }
          : null;
        return emit();
      } catch (error) {
        if (
          sequence !== state.presentationSequence
          || selectionRevision !== state.selectionRevision
          || !state.selectedLiveGame
          || state.selectedLiveGame.id !== id
          || !samePresentationIdentity(requestedIdentity, presentationIdentity(state.selectedLiveGame))
        ) return snapshot();
        const presentation = fallbackPresentation();
        state.selectedPresentation = presentation;
        state.selectedGameView = Object.freeze({ live: state.selectedLiveGame, presentation });
        state.metadataState = 'metadata_fallback';
        state.metadataError = errorSnapshot(error, 'Presentation metadata is unavailable.');
        return emit();
      }
    }

    async function selectGame(gameOrID) {
      const id = typeof gameOrID === 'object' ? gameOrID && gameOrID.id : gameOrID;
      const fresh = state.games.find(game => game.id === id)
        || (state.selectedLiveGame && Array.isArray(state.selectedLiveGame.variants)
          ? state.selectedLiveGame.variants.find(game => game.id === id)
          : null);
      if (!fresh) return snapshot();
      const index = state.games.findIndex(game => game.id === fresh.id || (fresh.group_key && game.group_key === fresh.group_key));
      resetSelectionState();
      state.selectedLiveGame = fresh;
      state.selectedGameView = index >= 0 ? state.gameViews[index] : Object.freeze({ live: fresh, presentation: enrich(fresh).presentation });
      state.detailState = 'loading';
      emit();
      if (!presentationEnabled) return refreshDetail(id);
      await Promise.all([refreshDetail(id), refreshPresentation(id)]);
      return snapshot();
    }

    function setCatalogFilter(name, value) {
      if (name === 'system') {
        if (state.libraryView === 'home') {
          clearHomePlatformFilter();
          return emit();
        }
        const next = String(value || '').trim();
        state.filters = Object.freeze({ ...state.filters, system: next });
        state.platformQuery = next;
        return reloadVisibleCatalog(state.query);
      }
      if (name === 'region' || name === 'genre' || name === 'year' || name === 'availability') {
        state.filters = Object.freeze({ ...state.filters, [name]: String(value || '').trim() });
        if (state.libraryView === 'home') return setLibraryNav('', '');
        return reloadVisibleCatalog(state.query);
      }
      if (name === 'hide_prerelease' || name === 'hide_hacks') {
        const enabled = value === true || value === '1' || value === 'true';
        state.filters = Object.freeze({ ...state.filters, [name]: enabled });
        if (state.libraryView === 'home') return loadHomeRails();
        return loadCatalog(state.query);
      }
      return snapshot();
    }

    function setCatalogSort(value) {
      if (value === 'recents') return emit();
      state.sort = value === 'year' || value === 'system' || value === 'recently_added' ? value : 'title';
      if (state.libraryView === 'home') return loadHomeRails();
      return loadCatalog(state.query);
    }

    function setCatalogLayout(layout) {
      if (layout !== 'cover' && layout !== 'list') return snapshot();
      if (state.catalogLayout === layout) return snapshot();
      state.catalogLayout = layout;
      return emit();
    }

    return Object.freeze({
      getState: snapshot,
      loadCatalog,
      reloadVisibleCatalog,
      searchVisibleCatalog,
      adoptSearchQuery,
      loadMoreCatalog,
      loadPlatforms,
      loadAttract,
      loadSettings,
      saveSettings,
      loadSession,
      selectGame,
      refreshDetail,
      refreshPresentation,
      launchSelected,
      stopSession,
      observeVisibleCovers,
      setCatalogFilter,
      setCatalogSort,
      setCatalogLayout,
      setLibraryNav,
      openHome,
      toggleFavorite,
      createCollection,
      renameCollection,
      deleteCollection,
      toggleCollectionMember,
    });
  }

  const api = Object.freeze({
    gamesPath,
    gameDetailPath,
    presentationPath,
    artworkPath,
    mediaPath,
    parsePresentation,
    launchRequest,
    sessionRequest,
    stopRequest,
    parseSession,
    sessionViewState,
    launchStatus,
    isSelectedGame,
    detailHeading,
    catalogViewState,
    createAppController,
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
    findLiveGameInState,
    regionLabel,
    catalogDumpRegions,
    systemLabel,
    sourceLabel,
    launchBlockReason,
    collectionIDFromName,
    uniqueCollectionID,
    parseCollection,
    parseCollectionList,
    collectionFetchSort,
    catalogSortOverridden,
    catalogEffectiveSort,
    catalogSortOverrideLabel,
  });
  root.FogCastApp = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;

  if (typeof document === 'undefined') return;

  let state;
  let controller;
  let keyboardPane = 'rail';
  let homeFocus = { rail: 0, card: 0 };
  let settingsReturnPane = 'rail';
  let collectionEditor = null;
  let forceKeyboardRestore = false;
  let settingsGeneration = 0;
  let searchTimer = null;
  let attractIdleHydrated = false;
  let gameActionsMenu = { open: false, gameId: '', pane: 'grid', home: null };

  const nodes = {
    health: document.getElementById('health'),
    search: document.getElementById('game-search'),
    refresh: document.getElementById('refresh-catalog'),
    systemFilter: document.getElementById('filter-system'),
    regionFilter: document.getElementById('filter-region'),
    genreFilter: document.getElementById('filter-genre'),
    yearFilter: document.getElementById('filter-year'),
    sortFilter: document.getElementById('catalog-sort'),
    sortFilterLabel: document.getElementById('catalog-sort-label'),
    hidePrerelease: document.getElementById('filter-hide-prerelease'),
    hideHacks: document.getElementById('filter-hide-hacks'),
    availabilityFilter: document.getElementById('filter-availability'),
    count: document.getElementById('catalog-count'),
    layoutGroup: document.getElementById('catalog-layout'),
    layoutCover: document.getElementById('layout-cover'),
    layoutList: document.getElementById('layout-list'),
    catalog: document.getElementById('catalog'),
    status: document.getElementById('catalog-status'),
    list: document.getElementById('catalog-list'),
    actions: document.getElementById('catalog-actions'),
    detail: document.getElementById('detail'),
    detailContent: document.getElementById('detail-content'),
    launchActions: document.getElementById('launch-actions'),
    launchStatus: document.getElementById('launch-status'),
    sessionPanel: document.getElementById('session-panel'),
    sessionStatus: document.getElementById('session-status'),
    sessionDetails: document.getElementById('session-details'),
    sessionActions: document.getElementById('session-actions'),
    sessionMessage: document.getElementById('session-message'),
    navHome: document.getElementById('nav-home'),
    navAll: document.getElementById('nav-all'),
    navFavorites: document.getElementById('nav-favorites'),
    navRecents: document.getElementById('nav-recents'),
    navContinue: document.getElementById('nav-continue'),
    navUnplayed: document.getElementById('nav-unplayed'),
    navRecentlyAdded: document.getElementById('nav-recently-added'),
    collectionList: document.getElementById('collection-list'),
    createCollection: document.getElementById('create-collection'),
    collectionNameLabel: document.getElementById('collection-name-label'),
    collectionName: document.getElementById('collection-name'),
    saveCollection: document.getElementById('save-collection'),
    renameCollection: document.getElementById('rename-collection'),
    deleteCollection: document.getElementById('delete-collection'),
    platformList: document.getElementById('platform-list'),
    attract: document.getElementById('attract'),
    attractTitle: document.getElementById('attract-title'),
    attractStage: document.getElementById('attract-stage'),
    openSettings: document.getElementById('open-settings'),
    settings: document.getElementById('settings'),
    settingsAttractIdle: document.getElementById('settings-attract-idle'),
    settingsPreferredRegions: document.getElementById('settings-preferred-regions'),
    settingsHostHealth: document.getElementById('settings-host-health'),
    settingsMessage: document.getElementById('settings-message'),
    saveSettings: document.getElementById('save-settings'),
    closeSettings: document.getElementById('close-settings'),
    gameActions: document.getElementById('game-actions-menu'),
  };

  function element(tag, className, text) {
    const result = document.createElement(tag);
    if (className) result.className = className;
    if (text !== undefined) result.textContent = String(text);
    return result;
  }

  function gameActionsMenuNode() {
    if (nodes.gameActions) return nodes.gameActions;
    const existing = typeof document.getElementById === 'function'
      ? document.getElementById('game-actions-menu')
      : null;
    if (existing) {
      nodes.gameActions = existing;
      return existing;
    }
    const menu = element('div', 'game-actions-menu');
    menu.id = 'game-actions-menu';
    menu.hidden = true;
    menu.setAttribute('role', 'menu');
    menu.setAttribute('aria-label', 'Game actions');
    nodes.gameActions = menu;
    if (document.body && typeof document.body.appendChild === 'function') {
      document.body.appendChild(menu);
    }
    return menu;
  }

  function gameActionsMenuIsOpen() {
    const menu = nodes.gameActions || (typeof document.getElementById === 'function'
      ? document.getElementById('game-actions-menu')
      : null);
    return Boolean(gameActionsMenu.open && menu && menu.hidden === false);
  }

  function isGameActionsMenuTarget(target) {
    let node = target;
    while (node) {
      if (node === nodes.gameActions
        || node.id === 'game-actions-menu'
        || String(node.className || '').includes('game-actions-menu')
        || String(node.className || '').includes('game-actions-item')) {
        return true;
      }
      node = node.parentNode;
    }
    return false;
  }

  function closestGameCard(node) {
    let current = node;
    while (current) {
      if (String(current.className || '').includes('game-card')
        && typeof current.getAttribute === 'function'
        && current.getAttribute('data-game-id')) {
        return current;
      }
      current = current.parentNode;
    }
    return null;
  }

  function paneForGameCard(card) {
    let node = card;
    while (node) {
      if (String(node.className || '').includes('home-rail-track')
        || (typeof node.getAttribute === 'function' && node.getAttribute('data-home-track'))) {
        return 'home';
      }
      node = node.parentNode;
    }
    return catalogPane();
  }

  function gameFromCard(card) {
    const id = card && typeof card.getAttribute === 'function' ? card.getAttribute('data-game-id') : '';
    if (!id) return null;
    if (state.selectedLiveGame && state.selectedLiveGame.id === id) return state.selectedLiveGame;
    return (state.games || []).find(item => item && item.id === id) || null;
  }

  function gameActionsMenuItems() {
    const menu = gameActionsMenuNode();
    const children = menu && menu.children ? Array.from(menu.children) : [];
    return children.filter(child => String(child.className || '').includes('game-actions-item'));
  }

  function homeCoordForCard(card) {
    const tracks = homeRailTracks();
    for (let rail = 0; rail < tracks.length; rail += 1) {
      const cards = cardsInTrack(tracks[rail]);
      const index = cards.indexOf(card);
      if (index >= 0) return { rail, card: index };
    }
    return null;
  }

  function originatingGameCard(origin) {
    const source = origin || gameActionsMenu;
    if (source.pane === 'home' && source.home) {
      homeFocus = { rail: source.home.rail, card: source.home.card };
      return selectedHomeCardNode();
    }
    const selected = selectedCardNode();
    if (selected && typeof selected.getAttribute === 'function'
      && selected.getAttribute('data-game-id') === source.gameId) {
      return selected;
    }
    return gameCardNodes().find(node => (
      typeof node.getAttribute === 'function' && node.getAttribute('data-game-id') === source.gameId
    )) || null;
  }

  function closeGameActionsMenu(options) {
    const restore = Boolean(options && options.restoreFocus);
    const origin = {
      gameId: gameActionsMenu.gameId,
      pane: gameActionsMenu.pane,
      home: gameActionsMenu.home,
    };
    const menu = nodes.gameActions || (typeof document.getElementById === 'function'
      ? document.getElementById('game-actions-menu')
      : null);
    if (menu) {
      menu.hidden = true;
      if (typeof menu.replaceChildren === 'function') menu.replaceChildren();
    }
    gameActionsMenu = { open: false, gameId: '', pane: origin.pane || catalogPane(), home: null };
    if (!restore || !origin.gameId) return;
    if (origin.home) homeFocus = { rail: origin.home.rail, card: origin.home.card };
    keyboardPane = origin.pane === 'home' || origin.pane === 'grid' ? origin.pane : catalogPane();
    const card = originatingGameCard(origin);
    if (card) focusWithoutScroll(card);
    writePaneAttribute();
  }

  function viewportSize() {
    const view = typeof window !== 'undefined' ? window : root;
    const width = Number(view && view.innerWidth);
    const height = Number(view && view.innerHeight);
    return {
      width: Number.isFinite(width) && width > 0 ? width : 0,
      height: Number.isFinite(height) && height > 0 ? height : 0,
    };
  }

  function measuredBox(node) {
    if (node && typeof node.getBoundingClientRect === 'function') {
      try {
        const rect = node.getBoundingClientRect();
        return {
          width: Number(rect.width) || 0,
          height: Number(rect.height) || 0,
        };
      } catch (_) {
        /* fall through */
      }
    }
    return {
      width: Number(node && node.offsetWidth) || 0,
      height: Number(node && node.offsetHeight) || 0,
    };
  }

  function clampGameActionsMenu(menu) {
    if (!menu || !menu.style) return;
    const view = viewportSize();
    const box = measuredBox(menu);
    if (!view.width || !view.height || !box.width || !box.height) return;
    const margin = 8;
    let left = Number.parseFloat(menu.style.left);
    let top = Number.parseFloat(menu.style.top);
    if (!Number.isFinite(left)) left = margin;
    if (!Number.isFinite(top)) top = margin;
    const maxLeft = Math.max(margin, view.width - box.width - margin);
    const maxTop = Math.max(margin, view.height - box.height - margin);
    menu.style.left = `${Math.min(Math.max(margin, left), maxLeft)}px`;
    menu.style.top = `${Math.min(Math.max(margin, top), maxTop)}px`;
  }

  function positionGameActionsMenu(menu, card, event) {
    let left = event && Number.isFinite(event.clientX) ? event.clientX : NaN;
    let top = event && Number.isFinite(event.clientY) ? event.clientY : NaN;
    if ((!Number.isFinite(left) || !Number.isFinite(top))
      && card && typeof card.getBoundingClientRect === 'function') {
      try {
        const rect = card.getBoundingClientRect();
        left = Number(rect.left) || 0;
        top = Number(rect.bottom) || 0;
      } catch (_) {
        left = 0;
        top = 0;
      }
    }
    if (!Number.isFinite(left)) left = 0;
    if (!Number.isFinite(top)) top = 0;
    if (!menu.style) menu.style = {};
    menu.style.position = 'fixed';
    menu.style.left = `${left}px`;
    menu.style.top = `${top}px`;
    clampGameActionsMenu(menu);
  }

  function populateGameActionsMenu(menu, game) {
    menu.replaceChildren();
    const favorite = element('button', 'game-actions-item', game.favorite === true ? 'Unfavorite' : 'Favorite');
    favorite.type = 'button';
    favorite.id = 'game-action-favorite';
    favorite.setAttribute('role', 'menuitem');
    favorite.addEventListener('click', () => {
      closeGameActionsMenu({ restoreFocus: true });
      return controller.toggleFavorite(game.id);
    });
    menu.appendChild(favorite);
    (state.collections || []).forEach(collection => {
      if (!collection || !collection.id) return;
      const member = Array.isArray(game.collections) && game.collections.includes(collection.id);
      const item = element('button', 'game-actions-item', member
        ? `Remove from ${collection.name}`
        : `Add to ${collection.name}`);
      item.type = 'button';
      item.id = `game-action-collection-${collection.id}`;
      item.setAttribute('role', 'menuitem');
      item.setAttribute('data-collection', collection.id);
      item.addEventListener('click', () => {
        closeGameActionsMenu({ restoreFocus: true });
        return controller.toggleCollectionMember(collection.id, game.id);
      });
      menu.appendChild(item);
    });
  }

  function openGameActionsMenu(card, event) {
    if (attractActive || settingsIsOpen()) return false;
    const target = closestGameCard(card) || card;
    const game = gameFromCard(target);
    if (!game || !target) return false;
    const menu = gameActionsMenuNode();
    const pane = paneForGameCard(target);
    const home = homeCoordForCard(target);
    if (home) homeFocus = { rail: home.rail, card: home.card };
    if (pane === 'home' || pane === 'grid') keyboardPane = pane;
    gameActionsMenu = { open: false, gameId: game.id, pane, home };
    const alreadySelected = Boolean(state.selectedLiveGame && state.selectedLiveGame.id === game.id);
    if (!alreadySelected) {
      void selectGame(game.id);
    } else if (home) {
      renderCatalog();
    }
    gameActionsMenu.open = true;
    populateGameActionsMenu(menu, game);
    positionGameActionsMenu(menu, originatingGameCard() || target, event);
    menu.hidden = false;
    clampGameActionsMenu(menu);
    writePaneAttribute();
    const first = gameActionsMenuItems()[0];
    if (first) focusGameActionsItem(first);
    return true;
  }

  function revealGameActionsItem(item) {
    if (!item) return;
    const menu = gameActionsMenuNode();
    const viewHeight = Number(menu && menu.clientHeight);
    const top = Number(item.offsetTop);
    if (menu && viewHeight > 0 && Number.isFinite(top)) {
      const height = Number(item.offsetHeight) || 0;
      const viewTop = Number(menu.scrollTop) || 0;
      if (top < viewTop) {
        menu.scrollTop = Math.max(0, top);
        return;
      }
      if (top + height > viewTop + viewHeight) {
        menu.scrollTop = Math.max(0, top + height - viewHeight);
      }
      return;
    }
    if (typeof item.scrollIntoView !== 'function') return;
    try {
      item.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    } catch (_) {
      try { item.scrollIntoView(); } catch (__) { /* ignore */ }
    }
  }

  function focusGameActionsItem(item) {
    if (!item) return;
    focusWithoutScroll(item);
    revealGameActionsItem(item);
  }

  function moveGameActionsMenu(delta, edge) {
    const items = gameActionsMenuItems();
    if (!items.length) return;
    if (edge === 'start') {
      focusGameActionsItem(items[0]);
      return;
    }
    if (edge === 'end') {
      focusGameActionsItem(items[items.length - 1]);
      return;
    }
    const active = typeof document !== 'undefined' ? document.activeElement : null;
    const current = items.indexOf(active);
    const next = current < 0 ? 0 : Math.max(0, Math.min(items.length - 1, current + delta));
    focusGameActionsItem(items[next]);
  }

  function isGameActionsMenuOpenKey(event) {
    if (!event) return false;
    if (event.key === 'ContextMenu') return true;
    return event.key === 'F10' && event.shiftKey === true;
  }

  function openGameActionsMenuFromEvent(event) {
    const fromTarget = closestGameCard(event && event.target);
    if (fromTarget) return openGameActionsMenu(fromTarget, event);
    if (keyboardPane !== 'grid' && keyboardPane !== 'home') return false;
    const selected = selectedCardNode();
    return selected ? openGameActionsMenu(selected, event) : false;
  }


  function setHealth(text, className) {
    nodes.health.textContent = text;
    nodes.health.className = className;
  }

  function retryButton(label, action) {
    const button = element('button', 'button secondary', label);
    button.type = 'button';
    button.addEventListener('click', action);
    return button;
  }

  function updateLaunchStatus() {
    const presentation = launchStatus(state.launchState, state.launchMessage);
    nodes.launchStatus.textContent = presentation.text;
    nodes.launchStatus.setAttribute('role', presentation.role);
  }

  let refreshSessionButton;
  let stopSessionButton;
  let sessionActionReason;

  function sessionStatusText() {
    if (state.activeMutation === 'launch') return 'Launching session…';
    if (state.activeMutation === 'stop') return 'Stopping session…';
    switch (state.sessionPhase) {
      case 'loading': return 'Checking session status…';
      case 'active': return 'Active session';
      case 'stopping': return 'Stopping session…';
      case 'stopped': return 'Session stopped.';
      case 'unavailable': return 'Session status unavailable. Retry to check the local host.';
      case 'malformed': return 'The local host returned an invalid session response. Retry.';
      case 'error': return state.session && state.session.state === 'failed'
        ? 'The local host reports a failed session.'
        : 'The session operation could not be confirmed.';
      default: return 'No active session.';
    }
  }

  function sessionFact(label, value) {
    const fact = element('div', 'session-detail');
    fact.appendChild(element('strong', '', label));
    fact.appendChild(element('span', 'session-id', value));
    return fact;
  }

  function renderSessionDetails() {
    nodes.sessionDetails.replaceChildren();
    const session = state.session;
    if (!session) {
      nodes.sessionDetails.appendChild(sessionFact('State', state.sessionPhase === 'loading' ? 'checking' : state.sessionPhase));
      return;
    }
    nodes.sessionDetails.appendChild(sessionFact('State', session.state));
    if (state.sessionAuthority !== 'authoritative') {
      const authorityText = state.sessionAuthority === 'last-known'
        ? state.sessionPhase === 'malformed'
          ? 'Last-known session details; current status response was malformed.'
          : 'Last-known session details; current status is unavailable.'
        : 'Last-known session details; current status is still being checked.';
      nodes.sessionDetails.appendChild(sessionFact('Authority', authorityText));
    }
    if (session.state !== 'active') return;
    if (session.game_id !== undefined) {
      nodes.sessionDetails.appendChild(sessionFact('Game ID', session.game_id));
      nodes.sessionDetails.appendChild(sessionFact(
        'Title',
        state.sessionGameTitle || 'Title unavailable in the current live catalog.',
      ));
    }
    if (session.system !== undefined) nodes.sessionDetails.appendChild(sessionFact('System', session.system));
    if (session.execution !== undefined) nodes.sessionDetails.appendChild(sessionFact('Execution', session.execution));
    if (session.media !== undefined) nodes.sessionDetails.appendChild(sessionFact('Media', session.media));
    if (session.progress) {
      nodes.sessionDetails.appendChild(sessionFact('Progress stage', boundedMessage(session.progress.stage, '—', 120)));
      nodes.sessionDetails.appendChild(sessionFact('Progress message', boundedMessage(session.progress.message, '—', 240)));
    }
    if (session.input) {
      nodes.sessionDetails.appendChild(sessionFact('Input state', session.input.state));
      nodes.sessionDetails.appendChild(sessionFact('Input readiness', session.input.ready ? 'Ready' : 'Not ready'));
    }
  }

  function ensureSessionActions() {
    if (!refreshSessionButton) {
      refreshSessionButton = element('button', 'button secondary', 'Refresh session');
      refreshSessionButton.id = 'refresh-session';
      refreshSessionButton.type = 'button';
      refreshSessionButton.addEventListener('click', loadSession);
      nodes.sessionActions.appendChild(refreshSessionButton);
    }
    if (!stopSessionButton) {
      stopSessionButton = element('button', 'button', 'Stop session');
      stopSessionButton.id = 'stop-session';
      stopSessionButton.type = 'button';
      stopSessionButton.addEventListener('click', stopSession);
      nodes.sessionActions.appendChild(stopSessionButton);
    }
    if (!sessionActionReason) {
      sessionActionReason = element('p', 'launch-reason');
      sessionActionReason.id = 'session-action-reason';
      nodes.sessionActions.appendChild(sessionActionReason);
    }
  }

  function sessionNeedsAttention() {
    if (state.activeMutation) return true;
    if (state.sessionMessage) return true;
    if (state.launchState === 'launching' || state.launchState === 'launch_error') return true;
    if (state.session && state.session.state === 'active') return true;
    return ['active', 'stopping', 'error', 'unavailable', 'malformed'].includes(state.sessionPhase);
  }

  function renderSession() {
    ensureSessionActions();
    const busy = Boolean(state.activeMutation) || state.sessionPhase === 'loading';
    nodes.sessionPanel.setAttribute('aria-busy', String(busy));
    nodes.sessionPanel.className = sessionNeedsAttention() ? 'session-panel' : 'session-panel session-quiet';
    nodes.sessionStatus.textContent = sessionStatusText();
    nodes.sessionMessage.textContent = state.sessionMessage || '';
    renderSessionDetails();
    const conflictReason = state.activeMutation ? 'A session transition is already in progress.' : '';
    const hasActiveSession = Boolean(
      state.sessionAuthority === 'authoritative'
      && state.session
      && state.session.state === 'active',
    );
    const stopReason = conflictReason || (hasActiveSession
      ? ''
      : state.sessionAuthority === 'last-known'
        ? 'Current session status is not authoritative; retry before stopping.'
        : 'No current authoritative active session is available to stop.');
    refreshSessionButton.disabled = Boolean(state.activeMutation);
    stopSessionButton.disabled = Boolean(stopReason);
    stopSessionButton.hidden = !hasActiveSession && !state.activeMutation;
    sessionActionReason.textContent = conflictReason || (!hasActiveSession ? stopReason : '');
    if (conflictReason || !hasActiveSession) {
      refreshSessionButton.setAttribute('aria-describedby', 'session-action-reason');
      stopSessionButton.setAttribute('aria-describedby', 'session-action-reason');
    } else {
      refreshSessionButton.removeAttribute?.('aria-describedby');
      stopSessionButton.removeAttribute?.('aria-describedby');
    }
  }

  function isHomeView() {
    return state.libraryView === 'home';
  }

  function isListLayout() {
    return !isHomeView() && state.catalogLayout === 'list';
  }

  function catalogPane() {
    return isHomeView() ? 'home' : 'grid';
  }

  function catalogColumns() {
    return isListLayout() ? 1 : wallColumns();
  }

  function syncCatalogLayoutControls() {
    const home = isHomeView();
    const list = state.catalogLayout === 'list';
    if (nodes.layoutGroup) nodes.layoutGroup.hidden = home;
    if (nodes.layoutCover) nodes.layoutCover.setAttribute('aria-pressed', String(!list));
    if (nodes.layoutList) nodes.layoutList.setAttribute('aria-pressed', String(list));
  }

  function navSelected(kind, platform) {
    if (kind === 'home') return isHomeView();
    if (isHomeView()) return false;
    if (kind === 'favorites') return state.collection === 'favorites' && !state.platformQuery;
    if (kind === 'recents') return state.collection === 'recents' && !state.platformQuery;
    if (kind === 'continue') return state.collection === 'continue' && !state.platformQuery;
    if (kind === 'unplayed') return state.collection === 'unplayed' && !state.platformQuery;
    if (kind === 'recently_added') return state.collection === 'recently_added' && !state.platformQuery;
    if (kind === 'collection') return Boolean(platform) && state.collection === platform && !state.platformQuery;
    if (kind === 'platform') return Boolean(platform) && state.platformQuery === platform;
    return !state.collection && !state.platformQuery;
  }

  function setRovingTab(node, selected) {
    if (!node) return;
    node.tabIndex = selected ? 0 : -1;
  }

  function renderLibraryNav() {
    if (nodes.navHome) nodes.navHome.className = 'nav-item' + (navSelected('home') ? ' selected' : '');
    if (nodes.navAll) nodes.navAll.className = 'nav-item' + (navSelected('all') ? ' selected' : '');
    if (nodes.navContinue) nodes.navContinue.className = 'nav-item' + (navSelected('continue') ? ' selected' : '');
    if (nodes.navFavorites) nodes.navFavorites.className = 'nav-item' + (navSelected('favorites') ? ' selected' : '');
    if (nodes.navRecents) nodes.navRecents.className = 'nav-item' + (navSelected('recents') ? ' selected' : '');
    if (nodes.navUnplayed) nodes.navUnplayed.className = 'nav-item' + (navSelected('unplayed') ? ' selected' : '');
    if (nodes.navRecentlyAdded) nodes.navRecentlyAdded.className = 'nav-item' + (navSelected('recently_added') ? ' selected' : '');
    setRovingTab(nodes.navHome, navSelected('home'));
    setRovingTab(nodes.navAll, navSelected('all'));
    setRovingTab(nodes.navContinue, navSelected('continue'));
    setRovingTab(nodes.navFavorites, navSelected('favorites'));
    setRovingTab(nodes.navRecents, navSelected('recents'));
    setRovingTab(nodes.navUnplayed, navSelected('unplayed'));
    setRovingTab(nodes.navRecentlyAdded, navSelected('recently_added'));
    if (nodes.collectionList) {
      nodes.collectionList.replaceChildren();
      (state.collections || []).forEach(collection => {
        const selected = navSelected('collection', collection.id);
        const button = element('button', 'nav-item' + (selected ? ' selected' : ''));
        button.type = 'button';
        button.id = `nav-collection-${collection.id}`;
        button.setAttribute('data-collection', collection.id);
        setRovingTab(button, selected);
        button.appendChild(element('span', '', collection.name || collection.id));
        button.addEventListener('click', () => {
          keyboardPane = 'rail';
          navigateLibrary(() => controller.setLibraryNav(collection.id, ''), true);
        });
        nodes.collectionList.appendChild(button);
      });
    }
    const customSelected = (state.collections || []).some(item => navSelected('collection', item.id));
    const editing = collectionEditor !== null;
    if (nodes.createCollection) nodes.createCollection.hidden = editing;
    if (nodes.collectionNameLabel) nodes.collectionNameLabel.hidden = !editing;
    if (nodes.saveCollection) nodes.saveCollection.hidden = !editing;
    if (nodes.renameCollection) nodes.renameCollection.hidden = !customSelected || editing;
    if (nodes.deleteCollection) nodes.deleteCollection.hidden = !customSelected || editing;
    if (!nodes.platformList) return;
    nodes.platformList.replaceChildren();
    (state.platforms || []).forEach(platform => {
      const selected = navSelected('platform', platform.id);
      const button = element('button', 'nav-item' + (selected ? ' selected' : ''));
      button.type = 'button';
      button.setAttribute('data-platform', platform.id);
      setRovingTab(button, selected);
      button.appendChild(element('span', '', platform.label || systemLabel(platform.id)));
      button.appendChild(element('span', 'platform-count', String(platform.game_count || 0)));
      button.addEventListener('click', () => {
        keyboardPane = 'rail';
        navigateLibrary(() => controller.setLibraryNav('', platform.id), true);
      });
      nodes.platformList.appendChild(button);
    });
  }

  function beginCollectionEditor(mode) {
    const selected = (state.collections || []).find(item => item.id === state.collection);
    collectionEditor = {
      mode,
      collectionID: mode === 'rename' && selected ? selected.id : '',
      busy: false,
    };
    if (nodes.collectionName) {
      nodes.collectionName.value = mode === 'rename' && selected ? selected.name : '';
    }
    if (nodes.saveCollection) nodes.saveCollection.disabled = false;
    renderLibraryNav();
    if (nodes.collectionName) focusWithoutScroll(nodes.collectionName);
  }

  function cancelCollectionEditor() {
    collectionEditor = null;
    if (nodes.collectionName) nodes.collectionName.value = '';
    if (nodes.saveCollection) nodes.saveCollection.disabled = false;
    renderLibraryNav();
  }

  async function saveCollectionEditor() {
    const editor = collectionEditor;
    if (!editor || editor.busy) return;
    const name = nodes.collectionName ? nodes.collectionName.value : '';
    editor.busy = true;
    if (nodes.saveCollection) nodes.saveCollection.disabled = true;
    try {
      if (editor.mode === 'rename') {
        if (!editor.collectionID) return;
        await controller.renameCollection(editor.collectionID, name);
      } else {
        await controller.createCollection(name);
      }
      if (collectionEditor !== editor) return;
      collectionEditor = null;
      if (nodes.collectionName) nodes.collectionName.value = '';
    } catch (_) {
      /* keep the same editor so retry stays on the original create/rename */
    } finally {
      if (collectionEditor === editor) {
        editor.busy = false;
        if (nodes.saveCollection) nodes.saveCollection.disabled = false;
      }
      renderLibraryNav();
    }
  }

  let wallObserver = null;
  let wallStartObserver = null;
  let wallStart = 0;
  let cardMarksObservers = [];

  function readStylePx(styles, names, fallback) {
    if (!styles) return fallback;
    const keys = Array.isArray(names) ? names : [names];
    for (const name of keys) {
      const raw = (String(name).startsWith('--') || String(name).includes('-'))
        && typeof styles.getPropertyValue === 'function'
        ? styles.getPropertyValue(name)
        : styles[name];
      const value = parseFloat(raw);
      if (Number.isFinite(value)) return value;
    }
    return fallback;
  }

  function wallMetrics() {
    const list = nodes.list;
    const width = Number(list && list.clientWidth) || 210;
    const styles = typeof root.getComputedStyle === 'function' && list
      ? root.getComputedStyle(list)
      : null;
    const padding = readStylePx(styles, ['padding-left', 'paddingLeft'], 0)
      + readStylePx(styles, ['padding-right', 'paddingRight'], 0);
    const gap = readStylePx(styles, ['column-gap', 'columnGap', 'gap'], 14);
    const minTrack = readStylePx(styles, ['--wall-min-track'], 210);
    const inner = Math.max(minTrack, width - padding);
    const cols = Math.max(1, Math.floor((inner + gap) / (minTrack + gap)));
    const cardWidth = Math.max(1, (inner - gap * Math.max(0, cols - 1)) / cols);
    return { cols, cardWidth, gap };
  }

  function wallColumns() {
    return wallMetrics().cols;
  }

  function listRowStride() {
    const list = nodes.list;
    const styles = typeof root.getComputedStyle === 'function' && list
      ? root.getComputedStyle(list)
      : null;
    return Math.max(1, readStylePx(styles, ['--list-row-stride'], 78));
  }

  function disconnectCardMarksObservers() {
    cardMarksObservers.forEach(observer => {
      if (observer && typeof observer.disconnect === 'function') observer.disconnect();
    });
    cardMarksObservers = [];
  }

  function disconnectWallObservers() {
    if (wallObserver) {
      wallObserver.disconnect();
      wallObserver = null;
    }
    if (wallStartObserver) {
      wallStartObserver.disconnect();
      wallStartObserver = null;
    }
    disconnectCardMarksObservers();
  }

  function appendWallSpacer(count, edge) {
    if (count <= 0) return null;
    const spacer = element('div', 'wall-spacer');
    spacer.setAttribute('data-wall-spacer', edge);
    if (isListLayout()) {
      spacer.style.height = `${count * listRowStride()}px`;
    } else {
      const metrics = wallMetrics();
      const rows = Math.ceil(count / metrics.cols);
      spacer.style.height = `${rows * Math.round(metrics.cardWidth * 1.5 + metrics.gap)}px`;
    }
    nodes.list.appendChild(spacer);
    return spacer;
  }

  function observeWallSentinel(sentinel, loadedCount) {
    const Observer = root.IntersectionObserver;
    if (typeof Observer !== 'function') return;
    if (wallObserver) wallObserver.disconnect();
    wallObserver = new Observer(entries => {
      if (!entries.some(entry => entry.isIntersecting)) return;
      if (state.nextCursor) {
        controller.loadMoreCatalog();
        return;
      }
      if (loadedCount > wallStart + WALL_WINDOW) {
        wallStart = Math.min(loadedCount - WALL_WINDOW, wallStart + Math.floor(WALL_WINDOW / 2));
        renderCatalog();
      }
    }, { root: null, rootMargin: '240px', threshold: 0.01 });
    wallObserver.observe(sentinel);
  }

  function observeWallShift(sentinel, delta) {
    const Observer = root.IntersectionObserver;
    if (typeof Observer !== 'function') return;
    if (wallStartObserver) wallStartObserver.disconnect();
    wallStartObserver = new Observer(entries => {
      if (!entries.some(entry => entry.isIntersecting)) return;
      const next = Math.max(0, wallStart + delta);
      if (next === wallStart) return;
      wallStart = next;
      renderCatalog();
    }, { root: null, rootMargin: '80px', threshold: 0.01 });
    wallStartObserver.observe(sentinel);
  }

  function renderHomeRails() {
    const view = catalogViewState(state);
    nodes.catalog.setAttribute('aria-busy', view === 'loading' ? 'true' : 'false');
    syncCatalogLayoutControls();
    if (nodes.list) nodes.list.className = 'home-rails';
    disconnectCardMarksObservers();
    nodes.list.replaceChildren();
    nodes.actions.replaceChildren();
    nodes.status.textContent = view;
    wallStart = 0;
    disconnectWallObservers();
    syncCatalogFilters();
    if (view === 'loading') {
      nodes.list.appendChild(element('p', 'status-message', 'Loading games…'));
      return;
    }
    if (view === 'catalog_error') {
      nodes.list.appendChild(element('p', 'status-message error', 'The catalog could not be loaded.'));
      nodes.actions.appendChild(retryButton('Retry catalog', loadCatalog));
      return;
    }
    if (view === 'empty' || view === 'no_matches') {
      nodes.list.appendChild(element('p', 'status-message', 'Home has no Continue, Favorites, Recent, or collection titles yet.'));
      nodes.actions.appendChild(retryButton('Refresh catalog', loadCatalog));
      return;
    }
    nodes.status.textContent = state.metadataFallbackCount ? 'populated metadata_fallback' : 'populated';
    const rails = state.homeRails || [];
    if (nodes.count) {
      const total = rails.reduce((sum, rail) => sum + ((rail.gameViews && rail.gameViews.length) || 0), 0);
      nodes.count.textContent = formatCatalogCount(total, total);
    }
    rails.forEach((rail, railIndex) => {
      const section = element('section', 'home-rail');
      section.setAttribute('data-home-rail', rail.id);
      const header = element('div', 'home-rail-header');
      header.appendChild(element('h2', 'home-rail-title', rail.name));
      const seeAll = element('button', 'home-rail-see-all', 'See all');
      seeAll.type = 'button';
      seeAll.setAttribute('data-see-all', rail.id);
      seeAll.addEventListener('click', () => {
        keyboardPane = 'rail';
        return navigateLibrary(() => controller.setLibraryNav(rail.id, ''), true);
      });
      header.appendChild(seeAll);
      section.appendChild(header);
      const track = element('div', 'home-rail-track');
      track.setAttribute('data-home-track', rail.id);
      (rail.gameViews || []).forEach((item, cardIndex) => {
        renderCard(item, item.live, track, 'home', { rail: railIndex, card: cardIndex });
      });
      section.appendChild(track);
      nodes.list.appendChild(section);
    });
  }

  function renderCatalog() {
    if (isHomeView()) {
      renderHomeRails();
      return;
    }
    const view = catalogViewState(state);
    nodes.catalog.setAttribute('aria-busy', view === 'loading' ? 'true' : 'false');
    syncCatalogLayoutControls();
    if (nodes.list) nodes.list.className = isListLayout() ? 'game-list' : 'game-grid';
    disconnectCardMarksObservers();
    nodes.list.replaceChildren();
    nodes.actions.replaceChildren();
    nodes.status.textContent = view;
    syncCatalogFilters();
    if (view === 'loading') {
      wallStart = 0;
      disconnectWallObservers();
      nodes.list.appendChild(element('p', 'status-message', 'Loading games…'));
      return;
    }
    if (view === 'catalog_error') {
      nodes.list.appendChild(element('p', 'status-message error', 'The catalog could not be loaded.'));
      nodes.actions.appendChild(retryButton('Retry catalog', loadCatalog));
      return;
    }
    if (view === 'empty' || view === 'no_matches') {
      nodes.list.appendChild(element('p', 'status-message', state.query ? 'No matching games.' : (state.collection ? 'This collection is empty.' : 'The library is empty.')));
      nodes.actions.appendChild(retryButton('Refresh catalog', loadCatalog));
      return;
    }
    nodes.status.textContent = state.metadataFallbackCount ? 'populated metadata_fallback' : 'populated';
    const visible = state.gameViews;
    if (visible.length === 0 && state.nextCursor) {
      nodes.list.appendChild(element('p', 'status-message', 'Looking for matching games…'));
      const sentinel = element('div', 'wall-sentinel');
      sentinel.setAttribute('data-wall-sentinel', 'true');
      nodes.list.appendChild(sentinel);
      observeWallSentinel(sentinel, 0);
      if (typeof root.IntersectionObserver !== 'function') {
        controller.loadMoreCatalog();
      }
      return;
    }
    if (nodes.count) {
      const extra = state.nextCursor ? '+' : '';
      nodes.count.textContent = extra
        ? `${formatCatalogCount(visible.length, state.games.length)} and more`
        : formatCatalogCount(visible.length, state.games.length);
    }
    const selectedIndex = visible.findIndex(item => isSelectedGame(state.selectedLiveGame, item.live));
    if (visible.length <= WALL_WINDOW) {
      wallStart = 0;
    } else if (selectedIndex >= 0 && (selectedIndex < wallStart || selectedIndex >= wallStart + WALL_WINDOW)) {
      wallStart = Math.max(0, Math.min(selectedIndex - Math.floor(WALL_WINDOW / 2), visible.length - WALL_WINDOW));
    } else {
      wallStart = Math.max(0, Math.min(wallStart, visible.length - WALL_WINDOW));
    }
    const remaining = Math.max(0, visible.length - wallStart - WALL_WINDOW);
    if (wallStart > 0) {
      const startSentinel = appendWallSpacer(wallStart, 'start');
      if (startSentinel) observeWallShift(startSentinel, -Math.floor(WALL_WINDOW / 2));
    }
    visible.slice(wallStart, wallStart + WALL_WINDOW).forEach(item => renderCard(item, item.live));
    if (remaining > 0 || state.nextCursor) {
      const sentinel = appendWallSpacer(remaining, 'end') || element('div', 'wall-sentinel');
      if (!sentinel.parentNode) {
        sentinel.setAttribute('data-wall-sentinel', 'true');
        nodes.list.appendChild(sentinel);
      } else {
        sentinel.setAttribute('data-wall-sentinel', 'true');
      }
      observeWallSentinel(sentinel, visible.length);
    }
  }

  function catalogSortRecentsOption() {
    const select = nodes.sortFilter;
    if (!select) return null;
    const kids = select.options || select.children || [];
    for (let index = 0; index < kids.length; index += 1) {
      if (kids[index] && kids[index].value === 'recents') return kids[index];
    }
    return null;
  }

  function syncCatalogSortRecentsOption(show) {
    const option = catalogSortRecentsOption();
    if (!option) return;
    option.hidden = !show;
    option.disabled = !show;
  }

  function syncCatalogSortControl() {
    if (!nodes.sortFilter) return;
    const overridden = catalogSortOverridden(state);
    const effective = catalogEffectiveSort(state);
    syncCatalogSortRecentsOption(effective === 'recents');
    nodes.sortFilter.value = effective;
    nodes.sortFilter.disabled = overridden;
    if (overridden) {
      nodes.sortFilter.setAttribute('data-sort-override', effective);
      nodes.sortFilter.setAttribute(
        'title',
        effective === 'recently_added'
          ? 'This view uses recently added order. Year and Platform apply to All games.'
          : effective === 'recents'
            ? 'This view uses recent play order. Year and Platform apply to All games.'
            : 'This view uses title order. Year and Platform apply to All games.',
      );
      nodes.sortFilter.setAttribute('aria-label', catalogSortOverrideLabel(effective));
    } else {
      nodes.sortFilter.removeAttribute('data-sort-override');
      nodes.sortFilter.removeAttribute('title');
      nodes.sortFilter.setAttribute('aria-label', 'Sort');
    }
    if (nodes.sortFilterLabel) {
      nodes.sortFilterLabel.textContent = overridden ? catalogSortOverrideLabel(effective) : 'Sort';
    }
  }

  function syncRegionFilterOptions() {
    if (!nodes.regionFilter) return;
    const wanted = catalogDumpRegions();
    const kids = nodes.regionFilter.options || nodes.regionFilter.children || [];
    const existing = [];
    for (let index = 0; index < kids.length; index += 1) {
      if (kids[index] && kids[index].value) existing.push(kids[index].value);
    }
    const missing = wanted.filter(token => !existing.includes(token));
    if (!missing.length && existing.length) return;
    const selected = state.filters && state.filters.region || '';
    nodes.regionFilter.replaceChildren();
    const all = element('option', '', 'All regions');
    all.value = '';
    nodes.regionFilter.appendChild(all);
    wanted.forEach(token => {
      const option = element('option', '', regionLabel(token));
      option.value = token;
      nodes.regionFilter.appendChild(option);
    });
    nodes.regionFilter.value = selected;
  }

  function syncCatalogFilters() {
    syncRegionFilterOptions();
    if (nodes.regionFilter) nodes.regionFilter.value = state.filters && state.filters.region || '';
    syncCatalogSortControl();
    if (nodes.availabilityFilter) nodes.availabilityFilter.value = state.filters && state.filters.availability || '';
    if (nodes.hidePrerelease) nodes.hidePrerelease.checked = Boolean(state.filters && state.filters.hide_prerelease);
    if (nodes.hideHacks) nodes.hideHacks.checked = Boolean(state.filters && state.filters.hide_hacks);
    if (nodes.systemFilter) {
      const selected = state.platformQuery || (state.filters && state.filters.system) || '';
      const seen = Object.create(null);
      const systems = [];
      (state.platforms || []).forEach(platform => {
        if (!platform.id || seen[platform.id]) return;
        seen[platform.id] = true;
        systems.push({ id: platform.id, label: platform.label || systemLabel(platform.id) });
      });
      (state.games || []).forEach(game => {
        if (!game.system || seen[game.system]) return;
        seen[game.system] = true;
        systems.push({ id: game.system, label: systemLabel(game.system) });
      });
      systems.sort((left, right) => left.label.localeCompare(right.label));
      nodes.systemFilter.replaceChildren();
      const all = element('option', '', 'All platforms');
      all.value = '';
      nodes.systemFilter.appendChild(all);
      systems.forEach(system => {
        const option = element('option', '', system.label);
        option.value = system.id;
        nodes.systemFilter.appendChild(option);
      });
      nodes.systemFilter.value = selected;
    }
    if (!nodes.genreFilter) return;
    const selectedGenre = state.filters && state.filters.genre || '';
    const genres = (state.facets && state.facets.genres || []).slice();
    nodes.genreFilter.replaceChildren();
    const allGenres = element('option', '', 'All genres');
    allGenres.value = '';
    nodes.genreFilter.appendChild(allGenres);
    genres.forEach(genre => {
      const option = element('option', '', genre);
      option.value = genre;
      nodes.genreFilter.appendChild(option);
    });
    nodes.genreFilter.value = selectedGenre;
    if (!nodes.yearFilter) return;
    const selectedYear = state.filters && state.filters.year || '';
    const years = (state.facets && state.facets.years || []).slice();
    nodes.yearFilter.replaceChildren();
    const allYears = element('option', '', 'All years');
    allYears.value = '';
    nodes.yearFilter.appendChild(allYears);
    years.forEach(year => {
      const option = element('option', '', year);
      option.value = year;
      nodes.yearFilter.appendChild(option);
    });
    nodes.yearFilter.value = selectedYear;
  }

  function neutralArtwork(role) {
    const tagName = role === 'cover' ? 'span' : 'div';
    return element(tagName, `${role}-art artwork-empty`);
  }

  function replaceWithNeutralArtwork(image, role) {
    const replacement = neutralArtwork(role);
    if (typeof image.replaceWith === 'function') {
      image.replaceWith(replacement);
      return;
    }
    image.tagName = replacement.tagName;
    image.className = replacement.className;
    image.textContent = replacement.textContent;
    if (image.attributes && typeof image.attributes.clear === 'function') image.attributes.clear();
  }

  function artworkElement(role, style, handle, loading) {
    if (!handle) {
      if (!style) return neutralArtwork(role);
      return element(
        role === 'cover' ? 'span' : 'div',
        `${role}-art palette-${style.palette} treatment-${style.treatment}`,
      );
    }
    const image = element('img', `${role}-art image-art`);
    image.setAttribute('src', artworkPath(handle));
    image.setAttribute('alt', '');
    image.setAttribute('loading', loading);
    image.addEventListener('error', () => replaceWithNeutralArtwork(image, role));
    return image;
  }

  function liveGameForID(id) {
    if (!id) return null;
    if (state.selectedLiveGame && state.selectedLiveGame.id === id) return state.selectedLiveGame;
    const listed = (state.games || []).find(item => item.id === id);
    if (listed) return listed;
    const pools = [state.selectedLiveGame].concat(state.games || []);
    for (const rail of state.homeRails || []) {
      for (const view of rail.gameViews || []) {
        if (view && view.live) {
          if (view.live.id === id) return view.live;
          pools.push(view.live);
        }
      }
    }
    for (const item of pools) {
      if (!item || !Array.isArray(item.variants)) continue;
      const variant = item.variants.find(candidate => candidate && candidate.id === id);
      if (!variant) continue;
      if (variant.group_key || !item.group_key) return variant;
      return Object.freeze({ ...variant, group_key: item.group_key });
    }
    return null;
  }

  function cardSessionPlaying(game) {
    return isSessionPlayingCard(
      state.session,
      game,
      state.sessionLiveGame || liveGameForID(state.session && state.session.game_id),
      state.sessionAuthority
    );
  }

  function appendCoverMarks(card, game) {
    const favorite = game && game.favorite === true;
    const offline = cardSourceOffline(game);
    const unreadable = cardSourceUnreadable(game);
    const playing = cardSessionPlaying(game);
    const dumpLabels = dumpFlagLabels(game);
    const labels = collectionLabels(game, state.collections);
    if (favorite) card.setAttribute('data-favorite', 'true');
    if (offline) card.setAttribute('data-unavailable', 'true');
    if (unreadable) card.setAttribute('data-invalid', 'true');
    if (playing) card.setAttribute('data-playing', 'true');
    if (!favorite && !offline && !unreadable && !playing && !dumpLabels.length && !labels.length) return;
    const marks = element('span', 'card-marks');
    if (offline) marks.appendChild(element('span', 'card-mark card-mark-offline', 'Offline'));
    if (unreadable) marks.appendChild(element('span', 'card-mark card-mark-invalid', 'Unreadable'));
    if (playing) marks.appendChild(element('span', 'card-mark card-mark-playing', 'Playing'));
    dumpLabels.forEach(label => {
      marks.appendChild(element('span', `card-mark card-mark-dump card-mark-${label.toLowerCase()}`, label));
    });
    if (labels.length) {
      marks.appendChild(element('span', 'card-mark card-mark-collection', labels[0]));
      if (labels.length > 1) {
        marks.appendChild(element('span', 'card-mark card-mark-collection-more', `+${labels.length - 1}`));
      }
    }
    if (favorite) marks.appendChild(element('span', 'card-mark card-mark-favorite', 'Favorite'));
    card.appendChild(marks);
    return marks;
  }

  function setCardMarksClearance(card, marks) {
    if (!card || !marks || String(card.className || '').includes('game-row')) return;
    const height = Number(marks.offsetHeight) || 0;
    if (!(height > 0) || !card.style) return;
    if (typeof card.style.setProperty === 'function') {
      card.style.setProperty('--card-marks-clearance', `${height}px`);
      return;
    }
    card.style['--card-marks-clearance'] = `${height}px`;
  }

  function scheduleCardMarksClearance(card, marks) {
    setCardMarksClearance(card, marks);
    const afterLayout = typeof root.requestAnimationFrame === 'function'
      ? root.requestAnimationFrame.bind(root)
      : (typeof root.setTimeout === 'function' ? fn => root.setTimeout(fn, 0) : null);
    if (afterLayout) afterLayout(() => setCardMarksClearance(card, marks));
    const Observer = root.ResizeObserver;
    if (typeof Observer !== 'function') return;
    const observer = new Observer(() => setCardMarksClearance(card, marks));
    observer.observe(card);
    cardMarksObservers.push(observer);
  }

  function renderCard(view, liveGame, parent, pane, homeCoord) {
    const game = view.live;
    const presentation = view.presentation;
    const selected = isSelectedGame(state.selectedLiveGame, game);
    const homeSelected = pane !== 'home' || !homeCoord
      || (homeFocus.rail === homeCoord.rail && homeFocus.card === homeCoord.card);
    const showSelected = selected && homeSelected;
    const listRow = isListLayout() && pane !== 'home';
    const card = element('button', 'game-card' + (listRow ? ' game-row' : '') + (showSelected ? ' selected' : ''));
    card.type = 'button';
    card.setAttribute('aria-pressed', String(selected));
    card.setAttribute('data-game-id', game.id);
    card.setAttribute('data-system', game.system);
    card.setAttribute('data-state', game.state || '');
    const coverHandle = presentation.coverArtworkHandle || game.cover;
    const cover = artworkElement('cover', presentation.cover, coverHandle, 'eager');
    card.appendChild(cover);
    let coverMarks = null;
    if (listRow) {
      const body = element('span', 'game-row-body');
      body.appendChild(element('h3', '', cardTitle(game)));
      const bits = [systemLabel(game.system)];
      const year = (game && game.year) || catalogYear(view);
      if (year && year !== '—') bits.push(year);
      const genre = catalogGenre(view);
      if (genre) bits.push(genre);
      const studio = catalogStudio(view);
      if (studio) bits.push(studio);
      const players = catalogPlayers(view);
      if (players) bits.push(players);
      dumpFlagLabels(game).forEach(label => bits.push(label));
      const labels = collectionLabels(game, state.collections);
      if (labels.length) bits.push(labels[0]);
      if (labels.length > 1) bits.push(`+${labels.length - 1}`);
      bits.push(coverStatusLabel(game));
      body.appendChild(element('p', 'game-meta', bits.join(' · ')));
      if (game.favorite === true) body.appendChild(element('p', 'game-row-favorite', 'Favorite'));
      if (game.variant_count > 1) {
        body.appendChild(element('p', 'game-meta game-meta-variant', `${game.variant_count} versions`));
      }
      card.appendChild(body);
    } else {
      card.appendChild(element('h3', '', cardTitle(game)));
      card.appendChild(element('p', 'game-meta', coverHoverMeta(game, view)));
      if (presentation.isFallback) {
        card.appendChild(element('p', 'fallback-note', 'Using local catalog data'));
      } else {
        const subtitle = [catalogStudio(view), catalogPlayers(view)].filter(Boolean).join(' · ');
        if (subtitle) card.appendChild(element('p', 'game-studio', subtitle));
      }
      if (game.variant_count > 1) {
        card.appendChild(element('p', 'game-meta game-meta-variant', `${game.variant_count} versions`));
      }
      coverMarks = appendCoverMarks(card, game);
    }
    if (game.title && cardTitle(game) !== game.title) card.setAttribute('title', game.title);
    card.tabIndex = showSelected || (!state.selectedLiveGame && !gameCardNodes().length && !(parent && parent.children && parent.children.length)) ? 0 : -1;
    card.addEventListener('click', () => {
      closeGameActionsMenu({ restoreFocus: false });
      keyboardPane = pane || 'grid';
      if (pane === 'home' && homeCoord) homeFocus = { rail: homeCoord.rail, card: homeCoord.card };
      return selectGame(liveGame.id);
    });
    card.addEventListener('contextmenu', event => {
      if (event) event.preventDefault?.();
      openGameActionsMenu(card, event);
    });
    (parent || nodes.list).appendChild(card);
    if (coverMarks) scheduleCardMarksClearance(card, coverMarks);
  }

  function launchControl(game) {
    const label = 'Launch';
    if (!game) return { label, reason: launchBlockReason(game), enabled: false };
    const blocked = launchBlockReason(game);
    if (blocked) return { label, reason: blocked, enabled: false };
    if (state.activeMutation) {
      return { label, reason: 'A session transition is already in progress.', enabled: false };
    }
    if (state.sessionStarted && !state.session) {
      return {
        label,
        reason: state.sessionPhase === 'unavailable'
          ? 'Session status is unavailable; retry before launching.'
          : 'Session status is not ready; wait for the host check to finish.',
        enabled: false,
      };
    }
    if (state.sessionStarted && !['idle', 'stopped', 'active'].includes(state.sessionPhase)) {
      return {
        label,
        reason: state.sessionPhase === 'malformed'
          ? 'Session status was malformed; retry before launching.'
          : 'The current session transition must finish before launching.',
        enabled: false,
      };
    }
    if (state.session && state.session.state === 'active' && state.session.game_id === game.id) {
      return { label, reason: 'Already active.', enabled: false };
    }
    if (state.session && state.session.state === 'active') {
      return { label: 'Replace active session', reason: '', enabled: true };
    }
    return { label, reason: '', enabled: true };
  }

  function renderDetail(gameView, liveGame) {
    const game = gameView ? gameView.live : null;
    const presentation = gameView ? gameView.presentation : null;
    nodes.detailContent.replaceChildren();
    nodes.launchActions.replaceChildren();
    updateLaunchStatus();
    if (!game) {
      nodes.detailContent.appendChild(element('p', 'eyebrow', 'Now viewing'));
    } else {
      const backdrop = artworkElement('backdrop', presentation.backdrop, presentation.backdropArtworkHandle, 'eager');
      nodes.detailContent.appendChild(backdrop);
      const hero = element('div', 'detail-hero');
      const coverHandle = presentation.coverArtworkHandle || game.cover;
      hero.appendChild(artworkElement('cover', presentation.cover, coverHandle, 'eager'));
      const heroCopy = element('div', 'detail-hero-copy');
      if (presentation.logoHandle) heroCopy.appendChild(artworkElement('logo', null, presentation.logoHandle, 'lazy'));
      hero.appendChild(heroCopy);
      nodes.detailContent.appendChild(hero);
      nodes.detailContent.appendChild(element('p', 'eyebrow', 'Game'));
    }
    const heading = element('h2', '', detailHeading(game));
    heading.id = 'detail-heading';
    nodes.detailContent.appendChild(heading);
    if (!game) {
      nodes.detailContent.appendChild(element('p', 'muted', 'Choose a game.'));
      return;
    }
    const summary = catalogSummary(gameView);
    if (summary) nodes.detailContent.appendChild(element('p', 'detail-summary', summary));
    if (presentation.isFallback) {
      nodes.detailContent.appendChild(element('p', 'fallback-note', 'Using local catalog data'));
      nodes.detailContent.appendChild(element('p', 'sr-only', 'metadata_fallback'));
    }
    if (presentation.attribution) nodes.detailContent.appendChild(element('p', 'attribution', presentation.attribution));
    const facts = element('div', 'detail-facts');
    const factRows = [
      [systemLabel(game.system), 'System'],
      [coverStatusLabel(game), 'Status'],
      [sourceKindLabel(liveGame || game), 'Source'],
      [catalogYear(gameView), 'Year'],
      [catalogGenre(gameView), 'Genre'],
      [catalogStudio(gameView), 'Studio'],
      [catalogPlayers(gameView), 'Players'],
    ];
    dumpIdentityFacts(game).forEach(fact => factRows.push([fact.value, fact.label]));
    const labels = collectionLabels(game, state.collections);
    if (labels.length) factRows.push([labels.join(', '), 'Collections']);
    factRows
      .filter(([value]) => value)
      .forEach(([value, label]) => {
      const fact = element('div', 'detail-fact');
      fact.appendChild(element('strong', '', value));
      fact.appendChild(element('span', '', label));
      facts.appendChild(fact);
      });
    nodes.detailContent.appendChild(facts);
    const favorite = element('button', 'button secondary favorite-button', game.favorite ? 'Favorited' : 'Favorite');
    favorite.type = 'button';
    favorite.id = 'favorite-game';
    favorite.addEventListener('click', () => controller.toggleFavorite(game.id));
    nodes.detailContent.appendChild(favorite);
    const membership = Array.isArray(game.collections) ? game.collections : [];
    (state.collections || []).forEach(collection => {
      const member = membership.includes(collection.id);
      const toggle = element('button', 'button secondary collection-member-button', member ? `Remove from ${collection.name}` : `Add to ${collection.name}`);
      toggle.type = 'button';
      toggle.id = `collection-member-${collection.id}`;
      toggle.setAttribute('data-collection', collection.id);
      toggle.addEventListener('click', () => controller.toggleCollectionMember(collection.id, game.id));
      nodes.detailContent.appendChild(toggle);
    });
    const variants = Array.isArray(game.variants) ? game.variants : [];
    if (variants.length > 1) {
      const label = element('label', 'filter-label', 'Version');
      const select = element('select');
      select.id = 'game-version';
      variants.forEach(variant => {
        const option = element('option', '', variantLabel(variant));
        option.value = variant.id;
        if (variant.id === game.id) option.selected = true;
        select.appendChild(option);
      });
      select.addEventListener('change', () => selectGame(select.value));
      label.appendChild(select);
      nodes.detailContent.appendChild(label);
    }
    if (presentation.marqueeHandle) {
      nodes.detailContent.appendChild(artworkElement('marquee', null, presentation.marqueeHandle, 'lazy'));
    }
    if (presentation.screenshotHandles && presentation.screenshotHandles.length) {
      const stills = element('div', 'extra-stills');
      presentation.screenshotHandles.forEach(handle => stills.appendChild(artworkElement('screenshot', null, handle, 'lazy')));
      nodes.detailContent.appendChild(stills);
    }
    if (presentation.videoHandle && !reducedMotion()) {
      const video = element('video', 'detail-video');
      video.setAttribute('src', mediaPath(presentation.videoHandle));
      video.muted = true;
      video.autoplay = true;
      video.loop = true;
      video.setAttribute('controls', '');
      video.setAttribute('muted', '');
      video.setAttribute('playsinline', '');
      nodes.detailContent.appendChild(video);
    }
    if (state.detailState === 'detail_error') {
      nodes.launchActions.appendChild(element('p', 'status-message error', 'The live detail could not be refreshed.'));
      nodes.launchActions.appendChild(retryButton('Retry detail', () => refreshDetail(liveGame)));
      return;
    }
    if (state.launchState === 'launching') {
      nodes.launchActions.appendChild(element('p', 'status-message', 'launching: preparing the selected live game…'));
    }
    if (state.launchState === 'launch_success') {
      nodes.launchActions.appendChild(element('p', 'status-message success', 'launch_success: session accepted by the local host.'));
    }
    if (state.launchState === 'launch_error') {
      const failure = element('p', 'status-message error', state.launchMessage);
      failure.setAttribute('role', 'alert');
      nodes.launchActions.appendChild(failure);
      nodes.launchActions.appendChild(retryButton('Retry launch', launchSelected));
    }
    const control = launchControl(liveGame);
    const reason = element('p', 'launch-reason', control.reason);
    reason.id = 'launch-reason';
    const launch = element('button', 'button', control.label);
    launch.type = 'button';
    launch.id = 'launch-game';
    launch.disabled = !control.enabled;
    if (control.reason) launch.setAttribute('aria-describedby', reason.id);
    launch.addEventListener('click', () => {
      keyboardPane = 'detail';
      return launchSelected();
    });
    nodes.launchActions.appendChild(launch);
    nodes.launchActions.appendChild(reason);
  }

  function loadCatalog() {
    return controller.reloadVisibleCatalog(nodes.search.value);
  }

  function cancelPendingSearch() {
    if (!searchTimer) return;
    root.clearTimeout(searchTimer);
    searchTimer = null;
  }

  function navigateLibrary(action, commitSearch) {
    cancelPendingSearch();
    if (commitSearch) {
      controller.adoptSearchQuery(nodes.search && nodes.search.value);
    } else if (nodes.search) {
      nodes.search.value = controller.getState().query || '';
    }
    return action();
  }

  function searchFromInput() {
    return controller.searchVisibleCatalog(nodes.search.value);
  }

  function loadSession() {
    return controller.loadSession();
  }

  function refreshDetail(game) {
    return controller.refreshDetail(game);
  }

  function selectGame(game) {
    return controller.selectGame(game);
  }

  function launchSelected() {
    return controller.launchSelected();
  }

  function stopSession() {
    return controller.stopSession();
  }

  function focusWithoutScroll(node) {
    if (node && typeof node.focus === 'function') node.focus({ preventScroll: true });
  }

  function scrollHomeCardIntoView(node) {
    if (!node || typeof node.scrollIntoView !== 'function') return;
    if (!String(node.className || '').includes('game-card')) return;
    try {
      node.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    } catch (_) {
      try { node.scrollIntoView(); } catch (__) { /* ignore */ }
    }
  }

  function libraryNavChanged(previous, next) {
    if (!previous) return true;
    return previous.collection !== next.collection
      || previous.platformQuery !== next.platformQuery
      || previous.libraryView !== next.libraryView
      || previous.platforms !== next.platforms
      || previous.collections !== next.collections;
  }

  function homeFocusFromRails(rails, selectedGame, previousFocus) {
    const rows = Array.isArray(rails) ? rails : [];
    if (selectedGame) {
      const prevRail = Math.max(0, Number(previousFocus && previousFocus.rail) || 0);
      const prevCard = Math.max(0, Number(previousFocus && previousFocus.card) || 0);
      const prevViews = rows[prevRail] && rows[prevRail].gameViews ? rows[prevRail].gameViews : [];
      const prevLive = prevViews[prevCard] && prevViews[prevCard].live;
      if (prevLive && isSelectedGame(selectedGame, prevLive)) {
        return { rail: prevRail, card: prevCard };
      }
      for (let rail = 0; rail < rows.length; rail += 1) {
        const views = rows[rail] && rows[rail].gameViews ? rows[rail].gameViews : [];
        const card = views.findIndex(view => view && view.live && isSelectedGame(selectedGame, view.live));
        if (card >= 0) return { rail, card };
      }
    }
    return { rail: 0, card: 0 };
  }

  function syncHomeFocus(next) {
    if (!next || next.libraryView !== 'home' || next.catalogState !== 'populated') return;
    if (!next.homeRails || !next.homeRails.length) return;
    homeFocus = homeFocusFromRails(next.homeRails, next.selectedLiveGame, homeFocus);
  }

  function handleStateChange(next) {
    const previous = state;
    state = next;
    if (gameActionsMenuIsOpen() && libraryNavChanged(previous, next)) {
      closeGameActionsMenu({ restoreFocus: false });
    }
    if (next.hostState === 'ready') setHealth('Local host ready', 'host-status');
    if (next.hostState === 'unavailable') setHealth('Catalog unavailable', 'host-status');
    syncHomeFocus(next);
    renderCatalog();
    if (libraryNavChanged(previous, next)) renderLibraryNav();
    if (controller && typeof controller.observeVisibleCovers === 'function') controller.observeVisibleCovers();
    renderDetail(state.selectedGameView, state.selectedLiveGame);
    renderSession();
    if (!attractActive) resetAttractTimer();
    if (previous && previous.activeMutation !== 'launch' && next.activeMutation === 'launch') {
      focusWithoutScroll(nodes.sessionPanel);
    }
    if (previous && previous.activeMutation === 'stop' && next.sessionPhase === 'stopped') {
      focusWithoutScroll(refreshSessionButton);
    }
    if ((keyboardPane === 'grid' || keyboardPane === 'home') && !next.selectedLiveGame && next.catalogState === 'populated' && next.gameViews && next.gameViews.length) {
      forceKeyboardRestore = true;
      controller.selectGame(next.gameViews[0].live.id);
      return;
    }
    restoreKeyboardFocus();
  }

  function attractIsDisabled() {
    return root.FogCastAttractDisabled === true;
  }

  let attractTimer = null;
  let attractCycleTimer = null;
  let attractItems = [];
  let attractIndex = 0;
  let attractActive = false;

  function currentAttractIdleMs() {
    if (Number(root.FogCastAttractIdleMs) > 0) {
      return Math.min(Number(root.FogCastAttractIdleMs), MAX_ATTRACT_IDLE_MS);
    }
    const seconds = Number(state && state.attractIdleSeconds);
    const ms = seconds > 0 ? seconds * 1000 : 60000;
    if (!Number.isFinite(ms) || ms < 1) return 60000;
    return ms > MAX_ATTRACT_IDLE_MS ? MAX_ATTRACT_IDLE_MS : ms;
  }

  function currentAttractCycleMs() {
    if (Number(root.FogCastAttractCycleMs) > 0) return Number(root.FogCastAttractCycleMs);
    return reducedMotion() ? 8000 : 12000;
  }

  function reducedMotion() {
    return Boolean(root.matchMedia && root.matchMedia('(prefers-reduced-motion: reduce)').matches);
  }

  function clearAttractCycle() {
    if (attractCycleTimer && typeof root.clearTimeout === 'function') {
      root.clearTimeout(attractCycleTimer);
      attractCycleTimer = null;
    }
  }

  function hideAttract() {
    attractActive = false;
    clearAttractCycle();
    if (!nodes.attract) return;
    nodes.attract.hidden = true;
    if (nodes.attractStage) nodes.attractStage.replaceChildren();
    if (nodes.attractTitle) nodes.attractTitle.textContent = '';
  }

  function scheduleAttractAdvance() {
    clearAttractCycle();
    if (!attractActive || attractItems.length < 2 || typeof root.setTimeout !== 'function') return;
    attractCycleTimer = root.setTimeout(() => {
      attractCycleTimer = null;
      if (!attractActive || attractItems.length < 2) return;
      attractIndex = (attractIndex + 1) % attractItems.length;
      showAttractItem();
    }, currentAttractCycleMs());
  }

  function showAttractItem() {
    if (!nodes.attract || !nodes.attractStage || !attractItems.length) return;
    const item = attractItems[attractIndex % attractItems.length];
    nodes.attract.hidden = false;
    nodes.attractTitle.textContent = item.title || '';
    nodes.attractStage.replaceChildren();
    if (item.video && !reducedMotion()) {
      const video = element('video', 'attract-video');
      video.setAttribute('src', mediaPath(item.video));
      video.muted = true;
      video.autoplay = true;
      video.loop = attractItems.length < 2;
      video.setAttribute('playsinline', '');
      nodes.attractStage.appendChild(video);
      if (typeof video.play === 'function') video.play().catch(() => {});
      scheduleAttractAdvance();
      return;
    }
    const handle = item.cover || item.backdrop || item.marquee;
    if (handle) nodes.attractStage.appendChild(artworkElement('cover', null, handle, 'eager'));
    scheduleAttractAdvance();
  }

  async function enterAttract() {
    if (attractIsDisabled() || attractActive || settingsIsOpen() || !nodes.attract) return;
    closeGameActionsMenu({ restoreFocus: false });
    try {
      const playlist = await controller.loadAttract(24);
      if (attractIsDisabled() || attractActive || settingsIsOpen() || !nodes.attract) return;
      attractItems = playlist.items || [];
      if (!attractItems.length) return;
      attractIndex = 0;
      attractActive = true;
      showAttractItem();
    } catch (_) {
      attractActive = false;
    }
  }

  function resetAttractTimer() {
    if (attractTimer && typeof root.clearTimeout === 'function') {
      root.clearTimeout(attractTimer);
      attractTimer = null;
    }
    if (attractActive) hideAttract();
    if (attractIsDisabled() || settingsIsOpen() || !nodes.attract || !attractIdleHydrated) return;
    if (typeof root.setTimeout !== 'function') return;
    attractTimer = root.setTimeout(enterAttract, currentAttractIdleMs());
  }

  async function hydrateAttractIdle() {
    try {
      await controller.loadSettings();
    } catch (_) {
      /* keep the last known idle */
    }
    attractIdleHydrated = true;
    resetAttractTimer();
  }

  function exitAttract() {
    if (!attractActive) {
      resetAttractTimer();
      return;
    }
    hideAttract();
    resetAttractTimer();
  }

  function settingsIsOpen() {
    return Boolean(nodes.settings && nodes.settings.hidden === false);
  }

  function isSettingsTarget(target) {
    let node = target;
    while (node) {
      const id = node.id;
      if (node === nodes.settings
        || id === 'settings'
        || id === 'close-settings'
        || id === 'save-settings'
        || id === 'settings-attract-idle'
        || id === 'settings-preferred-regions') {
        return true;
      }
      node = node.parentNode;
    }
    return false;
  }

  function settingsFocusables() {
    return [
      nodes.settingsAttractIdle,
      nodes.settingsPreferredRegions,
      nodes.saveSettings,
      nodes.closeSettings,
    ].filter(node => node && !node.disabled && node.hidden !== true);
  }

  function setSettingsChromeInert(inert) {
    const launcher = typeof document.getElementById === 'function' ? document.getElementById('launcher') : null;
    if (launcher) launcher.inert = Boolean(inert);
    if (nodes.attract) nodes.attract.inert = Boolean(inert);
  }

  function wrapSettingsFocus(event) {
    const focusables = settingsFocusables();
    event.preventDefault?.();
    if (!focusables.length) return;
    const active = typeof document !== 'undefined' ? document.activeElement : null;
    const index = focusables.indexOf(active);
    if (event.shiftKey) {
      focusWithoutScroll(index <= 0 ? focusables[focusables.length - 1] : focusables[index - 1]);
      return;
    }
    focusWithoutScroll(index < 0 || index >= focusables.length - 1 ? focusables[0] : focusables[index + 1]);
  }

  function bumpSettingsGeneration() {
    settingsGeneration += 1;
    return settingsGeneration;
  }

  function settingsRequestExpired(generation) {
    return generation !== settingsGeneration || !settingsIsOpen();
  }

  function fillSettingsForm(settings) {
    if (nodes.settingsAttractIdle) {
      nodes.settingsAttractIdle.value = String((settings && settings.attract_idle_seconds) || state.attractIdleSeconds || 60);
    }
    if (nodes.settingsPreferredRegions) {
      const regions = settings && Array.isArray(settings.preferred_regions) ? settings.preferred_regions : [];
      nodes.settingsPreferredRegions.value = regions.join(', ');
    }
    if (nodes.settingsHostHealth) {
      nodes.settingsHostHealth.textContent = nodes.health ? nodes.health.textContent : '';
    }
    if (nodes.settingsMessage) nodes.settingsMessage.textContent = '';
  }

  async function openSettings() {
    closeGameActionsMenu({ restoreFocus: false });
    if (keyboardPane !== 'settings') settingsReturnPane = keyboardPane;
    if (nodes.settings) nodes.settings.hidden = false;
    setSettingsChromeInert(true);
    keyboardPane = 'settings';
    writePaneAttribute();
    const generation = bumpSettingsGeneration();
    resetAttractTimer();
    try {
      await controller.loadSettings();
      if (settingsRequestExpired(generation)) return;
      fillSettingsForm(controller.getState().librarySettings);
    } catch (error) {
      if (settingsRequestExpired(generation)) return;
      fillSettingsForm(null);
      if (nodes.settingsMessage) {
        nodes.settingsMessage.textContent = privacyMessage(error, 'Library settings could not be loaded.');
      }
    }
    if (settingsRequestExpired(generation)) return;
    forceKeyboardRestore = true;
    restoreKeyboardFocus();
  }

  function closeSettings() {
    bumpSettingsGeneration();
    if (nodes.settings) nodes.settings.hidden = true;
    setSettingsChromeInert(false);
    setKeyboardPane(settingsReturnPane || 'rail');
    resetAttractTimer();
  }

  function parseRegionsInput(value) {
    return String(value || '')
      .split(',')
      .map(item => item.trim())
      .filter(Boolean);
  }

  async function saveSettingsFromForm() {
    const generation = settingsGeneration;
    const idle = Number(nodes.settingsAttractIdle && nodes.settingsAttractIdle.value);
    const regions = parseRegionsInput(nodes.settingsPreferredRegions && nodes.settingsPreferredRegions.value);
    try {
      await controller.saveSettings({
        attract_idle_seconds: idle,
        preferred_regions: regions,
      });
      if (settingsRequestExpired(generation)) return;
      fillSettingsForm(controller.getState().librarySettings);
    } catch (error) {
      if (settingsRequestExpired(generation)) return;
      if (nodes.settingsMessage) {
        nodes.settingsMessage.textContent = privacyMessage(error, 'Library settings could not be saved.');
      }
    }
  }

  function typingTarget(target) {
    if (!target) return false;
    if (nodes.search && target === nodes.search) return true;
    const tag = String(target.tagName || '').toUpperCase();
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
  }

  function isSearchInput(target) {
    return Boolean(target && (target === nodes.search || target.id === 'game-search'));
  }

  function isLaunchTarget(target) {
    return Boolean(target && target.id === 'launch-game');
  }

  function isCollectionEditor(target) {
    return Boolean(target && (
      target.id === 'create-collection'
      || target.id === 'save-collection'
      || target.id === 'rename-collection'
      || target.id === 'delete-collection'
      || target.id === 'collection-name'
    ));
  }

  function isLayoutToggle(target) {
    return Boolean(target && (
      target === nodes.layoutCover
      || target === nodes.layoutList
      || target.id === 'layout-cover'
      || target.id === 'layout-list'
    ));
  }

  function isCoverWallTarget(target) {
    if (!target) return false;
    if (String(target.className || '').includes('game-card')) return true;
    return Boolean(nodes.list && (target === nodes.list || target.parentNode === nodes.list));
  }

  function visibleGames() {
    return state.gameViews || [];
  }

  function nodeIsConnected(node) {
    if (!node) return false;
    if (typeof node.isConnected === 'boolean') return node.isConnected;
    let current = node;
    const seen = new Set();
    while (current && !seen.has(current)) {
      seen.add(current);
      if (current.id && typeof document.getElementById === 'function' && document.getElementById(current.id) === current) {
        return true;
      }
      current = current.parentNode;
    }
    return false;
  }

  function writePaneAttribute() {
    const launcher = typeof document.getElementById === 'function' ? document.getElementById('launcher') : null;
    if (launcher && typeof launcher.setAttribute === 'function') {
      launcher.setAttribute('data-keyboard-pane', keyboardPane);
    }
  }

  function railItems() {
    const items = [
      nodes.navHome,
      nodes.navAll,
      nodes.navContinue,
      nodes.navFavorites,
      nodes.navRecents,
      nodes.navUnplayed,
      nodes.navRecentlyAdded,
    ].filter(Boolean);
    const collections = nodes.collectionList && nodes.collectionList.children ? Array.from(nodes.collectionList.children) : [];
    collections.forEach(item => {
      if (item && String(item.className || '').includes('nav-item')) items.push(item);
    });
    const platforms = nodes.platformList && nodes.platformList.children ? Array.from(nodes.platformList.children) : [];
    platforms.forEach(item => {
      if (item && String(item.className || '').includes('nav-item')) items.push(item);
    });
    return items;
  }

  function collectClassNodes(root, className, found) {
    if (!root) return found;
    if (String(root.className || '').includes(className)) found.push(root);
    const children = root.children ? Array.from(root.children) : [];
    children.forEach(child => collectClassNodes(child, className, found));
    return found;
  }

  function gameCardNodes() {
    if (nodes.list && typeof nodes.list.querySelectorAll === 'function') {
      try {
        return Array.from(nodes.list.querySelectorAll('.game-card'));
      } catch (_) {
        /* fall through to a descendant walk */
      }
    }
    return collectClassNodes(nodes.list, 'game-card', []).filter(node => node !== nodes.list);
  }

  function homeRailTracks() {
    if (nodes.list && typeof nodes.list.querySelectorAll === 'function') {
      try {
        return Array.from(nodes.list.querySelectorAll('.home-rail-track'));
      } catch (_) {
        /* fall through to a descendant walk */
      }
    }
    return collectClassNodes(nodes.list, 'home-rail-track', []).filter(node => node !== nodes.list);
  }

  function cardsInTrack(track) {
    if (!track) return [];
    if (typeof track.querySelectorAll === 'function') {
      try {
        return Array.from(track.querySelectorAll('.game-card'));
      } catch (_) {
        /* fall through */
      }
    }
    const children = track.children ? Array.from(track.children) : [];
    return children.filter(child => String(child.className || '').includes('game-card'));
  }

  function detailLaunchButton() {
    const byId = typeof document.getElementById === 'function' ? document.getElementById('launch-game') : null;
    if (byId) return byId;
    const children = nodes.launchActions && nodes.launchActions.children ? Array.from(nodes.launchActions.children) : [];
    return children.find(child => child.id === 'launch-game') || null;
  }

  function selectedRailItem() {
    const items = railItems();
    return items.find(item => String(item.className || '').includes('selected')) || items[0] || null;
  }

  function selectedHomeCardNode() {
    const tracks = homeRailTracks();
    if (!tracks.length) return null;
    const rail = Math.max(0, Math.min(tracks.length - 1, Number(homeFocus.rail) || 0));
    const cards = cardsInTrack(tracks[rail]);
    if (!cards.length) return null;
    const card = Math.max(0, Math.min(cards.length - 1, Number(homeFocus.card) || 0));
    return cards[card] || null;
  }

  function selectedCardNode() {
    if (keyboardPane === 'home' || (isHomeView() && keyboardPane !== 'grid')) {
      return selectedHomeCardNode() || null;
    }
    const cards = gameCardNodes();
    return cards.find(card => String(card.className || '').includes('selected') || card.getAttribute('aria-pressed') === 'true')
      || cards[0]
      || null;
  }

  function syncPaneFromTarget(target) {
    if (!target) return;
    if (settingsIsOpen() || isSettingsTarget(target)) {
      keyboardPane = 'settings';
    } else if (nodes.search && (target === nodes.search || target.id === 'game-search')) {
      keyboardPane = 'search';
    } else if (railItems().includes(target) || (nodes.platformList && target.parentNode === nodes.platformList)) {
      keyboardPane = 'rail';
    } else if (String(target.className || '').includes('game-card') || (nodes.list && target.parentNode === nodes.list)) {
      keyboardPane = catalogPane();
    } else if (target.id === 'launch-game' || target.id === 'favorite-game' || target.id === 'game-version'
      || (nodes.detail && (target === nodes.detail || target.parentNode === nodes.detail
        || target.parentNode === nodes.detailContent || target.parentNode === nodes.launchActions))) {
      keyboardPane = 'detail';
    }
    writePaneAttribute();
  }

  function restoreKeyboardFocus() {
    if (attractActive) return;
    if (gameActionsMenuIsOpen()) {
      const activeMenu = typeof document !== 'undefined' ? document.activeElement : null;
      if (activeMenu && isGameActionsMenuTarget(activeMenu) && nodeIsConnected(activeMenu)) {
        writePaneAttribute();
        return;
      }
      const first = gameActionsMenuItems()[0];
      if (first) {
        focusGameActionsItem(first);
        writePaneAttribute();
        return;
      }
    }
    const active = typeof document !== 'undefined' ? document.activeElement : null;
    if (settingsIsOpen() || keyboardPane === 'settings') {
      keyboardPane = 'settings';
      if (!forceKeyboardRestore && active && nodeIsConnected(active) && isSettingsTarget(active)) {
        writePaneAttribute();
        return;
      }
      forceKeyboardRestore = false;
      focusWithoutScroll(nodes.settingsAttractIdle || nodes.openSettings);
      writePaneAttribute();
      return;
    }
    if (!forceKeyboardRestore && active && nodeIsConnected(active)) {
      syncPaneFromTarget(active);
      writePaneAttribute();
      return;
    }
    forceKeyboardRestore = false;
    if (keyboardPane === 'search') {
      focusWithoutScroll(nodes.search);
    } else if (keyboardPane === 'rail') {
      focusWithoutScroll(selectedRailItem());
    } else if (keyboardPane === 'detail') {
      focusWithoutScroll(detailLaunchButton() || nodes.detail);
    } else if (keyboardPane === 'home') {
      const card = selectedCardNode() || nodes.list;
      focusWithoutScroll(card);
      scrollHomeCardIntoView(card);
    } else {
      keyboardPane = 'grid';
      const card = selectedCardNode() || nodes.list;
      focusWithoutScroll(card);
      if (isListLayout()) scrollHomeCardIntoView(card);
    }
    writePaneAttribute();
  }

  function setKeyboardPane(pane) {
    keyboardPane = pane;
    forceKeyboardRestore = true;
    writePaneAttribute();
    restoreKeyboardFocus();
  }

  function notifySearchInput() {
    if (!nodes.search) return;
    if (typeof Event === 'function' && typeof nodes.search.dispatchEvent === 'function') {
      try {
        nodes.search.dispatchEvent(new Event('input', { bubbles: true }));
        return;
      } catch (_) {
        /* fall through to stored listener */
      }
    }
    const listener = nodes.search.listeners && typeof nodes.search.listeners.get === 'function'
      ? nodes.search.listeners.get('input')
      : null;
    if (typeof listener === 'function') listener({ target: nodes.search });
  }

  function typeToSearch(key) {
    if (!nodes.search) return;
    keyboardPane = 'search';
    if (key && key !== '/') nodes.search.value = String(key);
    forceKeyboardRestore = true;
    focusWithoutScroll(nodes.search);
    writePaneAttribute();
    if (key && key !== '/') notifySearchInput();
  }

  function isTypeToSearchKey(event) {
    if (!event || event.ctrlKey || event.metaKey || event.altKey) return false;
    const key = event.key;
    if (key === '/') return true;
    if (!key || key.length !== 1 || key === ' ') return false;
    return key !== '\u0000' && !/[\u0000-\u001f]/.test(key);
  }

  function focusedRailIndex() {
    const items = railItems();
    const active = typeof document !== 'undefined' ? document.activeElement : null;
    const fromFocus = items.findIndex(item => item === active);
    if (fromFocus >= 0) return fromFocus;
    const fromSelected = items.findIndex(item => String(item.className || '').includes('selected'));
    return fromSelected >= 0 ? fromSelected : 0;
  }

  function moveRail(delta) {
    const items = railItems();
    if (!items.length) return;
    const current = focusedRailIndex();
    const next = Math.max(0, Math.min(items.length - 1, current + delta));
    keyboardPane = 'rail';
    forceKeyboardRestore = true;
    focusWithoutScroll(items[next]);
    writePaneAttribute();
  }

  async function commitRail(item) {
    const target = item || railItems()[focusedRailIndex()] || selectedRailItem();
    if (target && typeof target.click === 'function') await target.click();
    if (isHomeView()) {
      keyboardPane = 'home';
      writePaneAttribute();
      if (!homeRailTracks().length) {
        forceKeyboardRestore = true;
        restoreKeyboardFocus();
        return;
      }
      return focusHomeCard(homeFocus.rail, homeFocus.card);
    }
    keyboardPane = 'grid';
    forceKeyboardRestore = true;
    writePaneAttribute();
    if (!state.selectedLiveGame && visibleGames().length) {
      await controller.selectGame(visibleGames()[0].live.id);
      return;
    }
    restoreKeyboardFocus();
  }

  function focusedHomePosition() {
    const tracks = homeRailTracks();
    const active = typeof document !== 'undefined' ? document.activeElement : null;
    for (let rail = 0; rail < tracks.length; rail += 1) {
      const cards = cardsInTrack(tracks[rail]);
      const card = cards.findIndex(node => node === active);
      if (card >= 0) return { rail, card, tracks, cards };
    }
    const storedRail = Math.max(0, Math.min(Math.max(0, tracks.length - 1), Number(homeFocus.rail) || 0));
    const storedCards = tracks.length ? cardsInTrack(tracks[storedRail]) : [];
    if (storedCards.length) {
      const storedCard = Math.max(0, Math.min(storedCards.length - 1, Number(homeFocus.card) || 0));
      return { rail: storedRail, card: storedCard, tracks, cards: storedCards };
    }
    const firstCards = tracks.length ? cardsInTrack(tracks[0]) : [];
    return { rail: 0, card: 0, tracks, cards: firstCards };
  }

  async function focusHomeCard(rail, card) {
    const tracks = homeRailTracks();
    if (!tracks.length) return;
    const nextRail = Math.max(0, Math.min(tracks.length - 1, rail));
    const cards = cardsInTrack(tracks[nextRail]);
    if (!cards.length) return;
    const nextCard = Math.max(0, Math.min(cards.length - 1, card));
    homeFocus = { rail: nextRail, card: nextCard };
    keyboardPane = 'home';
    forceKeyboardRestore = true;
    const node = cards[nextCard];
    const id = node && typeof node.getAttribute === 'function' ? node.getAttribute('data-game-id') : '';
    if (id) await controller.selectGame(id);
    const focused = selectedHomeCardNode() || node;
    focusWithoutScroll(focused);
    scrollHomeCardIntoView(focused);
    writePaneAttribute();
  }

  async function moveHome(dRail, dCard, edge) {
    const position = focusedHomePosition();
    if (!position.tracks.length) return;
    if (edge === 'start') return focusHomeCard(position.rail, 0);
    if (edge === 'end') {
      const cards = cardsInTrack(position.tracks[position.rail]);
      return focusHomeCard(position.rail, Math.max(0, cards.length - 1));
    }
    return focusHomeCard(position.rail + dRail, position.card + dCard);
  }

  function enterDetail() {
    const visible = visibleGames();
    keyboardPane = 'detail';
    forceKeyboardRestore = true;
    writePaneAttribute();
    if (!state.selectedLiveGame && visible.length) {
      controller.selectGame(visible[0].live.id);
      return;
    }
    restoreKeyboardFocus();
  }

  async function ensureMoreCatalog(index) {
    if (!state.nextCursor) return;
    const visible = visibleGames();
    const cols = catalogColumns();
    if (index < visible.length - Math.max(1, cols)) return;
    await controller.loadMoreCatalog();
  }

  async function moveWall(delta) {
    const visible = visibleGames();
    if (!visible.length) {
      if (state.nextCursor) await controller.loadMoreCatalog();
      return;
    }
    const current = visible.findIndex(item => isSelectedGame(state.selectedLiveGame, item.live));
    const next = current < 0 ? 0 : Math.max(0, Math.min(visible.length - 1, current + delta));
    keyboardPane = 'grid';
    forceKeyboardRestore = true;
    await controller.selectGame(visible[next].live.id);
    await ensureMoreCatalog(next);
  }

  async function jumpWall(edge) {
    keyboardPane = 'grid';
    forceKeyboardRestore = true;
    if (edge === 'end' && state.nextCursor) await controller.loadMoreCatalog();
    const visible = visibleGames();
    if (!visible.length) return;
    const index = edge === 'end' ? visible.length - 1 : 0;
    await controller.selectGame(visible[index].live.id);
    if (edge === 'end') await ensureMoreCatalog(index);
  }

  function moveGrid(key) {
    const cols = catalogColumns();
    const visible = visibleGames();
    const current = visible.findIndex(item => isSelectedGame(state.selectedLiveGame, item.live));
    if (key === 'ArrowLeft' && (current <= 0 || current % cols === 0)) {
      setKeyboardPane('rail');
      return;
    }
    if (key === 'ArrowRight') {
      if (isListLayout()) return;
      return moveWall(1);
    }
    if (key === 'ArrowLeft') return moveWall(-1);
    if (key === 'ArrowDown') return moveWall(cols);
    if (key === 'ArrowUp') return moveWall(current >= 0 && current < cols ? -current : -cols);
  }

  controller = createAppController({
    metadataAdapter: root.FogCastMetadata,
    onStateChange: handleStateChange,
  });
  state = controller.getState();
  nodes.refresh.addEventListener('click', loadCatalog);
  nodes.search.addEventListener('input', () => {
    if (searchTimer) root.clearTimeout(searchTimer);
    searchTimer = root.setTimeout(searchFromInput, 180);
  });
  if (nodes.systemFilter) nodes.systemFilter.addEventListener('change', () => controller.setCatalogFilter('system', nodes.systemFilter.value));
  if (nodes.regionFilter) nodes.regionFilter.addEventListener('change', () => controller.setCatalogFilter('region', nodes.regionFilter.value));
  if (nodes.genreFilter) nodes.genreFilter.addEventListener('change', () => controller.setCatalogFilter('genre', nodes.genreFilter.value));
  if (nodes.yearFilter) nodes.yearFilter.addEventListener('change', () => controller.setCatalogFilter('year', nodes.yearFilter.value));
  if (nodes.sortFilter) {
    nodes.sortFilter.addEventListener('change', () => {
      if (nodes.sortFilter.disabled) return;
      return controller.setCatalogSort(nodes.sortFilter.value);
    });
  }
  if (nodes.hidePrerelease) nodes.hidePrerelease.addEventListener('change', () => controller.setCatalogFilter('hide_prerelease', nodes.hidePrerelease.checked));
  if (nodes.hideHacks) nodes.hideHacks.addEventListener('change', () => controller.setCatalogFilter('hide_hacks', nodes.hideHacks.checked));
  if (nodes.availabilityFilter) nodes.availabilityFilter.addEventListener('change', () => controller.setCatalogFilter('availability', nodes.availabilityFilter.value));
  if (nodes.navHome) nodes.navHome.addEventListener('click', () => { keyboardPane = 'rail'; return navigateLibrary(() => controller.openHome()); });
  if (nodes.navAll) nodes.navAll.addEventListener('click', () => { keyboardPane = 'rail'; return navigateLibrary(() => controller.setLibraryNav('', ''), true); });
  if (nodes.navContinue) nodes.navContinue.addEventListener('click', () => { keyboardPane = 'rail'; return navigateLibrary(() => controller.setLibraryNav('continue', ''), true); });
  if (nodes.navFavorites) nodes.navFavorites.addEventListener('click', () => { keyboardPane = 'rail'; return navigateLibrary(() => controller.setLibraryNav('favorites', ''), true); });
  if (nodes.navRecents) nodes.navRecents.addEventListener('click', () => { keyboardPane = 'rail'; return navigateLibrary(() => controller.setLibraryNav('recents', ''), true); });
  if (nodes.navUnplayed) nodes.navUnplayed.addEventListener('click', () => { keyboardPane = 'rail'; return navigateLibrary(() => controller.setLibraryNav('unplayed', ''), true); });
  if (nodes.navRecentlyAdded) nodes.navRecentlyAdded.addEventListener('click', () => { keyboardPane = 'rail'; return navigateLibrary(() => controller.setLibraryNav('recently_added', ''), true); });
  if (nodes.layoutCover) nodes.layoutCover.addEventListener('click', () => controller.setCatalogLayout('cover'));
  if (nodes.layoutList) nodes.layoutList.addEventListener('click', () => controller.setCatalogLayout('list'));
  if (nodes.createCollection) nodes.createCollection.addEventListener('click', () => beginCollectionEditor('create'));
  if (nodes.renameCollection) nodes.renameCollection.addEventListener('click', () => beginCollectionEditor('rename'));
  if (nodes.saveCollection) nodes.saveCollection.addEventListener('click', () => { void saveCollectionEditor(); });
  if (nodes.deleteCollection) nodes.deleteCollection.addEventListener('click', () => { void controller.deleteCollection(state.collection); });
  if (nodes.openSettings) nodes.openSettings.addEventListener('click', () => { void openSettings(); });
  if (nodes.closeSettings) nodes.closeSettings.addEventListener('click', closeSettings);
  if (nodes.saveSettings) nodes.saveSettings.addEventListener('click', () => { void saveSettingsFromForm(); });
  if (nodes.settingsAttractIdle) nodes.settingsAttractIdle.addEventListener('input', bumpSettingsGeneration);
  if (nodes.settingsPreferredRegions) nodes.settingsPreferredRegions.addEventListener('input', bumpSettingsGeneration);
  if (typeof document.addEventListener === 'function') {
    document.addEventListener('keydown', event => {
      if (attractActive) {
        event.preventDefault?.();
        exitAttract();
        return;
      }
      if (gameActionsMenuIsOpen()) {
        if (event.key === 'Escape') {
          event.preventDefault?.();
          closeGameActionsMenu({ restoreFocus: true });
          return;
        }
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Home' || event.key === 'End') {
          event.preventDefault?.();
          if (event.key === 'Home') return moveGameActionsMenu(0, 'start');
          if (event.key === 'End') return moveGameActionsMenu(0, 'end');
          return moveGameActionsMenu(event.key === 'ArrowDown' ? 1 : -1);
        }
        if (event.key === 'Enter') {
          event.preventDefault?.();
          const items = gameActionsMenuItems();
          const active = typeof document !== 'undefined' ? document.activeElement : null;
          const item = items.includes(active) ? active : items[0];
          if (item && typeof item.click === 'function') return item.click();
          return;
        }
        if (isGameActionsMenuOpenKey(event)) {
          event.preventDefault?.();
          openGameActionsMenuFromEvent(event);
          return;
        }
        if (isTypeToSearchKey(event)) {
          closeGameActionsMenu({ restoreFocus: false });
        } else {
          return;
        }
      }
      if (settingsIsOpen()) {
        if (event.key === 'Escape') {
          event.preventDefault?.();
          closeSettings();
          return;
        }
        if (event.key === 'Tab') {
          wrapSettingsFocus(event);
          return;
        }
        if (typingTarget(event.target) && isSettingsTarget(event.target)) return;
        if (!isSettingsTarget(event.target)) {
          event.preventDefault?.();
          forceKeyboardRestore = true;
          restoreKeyboardFocus();
          return;
        }
        return;
      }
      if (typingTarget(event.target)) {
        if (isSearchInput(event.target)) {
          if (event.key === 'Escape') {
            event.preventDefault?.();
            if (nodes.search) nodes.search.blur?.();
            setKeyboardPane(state.selectedLiveGame ? catalogPane() : 'rail');
          } else if (event.key === 'ArrowDown' || event.key === 'Enter') {
            event.preventDefault?.();
            setKeyboardPane(catalogPane());
            if (!state.selectedLiveGame && visibleGames().length) {
              void controller.selectGame(visibleGames()[0].live.id);
            }
          }
        } else if (event.target && event.target.id === 'collection-name') {
          if (event.key === 'Escape') {
            event.preventDefault?.();
            cancelCollectionEditor();
          } else if (event.key === 'Enter') {
            event.preventDefault?.();
            void saveCollectionEditor();
          }
        }
        return;
      }
      if (isTypeToSearchKey(event)) {
        event.preventDefault?.();
        typeToSearch(event.key);
        return;
      }
      if (isGameActionsMenuOpenKey(event)) {
        const opened = openGameActionsMenuFromEvent(event);
        if (opened) {
          event.preventDefault?.();
          return;
        }
      }
      if (isCollectionEditor(event.target) || isLayoutToggle(event.target)) return;
      syncPaneFromTarget(event.target);
      if (event.key === 'Escape') {
        event.preventDefault?.();
        if (keyboardPane === 'detail') setKeyboardPane(catalogPane());
        else if (keyboardPane === 'grid' || keyboardPane === 'home') setKeyboardPane('rail');
        else if (keyboardPane === 'search') setKeyboardPane(state.selectedLiveGame ? catalogPane() : 'rail');
        return;
      }
      if (event.key === 'Enter') {
        if (keyboardPane === 'rail') {
          event.preventDefault?.();
          return commitRail();
        }
        if ((keyboardPane === 'grid' || keyboardPane === 'home') && isCoverWallTarget(event.target)) {
          event.preventDefault?.();
          return enterDetail();
        }
        if (keyboardPane === 'detail' && isLaunchTarget(event.target)) {
          event.preventDefault?.();
          return launchSelected();
        }
        if (keyboardPane === 'search' && isSearchInput(event.target)) {
          event.preventDefault?.();
          setKeyboardPane(catalogPane());
        }
        return;
      }
      if (event.key === 'Home' || event.key === 'End') {
        event.preventDefault?.();
        if (keyboardPane === 'rail') {
          const items = railItems();
          if (!items.length) return;
          keyboardPane = 'rail';
          forceKeyboardRestore = true;
          focusWithoutScroll(event.key === 'Home' ? items[0] : items[items.length - 1]);
          writePaneAttribute();
          return;
        }
        if (keyboardPane === 'home') {
          return moveHome(0, 0, event.key === 'Home' ? 'start' : 'end');
        }
        return jumpWall(event.key === 'Home' ? 'start' : 'end');
      }
      if (event.key === 'ArrowRight' || event.key === 'ArrowLeft' || event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault?.();
        if (keyboardPane === 'rail') {
          if (event.key === 'ArrowRight') return commitRail();
          if (event.key === 'ArrowDown') moveRail(1);
          else if (event.key === 'ArrowUp') moveRail(-1);
          return;
        }
        if (keyboardPane === 'detail') {
          if (event.key === 'ArrowLeft') setKeyboardPane(catalogPane());
          return;
        }
        if (keyboardPane === 'home' || (isHomeView() && keyboardPane !== 'grid')) {
          keyboardPane = 'home';
          if (event.key === 'ArrowRight') return moveHome(0, 1);
          if (event.key === 'ArrowLeft') return moveHome(0, -1);
          if (event.key === 'ArrowDown') return moveHome(1, 0);
          if (event.key === 'ArrowUp') return moveHome(-1, 0);
          return;
        }
        if (keyboardPane !== 'grid') setKeyboardPane('grid');
        return moveGrid(event.key);
      }
    });
    document.addEventListener('pointerdown', event => {
      if (gameActionsMenuIsOpen() && !isGameActionsMenuTarget(event && event.target)) {
        closeGameActionsMenu({ restoreFocus: false });
      }
      exitAttract();
    });
    if (!(Number(root.FogCastAttractIdleMs) > 0)) {
      document.addEventListener('mousemove', resetAttractTimer);
    }
    document.addEventListener('focusin', event => {
      if (gameActionsMenuIsOpen() && event && event.target && !isGameActionsMenuTarget(event.target)) {
        closeGameActionsMenu({ restoreFocus: false });
      }
      if (settingsIsOpen()) {
        if (event && event.target && !isSettingsTarget(event.target)) {
          forceKeyboardRestore = true;
          restoreKeyboardFocus();
        }
        resetAttractTimer();
        return;
      }
      if (attractActive) exitAttract();
      else resetAttractTimer();
      syncPaneFromTarget(event && event.target);
    });
  }
  if (nodes.attract) {
    nodes.attract.hidden = true;
    nodes.attract.addEventListener('click', exitAttract);
  }
  if (nodes.settings) nodes.settings.hidden = true;
  if (nodes.gameActions) nodes.gameActions.hidden = true;
  renderCatalog();
  renderLibraryNav();
  renderDetail(null);
  renderSession();
  writePaneAttribute();
  void loadSession();
  void controller.openHome();
  void controller.loadPlatforms();
  void hydrateAttractIdle();
})(globalThis);
