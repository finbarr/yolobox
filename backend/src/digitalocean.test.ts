import assert from "node:assert/strict";
import { test } from "node:test";
import { digitalOceanImageForCreate, digitalOceanProviderFromEnv, digitalOceanSizeForTier } from "./digitalocean.js";

test("DigitalOcean provider prefers generic yolobox remote image override", () => {
  const provider = digitalOceanProviderFromEnv({
    DIGITALOCEAN_ACCESS_TOKEN: "dop_v1_test",
    YOLOBOX_REMOTE_IMAGE: "yolobox-remote-snapshot",
    DIGITALOCEAN_IMAGE: "ubuntu-24-04-x64",
  });

  assert.equal((provider as unknown as { config: { image: string } }).config.image, "yolobox-remote-snapshot");
});

test("DigitalOcean provider keeps DigitalOcean image fallback", () => {
  const provider = digitalOceanProviderFromEnv({
    DIGITALOCEAN_ACCESS_TOKEN: "dop_v1_test",
    DIGITALOCEAN_IMAGE: "ubuntu-24-04-x64",
  });

  assert.equal((provider as unknown as { config: { image: string } }).config.image, "ubuntu-24-04-x64");
});

test("DigitalOcean provider sends numeric snapshot image IDs as numbers", () => {
  assert.equal(digitalOceanImageForCreate("123456789"), 123456789);
  assert.equal(digitalOceanImageForCreate("ubuntu-24-04-x64"), "ubuntu-24-04-x64");
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
