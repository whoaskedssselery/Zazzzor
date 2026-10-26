import { render, screen } from "@testing-library/react";
import { formatCount } from "./Badge";
import { Button } from "./Button";
import { guildAcronym } from "./GuildIcon";
import { roleColorCss } from "./RoleColorDot";

describe("design helpers", () => {
  it("builds a server acronym from up to three words", () => {
    expect(guildAcronym("Вечерний сеанс")).toBe("ВС");
    expect(guildAcronym("  кооп по пятницам и выходным ")).toBe("КПП");
    expect(guildAcronym("")).toBe("?");
  });

  it("turns API role colours into CSS, 0 meaning none", () => {
    expect(roleColorCss(0)).toBeNull();
    expect(roleColorCss(0x3dba86)).toBe("#3dba86");
    expect(roleColorCss(0x0000ff)).toBe("#0000ff");
    expect(roleColorCss(0x1000000)).toBeNull();
  });

  it("caps counters at 99+", () => {
    expect(formatCount(7)).toBe("7");
    expect(formatCount(100)).toBe("99+");
  });
});

describe("Button", () => {
  it("is a non-submitting button by default and locks itself while loading", () => {
    render(<Button loading>Сохранить</Button>);
    const button = screen.getByRole("button", { name: "Сохранить" });
    expect(button.getAttribute("type")).toBe("button");
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(button.getAttribute("aria-busy")).toBe("true");
  });

  it("renders its child with asChild", () => {
    render(
      <Button asChild variant="link">
        <a href="/invite/abc">Открыть инвайт</a>
      </Button>,
    );
    expect(screen.getByRole("link", { name: "Открыть инвайт" }).className).toContain("text-link");
  });
});
