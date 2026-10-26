import createClient, { type Client } from "openapi-fetch";
import type { paths } from "./schema.gen";
import { ERROR_TEXTS, GENERIC_ERROR, NETWORK_ERROR } from "./errors";

/**
 * The REST client. Same origin as the page (Caddy serves both; Vite proxies
 * in dev), with the HttpOnly session cookie. `fetch` is looked up per call so
 * test interceptors installed after import still see the requests.
 */
export const api: Client<paths> = createClient<paths>({
  baseUrl: "/api/v1",
  credentials: "include",
  fetch: (request) => globalThis.fetch(request),
});

/** An error response of the API. `code` is "unknown" when the body is not a problem. */
export class ApiError extends Error {
  override readonly name = "ApiError";

  constructor(
    readonly status: number,
    readonly code: string,
    /** Messages of `errors[]` by request field (snake_case, dotted for nesting). */
    readonly fields: Record<string, string> = {},
  ) {
    super(`${status} ${code}`);
  }
}

/** The data of a successful openapi-fetch result; throws ApiError otherwise. */
export function unwrap<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (!r.response.ok) {
    const p = parseProblem(r.error);
    throw new ApiError(r.response.status, p?.code ?? "unknown", p?.fields);
  }
  // openapi-fetch leaves data undefined only for bodiless successes (204),
  // whose T is then `undefined` or `never` as the schema says.
  return r.data as T;
}

/** Russian text for any error a request can end with. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    return Object.hasOwn(ERROR_TEXTS, e.code) ? (ERROR_TEXTS[e.code] ?? GENERIC_ERROR) : GENERIC_ERROR;
  }
  // fetch rejects with a TypeError when no response arrives at all.
  if (e instanceof TypeError) return NETWORK_ERROR;
  return GENERIC_ERROR;
}

/**
 * Code and field errors of a problem+json body (components.schemas.Problem).
 * The body comes off the network, so it is checked rather than trusted: a
 * proxy error page or an empty body yields undefined.
 */
function parseProblem(body: unknown): { code: string; fields: Record<string, string> } | undefined {
  if (!isRecord(body) || typeof body.code !== "string") return undefined;
  const fields: Record<string, string> = {};
  if (Array.isArray(body.errors)) {
    for (const e of body.errors as unknown[]) {
      if (isRecord(e) && typeof e.field === "string" && typeof e.message === "string" && !(e.field in fields)) {
        fields[e.field] = e.message;
      }
    }
  }
  return { code: body.code, fields };
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}
