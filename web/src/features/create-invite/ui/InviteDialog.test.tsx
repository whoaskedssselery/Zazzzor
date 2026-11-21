import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import type { components } from "@/shared/api";
import { apiHandler, makeUser, renderRouted, server } from "@/shared/test";
import { COPY_FAILED, InviteDialog, lifetimeNote } from "./InviteDialog";

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

const open = (onOpenChange: (open: boolean) => void = () => {}) =>
  renderRouted(<InviteDialog open onOpenChange={onOpenChange} channelId="101" channelName="общий" />, {
    path: "/channels/10/101",
  });

const optionsOf = async (user: Awaited<ReturnType<typeof open>>["user"], label: string) => {
  await user.click(screen.getByRole("combobox", { name: label }));
  const listbox = await screen.findByRole("listbox");
  return within(listbox)
    .getAllByRole("option")
    .map((o) => o.textContent);
};

const LABEL = "Отправьте другу ссылку-приглашение";

const linkField = async () => {
  const field = await screen.findByLabelText<HTMLInputElement>(LABEL);
  await waitFor(() => {
    expect(field.value).toBe(LINK);
  });
  return field;
};

describe("InviteDialog", () => {
  it("makes a link with the defaults (7 days, no limit) as it opens", async () => {
    await open();
    await screen.findByRole("dialog", { name: "Пригласить друзей в #общий" });
    const field = await linkField();
    expect(field.readOnly).toBe(true);
    expect(created).toEqual([{ max_age_seconds: 604800, max_uses: 0 }]);
    expect(screen.getByText(/Ссылка перестанет действовать через 7 дней./)).toBeTruthy();
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "Копировать" }));
    });
  });

  it("copies the link on «Копировать»", async () => {
    const { user } = await open();
    await linkField();
    await user.click(screen.getByRole("button", { name: "Копировать" }));
    expect(await screen.findByRole("button", { name: "Скопировано" })).toBeTruthy();
    expect(await navigator.clipboard.readText()).toBe(LINK);
  });

  it("offers the lifetimes and the use limits of Discord behind «Изменить ссылку-приглашение»", async () => {
    const { user } = await open();
    await linkField();
    expect(screen.queryByRole("combobox", { name: "Срок действия" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Изменить ссылку-приглашение" }));
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

  it("makes a new link with the chosen lifetime and use limit", async () => {
    const { user } = await open();
    await linkField();
    await user.click(screen.getByRole("button", { name: "Изменить ссылку-приглашение" }));
    await optionsOf(user, "Срок действия");
    await user.click(screen.getByRole("option", { name: "1 час" }));
    await optionsOf(user, "Число использований");
    await user.click(screen.getByRole("option", { name: "10" }));
    await user.click(screen.getByRole("button", { name: "Создать новую ссылку" }));
    await waitFor(() => {
      expect(created).toEqual([
        { max_age_seconds: 604800, max_uses: 0 },
        { max_age_seconds: 3600, max_uses: 10 },
      ]);
    });
    expect(await screen.findByText(/через 1 час или после 10 входов./)).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Срок действия" })).toBeNull();
  });

  it("says when the link stops working", () => {
    expect(lifetimeNote("604800", "0")).toBe("Ссылка перестанет действовать через 7 дней.");
    expect(lifetimeNote("0", "0")).toBe("Ссылка не истечёт.");
    expect(lifetimeNote("0", "1")).toBe("Ссылка не истечёт или после 1 входа.");
  });

  it("selects the link for copying by hand when the clipboard refuses", async () => {
    const { user } = await open();
    const field = await linkField();
    const refuse = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockRejectedValue(new DOMException("denied", "NotAllowedError"));
    await user.click(screen.getByRole("button", { name: "Копировать" }));

    expect(await screen.findByText(COPY_FAILED)).toBeTruthy();
    expect(document.activeElement).toBe(field);
    expect([field.selectionStart, field.selectionEnd]).toEqual([0, LINK.length]);
    expect(refuse).toHaveBeenCalledWith(LINK);
  });

  it("closes on the first Escape once the link is copied", async () => {
    const onOpenChange = vi.fn();
    const { user } = await open(onOpenChange);
    await linkField();
    await user.click(screen.getByRole("button", { name: "Копировать" }));
    await screen.findByRole("button", { name: "Скопировано" });
    await user.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
