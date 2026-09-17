"use client";

import { FormEvent, useMemo, useState } from "react";
import { BaristaAPIError, baristaClient, userFacingError } from "../lib/chat-client";
import type { ProfileList } from "../model/types";
import styles from "./profile-manager.module.css";

type Fields = { name: string; style: string; constraints: string; additional_context: string };
type Props = { initial: ProfileList | null; loading: boolean; error: string | null; onChange: (value: ProfileList) => void; onClose: () => void; onRetry: () => void };

const emptyFields: Fields = { name: "", style: "", constraints: "", additional_context: "" };

export function ProfileManager({ initial, loading, error: loadError, onChange, onClose, onRetry }: Props) {
  const [fields, setFields] = useState<Fields>(emptyFields);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const active = useMemo(() => initial?.profiles.find((profile) => profile.id === initial.active_profile_id) ?? null, [initial]);

  function update(field: keyof Fields, value: string) { setFields((current) => ({ ...current, [field]: value })); }
  function validate(): string | null {
    if (!fields.name.trim()) return "Введите название профиля.";
    if ([...fields.name.trim()].length > 60) return "Название профиля не должно быть длиннее 60 символов.";
    if (!fields.style.trim()) return "Опишите стиль ответа.";
    if (!fields.constraints.trim()) return "Опишите ограничения.";
    if (!fields.additional_context.trim()) return "Добавьте дополнительный контекст.";
    return null;
  }
  async function create(event: FormEvent) {
    event.preventDefault();
    const validation = validate();
    if (validation) { setError(validation); return; }
    setPending(true); setError(null);
    try { const result = await baristaClient.createProfile({ ...fields, name: fields.name.trim() }); onChange(result); setFields(emptyFields); }
    catch (cause) { setError(profileError(cause)); }
    finally { setPending(false); }
  }
  async function select(id: string) {
    if (pending || id === initial?.active_profile_id) return;
    setPending(true); setError(null);
    try { onChange(await baristaClient.selectProfile(id)); }
    catch (cause) { setError(profileError(cause)); }
    finally { setPending(false); }
  }
  async function remove() {
    if (!deleteTarget || pending) return;
    setPending(true); setError(null);
    try {
      await baristaClient.removeProfile(deleteTarget);
      onChange(await baristaClient.profiles());
      setDeleteTarget(null);
    } catch (cause) { setError(profileError(cause)); }
    finally { setPending(false); }
  }

  const target = initial?.profiles.find((profile) => profile.id === deleteTarget) ?? null;
  return <div className={styles.overlay} role="presentation">
    <section className={styles.panel} role="dialog" aria-modal="true" aria-labelledby="profiles-title" aria-busy={loading || pending}>
      <header className={styles.header}><div><h2 id="profiles-title">Профили ассистента</h2><p>Выбранный профиль применяется к следующему сообщению во всех чатах этого сеанса.</p></div><button type="button" className={styles.close} onClick={onClose} aria-label="Закрыть профили">×</button></header>
      {(loadError || error) && <p className={styles.error} role="alert">{loadError ?? error}</p>}
      {loading ? <p role="status" className={styles.loading}>Загружаем профили…</p> : !initial ? <div className={styles.loading}><p>Не удалось загрузить профили.</p><button type="button" onClick={onRetry}>Повторить загрузку</button></div> : <>
        <p className={styles.active} role="status">Активен: <strong>{active?.name ?? "—"}</strong></p>
        <section aria-label="Доступные профили" className={styles.list}>{initial.profiles.map((profile) => <article className={styles.card} key={profile.id} data-active={profile.id === initial.active_profile_id}>
          <div className={styles.cardHeader}><h3>{profile.name}</h3>{profile.built_in && <span>Встроенный</span>}</div>
          <dl><div><dt>Стиль</dt><dd>{profile.style}</dd></div><div><dt>Ограничения</dt><dd>{profile.constraints}</dd></div><div><dt>Дополнительный контекст</dt><dd>{profile.additional_context}</dd></div></dl>
          <div className={styles.actions}><button type="button" onClick={() => void select(profile.id)} disabled={pending || profile.id === initial.active_profile_id}>{profile.id === initial.active_profile_id ? "Активный профиль" : "Выбрать"}</button>{!profile.built_in && <button type="button" className={styles.delete} onClick={() => setDeleteTarget(profile.id)} disabled={pending}>Удалить</button>}</div>
        </article>)}</section>
        <form className={styles.form} onSubmit={(event) => void create(event)} noValidate>
          <h3>Новый кастомный профиль</h3><p>Все поля обязательны. Текст профиля сохранится только в этом browser-сеансе.</p>
          <label htmlFor="profile-name">Название</label><input id="profile-name" value={fields.name} onChange={(event) => update("name", event.target.value)} required disabled={pending}/>
          <label htmlFor="profile-style">Стиль</label><textarea id="profile-style" value={fields.style} onChange={(event) => update("style", event.target.value)} required disabled={pending}/>
          <label htmlFor="profile-constraints">Ограничения</label><textarea id="profile-constraints" value={fields.constraints} onChange={(event) => update("constraints", event.target.value)} required disabled={pending}/>
          <label htmlFor="profile-context">Дополнительный контекст</label><textarea id="profile-context" value={fields.additional_context} onChange={(event) => update("additional_context", event.target.value)} required disabled={pending}/>
          <button type="submit" disabled={pending}>{pending ? "Сохраняем…" : "Создать и выбрать"}</button>
        </form>
      </>}
      {target && <div className={styles.confirmOverlay} role="presentation"><section className={styles.confirm} role="alertdialog" aria-modal="true" aria-labelledby="delete-profile-title"><h3 id="delete-profile-title">Удалить профиль «{target.name}»?</h3><p>{target.id === initial?.active_profile_id ? "Активным сразу станет профиль «Бариста»." : "Активный профиль не изменится."}</p><div><button type="button" onClick={() => setDeleteTarget(null)} disabled={pending}>Отмена</button><button type="button" className={styles.delete} onClick={() => void remove()} disabled={pending}>Удалить</button></div></section></div>}
    </section>
  </div>;
}

function profileError(cause: unknown) {
  if (cause instanceof BaristaAPIError && cause.category === "validation") return "Проверьте заполнение полей и уникальность названия.";
  return userFacingError(cause);
}
