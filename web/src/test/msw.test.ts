import { HttpResponse } from "msw";
import { api, unwrap } from "../api/client";
import { apiHandler, problem, server } from "./msw";

// The handlers below are only type-checked (tsc runs in `build`): each
// expected error below proves apiHandler rejects something off-contract.
export const typeChecks = [
  // @ts-expect-error: the body does not match components.schemas.User
  apiHandler("get", "/users/@me", () => HttpResponse.json({ id: 1 })),
  // @ts-expect-error: an error body must be a full components.schemas.Problem
  apiHandler("get", "/users/@me", () => HttpResponse.json({ code: "not_found" }, { status: 404 })),
  // @ts-expect-error: the contract has no PUT /users/@me
  apiHandler("put", "/users/@me", () => new HttpResponse(null, { status: 204 })),
  // @ts-expect-error: not a path of the contract
  apiHandler("get", "/users/me", () => new HttpResponse(null, { status: 204 })),
  apiHandler("get", "/guilds/{guild_id}", ({ params }) => {
    const id: string = params.guild_id;
    const missing: unknown =
      // @ts-expect-error: the operation has no such path parameter
      params.channel_id;
    return problem(404, typeof missing === "string" ? missing : id);
  }),
];

describe("apiHandler", () => {
  it("serves an operation with typed params, request body and response", async () => {
    server.use(
      apiHandler("post", "/auth/register", async ({ request }) => {
        const body = await request.json();
        return HttpResponse.json(
          {
            id: "1",
            username: body.username.toLowerCase(),
            display_name: body.display_name ?? body.username,
            avatar_url: null,
            created_at: "2026-10-26T00:00:00.000Z",
          },
          { status: 201 },
        );
      }),
    );

    const user = unwrap(await api.POST("/auth/register", { body: { username: "Vasya", password: "password1" } }));

    expect(user).toMatchObject({ username: "vasya", display_name: "Vasya" });
  });
});
