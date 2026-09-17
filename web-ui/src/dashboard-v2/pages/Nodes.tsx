import { useEffect, useState } from 'react';
import { useLang } from '../lang';
import { Card, EmptyState, StatusDot, TableSkeleton } from '../components';
import { api, fetchRole, textVal, timeAgo, type ApiNode } from '../api';

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
  if (!res.ok && res.status !== 303) {
    throw new Error(`Request failed: ${res.status}`);
  }
}

function shortKey(key: string | null): string {
  if (!key) return '—';
  return key.length > 12 ? `${key.slice(0, 12)}...` : key;
}

export function NodesPage() {
  const { lang, t } = useLang();
  const [nodes, setNodes] = useState<ApiNode[] | null>(null);
  const [error, setError] = useState('');
  const [modal, setModal] = useState(false);
  const [form, setForm] = useState({ name: '', region: '', host: '', port: '443', public_key: '' });
  const [role, setRole] = useState('');

  const load = async () => {
    try {
      const res = await api.nodes();
      setNodes(res.items);
    } catch {
      setError(t('nodes.err.load'));
    }
  };

  useEffect(() => {
    void load();
    void fetchRole().then(setRole);
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    try {
      await postForm('/admin/nodes', form);
      setModal(false);
      setForm({ name: '', region: '', host: '', port: '443', public_key: '' });
      await load();
    } catch {
      setError(t('nodes.err.register'));
    }
  };

  const remove = async (n: ApiNode) => {
    const name = n.name;
    if (!window.confirm(`${t('nodes.confirmTitle')}\n${t('nodes.deleteConfirm', { name })}`)) {
      return;
    }
    try {
      await postForm(`/admin/nodes/${n.id}/delete`, {});
      await load();
    } catch {
      setError(t('nodes.err.delete'));
    }
  };

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));

  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)]';

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('nodes.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('nodes.subtitle')}</p>
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
          {t('nodes.total')} <span className="font-semibold text-[var(--text-primary)]">{nodes?.length ?? '—'}</span>
        </div>
        {role !== 'admin' && (
        <button
          onClick={() => setModal(true)}
          className="rounded bg-zinc-900 px-3 py-1.5 text-xs font-medium uppercase tracking-wide text-zinc-100 transition-colors hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200"
        >
          {t('nodes.register')}
        </button>
        )}
      </div>

      <Card className="overflow-hidden">
        {nodes === null ? (
          <TableSkeleton rows={5} />
        ) : nodes.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[840px] text-left text-xs">
              <thead>
                <tr className="tabular-nums border-b border-[var(--border-subtle)] bg-[var(--bg-hover)] text-[10px] uppercase text-[var(--text-muted)]">
                  <th className="px-5 py-2.5 font-medium">{t('nodes.col.name')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nodes.col.status')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nodes.col.endpoint')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nodes.col.pubkey')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nodes.col.grpc')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nodes.col.heartbeat')}</th>
                  <th className="px-5 py-2.5 text-right font-medium">{t('nodes.col.actions')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-subtle)]">
                {nodes.map((n) => (
                  <tr key={n.id} className="transition-colors hover:bg-[var(--bg-hover)]/50">
                    <td className="px-5 py-3 font-medium text-[var(--text-primary)]">
                      <a href={`/admin/node-v2?id=${n.id}`} className="flex items-center gap-2 hover:underline">
                        <span className="font-semibold">{n.name}</span>
                        <span className="tabular-nums rounded bg-[var(--bg-hover)] px-1.5 py-0.5 text-[10px] text-[var(--text-muted)]">
                          {textVal(n.region) || t('dash.global')}
                        </span>
                      </a>
                    </td>
                    <td className="px-5 py-3">
                      <StatusDot status={textVal(n.status)} />
                    </td>
                    <td className="tabular-nums px-5 py-3 text-[var(--text-secondary)]">{n.endpoint || '—'}</td>
                    <td className="tabular-nums max-w-[140px] truncate px-5 py-3 text-[var(--text-muted)]" title={n.public_key ?? ''}>
                      {shortKey(n.public_key)}
                    </td>
                    <td className="tabular-nums px-5 py-3 text-[var(--text-muted)]">{n.grpc_endpoint || '—'}</td>
                    <td className="tabular-nums px-5 py-3 text-[var(--text-muted)]">
                      {n.last_heartbeat != null ? timeAgo(n.last_heartbeat, lang) : t('dash.never')}
                    </td>
                    <td className="space-x-2 px-5 py-3 text-right">
                      <a
                        href={`/admin/node-v2?id=${n.id}`}
                        className="tabular-nums rounded bg-[var(--bg-hover)] px-2 py-1 text-[11px] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
                      >
                        {t('nodes.inspect')}
                      </a>
                      {role !== 'admin' && (
                      <button
                        onClick={() => void remove(n)}
                        className="tabular-nums cursor-pointer rounded bg-rose-500/10 px-2 py-1 text-[11px] text-rose-500 transition-colors hover:bg-rose-500/20"
                      >
                        {t('nodes.delete')}
                      </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState title={t('nodes.empty')} hint="" />
        )}
      </Card>

      {modal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
          <div className="w-full max-w-md rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{t('nodes.modal.title')}</h3>
              <button onClick={() => setModal(false)} className="text-[var(--text-muted)] hover:text-[var(--text-primary)]">
                &times;
              </button>
            </div>
            <form onSubmit={(e) => void submit(e)} className="space-y-3 text-xs">
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('nodes.modal.name')}</label>
                <input required value={form.name} onChange={set('name')} placeholder={t('nodes.modal.namePh')} className={inputCls} />
              </div>
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('nodes.modal.region')}</label>
                <input required value={form.region} onChange={set('region')} placeholder={t('nodes.modal.regionPh')} className={inputCls} />
              </div>
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('nodes.modal.host')}</label>
                <input required value={form.host} onChange={set('host')} placeholder={t('nodes.modal.hostPh')} className={inputCls} />
              </div>
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('nodes.modal.port')}</label>
                <input type="number" value={form.port} onChange={set('port')} className={inputCls} />
              </div>
              <div>
                <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('nodes.modal.pubkey')}</label>
                <input required value={form.public_key} onChange={set('public_key')} placeholder={t('nodes.modal.pubkeyPh')} className={inputCls} />
              </div>
              <div className="flex justify-end space-x-2 pt-3">
                <button
                  type="button"
                  onClick={() => setModal(false)}
                  className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-[var(--text-secondary)]"
                >
                  {t('nodes.modal.cancel')}
                </button>
                <button type="submit" className="rounded bg-zinc-900 px-3 py-1.5 font-medium text-zinc-100 dark:bg-zinc-100 dark:text-zinc-950">
                  {t('nodes.modal.save')}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
