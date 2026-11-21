/**
 * Merge-gate smoke: real LiveKit (Docker) + the production bundle under the
 * production CSP + two headless Chrome pages with a fake 1920×1080 screen.
 *
 * Asserted: viewer gets 1080p (adaptiveStream off), frames flow, bitrate > 0,
 * transport is UDP, the sharer encodes frames, “New measurement” resets the phase,
 * duplicate identity shows its message, no CSP violations or page errors.
 *
 * Not asserted (headless limits, measured on this machine): fps is ~48–55, not
 * 60, and the fake screen is so simple that H.264 needs only ~1.4 Mbit/s, so
 * "60 fps" and "bitrate > 2000 kbit/s" would be flaky. Those are checked by
 * people in the real test (deploy/README.md, section 8).
 */
import { type Browser, expect, type Page, test } from "@playwright/test";
import type { ConnectionEvents } from "../src/connection-events";
import type { ConnectivityAggregate } from "../src/report";
import { accessToken } from "./livekit";

type Report = ConnectivityAggregate &
  ConnectionEvents & { name: string; userAgent: string; startedAt: string; durationSec: number };

const ROOM = `e2e-${Date.now()}`;
const MEASURE_MS = 20_000;

interface Opened {
  page: Page;
  problems: string[];
}

/** Opens the page for `identity`, collecting CSP violations and uncaught errors. */
async function open(browser: Browser, identity: string): Promise<Opened> {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  const problems: string[] = [];
  page.on("pageerror", (err) => problems.push(`pageerror: ${err.message}`));
  page.on("console", (msg) => {
    if (/Content Security Policy|Refused to/i.test(msg.text())) problems.push(`csp: ${msg.text()}`);
  });
  await page.goto(`/connectivity-test/#token=${accessToken(identity, ROOM)}`);
  return { page, problems };
}

async function connect(page: Page, identity: string): Promise<void> {
  await page.getByRole("button", { name: "Connect" }).click();
  await expect(page.getByText(`Connected as ${identity}`)).toBeVisible({ timeout: 20_000 });
}

async function report(page: Page): Promise<Report> {
  await page.getByRole("button", { name: /Copy report|Copied|Copy the text below manually/ }).click();
  const text = (await page.locator("pre").textContent()) ?? "";
  return JSON.parse(text) as Report;
}

test("a 1080p screen share reaches a viewer over UDP", async ({ browser }) => {
  const a = await open(browser, "alice");
  const b = await open(browser, "bob");

  await expect(a.page.locator("html")).toHaveAttribute("lang", "en");
  const invite = new URL(a.page.url()).hash;
  await a.page.getByRole("link", { name: "Русский" }).click();
  await expect(a.page.locator("html")).toHaveAttribute("lang", "ru");
  await expect(a.page.getByRole("button", { name: "Подключиться" })).toBeVisible();
  expect(new URL(a.page.url()).hash === invite).toBe(true);
  await a.page.getByRole("link", { name: "English" }).click();

  await connect(a.page, "alice");
  await a.page.getByRole("button", { name: "Start sharing" }).click();
  await expect(a.page.getByRole("button", { name: "Stop sharing" })).toBeVisible({ timeout: 20_000 });

  await connect(b.page, "bob");
  await expect(b.page.locator("video.demo")).toHaveCount(1, { timeout: 20_000 });
  // Live panel names the sender by identity, not by track id.
  await expect(b.page.getByText("Receiving from alice")).toBeVisible({ timeout: 10_000 });

  await b.page.waitForTimeout(MEASURE_MS);
  const viewer = await report(b.page);
  const sharer = await report(a.page);
  console.log("viewer report:", JSON.stringify(viewer));
  console.log("sharer report:", JSON.stringify(sharer));

  expect(viewer.maxInboundHeight).toBe(1080);
  expect(viewer.minInboundFps ?? 0).toBeGreaterThan(0);
  expect(viewer.avgInboundKbps ?? 0).toBeGreaterThan(0);
  expect(viewer.protocolsSeen).toContain("udp");
  expect(viewer.disconnects).toBe(0);
  expect(viewer.userAgent).toContain("Chrome/");
  expect(sharer.maxOutboundFps ?? 0).toBeGreaterThan(0);

  // “New measurement” starts a fresh phase without dropping the connection.
  await b.page.getByRole("button", { name: "New measurement" }).click();
  await b.page.waitForTimeout(5_000);
  const phase2 = await report(b.page);
  expect(phase2.samples).toBeLessThanOrEqual(3);
  expect(phase2.durationSec).toBeLessThanOrEqual(7);
  expect(Date.parse(phase2.startedAt)).toBeGreaterThan(Date.parse(viewer.startedAt));
  expect(phase2.maxInboundHeight).toBe(1080);

  // Opening the same link elsewhere kicks the first tab with a clear message.
  const dup = await open(browser, "alice");
  await connect(dup.page, "alice");
  await expect(a.page.getByText("This link is open in another tab or on another device")).toBeVisible({
    timeout: 15_000,
  });
  const kicked = await report(a.page);
  expect(kicked.disconnects).toBe(1);
  expect(kicked.disconnectReasons).toEqual(["DUPLICATE_IDENTITY"]);

  expect([...a.problems, ...b.problems, ...dup.problems]).toEqual([]);
});
