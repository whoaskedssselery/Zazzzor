import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UiProviders } from "@/shared/ui";
import { type Attachment, attachmentKind, formatSize } from "../model/attachment";
import { Attachments } from "./Attachments";

const file = (id: string, filename: string, content_type: string, extra: Partial<Attachment> = {}): Attachment => ({
  id,
  filename,
  content_type,
  size: 2048,
  width: null,
  height: null,
  url: `/api/v1/attachments/101/${id}/${filename}`,
  pending: false,
  ...extra,
});

const show = (attachments: Attachment[]) =>
  render(
    <UiProviders>
      <Attachments attachments={attachments} />
    </UiProviders>,
  );

describe("attachments", () => {
  it("shows a file by the type the server sniffed, not its name", () => {
    expect(attachmentKind("image/png")).toBe("image");
    expect(attachmentKind("video/webm")).toBe("video");
    expect(attachmentKind("audio/mpeg")).toBe("audio");
    // An SVG or a page renamed to .png is served as a download.
    expect(attachmentKind("image/svg+xml")).toBe("file");
    expect(attachmentKind("application/octet-stream")).toBe("file");
  });

  it("writes sizes the Russian way", () => {
    expect(formatSize(512)).toBe("512 Б");
    expect(formatSize(1536)).toBe("1,5 КБ");
    expect(formatSize(25 * 1024 * 1024)).toBe("25 МБ");
  });

  it("keeps an image's shape in the feed before it loads, within 520×350", () => {
    show([file("1", "wide.png", "image/png", { width: 2080, height: 700 })]);
    const box = screen.getByRole("button", { name: "Открыть wide.png" });
    expect(box.style.width).toBe("520px");
    expect(box.style.aspectRatio).toBe("2080 / 700");
  });

  it("opens images over the window and moves between them", async () => {
    const user = userEvent.setup();
    show([
      file("1", "a.png", "image/png", { width: 10, height: 10 }),
      file("2", "b.png", "image/png", { width: 10, height: 10 }),
    ]);
    await user.click(screen.getByRole("button", { name: "Открыть a.png" }));
    const viewer = await screen.findByRole("dialog");
    expect(within(viewer).getByRole("img", { name: "a.png" })).toBeTruthy();
    await user.click(within(viewer).getByRole("button", { name: "Следующее изображение" }));
    expect(within(viewer).getByRole("img", { name: "b.png" })).toBeTruthy();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("gives other files a card with a download", () => {
    show([file("3", "report.pdf", "application/pdf")]);
    const link = screen.getByRole("link", { name: "Скачать report.pdf" });
    expect(link.getAttribute("href")).toBe("/api/v1/attachments/101/3/report.pdf");
    expect(link.getAttribute("download")).toBe("report.pdf");
    expect(screen.getByText("2 КБ")).toBeTruthy();
  });
});
