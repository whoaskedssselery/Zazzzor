import { Bell, Copy, LogOut, Pencil, Settings, Trash2, UserPlus, Volume2 } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { Avatar } from "../design/components/Avatar";
import { Badge } from "../design/components/Badge";
import { Button } from "../design/components/Button";
import { Checkbox } from "../design/components/Checkbox";
import { ConfirmDialog } from "../design/components/ConfirmDialog";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "../design/components/ContextMenu";
import { Dialog, DialogClose, DialogContent, DialogTrigger } from "../design/components/Dialog";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../design/components/DropdownMenu";
import { GuildIcon } from "../design/components/GuildIcon";
import { IconButton } from "../design/components/IconButton";
import { Field, Input } from "../design/components/Input";
import { RoleColorDot } from "../design/components/RoleColorDot";
import { Select } from "../design/components/Select";
import { Skeleton } from "../design/components/Skeleton";
import { Spinner } from "../design/components/Spinner";
import { Switch } from "../design/components/Switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../design/components/Tabs";
import { Textarea } from "../design/components/Textarea";
import { useToast } from "../design/components/Toast";
import { Tooltip } from "../design/components/Tooltip";
import { useTheme, type ThemePreference } from "../design/theme";
import { MainScreenMock } from "./dev.design-mock";

/**
 * /dev/design, dev builds only (see router.ts): the main screen mocked on
 * made-up data, then every primitive in its states. The theme switch in the
 * top bar flips the whole page.
 */
export function DesignPage() {
  return (
    <div className="flex min-h-dvh flex-col bg-chat">
      <DevBar />
      <div className="h-[calc(100dvh-2.75rem)] min-h-150 shrink-0">
        <MainScreenMock />
      </div>
      <Showcase />
    </div>
  );
}

const themeLabels: Record<ThemePreference, string> = { dark: "Тёмная", light: "Светлая", system: "Как в системе" };

function DevBar() {
  const { theme, setTheme } = useTheme();
  return (
    <header className="flex h-11 shrink-0 items-center gap-4 bg-rail px-4">
      <h1 className="text-base font-semibold">Дизайн-система</h1>
      <p className="text-sm text-fg-faint">Только в dev-сборке. Данные выдуманы.</p>
      <Tabs
        value={theme}
        onValueChange={(v) => {
          if (v === "dark" || v === "light" || v === "system") setTheme(v);
        }}
        className="ml-auto"
      >
        <TabsList aria-label="Тема">
          {(Object.keys(themeLabels) as ThemePreference[]).map((t) => (
            <TabsTrigger key={t} value={t} className="h-7">
              {themeLabels[t]}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
    </header>
  );
}

function Showcase() {
  return (
    <div className="mx-auto flex w-full max-w-300 flex-col gap-12 px-6 py-12">
      <Section title="Кнопки" note="Основная — одна на экран. Опасная — для удаления, кика и бана.">
        <Row>
          <Button>Создать сервер</Button>
          <Button variant="secondary">Отмена</Button>
          <Button variant="ghost">Пропустить</Button>
          <Button variant="danger">Удалить канал</Button>
          <Button variant="link">Забыли пароль?</Button>
        </Row>
        <Row>
          <Button size="sm">Маленькая</Button>
          <Button>Обычная</Button>
          <Button size="lg">Крупная</Button>
          <Button loading>Сохраняем</Button>
          <Button disabled>Недоступна</Button>
        </Row>
      </Section>

      <Section title="Иконки-кнопки" note="Всегда с подписью: она же озвучивается и всплывает подсказкой.">
        <Row>
          <IconButton label="Настройки" icon={Settings} />
          <IconButton label="Уведомления" icon={Bell} />
          <IconButton label="Пригласить" icon={UserPlus} />
          <IconButton label="Звук включён" icon={Volume2} pressed />
          <IconButton label="Удалить" icon={Trash2} variant="danger" />
          <IconButton label="Недоступно" icon={Pencil} disabled />
          <IconButton label="Изменить" icon={Pencil} size="sm" />
        </Row>
      </Section>

      <Section title="Поля" note="Подпись над полем, подсказка или ошибка под ним.">
        <Fields />
      </Section>

      <Section title="Выбор" note="Выпадающий список, флажок и переключатель.">
        <Choices />
      </Section>

      <Section title="Вкладки">
        <Tabs defaultValue="overview">
          <TabsList aria-label="Настройки сервера">
            <TabsTrigger value="overview">Обзор</TabsTrigger>
            <TabsTrigger value="roles">Роли</TabsTrigger>
            <TabsTrigger value="members">Участники</TabsTrigger>
            <TabsTrigger value="bans" disabled>
              Баны
            </TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="pt-3 text-sm text-fg-muted">
            Название, иконка и системный канал сервера.
          </TabsContent>
          <TabsContent value="roles" className="pt-3 text-sm text-fg-muted">
            Роли и их права.
          </TabsContent>
          <TabsContent value="members" className="pt-3 text-sm text-fg-muted">
            Список участников.
          </TabsContent>
        </Tabs>
      </Section>

      <Section title="Метки и роли">
        <Row>
          <Badge variant="count">3</Badge>
          <Badge variant="count">99+</Badge>
          <Badge>Владелец</Badge>
          <Badge variant="accent">Новое</Badge>
          <Badge variant="live">В эфире</Badge>
        </Row>
        <Row>
          <RoleLine color={0xd9944a} name="Админ" />
          <RoleLine color={0x5fa8d3} name="Киноклуб" />
          <RoleLine color={0x3dba86} name="Игроки" />
          <RoleLine color={0} name="@everyone" />
        </Row>
      </Section>

      <Section title="Аватары и серверы" note="Без картинки — инициалы на тоне от имени. Точка — статус.">
        <Row>
          <Avatar name="Аня Соколова" size={80} status="online" />
          <Avatar name="Дима Орлов" size={40} status="online" />
          <Avatar name="Катя" size={40} status="offline" />
          <Avatar name="Лёша Ким" size={32} status="online" />
          <Avatar name="Оля Белова" size={24} />
          <Avatar name="Паша" size={20} />
        </Row>
        <Row className="bg-rail p-3">
          <span className="group">
            <GuildIcon name="Вечерний сеанс" active />
          </span>
          <span className="group">
            <GuildIcon name="Кооп по пятницам" />
          </span>
          <span className="group">
            <GuildIcon name="Майнкрафт" />
          </span>
          <span className="group">
            <GuildIcon name="Дача" size={40} />
          </span>
          <span className="text-sm text-fg-faint">Наведите: круг становится квадратом.</span>
        </Row>
      </Section>

      <Section title="Загрузка" note="Заглушки в размер настоящего содержимого, без мерцающих градиентов.">
        <Row>
          <Spinner label="Загрузка" />
          <Spinner label="Загрузка" size={24} />
        </Row>
        <div className="flex flex-col gap-5" aria-busy="true" aria-label="Сообщения загружаются">
          {[0.62, 0.41, 0.8].map((w) => (
            <div key={w} className="flex gap-4">
              <Skeleton className="size-10 shrink-0 rounded-full" />
              <div className="flex flex-1 flex-col gap-2 pt-1">
                <Skeleton className="h-4 w-32 rounded-sm" />
                <Skeleton className="h-4 rounded-sm" style={{ width: `${String(w * 100)}%` }} />
              </div>
            </div>
          ))}
        </div>
      </Section>

      <Section title="Диалоги, меню, подсказки" note="Всё открывается и закрывается с клавиатуры; Esc возвращает фокус.">
        <Overlays />
      </Section>

      <Section title="Уведомления">
        <Toasts />
      </Section>

      <Section title="Палитра" note="Токены из tokens.css; переключите тему вверху.">
        <Palette />
      </Section>
    </div>
  );
}

function Section({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <section className="grid grid-cols-1 gap-4 md:grid-cols-[16rem_minmax(0,1fr)] md:gap-8">
      <div className="pt-1">
        <h2 className="text-lg font-semibold">{title}</h2>
        {note && <p className="mt-1 text-sm text-fg-muted">{note}</p>}
      </div>
      <div className="flex flex-col gap-5 rounded-lg bg-sidebar p-5">{children}</div>
    </section>
  );
}

function Row({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={`flex flex-wrap items-center gap-3 rounded-md ${className ?? ""}`}>{children}</div>;
}

function RoleLine({ color, name }: { color: number; name: string }) {
  return (
    <span className="inline-flex items-center gap-2 text-sm font-medium">
      <RoleColorDot color={color} />
      {name}
    </span>
  );
}

function Fields() {
  const id = useId();
  return (
    <div className="grid gap-5 sm:grid-cols-2">
      <Field htmlFor={`${id}-name`} label="Отображаемое имя" hint="Его видят все на ваших серверах.">
        <Input id={`${id}-name`} defaultValue="Максим" />
      </Field>
      <Field htmlFor={`${id}-login`} label="Логин" error="Этот логин уже занят." errorId={`${id}-login-error`}>
        <Input id={`${id}-login`} defaultValue="maksim" aria-invalid aria-describedby={`${id}-login-error`} />
      </Field>
      <Field htmlFor={`${id}-invite`} label="Ссылка-приглашение">
        <Input id={`${id}-invite`} placeholder="https://zazzzor.duckdns.org/invite/…" />
      </Field>
      <Field htmlFor={`${id}-off`} label="Недоступно">
        <Input id={`${id}-off`} defaultValue="Только для владельца" disabled />
      </Field>
      <Field htmlFor={`${id}-topic`} label="Тема канала" hint="До 1024 символов." className="sm:col-span-2">
        <Textarea id={`${id}-topic`} defaultValue="Болтаем обо всём. Кино — по пятницам в 21:00 в «Кинозале»." />
      </Field>
    </div>
  );
}

function Choices() {
  const id = useId();
  const [notify, setNotify] = useState(true);
  return (
    <div className="grid gap-5 sm:grid-cols-2">
      <Field htmlFor={`${id}-select`} label="Уведомления сервера">
        <Select
          id={`${id}-select`}
          defaultValue="mentions"
          options={[
            { value: "all", label: "Все сообщения" },
            { value: "mentions", label: "Только упоминания" },
            { value: "none", label: "Ничего" },
          ]}
        />
      </Field>
      <div className="flex flex-col gap-3 pt-7">
        <label className="flex cursor-pointer items-center gap-3 text-base">
          <Checkbox defaultChecked /> Показывать, во что я играю
        </label>
        <label className="flex cursor-pointer items-center gap-3 text-base">
          <Checkbox /> Скрывать спойлеры
        </label>
        <label className="flex items-center gap-3 text-base text-fg-faint">
          <Checkbox disabled /> Недоступно
        </label>
      </div>
      <div className="flex items-center justify-between gap-4 sm:col-span-2">
        <label htmlFor={`${id}-switch`} className="cursor-pointer">
          <span className="block text-base font-medium">Звук уведомлений</span>
          <span className="block text-sm text-fg-muted">Короткий сигнал, когда вас упоминают.</span>
        </label>
        <Switch id={`${id}-switch`} checked={notify} onCheckedChange={setNotify} />
      </div>
    </div>
  );
}

function Overlays() {
  const toast = useToast();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [compact, setCompact] = useState(false);
  return (
    <>
      <Row>
        <Dialog>
          <DialogTrigger asChild>
            <Button variant="secondary">Открыть диалог</Button>
          </DialogTrigger>
          <DialogContent
            title="Создать канал"
            description="Канал появится в категории «Текстовые каналы»."
            footer={
              <>
                <DialogClose asChild>
                  <Button variant="ghost">Отмена</Button>
                </DialogClose>
                <DialogClose asChild>
                  <Button>Создать</Button>
                </DialogClose>
              </>
            }
          >
            <Field htmlFor="dlg-channel" label="Название канала">
              <Input id="dlg-channel" placeholder="новый-канал" />
            </Field>
          </DialogContent>
        </Dialog>

        <Button
          variant="danger"
          onClick={() => {
            setConfirmOpen(true);
          }}
        >
          Удалить канал
        </Button>
        <ConfirmDialog
          open={confirmOpen}
          onOpenChange={setConfirmOpen}
          title="Удалить канал #мемы?"
          description="Все сообщения канала удалятся навсегда. Это нельзя отменить."
          confirmLabel="Удалить канал"
          tone="danger"
          onConfirm={() => {
            setConfirmOpen(false);
            toast.show({ text: "Канал #мемы удалён", tone: "success" });
          }}
        />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="secondary">Меню сервера</Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuLabel>Вечерний сеанс</DropdownMenuLabel>
            <DropdownMenuItem>
              Пригласить людей <UserPlus aria-hidden />
            </DropdownMenuItem>
            <DropdownMenuItem>
              Настройки сервера <Settings aria-hidden />
            </DropdownMenuItem>
            <DropdownMenuCheckboxItem checked={compact} onCheckedChange={setCompact}>
              Компактный режим
            </DropdownMenuCheckboxItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="danger">
              Покинуть сервер <LogOut aria-hidden />
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <Tooltip content="Подсказка появляется и по фокусу с клавиатуры">
          <Button variant="ghost">Наведите или нажмите Tab</Button>
        </Tooltip>
      </Row>
      <ContextMenu>
        <ContextMenuTrigger className="flex h-20 items-center justify-center rounded-md bg-chat text-sm text-fg-muted select-none">
          Щёлкните здесь правой кнопкой: меню сообщения
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem>
            Ответить
          </ContextMenuItem>
          <ContextMenuItem>
            Копировать текст <Copy aria-hidden />
          </ContextMenuItem>
          <ContextMenuItem>
            Изменить <Pencil aria-hidden />
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem variant="danger">
            Удалить сообщение <Trash2 aria-hidden />
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    </>
  );
}

function Toasts() {
  const toast = useToast();
  return (
    <Row>
      <Button
        variant="secondary"
        onClick={() => {
          toast.show({ text: "Ссылка-приглашение скопирована" });
        }}
      >
        Обычное
      </Button>
      <Button
        variant="secondary"
        onClick={() => {
          toast.show({ text: "Настройки сохранены", tone: "success" });
        }}
      >
        Успех
      </Button>
      <Button
        variant="secondary"
        onClick={() => {
          toast.show({ text: "Нет прав на это действие", tone: "error" });
        }}
      >
        Ошибка
      </Button>
    </Row>
  );
}

const swatches: { name: string; token: string }[] = [
  { name: "Полоса серверов", token: "--bg-rail" },
  { name: "Панель пользователя", token: "--bg-panel" },
  { name: "Каналы и участники", token: "--bg-sidebar" },
  { name: "Чат", token: "--bg-chat" },
  { name: "Поле ввода", token: "--bg-input" },
  { name: "Меню и диалоги", token: "--bg-overlay" },
  { name: "Акцент, «в сети»", token: "--accent" },
  { name: "Опасное действие", token: "--danger" },
  { name: "Предупреждение", token: "--warning" },
  { name: "Не в сети", token: "--status-offline" },
];

function Palette() {
  return (
    <div className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
      {swatches.map((s) => (
        <div key={s.token} className="flex items-center gap-3">
          <span className="size-8 shrink-0 rounded-md shadow-[inset_0_0_0_1px_var(--border)]" style={{ background: `var(${s.token})` }} />
          <span className="min-w-0">
            <span className="block truncate text-sm font-medium">{s.name}</span>
            <code className="block truncate text-xs text-fg-faint">{s.token}</code>
          </span>
        </div>
      ))}
    </div>
  );
}
