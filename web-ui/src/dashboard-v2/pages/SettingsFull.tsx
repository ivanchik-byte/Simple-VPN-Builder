import { useEffect, useState } from 'react';
import { useLang } from '../lang';
import { Card, TableSkeleton } from '../components';

async function postForm(action: string, fields: Record<string, string>): Promise<boolean> {
  try {
    const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
    if (!tokenRes.ok) return false;
    const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
    const body = new URLSearchParams({ ...fields, csrf_token });
    const res = await fetch(action, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });
    return res.ok || res.status === 303;
  } catch {
    return false;
  }
}

interface SettingsBundle {
  role: string;
  admin_id: string;
  totp: { enabled: boolean; secret?: string; otpauth_url?: string };
  admins: { id: string; email: string; role: string; totp_enabled: boolean; created_at: string }[];
  api_keys: { id: string; name: string; prefix: string; scopes: string[]; expires_at: string; created_at: string }[];
  gateways: { id: string; name: string; is_enabled: boolean; config: Record<string, unknown>; title: string }[];
  billing: Record<string, string | number | boolean>;
  bot_replies: Record<string, string>;
  bot_reply_categories: { name: string; badge: string; replies: { key: string; label: string; description: string }[] }[];
  referral: Record<string, string | number | boolean>;
  email: Record<string, string | number | boolean>;
  tenants: Record<string, unknown>[];
  ai: Record<string, string | boolean>;
  retention: Record<string, unknown>;
}

const TABS = ['admins', 'billing', 'replies', 'referrals', 'security', 'partners', 'ai'] as const;

export function SettingsFullPage() {
  const { t } = useLang();
  const [tab, setTab] = useState<(typeof TABS)[number]>('admins');
  const [data, setData] = useState<SettingsBundle | null>(null);
  const [msg, setMsg] = useState('');
  const [rawKey, setRawKey] = useState('');
  const [totpOpen, setTotpOpen] = useState(false);

  const load = async () => {
    try {
      const res = await fetch('/admin/settings-data', { credentials: 'include' });
      if (res.status === 401) {
        window.location.href = '/admin/login';
        return;
      }
      setData((await res.json()) as SettingsBundle);
    } catch {
      setMsg(t('set.err.load'));
    }
  };

  useEffect(() => {
    void load();
    const reload = () => void load();
    window.addEventListener('settings-reload', reload);
    return () => window.removeEventListener('settings-reload', reload);
  }, []);

  const save = async (action: string, fields: Record<string, string>) => {
    const ok = await postForm(action, fields);
    setMsg(ok ? String(t('set.saved')) : String(t('set.failed')));
    if (ok) await load();
  };

  const tabBtn = (id: (typeof TABS)[number], label: string, n: number) => (
    <button
      key={id}
      onClick={() => setTab(id)}
      className={`flex items-center gap-2 whitespace-nowrap rounded-full border px-3.5 py-1.5 text-xs transition-all ${
        tab === id
          ? 'border-emerald-500/40 bg-emerald-500/10 font-semibold text-emerald-700 dark:text-emerald-300 shadow-[0_0_12px_rgba(16,185,129,0.15)]'
          : 'border-[var(--border-subtle)] text-[var(--text-secondary)] hover:border-[var(--border-default)] hover:text-[var(--text-primary)]'
      }`}
    >
      <span className={`tabular-nums flex h-4 w-4 items-center justify-center rounded-full text-[9px] font-bold ${tab === id ? 'bg-emerald-500/20 text-emerald-700 dark:text-emerald-300' : 'bg-[var(--bg-hover)] text-[var(--text-muted)]'}`}>
        {n}
      </span>
      {label}
    </button>
  );

  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)] text-xs';
  const btnPrimary =
    'rounded bg-zinc-900 px-4 py-1.5 text-xs font-medium text-zinc-100 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200';

  if (!data) return <TableSkeleton rows={8} />;

  const str = (v: unknown): string => (typeof v === 'string' ? v : v == null ? '' : String(v));
  const bool = (v: unknown): boolean => v === true || v === 'true';

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('set.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('set.subtitle')}</p>
      </div>

      {msg && (
        <div className="tabular-nums flex items-center justify-between rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-3 text-xs text-[var(--text-secondary)]">
          <span>{msg}</span>
          <button onClick={() => setMsg('')} className="font-bold">
            &times;
          </button>
        </div>
      )}

      <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-4">
      <div className="flex flex-wrap gap-2 overflow-x-auto pb-1">
        {tabBtn('admins', t('set.tab.admins'), 1)}
        {tabBtn('billing', t('set.tab.billing'), 2)}
        {tabBtn('replies', t('set.tab.replies'), 3)}
        {tabBtn('referrals', t('set.tab.referrals'), 4)}
        {tabBtn('security', t('set.tab.security'), 5)}
        {tabBtn('partners', t('set.tab.partners'), 6)}
        {tabBtn('ai', t('set.tab.ai'), 7)}
      </div>
      <p className="mt-3 border-t border-[var(--border-subtle)] pt-3 text-xs text-[var(--text-muted)]">
        {tab === 'admins' && t('set.subtitle')}
        {tab === 'billing' && t('set.billing.sub')}
        {tab === 'replies' && t('set.replies.sub')}
        {tab === 'referrals' && t('set.referrals.sub')}
        {tab === 'security' && t('set.security.sub')}
        {tab === 'partners' && t('set.partners.sub')}
        {tab === 'ai' && t('set.subtitle')}
      </p>
      </div>

      {rawKey && (
        <div className="rounded border border-emerald-500/30 bg-emerald-500/10 p-4">
          <div className="text-xs font-semibold text-emerald-300">API key issued — copy now, it will not be shown again.</div>
          <div className="mt-2 flex items-center gap-2">
            <code className="tabular-nums flex-1 select-all break-all rounded bg-black/40 px-3 py-2 font-mono text-xs text-emerald-300">{rawKey}</code>
            <button
              onClick={() => {
                void navigator.clipboard.writeText(rawKey);
              }}
              className="shrink-0 rounded bg-emerald-500/20 px-3 py-2 text-xs font-semibold text-emerald-200 hover:bg-emerald-500/30"
            >
              {t('set.copyToken')}
            </button>
            <button onClick={() => setRawKey('')} className="shrink-0 text-[var(--text-muted)] hover:text-[var(--text-primary)]">
              &times;
            </button>
          </div>
        </div>
      )}

      {tab === 'admins' && (
        <AdminsTab
          data={data}
          inputCls={inputCls}
          btnPrimary={btnPrimary}
          onSave={save}
          onKeyIssued={(k) => setRawKey(k)}
          onTotp={() => setTotpOpen(true)}
        />
      )}
      {tab === 'billing' && (
        <BillingTab data={data} inputCls={inputCls} btnPrimary={btnPrimary} onSave={save} />
      )}
      {tab === 'replies' && (
        <RepliesTab data={data} inputCls={inputCls} btnPrimary={btnPrimary} onSave={save} />
      )}
      {tab === 'referrals' && (
        <GenericForm
          title={t('set.ref.title')}
          action="/admin/settings/referrals"
          inputCls={inputCls}
          btnPrimary={btnPrimary}
          onSave={save}
          fields={[
            { name: 'reward_model', label: String(t('set.ref.model')), value: str(data.referral['reward_model']) },
            { name: 'inviter_days', label: String(t('set.ref.inviter')), value: str(data.referral['inviter_days']), type: 'number' },
            { name: 'invitee_days', label: String(t('set.ref.invitee')), value: str(data.referral['invitee_days']), type: 'number' },
            { name: 'commission_percent', label: String(t('set.ref.commission')), value: str(data.referral['commission_percent']), type: 'number' },
            { name: 'min_purchase_amount', label: String(t('set.ref.min')), value: str(data.referral['min_purchase_amount']) },
            { name: 'qualification', label: String(t('set.ref.qual')), value: str(data.referral['qualification']) },
            { name: 'daily_cap', label: String(t('set.ref.cap')), value: str(data.referral['daily_cap']), type: 'number' },
          ]}
          checks={[
            { name: 'enabled', label: String(t('set.ref.on')), checked: bool(data.referral['enabled']) },
            { name: 'reward_expired', label: String(t('set.ref.expired')), checked: bool(data.referral['reward_expired']) },
          ]}
        />
      )}
      {tab === 'security' && (
        <GenericForm
          title={t('set.sec.title')}
          action="/admin/settings/security"
          inputCls={inputCls}
          btnPrimary={btnPrimary}
          onSave={save}
          fields={[
            { name: 'email_policy', label: String(t('set.sec.policyLabel')), value: str(data.email['policy']) },
            { name: 'smtp_host', label: String(t('set.sec.host')), value: str(data.email['smtp_host']) },
            { name: 'smtp_port', label: String(t('set.sec.port')), value: str(data.email['smtp_port']), type: 'number' },
            { name: 'smtp_user', label: String(t('set.sec.user')), value: str(data.email['smtp_user']) },
            { name: 'smtp_password', label: String(t('set.sec.pass')), value: str(data.email['smtp_password']), type: 'password' },
            { name: 'smtp_from_email', label: String(t('set.sec.from')), value: str(data.email['smtp_from_email']) },
          ]}
          checks={[
            { name: 'email_otp_enabled', label: String(t('set.sec.otp')), checked: bool(data.email['otp_enabled']) },
            { name: 'smtp_simulated', label: String(t('set.sec.sim')), checked: bool(data.email['smtp_simulated']) },
          ]}
        />
      )}
      {tab === 'partners' && (
        <PartnersTab data={data} inputCls={inputCls} btnPrimary={btnPrimary} onSave={save} />
      )}
      {totpOpen && data.totp && (
        <TotpModal
          totp={data.totp as { enabled: boolean; secret?: string; otpauth_url?: string }}
          onClose={() => setTotpOpen(false)}
          onDone={() => {
            setTotpOpen(false);
            void load();
          }}
        />
      )}

      {tab === 'ai' && (
        <GenericForm
          title={t('set.ai.title')}
          action="/admin/settings/ai"
          inputCls={inputCls}
          btnPrimary={btnPrimary}
          onSave={save}
          fields={[
            { name: 'ai_base_url', label: String(t('set.ai.url')), value: str(data.ai['base_url']) },
            { name: 'ai_model', label: String(t('set.ai.model')), value: str(data.ai['model']) },
            { name: 'ai_api_key', label: String(t('set.ai.key')), value: str(data.ai['key_masked']), type: 'password' },
          ]}
          checks={[{ name: 'ai_enabled', label: String(t('set.ai.on')), checked: bool(data.ai['enabled']) }]}
        />
      )}
    </div>
  );
}

function GenericForm({
  title, action, inputCls, btnPrimary, onSave, fields, checks,
}: {
  title: string;
  action: string;
  inputCls: string;
  btnPrimary: string;
  onSave: (action: string, fields: Record<string, string>) => Promise<void>;
  fields: { name: string; label: string; value: string; type?: string }[];
  checks: { name: string; label: string; checked: boolean }[];
}) {
  const { t } = useLang();
  const [vals, setVals] = useState<Record<string, string>>(() =>
    Object.fromEntries(fields.map((f) => [f.name, f.value])),
  );
  const [flags, setFlags] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(checks.map((c) => [c.name, c.checked])),
  );
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const out: Record<string, string> = { ...vals };
    for (const [k, v] of Object.entries(flags)) out[k] = v ? 'true' : 'false';
    await onSave(action, out);
  };
  return (
    <Card className="p-5">
      <h3 className="mb-4 text-sm font-semibold text-[var(--text-primary)]">{title}</h3>
      <form onSubmit={(e) => void submit(e)} className="space-y-3 text-xs">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {fields.map((f) => (
          <div key={f.name}>
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">{f.label}</label>
            <input
              type={f.type ?? 'text'}
              value={vals[f.name] ?? ''}
              onChange={(e) => setVals((p) => ({ ...p, [f.name]: e.target.value }))}
              className={inputCls}
            />
          </div>
        ))}
        </div>
        {checks.map((c) => (
          <label key={c.name} className="flex cursor-pointer items-center gap-2 text-xs text-[var(--text-primary)]">
            <input
              type="checkbox"
              checked={!!flags[c.name]}
              onChange={(e) => setFlags((p) => ({ ...p, [c.name]: e.target.checked }))}
              className="rounded border-[var(--border-default)]"
            />
            {c.label}
          </label>
        ))}
        <button type="submit" className={btnPrimary}>
          {t('set.save')}
        </button>
      </form>
    </Card>
  );
}

function AdminsTab({
  data, inputCls, btnPrimary, onSave, onKeyIssued, onTotp,
}: {
  data: {
    role: string;
    admins: { id: string; email: string; role: string; totp_enabled: boolean }[];
    api_keys: { id: string; name: string; prefix: string; scopes: string[]; expires_at: string }[];
  };
  inputCls: string;
  btnPrimary: string;
  onSave: (action: string, fields: Record<string, string>) => Promise<void>;
  onKeyIssued: (k: string) => void;
  onTotp: () => void;
}) {
  const { t } = useLang();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState('admin');
  const [keyName, setKeyName] = useState('');
  const [scope, setScope] = useState('admin');
  const readOnly = data.role === 'admin';

  const issueKey = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
      if (!tokenRes.ok) return;
      const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
      const res = await fetch('/admin/api-keys', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ name: keyName, scope, csrf_token }).toString(),
        redirect: 'manual',
      });
      const loc = res.headers.get('Location') ?? '';
      const m = loc.match(/generated_key=([^&]+)/);
      if (m) onKeyIssued(decodeURIComponent(m[1]));
      setKeyName('');
      window.dispatchEvent(new CustomEvent('settings-reload'));
    } catch {
      /* banner shows on next load */
    }
  };

  return (
    <div className="space-y-6">
      <Card className="p-5">
        <h3 className="mb-4 text-sm font-semibold text-[var(--text-primary)]">{t('set.admins')}</h3>
        <div className="space-y-2">
          {data.admins.map((a) => (
            <div key={a.id} className="flex items-center gap-3 rounded border border-[var(--border-subtle)] px-3 py-2 text-xs">
              <span className="font-medium text-[var(--text-primary)]">{a.email}</span>
              <span className="tabular-nums rounded bg-[var(--bg-hover)] px-1.5 py-0.5 text-[10px] uppercase text-[var(--text-muted)]">
                {a.role}
              </span>
              <span className="tabular-nums text-[10px] text-[var(--text-muted)]">
                2FA: {a.totp_enabled ? t('set.totpOn') : t('set.totpOff')}
              </span>
              {!readOnly && (
                <button
                  onClick={() => {
                    if (window.confirm(`Delete admin ${a.email}?`)) void onSave(`/admin/admins/${a.id}/delete`, {});
                  }}
                  className="ml-auto cursor-pointer rounded bg-rose-500/10 px-2 py-1 text-[11px] text-rose-500 hover:bg-rose-500/20"
                >
                  {t('set.delete')}
                </button>
              )}
            </div>
          ))}
        </div>
        {!readOnly && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void onSave('/admin/admins', { email, password, role }).then(() => {
                setEmail('');
                setPassword('');
              });
            }}
            className="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-4"
          >
            <input value={email} onChange={(e) => setEmail(e.target.value)} required type="email" placeholder="admin@example.com" className={inputCls} />
            <input value={password} onChange={(e) => setPassword(e.target.value)} required type="password" placeholder="••••••••" className={inputCls} />
            <select value={role} onChange={(e) => setRole(e.target.value)} className={inputCls}>
              <option value="admin">admin</option>
              <option value="superadmin">superadmin</option>
              <option value="owner">owner</option>
            </select>
            <button type="submit" className={btnPrimary}>
              {t('set.add')}
            </button>
          </form>
        )}
        <div className="mt-4 border-t border-[var(--border-subtle)] pt-3">
          <button onClick={onTotp} className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs hover:text-[var(--text-primary)]">
            Setup Two-Factor Authentication
          </button>
        </div>
      </Card>

      <RetentionCard inputCls={inputCls} btnPrimary={btnPrimary} onSave={onSave} retention={(data as unknown as { retention: Record<string, unknown> }).retention ?? {}} />
      <Card className="p-5">
        <h3 className="mb-4 text-sm font-semibold text-[var(--text-primary)]">{t('set.keys')}</h3>
        <div className="space-y-2">
          {data.api_keys.map((k) => (
            <div key={k.id} className="flex items-center gap-3 rounded border border-[var(--border-subtle)] px-3 py-2 text-xs">
              <span className="font-medium text-[var(--text-primary)]">{k.name}</span>
              <span className="tabular-nums text-[10px] text-[var(--text-muted)]">{k.prefix}…</span>
              {!readOnly && (
                <button
                  onClick={() => void onSave(`/admin/api-keys/${k.id}/delete`, {})}
                  className="ml-auto cursor-pointer rounded bg-rose-500/10 px-2 py-1 text-[11px] text-rose-500 hover:bg-rose-500/20"
                >
                  {t('set.delete')}
                </button>
              )}
            </div>
          ))}
        </div>
        {!readOnly && (
          <form onSubmit={(e) => void issueKey(e)} className="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-3">
            <input value={keyName} onChange={(e) => setKeyName(e.target.value)} required placeholder="e.g. Bot-Service-Key" className={inputCls} />
            <select value={scope} onChange={(e) => setScope(e.target.value)} className={inputCls}>
              <option value="admin">admin (Full control)</option>
              <option value="read">read (Telemetry only)</option>
              <option value="write">write (Node & User operations)</option>
            </select>
            <button type="submit" className={btnPrimary}>
              {t('set.generate')}
            </button>
          </form>
        )}
      </Card>
    </div>
  );
}

function TotpModal({
  totp, onClose, onDone,
}: {
  totp: { enabled: boolean; secret?: string; otpauth_url?: string };
  onClose: () => void;
  onDone: () => void;
}) {
  const [code, setCode] = useState('');
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
      if (!tokenRes.ok) return;
      const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
      await fetch('/admin/2fa/enable', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ secret: totp.secret ?? '', code, csrf_token }).toString(),
      });
      onDone();
    } catch {
      onDone();
    }
  };
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="w-full max-w-md rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-[var(--text-primary)]">Setup Two-Factor Authentication</h3>
          <button onClick={onClose} className="text-[var(--text-muted)] hover:text-[var(--text-primary)]">
            &times;
          </button>
        </div>
        {totp.enabled ? (
          <p className="text-xs text-[var(--text-secondary)]">Two-factor authentication is already enabled for your account.</p>
        ) : (
          <div className="space-y-4 text-xs">
            <p className="text-[var(--text-secondary)]">
              1. Scan this QR code using Google Authenticator, Yandex Key, Telegram, or any TOTP app:
            </p>
            {totp.otpauth_url && (
              <div className="flex justify-center rounded border border-[var(--border-subtle)] bg-white p-3">
                <img src={`/admin/qr?text=${encodeURIComponent(totp.otpauth_url)}`} alt="TOTP QR Code" className="block h-48 w-48" />
              </div>
            )}
            <div>
              <div className="tabular-nums mb-1 text-[11px] text-[var(--text-muted)]">Or enter this secret key manually:</div>
              <div className="tabular-nums break-all rounded bg-black/40 p-2 text-center text-sm font-bold tracking-wider text-emerald-400 select-all">
                {totp.secret}
              </div>
            </div>
            <form onSubmit={(e) => void submit(e)} className="space-y-3 pt-2">
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">
                  2. Enter the 6-digit code from your app to verify:
                </label>
                <input
                  value={code} onChange={(e) => setCode(e.target.value)} required
                  placeholder="000 000" maxLength={10} autoComplete="one-time-code"
                  className="w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 text-center font-mono text-[var(--text-primary)]"
                />
              </div>
              <button type="submit" className="w-full rounded bg-zinc-900 px-4 py-2 text-xs font-medium text-zinc-100 hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200">
                Enable 2FA
              </button>
            </form>
          </div>
        )}
      </div>
    </div>
  );
}

function BillingTab({
  data, inputCls, btnPrimary, onSave,
}: {
  data: {
    billing: Record<string, string | number | boolean>;
    gateways: { id: string; name: string; is_enabled: boolean; config: Record<string, unknown>; title: string }[];
  };
  inputCls: string;
  btnPrimary: string;
  onSave: (action: string, fields: Record<string, string>) => Promise<void>;
}) {
  const { t } = useLang();
  const b = data.billing;
  const str = (v: unknown): string => (typeof v === 'string' ? v : v == null ? '' : String(v));
  const [f, setF] = useState<Record<string, string>>({
    cryptobot_api_token: str(b['cryptobot_api_token']),
    webhook_secret: str(b['webhook_secret']),
    stars_price_per_month: str(b['stars_price_per_month']),
  });
  const [flags, setFlags] = useState({ crypto: b['cryptobot_enabled'] === true, stars: b['telegram_stars_enabled'] === true });
  const [gwName, setGwName] = useState('');
  const [gwToken, setGwToken] = useState('');

  return (
    <div className="space-y-6">
      <Card className="p-5">
        <h3 className="mb-4 text-sm font-semibold text-[var(--text-primary)]">{t('set.billing.title')}</h3>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void onSave('/admin/settings/billing', {
              ...f,
              cryptobot_enabled: flags.crypto ? 'true' : 'false',
              telegram_stars_enabled: flags.stars ? 'true' : 'false',
            });
          }}
          className="max-w-xl space-y-3 text-xs"
        >
          <div>
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('set.billing.crypto')}</label>
            <input type="password" value={f.cryptobot_api_token} onChange={(e) => setF((p) => ({ ...p, cryptobot_api_token: e.target.value }))} className={inputCls} />
          </div>
          <label className="flex cursor-pointer items-center gap-2 text-xs">
            <input type="checkbox" checked={flags.crypto} onChange={(e) => setFlags((p) => ({ ...p, crypto: e.target.checked }))} className="rounded" />
            {t('set.billing.cryptoOn')}
          </label>
          <label className="flex cursor-pointer items-center gap-2 text-xs">
            <input type="checkbox" checked={flags.stars} onChange={(e) => setFlags((p) => ({ ...p, stars: e.target.checked }))} className="rounded" />
            {t('set.billing.starsOn')}
          </label>
          <div>
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('set.billing.starsPrice')}</label>
            <input type="number" value={f.stars_price_per_month} onChange={(e) => setF((p) => ({ ...p, stars_price_per_month: e.target.value }))} className={inputCls} />
          </div>
          <div>
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('set.billing.webhook')}</label>
            <input type="password" value={f.webhook_secret} onChange={(e) => setF((p) => ({ ...p, webhook_secret: e.target.value }))} className={inputCls} />
          </div>
          <button type="submit" className={btnPrimary}>
            {t('set.save')}
          </button>
        </form>
      </Card>

      <Card className="p-5">
        <h3 className="mb-4 text-sm font-semibold text-[var(--text-primary)]">{t('set.gateways')}</h3>
        <div className="space-y-2">
          {data.gateways.map((g) => (
            <div key={g.id} className="flex items-center gap-3 rounded border border-[var(--border-subtle)] px-3 py-2 text-xs">
              <span className="font-medium text-[var(--text-primary)]">{g.title || g.name}</span>
              <span className={`tabular-nums rounded px-1.5 py-0.5 text-[10px] ${g.is_enabled ? 'bg-emerald-500/10 text-emerald-400' : 'bg-zinc-500/10 text-zinc-400'}`}>
                {g.is_enabled ? t('set.gw.on') : t('set.gw.off')}
              </span>
              <button
                onClick={() => void onSave(`/admin/gateways/${g.name}/toggle`, {})}
                className="ml-auto cursor-pointer rounded bg-[var(--bg-hover)] px-2 py-1 text-[11px] hover:text-[var(--text-primary)]"
              >
                {t('set.gw.toggle')}
              </button>
              <button
                onClick={() => void onSave(`/admin/gateways/${g.name}/delete`, {})}
                className="cursor-pointer rounded bg-rose-500/10 px-2 py-1 text-[11px] text-rose-500 hover:bg-rose-500/20"
              >
                {t('set.delete')}
              </button>
            </div>
          ))}
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void onSave('/admin/gateways/create', { name: gwName, token: gwToken }).then(() => {
              setGwName('');
              setGwToken('');
            });
          }}
          className="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-3"
        >
          <input value={gwName} onChange={(e) => setGwName(e.target.value)} required placeholder={t('set.gw.name')} className={inputCls} />
          <input value={gwToken} onChange={(e) => setGwToken(e.target.value)} type="password" placeholder={t('set.gw.token')} className={inputCls} />
          <button type="submit" className={btnPrimary}>
            {t('set.gw.add')}
          </button>
        </form>
      </Card>
    </div>
  );
}

function RepliesTab({
  data, inputCls, btnPrimary, onSave,
}: {
  data: {
    bot_replies: Record<string, string>;
    bot_reply_categories: { name: string; badge: string; replies: { key: string; label: string; description: string }[] }[];
  };
  inputCls: string;
  btnPrimary: string;
  onSave: (action: string, fields: Record<string, string>) => Promise<void>;
}) {
  const { t } = useLang();
  const [vals, setVals] = useState<Record<string, string>>({});
  const [extra, setExtra] = useState({ bot_token: '', channel_link: '', support_link: '' });

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const out: Record<string, string> = { ...extra };
    for (const cat of data.bot_reply_categories) {
      for (const r of cat.replies) {
        if (vals[r.key] !== undefined) out[`reply_${r.key}`] = vals[r.key];
      }
    }
    await onSave('/admin/settings/bot-replies', out);
  };

  return (
    <form onSubmit={(e) => void submit(e)} className="space-y-4">
      <Card className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-3">
        <div>
          <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">{t('set.replies.botToken')}</label>
          <input type="password" value={extra.bot_token} onChange={(e) => setExtra((p) => ({ ...p, bot_token: e.target.value }))} placeholder={t('set.replies.maskedHint')} className={inputCls} />
        </div>
        <div>
          <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">{t('set.replies.channel')}</label>
          <input value={extra.channel_link} onChange={(e) => setExtra((p) => ({ ...p, channel_link: e.target.value }))} className={inputCls} />
        </div>
        <div>
          <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">{t('set.replies.support')}</label>
          <input value={extra.support_link} onChange={(e) => setExtra((p) => ({ ...p, support_link: e.target.value }))} className={inputCls} />
        </div>
      </Card>
      {data.bot_reply_categories.map((cat) => (
        <Card key={cat.name} className="overflow-hidden">
          <div className="flex items-center justify-between border-b border-[var(--border-subtle)] bg-[var(--bg-hover)]/40 px-4 py-3">
            <span className="text-xs font-semibold text-[var(--text-primary)]">{cat.name}</span>
            <span className="tabular-nums text-[11px] text-[var(--text-muted)]">{cat.replies.length} {t('set.replies.templates')}</span>
          </div>
          <div className="divide-y divide-[var(--border-subtle)]">
            {cat.replies.map((r) => (
              <div key={r.key} className="p-4">
                <div className="mb-1 flex items-center gap-2">
                  <span className="text-xs font-medium text-[var(--text-primary)]">{r.label}</span>
                  <code className="tabular-nums rounded border border-[var(--border-subtle)] bg-[var(--bg-canvas)] px-1.5 py-0.5 text-[10px] text-[var(--text-muted)]">
                    {r.key}
                  </code>
                </div>
                {r.description && <p className="mb-2 text-[11px] leading-relaxed text-[var(--text-muted)]">{r.description}</p>}
                <textarea
                  rows={3}
                  value={vals[r.key] ?? data.bot_replies[r.key] ?? ''}
                  onChange={(e) => setVals((p) => ({ ...p, [r.key]: e.target.value }))}
                  className={`${inputCls} font-mono`}
                />
              </div>
            ))}
          </div>
        </Card>
      ))}
      <button type="submit" className={btnPrimary}>
        {t('set.save')}
      </button>
    </form>
  );
}

function PartnersTab({
  data, inputCls, btnPrimary, onSave,
}: {
  data: { tenants: Record<string, unknown>[] };
  inputCls: string;
  btnPrimary: string;
  onSave: (action: string, fields: Record<string, string>) => Promise<void>;
}) {
  const { t } = useLang();
  const [f, setF] = useState({ name: '', slug: '', bot_token: '', bot_username: '', channel_link: '', support_link: '', custom_domain: '', miniapp_url: '', welcome_text: '' });
  const set = (k: string, v: string) => setF((p) => ({ ...p, [k]: v }));
  const field = (k: string, label: string, ph = '') => (
    <div>
      <label className="mb-1 block font-medium text-[var(--text-secondary)]">{label}</label>
      <input value={f[k as keyof typeof f]} onChange={(e) => set(k, e.target.value)} placeholder={ph} className={inputCls} />
    </div>
  );

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {(data.tenants ?? []).map((tn: Record<string, unknown>, i: number) => (
          <Card key={String(tn['id'] ?? i)} className="p-5">
            <h3 className="text-sm font-semibold text-[var(--text-primary)]">{String(tn['name'] ?? '')}</h3>
            <p className="tabular-nums mt-1 text-[11px] text-[var(--text-muted)]">{String(tn['slug'] ?? '')}</p>
            <button
              onClick={() => {
                if (window.confirm(t('set.partner.deleteConfirm', { name: String(tn['name'] ?? '') }))) void onSave(`/admin/settings/partners/${String(tn['id'] ?? '')}/delete`, {});
              }}
              className="mt-3 cursor-pointer rounded bg-rose-500/10 px-2 py-1 text-[11px] text-rose-500 hover:bg-rose-500/20"
            >
              {t('set.delete')}
            </button>
          </Card>
        ))}
        {(data.tenants ?? []).length === 0 && (
          <Card className="p-5 text-xs text-[var(--text-muted)]">{t('set.partners.empty')}</Card>
        )}
      </div>
      <Card className="p-5">
        <h3 className="mb-4 text-sm font-semibold text-[var(--text-primary)]">{t('set.partners.add')}</h3>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void onSave('/admin/settings/partners/create', { ...f }).then(() =>
              setF({ name: '', slug: '', bot_token: '', bot_username: '', channel_link: '', support_link: '', custom_domain: '', miniapp_url: '', welcome_text: '' }),
            );
          }}
          className="grid grid-cols-1 gap-3 text-xs sm:grid-cols-2"
        >
          {field('name', t('set.partners.name'))}
          {field('slug', t('set.partners.slug'))}
          {field('bot_token', t('set.partners.token'))}
          {field('bot_username', t('set.partners.username'))}
          {field('channel_link', t('set.partners.channel'))}
          {field('support_link', t('set.partners.support'))}
          {field('custom_domain', t('set.partners.domain'))}
          {field('miniapp_url', t('set.partners.miniapp'))}
          <div className="sm:col-span-2">
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('set.partners.welcome')}</label>
            <textarea value={f.welcome_text} onChange={(e) => set('welcome_text', e.target.value)} rows={2} className={inputCls} />
          </div>
          <div className="sm:col-span-2">
            <button type="submit" className={btnPrimary}>
              {t('set.add')}
            </button>
          </div>
        </form>
      </Card>
    </div>
  );
}

function RetentionCard({
  inputCls, btnPrimary, onSave, retention,
}: {
  inputCls: string;
  btnPrimary: string;
  onSave: (action: string, fields: Record<string, string>) => Promise<void>;
  retention: Record<string, unknown>;
}) {
  const { t } = useLang();
  const num = (v: unknown, d: number): number => (typeof v === 'number' ? v : d);
  const [days, setDays] = useState(num(retention['retention_days'], 90));
  const flags = ['log_direct_messages', 'log_auth', 'log_user_mgmt', 'log_billing', 'log_nodes', 'log_settings'];
  const [on, setOn] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(flags.map((f) => [f, retention[f] !== false])),
  );
  return (
    <Card className="p-5">
      <h3 className="mb-1 text-sm font-semibold text-[var(--text-primary)]">{t('set.retention')}</h3>
      <p className="mb-4 text-xs text-[var(--text-muted)]">{t('set.retention.sub')}</p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          const out: Record<string, string> = { retention_days: String(days) };
          for (const [k, v] of Object.entries(on)) out[k] = v ? 'true' : 'false';
          void onSave('/admin/settings/retention', out);
        }}
        className="flex flex-wrap items-end gap-3 text-xs"
      >
        <div>
          <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('set.retention.days')}</label>
          <input type="number" min={0} max={3650} value={days} onChange={(e) => setDays(Number(e.target.value))} className={`${inputCls} w-32`} />
        </div>
        <div className="flex flex-wrap gap-3">
          {flags.map((f) => (
            <label key={f} className="flex cursor-pointer items-center gap-1.5 text-[11px] text-[var(--text-secondary)]">
              <input type="checkbox" checked={!!on[f]} onChange={(e) => setOn((p) => ({ ...p, [f]: e.target.checked }))} className="rounded" />
              {f.replace('log_', '')}
            </label>
          ))}
        </div>
        <button type="submit" className={btnPrimary}>
          {t('set.save')}
        </button>
      </form>
    </Card>
  );
}
