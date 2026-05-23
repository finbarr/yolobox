import { join } from "node:path";
import { homedir } from "node:os";
import { StateStore } from "./state.js";
import { createBackend } from "./server.js";
import { digitalOceanProviderFromEnv } from "./digitalocean.js";

const listen = process.env.YOLOBOX_BACKEND_LISTEN || "127.0.0.1:8787";
const token = process.env.YOLOBOX_BACKEND_TOKEN;
if (!token) {
  throw new Error("YOLOBOX_BACKEND_TOKEN is required");
}

const providerName = (process.env.YOLOBOX_BACKEND_PROVIDER || "digitalocean").toLowerCase();
const provider = providerName === "digitalocean"
  ? digitalOceanProviderFromEnv()
  : (() => { throw new Error(`unknown backend provider ${providerName}`); })();

const statePath = process.env.YOLOBOX_BACKEND_STATE || join(homedir(), ".local", "state", "yolobox", "backend.json");
const store = new StateStore(statePath, provider.name);
const app = createBackend({ token, provider, store });

const { host, port } = parseListen(listen);
await app.listen({ host, port });
console.error(`yolobox backend listening on ${host}:${port} with ${provider.name}`);

function parseListen(value: string): { host: string; port: number } {
  const lastColon = value.lastIndexOf(":");
  if (lastColon === -1) return { host: "127.0.0.1", port: Number(value) };
  return {
    host: value.slice(0, lastColon) || "127.0.0.1",
    port: Number(value.slice(lastColon + 1)),
  };
}
