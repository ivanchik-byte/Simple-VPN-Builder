import { useEffect, useState } from 'react';
import { useLang, type Key } from '../lang';
import { Card, EmptyState, TableSkeleton } from '../components';
import { fetchRole, formatBytes, textVal, timeAgo } from '../api';

interface Plan {
  id: string;
  name: string;
  traffic_limit: number | null;
  traffic_limit_gb: number | null;
  is_trial: boolean | null;
  trial_duration_hours: number | null;
  protocols: string[];
}

interface UserRow {
  id: string;
  username: string;
  email: string | null;
  status: string | null;
  plan_id: string | null;
  traffic_limit: number | null;
  traffic_used: number | null;
  expires_at: string | null;
  subscription_token: string;
  telegram_id: number | null;
  telegram_username: string | null;
  is_banned: boolean | null;
  last_seen_at: string | null;
  display_name: string;
  short_id: string;
  crm_status: string;
}

const SEGMENTS = ['all', 'active', 'lead', 'trial', 'expired', 'banned'] as const;

async function postForm(action: string, fields: Record<string, string>): Promise<void> {
  const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
  if (!tokenRes.ok) throw new Error('csrf');
  const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
  const body = new URLSearchParams({ ...fields, csrf_token });
  const res = await fetch(action, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
  });
  if (res.status === 401) {
    window.location.href = '/admin/login';
    throw new Error('Unauthorized');
  }
}

function timeFmt(iso: string): string {
  return new Date(iso).toLocaleString();
}

function StatusBadge({ st, t }: { st: string; t: (k: Key) => string }) {
  const map: Record<string, string> = {
    banned: 'bg-rose-500/10 text-rose-500 border-rose-500/20',
    lead: 'bg-amber-500/10 text-amber-500 border-amber-500/20',
    trial: 'bg-sky-500/10 text-sky-400 border-sky-500/20',
    expired: 'bg-zinc-500/10 text-zinc-400 border-zinc-500/20',
    active: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20',
  };
  const key = `users.st.${st}` as Key;
  const label =
    st === 'banned'
      ? t('users.st.banned')
      : st === 'lead'
        ? t('users.st.lead')
        : st === 'trial'
          ? t('users.st.trial')
          : st === 'expired'
            ? t('users.st.expired')
            : st === 'active'
              ? t('users.st.active')
              : st;
  return (
    <span className={`rounded border px-2 py-0.5 text-[10px] font-mono uppercase ${map[st] ?? map.active}`}>{label}</span>
  );
}

export function UsersPage() {
  const { lang, t } = useLang();
  const [segment, setSegment] = useState<string>('all');
  const [users, setUsers] = useState<UserRow[] | null>(null);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [plans, setPlans] = useState<Plan[]>([]);
  const [error, setError] = useState('');
  const [qr, setQr] = useState<{ username: string; token: string } | null>(null);
  const [role, setRole] = useState('');
  const [dmUser, setDmUser] = useState<UserRow | null>(null);
  const [assignUser, setAssignUser] = useState<UserRow | null>(null);
  const [emailUser, setEmailUser] = useState<UserRow | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const load = async (seg: string) => {
    try {
      const res = await fetch(`/admin/users-data?segment=${seg}`, { credentials: 'include' });
      if (res.status === 401) {
        window.location.href = '/admin/login';
        return;
      }
      const data = (await res.json()) as { users: UserRow[]; counts: Record<string, number>; plans: Plan[] };
      setUsers(data.users);
      setCounts(data.counts);
      setPlans(data.plans ?? []);
    } catch {
      setError(t('users.err.load'));
    }
  };

  useEffect(() => {
    void load(segment);
    void fetchRole().then(setRole);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [segment]);

  const act = async (action: string, fields: Record<string, string> = {}) => {
    setError('');
    try {
      await postForm(action, fields);
      await load(segment);
    } catch {
      setError(t('users.err.action'));
    }
  };

  const segLabel = (s: string) => {
    const base =
      s === 'all' ? t('users.seg.all') : s === 'active' ? t('users.seg.active') : s === 'lead' ? t('users.seg.lead') : s === 'trial' ? t('users.seg.trial') : s === 'expired' ? t('users.seg.expired') : t('users.seg.banned');
    return `${base} (${counts[s] ?? counts.all ?? 0})`;
  };

  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)] text-xs';
  const origin = window.location.origin;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('users.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('users.subtitle')}</p>
      </div>

      {error && (
        <div className="tabular-nums flex items-center justify-between rounded border border-rose-500/30 bg-rose-500/10 p-4 text-xs text-rose-500">
          <span>{error}</span>
          <button onClick={() => setError('')} className="text-sm font-bold text-rose-400 hover:text-rose-200">
            &times;
          </button>
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border-subtle)] pb-3">
        <div className="tabular-nums flex items-center gap-1 overflow-x-auto py-1 text-xs">
          {SEGMENTS.map((s) => (
            <button
              key={s}
              onClick={() => setSegment(s)}
              className={`whitespace-nowrap rounded px-3 py-1.5 transition-colors ${
                segment === s
                  ? 'bg-zinc-900 font-bold text-zinc-100 dark:bg-zinc-100 dark:text-zinc-950'
                  : 'border border-[var(--border-subtle)] bg-[var(--bg-canvas)] text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]'
              }`}
            >
              {segLabel(s)}
            </button>
          ))}
        </div>
        <button
          onClick={() => setCreateOpen(true)}
          className="rounded bg-zinc-900 px-3 py-1.5 text-xs font-medium uppercase tracking-wide text-zinc-100 transition-colors hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200"
        >
          {t('users.create')}
        </button>
      </div>

      <Card className="overflow-hidden">
        {users === null ? (
          <TableSkeleton rows={6} />
        ) : users.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[960px] text-left text-xs">
              <thead>
                <tr className="tabular-nums border-b border-[var(--border-subtle)] bg-[var(--bg-hover)] text-[10px] uppercase text-[var(--text-muted)]">
                  <th className="px-5 py-2.5 font-medium">{t('users.col.user')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('users.col.life')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('users.col.quota')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('users.col.token')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('users.col.exp')}</th>
                  <th className="px-5 py-2.5 text-right font-medium">{t('users.col.actions')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-subtle)]">
                {users.map((u) => {
                  const banned = !!u.is_banned;
                  const used = u.traffic_used ?? 0;
                  const limit = u.traffic_limit != null && u.traffic_limit > 0 ? u.traffic_limit : null;
                  return (
                    <tr key={u.id} className="transition-colors hover:bg-[var(--bg-hover)]/50">
                      <td className="px-5 py-3">
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 font-medium text-[var(--text-primary)]">
                          <span className="font-semibold">{u.display_name}</span>
                          <span className="tabular-nums rounded border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-1.5 py-0.5 text-[10px] text-[var(--text-muted)]" title={t('users.tip.id')}>
                            usr_{u.short_id}
                          </span>
                          {u.plan_id != null ? (
                            <span className="tabular-nums rounded border border-sky-500/20 bg-sky-500/10 px-1.5 py-0.5 text-[10px] text-sky-400">{t('users.plan')}</span>
                          ) : u.crm_status === 'lead' ? (
                            <span className="tabular-nums rounded border border-amber-500/20 bg-amber-500/10 px-1.5 py-0.5 text-[10px] text-amber-400">{t('users.lead')}</span>
                          ) : (
                            <span className="tabular-nums rounded border border-purple-500/20 bg-purple-500/10 px-1.5 py-0.5 text-[10px] text-purple-400">{t('users.custom')}</span>
                          )}
                          {textVal(u.telegram_username) && (
                            <a
                              href={`https://t.me/${textVal(u.telegram_username)}`}
                              target="_blank"
                              rel="noreferrer"
                              className="tabular-nums inline-flex items-center rounded border border-blue-500/20 bg-blue-500/10 px-1.5 py-0.5 text-[10px] text-blue-400 hover:bg-blue-500/20"
                            >
                              @{textVal(u.telegram_username)}
                            </a>
                          )}
                          {u.telegram_id != null && (
                            <a
                              href={`tg://user?id=${u.telegram_id}`}
                              title={t('users.tip.openTg')}
                              className="tabular-nums inline-flex items-center rounded border border-zinc-700 bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-300 hover:bg-zinc-700"
                            >
                              TG: {u.telegram_id}
                            </a>
                          )}
                        </div>
                        <div className="tabular-nums mt-1 flex items-center gap-2 text-[11px] text-[var(--text-muted)]">
                          {textVal(u.email) ? (
                            <span className="text-[var(--text-secondary)]">{textVal(u.email)}</span>
                          ) : (
                            <span className="text-zinc-500">{t('users.noEmail')}</span>
                          )}
                          <button
                            onClick={() => setEmailUser(u)}
                            className="text-[10px] text-sky-400 hover:underline"
                          >
                            {t('users.editEmail')}
                          </button>
                          {u.last_seen_at != null && (
                            <span className="text-[10px] text-zinc-500">
                              &bull; {t('users.seen')} {timeAgo(u.last_seen_at, lang)}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-5 py-3 font-mono">
                        <StatusBadge st={u.crm_status} t={t} />
                      </td>
                      <td className="px-5 py-3 font-mono">
                        <div className="font-semibold text-[var(--text-primary)]">
                          {formatBytes(used)} / {limit != null ? formatBytes(limit) : t('users.unlimited')}
                        </div>
                        <div className="mt-1.5 h-1.5 w-32 overflow-hidden rounded-full bg-[var(--bg-hover)]">
                          <div
                            className="h-full rounded-full bg-emerald-500"
                            style={{ width: limit ? `${Math.min(100, (used / limit) * 100)}%` : '0%' }}
                          />
                        </div>
                      </td>
                      <td className="px-5 py-3 font-mono text-[var(--text-muted)]">
                        <div className="flex items-center gap-2">
                          <span>{u.subscription_token.slice(0, 8)}...</span>
                          <button
                            onClick={() => setQr({ username: u.username, token: u.subscription_token })}
                            title={t('users.tip.viewSub')}
                            className="rounded bg-[var(--bg-hover)] p-1 transition-colors hover:text-[var(--text-primary)]"
                          >
                            QR
                          </button>
                        </div>
                      </td>
                      <td className="px-5 py-3 font-mono text-[var(--text-muted)]">
                        {u.expires_at != null ? timeFmt(u.expires_at) : t('users.permanent')}
                      </td>
                      <td className="space-x-1.5 space-y-1 px-5 py-3 text-right">
                        {u.telegram_id != null && (
                          <button
                            onClick={() => setDmUser(u)}
                            title={t('users.tip.message')}
                            className="tabular-nums cursor-pointer rounded bg-sky-500/10 px-2 py-1 text-[11px] text-sky-400 transition-colors hover:bg-sky-500/20"
                          >
                            {t('users.message')}
                          </button>
                        )}
                        <button
                          onClick={() => setAssignUser(u)}
                          title={t('users.tip.assign')}
                          className="tabular-nums cursor-pointer rounded bg-amber-500/10 px-2 py-1 text-[11px] text-amber-400 transition-colors hover:bg-amber-500/20"
                        >
                          {t('users.assign')}
                        </button>
                        <button
                          onClick={() => void act(`/admin/users/${u.id}/reset-traffic`)}
                          title={t('users.tip.resetQuota')}
                          className="tabular-nums cursor-pointer rounded bg-[var(--bg-hover)] px-2 py-1 text-[11px] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
                        >
                          {t('users.reset')}
                        </button>
                        <button
                          onClick={() => void act(`/admin/users/${u.id}/ban`)}
                          title={banned ? t('users.tip.unban') : t('users.tip.ban')}
                          className={`tabular-nums cursor-pointer rounded px-2 py-1 text-[11px] transition-colors ${
                            banned
                              ? 'bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20'
                              : 'bg-amber-500/10 text-amber-500 hover:bg-amber-500/20'
                          }`}
                        >
                          {banned ? t('users.unban') : t('users.ban')}
                        </button>
                        {role !== 'admin' && (
                        <button
                          onClick={() => {
                            if (window.confirm(`${t('users.delTitle')}\n${t('users.deleteConfirm', { name: u.username })}`)) {
                              void act(`/admin/users/${u.id}/delete`);
                            }
                          }}
                          className="tabular-nums cursor-pointer rounded bg-rose-500/10 px-2 py-1 text-[11px] text-rose-500 transition-colors hover:bg-rose-500/20"
                        >
                          {t('users.delete')}
                        </button>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState title={t('users.empty')} hint="" />
        )}
      </Card>

      {qr && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
          <div className="w-full max-w-lg rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl">
            <div className="mb-4 flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
              <div>
                <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{t('users.qr.title')}</h3>
                <p className="tabular-nums mt-0.5 text-xs text-[var(--text-muted)]">{t('users.qr.user')}{qr.username}</p>
              </div>
              <button onClick={() => setQr(null)} className="text-[var(--text-muted)] hover:text-[var(--text-primary)]">
                &times;
              </button>
            </div>
            <div className="my-4 flex flex-col items-center justify-center rounded bg-white p-4">
              <img src={`/admin/qr?text=${encodeURIComponent(`${origin}/sub/${qr.token}`)}`} alt="Subscription QR" className="h-48 w-48" />
              <p className="tabular-nums mt-2 text-[11px] text-zinc-600">{t('users.qr.scan')}</p>
            </div>
            <div className="space-y-3 text-xs">
              <div>
                <label className="tabular-nums mb-1 block text-[11px] text-[var(--text-muted)]">{t('users.qr.url')}</label>
                <div className="flex items-center gap-2">
                  <input type="text" readOnly value={`${origin}/sub/${qr.token}`} className={`${inputCls} flex-1`} />
                  <button
                    onClick={() => {
                      void navigator.clipboard.writeText(`${origin}/sub/${qr.token}`);
                      alert(t('users.qr.copied'));
                    }}
                    className="tabular-nums rounded bg-zinc-900 px-3 py-1.5 text-xs font-semibold text-zinc-100 dark:bg-zinc-100 dark:text-zinc-950"
                  >
                    {t('users.qr.copy')}
                  </button>
                </div>
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--border-subtle)] pt-2">
                <a href={`/sub/${qr.token}`} target="_blank" rel="noreferrer" className="tabular-nums inline-flex items-center gap-1 rounded bg-zinc-800 px-3 py-1.5 text-xs text-zinc-100 hover:bg-zinc-700">
                  {t('users.qr.raw')}
                </a>
                <div className="tabular-nums flex items-center gap-1 text-[11px]">
                  <a href={`/sub/${qr.token}?format=wireguard`} download="wireguard.conf" className="rounded bg-[var(--bg-hover)] px-2 py-1 text-[var(--text-primary)]">
                    .conf
                  </a>
                  <a href={`/sub/${qr.token}?format=singbox`} download="sing-box.json" className="rounded bg-[var(--bg-hover)] px-2 py-1 text-[var(--text-primary)]">
                    sing-box
                  </a>
                  <a href={`/sub/${qr.token}?format=clash`} download="clash.yaml" className="rounded bg-[var(--bg-hover)] px-2 py-1 text-[var(--text-primary)]">
                    clash
                  </a>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {dmUser && (
        <DmModal
          user={dmUser}
          onClose={() => setDmUser(null)}
          onDone={() => {
            setDmUser(null);
            void load(segment);
          }}
        />
      )}

      {assignUser && (
        <AssignModal
          user={assignUser}
          plans={plans}
          onClose={() => setAssignUser(null)}
          onDone={() => {
            setAssignUser(null);
            void load(segment);
          }}
        />
      )}

      {emailUser && (
        <EmailModal
          user={emailUser}
          onClose={() => setEmailUser(null)}
          onDone={() => {
            setEmailUser(null);
            void load(segment);
          }}
        />
      )}

      {createOpen && (
        <CreateModal
          plans={plans}
          onClose={() => setCreateOpen(false)}
          onDone={() => {
            setCreateOpen(false);
            void load(segment);
          }}
        />
      )}
    </div>
  );
}

function CreateModal({ plans, onClose, onDone }: { plans: Plan[]; onClose: () => void; onDone: () => void }) {
  const { t } = useLang();
  const [mode, setMode] = useState<'preset' | 'custom'>('preset');
  const [unlimited, setUnlimited] = useState(false);
  const [neverExp, setNeverExp] = useState(false);
  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)] text-xs';

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const fields: Record<string, string> = {};
    fd.forEach((v, k) => {
      fields[k] = String(v);
    });
    fields['plan_type'] = mode;
    if (mode === 'custom') {
      if (unlimited) fields['unlimited_traffic'] = 'true';
      if (neverExp) fields['never_expires'] = 'true';
    }
    try {
      await postForm('/admin/users', fields);
      onDone();
    } catch {
      onDone();
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl">
        <div className="mb-4 flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
          <div>
            <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{t('users.c.title')}</h3>
            <p className="tabular-nums mt-0.5 text-xs text-[var(--text-muted)]">{t('users.c.sub')}</p>
          </div>
          <button onClick={onClose} className="text-[var(--text-muted)] hover:text-[var(--text-primary)]">
            &times;
          </button>
        </div>
        <form onSubmit={(e) => void submit(e)} className="space-y-4 text-xs">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block font-medium text-[var(--text-secondary)]">
                {t('users.c.username')} <span className="text-rose-500">*</span>
              </label>
              <input name="username" required placeholder={t('users.c.usernamePh')} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.c.email')}</label>
              <input type="email" name="email" placeholder="alex@example.com" className={inputCls} />
            </div>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.c.tgId')}</label>
              <input type="number" name="telegram_id" placeholder="e.g. 123456789" className={inputCls} />
              <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">{t('users.c.tgIdHint')}</p>
            </div>
            <div>
              <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.c.tgUser')}</label>
              <input name="telegram_username" placeholder="e.g. alexsmith" className={inputCls} />
              <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">{t('users.c.tgUserHint')}</p>
            </div>
          </div>
          <div>
            <label className="mb-1.5 block font-medium text-[var(--text-secondary)]">{t('users.c.mode')}</label>
            <div className="grid grid-cols-2 gap-2 rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] p-1">
              {(['preset', 'custom'] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => setMode(m)}
                  className={`rounded py-1.5 text-xs transition-all ${
                    mode === m
                      ? 'bg-[var(--bg-surface)] font-semibold text-[var(--text-primary)] shadow'
                      : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]'
                  }`}
                >
                  {m === 'preset' ? t('users.c.preset') : t('users.c.custom')}
                </button>
              ))}
            </div>
          </div>
          {mode === 'preset' ? (
            <div className="space-y-3 rounded border border-[var(--border-subtle)] bg-[var(--bg-canvas)] p-3">
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.c.plan')}</label>
                <select name="plan_id" className="w-full rounded border border-[var(--border-default)] bg-[var(--bg-surface)] px-3 py-1.5 font-mono text-xs text-[var(--text-primary)]">
                  {plans.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} —{' '}
                      {p.is_trial
                        ? t('users.trialDuration', { hours: p.trial_duration_hours ?? 24 })
                        : p.traffic_limit_gb != null
                          ? `${p.traffic_limit_gb} GB`
                          : p.traffic_limit != null
                            ? formatBytes(p.traffic_limit)
                            : t('users.unlimited')}{' '}
                      ({(p.protocols ?? []).join(' ')})
                    </option>
                  ))}
                  {plans.length === 0 && <option value="">{t('users.c.noPlans')}</option>}
                </select>
              </div>
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.c.duration')}</label>
                <input type="number" name="duration_days" defaultValue={30} min={1} className="w-full rounded border border-[var(--border-default)] bg-[var(--bg-surface)] px-3 py-1.5 font-mono text-xs text-[var(--text-primary)]" />
                <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">{t('users.c.durationHint')}</p>
              </div>
            </div>
          ) : (
            <div className="space-y-3 rounded border border-[var(--border-subtle)] bg-[var(--bg-canvas)] p-3">
              <div>
                <div className="mb-1 flex items-center justify-between">
                  <label className="font-medium text-[var(--text-secondary)]">{t('users.c.limit')}</label>
                  <label className="flex cursor-pointer items-center gap-1.5 text-[11px] text-[var(--text-muted)]">
                    <input type="checkbox" checked={unlimited} onChange={(e) => setUnlimited(e.target.checked)} className="rounded border-[var(--border-default)]" />
                    <span>{t('users.c.unlimited')}</span>
                  </label>
                </div>
                <input type="number" name="traffic_limit_gb" defaultValue={100} min={1} disabled={unlimited} className={`${inputCls} ${unlimited ? 'cursor-not-allowed opacity-40' : ''}`} />
              </div>
              <div>
                <div className="mb-1 flex items-center justify-between">
                  <label className="font-medium text-[var(--text-secondary)]">{t('users.c.validity')}</label>
                  <label className="flex cursor-pointer items-center gap-1.5 text-[11px] text-[var(--text-muted)]">
                    <input type="checkbox" checked={neverExp} onChange={(e) => setNeverExp(e.target.checked)} className="rounded border-[var(--border-default)]" />
                    <span>{t('users.c.never')}</span>
                  </label>
                </div>
                <input type="number" name="duration_days" defaultValue={365} min={1} disabled={neverExp} className={`${inputCls} ${neverExp ? 'cursor-not-allowed opacity-40' : ''}`} />
              </div>
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.c.note')}</label>
                <input name="note" placeholder={t('users.c.notePh')} className={inputCls} />
              </div>
            </div>
          )}
          <div className="flex justify-end space-x-2 border-t border-[var(--border-subtle)] pt-3">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
            >
              {t('users.c.cancel')}
            </button>
            <button type="submit" className="rounded bg-zinc-900 px-4 py-1.5 text-xs font-medium text-zinc-100 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200">
              {t('users.c.create')}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function ModalShell({ title, sub, onClose, children }: { title: string; sub: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="w-full max-w-lg rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl">
        <div className="mb-4 flex items-center justify-between border-b border-[var(--border-subtle)] pb-3">
          <div>
            <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{title}</h3>
            <p className="tabular-nums mt-0.5 text-xs text-[var(--text-muted)]">{sub}</p>
          </div>
          <button onClick={onClose} className="text-[var(--text-muted)] hover:text-[var(--text-primary)]">
            &times;
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

export function DmModal({ user, onClose, onDone }: { user: UserRow; onClose: () => void; onDone: () => void }) {
  const { t } = useLang();
  const [text, setText] = useState('');
  const [btnText, setBtnText] = useState('');
  const [btnUrl, setBtnUrl] = useState('');
  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)] text-xs';

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
      if (!tokenRes.ok) return;
      const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
      await fetch(`/admin/users/${user.id}/message`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ message_text: text, button_text: btnText, button_url: btnUrl, csrf_token }).toString(),
      });
      onDone();
    } catch {
      onDone();
    }
  };

  return (
    <ModalShell title={t('users.dm.title')} sub={`${user.username}`} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)} className="space-y-4 text-xs">
        <div>
          <label className="mb-1 block font-medium text-[var(--text-secondary)]">
            {t('users.dm.text')} <span className="text-rose-500">*</span>
          </label>
          <textarea value={text} onChange={(e) => setText(e.target.value)} rows={4} required className={inputCls} />
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">Button Text</label>
            <input value={btnText} onChange={(e) => setBtnText(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">Button URL</label>
            <input value={btnUrl} onChange={(e) => setBtnUrl(e.target.value)} className={inputCls} />
          </div>
        </div>
        <div className="flex justify-end space-x-2 border-t border-[var(--border-subtle)] pt-3">
          <button
            type="button" onClick={onClose}
            className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
          >
            {t('users.c.cancel')}
          </button>
          <button type="submit" className="rounded bg-zinc-900 px-4 py-1.5 text-xs font-medium text-zinc-100 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200">
            {t('users.dm.send')}
          </button>
        </div>
      </form>
    </ModalShell>
  );
}

export function AssignModal({
  user, plans, onClose, onDone,
}: {
  user: UserRow;
  plans: { id: string; name: string }[];
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useLang();
  const [planId, setPlanId] = useState(plans[0]?.id ?? '');
  const [days, setDays] = useState(30);
  const [notify, setNotify] = useState(true);
  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)] text-xs';

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
      if (!tokenRes.ok) return;
      const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
      await fetch(`/admin/users/${user.id}/assign-plan`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({
          plan_id: planId,
          duration_days: String(days),
          notify_user: notify ? 'true' : 'false',
          csrf_token,
        }).toString(),
      });
      onDone();
    } catch {
      onDone();
    }
  };

  return (
    <ModalShell title={t('users.assign.title')} sub={`${t('users.assign.sub')} — ${user.username}`} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)} className="space-y-4 text-xs">
        <div>
          <label className="mb-1 block font-medium text-[var(--text-secondary)]">
            {t('users.assign.plan')} <span className="text-rose-500">*</span>
          </label>
          <select value={planId} onChange={(e) => setPlanId(e.target.value)} required className={inputCls}>
            {plans.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.assign.duration')}</label>
          <input type="number" value={days} min={1} onChange={(e) => setDays(Number(e.target.value))} className={inputCls} />
          <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">{t('users.assign.durationHint')}</p>
        </div>
        <label className="flex cursor-pointer items-center gap-2">
          <input type="checkbox" checked={notify} onChange={(e) => setNotify(e.target.checked)} className="rounded border-[var(--border-default)]" />
          <span className="text-xs font-medium text-[var(--text-primary)]">{t('users.assign.notify')}</span>
        </label>
        <div className="flex justify-end space-x-2 border-t border-[var(--border-subtle)] pt-3">
          <button
            type="button" onClick={onClose}
            className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
          >
            {t('users.c.cancel')}
          </button>
          <button type="submit" className="rounded bg-zinc-900 px-4 py-1.5 text-xs font-medium text-zinc-100 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200">
            {t('users.assign.go')}
          </button>
        </div>
      </form>
    </ModalShell>
  );
}

export function EmailModal({ user, onClose, onDone }: { user: UserRow; onClose: () => void; onDone: () => void }) {
  const { t } = useLang();
  const [email, setEmail] = useState(typeof user.email === 'string' ? user.email : '');
  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)] text-xs';

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
      if (!tokenRes.ok) return;
      const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
      await fetch(`/admin/users/${user.id}/email`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ email, csrf_token }).toString(),
      });
      onDone();
    } catch {
      onDone();
    }
  };

  return (
    <ModalShell title={t('users.email.title')} sub={`${t('users.email.sub')} — ${user.username}`} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)} className="space-y-4 text-xs">
        <div>
          <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('users.email.label')}</label>
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="user@example.com" className={inputCls} />
        </div>
        <div className="flex justify-end space-x-2 border-t border-[var(--border-subtle)] pt-3">
          <button
            type="button" onClick={onClose}
            className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
          >
            {t('users.c.cancel')}
          </button>
          <button type="submit" className="rounded bg-zinc-900 px-4 py-1.5 text-xs font-medium text-zinc-100 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200">
            {t('users.email.save')}
          </button>
        </div>
      </form>
    </ModalShell>
  );
}
