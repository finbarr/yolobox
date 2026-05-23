import Fastify, { FastifyInstance } from "fastify";
import { StateStore } from "./state.js";
import { EnsureMachineRequest, MachineProvider, RemoteMachine, defaultProjectPath } from "./types.js";

export type BackendOptions = {
  token: string;
  provider: MachineProvider;
  store: StateStore;
};

const namePattern = /^[a-z0-9][a-z0-9-]{0,62}$/;

export function createBackend(options: BackendOptions): FastifyInstance {
  const app = Fastify({ logger: false });

  app.addHook("preHandler", async (request, reply) => {
    if (request.method === "GET" && request.url === "/healthz") return;
    const header = request.headers.authorization || "";
    if (header !== `Bearer ${options.token}`) {
      return reply.code(401).send({ id: "unauthorized", message: "missing or invalid bearer token" });
    }
  });

  app.get("/healthz", async () => "ok\n");

  app.get("/v1/auth/whoami", async () => ({
    authenticated: true,
    provider: options.provider.name,
  }));

  app.post<{ Body: EnsureMachineRequest }>("/v1/machines/ensure", async (request, reply) => {
    const body = normalizeEnsureRequest(request.body);
    const error = validateName(body.name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });

    const existing = await options.store.getMachine(body.name);
    if (existing) {
      const refreshed = await options.provider.getMachine(existing);
      const machine = normalizeMachine({
        ...existing,
        ...refreshed.machine,
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
      source_path: body.source_path,
      repo_url: body.repo_url,
      branch: body.branch,
      ssh_user: provisioned.machine.ssh_user || body.ssh_user || "root",
    });
    await options.store.putMachine(machine);
    return { machine, status: provisioned.status || "leased" };
  });

  app.get("/v1/machines", async () => ({
    machines: (await options.store.listMachines()).map(normalizeMachine),
  }));

  app.get<{ Params: { name: string } }>("/v1/machines/:name", async (request, reply) => {
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    const existing = await options.store.getMachine(name);
    if (!existing) return reply.code(404).send({ id: "not_found", message: "machine is not leased" });
    const refreshed = await options.provider.getMachine(existing);
    const machine = normalizeMachine({ ...existing, ...refreshed.machine, name });
    await options.store.putMachine(machine);
    return { machine, status: refreshed.status || "leased" };
  });

  app.patch<{ Params: { name: string }; Body: RemoteMachine }>("/v1/machines/:name", async (request, reply) => {
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    try {
      const machine = normalizeMachine(await options.store.patchMachine(name, request.body));
      return { machine };
    } catch (err) {
      return reply.code(404).send({ id: "not_found", message: (err as Error).message });
    }
  });

  app.delete<{ Params: { name: string } }>("/v1/machines/:name", async (request, reply) => {
    const name = request.params.name;
    const error = validateName(name);
    if (error) return reply.code(400).send({ id: "bad_request", message: error });
    const existing = await options.store.getMachine(name);
    if (existing) {
      await options.provider.releaseMachine(existing);
      await options.store.deleteMachine(name);
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
