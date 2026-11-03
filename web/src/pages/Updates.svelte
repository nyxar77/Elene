<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { router } from "../lib/router.svelte";
  import { api, type CatalogApp, type InstalledApp } from "../lib/api";
  import AppDetailsModal from "../components/AppDetailsModal.svelte";
  import {
    Download,
    Loader2,
    Check,
    X,
    Clock,
    Smartphone,
    Package,
    ArrowUpRight,
    ArrowRight,
  } from "lucide-svelte";

  interface UpdateItem {
    id: string;
    installedVersionCode?: number;
    catalogApp: CatalogApp;
  }

  let isLoading = $state(false);
  let errorMsg = $state<string | null>(null);
  let availableUpdates = $state<UpdateItem[]>([]);
  let updatingSet = $state<Set<string>>(new Set());
  let isUpdatingAll = $state(false);
  let modalApp = $state<CatalogApp | null>(null);

  $effect(() => {
    if (store.selectedDevice) {
      checkForUpdates(store.selectedDevice);
    } else {
      availableUpdates = [];
    }
  });

  async function checkForUpdates(serial: string) {
    try {
      isLoading = true;
      errorMsg = null;

      // 1. Get installed packages
      const rawPackages: InstalledApp[] = await api.packages(serial);

      // 2. Resolve against repository catalogs
      const packageIDs = rawPackages.map((p) => p.id);
      const catalogMatches = await api.resolvePackages(packageIDs);

      // 3. Filter for newer versions
      const updates: UpdateItem[] = [];
      for (const raw of rawPackages) {
        const match = catalogMatches[raw.id];
        if (
          match &&
          match.versionCode &&
          raw.versionCode &&
          match.versionCode > raw.versionCode
        ) {
          updates.push({
            id: raw.id,
            installedVersionCode: raw.versionCode,
            catalogApp: match,
          });
        }
      }

      availableUpdates = updates;
    } catch (err: unknown) {
      errorMsg = err instanceof Error ? err.message : String(err);
      availableUpdates = [];
    } finally {
      isLoading = false;
    }
  }

  async function updateSingleApp(item: UpdateItem, event?: MouseEvent) {
    if (event) {
      event.stopPropagation();
    }
    if (!store.selectedDevice) return;
    try {
      updatingSet.add(item.id);
      updatingSet = new Set(updatingSet);
      await api.installRemote(store.selectedDevice, "fdroid", item.id);
      await store.refreshJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      store.setError("Failed to queue update for " + item.catalogApp.name + ": " + msg);
    } finally {
      updatingSet.delete(item.id);
      updatingSet = new Set(updatingSet);
    }
  }

  async function updateAllApps() {
    if (!store.selectedDevice || availableUpdates.length === 0) return;
    try {
      isUpdatingAll = true;
      for (const item of availableUpdates) {
        try {
          updatingSet.add(item.id);
          updatingSet = new Set(updatingSet);
          await api.installRemote(store.selectedDevice, "fdroid", item.id);
        } catch {
          // Continue queueing remaining apps
        }
      }
      await store.refreshJobs();
    } finally {
      isUpdatingAll = false;
      updatingSet.clear();
      updatingSet = new Set();
    }
  }

  function formatBytes(bytes?: number): string {
    if (!bytes) return "";
    const mib = bytes / (1024 * 1024);
    return `${mib.toFixed(1)} MiB`;
  }
</script>

<div class="page-heading">
  <div>
    <h1>Application Updates</h1>
    <p>Compare installed applications with verified releases in the F-Droid &amp; IzzyOnDroid repositories.</p>
  </div>
  {#if store.selectedDevice && availableUpdates.length > 0}
    <span class="pill update-pill-highlight" style="gap: 5px;">
      <Download size={12} />
      {availableUpdates.length} updates available
    </span>
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
      <p>Choose an online device from the toolbar above to check for application updates.</p>
    </div>
  </section>
{:else if isLoading}
  <div class="loading-state store-loading">
    <div class="spin-large"><Loader2 size={32} /></div>
    <p>Comparing installed package versions with repository releases...</p>
  </div>
{:else if availableUpdates.length > 0}
  <div class="updates-header-banner">
    <div>
      <h2>{availableUpdates.length} updates ready to install</h2>
      <p>
        Target device: <strong>{store.activeDevice?.Model || store.selectedDevice}</strong>.
        Updates are queued sequentially and applied in place via ADB preserving application data.
      </p>
    </div>

    <button
      type="button"
      class="button primary update-all-btn"
      disabled={isUpdatingAll}
      onclick={updateAllApps}
    >
      {#if isUpdatingAll}
        <Loader2 size={15} class="spin" /> Queueing Updates...
      {:else}
        <Download size={15} /> Update All ({availableUpdates.length})
      {/if}
    </button>
  </div>

  <div class="app-grid updates-grid">
    {#each availableUpdates as item}
      {@const pkgJob = store.getJobForPackage(item.id)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <article
        class="application-card update-card"
        onclick={() => (modalApp = item.catalogApp)}
      >
        <div class="application-icon update-icon">
          {#if item.catalogApp.icon}
            <img
              loading="lazy"
              src={"/api/catalog/icon?path=" + encodeURIComponent(item.catalogApp.icon)}
              alt=""
            />
          {:else}
            <Package size={24} />
          {/if}
        </div>

        <div class="card-details">
          <div class="card-header-line">
            <h3 class="card-title">{item.catalogApp.name}</h3>
            <span class="pill update-badge">Update Available</span>
          </div>

          <div class="version-diff-box">
            <span class="diff-from">Code {item.installedVersionCode}</span>
            <span class="diff-arrow"><ArrowRight size={12} /></span>
            <span class="diff-to">v{item.catalogApp.versionName || item.catalogApp.versionCode}</span>
            {#if item.catalogApp.size}
              <span class="diff-size">({formatBytes(item.catalogApp.size)})</span>
            {/if}
          </div>

          <p class="card-summary">{item.catalogApp.summary}</p>
          <code class="package-id-code">{item.id}</code>

          <div class="card-actions-row" style="margin-top: 14px;">
            <button
              type="button"
              class="button secondary details-btn"
              onclick={(e) => {
                e.stopPropagation();
                modalApp = item.catalogApp;
              }}
            >
              Release Notes
              <ArrowUpRight size={13} />
            </button>

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
                {:else if pkgJob.state === "downloading"}
                  <Loader2 size={13} class="spin" /> {pkgJob.progress ?? 0}% · Cancel
                {:else}
                  <Loader2 size={13} class="spin" /> Installing...
                {/if}
              </button>
            {:else if !store.hasUpdate(item.id, item.catalogApp.versionCode) && store.isInstalled(item.id)}
              <button
                type="button"
                class="button secondary update-action-btn installed-badge-btn"
                disabled
              >
                <Check size={13} strokeWidth={2.5} /> Updated
              </button>
            {:else}
              <button
                type="button"
                class="button primary update-action-btn"
                disabled={updatingSet.has(item.id) || isUpdatingAll}
                onclick={(e) => updateSingleApp(item, e)}
              >
                {#if updatingSet.has(item.id)}
                  <Loader2 size={13} class="spin" /> Queueing...
                {:else if store.busyInstalling}
                  <Clock size={13} /> Queue Update
                {:else}
                  <Download size={13} /> Update
                {/if}
              </button>
            {/if}
          </div>
        </div>
      </article>
    {/each}
  </div>

  <!-- APP DETAILS MODAL -->
  <AppDetailsModal
    app={modalApp}
    onclose={() => (modalApp = null)}
  />

{:else}
  <!-- CELEBRATORY UP-TO-DATE STATE -->
  <section class="panel">
    <div class="empty-state up-to-date-state">
      <div class="celebrate-icon" aria-hidden="true">
        <Check size={32} strokeWidth={2.5} />
      </div>
      <h2>All applications are up to date!</h2>
      <p>
        Every matched application on <strong>{store.activeDevice?.Model || store.selectedDevice}</strong>
        is currently running the latest version available in the catalog.
      </p>

      <div style="display: flex; gap: 12px; margin-top: 24px; justify-content: center;">
        <button
          type="button"
          class="button secondary"
          onclick={() => store.selectedDevice && checkForUpdates(store.selectedDevice)}
        >
          Check again
        </button>

        <button
          type="button"
          class="button primary"
          onclick={() => router.navigate("/", { source: "fdroid" })}
        >
          Explore Store
        </button>
      </div>
    </div>
  </section>
{/if}
