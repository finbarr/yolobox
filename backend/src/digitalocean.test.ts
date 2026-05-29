import assert from "node:assert/strict";
import { test } from "node:test";
import { digitalOceanAgentUserData, digitalOceanImageForCreate, digitalOceanImageIsYoloboxRemote, digitalOceanProviderFromEnv, digitalOceanSizeForTier } from "./digitalocean.js";

test("DigitalOcean provider prefers generic yolobox remote image override", () => {
  const provider = digitalOceanProviderFromEnv({
    DIGITALOCEAN_ACCESS_TOKEN: "dop_v1_test",
    YOLOBOX_REMOTE_IMAGE: "yolobox-remote-snapshot",
    DIGITALOCEAN_IMAGE: "ubuntu-24-04-x64",
  });

  assert.equal((provider as unknown as { config: { image: string } }).config.image, "yolobox-remote-snapshot");
  assert.equal((provider as unknown as { config: { imageBootstrapped: boolean } }).config.imageBootstrapped, true);
});

test("DigitalOcean provider keeps DigitalOcean image fallback", () => {
  const provider = digitalOceanProviderFromEnv({
    DIGITALOCEAN_ACCESS_TOKEN: "dop_v1_test",
    DIGITALOCEAN_IMAGE: "ubuntu-24-04-x64",
  });

  assert.equal((provider as unknown as { config: { image: string } }).config.image, "ubuntu-24-04-x64");
  assert.equal((provider as unknown as { config: { imageBootstrapped: boolean } }).config.imageBootstrapped, false);
});

test("DigitalOcean provider sends numeric snapshot image IDs as numbers", () => {
  assert.equal(digitalOceanImageForCreate("123456789"), 123456789);
  assert.equal(digitalOceanImageForCreate("ubuntu-24-04-x64"), "ubuntu-24-04-x64");
});

test("DigitalOcean provider recognizes yolobox remote image names as bootstrapped", () => {
  assert.equal(digitalOceanImageIsYoloboxRemote("yolobox-remote-504361a46b0a-20260526234009"), true);
  assert.equal(digitalOceanImageIsYoloboxRemote("ubuntu-24-04-x64"), false);
  assert.equal(digitalOceanImageIsYoloboxRemote(undefined), false);
});

test("DigitalOcean provider defaults to the small AMD size", () => {
  const provider = digitalOceanProviderFromEnv({
    DIGITALOCEAN_ACCESS_TOKEN: "dop_v1_test",
  });

  assert.equal((provider as unknown as { config: { size: string } }).config.size, "s-2vcpu-4gb-amd");
});

test("DigitalOcean size tiers map to provider slugs", () => {
  assert.equal(digitalOceanSizeForTier("small"), "s-2vcpu-4gb-amd");
  assert.equal(digitalOceanSizeForTier("medium"), "s-4vcpu-8gb-amd");
  assert.equal(digitalOceanSizeForTier("large"), "s-8vcpu-16gb-amd");
});

test("DigitalOcean creates machines with a throwaway no-login SSH key", async () => {
  const requests: Array<{ url: string; method: string; body?: Record<string, unknown> }> = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method || "GET";
    const body = init?.body ? JSON.parse(String(init.body)) as Record<string, unknown> : undefined;
    requests.push({ url, method, body });
    if (url.includes("/v2/droplets?")) return jsonResponse({ droplets: [] });
    if (url.endsWith("/v2/account/keys") && method === "POST") {
      assert.match(String(body?.name || ""), /^yolobox-no-login-foo-[a-f0-9]{12}$/);
      assert.match(String(body?.public_key || ""), /^ssh-ed25519 /);
      return jsonResponse({ ssh_key: { id: 321 } });
    }
    if (url.endsWith("/v2/droplets") && method === "POST") {
      return jsonResponse({
        droplet: {
          id: 123,
          name: "yolobox-foo",
          status: "new",
          tags: ["yolobox", "yolobox-foo"],
          size_slug: "s-2vcpu-4gb-amd",
          region: { slug: "nyc3" },
          image: { id: 456, name: "yolobox-remote-test" },
          networks: { v4: [{ ip_address: "203.0.113.10", type: "public" }] },
          created_at: "2026-05-29T00:00:00Z",
        },
      });
    }
    if (url.endsWith("/v2/account/keys/321") && method === "DELETE") {
      return new Response("", { status: 204 });
    }
    throw new Error(`unexpected request ${method} ${url}`);
  }) as typeof fetch;

  try {
    const provider = digitalOceanProviderFromEnv({
      DIGITALOCEAN_ACCESS_TOKEN: "dop_v1_test",
      YOLOBOX_DIGITALOCEAN_API_URL: "https://digitalocean.example.test",
    });

    await provider.createMachine({
      name: "foo",
      agent_token: "agent-token",
      agent_backend_url: "https://api.example.com",
      ssh_user_ca_public_key: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest yolobox-ca",
      ssh_authorized_principal: "yolobox:foo-123",
    });

    const createRequest = requests.find((request) => request.method === "POST" && request.url.endsWith("/v2/droplets"));
    assert.ok(createRequest?.body);
    assert.deepEqual(createRequest.body.ssh_keys, [321]);
    assert.equal(typeof createRequest.body.user_data, "string");
    assert.equal(requests.some((request) => request.method === "DELETE" && request.url.endsWith("/v2/account/keys/321")), true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("DigitalOcean agent user data carries token credentials and SSH CA trust", () => {
  const userData = digitalOceanAgentUserData({
    name: "victim-name",
    agent_token: "test-token",
    agent_backend_url: "https://api.example.com/",
    ssh_user_ca_public_key: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest yolobox-ca",
    ssh_authorized_principal: "yolobox:foo-123",
  });

  assert.match(userData, /YOLOBOX_AGENT_TOKEN='test-token'/);
  assert.match(userData, /YOLOBOX_AGENT_BACKEND_URL='https:\/\/api\.example\.com'/);
  assert.match(userData, /ssh_pwauth: false/);
  assert.match(userData, /\/run\/sshd/);
  assert.match(userData, /TrustedUserCAKeys \/etc\/ssh\/yolobox_user_ca_keys/);
  assert.match(userData, /AuthorizedPrincipalsFile \/etc\/ssh\/auth_principals\/%u/);
  assert.match(userData, /PasswordAuthentication no/);
  assert.match(userData, /yolobox:foo-123/);
  assert.doesNotMatch(userData, /victim-name/);
});

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}
