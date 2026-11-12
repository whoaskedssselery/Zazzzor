import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { type NotifyPatch, type NotifySettings, resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  fakeListLayout,
  makeChannel,
  makeGuildState,
  makeReady,
  readyGateway,
  server,
} from "@/shared/test";

const G = "10";
const TEXT = "101";

/**
 * Signs in to server 10 with #общий (unread), answers its history with
 * nothing, and answers each settings change the way the server would
 * merge it. Hands back the PATCH bodies.
 */
const signedIn = () => {
  const patches: { scope: string; body: NotifyPatch }[] = [];
  let settings: NotifySettings = {
    guild_id: G,
    muted: false,
    mute_until: null,
    message_notifications: 1,
    suppress_everyone: false,
    channel_overrides: [],
  };
  server.use(
    sessionHandler(testUser),
    readyGateway(
      makeReady(testUser, [
        makeGuildState(G, testUser, { channels: [makeChannel(TEXT, G, { name: "общий", last_message_id: "5" })] }),
      ]),
    ),
    apiHandler("get", "/channels/{channel_id}/messages", () => HttpResponse.json([])),
    apiHandler(
      "post",
      "/channels/{channel_id}/messages/{message_id}/ack",
      () => new HttpResponse(null, { status: 204 }),
    ),
    apiHandler("patch", "/users/@me/guilds/{guild_id}/settings", async ({ params, request }) => {
      const body = (await request.json());
      patches.push({ scope: params.guild_id, body });
      const { channel_overrides: overrides, ...scope } = body;
      settings = { ...settings, ...scope };
      for (const [id, o] of Object.entries(overrides ?? {})) {
        const held = settings.channel_overrides.find((c) => c.channel_id === id) ?? {
          channel_id: id,
          muted: false,
          mute_until: null,
          message_notifications: 3 as const,
        };
        settings = {
          ...settings,
          channel_overrides: [
            ...settings.channel_overrides.filter((c) => c.channel_id !== id),
            { ...held, ...o, mute_until: o.muted === false ? null : (o.mute_until ?? held.mute_until) },
          ],
        };
      }
      return HttpResponse.json(settings);
    }),
  );
  return patches;
};

let restoreLayout = () => {};

beforeEach(() => {
  restoreLayout = fakeListLayout();
});

afterEach(() => {
  restoreLayout();
  resetChannelUi();
  resetGuilds();
});

const channels = () => screen.getByRole("navigation", { name: "Каналы" });

describe("notification settings", () => {
  it("mutes a channel for an hour from the bell, and it stops lighting up", async () => {
    const patches = signedIn();
    const { user } = renderApp(`/channels/${G}`);
    await screen.findByRole("heading", { level: 1, name: "общий" });
    await user.click(screen.getByRole("button", { name: "Уведомления" }));
    // By keyboard, as jsdom has no pointer geometry for the submenu.
    (await screen.findByRole("menuitem", { name: "Заглушить канал" })).focus();
    await user.keyboard("{ArrowRight}");
    await screen.findByRole("menuitem", { name: "На 15 минут" });
    await user.keyboard("{ArrowDown}{Enter}");
    await waitFor(() => {
      expect(patches).toHaveLength(1);
    });
    const muted = patches[0]?.body.channel_overrides?.[TEXT];
    expect(patches[0]?.scope).toBe(G);
    expect(muted?.muted).toBe(true);
    expect(Date.parse(muted?.mute_until ?? "") - Date.now()).toBeGreaterThan(59 * 60_000);
    expect(await screen.findByRole("button", { name: "Уведомления (заглушено)" })).toBeTruthy();
    expect(within(channels()).getByRole("link", { name: /общий, заглушён/ })).toBeTruthy();

    // The same menu turns the sound back on.
    await user.click(screen.getByRole("button", { name: "Уведомления (заглушено)" }));
    await user.click(await screen.findByRole("menuitem", { name: "Включить звук" }));
    await waitFor(() => {
      expect(patches[1]?.body).toEqual({ channel_overrides: { [TEXT]: { muted: false } } });
    });
    expect(await screen.findByRole("button", { name: "Уведомления" })).toBeTruthy();
  });

  it("sets what alerts on the whole server from its menu", async () => {
    const patches = signedIn();
    const { user } = renderApp(`/channels/${G}`);
    await user.click(await screen.findByRole("button", { name: /Меню сервера/ }));
    (await screen.findByRole("menuitem", { name: "Уведомления" })).focus();
    await user.keyboard("{ArrowRight}");
    const mentions = await screen.findByRole("menuitemradio", { name: "Только @упоминания" });
    expect(mentions.getAttribute("aria-checked")).toBe("true");
    screen.getByRole("menuitemradio", { name: "Ничего" }).focus();
    await user.keyboard("{Enter}");
    await waitFor(() => {
      expect(patches).toEqual([{ scope: G, body: { message_notifications: 2 } }]);
    });
  });
});
