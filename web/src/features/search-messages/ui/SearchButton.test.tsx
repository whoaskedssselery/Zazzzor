import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import { useFocusRequest } from "@/entities/message";
import {
  apiHandler,
  makeChannel,
  makeGuildState,
  makeMember,
  makeMessage,
  makeReady,
  makeUser,
  renderRouted,
  server,
} from "@/shared/test";
import { SearchButton } from "./SearchButton";

const G = "10";
const GENERAL = "101";
const me = makeUser("1", { display_name: "Аня" });
const petr = makeUser("2", { display_name: "Пётр" });

beforeEach(() => {
  applyGatewayDispatch(
    "READY",
    makeReady(me, [
      makeGuildState(G, me, {
        channels: [makeChannel(GENERAL, G, { name: "общий" })],
        members: [makeMember(me), makeMember(petr, { nick: "Петя" })],
      }),
    ]),
  );
});

afterEach(() => {
  resetGuilds();
});

let focused: string | null = null;
const Probe = () => {
  focused = useFocusRequest(GENERAL);
  return null;
};

describe("SearchButton", () => {
  it("searches the server and opens a result at its message", async () => {
    const asked: URLSearchParams[] = [];
    server.use(
      apiHandler("get", "/guilds/{guild_id}/messages/search", ({ request }) => {
        asked.push(new URL(request.url).searchParams);
        return HttpResponse.json({
          messages: [makeMessage("500", GENERAL, petr, { content: "Пойдём в **кино**" })],
          total_results: 1,
        });
      }),
    );
    const { user, router } = await renderRouted(
      <>
        <SearchButton scope={{ kind: "guild", guildId: G }} placeholder="Искать на сервере" />
        <Probe />
      </>,
      { path: "/channels/10/999" },
    );
    await user.click(screen.getByRole("button", { name: "Поиск по сообщениям" }));
    await user.type(screen.getByRole("searchbox", { name: "Что искать" }), "кин{Enter}");

    const list = await screen.findByRole("list", { name: "Результаты поиска" });
    expect(screen.getByText("Найдено: 1")).toBeTruthy();
    const hit = within(list).getByRole("button");
    expect(hit.textContent).toContain("Петя");
    expect(hit.textContent).toContain("#общий");
    expect(hit.textContent).toContain("Пойдём в кино");
    expect(asked[0]?.get("content")).toBe("кин");

    await user.click(hit);
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/channels/${G}/${GENERAL}`);
    });
    expect(focused).toBe("500");
  });

  it("does not search without a word or an author", async () => {
    const { user } = await renderRouted(
      <SearchButton scope={{ kind: "guild", guildId: G }} placeholder="Искать на сервере" />,
    );
    await user.click(screen.getByRole("button", { name: "Поиск по сообщениям" }));
    await user.type(screen.getByRole("searchbox", { name: "Что искать" }), "!!");
    expect(screen.getByRole("button", { name: "Найти" })).toHaveProperty("disabled", true);
  });
});
