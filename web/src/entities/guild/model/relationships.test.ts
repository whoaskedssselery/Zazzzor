import { makeRelationship, makeUser } from "@/shared/test";
import { friendLists, incomingCount } from "./relationships";

const anya = makeUser("1", { display_name: "Аня" });
const boris = makeUser("2", { display_name: "Борис" });
const vera = makeUser("3", { display_name: "Вера" });
const gleb = makeUser("4", { display_name: "Глеб" });
const dima = makeUser("5", { display_name: "Дима" });

const state = {
  relationships: {
    [boris.id]: makeRelationship(boris, 1),
    [anya.id]: makeRelationship(anya, 1),
    [vera.id]: makeRelationship(vera, 4),
    [gleb.id]: makeRelationship(gleb, 3),
    [dima.id]: makeRelationship(dima, 2),
  },
  presences: { [boris.id]: "online" as const, [vera.id]: "online" as const },
};

describe("friendLists", () => {
  it("lists friends by name, online ones apart, incoming requests before outgoing, and blocks", () => {
    const lists = friendLists(state);
    const names = (rs: { user: { display_name: string } }[]) => rs.map((r) => r.user.display_name);
    expect(names(lists.all)).toEqual(["Аня", "Борис"]);
    expect(names(lists.online)).toEqual(["Борис"]);
    expect(names(lists.pending)).toEqual(["Глеб", "Вера"]);
    expect(names(lists.blocked)).toEqual(["Дима"]);
  });

  it("counts the requests that wait on the user", () => {
    expect(incomingCount(state)).toBe(1);
  });
});
