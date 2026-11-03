<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { router } from "../lib/router.svelte";
  import { api, validateAPK } from "../lib/api";
  import { X } from "lucide-svelte";

  let selectedFile = $state<File | null>(null);
  let validationError = $state<string | null>(null);
  let isSubmitting = $state(false);
  let isDragging = $state(false);

  function close() {
    store.installDialogOpen = false;
    selectedFile = null;
    validationError = null;
    isSubmitting = false;
  }

  function handleFileSelect(event: Event) {
    const input = event.target as HTMLInputElement;
    if (input.files && input.files.length > 0) {
      setFile(input.files[0]);
    }
  }

  function setFile(file: File) {
    const error = validateAPK(file);
    if (error) {
      validationError = error;
      selectedFile = null;
    } else {
      validationError = null;
      selectedFile = file;
    }
  }

  function onDragOver(event: DragEvent) {
    event.preventDefault();
    isDragging = true;
  }

  function onDragLeave(event: DragEvent) {
    event.preventDefault();
    isDragging = false;
  }

  function onDrop(event: DragEvent) {
    event.preventDefault();
    isDragging = false;
    if (event.dataTransfer?.files && event.dataTransfer.files.length > 0) {
      setFile(event.dataTransfer.files[0]);
    }
  }

  async function handleSubmit(event: SubmitEvent) {
    event.preventDefault();
    if (!store.selectedDevice) {
      validationError = "Please select a connected device first.";
      return;
    }
    if (!selectedFile) {
      validationError = "Please choose a valid .apk file.";
      return;
    }

    try {
      isSubmitting = true;
      validationError = null;
      await api.installLocal(store.selectedDevice, selectedFile);
      close();
      router.navigate("/activity", { device: store.selectedDevice });
      store.refreshJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      validationError = msg;
    } finally {
      isSubmitting = false;
    }
  }
</script>

{#if store.installDialogOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="dialog-overlay" onclick={close}>
    <div
      class="dialog-content"
      onclick={(e) => e.stopPropagation()}
      role="dialog"
      aria-modal="true"
      aria-labelledby="dialog-title"
      tabindex="-1"
    >
      <div class="dialog-heading">
        <div>
          <h2 id="dialog-title">Install APK</h2>
          <p>Sideload an Android application package directly over USB debugging.</p>
        </div>
        <button
          type="button"
          class="icon-button"
          onclick={close}
          aria-label="Close dialog"
        >
          <X size={16} />
        </button>
      </div>

      {#if validationError}
        <div class="error-message" role="alert">{validationError}</div>
      {/if}

      <form onsubmit={handleSubmit}>
        <label class="field-label" for="dialog-device-select">Target Device</label>
        <select
          id="dialog-device-select"
          class="select-trigger"
          bind:value={store.selectedDevice}
          required
        >
          <option value="">Choose a device</option>
          {#each store.devices as device}
            <option
              value={device.Serial}
              disabled={device.State !== "online"}
            >
              {device.Model || "Android Device"} · {device.Serial} ({device.State})
            </option>
          {/each}
        </select>

        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <label
          class="file-picker"
          class:dragging={isDragging}
          ondragover={onDragOver}
          ondragleave={onDragLeave}
          ondrop={onDrop}
        >
          {#if selectedFile}
            <strong>{selectedFile.name}</strong>
            <span>{(selectedFile.size / (1024 * 1024)).toFixed(1)} MiB · Ready to install</span>
          {:else}
            <strong>Choose an APK file or drop it here</strong>
            <span>Single .apk · Maximum 512 MiB</span>
          {/if}
          <input
            type="file"
            accept=".apk"
            onchange={handleFileSelect}
            style="display: none;"
          />
        </label>

        <button
          type="submit"
          class="button primary full-width"
          disabled={isSubmitting || !selectedFile || !store.selectedDevice}
        >
          {#if isSubmitting}
            Installing package...
          {:else}
            Install to device
          {/if}
        </button>
      </form>
    </div>
  </div>
{/if}
