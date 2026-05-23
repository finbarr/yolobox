import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdtemp } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
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
        provider: this.name,
        provider_id: `fake-${request.name}`,
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
  const dir = await mkdtemp(join(tmpdir(), "yolobox-backend-"));
  const provider = new FakeProvider();
  const store = new StateStore(join(dir, "state.json"), provider.name);
  const app = createBackend({ token: "secret", provider, store });

  const unauthorized = await app.inject({ method: "GET", url: "/v1/machines" });
  assert.equal(unauthorized.statusCode, 401);

  const headers = { authorization: "Bearer secret" };
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
