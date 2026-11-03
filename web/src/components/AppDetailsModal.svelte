<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { router } from "../lib/router.svelte";
  import { api, type CatalogApp } from "../lib/api";
  import {
    Download,
    Loader2,
    Check,
    X,
    Clock,
    Package,
    ArrowUpRight,
    Upload,
  } from "lucide-svelte";

  interface Props {
    app: CatalogApp | null;
    onclose: () => void;
  }

  let { app, onclose }: Props = $props();

  let isInstalling = $state(false);
  let installError = $state<string | null>(null);
  let fullAppDetails = $state<CatalogApp | null>(null);
  let isLoadingDetails = $state(false);

  $effect(() => {
    if (app?.id) {
      loadFullDetails(app.id);
    } else {
      fullAppDetails = null;
    }
  });

  async function loadFullDetails(id: string) {
    try {
      isLoadingDetails = true;
      const details = await api.catalogApp(id, router.source);
      fullAppDetails = details;
    } catch {
      // Fallback to basic app data
      fullAppDetails = app;
    } finally {
      isLoadingDetails = false;
    }
  }

  async function handleAction(downloadOnly: boolean = false) {
    if (!app) return;
    if (!downloadOnly && !store.selectedDevice) {
      installError = "Please select a connected device in the top toolbar first.";
      return;
    }

    const remoteSource = router.source === "aurora" ? "aurora" : "fdroid";

    try {
      isInstalling = true;
      installError = null;
      await api.installRemote(store.selectedDevice, remoteSource, app.id, downloadOnly);
      onclose();
      store.refreshJobs();
      if (downloadOnly) {
        store.refreshCachedAPKs();
      }
    } catch (err: unknown) {
      installError = err instanceof Error ? err.message : String(err);
    } finally {
      isInstalling = false;
    }
  }

  function formatBytes(bytes?: number): string {
    if (!bytes) return "";
    const mib = bytes / (1024 * 1024);
    return `${mib.toFixed(1)} MiB`;
  }

  function formatDate(epochMs?: number): string {
    if (!epochMs) return "";
    return new Date(epochMs).toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
  }

  const displayApp = $derived(fullAppDetails || app);
</script>

{#if app && displayApp}
  {@const pkgJob = store.getJobForPackage(displayApp.id)}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="dialog-overlay" onclick={onclose}>
    <div
      class="dialog-content app-details-modal"
      onclick={(e) => e.stopPropagation()}
      role="dialog"
      aria-modal="true"
      aria-labelledby="app-modal-title"
      tabindex="-1"
    >
      <div class="modal-header-hero">
        <div class="modal-app-icon">
          {#if displayApp.icon}
            <img
              src={"/api/catalog/icon?path=" + encodeURIComponent(displayApp.icon) + "&source=" + encodeURIComponent(router.source)}
              alt=""
            />
          {:else}
            <Package size={28} />
          {/if}
        </div>

        <div class="modal-app-title">
          <div style="display: flex; justify-content: space-between; align-items: flex-start; gap: 12px;">
            <h2 id="app-modal-title">{displayApp.name}</h2>
            <button
              type="button"
              class="icon-button modal-close"
              onclick={onclose}
              aria-label="Close"
            >
              <X size={15} />
            </button>
          </div>

          <p class="modal-app-summary">{displayApp.summary}</p>

          <div class="modal-badges">
            <span class="pill package-badge">{displayApp.id}</span>
            {#if displayApp.versionName}
              <span class="pill version-pill">v{displayApp.versionName}</span>
            {/if}
            {#if displayApp.size}
              <span class="pill">{formatBytes(displayApp.size)}</span>
            {/if}
            {#if displayApp.license}
              <span class="pill license-pill">{displayApp.license}</span>
            {/if}
          </div>
        </div>
      </div>

      {#if installError}
        <div class="error-message" role="alert" style="margin-top: 18px;">
          {installError}
        </div>
      {/if}

      <div class="modal-actions-bar">
        {#if pkgJob}
          <button
            type="button"
            class="button secondary"
            style="padding: 10px 22px; font-weight: 600;"
            onclick={() => store.cancelJob(pkgJob.id)}
          >
            {#if pkgJob.state === "queued"}
              <Clock size={15} /> Queued · Click to Cancel
            {:else if pkgJob.state === "downloading"}
              <Loader2 size={15} class="spin" /> Downloading {pkgJob.progress ?? 0}% · Cancel
            {:else}
              <Loader2 size={15} class="spin" /> Installing to device...
            {/if}
          </button>
        {:else if store.hasUpdate(displayApp.id, displayApp.versionCode)}
          <button
            type="button"
            class="button primary"
            style="padding: 10px 22px; font-weight: 600;"
            disabled={isInstalling || !store.selectedDevice}
            onclick={handleInstall}
          >
            {#if isInstalling}
              <Loader2 size={15} class="spin" /> Queueing...
            {:else if store.busyInstalling}
              <Clock size={15} /> Queue Update to v{displayApp.versionName || displayApp.versionCode}
            {:else}
              <Download size={15} /> Update to v{displayApp.versionName || displayApp.versionCode}
            {/if}
          </button>
        {:else if store.isInstalled(displayApp.id)}
          <button
            type="button"
            class="button secondary"
            style="padding: 10px 22px; font-weight: 600; opacity: 0.75;"
            disabled
            title="This application is already installed and up to date."
          >
            <Check size={15} strokeWidth={2.5} /> Installed (Up to date)
          </button>
        {:else}
          <div style="display: flex; gap: 8px; flex-wrap: wrap;">
            <button
              type="button"
              class="button primary"
              style="padding: 10px 22px; font-weight: 600;"
              disabled={isInstalling || !store.selectedDevice}
              onclick={() => handleAction(false)}
            >
              {#if isInstalling}
                <Loader2 size={15} class="spin" /> Queueing...
              {:else if !store.selectedDevice}
                Select a device in toolbar to install
              {:else if store.busyInstalling}
                <Clock size={15} /> Queue Install to {store.activeDevice?.Model || store.selectedDevice}
              {:else}
                <Upload size={15} /> Install to {store.activeDevice?.Model || store.selectedDevice}
              {/if}
            </button>

            <button
              type="button"
              class="button secondary"
              style="padding: 10px 18px;"
              disabled={isInstalling}
              onclick={() => handleAction(true)}
              title="Download APK file and store in local machine archive"
            >
              <Download size={14} /> Download Only
            </button>
          </div>
        {/if}

        <div class="modal-external-links">
          {#if displayApp.url}
            <a
              href={displayApp.url}
              target="_blank"
              rel="noopener noreferrer"
              class="button secondary"
            >
              Listing <ArrowUpRight size={13} />
            </a>
          {/if}
          {#if displayApp.sourceCode}
            <a
              href={displayApp.sourceCode}
              target="_blank"
              rel="noopener noreferrer"
              class="button secondary"
            >
              Source Code <ArrowUpRight size={13} />
            </a>
          {/if}
          {#if displayApp.issueTracker}
            <a
              href={displayApp.issueTracker}
              target="_blank"
              rel="noopener noreferrer"
              class="button secondary"
            >
              Issues <ArrowUpRight size={13} />
            </a>
          {/if}
        </div>
      </div>

      <div class="modal-details-body">
        <div class="details-section">
          <h3>About this application</h3>
          {#if displayApp.description}
            <div class="app-description-text">
              {#each displayApp.description.split("\n\n") as paragraph}
                <p>{paragraph}</p>
              {/each}
            </div>
          {:else if isLoadingDetails}
            <p class="muted">Loading full description...</p>
          {:else}
            <p>{displayApp.summary}</p>
          {/if}
        </div>

        <div class="details-section">
          <h3>Package details</h3>
          <dl class="details-spec-grid">
            <div>
              <dt>Package ID</dt>
              <dd><code>{displayApp.id}</code></dd>
            </div>
            <div>
              <dt>Latest Version</dt>
              <dd>
                {displayApp.versionName || "Unknown"}
                {#if displayApp.versionCode}
                  <small class="muted">({displayApp.versionCode})</small>
                {/if}
              </dd>
            </div>
            <div>
              <dt>Download Size</dt>
              <dd>{formatBytes(displayApp.size) || "Unknown"}</dd>
            </div>
            <div>
              <dt>Last Catalog Update</dt>
              <dd>{formatDate(displayApp.updated) || "Recent"}</dd>
            </div>
            <div>
              <dt>Categories</dt>
              <dd>
                {#each displayApp.categories as cat}
                  <span class="pill" style="margin-right: 6px; margin-bottom: 4px;">{cat}</span>
                {/each}
              </dd>
            </div>
            <div>
              <dt>License</dt>
              <dd>{displayApp.license || "Open Source"}</dd>
            </div>
          </dl>
        </div>
      </div>
    </div>
  </div>
{/if}
