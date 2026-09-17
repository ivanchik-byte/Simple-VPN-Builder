import { useState } from 'react';
import { useLang } from '../lang';

export function LoginPage() {
  const { t } = useLang();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError('');
    setBusy(true);
    try {
      const fd = new FormData(e.currentTarget);
      const res = await fetch('/admin/login', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({
          username: String(fd.get('username') ?? ''),
          password: String(fd.get('password') ?? ''),
          totp_code: String(fd.get('totp_code') ?? ''),
        }).toString(),
        redirect: 'manual',
      });
      if (res.status === 303 || res.status === 302 || res.status === 0 || res.ok) {
        const loc = res.headers.get('Location') ?? '';
        if (loc.includes('/admin/login')) {
          setError(t('login.failed'));
        } else {
          window.location.href = '/admin/dashboard-v2';
        }
      } else {
        setError(t('login.failed'));
      }
    } catch {
      setError(t('login.failed'));
    } finally {
      setBusy(false);
    }
  };

  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)] text-sm focus:outline-none focus:ring-1 focus:ring-zinc-400';

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="w-full max-w-sm rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-lg">
        <div className="mb-6 text-center">
          <h1 className="text-lg font-semibold tracking-tight text-[var(--text-primary)]">{t('login.title')}</h1>
          <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('login.subtitle')}</p>
        </div>
        {error && (
          <div className="tabular-nums mb-4 rounded border border-rose-500/20 bg-rose-500/10 p-2.5 text-xs text-rose-500">
            {error}
          </div>
        )}
        <form onSubmit={(e) => void submit(e)} className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">{t('login.user')}</label>
            <input name="username" required autoFocus autoComplete="username" placeholder={t('login.userPh')} className={inputCls} />
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">{t('login.pass')}</label>
            <input type="password" name="password" required autoComplete="current-password" placeholder="••••••••" className={inputCls} />
          </div>
          <div>
            <label className="mb-1 block text-xs font-medium text-[var(--text-secondary)]">
              {t('login.totp')} <span className="font-normal text-[var(--text-muted)]">{t('login.totpHint')}</span>
            </label>
            <input name="totp_code" placeholder="000 000" maxLength={10} autoComplete="one-time-code" className={`${inputCls} text-center tracking-widest`} />
          </div>
          <button
            type="submit"
            disabled={busy}
            className="mt-2 w-full rounded bg-zinc-900 px-4 py-2 text-xs font-medium uppercase tracking-wide text-zinc-100 transition-colors hover:bg-zinc-800 disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200"
          >
            {t('login.go')}
          </button>
        </form>
      </div>
    </div>
  );
}
