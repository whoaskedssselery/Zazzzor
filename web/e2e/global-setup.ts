import { API_PORT } from "./env";

/** Fails fast, with what to run, when the API of e2e/compose.yml is not up. */
export default async function globalSetup(): Promise<void> {
  const url = `http://127.0.0.1:${API_PORT}/api/v1/readyz`;
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(5_000) });
    if (res.ok) return;
    throw new Error(`status ${String(res.status)}`);
  } catch (e) {
    throw new Error(`The e2e API is not ready at ${url} (${String(e)}). Start it: pnpm e2e:up`);
  }
}
