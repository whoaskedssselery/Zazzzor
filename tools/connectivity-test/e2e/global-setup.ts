import { liveKitLogs, removeStaleContainers, startLiveKit, stopLiveKit, waitForLiveKit } from "./livekit";

/** Starts a throwaway LiveKit for the run; the returned function always removes it. */
export default async function globalSetup(): Promise<() => Promise<void>> {
  removeStaleContainers();
  const name = startLiveKit();
  try {
    await waitForLiveKit();
  } catch (err) {
    console.error(liveKitLogs(name));
    stopLiveKit(name);
    throw err;
  }
  return async () => {
    if (process.env.E2E_LIVEKIT_LOGS) console.log(liveKitLogs(name));
    stopLiveKit(name);
  };
}
