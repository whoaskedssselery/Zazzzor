import { screen } from "@testing-library/react";
import { renderUi } from "@/shared/test";
import { type Embed, Embeds } from "./Embeds";

const link = (over: Partial<Embed> = {}): Embed => ({
  url: "https://github.com/livekit/livekit",
  type: "link",
  site_name: "GitHub",
  title: "livekit/livekit",
  description: "Realtime stack",
  image: null,
  video: null,
  ...over,
});

describe("Embeds", () => {
  it("shows a page as a card whose title opens it in a new tab, without telling it where from", () => {
    renderUi(<Embeds embeds={[link()]} />);
    const card = screen.getByRole("article", { name: "livekit/livekit" });
    expect(card.textContent).toContain("GitHub");
    expect(card.textContent).toContain("Realtime stack");
    const title = screen.getByRole("link", { name: "livekit/livekit" });
    expect(title.getAttribute("href")).toBe("https://github.com/livekit/livekit");
    expect(title.getAttribute("target")).toBe("_blank");
    expect(title.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("names a card without a site by its host, and shows nothing for no embeds", () => {
    const { container } = renderUi(<Embeds embeds={[]} />);
    expect(container.textContent).toBe("");
    renderUi(<Embeds embeds={[link({ site_name: null, url: "https://www.example.org/a" })]} />);
    expect(screen.getByRole("article").textContent).toContain("example.org");
  });

  it("shows an image link as the image, sized before it loads", () => {
    renderUi(
      <Embeds
        embeds={[link({ type: "image", title: null, image: { url: "/api/v1/embeds/T", width: 1040, height: 700 } })]}
      />,
    );
    const img = screen.getByRole("img", { name: "Картинка по ссылке" });
    expect(img.getAttribute("src")).toBe("/api/v1/embeds/T");
    expect(img.style.width).toBe("520px");
  });

  it("loads the YouTube player only on the click", async () => {
    const { user } = renderUi(
      <Embeds
        embeds={[
          link({
            type: "video",
            title: "Клип",
            site_name: "YouTube",
            url: "https://youtu.be/dQw4w9WgXcQ",
            image: { url: "/api/v1/embeds/Y", width: 480, height: 360 },
            video: { url: "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", width: 560, height: 315 },
          }),
        ]}
      />,
    );
    expect(document.querySelector("iframe")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Смотреть: Клип" }));
    const frame = document.querySelector("iframe");
    expect(frame?.getAttribute("src")).toBe("https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?autoplay=1");
    expect(frame?.getAttribute("title")).toBe("Клип");
  });
});
