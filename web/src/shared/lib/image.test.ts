import { IMAGE_TOO_LARGE, imageProblem, WRONG_IMAGE } from "./image";

const file = (type: string, size: number) => new File([new Uint8Array(size)], "f", { type });

describe("imageProblem", () => {
  it("takes PNG, JPEG, GIF and WebP up to 4 MB", () => {
    for (const type of ["image/png", "image/jpeg", "image/gif", "image/webp"]) {
      expect(imageProblem(file(type, 4 * 1024 * 1024))).toBeNull();
    }
  });

  it("refuses other types and larger files", () => {
    expect(imageProblem(file("image/svg+xml", 10))).toBe(WRONG_IMAGE);
    expect(imageProblem(file("image/png", 4 * 1024 * 1024 + 1))).toBe(IMAGE_TOO_LARGE);
  });
});
