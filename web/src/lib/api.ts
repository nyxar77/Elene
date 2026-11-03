export interface Device {
  Serial: string;
  Model: string;
  State: "online" | "offline" | "unauthorized";
}

export interface Job {
  id: string;
  name: string;
  packageId?: string;
  serial: string;
  state:
    | "queued"
    | "downloading"
    | "installing"
    | "succeeded"
    | "failed"
    | "cancelled";
  progress?: number;
  downloadedBytes?: number;
  totalBytes?: number;
  source?: string;
  error?: string;
  created: string;
}

export interface Preferences {
  theme: string;
  accent: string;
  contrast: string;
  interface: "enhanced" | "basic";
  installStrategy: "auto" | "download_and_install" | "download_only";
  maxCachedAPKs: number;
  cacheRetentionDays: number;
}

export interface CachedAPK {
  sha256: string;
  packageId: string;
  name: string;
  versionName: string;
  size: number;
  downloaded: string;
}

export interface CatalogStatus {
  source: string;
  state: "idle" | "loading" | "ready" | "error";
  downloadedBytes: number;
  totalBytes: number;
  percent: number;
  error?: string;
  appCount: number;
  loadedAt?: string;
}

export interface LogEntry {
  timestamp: string;
  level: "INFO" | "WARN" | "ERROR" | "SUCCESS";
  source: string;
  message: string;
}

export interface CatalogApp {
  id: string;
  name: string;
  summary: string;
  description?: string;
  icon?: string;
  categories: string[];
  updated: number;
  url: string;
  license?: string;
  sourceCode?: string;
  issueTracker?: string;
  webSite?: string;
  versionName?: string;
  versionCode?: number;
  size?: number;
}

export interface CatalogPage {
  apps: CatalogApp[];
  total: number;
  page: number;
  pages: number;
  prev?: number;
  next?: number;
  categories: string[];
  fetched: string;
}

export interface InstalledApp {
  id: string;
  versionCode?: number;
}

export const sourceOptions = [
  { value: "local", label: "Local APKs" },
  { value: "fdroid", label: "F-Droid" },
  { value: "aurora", label: "IzzyOnDroid / Aurora" },
];

export const themeOptions = [
  { value: "light", label: "Elene Light" },
  { value: "dark", label: "Elene Dark" },
  { value: "dracula", label: "Dracula" },
  { value: "latte", label: "Catppuccin Latte" },
  { value: "frappe", label: "Catppuccin Frappé" },
  { value: "macchiato", label: "Catppuccin Macchiato" },
  { value: "mocha", label: "Catppuccin Mocha" },
  { value: "gruvbox-light", label: "Gruvbox Light" },
  { value: "gruvbox-dark", label: "Gruvbox Dark" },
];

export const accentOptions = [
  "rosewater",
  "flamingo",
  "pink",
  "mauve",
  "red",
  "maroon",
  "peach",
  "yellow",
  "green",
  "teal",
  "sky",
  "sapphire",
  "blue",
  "lavender",
].map((accent) => ({
  value: accent,
  label: accent.charAt(0).toUpperCase() + accent.slice(1),
}));

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  const data: unknown = await response.json();

  if (!response.ok) {
    const message =
      typeof data === "object" && data !== null && "error" in data
        ? String(data.error)
        : `Request to ${path} failed with status ${response.status}`;
    throw new Error(message);
  }

  return data as T;
}

let cachedToken: string | null = null;

export async function getSessionToken(): Promise<string> {
  if (!cachedToken) {
    const res = await request<{ token: string }>("/api/session");
    cachedToken = res.token;
  }
  return cachedToken;
}

export const api = {
  devices: () => request<Device[]>("/api/devices"),

  catalog: (query: string, category: string, page: number, source?: string) => {
    const params = new URLSearchParams();
    if (query) params.set("q", query);
    if (category) params.set("category", category);
    if (page > 1) params.set("page", String(page));
    if (source) params.set("source", source);
    const qs = params.toString();
    return request<CatalogPage>("/api/catalog" + (qs ? "?" + qs : ""));
  },

  catalogApp: (id: string, source?: string) => {
    const params = new URLSearchParams({ id });
    if (source) params.set("source", source);
    return request<CatalogApp>("/api/catalog/app?" + params.toString());
  },

  resolvePackages: async (packageIDs: string[]) => {
    const token = await getSessionToken();
    return request<Record<string, CatalogApp>>("/api/catalog/resolve", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Elene-Token": token,
      },
      body: JSON.stringify(packageIDs),
    });
  },

  jobs: () => request<Job[]>("/api/jobs"),

  preferences: () => request<Preferences>("/api/preferences"),

  packages: (deviceSerial: string) => {
    const params = new URLSearchParams({ device: deviceSerial });
    return request<InstalledApp[]>("/api/packages?" + params.toString());
  },

  async savePreferences(prefs: Preferences): Promise<Preferences> {
    const token = await getSessionToken();
    return request<Preferences>("/api/preferences", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        "X-Elene-Token": token,
      },
      body: JSON.stringify(prefs),
    });
  },

  async installLocal(deviceSerial: string, file: File): Promise<Job> {
    const token = await getSessionToken();
    const params = new URLSearchParams({
      serial: deviceSerial,
      name: file.name,
    });
    return request<Job>("/api/install?" + params.toString(), {
      method: "POST",
      headers: {
        "Content-Type": "application/vnd.android.package-archive",
        "X-Elene-Token": token,
      },
      body: file,
    });
  },

  async installRemote(
    deviceSerial: string,
    source: string,
    packageID: string,
    downloadOnly: boolean = false,
  ): Promise<Job> {
    const token = await getSessionToken();
    return request<Job>("/api/install-remote", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Elene-Token": token,
      },
      body: JSON.stringify({
        serial: deviceSerial,
        source: source,
        package: packageID,
        downloadOnly: downloadOnly,
      }),
    });
  },

  catalogStatus: (source: string) => {
    return request<CatalogStatus>(
      "/api/catalog/status?source=" + encodeURIComponent(source),
    );
  },

  refreshCatalog: async (source: string, force: boolean = false) => {
    const token = await getSessionToken();
    return request<CatalogStatus>(
      "/api/catalog/refresh?source=" +
        encodeURIComponent(source) +
        (force ? "&force=true" : ""),
      {
        method: "POST",
        headers: {
          "X-Elene-Token": token,
        },
      },
    );
  },

  cachedAPKs: () => request<CachedAPK[]>("/api/cached-apks"),

  async installCachedAPK(sha256: string, serial: string): Promise<Job> {
    const token = await getSessionToken();
    return request<Job>("/api/cached-apks/install", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Elene-Token": token,
      },
      body: JSON.stringify({ sha256, serial }),
    });
  },

  async deleteCachedAPK(sha256: string): Promise<{ deleted: boolean }> {
    const token = await getSessionToken();
    return request<{ deleted: boolean }>(
      "/api/cached-apks?sha256=" + encodeURIComponent(sha256),
      {
        method: "DELETE",
        headers: {
          "X-Elene-Token": token,
        },
      },
    );
  },

  logs: () => request<LogEntry[]>("/api/logs"),

  async clearLogs(): Promise<{ cleared: boolean }> {
    const token = await getSessionToken();
    return request<{ cleared: boolean }>("/api/logs/clear", {
      method: "POST",
      headers: {
        "X-Elene-Token": token,
      },
    });
  },

  async cancelJob(jobId: string): Promise<Job> {
    const token = await getSessionToken();
    return request<Job>("/api/jobs/cancel", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Elene-Token": token,
      },
      body: JSON.stringify({ id: jobId }),
    });
  },
};

export function validateAPK(
  file: Pick<File, "name" | "size"> | null | undefined,
): string | null {
  if (!file) {
    return "Please select an APK file.";
  }
  if (!file.name.toLowerCase().endsWith(".apk")) {
    return "Only single .apk files are supported.";
  }
  const maxBytes = 512 * 1024 * 1024;
  if (file.size === 0 || file.size > maxBytes) {
    return "The APK file must be non-empty and under 512 MiB.";
  }
  return null;
}
