import { screen, waitFor, within } from "@testing-library/react";
import type { WebSocketHandlerConnection } from "msw";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { resetChannelUi } from "@/entities/channel";
import { NO_SUCH_USER, resetGuilds } from "@/entities/guild";
import {
  apiHandler,
  dispatchFrame,
  makeReady,
  makeRelationship,
  makeUser,
  problem,
  readyGateway,
  server,
} from "@/shared/test";

type Client = WebSocketHandlerConnection["client"];

const anya = makeUser("2", { display_name: "Аня", username: "anya" });
const boris = makeUser("3", { display_name: "Борис", username: "boris" });
const vera = makeUser("4", { display_name: "Вера", username: "vera" });

/** Signs in with these relationships and online friends; hands back a way to send later events. */
const signedIn = (relationships = [makeRelationship(anya, 1), makeRelationship(boris, 3)], online = [anya.id]) => {
  let client: Client | undefined;
  server.use(
    sessionHandler(testUser),
    readyGateway(
      {
        ...makeReady(testUser),
        relationships,
        presences: online.map((id) => ({ user_id: id, status: "online" as const, custom_status: null })),
      },
      (c) => {
        client = c;
      },
    ),
  );
  let seq = 1;
  return (t: string, d: unknown) => {
    if (!client) throw new Error("the gateway is not connected");
    seq += 1;
    client.send(dispatchFrame(t, seq, d));
  };
};

afterEach(() => {
  resetChannelUi();
  resetGuilds();
});

describe("friends", () => {
  it("shows online friends first and the waiting requests on the home column and the tab", async () => {
    signedIn();
    renderApp("/channels/@me");
    const online = await screen.findByRole("region", { name: "В сети" });
    expect(within(online).getByText("Аня")).toBeTruthy();
    expect(screen.getByLabelText("Входящих заявок: 1")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Ожидание, входящих заявок: 1" })).toBeTruthy();
  });

  it("accepts a request, and the friend moves over when the server says so", async () => {
    const send = signedIn();
    const accepted: string[] = [];
    server.use(
      apiHandler("put", "/users/@me/relationships/{user_id}", async ({ params, request }) => {
        const body = await request.json();
        accepted.push(`${params.user_id}:${String(body.type)}`);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp("/channels/@me");
    await user.click(await screen.findByRole("tab", { name: /^Ожидание/ }));
    await user.click(screen.getByRole("button", { name: "Принять заявку от Борис" }));
    await waitFor(() => {
      expect(accepted).toEqual([`${boris.id}:1`]);
    });
    send("RELATIONSHIP_ADD", makeRelationship(boris, 1));
    await user.click(screen.getByRole("tab", { name: "Все" }));
    const all = await screen.findByRole("region", { name: "Все друзья" });
    expect(within(all).getByText("Борис")).toBeTruthy();
    expect(screen.queryByLabelText(/Входящих заявок/)).toBeNull();
  });

  it("sends a request by username and says when there is nobody by that name", async () => {
    signedIn([]);
    const asked: string[] = [];
    server.use(
      apiHandler("post", "/users/@me/relationships", async ({ request }) => {
        const { username } = await request.json();
        asked.push(username);
        return username === "vera" ? new HttpResponse(null, { status: 204 }) : problem(404, "not_found");
      }),
    );
    const { user } = renderApp("/channels/@me");
    await user.click(await screen.findByRole("tab", { name: "Добавить в друзья" }));
    const field = screen.getByRole("textbox", { name: "Имя пользователя" });
    await user.type(field, "nobody{Enter}");
    expect(await screen.findByText(NO_SUCH_USER)).toBeTruthy();
    await user.clear(field);
    await user.type(field, `@${vera.username}{Enter}`);
    expect(await screen.findByText("Заявка для vera отправлена.")).toBeTruthy();
    expect(asked).toEqual(["nobody", "vera"]);
  });

  it("removes a friend after asking", async () => {
    signedIn();
    const removed: string[] = [];
    server.use(
      apiHandler("delete", "/users/@me/relationships/{user_id}", ({ params }) => {
        removed.push(params.user_id);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp("/channels/@me");
    await user.click(await screen.findByRole("button", { name: "Действия с Аня" }));
    await user.click(await screen.findByRole("menuitem", { name: "Удалить из друзей" }));
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Удалить из друзей" }));
    await waitFor(() => {
      expect(removed).toEqual([anya.id]);
    });
  });
});
