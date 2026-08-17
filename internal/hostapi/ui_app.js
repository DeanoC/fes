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
    cover: Object.freeze({ palette: 'ember', treatment: 'grid' }),
    backdrop: Object.freeze({ palette: 'ember', treatment: 'grid' }),
    summary: 'Presentation metadata is unavailable; using deterministic demo artwork.',
    year: '—',
    genre: 'Unknown',
    studio: 'FogCast demo',
    players: 'Unknown players',
    isFallback: true,
    metadataState: 'fallback_offline',
  });
  const PRESENTATION_STATE_ALLOWLIST = Object.freeze([
    'ready', 'fallback_disabled', 'fallback_unconfigured', 'fallback_no_match',
    'fallback_ambiguous', 'fallback_offline', 'fallback_malformed', 'ready_artwork_error',
  ]);

  function gamesPath(query) {
    const value = String(query || '').trim();
    return value ? `/api/v1/games?q=${encodeURIComponent(value)}` : '/api/v1/games';
  }

  function gameDetailPath(id) {
    return `/api/v1/games/${encodeURIComponent(String(id))}`;
  }

  const GAME_ID_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
  const ARTWORK_HANDLE_PATTERN = /^[a-f0-9]{64}$/;

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
    return Boolean(selectedLiveGame && game && selectedLiveGame.id === game.id);
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
    return game ? String(game.title) : 'Select a game';
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

  function snapshotLiveGame(game) {
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

  function catalogGenre(view) {
    const presentation = view && view.presentation;
    if (presentation && !presentation.isFallback && presentation.genre && presentation.genre !== 'Unknown') {
      return presentation.genre;
    }
    const live = view && view.live;
    return live && live.genre ? live.genre : '';
  }

  function filterCatalogViews(views, filters) {
    const system = filters && filters.system || '';
    const region = filters && filters.region || '';
    const genre = filters && filters.genre || '';
    return (views || []).filter(view => {
      const live = view && view.live || {};
      if (system && live.system !== system) return false;
      if (region && catalogRegion(live.title) !== region) return false;
      if (genre && catalogGenre(view) !== genre) return false;
      return true;
    });
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
  const SESSION_SYSTEM_ALLOWLIST = Object.freeze(['megadrive', 'snes']);
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
    if (system !== undefined && !SESSION_SYSTEM_ALLOWLIST.includes(system)) {
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
      const attribution = value.attribution;
      const metadataState = value.metadataState || (isFallback ? 'fallback_offline' : 'ready');
      const artworkStyleRequired = isFallback;
      if (
        typeof isFallback !== 'boolean'
        || !PRESENTATION_STATE_ALLOWLIST.includes(metadataState)
        || (artworkStyleRequired && (
          !cover || typeof cover !== 'object' || Array.isArray(cover)
          || !backdrop || typeof backdrop !== 'object' || Array.isArray(backdrop)
          || typeof coverPalette !== 'string'
          || typeof coverTreatment !== 'string'
          || typeof backdropPalette !== 'string'
          || typeof backdropTreatment !== 'string'
          || !PALETTE_ALLOWLIST.includes(coverPalette)
          || !PALETTE_ALLOWLIST.includes(backdropPalette)
          || !TREATMENT_ALLOWLIST.includes(coverTreatment)
          || !TREATMENT_ALLOWLIST.includes(backdropTreatment)
        ))
        || (!artworkStyleRequired && (cover !== undefined || backdrop !== undefined))
        || (!isFallback && !['ready', 'ready_artwork_error'].includes(metadataState))
        || (isFallback && !metadataState.startsWith('fallback_'))
        || (coverArtworkHandle !== undefined && (typeof coverArtworkHandle !== 'string' || !ARTWORK_HANDLE_PATTERN.test(coverArtworkHandle)))
        || (backdropArtworkHandle !== undefined && (typeof backdropArtworkHandle !== 'string' || !ARTWORK_HANDLE_PATTERN.test(backdropArtworkHandle)))
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
        attribution,
      });
    } catch (_) {
      return fallbackPresentation();
    }
  }

  function optionalArtworkHandle(value) {
    if (value === undefined || value === null || value === '') return undefined;
    return value;
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
      if (Object.prototype.hasOwnProperty.call(stateMap, payload.state)) return fallback(stateMap[payload.state]);
      if (payload.state !== 'ready') return fallback('fallback_malformed');
      if (!payload.presentation || typeof payload.presentation !== 'object' || Array.isArray(payload.presentation)) return fallback('fallback_malformed');
      if (!payload.attribution || typeof payload.attribution !== 'object' || Array.isArray(payload.attribution)
        || !acceptedAttribution(payload.attribution)) return fallback('fallback_malformed');
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
      presentation: trustedPresentation(presentation),
    });
  }

  function catalogViewState(state) {
    if (state.catalogState === 'loading') return 'loading';
    if (state.catalogState === 'catalog_error') return 'catalog_error';
    if (state.games.length === 0) return state.query ? 'no_matches' : 'empty';
    if (filterCatalogViews(state.gameViews, state.filters).length === 0) return 'no_matches';
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
    let coverObserver = null;
    const state = {
      query: '',
      games: [],
      gameViews: [],
      filters: Object.freeze({ system: '', region: '', genre: '' }),
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

    function applyCatalogPresentation(id, presentation) {
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
      if (!prefetchVisibleCovers || !presentationEnabled || !id || coverRequests[id]) return;
      const index = state.games.findIndex(game => game.id === id);
      if (index < 0) return;
      const current = state.gameViews[index] && state.gameViews[index].presentation;
      if (current && (current.coverArtworkHandle || current.metadataState === 'ready')) return;
      coverRequests[id] = true;
      request(fetchImpl, presentationPath(id)).then(payload => {
        const game = state.games.find(item => item.id === id);
        if (!game) return;
        applyCatalogPresentation(id, parsePresentation(payload, game));
        emit();
      }).catch(() => {
        delete coverRequests[id];
      });
    }

    function observeVisibleCovers() {
      if (!prefetchVisibleCovers || !presentationEnabled) return;
      const Observer = options.IntersectionObserver || root.IntersectionObserver;
      let cards = [];
      if (typeof document !== 'undefined' && document && typeof document.getElementById === 'function') {
        const list = document.getElementById('catalog-list');
        if (list && typeof list.querySelectorAll === 'function') {
          cards = list.querySelectorAll('.game-card[data-game-id]');
        }
      }
      if (typeof Observer !== 'function' || !cards.length) {
        state.games.forEach(game => queueVisiblePresentation(game.id));
        return;
      }
      if (coverObserver) coverObserver.disconnect();
      coverObserver = new Observer(entries => {
        entries.forEach(entry => {
          if (!entry.isIntersecting) return;
          const id = entry.target.getAttribute('data-game-id');
          if (id) queueVisiblePresentation(id);
        });
      }, { root: null, rootMargin: '200px', threshold: 0.01 });
      Array.prototype.forEach.call(cards, card => {
        coverObserver.observe(card);
      });
    }

    function enrich(game) {
      return launcherGame(game, presentationEnabled ? null : metadataAdapter);
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
      const index = state.games.findIndex(game => game.id === state.selectedLiveGame.id);
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
      if (!selected || selected.state !== 'available' || !selected.content_prepared) return false;
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

    async function loadCatalog(query) {
      const sequence = ++state.requestSequence;
      state.query = String(query || '').trim();
      state.catalogState = 'loading';
      state.catalogError = null;
      emit();
      try {
        const result = await request(fetchImpl, gamesPath(state.query));
        if (sequence !== state.requestSequence) return snapshot();
        const games = parseCatalog(result);
        const gameViews = Object.freeze(games.map(game => enrich(game)));
        state.games = games;
        state.gameViews = gameViews;
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
      const fresh = state.games.find(game => game.id === id);
      if (!fresh) return snapshot();
      const index = state.games.indexOf(fresh);
      resetSelectionState();
      state.selectedLiveGame = fresh;
      state.selectedGameView = state.gameViews[index] || null;
      state.detailState = 'loading';
      emit();
      if (!presentationEnabled) return refreshDetail(id);
      await Promise.all([refreshDetail(id), refreshPresentation(id)]);
      return snapshot();
    }

    function setCatalogFilter(name, value) {
      if (name !== 'system' && name !== 'region' && name !== 'genre') return snapshot();
      state.filters = Object.freeze({
        system: state.filters.system,
        region: state.filters.region,
        genre: state.filters.genre,
        [name]: String(value || '').trim(),
      });
      return emit();
    }

    return Object.freeze({
      getState: snapshot,
      loadCatalog,
      loadSession,
      selectGame,
      refreshDetail,
      refreshPresentation,
      launchSelected,
      stopSession,
      observeVisibleCovers,
      setCatalogFilter,
    });
  }

  const api = Object.freeze({
    gamesPath,
    gameDetailPath,
    presentationPath,
    artworkPath,
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

  function renderCatalog() {
    const view = catalogViewState(state);
    nodes.catalog.setAttribute('aria-busy', view === 'loading' ? 'true' : 'false');
    nodes.list.replaceChildren();
    nodes.actions.replaceChildren();
    nodes.status.textContent = view;
    if (view === 'loading') {
      nodes.list.appendChild(element('p', 'status-message', 'Loading the live catalog…'));
      return;
    }
    if (view === 'catalog_error') {
      nodes.list.appendChild(element('p', 'status-message error', 'The catalog could not be loaded.'));
      nodes.actions.appendChild(retryButton('Retry catalog', loadCatalog));
      return;
    }
    if (view === 'empty' || view === 'no_matches') {
      nodes.list.appendChild(element('p', 'status-message', state.query ? 'No matches in the live catalog.' : 'The live catalog is empty.'));
      nodes.actions.appendChild(retryButton('Refresh catalog', loadCatalog));
      return;
    }
    nodes.status.textContent = state.metadataFallbackCount ? 'populated metadata_fallback' : 'populated';
    syncCatalogFilters();
    const visible = filterCatalogViews(state.gameViews, state.filters);
    visible.forEach(view => renderCard(view, view.live));
  }

  function syncCatalogFilters() {
    if (nodes.systemFilter) nodes.systemFilter.value = state.filters && state.filters.system || '';
    if (nodes.regionFilter) nodes.regionFilter.value = state.filters && state.filters.region || '';
    if (!nodes.genreFilter) return;
    const selected = state.filters && state.filters.genre || '';
    const seen = Object.create(null);
    const genres = [];
    (state.gameViews || []).forEach(view => {
      const genre = catalogGenre(view);
      if (!genre || seen[genre]) return;
      seen[genre] = true;
      genres.push(genre);
    });
    genres.sort((left, right) => left.localeCompare(right));
    nodes.genreFilter.replaceChildren();
    const all = element('option', '', 'All genres');
    all.value = '';
    nodes.genreFilter.appendChild(all);
    genres.forEach(genre => {
      const option = element('option', '', genre);
      option.value = genre;
      nodes.genreFilter.appendChild(option);
    });
    nodes.genreFilter.value = selected;
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
    const cover = artworkElement('cover', presentation.cover, presentation.coverArtworkHandle, 'lazy');
    card.appendChild(cover);
    card.appendChild(element('h3', '', game.title));
    card.appendChild(element('p', 'game-meta', `${game.system} · ${game.state}`));
    if (presentation.isFallback) card.appendChild(element('p', 'fallback-note', 'Demo presentation fallback'));
    card.addEventListener('click', () => selectGame(liveGame.id));
    nodes.list.appendChild(card);
  }

  function launchControl(game) {
    let label = 'Launch live game';
    let reason = '';
    if (!game) return { label, reason: 'Select a live catalog game first.', enabled: false };
    if (game.state !== 'available' || !game.content_prepared) {
      reason = 'The selected live game is not ready to launch.';
    } else if (state.activeMutation) {
      reason = 'A session transition is already in progress.';
    } else if (state.sessionStarted && !state.session) {
      reason = state.sessionPhase === 'unavailable'
        ? 'Session status is unavailable; retry before launching.'
        : 'Session status is not ready; wait for the host check to finish.';
    } else if (state.sessionStarted && !['idle', 'stopped', 'active'].includes(state.sessionPhase)) {
      reason = state.sessionPhase === 'malformed'
        ? 'Session status was malformed; retry before launching.'
        : 'The current session transition must finish before launching.';
    } else if (state.session && state.session.state === 'active' && state.session.game_id === game.id) {
      reason = 'Already active.';
    } else if (state.session && state.session.state === 'active') {
      label = 'Replace active session';
    }
    return { label, reason, enabled: reason === '' };
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
      nodes.detailContent.appendChild(element('p', 'eyebrow', 'Live catalog detail'));
    }
    const heading = element('h2', '', detailHeading(game));
    heading.id = 'detail-heading';
    nodes.detailContent.appendChild(heading);
    if (!game) {
      nodes.detailContent.appendChild(element('p', 'muted', 'Choose a live catalog entry to inspect its launch readiness.'));
      return;
    }
    if (presentation.summary) nodes.detailContent.appendChild(element('p', 'detail-summary', presentation.summary));
    if (presentation.attribution) nodes.detailContent.appendChild(element('p', 'attribution', presentation.attribution));
    const facts = element('div', 'detail-facts');
    [[game.system, 'System'], [game.state, 'Source state'], [presentation.year, 'Year'], [presentation.players, 'Players']]
      .filter(([value]) => value)
      .forEach(([value, label]) => {
      const fact = element('div', 'detail-fact');
      fact.appendChild(element('strong', '', value));
      fact.appendChild(element('span', '', label));
      facts.appendChild(fact);
      });
    nodes.detailContent.appendChild(facts);
    if (presentation.isFallback) nodes.detailContent.appendChild(element('p', 'fallback-note', 'metadata_fallback: using deterministic demo presentation'));
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

  function handleStateChange(next) {
    const previous = state;
    state = next;
    if (next.hostState === 'ready') setHealth('Local host ready', 'host-status');
    if (next.hostState === 'unavailable') setHealth('Catalog unavailable', 'host-status');
    renderCatalog();
    if (controller && typeof controller.observeVisibleCovers === 'function') controller.observeVisibleCovers();
    renderDetail(state.selectedGameView, state.selectedLiveGame);
    renderSession();
    if (previous && previous.activeMutation !== 'launch' && next.activeMutation === 'launch') {
      focusWithoutScroll(nodes.sessionPanel);
    }
    if (previous && previous.activeMutation === 'stop' && next.sessionPhase === 'stopped') {
      focusWithoutScroll(refreshSessionButton);
    }
  }

  controller = createAppController({
    metadataAdapter: root.FogCastMetadata,
    onStateChange: handleStateChange,
  });
  state = controller.getState();
  nodes.refresh.addEventListener('click', loadCatalog);
  nodes.search.addEventListener('input', loadCatalog);
  if (nodes.systemFilter) nodes.systemFilter.addEventListener('change', () => controller.setCatalogFilter('system', nodes.systemFilter.value));
  if (nodes.regionFilter) nodes.regionFilter.addEventListener('change', () => controller.setCatalogFilter('region', nodes.regionFilter.value));
  if (nodes.genreFilter) nodes.genreFilter.addEventListener('change', () => controller.setCatalogFilter('genre', nodes.genreFilter.value));
  renderCatalog();
  renderDetail(null);
  renderSession();
  void loadSession();
  void loadCatalog();
})(globalThis);
