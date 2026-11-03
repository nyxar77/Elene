<script lang="ts">
  interface Props {
    size?: number;
    showText?: boolean;
    subtitle?: string;
  }

  let { size = 34, showText = true, subtitle = "Local ADB Station" }: Props = $props();
</script>

<div class="brand-logo" style="--logo-size: {size}px;">
  <div class="logo-icon-wrapper" aria-hidden="true">
    <svg
      width={size}
      height={size}
      viewBox="0 0 200 200"
      class="logo-svg"
      role="img"
    >
      <defs>
        <mask id="brand-face-mask" maskUnits="userSpaceOnUse" x="0" y="0" width="200" height="200">
          <rect width="200" height="200" fill="#fff" />
          <!-- E cut out of the head -->
          <rect x="76" y="88" width="11" height="52" rx="3" fill="#000" />
          <rect x="76" y="88" width="48" height="11" rx="3" fill="#000" />
          <rect x="76" y="129" width="48" height="11" rx="3" fill="#000" />
          <rect x="76" y="108.5" width="30" height="11" rx="3" fill="#000" />
          <!-- the eye -->
          <circle cx="116" cy="114" r="8" fill="#000" />
        </mask>
      </defs>
      <!-- Head filled with theme accent color -->
      <path
        mask="url(#brand-face-mask)"
        fill="currentColor"
        d="M30 150V130A70 70 0 0 1 170 130V150Q170 160 160 160H40Q30 160 30 150Z"
      />
      <!-- Antennae stroked with theme accent color -->
      <g stroke="currentColor" stroke-width="8" stroke-linecap="round" fill="none">
        <line x1="68" y1="58" x2="50" y2="26" />
        <line x1="132" y1="58" x2="150" y2="26" />
      </g>
    </svg>
  </div>

  {#if showText}
    <div class="logo-text">
      <span class="logo-title">Elene</span>
      {#if subtitle}
        <span class="logo-subtitle">{subtitle}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .brand-logo {
    display: inline-flex;
    align-items: center;
    gap: 12px;
    user-select: none;
    text-decoration: none;
    color: inherit;
  }

  .logo-icon-wrapper {
    display: grid;
    place-items: center;
    line-height: 0;
    color: var(--accent);
    transition:
      transform 0.25s cubic-bezier(0.16, 1, 0.3, 1),
      color 0.2s ease,
      filter 0.2s ease;
  }

  :global(.brand:hover) .logo-icon-wrapper,
  .brand-logo:hover .logo-icon-wrapper {
    transform: scale(1.1) rotate(3deg);
    filter: drop-shadow(0 2px 10px var(--accent));
  }

  .logo-svg {
    display: block;
  }

  .logo-text {
    display: flex;
    flex-direction: column;
    line-height: 1.15;
  }

  .logo-title {
    font-size: 21px;
    font-weight: 750;
    letter-spacing: -0.6px;
    background: linear-gradient(135deg, var(--text) 40%, var(--muted));
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
  }

  .logo-subtitle {
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.8px;
    text-transform: uppercase;
    color: var(--muted);
  }
</style>
