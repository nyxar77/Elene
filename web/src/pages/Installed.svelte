<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { api, type InstalledApp, type CatalogApp } from "../lib/api";
  import AppDetailsModal from "../components/AppDetailsModal.svelte";
  import {
    Download,
    Loader2,
    Smartphone,
    Search,
    X,
    Clock,
    ArrowUpRight,
  } from "lucide-svelte";

  interface DisplayInstalledApp {
    id: string;
    versionCode?: number;
    catalogMatch?: CatalogApp;
    displayName: string;
    hasUpdate: boolean;
    gradientBg: string;
    monogram: string;
  }

  let filterQuery = $state("");
  let selectedFilter = $state<"all" | "updates" | "fdroid" | "other">("all");
  let installedApps = $state<DisplayInstalledApp[]>([]);
  let isLoading = $state(false);
  let errorMsg = $state<string | null>(null);

  // Expanded modal app state
  let modalApp = $state<CatalogApp | null>(null);

  // Updating packages tracking
  let updatingPackages = $state<Set<string>>(new Set());

  const gradients = [
    "linear-gradient(135deg, #6366f1 0%, #a855f7 100%)",
    "linear-gradient(135deg, #3b82f6 0%, #06b6d4 100%)",
    "linear-gradient(135deg, #10b981 0%, #14b8a6 100%)",
    "linear-gradient(135deg, #f59e0b 0%, #ef4444 100%)",
    "linear-gradient(135deg, #ec4899 0%, #8b5cf6 100%)",
    "linear-gradient(135deg, #0ea5e9 0%, #6366f1 100%)",
  ];

  function getGradientForPackage(id: string): string {
    let hash = 0;
    for (let i = 0; i < id.length; i++) {
      hash = (hash << 5) - hash + id.charCodeAt(i);
      hash |= 0;
    }
    const idx = Math.abs(hash) % gradients.length;
    return gradients[idx];
  }

  function getMonogram(name: string): string {
    const parts = name.trim().split(/[\s._-]+/);
    if (parts.length >= 2 && parts[0] && parts[1]) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return (name.slice(0, 2) || "AP").toUpperCase();
  }

  function formatPackageName(id: string): string {
    const parts = id.split(".");
    // If standard reverse-domain e.g. com.example.app, take last or last two
    if (parts.length >= 3) {
      const candidate = parts.slice(parts.length - 2).join(" ");
      return candidate
        .replace(/([A-Z])/g, " $1")
        .replace(/[-_]/g, " ")
        .split(" ")
        .filter(Boolean)
        .map((w) => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase())
        .join(" ");
    }
    return parts[parts.length - 1] || id;
  }

  $effect(() => {
    if (store.selectedDevice) {
      loadInstalled(store.selectedDevice);
    } else {
      installedApps = [];
    }
  });

  async function loadInstalled(serial: string) {
    try {
      isLoading = true;
      errorMsg = null;

      // 1. Fetch raw installed package list from device ADB
      const rawPackages: InstalledApp[] = await api.packages(serial);

      // 2. Resolve packages against F-Droid catalog
      const packageIDs = rawPackages.map((p) => p.id);
      let catalogMatches: Record<string, CatalogApp> = {};
      try {
        catalogMatches = await api.resolvePackages(packageIDs);
      } catch {
        // If catalog lookup fails, continue with raw packages
        catalogMatches = {};
      }

      // 3. Combine into rich display models
      installedApps = rawPackages.map((raw) => {
        const match = catalogMatches[raw.id];
        const hasUpdate = Boolean(
          match &&
            match.versionCode &&
            raw.versionCode &&
            match.versionCode > raw.versionCode,
        );

        const displayName = match?.name || formatPackageName(raw.id);
        const monogram = getMonogram(displayName);
        const gradientBg = getGradientForPackage(raw.id);

        return {
          id: raw.id,
          versionCode: raw.versionCode,
          catalogMatch: match,
          displayName,
          hasUpdate,
          gradientBg,
          monogram,
        };
      });
    } catch (err: unknown) {
      errorMsg = err instanceof Error ? err.message : String(err);
      installedApps = [];
    } finally {
      isLoading = false;
    }
  }

  async function updateApp(app: DisplayInstalledApp, event?: MouseEvent) {
    if (event) {
      event.stopPropagation();
    }
    if (!store.selectedDevice) return;

    try {
      updatingPackages.add(app.id);
      updatingPackages = new Set(updatingPackages);
      await api.installRemote(store.selectedDevice, "fdroid", app.id);
      await store.refreshJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      store.setError("Failed to queue update for " + app.displayName + ": " + msg);
    } finally {
      updatingPackages.delete(app.id);
      updatingPackages = new Set(updatingPackages);
    }
  }

  const updatesCount = $derived(installedApps.filter((a) => a.hasUpdate).length);
  const fdroidCount = $derived(installedApps.filter((a) => a.catalogMatch).length);

  const filteredApps = $derived(
    installedApps.filter((app) => {
      // Category filter
      if (selectedFilter === "updates" && !app.hasUpdate) return false;
      if (selectedFilter === "fdroid" && !app.catalogMatch) return false;
      if (selectedFilter === "other" && app.catalogMatch) return false;

      // Text query filter
      if (filterQuery.trim()) {
        const q = filterQuery.toLowerCase();
        return (
          app.id.toLowerCase().includes(q) ||
          app.displayName.toLowerCase().includes(q) ||
          (app.catalogMatch?.summary?.toLowerCase().includes(q) ?? false)
        );
      }
      return true;
    }),
  );
</script>

<div class="page-heading">
  <div>
    <h1>Installed Applications</h1>
    <p>Applications installed on your selected Android device with repository verification.</p>
  </div>
  {#if store.selectedDevice}
    <div class="installed-stat-badges">
      <span class="pill">{installedApps.length} packages</span>
      {#if updatesCount > 0}
        <span class="pill update-pill-highlight" style="gap: 5px;">
          <Download size={12} /> {updatesCount} updates available
        </span>
      {/if}
    </div>
  {/if}
</div>

{#if errorMsg}
  <div class="error-message" role="alert">{errorMsg}</div>
{/if}

{#if !store.selectedDevice}
  <section class="panel">
    <div class="empty-state">
      <div class="empty-icon" aria-hidden="true">
        <Smartphone size={34} />
      </div>
      <h2>Select a connected device</h2>
      <p>Choose an online device from the toolbar above to view its installed packages.</p>
    </div>
  </section>
{:else if isLoading}
  <div class="loading-state store-loading">
    <div class="spin-large"><Loader2 size={32} /></div>
    <p>Querying third-party packages and matching against repository catalog...</p>
  </div>
{:else}
  <!-- FILTER BAR -->
  <div class="installed-filter-toolbar">
    <div class="installed-tabs">
      <button
        type="button"
        class="tab-button"
        class:active={selectedFilter === "all"}
        onclick={() => (selectedFilter = "all")}
      >
        All ({installedApps.length})
      </button>

      <button
        type="button"
        class="tab-button"
        class:active={selectedFilter === "updates"}
        onclick={() => (selectedFilter = "updates")}
      >
        Updates Available
        {#if updatesCount > 0}
          <span class="badge-count">{updatesCount}</span>
        {/if}
      </button>

      <button
        type="button"
        class="tab-button"
        class:active={selectedFilter === "fdroid"}
        onclick={() => (selectedFilter = "fdroid")}
      >
        Catalog Verified ({fdroidCount})
      </button>

      <button
        type="button"
        class="tab-button"
        class:active={selectedFilter === "other"}
        onclick={() => (selectedFilter = "other")}
      >
        Other ({installedApps.length - fdroidCount})
      </button>
    </div>

    <div class="search-box installed-search">
      <span class="search-icon" aria-hidden="true"><Search size={14} /></span>
      <input
        bind:value={filterQuery}
        placeholder="Filter by name or package ID..."
        aria-label="Filter packages"
      />
      {#if filterQuery}
        <button
          type="button"
          class="clear-search-button"
          onclick={() => (filterQuery = "")}
          aria-label="Clear filter"
        >
          <X size={13} />
        </button>
      {/if}
    </div>
  </div>

  <div class="app-grid installed-grid">
    {#each filteredApps as app}
      {@const pkgJob = store.getJobForPackage(app.id)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <article
        class="application-card installed-card"
        class:has-update-border={app.hasUpdate}
        onclick={() => {
          if (app.catalogMatch) {
            modalApp = app.catalogMatch;
          }
        }}
      >
        <!-- APP ICON (REAL LOGO OR STYLED GRADIENT MONOGRAM) -->
        <div class="application-icon installed-icon">
          {#if app.catalogMatch?.icon}
            <img
              loading="lazy"
              src={"/api/catalog/icon?path=" + encodeURIComponent(app.catalogMatch.icon)}
              alt={app.displayName}
              class="real-app-logo"
            />
          {:else}
            <div class="fallback-avatar" style="background: {app.gradientBg}">
              <span>{app.monogram}</span>
            </div>
          {/if}
        </div>

        <div class="card-details">
          <div class="card-header-line">
            <h3 class="card-title" title={app.displayName}>{app.displayName}</h3>
            {#if app.hasUpdate}
              <span class="pill update-badge">Update to v{app.catalogMatch?.versionName}</span>
            {:else if app.catalogMatch}
              <span class="pill verified-badge">Verified</span>
            {/if}
          </div>

          <p class="card-summary">
            {#if app.catalogMatch?.summary}
              {app.catalogMatch.summary}
            {:else}
              Installed third-party Android application
            {/if}
          </p>

          <div class="card-meta-line">
            <code class="package-id-code">{app.id}</code>
            {#if app.versionCode}
              <span class="version-meta">Code: {app.versionCode}</span>
            {/if}
          </div>

          <div class="card-actions-row">
            {#if app.catalogMatch}
              <button
                type="button"
                class="button secondary details-btn"
                onclick={(e) => {
                  e.stopPropagation();
                  modalApp = app.catalogMatch!;
                }}
              >
                App Details
                <ArrowUpRight size={13} />
              </button>
            {/if}

            {#if pkgJob}
              <button
                type="button"
                class="button secondary update-action-btn"
                onclick={(e) => {
                  e.stopPropagation();
                  store.cancelJob(pkgJob.id);
                }}
                title="Click to cancel"
              >
                {#if pkgJob.state === "queued"}
                  <Clock size={13} /> Queued · Cancel
                {:else}
                  <Loader2 size={13} class="spin" /> {pkgJob.progress ?? 0}% · Cancel
                {/if}
              </button>
            {:else if app.hasUpdate}
              <button
                type="button"
                class="button primary update-action-btn"
                disabled={updatingPackages.has(app.id)}
                onclick={(e) => updateApp(app, e)}
              >
                {#if updatingPackages.has(app.id)}
                  <Loader2 size={13} class="spin" /> Queueing...
                {:else if store.busyInstalling}
                  <Clock size={13} /> Queue Update
                {:else}
                  <Download size={13} /> Update Now
                {/if}
              </button>
            {/if}
          </div>
        </div>
      </article>
    {:else}
      <div class="empty-state" style="grid-column: 1 / -1;">
        <h2>No packages found</h2>
        <p>
          {#if filterQuery}
            No installed packages matched "{filterQuery}".
          {:else if selectedFilter === "updates"}
            All installed applications are up to date!
          {:else}
            No packages matched the selected filter.
          {/if}
        </p>
      </div>
    {/each}
  </div>

  <!-- APP DETAILS MODAL -->
  <AppDetailsModal
    app={modalApp}
    onclose={() => (modalApp = null)}
  />
{/if}
