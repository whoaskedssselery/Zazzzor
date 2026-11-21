import { readdirSync, readFileSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { TRANSLATED } from "./resources";

// Vitest runs in web/ (`pnpm test` goes into each package).
const web = process.cwd();

/**
 * What is never read by a person using the app: the texts themselves,
 * tests and test helpers, generated types, and the dev-only design page
 * (/dev/design, left out of production builds), whose sample content is
 * Russian on purpose.
 */
const NOT_READ = /\.test\.tsx?$|\.gen\.ts$|\/locales\/|^src\/(shared|app)\/test\/|^src\/pages\/dev-design\//;

/** Files a person reads text from: the code, and the page around it. */
const sources = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name);
    if (e.isDirectory()) return sources(path);
    const rel = relative(web, path).split(sep).join("/");
    return /\.(ts|tsx)$/.test(rel) && !NOT_READ.test(rel) ? [rel] : [];
  });

/** Lines with Russian outside comments, as `file:line`. */
const russianLeft = (): string[] =>
  [...sources(join(web, "src")), "index.html"].flatMap((file) =>
    readFileSync(join(web, file), "utf8")
      .split("\n")
      .flatMap((line, i) =>
        /\p{Script=Cyrillic}/u.test(line) && !/^\s*(\/\/|\/\*|\*|\{\/\*)/.test(line)
          ? [`${file}:${String(i + 1)}`]
          : [],
      ),
  );

describe("the translation", () => {
  it("is offered only when no Russian is left in the code, and is once none is", () => {
    const left = russianLeft();
    const where = left.slice(0, 20).join("\n");
    // While text is left, TRANSLATED must stay false; once none is, it must turn true.
    expect(TRANSLATED, `${String(left.length)} lines with Russian left, e.g.:\n${where}`).toBe(left.length === 0);
  });
});
