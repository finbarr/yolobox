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
import { EnsureMachineRequest, MachineProvider, RemoteMachine } from "./types.js";

class FakeProvider implements MachineProvider {
  readonly name = "fake";
  readonly label = "Fake Cloud";
  ensured: EnsureMachineRequest[] = [];
  released: string[] = [];
  discovered: RemoteMachine[] = [];
  publicIPv4 = "203.0.113.10";

  async ensureMachine(request: EnsureMachineRequest) {
    this.ensured.push(request);
    return {
      status: "created",
      machine: {
        name: request.name,
        provider_name: request.provider_name,
        provider: this.name,
        provider_id: `fake-${request.provider_name || request.name}`,
        public_ipv4: this.publicIPv4,
        ssh_user: request.ssh_user || "root",
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

test("backend leases, updates, lists, and releases one machine", async () => {
  const { app, provider, token } = await createTestBackend();

  const unauthorized = await app.inject({ method: "GET", url: "/v1/machines" });
  assert.equal(unauthorized.statusCode, 401);

  const headers = { authorization: `Bearer ${token}` };
  const ensured = await app.inject({
    method: "POST",
    url: "/v1/machines/ensure",
    headers,
    payload: {
      name: "Foo",
      ssh_user: "ubuntu",
      source_path: "/Users/example/project",
      repo_url: "git@example.com:repo.git",
      branch: "main",
    },
  });
  assert.equal(ensured.statusCode, 200);
  const ensureBody = ensured.json();
  assert.equal(ensureBody.machine.name, "foo");
  assert.equal(ensureBody.machine.user_id.length > 0, true);
  assert.match(ensureBody.machine.provider_name, /^foo-[a-f0-9]{10}$/);
  assert.match(ensureBody.machine.preview_hostname, /^[a-z0-9]+-[a-z0-9]+-[a-f0-9]{6}\.hosted\.test$/);
  assert.equal(ensureBody.machine.preview_url, `https://${ensureBody.machine.preview_hostname}`);
  assert.equal(ensureBody.machine.project_path, "/opt/yolobox/project");
  assert.equal(provider.ensured.length, 1);

  const patched = await app.inject({
    method: "PATCH",
    url: "/v1/machines/foo",
    headers,
    payload: {
      last_command: ["codex"],
      bootstrap_complete: true,
      public_ipv4: "127.0.0.1",
      preview_hostname: "takeover.hosted.test",
    },
  });
  assert.equal(patched.statusCode, 200);
  assert.deepEqual(patched.json().machine.last_command, ["codex"]);
  assert.equal(patched.json().machine.public_ipv4, "203.0.113.10");
  assert.equal(patched.json().machine.preview_hostname, ensureBody.machine.preview_hostname);

  const listed = await app.inject({ method: "GET", url: "/v1/machines", headers });
  assert.equal(listed.statusCode, 200);
  assert.equal(listed.json().machines.length, 1);
  assert.equal(listed.json().machines[0].name, "foo");
  assert.equal(listed.json().machines[0].provider_label, "Fake Cloud");

  const fetched = await app.inject({ method: "GET", url: "/v1/machines/foo", headers });
  assert.equal(fetched.statusCode, 200);
  assert.equal(fetched.json().status, "active");

  const deleted = await app.inject({ method: "DELETE", url: "/v1/machines/foo", headers });
  assert.equal(deleted.statusCode, 204);
  assert.deepEqual(provider.released, ["foo"]);
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
  assert.equal(connect.json().connect.ssh, "ssh root@203.0.113.20");
  assert.equal(connect.json().connect.cli, "yolobox remote connect already-there");
});

test("backend auth supports multiple users with isolated machine names", async () => {
  const { app, provider, token } = await createTestBackend("first@example.com");
  const secondToken = await signUp(app, "second@example.com");

  const firstHeaders = { authorization: `Bearer ${token}` };
  const secondHeaders = { authorization: `Bearer ${secondToken}` };

  const firstEnsure = await app.inject({
    method: "POST",
    url: "/v1/machines/ensure",
    headers: firstHeaders,
    payload: { name: "foo" },
  });
  assert.equal(firstEnsure.statusCode, 200);

  const secondListBefore = await app.inject({ method: "GET", url: "/v1/machines", headers: secondHeaders });
  assert.equal(secondListBefore.statusCode, 200);
  assert.equal(secondListBefore.json().machines.length, 0);

  const secondFetchBefore = await app.inject({ method: "GET", url: "/v1/machines/foo", headers: secondHeaders });
  assert.equal(secondFetchBefore.statusCode, 404);

  const secondEnsure = await app.inject({
    method: "POST",
    url: "/v1/machines/ensure",
    headers: secondHeaders,
    payload: { name: "foo" },
  });
  assert.equal(secondEnsure.statusCode, 200);
  assert.notEqual(firstEnsure.json().machine.user_id, secondEnsure.json().machine.user_id);
  assert.notEqual(firstEnsure.json().machine.provider_name, secondEnsure.json().machine.provider_name);
  assert.equal(provider.ensured.length, 2);
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
    const ensured = await app.inject({
      method: "POST",
      url: "/v1/machines/ensure",
      headers,
      payload: { name: "preview" },
    });
    assert.equal(ensured.statusCode, 200, ensured.body);
    previewHost = ensured.json().machine.preview_hostname;
    assert.equal(ensured.json().machine.preview_url, `https://${previewHost}`);

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

async function createTestBackend(email = "user@example.com", password = "password123", options: { previewTargetPort?: number } = {}) {
  const dir = await mkdtemp(join(tmpdir(), "yolobox-backend-"));
  const provider = new FakeProvider();
  const store = new StateStore(join(dir, "state.json"), provider.name);
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
    previewBaseDomain: "hosted.test",
    previewTargetPort: options.previewTargetPort,
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

function hashUserID(userID: string): string {
  return createHash("sha256").update(userID).digest("hex").slice(0, 10);
}
