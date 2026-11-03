import { randomInt, randomUUID } from "node:crypto";
import { type Browser, type BrowserContext, test as base, expect, type Page } from "@playwright/test";
import { ORIGIN } from "./env";

/** A signed-in friend: their own browser context (cookies, address) and page. */
export interface Person {
  id: string;
  username: string;
  password: string;
  displayName: string;
  context: BrowserContext;
  page: Page;
}

/** A login no other run has used: 2–32 of [a-z0-9_.]. */
export const uniqueLogin = (prefix: string): string =>
  `${prefix}_${Date.now().toString(36)}${randomInt(36 ** 4).toString(36)}`.slice(0, 32);

/**
 * A made-up client address. The e2e API trusts X-Forwarded-For from any
 * peer (e2e/compose.yml), so each person gets the per-address limits (three
 * sign-ups an hour, gateway upgrades) to themselves, as on a real instance.
 */
const madeUpAddress = (): string => `10.${randomInt(256)}.${randomInt(256)}.${randomInt(1, 255)}`;

/** A browser context of its own, coming from its own address. */
export const newVisitor = (browser: Browser): Promise<BrowserContext> =>
  browser.newContext({ extraHTTPHeaders: { "X-Forwarded-For": madeUpAddress() } });

/**
 * Calls the API as `who` (their cookie and address), with the Origin every
 * state-changing request needs; fails the test on an error status.
 */
export const call = async <T = unknown>(
  who: Person | BrowserContext,
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE",
  path: string,
  data?: unknown,
): Promise<T> => {
  const context = "page" in who ? who.context : who;
  const res = await context.request.fetch(`/api/v1${path}`, {
    method,
    headers: { Origin: ORIGIN },
    ...(data === undefined ? {} : { data }),
  });
  expect(res.ok(), `${method} ${path}: ${String(res.status())} ${await res.text()}`).toBe(true);
  return (res.status() === 204 ? undefined : await res.json()) as T;
};

/** Registers a new account in a fresh context and opens a page there, signed in. */
export const signUp = async (browser: Browser, displayName: string, username = uniqueLogin("u")): Promise<Person> => {
  const context = await newVisitor(browser);
  const password = randomUUID();
  const user = await call<{ id: string }>(context, "POST", "/auth/register", {
    username,
    password,
    display_name: displayName,
  });
  const page = await context.newPage();
  return { id: user.id, username, password, displayName, context, page };
};

interface Guild {
  id: string;
}
interface Channel {
  id: string;
  name: string;
  type: number;
}

/** A server of `owner` and its first text channel. */
export const createGuild = async (owner: Person, name: string) => {
  const guild = await call<Guild>(owner, "POST", "/guilds", { name });
  const channels = await call<Channel[]>(owner, "GET", `/guilds/${guild.id}/channels`);
  const text = channels.find((c) => c.type === 0);
  if (!text) throw new Error("a new server has no text channel");
  return { guildId: guild.id, channelId: text.id, channelName: text.name };
};

/** `member` joins through a fresh invite to `channelId` made by `owner`. */
export const invite = async (owner: Person, channelId: string): Promise<string> => {
  const created = await call<{ code: string }>(owner, "POST", `/channels/${channelId}/invites`, {
    max_age_seconds: 3600,
    max_uses: 0,
  });
  return created.code;
};

export const join = (member: Person, code: string) => call(member, "POST", `/invites/${code}`);

/** A text channel at the address of the app, once its chat is ready to type into. */
export const openChannel = async (who: Person, guildId: string, channelId: string, channelName: string) => {
  await who.page.goto(`/channels/${guildId}/${channelId}`);
  await expect(composer(who.page, channelName)).toBeVisible();
};

export const composer = (page: Page, channelName: string) =>
  page.getByRole("textbox", { name: `Сообщение в #${channelName}` });

/** The message list of the open channel. */
export const messages = (page: Page, channelName: string) =>
  page.getByRole("list", { name: `Сообщения в #${channelName}` });

/** Every context a test opens is closed after it, also when it fails. */
export const test = base.extend<{ people: Person[] }>({
  // biome-ignore lint/correctness/noEmptyPattern: Playwright reads the fixtures a fixture needs from this pattern.
  people: async ({}, provide) => {
    const people: Person[] = [];
    await provide(people);
    await Promise.all(people.map((p) => p.context.close()));
  },
});

export { expect };
