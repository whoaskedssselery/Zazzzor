import createClient, { type Client } from "openapi-fetch";
import { errorCodeText, genericErrorText, networkErrorText } from "./errors";
import type { paths } from "./schema.gen";

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
    /** Seconds from the Retry-After header (429, 503), when it is a positive integer. */
    readonly retryAfter?: number,
  ) {
    super(`${status} ${code}`);
  }
}

/** The data of a successful openapi-fetch result; throws ApiError otherwise. */
export function unwrap<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (!r.response.ok) {
    const p = parseProblem(r.error);
    throw new ApiError(r.response.status, p?.code ?? "unknown", p?.fields, retryAfterSeconds(r.response.headers));
  }
  // openapi-fetch leaves data undefined only for bodiless successes (204),
  // whose T is then `undefined` or `never` as the schema says.
  return r.data as T;
}

/** The text, in the interface's language, for any error a request can end with. */
export function errorText(e: unknown): string {
  if (e instanceof ApiError) {
    return errorCodeText(e.code) ?? genericErrorText();
  }
  // fetch rejects with a TypeError when no response arrives at all.
  if (e instanceof TypeError) return networkErrorText();
  return genericErrorText();
}

/**
 * Code and field errors of a problem+json body (components.schemas.Problem).
 * The body comes off the network, so it is checked rather than trusted: a
 * proxy error page or an empty body yields undefined.
 */
function parseProblem(body: unknown): { code: string; fields: Record<string, string> } | undefined {
  if (!isRecord(body) || typeof body.code !== "string") return undefined;
  const list: readonly unknown[] = Array.isArray(body.errors) ? body.errors : [];
  // Field names come off the network: "__proto__" or "constructor" must end
  // up as plain own keys, never touch a prototype. Object.fromEntries defines
  // own properties; the first message of a field wins.
  const seen = new Set<string>();
  const entries: [string, string][] = [];
  for (const e of list) {
    if (isRecord(e) && typeof e.field === "string" && typeof e.message === "string" && !seen.has(e.field)) {
      seen.add(e.field);
      entries.push([e.field, e.message]);
    }
  }
  return { code: body.code, fields: Object.fromEntries(entries) };
}

/**
 * The Retry-After of a response in seconds. The contract always sends whole
 * seconds (an integer >= 1); an HTTP date or anything else is ignored.
 */
function retryAfterSeconds(headers: Headers): number | undefined {
  const value = headers.get("Retry-After")?.trim() ?? "";
  if (!/^[0-9]{1,9}$/.test(value)) return undefined;
  const seconds = Number(value);
  return seconds >= 1 ? seconds : undefined;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}
