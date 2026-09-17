import { useEffect, useState } from 'react';
import { useLang } from '../lang';
import { EmptyState, TableSkeleton } from '../components';

interface Campaign {
  id: string;
  title: string;
  target_segment: string;
  total_recipients: number | null;
  sent_count: number | null;
  failed_count: number | null;
  status: string | null;
  created_at: string | null;
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useLang();
  const s = (status || '').toLowerCase();
  if (s === 'completed') {
    return (
      <span className="tabular-nums rounded border border-emerald-500/20 bg-emerald-500/10 px-2 py-0.5 text-[10px] font-bold text-emerald-700 dark:text-emerald-400">{t('bc.status.completed')}</span>
    );
  }
  if (s === 'in_progress') {
    return (
      <span className="tabular-nums rounded border border-blue-500/20 bg-blue-500/10 px-2 py-0.5 text-[10px] font-bold text-blue-700 dark:text-blue-400">{t('bc.status.sending')}</span>
    );
  }
  if (s === 'failed') {
    return (
      <span className="tabular-nums rounded border border-rose-500/20 bg-rose-500/10 px-2 py-0.5 text-[10px] font-bold text-rose-700 dark:text-rose-400">{t('bc.status.failed')}</span>
    );
  }
  return (
    <span className="tabular-nums rounded border border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-[10px] font-bold text-amber-700 dark:text-amber-400">{t('bc.status.pending')}</span>
  );
}

export function BroadcastPage() {
  const { t } = useLang();
  const [list, setList] = useState<Campaign[] | null>(null);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState('');

  const load = async () => {
    try {
      const res = await fetch('/api/v1/billing/broadcasts', { credentials: 'include' });
      if (res.status === 401) {
        window.location.href = '/admin/login';
        return;
      }
      const data = (await res.json()) as unknown;
      if (Array.isArray(data)) setList(data as Campaign[]);
      else if (data && typeof data === 'object' && 'campaigns' in data) setList((data as { campaigns: Campaign[] }).campaigns ?? []);
      else if (data && typeof data === 'object' && 'items' in data) setList((data as { items: Campaign[] }).items ?? []);
      else setList([]);
    } catch {
      setList([]);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError('');
    setSent(false);
    const fd = new FormData(e.currentTarget);
    const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
    if (!tokenRes.ok) {
      setError(t('bc.err.csrf'));
      return;
    }
    const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
    const body = new URLSearchParams();
    body.set('csrf_token', csrf_token);
    fd.forEach((v, k) => body.set(k, String(v)));
    const res = await fetch('/admin/broadcast', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    });
    if (res.ok || res.status === 303) {
      setSent(true);
      (e.target as HTMLFormElement).reset();
      await load();
    } else {
      setError(t('bc.err.create'));
    }
  };

  const inputCls =
    'w-full rounded border border-[var(--border-subtle)] bg-[var(--bg-card)] px-3 py-2 text-xs text-[var(--text-primary)] focus:outline-none focus:border-zinc-500';

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('bc.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('bc.subtitle')}</p>
      </div>

      {sent && (
        <div className="tabular-nums rounded border border-emerald-500/20 bg-emerald-500/10 p-4 text-xs text-emerald-400">
          {t('bc.sent')}
        </div>
      )}
      {error && (
        <div className="tabular-nums flex items-center justify-between rounded border border-rose-500/20 bg-rose-500/10 p-4 text-xs text-rose-400">
          <span>{error}</span>
          <button onClick={() => setError('')} className="text-sm font-bold">
            &times;
          </button>
        </div>
      )}

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <div className="rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 lg:sticky lg:top-4 lg:self-start">
          <div className="mb-4 flex items-center gap-2.5">
            <span className="tabular-nums flex h-5 w-5 items-center justify-center rounded-full bg-emerald-500/15 text-[10px] font-bold text-emerald-400">1</span>
            <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{t('bc.compose')}</h3>
          </div>
          <form onSubmit={(e) => void submit(e)} className="space-y-4">
            <div>
              <label className="tabular-nums mb-1 block text-xs uppercase tracking-wider text-[var(--text-muted)]">{t('bc.cTitle')}</label>
              <input name="title" required placeholder={t('bc.cTitlePh')} className={inputCls} />
            </div>
            <div>
              <label className="tabular-nums mb-1 block text-xs uppercase tracking-wider text-[var(--text-muted)]">{t('bc.segment')}</label>
              <select name="segment" className={inputCls}>
                <option value="all">{t('bc.seg.all')}</option>
                <option value="leads">{t('bc.seg.leads')}</option>
                <option value="active">{t('bc.seg.active')}</option>
                <option value="expired">{t('bc.seg.expired')}</option>
              </select>
            </div>
            <div>
              <label className="tabular-nums mb-1 block text-xs uppercase tracking-wider text-[var(--text-muted)]">{t('bc.body')}</label>
              <textarea name="message_text" rows={5} required placeholder={t('bc.bodyPh')} className={`${inputCls} font-mono`} />
            </div>
            <div className="space-y-3 border-t border-[var(--border-subtle)] pt-2">
              <div className="tabular-nums text-[11px] uppercase text-[var(--text-muted)]">{t('bc.btnBox')}</div>
              <div>
                <label className="tabular-nums mb-0.5 block text-[11px] text-[var(--text-muted)]">{t('bc.btnLabel')}</label>
                <input name="button_text" placeholder={t('bc.btnLabelPh')} className={`${inputCls} py-1.5`} />
              </div>
              <div>
                <label className="tabular-nums mb-0.5 block text-[11px] text-[var(--text-muted)]">{t('bc.btnUrl')}</label>
                <input type="url" name="button_url" placeholder="e.g. https://t.me/YourBot?start=buy" className={`${inputCls} py-1.5`} />
              </div>
            </div>
            <div className="rounded border border-[var(--border-subtle)] bg-[var(--bg-card)] p-3 text-[11px] text-[var(--text-muted)]">
              {t('bc.note')}
            </div>
            <button
              type="submit"
              className="w-full rounded bg-zinc-900 px-4 py-2 text-xs font-medium uppercase tracking-wider text-zinc-100 transition-colors hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200"
            >
              {t('bc.dispatch')}
            </button>
          </form>
        </div>

        <div className="rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 lg:col-span-2">
          <div className="mb-4 flex items-center justify-between">
            <span className="tabular-nums mr-2 flex h-5 w-5 items-center justify-center rounded-full bg-[var(--bg-hover)] text-[10px] font-bold text-[var(--text-muted)]">2</span>
            <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{t('bc.history')}</h3>
            <span className="tabular-nums text-xs text-[var(--text-muted)]">
              {t('bc.total')} {list?.length ?? '—'}
            </span>
          </div>
          {list === null ? (
            <TableSkeleton rows={4} />
          ) : list.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="tabular-nums border-b border-[var(--border-subtle)] text-[10px] uppercase text-[var(--text-muted)]">
                    <th className="pb-2 font-medium">{t('bc.col.title')}</th>
                    <th className="pb-2 font-medium">{t('bc.col.status')}</th>
                    <th className="pb-2 font-medium">{t('bc.col.sent')}</th>
                    <th className="pb-2 font-medium">{t('bc.col.failed')}</th>
                    <th className="pb-2 font-medium">{t('bc.col.created')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[var(--border-subtle)]">
                  {list.map((c) => (
                    <tr key={c.id} className="transition-colors hover:bg-[var(--bg-hover)]">
                      <td className="py-3 pr-2">
                        <div className="font-medium text-[var(--text-primary)]">{c.title}</div>
                        <div className="tabular-nums mt-0.5 text-[10px] uppercase tracking-wider text-[var(--text-muted)]">
                          {t('bc.segmentLbl')} <span className="text-zinc-400">{c.target_segment}</span>
                        </div>
                      </td>
                      <td className="py-3 pr-2">
                        <StatusBadge status={c.status ?? ''} />
                      </td>
                      <td className="tabular-nums py-3 pr-2">
                        {c.sent_count ?? 0} / {c.total_recipients ?? 0}
                      </td>
                      <td className={`tabular-nums py-3 pr-2 ${(c.failed_count ?? 0) > 0 ? 'font-bold text-rose-400' : 'text-[var(--text-muted)]'}`}>
                        {c.failed_count ?? 0}
                      </td>
                      <td className="tabular-nums py-3 text-[11px] text-[var(--text-muted)]">
                        {c.created_at ? new Date(c.created_at).toLocaleString() : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState title={t('bc.empty')} hint="" />
          )}
        </div>
      </div>
    </div>
  );
}
