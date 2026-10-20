import { execFileSync } from "node:child_process";
import { createHmac } from "node:crypto";

// Pinned like deploy/.env.example; `--dev` uses the well-known devkey/secret pair.
export const LIVEKIT_IMAGE = "livekit/livekit-server:v1.13.10";
export const API_KEY = "devkey";
const API_SECRET = "secret";
const LABEL = "zazzzor-e2e";

const docker = (args: string[]): string => execFileSync("docker", args, { encoding: "utf8" }).trim();

/** Removes containers left over by an interrupted run. */
export function removeStaleContainers(): void {
  const ids = docker(["ps", "-aq", "--filter", `label=${LABEL}`]);
  if (ids) docker(["rm", "-f", ...ids.split(/\s+/)]);
}

export function startLiveKit(): string {
  const name = `zazzzor-e2e-livekit-${process.pid}-${Date.now()}`;
  docker([
    "run", "-d", "--rm", "--name", name, "--label", LABEL,
    "-p", "127.0.0.1:7880:7880", "-p", "127.0.0.1:7881:7881", "-p", "127.0.0.1:7882:7882/udp",
    LIVEKIT_IMAGE, "--dev", "--bind", "0.0.0.0", "--node-ip", "127.0.0.1",
  ]);
  return name;
}

export function stopLiveKit(name: string): void {
  try {
    docker(["rm", "-f", name]);
  } catch (err) {
    console.warn(`failed to remove ${name}`, err);
  }
}

export function liveKitLogs(name: string): string {
  try {
    return execFileSync("docker", ["logs", name], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  } catch {
    return "";
  }
}

export async function waitForLiveKit(timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch("http://127.0.0.1:7880/");
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error("LiveKit did not become ready on 127.0.0.1:7880");
}

const b64url = (data: string | Buffer): string => Buffer.from(data).toString("base64url");

/** HS256 access token, same claims as `lk token create --join`. */
export function accessToken(identity: string, room: string, ttlSec = 600): string {
  const now = Math.floor(Date.now() / 1000);
  const header = b64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const payload = b64url(
    JSON.stringify({
      iss: API_KEY,
      sub: identity,
      name: identity,
      nbf: now - 10,
      exp: now + ttlSec,
      video: { room, roomJoin: true, canPublish: true, canSubscribe: true, canPublishData: true },
    }),
  );
  const signature = createHmac("sha256", API_SECRET).update(`${header}.${payload}`).digest("base64url");
  return `${header}.${payload}.${signature}`;
}
