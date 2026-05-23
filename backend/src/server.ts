import { createHash } from "node:crypto";
import { readFile, stat } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import cors from "@fastify/cors";
import Fastify, { FastifyInstance } from "fastify";
import { BackendAuth } from "./auth.js";
import { StateStore } from "./state.js";
import { EnsureMachineRequest, MachineProvider, MachineProviderInfo, RemoteMachine, defaultProjectPath } from "./types.js";

export type BackendOptions = {
  auth: BackendAuth;
  provider: MachineProvider;
  store: StateStore;
  appDir?: string;
  apiPublicURL?: string;
  appPublicURL?: string;
  corsOrigins?: string[];
};

type AuthContext = {
  userID: string;
  email: string;
};

const namePattern = /^[a-z0-9][a-z0-9-]{0,62}$/;

export function createBackend(options: BackendOptions): FastifyInstance {
  const app = Fastify({ logger: false });
  registerCors(app, options.corsOrigins || []);

  app.get("/healthz", async () => "ok\n");

  app.get("/v1/providers", async () => ({
    providers: [providerInfo(options.provider)],
  }));

  app.get("/v1/auth/whoami", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    return {
      authenticated: true,
      provider: options.provider.name,
      user: {
        id: auth.userID,
        email: auth.email,
      },
    };
  });

  app.all("/v1/auth/*", async (request, reply) => {
    await sendAuthResponse(reply, await options.auth.handler(toWebRequest(request)));
  });

  app.post<{ Body: EnsureMachineRequest }>("/v1/machines/ensure", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const body = normalizeEnsureRequest(request.body, options.provider.name);
    if (body.provider && body.provider !== options.provider.name) {
      return reply.code(400).send({ id: "bad_request", message: `provider ${body.provider} is not configured` });
    }
    const error = validateName(body.name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    return leaseMachine(options, auth, body);
  });

  app.post<{ Body: EnsureMachineRequest }>("/v1/machines", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const body = normalizeEnsureRequest(request.body, options.provider.name);
    if (body.provider && body.provider !== options.provider.name) {
      return reply.code(400).send({ id: "bad_request", message: `provider ${body.provider} is not configured` });
    }
    const error = validateName(body.name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    return leaseMachine(options, auth, body);
  });

  app.get("/v1/machines", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    return {
      machines: await listAccountMachines(options, auth),
    };
  });

  app.get<{ Params: { name: string } }>("/v1/machines/:name", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    let existing = await options.store.getMachine(auth.userID, name);
    if (!existing) {
      await syncProviderMachines(options, auth);
      existing = await options.store.getMachine(auth.userID, name);
    }
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine is not leased" });
    const refreshed = await options.provider.getMachine(existing);
    const machine = normalizeMachine(options.provider, { ...existing, ...refreshed.machine, name, user_id: auth.userID });
    await options.store.putMachine(machine);
    return { machine, status: refreshed.status || "leased" };
  });

  app.get<{ Params: { name: string } }>("/v1/machines/:name/connect", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    let existing = await options.store.getMachine(auth.userID, name);
    if (!existing) {
      await syncProviderMachines(options, auth);
      existing = await options.store.getMachine(auth.userID, name);
    }
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine is not leased" });
    const refreshed = await options.provider.getMachine(existing);
    const machine = normalizeMachine(options.provider, { ...existing, ...refreshed.machine, name, user_id: auth.userID });
    await options.store.putMachine(machine);
    const target = sshTarget(machine);
    return {
      machine,
      status: refreshed.status || "leased",
      connect: {
        ssh: target ? `ssh ${target}` : "",
        cli: `yolobox remote connect ${machine.name}`,
        cli_run: `yolobox remote --name ${machine.name}`,
      },
    };
  });

  app.patch<{ Params: { name: string }; Body: RemoteMachine }>("/v1/machines/:name", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    try {
      const machine = normalizeMachine(options.provider, await options.store.patchMachine(auth.userID, name, request.body));
      return { machine };
    } catch (err) {
      return reply.code(404).send({ id: "not_found", message: (err as Error).message });
    }
  });

  app.delete<{ Params: { name: string } }>("/v1/machines/:name", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    let existing = await options.store.getMachine(auth.userID, name);
    if (!existing) {
      await syncProviderMachines(options, auth);
      existing = await options.store.getMachine(auth.userID, name);
    }
    if (existing) {
      await options.provider.releaseMachine(existing);
      await options.store.deleteMachine(auth.userID, name);
    }
    return reply.code(204).send();
  });

  if (options.appDir) {
    registerAppRoutes(app, options.appDir);
  }

  return app;
}

async function leaseMachine(options: BackendOptions, auth: AuthContext, body: EnsureMachineRequest) {
  body.provider_name = providerMachineName(auth.userID, body.name);

  const existing = await options.store.getMachine(auth.userID, body.name);
  if (existing) {
    const refreshed = await options.provider.getMachine(existing);
    const machine = normalizeMachine(options.provider, {
      ...existing,
      ...refreshed.machine,
      user_id: auth.userID,
      source_path: body.source_path || existing.source_path,
      repo_url: body.repo_url || existing.repo_url,
      branch: body.branch || existing.branch,
      ssh_user: refreshed.machine.ssh_user || body.ssh_user || existing.ssh_user || "root",
    });
    await options.store.putMachine(machine);
    return { machine, status: refreshed.status || "leased" };
  }

  const provisioned = await options.provider.ensureMachine(body);
  const machine = normalizeMachine(options.provider, {
    ...provisioned.machine,
    name: body.name,
    user_id: auth.userID,
    provider_name: body.provider_name,
    source_path: body.source_path,
    repo_url: body.repo_url,
    branch: body.branch,
    ssh_user: provisioned.machine.ssh_user || body.ssh_user || "root",
  });
  await options.store.putMachine(machine);
  return { machine, status: provisioned.status || "leased" };
}

async function listAccountMachines(options: BackendOptions, auth: AuthContext): Promise<RemoteMachine[]> {
  await syncProviderMachines(options, auth);
  return (await options.store.listMachinesForUser(auth.userID)).map((machine) => normalizeMachine(options.provider, machine));
}

async function syncProviderMachines(options: BackendOptions, auth: AuthContext): Promise<void> {
  const discovered = await options.provider.listMachines({
    provider_name_suffix: providerUserHash(auth.userID),
    ssh_user: "root",
  });
  for (const item of discovered) {
    const existing = await options.store.getMachine(auth.userID, item.machine.name);
    const machine = normalizeMachine(options.provider, {
      ...existing,
      ...item.machine,
      name: item.machine.name,
      user_id: auth.userID,
    });
    await options.store.putMachine(machine);
  }
}

function normalizeEnsureRequest(body: EnsureMachineRequest | undefined, providerName: string): EnsureMachineRequest {
  const input = body || { name: "" };
  return {
    ...input,
    name: (input.name || "").trim().toLowerCase(),
    provider: input.provider?.trim().toLowerCase() || providerName,
    provider_name: input.provider_name?.trim(),
    ssh_user: input.ssh_user?.trim() || "root",
    source_path: input.source_path?.trim(),
    repo_url: input.repo_url?.trim(),
    branch: input.branch?.trim(),
  };
}

function normalizeMachine(provider: MachineProvider, machine: RemoteMachine): RemoteMachine {
  const now = new Date().toISOString();
  return {
    ...machine,
    name: machine.name.trim().toLowerCase(),
    provider: machine.provider || provider.name,
    provider_label: machine.provider_label || provider.label || provider.name,
    project_path: machine.project_path || defaultProjectPath,
    ssh_user: machine.ssh_user || "root",
    created_at: machine.created_at || now,
    updated_at: now,
  };
}

function providerInfo(provider: MachineProvider): MachineProviderInfo {
  return provider.info || {
    name: provider.name,
    label: provider.label || provider.name,
    capabilities: ["create", "destroy", "list", "connect"],
  };
}

function validateName(name: string): string | undefined {
  if (!namePattern.test(name)) {
    return "invalid machine name; expected lowercase letters, numbers, and hyphens";
  }
  return undefined;
}

async function requireAuth(options: BackendOptions, request: { headers: Record<string, string | string[] | undefined> }, reply: { code: (statusCode: number) => { send: (payload: unknown) => unknown } }): Promise<AuthContext | undefined> {
  const session = await options.auth.api.getSession({ headers: toHeaders(request.headers) });
  if (!session) {
    reply.code(401).send({ id: "unauthorized", message: "missing or invalid bearer token" });
    return undefined;
  }
  return {
    userID: session.user.id,
    email: session.user.email,
  };
}

function toWebRequest(request: { method: string; url: string; headers: Record<string, string | string[] | undefined>; body?: unknown; ip?: string }): Request {
  const headers = toHeaders(request.headers);
  if (request.ip && !headers.has("x-forwarded-for")) {
    headers.set("x-forwarded-for", request.ip);
  }
  const url = `http://${headers.get("host") || "127.0.0.1"}${request.url}`;
  const init: RequestInit = {
    method: request.method,
    headers,
  };
  if (request.method !== "GET" && request.method !== "HEAD") {
    if (request.body === undefined) {
      init.body = null;
    } else if (typeof request.body === "string") {
      init.body = request.body;
    } else if (request.body instanceof Uint8Array) {
      init.body = Buffer.from(request.body).toString("utf8");
    } else {
      init.body = JSON.stringify(request.body);
      if (!headers.has("content-type")) headers.set("content-type", "application/json");
    }
  }
  return new Request(url, init);
}

function toHeaders(headers: Record<string, string | string[] | undefined>): Headers {
  const result = new Headers();
  for (const [key, value] of Object.entries(headers)) {
    if (value === undefined) continue;
    if (Array.isArray(value)) {
      for (const item of value) result.append(key, item);
      continue;
    }
    result.set(key, value);
  }
  return result;
}

async function sendAuthResponse(reply: { code: (statusCode: number) => { send: (payload: Buffer) => unknown }; header: (name: string, value: string | string[]) => void }, response: Response): Promise<void> {
  for (const [name, value] of response.headers.entries()) {
    if (name.toLowerCase() === "set-cookie") continue;
    reply.header(name, value);
  }
  const getSetCookie = (response.headers as Headers & { getSetCookie?: () => string[] }).getSetCookie;
  const fallbackCookie = response.headers.get("set-cookie");
  const cookies = getSetCookie ? getSetCookie.call(response.headers) : fallbackCookie ? [fallbackCookie] : [];
  if (cookies.length > 0) reply.header("set-cookie", cookies);
  reply.code(response.status).send(Buffer.from(await response.arrayBuffer()));
}

function providerMachineName(userID: string, machineName: string): string {
  const hash = providerUserHash(userID);
  const base = machineName.trim().toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-|-$/g, "") || "machine";
  return `${base.slice(0, 52)}-${hash}`;
}

function providerUserHash(userID: string): string {
  return createHash("sha256").update(userID).digest("hex").slice(0, 10);
}

function sshTarget(machine: RemoteMachine): string {
  if (!machine.public_ipv4) return "";
  return `${machine.ssh_user || "root"}@${machine.public_ipv4}`;
}

function registerCors(app: FastifyInstance, origins: string[]): void {
  const allowed = new Set(origins.map((origin) => origin.trim()).filter(Boolean));
  if (allowed.size === 0) return;
  void app.register(cors, {
    credentials: true,
    allowedHeaders: ["authorization", "content-type"],
    methods: ["GET", "POST", "PATCH", "DELETE", "OPTIONS"],
    origin(origin, callback) {
      callback(null, !origin || allowed.has(origin));
    },
  });
}

function registerAppRoutes(app: FastifyInstance, appDir: string): void {
  app.get("/", async (_request, reply) => {
    await sendAppFile(reply, appDir, "index.html");
  });
  app.get("/*", async (request, reply) => {
    const path = request.url.split("?")[0] || "/";
    if (path.startsWith("/v1/") || path === "/healthz") {
      return reply.code(404).send({ id: "not_found", message: "not found" });
    }
    const decoded = decodeURIComponent(path);
    const filePath = appFilePath(decoded);
    await sendAppFile(reply, appDir, filePath);
  });
}

function appFilePath(path: string): string {
  const clean = normalize(path.replace(/^\/+/, ""));
  if (!clean || clean.startsWith("..") || clean.includes("/../")) return "index.html";
  return extname(clean) ? clean : "index.html";
}

async function sendAppFile(reply: { code: (statusCode: number) => { send: (payload: unknown) => unknown }; type: (contentType: string) => unknown; send: (payload: Buffer | string) => unknown }, appDir: string, filePath: string): Promise<unknown> {
  const absolutePath = join(appDir, filePath);
  try {
    const fileStat = await stat(absolutePath);
    if (!fileStat.isFile()) throw new Error("not a file");
    const data = await readFile(absolutePath);
    reply.type(contentType(filePath));
    return reply.send(data);
  } catch {
    if (filePath !== "index.html") return sendAppFile(reply, appDir, "index.html");
    return reply.code(404).send("app is not built");
  }
}

function contentType(path: string): string {
  switch (extname(path)) {
    case ".html":
      return "text/html; charset=utf-8";
    case ".js":
      return "text/javascript; charset=utf-8";
    case ".css":
      return "text/css; charset=utf-8";
    case ".svg":
      return "image/svg+xml";
    case ".png":
      return "image/png";
    default:
      return "application/octet-stream";
  }
}
