import { Button, Switch } from "@/shared/ui";
import { useNotificationPermission } from "../model/permission";
import { setNotifyPrefs, useNotifyPrefs } from "../model/prefs";
import { playMentionSound } from "../model/sound";

const Row = ({
  id,
  label,
  hint,
  checked,
  disabled = false,
  onChange,
}: {
  id: string;
  label: string;
  hint: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (on: boolean) => void;
}) => (
  <div className="flex items-start gap-4 py-3">
    <div className="min-w-0 flex-1">
      <label htmlFor={id} className="block text-base font-medium">
        {label}
      </label>
      <p id={`${id}-hint`} className="mt-1 text-sm text-fg-muted">
        {hint}
      </p>
    </div>
    <Switch id={id} aria-describedby={`${id}-hint`} checked={checked} disabled={disabled} onCheckedChange={onChange} />
  </div>
);

/** What the browser allows, in words, with the way to change it. */
const PermissionNote = () => {
  const { permission, request } = useNotificationPermission();
  switch (permission) {
    case "granted":
      return null;
    case "unsupported":
      return <p className="mt-2 text-sm text-fg-muted">Этот браузер не умеет показывать уведомления.</p>;
    case "denied":
      return (
        <p className="mt-2 text-sm text-fg-danger">
          Браузер запретил уведомления для этого сайта. Разрешите их в настройках сайта (значок слева от адреса) и
          вернитесь сюда.
        </p>
      );
    case "default":
      return (
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <p className="text-sm text-fg-muted">Браузер ещё не разрешил уведомления.</p>
          <Button
            size="sm"
            onClick={() => {
              void request();
            }}
          >
            Разрешить
          </Button>
        </div>
      );
  }
};

/**
 * «Уведомления»: how a mention reaches the user on this device. The choice
 * is kept in this browser, as each device may want its own.
 */
export const NotificationsTab = () => {
  const prefs = useNotifyPrefs();
  return (
    <section aria-labelledby="notify-title">
      <h2 id="notify-title" className="mb-2 text-xl font-semibold">
        Уведомления
      </h2>
      <p className="mb-3 text-sm text-fg-muted">
        О сообщениях, где упомянули вас, вашу роль, @everyone или @here. Пока вы смотрите этот канал, они не беспокоят.
      </p>
      <div className="max-w-140 divide-y divide-border">
        <div>
          <Row
            id="notify-desktop"
            label="Уведомления на рабочем столе"
            hint="Когда окно Zazzzor свёрнуто или в фоне. Клик открывает канал."
            checked={prefs.desktop}
            onChange={(desktop) => {
              setNotifyPrefs({ desktop });
            }}
          />
          {prefs.desktop && (
            <div className="pb-3">
              <PermissionNote />
            </div>
          )}
        </div>
        <Row
          id="notify-sound"
          label="Звук"
          hint="Короткий сигнал, даже когда открыт другой канал."
          checked={prefs.sound}
          onChange={(sound) => {
            setNotifyPrefs({ sound });
            if (sound) playMentionSound();
          }}
        />
      </div>
    </section>
  );
};
