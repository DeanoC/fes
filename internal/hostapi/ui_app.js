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
    if (extra.sort && extra.sort !== 'title') {
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

  function cardTitle(game) {
    if (!game) return '';
    if (game.canonical_title) return game.canonical_title;
    return displayTitle(game.title);
  }

  function variantLabel(game) {
    const parts = [];
    if (game && game.region) parts.push(game.region);
    if (game && game.revision) parts.push(`rev ${game.revision}`);
    if (game && game.dump_flags) parts.push(game.dump_flags.replace(/,/g, ', '));
    return parts.join(' · ') || game.title || 'Dump';
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
    if (presentation && !presentation.isFallback && presentation.genre && presentation.genre !== 'Unknown') {
      return presentation.genre;
    }
    const live = view && view.live;
    return live && live.genre ? live.genre : '';
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
    if (state.games.length === 0) return state.query ? 'no_matches' : 'empty';
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
      nextCursor: '',
      loadingMore: false,
      platforms: [],
      attractIdleSeconds: 60,
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

    function sessionTitle() {
      if (!state.session || state.session.state !== 'active' || !state.session.game_id) return '';
      const game = state.games.find(candidate => candidate.id === state.session.game_id);
      return game ? game.title : '';
    }

    function snapshot() {
      return {
        ...state,
        games: state.games.slice(),
        gameViews: state.gameViews.slice(),
        selectedLiveGame: state.selectedLiveGame,
        selectedGameView: state.selectedGameView,
        sessionGameTitle: sessionTitle(),
      };
    }

    function emit() {
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

    function applyCatalogPresentation(id, presentation) {
      presentationByID[id] = presentation;
      const index = state.games.findIndex(game => game.id === id);
      if (index < 0) return;
      const nextViews = state.gameViews.slice();
      nextViews[index] = Object.freeze({ live: state.games[index], presentation });
      state.gameViews = Object.freeze(nextViews);
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
        return emit();
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
        if (!operationError && reconciled.state === 'active' && reconciled.game_id === mutation.requestedID) {
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
      return emit();
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

    function catalogExtras(cursor) {
      const extras = { grouped: 1 };
      if (state.collection) extras.collection = state.collection;
      if (state.platformQuery) extras.platform = state.platformQuery;
      if (state.filters.region) extras.region = state.filters.region;
      if (state.filters.genre) extras.genre = state.filters.genre;
      if (state.filters.year) extras.year = state.filters.year;
      if (state.filters.hide_prerelease) extras.hide_prerelease = 1;
      if (state.filters.hide_hacks) extras.hide_hacks = 1;
      if (state.filters.availability) extras.availability = state.filters.availability;
      if (state.sort && state.sort !== 'title') extras.sort = state.sort;
      if (cursor) extras.cursor = cursor;
      return extras;
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
        if (state.selectedLiveGame && state.selectedLiveGame.id === updated.id) {
          state.selectedLiveGame = updated;
          state.selectedGameView = views[index];
        }
      } else if (state.selectedLiveGame && state.selectedLiveGame.id === updated.id) {
        state.selectedLiveGame = updated;
        if (state.selectedGameView) {
          state.selectedGameView = Object.freeze({ live: updated, presentation: state.selectedGameView.presentation });
        }
      }
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
        return emit();
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
        return emit();
      } catch (error) {
        if (sequence !== state.requestSequence) return snapshot();
        return emit();
      } finally {
        state.loadingMore = false;
      }
    }

    async function setLibraryNav(collection, platform) {
      const allowed = {
        favorites: true, recents: true, continue: true, unplayed: true, recently_added: true,
      };
      state.collection = allowed[collection] ? collection : '';
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
        const attract = await request(fetchImpl, '/api/v1/library/attract?limit=1');
        if (Number.isFinite(attract && attract.idle_seconds) && attract.idle_seconds > 0) {
          state.attractIdleSeconds = attract.idle_seconds;
        }
      } catch (_) {
        /* attract idle stays at the last known value */
      }
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
        return emit();
      } catch (error) {
        return emit();
      }
    }

    async function loadAttract(limit) {
      const size = Number(limit) > 0 ? Number(limit) : 24;
      const payload = await request(fetchImpl, `/api/v1/library/attract?limit=${encodeURIComponent(String(size))}`);
      const items = payload && Array.isArray(payload.items) ? payload.items : [];
      const idle = Number.isFinite(payload && payload.idle_seconds) ? payload.idle_seconds : 60;
      if (idle > 0) state.attractIdleSeconds = idle;
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
        const next = String(value || '').trim();
        state.filters = Object.freeze({ ...state.filters, system: next });
        state.platformQuery = next;
        return loadCatalog(state.query);
      }
      if (name === 'region' || name === 'genre' || name === 'year' || name === 'availability') {
        state.filters = Object.freeze({ ...state.filters, [name]: String(value || '').trim() });
        return loadCatalog(state.query);
      }
      if (name === 'hide_prerelease' || name === 'hide_hacks') {
        const enabled = value === true || value === '1' || value === 'true';
        state.filters = Object.freeze({ ...state.filters, [name]: enabled });
        return loadCatalog(state.query);
      }
      return snapshot();
    }

    function setCatalogSort(value) {
      state.sort = value === 'year' || value === 'system' || value === 'recently_added' ? value : 'title';
      return loadCatalog(state.query);
    }

    return Object.freeze({
      getState: snapshot,
      loadCatalog,
      loadMoreCatalog,
      loadPlatforms,
      loadAttract,
      loadSession,
      selectGame,
      refreshDetail,
      refreshPresentation,
      launchSelected,
      stopSession,
      observeVisibleCovers,
      setCatalogFilter,
      setCatalogSort,
      setLibraryNav,
      toggleFavorite,
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
    filterCatalogViews,
    sortCatalogViews,
    formatCatalogCount,
    fallbackPresentation,
    displayTitle,
    cardTitle,
    variantLabel,
    systemLabel,
    sourceLabel,
    launchBlockReason,
  });
  root.FogCastApp = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;

  if (typeof document === 'undefined') return;

  let state;
  let controller;

  const nodes = {
    health: document.getElementById('health'),
    search: document.getElementById('game-search'),
    refresh: document.getElementById('refresh-catalog'),
    systemFilter: document.getElementById('filter-system'),
    regionFilter: document.getElementById('filter-region'),
    genreFilter: document.getElementById('filter-genre'),
    yearFilter: document.getElementById('filter-year'),
    sortFilter: document.getElementById('catalog-sort'),
    hidePrerelease: document.getElementById('filter-hide-prerelease'),
    hideHacks: document.getElementById('filter-hide-hacks'),
    availabilityFilter: document.getElementById('filter-availability'),
    count: document.getElementById('catalog-count'),
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
    navAll: document.getElementById('nav-all'),
    navFavorites: document.getElementById('nav-favorites'),
    navRecents: document.getElementById('nav-recents'),
    navContinue: document.getElementById('nav-continue'),
    navUnplayed: document.getElementById('nav-unplayed'),
    navRecentlyAdded: document.getElementById('nav-recently-added'),
    platformList: document.getElementById('platform-list'),
    attract: document.getElementById('attract'),
    attractTitle: document.getElementById('attract-title'),
    attractStage: document.getElementById('attract-stage'),
  };

  function element(tag, className, text) {
    const result = document.createElement(tag);
    if (className) result.className = className;
    if (text !== undefined) result.textContent = String(text);
    return result;
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

  function renderSession() {
    ensureSessionActions();
    const busy = Boolean(state.activeMutation) || state.sessionPhase === 'loading';
    nodes.sessionPanel.setAttribute('aria-busy', String(busy));
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

  function navSelected(kind, platform) {
    if (kind === 'favorites') return state.collection === 'favorites' && !state.platformQuery;
    if (kind === 'recents') return state.collection === 'recents' && !state.platformQuery;
    if (kind === 'continue') return state.collection === 'continue' && !state.platformQuery;
    if (kind === 'unplayed') return state.collection === 'unplayed' && !state.platformQuery;
    if (kind === 'recently_added') return state.collection === 'recently_added' && !state.platformQuery;
    if (kind === 'platform') return Boolean(platform) && state.platformQuery === platform;
    return !state.collection && !state.platformQuery;
  }

  function renderLibraryNav() {
    if (nodes.navAll) nodes.navAll.className = 'nav-item' + (navSelected('all') ? ' selected' : '');
    if (nodes.navContinue) nodes.navContinue.className = 'nav-item' + (navSelected('continue') ? ' selected' : '');
    if (nodes.navFavorites) nodes.navFavorites.className = 'nav-item' + (navSelected('favorites') ? ' selected' : '');
    if (nodes.navRecents) nodes.navRecents.className = 'nav-item' + (navSelected('recents') ? ' selected' : '');
    if (nodes.navUnplayed) nodes.navUnplayed.className = 'nav-item' + (navSelected('unplayed') ? ' selected' : '');
    if (nodes.navRecentlyAdded) nodes.navRecentlyAdded.className = 'nav-item' + (navSelected('recently_added') ? ' selected' : '');
    if (!nodes.platformList) return;
    nodes.platformList.replaceChildren();
    (state.platforms || []).forEach(platform => {
      const button = element('button', 'nav-item' + (navSelected('platform', platform.id) ? ' selected' : ''));
      button.type = 'button';
      button.setAttribute('data-platform', platform.id);
      button.appendChild(element('span', '', platform.label || systemLabel(platform.id)));
      button.appendChild(element('span', 'platform-count', String(platform.game_count || 0)));
      button.addEventListener('click', () => controller.setLibraryNav('', platform.id));
      nodes.platformList.appendChild(button);
    });
  }

  let wallObserver = null;
  let wallStartObserver = null;
  let wallStart = 0;

  function wallColumns() {
    const width = Number(nodes.list && nodes.list.clientWidth) || 210;
    return Math.max(1, Math.floor((width + 14) / 224));
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
  }

  function appendWallSpacer(count, edge) {
    if (count <= 0) return null;
    const spacer = element('div', 'wall-spacer');
    spacer.setAttribute('data-wall-spacer', edge);
    const rows = Math.ceil(count / wallColumns());
    spacer.style.height = `${rows * 264}px`;
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

  function renderCatalog() {
    const view = catalogViewState(state);
    nodes.catalog.setAttribute('aria-busy', view === 'loading' ? 'true' : 'false');
    nodes.list.replaceChildren();
    nodes.actions.replaceChildren();
    nodes.status.textContent = view;
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
      nodes.list.appendChild(element('p', 'status-message', state.query ? 'No matching games.' : 'The library is empty.'));
      nodes.actions.appendChild(retryButton('Refresh catalog', loadCatalog));
      return;
    }
    nodes.status.textContent = state.metadataFallbackCount ? 'populated metadata_fallback' : 'populated';
    syncCatalogFilters();
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

  function syncCatalogFilters() {
    if (nodes.regionFilter) nodes.regionFilter.value = state.filters && state.filters.region || '';
    if (nodes.sortFilter) nodes.sortFilter.value = state.sort || 'title';
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

  function renderCard(view, liveGame) {
    const game = view.live;
    const presentation = view.presentation;
    const selected = isSelectedGame(state.selectedLiveGame, game);
    const card = element('button', 'game-card' + (selected ? ' selected' : ''));
    card.type = 'button';
    card.setAttribute('aria-pressed', String(selected));
    card.setAttribute('data-game-id', game.id);
    card.setAttribute('data-system', game.system);
    card.setAttribute('data-state', game.state || '');
    const coverHandle = presentation.coverArtworkHandle || game.cover;
    const cover = artworkElement('cover', presentation.cover, coverHandle, 'eager');
    card.appendChild(cover);
    card.appendChild(element('h3', '', cardTitle(game)));
    card.appendChild(element('p', 'game-meta', `${systemLabel(game.system)} · ${sourceLabel(game.state)}`));
    if (presentation.isFallback) {
      card.appendChild(element('p', 'fallback-note', 'Using local catalog data'));
    }
    if (game.variant_count > 1) {
      card.appendChild(element('p', 'game-meta', `${game.variant_count} versions`));
    }
    if (game.title && cardTitle(game) !== game.title) card.setAttribute('title', game.title);
    card.addEventListener('click', () => selectGame(liveGame.id));
    nodes.list.appendChild(card);
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
      nodes.detailContent.appendChild(element('p', 'eyebrow', 'Game'));
    }
    const heading = element('h2', '', detailHeading(game));
    heading.id = 'detail-heading';
    nodes.detailContent.appendChild(heading);
    if (!game) {
      nodes.detailContent.appendChild(element('p', 'muted', 'Choose a game.'));
      return;
    }
    if (presentation.summary) nodes.detailContent.appendChild(element('p', 'detail-summary', presentation.summary));
    if (presentation.isFallback) {
      nodes.detailContent.appendChild(element('p', 'fallback-note', 'Using local catalog data'));
      nodes.detailContent.appendChild(element('p', 'sr-only', 'metadata_fallback'));
    }
    if (presentation.attribution) nodes.detailContent.appendChild(element('p', 'attribution', presentation.attribution));
    const facts = element('div', 'detail-facts');
    [[systemLabel(game.system), 'System'], [sourceLabel(game.state), 'Status'], [game.content_prepared ? 'Prepared' : 'On demand', 'Staging'], [presentation.year !== '—' ? presentation.year : '', 'Year'], [presentation.players, 'Players']]
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
    if (presentation.logoHandle) nodes.detailContent.appendChild(artworkElement('logo', null, presentation.logoHandle, 'lazy'));
    if (presentation.marqueeHandle) {
      nodes.detailContent.appendChild(artworkElement('marquee', null, presentation.marqueeHandle, 'lazy'));
    }
    if (presentation.screenshotHandles && presentation.screenshotHandles.length) {
      const stills = element('div', 'extra-stills');
      presentation.screenshotHandles.forEach(handle => stills.appendChild(artworkElement('screenshot', null, handle, 'lazy')));
      nodes.detailContent.appendChild(stills);
    }
    if (presentation.videoHandle) {
      const video = element('video', 'detail-video');
      video.setAttribute('src', mediaPath(presentation.videoHandle));
      video.setAttribute('controls', '');
      video.muted = true;
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
    launch.addEventListener('click', launchSelected);
    nodes.launchActions.appendChild(launch);
    nodes.launchActions.appendChild(reason);
  }

  function loadCatalog() {
    return controller.loadCatalog(nodes.search.value);
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

  function libraryNavChanged(previous, next) {
    if (!previous) return true;
    return previous.collection !== next.collection
      || previous.platformQuery !== next.platformQuery
      || previous.platforms !== next.platforms;
  }

  function handleStateChange(next) {
    const previous = state;
    state = next;
    if (next.hostState === 'ready') setHealth('Local host ready', 'host-status');
    if (next.hostState === 'unavailable') setHealth('Catalog unavailable', 'host-status');
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
    if (Number(root.FogCastAttractIdleMs) > 0) return Number(root.FogCastAttractIdleMs);
    const seconds = Number(state && state.attractIdleSeconds);
    return seconds > 0 ? seconds * 1000 : 60000;
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
    if (attractIsDisabled() || attractActive || !nodes.attract) return;
    try {
      const playlist = await controller.loadAttract(24);
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
    if (attractIsDisabled() || !nodes.attract) return;
    if (typeof root.setTimeout !== 'function') return;
    attractTimer = root.setTimeout(enterAttract, currentAttractIdleMs());
  }

  function exitAttract() {
    if (!attractActive) {
      resetAttractTimer();
      return;
    }
    hideAttract();
    resetAttractTimer();
  }

  function typingTarget(target) {
    const tag = String(target && target.tagName || '').toUpperCase();
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
  }

  function visibleGames() {
    return state.gameViews || [];
  }

  function moveWall(delta) {
    const visible = visibleGames();
    if (!visible.length) return;
    const current = visible.findIndex(item => isSelectedGame(state.selectedLiveGame, item.live));
    const next = current < 0 ? 0 : Math.max(0, Math.min(visible.length - 1, current + delta));
    controller.selectGame(visible[next].live.id);
  }

  controller = createAppController({
    metadataAdapter: root.FogCastMetadata,
    onStateChange: handleStateChange,
  });
  state = controller.getState();
  nodes.refresh.addEventListener('click', loadCatalog);
  let searchTimer = null;
  nodes.search.addEventListener('input', () => {
    if (searchTimer) root.clearTimeout(searchTimer);
    searchTimer = root.setTimeout(loadCatalog, 180);
  });
  if (nodes.systemFilter) nodes.systemFilter.addEventListener('change', () => controller.setCatalogFilter('system', nodes.systemFilter.value));
  if (nodes.regionFilter) nodes.regionFilter.addEventListener('change', () => controller.setCatalogFilter('region', nodes.regionFilter.value));
  if (nodes.genreFilter) nodes.genreFilter.addEventListener('change', () => controller.setCatalogFilter('genre', nodes.genreFilter.value));
  if (nodes.yearFilter) nodes.yearFilter.addEventListener('change', () => controller.setCatalogFilter('year', nodes.yearFilter.value));
  if (nodes.sortFilter) nodes.sortFilter.addEventListener('change', () => controller.setCatalogSort(nodes.sortFilter.value));
  if (nodes.hidePrerelease) nodes.hidePrerelease.addEventListener('change', () => controller.setCatalogFilter('hide_prerelease', nodes.hidePrerelease.checked));
  if (nodes.hideHacks) nodes.hideHacks.addEventListener('change', () => controller.setCatalogFilter('hide_hacks', nodes.hideHacks.checked));
  if (nodes.availabilityFilter) nodes.availabilityFilter.addEventListener('change', () => controller.setCatalogFilter('availability', nodes.availabilityFilter.value));
  if (nodes.navAll) nodes.navAll.addEventListener('click', () => controller.setLibraryNav('', ''));
  if (nodes.navContinue) nodes.navContinue.addEventListener('click', () => controller.setLibraryNav('continue', ''));
  if (nodes.navFavorites) nodes.navFavorites.addEventListener('click', () => controller.setLibraryNav('favorites', ''));
  if (nodes.navRecents) nodes.navRecents.addEventListener('click', () => controller.setLibraryNav('recents', ''));
  if (nodes.navUnplayed) nodes.navUnplayed.addEventListener('click', () => controller.setLibraryNav('unplayed', ''));
  if (nodes.navRecentlyAdded) nodes.navRecentlyAdded.addEventListener('click', () => controller.setLibraryNav('recently_added', ''));
  if (typeof document.addEventListener === 'function') {
    document.addEventListener('keydown', event => {
      if (attractActive) {
        event.preventDefault?.();
        exitAttract();
        return;
      }
      if (typingTarget(event.target)) return;
      if (event.key === 'Escape') {
        exitAttract();
        return;
      }
      if (event.key === 'f' || event.key === 'F') {
        controller.toggleFavorite();
        return;
      }
      if (event.key === 'Enter') {
        launchSelected();
        return;
      }
      if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
        event.preventDefault?.();
        moveWall(1);
        return;
      }
      if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
        event.preventDefault?.();
        moveWall(-1);
      }
    });
    document.addEventListener('pointerdown', exitAttract);
    if (!(Number(root.FogCastAttractIdleMs) > 0)) {
      document.addEventListener('mousemove', resetAttractTimer);
    }
    document.addEventListener('focusin', () => {
      if (attractActive) exitAttract();
      else resetAttractTimer();
    });
  }
  if (nodes.attract) {
    nodes.attract.hidden = true;
    nodes.attract.addEventListener('click', exitAttract);
  }
  renderCatalog();
  renderLibraryNav();
  renderDetail(null);
  renderSession();
  void loadSession();
  void loadCatalog();
  void controller.loadPlatforms();
  resetAttractTimer();
})(globalThis);
