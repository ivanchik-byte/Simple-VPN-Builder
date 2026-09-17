import { useEffect, useState } from 'react';
import { useLang } from '../lang';
import { Card, EmptyState, TableSkeleton } from '../components';
import { api, type ApiCredential } from '../api';

async function postForm(action: string): Promise<void> {
  const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
  if (!tokenRes.ok) throw new Error('csrf');
  const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
  const body = new URLSearchParams({ csrf_token });
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

export function CredentialsPage() {
  const { t } = useLang();
  const [creds, setCreds] = useState<ApiCredential[] | null>(null);
  const [error, setError] = useState('');

  const load = async () => {
    try {
      setCreds(await api.credentials());
    } catch {
      setError(t('creds.err.load'));
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const act = async (action: string) => {
    setError('');
    try {
      await postForm(action);
      await load();
    } catch {
      setError(t('creds.err.action'));
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('creds.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('creds.subtitle')}</p>
      </div>

      {error && (
        <div className="tabular-nums flex items-center justify-between rounded border border-rose-500/30 bg-rose-500/10 p-4 text-xs text-rose-500">
          <span>{error}</span>
          <button onClick={() => setError('')} className="text-sm font-bold text-rose-400 hover:text-rose-200">
            &times;
          </button>
        </div>
      )}

      <div className="flex items-center justify-between">
        <div className="tabular-nums text-xs text-[var(--text-muted)]">
          {t('creds.total')} <span className="font-semibold text-[var(--text-primary)]">{creds?.length ?? '—'}</span>
        </div>
      </div>

      <Card className="overflow-hidden">
        {creds === null ? (
          <TableSkeleton rows={5} />
        ) : creds.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[820px] text-left text-xs">
              <thead>
                <tr className="tabular-nums border-b border-[var(--border-subtle)] bg-[var(--bg-hover)] text-[10px] uppercase text-[var(--text-muted)]">
                  <th className="px-5 py-2.5 font-medium">{t('creds.col.user')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('creds.col.proto')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('creds.col.ipv4')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('creds.col.key')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('creds.col.status')}</th>
                  <th className="px-5 py-2.5 text-right font-medium">{t('creds.col.actions')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-subtle)]">
                {creds.map((c) => (
                  <tr key={c.id} className="transition-colors hover:bg-[var(--bg-hover)]/50">
                    <td className="px-5 py-3 font-mono font-medium text-[var(--text-primary)]">
                      <a href={`/admin/users/${c.user_id}`} className="hover:underline">
                        {c.user_id.slice(0, 8)}...
                      </a>
                    </td>
                    <td className="px-5 py-3 font-mono uppercase text-[var(--text-secondary)]">{c.protocol}</td>
                    <td className="px-5 py-3 font-mono font-semibold text-[var(--text-primary)]">{c.ipv4 ?? '—'}</td>
                    <td className="max-w-[200px] truncate px-5 py-3 font-mono text-[var(--text-muted)]" title={c.public_key ?? t('creds.vless')}>
                      {c.public_key ?? t('creds.uuid')}
                    </td>
                    <td className="px-5 py-3">
                      <span className="tabular-nums rounded px-2 py-0.5 text-[10px]">{c.status ?? ''}</span>
                    </td>
                    <td className="space-x-2 px-5 py-3 text-right">
                      <button
                        onClick={() => void act(`/admin/credentials/${c.id}/rotate`)}
                        title={t('creds.rotateTitle')}
                        className="tabular-nums cursor-pointer rounded bg-[var(--bg-hover)] px-2 py-1 text-[11px] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
                      >
                        {t('creds.rotate')}
                      </button>
                      <button
                        onClick={() => {
                          if (window.confirm(`${t('creds.delTitle')}\n${t('creds.deleteConfirm')}`)) {
                            void act(`/admin/credentials/${c.id}/delete`);
                          }
                        }}
                        className="tabular-nums cursor-pointer rounded bg-rose-500/10 px-2 py-1 text-[11px] text-rose-500 transition-colors hover:bg-rose-500/20"
                      >
                        {t('creds.revoke')}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState title={t('creds.empty')} hint="" />
        )}
      </Card>
    </div>
  );
}
