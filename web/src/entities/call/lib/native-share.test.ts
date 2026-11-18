import { decodeThumbnail } from "./native-share";

describe("the desktop app's thumbnail", () => {
  it("reads the size, then the RGBA rows", () => {
    const bytes = new Uint8Array([2, 0, 0, 0, 1, 0, 0, 0, 1, 2, 3, 255, 4, 5, 6, 255]);
    const thumb = decodeThumbnail(bytes.buffer);
    expect([thumb.width, thumb.height]).toEqual([2, 1]);
    expect([...thumb.rgba]).toEqual([1, 2, 3, 255, 4, 5, 6, 255]);
  });
});
