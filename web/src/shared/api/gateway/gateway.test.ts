import { renderHook, waitFor } from "@testing-library/react";
import { dispatchFrame, gateway, helloFrame, makeReady, makeUser, server } from "@/shared/test";
import { getGateway, useGatewayEvent } from "./gateway";

const me = makeUser("100");
const typing = { channel_id: "101", guild_id: "10", user_id: "200", timestamp: "2026-10-21T12:00:00.000Z" };

afterEach(() => {
  getGateway().stop();
});

/** A gateway that sends HELLO and READY, and hands the test a way to send more. */
const talkingGateway = () => {
  const sent: { send?: (data: string) => void } = {};
  server.use(
    gateway.addEventListener("connection", ({ client }) => {
      client.send(helloFrame());
      client.send(dispatchFrame("READY", 1, makeReady(me)));
      sent.send = (data) => {
        client.send(data);
      };
    }),
  );
  return (data: string) => {
    if (!sent.send) throw new Error("the gateway is not connected");
    sent.send(data);
  };
};

describe("getGateway", () => {
  it("is the one connection of the app", () => {
    expect(getGateway()).toBe(getGateway());
  });

  it("connects to the gateway of the page's own host", async () => {
    const urls: string[] = [];
    server.use(
      gateway.addEventListener("connection", ({ client }) => {
        urls.push(client.url.href);
      }),
    );
    getGateway().start();
    await waitFor(() => {
      expect(urls).toEqual([`ws://${window.location.host}/api/v1/gateway`]);
    });
  });
});

describe("useGatewayEvent", () => {
  it("calls the latest callback while the component is mounted, and nothing after", async () => {
    const send = talkingGateway();
    const first = vi.fn();
    const second = vi.fn();
    const { rerender, unmount } = renderHook(
      ({ fn }) => {
        useGatewayEvent("TYPING_START", fn);
      },
      {
        initialProps: { fn: first },
      },
    );
    getGateway().start();
    await waitFor(() => {
      expect(getGateway().status).toBe("ready");
    });

    send(dispatchFrame("TYPING_START", 2, typing));
    await waitFor(() => {
      expect(first).toHaveBeenCalledWith(typing);
    });

    rerender({ fn: second });
    send(dispatchFrame("TYPING_START", 3, typing));
    await waitFor(() => {
      expect(second).toHaveBeenCalledTimes(1);
    });
    expect(first).toHaveBeenCalledTimes(1);

    unmount();
    const probe = vi.fn();
    const off = getGateway().on("TYPING_START", probe);
    send(dispatchFrame("TYPING_START", 4, typing));
    await waitFor(() => {
      expect(probe).toHaveBeenCalledTimes(1);
    });
    expect(second).toHaveBeenCalledTimes(1);
    off();
  });
});
