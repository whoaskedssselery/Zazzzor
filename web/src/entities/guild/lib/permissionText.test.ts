import { PERM } from "../model/permissions";
import { CHANNEL_PERMISSION_GROUPS, PERMISSION_GROUPS } from "./permissionText";

const bitsOf = (groups: typeof PERMISSION_GROUPS) => groups.flatMap((g) => g.items.map((i) => i.bit));

describe("PERMISSION_GROUPS", () => {
  it("names every permission exactly once, in the five groups", () => {
    const bits = bitsOf(PERMISSION_GROUPS);
    expect(new Set(bits).size).toBe(bits.length);
    expect([...bits].sort()).toEqual(Object.values(PERM).sort());
    expect(PERMISSION_GROUPS.map((g) => g.title)).toEqual([
      "Общие",
      "Участники",
      "Текстовые каналы",
      "Голосовые каналы",
      "Сервер",
    ]);
  });

  it("leaves server-only permissions, ADMINISTRATOR first of all, out of channel overwrites", () => {
    const channel = bitsOf(CHANNEL_PERMISSION_GROUPS);
    for (const bit of [PERM.ADMINISTRATOR, PERM.MANAGE_GUILD, PERM.KICK_MEMBERS, PERM.BAN_MEMBERS]) {
      expect(channel).not.toContain(bit);
    }
    expect(channel).toContain(PERM.VIEW_CHANNEL);
    expect(channel).toContain(PERM.CONNECT);
  });
});
