<script lang="ts">
  import { router } from "../lib/router.svelte";
  import { store } from "../lib/store.svelte";
  import Logo from "./Logo.svelte";
  import { Settings } from "lucide-svelte";

  const navigation = [
    { path: "/", label: "Store" },
    { path: "/installed", label: "Installed apps" },
    { path: "/updates", label: "Updates" },
    { path: "/activity", label: "Activity" },
  ];

  function onNavClick(event: MouseEvent, path: string) {
    event.preventDefault();
    router.navigate(path, {
      device: store.selectedDevice,
      source: router.source,
    });
  }
</script>

<header class="app-header">
  <div class="header-inner">
    <a
      class="brand"
      href="/"
      onclick={(e) => onNavClick(e, "/")}
      aria-label="Elene home"
    >
      <Logo size={34} subtitle="ADB Station" />
    </a>

    <nav aria-label="Main navigation">
      {#each navigation as item}
        <a
          href={item.path}
          class:active={router.path === item.path}
          aria-current={router.path === item.path ? "page" : undefined}
          onclick={(e) => onNavClick(e, item.path)}
        >
          {item.label}
        </a>
      {/each}
    </nav>

    <div class="header-right-controls">
      <a
        href="/settings"
        class="settings-gear-btn"
        class:active={router.path === "/settings"}
        aria-label="Settings"
        aria-current={router.path === "/settings" ? "page" : undefined}
        title="Settings"
        onclick={(e) => onNavClick(e, "/settings")}
      >
        <Settings size={18} strokeWidth={2} />
      </a>
    </div>
  </div>
</header>

<style>
  .header-right-controls {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-left: auto;
  }

  .settings-gear-btn {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 8px;
    border: 1px solid var(--line);
    background: var(--panel);
    color: var(--muted);
    transition:
      color 0.15s ease,
      border-color 0.15s ease,
      background 0.15s ease;
    flex-shrink: 0;
  }

  .settings-gear-btn:hover {
    color: var(--text);
    border-color: var(--accent);
    background: var(--surface);
  }

  .settings-gear-btn.active {
    color: var(--on-accent);
    background: var(--accent);
    border-color: var(--accent);
  }
</style>
