export type ShareState =
  | { status: "idle" }
  | { status: "starting" }
  | { status: "sharing" }
  | { status: "error"; message: string };

export type ShareEvent =
  | { type: "start" }
  | { type: "started" }
  | { type: "cancelled" }
  | { type: "failed"; message: string }
  | { type: "stopped" };

export function shareReducer(state: ShareState, event: ShareEvent): ShareState {
  switch (event.type) {
    case "start":
      return state.status === "idle" || state.status === "error" ? { status: "starting" } : state;
    case "started":
      return state.status === "starting" ? { status: "sharing" } : state;
    case "cancelled":
      return state.status === "starting" ? { status: "idle" } : state;
    case "failed":
      return state.status === "starting" ? { status: "error", message: event.message } : state;
    case "stopped":
      return state.status === "sharing" ? { status: "idle" } : state;
  }
}
