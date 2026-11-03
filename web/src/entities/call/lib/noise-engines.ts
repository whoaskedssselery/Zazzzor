import rnnoiseWasmUrl from "@sapphi-red/web-noise-suppressor/rnnoise.wasm?url";
import rnnoiseSimdWasmUrl from "@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url";
import rnnoiseWorkletUrl from "@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url";
import type { NoiseEngines } from "./noise";

/**
 * The real engines, loaded on first use from this origin: DeepFilterNet3
 * (a 14.6 MB Wasm and an 8 MB model, cached by the browser after the first
 * call) and RNNoise (~0.3 MB).
 */
export const noiseEngines: NoiseEngines = {
  async deepFilterNet(ctx, onOverload) {
    const { createDeepFilterNetAudioWorklet } = await import("@workadventure/noise-suppression/deepfilternet");
    const handle = await createDeepFilterNetAudioWorklet(ctx, {
      bypassUntilReady: true,
      // 1 frame of look-ahead in the pause gate: about 50 ms in all, not 70.
      pauseGateLookaheadFrames: 1,
      // Levels the voice after the denoiser, instead of the browser's gain control (off in this mode).
      postGain: true,
      onOverload,
    });
    try {
      await handle.ready;
    } catch (e) {
      handle.dispose();
      throw e;
    }
    return handle;
  },

  async rnnoise(ctx) {
    const { loadRnnoise, RnnoiseWorkletNode } = await import("@sapphi-red/web-noise-suppressor");
    const [wasmBinary] = await Promise.all([
      loadRnnoise({ url: rnnoiseWasmUrl, simdUrl: rnnoiseSimdWasmUrl }),
      ctx.audioWorklet.addModule(rnnoiseWorkletUrl),
    ]);
    const node = new RnnoiseWorkletNode(ctx, { maxChannels: 1, wasmBinary });
    return {
      node,
      dispose() {
        node.destroy();
        node.disconnect();
      },
    };
  },
};
