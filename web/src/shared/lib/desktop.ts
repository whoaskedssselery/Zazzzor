/**
 * The desktop app (desktop/app) around this page. Tauri puts its IPC on
 * `window.__TAURI_INTERNALS__`; the app lets the pages of the server it was
 * pointed at call its screen share commands, and nothing else.
 */
interface TauriInternals {
  invoke(cmd: string, args?: Record<string, unknown>): Promise<unknown>;
}

const internals = (): TauriInternals | undefined =>
  (globalThis as { __TAURI_INTERNALS__?: TauriInternals }).__TAURI_INTERNALS__;

/** Whether the page runs inside the desktop app. */
export const inDesktopApp = (): boolean => internals() !== undefined;

/** Calls the app's command `cmd`; rejects outside the app and when the app refuses. */
export const desktopInvoke = async <T>(cmd: string, args?: Record<string, unknown>): Promise<T> => {
  const tauri = internals();
  if (!tauri) throw new Error("desktop: not in the desktop app");
  return (await tauri.invoke(cmd, args)) as T;
};
