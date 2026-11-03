<script lang="ts">
import { onMount, onDestroy } from "svelte";
import { router } from "./lib/router.svelte";
import { store } from "./lib/store.svelte";
import { X } from "lucide-svelte";

import Header from "./components/Header.svelte";
import WorkspaceToolbar from "./components/WorkspaceToolbar.svelte";
import InstallDialog from "./components/InstallDialog.svelte";
import InstallProgressBar from "./components/InstallProgressBar.svelte";

import Home from "./pages/Home.svelte";
import Installed from "./pages/Installed.svelte";
import Updates from "./pages/Updates.svelte";
import Activity from "./pages/Activity.svelte";
import Settings from "./pages/Settings.svelte";

onMount(() => {
	store.init();
});

onDestroy(() => {
	store.destroy();
});
</script>

<Header />
<WorkspaceToolbar />



<main class="app-main">
  {#if store.errorMessage}
    <div class="error-message" role="alert">
      <span>{store.errorMessage}</span>
      <button
        type="button"
        class="icon-button"
        style="float: right;"
        onclick={() => store.clearError()}
        aria-label="Dismiss error"
      >
        <X size={15} />
      </button>
    </div>
  {/if}

  {#if router.path === "/"}
    <Home />
  {:else if router.path === "/installed"}
    <Installed />
  {:else if router.path === "/updates"}
    <Updates />
  {:else if router.path === "/activity"}
    <Activity />
  {:else if router.path === "/settings"}
    <Settings />
  {:else}
    <Home />
  {/if}
</main>

<footer class="app-footer">
  <span>Elene · Local Android Installer</span>
  <span>Connected over USB Debugging · No External Servers</span>
</footer>

<InstallDialog />
<InstallProgressBar />
