import { screen, waitFor } from "@testing-library/react";
import { applyGatewayDispatch, resetGuilds } from "@/entities/guild";
import { makeChannel, makeGuildState, makeReady, makeUser, renderRouted } from "@/shared/test";
import { resetChannelUi, useChannelUi } from "./ui";
import { useNavigateToFallback } from "./useNavigateToFallback";

const me = makeUser("100");

const Go = ({ guildId }: { guildId: string | null }) => {
  const go = useNavigateToFallback();
  return (
    <button
      type="button"
      onClick={() => {
        go(guildId);
      }}
    >
      Перейти
    </button>
  );
};

beforeEach(() => {
  applyGatewayDispatch(
    "READY",
    makeReady(me, [
      makeGuildState("10", me, {
        channels: [makeChannel("101", "10", { position: 0 }), makeChannel("102", "10", { position: 1 })],
      }),
    ]),
  );
});

afterEach(() => {
  resetGuilds();
  resetChannelUi();
});

describe("useNavigateToFallback", () => {
  it("opens the first visible text channel of the server", async () => {
    const { user, router } = await renderRouted(<Go guildId="10" />, { path: "/channels/10/999" });
    await user.click(await screen.findByRole("button", { name: "Перейти" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/10/101");
    });
  });

  it("opens the channel last opened in the server while it is visible", async () => {
    useChannelUi.getState().setLastChannel("10", "102");
    const { user, router } = await renderRouted(<Go guildId="10" />, { path: "/channels/@me" });
    await user.click(await screen.findByRole("button", { name: "Перейти" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/10/102");
    });
  });

  it("goes home when the server is gone", async () => {
    const { user, router } = await renderRouted(<Go guildId="20" />, { path: "/channels/20/201" });
    await user.click(await screen.findByRole("button", { name: "Перейти" }));
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/channels/@me");
    });
  });
});
