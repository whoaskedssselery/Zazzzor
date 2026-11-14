import { fireEvent, screen } from "@testing-library/react";
import { renderUi } from "@/shared/test";
import { getVolumes, reloadVolumes } from "../model/volumes";
import { snapVolume, VolumeSlider } from "./VolumeSlider";

afterEach(() => {
  localStorage.clear();
  reloadVolumes();
});

describe("VolumeSlider", () => {
  it("starts at 100 % and goes from 0 to 200", () => {
    renderUi(<VolumeSlider kind="voice" userId="7" label="Громкость пользователя" />);
    const slider = screen.getByRole<HTMLInputElement>("slider", { name: "Громкость пользователя" });
    expect(slider.value).toBe("100");
    expect(slider.min).toBe("0");
    expect(slider.max).toBe("200");
    expect(screen.getByText("100%")).toBeTruthy();
  });

  it("sets the volume of that user's voice, and a double click returns it to 100 %", () => {
    renderUi(<VolumeSlider kind="voice" userId="7" label="Громкость пользователя" />);
    const slider = screen.getByRole("slider");
    fireEvent.change(slider, { target: { value: "170" } });
    expect(getVolumes()).toEqual({ voice: { "7": 170 }, stream: {} });
    expect(screen.getByText("170%")).toBeTruthy();
    fireEvent.doubleClick(slider);
    expect(getVolumes().voice).toEqual({});
  });

  it("keeps a screen share's volume apart from the voice's", () => {
    renderUi(<VolumeSlider inline kind="stream" userId="7" label="Громкость трансляции" />);
    fireEvent.change(screen.getByRole("slider", { name: "Громкость трансляции" }), { target: { value: "0" } });
    expect(getVolumes()).toEqual({ voice: {}, stream: { "7": 0 } });
  });

  it("lands a drag on 100 % when it is close; the keyboard steps freely", () => {
    expect([95, 96, 100, 104, 105].map(snapVolume)).toEqual([95, 100, 100, 100, 105]);
    renderUi(<VolumeSlider kind="voice" userId="7" label="Громкость пользователя" />);
    const slider = screen.getByRole("slider");
    fireEvent.change(slider, { target: { value: "101" } });
    expect(getVolumes().voice).toEqual({ "7": 101 });
    fireEvent.pointerDown(slider);
    fireEvent.change(slider, { target: { value: "103" } });
    expect(getVolumes().voice).toEqual({});
  });
});
