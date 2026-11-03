<script lang="ts">
  import { store } from "../lib/store.svelte";
  import { themeOptions, accentOptions } from "../lib/api";
  import { Check, ShieldCheck, HardDrive, Cpu } from "lucide-svelte";

  let selectedTheme = $state(store.preferences.theme);
  let selectedAccent = $state(store.preferences.accent);
  let selectedContrast = $state(store.preferences.contrast);
  let selectedInterface = $state(store.preferences.interface);
  let selectedStrategy = $state(store.preferences.installStrategy || "auto");
  let selectedMaxCached = $state(store.preferences.maxCachedAPKs || 10);
  let selectedRetentionDays = $state(store.preferences.cacheRetentionDays || 14);

  let isSaving = $state(false);
  let saveSuccess = $state(false);
  let saveError = $state<string | null>(null);

  // Keep local state in sync when preferences load
  $effect(() => {
    selectedTheme = store.preferences.theme;
    selectedAccent = store.preferences.accent;
    selectedContrast = store.preferences.contrast;
    selectedInterface = store.preferences.interface;
    selectedStrategy = store.preferences.installStrategy || "auto";
    selectedMaxCached = store.preferences.maxCachedAPKs || 10;
    selectedRetentionDays = store.preferences.cacheRetentionDays || 14;
  });

  const isCatppuccin = $derived(
    ["latte", "frappe", "macchiato", "mocha"].includes(selectedTheme),
  );

  const isGruvbox = $derived(
    ["gruvbox-light", "gruvbox-dark"].includes(selectedTheme),
  );

  // Live preview as user selects appearance values
  $effect(() => {
    store.applyTheme({
      theme: selectedTheme,
      accent: selectedAccent,
      contrast: selectedContrast,
      interface: selectedInterface,
      installStrategy: selectedStrategy,
      maxCachedAPKs: selectedMaxCached,
      cacheRetentionDays: selectedRetentionDays,
    });
  });

  async function handleSave(event: SubmitEvent) {
    event.preventDefault();
    try {
      isSaving = true;
      saveSuccess = false;
      saveError = null;

      // Strict validation: Reject non-integers (e.g. 19.5 or 18.0)
      const maxStr = String(selectedMaxCached).trim();
      if (maxStr.includes(".") || isNaN(Number(maxStr))) {
        throw new Error("Maximum cached APKs must be a whole integer without decimals.");
      }
      const maxVal = parseInt(maxStr, 10);
      if (maxVal < 5 || maxVal > 20) {
        throw new Error("Maximum cached APKs must be between 5 and 20.");
      }

      const retStr = String(selectedRetentionDays).trim();
      if (retStr.includes(".") || isNaN(Number(retStr))) {
        throw new Error("Cache retention days must be a whole integer without decimals.");
      }
      const retVal = parseInt(retStr, 10);
      if (retVal < 1 || retVal > 30) {
        throw new Error("Cache retention duration must be between 1 and 30 days (max 1 month).");
      }

      await store.updatePreferences({
        theme: selectedTheme,
        accent: selectedAccent,
        contrast: selectedContrast,
        interface: selectedInterface,
        installStrategy: selectedStrategy,
        maxCachedAPKs: maxVal,
        cacheRetentionDays: retVal,
      });

      saveSuccess = true;
      setTimeout(() => {
        saveSuccess = false;
      }, 3000);
    } catch (err: unknown) {
      saveError = err instanceof Error ? err.message : String(err);
    } finally {
      isSaving = false;
    }
  }
</script>

<div class="page-heading">
  <div>
    <h1>Settings</h1>
    <p>Appearance, download strategy, and local APK cache retention preferences.</p>
  </div>
</div>

{#if saveError}
  <div class="error-message" role="alert">{saveError}</div>
{/if}

<form onsubmit={handleSave}>
  <!-- SECTION 1: DOWNLOAD & INSTALL STRATEGY -->
  <section class="panel settings-panel">
    <div class="settings-section-heading">
      <h2>Pipeline Strategy</h2>
      <p>Configure how store actions behave when clicking application cards.</p>
    </div>

    <div class="settings-fields">
      <label>
        Preferred Action Strategy
        <select class="select-trigger" bind:value={selectedStrategy}>
          <option value="auto">Auto (Adaptive) · Install if device is connected, download only otherwise</option>
          <option value="download_and_install">Download & Install · Always load to device via USB (requires device)</option>
          <option value="download_only">Download Only · Store APK in local machine cache without ADB install</option>
        </select>
        <small>
          In "Download Only" mode, APKs are verified and archived in <code>~/.cache/elene/apks/</code>. You can install them anytime later with 1 click from the Activity tab.
        </small>
      </label>
    </div>
  </section>

  <!-- SECTION 2: CACHE STORAGE LIMITS -->
  <section class="panel settings-panel">
    <div class="settings-section-heading">
      <h2>Local APK Storage & Cache Retention</h2>
      <p>Control the quantity and duration of downloaded APK archives stored on this machine.</p>
    </div>

    <div class="settings-fields">
      <label>
        Max Stored APKs (limit: 5 to 20, whole integer)
        <input
          type="number"
          min="5"
          max="20"
          step="1"
          class="select-trigger"
          bind:value={selectedMaxCached}
          required
        />
        <small>
          Number of APK files the system is allowed to keep in local cache. Range: 5 to 20. Decimal values are strictly rejected.
        </small>
      </label>

      <label>
        Retention Period (limit: 1 to 30 days, whole integer)
        <input
          type="number"
          min="1"
          max="30"
          step="1"
          class="select-trigger"
          bind:value={selectedRetentionDays}
          required
        />
        <small>
          Maximum duration to retain downloaded APK files before automated background pruning. Max: 30 days (1 month).
        </small>
      </label>
    </div>
  </section>

  <!-- SECTION 3: APPEARANCE -->
  <section class="panel settings-panel">
    <div class="settings-section-heading">
      <h2>Appearance</h2>
      <p>Choose a color scheme and accent for the interface.</p>
    </div>

    <div class="settings-fields">
      <label>
        Color scheme
        <select class="select-trigger" bind:value={selectedTheme}>
          {#each themeOptions as opt}
            <option value={opt.value}>{opt.label}</option>
          {/each}
        </select>
      </label>

      {#if isCatppuccin}
        <label>
          Accent
          <select class="select-trigger" bind:value={selectedAccent}>
            {#each accentOptions as opt}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </label>
      {/if}

      {#if isGruvbox}
        <label>
          Contrast
          <select class="select-trigger" bind:value={selectedContrast}>
            <option value="soft">Soft</option>
            <option value="medium">Medium</option>
            <option value="hard">Hard</option>
          </select>
        </label>
      {/if}
    </div>
  </section>

  <!-- SECTION 4: INTERFACE & SECURITY -->
  <section class="panel settings-panel">
    <div class="settings-section-heading">
      <h2>Interface & Security</h2>
      <p>Toggle client-side JavaScript execution.</p>
    </div>

    <div class="settings-fields">
      <label>
        JavaScript Mode
        <select class="select-trigger" bind:value={selectedInterface}>
          <option value="enhanced">Enabled · reactive Svelte application</option>
          <option value="basic">Disabled · server-rendered HTML forms</option>
        </select>
        <small>
          Selecting "Disabled" causes the daemon to block scripts via Content-Security-Policy (CSP)
          and render full HTML forms. You can also disable JavaScript directly in your browser.
        </small>
      </label>
    </div>
  </section>

  <div class="settings-actions">
    <span class="muted small">
      {#if saveSuccess}
        <Check size={14} strokeWidth={2.5} /> Preferences saved to ~/.config/elene/config.json
      {:else}
        Saved atomically to local disk.
      {/if}
    </span>

    <button type="submit" class="button primary" disabled={isSaving}>
      {#if isSaving}
        Saving...
      {:else}
        Save settings
      {/if}
    </button>
  </div>
</form>
