import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { createQueryClient } from "../api/query-client";
import { createAppRouter } from "./router";

function renderAt(path: string) {
  const router = createAppRouter(createQueryClient(), createMemoryHistory({ initialEntries: [path] }));
  render(<RouterProvider router={router} />);
}

describe("router", () => {
  it("renders the start page at /", async () => {
    renderAt("/");
    expect(await screen.findByRole("heading", { name: "Zazzzor" })).toBeTruthy();
  });

  it("renders a Russian not-found page for unknown paths", async () => {
    renderAt("/channels/1/2/nope");
    expect(await screen.findByRole("heading", { name: "Страница не найдена" })).toBeTruthy();
  });
});
