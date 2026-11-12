import { useMutation } from "@tanstack/react-query";
import { api, type components, errorText, unwrap } from "@/shared/api";
import { useToast } from "@/shared/ui";
import type { NotifySettings } from "../model/reducer";
import { applyGatewayDispatch } from "../model/store";

export type NotifyPatch = components["schemas"]["UpdateUserGuildSettingsRequest"];

// The server tells every session of the user by USER_GUILD_SETTINGS_UPDATE;
// this one takes the answer at once, so a menu shows the change before the
// event arrives (the event changes nothing more).

/** Changes the settings of server `guildId`, or of the private channels when null. */
export const updateNotify = async (guildId: string | null, patch: NotifyPatch): Promise<NotifySettings> => {
  const settings = unwrap(
    await api.PATCH("/users/@me/guilds/{guild_id}/settings", {
      params: { path: { guild_id: guildId ?? "@me" } },
      body: patch,
    }),
  );
  applyGatewayDispatch("USER_GUILD_SETTINGS_UPDATE", settings);
  return settings;
};

/** Changing notification settings; a refusal is a toast. */
export const useUpdateNotify = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: ({ guildId, patch }: { guildId: string | null; patch: NotifyPatch }) => updateNotify(guildId, patch),
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};
