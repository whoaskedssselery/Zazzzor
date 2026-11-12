import { type GuildsState, RELATIONSHIP, type Relationship } from "./reducer";

/** The user's ties sorted for the friends screen. */
export interface FriendLists {
  /** Friends online now. */
  online: Relationship[];
  /** Every friend. */
  all: Relationship[];
  /** Requests: incoming first, as they wait on the user, then outgoing. */
  pending: Relationship[];
  blocked: Relationship[];
}

const byName = (a: Relationship, b: Relationship) =>
  a.user.display_name.localeCompare(b.user.display_name, "ru") || a.user.username.localeCompare(b.user.username);

/** Splits the user's relationships into the friends screen's lists, each by name. */
export function friendLists(s: Pick<GuildsState, "relationships" | "presences">): FriendLists {
  const list = Object.values(s.relationships).sort(byName);
  const all = list.filter((r) => r.type === RELATIONSHIP.FRIEND);
  return {
    online: all.filter((r) => (s.presences[r.id] ?? "offline") !== "offline"),
    all,
    pending: [
      ...list.filter((r) => r.type === RELATIONSHIP.INCOMING),
      ...list.filter((r) => r.type === RELATIONSHIP.OUTGOING),
    ],
    blocked: list.filter((r) => r.type === RELATIONSHIP.BLOCKED),
  };
}

/** How many friend requests wait on the user, for the badges of home. */
export const incomingCount = (s: Pick<GuildsState, "relationships">): number =>
  Object.values(s.relationships).filter((r) => r.type === RELATIONSHIP.INCOMING).length;
