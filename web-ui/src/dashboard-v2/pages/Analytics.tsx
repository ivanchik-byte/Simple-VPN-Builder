import { useEffect, useState } from 'react';
import { useLang, type Key } from '../lang';
import { Card, EmptyState, TableSkeleton } from '../components';
import { api, formatBytes } from '../api';

interface AuditRow {
  id: number;
  action: string;
  resource_type: string | null;
  diff: string | null;
  ip_address: string | null;
  created_at: string | null;
}

function prettyDiff(raw: string | null): string {
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
}

function ActionBadge({ action, t }: { action: string; t: (k: Key) => string }) {
  if (action === 'PrivilegeEscalationAttempt') {
    return (
      <span className="rounded border border-rose-500/30 bg-rose-500/10 px-2 py-0.5 text-[11px] font-bold text-rose-500">
        [{t('analytics.alertBadge')}] {action}
      </span>
    );
  }
  const color =
    action === 'UpdateAdminPermissions'
      ? 'border-indigo-500/30 bg-indigo-500/10 text-indigo-400'
      : action.startsWith('Delete')
        ? 'border-rose-500/20 bg-rose-500/10 text-rose-500'
        : action.startsWith('Ban')
          ? 'border-amber-500/20 bg-amber-500/10 text-amber-500'
          : action.startsWith('Create')
            ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-500'
            : 'border-zinc-500/20 bg-zinc-500/10 text-zinc-400';
  return <span className={`rounded border px-2 py-0.5 text-[11px] ${color}`}>{action}</span>;
}

export function AuditTable({ limit, showHeader }: { limit: number; showHeader: boolean }) {
  const { t } = useLang();
  const [logs, setLogs] = useState<AuditRow[] | null>(null);
  const [inspect, setInspect] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const res = await fetch(`/admin/audit-data?limit=${limit}`, { credentials: 'include' });
        if (res.status === 401) {
          window.location.href = '/admin/login';
          return;
        }
        const data = (await res.json()) as { logs: AuditRow[] };
        if (live) setLogs(data.logs ?? []);
      } catch {
        if (live) setLogs([]);
      }
    })();
    return () => {
      live = false;
    };
  }, [limit]);

  return (
    <>
      <Card className="overflow-hidden">
        {showHeader && (
          <div className="flex items-center justify-between border-b border-[var(--border-subtle)] px-5 py-4">
            <div>
              <h2 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{t('analytics.trail')}</h2>
              <p className="tabular-nums mt-0.5 text-xs text-[var(--text-muted)]">{t('analytics.trail.sub')}</p>
            </div>
            <a
              href="/admin/audit-v2"
              className="tabular-nums rounded-md border border-[var(--border-subtle)] bg-[var(--bg-canvas)] px-3 py-1.5 text-xs text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]"
            >
              {t('analytics.full')}
            </a>
          </div>
        )}
        {logs === null ? (
          <TableSkeleton rows={5} />
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
                      <ActionBadge action={l.action} t={t} />
                    </td>
                    <td className="px-5 py-3 font-mono text-[11px] uppercase text-[var(--text-secondary)]">
                      {l.resource_type ?? t('analytics.system')}
                    </td>
                    <td className="max-w-md truncate px-5 py-3 font-mono text-[var(--text-primary)]">
                      {l.diff ? (
                        <span className="block max-w-md truncate text-xs text-[var(--text-secondary)]" title={prettyDiff(l.diff)}>
                          {prettyDiff(l.diff).slice(0, 120)}
                        </span>
                      ) : (
                        <span className="text-[var(--text-muted)]">—</span>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-5 py-3 font-mono text-[var(--text-muted)]">
                      {l.ip_address ?? t('analytics.internal')}
                    </td>
                    <td className="whitespace-nowrap px-5 py-3 text-right font-mono">
                      {l.diff ? (
                        <button
                          onClick={() => setInspect(prettyDiff(l.diff))}
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
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/70 p-4">
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

export function AnalyticsPage() {
  const { t } = useLang();
  const [over, setOver] = useState<{ rx: number; tx: number } | null>(null);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const o = await api.overview();
        if (live) setOver({ rx: o.total_rx_bytes, tx: o.total_tx_bytes });
      } catch {
        /* offline shows dashes */
      }
    })();
    return () => {
      live = false;
    };
  }, []);

  const cards = [
    { label: t('analytics.rx'), value: over ? formatBytes(over.rx) : '—', sub: t('analytics.rx.sub') },
    { label: t('analytics.tx'), value: over ? formatBytes(over.tx) : '—', sub: t('analytics.tx.sub') },
    { label: t('analytics.total'), value: over ? formatBytes(over.rx + over.tx) : '—', sub: t('analytics.total.sub') },
  ];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('analytics.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('analytics.subtitle')}</p>
      </div>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {cards.map((c) => (
          <Card key={c.label} className="p-4">
            <div className="text-xs uppercase tracking-wide text-[var(--text-secondary)]">{c.label}</div>
            <div className="tabular-nums mt-1 text-2xl font-semibold text-[var(--text-primary)]">{c.value}</div>
            <div className="tabular-nums mt-1 text-[11px] text-[var(--text-muted)]">{c.sub}</div>
          </Card>
        ))}
      </div>
      <AuditTable limit={20} showHeader />
    </div>
  );
}
