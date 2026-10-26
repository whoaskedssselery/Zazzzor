import { HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { components, paths } from "../api/schema.gen";

type Problem = components["schemas"]["Problem"];
type FieldError = components["schemas"]["FieldError"];

/** One MSW server for every test file; handlers are added per test with `server.use`. */
export const server = setupServer();

/**
 * MSW path for an operation of the contract: `apiPath("/guilds/{guild_id}")`
 * → `"/api/v1/guilds/:guild_id"`. Only paths that exist in the schema compile.
 */
export function apiPath(path: keyof paths): string {
  return `/api/v1${path.replace(/\{(\w+)\}/g, ":$1")}`;
}

/** A problem+json response shaped like the server's (`components.schemas.Problem`). */
export function problem(status: number, code: string, errors?: FieldError[]): HttpResponse<Problem> {
  const body: Problem = { type: `urn:zazzzor:error:${code}`, title: code, status, code };
  if (errors) body.errors = errors;
  return HttpResponse.json(body, { status, headers: { "Content-Type": "application/problem+json" } });
}
