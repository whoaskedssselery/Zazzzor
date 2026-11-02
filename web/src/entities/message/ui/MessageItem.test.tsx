import { screen, within } from "@testing-library/react";
import { makeMessage, makeUser, renderUi } from "@/shared/test";
import { type AuthorView, DELETED_USER, MessageItem, REPLY_DELETED } from "./MessageItem";

const anna = makeUser("1", { display_name: "Аня" });
const view: AuthorView = { name: "Аня", color: 0xd9944a, avatarUrl: null };
const now = new Date("2026-10-23T22:00:00");
const cp = (...codes: number[]) => String.fromCodePoint(...codes);

describe("MessageItem", () => {
  it("heads a group with the name in its role colour and the time", () => {
    const m = makeMessage("5", "101", anna, { created_at: new Date("2026-10-23T21:04:00").toISOString() });
    renderUi(<MessageItem message={m} author={view} replyAuthor={null} grouped={false} now={now} />);
    const heading = screen.getByRole("heading", { level: 3 });
    expect(within(heading).getByText("Аня").tagName).toBe("BDI");
    expect(within(heading).getByText("Сегодня в 21:04")).toBeTruthy();
  });

  it("shows a continued message without the name", () => {
    const m = makeMessage("5", "101", anna);
    renderUi(<MessageItem message={m} author={view} replyAuthor={null} grouped now={now} />);
    expect(screen.queryByRole("heading")).toBeNull();
    expect(screen.getByText("сообщение 5")).toBeTruthy();
  });

  it("names a deleted account «Удалённый пользователь»", () => {
    renderUi(
      <MessageItem
        message={makeMessage("5", "101", null)}
        author={null}
        replyAuthor={null}
        grouped={false}
        now={now}
      />,
    );
    expect(screen.getByText(DELETED_USER)).toBeTruthy();
  });

  it("marks an edited message and tells when it was edited", async () => {
    const m = makeMessage("5", "101", anna, { edited_at: new Date("2026-10-23T21:30:00").toISOString() });
    const { user } = renderUi(<MessageItem message={m} author={view} replyAuthor={null} grouped now={now} />);
    await user.hover(screen.getByText("(изменено)"));
    expect((await screen.findAllByText("Сегодня в 21:30")).length).toBeGreaterThan(0);
  });

  it("previews the answered message and jumps to it", async () => {
    const jump = vi.fn();
    const m = makeMessage("6", "101", anna, {
      reply_to_id: "5",
      referenced_message: { id: "5", author: anna, content: "вопрос" },
    });
    const { user } = renderUi(
      <MessageItem message={m} author={view} replyAuthor={view} grouped={false} now={now} onJumpToReply={jump} />,
    );
    await user.click(screen.getByRole("button", { name: "Перейти к исходному сообщению" }));
    expect(jump).toHaveBeenCalledTimes(1);
    expect(screen.getByText("вопрос")).toBeTruthy();
  });

  it("says the answered message is gone when it was deleted", () => {
    const m = makeMessage("6", "101", anna, { reply_to_id: "5", referenced_message: null });
    renderUi(<MessageItem message={m} author={view} replyAuthor={null} grouped={false} now={now} />);
    expect(screen.getByText(REPLY_DELETED)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Перейти к исходному сообщению" })).toBeNull();
  });

  it("keeps a right-to-left override in a name inside its <bdi>", () => {
    const name = `${cp(0x202e)}яна`;
    renderUi(
      <MessageItem
        message={makeMessage("5", "101", anna)}
        author={{ ...view, name }}
        replyAuthor={null}
        grouped={false}
        now={now}
      />,
    );
    expect(screen.getByText(name).tagName).toBe("BDI");
  });

  it("wraps hostile content as text: HTML, a long word and 4000 emoji", () => {
    const content = `<img src=x onerror=alert(1)> ${"a".repeat(4000)} ${"😀".repeat(4000)}`;
    renderUi(
      <MessageItem
        message={makeMessage("5", "101", anna, { content })}
        author={view}
        replyAuthor={null}
        grouped
        now={now}
      />,
    );
    expect(document.querySelector("img[src='x']")).toBeNull();
    expect(screen.getByText(content).className).toContain("[overflow-wrap:anywhere]");
  });
});
