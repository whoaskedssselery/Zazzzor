import { screen, within } from "@testing-library/react";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import {
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeReady,
  makeRole,
  makeUser,
  renderUi,
} from "@/shared/test";
import { MemberList } from "./MemberList";

const G = "10";
const ADMIN = 0xd9944a;
const CINEMA = 0x5fa8d3;
const GREEN = 0x3ba55c;

const anya = makeUser("1", { display_name: "Аня" });
const katya = makeUser("2", { display_name: "Катя" });
const dima = makeUser("3", { display_name: "Дмитрий" });
const maxim = makeUser("4", { display_name: "Максим" });
const olya = makeUser("5", { display_name: "Оля" });
const seryozha = makeUser("6", { display_name: "Серёжа" });

beforeEach(() => {
  const mine = makeMember(maxim);
  applyGatewayDispatch("READY", {
    ...makeReady(maxim, [
      makeGuildState(G, maxim, {
        guild: makeGuild(G, anya.id),
        roles: [
          makeEveryone(G),
          makeRole("50", { name: "Админ", position: 3, color: ADMIN, hoist: true }),
          makeRole("51", { name: "Киноклуб", position: 2, color: CINEMA, hoist: true }),
          makeRole("52", { name: "Зелёные", position: 1, color: GREEN }),
          makeRole("53", { name: "Бесцветная", position: 4, color: 0 }),
        ],
        members: [
          makeMember(anya, { roles: ["50"] }),
          makeMember(katya, { roles: ["51", "50", "53"] }),
          makeMember(dima, { roles: ["51"], nick: "Димон" }),
          mine,
          makeMember(olya, { roles: ["52"] }),
          makeMember(seryozha, { roles: ["51"] }),
        ],
        my_member: mine,
        presences: ["1", "2", "3", "4", "5"].map((id) => ({ user_id: id, status: "online" as const })),
      }),
    ]),
  });
});

afterEach(() => {
  resetGuilds();
});

const groups = () =>
  within(screen.getByRole("complementary", { name: "Участники" }))
    .getAllByRole("list")
    .map((ul) => [
      document.getElementById(ul.getAttribute("aria-labelledby") ?? "")?.textContent,
      within(ul)
        .getAllByRole("listitem")
        .map((li) => li.querySelector("[data-member-name]")?.textContent),
    ]);

const nameEl = (name: string) => screen.getByText(name, { selector: "[data-member-name]" });

describe("MemberList", () => {
  it("groups hoisted roles by position, then «В сети», then «Не в сети»", () => {
    renderUi(<MemberList guildId={G} />);
    expect(groups()).toEqual([
      ["Админ — 2", ["Аня", "Катя"]],
      ["Киноклуб — 1", ["Димон"]],
      ["В сети — 2", ["Максим", "Оля"]],
      ["Не в сети — 1", ["Серёжа"]],
    ]);
    expect(screen.getByRole("heading", { name: "Админ — 2" })).toBeTruthy();
  });

  it("colours a name by the highest role that has a colour", () => {
    renderUi(<MemberList guildId={G} />);
    expect(nameEl("Катя").style.getPropertyValue("--role")).toBe("#d9944a");
    expect(nameEl("Димон").style.getPropertyValue("--role")).toBe("#5fa8d3");
    expect(nameEl("Оля").style.getPropertyValue("--role")).toBe("#3ba55c");
    expect(nameEl("Максим").style.getPropertyValue("--role")).toBe("");
  });

  it("shows the nickname, or else the display name", () => {
    renderUi(<MemberList guildId={G} />);
    expect(screen.getByText("Димон", { selector: "[data-member-name]" })).toBeTruthy();
    expect(screen.queryByText("Дмитрий")).toBeNull();
  });

  it("tells the presence of each member to assistive tech", () => {
    renderUi(<MemberList guildId={G} />);
    expect(screen.getByRole("img", { name: "Аня, в сети" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "Серёжа, не в сети" })).toBeTruthy();
  });
});
