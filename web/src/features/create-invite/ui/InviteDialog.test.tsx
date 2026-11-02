import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import type { components } from "@/shared/api";
import { apiHandler, makeUser, renderRouted, server } from "@/shared/test";
import { InviteDialog } from "./InviteDialog";

type CreateInviteRequest = components["schemas"]["CreateInviteRequest"];

const inviter = makeUser("1");
const LINK = `${window.location.origin}/invite/Zz9Yy8Xx`;

const created: CreateInviteRequest[] = [];

beforeEach(() => {
  created.length = 0;
  server.use(
    apiHandler("post", "/channels/{channel_id}/invites", async ({ request, params }) => {
      created.push(await request.json());
      return HttpResponse.json(
        {
          code: "Zz9Yy8Xx",
          guild: { id: "10", name: "Дача", icon_url: null },
          channel: { id: params.channel_id, name: "общий" },
          inviter,
          uses: 0,
          max_uses: 0,
          expires_at: null,
        },
        { status: 201 },
      );
    }),
  );
});

const open = () =>
  renderRouted(<InviteDialog open onOpenChange={() => {}} channelId="101" channelName="общий" />, {
    path: "/channels/10/101",
  });

const optionsOf = async (user: Awaited<ReturnType<typeof open>>["user"], label: string) => {
  await user.click(screen.getByRole("combobox", { name: label }));
  const listbox = await screen.findByRole("listbox");
  return within(listbox)
    .getAllByRole("option")
    .map((o) => o.textContent);
};

describe("InviteDialog", () => {
  it("offers the lifetimes and the use limits of Discord", async () => {
    const { user } = await open();
    await screen.findByRole("dialog", { name: "Пригласить друзей в #общий" });
    expect(await optionsOf(user, "Срок действия")).toEqual([
      "30 минут",
      "1 час",
      "6 часов",
      "12 часов",
      "1 день",
      "7 дней",
      "Никогда",
    ]);
    await user.keyboard("{Escape}");
    expect(await optionsOf(user, "Число использований")).toEqual([
      "Без ограничений",
      "1",
      "5",
      "10",
      "25",
      "50",
      "100",
    ]);
  });

  it("creates a link with the defaults (7 days, no limit) and copies it", async () => {
    const { user } = await open();
    await user.click(await screen.findByRole("button", { name: "Создать ссылку" }));

    const field = await screen.findByLabelText<HTMLInputElement>("Ссылка-приглашение");
    expect(field.value).toBe(LINK);
    expect(field.readOnly).toBe(true);
    expect(created).toEqual([{ max_age_seconds: 604800, max_uses: 0 }]);
    expect(await screen.findByText("Ссылка скопирована")).toBeTruthy();
    expect(await navigator.clipboard.readText()).toBe(LINK);
  });

  it("creates a link with the chosen lifetime and use limit", async () => {
    const { user } = await open();
    await screen.findByRole("dialog", { name: "Пригласить друзей в #общий" });
    await optionsOf(user, "Срок действия");
    await user.click(screen.getByRole("option", { name: "1 час" }));
    await optionsOf(user, "Число использований");
    await user.click(screen.getByRole("option", { name: "10" }));
    await user.click(screen.getByRole("button", { name: "Создать ссылку" }));
    await screen.findByLabelText("Ссылка-приглашение");
    expect(created).toEqual([{ max_age_seconds: 3600, max_uses: 10 }]);

    await optionsOf(user, "Срок действия");
    await user.click(screen.getByRole("option", { name: "Никогда" }));
    await user.click(screen.getByRole("button", { name: "Создать новую ссылку" }));
    await waitFor(() => {
      expect(created).toEqual([
        { max_age_seconds: 3600, max_uses: 10 },
        { max_age_seconds: 0, max_uses: 10 },
      ]);
    });
  });

  it("selects the link for copying by hand when the clipboard refuses", async () => {
    const { user } = await open();
    const refuse = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockRejectedValue(new DOMException("denied", "NotAllowedError"));
    await user.click(await screen.findByRole("button", { name: "Создать ссылку" }));

    expect(await screen.findByText("Не получилось скопировать. Ссылка выделена: скопируйте её вручную.")).toBeTruthy();
    const field = screen.getByLabelText<HTMLInputElement>("Ссылка-приглашение");
    expect(document.activeElement).toBe(field);
    expect([field.selectionStart, field.selectionEnd]).toEqual([0, LINK.length]);
    expect(refuse).toHaveBeenCalledWith(LINK);
  });

  it("copies the link again on «Копировать»", async () => {
    const { user } = await open();
    await user.click(await screen.findByRole("button", { name: "Создать ссылку" }));
    await screen.findByLabelText("Ссылка-приглашение");
    await navigator.clipboard.writeText("");
    await user.click(screen.getByRole("button", { name: "Копировать" }));
    await waitFor(async () => {
      expect(await navigator.clipboard.readText()).toBe(LINK);
    });
  });
});
