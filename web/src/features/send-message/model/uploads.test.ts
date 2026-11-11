import { waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { type Attachment, MAX_ATTACHMENT_BYTES, MAX_ATTACHMENTS } from "@/entities/message";
import { server } from "@/shared/test";
import {
  addFiles,
  EMPTY_FILE,
  FILE_TOO_BIG,
  MAX_PARALLEL_UPLOADS,
  NO_ATTACH_PERMISSION,
  peekUploads,
  removeUpload,
  resetUploads,
  retryUpload,
  TOO_MANY_FILES,
  takeUploads,
  type Upload,
} from "./uploads";

const C = "101";

const attachment = (id: string, name: string): Attachment => ({
  id,
  filename: name,
  content_type: "image/png",
  size: 4,
  width: 2,
  height: 1,
  url: `/api/v1/attachments/${C}/${id}/${name}`,
  pending: true,
});

/** The composer's files of channel C. */
const uploads = (): Upload[] => peekUploads(C);

const png = (name = "shot.png", bytes = 4) => new File([new Uint8Array(bytes)], name, { type: "image/png" });

/** Answers uploads with a pending attachment named after the file; records each file name. */
const acceptUploads = () => {
  const names: string[] = [];
  server.use(
    http.post("/api/v1/channels/:channel_id/attachments", async ({ request }) => {
      // The multipart part `file` with the file name; jsdom's FormData does not parse back in Node.
      const body = await request.text();
      const name = /name="file"; filename="([^"]+)"/.exec(body)?.[1] ?? "?";
      names.push(name);
      return HttpResponse.json(attachment(String(names.length), name), { status: 201 });
    }),
  );
  return names;
};

afterEach(() => {
  resetUploads();
});

describe("uploads", () => {
  it("uploads a file at once and keeps the server's attachment", async () => {
    const names = acceptUploads();
    expect(addFiles(C, [png()])).toBeNull();
    expect(uploads()[0]?.status).toBe("uploading");
    await waitFor(() => {
      expect(uploads()[0]?.status).toBe("done");
    });
    expect(uploads()[0]?.attachment?.id).toBe("1");
    expect(names).toEqual(["shot.png"]);
  });

  it("uploads two at a time, as the server allows a user, and the rest after them", async () => {
    let inFlight = 0;
    let most = 0;
    let n = 0;
    server.use(
      http.post("/api/v1/channels/:channel_id/attachments", async () => {
        inFlight += 1;
        most = Math.max(most, inFlight);
        await new Promise((r) => setTimeout(r, 20));
        inFlight -= 1;
        n += 1;
        return HttpResponse.json(attachment(String(n), "x.png"), { status: 201 });
      }),
    );
    addFiles(C, [png("a.png"), png("b.png"), png("c.png")]);
    await waitFor(() => {
      expect(uploads().map((u) => u.status)).toEqual(["done", "done", "done"]);
    });
    expect(most).toBe(MAX_PARALLEL_UPLOADS);
  });

  it("leaves out empty files, files over 25 MB and files past ten, and says why", () => {
    acceptUploads();
    expect(addFiles(C, [png("empty.png", 0)])).toBe(EMPTY_FILE);
    const big = png("big.png", 1);
    Object.defineProperty(big, "size", { value: MAX_ATTACHMENT_BYTES + 1 });
    expect(addFiles(C, [big])).toBe(FILE_TOO_BIG);
    const many = Array.from({ length: MAX_ATTACHMENTS + 2 }, (_, i) => png(`${String(i)}.png`));
    expect(addFiles(C, many)).toBe(TOO_MANY_FILES);
    expect(uploads()).toHaveLength(MAX_ATTACHMENTS);
  });

  it("says the file is too big on 413 and why on a refusal, and retries", async () => {
    let answer: "big" | "forbidden" | "ok" = "big";
    server.use(
      http.post("/api/v1/channels/:channel_id/attachments", () => {
        if (answer === "big") return HttpResponse.json({ code: "file_too_large" }, { status: 413 });
        if (answer === "forbidden") return HttpResponse.json({ code: "missing_permission" }, { status: 403 });
        return HttpResponse.json(attachment("9", "shot.png"), { status: 201 });
      }),
    );
    addFiles(C, [png()]);
    await waitFor(() => {
      expect(uploads()[0]?.error).toBe(FILE_TOO_BIG);
    });
    answer = "forbidden";
    retryUpload(C, uploads()[0]?.key ?? "");
    await waitFor(() => {
      expect(uploads()[0]?.error).toBe(NO_ATTACH_PERMISSION);
    });
    answer = "ok";
    retryUpload(C, uploads()[0]?.key ?? "");
    await waitFor(() => {
      expect(uploads()[0]?.status).toBe("done");
    });
  });

  it("deletes an uploaded file on the server when it is taken out", async () => {
    acceptUploads();
    const deleted: string[] = [];
    server.use(
      http.delete("/api/v1/channels/:channel_id/attachments/:id", ({ params }) => {
        deleted.push(String(params.id));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    addFiles(C, [png()]);
    await waitFor(() => {
      expect(uploads()[0]?.status).toBe("done");
    });
    removeUpload(C, uploads()[0]?.key ?? "");
    expect(uploads()).toEqual([]);
    await waitFor(() => {
      expect(deleted).toEqual(["1"]);
    });
  });

  it("hands the uploaded files to a message and keeps the failed ones", async () => {
    let n = 0;
    server.use(
      http.post("/api/v1/channels/:channel_id/attachments", () => {
        n += 1;
        return n === 1
          ? HttpResponse.json(attachment("1", "a.png"), { status: 201 })
          : HttpResponse.json({ code: "server_busy" }, { status: 503 });
      }),
    );
    addFiles(C, [png("a.png")]);
    await waitFor(() => {
      expect(uploads()[0]?.status).toBe("done");
    });
    addFiles(C, [png("b.png")]);
    await waitFor(() => {
      expect(uploads()[1]?.status).toBe("failed");
    });
    expect(takeUploads(C).map((a) => a.id)).toEqual(["1"]);
    expect(uploads().map((u) => u.file.name)).toEqual(["b.png"]);
  });
});
