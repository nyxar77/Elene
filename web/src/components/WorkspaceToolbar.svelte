<script lang="ts">
  import { router } from "../lib/router.svelte";
  import { store } from "../lib/store.svelte";
  import { sourceOptions } from "../lib/api";
  import { RefreshCw } from "lucide-svelte";

  function onSourceChange(event: Event) {
    const target = event.target as HTMLSelectElement;
    router.navigate(router.path, {
      source: target.value,
      device: store.selectedDevice,
    });
  }

  function onDeviceChange(event: Event) {
    const target = event.target as HTMLSelectElement;
    store.selectedDevice = target.value;
    router.setParam("device", target.value);
  }

  function onRefresh() {
    store.refreshDevices();
    store.refreshJobs();
    store.refreshCachedAPKs();
  }

  const sourceLabels: Record<string, string> = {
    local: "Local APKs",
    fdroid: "F-Droid",
    aurora: "IzzyOnDroid / Aurora",
  };
</script>

<div class="workspace-toolbar">
  <div class="toolbar-inner">
    <span class="breadcrumb">
      Workspace / <span>{sourceLabels[router.source] ?? router.source}</span>
    </span>

    <div class="toolbar-actions">
      <select
        class="select-trigger source-dropdown"
        value={router.source}
        onchange={onSourceChange}
        aria-label="Application repository source"
      >
        {#each sourceOptions as opt}
          <option value={opt.value}>{opt.label}</option>
        {/each}
      </select>

      <select
        class="select-trigger"
        value={store.selectedDevice}
        onchange={onDeviceChange}
        aria-label="Selected Android device"
      >
        <option value="">Select a connected device</option>
        {#each store.devices as device}
          <option
            value={device.Serial}
            disabled={device.State !== "online"}
          >
            {device.Model || "Android Device"} · {device.Serial} ({device.State})
          </option>
        {/each}
      </select>

      <button
        class="button secondary"
        type="button"
        onclick={onRefresh}
        disabled={store.loadingDevices}
        title="Refresh connected devices and tasks"
      >
        <RefreshCw size={14} class={store.loadingDevices ? "spin" : ""} />
        Refresh
      </button>
    </div>
  </div>
</div>
