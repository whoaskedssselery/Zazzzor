import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import type { ReactElement } from "react";
import { inOrder, type MessagePages, messagesKey } from "@/entities/message";
import { apiHandler, makeMessage, makeUser, problem, server } from "@/shared/test";
import { UiProviders } from "@/shared/ui";
import { useDeleteMessage } from "../model/mutations";
import { type MessageActionHandlers, MessageContextMenu, MessageToolbar } from "./MessageActions";
import { MessageEditor } from "./MessageEditor";

const me = makeUser("100");
const C = "101";
const message = makeMessage("5", C, me, { content: "исходный текст" });

const renderWith = (ui: ReactElement, history?: MessagePages) => {
  const qc = new QueryClient();
  if (history) qc.setQueryData(messagesKey(C), history);
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={qc}>
      <UiProviders>{ui}</UiProviders>
    </QueryClientProvider>,
  );
  return { user, qc };
};

const handlers = (over: Partial<MessageActionHandlers> = {}): MessageActionHandlers => ({
  message,
  canEdit: true,
  canDelete: true,
  canReply: true,
  onReply: vi.fn(),
  onEdit: vi.fn(),
  onDelete: vi.fn(),
  ...over,
});

describe("MessageToolbar", () => {
  it.each([
    ["the author", { canEdit: true, canDelete: true }, ["Ответить", "Изменить", "Удалить", "Ещё"]],
    ["a moderator", { canEdit: false, canDelete: true }, ["Ответить", "Удалить", "Ещё"]],
    ["anyone else", { canEdit: false, canDelete: false }, ["Ответить", "Ещё"]],
  ])("offers %s what they may do", (_, flags, labels) => {
    renderWith(<MessageToolbar {...handlers(flags)} />);
    expect(screen.getAllByRole("button").map((b) => b.getAttribute("aria-label"))).toEqual(labels);
  });

  it("asks before deleting, and Shift+click skips the question", () => {
    const h = handlers();
    renderWith(<MessageToolbar {...h} />);
    const remove = screen.getByRole("button", { name: "Удалить" });
    fireEvent.click(remove);
    fireEvent.click(remove, { shiftKey: true });
    expect(h.onDelete).toHaveBeenNthCalledWith(1, false);
    expect(h.onDelete).toHaveBeenNthCalledWith(2, true);
  });

  it("puts the copy actions under «Ещё»", async () => {
    const { user } = renderWith(<MessageToolbar {...handlers()} />);
    await user.click(screen.getByRole("button", { name: "Ещё" }));
    const menu = await screen.findByRole("menu");
    await user.click(within(menu).getByRole("menuitem", { name: "Копировать ID" }));
    expect(await navigator.clipboard.readText()).toBe("5");
    expect(await screen.findByText("ID скопирован")).toBeTruthy();
  });
});

describe("MessageContextMenu", () => {
  it("offers the same actions on right click", async () => {
    const h = handlers({ canEdit: false });
    const { user } = renderWith(
      <MessageContextMenu {...h}>
        <p>сообщение</p>
      </MessageContextMenu>,
    );
    fireEvent.contextMenu(screen.getByText("сообщение"));
    const menu = await screen.findByRole("menu");
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((i) => i.textContent),
    ).toEqual(["Ответить", "Копировать текст", "Копировать ID", "Удалить"]);
    await user.click(within(menu).getByRole("menuitem", { name: "Копировать текст" }));
    expect(await navigator.clipboard.readText()).toBe("исходный текст");
  });
});

describe("MessageEditor", () => {
  it("saves a changed text on Enter", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("patch", "/channels/{channel_id}/messages/{message_id}", async ({ request }) => {
        const body = await request.json();
        bodies.push(body);
        return HttpResponse.json({ ...message, content: body.content, edited_at: "2026-10-23T18:10:00Z" });
      }),
    );
    const onDone = vi.fn();
    const { user, qc } = renderWith(<MessageEditor message={message} onDone={onDone} />, {
      pages: [[message]],
      pageParams: [undefined],
    });
    const field = screen.getByRole("textbox", { name: "Изменить сообщение" });
    expect(document.activeElement).toBe(field);
    await user.keyboard(" и правка{Enter}");
    expect(onDone).toHaveBeenCalledTimes(1);
    await waitFor(() => {
      expect(bodies).toEqual([{ content: "исходный текст и правка" }]);
    });
    await waitFor(() => {
      expect(inOrder(qc.getQueryData<MessagePages>(messagesKey(C)) ?? { pages: [], pageParams: [] })[0]?.content).toBe(
        "исходный текст и правка",
      );
    });
  });

  it("closes without a request on Esc and for the same text", async () => {
    let patches = 0;
    server.use(
      apiHandler("patch", "/channels/{channel_id}/messages/{message_id}", () => {
        patches += 1;
        return HttpResponse.json(message);
      }),
    );
    const onDone = vi.fn();
    const { user } = renderWith(<MessageEditor message={message} onDone={onDone} />);
    await user.keyboard("{Escape}");
    await user.keyboard("{Enter}");
    expect(onDone).toHaveBeenCalledTimes(2);
    expect(patches).toBe(0);
  });

  it("will not save an empty text", async () => {
    const onDone = vi.fn();
    const { user } = renderWith(<MessageEditor message={message} onDone={onDone} />);
    await user.clear(screen.getByRole("textbox", { name: "Изменить сообщение" }));
    await user.keyboard("{Enter}");
    expect(onDone).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "сохранить" })).toHaveProperty("disabled", true);
  });
});

describe("useDeleteMessage", () => {
  const Delete = () => {
    const del = useDeleteMessage();
    return (
      <button
        type="button"
        onClick={() => {
          del.mutate(message);
        }}
      >
        удалить
      </button>
    );
  };

  it.each([
    ["deleted now", 204],
    ["already gone", 404],
  ])("takes the message out of the history when it is %s", async (_, status) => {
    server.use(
      apiHandler("delete", "/channels/{channel_id}/messages/{message_id}", () =>
        status === 204 ? new HttpResponse(null, { status: 204 }) : problem(404, "not_found"),
      ),
    );
    const { user, qc } = renderWith(<Delete />, { pages: [[message]], pageParams: [undefined] });
    await user.click(screen.getByRole("button", { name: "удалить" }));
    await waitFor(() => {
      expect(qc.getQueryData<MessagePages>(messagesKey(C))?.pages[0]).toEqual([]);
    });
  });

  it("says why it could not", async () => {
    server.use(
      apiHandler("delete", "/channels/{channel_id}/messages/{message_id}", () => problem(403, "missing_permission")),
    );
    const { user } = renderWith(<Delete />, { pages: [[message]], pageParams: [undefined] });
    await user.click(screen.getByRole("button", { name: "удалить" }));
    expect(await screen.findByText("Недостаточно прав для этого действия.")).toBeTruthy();
  });
});
