import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { HttpResponse } from "msw";
import { renderApp, sessionHandler, testUser } from "@/app/test";
import { readVoicePrefs, resetCall, setCall } from "@/entities/call";
import { resetGuilds } from "@/entities/guild";
import { PASSWORD_CHANGED, PASSWORDS_DIFFER } from "@/features/change-password";
import { NAME_RULE } from "@/features/edit-profile";
import { PTT_FOCUS_HINT } from "@/features/voice-call";
import { LANGUAGE_STORAGE_KEY, TRANSLATED } from "@/shared/i18n";
import { apiHandler, dispatchFrame, makeReady, problem, readyGateway, server } from "@/shared/test";
import { THEME_STORAGE_KEY } from "@/shared/ui";

const AVATAR = "/api/v1/files/avatars/1234567890123456789/0123456789abcdef.png";

const signedIn = (user = testUser) => {
  server.use(sessionHandler(user), readyGateway(makeReady(user, [])));
};

/** The file input behind «Сменить аватар». */
const picker = () => {
  const input = document.querySelector<HTMLInputElement>('input[type="file"]');
  if (!input) throw new Error("no file input");
  return input;
};

const png = () => new File([new Uint8Array(8)], "me.png", { type: "image/png" });

const panelName = () => document.querySelector("[data-user-name]")?.textContent;

afterEach(() => {
  resetGuilds();
});

describe("user settings", () => {
  it("opens the first section for an unknown one", async () => {
    signedIn();
    const { router } = renderApp("/settings/nope");
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/settings/profile");
    });
    expect(await screen.findByRole("heading", { name: "Профиль" })).toBeTruthy();
  });

  it("renames the user, and the user panel follows once the settings close", async () => {
    const bodies: unknown[] = [];
    signedIn();
    server.use(
      apiHandler("patch", "/users/@me", async ({ request }) => {
        const body = (await request.json()) as { display_name: string };
        bodies.push(body);
        return HttpResponse.json({ ...testUser, display_name: body.display_name });
      }),
    );
    const { user, router } = renderApp("/channels/@me");
    await user.click(await screen.findByRole("button", { name: "Настройки пользователя" }));
    const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
    await user.clear(name);
    await user.type(name, "  Василий  ");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
    });
    expect(bodies).toEqual([{ display_name: "Василий" }]);
    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
    expect(panelName()).toBe("Василий");
  });

  it("saves «О себе» trimmed, with its line breaks", async () => {
    const bodies: unknown[] = [];
    signedIn();
    server.use(
      apiHandler("patch", "/users/@me", async ({ request }) => {
        const body = (await request.json()) as { bio: string };
        bodies.push(body);
        return HttpResponse.json({ ...testUser, bio: body.bio });
      }),
    );
    const { user } = renderApp("/settings/profile");
    const bio = await screen.findByRole("textbox", { name: "О себе" });
    await user.type(bio, "  Люблю кино{Enter}и чай  ");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
    });
    expect(bodies).toEqual([{ bio: "Люблю кино\nи чай" }]);
  });

  it("checks the display name before sending it", async () => {
    signedIn();
    const { user } = renderApp("/settings/profile");
    const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
    await user.clear(name);
    await user.type(name, "   ");
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    expect(await screen.findByText(NAME_RULE)).toBeTruthy();
  });

  it("keeps an avatar picked while the previous one uploads", async () => {
    const sizes: number[] = [];
    const appended = vi.spyOn(FormData.prototype, "append");
    onTestFinished(() => {
      appended.mockRestore();
    });
    let finish = () => {};
    signedIn();
    server.use(
      apiHandler("put", "/users/@me/avatar", async () => {
        // The file as the app put it in the form: since Node 26 fetch cannot read a
        // jsdom file back out of the request body (the browser has no such problem).
        const file = appended.mock.calls.at(-1)?.[1];
        sizes.push(file instanceof Blob ? file.size : -1);
        // The first upload waits for the test; later ones answer at once.
        if (sizes.length === 1) {
          await new Promise<void>((resolve) => {
            finish = resolve;
          });
        }
        return HttpResponse.json({ ...testUser, avatar_url: AVATAR });
      }),
    );
    const { user } = renderApp("/settings/profile");
    await screen.findByRole("textbox", { name: "Отображаемое имя" });
    fireEvent.change(picker(), { target: { files: [png()] } });
    await user.click(await screen.findByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(sizes).toEqual([8]);
    });
    const second = new File([new Uint8Array(16)], "second.png", { type: "image/png" });
    fireEvent.change(picker(), { target: { files: [second] } });
    finish();
    // The first save is done, the second pick still waits for its own.
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Сохранить изменения" })).toHaveProperty("disabled", false);
    });
    expect(screen.getByRole("region", { name: "Есть несохранённые изменения" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
    });
    expect(sizes).toEqual([8, 16]);
  });

  it("uploads a new avatar after showing it", async () => {
    let uploaded = false;
    signedIn();
    server.use(
      apiHandler("put", "/users/@me/avatar", () => {
        uploaded = true;
        return HttpResponse.json({ ...testUser, avatar_url: AVATAR });
      }),
    );
    const { user } = renderApp("/settings/profile");
    await screen.findByRole("textbox", { name: "Отображаемое имя" });
    fireEvent.change(picker(), { target: { files: [png()] } });
    expect(await screen.findByRole("region", { name: "Есть несохранённые изменения" })).toBeTruthy();
    expect(uploaded).toBe(false);
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(uploaded).toBe(true);
    });
    await waitFor(() => {
      expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
    });
  });

  it("refuses a file that is not an image before uploading it", async () => {
    signedIn();
    renderApp("/settings/profile");
    await screen.findByRole("textbox", { name: "Отображаемое имя" });
    fireEvent.change(picker(), { target: { files: [new File(["x"], "a.txt", { type: "text/plain" })] } });
    expect((await screen.findByRole("alert")).textContent).toBe("Нужна картинка PNG, JPEG, GIF или WebP.");
    expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
  });

  it("removes the avatar on save", async () => {
    let removed = false;
    signedIn({ ...testUser, avatar_url: AVATAR });
    server.use(
      apiHandler("delete", "/users/@me/avatar", () => {
        removed = true;
        return HttpResponse.json(testUser);
      }),
    );
    const { user } = renderApp("/settings/profile");
    await user.click(await screen.findByRole("button", { name: "Удалить аватар" }));
    expect(screen.queryByRole("button", { name: "Удалить аватар" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Сохранить изменения" }));
    await waitFor(() => {
      expect(removed).toBe(true);
    });
  });

  it("takes a USER_UPDATE into the form while it holds nothing of its own", async () => {
    let send: ((d: unknown) => void) | undefined;
    server.use(
      sessionHandler(testUser),
      readyGateway(makeReady(testUser, []), (client) => {
        send = (d) => {
          client.send(dispatchFrame("USER_UPDATE", 2, d));
        };
      }),
    );
    renderApp("/settings/profile");
    const name = await screen.findByRole("textbox", { name: "Отображаемое имя" });
    await waitFor(() => {
      expect(send).toBeDefined();
    });
    send?.({ ...testUser, display_name: "Васёк" });
    await waitFor(() => {
      expect(name).toHaveProperty("value", "Васёк");
    });
    expect(screen.queryByRole("region", { name: "Есть несохранённые изменения" })).toBeNull();
  });

  it("asks before closing with unsaved changes", async () => {
    signedIn();
    const { user, router } = renderApp("/settings/profile");
    await user.type(await screen.findByRole("textbox", { name: "Отображаемое имя" }), "!");
    await user.click(screen.getByRole("button", { name: "Закрыть настройки" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Уйти без сохранения?" });
    expect(router.state.location.pathname).toBe("/settings/profile");
    await user.click(within(dialog).getByRole("button", { name: "Уйти" }));
    // Opened directly, so there is nothing to go back to: home.
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
  });
});

describe("password", () => {
  const fill = async (user: ReturnType<typeof renderApp>["user"], current: string, next: string, repeat: string) => {
    await user.type(await screen.findByLabelText("Текущий пароль"), current);
    await user.type(screen.getByLabelText("Новый пароль"), next);
    await user.type(screen.getByLabelText("Новый пароль ещё раз"), repeat);
    await user.click(screen.getByRole("button", { name: "Сменить пароль" }));
  };

  it("changes the password and says the other sessions ended", async () => {
    const bodies: unknown[] = [];
    signedIn();
    server.use(
      apiHandler("post", "/users/@me/password", async ({ request }) => {
        bodies.push(await request.json());
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp("/settings/account");
    await fill(user, "old-secret", "new-secret-1", "new-secret-1");
    expect(await screen.findByText(PASSWORD_CHANGED)).toBeTruthy();
    expect(bodies).toEqual([{ current_password: "old-secret", new_password: "new-secret-1" }]);
    expect(screen.getByLabelText("Текущий пароль")).toHaveProperty("value", "");
  });

  it("catches new passwords that differ before sending them", async () => {
    let sent = false;
    signedIn();
    server.use(
      apiHandler("post", "/users/@me/password", () => {
        sent = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user } = renderApp("/settings/account");
    await fill(user, "old-secret", "new-secret-1", "new-secret-2");
    expect(await screen.findByText(PASSWORDS_DIFFER)).toBeTruthy();
    expect(sent).toBe(false);
  });

  it("puts a wrong current password under its field", async () => {
    signedIn();
    server.use(
      apiHandler("post", "/users/@me/password", () =>
        problem(400, "validation_failed", [{ field: "current_password", message: "wrong" }]),
      ),
    );
    const { user } = renderApp("/settings/account");
    await fill(user, "not-it", "new-secret-1", "new-secret-1");
    expect(await screen.findByText("Неверный текущий пароль.")).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByLabelText("Текущий пароль"));
  });
});

describe("appearance", () => {
  afterEach(() => {
    localStorage.clear();
  });

  it("switches the theme at once and keeps the choice", async () => {
    signedIn();
    const { user } = renderApp("/settings/appearance");
    const light = await screen.findByRole("radio", { name: "Светлая" });
    expect(screen.getByRole("radio", { name: "Тёмная" })).toHaveProperty("ariaChecked", "true");
    await user.click(light);
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    await user.click(screen.getByRole("radio", { name: "Как в системе" }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
  });
});

describe("sign-out", () => {
  it("asks first, then signs out", async () => {
    let out = false;
    signedIn();
    server.use(
      apiHandler("post", "/auth/logout", () => {
        out = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { user, router } = renderApp("/settings/profile");
    await user.click(await screen.findByRole("button", { name: "Выйти" }));
    const dialog = await screen.findByRole("alertdialog", { name: "Выйти из аккаунта?" });
    expect(out).toBe(false);
    await user.click(within(dialog).getByRole("button", { name: "Выйти" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/login");
    });
    expect(out).toBe(true);
  });
});

describe("voice and video", () => {
  const devices = [
    { kind: "audioinput", deviceId: "mic-1", label: "Микрофон USB", groupId: "a" },
    { kind: "audiooutput", deviceId: "out-1", label: "Наушники", groupId: "b" },
  ];

  beforeEach(() => {
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      // MediaDevices is an EventTarget (devicechange).
      value: Object.assign(new EventTarget(), {
        enumerateDevices: () => Promise.resolve(devices),
        getSupportedConstraints: () => ({}),
      }),
    });
    Object.defineProperty(HTMLMediaElement.prototype, "setSinkId", {
      configurable: true,
      value: () => Promise.resolve(),
    });
  });

  afterEach(() => {
    localStorage.clear();
    Reflect.deleteProperty(navigator, "mediaDevices");
    Reflect.deleteProperty(HTMLMediaElement.prototype, "setSinkId");
  });

  const pick = async (user: ReturnType<typeof renderApp>["user"], label: string, option: string) => {
    await user.click(await screen.findByRole("combobox", { name: label }));
    await user.click(await screen.findByRole("option", { name: option }));
  };

  it("lists the devices and keeps the chosen ones", async () => {
    signedIn();
    const { user } = renderApp("/settings/voice");
    await pick(user, "Устройство ввода", "Микрофон USB");
    await pick(user, "Устройство вывода", "Наушники");
    expect(readVoicePrefs()).toMatchObject({ inputId: "mic-1", outputId: "out-1" });
  });

  it("switches to push-to-talk on a key the user presses", async () => {
    signedIn();
    const { user } = renderApp("/settings/voice");
    await user.click(await screen.findByRole("radio", { name: "Push-to-talk" }));
    expect(readVoicePrefs().mode).toBe("ptt");
    expect(screen.getByText(PTT_FOCUS_HINT)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Изменить клавишу" }));
    expect(screen.getByText("Нажмите клавишу…")).toBeTruthy();
    await user.keyboard("b");
    expect(readVoicePrefs().pttKey).toBe("KeyB");
    expect(screen.getByText("B", { selector: "kbd" })).toBeTruthy();
  });

  it("sets the combination that flips the microphone, and clears it", async () => {
    signedIn();
    const { user } = renderApp("/settings/voice");
    await user.click(await screen.findByRole("button", { name: "Изменить: Выключить микрофон" }));
    expect(screen.getByText("Нажмите сочетание…")).toBeTruthy();
    // A modifier alone waits for its key.
    await user.keyboard("{Control>}{Shift>}m{/Shift}{/Control}");
    expect(readVoicePrefs().muteKey).toBe("Control+Shift+KeyM");
    expect(screen.getByText("Ctrl + Shift + M", { selector: "kbd" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Сбросить: Выключить микрофон" }));
    expect(readVoicePrefs().muteKey).toBeNull();
    expect(screen.getAllByText("Не назначено", { selector: "kbd" })).toHaveLength(2);
  });

  it("chooses the noise suppression, enhanced by default", async () => {
    signedIn();
    const { user } = renderApp("/settings/voice");
    const noise = await screen.findByRole("combobox", { name: "Шумоподавление" });
    expect(noise.textContent).toContain("Улучшенное");
    await pick(user, "Шумоподавление", "Стандартное");
    expect(readVoicePrefs().noise).toBe("standard");
    await pick(user, "Шумоподавление", "Выкл");
    expect(readVoicePrefs().noise).toBe("off");
    await pick(user, "Шумоподавление", "Улучшенное");
    expect(readVoicePrefs().noise).toBe("enhanced");
  });

  it("shows what cleans the microphone in the call, and why it fell back", async () => {
    onTestFinished(resetCall);
    signedIn();
    renderApp("/settings/voice");
    await screen.findByRole("combobox", { name: "Шумоподавление" });
    expect(screen.queryByText(/^Сейчас:/)).toBeNull();
    act(() => {
      setCall({ status: "connected", guildId: "10", channelId: "102", noise: { running: "loading" } });
    });
    expect(screen.getByText("Сейчас: загружается улучшенное, пока без шумоподавления")).toBeTruthy();
    act(() => {
      setCall({ noise: { running: "enhanced" } });
    });
    expect(screen.getByText("Сейчас: улучшенное")).toBeTruthy();
    act(() => {
      setCall({ noise: { running: "rnnoise", reason: "overload" } });
    });
    expect(screen.getByText("Сейчас: запасное — компьютер не успевает с улучшенным")).toBeTruthy();
    act(() => {
      setCall({ noise: { running: "standard", reason: "failed" } });
    });
    expect(screen.getByText("Сейчас: стандартное — улучшенное не загрузилось")).toBeTruthy();
  });
});

describe("voice devices that change", () => {
  afterEach(() => {
    localStorage.clear();
    Reflect.deleteProperty(navigator, "mediaDevices");
  });

  /** The browser's devices as an event target, with the list it reports now. */
  const devicesBrowser = (list: () => Promise<MediaDeviceInfo[]>) => {
    const target = new EventTarget();
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: Object.assign(target, { enumerateDevices: list, getSupportedConstraints: () => ({}) }),
    });
    return target;
  };

  const device = (deviceId: string, label: string) =>
    ({ kind: "audioinput", deviceId, label, groupId: deviceId }) as MediaDeviceInfo;

  it("lists a headset plugged in while the tab is open", async () => {
    let devices = [device("mic-1", "Микрофон USB")];
    const target = devicesBrowser(() => Promise.resolve(devices));
    signedIn();
    const { user } = renderApp("/settings/voice");
    await user.click(await screen.findByRole("combobox", { name: "Устройство ввода" }));
    expect(screen.queryByRole("option", { name: "Гарнитура" })).toBeNull();
    await user.keyboard("{Escape}");
    devices = [...devices, device("mic-2", "Гарнитура")];
    target.dispatchEvent(new Event("devicechange"));
    await user.click(await screen.findByRole("combobox", { name: "Устройство ввода" }));
    expect(await screen.findByRole("option", { name: "Гарнитура" })).toBeTruthy();
  });

  it("still opens when the browser cannot list the devices", async () => {
    devicesBrowser(() => Promise.reject(new DOMException("blocked", "NotAllowedError")));
    signedIn();
    const { user } = renderApp("/settings/voice");
    await user.click(await screen.findByRole("combobox", { name: "Устройство ввода" }));
    expect(await screen.findByRole("option", { name: "Как в системе" })).toBeTruthy();
  });
});

describe("voice output in Firefox", () => {
  afterEach(() => {
    localStorage.clear();
    Reflect.deleteProperty(navigator, "mediaDevices");
    Reflect.deleteProperty(HTMLMediaElement.prototype, "setSinkId");
  });

  it("picks the output through the browser's own list", async () => {
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: Object.assign(new EventTarget(), {
        enumerateDevices: () => Promise.resolve([]),
        getSupportedConstraints: () => ({}),
        selectAudioOutput: () =>
          Promise.resolve({ kind: "audiooutput", deviceId: "out-9", label: "Колонки", groupId: "c" }),
      }),
    });
    Object.defineProperty(HTMLMediaElement.prototype, "setSinkId", {
      configurable: true,
      value: () => Promise.resolve(),
    });
    signedIn();
    const { user } = renderApp("/settings/voice");
    await user.click(await screen.findByRole("button", { name: "Выбрать устройство вывода" }));
    await waitFor(() => {
      expect(readVoicePrefs().outputId).toBe("out-9");
    });
    expect(screen.getByRole("combobox", { name: "Устройство вывода" }).textContent).toContain("Колонки");
  });
});

describe("the interface language", () => {
  it("switches at once and is kept, and is listed only once everything is translated", async () => {
    signedIn();
    const { user } = renderApp("/settings/language");
    await user.click(await screen.findByRole("radio", { name: "English" }));
    expect(await screen.findByRole("heading", { name: "Language" })).toBeTruthy();
    // The frame around it switches too: the sections, the way out, the close button.
    expect(screen.getByRole("link", { name: "Profile" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Log out" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Close settings" })).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Language" }) !== null).toBe(TRANSLATED);
    expect(document.documentElement.lang).toBe("en");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("en");
    await user.click(screen.getByRole("radio", { name: "Русский" }));
    expect(await screen.findByRole("heading", { name: "Язык" })).toBeTruthy();
  });
});
