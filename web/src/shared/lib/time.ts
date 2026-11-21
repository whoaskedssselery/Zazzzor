import { t } from "@/shared/i18n";

const pad = (n: number) => String(n).padStart(2, "0");

/** Calendar day of `d` in local time, as a comparable number. */
const dayKey = (d: Date) => d.getFullYear() * 10_000 + (d.getMonth() + 1) * 100 + d.getDate();

/**
 * Message timestamp in local time, as Discord shows it: «Сегодня в 21:04»,
 * «Вчера в 21:04», otherwise «23.10.2026 21:04». Days are calendar days, not
 * 24-hour spans, so a message from 23:59 is «Вчера» at 00:01.
 */
export function formatMessageTime(d: Date, now: Date): string {
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  const day = dayKey(d);
  if (day === dayKey(now)) return t("common:time.today", { time });
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  if (day === dayKey(yesterday)) return t("common:time.yesterday", { time });
  return `${pad(d.getDate())}.${pad(d.getMonth() + 1)}.${d.getFullYear()} ${time}`;
}
