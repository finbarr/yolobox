import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import { AddressInfo } from "node:net";
import { test } from "node:test";
import { mkdtemp } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createBackendAuth, migrateBackendAuth } from "./auth.js";
import { StateStore } from "./state.js";
import { createBackend } from "./server.js";
import { SSHCertificateAuthority } from "./ssh_ca.js";
import { CreateMachineRequest, MachineProvider, RemoteMachine } from "./types.js";

const testSSHUserPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBxsqJzPGdcbwFthXVe2lyIImV6BwTw4Ee5WcoeczwJf test";

class FakeProvider implements MachineProvider {
  readonly name = "fake";
  readonly label = "Fake Cloud";
  created: CreateMachineRequest[] = [];
  released: string[] = [];
  discovered: RemoteMachine[] = [];
  publicIPv4 = "203.0.113.10";

  async createMachine(request: CreateMachineRequest) {
    this.created.push(request);
    return {
      status: "created",
      machine: {
        name: request.name,
        provider_name: request.provider_name,
        provider: this.name,
        provider_id: `fake-${request.provider_name || request.name}`,
        public_ipv4: this.publicIPv4,
        ssh_user: request.ssh_user || "root",
        bootstrap_complete: true,
      },
    };
  }

  async getMachine(machine: RemoteMachine) {
    return {
      status: "active",
      machine: {
        ...machine,
        public_ipv4: machine.public_ipv4 || this.publicIPv4,
      },
    };
  }

  async listMachines() {
    return this.discovered.map((machine) => ({
      status: "active",
      machine: {
        provider: this.name,
        ssh_user: "root",
        ...machine,
      },
    }));
  }

  async releaseMachine(machine: RemoteMachine) {
    this.released.push(machine.name);
  }
}

test("backend creates, records, lists, and releases one machine", async () => {
  const { app, provider, token } = await createTestBackend();

  const unauthorized = await app.inject({ method: "GET", url: "/v1/machines" });
  assert.equal(unauthorized.statusCode, 401);

  const headers = { authorization: `Bearer ${token}` };
  const created = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers,
    payload: {
      name: "Foo",
      ssh_user: "ubuntu",
      tier: "Medium",
      source_path: "/Users/example/project",
      repo_url: "git@example.com:repo.git",
      branch: "main",
    },
  });
  assert.equal(created.statusCode, 201);
  const createBody = created.json();
  assert.equal(createBody.machine.name, "foo");
  assert.equal(createBody.machine.user_id.length > 0, true);
  assert.match(createBody.machine.provider_name, /^foo-[a-f0-9]{10}$/);
  assert.match(createBody.machine.preview_hostname, /^[a-z0-9]+-[a-z0-9]+-[a-f0-9]{6}\.hosted\.test$/);
  assert.equal(createBody.machine.preview_url, `https://${createBody.machine.preview_hostname}`);
  assert.equal(createBody.machine.project_path, "/opt/yolobox/project");
  assert.equal(createBody.machine.agent_token_hash, undefined);
  assert.match(createBody.machine.ssh_principal, /^yolobox:foo-[a-f0-9]{10}$/);
  assert.equal(provider.created.length, 1);
  assert.equal(provider.created[0].tier, "medium");
  assert.match(provider.created[0].agent_token || "", /^[A-Za-z0-9_-]{64}$/);
  assert.equal(provider.created[0].agent_backend_url, "https://api.hosted.test");
  assert.match(provider.created[0].ssh_user_ca_public_key || "", /^ssh-ed25519 /);
  assert.equal(provider.created[0].ssh_authorized_principal, createBody.machine.ssh_principal);

  const recorded = await app.inject({
    method: "POST",
    url: "/v1/machines/foo/commands/record",
    headers,
    payload: { command: ["codex"] },
  });
  assert.equal(recorded.statusCode, 200);
  assert.deepEqual(recorded.json().machine.last_command, ["codex"]);
  assert.equal(recorded.json().machine.bootstrap_complete, true);
  assert.equal(recorded.json().machine.preview_hostname, createBody.machine.preview_hostname);

  const listed = await app.inject({ method: "GET", url: "/v1/machines", headers });
  assert.equal(listed.statusCode, 200);
  assert.equal(listed.json().machines.length, 1);
  assert.equal(listed.json().machines[0].name, "foo");
  assert.equal(listed.json().machines[0].provider_label, "Fake Cloud");
  assert.equal(listed.json().machines[0].agent_token_hash, undefined);

  const fetched = await app.inject({ method: "GET", url: "/v1/machines/foo", headers });
  assert.equal(fetched.statusCode, 200);
  assert.equal(fetched.json().status, "active");

  const deleted = await app.inject({ method: "DELETE", url: "/v1/machines/foo", headers });
  assert.equal(deleted.statusCode, 204);
  assert.deepEqual(provider.released, ["foo"]);
});

test("backend authenticates machine agents only by opaque token", async () => {
  const { app, provider, token } = await createTestBackend();
  const created = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers: { authorization: `Bearer ${token}` },
    payload: { name: "foo" },
  });
  assert.equal(created.statusCode, 201, created.body);
  const agentToken = provider.created[0].agent_token || "";
  assert.match(agentToken, /^[A-Za-z0-9_-]{64}$/);

  const spoofedName = await app.inject({
    method: "POST",
    url: "/v1/agent/heartbeat?name=not-foo",
    headers: { authorization: `Bearer ${agentToken}` },
  });
  assert.equal(spoofedName.statusCode, 200, spoofedName.body);
  assert.equal(spoofedName.json().machine.name, "foo");
  assert.equal(typeof spoofedName.json().machine.agent_last_seen_at, "string");
  assert.equal(spoofedName.json().machine.agent_token_hash, undefined);

  const guessedName = await app.inject({
    method: "POST",
    url: "/v1/agent/heartbeat",
    headers: { authorization: "Bearer foo" },
  });
  assert.equal(guessedName.statusCode, 401, guessedName.body);
});

test("backend waits for a created machine agent before returning", async () => {
  const { app, provider, token } = await createTestBackend("ready@example.com", "password123", { machineReadyTimeoutMs: 2000 });
  const headers = { authorization: `Bearer ${token}` };
  const create = app.inject({
    method: "POST",
    url: "/v1/machines",
    headers,
    payload: { name: "foo" },
  });

  for (let i = 0; i < 50 && provider.created.length === 0; i++) {
    await delay(10);
  }
  const agentToken = provider.created[0]?.agent_token || "";
  assert.match(agentToken, /^[A-Za-z0-9_-]{64}$/);

  for (let i = 0; i < 50; i++) {
    const fetched = await app.inject({ method: "GET", url: "/v1/machines/foo", headers });
    if (fetched.statusCode === 200) break;
    await delay(10);
  }

  await app.ready();
  const agent = await app.injectWS("/v1/agent/connect", { headers: { authorization: `Bearer ${agentToken}`, host: "127.0.0.1" } });
  const created = await create;
  assert.equal(created.statusCode, 201, created.body);
  assert.equal(typeof created.json().machine.agent_last_seen_at, "string");
  agent.terminate();
});

test("backend signs short-lived SSH certificates for owned machines", async () => {
  const { app, provider, token } = await createTestBackend();
  const created = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers: { authorization: `Bearer ${token}` },
    payload: { name: "foo" },
  });
  assert.equal(created.statusCode, 201, created.body);
  const agentToken = provider.created[0].agent_token || "";
  assert.match(agentToken, /^[A-Za-z0-9_-]{64}$/);
  assert.equal(provider.created[0].ssh_authorized_principal, created.json().machine.ssh_principal);

  await app.ready();
  const agent = await app.injectWS("/v1/agent/connect", { headers: { authorization: `Bearer ${agentToken}`, host: "127.0.0.1" } });

  const cert = await app.inject({
    method: "POST",
    url: "/v1/machines/foo/ssh-cert",
    headers: { authorization: `Bearer ${token}` },
    payload: { public_key: testSSHUserPublicKey },
  });
  assert.equal(cert.statusCode, 200, cert.body);
  assert.match(cert.json().certificate, /^ssh-ed25519-cert-v01@openssh.com /);
  assert.equal(cert.json().principal, created.json().machine.ssh_principal);
  assert.equal(cert.json().host, "203.0.113.10");
  assert.equal(cert.json().ssh_user, "root");
  agent.terminate();
});

test("backend refuses SSH certificates when the machine agent is disconnected", async () => {
  const { app, token } = await createTestBackend();
  const created = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers: { authorization: `Bearer ${token}` },
    payload: { name: "foo" },
  });
  assert.equal(created.statusCode, 201, created.body);

  const cert = await app.inject({
    method: "POST",
    url: "/v1/machines/foo/ssh-cert",
    headers: { authorization: `Bearer ${token}` },
    payload: { public_key: testSSHUserPublicKey },
  });
  assert.equal(cert.statusCode, 409, cert.body);
  assert.equal(cert.json().id, "agent_disconnected");
});

test("backend delegates session lifecycle to the machine agent", async () => {
  const { app, provider, token } = await createTestBackend();
  const headers = { authorization: `Bearer ${token}` };
  const created = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers,
    payload: { name: "foo" },
  });
  assert.equal(created.statusCode, 201, created.body);
  const agentToken = provider.created[0].agent_token || "";

  await app.ready();
  const agent = await app.injectWS("/v1/agent/connect", { headers: { authorization: `Bearer ${agentToken}`, host: "127.0.0.1" } });

  const sessionRequest = app.inject({
    method: "POST",
    url: "/v1/machines/foo/sessions/yolobox/prepare",
    headers,
    payload: { command: ["codex"], attach: true },
  });
  const sessionRPC = JSON.parse((await nextWSMessage(agent)).toString());
  assert.equal(sessionRPC.type, "rpc");
  assert.equal(sessionRPC.action, "prepare_session");
  assert.deepEqual(sessionRPC.payload.command, ["codex"]);
  assert.equal(sessionRPC.payload.attach, true);
  agent.send(JSON.stringify({
    type: "rpc_result",
    rpc_id: sessionRPC.rpc_id,
    ok: true,
    result: { status: "started", attach_command: "tmux attach-session -t 'yolobox'", record_command: true },
  }));
  const session = await sessionRequest;
  assert.equal(session.statusCode, 200, session.body);
  assert.equal(session.json().result.attach_command, "tmux attach-session -t 'yolobox'");

  const fetched = await app.inject({ method: "GET", url: "/v1/machines/foo", headers });
  assert.deepEqual(fetched.json().machine.last_command, ["codex"]);

  agent.terminate();
});

test("backend has no user-callable workspace preparation endpoint", async () => {
  const { app, token } = await createTestBackend();
  const response = await app.inject({
    method: "POST",
    url: "/v1/machines/foo/workspace",
    headers: { authorization: `Bearer ${token}` },
    payload: { source_path: "/Users/example/project" },
  });
  assert.equal(response.statusCode, 404);
});

test("backend rejects duplicate machine creates", async () => {
  const { app, provider, token } = await createTestBackend();
  const headers = { authorization: `Bearer ${token}` };

  const first = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers,
    payload: { name: "foo" },
  });
  assert.equal(first.statusCode, 201, first.body);

  const duplicate = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers,
    payload: { name: "foo" },
  });
  assert.equal(duplicate.statusCode, 409, duplicate.body);
  assert.match(duplicate.body, /remote machine foo already exists/);
  assert.equal(provider.created.length, 1);
});

test("backend rejects unknown machine tiers", async () => {
  const { app, token } = await createTestBackend();
  const response = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers: { authorization: `Bearer ${token}` },
    payload: {
      name: "foo",
      tier: "enormous",
    },
  });
  assert.equal(response.statusCode, 400, response.body);
  assert.match(response.body, /invalid machine tier/);
});

test("backend imports provider machines for the authenticated user", async () => {
  const { app, provider, token } = await createTestBackend();
  const headers = { authorization: `Bearer ${token}` };
  const user = await app.inject({ method: "GET", url: "/v1/auth/whoami", headers });
  assert.equal(user.statusCode, 200);
  const suffix = hashUserID(user.json().user.id);

  provider.discovered = [{
    name: "already-there",
    provider_name: `already-there-${suffix}`,
    provider: provider.name,
    provider_id: "fake-imported",
    public_ipv4: "203.0.113.20",
  }];

  const listed = await app.inject({ method: "GET", url: "/v1/machines", headers });
  assert.equal(listed.statusCode, 200);
  assert.equal(listed.json().machines.length, 1);
  assert.equal(listed.json().machines[0].name, "already-there");

  const fetched = await app.inject({ method: "GET", url: "/v1/machines/already-there", headers });
  assert.equal(fetched.statusCode, 200);
  assert.equal(fetched.json().machine.public_ipv4, "203.0.113.20");

  const connect = await app.inject({ method: "GET", url: "/v1/machines/already-there/connect", headers });
  assert.equal(connect.statusCode, 200);
  assert.equal(connect.json().connect.transport, "direct_ssh_certificate");
  assert.equal(connect.json().connect.cli, "yolobox remote connect already-there");
  assert.equal(connect.json().connect.cli_run, "yolobox remote run already-there");
});

test("backend rejects creates that collide with provider-owned machines", async () => {
  const { app, provider, token } = await createTestBackend();
  const headers = { authorization: `Bearer ${token}` };
  const user = await app.inject({ method: "GET", url: "/v1/auth/whoami", headers });
  assert.equal(user.statusCode, 200);
  const suffix = hashUserID(user.json().user.id);

  provider.discovered = [{
    name: "already-there",
    provider_name: `already-there-${suffix}`,
    provider: provider.name,
    provider_id: "fake-imported",
    public_ipv4: "203.0.113.20",
  }];

  const response = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers,
    payload: { name: "already-there" },
  });
  assert.equal(response.statusCode, 409, response.body);
  assert.match(response.body, /remote machine already-there already exists/);
  assert.equal(provider.created.length, 0);
});

test("backend auth supports multiple users with isolated machine names", async () => {
  const { app, provider, token } = await createTestBackend("first@example.com");
  const secondToken = await signUp(app, "second@example.com");

  const firstHeaders = { authorization: `Bearer ${token}` };
  const secondHeaders = { authorization: `Bearer ${secondToken}` };

  const firstCreate = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers: firstHeaders,
    payload: { name: "foo" },
  });
  assert.equal(firstCreate.statusCode, 201);

  const secondListBefore = await app.inject({ method: "GET", url: "/v1/machines", headers: secondHeaders });
  assert.equal(secondListBefore.statusCode, 200);
  assert.equal(secondListBefore.json().machines.length, 0);

  const secondFetchBefore = await app.inject({ method: "GET", url: "/v1/machines/foo", headers: secondHeaders });
  assert.equal(secondFetchBefore.statusCode, 404);

  const secondCreate = await app.inject({
    method: "POST",
    url: "/v1/machines",
    headers: secondHeaders,
    payload: { name: "foo" },
  });
  assert.equal(secondCreate.statusCode, 201);
  assert.notEqual(firstCreate.json().machine.user_id, secondCreate.json().machine.user_id);
  assert.notEqual(firstCreate.json().machine.provider_name, secondCreate.json().machine.provider_name);
  assert.equal(provider.created.length, 2);
});

test("backend registers preview hostnames and proxies them to the machine", async () => {
  let previewHost = "";
  const upstream = createServer((request, response) => {
    assert.equal(request.headers["x-forwarded-host"], previewHost);
    response.setHeader("content-type", "text/plain");
    response.end(`preview:${request.url}`);
  });
  await new Promise<void>((resolve) => upstream.listen(0, "127.0.0.1", resolve));
  try {
    const address = upstream.address();
    assert.equal(typeof address, "object");
    const { app, provider, token } = await createTestBackend("preview@example.com", "password123", { previewTargetPort: (address as AddressInfo).port });
    provider.publicIPv4 = "127.0.0.1";
    const headers = { authorization: `Bearer ${token}` };
    const created = await app.inject({
      method: "POST",
      url: "/v1/machines",
      headers,
      payload: { name: "preview" },
    });
    assert.equal(created.statusCode, 201, created.body);
    previewHost = created.json().machine.preview_hostname;
    assert.equal(created.json().machine.preview_url, `https://${previewHost}`);

    const tlsCheck = await app.inject({ method: "GET", url: `/v1/preview/tls-check?domain=${encodeURIComponent(previewHost)}` });
    assert.equal(tlsCheck.statusCode, 200, tlsCheck.body);

    const unknownTLSCheck = await app.inject({ method: "GET", url: "/v1/preview/tls-check?domain=missing.hosted.test" });
    assert.equal(unknownTLSCheck.statusCode, 404);

    const proxied = await app.inject({ method: "GET", url: `/v1/preview/proxy/${previewHost}/hello?x=1` });
    assert.equal(proxied.statusCode, 200, proxied.body);
    assert.equal(proxied.headers["x-yolobox-preview-machine"], "preview");
    assert.equal(proxied.body, "preview:/hello?x=1");
  } finally {
    await new Promise<void>((resolve, reject) => upstream.close((error) => error ? reject(error) : resolve()));
  }
});

test("backend login and logout are handled by Better Auth", async () => {
  const { app, token } = await createTestBackend("login@example.com", "correct horse battery staple");

  const login = await app.inject({
    method: "POST",
    url: "/v1/auth/sign-in/email",
    payload: {
      email: "login@example.com",
      password: "correct horse battery staple",
    },
  });
  assert.equal(login.statusCode, 200);
  assert.equal(typeof login.json().token, "string");

  const whoami = await app.inject({ method: "GET", url: "/v1/auth/whoami", headers: { authorization: `Bearer ${login.json().token}` } });
  assert.equal(whoami.statusCode, 200);
  assert.equal(whoami.json().user.email, "login@example.com");

  const logout = await app.inject({ method: "POST", url: "/v1/auth/sign-out", headers: { authorization: `Bearer ${token}` } });
  assert.equal(logout.statusCode, 200);

  const afterLogout = await app.inject({ method: "GET", url: "/v1/auth/whoami", headers: { authorization: `Bearer ${token}` } });
  assert.equal(afterLogout.statusCode, 401);
});

test("backend supports browser-approved CLI device login", async () => {
  const { app, token } = await createTestBackend("cli@example.com");

  const code = await app.inject({
    method: "POST",
    url: "/v1/auth/device/code",
    payload: {
      client_id: "yolobox-cli",
      scope: "remote",
    },
  });
  assert.equal(code.statusCode, 200, code.body);
  const device = code.json();
  assert.equal(typeof device.device_code, "string");
  assert.equal(typeof device.user_code, "string");
  assert.match(device.verification_uri_complete, /\/device\?user_code=/);

  const headers = { authorization: `Bearer ${token}` };
  const verified = await app.inject({
    method: "GET",
    url: `/v1/auth/device?user_code=${encodeURIComponent(device.user_code)}`,
    headers,
  });
  assert.equal(verified.statusCode, 200, verified.body);
  assert.equal(verified.json().status, "pending");

  const approved = await app.inject({
    method: "POST",
    url: "/v1/auth/device/approve",
    headers,
    payload: { userCode: device.user_code },
  });
  assert.equal(approved.statusCode, 200, approved.body);
  assert.equal(approved.json().success, true);

  const exchanged = await app.inject({
    method: "POST",
    url: "/v1/auth/device/token",
    payload: {
      grant_type: "urn:ietf:params:oauth:grant-type:device_code",
      device_code: device.device_code,
      client_id: "yolobox-cli",
    },
  });
  assert.equal(exchanged.statusCode, 200, exchanged.body);
  assert.equal(typeof exchanged.json().access_token, "string");

  const whoami = await app.inject({
    method: "GET",
    url: "/v1/auth/whoami",
    headers: { authorization: `Bearer ${exchanged.json().access_token}` },
  });
  assert.equal(whoami.statusCode, 200);
  assert.equal(whoami.json().user.email, "cli@example.com");
});

async function createTestBackend(email = "user@example.com", password = "password123", options: { previewTargetPort?: number; machineReadyTimeoutMs?: number } = {}) {
  const dir = await mkdtemp(join(tmpdir(), "yolobox-backend-"));
  const provider = new FakeProvider();
  const store = new StateStore(join(dir, "state.json"), provider.name);
  const sshCA = new SSHCertificateAuthority(join(dir, "ssh_ca_ed25519"));
  const authOptions = {
    baseURL: "http://127.0.0.1/v1/auth",
    databasePath: join(dir, "auth.sqlite"),
    secret: "test-secret-with-at-least-thirty-two-bytes",
    deviceVerificationURL: "http://127.0.0.1/device",
  };
  await migrateBackendAuth(authOptions);
  const auth = createBackendAuth(authOptions);
  const app = createBackend({
    auth,
    provider,
    store,
    sshCA,
    apiPublicURL: "https://api.hosted.test",
    previewBaseDomain: "hosted.test",
    previewTargetPort: options.previewTargetPort,
    machineReadyTimeoutMs: options.machineReadyTimeoutMs ?? 0,
  });
  const token = await signUp(app, email, password);
  return { app, provider, token };
}

async function signUp(app: ReturnType<typeof createBackend>, email: string, password = "password123"): Promise<string> {
  const response = await app.inject({
    method: "POST",
    url: "/v1/auth/sign-up/email",
    payload: {
      email,
      password,
      name: email.split("@")[0],
    },
  });
  assert.equal(response.statusCode, 200, response.body);
  const body = response.json();
  assert.equal(typeof body.token, "string");
  return body.token;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function hashUserID(userID: string): string {
  return createHash("sha256").update(userID).digest("hex").slice(0, 10);
}

function nextWSMessage(socket: { once: (event: "message" | "error", handler: (data: Buffer | Error) => void) => void }): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    socket.once("message", (data) => resolve(Buffer.isBuffer(data) ? data : Buffer.from(data as unknown as ArrayBuffer)));
    socket.once("error", (error) => reject(error));
  });
}
