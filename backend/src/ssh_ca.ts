import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const defaultCertTTLSeconds = 15 * 60;
const minCertTTLSeconds = 60;
const maxCertTTLSeconds = 60 * 60;

export type SignedSSHCertificate = {
  certificate: string;
  expires_at: string;
  principal: string;
};

export class SSHCertificateAuthority {
  private ready: Promise<string> | undefined;

  constructor(private readonly keyPath: string) {}

  async publicKey(): Promise<string> {
    if (!this.ready) this.ready = this.ensureKeyPair();
    return this.ready;
  }

  async signUserPublicKey(request: {
    publicKey: string;
    principal: string;
    identity: string;
    ttlSeconds?: number;
  }): Promise<SignedSSHCertificate> {
    const publicKey = request.publicKey.trim();
    const principal = request.principal.trim();
    const identity = sanitizeIdentity(request.identity || "yolobox");
    if (!publicKey) throw new Error("SSH public key is required");
    if (!principal) throw new Error("SSH certificate principal is required");
    await this.publicKey();

    const ttlSeconds = normalizeTTL(request.ttlSeconds);
    const dir = await mkdtemp(join(tmpdir(), "yolobox-ssh-cert-"));
    try {
      const publicKeyPath = join(dir, "id.pub");
      const certPath = join(dir, "id-cert.pub");
      await writeFile(publicKeyPath, `${publicKey}\n`, { mode: 0o600 });
      await execFileAsync("ssh-keygen", ["-l", "-f", publicKeyPath]);
      await execFileAsync("ssh-keygen", [
        "-q",
        "-s", this.keyPath,
        "-I", identity,
        "-n", principal,
        "-V", `+${ttlSeconds}s`,
        "-z", String(Date.now()),
        publicKeyPath,
      ]);
      return {
        certificate: (await readFile(certPath, "utf8")).trim(),
        expires_at: new Date(Date.now() + ttlSeconds * 1000).toISOString(),
        principal,
      };
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  }

  private async ensureKeyPair(): Promise<string> {
    const publicKeyPath = `${this.keyPath}.pub`;
    try {
      const existing = (await readFile(publicKeyPath, "utf8")).trim();
      if (existing) return existing;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }

    await mkdir(dirname(this.keyPath), { recursive: true });
    await rm(this.keyPath, { force: true });
    await rm(publicKeyPath, { force: true });
    await execFileAsync("ssh-keygen", [
      "-q",
      "-t", "ed25519",
      "-N", "",
      "-C", "yolobox-remote-user-ca",
      "-f", this.keyPath,
    ]);
    await chmod(this.keyPath, 0o600);
    return (await readFile(publicKeyPath, "utf8")).trim();
  }
}

function normalizeTTL(value: number | undefined): number {
  const ttl = Number.isFinite(value) ? Math.floor(value as number) : defaultCertTTLSeconds;
  return Math.min(maxCertTTLSeconds, Math.max(minCertTTLSeconds, ttl));
}

function sanitizeIdentity(value: string): string {
  return value.trim().replace(/[^A-Za-z0-9._:-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 128) || `yolobox-${randomUUID()}`;
}
