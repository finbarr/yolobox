import { createHash } from "node:crypto";
import Fastify, { FastifyInstance } from "fastify";
import { BackendAuth } from "./auth.js";
import { StateStore } from "./state.js";
import { EnsureMachineRequest, MachineProvider, RemoteMachine, defaultProjectPath } from "./types.js";

export type BackendOptions = {
  auth: BackendAuth;
  provider: MachineProvider;
  store: StateStore;
};

type AuthContext = {
  userID: string;
  email: string;
};

const namePattern = /^[a-z0-9][a-z0-9-]{0,62}$/;

export function createBackend(options: BackendOptions): FastifyInstance {
  const app = Fastify({ logger: false });

  app.get("/healthz", async () => "ok\n");

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
    const body = normalizeEnsureRequest(request.body);
    const error = validateName(body.name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    body.provider_name = providerMachineName(auth.userID, body.name);

    const existing = await options.store.getMachine(auth.userID, body.name);
    if (existing) {
      const refreshed = await options.provider.getMachine(existing);
      const machine = normalizeMachine({
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
    const machine = normalizeMachine({
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
  });

  app.get("/v1/machines", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    return {
      machines: (await options.store.listMachinesForUser(auth.userID)).map(normalizeMachine),
    };
  });

  app.get<{ Params: { name: string } }>("/v1/machines/:name", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    const existing = await options.store.getMachine(auth.userID, name);
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine is not leased" });
    const refreshed = await options.provider.getMachine(existing);
    const machine = normalizeMachine({ ...existing, ...refreshed.machine, name, user_id: auth.userID });
    await options.store.putMachine(machine);
    return { machine, status: refreshed.status || "leased" };
  });

  app.patch<{ Params: { name: string }; Body: RemoteMachine }>("/v1/machines/:name", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    try {
      const machine = normalizeMachine(await options.store.patchMachine(auth.userID, name, request.body));
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
    const existing = await options.store.getMachine(auth.userID, name);
    if (existing) {
      await options.provider.releaseMachine(existing);
      await options.store.deleteMachine(auth.userID, name);
    }
    return reply.code(204).send();
  });

  return app;
}

function normalizeEnsureRequest(body: EnsureMachineRequest | undefined): EnsureMachineRequest {
  const input = body || { name: "" };
  return {
    ...input,
    name: (input.name || "").trim().toLowerCase(),
    provider_name: input.provider_name?.trim(),
    ssh_user: input.ssh_user?.trim() || "root",
    source_path: input.source_path?.trim(),
    repo_url: input.repo_url?.trim(),
    branch: input.branch?.trim(),
  };
}

function normalizeMachine(machine: RemoteMachine): RemoteMachine {
  const now = new Date().toISOString();
  return {
    ...machine,
    name: machine.name.trim().toLowerCase(),
    project_path: machine.project_path || defaultProjectPath,
    ssh_user: machine.ssh_user || "root",
    created_at: machine.created_at || now,
    updated_at: now,
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

function toWebRequest(request: { method: string; url: string; headers: Record<string, string | string[] | undefined>; body?: unknown }): Request {
  const headers = toHeaders(request.headers);
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
  const hash = createHash("sha256").update(userID).digest("hex").slice(0, 10);
  const base = machineName.trim().toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-|-$/g, "") || "machine";
  return `${base.slice(0, 52)}-${hash}`;
}
