// Clean, lightweight client-side routing using the browser History API.

class Router {
  path = $state(window.location.pathname);
  search = $state(window.location.search);

  constructor() {
    window.addEventListener("popstate", () => {
      this.path = window.location.pathname;
      this.search = window.location.search;
    });
  }

  get query(): URLSearchParams {
    return new URLSearchParams(this.search);
  }

  get source(): string {
    const s = this.query.get("source");
    if (s === "fdroid" || s === "aurora") {
      return s;
    }
    return "local";
  }

  navigate(newPath: string, params?: Record<string, string>) {
    const searchParams = new URLSearchParams(this.search);
    if (params) {
      for (const [key, value] of Object.entries(params)) {
        if (value) {
          searchParams.set(key, value);
        } else {
          searchParams.delete(key);
        }
      }
    }
    const qs = searchParams.toString();
    const fullURL = newPath + (qs ? "?" + qs : "");

    window.history.pushState({}, "", fullURL);
    this.path = newPath;
    this.search = qs ? "?" + qs : "";
  }

  setParam(key: string, value: string) {
    this.navigate(this.path, { [key]: value });
  }
}

export const router = new Router();
