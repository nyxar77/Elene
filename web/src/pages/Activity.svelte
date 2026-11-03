<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { router } from "../lib/router.svelte";
  import { api, type CachedAPK } from "../lib/api";
  import {
    Download,
    Loader2,
    Check,
    X,
    Clock,
    Ban,
    RefreshCw,
    Activity as ActivityIcon,
    Terminal,
    Copy,
    Trash2,
    Smartphone,
    HardDrive,
    Package,
    ArrowUpRight,
  } from "lucide-svelte";

  let activeSubTab = $state<"tasks" | "logs">("tasks");
  let isInstallingCached = $state<string | null>(null);
  let isDeletingCached = $state<string | null>(null);
  let copiedLogs = $state(false);

  $effect(() => {
    store.refreshJobs();
    store.refreshCachedAPKs();
    if (activeSubTab === "logs") {
      store.refreshLogs();
    }
  });

  function formatTime(isoString: string): string {
    try {
      const date = new Date(isoString);
      return date.toLocaleTimeString([], {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      });
    } catch {
      return isoString;
    }
  }

  function formatFileSize(bytes: number): string {
    if (!bytes || bytes <= 0) return "0 Mo";
    const mb = bytes / (1024 * 1024);
    if (mb < 0.1) return (bytes / 1024).toFixed(0) + " Ko";
    return mb.toFixed(1) + " Mo";
  }

  async function installCached(apk: CachedAPK) {
    if (!store.selectedDevice) {
      store.setError("Please select an online device in the top toolbar first.");
      return;
    }
    try {
      isInstallingCached = apk.sha256;
      store.clearError();
      await api.installCachedAPK(apk.sha256, store.selectedDevice);
      await store.refreshJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      store.setError("Failed to install cached APK: " + msg);
    } finally {
      isInstallingCached = null;
    }
  }

  async function deleteCached(apk: CachedAPK) {
    try {
      isDeletingCached = apk.sha256;
      await api.deleteCachedAPK(apk.sha256);
      await store.refreshCachedAPKs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      store.setError("Failed to delete cached APK: " + msg);
    } finally {
      isDeletingCached = null;
    }
  }

  async function copyLogs() {
    try {
      const lines = store.logs.map(
        (l) => `[${formatTime(l.timestamp)}] [${l.level}] [${l.source}] ${l.message}`,
      );
      await navigator.clipboard.writeText(lines.join("\n"));
      copiedLogs = true;
      setTimeout(() => {
        copiedLogs = false;
      }, 2500);
    } catch {
      store.setError("Could not copy logs to clipboard.");
    }
  }

  async function clearLogs() {
    try {
      await api.clearLogs();
      await store.refreshLogs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      store.setError("Could not clear logs: " + msg);
    }
  }
</script>

<div class="page-heading">
  <div>
    <h1>Activity & Downloads</h1>
    <p>Monitor installation tasks, inspect downloaded APK archives, and view diagnostic logs.</p>
  </div>
  <div style="display: flex; gap: 8px;">
    <button
      class="button secondary"
      type="button"
      onclick={() => {
        store.refreshJobs();
        store.refreshCachedAPKs();
        if (activeSubTab === "logs") store.refreshLogs();
      }}
      title="Refresh status"
    >
      <RefreshCw size={14} />
      Refresh
    </button>
  </div>
</div>

<!-- SUB-TABS NAVIGATION -->
<div class="sub-tab-bar" role="tablist">
  <button
    type="button"
    role="tab"
    class="sub-tab-btn"
    class:active={activeSubTab === "tasks"}
    aria-selected={activeSubTab === "tasks"}
    onclick={() => (activeSubTab = "tasks")}
  >
    <HardDrive size={15} />
    Installations & Downloads
    {#if store.cachedAPKs.length > 0}
      <span class="sub-tab-pill">{store.cachedAPKs.length}</span>
    {/if}
  </button>
  <button
    type="button"
    role="tab"
    class="sub-tab-btn"
    class:active={activeSubTab === "logs"}
    aria-selected={activeSubTab === "logs"}
    onclick={() => {
      activeSubTab = "logs";
      store.refreshLogs();
    }}
  >
    <Terminal size={15} />
    Activity Logs
    {#if store.logs.length > 0}
      <span class="sub-tab-pill">{store.logs.length}</span>
    {/if}
  </button>
</div>

{#if activeSubTab === "tasks"}
  <!-- SECTION 1: RUNNING & RECENT TASKS -->
  <div class="activity-section-header">
    <h2>Active Tasks & Queue</h2>
    <span class="muted small">{store.jobs.length} recorded in session</span>
  </div>

  <section class="panel" style="margin-bottom: 30px;">
    <div class="job-list">
      {#each store.jobs as job}
        <article class="job-row">
          <div class="application-icon" aria-hidden="true">
            {#if job.state === "queued"}
              <Clock size={18} />
            {:else if job.state === "downloading"}
              <Download size={18} />
            {:else if job.state === "installing"}
              <Loader2 size={18} class="spin" />
            {:else if job.state === "succeeded"}
              <Check size={18} strokeWidth={2.5} />
            {:else if job.state === "cancelled"}
              <Ban size={18} />
            {:else}
              <X size={18} strokeWidth={2.5} />
            {/if}
          </div>

          <div class="job-description">
            <h2>{job.name}</h2>
            <p>
              Target: <strong>{job.serial || "Local Machine"}</strong> · {formatTime(job.created)}
              {#if job.source}
                · Source: {job.source.toUpperCase()}
              {/if}
              {#if job.downloadOnly}
                · <span class="badge-download-only">Download Only</span>
              {/if}
            </p>

            {#if job.state === "downloading" || job.state === "installing" || job.state === "queued"}
              <div style="margin: 8px 0; max-width: 440px;">
                <div style="height: 6px; background: var(--surface); border-radius: 99px; overflow: hidden;">
                  <div
                    class:is-indeterminate={job.state === "installing" || job.state === "queued"}
                    style="height: 100%; background: var(--accent); transition: width 0.3s ease; width: {job.state === 'installing' ? 100 : (job.progress ?? 0)}%;"
                  ></div>
                </div>
                <small class="muted" style="margin-top: 4px; display: inline-block;">
                  {#if job.state === "queued"}
                    Waiting in installation queue...
                  {:else if job.state === "downloading"}
                    {#if job.downloadedBytes && job.totalBytes}
                      {job.progress ?? 0}% ({formatFileSize(job.downloadedBytes)} / {formatFileSize(job.totalBytes)})
                    {:else}
                      {job.progress ?? 0}% downloading
                    {/if}
                  {:else}
                    Installing to device via ADB...
                  {/if}
                </small>
              </div>
            {/if}

            {#if job.error}
              <p class="error-text">{job.error}</p>
            {/if}
          </div>

          <div style="display: flex; align-items: center; gap: 10px;">
            {#if job.state === "queued" || job.state === "downloading" || job.state === "installing"}
              <button
                type="button"
                class="button secondary details-btn"
                onclick={() => store.cancelJob(job.id)}
                title="Cancel this task"
              >
                <X size={13} />
                Cancel
              </button>
            {/if}

            <span class={"job-status " + job.state} style="display: inline-flex; align-items: center; gap: 5px;">
              {#if job.state === "queued"}
                <Clock size={13} /> Queued
              {:else if job.state === "downloading"}
                <Loader2 size={13} class="spin" /> {job.progress ?? 0}%
              {:else if job.state === "installing"}
                <Loader2 size={13} class="spin" /> Installing...
              {:else if job.state === "succeeded"}
                <Check size={13} strokeWidth={2.5} /> Succeeded
              {:else if job.state === "cancelled"}
                <Ban size={13} /> Cancelled
              {:else if job.state === "failed"}
                <X size={13} strokeWidth={2.5} /> Failed
              {:else}
                {job.state}
              {/if}
            </span>
          </div>
        </article>
      {:else}
        <div class="empty-state">
          <div class="empty-icon" aria-hidden="true">
            <ActivityIcon size={32} />
          </div>
          <h2>No tasks in queue</h2>
          <p>Install or download an APK from the store or your local files to see live task progress here.</p>
        </div>
      {/each}
    </div>
  </section>

  <!-- SECTION 2: DOWNLOADED APKS IN LOCAL STORAGE -->
  <div class="activity-section-header">
    <div>
      <h2>Downloaded APKs on this Machine</h2>
      <p class="muted small">
        Cached in <code>~/.cache/elene/apks/</code>. You can install any APK to your connected phone with 1 click.
      </p>
    </div>
    <span class="pill-count">{store.cachedAPKs.length} stored</span>
  </div>

  <section class="panel">
    {#if store.loadingCachedAPKs}
      <div class="loading-state" style="padding: 30px;">
        <Loader2 size={24} class="spin" />
        <p>Scanning local APK repository...</p>
      </div>
    {:else if store.cachedAPKs.length > 0}
      <div class="cached-apk-list">
        {#each store.cachedAPKs as apk}
          <div class="cached-apk-card">
            <div class="cached-apk-icon" aria-hidden="true">
              <Package size={22} />
            </div>

            <div class="cached-apk-info">
              <div class="cached-apk-title-line">
                <h3>{apk.name}</h3>
                {#if apk.versionName}
                  <span class="version-tag">v{apk.versionName}</span>
                {/if}
              </div>
              <div class="cached-apk-meta">
                {#if apk.packageId}
                  <span class="pkg-id">{apk.packageId}</span>
                  <span class="separator">·</span>
                {/if}
                <span>{formatFileSize(apk.size)}</span>
                <span class="separator">·</span>
                <span class="muted">{formatTime(apk.downloaded)}</span>
                <span class="separator">·</span>
                <code class="sha-preview" title={apk.sha256}>sha: {apk.sha256.slice(0, 10)}...</code>
              </div>
            </div>

            <div class="cached-apk-actions">
              <button
                type="button"
                class="button primary install-cached-btn"
                disabled={isInstallingCached === apk.sha256 || !store.selectedDevice}
                onclick={() => installCached(apk)}
                title={store.selectedDevice ? "Install directly to " + store.selectedDevice : "Connect and select a device in top bar"}
              >
                {#if isInstallingCached === apk.sha256}
                  <Loader2 size={13} class="spin" />
                  Installing...
                {:else}
                  <Smartphone size={13} />
                  Install to Device
                {/if}
              </button>

              <button
                type="button"
                class="button secondary delete-cached-btn"
                disabled={isDeletingCached === apk.sha256}
                onclick={() => deleteCached(apk)}
                title="Remove this APK from local disk cache"
              >
                {#if isDeletingCached === apk.sha256}
                  <Loader2 size={13} class="spin" />
                {:else}
                  <Trash2 size={13} />
                {/if}
              </button>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <div class="empty-state">
        <div class="empty-icon" aria-hidden="true">
          <HardDrive size={32} />
        </div>
        <h2>No APKs in local storage</h2>
        <p>
          Set your preference to <em>"Download Only"</em> or browse apps in the Store to archive APKs on this machine.
        </p>
        <button
          class="button secondary"
          type="button"
          onclick={() => router.navigate("/", { source: "fdroid" })}
        >
          Browse F-Droid Store
        </button>
      </div>
    {/if}
  </section>
{:else}
  <!-- SUB-TAB 2: LOGS & DIAGNOSTICS -->
  <div class="activity-section-header">
    <div>
      <h2>Operational & Execution Logs</h2>
      <p class="muted small">Chronological ADB events, repository sync diagnostics, and error stacktraces.</p>
    </div>
    <div style="display: flex; gap: 8px;">
      <button
        type="button"
        class="button secondary"
        onclick={copyLogs}
        title="Copy complete log history to clipboard"
      >
        {#if copiedLogs}
          <Check size={13} strokeWidth={2.5} />
          Copied!
        {:else}
          <Copy size={13} />
          Copy Logs
        {/if}
      </button>

      <button
        type="button"
        class="button secondary"
        onclick={clearLogs}
        title="Clear stored logs"
      >
        <Trash2 size={13} />
        Clear
      </button>
    </div>
  </div>

  <section class="panel console-panel">
    <div class="log-console-container">
      {#if store.logs.length === 0}
        <div class="console-empty">No activity logs recorded yet.</div>
      {:else}
        {#each store.logs as log}
          <div class="log-line" class:log-error={log.level === "ERROR"} class:log-warn={log.level === "WARN"} class:log-success={log.level === "SUCCESS"}>
            <span class="log-ts">[{formatTime(log.timestamp)}]</span>
            <span class={"log-badge log-badge-" + log.level.toLowerCase()}>{log.level}</span>
            <span class="log-src">[{log.source}]</span>
            <span class="log-msg">{log.message}</span>
          </div>
        {/each}
      {/if}
    </div>
  </section>
{/if}
