<script lang="ts">
  import { onMount } from 'svelte';
  import * as App from './lib/wailsjs/go/main/App';
  import appIcon from './assets/appicon.png';
  import { EventsOn, OnFileDrop, OnFileDropOff } from './lib/wailsjs/runtime/runtime';
  import { main } from './lib/wailsjs/go/models';
  import { _, setLocale, SUPPORTED_LOCALES, type SupportedLocale } from './lib/i18n';

  type Status = main.Status;
  type Config = main.Config;
  type WatcherStatus = main.WatcherStatus;
  type PortStatus = main.PortStatus;
  type UpdateCheckResult = main.UpdateCheckResult;

  interface LogEntry {
    source: string;
    message: string;
  }

  let status: Status = $state({
    backend: 'stopped',
    frontend: 'stopped',
    backendPid: 0,
    frontendPid: 0,
    mtoolsExists: false,
    libraryPath: '',
    frontendPort: '7750',
    backendPort: '7754',
    isProduction: false
  });

  let logs: string[] = $state([]);
  let showLogs = $state(false);
  let showSettings = $state(false);
  let showEmbed = $state(true);
  let loading = $state(false);
  let reloadBooksLoading = $state(false);
  let error = $state('');
  let frontendUrl = $state('');
  let iframeKey = $state(0);

  let config: Config = $state({
    libraryPath: '',
    frontendPort: '7750',
    autoLoadBooks: false,
    language: '',
    disableAutoUpdateCheck: false
  });

  let showUpdateModal = $state(false);
  let pendingUpdate: UpdateCheckResult | null = $state(null);
  /** Result of the last GitHub check when production + auto check is on */
  let lastUpdateCheck: UpdateCheckResult | null = $state(null);

  let deps: Record<string, boolean> = $state({});

  // Watcher status
  let watcherStatus: WatcherStatus = $state({ available: false, enabled: false });
  let watcherLoading = $state(false);

  // Ports status for kill buttons
  let portsStatus: PortStatus[] = $state([]);
  let portsLoading = $state(false);

  // Language
  let currentLanguage: SupportedLocale = $state('en');

  /** Wails sometimes leaves `wails-drop-target-active` after drop — cyan “stuck” highlight */
  let libraryDropFlashTimer: ReturnType<typeof setTimeout> | null = null;
  let libraryDropSuccessFlash = $state(false);

  function stripWailsDropTargetActive() {
    const run = () => {
      document.querySelectorAll('.wails-drop-target-active').forEach(el => {
        el.classList.remove('wails-drop-target-active');
      });
    };
    requestAnimationFrame(run);
    setTimeout(run, 0);
    setTimeout(run, 80);
  }

  function flashLibraryPathReplaced() {
    if (libraryDropFlashTimer) clearTimeout(libraryDropFlashTimer);
    libraryDropSuccessFlash = true;
    libraryDropFlashTimer = setTimeout(() => {
      libraryDropSuccessFlash = false;
      libraryDropFlashTimer = null;
    }, 2000);
  }

  onMount(() => {
    let cancelled = false;

    void (async () => {
      await new Promise(resolve => setTimeout(resolve, 100));
      if (cancelled) return;

      OnFileDrop((_x, _y, paths) => {
        void (async () => {
          const raw = paths?.[0];
          try {
            if (!raw) {
              stripWailsDropTargetActive();
              return;
            }
            const resolved = await App.SetLibraryPathResolved(raw);
            config.libraryPath = resolved;
            error = '';
            await refreshStatus();
            stripWailsDropTargetActive();
            flashLibraryPathReplaced();
          } catch (e: unknown) {
            error = e instanceof Error ? e.message : String(e);
            stripWailsDropTargetActive();
          }
        })();
      }, true);

      try {
        const lang = await App.GetLanguage() as SupportedLocale;
        currentLanguage = SUPPORTED_LOCALES.includes(lang) ? lang : 'en';
        setLocale(currentLanguage);

        await refreshStatus();
        config = await App.GetConfig();
        deps = await App.CheckDependencies();
        frontendUrl = await App.GetFrontendURL();

        await refreshLauncherUpdateStatus({ showModalIfOutdated: true });

        EventsOn('log', (data: LogEntry) => {
          logs.push(`[${data.source.toUpperCase()}] ${data.message}`);
          if (logs.length > 200) {
            logs = logs.slice(-200);
          }
        });

        EventsOn('statusChange', (newStatus: Status) => {
          status = newStatus;
        });

        setInterval(refreshStatus, 2000);
      } catch (e) {
        console.error('Failed to initialize:', e);
        error = 'Failed to connect to backend';
      }
    })();

    return () => {
      cancelled = true;
      if (libraryDropFlashTimer) clearTimeout(libraryDropFlashTimer);
      OnFileDropOff();
    };
  });

  async function refreshStatus() {
    try {
      status = await App.GetStatus();
      frontendUrl = await App.GetFrontendURL();
    } catch (e) {
      console.error('Failed to get status:', e);
    }
  }

  async function startAll() {
    loading = true;
    error = '';
    try {
      await App.StartAll();
      await refreshStatus();
      // Reload iframe after a delay
      setTimeout(() => iframeKey++, 3000);
    } catch (e: any) {
      error = e.message || String(e);
    }
    loading = false;
  }

  async function stopAll() {
    loading = true;
    error = '';
    try {
      await App.StopAll();
      await refreshStatus();
    } catch (e: any) {
      error = e.message || String(e);
    }
    loading = false;
  }

  async function restartAll() {
    loading = true;
    error = '';
    try {
      await App.RestartAll();
      await refreshStatus();
      setTimeout(() => iframeKey++, 3000);
    } catch (e: any) {
      error = e.message || String(e);
    }
    loading = false;
  }

  let selectingFolder = $state(false);

  async function selectFolder() {
    error = '';
    selectingFolder = true;
    try {
      const path = await App.SelectLibraryFolder();
      if (path) {
        config.libraryPath = path;
        await refreshStatus();
      }
    } catch (e: any) {
      console.error('selectFolder error:', e);
      error = e.message || String(e);
    } finally {
      selectingFolder = false;
    }
  }

  async function refreshLauncherUpdateStatus(opts?: { showModalIfOutdated?: boolean }) {
    const prod = await App.IsProductionMode();
    if (!prod || config.disableAutoUpdateCheck) {
      lastUpdateCheck = null;
      return;
    }
    try {
      const u = await App.CheckForUpdates();
      lastUpdateCheck = u;
      if (opts?.showModalIfOutdated && u.updateAvailable && u.releaseUrl) {
        pendingUpdate = u;
        showUpdateModal = true;
      }
    } catch (e) {
      console.warn('CheckForUpdates:', e);
      lastUpdateCheck = null;
    }
  }

  async function saveConfig() {
    try {
      await App.SetConfig(config);
      showSettings = false;
      await refreshStatus();
      await refreshLauncherUpdateStatus({ showModalIfOutdated: false });
    } catch (e: any) {
      error = e.message || String(e);
    }
  }

  async function openUpdateRelease() {
    const url = pendingUpdate?.releaseUrl;
    if (!url) return;
    try {
      await App.OpenReleasePage(url);
    } catch (e: any) {
      error = e.message || String(e);
    }
    showUpdateModal = false;
    pendingUpdate = null;
  }

  function dismissUpdateModal() {
    showUpdateModal = false;
    pendingUpdate = null;
  }

  async function openInBrowser() {
    try {
      await App.OpenMTools();
    } catch (e: any) {
      error = e.message || String(e);
    }
  }

  function clearLogs() {
    App.ClearLogs('all');
    logs = [];
  }

  function reloadIframe() {
    iframeKey++;
  }

  async function reloadBooks() {
    if (reloadBooksLoading) return;
    reloadBooksLoading = true;
    error = '';
    try {
      await App.ReloadBooks();
      await refreshStatus();
      reloadIframe();
    } catch (e: any) {
      error = e.message || String(e);
    } finally {
      reloadBooksLoading = false;
    }
  }

  // Watcher functions
  async function refreshWatcherStatus() {
    if (status.backend !== 'running') {
      watcherStatus = { available: false, enabled: false };
      return;
    }
    try {
      watcherStatus = await App.GetWatcherStatus();
    } catch (e) {
      console.error('Failed to get watcher status:', e);
      watcherStatus = { available: false, enabled: false };
    }
  }

  async function toggleWatcher() {
    watcherLoading = true;
    try {
      await App.SetWatcherEnabled(!watcherStatus.enabled);
      await refreshWatcherStatus();
    } catch (e: any) {
      error = e.message || String(e);
    }
    watcherLoading = false;
  }

  // Port management functions
  async function checkPorts() {
    portsLoading = true;
    try {
      portsStatus = await App.GetPortsStatus();
    } catch (e: any) {
      console.error('Failed to check ports:', e);
      portsStatus = [];
    }
    portsLoading = false;
  }

  async function killPortProcess(port: string) {
    portsLoading = true;
    error = '';
    try {
      await App.KillProcessOnPort(port);
      await checkPorts();
    } catch (e: any) {
      error = e.message || String(e);
    }
    portsLoading = false;
  }

  async function killAllPortProcesses() {
    portsLoading = true;
    error = '';
    try {
      await App.KillPortProcesses();
      await checkPorts();
    } catch (e: any) {
      error = e.message || String(e);
    }
    portsLoading = false;
  }

  async function handleLanguageChange(lang: SupportedLocale) {
    currentLanguage = lang;
    setLocale(lang);
    try {
      await App.SetLanguage(lang);
      // Refresh frontend URL to include new language parameter
      frontendUrl = await App.GetFrontendURL();
      // Reload iframe if running
      if (status.frontend === 'running') {
        iframeKey++;
      }
    } catch (e: any) {
      console.error('Failed to save language:', e);
    }
  }

  $effect(() => {
    // Auto-show embed when frontend is running
    if (status.frontend === 'running' && !showSettings && !showLogs) {
      showEmbed = true;
    }
  });

  // Refresh watcher status when backend status changes
  $effect(() => {
    if (status.backend === 'running') {
      refreshWatcherStatus();
    } else {
      watcherStatus = { available: false, enabled: false };
    }
  });

  // Check ports when services are stopped
  $effect(() => {
    if (status.backend === 'stopped' && status.frontend === 'stopped') {
      checkPorts();
    }
  });

  // Language picker state
  let langPickerOpen = $state(false);

  const langFlags: Record<SupportedLocale, string> = {
    en: '🇬🇧',
    ru: '🇷🇺',
    es: '🇪🇸',
    zh: '🇨🇳',
    fr: '🇫🇷',
    it: '🇮🇹',
    de: '🇩🇪',
    ko: '🇰🇷',
    pt: '🇵🇹',
    el: '🇬🇷',
    tr: '🇹🇷',
    vi: '🇻🇳',
    th: '🇹🇭',
    fi: '🇫🇮'
  };

  function selectLanguage(lang: SupportedLocale) {
    handleLanguageChange(lang);
    langPickerOpen = false;
  }

  // Greetings for orbital animation
  const greetings = [
    'Hello', 'Привет', 'Hola', '你好', 'Bonjour',
    'Ciao', 'Hallo', '안녕', 'Olá', 'Γεια',
    'Merhaba', 'Xin chào', 'สวัสดี', 'Hei'
  ];
</script>

<div class="app">
  <!-- Compact Header (visible when running or in panels) -->
  {#if showSettings || showLogs || status.frontend === 'running'}
    <header class="header header-compact">
      <div class="header-bar">
        <div class="header-shell">
          <div class="header-brand">
            <div class="header-logo-frame" aria-hidden="true">
              <div class="header-logo-core">
                <img src={appIcon} alt="" class="header-logo-img" />
              </div>
            </div>
            <div class="header-brand-text">
              <h1 class="header-app-title">{$_('app.title')}</h1>
            </div>
            <div class="header-status-cluster" aria-label="Services">
              <div class="status-chip" class:running={status.backend === 'running'}>
                <span class="status-dot" class:running={status.backend === 'running'} class:stopped={status.backend === 'stopped'}></span>
                <span class="status-chip-label">{$_('app.backend')}</span>
              </div>
              <div class="status-chip" class:running={status.frontend === 'running'}>
                <span class="status-dot" class:running={status.frontend === 'running'} class:stopped={status.frontend === 'stopped'}></span>
                <span class="status-chip-label">{$_('app.frontend')}</span>
              </div>
            </div>
          </div>

          <div class="header-actions">
            {#if status.frontend === 'running'}
              <button
                type="button"
                class="header-icon-btn"
                onclick={openInBrowser}
                title={$_('buttons.openBrowser')}
                aria-label={$_('buttons.openBrowser')}
              >
                🌐
              </button>
              <button
                type="button"
                class="header-icon-btn"
                onclick={reloadBooks}
                disabled={reloadBooksLoading}
                title={$_('buttons.reload')}
                aria-label={$_('buttons.reload')}
              >
                {#if reloadBooksLoading}⏳{:else}🔄{/if}
              </button>
              <button type="button" class="header-btn danger" onclick={stopAll} disabled={loading}>
                ⏹️ <span class="header-btn-text">{$_('buttons.stop')}</span>
              </button>
            {/if}

            <div class="lang-picker compact" class:open={langPickerOpen}>
              <button
                type="button"
                class="lang-picker-trigger"
                onclick={() => langPickerOpen = !langPickerOpen}
                onblur={() => setTimeout(() => langPickerOpen = false, 150)}
                aria-expanded={langPickerOpen}
                aria-haspopup="listbox"
              >
                <span class="lang-flag">{langFlags[currentLanguage]}</span>
                <span class="lang-arrow">▾</span>
              </button>
              {#if langPickerOpen}
                <div class="lang-picker-dropdown header-dropdown" role="listbox">
                  {#each SUPPORTED_LOCALES as lang (lang)}
                    <button
                      type="button"
                      role="option"
                      aria-selected={currentLanguage === lang}
                      class="lang-option"
                      class:active={currentLanguage === lang}
                      onclick={() => selectLanguage(lang)}
                    >
                      <span class="lang-flag">{langFlags[lang]}</span>
                      <span class="lang-name">{$_(`languages.${lang}`)}</span>
                      {#if currentLanguage === lang}
                        <span class="lang-check">✓</span>
                      {/if}
                    </button>
                  {/each}
                </div>
              {/if}
            </div>

            <button
              type="button"
              class="header-icon-btn"
              class:active={showSettings}
              onclick={() => showSettings = !showSettings}
              title={$_('settings.title')}
              aria-label={$_('settings.title')}
            >
              ⚙️
            </button>
            <button
              type="button"
              class="header-icon-btn"
              class:active={showLogs}
              onclick={() => {
                showLogs = !showLogs;
                showEmbed = !showLogs;
              }}
              title={$_('logs.title')}
              aria-label={$_('logs.title')}
            >
              📋
            </button>
          </div>
        </div>
      </div>
    </header>
  {/if}

  <!-- Main Content -->
  <main class="main" class:full-height={!showSettings && !showLogs && status.frontend !== 'running'}>
    {#if showSettings}
      <div class="settings-panel">
        <h2>{$_('settings.title')}</h2>

        <div
          class="setting-group library-path-drop-zone"
          class:library-drop-success-flash={libraryDropSuccessFlash}
        >
          <label for="library-path">{$_('settings.libraryPath')}</label>
          <div class="input-with-button">
            <input id="library-path" type="text" bind:value={config.libraryPath} placeholder="/path/to/publish_files" />
            <button class="secondary" onclick={selectFolder} disabled={selectingFolder}>
              {selectingFolder ? '...' : $_('buttons.browse')}
            </button>
          </div>
          <p class="setting-hint">{$_('settings.libraryPathDropHint')}</p>
          {#if libraryDropSuccessFlash}
            <p class="library-drop-ok">{$_('settings.libraryPathUpdated')}</p>
          {/if}
        </div>

        <div class="setting-group">
          <label for="frontend-port">{$_('settings.frontendPort')}</label>
          <input id="frontend-port" type="text" bind:value={config.frontendPort} placeholder="7750" />
        </div>

        <div class="setting-group">
          <label class="checkbox-setting">
            <input type="checkbox" bind:checked={config.autoLoadBooks} />
            <span>{$_('settings.autoLoadBooksLong')}</span>
          </label>
          <p class="setting-hint">{$_('settings.autoLoadBooksHint')}</p>
        </div>

        <div class="setting-group">
          <label class="checkbox-setting">
            <input type="checkbox" bind:checked={config.disableAutoUpdateCheck} />
            <span>{$_('settings.disableAutoUpdateCheck')}</span>
          </label>
          <p class="setting-hint">{$_('settings.disableAutoUpdateCheckHint')}</p>
        </div>

        {#if status.isProduction && lastUpdateCheck && !config.disableAutoUpdateCheck}
          <div class="setting-group launcher-version-panel">
            <span class="setting-label">{$_('updates.sectionTitle')}</span>
            {#if lastUpdateCheck.checkFailed}
              <p class="launcher-version-line warn">
                {$_('updates.checkFailed', { values: { current: lastUpdateCheck.currentVersion } })}
              </p>
            {:else if lastUpdateCheck.skipped}
              <p class="launcher-version-line muted">
                {$_('updates.checkSkipped', { values: { current: lastUpdateCheck.currentVersion } })}
              </p>
            {:else if lastUpdateCheck.updateAvailable}
              <p class="launcher-version-line warn">
                {$_('updates.statusOutdated', {
                  values: { current: lastUpdateCheck.currentVersion, latest: lastUpdateCheck.latestVersion }
                })}
              </p>
            {:else if lastUpdateCheck.upToDate}
              <p class="launcher-version-line ok">
                {$_('updates.statusUpToDate', {
                  values: { current: lastUpdateCheck.currentVersion, latest: lastUpdateCheck.latestVersion }
                })}
              </p>
            {/if}
          </div>
        {/if}

        <div class="setting-group">
          <span class="setting-label">{$_('settings.dependencies')}</span>
          <div class="deps-list">
            {#each Object.entries(deps) as [name, installed]}
              <div class="dep-item">
                <span class="status-dot" class:running={installed} class:stopped={!installed}></span>
                <span>{name}</span>
                <span class="dep-status">{installed ? '✓' : '✗'}</span>
              </div>
            {/each}
          </div>
        </div>

        <div class="setting-actions">
          <button class="primary" onclick={saveConfig}>{$_('buttons.save')}</button>
          <button class="secondary" onclick={() => showSettings = false}>{$_('buttons.cancel')}</button>
        </div>
      </div>
    {:else if showLogs}
      <div class="logs-panel">
        <div class="logs-header">
          <h2>{$_('logs.title')}</h2>
          <button class="secondary" onclick={clearLogs}>{$_('buttons.clear')}</button>
        </div>
        <div class="logs-content">
          {#each logs as log}
            <div class="log-line" class:backend={log.includes('[BACKEND]')} class:frontend={log.includes('[FRONTEND]')}>
              {log}
            </div>
          {/each}
          {#if logs.length === 0}
            <div class="logs-empty">{$_('logs.noLogs')}</div>
          {/if}
        </div>
      </div>
    {:else if showEmbed && status.frontend === 'running'}
      <div class="embed-container">
        {#key iframeKey}
          <iframe
            src={frontendUrl}
            title="MTools"
            sandbox="allow-same-origin allow-scripts allow-forms allow-popups"
          ></iframe>
        {/key}
      </div>
    {:else}
      <!-- Welcome Screen with Orbits -->
      <div class="welcome-hero">
        <!-- Orbital rings with greetings -->
        <div class="orbits-container">
          <div class="orbit orbit-1">
            {#each greetings.slice(0, 5) as word, i}
              <span class="orbit-word" style="--i: {i}; --total: 5">{word}</span>
            {/each}
          </div>
          <div class="orbit orbit-2">
            {#each greetings.slice(5, 10) as word, i}
              <span class="orbit-word" style="--i: {i}; --total: 5">{word}</span>
            {/each}
          </div>
          <div class="orbit orbit-3">
            {#each greetings.slice(10, 15) as word, i}
              <span class="orbit-word" style="--i: {i}; --total: 5">{word}</span>
            {/each}
          </div>
        </div>

        <!-- Center content -->
        <div class="hero-center">
          <img src={appIcon} alt="Mnemoo Tools" class="hero-logo" />
          <h1 class="hero-title">{$_('app.title')}</h1>
          <p class="hero-subtitle">{$_('welcome.description')}</p>

          {#if status.isProduction && lastUpdateCheck && !config.disableAutoUpdateCheck}
            <div class="hero-version-banner">
              {#if lastUpdateCheck.checkFailed}
                <p class="launcher-version-line warn">
                  {$_('updates.checkFailed', { values: { current: lastUpdateCheck.currentVersion } })}
                </p>
              {:else if lastUpdateCheck.skipped}
                <p class="launcher-version-line muted">
                  {$_('updates.checkSkipped', { values: { current: lastUpdateCheck.currentVersion } })}
                </p>
              {:else if lastUpdateCheck.updateAvailable}
                <p class="launcher-version-line warn">
                  {$_('updates.statusOutdated', {
                    values: { current: lastUpdateCheck.currentVersion, latest: lastUpdateCheck.latestVersion }
                  })}
                </p>
              {:else if lastUpdateCheck.upToDate}
                <p class="launcher-version-line ok">
                  {$_('updates.statusUpToDate', {
                    values: { current: lastUpdateCheck.currentVersion, latest: lastUpdateCheck.latestVersion }
                  })}
                </p>
              {/if}
            </div>
          {/if}

          <!-- Status indicators -->
          <div class="hero-status">
            <div class="status-pill">
              <span class="status-dot" class:running={status.backend === 'running'} class:stopped={status.backend === 'stopped'}></span>
              <span>{$_('app.backend')}</span>
            </div>
            <div class="status-pill">
              <span class="status-dot" class:running={status.frontend === 'running'} class:stopped={status.frontend === 'stopped'}></span>
              <span>{$_('app.frontend')}</span>
            </div>
          </div>

          <!-- Action buttons -->
          <div
            class="hero-actions welcome-library-drop"
            class:library-drop-success-flash={libraryDropSuccessFlash}
          >
            <p class="welcome-drop-hint">
              {status.libraryPath
                ? $_('settings.libraryPathDropHintReplace')
                : $_('settings.libraryPathDropHint')}
            </p>
            {#if libraryDropSuccessFlash}
              <p class="welcome-drop-ok">{$_('settings.libraryPathUpdated')}</p>
            {/if}
            {#if !status.libraryPath}
              <button class="btn-hero" onclick={selectFolder} disabled={selectingFolder}>
                <span class="btn-icon">📂</span>
                <span>{$_('buttons.selectFolder')}</span>
              </button>
            {:else if status.backend === 'stopped' && status.frontend === 'stopped'}
              <button class="btn-hero btn-primary" onclick={startAll} disabled={loading}>
                <span class="btn-icon">▶️</span>
                <span>{$_('buttons.startAll')}</span>
              </button>
              <button class="btn-hero btn-secondary" onclick={selectFolder} disabled={selectingFolder}>
                <span class="btn-icon">📂</span>
                <span>{$_('buttons.changeDir')}</span>
              </button>
            {:else}
              <button class="btn-hero btn-danger" onclick={stopAll} disabled={loading}>
                <span class="btn-icon">⏹️</span>
                <span>{$_('buttons.stopAll')}</span>
              </button>
              <button class="btn-hero btn-secondary" onclick={restartAll} disabled={loading}>
                <span class="btn-icon">🔄</span>
                <span>{$_('buttons.restart')}</span>
              </button>
            {/if}
          </div>

          <!-- Settings row -->
          <div class="hero-settings">
            <label class="toggle-pill">
              <input type="checkbox" bind:checked={config.autoLoadBooks} onchange={saveConfig} />
              <span>{$_('settings.autoLoadBooks')}</span>
            </label>

            <!-- Custom Language Picker -->
            <div class="lang-picker" class:open={langPickerOpen}>
              <button
                class="lang-picker-trigger"
                onclick={() => langPickerOpen = !langPickerOpen}
                onblur={() => setTimeout(() => langPickerOpen = false, 150)}
              >
                <span class="lang-flag">{langFlags[currentLanguage]}</span>
                <span class="lang-name">{$_(`languages.${currentLanguage}`)}</span>
                <span class="lang-arrow">▾</span>
              </button>
              {#if langPickerOpen}
                <div class="lang-picker-dropdown">
                  {#each SUPPORTED_LOCALES as lang}
                    <button
                      class="lang-option"
                      class:active={currentLanguage === lang}
                      onclick={() => selectLanguage(lang)}
                    >
                      <span class="lang-flag">{langFlags[lang]}</span>
                      <span class="lang-name">{$_(`languages.${lang}`)}</span>
                      {#if currentLanguage === lang}
                        <span class="lang-check">✓</span>
                      {/if}
                    </button>
                  {/each}
                </div>
              {/if}
            </div>

            <button class="icon-btn-hero" onclick={() => showSettings = true}>⚙️</button>
            <button class="icon-btn-hero" onclick={() => { showLogs = true; showEmbed = false; }}>📋</button>
          </div>

          <!-- Warnings -->
          {#if error}
            <div class="hero-error">{error}</div>
          {/if}

          {#if portsStatus.some(p => p.inUse) && status.backend === 'stopped'}
            <div class="hero-warning">
              ⚠️ {$_('errors.portsInUse')}
              {#each portsStatus.filter(p => p.inUse) as port}
                <span class="port-badge">
                  {port.port}
                  <button class="kill-btn" onclick={() => killPortProcess(port.port)} disabled={portsLoading}>✕</button>
                </span>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </main>

  {#if showUpdateModal && pendingUpdate}
    <div
      class="update-modal-backdrop"
      role="presentation"
      tabindex="-1"
      onclick={dismissUpdateModal}
      onkeydown={e => e.key === 'Escape' && dismissUpdateModal()}
    >
      <div
        class="update-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="update-modal-title"
        tabindex="0"
        onclick={e => e.stopPropagation()}
        onkeydown={e => e.stopPropagation()}
      >
        <h3 id="update-modal-title">{$_('updates.title')}</h3>
        <p class="update-modal-msg">
          {$_('updates.message', {
            values: { current: pendingUpdate.currentVersion, latest: pendingUpdate.latestVersion }
          })}
        </p>
        <div class="update-modal-actions">
          <button type="button" class="secondary" onclick={dismissUpdateModal}>
            {$_('updates.later')}
          </button>
          <button type="button" class="primary" onclick={openUpdateRelease}>
            {$_('updates.download')}
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: var(--bg-primary);
    overflow: hidden;
  }

  /* ===== Compact Header (glass shell — aligned with web app) ===== */
  .header {
    position: relative;
    z-index: 100;
    flex-shrink: 0;
    padding: 10px 14px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
    background: transparent;
    animation: slideDown 200ms var(--ease-out-quart);
  }

  @keyframes slideDown {
    from {
      opacity: 0;
      transform: translateY(-8px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .header-bar {
    max-width: 100%;
    margin: 0 auto;
  }

  .header-shell {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    padding: 0.65rem 0.8rem;
    border-radius: 0.95rem;
    background: linear-gradient(135deg, rgba(26, 26, 31, 0.84) 0%, rgba(14, 14, 17, 0.92) 100%);
    border: 1px solid rgba(255, 255, 255, 0.06);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    box-shadow:
      0 1px 0 rgba(255, 255, 255, 0.04) inset,
      0 0 0 1px rgba(0, 0, 0, 0.22),
      0 14px 42px rgba(0, 0, 0, 0.38);
  }

  .header-brand {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    min-width: 0;
    flex: 1 1 auto;
  }

  .header-logo-frame {
    position: relative;
    display: grid;
    place-items: center;
    width: 2.6rem;
    height: 2.6rem;
    border-radius: 0.75rem;
    flex-shrink: 0;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
  }

  .header-logo-frame::after {
    content: '';
    position: absolute;
    inset: -1px;
    border-radius: inherit;
    border: 1px solid var(--launcher-cyan-dim);
    pointer-events: none;
  }

  .header-logo-core {
    display: grid;
    place-items: center;
    width: 1.9rem;
    height: 1.9rem;
    border-radius: 0.5rem;
    background: rgba(10, 10, 12, 0.96);
    border: 1px solid rgba(0, 212, 255, 0.32);
    box-shadow: 0 0 16px var(--launcher-cyan-glow);
  }

  .header-logo-img {
    width: 1.35rem;
    height: 1.35rem;
    object-fit: contain;
    border-radius: 4px;
  }

  .header-brand-text {
    min-width: 0;
  }

  .header-app-title {
    font-size: 0.95rem;
    font-weight: 600;
    letter-spacing: 0.03em;
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: min(14rem, 42vw);
  }

  .header-status-cluster {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    flex-wrap: wrap;
  }

  @media (min-width: 720px) {
    .header-status-cluster {
      margin-left: 0.35rem;
      padding-left: 0.65rem;
      border-left: 1px solid rgba(255, 255, 255, 0.06);
    }
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .header-icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.05rem;
    height: 2.05rem;
    padding: 0;
    font-size: 0.95rem;
    line-height: 1;
    color: var(--text-primary);
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 0.55rem;
    cursor: pointer;
    transition:
      background-color 180ms var(--ease-out-quart),
      border-color 180ms var(--ease-out-quart),
      box-shadow 180ms var(--ease-out-quart),
      transform 100ms var(--ease-out-quart);
  }

  .header-icon-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.12);
    box-shadow: 0 0 18px rgba(0, 212, 255, 0.06);
  }

  .header-icon-btn.active {
    background: linear-gradient(180deg, rgba(0, 212, 255, 0.12) 0%, rgba(0, 168, 204, 0.06) 100%);
    border-color: rgba(0, 212, 255, 0.34);
    box-shadow: 0 0 20px rgba(0, 212, 255, 0.12);
  }

  .header-icon-btn:focus-visible {
    outline: none;
    border-color: rgba(0, 212, 255, 0.45);
    box-shadow: 0 0 0 2px var(--launcher-focus-ring);
  }

  .header-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.42rem 0.65rem;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 0.55rem;
    color: var(--text-primary);
    font-size: 0.75rem;
    font-weight: 500;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    font-family: ui-monospace, 'SF Mono', Monaco, monospace;
    cursor: pointer;
    transition:
      background-color 180ms var(--ease-out-quart),
      border-color 180ms var(--ease-out-quart),
      box-shadow 180ms var(--ease-out-quart),
      transform 100ms var(--ease-out-quart);
  }

  .header-btn-text {
    text-transform: none;
    letter-spacing: 0.02em;
    font-family: inherit;
    font-size: 0.8125rem;
    font-weight: 500;
  }

  .header-btn:hover:not(:disabled) {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.12);
  }

  .header-btn:focus-visible {
    outline: none;
    border-color: rgba(0, 212, 255, 0.45);
    box-shadow: 0 0 0 2px var(--launcher-focus-ring);
  }

  .header-btn.danger {
    background: rgba(239, 68, 68, 0.08);
    border-color: rgba(239, 68, 68, 0.28);
    color: #fecaca;
  }

  .header-btn.danger:hover:not(:disabled) {
    background: rgba(239, 68, 68, 0.16);
    border-color: rgba(239, 68, 68, 0.42);
    box-shadow: 0 0 22px rgba(239, 68, 68, 0.15);
  }

  .status-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.38rem 0.55rem;
    border-radius: 0.55rem;
    font-size: 0.65rem;
    font-weight: 500;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    font-family: ui-monospace, 'SF Mono', Monaco, monospace;
    color: var(--text-secondary);
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.08);
    transition:
      border-color 180ms var(--ease-out-quart),
      background 180ms var(--ease-out-quart),
      box-shadow 180ms var(--ease-out-quart),
      color 180ms var(--ease-out-quart);
  }

  .status-chip-label {
    line-height: 1.2;
  }

  .status-chip .status-dot {
    margin-right: 0;
  }

  .status-chip.running {
    color: rgba(220, 252, 231, 0.95);
    background: rgba(34, 197, 94, 0.08);
    border-color: rgba(34, 197, 94, 0.24);
    box-shadow: 0 0 16px rgba(34, 197, 94, 0.12);
  }

  /* ===== Main Content ===== */
  .main {
    flex: 1;
    overflow: hidden;
    position: relative;
  }

  .main.full-height {
    height: 100vh;
  }

  /* ===== Welcome Hero with Orbits ===== */
  .welcome-hero {
    height: 100%;
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
    overflow: hidden;
    background:
      radial-gradient(ellipse at 50% 30%, rgba(99, 102, 241, 0.12) 0%, transparent 50%),
      radial-gradient(ellipse at 80% 80%, rgba(139, 92, 246, 0.08) 0%, transparent 40%),
      radial-gradient(ellipse at 20% 70%, rgba(59, 130, 246, 0.06) 0%, transparent 40%);
  }

  /* Orbits Container */
  .orbits-container {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    pointer-events: none;
  }

  .orbit {
    position: absolute;
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 50%;
    animation: rotate linear infinite;
  }

  .orbit-1 {
    width: 500px;
    height: 500px;
    animation-duration: 60s;
  }

  .orbit-2 {
    width: 700px;
    height: 700px;
    animation-duration: 90s;
    animation-direction: reverse;
  }

  .orbit-3 {
    width: 900px;
    height: 900px;
    animation-duration: 120s;
  }

  @keyframes rotate {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }

  .orbit-word {
    position: absolute;
    font-size: 14px;
    font-weight: 500;
    color: rgba(255, 255, 255, 0.5);
    text-shadow: 0 0 20px rgba(99, 102, 241, 0.4);
    white-space: nowrap;
    /* Position at center, then move out and rotate */
    left: 50%;
    top: 50%;
    --angle: calc(var(--i) * (360deg / var(--total)));
    transform:
      translate(-50%, -50%)
      rotate(var(--angle))
      translateX(calc(50% + 10px));
  }

  .orbit-1 .orbit-word {
    transform:
      translate(-50%, -50%)
      rotate(var(--angle))
      translateX(250px);
  }

  .orbit-2 .orbit-word {
    font-size: 13px;
    color: rgba(255, 255, 255, 0.35);
    transform:
      translate(-50%, -50%)
      rotate(var(--angle))
      translateX(350px);
  }

  .orbit-3 .orbit-word {
    font-size: 12px;
    color: rgba(255, 255, 255, 0.25);
    transform:
      translate(-50%, -50%)
      rotate(var(--angle))
      translateX(450px);
  }

  /* Hero Center Content */
  .hero-center {
    position: relative;
    z-index: 10;
    text-align: center;
    padding: 40px;
    animation: fadeInUp 500ms var(--ease-out-quart);
  }

  @keyframes fadeInUp {
    from {
      opacity: 0;
      transform: translateY(20px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .hero-logo {
    width: 100px;
    height: 100px;
    margin: 0 auto 24px;
    border-radius: 24px;
    object-fit: contain;
    box-shadow:
      0 0 0 1px rgba(255, 255, 255, 0.1),
      0 20px 40px rgba(99, 102, 241, 0.3),
      0 0 80px rgba(99, 102, 241, 0.2);
  }

  .hero-title {
    font-size: 36px;
    font-weight: 700;
    margin-bottom: 8px;
    background: linear-gradient(135deg, #fff 0%, #c4c4c4 50%, #fff 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .hero-subtitle {
    font-size: 16px;
    color: var(--text-secondary);
    margin-bottom: 32px;
    max-width: 400px;
    margin-left: auto;
    margin-right: auto;
  }

  /* Status Pills */
  .hero-status {
    display: flex;
    justify-content: center;
    gap: 12px;
    margin-bottom: 32px;
  }

  .status-pill {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 16px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    border-radius: 100px;
    font-size: 13px;
  }

  /* Hero Action Buttons */
  .hero-actions {
    display: flex;
    justify-content: center;
    gap: 12px;
    margin-bottom: 24px;
  }

  .hero-actions.welcome-library-drop {
    --wails-drop-target: drop;
    flex-wrap: wrap;
    padding: 16px 20px;
    border-radius: 14px;
    border: 1px dashed rgba(255, 255, 255, 0.12);
    background: rgba(0, 0, 0, 0.15);
    transition:
      border-color 150ms ease,
      background 150ms ease,
      box-shadow 150ms ease;
  }

  .hero-actions.welcome-library-drop:global(.wails-drop-target-active) {
    border-color: var(--launcher-cyan-dim);
    background: rgba(0, 212, 255, 0.06);
    box-shadow: 0 0 0 1px var(--launcher-cyan-dim);
  }

  .hero-actions.welcome-library-drop.library-drop-success-flash {
    border-color: rgba(34, 197, 94, 0.75);
    background: rgba(34, 197, 94, 0.1);
    box-shadow: 0 0 0 1px rgba(34, 197, 94, 0.45);
  }

  .hero-actions.welcome-library-drop.library-drop-success-flash:global(.wails-drop-target-active) {
    border-color: rgba(34, 197, 94, 0.75);
    background: rgba(34, 197, 94, 0.12);
    box-shadow: 0 0 0 1px rgba(34, 197, 94, 0.5);
  }

  .welcome-drop-hint {
    flex: 1 0 100%;
    text-align: center;
    margin: 0 0 4px;
    font-size: 13px;
    color: var(--text-secondary);
    line-height: 1.4;
  }

  .welcome-drop-ok {
    flex: 1 0 100%;
    text-align: center;
    margin: 0 0 8px;
    font-size: 13px;
    font-weight: 600;
    color: rgba(74, 222, 128, 0.95);
  }

  .btn-hero {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 14px 28px;
    font-size: 15px;
    font-weight: 600;
    border-radius: 12px;
    border: none;
    cursor: pointer;
    transition:
      transform 150ms var(--ease-out-quart),
      box-shadow 150ms ease,
      background-color 150ms ease;
  }

  .btn-hero:active:not(:disabled) {
    transform: scale(0.97);
  }

  .btn-hero:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-hero.btn-primary {
    background: #fff;
    color: #0a0a0b;
    font-weight: 700;
    box-shadow:
      0 1px 2px rgba(0, 0, 0, 0.3),
      0 4px 12px rgba(255, 255, 255, 0.1);
  }

  .btn-hero.btn-primary:hover:not(:disabled) {
    background: #e5e5e5;
    box-shadow:
      0 2px 4px rgba(0, 0, 0, 0.4),
      0 8px 20px rgba(255, 255, 255, 0.15);
  }

  .btn-hero.btn-secondary {
    background: var(--bg-tertiary);
    color: var(--text-primary);
    border: 1px solid var(--border);
  }

  .btn-hero.btn-secondary:hover:not(:disabled) {
    background: var(--bg-hover);
    border-color: rgba(255, 255, 255, 0.12);
  }

  .btn-hero.btn-danger {
    background: linear-gradient(135deg, var(--error) 0%, #dc2626 100%);
    color: white;
    box-shadow: 0 4px 20px rgba(239, 68, 68, 0.3);
  }

  .btn-hero.btn-danger:hover:not(:disabled) {
    box-shadow: 0 8px 30px rgba(239, 68, 68, 0.4);
  }

  .btn-icon {
    font-size: 18px;
  }

  /* Hero Settings Row */
  .hero-settings {
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }

  .toggle-pill {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    border-radius: 100px;
    font-size: 13px;
    cursor: pointer;
    transition: background-color 150ms ease, transform 100ms ease-out;
  }

  .toggle-pill:hover {
    background: rgba(255, 255, 255, 0.08);
  }

  .toggle-pill:active {
    transform: scale(0.98);
  }

  .toggle-pill input {
    width: 14px;
    height: 14px;
    cursor: pointer;
  }

  /* Custom Language Picker */
  .lang-picker {
    position: relative;
  }

  .lang-picker-trigger {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 14px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    border-radius: 100px;
    color: var(--text-primary);
    cursor: pointer;
    transition: background-color 150ms ease, border-color 150ms ease;
    font-size: 13px;
  }

  .lang-picker-trigger:hover {
    background: rgba(255, 255, 255, 0.08);
  }

  .lang-picker.open .lang-picker-trigger {
    border-color: var(--accent);
    background: rgba(255, 255, 255, 0.08);
  }

  .lang-flag {
    font-size: 16px;
    line-height: 1;
  }

  .lang-name {
    font-weight: 500;
    color: var(--text-primary);
  }

  .lang-arrow {
    font-size: 10px;
    color: var(--text-secondary);
    transition: transform 150ms ease;
  }

  .lang-picker.open .lang-arrow {
    transform: rotate(180deg);
  }

  .lang-picker-dropdown {
    position: absolute;
    bottom: calc(100% + 8px);
    left: 50%;
    transform: translateX(-50%);
    min-width: 320px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 8px;
    box-shadow:
      0 0 0 1px rgba(255, 255, 255, 0.05),
      0 10px 40px rgba(0, 0, 0, 0.5);
    animation: dropdownIn 150ms var(--ease-out-quart);
    z-index: 100;
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 4px;
  }

  @keyframes dropdownIn {
    from {
      opacity: 0;
      transform: translateX(-50%) translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateX(-50%) translateY(0);
    }
  }

  .lang-option {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: transparent;
    border: none;
    border-radius: 8px;
    color: var(--text-primary);
    cursor: pointer;
    transition: background-color 100ms ease;
    font-size: 13px;
    text-align: left;
    white-space: nowrap;
  }

  .lang-option:hover {
    background: rgba(255, 255, 255, 0.08);
  }

  .lang-option.active {
    background: rgba(99, 102, 241, 0.15);
  }

  .lang-option .lang-flag {
    font-size: 18px;
  }

  .lang-option .lang-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .lang-check {
    margin-left: auto;
    color: var(--accent);
    font-size: 14px;
  }

  /* Compact language picker for header */
  .lang-picker.compact .lang-picker-trigger {
    min-height: 2.05rem;
    padding: 0 0.55rem;
    border-radius: 0.55rem;
    background: rgba(255, 255, 255, 0.03);
    border: 1px solid rgba(255, 255, 255, 0.08);
    gap: 0.35rem;
    transition:
      background-color 180ms var(--ease-out-quart),
      border-color 180ms var(--ease-out-quart),
      box-shadow 180ms var(--ease-out-quart);
  }

  .lang-picker.compact .lang-picker-trigger:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(255, 255, 255, 0.12);
    box-shadow: 0 0 16px rgba(0, 212, 255, 0.05);
  }

  .lang-picker.compact .lang-picker-trigger:focus-visible {
    outline: none;
    border-color: rgba(0, 212, 255, 0.45);
    box-shadow: 0 0 0 2px var(--launcher-focus-ring);
  }

  .lang-picker.compact.open .lang-picker-trigger {
    border-color: rgba(0, 212, 255, 0.34);
    background: linear-gradient(180deg, rgba(0, 212, 255, 0.1) 0%, rgba(0, 168, 204, 0.05) 100%);
    box-shadow: 0 0 18px rgba(0, 212, 255, 0.1);
  }

  .lang-picker-dropdown.header-dropdown {
    bottom: auto;
    top: calc(100% + 8px);
    z-index: 1000;
    animation: dropdownInDown 150ms var(--ease-out-quart);
    background: linear-gradient(145deg, rgba(28, 28, 32, 0.96) 0%, rgba(14, 14, 17, 0.98) 100%);
    border: 1px solid rgba(255, 255, 255, 0.07);
    backdrop-filter: blur(18px);
    -webkit-backdrop-filter: blur(18px);
    box-shadow:
      0 0 0 1px rgba(0, 0, 0, 0.35),
      0 16px 48px rgba(0, 0, 0, 0.55),
      0 0 40px rgba(0, 212, 255, 0.04);
  }

  @keyframes dropdownInDown {
    from {
      opacity: 0;
      transform: translateX(-50%) translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateX(-50%) translateY(0);
    }
  }

  /* Ensure text colors in dropdown */
  .lang-picker-dropdown .lang-option {
    color: var(--text-primary);
  }

  .lang-picker-dropdown .lang-name {
    color: var(--text-primary);
  }

  .lang-picker-dropdown.header-dropdown .lang-option.active {
    background: rgba(0, 212, 255, 0.1);
  }

  .lang-picker-dropdown.header-dropdown .lang-check {
    color: var(--launcher-cyan);
  }

  .icon-btn-hero {
    width: 36px;
    height: 36px;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    border-radius: 50%;
    font-size: 16px;
    cursor: pointer;
    transition: background-color 150ms ease, transform 100ms ease-out;
  }

  .icon-btn-hero:hover {
    background: rgba(255, 255, 255, 0.1);
  }

  .icon-btn-hero:active {
    transform: scale(0.95);
  }

  /* Hero Warnings */
  .hero-error,
  .hero-warning {
    margin-top: 16px;
    padding: 10px 16px;
    border-radius: 10px;
    font-size: 13px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    flex-wrap: wrap;
    animation: fadeIn 200ms ease;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  .hero-error {
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: var(--error);
  }

  .hero-warning {
    background: rgba(245, 158, 11, 0.1);
    border: 1px solid rgba(245, 158, 11, 0.3);
    color: var(--warning);
  }

  /* ===== Panel Styles (unchanged) ===== */
  @keyframes slideIn {
    from { opacity: 0; transform: translateY(-8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  /* Settings */
  .settings-panel {
    padding: 24px;
    max-width: 600px;
    margin: 0 auto;
    animation: slideIn 200ms var(--ease-out-quart);
    overflow-y: auto;
    height: 100%;
  }

  .settings-panel h2 {
    margin-bottom: 24px;
  }

  .setting-group {
    box-shadow: 0 0 0 1px var(--border-subtle), var(--shadow-sm);
    border-radius: 8px;
    padding: 16px;
    background: var(--bg-secondary);
  }

  .setting-group + .setting-group {
    margin-top: 16px;
  }

  .setting-group label,
  .setting-label {
    display: block;
    margin-bottom: 8px;
    font-size: 14px;
    color: var(--text-secondary);
  }

  .setting-group input[type="text"] {
    width: 100%;
  }

  .setting-group select {
    width: 100%;
    padding: 8px 12px;
    font-size: 14px;
    background: var(--bg-tertiary);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    cursor: pointer;
  }

  .setting-group select:hover {
    border-color: var(--accent);
  }

  .setting-group select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .input-with-button {
    display: flex;
    gap: 8px;
  }

  .input-with-button input {
    flex: 1;
  }

  .library-path-drop-zone {
    --wails-drop-target: drop;
    transition: box-shadow 150ms ease;
  }

  .library-path-drop-zone:global(.wails-drop-target-active) {
    box-shadow: 0 0 0 2px var(--launcher-cyan-dim), 0 0 0 1px var(--border-subtle), var(--shadow-sm);
  }

  .library-path-drop-zone.library-drop-success-flash {
    box-shadow: 0 0 0 2px rgba(34, 197, 94, 0.55), 0 0 0 1px var(--border-subtle), var(--shadow-sm);
  }

  .library-path-drop-zone.library-drop-success-flash:global(.wails-drop-target-active) {
    box-shadow: 0 0 0 2px rgba(34, 197, 94, 0.6), 0 0 0 1px var(--border-subtle), var(--shadow-sm);
  }

  .library-drop-ok {
    margin: 10px 0 0;
    font-size: 13px;
    font-weight: 600;
    color: rgba(74, 222, 128, 0.95);
  }

  .deps-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bg-tertiary);
    border-radius: 6px;
    box-shadow: 0 0 0 1px var(--border), var(--shadow-sm);
  }

  .dep-item {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .dep-status {
    margin-left: auto;
    font-size: 12px;
  }

  .setting-actions {
    display: flex;
    gap: 8px;
    margin-top: 24px;
  }

  /* Logs */
  .logs-panel {
    height: 100%;
    display: flex;
    flex-direction: column;
    animation: slideIn 200ms var(--ease-out-quart);
  }

  .logs-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border);
  }

  .logs-header h2 {
    font-size: 16px;
  }

  .logs-content {
    flex: 1;
    overflow-y: auto;
    padding: 12px 20px;
    padding-top: 24px;
    font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
    font-size: 12px;
    line-height: 1.6;
    mask-image: linear-gradient(to bottom, transparent 0%, black 24px);
    -webkit-mask-image: linear-gradient(to bottom, transparent 0%, black 24px);
  }

  .log-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .log-line.backend {
    color: #60a5fa;
  }

  .log-line.frontend {
    color: #34d399;
  }

  .logs-empty {
    color: var(--text-secondary);
    text-align: center;
    padding: 40px;
  }

  /* Embed */
  .embed-container {
    height: 100%;
    width: 100%;
  }

  .embed-container iframe {
    width: 100%;
    height: 100%;
    border: none;
    background: white;
  }

  /* Checkbox setting in settings panel */
  .checkbox-setting {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 14px;
  }

  .checkbox-setting input {
    width: 16px;
    height: 16px;
    cursor: pointer;
  }

  .setting-hint {
    margin-top: 6px;
    font-size: 12px;
    color: var(--text-secondary);
    line-height: 1.4;
  }

  /* Port badges */
  .port-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 8px;
    background: rgba(239, 68, 68, 0.2);
    border-radius: 4px;
    font-family: monospace;
    font-size: 13px;
    transition: background-color 150ms ease;
  }

  .port-badge:hover {
    background: rgba(239, 68, 68, 0.3);
  }

  .kill-btn {
    padding: 0 4px;
    background: transparent;
    border: none;
    color: var(--error);
    cursor: pointer;
    font-size: 12px;
    line-height: 1;
  }

  .kill-btn:hover {
    color: #dc2626;
  }

  .kill-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .update-modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 10000;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 28px 24px;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
    animation: fadeIn 200ms ease;
  }

  .update-modal {
    width: 100%;
    max-width: 440px;
    padding: 28px 26px 26px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    border-radius: 12px;
    box-shadow: var(--shadow-md, 0 16px 48px rgba(0, 0, 0, 0.35));
    animation: slideIn 220ms var(--ease-out-quart);
  }

  .update-modal h3 {
    margin: 0 0 16px;
    font-size: 18px;
    color: var(--text-primary);
  }

  .update-modal-msg {
    margin: 0;
    padding: 2px 0 4px;
    font-size: 14px;
    line-height: 1.55;
    color: var(--text-secondary);
  }

  .update-modal-actions {
    display: flex;
    gap: 12px;
    justify-content: flex-end;
    margin-top: 26px;
    padding-top: 4px;
    flex-wrap: wrap;
  }

  .setting-group.launcher-version-panel {
    padding: 20px 22px;
  }

  .launcher-version-panel .setting-label {
    margin-bottom: 12px;
  }

  .launcher-version-panel .launcher-version-line {
    margin: 0;
    padding: 14px 16px;
    font-size: 14px;
    line-height: 1.5;
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.04);
    box-shadow: inset 0 0 0 1px var(--border-subtle, rgba(255, 255, 255, 0.06));
  }

  .hero-version-banner {
    margin: 18px auto 0;
    max-width: 520px;
    text-align: center;
    padding: 16px 24px 20px;
  }

  .hero-version-banner .launcher-version-line {
    margin: 0;
    padding: 12px 18px;
    font-size: 13px;
    line-height: 1.55;
    border-radius: 10px;
    background: rgba(255, 255, 255, 0.05);
    box-shadow: inset 0 0 0 1px var(--border-subtle, rgba(255, 255, 255, 0.07));
  }

  .launcher-version-line.ok {
    color: #34d399;
  }

  .launcher-version-line.warn {
    color: var(--warning, #f59e0b);
  }

  .launcher-version-line.muted {
    color: var(--text-secondary);
  }
</style>
