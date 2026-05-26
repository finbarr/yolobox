import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createBackendAuth, migrateBackendAuth } from "./auth.js";
import { StateStore } from "./state.js";
import { createBackend } from "./server.js";
import { digitalOceanProviderFromEnv } from "./digitalocean.js";

const listen = process.env.YOLOBOX_BACKEND_LISTEN || "127.0.0.1:8787";
const authSecret = process.env.BETTER_AUTH_SECRET;
if (!authSecret) {
  throw new Error("BETTER_AUTH_SECRET is required");
}

const providerName = (process.env.YOLOBOX_BACKEND_PROVIDER || "digitalocean").toLowerCase();
const provider = providerName === "digitalocean"
  ? digitalOceanProviderFromEnv()
  : (() => { throw new Error(`unknown backend provider ${providerName}`); })();

const statePath = process.env.YOLOBOX_BACKEND_STATE || join(homedir(), ".local", "state", "yolobox", "backend.json");
const store = new StateStore(statePath, provider.name);
const { host, port } = parseListen(listen);
const defaultAppDir = resolve(dirname(fileURLToPath(import.meta.url)), "..", "dist-app");
const publicHost = host === "0.0.0.0" || host === "::" ? "127.0.0.1" : host;
const authBaseURL = process.env.BETTER_AUTH_URL || `http://${publicHost}:${port}/v1/auth`;
const defaultPublicURL = publicOrigin(authBaseURL) || `http://${host}:${port}`;
const apiPublicURL = trimURL(process.env.YOLOBOX_API_URL) || defaultPublicURL;
const appPublicURL = trimURL(process.env.YOLOBOX_APP_URL) || defaultPublicURL;
const authOptions = {
  baseURL: authBaseURL,
  databasePath: process.env.YOLOBOX_BACKEND_AUTH_DB || join(homedir(), ".local", "state", "yolobox", "auth.sqlite"),
  secret: authSecret,
  trustedOrigins: splitList(process.env.BETTER_AUTH_TRUSTED_ORIGINS),
  deviceVerificationURL: `${appPublicURL}/device`,
};
await migrateBackendAuth(authOptions);
const auth = createBackendAuth(authOptions);
const app = createBackend({
  auth,
  provider,
  store,
  appDir: process.env.YOLOBOX_BACKEND_APP_DIR || defaultAppDir,
  apiPublicURL,
  appPublicURL,
  corsOrigins: splitList(process.env.YOLOBOX_BACKEND_CORS_ORIGINS || process.env.BETTER_AUTH_TRUSTED_ORIGINS),
  previewBaseDomain: process.env.YOLOBOX_PREVIEW_BASE_DOMAIN,
  previewTargetPort: Number(process.env.YOLOBOX_PREVIEW_TARGET_PORT || 80),
});

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

function splitList(value: string | undefined): string[] {
  return (value || "").split(",").map((part) => part.trim()).filter(Boolean);
}

function publicOrigin(value: string): string {
  try {
    const url = new URL(value);
    return url.origin;
  } catch {
    return "";
  }
}

function trimURL(value: string | undefined): string {
  return (value || "").trim().replace(/\/+$/, "");
}
