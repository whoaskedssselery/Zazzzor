import { HttpResponse, http } from "msw";
import { setLanguage } from "@/shared/i18n";
import { apiHandler, problem, server } from "@/shared/test";
import { ApiError, api, errorText, unwrap } from "./client";
import { fieldErrorText, tooManyAttemptsText } from "./errors";
import type { components } from "./schema.gen";

type User = components["schemas"]["CurrentUser"];

const user: User = {
  id: "1234567890123456789",
  username: "vasya",
  display_name: "Вася",
  avatar_url: null,
  created_at: "2026-10-21T12:00:00.000Z",
  bio: "",
  status: "online",
  custom_status: null,
};

/** The error `fn` throws, or a failure when it does not throw. */
function thrown(fn: () => unknown): unknown {
  try {
    fn();
  } catch (e) {
    return e;
  }
  throw new Error("expected the call to throw");
}

describe("api client", () => {
  it("sends same-origin requests with credentials and returns the data", async () => {
    // MSW in Node rebuilds the request and drops `credentials`, so read the
    // Request openapi-fetch hands to fetch through a middleware instead.
    let sent: Request | undefined;
    const spy = {
      onRequest: ({ request }: { request: Request }) => {
        sent = request;
      },
    };
    api.use(spy);
    server.use(apiHandler("get", "/users/@me", () => HttpResponse.json(user)));

    const me = await api
      .GET("/users/@me")
      .then(unwrap)
      .finally(() => {
        api.eject(spy);
      });

    expect(me).toEqual(user);
    expect(sent?.url).toBe(`${window.location.origin}/api/v1/users/@me`);
    expect(sent?.credentials).toBe("include");
  });

  it("turns a problem+json response into ApiError with code and field errors", async () => {
    server.use(
      apiHandler("post", "/auth/register", () =>
        problem(422, "validation_failed", [
          { field: "username", message: "too short" },
          { field: "password", message: "too short" },
        ]),
      ),
    );

    const r = await api.POST("/auth/register", { body: { username: "a", password: "1" } });
    const e = thrown(() => unwrap(r));

    expect(e).toBeInstanceOf(ApiError);
    expect(e).toMatchObject({
      status: 422,
      code: "validation_failed",
      fields: { username: "too short", password: "too short" },
    });
  });

  it("keeps the Retry-After seconds of a 429", async () => {
    server.use(
      apiHandler("post", "/auth/login", () => problem(429, "rate_limited", undefined, { "Retry-After": "42" })),
    );

    const r = await api.POST("/auth/login", { body: { username: "vasya", password: "password1" } });

    expect(thrown(() => unwrap(r))).toMatchObject({ status: 429, code: "rate_limited", retryAfter: 42 });
  });

  it.each([["soon"], ["0"], ["-5"], ["1.5"], ["Wed, 21 Oct 2026 07:28:00 GMT"]])(
    "leaves retryAfter undefined for the Retry-After %j",
    async (header) => {
      server.use(
        apiHandler("post", "/auth/login", () => problem(429, "rate_limited", undefined, { "Retry-After": header })),
      );

      const r = await api.POST("/auth/login", { body: { username: "vasya", password: "password1" } });
      const e = thrown(() => unwrap(r));

      expect(e).toBeInstanceOf(ApiError);
      expect(e).toHaveProperty("retryAfter", undefined);
    },
  );

  it("gives code «unknown» to an error response without a body", async () => {
    server.use(apiHandler("get", "/users/@me", () => new HttpResponse(null, { status: 500 })));

    const r = await api.GET("/users/@me");
    const e = thrown(() => unwrap(r));

    expect(e).toBeInstanceOf(ApiError);
    expect(e).toMatchObject({ status: 500, code: "unknown", fields: {} });
  });

  it("gives code «unknown» to an error body that is not a problem (a proxy page)", async () => {
    // Off-contract on purpose (a proxy page), so a bare MSW handler.
    server.use(
      http.get(
        "/api/v1/users/@me",
        () =>
          new HttpResponse("<html>502 Bad Gateway</html>", { status: 502, headers: { "Content-Type": "text/html" } }),
      ),
    );

    const r = await api.GET("/users/@me");

    expect(thrown(() => unwrap(r))).toMatchObject({ status: 502, code: "unknown" });
  });

  it("keeps hostile field names as plain own keys", async () => {
    server.use(
      apiHandler("post", "/auth/register", () =>
        problem(400, "validation_failed", [
          { field: "__proto__", message: "proto" },
          { field: "constructor", message: "ctor" },
          { field: "username", message: "first" },
          { field: "username", message: "second" },
        ]),
      ),
    );

    const r = await api.POST("/auth/register", { body: { username: "a", password: "1" } });
    const e = thrown(() => unwrap(r));

    if (!(e instanceof ApiError)) throw new Error("expected ApiError");
    const { fields } = e;
    expect(Object.keys(fields)).toEqual(["__proto__", "constructor", "username"]);
    expect(Object.getOwnPropertyDescriptor(fields, "__proto__")?.value).toBe("proto");
    expect(Object.getPrototypeOf(fields)).toBe(Object.prototype);
    expect(fields.constructor).toBe("ctor");
    expect(fields.username).toBe("first");
  });

  it("rejects with a network error when no response arrives", async () => {
    server.use(apiHandler("get", "/users/@me", () => HttpResponse.error()));

    const failure = await api.GET("/users/@me").then(
      () => undefined,
      (e: unknown) => e,
    );

    expect(failure).toBeInstanceOf(TypeError);
    expect(errorText(failure)).toBe("Нет связи с сервером. Проверьте подключение.");
  });
});

describe("tooManyAttemptsText", () => {
  it.each([
    [1, "Слишком много попыток, подождите 1 с."],
    [59, "Слишком много попыток, подождите 59 с."],
    [60, "Слишком много попыток, подождите 1 мин."],
    [61, "Слишком много попыток, подождите 2 мин."],
    [3600, "Слишком много попыток, подождите 60 мин."],
  ])("words a wait of %i s", (seconds, text) => {
    expect(tooManyAttemptsText(seconds)).toBe(text);
  });

  it("falls back to the rate_limited text without Retry-After", () => {
    expect(tooManyAttemptsText(undefined)).toBe("Слишком много запросов. Подождите немного.");
  });
});

describe("the error texts in English", () => {
  beforeEach(() => {
    setLanguage("en");
  });

  it("follow the interface's language", () => {
    expect(errorText(new ApiError(403, "missing_permission", {}))).toBe("You don't have permission to do this.");
    expect(errorText(new ApiError(500, "internal", {}))).toBe("Something went wrong. Please try again.");
    expect(errorText(new TypeError("fetch failed"))).toBe("Can't reach the server. Check your connection.");
    expect(tooManyAttemptsText(90)).toBe("Too many attempts, wait 2 min.");
    expect(fieldErrorText("password")).toBe("Password: 8 to 128 characters.");
    expect(fieldErrorText("nonsense")).toBe("Check this field.");
  });
});

describe("errorText", () => {
  it.each([
    ["invalid_credentials", "Неверный логин или пароль."],
    ["username_taken", "Этот логин уже занят."],
    ["missing_permission", "Недостаточно прав для этого действия."],
    ["rate_limited", "Слишком много запросов. Подождите немного."],
    ["not_found", "Не найдено. Возможно, это уже удалили."],
    ["invalid_body", "Сервер не понял запрос. Обновите страницу и попробуйте снова."],
    ["unsupported_media_type", "Что-то пошло не так. Попробуйте ещё раз."],
    ["unknown", "Что-то пошло не так. Попробуйте ещё раз."],
    ["no_such_code_anywhere", "Что-то пошло не так. Попробуйте ещё раз."],
  ])("maps %s to Russian text", (code, text) => {
    expect(errorText(new ApiError(400, code))).toBe(text);
  });

  it("explains a failed connection", () => {
    expect(errorText(new TypeError("Failed to fetch"))).toBe("Нет связи с сервером. Проверьте подключение.");
  });

  it("falls back to the generic text for anything else", () => {
    expect(errorText(new Error("boom"))).toBe("Что-то пошло не так. Попробуйте ещё раз.");
    expect(errorText("строка")).toBe("Что-то пошло не так. Попробуйте ещё раз.");
    expect(errorText(undefined)).toBe("Что-то пошло не так. Попробуйте ещё раз.");
  });

  it("does not treat inherited object keys as codes", () => {
    expect(errorText(new ApiError(400, "constructor"))).toBe("Что-то пошло не так. Попробуйте ещё раз.");
  });
});
