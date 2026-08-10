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
  });

  function gamesPath(query) {
    const value = String(query || '').trim();
    return value ? `/api/v1/games?q=${encodeURIComponent(value)}` : '/api/v1/games';
  }

  function gameDetailPath(id) {
    return `/api/v1/games/${encodeURIComponent(String(id))}`;
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
    return Object.freeze({
      id,
      title,
      system,
      kind,
      state,
      root_online: rootOnline,
      content_prepared: contentPrepared,
      execution,
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

  function validateLaunchSuccess(payload, requestedID) {
    if (!payload || typeof payload !== 'object' || Array.isArray(payload) || payload.state !== 'active') {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    if (
      Object.prototype.hasOwnProperty.call(payload, 'game_id')
      && (typeof payload.game_id !== 'string' || payload.game_id !== requestedID)
    ) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    if (!['execution', 'media'].every(field => validOptionalString(payload, field))) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    if (
      Object.prototype.hasOwnProperty.call(payload, 'system')
      && payload.system !== undefined
      && payload.system !== null
      && typeof payload.system !== 'string'
    ) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    if (
      Object.prototype.hasOwnProperty.call(payload, 'progress')
      && payload.progress !== undefined
      && payload.progress !== null
      && (typeof payload.progress !== 'object' || Array.isArray(payload.progress))
    ) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    if (
      Object.prototype.hasOwnProperty.call(payload, 'input')
      && payload.input !== undefined
      && payload.input !== null
      && (typeof payload.input !== 'object' || Array.isArray(payload.input))
    ) {
      throw createError('MALFORMED_RESPONSE', 'The local host returned an invalid launch response.');
    }
    return payload;
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
    return text && text.length <= limit ? text : null;
  }

  function freezePresentation(value) {
    return Object.freeze({
      cover: Object.freeze({
        palette: value.cover.palette,
        treatment: value.cover.treatment,
      }),
      backdrop: Object.freeze({
        palette: value.backdrop.palette,
        treatment: value.backdrop.treatment,
      }),
      summary: value.summary,
      year: value.year,
      genre: value.genre,
      studio: value.studio,
      players: value.players,
      isFallback: value.isFallback,
    });
  }

  function fallbackPresentation() {
    return freezePresentation(PRESENTATION_FALLBACK);
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
      if (
        !cover || typeof cover !== 'object' || Array.isArray(cover)
        || !backdrop || typeof backdrop !== 'object' || Array.isArray(backdrop)
        || typeof coverPalette !== 'string'
        || typeof coverTreatment !== 'string'
        || typeof backdropPalette !== 'string'
        || typeof backdropTreatment !== 'string'
        || typeof isFallback !== 'boolean'
        || !PALETTE_ALLOWLIST.includes(coverPalette)
        || !PALETTE_ALLOWLIST.includes(backdropPalette)
        || !TREATMENT_ALLOWLIST.includes(coverTreatment)
        || !TREATMENT_ALLOWLIST.includes(backdropTreatment)
      ) return fallbackPresentation();
      if ([summary, year, genre, studio, players].some(item => typeof item !== 'string')) {
        return fallbackPresentation();
      }
      const text = {
        summary: boundedPresentationText(summary, PRESENTATION_TEXT_LIMITS.summary),
        year: boundedPresentationText(year, PRESENTATION_TEXT_LIMITS.year),
        genre: boundedPresentationText(genre, PRESENTATION_TEXT_LIMITS.genre),
        studio: boundedPresentationText(studio, PRESENTATION_TEXT_LIMITS.studio),
        players: boundedPresentationText(players, PRESENTATION_TEXT_LIMITS.players),
      };
      if (Object.values(text).some(item => item === null)) return fallbackPresentation();
      return freezePresentation({
        cover: { palette: coverPalette, treatment: coverTreatment },
        backdrop: { palette: backdropPalette, treatment: backdropTreatment },
        ...text,
        isFallback,
      });
    } catch (_) {
      return fallbackPresentation();
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
    return 'populated';
  }

  function createAppController(options = {}) {
    const fetchImpl = options.fetchImpl
      || (typeof root.fetch === 'function' ? root.fetch.bind(root) : null);
    const metadataAdapter = options.metadataAdapter || root.FogCastMetadata;
    const notify = typeof options.onStateChange === 'function' ? options.onStateChange : null;
    const state = {
      query: '',
      games: [],
      gameViews: [],
      selectedLiveGame: null,
      selectedGameView: null,
      requestSequence: 0,
      detailSequence: 0,
      launchSequence: 0,
      selectionRevision: 0,
      catalogState: 'loading',
      catalogError: null,
      metadataFallbackCount: 0,
      metadataState: 'metadata_fallback',
      launchState: 'idle',
      launchError: null,
      launchMessage: '',
      detailState: 'idle',
      detailError: null,
      hostState: 'unknown',
    };

    function snapshot() {
      return {
        ...state,
        games: state.games.slice(),
        gameViews: state.gameViews.slice(),
        selectedLiveGame: state.selectedLiveGame,
        selectedGameView: state.selectedGameView,
      };
    }

    function emit() {
      const next = snapshot();
      if (notify) {
        try { notify(next); } catch (_) { /* rendering must not change request state */ }
      }
      return next;
    }

    function enrich(game) {
      return launcherGame(game, metadataAdapter);
    }

    function metadataFallbackCount(views) {
      return views.reduce((count, view) => count + (view.presentation.isFallback ? 1 : 0), 0);
    }

    function resetSelectionState() {
      state.selectionRevision += 1;
      state.detailSequence += 1;
      state.launchSequence += 1;
      state.launchState = 'idle';
      state.launchError = null;
      state.launchMessage = '';
      state.detailState = 'idle';
      state.detailError = null;
    }

    function reconcileSelection() {
      if (!state.selectedLiveGame) return;
      const index = state.games.findIndex(game => game.id === state.selectedLiveGame.id);
      resetSelectionState();
      state.selectedLiveGame = index >= 0 ? state.games[index] : null;
      state.selectedGameView = index >= 0 ? state.gameViews[index] : null;
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
        state.metadataState = state.metadataFallbackCount ? 'metadata_fallback' : 'curated';
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
        const view = enrich(detail);
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
        return emit();
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
      return refreshDetail(id);
    }

    async function launchSelected() {
      const selected = state.selectedLiveGame;
      if (!selected) return snapshot();
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
        state.launchError = null;
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

    return Object.freeze({
      getState: snapshot,
      loadCatalog,
      selectGame,
      refreshDetail,
      launchSelected,
    });
  }

  const api = Object.freeze({
    gamesPath,
    gameDetailPath,
    launchRequest,
    launchStatus,
    isSelectedGame,
    detailHeading,
    catalogViewState,
    createAppController,
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
    catalog: document.getElementById('catalog'),
    status: document.getElementById('catalog-status'),
    list: document.getElementById('catalog-list'),
    actions: document.getElementById('catalog-actions'),
    detail: document.getElementById('detail'),
    detailContent: document.getElementById('detail-content'),
    launchActions: document.getElementById('launch-actions'),
    launchStatus: document.getElementById('launch-status'),
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
    state.gameViews.forEach((view, index) => renderCard(view, state.games[index]));
  }

  function renderCard(view, liveGame) {
    const game = view.live;
    const presentation = view.presentation;
    const selected = isSelectedGame(state.selectedLiveGame, game);
    const card = element('button', 'game-card' + (selected ? ' selected' : ''));
    card.type = 'button';
    card.setAttribute('aria-pressed', String(selected));
    const cover = element('span', `cover-art palette-${presentation.cover.palette} treatment-${presentation.cover.treatment}`);
    card.appendChild(cover);
    card.appendChild(element('h3', '', game.title));
    card.appendChild(element('p', 'game-meta', `${game.system} · ${game.state}`));
    if (presentation.isFallback) card.appendChild(element('p', 'fallback-note', 'Demo presentation fallback'));
    card.addEventListener('click', () => selectGame(liveGame.id));
    nodes.list.appendChild(card);
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
      const backdrop = element('div', `backdrop-art palette-${presentation.backdrop.palette} treatment-${presentation.backdrop.treatment}`);
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
    nodes.detailContent.appendChild(element('p', 'detail-summary', presentation.summary));
    const facts = element('div', 'detail-facts');
    [[game.system, 'System'], [game.state, 'Source state'], [presentation.year, 'Year'], [presentation.players, 'Players']].forEach(([value, label]) => {
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
      const progress = element('p', 'status-message', 'launching: preparing the selected live game…');
      nodes.launchActions.appendChild(progress);
      return;
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
    const launch = element('button', 'button', 'Launch live game');
    launch.type = 'button';
    launch.disabled = !canLaunch(liveGame);
    launch.addEventListener('click', launchSelected);
    nodes.launchActions.appendChild(launch);
  }

  function loadCatalog() {
    return controller.loadCatalog(nodes.search.value);
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

  function canLaunch(game) {
    return game.state === 'available' && game.content_prepared;
  }

  function handleStateChange(next) {
    state = next;
    if (next.hostState === 'ready') setHealth('Local host ready', 'host-status');
    if (next.hostState === 'unavailable') setHealth('Catalog unavailable', 'host-status');
    renderCatalog();
    renderDetail(state.selectedGameView, state.selectedLiveGame);
  }

  controller = createAppController({
    metadataAdapter: root.FogCastMetadata,
    onStateChange: handleStateChange,
  });
  state = controller.getState();
  nodes.refresh.addEventListener('click', loadCatalog);
  nodes.search.addEventListener('input', loadCatalog);
  renderCatalog();
  renderDetail(null);
  void loadCatalog();
})(globalThis);
