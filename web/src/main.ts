import { mount } from "svelte";
import App from "./App.svelte";
import "./styles.css";

const root = document.getElementById("root");
if (root) {
  // Clear the server-rendered fallback HTML before mounting the Svelte app
  root.innerHTML = "";
  mount(App, { target: root });
}
