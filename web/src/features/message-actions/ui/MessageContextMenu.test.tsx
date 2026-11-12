import { fireEvent, screen, within } from "@testing-library/react";
import { makeMessage, makeUser, renderUi, setTouchScreen } from "@/shared/test";
import { DEFAULT_RECENT } from "../model/emoji";
import { MessageContextMenu } from "./MessageActions";

const handlers = (onReact?: (emoji: string) => void) => ({
  message: makeMessage("1", "101", makeUser("2")),
  canEdit: false,
  canDelete: false,
  canReply: true,
  onReply: () => {},
  onEdit: () => {},
  onDelete: () => {},
  onReact,
});

describe("MessageContextMenu", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("starts with the recent reactions, so a long press on a phone can react", async () => {
    const reacted: string[] = [];
    const { user } = renderUi(
      <MessageContextMenu {...handlers((e) => reacted.push(e))}>
        <div>сообщение</div>
      </MessageContextMenu>,
    );
    fireEvent.contextMenu(screen.getByText("сообщение"));
    const quick = await screen.findAllByRole("menuitem", { name: /^Реакция / });
    expect(quick).toHaveLength(6);
    await user.click(screen.getByRole("menuitem", { name: `Реакция ${DEFAULT_RECENT[1] ?? ""}` }));
    expect(reacted).toEqual([DEFAULT_RECENT[1]]);
  });

  it("opens the picker from «Ещё реакции»", async () => {
    const { user } = renderUi(
      <MessageContextMenu {...handlers(() => {})}>
        <div>сообщение</div>
      </MessageContextMenu>,
    );
    fireEvent.contextMenu(screen.getByText("сообщение"));
    await user.click(await screen.findByRole("menuitem", { name: "Ещё реакции" }));
    expect(await screen.findByRole("dialog", { name: "Выбор реакции" })).toBeTruthy();
  });

  it("offers no reactions without ADD_REACTIONS", async () => {
    renderUi(
      <MessageContextMenu {...handlers()}>
        <div>сообщение</div>
      </MessageContextMenu>,
    );
    fireEvent.contextMenu(screen.getByText("сообщение"));
    await screen.findByRole("menuitem", { name: "Ответить" });
    expect(screen.queryByRole("menuitem", { name: "Ещё реакции" })).toBeNull();
  });
});

describe("MessageContextMenu on a touch screen", () => {
  beforeEach(() => {
    localStorage.clear();
    setTouchScreen();
  });

  it("opens the actions as a sheet from the bottom on a long press, reactions first", async () => {
    const reacted: string[] = [];
    let replied = false;
    const { user } = renderUi(
      <MessageContextMenu
        {...handlers((e) => reacted.push(e))}
        onReply={() => {
          replied = true;
        }}
      >
        <div>сообщение</div>
      </MessageContextMenu>,
    );
    fireEvent.contextMenu(screen.getByText("сообщение"));
    const sheet = await screen.findByRole("dialog", { name: "Действия с сообщением" });
    expect(screen.queryByRole("menu")).toBeNull();
    await user.click(within(sheet).getByRole("button", { name: `Реакция ${DEFAULT_RECENT[0] ?? ""}` }));
    expect(reacted).toEqual([DEFAULT_RECENT[0]]);
    fireEvent.contextMenu(screen.getByText("сообщение"));
    await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Ответить" }));
    expect(replied).toBe(true);
  });

  it("leaves a link's own long press to the browser", () => {
    renderUi(
      <MessageContextMenu {...handlers(() => {})}>
        <div>
          <a href="https://example.org">ссылка</a>
        </div>
      </MessageContextMenu>,
    );
    fireEvent.contextMenu(screen.getByText("ссылка"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
