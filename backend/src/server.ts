import { createHash, randomBytes, randomUUID, timingSafeEqual } from "node:crypto";
import { readFile, stat } from "node:fs/promises";
import { extname, join, normalize } from "node:path";
import cors from "@fastify/cors";
import websocket from "@fastify/websocket";
import Fastify, { FastifyInstance } from "fastify";
import { WebSocket } from "ws";
import { BackendAuth } from "./auth.js";
import { SSHCertificateAuthority } from "./ssh_ca.js";
import { StateStore } from "./state.js";
import { CreateMachineRequest, MachineProvider, MachineProviderInfo, RemoteMachine, defaultProjectPath } from "./types.js";

export type BackendOptions = {
  auth: BackendAuth;
  provider: MachineProvider;
  store: StateStore;
  sshCA: SSHCertificateAuthority;
  appDir?: string;
  apiPublicURL?: string;
  appPublicURL?: string;
  corsOrigins?: string[];
  previewBaseDomain?: string;
  previewTargetPort?: number;
  machineReadyTimeoutMs?: number;
};

type AuthContext = {
  userID: string;
  email: string;
};

type PreviewOptions = {
  baseDomain: string;
  targetPort: number;
};

type AgentConnection = {
  machineKey: string;
  machine: RemoteMachine;
  socket: WebSocket;
  calls: Map<string, AgentCall>;
};

type AgentCall = {
  resolve: (result: unknown) => void;
  reject: (error: Error) => void;
  timer: NodeJS.Timeout;
};

type AgentMessage = {
  type?: string;
  rpc_id?: string;
  action?: string;
  payload?: unknown;
  ok?: boolean;
  result?: unknown;
  code?: string;
  message?: string;
};

type RemoteSetupRequest = {
  commands?: string[];
};

type RemoteSyncCompleteRequest = {
  source_path?: string;
  project_path?: string;
  repo_url?: string;
  branch?: string;
};

type RemoteSessionPrepareRequest = {
  command?: string[];
  attach?: boolean;
};

type RemoteCommandRequest = {
  command?: string[];
};

type SSHCertificateRequest = {
  public_key?: string;
  ttl_seconds?: number;
};

const namePattern = /^[a-z0-9][a-z0-9-]{0,62}$/;
const agentTokenBytes = 48;
const agentRPCDefaultTimeout = 60_000;
const agentRPCSetupTimeout = 30 * 60_000;

export function createBackend(options: BackendOptions): FastifyInstance {
  const app = Fastify({ logger: false });
  void app.register(websocket, { options: { maxPayload: 8 * 1024 * 1024 } });
  registerCors(app, options.corsOrigins || []);
  const agents = new Map<string, AgentConnection>();
  app.after(() => {
    app.get("/v1/agent/connect", { websocket: true }, async (socket, request) => {
      await handleAgentConnection(options, agents, socket, request);
    });
  });

  app.get("/healthz", async () => "ok\n");

  app.get("/v1/providers", async () => ({
    providers: [providerInfo(options.provider)],
  }));

  app.get<{ Querystring: { domain?: string } }>("/v1/preview/tls-check", async (request, reply) => {
    const hostname = normalizeHostname(request.query.domain || "");
    if (!hostname || !(await findMachineByPreviewHostname(options, hostname))) {
      return reply.code(404).send({ id: "not_found", message: "preview hostname is not registered" });
    }
    return "ok\n";
  });

  app.all<{ Params: { hostname: string; "*": string } }>("/v1/preview/proxy/:hostname/*", async (request, reply) => {
    return proxyPreviewRequest(options, request, reply);
  });

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

  app.post("/v1/agent/heartbeat", async (request, reply) => {
    const machine = await requireAgentMachine(options, request, reply);
    if (!machine) return;
    const updated = {
      ...machine,
      agent_last_seen_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    await options.store.putMachine(updated);
    return { machine: publicMachine(normalizeMachine(options, updated)) };
  });

  app.post<{ Body: CreateMachineRequest }>("/v1/machines", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const body = normalizeCreateRequest(request.body, options.provider.name);
    if (body.provider && body.provider !== options.provider.name) {
      return reply.code(400).send({ id: "bad_request", message: `provider ${body.provider} is not configured` });
    }
    const error = validateName(body.name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    const tierError = validateMachineTier(body.tier);
    if (tierError) return reply.code(400).send({ id: "bad_request", message: tierError });
    return createMachine(options, agents, auth, body, reply);
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
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine does not exist" });
    const refreshed = await options.provider.getMachine(existing);
    const machine = normalizeMachine(options, { ...existing, ...refreshed.machine, name, user_id: auth.userID });
    await options.store.putMachine(machine);
    return { machine: publicMachine(machine), status: refreshed.status || "leased" };
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
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine does not exist" });
    const refreshed = await options.provider.getMachine(existing);
    const machine = normalizeMachine(options, { ...existing, ...refreshed.machine, name, user_id: auth.userID });
    await options.store.putMachine(machine);
    return {
      machine: publicMachine(machine),
      status: refreshed.status || "leased",
      connect: {
        transport: "direct_ssh_certificate",
        cli: `yolobox remote connect ${machine.name}`,
        cli_run: `yolobox remote run ${machine.name}`,
      },
    };
  });

  app.post<{ Params: { name: string }; Body: SSHCertificateRequest }>("/v1/machines/:name/ssh-cert", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    const machine = await options.store.getMachine(auth.userID, name);
    if (!machine) return reply.code(404).send({ id: "not_found", message: "machine does not exist" });
    if (!machine.bootstrap_complete) {
      return reply.code(409).send({ id: "not_bootstrapped", message: "remote machine is not bootstrapped" });
    }
    const normalized = normalizeMachine(options, machine);
    const principal = normalized.ssh_principal || sshPrincipalForMachine(normalized);
    if (!normalized.public_ipv4) {
      return reply.code(409).send({ id: "not_ready", message: "remote machine does not have a public IPv4 yet" });
    }
    const publicKey = request.body?.public_key?.trim() || "";
    if (!/^ssh-[A-Za-z0-9-]+\s+\S+/.test(publicKey)) {
      return reply.code(400).send({ id: "bad_request", message: "valid SSH public key is required" });
    }
    const signed = await options.sshCA.signUserPublicKey({
      publicKey,
      principal,
      identity: `yolobox-${auth.userID}-${normalized.name}`,
      ttlSeconds: request.body?.ttl_seconds,
    });
    return {
      ...signed,
      host: normalized.public_ipv4,
      port: 22,
      ssh_user: normalized.ssh_user || "root",
    };
  });

  app.post<{ Params: { name: string }; Body: RemoteSetupRequest }>("/v1/machines/:name/setup", async (request, reply) => {
    const context = await requireMachineAgentContext(options, agents, request, reply);
    if (!context) return;
    const commands = normalizeStringArray(request.body?.commands);
    if (commands.length === 0) {
      return { result: { skipped: true } };
    }
    const result = await callAgentRPC(context.agent, "run_setup", {
      ...machineRuntimePayload(context.machine),
      commands,
    }, agentRPCSetupTimeout);
    return { result };
  });

  app.post<{ Params: { name: string }; Body: RemoteSyncCompleteRequest }>("/v1/machines/:name/sync-complete", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    const existing = await options.store.getMachine(auth.userID, name);
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine does not exist" });
    const body = normalizeWorkspaceRequest(request.body, existing);
    const now = new Date().toISOString();
    const machine = normalizeMachine(options, {
      ...existing,
      ...body,
      last_synced_at: now,
      updated_at: now,
    });
    await options.store.putMachine(machine);
    const agent = agents.get(agentMachineKey(machine));
    if (agent) agent.machine = machine;
    return { machine: publicMachine(machine) };
  });

  app.post<{ Params: { name: string }; Body: RemoteSessionPrepareRequest }>("/v1/machines/:name/sessions/yolobox/prepare", async (request, reply) => {
    const context = await requireMachineAgentContext(options, agents, request, reply);
    if (!context) return;
    const command = normalizeStringArray(request.body?.command);
    const attach = request.body?.attach === true;
    try {
      const result = await callAgentRPC(context.agent, "prepare_session", {
        ...machineRuntimePayload(context.machine),
        command,
        attach,
      }, agentRPCDefaultTimeout);
      if (agentResultRecordCommand(result)) {
        const machine = normalizeMachine(options, {
          ...context.machine,
          last_command: command,
          updated_at: new Date().toISOString(),
        });
        await options.store.putMachine(machine);
        context.agent.machine = machine;
        return { machine: publicMachine(machine), result };
      }
      return { machine: publicMachine(context.machine), result };
    } catch (err) {
      if (err instanceof AgentRPCError && err.code === "session_exists") {
        return reply.code(409).send({ id: "session_exists", message: err.message });
      }
      throw err;
    }
  });

  app.post<{ Params: { name: string }; Body: RemoteCommandRequest }>("/v1/machines/:name/commands/ssh", async (request, reply) => {
    const context = await requireMachineAgentContext(options, agents, request, reply);
    if (!context) return;
    const command = normalizeStringArray(request.body?.command);
    const result = await callAgentRPC(context.agent, "direct_command", {
      ...machineRuntimePayload(context.machine),
      command,
    }, agentRPCDefaultTimeout);
    return { machine: publicMachine(context.machine), result };
  });

  app.post<{ Params: { name: string }; Body: RemoteCommandRequest }>("/v1/machines/:name/commands/record", async (request, reply) => {
    const auth = await requireAuth(options, request, reply);
    if (!auth) return;
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    const existing = await options.store.getMachine(auth.userID, name);
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine does not exist" });
    const machine = normalizeMachine(options, {
      ...existing,
      last_command: normalizeStringArray(request.body?.command),
      updated_at: new Date().toISOString(),
    });
    await options.store.putMachine(machine);
    const agent = agents.get(agentMachineKey(machine));
    if (agent) agent.machine = machine;
    return { machine: publicMachine(machine) };
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

async function createMachine(
  options: BackendOptions,
  agents: Map<string, AgentConnection>,
  auth: AuthContext,
  body: CreateMachineRequest,
  reply: { code: (statusCode: number) => { send: (payload: unknown) => unknown } },
) {
  body.provider_name = providerMachineName(auth.userID, body.name);
  const agentBackendURL = (options.apiPublicURL || "").trim().replace(/\/+$/, "");
  if (!agentBackendURL) {
    throw new Error("backend public API URL is required to provision remote machine agent credentials");
  }
  const agentToken = generateAgentToken();
  const sshPrincipal = sshPrincipalForMachine({ name: body.name, user_id: auth.userID, provider_name: body.provider_name });
  body.agent_token = agentToken;
  body.agent_backend_url = agentBackendURL;
  body.ssh_user_ca_public_key = await options.sshCA.publicKey();
  body.ssh_authorized_principal = sshPrincipal;

  await syncProviderMachines(options, auth);
  const existing = await options.store.getMachine(auth.userID, body.name);
  if (existing) {
    return reply.code(409).send(machineNameConflict(body.name));
  }

  let provisioned: { machine: RemoteMachine; status?: string };
  try {
    provisioned = await options.provider.createMachine(body);
  } catch (err) {
    if ((err as Error).message.includes("already exists")) {
      return reply.code(409).send(machineNameConflict(body.name));
    }
    throw err;
  }
  const machine = normalizeMachine(options, {
    ...provisioned.machine,
    name: body.name,
    user_id: auth.userID,
    provider_name: body.provider_name,
    source_path: body.source_path,
    repo_url: body.repo_url,
    branch: body.branch,
    ssh_user: provisioned.machine.ssh_user || body.ssh_user || "root",
    agent_token_hash: hashAgentToken(agentToken),
    ssh_principal: sshPrincipal,
  });
  await options.store.putMachine(machine);
  let ready: RemoteMachine;
  try {
    ready = await waitForCreatedMachineAgent(options, agents, machine);
  } catch (err) {
    if (err instanceof AgentRPCError) {
      return reply.code(504).send({ id: err.code, message: err.message, machine: publicMachine(machine) });
    }
    throw err;
  }
  return reply.code(201).send({ machine: publicMachine(ready), status: provisioned.status || "created" });
}

async function listAccountMachines(options: BackendOptions, auth: AuthContext): Promise<RemoteMachine[]> {
  await syncProviderMachines(options, auth);
  return (await options.store.listMachinesForUser(auth.userID)).map((machine) => publicMachine(normalizeMachine(options, machine)));
}

async function syncProviderMachines(options: BackendOptions, auth: AuthContext): Promise<void> {
  const discovered = await options.provider.listMachines({
    provider_name_suffix: providerUserHash(auth.userID),
    ssh_user: "root",
  });
  for (const item of discovered) {
    const existing = await options.store.getMachine(auth.userID, item.machine.name);
    const machine = normalizeMachine(options, {
      ...existing,
      ...item.machine,
      name: item.machine.name,
      user_id: auth.userID,
    });
    await options.store.putMachine(machine);
  }
}

function normalizeCreateRequest(body: CreateMachineRequest | undefined, providerName: string): CreateMachineRequest {
  const input = body || { name: "" };
  return {
    ...input,
    name: (input.name || "").trim().toLowerCase(),
    provider: input.provider?.trim().toLowerCase() || providerName,
    provider_name: input.provider_name?.trim(),
    tier: input.tier?.trim().toLowerCase(),
    ssh_user: input.ssh_user?.trim() || "root",
    source_path: input.source_path?.trim(),
    repo_url: input.repo_url?.trim(),
    branch: input.branch?.trim(),
  };
}

function normalizeWorkspaceRequest(body: RemoteSyncCompleteRequest | undefined, existing: RemoteMachine): Partial<RemoteMachine> {
  const input = body || {};
  return {
    source_path: input.source_path?.trim() || existing.source_path,
    project_path: normalizeAbsoluteRemotePath(input.project_path || "") || existing.project_path || defaultProjectPath,
    repo_url: input.repo_url?.trim() || existing.repo_url,
    branch: input.branch?.trim() || existing.branch,
  };
}

function normalizeMachine(options: BackendOptions, machine: RemoteMachine): RemoteMachine {
  const now = new Date().toISOString();
  const name = machine.name.trim().toLowerCase();
  const preview = previewOptions(options);
  const previewHostname = preview && machine.user_id
    ? normalizeHostname(machine.preview_hostname || generatedPreviewHostname(machine.user_id, name, preview.baseDomain))
    : normalizeHostname(machine.preview_hostname || "");
  return {
    ...machine,
    name,
    provider: machine.provider || options.provider.name,
    provider_label: machine.provider_label || options.provider.label || options.provider.name,
    project_path: machine.project_path || defaultProjectPath,
    ssh_user: machine.ssh_user || "root",
    ssh_principal: machine.ssh_principal || sshPrincipalForMachine(machine),
    ...(previewHostname ? { preview_hostname: previewHostname, preview_url: `https://${previewHostname}` } : {}),
    created_at: machine.created_at || now,
    updated_at: now,
  };
}

function publicMachine(machine: RemoteMachine): RemoteMachine {
  const safe = { ...machine } as RemoteMachine & Record<string, unknown>;
  delete safe.agent_token_hash;
  for (const key of Object.keys(safe)) {
    if (key.startsWith("ssh_") && key.endsWith("_key")) delete safe[key];
  }
  return safe as RemoteMachine;
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

function validateMachineTier(tier: string | undefined): string | undefined {
  if (!tier) return undefined;
  if (tier === "small" || tier === "medium" || tier === "large") return undefined;
  return "invalid machine tier; expected small, medium, or large";
}

function machineNameConflict(name: string) {
  return {
    id: "conflict",
    message: `remote machine ${name} already exists; use yolobox remote run ${name} <cmd>, yolobox remote connect ${name}, or yolobox remote status ${name}`,
  };
}

class AgentRPCError extends Error {
  code: string;

  constructor(message: string, code = "agent_rpc_failed") {
    super(message);
    this.code = code;
  }
}

function connectedAgent(agents: Map<string, AgentConnection>, machine: RemoteMachine): AgentConnection | undefined {
  const agent = agents.get(agentMachineKey(machine));
  return agent?.socket.readyState === WebSocket.OPEN ? agent : undefined;
}

async function waitForCreatedMachineAgent(
  options: BackendOptions,
  agents: Map<string, AgentConnection>,
  machine: RemoteMachine,
): Promise<RemoteMachine> {
  const timeout = options.machineReadyTimeoutMs ?? 5 * 60_000;
  if (timeout <= 0) return machine;
  const deadline = Date.now() + timeout;
  while (Date.now() <= deadline) {
    const agent = connectedAgent(agents, machine);
    if (agent) return normalizeMachine(options, agent.machine);
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new AgentRPCError("remote machine was created, but its agent did not connect before the readiness timeout", "agent_disconnected");
}

async function requireMachineAgentContext(
  options: BackendOptions,
  agents: Map<string, AgentConnection>,
  request: { headers: Record<string, string | string[] | undefined>; params: { name: string } },
  reply: { code: (statusCode: number) => { send: (payload: unknown) => unknown } },
): Promise<{ auth: AuthContext; machine: RemoteMachine; agent: AgentConnection } | undefined> {
  const auth = await requireAuth(options, request, reply);
  if (!auth) return undefined;
  const name = request.params.name;
  const error = validateName(name);
  if (error) {
    reply.code(400).send({ id: "bad_request", message: error });
    return undefined;
  }
  const machine = await options.store.getMachine(auth.userID, name);
  if (!machine?.user_id) {
    reply.code(404).send({ id: "not_found", message: "machine does not exist" });
    return undefined;
  }
  if (!machine.bootstrap_complete) {
    reply.code(409).send({ id: "not_bootstrapped", message: "remote machine is not bootstrapped" });
    return undefined;
  }
  const agent = connectedAgent(agents, machine);
  if (!agent) {
    reply.code(409).send({ id: "agent_disconnected", message: "remote machine agent is not connected" });
    return undefined;
  }
  return { auth, machine: normalizeMachine(options, machine), agent };
}

function normalizeAbsoluteRemotePath(value: string): string {
  value = value.trim();
  if (!value || !value.startsWith("/") || value === "/") return "";
  return normalize(value);
}

function normalizeStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => String(item).trim()).filter(Boolean);
}

function machineRuntimePayload(machine: RemoteMachine): Record<string, unknown> {
  const projectPath = normalizeAbsoluteRemotePath(machine.project_path || defaultProjectPath) || defaultProjectPath;
  return {
    name: machine.name,
    project_path: projectPath,
    preview_url: machine.preview_url || "",
    preview_hostname: machine.preview_hostname || "",
  };
}

function agentResultRecordCommand(result: unknown): boolean {
  return !!result && typeof result === "object" && (result as { record_command?: unknown }).record_command === true;
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

async function requireAgentMachine(options: BackendOptions, request: { headers: Record<string, string | string[] | undefined> }, reply: { code: (statusCode: number) => { send: (payload: unknown) => unknown } }): Promise<RemoteMachine | undefined> {
  const token = bearerToken(request.headers);
  if (!token) {
    reply.code(401).send({ id: "unauthorized", message: "missing machine agent token" });
    return undefined;
  }
  const tokenHash = hashAgentToken(token);
  const machine = await findMachineByAgentTokenHash(options, tokenHash);
  if (!machine) {
    reply.code(401).send({ id: "unauthorized", message: "invalid machine agent token" });
    return undefined;
  }
  return machine;
}

async function findMachineByAgentTokenHash(options: BackendOptions, tokenHash: string): Promise<RemoteMachine | undefined> {
  for (const machine of await options.store.listMachines()) {
    if (!machine.agent_token_hash) continue;
    if (constantTimeEqual(machine.agent_token_hash, tokenHash)) return machine;
  }
  return undefined;
}

function bearerToken(headers: Record<string, string | string[] | undefined>): string {
  const raw = headers.authorization;
  const value = Array.isArray(raw) ? raw[0] : raw;
  const match = /^Bearer\s+(.+)$/i.exec(value || "");
  return match ? match[1].trim() : "";
}

function generateAgentToken(): string {
  return randomBytes(agentTokenBytes).toString("base64url");
}

export function hashAgentToken(token: string): string {
  return createHash("sha256").update("yolobox-agent-token:v1\0").update(token).digest("hex");
}

function constantTimeEqual(left: string, right: string): boolean {
  const leftBuffer = Buffer.from(left, "hex");
  const rightBuffer = Buffer.from(right, "hex");
  return leftBuffer.length === rightBuffer.length && timingSafeEqual(leftBuffer, rightBuffer);
}

async function handleAgentConnection(
  options: BackendOptions,
  agents: Map<string, AgentConnection>,
  socket: WebSocket,
  request: { headers: Record<string, string | string[] | undefined> },
): Promise<void> {
  let agent: AgentConnection | undefined;
  let key = "";
  const pendingMessages: WebSocket.RawData[] = [];
  socket.on("message", (data) => {
    if (!agent) {
      pendingMessages.push(data);
      return;
    }
    void handleAgentMessage(options, agent, data);
  });
  socket.on("close", () => {
    if (!agent) return;
    if (agents.get(key) === agent) agents.delete(key);
    for (const call of agent.calls.values()) {
      clearTimeout(call.timer);
      call.reject(new AgentRPCError("remote machine agent disconnected", "agent_disconnected"));
    }
    agent.calls.clear();
  });

  const machine = await agentMachineFromHeaders(options, request.headers);
  if (!machine?.user_id) {
    sendAgentError(socket, "missing or invalid machine agent token");
    socket.close(1008, "unauthorized");
    return;
  }
  if (socket.readyState !== WebSocket.OPEN) return;

  key = agentMachineKey(machine);
  const existing = agents.get(key);
  if (existing) {
    closeAgent(existing, "machine agent reconnected");
  }
  agent = {
    machineKey: key,
    machine,
    socket,
    calls: new Map(),
  };
  agents.set(key, agent);
  agent.machine = await markAgentSeen(options, machine);
  for (const message of pendingMessages.splice(0)) {
    void handleAgentMessage(options, agent, message);
  }
}

async function handleAgentMessage(options: BackendOptions, agent: AgentConnection, raw: WebSocket.RawData): Promise<void> {
  const message = parseAgentMessage(raw);
  if (!message.type) return;
  if (message.type === "ping") {
    agent.machine = await markAgentSeen(options, agent.machine);
    sendAgentJSON(agent.socket, { type: "pong" });
    return;
  }
  if (message.type === "rpc_result") {
    const rpcID = message.rpc_id || "";
    const call = agent.calls.get(rpcID);
    if (!call) return;
    clearTimeout(call.timer);
    agent.calls.delete(rpcID);
    if (message.ok === true) {
      call.resolve(message.result);
      return;
    }
    call.reject(new AgentRPCError(message.message || "remote machine agent RPC failed", message.code || "agent_rpc_failed"));
    return;
  }
}

async function agentMachineFromHeaders(options: BackendOptions, headers: Record<string, string | string[] | undefined>): Promise<RemoteMachine | undefined> {
  const token = bearerToken(headers);
  if (!token) return undefined;
  return findMachineByAgentTokenHash(options, hashAgentToken(token));
}

async function markAgentSeen(options: BackendOptions, machine: RemoteMachine): Promise<RemoteMachine> {
  const current = machine.user_id ? await options.store.getMachine(machine.user_id, machine.name) : undefined;
  const updated = {
    ...(current || machine),
    agent_last_seen_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
  await options.store.putMachine(updated);
  return updated;
}

function closeAgent(agent: AgentConnection, reason: string): void {
  for (const call of agent.calls.values()) {
    clearTimeout(call.timer);
    call.reject(new AgentRPCError(reason, "agent_disconnected"));
  }
  agent.calls.clear();
  agent.socket.close(1000, reason);
}

function callAgentRPC(agent: AgentConnection, action: string, payload: unknown, timeout: number): Promise<unknown> {
  if (agent.socket.readyState !== WebSocket.OPEN) {
    return Promise.reject(new AgentRPCError("remote machine agent is not connected", "agent_disconnected"));
  }
  const rpcID = randomUUID();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      agent.calls.delete(rpcID);
      reject(new AgentRPCError(`timed out waiting for remote machine agent action ${action}`, "agent_timeout"));
    }, timeout);
    agent.calls.set(rpcID, { resolve, reject, timer });
    sendAgentJSON(agent.socket, { type: "rpc", rpc_id: rpcID, action, payload });
  });
}

function agentMachineKey(machine: RemoteMachine): string {
  return `${machine.user_id || ""}:${machine.name}`;
}

function parseAgentMessage(raw: WebSocket.RawData): AgentMessage {
  try {
    return JSON.parse(normalizeWebSocketBytes(raw).toString("utf8")) as AgentMessage;
  } catch {
    return {};
  }
}

function normalizeWebSocketBytes(raw: WebSocket.RawData): Buffer {
  if (Buffer.isBuffer(raw)) return raw;
  if (Array.isArray(raw)) return Buffer.concat(raw);
  return Buffer.from(raw);
}

function sendAgentJSON(socket: WebSocket, payload: AgentMessage): void {
  if (socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(payload));
  }
}

function sendAgentError(socket: WebSocket, message: string): void {
  sendAgentJSON(socket, { type: "error", message });
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

function sshPrincipalForMachine(machine: Pick<RemoteMachine, "name" | "user_id" | "provider_name">): string {
  const source = machine.provider_name || (machine.user_id ? providerMachineName(machine.user_id, machine.name) : machine.name);
  const principal = source.trim().toLowerCase().replace(/[^a-z0-9._:-]+/g, "-").replace(/^-+|-+$/g, "");
  return `yolobox:${principal || "machine"}`;
}

function providerUserHash(userID: string): string {
  return createHash("sha256").update(userID).digest("hex").slice(0, 10);
}

const previewLeftWords = [
  "amber",
  "atlas",
  "banana",
  "cedar",
  "cobalt",
  "delta",
  "ember",
  "frost",
  "golden",
  "harbor",
  "indigo",
  "juniper",
  "lunar",
  "maple",
  "nova",
  "opal",
];

const previewRightWords = [
  "arc",
  "beacon",
  "bridge",
  "cloud",
  "dune",
  "field",
  "forge",
  "grove",
  "haven",
  "lane",
  "orbit",
  "path",
  "ridge",
  "signal",
  "stone",
  "vault",
];

function previewOptions(options: BackendOptions): PreviewOptions | undefined {
  const baseDomain = normalizeHostname(options.previewBaseDomain || "");
  if (!baseDomain) return undefined;
  const targetPort = Number(options.previewTargetPort || 80);
  return {
    baseDomain,
    targetPort: Number.isInteger(targetPort) && targetPort > 0 && targetPort <= 65535 ? targetPort : 80,
  };
}

function generatedPreviewHostname(userID: string, machineName: string, baseDomain: string): string {
  const hash = createHash("sha256").update(`${userID}:${machineName}`).digest("hex");
  const left = previewLeftWords[Number.parseInt(hash.slice(0, 8), 16) % previewLeftWords.length];
  const right = previewRightWords[Number.parseInt(hash.slice(8, 16), 16) % previewRightWords.length];
  return `${left}-${right}-${hash.slice(16, 22)}.${baseDomain}`;
}

function normalizeHostname(value: string): string {
  return value.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/\/.*$/, "").replace(/:\d+$/, "").replace(/^\.+|\.+$/g, "");
}

async function findMachineByPreviewHostname(options: BackendOptions, hostname: string): Promise<RemoteMachine | undefined> {
  const preview = previewOptions(options);
  const normalized = normalizeHostname(hostname);
  if (!preview || !normalized.endsWith(`.${preview.baseDomain}`)) return undefined;
  for (const machine of await options.store.listMachines()) {
    const normalizedMachine = normalizeMachine(options, machine);
    if (normalizedMachine.preview_hostname === normalized) return normalizedMachine;
  }
  return undefined;
}

async function proxyPreviewRequest(
  options: BackendOptions,
  request: { method: string; url: string; headers: Record<string, string | string[] | undefined>; params: { hostname: string }; body?: unknown },
  reply: { code: (statusCode: number) => { send: (payload: unknown) => unknown }; header: (name: string, value: string) => unknown; send: (payload: Buffer | string) => unknown },
): Promise<unknown> {
  const preview = previewOptions(options);
  const hostname = normalizeHostname(request.params.hostname);
  const machine = await findMachineByPreviewHostname(options, hostname);
  if (!preview || !machine) {
    return reply.code(404).send({ id: "not_found", message: "preview hostname is not registered" });
  }
  if (!machine.public_ipv4) {
    return reply.code(503).send({ id: "not_ready", message: "preview machine does not have a public IPv4 yet" });
  }

  const targetURL = new URL(previewTargetSuffix(request.url, hostname), `http://${machine.public_ipv4}:${preview.targetPort}`);
  const headers = previewProxyHeaders(request.headers, hostname);
  const init: RequestInit = {
    method: request.method,
    headers,
  };
  if (request.method !== "GET" && request.method !== "HEAD" && request.body !== undefined) {
    init.body = previewProxyBody(request.body);
  }

  let upstream: Response;
  try {
    upstream = await fetch(targetURL, init);
  } catch (error) {
    return reply.code(502).send({ id: "preview_unreachable", message: `preview upstream is unreachable: ${(error as Error).message}` });
  }

  reply.code(upstream.status);
  upstream.headers.forEach((value, name) => {
    if (!hopByHopHeaders.has(name.toLowerCase())) {
      reply.header(name, value);
    }
  });
  reply.header("x-yolobox-preview-machine", machine.name);
  if (request.method === "HEAD") return reply.send("");
  return reply.send(Buffer.from(await upstream.arrayBuffer()));
}

function previewTargetSuffix(requestURL: string, hostname: string): string {
  const marker = `/v1/preview/proxy/${hostname}`;
  const index = requestURL.indexOf(marker);
  let suffix = index === -1 ? "/" : requestURL.slice(index + marker.length);
  if (suffix === "") suffix = "/";
  if (!suffix.startsWith("/")) suffix = `/${suffix}`;
  return suffix;
}

function previewProxyHeaders(input: Record<string, string | string[] | undefined>, hostname: string): Headers {
  const headers = new Headers();
  for (const [name, rawValue] of Object.entries(input)) {
    const lower = name.toLowerCase();
    if (hopByHopHeaders.has(lower) || lower === "host") continue;
    const values = Array.isArray(rawValue) ? rawValue : rawValue === undefined ? [] : [rawValue];
    for (const value of values) headers.append(name, value);
  }
  headers.set("x-forwarded-host", hostname);
  headers.set("x-forwarded-proto", "https");
  return headers;
}

function previewProxyBody(body: unknown): BodyInit {
  if (typeof body === "string") return body;
  if (body instanceof Uint8Array) return body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength) as BodyInit;
  return JSON.stringify(body);
}

const hopByHopHeaders = new Set([
  "connection",
  "keep-alive",
  "proxy-authenticate",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
]);

function registerCors(app: FastifyInstance, origins: string[]): void {
  const allowed = new Set(origins.map((origin) => origin.trim()).filter(Boolean));
  if (allowed.size === 0) return;
  void app.register(cors, {
    credentials: true,
    allowedHeaders: ["authorization", "content-type"],
    methods: ["GET", "POST", "DELETE", "OPTIONS"],
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
