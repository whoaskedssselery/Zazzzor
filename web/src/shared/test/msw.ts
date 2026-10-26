import { type DefaultBodyType, type HttpHandler, HttpResponse, type HttpResponseResolver, http } from "msw";
import { setupServer } from "msw/node";
import type { components, paths } from "@/shared/api";

type Problem = components["schemas"]["Problem"];
type FieldError = components["schemas"]["FieldError"];

/** One MSW server for every test file; handlers are added per test with `server.use`. */
export const server = setupServer();

type HttpMethod = "get" | "put" | "post" | "delete" | "patch";

/** Methods the contract defines for `P` (an absent method is `?: never` in the schema). */
export type ApiMethod<P extends keyof paths> = {
  [M in HttpMethod]-?: [NonNullable<paths[P][M]>] extends [never] ? never : M;
}[HttpMethod];

type Operation<P extends keyof paths, M extends ApiMethod<P>> = NonNullable<paths[P][M]>;

type JsonOf<R> = R extends { content: { "application/json": infer B extends DefaultBodyType } } ? B : never;
type Responses<O> = O extends { responses: infer R } ? R : never;
type SuccessStatus = 200 | 201 | 202 | 203 | 204;

/** JSON body of the 2xx responses of an operation (never for bodiless ones such as 204). */
export type ApiSuccessBody<P extends keyof paths, M extends ApiMethod<P>> = {
  [S in keyof Responses<Operation<P, M>> & SuccessStatus]: JsonOf<Responses<Operation<P, M>>[S]>;
}[keyof Responses<Operation<P, M>> & SuccessStatus];

/** JSON request body of an operation (never when it takes none or another media type). */
export type ApiRequestBody<P extends keyof paths, M extends ApiMethod<P>> =
  Operation<P, M> extends { requestBody?: { content: { "application/json": infer B extends DefaultBodyType } } }
    ? B
    : never;

/** Route parameters as MSW parses them: `{guild_id}` → `params.guild_id` (always a string). */
export type ApiPathParams<P extends keyof paths, M extends ApiMethod<P>> =
  Operation<P, M> extends { parameters: { path?: infer PP } }
    ? [PP] extends [Record<string, unknown>]
      ? { [K in keyof PP & string]: string }
      : Record<string, never>
    : Record<string, never>;

/**
 * Every response a handler for `M P` may return: the operation's 2xx JSON
 * body, a problem+json error, or no body at all (204, or a bare 5xx).
 */
type ApiResponseBody<P extends keyof paths, M extends ApiMethod<P>> = ApiSuccessBody<P, M> | Problem | null;

/**
 * MSW handler for one operation of api/openapi.yaml, checked against the
 * contract. Use it for every API mock instead of bare `http.*`:
 *
 * - `path` must be a key of `paths` and `method` one the contract defines;
 * - `params` has the operation's path parameters, `request.json()` its body type;
 * - the returned `HttpResponse.json(...)` must match the 2xx JSON schema of
 *   the operation, or be `problem(...)` (components.schemas.Problem), or have
 *   no body (`new HttpResponse(null, { status: 204 })`).
 *
 * @example
 * server.use(apiHandler("get", "/users/@me", () => HttpResponse.json(user)));
 * server.use(apiHandler("post", "/guilds/{guild_id}/bans/{user_id}", ({ params }) =>
 *   params.user_id === me.id ? problem(400, "cannot_target_self") : new HttpResponse(null, { status: 204 })));
 */
export function apiHandler<P extends keyof paths, M extends ApiMethod<P>>(
  method: M,
  path: P,
  resolver: HttpResponseResolver<ApiPathParams<P, M>, ApiRequestBody<P, M>, ApiResponseBody<P, M>>,
): HttpHandler {
  return http[method]<ApiPathParams<P, M>, ApiRequestBody<P, M>, ApiResponseBody<P, M>>(apiUrlPattern(path), resolver);
}

/** MSW URL pattern for a contract path: `"/guilds/{guild_id}"` → `"/api/v1/guilds/:guild_id"`. */
function apiUrlPattern(path: keyof paths): string {
  return `/api/v1${path.replace(/\{(\w+)\}/g, ":$1")}`;
}

/**
 * A problem+json response shaped like the server's (`components.schemas.Problem`),
 * with extra `headers` such as `Retry-After`.
 */
export function problem(
  status: number,
  code: string,
  errors?: FieldError[],
  headers?: Record<string, string>,
): HttpResponse<Problem> {
  const body: Problem = { type: `urn:zazzzor:error:${code}`, title: code, status, code };
  if (errors) body.errors = errors;
  return HttpResponse.json(body, { status, headers: { ...headers, "Content-Type": "application/problem+json" } });
}
