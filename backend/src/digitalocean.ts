import { createHash } from "node:crypto";
import { hostname } from "node:os";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { homedir } from "node:os";
import { CreateMachineRequest, ListProviderMachinesRequest, MachineProvider, MachineProviderInfo, RemoteMachine } from "./types.js";

const apiBaseURL = "https://api.digitalocean.com";
const digitalOceanDefaultSize = "s-2vcpu-4gb-amd";
const digitalOceanTierSizes: Record<string, string> = {
  small: "s-2vcpu-4gb-amd",
  medium: "s-4vcpu-8gb-amd",
  large: "s-8vcpu-16gb-amd",
};

type DigitalOceanConfig = {
  token: string;
  region: string;
  size: string;
  image: string;
  imageBootstrapped: boolean;
  sshKeys: string[];
  tags: string[];
  vpcUUID?: string;
  apiURL?: string;
};

type Droplet = {
  id: number;
  name: string;
  status?: string;
  tags?: string[];
  size_slug?: string;
  region?: { slug?: string };
  image?: { id?: number; slug?: string; name?: string };
  networks?: { v4?: Array<{ ip_address?: string; type?: string }> };
  created_at?: string;
};

type DropletList = {
  droplets: Droplet[];
  links?: {
    pages?: {
      next?: string;
    };
  };
};

type SSHKey = {
  id: number;
  fingerprint?: string;
  name?: string;
  public_key?: string;
};

export class DigitalOceanProvider implements MachineProvider {
  readonly name = "digitalocean";
  readonly label = "DigitalOcean";
  readonly info: MachineProviderInfo = {
    name: this.name,
    label: this.label,
    capabilities: ["create", "destroy", "list", "connect"],
  };
  private readonly apiURL: string;

  constructor(private readonly config: DigitalOceanConfig) {
    this.apiURL = (config.apiURL || apiBaseURL).replace(/\/+$/, "");
  }

  async createMachine(request: CreateMachineRequest): Promise<{ machine: RemoteMachine; status?: string }> {
    const providerName = request.provider_name || request.name;
    const existing = await this.findDroplet(providerName);
    if (existing) {
      throw new Error(`DigitalOcean droplet for ${request.name} already exists`);
    }

    const sshKeys = this.config.sshKeys.length > 0
      ? this.config.sshKeys
      : [String(await this.ensureDefaultSSHKey())];
    const agentUserData = digitalOceanAgentUserData(request);
    const droplet = await this.request<{ droplet: Droplet }>("/v2/droplets", {
      method: "POST",
      body: {
        name: machineResourceName(providerName),
        region: this.config.region,
        size: digitalOceanSizeForRequest(request, this.config),
        image: digitalOceanImageForCreate(this.config.image),
        ssh_keys: sshKeys.map((key) => (/^\d+$/.test(key) ? Number(key) : key)),
        tags: this.machineTags(providerName),
        monitoring: true,
        ...(agentUserData ? { user_data: agentUserData } : {}),
        ...(this.config.vpcUUID ? { vpc_uuid: this.config.vpcUUID } : {}),
      },
    });
    const ready = publicIPv4(droplet.droplet) ? droplet.droplet : await this.waitForAddress(droplet.droplet.id);
    return { machine: this.machineFromDroplet(request.name, providerName, request.ssh_user, ready), status: ready.status };
  }

  async getMachine(machine: RemoteMachine): Promise<{ machine: RemoteMachine; status?: string }> {
    const providerName = machine.provider_name || machine.name;
    const droplet = machine.provider_id
      ? (await this.request<{ droplet: Droplet }>(`/v2/droplets/${encodeURIComponent(machine.provider_id)}`)).droplet
      : await this.findDroplet(providerName);
    if (!droplet) throw new Error(`DigitalOcean droplet for ${machine.name} was not found`);
    return { machine: this.machineFromDroplet(machine.name, providerName, machine.ssh_user, droplet), status: droplet.status };
  }

  async listMachines(request: ListProviderMachinesRequest): Promise<Array<{ machine: RemoteMachine; status?: string }>> {
    const suffix = request.provider_name_suffix ? `-${request.provider_name_suffix}` : "";
    const droplets = await this.listDroplets();
    return droplets.flatMap((droplet) => {
      const providerName = providerNameFromDroplet(droplet);
      if (!providerName) return [];
      if (suffix && !providerName.endsWith(suffix)) return [];
      const logicalName = suffix ? providerName.slice(0, -suffix.length) : providerName;
      if (!logicalName) return [];
      return [{
        machine: this.machineFromDroplet(logicalName, providerName, request.ssh_user, droplet),
        status: droplet.status,
      }];
    });
  }

  async releaseMachine(machine: RemoteMachine): Promise<void> {
    const id = machine.provider_id || (await this.findDroplet(machine.provider_name || machine.name))?.id;
    if (!id) return;
    await this.request(`/v2/droplets/${encodeURIComponent(String(id))}`, { method: "DELETE" });
  }

  private async findDroplet(machineName: string): Promise<Droplet | undefined> {
    const tag = machineTag(machineName);
    const response = await this.request<{ droplets: Droplet[] }>(`/v2/droplets?tag_name=${encodeURIComponent(tag)}&per_page=200`);
    const want = machineResourceName(machineName);
    return response.droplets.find((droplet) => droplet.name === want && (droplet.tags || []).includes(tag));
  }

  private async listDroplets(): Promise<Droplet[]> {
    const baseTags = this.config.tags.length > 0 ? this.config.tags : ["yolobox"];
    const seen = new Map<number, Droplet>();
    for (const tag of baseTags) {
      let path = `/v2/droplets?tag_name=${encodeURIComponent(tag)}&per_page=200`;
      while (path) {
        const response = await this.request<DropletList>(path);
        for (const droplet of response.droplets) {
          seen.set(droplet.id, droplet);
        }
        path = nextPath(response.links?.pages?.next);
      }
    }
    return [...seen.values()];
  }

  private async waitForAddress(id: number): Promise<Droplet> {
    const deadline = Date.now() + 4 * 60 * 1000;
    while (Date.now() < deadline) {
      const response = await this.request<{ droplet: Droplet }>(`/v2/droplets/${id}`);
      if (publicIPv4(response.droplet)) return response.droplet;
      await new Promise((resolve) => setTimeout(resolve, 5000));
    }
    throw new Error(`timed out waiting for DigitalOcean droplet ${id} to receive a public IPv4`);
  }

  private async ensureDefaultSSHKey(): Promise<number> {
    const publicKey = await defaultPublicKey();
    const keys = await this.request<{ ssh_keys: SSHKey[] }>("/v2/account/keys?per_page=200");
    const existing = keys.ssh_keys.find((key) => key.public_key?.trim() === publicKey);
    if (existing) return existing.id;
    const hash = createHash("sha256").update(publicKey).digest("hex").slice(0, 12);
    const created = await this.request<{ ssh_key: SSHKey }>("/v2/account/keys", {
      method: "POST",
      body: {
        name: `yolobox-${sanitize(hostname() || "host")}-${hash}`,
        public_key: publicKey,
      },
    });
    return created.ssh_key.id;
  }

  private machineFromDroplet(machineName: string, providerName: string, sshUser: string | undefined, droplet: Droplet): RemoteMachine {
    const now = new Date().toISOString();
    return {
      name: machineName,
      provider_name: providerName,
      provider: this.name,
      provider_id: String(droplet.id),
      public_ipv4: publicIPv4(droplet),
      region: droplet.region?.slug || this.config.region,
      size: droplet.size_slug || this.config.size,
      image: droplet.image?.slug || droplet.image?.name || this.config.image,
      ssh_user: sshUser || "root",
      created_at: droplet.created_at || now,
      updated_at: now,
      bootstrap_complete: this.dropletBootstrapComplete(droplet),
    };
  }

  private dropletBootstrapComplete(droplet: Droplet): boolean {
    if (digitalOceanImageIsYoloboxRemote(droplet.image?.slug) || digitalOceanImageIsYoloboxRemote(droplet.image?.name)) {
      return true;
    }
    if (digitalOceanImageIsYoloboxRemote(this.config.image)) {
      return true;
    }
    if (this.config.imageBootstrapped && droplet.image?.id && String(droplet.image.id) === this.config.image) {
      return true;
    }
    return false;
  }

  private machineTags(name: string): string[] {
    return [...new Set([...this.config.tags, machineTag(name)])];
  }

  private async request<T = unknown>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
    const response = await fetch(`${this.apiURL}${path}`, {
      method: init.method || "GET",
      headers: {
        Authorization: `Bearer ${this.config.token}`,
        ...(init.body ? { "Content-Type": "application/json" } : {}),
      },
      body: init.body ? JSON.stringify(init.body) : undefined,
    });
    if (!response.ok) {
      const detail = await response.text();
      throw new Error(`DigitalOcean ${init.method || "GET"} ${path} failed: ${detail || response.statusText}`);
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }
}

export function digitalOceanProviderFromEnv(env = process.env): DigitalOceanProvider {
  const token = env.DIGITALOCEAN_ACCESS_TOKEN || env.DIGITALOCEAN_TOKEN || env.DO_API_TOKEN;
  if (!token) throw new Error("DigitalOcean provider requires DIGITALOCEAN_ACCESS_TOKEN");
  return new DigitalOceanProvider({
    token,
    region: env.DIGITALOCEAN_REGION || "nyc3",
    size: env.DIGITALOCEAN_SIZE || digitalOceanDefaultSize,
    image: env.YOLOBOX_REMOTE_IMAGE || env.DIGITALOCEAN_IMAGE || "ubuntu-24-04-x64",
    imageBootstrapped: Boolean(env.YOLOBOX_REMOTE_IMAGE),
    sshKeys: splitList(env.DIGITALOCEAN_SSH_KEYS),
    tags: splitList(env.DIGITALOCEAN_TAGS, ["yolobox"]),
    vpcUUID: env.DIGITALOCEAN_VPC_UUID,
    apiURL: env.YOLOBOX_DIGITALOCEAN_API_URL,
  });
}

function digitalOceanSizeForRequest(request: CreateMachineRequest, config: DigitalOceanConfig): string {
  const tierSize = request.tier ? digitalOceanSizeForTier(request.tier) : "";
  return tierSize || config.size || digitalOceanDefaultSize;
}

export function digitalOceanSizeForTier(tier: string): string | undefined {
  return digitalOceanTierSizes[tier];
}

export function digitalOceanImageForCreate(image: string): string | number {
  const value = image.trim();
  return /^\d+$/.test(value) ? Number(value) : value;
}

export function digitalOceanImageIsYoloboxRemote(image: string | undefined): boolean {
  return Boolean(image?.trim().toLowerCase().startsWith("yolobox-remote-"));
}

export function digitalOceanAgentUserData(request: CreateMachineRequest): string {
  const token = request.agent_token?.trim();
  const backendURL = request.agent_backend_url?.trim().replace(/\/+$/, "");
  const authorizedKey = request.agent_ssh_authorized_key?.trim();
  if (!token || !backendURL) return "";
  return `#cloud-config
write_files:
  - path: /etc/yolobox/agent.env
    owner: root:root
    permissions: '0600'
    content: |
      ${shellEnvAssignment("YOLOBOX_AGENT_TOKEN", token)}
      ${shellEnvAssignment("YOLOBOX_AGENT_BACKEND_URL", backendURL)}
runcmd:
  - [sh, -lc, ${cloudInitSingleQuote(rootAuthorizedKeyCommand(authorizedKey))}]
  - [sh, -lc, 'systemctl enable --now yolobox-agent || true']
`;
}

function shellEnvAssignment(name: string, value: string): string {
  return `${name}='${value.replace(/'/g, "'\"'\"'")}'`;
}

function rootAuthorizedKeyCommand(authorizedKey: string | undefined): string {
  if (!authorizedKey) return "true";
  return `mkdir -p /root/.ssh && touch /root/.ssh/authorized_keys && (grep -qxF ${shellSingleQuote(authorizedKey)} /root/.ssh/authorized_keys || printf '%s\\n' ${shellSingleQuote(authorizedKey)} >> /root/.ssh/authorized_keys) && chmod 700 /root/.ssh && chmod 600 /root/.ssh/authorized_keys`;
}

function cloudInitSingleQuote(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

function shellSingleQuote(value: string): string {
  return `'${value.replace(/'/g, "'\"'\"'")}'`;
}

function publicIPv4(droplet: Droplet): string {
  return droplet.networks?.v4?.find((network) => network.type === "public")?.ip_address || "";
}

function machineResourceName(name: string): string {
  return `yolobox-${sanitize(name)}`;
}

function machineTag(name: string): string {
  return `yolobox-${sanitize(name)}`;
}

function providerNameFromDroplet(droplet: Droplet): string {
  if (droplet.name.startsWith("yolobox-")) return droplet.name.slice("yolobox-".length);
  const tag = (droplet.tags || []).find((value) => value.startsWith("yolobox-") && value !== "yolobox");
  return tag ? tag.slice("yolobox-".length) : "";
}

function nextPath(next: string | undefined): string {
  if (!next) return "";
  try {
    const parsed = new URL(next);
    return `${parsed.pathname}${parsed.search}`;
  } catch {
    return next.startsWith("/") ? next : "";
  }
}

function sanitize(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "default";
}

function splitList(value: string | undefined, fallback: string[] = []): string[] {
  const parts = (value || "").split(",").map((part) => part.trim()).filter(Boolean);
  return parts.length > 0 ? parts : fallback;
}

async function defaultPublicKey(): Promise<string> {
  const configured = process.env.YOLOBOX_REMOTE_SSH_PUBLIC_KEY?.trim();
  if (configured) return configured;
  for (const name of ["id_ed25519.pub", "id_rsa.pub"]) {
    try {
      const key = (await readFile(join(homedir(), ".ssh", name), "utf8")).trim();
      if (key) return key;
    } catch {
      // Try the next common public key path.
    }
  }
  throw new Error("no SSH public key found; set DIGITALOCEAN_SSH_KEYS or YOLOBOX_REMOTE_SSH_PUBLIC_KEY");
}
