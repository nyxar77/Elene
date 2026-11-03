<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { router } from "../lib/router.svelte";
  import { Download, Loader2, Check, X, Clock, ArrowUpRight, Ban } from "lucide-svelte";

  const job = $derived(store.activeJob);
  const queuedCount = $derived(store.queuedJobs.length);

  function formatBytes(bytes?: number): string {
    if (!bytes) return "";
    const mib = bytes / (1024 * 1024);
    return `${mib.toFixed(1)} MiB`;
  }
</script>

{#if job}
  <aside
    class="install-progress-dock"
    class:state-queued={job.state === "queued"}
    class:state-downloading={job.state === "downloading"}
    class:state-installing={job.state === "installing"}
    class:state-succeeded={job.state === "succeeded"}
    class:state-failed={job.state === "failed" || job.state === "cancelled"}
    role="status"
    aria-live="polite"
    aria-label="Installation progress"
  >
    <div class="dock-inner">
      <div class="dock-icon-area" aria-hidden="true">
        {#if job.state === "queued"}
          <Clock size={19} />
        {:else if job.state === "downloading"}
          <Download size={19} />
        {:else if job.state === "installing"}
          <Loader2 size={19} class="spin" />
        {:else if job.state === "succeeded"}
          <span class="dock-icon success-mark"><Check size={19} strokeWidth={2.5} /></span>
        {:else if job.state === "cancelled"}
          <span class="dock-icon failure-mark"><Ban size={19} /></span>
        {:else}
          <span class="dock-icon failure-mark"><X size={19} strokeWidth={2.5} /></span>
        {/if}
      </div>

      <div class="dock-info">
        <div class="dock-headline">
          <strong class="dock-title">
            {#if job.state === "queued"}
              Queued {job.name}
            {:else if job.state === "downloading"}
              Downloading {job.name}
            {:else if job.state === "installing"}
              Installing {job.name}
            {:else if job.state === "succeeded"}
              Successfully installed {job.name}
            {:else if job.state === "cancelled"}
              Cancelled {job.name}
            {:else}
              Installation failed for {job.name}
            {/if}
          </strong>
          <span class="dock-device-tag">→ {store.activeDevice?.Model || job.serial}</span>
          {#if queuedCount > 0 && job.state !== "queued"}
            <span class="pill dock-queue-pill">+{queuedCount} queued</span>
          {:else if queuedCount > 1 && job.state === "queued"}
            <span class="pill dock-queue-pill">+{queuedCount - 1} more</span>
          {/if}
        </div>

        <div class="dock-subtitle">
          {#if job.state === "queued"}
            <span>Waiting in queue for current installation to finish...</span>
          {:else if job.state === "downloading"}
            {#if job.downloadedBytes && job.totalBytes}
              <span>{job.progress ?? 0}% · {formatBytes(job.downloadedBytes)} of {formatBytes(job.totalBytes)}</span>
            {:else}
              <span>{job.progress ?? 0}% · Streaming APK from verified mirror</span>
            {/if}
          {:else if job.state === "installing"}
            <span>Streaming and verifying package over USB debugging stream (adb install -r)...</span>
          {:else if job.state === "succeeded"}
            <span>Application is ready to use on your Android device.</span>
          {:else if job.state === "cancelled"}
            <span>Installation task was cancelled.</span>
          {:else if job.state === "failed"}
            <span class="dock-error">{job.error || "Installation could not be completed."}</span>
          {/if}
        </div>

        <!-- PROGRESS BAR -->
        <div class="progress-track" aria-hidden="true">
          <div
            class="progress-fill"
            class:is-indeterminate={job.state === "installing" || job.state === "queued"}
            style="width: {job.state === 'installing' || job.state === 'succeeded' ? 100 : (job.progress ?? 0)}%;"
          ></div>
        </div>
      </div>

      <div class="dock-actions">
        {#if job.state === "queued" || job.state === "downloading" || job.state === "installing"}
          <button
            type="button"
            class="button secondary dock-cancel-btn"
            onclick={() => store.cancelJob(job.id)}
            title="Cancel installation"
          >
            <X size={13} />
            Cancel
          </button>
        {/if}

        <button
          type="button"
          class="button secondary dock-activity-btn"
          onclick={() => router.navigate("/activity", { device: store.selectedDevice })}
        >
          Activity
          <ArrowUpRight size={13} />
        </button>
      </div>
    </div>
  </aside>
{/if}

<style>
  .install-progress-dock {
    position: fixed;
    bottom: 24px;
    left: 50%;
    transform: translateX(-50%);
    width: min(720px, calc(100vw - 32px));
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 16px;
    box-shadow: 0 16px 40px -8px rgba(0, 0, 0, 0.35);
    z-index: 999;
    padding: 14px 18px;
    backdrop-filter: blur(12px);
    transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);
    animation: dockSlideUp 0.3s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes dockSlideUp {
    from {
      opacity: 0;
      transform: translate(-50%, 20px);
    }
    to {
      opacity: 1;
      transform: translate(-50%, 0);
    }
  }

  .state-downloading,
  .state-installing {
    border-color: var(--accent);
  }

  .state-succeeded {
    border-color: #10b981;
  }

  .state-failed {
    border-color: #ef4444;
  }

  .dock-inner {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .dock-icon-area {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 10px;
    background: var(--surface);
    flex-shrink: 0;
    font-size: 18px;
  }

  .success-mark {
    color: #10b981;
    font-weight: 700;
  }

  .failure-mark {
    color: #ef4444;
    font-weight: 700;
  }

  .dock-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .dock-headline {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .dock-title {
    font-size: 13.5px;
    font-weight: 650;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .dock-device-tag {
    font-size: 11px;
    color: var(--muted);
    font-weight: 500;
  }

  .dock-subtitle {
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .dock-error {
    color: #ef4444;
  }

  /* Progress Bar Track & Fill */
  .progress-track {
    width: 100%;
    height: 6px;
    background: var(--surface);
    border-radius: 99px;
    overflow: hidden;
    margin-top: 4px;
    position: relative;
  }

  .progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 99px;
    transition: width 0.3s ease;
  }

  .progress-fill.is-indeterminate {
    background: linear-gradient(
      90deg,
      var(--accent) 0%,
      var(--on-accent) 50%,
      var(--accent) 100%
    );
    background-size: 200% 100%;
    animation: indeterminateShimmer 1.5s infinite linear;
  }

  @keyframes indeterminateShimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }

  .dock-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .dock-activity-btn,
  .dock-cancel-btn {
    padding: 6px 12px;
    font-size: 11px;
    font-weight: 600;
    min-height: 32px;
    gap: 5px;
  }

  .dock-cancel-btn:hover {
    border-color: #ef4444;
    color: #ef4444;
  }

  .dock-queue-pill {
    font-size: 9.5px;
    font-weight: 700;
    padding: 2px 7px;
    background: var(--accent);
    color: var(--on-accent);
    border-color: var(--accent);
  }
</style>
