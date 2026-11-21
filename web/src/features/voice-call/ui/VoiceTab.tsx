import { RadioGroup } from "radix-ui";
import { useEffect, useState } from "react";
import { type NoiseStatus, readVoicePrefs, useCall, type VoicePrefs, writeVoicePrefs } from "@/entities/call";
import { onLanguage, t } from "@/shared/i18n";
import { inDesktopApp } from "@/shared/lib";
import { Button, cn, Field, Select } from "@/shared/ui";
import { applySwitchKeys, applyVoicePrefs, switchAudioDevice } from "../model/session";
import { comboName, comboOf } from "../model/switch-keys";

export let PTT_FOCUS_HINT = "";
export let PTT_GLOBAL_HINT = "";
export let SWITCH_FOCUS_HINT = "";
export let SWITCH_GLOBAL_HINT = "";
onLanguage(() => {
  PTT_FOCUS_HINT = t("voice:tab.pttFocus");
  PTT_GLOBAL_HINT = t("voice:tab.pttGlobal");
  SWITCH_FOCUS_HINT = t("voice:tab.switchFocus");
  SWITCH_GLOBAL_HINT = t("voice:tab.switchGlobal");
});

/** The switches a combination may flip, by the setting that keeps it. */
type KeyField = "muteKey" | "deafenKey";
const SWITCH_KEYS: { field: KeyField; label: string }[] = [
  {
    field: "muteKey",
    get label() {
      return t("voice:tab.switchMute");
    },
  },
  {
    field: "deafenKey",
    get label() {
      return t("voice:tab.switchDeafen");
    },
  },
];
const DEFAULT = "default";

const MODES: { value: VoicePrefs["mode"]; label: string; hint: string }[] = [
  {
    value: "voice",
    get label() {
      return t("voice:tab.modeVoice");
    },
    get hint() {
      return t("voice:tab.modeVoiceHint");
    },
  },
  {
    value: "ptt",
    get label() {
      return t("voice:tab.modePtt");
    },
    get hint() {
      return t("voice:tab.modePttHint");
    },
  },
];

const NOISE: { value: VoicePrefs["noise"]; label: string }[] = [
  {
    value: "enhanced",
    get label() {
      return t("voice:tab.noiseEnhanced");
    },
  },
  {
    value: "standard",
    get label() {
      return t("voice:tab.noiseStandard");
    },
  },
  {
    value: "off",
    get label() {
      return t("voice:tab.noiseOff");
    },
  },
];

/** What cleans the microphone in the call now, and why it is not what was chosen. */
const noiseNow = ({ running, reason }: NoiseStatus): string => {
  switch (running) {
    case "loading":
      return t("voice:tab.nowLoading");
    case "enhanced":
      return t("voice:tab.nowEnhanced");
    case "rnnoise":
      return reason === "overload" ? t("voice:tab.nowFallbackOverload") : t("voice:tab.nowFallbackFailed");
    case "standard":
      return reason === "failed" ? t("voice:tab.nowStandardFailed") : t("voice:tab.nowStandard");
    case "off":
      return reason === "failed" ? t("voice:tab.nowOffFailed") : t("voice:tab.nowOff");
  }
};

/** «KeyV» → «V», «Digit1» → «1»; other codes as they are («Space», «ShiftLeft»). */
const keyName = (code: string): string => code.replace(/^Key/, "").replace(/^Digit/, "");

const canChooseOutput = (): boolean => "setSinkId" in HTMLMediaElement.prototype;

/** MediaDevices with Firefox's output picker, which TypeScript's DOM types do not have yet. */
type WithOutputPicker = MediaDevices & { selectAudioOutput: () => Promise<MediaDeviceInfo> };

/** Firefox lists outputs only through its own picker. */
const hasOutputPicker = (): boolean => "mediaDevices" in navigator && "selectAudioOutput" in navigator.mediaDevices;

/**
 * The browser's microphones and speakers, read again when a device comes or
 * goes and when a call starts: names appear only once the page may use the
 * microphone, which joining asks for.
 */
const useDevices = () => {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([]);
  const inCall = useCall((s) => s.status !== "idle");
  useEffect(() => {
    if (!("mediaDevices" in navigator)) return;
    const media = navigator.mediaDevices;
    let live = true;
    const read = () => {
      media
        .enumerateDevices()
        .then((list) => {
          if (live) setDevices(list);
        })
        // Without the list the tab still offers the system's devices.
        .catch(() => undefined);
    };
    read();
    media.addEventListener("devicechange", read);
    return () => {
      live = false;
      media.removeEventListener("devicechange", read);
    };
  }, [inCall]);
  /** A device the browser handed over through its own list (Firefox's selectAudioOutput). */
  const add = (device: MediaDeviceInfo) => {
    setDevices((list) => [...list.filter((d) => d.deviceId !== device.deviceId), device]);
  };
  const options = (kind: MediaDeviceKind, unnamed: string) => [
    { value: DEFAULT, label: t("voice:tab.systemDefault") },
    ...devices
      .filter((d) => d.kind === kind && d.deviceId !== "" && d.deviceId !== DEFAULT)
      .map((d, i) => ({ value: d.deviceId, label: d.label || `${unnamed} ${String(i + 1)}` })),
  ];
  return {
    inputs: options("audioinput", t("voice:tab.microphone")),
    outputs: options("audiooutput", t("voice:tab.outputDevice")),
    add,
  };
};

/**
 * «Голос и видео»: the microphone and the speakers, voice activity or
 * push-to-talk with its key, and the noise suppression with what actually
 * runs in the call. Kept in this browser and applied to the call at once.
 */
export const VoiceTab = () => {
  const [prefs, setPrefs] = useState(readVoicePrefs);
  /** Which key the next press sets: push-to-talk's, or a switch's combination. */
  const [listening, setListening] = useState<"ptt" | KeyField | null>(null);
  const { inputs, outputs, add } = useDevices();
  const noise = useCall((s) => (s.status === "idle" ? null : s.noise));

  const chooseOutput = (deviceId: string) => {
    save({ ...readVoicePrefs(), outputId: deviceId === DEFAULT ? null : deviceId });
    void switchAudioDevice("audiooutput", deviceId);
  };

  const save = (next: VoicePrefs) => {
    setPrefs(next);
    writeVoicePrefs(next);
    applyVoicePrefs();
    applySwitchKeys();
  };

  // The next key pressed becomes the push-to-talk key, or the next combination
  // the switch's (a modifier alone waits for its key); Esc keeps the old one.
  useEffect(() => {
    if (listening === null) return;
    const target = listening;
    const onKey = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.code === "Escape") {
        setListening(null);
        return;
      }
      if (target === "ptt") {
        setListening(null);
        save({ ...readVoicePrefs(), pttKey: e.code });
        return;
      }
      const combo = comboOf(e);
      if (combo === null) return;
      setListening(null);
      save({ ...readVoicePrefs(), [target]: combo });
    };
    window.addEventListener("keydown", onKey, { capture: true });
    return () => {
      window.removeEventListener("keydown", onKey, { capture: true });
    };
  }, [listening]);

  return (
    <section aria-labelledby="voice-title" className="flex max-w-120 flex-col gap-6">
      <h2 id="voice-title" className="text-xl font-semibold">
        {t("voice:tab.title")}
      </h2>
      <div className="grid grid-cols-2 gap-4 max-sm:grid-cols-1">
        <Field htmlFor="voice-input" label={t("voice:tab.inputDevice")}>
          <Select
            options={inputs}
            value={prefs.inputId ?? DEFAULT}
            onValueChange={(v) => {
              save({ ...prefs, inputId: v === DEFAULT ? null : v });
              void switchAudioDevice("audioinput", v);
            }}
          />
        </Field>
        {canChooseOutput() && (
          <Field htmlFor="voice-output" label={t("voice:tab.outputDevice")}>
            <Select options={outputs} value={prefs.outputId ?? DEFAULT} onValueChange={chooseOutput} />
            {hasOutputPicker() && (
              <Button
                variant="link"
                size="sm"
                className="self-start"
                onClick={() => {
                  void (navigator.mediaDevices as WithOutputPicker)
                    .selectAudioOutput()
                    .then((device) => {
                      add(device);
                      chooseOutput(device.deviceId);
                    })
                    .catch(() => undefined);
                }}
              >
                {t("voice:tab.pickOutput")}
              </Button>
            )}
          </Field>
        )}
      </div>

      <div>
        <h3 id="voice-mode" className="mb-2 text-sm font-semibold text-fg-muted">
          {t("voice:tab.inputMode")}
        </h3>
        <RadioGroup.Root
          aria-labelledby="voice-mode"
          value={prefs.mode}
          onValueChange={(v) => {
            save({ ...prefs, mode: v === "ptt" ? "ptt" : "voice" });
          }}
          className="flex flex-col gap-2"
        >
          {MODES.map((m) => (
            <RadioGroup.Item
              key={m.value}
              value={m.value}
              aria-label={m.label}
              aria-describedby={`voice-mode-${m.value}`}
              className={cn(
                "group flex cursor-pointer items-center gap-3 rounded-md bg-control px-3 py-2.5 text-left transition-colors",
                "hover:bg-control-hover data-[state=checked]:bg-selected",
              )}
            >
              <span
                aria-hidden
                className="flex size-5 shrink-0 items-center justify-center rounded-full border-2 border-fg-muted group-data-[state=checked]:border-accent"
              >
                <RadioGroup.Indicator className="size-2.5 rounded-full bg-accent" />
              </span>
              <span className="min-w-0">
                <span className="block text-base font-medium">{m.label}</span>
                <span id={`voice-mode-${m.value}`} className="block text-sm text-fg-muted">
                  {m.hint}
                </span>
              </span>
            </RadioGroup.Item>
          ))}
        </RadioGroup.Root>
      </div>

      {prefs.mode === "ptt" && (
        <div>
          <h3 className="mb-2 text-sm font-semibold text-fg-muted">{t("voice:tab.key")}</h3>
          <div className="flex items-center gap-3">
            {listening === "ptt" ? (
              <span className="text-base text-fg">{t("voice:tab.pressKey")}</span>
            ) : (
              <kbd className="rounded-md bg-control px-2.5 py-1 font-mono text-base">{keyName(prefs.pttKey)}</kbd>
            )}
            <Button
              variant="secondary"
              size="sm"
              disabled={listening !== null}
              onClick={() => {
                setListening("ptt");
              }}
            >
              {t("voice:tab.changeKey")}
            </Button>
          </div>
          <p className="mt-2 text-sm text-fg-muted">{inDesktopApp() ? PTT_GLOBAL_HINT : PTT_FOCUS_HINT}</p>
        </div>
      )}

      <div>
        <h3 className="mb-2 text-sm font-semibold text-fg-muted">{t("voice:tab.hotkeys")}</h3>
        <div className="flex flex-col gap-2">
          {SWITCH_KEYS.map(({ field, label }) => {
            const combo = prefs[field];
            return (
              <div key={field} className="flex flex-wrap items-center gap-3">
                <span className="w-40 text-base">{label}</span>
                {listening === field ? (
                  <span className="text-base text-fg">{t("voice:tab.pressCombo")}</span>
                ) : (
                  <kbd
                    className={cn(
                      "rounded-md bg-control px-2.5 py-1 text-base",
                      combo === null ? "text-fg-muted" : "font-mono",
                    )}
                  >
                    {combo === null ? t("voice:tab.unassigned") : comboName(combo)}
                  </kbd>
                )}
                <Button
                  variant="secondary"
                  size="sm"
                  aria-label={t("voice:tab.changeOf", { label })}
                  disabled={listening !== null}
                  onClick={() => {
                    setListening(field);
                  }}
                >
                  {t("voice:tab.change")}
                </Button>
                {combo !== null && listening !== field && (
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={t("voice:tab.resetOf", { label })}
                    onClick={() => {
                      save({ ...readVoicePrefs(), [field]: null });
                    }}
                  >
                    {t("common:reset")}
                  </Button>
                )}
              </div>
            );
          })}
        </div>
        <p className="mt-2 text-sm text-fg-muted">{inDesktopApp() ? SWITCH_GLOBAL_HINT : SWITCH_FOCUS_HINT}</p>
      </div>

      <Field htmlFor="voice-noise" label={t("voice:tab.noise")} hint={t("voice:tab.noiseHint")}>
        <Select
          options={NOISE}
          value={prefs.noise}
          onValueChange={(v) => {
            save({ ...prefs, noise: NOISE.find((o) => o.value === v)?.value ?? "enhanced" });
          }}
        />
        {noise && (
          <p role="status" className="text-sm text-fg">
            {noiseNow(noise)}
          </p>
        )}
      </Field>
    </section>
  );
};
