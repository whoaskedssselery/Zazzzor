import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse } from "msw";
import { applyReady, type GuildEntry } from "@/entities/guild";
import { apiHandler, makeGuildEmoji, makeGuildState, makeMember, makeReady, makeUser, server } from "@/shared/test";
import { UiProviders } from "@/shared/ui";
import { EMOJI_TOO_LARGE, EmojiTab, freeName, nameFromFile } from "./EmojiTab";

const G = "10";
const me = makeUser("100", { display_name: "Аня" });

const entryWith = (emojis: ReturnType<typeof makeGuildEmoji>[]): GuildEntry => {
  const state = makeGuildState(G, me, { emojis, members: [makeMember(me)] });
  const entry = applyReady(makeReady(me, [state])).guilds[G];
  if (!entry) throw new Error("unreachable");
  return entry;
};

const renderTab = (entry: GuildEntry) => {
  const user = userEvent.setup();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <UiProviders>
        <EmojiTab entry={entry} />
      </UiProviders>
    </QueryClientProvider>,
  );
  return user;
};

const png = (name: string, bytes = 10) => new File([new Uint8Array(bytes)], name, { type: "image/png" });

describe("emoji names from files", () => {
  it("keeps what the server takes, makes the rest _, and falls back to emoji", () => {
    expect(nameFromFile("vasya face.png")).toBe("vasya_face");
    expect(nameFromFile("кот.gif")).toBe("emoji");
    expect(nameFromFile(`${"a".repeat(40)}.png`)).toBe("a".repeat(32));
  });

  it("numbers a name already taken", () => {
    expect(freeName("vasya", new Set(["vasya", "vasya_2"]))).toBe("vasya_3");
    expect(freeName("Kot", new Set(["vasya"]))).toBe("Kot");
  });
});

describe("EmojiTab", () => {
  it("lists still and animated emoji with their counts and who added them", () => {
    renderTab(
      entryWith([
        makeGuildEmoji("501", G, "vasya", { creator_id: me.id }),
        makeGuildEmoji("502", G, "dance", { animated: true }),
      ]),
    );
    const still = screen.getByRole("region", { name: "Обычные" });
    expect(within(still).getByText("1 из 50")).toBeTruthy();
    expect(within(still).getByRole("textbox", { name: "Имя эмодзи :vasya:" })).toBeTruthy();
    expect(within(still).getByText("Аня")).toBeTruthy();
    const moving = screen.getByRole("region", { name: "Анимированные" });
    expect(within(moving).getByRole("img", { name: ":dance:" })).toBeTruthy();
  });

  it("uploads a picked file under its file's name, and refuses one over 256 KB", async () => {
    // jsdom's multipart bodies do not reach the handler whole: the parts are read as they are added.
    const parts: [string, string][] = [];
    const append = vi.spyOn(FormData.prototype, "append").mockImplementation((name, value) => {
      parts.push([name, value instanceof File ? value.name : typeof value === "string" ? value : ""]);
    });
    onTestFinished(() => {
      append.mockRestore();
    });
    let posts = 0;
    server.use(
      apiHandler("post", "/guilds/{guild_id}/emojis", () => {
        posts += 1;
        return HttpResponse.json(makeGuildEmoji("503", G, "vasya_2"), { status: 201 });
      }),
    );
    const user = renderTab(entryWith([makeGuildEmoji("501", G, "vasya")]));
    const input = document.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error("no file input");
    await user.upload(input, [png("vasya.png"), png("big.png", 256 * 1024 + 1)]);
    await waitFor(() => {
      expect(posts).toBe(1);
    });
    expect(parts).toEqual([
      ["name", "vasya_2"],
      ["file", "vasya.png"],
    ]);
    expect(await screen.findByText(`big.png: ${EMOJI_TOO_LARGE}`)).toBeTruthy();
  });

  it("renames on Enter and deletes after asking", async () => {
    const renamed: string[] = [];
    const deleted: string[] = [];
    server.use(
      apiHandler("patch", "/guilds/{guild_id}/emojis/{emoji_id}", async ({ request, params }) => {
        const body = await request.json();
        renamed.push(`${params.emoji_id}:${body.name}`);
        return HttpResponse.json(makeGuildEmoji(params.emoji_id, G, body.name));
      }),
      apiHandler("delete", "/guilds/{guild_id}/emojis/{emoji_id}", ({ params }) => {
        deleted.push(params.emoji_id);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = renderTab(entryWith([makeGuildEmoji("501", G, "vasya")]));
    const field = screen.getByRole("textbox", { name: "Имя эмодзи :vasya:" });
    await user.clear(field);
    await user.type(field, "vasily{Enter}");
    await waitFor(() => {
      expect(renamed).toEqual(["501:vasily"]);
    });
    await user.click(screen.getByRole("button", { name: "Удалить :vasya:" }));
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Удалить" }));
    await waitFor(() => {
      expect(deleted).toEqual(["501"]);
    });
  });
});
