import { useEffect, useState } from 'react';
import { useLang } from '../lang';
import { Card, EmptyState, StatusDot, TableSkeleton } from '../components';
import { formatBytes, textVal } from '../api';

interface DetailNode {
  id: string;
  name: string;
  endpoint: string | null;
  grpc_endpoint: string | null;
  region: string | null;
  status: string | null;
}

interface DetailTele {
  CPUPercent: number;
  RAMPercent: number;
  RAMUsed: number;
  RAMTotal: number;
  DiskPercent: number;
  DiskUsed: number;
  DiskTotal: number;
  RxSpeed: number;
  TxSpeed: number;
}

interface DetailCred {
  user_id: string;
  protocol: string;
  ipv4: string | null;
  public_key: string | null;
  status: string | null;
}

function Bar({ pct, color }: { pct: number; color: string }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-[var(--bg-hover)]">
      <div className="h-full rounded-full transition-all duration-500" style={{ width: `${Math.min(100, Math.max(0, pct))}%`, backgroundColor: color }} />
    </div>
  );
}

export function NodeDetailPage({ id }: { id: string }) {
  const { t } = useLang();
  const [node, setNode] = useState<DetailNode | null>(null);
  const [tele, setTele] = useState<DetailTele | null>(null);
  const [creds, setCreds] = useState<DetailCred[] | null>(null);

  const load = async () => {
    const res = await fetch(`/admin/node-data?id=${encodeURIComponent(id)}`, { credentials: 'include' });
    if (res.status === 401) {
      window.location.href = '/admin/login';
      return;
    }
    if (!res.ok) return;
    const data = (await res.json()) as { node: DetailNode; telemetry: DetailTele | null; credentials: DetailCred[] };
    setNode(data.node);
    setTele(data.telemetry);
    setCreds(data.credentials ?? []);
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  const setStatus = async (status: string) => {
    const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
    if (!tokenRes.ok) return;
    const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
    await fetch(`/admin/nodes/${id}/status`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ status, csrf_token }).toString(),
    });
    await load();
  };

  if (!node) return <TableSkeleton rows={6} />;
  const online = textVal(node.status) === 'online';

  return (
    <div className="space-y-6">
      <a href="/admin/nodes-v2" className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)]">
        {t('nd.back')}
      </a>

      <div className="flex flex-col justify-between gap-4 rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 md:flex-row md:items-center">
        <div className="flex items-center gap-4">
          <div className="tabular-nums flex h-10 w-10 items-center justify-center rounded border border-[var(--border-subtle)] bg-[var(--bg-hover)] text-xs font-bold text-[var(--text-primary)]">
            {textVal(node.region) || t('dash.global')}
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-base font-semibold tracking-tight text-[var(--text-primary)]">{node.name}</h1>
              <StatusDot status={textVal(node.status)} />
            </div>
            <div className="tabular-nums mt-0.5 text-xs text-[var(--text-muted)]">
              Endpoint: {node.endpoint || '—'} • {t('nd.grpc')} {node.grpc_endpoint || '—'}
            </div>
          </div>
        </div>
        <button
          onClick={() => window.dispatchEvent(new CustomEvent('open-copilot', { detail: `Why is traffic low on node ${node.name}? Diagnose it.` }))}
          className="tabular-nums rounded border border-violet-500/30 bg-violet-500/10 px-3 py-1.5 text-xs text-violet-300 transition-colors hover:bg-violet-500/20"
        >
          Diagnose in Copilot
        </button>
        <button
          onClick={() => void setStatus(online ? 'draining' : 'online')}
          className="tabular-nums rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs transition-colors hover:text-[var(--text-primary)]"
        >
          {online ? t('nd.drain') : t('nd.online')}
        </button>
      </div>

      <div className="rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5">
        <div className="mb-4 text-xs font-semibold uppercase tracking-wider text-[var(--text-secondary)]">{t('nd.telemetry')}</div>
        {tele ? (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <div className="mb-1 flex justify-between font-mono text-xs">
                <span className="text-[var(--text-secondary)]">CPU</span>
                <span className="font-semibold text-[var(--text-primary)]">{tele.CPUPercent.toFixed(1)}%</span>
              </div>
              <Bar pct={tele.CPUPercent} color="#f43f5e" />
            </div>
            <div>
              <div className="mb-1 flex justify-between font-mono text-xs">
                <span className="text-[var(--text-secondary)]">RAM</span>
                <span className="font-semibold text-[var(--text-primary)]">
                  {tele.RAMPercent.toFixed(1)}% ({formatBytes(tele.RAMUsed)})
                </span>
              </div>
              <Bar pct={tele.RAMPercent} color="#f59e0b" />
            </div>
            <div>
              <div className="mb-1 flex justify-between font-mono text-xs">
                <span className="text-[var(--text-secondary)]">Disk</span>
                <span className="font-semibold text-[var(--text-primary)]">
                  {tele.DiskPercent.toFixed(1)}% ({formatBytes(tele.DiskUsed)})
                </span>
              </div>
              <Bar pct={tele.DiskPercent} color="#38bdf8" />
            </div>
            <div>
              <div className="mb-1 flex justify-between font-mono text-xs">
                <span className="text-[var(--text-secondary)]">{t('dash.net')}</span>
                <span className="font-semibold text-[var(--text-primary)]">
                  ↓{formatBytes(tele.RxSpeed)}/s ↑{formatBytes(tele.TxSpeed)}/s
                </span>
              </div>
              <Bar pct={50} color="#34d399" />
            </div>
          </div>
        ) : (
          <EmptyState title={t('nd.offline')} hint={t('nd.offlineHint')} />
        )}
      </div>

      <div className="rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5">
        <h2 className="mb-4 text-sm font-semibold tracking-tight text-[var(--text-primary)]">{t('nd.protos')}</h2>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
          {[
            { name: 'WireGuard', color: 'var(--proto-wireguard)', extra: `Endpoint: ${node.endpoint || '0.0.0.0:51820'} • ${t('nd.proto.wgDesc')}` },
            { name: 'AmneziaWG', color: 'var(--proto-amnezia)', extra: `Endpoint: ${node.endpoint || '0.0.0.0:51821'} • ${t('nd.proto.awgDesc')}` },
            { name: 'VLESS Reality', color: 'var(--proto-vless)', extra: `Endpoint: ${node.endpoint || '0.0.0.0:443'} • ${t('nd.proto.vlessDesc')}` },
          ].map((p) => (
            <div key={p.name} className="rounded border border-[var(--border-subtle)] bg-[var(--bg-canvas)] p-4">
              <div className="flex items-center gap-2 text-xs font-semibold" style={{ color: p.color }}>
                <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: p.color }} />
                {p.name}
              </div>
              <div className="tabular-nums mt-2 text-[11px] text-[var(--text-secondary)]">{p.extra}</div>
            </div>
          ))}
        </div>
      </div>

      <Card className="overflow-hidden">
        <div className="border-b border-[var(--border-subtle)] px-5 py-4">
          <h2 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">
            {t('nd.peers')} ({creds?.length ?? 0})
          </h2>
        </div>
        {creds === null ? (
          <TableSkeleton rows={4} />
        ) : creds.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-left text-xs">
              <thead>
                <tr className="tabular-nums border-b border-[var(--border-subtle)] bg-[var(--bg-hover)] text-[10px] uppercase text-[var(--text-muted)]">
                  <th className="px-5 py-2.5 font-medium">{t('nd.col.user')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nd.col.proto')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nd.col.ipv4')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nd.col.key')}</th>
                  <th className="px-5 py-2.5 font-medium">{t('nd.col.status')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[var(--border-subtle)]">
                {creds.map((c, i) => (
                  <tr key={i} className="transition-colors hover:bg-[var(--bg-hover)]/50">
                    <td className="px-5 py-3 font-mono text-[var(--text-primary)]">
                      <a href="/admin/users-v2" className="hover:underline">
                        {c.user_id.slice(0, 8)}...
                      </a>
                    </td>
                    <td className="px-5 py-3 font-mono uppercase text-[var(--text-secondary)]">{c.protocol}</td>
                    <td className="px-5 py-3 font-mono text-[var(--text-primary)]">{c.ipv4 ?? '—'}</td>
                    <td className="max-w-[140px] truncate px-5 py-3 font-mono text-[var(--text-muted)]">
                      {c.public_key ? `${c.public_key.slice(0, 16)}...` : '—'}
                    </td>
                    <td className="px-5 py-3">
                      <StatusDot status={c.status ?? ''} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState title={t('nd.empty')} hint="" />
        )}
      </Card>
    </div>
  );
}
