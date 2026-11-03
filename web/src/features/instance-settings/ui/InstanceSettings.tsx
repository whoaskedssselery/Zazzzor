import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ADMIN_SETTINGS_KEY, type InstanceSettings as Settings } from "@/entities/session";
import { api, errorText, unwrap } from "@/shared/api";
import { Switch, useToast } from "@/shared/ui";

/**
 * The instance's own settings for its administrators: for now whether new
 * accounts may register. A switch applies at once; the cached settings
 * (which are also the admin check) take the server's answer.
 */
export const InstanceSettings = ({ settings }: { settings: Settings }) => {
  const queryClient = useQueryClient();
  const toast = useToast();
  const update = useMutation({
    mutationFn: async (registration_open: boolean) =>
      unwrap(await api.PATCH("/admin/settings", { body: { registration_open } })),
    onSuccess: (next) => {
      queryClient.setQueryData(ADMIN_SETTINGS_KEY, next);
      toast.show({
        text: next.registration_open ? "Регистрация открыта" : "Регистрация закрыта",
        tone: "success",
      });
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
  const open = update.isPending ? update.variables : settings.registration_open;
  return (
    <section aria-labelledby="instance-title">
      <h2 id="instance-title" className="mb-5 text-xl font-semibold">
        Сервер Zazzzor
      </h2>
      <div className="flex items-start gap-4 rounded-lg bg-sidebar p-4">
        <div className="min-w-0 flex-1">
          <label htmlFor="registration-open" className="block text-base font-medium">
            Регистрация открыта
          </label>
          <p id="registration-open-hint" className="mt-1 text-sm text-fg-muted">
            {open
              ? "Любой, кто откроет сайт, может создать аккаунт."
              : "Новые аккаунты создать нельзя. Те, что уже есть, работают как прежде."}
          </p>
        </div>
        <Switch
          id="registration-open"
          aria-describedby="registration-open-hint"
          checked={open}
          disabled={update.isPending}
          onCheckedChange={(checked) => {
            update.mutate(checked);
          }}
        />
      </div>
    </section>
  );
};
