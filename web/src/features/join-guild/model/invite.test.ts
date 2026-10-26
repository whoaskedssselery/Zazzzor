import { ApiError } from "@/shared/api";
import { inviteErrorText, parseInviteCode } from "./invite";

describe("parseInviteCode", () => {
  it("takes a bare code", () => {
    expect(parseInviteCode("AbCd1234")).toBe("AbCd1234");
    expect(parseInviteCode("  AbCd1234 \n")).toBe("AbCd1234");
  });

  it("takes an invite link of any host", () => {
    expect(parseInviteCode("https://chat.example.org/invite/AbCd1234")).toBe("AbCd1234");
    expect(parseInviteCode("http://192.168.1.5:8080/invite/AbCd1234/")).toBe("AbCd1234");
    expect(parseInviteCode("https://zz.duckdns.org/invite/AbCd1234?utm=x#top")).toBe("AbCd1234");
  });

  it("refuses anything else", () => {
    for (const input of [
      "",
      "AbCd123",
      "AbCd12345",
      "AbCd-234",
      "https://chat.example.org/AbCd1234",
      "https://chat.example.org/invite/AbCd1234/more",
      "https://chat.example.org/x/invite/AbCd1234",
      "javascript:alert(1)//invite/AbCd1234",
      "ftp://chat.example.org/invite/AbCd1234",
      "chat.example.org/invite/AbCd1234 extra",
    ]) {
      expect(parseInviteCode(input), input).toBeNull();
    }
  });
});

describe("inviteErrorText", () => {
  it("names a ban and a dead invite in the invite's own words", () => {
    expect(inviteErrorText(new ApiError(403, "banned"))).toBe("Вы заблокированы на этом сервере");
    expect(inviteErrorText(new ApiError(410, "invite_expired"))).toBe("Приглашение недействительно");
    expect(inviteErrorText(new ApiError(410, "invite_exhausted"))).toBe("Приглашение недействительно");
    expect(inviteErrorText(new ApiError(404, "not_found"))).toBe("Приглашение недействительно");
  });

  it("falls back to the common texts", () => {
    expect(inviteErrorText(new ApiError(400, "too_many_guilds"))).toBe("Достигнут предел числа серверов.");
    expect(inviteErrorText(new TypeError("fetch failed"))).toBe("Нет связи с сервером. Проверьте подключение.");
  });
});
