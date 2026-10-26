import { fireEvent, render, screen } from "@testing-library/react";
import { Avatar, initials } from "./Avatar";

describe("Avatar", () => {
  it("shows initials without a picture and is named after the person", () => {
    render(<Avatar name="Аня Соколова" />);
    const avatar = screen.getByRole("img", { name: "Аня Соколова" });
    expect(avatar.textContent).toBe("АС");
  });

  it("uses the name as the picture's alt text", () => {
    render(<Avatar name="Дима" src="/api/v1/avatars/1.webp" />);
    const img = screen.getByRole("img", { name: "Дима" });
    expect(img.tagName).toBe("IMG");
    expect(img.getAttribute("alt")).toBe("Дима");
  });

  it("falls back to initials when the picture fails to load", () => {
    render(<Avatar name="Дима Орлов" src="/broken.webp" />);
    fireEvent.error(screen.getByRole("img", { name: "Дима Орлов" }));
    expect(screen.getByRole("img", { name: "Дима Орлов" }).textContent).toBe("ДО");
  });

  it("names the presence status for screen readers", () => {
    render(<Avatar name="Катя" status="online" />);
    expect(screen.getByRole("img", { name: "Катя, в сети" })).toBeTruthy();
  });

  it("takes initials from the first two words, ignoring extra spaces", () => {
    expect(initials("  максим   петров  иванов ")).toBe("МП");
    expect(initials("ksenia")).toBe("K");
    expect(initials("")).toBe("?");
  });
});
