import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type ChangeEvent, useEffect, useRef, useState } from "react";
import { type User, updateSignedIn } from "@/entities/session";
import { ApiError, api, errorText, fieldErrorText, unwrap } from "@/shared/api";
import { fileForm, IMAGE_ACCEPT, imageProblem, runeLength, trimContent, useObjectUrl } from "@/shared/lib";
import { Avatar, Button, Field, FormAlert, Input, SaveBar, Textarea, useToast } from "@/shared/ui";

export const NAME_RULE = fieldErrorText("display_name");

/** "About me" is at most this many characters. */
export const BIO_MAX = 190;

/** The avatar as the form holds it: unchanged, a new file, or removed. */
type AvatarChange = File | "remove" | null;

const uploadAvatar = async (file: File) =>
  unwrap(await api.PUT("/users/@me/avatar", { body: { file: "" }, bodySerializer: fileForm(file) }));

/**
 * The user's display name, "about me" and avatar. Changes wait in the form until
 * «Сохранить изменения», as in the server settings (`onDirtyChange` lets
 * the screen ask before they are left). Each answer goes into the session
 * at once; the USER_UPDATE the server sends brings the same user, and the
 * form starts over from it.
 */
export const ProfileTab = ({ user, onDirtyChange }: { user: User; onDirtyChange: (dirty: boolean) => void }) => {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(user.display_name);
  const [bio, setBio] = useState(user.bio);
  const [bioError, setBioError] = useState<string | undefined>(undefined);
  const [avatar, setAvatar] = useState<AvatarChange>(null);
  const [nameError, setNameError] = useState<string | undefined>(undefined);
  const [avatarError, setAvatarError] = useState<string | null>(null);
  const preview = useObjectUrl(avatar instanceof File ? avatar : null);
  const picker = useRef<HTMLInputElement>(null);

  // A change made elsewhere (another tab) shows here while this form has nothing of its own.
  const [base, setBase] = useState(user.display_name);
  if (base !== user.display_name) {
    setBase(user.display_name);
    if (trimContent(name) === base) setName(user.display_name);
  }
  const [bioBase, setBioBase] = useState(user.bio);
  if (bioBase !== user.bio) {
    setBioBase(user.bio);
    if (bio.trim() === bioBase) setBio(user.bio);
  }

  const dirty = trimContent(name) !== user.display_name || bio.trim() !== user.bio || avatar !== null;
  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  const save = useMutation({
    // Takes the avatar it sends: another may be picked while this one uploads.
    mutationFn: async (sent: AvatarChange) => {
      const value = trimContent(name);
      const about = bio.trim();
      const body = {
        ...(value !== user.display_name ? { display_name: value } : {}),
        ...(about !== user.bio ? { bio: about } : {}),
      };
      if (Object.keys(body).length > 0) {
        updateSignedIn(queryClient, unwrap(await api.PATCH("/users/@me", { body })));
      }
      if (sent === "remove") updateSignedIn(queryClient, unwrap(await api.DELETE("/users/@me/avatar")));
      else if (sent !== null) updateSignedIn(queryClient, await uploadAvatar(sent));
    },
    onSuccess: (_, sent) => {
      setAvatar((current) => (current === sent ? null : current));
      toast.show({ text: "Изменения сохранены", tone: "success" });
    },
    onError: (e) => {
      if (e instanceof ApiError && "display_name" in e.fields) setNameError(NAME_RULE);
      if (e instanceof ApiError && "bio" in e.fields) setBioError(`Не длиннее ${String(BIO_MAX)} символов`);
    },
  });

  const pick = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = "";
    if (!file) return;
    const problem = imageProblem(file);
    setAvatarError(problem);
    if (problem === null) setAvatar(file);
  };

  const submit = () => {
    const length = runeLength(trimContent(name));
    if (length < 1 || length > 32) {
      setNameError(NAME_RULE);
      return;
    }
    setNameError(undefined);
    if (runeLength(bio.trim()) > BIO_MAX) {
      setBioError(`Не длиннее ${String(BIO_MAX)} символов`);
      return;
    }
    setBioError(undefined);
    save.mutate(avatar);
  };

  const reset = () => {
    setName(user.display_name);
    setBio(user.bio);
    setAvatar(null);
    setNameError(undefined);
    setBioError(undefined);
    setAvatarError(null);
    save.reset();
  };

  const shown = avatar === "remove" ? null : (preview ?? user.avatar_url);
  const failed =
    save.isError &&
    !(save.error instanceof ApiError && ("display_name" in save.error.fields || "bio" in save.error.fields));
  return (
    <section aria-labelledby="profile-title">
      <h2 id="profile-title" className="mb-5 text-xl font-semibold">
        Профиль
      </h2>
      {failed && <FormAlert>{errorText(save.error)}</FormAlert>}
      <div className="flex gap-8 max-sm:flex-col">
        <div className="flex flex-col items-center gap-3">
          <Avatar name={trimContent(name) || user.display_name} src={shown} size={80} />
          <input
            ref={picker}
            type="file"
            accept={IMAGE_ACCEPT}
            className="sr-only"
            tabIndex={-1}
            aria-hidden
            onChange={pick}
          />
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              picker.current?.click();
            }}
          >
            Сменить аватар
          </Button>
          {shown !== null && (
            <Button
              variant="link"
              size="sm"
              onClick={() => {
                setAvatarError(null);
                setAvatar(user.avatar_url === null ? null : "remove");
              }}
            >
              Удалить аватар
            </Button>
          )}
          <p className="max-w-40 text-center text-xs text-fg-muted">Квадрат не меньше 128×128, до 4 МБ.</p>
          {avatarError && (
            <p role="alert" className="max-w-40 text-center text-xs text-fg-danger">
              {avatarError}
            </p>
          )}
        </div>
        <form
          className="min-w-0 flex-1"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Field htmlFor="profile-name" label="Отображаемое имя" error={nameError}>
            <Input
              value={name}
              maxLength={64}
              autoComplete="nickname"
              onChange={(e) => {
                setName(e.currentTarget.value);
              }}
            />
          </Field>
          <Field
            htmlFor="profile-bio"
            label="О себе"
            hint={`Видят друзья и участники ваших серверов. ${String(runeLength(bio.trim()))} из ${String(BIO_MAX)}.`}
            error={bioError}
            className="mt-5"
          >
            <Textarea
              value={bio}
              rows={4}
              onChange={(e) => {
                setBio(e.currentTarget.value);
              }}
            />
          </Field>
          <dl className="mt-5">
            <dt className="text-sm font-semibold text-fg-muted">Имя пользователя</dt>
            <dd className="mt-1 text-base">{user.username}</dd>
          </dl>
        </form>
      </div>
      {dirty && <SaveBar pending={save.isPending} onReset={reset} onSave={submit} />}
    </section>
  );
};
