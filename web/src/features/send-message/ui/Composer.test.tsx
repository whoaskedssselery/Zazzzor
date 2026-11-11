import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { useState } from "react";
import { resetGuilds, setConnectionStatus } from "@/entities/guild";
import { resetPending, usePendingMessages } from "@/entities/message";
import { apiHandler, makeMessage, makeUser, server } from "@/shared/test";
import { UiProviders } from "@/shared/ui";
import { resetDrafts } from "../model/drafts";
import { resetMentions } from "../model/mentions";
import { NO_ATTACH_PERMISSION, resetUploads } from "../model/uploads";
import { Composer, type ComposerProps, NO_SEND_PERMISSION, type ReplyTarget } from "./Composer";

const me = makeUser("100");

/** Counts the sends and typing notices that reach the server. */
const serverCounts = () => {
  const counts = { sends: [] as string[], typing: 0 };
  server.use(
    apiHandler("post", "/channels/{channel_id}/messages", async ({ request, params }) => {
      const body = await request.json();
      counts.sends.push(body.content ?? "");
      return HttpResponse.json(makeMessage(String(counts.sends.length), params.channel_id, me, body), { status: 201 });
    }),
    apiHandler("post", "/channels/{channel_id}/typing", () => {
      counts.typing += 1;
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return counts;
};

const Pending = ({ channelId }: { channelId: string }) => (
  <ul aria-label="Отправляются">
    {usePendingMessages(channelId).map((p) => (
      <li key={p.nonce}>{p.content}</li>
    ))}
  </ul>
);

const renderComposer = (props: Partial<ComposerProps> = {}) => {
  const qc = new QueryClient();
  const user = userEvent.setup();
  const all: ComposerProps = {
    channelId: "101",
    channelName: "общий",
    canSend: true,
    canAttach: true,
    replyTo: null,
    onCancelReply: () => {},
    onEditLast: () => false,
    ...props,
  };
  const view = render(
    <QueryClientProvider client={qc}>
      <UiProviders>
        <Composer {...all} />
        <Pending channelId={all.channelId} />
      </UiProviders>
    </QueryClientProvider>,
  );
  const field = () => screen.getByRole<HTMLTextAreaElement>("textbox", { name: `Сообщение в #${all.channelName}` });
  return { user, field, ...view, props: all };
};

/** A composer answering message 7 by Аня, whose reply state lives where the chat keeps it. */
const Replying = () => {
  const [target, setTarget] = useState<ReplyTarget | null>({
    message: { id: "7", author: me, content: "вопрос" },
    name: "Аня",
  });
  return (
    <Composer
      channelId="101"
      channelName="общий"
      canSend
      canAttach
      replyTo={target}
      onCancelReply={() => {
        setTarget(null);
      }}
      onEditLast={() => false}
    />
  );
};

const renderReplying = () => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UiProviders>
        <Replying />
      </UiProviders>
    </QueryClientProvider>,
  );
  return user;
};

beforeEach(() => {
  setConnectionStatus("ready");
});

afterEach(() => {
  resetMentions();
  resetUploads();
  resetDrafts();
  resetPending();
  resetGuilds();
});

describe("Composer", () => {
  it("sends on Enter and breaks the line on Shift+Enter", async () => {
    const counts = serverCounts();
    const { user, field } = renderComposer();
    await user.type(field(), "первая{Shift>}{Enter}{/Shift}вторая");
    expect(field().value).toBe("первая\nвторая");
    await user.keyboard("{Enter}");
    expect(field().value).toBe("");
    await waitFor(() => {
      expect(counts.sends).toEqual(["первая\nвторая"]);
    });
  });

  it("sends nothing empty or made of spaces", async () => {
    const counts = serverCounts();
    const { user, field } = renderComposer();
    await user.type(field(), "   {Enter}");
    await user.click(screen.getByRole("button", { name: "Отправить" }));
    expect(counts.sends).toEqual([]);
    expect(screen.getByRole("button", { name: "Отправить" })).toHaveProperty("disabled", true);
  });

  it("counts down from 3800 characters and blocks the send past 4000", async () => {
    const counts = serverCounts();
    const { user, field } = renderComposer();
    fireEvent.change(field(), { target: { value: "я".repeat(3800) } });
    expect(screen.queryByLabelText("Осталось символов")).toBeNull();
    fireEvent.change(field(), { target: { value: "😀".repeat(3801) } });
    expect(screen.getByLabelText("Осталось символов").textContent).toBe("199");
    fireEvent.change(field(), { target: { value: "я".repeat(4001) } });
    const counter = screen.getByLabelText("Осталось символов");
    expect(counter.textContent).toBe("-1");
    expect(counter.className).toContain("text-fg-danger");
    await user.type(field(), "{Enter}");
    expect(screen.getByRole("button", { name: "Отправить" })).toHaveProperty("disabled", true);
    expect(counts.sends).toEqual([]);
  });

  it("sends one message for a double Enter and for a double click", async () => {
    const counts = serverCounts();
    const { user, field } = renderComposer();
    await user.type(field(), "раз");
    // Both keydowns land before React renders the cleared field.
    fireEvent.keyDown(field(), { key: "Enter" });
    fireEvent.keyDown(field(), { key: "Enter" });
    await user.type(field(), "два");
    const button = screen.getByRole("button", { name: "Отправить" });
    fireEvent.click(button);
    fireEvent.click(button);
    await waitFor(() => {
      expect(counts.sends).toEqual(["раз", "два"]);
    });
  });

  it("edits the last own message on ↑ in the empty field", async () => {
    const onEditLast = vi.fn(() => true);
    const { user, field } = renderComposer({ onEditLast });
    await user.click(field());
    await user.keyboard("{ArrowUp}");
    expect(onEditLast).toHaveBeenCalledTimes(1);
    await user.type(field(), "текст{ArrowUp}");
    expect(onEditLast).toHaveBeenCalledTimes(1);
  });

  it("answers the message being replied to, then drops the reply bar", async () => {
    const bodies: unknown[] = [];
    server.use(
      apiHandler("post", "/channels/{channel_id}/messages", async ({ request }) => {
        const body = await request.json();
        bodies.push(body);
        return HttpResponse.json(makeMessage("8", "101", me, { content: body.content }), { status: 201 });
      }),
    );
    const user = renderReplying();
    expect(screen.getByText("Аня")).toBeTruthy();
    await user.type(screen.getByRole("textbox"), "ответ{Enter}");
    await waitFor(() => {
      expect(bodies).toMatchObject([{ content: "ответ", reply_to_id: "7" }]);
    });
    expect(screen.queryByText("Аня")).toBeNull();
  });

  it("cancels the reply on Esc", async () => {
    const user = renderReplying();
    await user.type(screen.getByRole("textbox"), "{Escape}");
    expect(screen.queryByText("Аня")).toBeNull();
  });

  it("gives way to a notice without SEND_MESSAGES", () => {
    renderComposer({ canSend: false });
    expect(screen.getByText(NO_SEND_PERMISSION)).toBeTruthy();
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("keeps each channel's draft across a switch and back", async () => {
    const { user, field, rerender, props } = renderComposer();
    await user.type(field(), "черновик");
    const at = (channelId: string, channelName: string) => (
      <QueryClientProvider client={new QueryClient()}>
        <UiProviders>
          <Composer {...props} channelId={channelId} channelName={channelName} />
        </UiProviders>
      </QueryClientProvider>
    );
    rerender(at("102", "мемы"));
    expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "Сообщение в #мемы" }).value).toBe("");
    rerender(at("101", "общий"));
    expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "Сообщение в #общий" }).value).toBe("черновик");
  });

  it("tells the channel once while the user keeps typing", async () => {
    const counts = serverCounts();
    const { user, field } = renderComposer();
    await user.type(field(), "долго печатаю");
    await waitFor(() => {
      expect(counts.typing).toBe(1);
    });
  });
});

describe("Composer with files", () => {
  const shot = () => new File([new Uint8Array(4)], "shot.png", { type: "image/png" });

  /** Uploads answer when `release` is called; sends record their bodies. */
  const filesServer = () => {
    const sent: unknown[] = [];
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    server.use(
      http.post("/api/v1/channels/:channel_id/attachments", async () => {
        await gate;
        return HttpResponse.json(
          {
            id: "900",
            filename: "shot.png",
            content_type: "image/png",
            size: 4,
            width: 2,
            height: 1,
            url: "/api/v1/attachments/101/900/shot.png",
            pending: true,
          },
          { status: 201 },
        );
      }),
      apiHandler("post", "/channels/{channel_id}/messages", async ({ request, params }) => {
        const body = await request.json();
        sent.push(body);
        return HttpResponse.json(makeMessage("1", params.channel_id, me, body), { status: 201 });
      }),
    );
    return {
      sent,
      release: () => {
        release();
      },
    };
  };

  it("takes a pasted screenshot, waits for its upload, then sends it without text", async () => {
    const { sent, release } = filesServer();
    const { user, field } = renderComposer();
    fireEvent.paste(field(), { clipboardData: { files: [shot()], getData: () => "" } });
    expect(await screen.findByRole("listitem", { name: "shot.png" })).toBeTruthy();
    const send = screen.getByRole("button", { name: "Файлы ещё загружаются" });
    expect(send).toHaveProperty("disabled", true);
    release();
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Отправить" })).toHaveProperty("disabled", false);
    });
    await user.click(screen.getByRole("button", { name: "Отправить" }));
    expect(screen.queryByRole("listitem", { name: "shot.png" })).toBeNull();
    await waitFor(() => {
      expect(sent).toHaveLength(1);
    });
    expect(sent[0]).toMatchObject({ content: "", reply_to_id: null, attachment_ids: ["900"] });
    await waitFor(() => {
      expect(sent).toHaveLength(1);
    });
  });

  it("has no file button without ATTACH_FILES and refuses a pasted file", async () => {
    filesServer();
    const { field } = renderComposer({ canAttach: false });
    expect(screen.queryByRole("button", { name: "Прикрепить файлы" })).toBeNull();
    fireEvent.paste(field(), { clipboardData: { files: [shot()], getData: () => "" } });
    expect(await screen.findByText(NO_ATTACH_PERMISSION)).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Прикреплённые файлы" })).toBeNull();
  });
});

describe("Composer mentions", () => {
  const people = () => [
    { kind: "user" as const, id: "1", name: "Аня", detail: "anya" },
    { kind: "user" as const, id: "2", name: "Маня", detail: "manya" },
    { kind: "everyone" as const, id: null, name: "everyone", detail: "Все" },
  ];

  it("suggests on @, picks with Enter and sends the server's token", async () => {
    const counts = serverCounts();
    const { user, field } = renderComposer({ mentionables: people });
    await user.type(field(), "привет @ан");
    const list = screen.getByRole("listbox", { name: "Кого упомянуть" });
    const options = screen.getAllByRole("option");
    expect(options.map((o) => o.querySelector("bdi")?.textContent)).toEqual(["Аня", "Маня"]);
    expect(options[0]?.getAttribute("aria-selected")).toBe("true");
    expect(field().getAttribute("aria-activedescendant")).toBe(list.firstElementChild?.id);
    await user.keyboard("{Enter}");
    expect(field().value).toBe("привет @Аня ");
    expect(screen.queryByRole("listbox")).toBeNull();
    await user.type(field(), "глянь{Enter}");
    await waitFor(() => {
      expect(counts.sends).toEqual(["привет <@1> глянь"]);
    });
  });

  it("moves with the arrows, picks @everyone as text and closes on Esc without sending", async () => {
    const counts = serverCounts();
    const { user, field } = renderComposer({ mentionables: people });
    await user.type(field(), "@");
    expect(screen.getAllByRole("option")).toHaveLength(3);
    await user.keyboard("{ArrowUp}{Tab}");
    expect(field().value).toBe("@everyone ");
    await user.type(field(), "@м");
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(counts.sends).toEqual([]);
  });
});
