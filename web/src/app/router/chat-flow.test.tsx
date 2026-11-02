import { screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { resetGuilds } from "@/entities/guild";
import { resetPending } from "@/entities/message";
import { resetDrafts } from "@/features/send-message";
import type { components } from "@/shared/api";
import {
  apiHandler,
  dispatchFrame,
  fakeListLayout,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeMessage,
  makeReady,
  makeUser,
  readyGateway,
  server,
} from "@/shared/test";
import { NO_HISTORY } from "@/widgets/chat";

type Message = components["schemas"]["Message"];
type Client = WebSocketHandlerConnection["client"];

const anna = makeUser("1", { display_name: "Аня" });
const G = "10";
const C = "101";
const general = makeChannel(C, G, { name: "общий" });

/** Server 10 with Аня, as testUser sees it; `everyone` is the @everyone mask. */
const guild = (everyone?: string) => {
  const me = makeMember(testUser);
  return makeGuildState(G, testUser, {
    guild: makeGuild(G, anna.id, { name: "Вечерний сеанс" }),
    roles: [makeEveryone(G, everyone)],
    channels: [general],
    members: [me, makeMember(anna)],
    my_member: me,
  });
};

/** The channel's messages on the server; GET answers from them as the API does. */
const history = (initial: Message[]) => {
  const all = [...initial];
  const queries: string[] = [];
  server.use(
    apiHandler("get", "/channels/{channel_id}/messages", ({ request }) => {
      const url = new URL(request.url);
      queries.push(url.search);
      const after = url.searchParams.get("after");
      const before = url.searchParams.get("before");
      const limit = Number(url.searchParams.get("limit") ?? 50);
      if (after !== null) return HttpResponse.json(all.filter((m) => Number(m.id) > Number(after)).slice(0, limit));
      const older = all.filter((m) => before === null || Number(m.id) < Number(before));
      return HttpResponse.json(older.slice(-limit).reverse());
    }),
  );
  return { all, queries };
};

/** Signs in with READY for `guilds` on every connection; hands back a sender for the latest socket. */
const signedIn = (everyone?: string, onConnect?: (client: Client, n: number) => void) => {
  let client: Client | undefined;
  let seq = 1;
  server.use(
    sessionHandler(testUser),
    readyGateway(makeReady(testUser, [guild(everyone)]), (c, n) => {
      client = c;
      seq = 1;
      onConnect?.(c, n);
    }),
  );
  return (t: string, d: unknown) => {
    seq += 1;
    client?.send(dispatchFrame(t, seq, d));
  };
};

const list = () => screen.findByRole("list", { name: "Сообщения в #общий" });

let restoreLayout: () => void = () => {};
beforeEach(() => {
  restoreLayout = fakeListLayout();
});

afterEach(() => {
  restoreLayout();
  resetChannelUi();
  resetGuilds();
  resetPending();
  resetDrafts();
});

describe("chat", () => {
  it("shows the channel's history with the author's name", async () => {
    history([makeMessage("1", C, anna, { content: "привет всем" })]);
    signedIn();
    renderApp(`/channels/${G}/${C}`);
    const messages = await list();
    expect(await within(messages).findByText("привет всем")).toBeTruthy();
    expect(within(messages).getByText("Аня")).toBeTruthy();
  });

  it("sends a message and shows it once, whatever comes first", async () => {
    const h = history([]);
    const send = signedIn();
    server.use(
      apiHandler("post", "/channels/{channel_id}/messages", async ({ request }) => {
        const body = await request.json();
        const m = makeMessage("2", C, testUser, {
          content: body.content,
          ...(body.nonce ? { nonce: body.nonce } : {}),
        });
        h.all.push(m);
        // The event overtakes the response.
        send("MESSAGE_CREATE", m);
        return HttpResponse.json(m, { status: 201 });
      }),
    );
    const { user } = renderApp(`/channels/${G}/${C}`);
    await list();
    await user.type(screen.getByRole("textbox", { name: "Сообщение в #общий" }), "всем привет{Enter}");
    await waitFor(() => {
      expect(screen.getAllByText("всем привет")).toHaveLength(1);
    });
    // The response settled too: still one.
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.getAllByText("всем привет")).toHaveLength(1);
  });

  it("shows a message of someone else as it arrives", async () => {
    history([]);
    const send = signedIn();
    renderApp(`/channels/${G}/${C}`);
    await list();
    send("MESSAGE_CREATE", makeMessage("3", C, anna, { content: "я тут" }));
    expect(await screen.findByText("я тут")).toBeTruthy();
  });

  it("catches up after a reconnect with after=<newest>, without duplicates", async () => {
    const h = history([makeMessage("1", C, anna, { content: "до обрыва" })]);
    let first: Client | undefined;
    signedIn(undefined, (client, n) => {
      if (n === 1) first = client;
    });
    renderApp(`/channels/${G}/${C}`);
    await screen.findByText("до обрыва");
    // The history is on screen; then the server restarts and a message is sent meanwhile.
    h.all.push(makeMessage("2", C, anna, { content: "пока вас не было" }));
    first?.close(1012, "service_restart");
    expect(await screen.findByText("пока вас не было", {}, { timeout: 3000 })).toBeTruthy();
    expect(screen.getAllByText("до обрыва")).toHaveLength(1);
    expect(h.queries.some((q) => q.includes("after=1"))).toBe(true);
  });

  it("asks for no history without READ_MESSAGE_HISTORY, and still lets the user write", async () => {
    const h = history([makeMessage("1", C, anna)]);
    // @everyone: VIEW_CHANNEL and SEND_MESSAGES only.
    signedIn(String((1n << 10n) | (1n << 11n)));
    renderApp(`/channels/${G}/${C}`);
    expect(await screen.findByText(NO_HISTORY)).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Сообщение в #общий" })).toBeTruthy();
    expect(h.queries).toEqual([]);
  });
});
