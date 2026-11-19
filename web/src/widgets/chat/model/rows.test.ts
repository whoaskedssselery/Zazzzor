import type { PendingMessage } from "@/entities/message";
import { makeMessage, makeUser } from "@/shared/test";
import { buildRows, type ChatRow, GROUP_MS } from "./rows";

const anna = makeUser("1");
const petr = makeUser("2");
const C = "101";
const at = (iso: string) => ({ created_at: iso });
const now = new Date("2026-10-24T12:00:00");

const summary = (rows: ChatRow[]) =>
  rows.map((r) => {
    switch (r.kind) {
      case "message":
        return `${r.message.id}${r.grouped ? "+" : ""}`;
      case "pending":
        return `p:${r.pending.content}${r.grouped ? "+" : ""}`;
      case "day":
        return `[${r.label}]`;
      default:
        return r.kind;
    }
  });

describe("buildRows", () => {
  it("groups one author's messages within 7 minutes and starts a new group otherwise", () => {
    const base = new Date("2026-10-23T21:00:00").getTime();
    const t = (ms: number) => new Date(base + ms).toISOString();
    const rows = buildRows(
      [
        makeMessage("1", C, anna, at(t(0))),
        makeMessage("2", C, anna, at(t(60_000))),
        makeMessage("3", C, anna, at(t(60_000 + GROUP_MS))),
        makeMessage("4", C, petr, at(t(60_000 + GROUP_MS + 1000))),
        makeMessage("5", C, petr, { ...at(t(60_000 + GROUP_MS + 2000)), reply_to_id: "1" }),
      ],
      [],
      { hasOlder: false, meId: null, now },
    );
    expect(summary(rows)).toEqual(["start", "[23 октября 2026 г.]", "1", "2+", "3", "4", "5"]);
  });

  it("separates days and never groups across midnight", () => {
    const rows = buildRows(
      [
        makeMessage("1", C, anna, at(new Date("2026-10-23T23:58:00").toISOString())),
        makeMessage("2", C, anna, at(new Date("2026-10-24T00:01:00").toISOString())),
      ],
      [],
      { hasOlder: true, meId: null, now },
    );
    expect(summary(rows)).toEqual(["older", "[23 октября 2026 г.]", "1", "[24 октября 2026 г.]", "2"]);
  });

  it("never groups the messages of deleted accounts", () => {
    const t = new Date("2026-10-23T21:00:00").toISOString();
    const rows = buildRows([makeMessage("1", C, null, at(t)), makeMessage("2", C, null, at(t))], [], {
      hasOlder: false,
      meId: null,
      now,
    });
    expect(summary(rows).slice(2)).toEqual(["1", "2"]);
  });

  it("puts the messages being sent last, joining the user's own recent group", () => {
    const pending = (content: string): PendingMessage => ({
      nonce: content,
      channelId: C,
      content,
      replyTo: null,
      attachments: [],
      status: "sending",
      error: null,
      sentAt: Date.now(),
    });
    const recent = new Date(now.getTime() - 60_000).toISOString();
    const rows = buildRows([makeMessage("1", C, anna, at(recent))], [pending("а"), pending("б")], {
      hasOlder: false,
      meId: anna.id,
      now,
    });
    expect(summary(rows).slice(2)).toEqual(["1", "p:а+", "p:б+"]);
    const someoneElse = buildRows([makeMessage("1", C, petr, at(recent))], [pending("а")], {
      hasOlder: false,
      meId: anna.id,
      now,
    });
    expect(summary(someoneElse).slice(2)).toEqual(["1", "p:а"]);
  });

  it("draws the «Новые» line above the first message after the mark by someone else", () => {
    const messages = ["1", "2", "3", "4"].map((id) => makeMessage(id, C, id === "3" ? anna : petr));
    const line = (readFrom: string | null, hasOlder = false) =>
      summary(buildRows(messages, [], { hasOlder, meId: anna.id, now, readFrom })).filter((r) => r === "new");
    const rows = buildRows(messages, [], { hasOlder: false, meId: anna.id, now, readFrom: "2" });
    expect(summary(rows).slice(2)).toEqual(["1", "2+", "3", "new", "4"]);
    expect(line(null)).toEqual([]);
    // The mark is in history not loaded yet: where the line goes is not known.
    expect(line("0", true)).toEqual([]);
    expect(line("1", true)).toEqual(["new"]);
  });
});
