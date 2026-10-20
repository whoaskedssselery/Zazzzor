import { DisconnectReason } from "livekit-client";

/** Room connection events over one measurement phase. */
export interface ConnectionEvents {
  disconnects: number;
  /** RoomEvent.Reconnecting: the connection dropped and LiveKit started to recover it. */
  reconnectAttempts: number;
  /** RoomEvent.Reconnected: recovery succeeded. */
  reconnects: number;
  disconnectReasons: string[];
}

export type ConnectionEvent =
  | { type: "disconnected"; reason?: DisconnectReason }
  | { type: "reconnecting" }
  | { type: "reconnected" };

export function emptyConnectionEvents(): ConnectionEvents {
  return { disconnects: 0, reconnectAttempts: 0, reconnects: 0, disconnectReasons: [] };
}

export function countConnectionEvent(events: ConnectionEvents, event: ConnectionEvent): ConnectionEvents {
  switch (event.type) {
    case "disconnected":
      return { ...events, disconnects: events.disconnects + 1, disconnectReasons: [...events.disconnectReasons, disconnectReasonName(event.reason)] };
    case "reconnecting":
      return { ...events, reconnectAttempts: events.reconnectAttempts + 1 };
    case "reconnected":
      return { ...events, reconnects: events.reconnects + 1 };
  }
}

export function disconnectReasonName(reason: DisconnectReason | undefined): string {
  // Numeric TS enum: the reverse mapping gives the name.
  const name: unknown = reason === undefined ? undefined : DisconnectReason[reason];
  return typeof name === "string" ? name : "UNKNOWN_REASON";
}

const MESSAGES: Partial<Record<DisconnectReason, string>> = {
  [DisconnectReason.DUPLICATE_IDENTITY]: "Эта ссылка открыта в другой вкладке или на другом устройстве",
  [DisconnectReason.CLIENT_INITIATED]: "Ты отключился",
  [DisconnectReason.SERVER_SHUTDOWN]: "Сервер перезапускается, подключись снова через минуту",
  [DisconnectReason.PARTICIPANT_REMOVED]: "Тебя удалили из комнаты",
  [DisconnectReason.ROOM_DELETED]: "Комната закрыта",
  [DisconnectReason.ROOM_CLOSED]: "Комната закрыта",
};

export function disconnectMessage(reason: DisconnectReason | undefined): string {
  return (reason !== undefined ? MESSAGES[reason] : undefined) ?? `Соединение потеряно (${disconnectReasonName(reason)})`;
}
