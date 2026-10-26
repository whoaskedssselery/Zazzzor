/**
 * The one table of user-facing error texts, keyed by the `code` of the API's
 * problem+json responses (api/openapi.yaml). Codes missing here fall back to
 * GENERIC_ERROR; `field` errors of validation_failed are shown under the
 * form fields instead.
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
  unsupported_image: "Нужна картинка PNG, JPEG, GIF или WebP.",
  body_too_large: "Слишком большой запрос.",
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
