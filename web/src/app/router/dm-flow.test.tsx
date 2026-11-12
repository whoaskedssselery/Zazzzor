import { screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  dispatchFrame,
  fakeListLayout,
  makeDMChannel,
  makeMessage,
  makeReady,
  makeRelationship,
  makeUser,
  readyGateway,
  server,
} from "@/shared/test";

type Client = WebSocketHandlerConnection["client"];

const anya = makeUser("2", { display_name: "Аня", username: "anya" });
const boris = makeUser("3", { display_name: "Борис", username: "boris" });
const vera = makeUser("4", { display_name: "Вера", username: "vera" });

const DM = "900";
const GROUP = "901";

/** A message in a private channel, as the API and the gateway carry it. */
const dmMessage = (id: string, channelId: string, author = anya, content = `сообщение ${id}`) =>
  makeMessage(id, channelId, author, { guild_id: null, content });

/**
 * Signs in with friends Аня, Борис and Вера, a DM with Аня (two unread)
 * and a group «Пятница» with Аня and Борис; hands back a way to send events.
 */
const signedIn = (blocked: string[] = []) => {
  let client: Client | undefined;
  server.use(
    sessionHandler(testUser),
    readyGateway(
      {
        ...makeReady(testUser),
        relationships: [
          makeRelationship(anya, 1),
          makeRelationship(boris, 1),
          makeRelationship(vera, 1),
          ...blocked.map((id) => makeRelationship(makeUser(id), 2)),
        ],
        private_channels: [
          makeDMChannel(DM, [anya], { last_message_id: "12" }),
          makeDMChannel(GROUP, [anya, boris], { name: "Пятница", owner_id: testUser.id, last_message_id: "5" }),
        ],
        read_states: [
          { channel_id: DM, last_read_id: "10", mention_count: 2 },
          { channel_id: GROUP, last_read_id: "5", mention_count: 0 },
        ],
      },
      (c) => {
        client = c;
      },
    ),
    apiHandler("get", "/channels/{channel_id}/messages", ({ params }) =>
      HttpResponse.json(
        params.channel_id === DM
          ? [dmMessage("12", DM), dmMessage("11", DM), dmMessage("10", DM, testUser, "моё")]
          : [dmMessage("5", GROUP, boris, "в пятницу?")],
      ),
    ),
    apiHandler(
      "post",
      "/channels/{channel_id}/messages/{message_id}/ack",
      () => new HttpResponse(null, { status: 204 }),
    ),
  );
  let seq = 1;
  return (t: string, d: unknown) => {
    if (!client) throw new Error("the gateway is not connected");
    seq += 1;
    client.send(dispatchFrame(t, seq, d));
  };
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

const list = () => screen.findByRole("region", { name: "Личные сообщения" });

describe("direct messages", () => {
  it("lists the DMs and groups on home, the most recent first, with what is unread", async () => {
    signedIn();
    renderApp("/channels/@me");
    const dms = await list();
    const links = within(dms).getAllByRole("link");
    expect(links.map((l) => l.getAttribute("aria-label"))).toEqual(["Аня, непрочитанных: 2", "Пятница"]);
    // The unread DM also waits under home in the rail.
    const rail = screen.getByRole("navigation", { name: "Серверы" });
    expect(within(rail).getByRole("link", { name: "Аня, непрочитанных: 2" })).toBeTruthy();
  });

  it("opens a DM with its history and reads it", async () => {
    signedIn();
    const acks: string[] = [];
    server.use(
      apiHandler("post", "/channels/{channel_id}/messages/{message_id}/ack", ({ params }) => {
        acks.push(`${params.channel_id}:${params.message_id}`);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, router } = renderApp("/channels/@me");
    await user.click(within(await list()).getByRole("link", { name: "Аня, непрочитанных: 2" }));
    expect(router.state.location.pathname).toBe(`/channels/@me/${DM}`);
    expect(screen.getByRole("heading", { level: 1, name: "Аня" })).toBeTruthy();
    const history = await screen.findByRole("list", { name: "Сообщения с @Аня" });
    expect(await within(history).findByText("сообщение 12")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Сообщение для @Аня" })).toBeTruthy();
    await waitFor(() => {
      expect(acks).toEqual([`${DM}:12`]);
    });
    expect(within(await list()).getByRole("link", { name: "Аня" })).toBeTruthy();
  });

  it("counts a new message from someone else in the list", async () => {
    const send = signedIn();
    renderApp("/channels/@me");
    await list();
    send("MESSAGE_CREATE", dmMessage("20", GROUP, boris));
    expect(await within(await list()).findByRole("link", { name: "Пятница, непрочитанных: 1" })).toBeTruthy();
  });

  it("writes to a friend from the friends screen", async () => {
    signedIn();
    const opened: unknown[] = [];
    server.use(
      apiHandler("post", "/users/@me/channels", async ({ request }) => {
        opened.push(await request.json());
        return HttpResponse.json(makeDMChannel("902", [vera]), { status: 201 });
      }),
    );
    const { user, router } = renderApp("/channels/@me");
    await user.click(await screen.findByRole("tab", { name: "Все" }));
    await user.click(screen.getByRole("button", { name: "Написать Вера" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me/902");
    });
    expect(opened).toEqual([{ recipient_id: vera.id }]);
    expect(await screen.findByRole("heading", { level: 1, name: "Вера" })).toBeTruthy();
  });

  it("makes a group of the friends picked by the plus", async () => {
    signedIn();
    let body: unknown;
    server.use(
      apiHandler("post", "/users/@me/channels", async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(makeDMChannel("903", [boris, vera], { owner_id: testUser.id }), { status: 201 });
      }),
    );
    const { user, router } = renderApp("/channels/@me");
    await user.click(within(await list()).getByRole("button", { name: "Создать личное сообщение" }));
    const dialog = await screen.findByRole("dialog", { name: "Выберите друзей" });
    await user.click(within(dialog).getByRole("checkbox", { name: /Борис/ }));
    await user.click(within(dialog).getByRole("checkbox", { name: /Вера/ }));
    await user.click(within(dialog).getByRole("button", { name: "Создать группу" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me/903");
    });
    expect(body).toEqual({ recipient_ids: [boris.id, vera.id] });
    expect(await screen.findByRole("heading", { level: 1, name: /Борис, Вера/ })).toBeTruthy();
  });

  it("leaves a group after asking, and goes home if it was open", async () => {
    const send = signedIn();
    const removed: string[] = [];
    server.use(
      apiHandler("delete", "/channels/{channel_id}/recipients/{user_id}", ({ params }) => {
        removed.push(`${params.channel_id}:${params.user_id}`);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, router } = renderApp(`/channels/@me/${GROUP}`);
    await user.click(within(await list()).getByRole("button", { name: "Покинуть группу Пятница" }));
    const ask = await screen.findByRole("alertdialog", { name: "Покинуть группу «Пятница»?" });
    await user.click(within(ask).getByRole("button", { name: "Покинуть группу" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
    expect(removed).toEqual([`${GROUP}:${testUser.id}`]);
    send("DM_CHANNEL_DELETE", { id: GROUP });
    await waitFor(() => {
      expect(within(screen.getByRole("region", { name: "Личные сообщения" })).queryByText("Пятница")).toBeNull();
    });
  });

  it("shows a group's people with its owner, and folds a blocked author's message", async () => {
    signedIn([boris.id]);
    const { user } = renderApp(`/channels/@me/${GROUP}`);
    const history = await screen.findByRole("list", { name: "Сообщения в группе Пятница" });
    const show = await within(history).findByRole("button", { name: "Показать" });
    expect(within(history).queryByText("в пятницу?")).toBeNull();
    await user.click(show);
    expect(within(history).getByText("в пятницу?")).toBeTruthy();
    const toggle = screen.queryByRole("button", { name: "Показать участников" });
    if (toggle) await user.click(toggle);
    const people = await screen.findByRole("region", { name: "Участники группы" });
    expect(within(people).getByRole("img", { name: "Создатель группы" })).toBeTruthy();
    expect(within(people).getByRole("button", { name: "Исключить Аня из группы" })).toBeTruthy();
  });
});
