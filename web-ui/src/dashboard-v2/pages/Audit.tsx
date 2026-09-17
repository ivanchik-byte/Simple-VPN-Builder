import { useState } from 'react';
import { useLang } from '../lang';
import { AuditTable } from './Analytics';

const RESOURCES = ['auth', 'user', 'billing', 'node', 'settings'];

export function AuditPage() {
  const { t } = useLang();
  const [action, setAction] = useState('');
  const [resource, setResource] = useState('');
  const [applied, setApplied] = useState({ action: '', resource: '' });

  const inputCls =
    'rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-1.5 font-mono text-xs text-[var(--text-primary)]';

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('audit.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('audit.subtitle')}</p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <input
          value={action}
          onChange={(e) => setAction(e.target.value)}
          placeholder={t('audit.filterAction')}
          className={inputCls}
        />
        <input
          value={resource}
          onChange={(e) => setResource(e.target.value)}
          placeholder={t('audit.filterResource')}
          className={inputCls}
        />
        <button
          onClick={() => setApplied({ action: action.trim(), resource: resource.trim() })}
          className="tabular-nums rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-1.5 text-xs text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-hover)]"
        >
          {t('audit.apply')}
        </button>
        <div className="flex flex-wrap items-center gap-1.5">
          {RESOURCES.map((r) => (
            <button
              key={r}
              onClick={() => {
                setResource(r);
                setApplied({ action: action.trim(), resource: r });
              }}
              className={`tabular-nums rounded border px-2.5 py-1.5 text-xs transition-colors ${
                applied.resource === r
                  ? 'border-cyan-500/30 bg-cyan-500/10 font-bold text-cyan-400'
                  : 'border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-secondary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              {r}
            </button>
          ))}
          {(applied.action || applied.resource) && (
            <button
              onClick={() => {
                setAction('');
                setResource('');
                setApplied({ action: '', resource: '' });
              }}
              className="tabular-nums rounded border border-[var(--border-subtle)] px-2.5 py-1.5 text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)]"
            >
              ×
            </button>
          )}
        </div>
      </div>

      <AuditTableFiltered action={applied.action} resource={applied.resource} />
    </div>
  );
}

import { useEffect } from 'react';
import { Card, EmptyState, TableSkeleton } from '../components';

function AuditTableFiltered({ action, resource }: { action: string; resource: string }) {
  const { t } = useLang();
  const [logs, setLogs] = useState<
    { id: number; action: string; resource_type: string | null; diff: string | null; ip_address: string | null; created_at: string | null }[] | null
  >(null);
  const [inspect, setInspect] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const q = new URLSearchParams({ limit: '100' });
        if (action) q.set('action', action);
        if (resource) q.set('resource', resource);
        const res = await fetch(`/admin/audit-data?${q.toString()}`, { credentials: 'include' });
        if (res.status === 401) {
          window.location.href = '/admin/login';
          return;
        }
        const data = (await res.json()) as { logs: NonNullable<typeof logs> };
        if (live) setLogs(data.logs ?? []);
      } catch {
        if (live) setLogs([]);
      }
    })();
    return () => {
      live = false;
    };
  }, [action, resource]);

  const pretty = (raw: string | null): string => {
    if (!raw) return '';
    try {
      const bin = atob(raw);
      try {
        return JSON.stringify(JSON.parse(bin), null, 2);
      } catch {
        return bin;
      }
    } catch {
      return raw;
    }
  };

  return (
    <>
      <Card className="overflow-hidden">
        {logs === null ? (
          <TableSkeleton rows={8} />
        ) : logs.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[860px] text-left text-xs">
              <thead>
                <tr className="tabular-nums border-b border-[var(--border-subtle)] bg-[var(--bg-hover)] text-[10px] uppercase text-[var(--text-muted)]">
                  <th className="px-5 py-3 font-medium">{t('analytics.col.time')}</th>
                  <th className="px-5 py-3 font-medium">{t('analytics.col.action')}</th>
                  <th className="px-5 py-3 font-medium">{t('analytics.col.resource')}</th>
                  <th className="px-5 py-3 font-medium">{t('analytics.col.details')}</th>
                  <th className="px-5 py-3 font-medium">{t('analytics.col.ip')}</th>
                  <th className="px-5 py-3 text-right font-medium">{t('analytics.col.inspect')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-subtle)]">
                {logs.map((l) => (
                  <tr key={l.id} className="transition-colors hover:bg-[var(--bg-hover)]/50">
                    <td className="whitespace-nowrap px-5 py-3 font-mono text-[var(--text-muted)]">
                      {l.created_at ? new Date(l.created_at).toLocaleString() : '—'}
                    </td>
                    <td className="whitespace-nowrap px-5 py-3 font-mono font-semibold">
                      <span className="rounded border border-zinc-500/20 bg-zinc-500/10 px-2 py-0.5 text-[11px] text-zinc-400">
                        {l.action}
                      </span>
                    </td>
                    <td className="px-5 py-3 font-mono text-[11px] uppercase text-[var(--text-secondary)]">
                      {l.resource_type ?? t('analytics.system')}
                    </td>
                    <td className="max-w-md truncate px-5 py-3 font-mono text-xs text-[var(--text-secondary)]">
                      {l.diff ? pretty(l.diff).slice(0, 120) : '—'}
                    </td>
                    <td className="whitespace-nowrap px-5 py-3 font-mono text-[var(--text-muted)]">
                      {l.ip_address ?? t('analytics.internal')}
                    </td>
                    <td className="whitespace-nowrap px-5 py-3 text-right font-mono">
                      {l.diff ? (
                        <button
                          onClick={() => setInspect(pretty(l.diff))}
                          className="cursor-pointer rounded bg-[var(--bg-hover)] px-2 py-1 text-[11px] text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
                        >
                          {t('analytics.col.inspect')}
                        </button>
                      ) : (
                        <span className="text-[11px] text-[var(--text-muted)]">—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState title={t('analytics.empty')} hint="" />
        )}
      </Card>
      {inspect !== null && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
          <div className="flex max-h-[85vh] w-full max-w-2xl flex-col rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl">
            <div className="flex shrink-0 items-center justify-between border-b border-[var(--border-subtle)] pb-3">
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold text-[var(--text-primary)]">{t('analytics.modal')}</h3>
                <span className="tabular-nums rounded border border-zinc-500/20 bg-zinc-500/10 px-2 py-0.5 text-[10px] uppercase text-zinc-400">
                  {t('analytics.payload')}
                </span>
              </div>
              <button onClick={() => setInspect(null)} aria-label="Close dialog" className="text-lg leading-none text-[var(--text-muted)] hover:text-[var(--text-primary)]">
                &times;
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto py-4">
              <pre className="select-all whitespace-pre-wrap break-all rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-canvas)] p-4 font-mono text-xs text-[var(--text-primary)]">
                {inspect}
              </pre>
            </div>
            <div className="flex shrink-0 justify-end border-t border-[var(--border-subtle)] pt-3">
              <button
                onClick={() => setInspect(null)}
                className="rounded bg-zinc-900 px-4 py-2 text-xs font-medium text-zinc-100 transition-opacity hover:opacity-90 dark:bg-zinc-100 dark:text-zinc-950"
              >
                {t('analytics.close')}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
