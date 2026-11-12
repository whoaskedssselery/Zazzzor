/**
 * The one table of user-facing error texts, keyed by the `code` of the API's
 * problem+json responses (api/openapi.yaml). Codes missing here fall back to
 * GENERIC_ERROR; `field` errors of validation_failed are shown under the
 * form fields instead.
 *
 * Deliberately left unmapped (GENERIC_ERROR), because each one means a bug in
 * the client or the server rather than something the user can fix:
 * unsupported_media_type, upgrade_required, method_not_allowed, internal.
 */
export const GENERIC_ERROR = "Что-то пошло не так. Попробуйте ещё раз.";

/** A request that never got a response (offline, DNS, server down). */
export const NETWORK_ERROR = "Нет связи с сервером. Проверьте подключение.";

export const ERROR_TEXTS: Readonly<Record<string, string>> = {
  // Session and account
  unauthenticated: "Сессия закончилась. Войдите снова.",
  invalid_credentials: "Неверный логин или пароль.",
  account_disabled: "Аккаунт отключён администратором.",
  registration_closed: "Регистрация на этом сервере закрыта.",
  username_taken: "Этот логин уже занят.",
  cannot_disable_self: "Нельзя отключить собственный аккаунт.",
  admin_only: "Это доступно только администраторам.",
  // Permissions
  missing_permission: "Недостаточно прав для этого действия.",
  role_hierarchy: "Нельзя: ваша роль не выше роли участника.",
  banned: "Вы забанены на этом сервере.",
  cannot_target_self: "Это нельзя сделать с самим собой.",
  owner_cannot_leave: "Владелец не может покинуть свой сервер.",
  cannot_delete_everyone: "Роль @everyone удалить нельзя.",
  // Content and limits
  not_found: "Не найдено. Возможно, это уже удалили.",
  not_a_text_channel: "Это не текстовый канал.",
  validation_failed: "Проверьте введённые данные.",
  too_many_guilds: "Достигнут предел числа серверов.",
  too_many_channels: "Достигнут предел числа каналов на сервере.",
  too_many_roles: "Достигнут предел числа ролей на сервере.",
  file_too_large: "Файл больше 4 МБ.",
  attachment_not_found: "Файл больше недоступен. Прикрепите его заново.",
  too_many_attachments: "Не больше 10 файлов в одном сообщении.",
  unsupported_image: "Нужна картинка PNG, JPEG, GIF или WebP.",
  body_too_large: "Слишком большой запрос.",
  invalid_body: "Сервер не понял запрос. Обновите страницу и попробуйте снова.",
  // Friends
  blocked_by_you: "Сначала разблокируйте этого пользователя.",
  too_many_friends: "Достигнут предел в 1000 друзей.",
  too_many_outgoing_requests: "Слишком много неотвеченных заявок. Отмените часть из них.",
  // Direct messages and groups
  cannot_message_user:
    "Не получилось отправить: у вас нет общего сервера, вы не друзья или этот человек ограничил личные сообщения.",
  not_a_friend: "В группу можно позвать только друзей.",
  group_full: "В группе уже 10 человек — больше нельзя.",
  too_many_overrides: "Слишком много каналов с отдельными настройками уведомлений — не больше 500.",
  unknown_channel: "Этого канала больше нет.",
  too_many_groups: "У кого-то из участников уже 100 групп — это предел.",
  // Invites
  invite_expired: "Срок действия приглашения истёк.",
  invite_exhausted: "Приглашение больше не действует: все использования исчерпаны.",
  // Load and transport
  rate_limited: "Слишком много запросов. Подождите немного.",
  server_busy: "Сервер перегружен. Попробуйте через несколько секунд.",
  not_ready: "Сервер ещё запускается. Попробуйте через минуту.",
  too_many_connections: "Открыто слишком много вкладок Zazzzor. Закройте лишние.",
  origin_not_allowed: "Сервер не принимает запросы с этого адреса.",
};

/**
 * Sign-in and registration past their limits (429 rate_limited): the wait
 * comes from Retry-After, in seconds under a minute and in whole minutes,
 * rounded up, from a minute on (registration waits up to an hour). Without
 * Retry-After the plain rate_limited text is used.
 */
export function tooManyAttemptsText(retryAfter: number | undefined): string {
  if (!retryAfter) return ERROR_TEXTS.rate_limited ?? GENERIC_ERROR;
  const wait = retryAfter < 60 ? `${retryAfter} с` : `${Math.ceil(retryAfter / 60)} мин`;
  return `Слишком много попыток, подождите ${wait}.`;
}

/**
 * Texts for the request fields of `validation_failed` errors[] and for the
 * same rules checked in the forms before sending. The server's own messages
 * are English, so the UI shows these by field name instead; a field missing
 * here gets FIELD_ERROR.
 */
export const FIELD_ERROR = "Проверьте это поле.";

export const FIELD_ERROR_TEXTS: Readonly<Record<string, string>> = {
  username: "Логин: 2–32 символа, латиница, цифры, «_» и «.».",
  password: "Пароль: от 8 до 128 символов.",
  display_name: "Имя: от 1 до 32 символов, без управляющих и невидимых символов.",
  current_password: "Неверный текущий пароль.",
  new_password: "Пароль: от 8 до 128 символов.",
};

/** Russian text for a field error of the API (`ApiError.fields`). */
export function fieldErrorText(field: string): string {
  return Object.hasOwn(FIELD_ERROR_TEXTS, field) ? (FIELD_ERROR_TEXTS[field] ?? FIELD_ERROR) : FIELD_ERROR;
}
