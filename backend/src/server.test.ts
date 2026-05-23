import assert from "node:assert/strict";
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
  ensured: EnsureMachineRequest[] = [];
  released: string[] = [];

  async ensureMachine(request: EnsureMachineRequest) {
    this.ensured.push(request);
    return {
      status: "created",
      machine: {
        name: request.name,
        provider_name: request.provider_name,
        provider: this.name,
        provider_id: `fake-${request.provider_name || request.name}`,
        public_ipv4: "203.0.113.10",
        ssh_user: request.ssh_user || "root",
      },
    };
  }

  async getMachine(machine: RemoteMachine) {
    return {
      status: "active",
      machine: {
        ...machine,
        public_ipv4: machine.public_ipv4 || "203.0.113.10",
      },
    };
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
  assert.equal(ensureBody.machine.project_path, "/opt/yolobox/project");
  assert.equal(provider.ensured.length, 1);

  const patched = await app.inject({
    method: "PATCH",
    url: "/v1/machines/foo",
    headers,
    payload: {
      last_command: ["codex"],
      bootstrap_complete: true,
    },
  });
  assert.equal(patched.statusCode, 200);
  assert.deepEqual(patched.json().machine.last_command, ["codex"]);

  const listed = await app.inject({ method: "GET", url: "/v1/machines", headers });
  assert.equal(listed.statusCode, 200);
  assert.equal(listed.json().machines.length, 1);
  assert.equal(listed.json().machines[0].name, "foo");

  const fetched = await app.inject({ method: "GET", url: "/v1/machines/foo", headers });
  assert.equal(fetched.statusCode, 200);
  assert.equal(fetched.json().status, "active");

  const deleted = await app.inject({ method: "DELETE", url: "/v1/machines/foo", headers });
  assert.equal(deleted.statusCode, 204);
  assert.deepEqual(provider.released, ["foo"]);
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

async function createTestBackend(email = "user@example.com", password = "password123") {
  const dir = await mkdtemp(join(tmpdir(), "yolobox-backend-"));
  const provider = new FakeProvider();
  const store = new StateStore(join(dir, "state.json"), provider.name);
  const authOptions = {
    baseURL: "http://127.0.0.1/v1/auth",
    databasePath: join(dir, "auth.sqlite"),
    secret: "test-secret-with-at-least-thirty-two-bytes",
  };
  await migrateBackendAuth(authOptions);
  const auth = createBackendAuth(authOptions);
  const app = createBackend({ auth, provider, store });
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
