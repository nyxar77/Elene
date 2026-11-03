<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { router } from "../lib/router.svelte";
  import { api, type CatalogPage, type CatalogApp, validateAPK } from "../lib/api";
  import AppDetailsModal from "../components/AppDetailsModal.svelte";
  import {
    Download,
    Loader2,
    Check,
    X,
    Clock,
    Smartphone,
    Search,
    Shield,
    Zap,
    Lock,
    Globe,
    Package,
    ArrowUpRight,
    ArrowLeft,
    ArrowRight,
    RefreshCw,
    AlertCircle,
    HardDrive,
    Upload,
  } from "lucide-svelte";
  import { onDestroy } from "svelte";
  import type { CatalogStatus } from "../lib/api";

  // State for catalog browsing (F-Droid & IzzyOnDroid / Aurora)
  let searchQuery = $state(router.query.get("q") || "");
  let selectedCategory = $state(router.query.get("category") || "");
  let currentPage = $state(Number(router.query.get("page")) || 1);

  let catalogData = $state<CatalogPage | null>(null);
  let catalogStatus = $state<CatalogStatus | null>(null);
  let isLoadingCatalog = $state(false);
  let isRetryingCatalog = $state(false);
  let catalogError = $state<string | null>(null);

  // Expanded app modal state
  let modalApp = $state<CatalogApp | null>(null);

  // Installing state per package
  let installingPackage = $state<string | null>(null);

  // Local APK quick upload state
  let localFile = $state<File | null>(null);
  let localFileError = $state<string | null>(null);
  let isLocalSubmitting = $state(false);

  let statusInterval: ReturnType<typeof setInterval> | null = null;

  onDestroy(() => {
    if (statusInterval) clearInterval(statusInterval);
  });

  // Popular curated category chips
  const popularCategories = [
    { id: "", label: "All Apps" },
    { id: "Internet", label: "Internet" },
    { id: "Multimedia", label: "Multimedia" },
    { id: "Security", label: "Security & Privacy" },
    { id: "Development", label: "Development" },
    { id: "System", label: "System & Tools" },
    { id: "Navigation", label: "Navigation & Maps" },
    { id: "Games", label: "Games" },
    { id: "Reading", label: "Reading & Docs" },
    { id: "Science & Education", label: "Science & Education" },
    { id: "Graphics", label: "Graphics & Photos" },
    { id: "Time", label: "Productivity & Time" },
  ];

  function formatBytes(bytes?: number): string {
    if (!bytes || bytes <= 0) return "0 Mo";
    const mb = bytes / (1024 * 1024);
    if (mb < 0.1) return (bytes / 1024).toFixed(0) + " Ko";
    return mb.toFixed(1) + " Mo";
  }

  function getActionType(): "install" | "download" {
    const strat = store.preferences.installStrategy || "auto";
    if (strat === "download_only") return "download";
    if (strat === "download_and_install") return "install";
    // auto: adapt based on whether device is selected
    return store.selectedDevice ? "install" : "download";
  }

  // Watch query params and load catalog when in fdroid or aurora (IzzyOnDroid) source
  $effect(() => {
    if (router.source === "fdroid" || router.source === "aurora") {
      loadCatalog(searchQuery, selectedCategory, currentPage, router.source);
      checkStatus(router.source);
      startStatusPolling(router.source);
    } else {
      if (statusInterval) clearInterval(statusInterval);
    }
  });

  function startStatusPolling(source: string) {
    if (statusInterval) clearInterval(statusInterval);
    statusInterval = setInterval(() => {
      checkStatus(source);
    }, 2000);
  }

  async function checkStatus(source: string) {
    try {
      const st = await api.catalogStatus(source);
      catalogStatus = st;
      // If was loading and now finished, reload catalog if empty
      if (st.state === "ready" && (!catalogData || catalogData.apps.length === 0)) {
        loadCatalog(searchQuery, selectedCategory, currentPage, source);
      }
      if (st.state !== "loading" && statusInterval) {
        // Slow down polling when idle/ready
        clearInterval(statusInterval);
        statusInterval = setInterval(() => checkStatus(source), 8000);
      }
    } catch {
      // Ignore background status polling error
    }
  }

  async function retryCatalog() {
    try {
      isRetryingCatalog = true;
      catalogError = null;
      catalogStatus = await api.refreshCatalog(router.source, true);
      startStatusPolling(router.source);
      await loadCatalog(searchQuery, selectedCategory, currentPage, router.source);
    } catch (err: unknown) {
      catalogError = err instanceof Error ? err.message : String(err);
    } finally {
      isRetryingCatalog = false;
    }
  }

  async function loadCatalog(query: string, category: string, page: number, source: string) {
    try {
      isLoadingCatalog = true;
      catalogError = null;
      catalogData = await api.catalog(query, category, page, source);
    } catch (err: unknown) {
      catalogError = err instanceof Error ? err.message : String(err);
    } finally {
      isLoadingCatalog = false;
    }
  }

  function handleSearch(event: SubmitEvent) {
    event.preventDefault();
    currentPage = 1;
    router.navigate("/", {
      source: router.source,
      q: searchQuery,
      category: selectedCategory,
      page: "1",
      device: store.selectedDevice,
    });
    loadCatalog(searchQuery, selectedCategory, 1, router.source);
  }

  function clearSearch() {
    searchQuery = "";
    currentPage = 1;
    router.navigate("/", {
      source: router.source,
      q: "",
      category: selectedCategory,
      page: "1",
      device: store.selectedDevice,
    });
    loadCatalog("", selectedCategory, 1, router.source);
  }

  function selectCategory(cat: string) {
    selectedCategory = cat;
    currentPage = 1;
    router.navigate("/", {
      source: router.source,
      q: searchQuery,
      category: cat,
      page: "1",
      device: store.selectedDevice,
    });
    loadCatalog(searchQuery, cat, 1, router.source);
  }

  function changePage(newPage: number) {
    currentPage = newPage;
    router.navigate("/", {
      source: router.source,
      q: searchQuery,
      category: selectedCategory,
      page: String(newPage),
      device: store.selectedDevice,
    });
    loadCatalog(searchQuery, selectedCategory, newPage, router.source);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  async function handleAppAction(packageID: string, event?: MouseEvent, forceDownloadOnly?: boolean) {
    if (event) {
      event.stopPropagation();
    }

    const action = forceDownloadOnly ? "download" : getActionType();
    if (action === "install" && !store.selectedDevice) {
      store.setError("Please select a connected device in the top toolbar first.");
      return;
    }

    const remoteSource = router.source === "aurora" ? "aurora" : "fdroid";
    const downloadOnly = action === "download";

    try {
      installingPackage = packageID;
      store.clearError();
      await api.installRemote(store.selectedDevice, remoteSource, packageID, downloadOnly);
      await store.refreshJobs();
      if (downloadOnly) {
        store.refreshCachedAPKs();
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      store.setError("Action request failed: " + msg);
    } finally {
      installingPackage = null;
    }
  }

  function openAppDetails(app: CatalogApp) {
    modalApp = app;
  }

  function handleLocalFile(event: Event) {
    const input = event.target as HTMLInputElement;
    if (input.files && input.files.length > 0) {
      const file = input.files[0];
      const err = validateAPK(file);
      if (err) {
        localFileError = err;
        localFile = null;
      } else {
        localFileError = null;
        localFile = file;
      }
    }
  }

  async function handleLocalInstall(event: SubmitEvent) {
    event.preventDefault();
    if (!store.selectedDevice) {
      localFileError = "Please select a device.";
      return;
    }
    if (!localFile) {
      localFileError = "Please choose a valid APK.";
      return;
    }

    try {
      isLocalSubmitting = true;
      localFileError = null;
      await api.installLocal(store.selectedDevice, localFile);
      router.navigate("/activity", { device: store.selectedDevice });
      store.refreshJobs();
    } catch (err: unknown) {
      localFileError = err instanceof Error ? err.message : String(err);
    } finally {
      isLocalSubmitting = false;
    }
  }
</script>

{#if router.source === "local"}
  <!-- LOCAL APKs VIEW -->
  <div class="page-heading">
    <div>
      <h1>Local APK Workspace</h1>
      <p>Install and manage standalone Android packages from your computer over USB debugging.</p>
    </div>
    <span class="pill">Direct ADB Mode</span>
  </div>

  <div class="marketing-hero">
    <div class="hero-content">
      <span class="hero-badge">OFFLINE & PRIVACY-FIRST</span>
      <h2>Zero-Cloud Android Side-Loading</h2>
      <p>
        Bypasses vendor app stores and sideloading friction. Communicates directly with your Android
        hardware through the official ADB wire protocol. No accounts, no cloud proxy, no data collection.
      </p>
      <div class="hero-pills">
        <span class="hero-pill" style="display: inline-flex; align-items: center; gap: 5px;"><Zap size={12} /> Direct USB Stream</span>
        <span class="hero-pill" style="display: inline-flex; align-items: center; gap: 5px;"><Shield size={12} /> In-Place Updates (adb -r)</span>
        <span class="hero-pill" style="display: inline-flex; align-items: center; gap: 5px;"><Lock size={12} /> 100% Local Daemon</span>
      </div>
    </div>
  </div>

  <div class="section-title">
    <h2>Connected devices</h2>
    <span>{store.devices.length} detected</span>
  </div>

  <div class="device-grid">
    {#each store.devices as device}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="device-card"
        class:selected={device.Serial === store.selectedDevice}
        onclick={() => {
          if (device.State === "online") {
            store.selectedDevice = device.Serial;
            router.setParam("device", device.Serial);
          }
        }}
      >
        <span class="device-icon" aria-hidden="true"><Smartphone size={18} /></span>
        <span>
          <strong>{device.Model || "Android device"}</strong>
          <small>{device.Serial}</small>
          <small>
            {#if device.State === "unauthorized"}
              Accept the debugging prompt on your device.
            {:else if device.State === "offline"}
              Reconnect USB cable and try again.
            {:else}
              Ready to install apps
            {/if}
          </small>
        </span>
        <span
          class="status-dot"
          class:online={device.State === "online"}
          title={device.State}
        ></span>
      </div>
    {:else}
      <section class="panel" style="grid-column: 1 / -1;">
        <div class="empty-state">
          <div class="empty-icon" aria-hidden="true"><Smartphone size={34} /></div>
          <h2>No devices connected</h2>
          <p>
            Connect an Android device via USB, turn on Developer Options &amp; USB Debugging,
            and tap "Allow USB debugging" on the screen.
          </p>
          <button class="button secondary" type="button" onclick={() => store.refreshDevices()}>
            Check again
          </button>
        </div>
      </section>
    {/each}
  </div>

  <div class="section-title" id="install">
    <h2>Quick Install APK</h2>
  </div>

  <section class="panel settings-panel">
    <div class="settings-section-heading">
      <h2>Install from your files</h2>
      <p>Choose an APK from your computer to install directly on the selected device.</p>
    </div>

    <form class="settings-fields" onsubmit={handleLocalInstall}>
      {#if localFileError}
        <div class="error-message">{localFileError}</div>
      {/if}

      <label>
        Target Device
        <select class="select-trigger" bind:value={store.selectedDevice} required>
          <option value="">Select a connected device</option>
          {#each store.devices as device}
            <option value={device.Serial} disabled={device.State !== "online"}>
              {device.Model || "Android Device"} · {device.Serial} ({device.State})
            </option>
          {/each}
        </select>
      </label>

      <label class="file-picker">
        {#if localFile}
          <strong>{localFile.name}</strong>
          <span>{(localFile.size / (1024 * 1024)).toFixed(1)} MiB · Click to change file</span>
        {:else}
          <strong>Choose an APK file</strong>
          <span>Single .apk · Maximum 512 MiB</span>
        {/if}
        <input type="file" accept=".apk" onchange={handleLocalFile} required />
      </label>

      <button
        type="submit"
        class="button primary"
        disabled={isLocalSubmitting || !localFile || !store.selectedDevice}
      >
        {#if isLocalSubmitting}
          Installing application...
        {:else}
          Install to device
        {/if}
      </button>
    </form>
  </section>

{:else if router.source === "fdroid" || router.source === "aurora"}
  <!-- F-DROID OR IZZYONDROID / AURORA CATALOG VIEW -->
  <div class="marketing-hero store-hero">
    <div class="hero-content">
      <div class="hero-header-row">
        <span class="hero-badge">
          {router.source === "aurora" ? "IZZYONDROID & AURORA ECOSYSTEM" : "OFFICIAL F-DROID REPOSITORY"}
        </span>
        {#if store.selectedDevice}
          <span class="hero-target-pill">Target: {store.activeDevice?.Model || store.selectedDevice}</span>
        {/if}
      </div>
      <h2>
        {router.source === "aurora"
          ? "IzzyOnDroid Repository & Aurora Store Client"
          : "Free & Open Source Application Store"}
      </h2>
      <p>
        {#if router.source === "aurora"}
          Aurora Store does not provide a public web API (it is an Android client that talks directly to Google Play's proprietary Protobuf endpoints behind Cloudflare token dispensers). This repository connects to <strong>IzzyOnDroid</strong>—giving you direct one-click installation of <strong>Aurora Store</strong> itself, Obtainium, MicroG, LibreTube, Cromite, and 1,200+ modern releases!
        {:else}
          Browse community-verified Android applications with source code transparency.
          Stream APKs directly to your device via USB with automated SHA-256 integrity verification.
        {/if}
      </p>
      <div class="hero-pills">
        <span class="hero-pill" style="display: inline-flex; align-items: center; gap: 5px;"><Zap size={12} /> Direct USB Pipeline</span>
        <span class="hero-pill" style="display: inline-flex; align-items: center; gap: 5px;"><Shield size={12} /> SHA-256 Verified</span>
        <span class="hero-pill" style="display: inline-flex; align-items: center; gap: 5px;"><Globe size={12} /> Zero Cloud Accounts</span>
        <span class="hero-pill" style="display: inline-flex; align-items: center; gap: 5px;">
          <Package size={12} />
          {router.source === "aurora" ? "IzzyOnDroid v2 Index" : "Official F-Droid Catalog"}
        </span>
        {#if router.source === "aurora"}
          <button
            type="button"
            class="button primary"
            style="min-height: 28px; padding: 4px 12px; font-size: 11px;"
            onclick={() => {
              searchQuery = "Aurora Store";
              loadCatalog("Aurora Store", "", 1, "aurora");
            }}
          >
            <Search size={12} /> Find Aurora Store APK
          </button>
        {/if}
      </div>
    </div>
  </div>

  <!-- CATEGORY CHIPS BAR -->
  <div class="category-chips-container" role="region" aria-label="App categories">
    <div class="category-chips-scroll">
      {#each popularCategories as cat}
        <button
          type="button"
          class="category-chip"
          class:active={selectedCategory === cat.id}
          onclick={() => selectCategory(cat.id)}
        >
          {cat.label}
        </button>
      {/each}
    </div>
  </div>

  <!-- STORE TOOLBAR (SEARCH & STATS) -->
  <div class="catalog-toolbar">
    <form class="search-box store-search" onsubmit={handleSearch}>
      <span class="search-icon" aria-hidden="true"><Search size={14} /></span>
      <input
        bind:value={searchQuery}
        placeholder={router.source === "aurora"
          ? "Search IzzyOnDroid (e.g. Aurora Store, Obtainium, Cromite, LibreTube)..."
          : "Search applications (e.g. VLC, NewPipe, Signal, KeePassDX)..."}
        maxlength="120"
        aria-label="Search catalog apps"
      />
      {#if searchQuery}
        <button
          type="button"
          class="clear-search-button"
          onclick={clearSearch}
          aria-label="Clear search"
        >
          <X size={13} />
        </button>
      {/if}
      <button type="submit" class="button secondary">Search</button>
    </form>

    <div class="toolbar-stats">
      {#if catalogData}
        <span class="pill-count">{catalogData.total.toLocaleString()} apps</span>
      {/if}
    </div>
  </div>

  <!-- REPOSITORY SYNC PROGRESS / RETRY BANNER -->
  {#if catalogStatus && (catalogStatus.state === "loading" || catalogStatus.state === "error" || catalogError)}
    <div class="source-sync-banner" class:sync-error={catalogStatus.state === "error" || catalogError}>
      <div class="sync-banner-main">
        {#if catalogStatus.state === "loading"}
          <div class="orbital-orbit-container" aria-hidden="true">
            <div class="orbital-orbit-ring"></div>
            <div class="orbital-orbit-dot"></div>
          </div>
        {:else}
          <div class="sync-error-badge" aria-hidden="true">
            <AlertCircle size={20} />
          </div>
        {/if}

        <div class="sync-banner-text">
          <div class="sync-banner-heading">
            {#if catalogStatus.state === "loading"}
              Synchronizing {router.source === "aurora" ? "IzzyOnDroid" : "F-Droid"} repository...
            {:else}
              Repository synchronization stopped
            {/if}
          </div>
          <div class="sync-banner-details">
            {#if catalogStatus.state === "loading"}
              {#if catalogStatus.totalBytes > 0}
                {formatBytes(catalogStatus.downloadedBytes)} / {formatBytes(catalogStatus.totalBytes)} ({catalogStatus.percent}%)
              {:else if catalogStatus.downloadedBytes > 0}
                {formatBytes(catalogStatus.downloadedBytes)} transferred...
              {:else}
                Connecting to repository mirror...
              {/if}
            {:else}
              {catalogStatus.error || catalogError || "Catalog download was interrupted or incomplete. Click Retry to continue."}
            {/if}
          </div>
        </div>
      </div>

      <div class="sync-banner-action">
        {#if catalogStatus.state === "loading"}
          <div class="sync-bar-track">
            <div
              class="sync-bar-fill"
              style="width: {catalogStatus.percent > 0 ? catalogStatus.percent : 15}%"
              class:indeterminate={catalogStatus.percent <= 0}
            ></div>
          </div>
        {:else}
          <button
            type="button"
            class="button primary sync-retry-btn"
            disabled={isRetryingCatalog}
            onclick={retryCatalog}
          >
            <RefreshCw size={13} class={isRetryingCatalog ? "spin" : ""} />
            Retry
          </button>
        {/if}
      </div>
    </div>
  {/if}

  {#if catalogError && (!catalogStatus || catalogStatus.state !== "error")}
    <div class="error-message" role="alert">{catalogError}</div>
  {/if}

  {#if isLoadingCatalog && (!catalogData || catalogData.apps.length === 0) && catalogStatus?.state !== "loading"}
    <div class="loading-state store-loading">
      <div class="spin-large"><Loader2 size={32} /></div>
      <p>
        Connecting to {router.source === "aurora" ? "IzzyOnDroid" : "F-Droid"} repository...
      </p>
    </div>
  {:else if catalogStatus?.state === "loading" && (!catalogData || catalogData.apps.length === 0)}
    <div class="empty-state store-sync-waiting" style="padding: 40px 20px;">
      <p class="muted">
        Downloading repository catalog. Applications will appear automatically as soon as the index is saved.
      </p>
    </div>
  {:else if catalogData}
    <div class="section-title">
      <h2>
        {#if searchQuery}
          Search results for "{searchQuery}"
        {:else if selectedCategory}
          {selectedCategory}
        {:else}
          Featured &amp; Recently Updated
        {/if}
      </h2>
      <span>Showing {(catalogData.page - 1) * 24 + 1}–{Math.min(catalogData.page * 24, catalogData.total)} of {catalogData.total.toLocaleString()}</span>
    </div>

    <div class="app-grid store-grid">
      {#each catalogData.apps as app}
        {@const pkgJob = store.getJobForPackage(app.id)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <article
          class="application-card catalog-card store-card"
          onclick={() => openAppDetails(app)}
          title="Click to expand details and releases"
        >
          <div class="application-icon store-icon">
            {#if app.icon}
              <img
                loading="lazy"
                src={"/api/catalog/icon?path=" + encodeURIComponent(app.icon) + "&source=" + encodeURIComponent(router.source)}
                alt=""
              />
            {:else}
              <span class="placeholder-icon"><Package size={24} /></span>
            {/if}
          </div>

          <div class="card-details">
            <div class="card-header-line">
              <h3 class="card-title">{app.name}</h3>
              {#if app.versionName}
                <span class="version-tag">v{app.versionName}</span>
              {/if}
            </div>

            <p class="card-summary">{app.summary || "No summary provided."}</p>

            <div class="card-meta-line">
              <span class="card-pkg-id">{app.id}</span>
              {#if app.categories && app.categories.length > 0}
                <span class="card-category-tag">{app.categories[0]}</span>
              {/if}
            </div>

            <div class="card-actions-row">
              <button
                type="button"
                class="button secondary details-btn"
                onclick={(e) => {
                  e.stopPropagation();
                  openAppDetails(app);
                }}
              >
                Details
                <ArrowUpRight size={13} />
              </button>

              {#if pkgJob}
                <button
                  type="button"
                  class="button secondary install-btn"
                  onclick={(e) => {
                    e.stopPropagation();
                    store.cancelJob(pkgJob.id);
                  }}
                  title="Click to cancel"
                >
                  {#if pkgJob.state === "queued"}
                    <Clock size={13} /> Queued · Cancel
                  {:else if pkgJob.state === "downloading"}
                    <Loader2 size={13} class="spin" /> {pkgJob.progress ?? 0}% · Cancel
                  {:else}
                    <Loader2 size={13} class="spin" /> Installing...
                  {/if}
                </button>
              {:else if store.hasUpdate(app.id, app.versionCode)}
                <button
                  type="button"
                  class="button primary install-btn update-action-btn"
                  disabled={installingPackage === app.id || !store.selectedDevice}
                  onclick={(e) => handleAppAction(app.id, e)}
                  title="Update to newer version"
                >
                  {#if store.busyInstalling}
                    <Clock size={13} /> Queue Update
                  {:else}
                    <Download size={13} /> Update
                  {/if}
                </button>
              {:else if store.isInstalled(app.id)}
                <button
                  type="button"
                  class="button secondary install-btn installed-badge-btn"
                  disabled
                  title="Already installed on device"
                >
                  <Check size={13} strokeWidth={2.5} /> Installed
                </button>
              {:else}
                <button
                  type="button"
                  class="button primary install-btn"
                  disabled={installingPackage === app.id || (getActionType() === 'install' && !store.selectedDevice)}
                  onclick={(e) => handleAppAction(app.id, e)}
                >
                  {#if installingPackage === app.id}
                    <Loader2 size={13} class="spin" /> Queueing...
                  {:else if getActionType() === 'download'}
                    <Download size={13} /> Download
                  {:else if !store.selectedDevice}
                    <Smartphone size={13} /> Select device
                  {:else if store.busyInstalling}
                    <Clock size={13} /> Queue
                  {:else}
                    <Upload size={13} /> Install
                  {/if}
                </button>
              {/if}
            </div>
          </div>
        </article>
      {:else}
        <div class="empty-state" style="grid-column: 1 / -1;">
          <h2>No applications found</h2>
          <p>
            {#if searchQuery}
              No applications matched "{searchQuery}" in {selectedCategory || "all categories"}.
            {:else}
              No applications available for this filter.
            {/if}
          </p>
          <button type="button" class="button secondary" onclick={clearSearch}>
            Clear filters
          </button>
        </div>
      {/each}
    </div>

    {#if catalogData.pages > 1}
      <nav class="catalog-pagination" aria-label="Catalog pages">
        <button
          class="button secondary"
          type="button"
          disabled={!catalogData.prev}
          onclick={() => catalogData?.prev && changePage(catalogData.prev)}
        >
          <ArrowLeft size={14} /> Previous
        </button>

        <span class="page-indicator">
          Page <strong>{catalogData.page}</strong> of <strong>{catalogData.pages}</strong>
        </span>

        <button
          class="button secondary"
          type="button"
          disabled={!catalogData.next}
          onclick={() => catalogData?.next && changePage(catalogData.next)}
        >
          Next <ArrowRight size={14} />
        </button>
      </nav>
    {/if}

    <p class="muted small text-center" style="margin-top: 30px; text-align: center;">
      Packages are downloaded securely from verified repository mirrors and checked against cryptographic SHA-256 signatures before installation via ADB.
    </p>
  {/if}

  <!-- APP DETAILS MODAL -->
  <AppDetailsModal
    app={modalApp}
    onclose={() => (modalApp = null)}
  />
{/if}
