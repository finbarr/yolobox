import { generateKeyPairSync, randomBytes } from "node:crypto";

export type MachineSSHKeyPair = {
  private_key: string;
  authorized_key: string;
};

export function generateMachineSSHKeyPair(): MachineSSHKeyPair {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519", {
    publicKeyEncoding: { type: "spki", format: "der" },
    privateKeyEncoding: { type: "pkcs8", format: "der" },
  });
  const publicRaw = Buffer.from(publicKey).subarray(-32);
  const seed = Buffer.from(privateKey).subarray(-32);
  const publicBlob = sshBuffer(
    sshString("ssh-ed25519"),
    sshString(publicRaw),
  );
  const privatePayload = paddedPrivatePayload(publicRaw, seed);
  const openssh = sshBuffer(
    Buffer.from("openssh-key-v1\0", "binary"),
    sshString("none"),
    sshString("none"),
    sshString(Buffer.alloc(0)),
    sshUint32(1),
    sshString(publicBlob),
    sshString(privatePayload),
  );
  return {
    private_key: pemBlock("OPENSSH PRIVATE KEY", openssh),
    authorized_key: `ssh-ed25519 ${publicBlob.toString("base64")} yolobox-remote`,
  };
}

function paddedPrivatePayload(publicRaw: Buffer, seed: Buffer): Buffer {
  const check = randomBytes(4);
  const payload = sshBuffer(
    check,
    check,
    sshString("ssh-ed25519"),
    sshString(publicRaw),
    sshString(Buffer.concat([seed, publicRaw])),
    sshString("yolobox-remote"),
  );
  const blockSize = 8;
  let padLength = blockSize - (payload.length % blockSize);
  if (padLength === 0) padLength = blockSize;
  const padding = Buffer.alloc(padLength);
  for (let i = 0; i < padLength; i++) padding[i] = i + 1;
  return Buffer.concat([payload, padding]);
}

function pemBlock(label: string, data: Buffer): string {
  const base64 = data.toString("base64");
  const lines = base64.match(/.{1,70}/g) || [];
  return `-----BEGIN ${label}-----\n${lines.join("\n")}\n-----END ${label}-----\n`;
}

function sshString(value: string | Buffer): Buffer {
  const data = Buffer.isBuffer(value) ? value : Buffer.from(value, "utf8");
  return Buffer.concat([sshUint32(data.length), data]);
}

function sshUint32(value: number): Buffer {
  const out = Buffer.alloc(4);
  out.writeUInt32BE(value, 0);
  return out;
}

function sshBuffer(...parts: Buffer[]): Buffer {
  return Buffer.concat(parts);
}
