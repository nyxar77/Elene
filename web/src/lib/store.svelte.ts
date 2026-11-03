// Global reactive application state powered by Svelte 5 runes.

import {
  api,
  type Device,
  type Job,
  type Preferences,
  type CachedAPK,
  type LogEntry,
} from "./api";

class AppStore {
  devices = $state<Device[]>([]);
  selectedDevice = $state<string>("");
  jobs = $state<Job[]>([]);
  cachedAPKs = $state<CachedAPK[]>([]);
  loadingCachedAPKs = $state(false);
  logs = $state<LogEntry[]>([]);
  loadingLogs = $state(false);
  installedPackages = $state<Record<string, number>>({});
  preferences = $state<Preferences>({
    theme: "light",
    accent: "mauve",
    contrast: "medium",
    interface: "enhanced",
    installStrategy: "auto",
    maxCachedAPKs: 10,
    cacheRetentionDays: 14,
  });

  installDialogOpen = $state(false);
  errorMessage = $state<string | null>(null);
  loadingDevices = $state(false);
  busyInstalling = $state(false);

  private deviceTimer: ReturnType<typeof setInterval> | null = null;
  private jobTimer: ReturnType<typeof setInterval> | null = null;

  constructor() {
    // Read pre-selected device from URL if available
    const params = new URLSearchParams(window.location.search);
    const initialDevice = params.get("device");
    if (initialDevice) {
      this.selectedDevice = initialDevice;
    }
  }

  async init() {
    await Promise.all([
      this.refreshDevices(),
      this.refreshJobs(),
      this.loadPreferences(),
    ]);

    if (this.selectedDevice) {
      await this.refreshInstalled(this.selectedDevice);
    }

    // Start background polling (adaptive polling when busy)
    this.deviceTimer = setInterval(() => this.refreshDevices(), 4000);
    this.startJobPolling();
  }

  private startJobPolling() {
    if (this.jobTimer) clearInterval(this.jobTimer);
    const interval = this.busyInstalling ? 1000 : 2500;
    this.jobTimer = setInterval(() => this.refreshJobs(), interval);
  }

  destroy() {
    if (this.deviceTimer) clearInterval(this.deviceTimer);
    if (this.jobTimer) clearInterval(this.jobTimer);
  }

  get onlineDevices(): Device[] {
    return this.devices.filter((d) => d.State === "online");
  }

  get activeDevice(): Device | undefined {
    return this.devices.find((d) => d.Serial === this.selectedDevice);
  }

  get activeJob(): Job | undefined {
    // Find active downloading or installing job
    const inProgress = this.jobs.find(
      (j) => j.state === "downloading" || j.state === "installing",
    );
    if (inProgress) return inProgress;

    // Next check if there is a queued job waiting
    const queued = this.jobs.find((j) => j.state === "queued");
    if (queued) return queued;

    // Or show recently finished job if created/updated within last 6 seconds
    if (this.jobs.length > 0) {
      const latest = this.jobs[0];
      const elapsed = Date.now() - new Date(latest.created).getTime();
      if (
        elapsed < 6000 &&
        (latest.state === "succeeded" ||
          latest.state === "failed" ||
          latest.state === "cancelled")
      ) {
        return latest;
      }
    }
    return undefined;
  }

  get queuedJobs(): Job[] {
    return this.jobs.filter((j) => j.state === "queued");
  }

  getJobForPackage(packageId: string): Job | undefined {
    return this.jobs.find(
      (j) =>
        (j.packageId === packageId || j.name === packageId) &&
        (j.state === "queued" ||
          j.state === "downloading" ||
          j.state === "installing"),
    );
  }

  async cancelJob(jobId: string) {
    try {
      await api.cancelJob(jobId);
      await this.refreshJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      this.setError("Could not cancel task: " + msg);
    }
  }

  isInstalled(packageId: string): boolean {
    return this.installedPackages[packageId] !== undefined;
  }

  getInstalledVersion(packageId: string): number | undefined {
    return this.installedPackages[packageId];
  }

  hasUpdate(packageId: string, catalogVersionCode?: number): boolean {
    const installedCode = this.installedPackages[packageId];
    if (installedCode === undefined) return false;
    if (!catalogVersionCode) return false;
    return catalogVersionCode > installedCode;
  }

  async refreshInstalled(serial?: string) {
    const targetSerial = serial || this.selectedDevice;
    if (!targetSerial) {
      this.installedPackages = {};
      return;
    }
    try {
      const list = await api.packages(targetSerial);
      const map: Record<string, number> = {};
      for (const item of list) {
        map[item.id] = item.versionCode ?? 0;
      }
      this.installedPackages = map;
    } catch {
      // Keep existing map on temporary read error
    }
  }

  async refreshDevices() {
    try {
      this.loadingDevices = true;
      const list = await api.devices();
      this.devices = list;

      // Auto-select first online device if current selection is invalid
      const currentValid = list.some(
        (d) => d.Serial === this.selectedDevice && d.State === "online",
      );
      if (!currentValid) {
        const firstOnline = list.find((d) => d.State === "online");
        if (firstOnline) {
          this.selectedDevice = firstOnline.Serial;
          this.refreshInstalled(firstOnline.Serial);
        }
      }
    } catch (err: unknown) {
      console.warn("Failed refreshing devices:", err);
    } finally {
      this.loadingDevices = false;
    }
  }

  async refreshJobs() {
    try {
      const list = await api.jobs();
      this.jobs = list;
      const wasBusy = this.busyInstalling;
      this.busyInstalling = list.some(
        (j) =>
          j.state === "queued" ||
          j.state === "downloading" ||
          j.state === "installing",
      );

      // If active state changed, adjust polling frequency
      if (wasBusy !== this.busyInstalling) {
        this.startJobPolling();
      }

      // If all jobs finished, refresh installed packages
      const currentActive = list.find(
        (j) =>
          j.state === "queued" ||
          j.state === "downloading" ||
          j.state === "installing",
      );
      if (!currentActive && wasBusy) {
        this.refreshInstalled();
      }
    } catch (err: unknown) {
      console.warn("Failed refreshing jobs:", err);
    }
  }

  async loadPreferences() {
    try {
      const prefs = await api.preferences();
      this.preferences = prefs;
      this.applyTheme(prefs);
    } catch (err: unknown) {
      console.warn("Failed loading preferences:", err);
    }
  }

  applyTheme(prefs: Preferences) {
    const root = document.documentElement;
    root.setAttribute("data-theme", prefs.theme);
    root.setAttribute("data-accent", prefs.accent);
    root.setAttribute("data-contrast", prefs.contrast);
  }

  async updatePreferences(newPrefs: Preferences) {
    try {
      const saved = await api.savePreferences(newPrefs);
      this.preferences = saved;
      this.applyTheme(saved);
      this.errorMessage = null;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      this.errorMessage = "Failed to save preferences: " + msg;
      throw err;
    }
  }

  async refreshCachedAPKs() {
    try {
      this.loadingCachedAPKs = true;
      this.cachedAPKs = await api.cachedAPKs();
    } catch (err: unknown) {
      console.warn("Failed loading cached APKs:", err);
    } finally {
      this.loadingCachedAPKs = false;
    }
  }

  async refreshLogs() {
    try {
      this.loadingLogs = true;
      this.logs = await api.logs();
    } catch (err: unknown) {
      console.warn("Failed loading logs:", err);
    } finally {
      this.loadingLogs = false;
    }
  }

  clearError() {
    this.errorMessage = null;
  }

  setError(msg: string) {
    this.errorMessage = msg;
  }
}

export const store = new AppStore();
