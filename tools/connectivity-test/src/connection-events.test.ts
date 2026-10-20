import { DisconnectReason } from "livekit-client";
import { countConnectionEvent, disconnectMessage, disconnectReasonName, emptyConnectionEvents } from "./connection-events";

test("counts disconnects with reason names, reconnect attempts and successes", () => {
  let e = emptyConnectionEvents();
  e = countConnectionEvent(e, { type: "reconnecting" });
  e = countConnectionEvent(e, { type: "reconnected" });
  e = countConnectionEvent(e, { type: "reconnecting" });
  e = countConnectionEvent(e, { type: "disconnected", reason: DisconnectReason.SIGNAL_CLOSE });
  e = countConnectionEvent(e, { type: "disconnected" });
  expect(e).toEqual({ disconnects: 2, reconnectAttempts: 2, reconnects: 1, disconnectReasons: ["SIGNAL_CLOSE", "UNKNOWN_REASON"] });
});

test("counting does not mutate the previous state", () => {
  const e = emptyConnectionEvents();
  countConnectionEvent(e, { type: "disconnected", reason: DisconnectReason.CLIENT_INITIATED });
  expect(e).toEqual(emptyConnectionEvents());
});

test("reason names come from the protocol enum", () => {
  expect(disconnectReasonName(DisconnectReason.DUPLICATE_IDENTITY)).toBe("DUPLICATE_IDENTITY");
  expect(disconnectReasonName(undefined)).toBe("UNKNOWN_REASON");
  expect(disconnectReasonName(999 as DisconnectReason)).toBe("UNKNOWN_REASON");
});

test("duplicate identity explains the second tab", () => {
  expect(disconnectMessage(DisconnectReason.DUPLICATE_IDENTITY)).toBe("Эта ссылка открыта в другой вкладке или на другом устройстве");
});

test("other reasons keep the technical name for the report", () => {
  expect(disconnectMessage(DisconnectReason.SIGNAL_CLOSE)).toBe("Соединение потеряно (SIGNAL_CLOSE)");
  expect(disconnectMessage(undefined)).toBe("Соединение потеряно (UNKNOWN_REASON)");
});
