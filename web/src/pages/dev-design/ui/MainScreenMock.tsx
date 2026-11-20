// biome-ignore-all lint/a11y/useValidAnchor: a static dev-only mock; its links only look like Discord's and go nowhere.
// biome-ignore-all lint/suspicious/noArrayIndexKey: a static dev-only mock; its lists never reorder.
import {
  ChevronDown,
  ChevronRight,
  CirclePlus,
  Hash,
  Headphones,
  Mic,
  Pencil,
  Plus,
  Reply,
  Settings,
  SmilePlus,
  Users,
  Volume2,
} from "lucide-react";
import { type CSSProperties, type ReactNode, useState } from "react";
import { Avatar, Badge, cn, GuildIcon, IconButton, Logo, type Presence, roleTextStyle, Tooltip } from "@/shared/ui";

/*
 * A static mock of the main screen on made-up data, to judge the design
 * system at full density. Not wired to anything; Task 5 builds the real shell.
 */

const ADMIN = 0xd9944a;
const CINEMA = 0x5fa8d3;

const guilds = [
  { name: "Вечерний сеанс", active: true },
  { name: "Кооп по пятницам", unread: true },
  { name: "Книжный клуб" },
  { name: "Майнкрафт", mentions: 3 },
  { name: "Дача" },
];

interface MemberMock {
  name: string;
  status: Presence;
  color?: number;
}

const memberGroups: { title: string; members: MemberMock[] }[] = [
  { title: "Админ — 1", members: [{ name: "Аня Соколова", status: "online", color: ADMIN }] },
  {
    title: "Киноклуб — 3",
    members: [
      { name: "Дима Орлов", status: "online", color: CINEMA },
      { name: "Катя", status: "online", color: CINEMA },
      { name: "Лёша Ким", status: "online", color: CINEMA },
    ],
  },
  { title: "В сети — 1", members: [{ name: "Максим", status: "online" }] },
  {
    title: "Не в сети — 3",
    members: [
      { name: "Серёжа", status: "offline", color: CINEMA },
      { name: "Оля Белова", status: "offline" },
      { name: "Паша", status: "offline" },
    ],
  },
];

export const MainScreenMock = () => {
  const [showMembers, setShowMembers] = useState(true);
  return (
    // The member list folds away below 1100px, as Discord does on narrow windows.
    <div
      className={cn(
        "grid h-full min-h-0 grid-cols-[var(--rail-width)_var(--sidebar-width)_minmax(0,1fr)] overflow-hidden",
        showMembers &&
          "min-[1100px]:grid-cols-[var(--rail-width)_var(--sidebar-width)_minmax(0,1fr)_var(--members-width)]",
      )}
    >
      <Rail />
      <ChannelSidebar />
      <Chat
        showMembers={showMembers}
        onToggleMembers={() => {
          setShowMembers((v) => !v);
        }}
      />
      {showMembers && <MemberList />}
    </div>
  );
};

const Rail = () => (
  <nav aria-label="Серверы" className="flex min-h-0 flex-col items-center gap-2 overflow-y-auto bg-rail py-3">
    <RailItem label="Личные сообщения">
      <span className="flex size-12 items-center justify-center rounded-[50%] bg-guild text-on-guild transition-[border-radius,background-color,color] duration-(--duration-base) group-hover:rounded-xl">
        <Logo size={30} />
      </span>
    </RailItem>
    <div aria-hidden className="h-0.5 w-8 shrink-0 rounded-full bg-control" />
    {guilds.map((g) => (
      <RailItem
        key={g.name}
        label={g.name}
        active={g.active}
        unread={g.unread}
        {...(g.mentions ? { mentions: g.mentions } : {})}
      >
        <GuildIcon name={g.name} active={g.active} />
      </RailItem>
    ))}
    <RailItem label="Создать сервер">
      <span className="flex size-12 items-center justify-center rounded-[50%] bg-guild text-fg-success transition-[border-radius,background-color,color] duration-(--duration-base) group-hover:rounded-xl group-hover:bg-accent group-hover:text-on-accent">
        <Plus aria-hidden className="size-6" strokeWidth={2} />
      </span>
    </RailItem>
  </nav>
);

const RailItem = ({
  label,
  active = false,
  unread = false,
  mentions,
  children,
}: {
  label: string;
  active?: boolean | undefined;
  unread?: boolean | undefined;
  mentions?: number;
  children: ReactNode;
}) => {
  const name = mentions ? `${label}, упоминаний: ${String(mentions)}` : label;
  return (
    <div className="relative flex w-full shrink-0 justify-center">
      <span
        aria-hidden
        className={cn(
          "absolute top-1/2 left-0 w-1 -translate-y-1/2 rounded-r-full bg-accent transition-[height] duration-(--duration-base)",
          active ? "h-10" : unread ? "h-2" : "h-0",
        )}
      />
      <Tooltip content={label} side="right" delayDuration={0}>
        <button
          type="button"
          aria-label={name}
          aria-current={active ? "page" : undefined}
          className="group relative cursor-pointer rounded-xl"
        >
          {children}
          {mentions !== undefined && (
            <Badge variant="count" className="absolute -right-1 -bottom-1 h-5 min-w-5 px-1.5 ring-4 ring-(--bg-rail)">
              {mentions}
            </Badge>
          )}
        </button>
      </Tooltip>
    </div>
  );
};

const ChannelSidebar = () => (
  <div className="flex min-h-0 flex-col bg-sidebar">
    <button
      type="button"
      className="flex h-(--header-height) shrink-0 cursor-pointer items-center justify-between px-4 text-base font-semibold shadow-[0_1px_0_var(--border)] transition-colors hover:bg-hover"
    >
      <span className="truncate">Вечерний сеанс</span>
      <ChevronDown aria-hidden className="size-5 text-fg-muted" />
    </button>
    <nav aria-label="Каналы" className="min-h-0 flex-1 overflow-y-auto pt-2 pb-3">
      <Category name="Текстовые каналы">
        <ChannelRow name="общий" selected />
        <ChannelRow name="сериалы" unread />
        <ChannelRow name="игры" />
        <ChannelRow name="мемы" mentions={2} />
        <ChannelRow name="объявления" />
      </Category>
      <Category name="Голосовые каналы">
        <ChannelRow name="Кинозал" voice />
        <div className="mb-1 ml-9 flex flex-col">
          <VoiceUser name="Аня Соколова" live />
          <VoiceUser name="Дима Орлов" />
          <VoiceUser name="Катя" />
        </div>
        <ChannelRow name="Игровая" voice />
        <ChannelRow name="Тихий угол" voice />
      </Category>
      <Category name="Архив" collapsed />
    </nav>
    <UserPanel />
  </div>
);

const Category = ({
  name,
  collapsed = false,
  children,
}: {
  name: string;
  collapsed?: boolean;
  children?: ReactNode;
}) => {
  const Chevron = collapsed ? ChevronRight : ChevronDown;
  return (
    <section className="mt-3 first:mt-1">
      <h3>
        <button
          type="button"
          aria-expanded={!collapsed}
          className="group flex h-7 w-full cursor-pointer items-center gap-0.5 pr-2 pl-1.5 text-sm font-semibold text-fg-faint hover:text-fg"
        >
          <Chevron aria-hidden className="size-3.5" strokeWidth={2.5} />
          <span className="truncate">{name}</span>
        </button>
      </h3>
      {!collapsed && <div className="flex flex-col gap-px">{children}</div>}
    </section>
  );
};

const ChannelRow = ({
  name,
  selected = false,
  unread = false,
  voice = false,
  mentions,
}: {
  name: string;
  selected?: boolean;
  unread?: boolean;
  voice?: boolean;
  mentions?: number;
}) => {
  const Icon = voice ? Volume2 : Hash;
  const bright = selected || unread || Boolean(mentions);
  return (
    <div className="relative px-2">
      {unread && !selected && (
        <span aria-hidden className="absolute top-1/2 left-0 h-2 w-1 -translate-y-1/2 rounded-r-full bg-accent" />
      )}
      <a
        href="#"
        aria-current={selected ? "page" : undefined}
        onClick={(e) => {
          e.preventDefault();
        }}
        className={cn(
          "flex h-8 items-center gap-1.5 rounded-md px-2 transition-colors duration-(--duration-fast)",
          selected ? "bg-selected text-fg" : "hover:bg-hover hover:text-fg-muted",
          bright ? "text-fg" : "text-fg-faint",
        )}
      >
        <Icon aria-hidden className="size-5 shrink-0 text-fg-faint" strokeWidth={1.75} />
        <span className={cn("truncate text-base", unread || mentions ? "font-semibold" : "font-medium")}>{name}</span>
        {mentions !== undefined && (
          <Badge variant="count" className="ml-auto" aria-label={`упоминаний: ${String(mentions)}`}>
            {mentions}
          </Badge>
        )}
      </a>
    </div>
  );
};

const VoiceUser = ({ name, live = false }: { name: string; live?: boolean }) => (
  <div className="flex h-7.5 items-center gap-2 rounded-md px-2 text-sm font-medium text-fg-muted hover:bg-hover hover:text-fg">
    <Avatar name={name} size={24} />
    <span className="truncate">{name}</span>
    {live && (
      <Badge variant="live" className="ml-auto">
        В эфире
      </Badge>
    )}
  </div>
);

const UserPanel = () => (
  <div
    className="flex h-(--user-panel-height) shrink-0 items-center gap-1 bg-panel px-2"
    style={{ "--avatar-cutout": "var(--bg-panel)" } as CSSProperties}
  >
    <button
      type="button"
      className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded-md py-1 pr-1 pl-0.5 text-left hover:bg-hover"
    >
      <Avatar name="Максим" size={32} status="online" />
      <span className="min-w-0">
        <span className="block truncate text-sm leading-4 font-semibold">Максим</span>
        <span className="block truncate text-xs text-fg-faint">в сети</span>
      </span>
    </button>
    <IconButton label="Выключить микрофон" icon={Mic} />
    <IconButton label="Выключить звук" icon={Headphones} />
    <IconButton label="Настройки пользователя" icon={Settings} />
  </div>
);

interface MessageMock {
  author: string;
  color?: number;
  time: string;
  lines: ReactNode[];
  reply?: { author: string; color?: number; text: string };
  mentionsMe?: boolean;
}

const messages: MessageMock[] = [
  {
    author: "Аня Соколова",
    color: ADMIN,
    time: "Сегодня в 20:41",
    lines: [
      "Всем привет! Сегодня досматриваем «Тьму», третий сезон.",
      "Начинаем в 21:00, звук идёт прямо через демку, наушники можно не искать.",
    ],
  },
  {
    author: "Дима Орлов",
    color: CINEMA,
    time: "Сегодня в 20:43",
    lines: ["Я за. Только напомните, на какой серии мы остановились?"],
  },
  {
    author: "Катя",
    color: CINEMA,
    time: "Сегодня в 20:44",
    reply: { author: "Дима Орлов", color: CINEMA, text: "Я за. Только напомните, на какой серии мы остановились?" },
    lines: ["На пятой, на моменте с пещерой. Дальше без спойлеров!"],
  },
  {
    author: "Максим",
    time: "Сегодня в 20:47",
    lines: [
      "Подключусь минут через десять, доем.",
      <>
        Кстати, <code className="rounded-xs bg-input px-1 py-0.5">Ctrl+Shift+M</code> выключает микрофон, если кто-то
        опять будет хрустеть. <span className="text-xs text-fg-faint">(изменено)</span>
      </>,
    ],
  },
  {
    author: "Аня Соколова",
    color: ADMIN,
    time: "Сегодня в 20:52",
    mentionsMe: true,
    lines: [
      <>
        <span className="rounded-xs bg-mention-chip px-0.5 font-medium text-fg-mention">@Максим</span> не забудь
        попкорн, в прошлый раз кончился на второй серии.
      </>,
    ],
  },
  {
    author: "Лёша Ким",
    color: CINEMA,
    time: "Сегодня в 20:55",
    lines: [
      <>
        Я сегодня только на час, завтра рано вставать. Ссылка на расписание:{" "}
        <a
          href="#"
          className="text-link hover:underline"
          onClick={(e) => {
            e.preventDefault();
          }}
        >
          https://example.com/raspisanie
        </a>
      </>,
    ],
  },
];

const Chat = ({ showMembers, onToggleMembers }: { showMembers: boolean; onToggleMembers: () => void }) => (
  <main
    className="flex min-h-0 min-w-0 flex-col bg-chat"
    style={{ "--avatar-cutout": "var(--bg-chat)" } as CSSProperties}
  >
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 px-4 shadow-[0_1px_0_var(--border)]">
      <Hash aria-hidden className="size-6 shrink-0 text-fg-faint" strokeWidth={1.75} />
      <h2 className="text-base font-semibold">общий</h2>
      <span aria-hidden className="mx-2 h-6 w-px shrink-0 bg-border" />
      <p className="min-w-0 flex-1 truncate text-sm text-fg-muted">
        Болтаем обо всём. Кино — по пятницам в 21:00 в «Кинозале».
      </p>
      <IconButton
        label={showMembers ? "Скрыть участников" : "Показать участников"}
        icon={Users}
        pressed={showMembers}
        onClick={onToggleMembers}
        tooltipSide="bottom"
      />
    </header>
    <div className="flex min-h-0 flex-1 flex-col justify-end overflow-y-auto">
      <ol aria-label="Сообщения в #общий" className="pb-4">
        <li className="mx-4 mt-6 mb-2">
          <span aria-hidden className="flex size-17 items-center justify-center rounded-full bg-control">
            <Hash className="size-10 text-fg" strokeWidth={1.75} />
          </span>
          <h3 className="mt-3 text-xl font-bold">Добро пожаловать в #общий!</h3>
          <p className="mt-1 text-base text-fg-muted">
            Это начало канала #общий. Пишите, договаривайтесь о вечере, кидайте ссылки.
          </p>
        </li>
        <li aria-hidden className="mx-4 mt-6 mb-1 flex items-center gap-2 text-xs font-semibold text-fg-faint">
          <span className="h-px flex-1 bg-border" />
          23 октября 2026 г.
          <span className="h-px flex-1 bg-border" />
        </li>
        {messages.map((m) => (
          <MessageGroup key={m.time} message={m} />
        ))}
      </ol>
    </div>
    <Composer />
  </main>
);

const MessageGroup = ({ message: m }: { message: MessageMock }) => {
  const [first, ...rest] = m.lines;
  return (
    <>
      <li
        className={cn(
          "group relative mt-4 py-0.5 pr-12 pl-18 transition-colors hover:bg-message-hover",
          m.mentionsMe && "bg-mention hover:bg-mention",
        )}
      >
        {m.reply && (
          <div className="relative mb-1 flex items-center gap-1.5 text-sm text-fg-muted">
            <span
              aria-hidden
              className="absolute top-2.5 -left-9 h-5 w-7.5 rounded-tl-md border-t-2 border-l-2 border-border-strong"
            />
            <Avatar name={m.reply.author} size={20} />
            <span className="role-text shrink-0 font-semibold" style={roleTextStyle(m.reply.color ?? 0)}>
              @{m.reply.author}
            </span>
            <span className="truncate">{m.reply.text}</span>
          </div>
        )}
        <div className={cn("absolute left-4", m.reply ? "top-7.5" : "top-1")}>
          <Avatar name={m.author} size={40} />
        </div>
        <h3 className="flex items-baseline gap-2">
          <span
            className="role-text cursor-pointer text-base font-semibold hover:underline"
            style={roleTextStyle(m.color ?? 0)}
          >
            {m.author}
          </span>
          <time className="text-xs text-fg-faint">{m.time}</time>
        </h3>
        <p className="text-base break-words">{first}</p>
        <MessageActions />
      </li>
      {rest.map((line, i) => (
        <li key={i} className="group relative py-0.5 pr-12 pl-18 hover:bg-message-hover">
          <time className="absolute left-0 w-18 pt-0.5 text-center text-[11px] text-fg-faint opacity-0 group-hover:opacity-100">
            {m.time.slice(-5)}
          </time>
          <p className="text-base break-words">{line}</p>
          <MessageActions />
        </li>
      ))}
    </>
  );
};

const MessageActions = () => (
  <div className="absolute -top-4 right-4 hidden items-center rounded-md bg-overlay p-0.5 shadow-float group-hover:flex">
    <IconButton size="sm" label="Реакция" icon={SmilePlus} />
    <IconButton size="sm" label="Ответить" icon={Reply} />
    <IconButton size="sm" label="Изменить" icon={Pencil} />
  </div>
);

const Composer = () => (
  <div className="shrink-0 px-4 pb-6">
    <div className="flex min-h-11 items-center gap-1 rounded-lg bg-input pr-3 pl-2">
      <IconButton label="Прикрепить файл" icon={CirclePlus} />
      <input
        aria-label="Сообщение в #общий"
        placeholder="Написать в #общий"
        className="h-11 min-w-0 flex-1 bg-transparent text-base text-fg outline-none"
      />
    </div>
    <p className="-mb-5 h-5 pt-0.5 pl-1 text-xs text-fg-muted">
      <span className="font-semibold text-fg">Дима Орлов</span> печатает…
    </p>
  </div>
);

const MemberList = () => (
  <aside aria-label="Участники" className="min-h-0 overflow-y-auto bg-sidebar px-2 pb-4 max-[1099px]:hidden">
    {memberGroups.map((g) => (
      <section key={g.title}>
        <h3 className="px-2 pt-6 pb-1 text-xs font-semibold text-fg-faint">{g.title}</h3>
        <ul>
          {g.members.map((m) => (
            <li key={m.name}>
              <button
                type="button"
                className="flex h-11 w-full cursor-pointer items-center gap-3 rounded-md px-2 text-left hover:bg-hover"
              >
                <Avatar
                  name={m.name}
                  size={32}
                  status={m.status}
                  className={cn(m.status === "offline" && "opacity-45")}
                />
                <span
                  className={cn(
                    "truncate text-base font-medium",
                    m.status === "offline" ? "text-fg-faint" : "role-text",
                  )}
                  style={m.status === "offline" ? undefined : roleTextStyle(m.color ?? 0)}
                >
                  {m.name}
                </span>
              </button>
            </li>
          ))}
        </ul>
      </section>
    ))}
  </aside>
);
