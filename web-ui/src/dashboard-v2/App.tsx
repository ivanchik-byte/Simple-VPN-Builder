import { useCallback, useEffect, useRef, useState } from 'react';
import { Activity, ArrowDownToLine, ArrowUpFromLine, Cpu, HardDrive, Layers, MemoryStick, Network, Server, Users } from 'lucide-react';
import { Sidebar } from './Sidebar';
import { CopilotDrawer } from './Copilot';
import { CmdK } from './CmdK';
import { NodesPage } from './pages/Nodes';
import { UsersPage } from './pages/Users';
import { PlansPage } from './pages/Plans';
import { CredentialsPage } from './pages/Credentials';
import { AnalyticsPage } from './pages/Analytics';
import { AuditPage } from './pages/Audit';
import { BroadcastPage } from './pages/Broadcast';
import { SettingsFullPage as SettingsPage } from './pages/SettingsFull';
import { NodeDetailPage } from './pages/NodeDetail';
import { LoginPage } from './pages/Login';
import { LangProvider, LangToggle, useLang } from './lang';
import { Card, DualSparkline, EmptyState, HistoryChart, MeterCard, ProtoShare, StatCard, StatusDot, TableSkeleton, ThemeToggle } from './components';
import { api, formatBytes, formatRate, numVal, textVal, timeAgo, type ApiNode, type Telemetry } from './api';

function usePoll<T>(fn: () => Promise<T>, ms: number, paused: boolean) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState(false);
  const timer = useRef<number | null>(null);

  const tick = useCallback(async () => {
    try {
      setData(await fn());
      setError(false);
    } catch {
      setError(true);
    }
  }, [fn]);

  useEffect(() => {
    void tick();
    if (paused) return;
    timer.current = window.setInterval(() => void tick(), ms);
    return () => {
      if (timer.current) window.clearInterval(timer.current);
    };
  }, [tick, ms, paused]);

  return { data, error, retry: tick };
}

export default function App() {
  const isLogin = typeof window !== 'undefined' && window.location.pathname.includes('login-v2');
  return (
    <LangProvider>
      {isLogin ? <LoginPage /> : <Dashboard />}
    </LangProvider>
  );
}

function Dashboard() {
  const { lang, t: t2 } = useLang();
  const path = window.location.pathname;
  const nodeId = new URLSearchParams(window.location.search).get('id');
  const page = path.includes('users-v2') ? 'users' : path.includes('nodes-v2') ? 'nodes' : path.includes('plans-v2') ? 'plans' : path.includes('credentials-v2') ? 'credentials' : path.includes('analytics-v2') ? 'analytics' : path.includes('audit-v2') ? 'audit' : path.includes('broadcast-v2') ? 'broadcast' : path.includes('settings-v2') ? 'settings' : path.includes('node-v2') ? 'node' : 'dashboard';
  const [copilot, setCopilot] = useState(false);
  const [copilotQ, setCopilotQ] = useState('');
  const [cmdk, setCmdk] = useState(false);

  useEffect(() => {
    const openCopilot = (e: Event) => {
      const detail = (e as CustomEvent).detail as string | undefined;
      if (typeof detail === 'string' && detail) setCopilotQ(detail + ' #' + Date.now());
      setCopilot(true);
    };
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setCmdk((v) => !v);
      }
    };
    window.addEventListener('open-copilot', openCopilot);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('open-copilot', openCopilot);
      window.removeEventListener('keydown', onKey);
    };
  }, []);
  const [bgPaused, setBgPaused] = useState(false);
  const [expanded, setExpanded] = useState<'cpu' | 'ram' | 'disk' | 'net' | null>(() => {
    const v = localStorage.getItem('vpn_expanded_metric');
    return v === 'cpu' || v === 'ram' || v === 'disk' || v === 'net' ? v : null;
  });

  const toggle = (m: 'cpu' | 'ram' | 'disk' | 'net') => {
    setExpanded((prev) => {
      const next = prev === m ? null : m;
      if (next) localStorage.setItem('vpn_expanded_metric', next);
      else localStorage.removeItem('vpn_expanded_metric');
      return next;
    });
  };

  useEffect(() => {
    const onVis = () => setBgPaused(document.hidden);
    document.addEventListener('visibilitychange', onVis);
    return () => document.removeEventListener('visibilitychange', onVis);
  }, []);

  const [nodes, setNodes] = useState<ApiNode[] | null>(null);
  const [counts, setCounts] = useState<{ online: number; total: number; activeUsers: number; totalUsers: number } | null>(null);
  const [traffic, setTraffic] = useState<number | null>(null);
  const [shares, setShares] = useState<{ key: string; count: number }[]>([]);
  const [top, setTop] = useState<{ name: string; used: number }[]>([]);
  const tele = usePoll(api.telemetry, 5000, bgPaused);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const [all, online, users, over, creds, allUsers] = await Promise.all([
          api.nodes(),
          api.onlineNodes(),
          api.activeUsers(),
          api.overview(),
          api.credentials().catch(() => [] as { protocol: string }[]),
          api.users().catch(() => ({ items: [], pagination: { page: 1, per_page: 100, total_items: 0, total_pages: 0 } })),
        ]);
        if (!live) return;
        setNodes(all.items);
        setCounts({
          online: online.pagination.total_items,
          total: all.pagination.total_items,
          activeUsers: users.pagination.total_items,
          totalUsers: allUsers.pagination.total_items,
        });
        setTraffic(over.total_rx_bytes + over.total_tx_bytes);
        const byProto = new Map<string, number>();
        for (const c of creds) {
          const k = (c.protocol || 'unknown').toLowerCase();
          byProto.set(k, (byProto.get(k) ?? 0) + 1);
        }
        setShares([...byProto.entries()].map(([key, count]) => ({ key, count })));
        setTop(
          (allUsers.items as { username: string; traffic_used: number }[])
            .map((u) => ({ name: u.username, used: numVal(u.traffic_used) }))
            .sort((a, b) => b.used - a.used)
            .slice(0, 5),
        );
      } catch {
        /* error surfaces via telemetry banner */
      }
    })();
    return () => {
      live = false;
    };
  }, []);

  const t: Telemetry | null = tele.data;
  const rx = (t?.history ?? []).map((h) => h.rx_speed);
  const tx = (t?.history ?? []).map((h) => h.tx_speed);
  const loading = nodes === null;
  const allHealthy = counts != null && counts.total > 0 && counts.online === counts.total;

  return (
    <div className="min-h-screen bg-[var(--bg-canvas)] text-[var(--text-primary)]">
      <div
        className="pointer-events-none absolute inset-x-0 top-0 h-72"
        style={{ background: 'radial-gradient(60% 100% at 70% 0%, rgba(16,185,129,0.08), transparent 70%), radial-gradient(40% 80% at 20% 0%, rgba(34,211,238,0.06), transparent 70%)' }}
      />
      <div className="relative flex min-h-screen">
        <Sidebar active={`${page}-v2`} />
        <main className="min-w-0 flex-1 px-4 py-6 md:px-8 md:py-8 2xl:px-12">
          {page === 'users' ? (
            <UsersPage />
          ) : page === 'nodes' ? (
            <NodesPage />
          ) : page === 'plans' ? (
            <PlansPage />
          ) : page === 'credentials' ? (
            <CredentialsPage />
          ) : page === 'analytics' ? (
            <AnalyticsPage />
          ) : page === 'audit' ? (
            <AuditPage />
          ) : page === 'broadcast' ? (
            <BroadcastPage />
          ) : page === 'settings' ? (
            <SettingsPage />
          ) : page === 'node' && nodeId ? (
            <NodeDetailPage id={nodeId} />
          ) : (
          <>
          <header className="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t2('dash.title')}</h1>
              <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">
                {t2('dash.subtitle')}
                {bgPaused && <span> — {t2('dash.paused')}</span>}
              </p>
            </div>
            <div className="tabular-nums flex items-center gap-2 text-[11px] text-[var(--text-muted)]">
              <ThemeToggle />
              <LangToggle />
              <span
                className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 font-semibold ${allHealthy ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400' : 'border-amber-500/30 bg-amber-500/10 text-amber-400'}`}
              >
                <span className={`h-1.5 w-1.5 rounded-full ${allHealthy ? 'animate-pulse bg-emerald-500' : 'bg-amber-500'}`} />
                {counts ? (allHealthy ? t2('dash.active') : `${counts.online}/${counts.total} ${lang === 'ru' ? 'ОНЛАЙН' : 'ONLINE'}`) : t2('dash.cluster')}
              </span>
              <span
                className={`h-1.5 w-1.5 rounded-full ${tele.error ? 'bg-rose-500' : 'animate-pulse bg-emerald-500'}`}
              />
              {tele.error ? t2('dash.unreachable') : t ? `${formatRate(t.rx_speed)} ${t2('dash.in')} / ${formatRate(t.tx_speed)} ${t2('dash.out')}` : t2('dash.connecting')}
            </div>
          </header>

          {tele.error && (
            <button
              onClick={() => void tele.retry()}
              className="mt-4 w-full rounded-xl border border-rose-500/30 bg-rose-500/10 px-4 py-2.5 text-left text-xs text-rose-300 transition-colors hover:bg-rose-500/15"
            >
              {t2('dash.retry')}
            </button>
          )}

          <section className="mt-6 grid grid-cols-2 gap-4 xl:grid-cols-4">
            <MeterCard
              label={t2('dash.cpu')}
              value={t ? `${t.cpu_percent.toFixed(1)}%` : '—'}
              subLeft={t2('dash.cpu.sub')}
              subRight={t?.cpu_model ? t.cpu_model.slice(0, 22) : t2('dash.cpu.host')}
              percent={t?.cpu_percent ?? 0}
              color="#f43f5e"
              icon={<Cpu size={14} />}
              loading={!t && !tele.error}
              expanded={expanded === 'cpu'}
              onToggle={() => toggle('cpu')}
            />
            <MeterCard
              label={t2('dash.mem')}
              value={t ? `${t.ram_percent.toFixed(1)}%` : '—'}
              subLeft={t ? formatBytes(t.ram_used) : '—'}
              subRight={t ? formatBytes(t.ram_total) : '—'}
              percent={t?.ram_percent ?? 0}
              color="#f59e0b"
              icon={<MemoryStick size={14} />}
              loading={!t && !tele.error}
              expanded={expanded === 'ram'}
              onToggle={() => toggle('ram')}
            />
            <MeterCard
              label={t2('dash.disk')}
              value={t ? `${t.disk_percent.toFixed(1)}%` : '—'}
              subLeft={t ? formatBytes(t.disk_used) : '—'}
              subRight={t ? formatBytes(t.disk_total) : '—'}
              percent={t?.disk_percent ?? 0}
              color="#38bdf8"
              icon={<HardDrive size={14} />}
              loading={!t && !tele.error}
              expanded={expanded === 'disk'}
              onToggle={() => toggle('disk')}
            />
            <MeterCard
              label={t2('dash.net')}
              value={t ? formatRate(t.rx_speed + t.tx_speed) : '—'}
              subLeft={t ? `${t2('dash.in')} ${formatRate(t.rx_speed)}` : '—'}
              subRight={t ? `${t2('dash.out')} ${formatRate(t.tx_speed)}` : '—'}
              percent={t && t.history.length > 1 ? Math.min(100, ((t.rx_speed + t.tx_speed) / (Math.max(...t.history.map((h) => h.rx_speed + h.tx_speed), 1))) * 100) : 0}
              color="#34d399"
              icon={<Network size={14} />}
              loading={!t && !tele.error}
              expanded={expanded === 'net'}
              onToggle={() => toggle('net')}
            />
          </section>

          {expanded && t && (
            <section className="mt-4">
              <Card className="p-5">
                <div className="mb-3 flex items-center justify-between">
                  <span className="text-xs font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                    {expanded === 'cpu' && t2('dash.h.cpu')}
                    {expanded === 'ram' && t2('dash.h.mem')}
                    {expanded === 'disk' && t2('dash.h.disk')}
                    {expanded === 'net' && t2('dash.h.net')}
                  </span>
                  <button
                    onClick={() => toggle(expanded)}
                    className="rounded-lg px-2 py-1 text-[11px] text-[var(--text-muted)] transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]"
                  >
                    {t2('dash.collapse')}
                  </button>
                </div>
                {expanded === 'net' ? (
                  <DualSparkline rx={(t.history ?? []).map((h) => h.rx_speed)} tx={(t.history ?? []).map((h) => h.tx_speed)} height={220} />
                ) : (
                  <HistoryChart
                    series={(t.history ?? []).map((h) =>
                      expanded === 'cpu' ? h.cpu_percent : expanded === 'ram' ? h.ram_percent : h.disk_percent,
                    )}
                    color={expanded === 'cpu' ? '#f43f5e' : expanded === 'ram' ? '#f59e0b' : '#38bdf8'}
                    format={(v) => `${v.toFixed(1)}%`}
                    height={220}
                  />
                )}
              </Card>
            </section>
          )}

          <section className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-5">
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-2 lg:col-span-3 lg:grid-cols-2">
              <StatCard
                label={t2('dash.nodes')}
                value={counts ? `${counts.online} / ${counts.total}` : '—'}
                sub={t2('dash.nodes.sub')}
                icon={<Server size={15} />}
                loading={loading}
              />
              <StatCard
                label={t2('dash.users')}
                value={counts ? String(counts.activeUsers) : '—'}
                sub={counts ? `${counts.totalUsers} ${t2('dash.users.registered')}` : t2('dash.users.registered')}
                icon={<Users size={15} />}
                loading={loading}
              />
              <StatCard
                label={t2('dash.traffic')}
                value={traffic != null ? formatBytes(traffic) : '—'}
                sub={t2('dash.traffic.sub')}
                icon={<Activity size={15} />}
                loading={loading}
              />
              <StatCard
                label={t2('dash.protoCount')}
                value="3"
                sub={t2('dash.protoCount.sub')}
                icon={<Layers size={15} />}
                loading={loading}
              />
            </div>
            <Card className="flex flex-col p-5 lg:col-span-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="text-xs font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                  {t2('dash.throughput')}
                </span>
                <span className="tabular-nums flex items-center gap-4 text-xs">
                  <span className="flex items-center gap-1.5 font-semibold text-emerald-400">
                    <ArrowDownToLine size={13} /> {t ? formatRate(t.rx_speed) : '—'}
                  </span>
                  <span className="flex items-center gap-1.5 font-semibold text-cyan-400">
                    <ArrowUpFromLine size={13} /> {t ? formatRate(t.tx_speed) : '—'}
                  </span>
                </span>
              </div>
              <div className="mt-3 flex-1">
                <DualSparkline rx={rx} tx={tx} />
              </div>
              <div className="tabular-nums mt-2 flex gap-4 text-[10px] text-[var(--text-muted)]">
                <span className="flex items-center gap-1.5"><span className="h-1 w-4 rounded-full bg-emerald-500" /> {t2('dash.in')}</span>
                <span className="flex items-center gap-1.5"><span className="h-1 w-4 rounded-full bg-cyan-500" /> {t2('dash.out')}</span>
              </div>
            </Card>
          </section>

          <section className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
            <Card className="p-5">
              <div className="text-xs font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                {t2('dash.proto')}
              </div>
              <p className="tabular-nums mt-0.5 text-[11px] text-[var(--text-muted)]">
                {t2('dash.proto.sub')}
              </p>
              <div className="mt-4">
                {loading ? (
                  <div className="skeleton-shimmer h-16 rounded-lg" />
                ) : shares.length > 0 ? (
                  <ProtoShare shares={shares} />
                ) : (
                  <EmptyState title={t2('dash.proto.empty')} hint={t2('dash.proto.emptyHint')} />
                )}
              </div>
            </Card>
            <Card className="p-5 lg:col-span-2">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-xs font-medium uppercase tracking-wider text-[var(--text-secondary)]">
                    {t2('dash.top')}
                  </div>
                  <p className="tabular-nums mt-0.5 text-[11px] text-[var(--text-muted)]">
                    {t2('dash.top.sub')}
                  </p>
                </div>
                <a href="/admin/users-v2" className="text-xs font-medium text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]">
                  {t2('dash.allUsers')}
                </a>
              </div>
              {loading ? (
                <div className="mt-4 space-y-2.5">
                  {[0, 1, 2].map((i) => (
                    <div key={i} className="skeleton-shimmer h-5 rounded" />
                  ))}
                </div>
              ) : top.length > 0 ? (
                <div className="mt-4 space-y-2.5">
                  {top.map((u, i) => {
                    const max = top[0].used || 1;
                    return (
                      <div key={u.name} className="flex items-center gap-3 text-xs">
                        <span className="tabular-nums w-4 text-[var(--text-muted)]">{i + 1}</span>
                        <a href="/admin/users-v2" className="truncate font-medium hover:underline">
                          {u.name}
                        </a>
                        <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-[var(--bg-hover)]">
                          <div
                            className="h-full rounded-full bg-gradient-to-r from-emerald-500 to-cyan-500"
                            style={{ width: `${u.used > 0 ? Math.max(4, (u.used / max) * 100) : 0}%` }}
                          />
                        </div>
                        <span className="tabular-nums w-20 text-right text-[var(--text-secondary)]">
                          {formatBytes(u.used)}
                        </span>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <EmptyState title={t2('dash.top.empty')} hint={t2('dash.top.emptyHint')} />
              )}
            </Card>
          </section>

          <section className="mt-4">
            <Card className="overflow-hidden">
              <div className="flex items-center justify-between border-b border-[var(--border-subtle)] px-5 py-4">
                <div>
                  <h2 className="text-sm font-semibold tracking-tight">{t2('dash.fleet')}</h2>
                  <p className="tabular-nums mt-0.5 text-[11px] text-[var(--text-muted)]">
                    {t2('dash.fleet.sub')}
                  </p>
                </div>
                <a
                  href="/admin/nodes-v2"
                  className="rounded-lg border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-xs font-medium transition-colors hover:border-[var(--border-strong)]"
                >
                  {t2('dash.manage')}
                </a>
              </div>
              {loading ? (
                <TableSkeleton />
              ) : nodes && nodes.length > 0 ? (
                <div className="overflow-x-auto">
                  <table className="w-full min-w-[640px] text-left text-xs">
                    <thead>
                      <tr className="tabular-nums border-b border-[var(--border-subtle)] bg-[var(--bg-hover)] text-[10px] uppercase text-[var(--text-muted)]">
                        <th className="px-5 py-2.5 font-medium">{t2('dash.col.node')}</th>
                        <th className="px-5 py-2.5 font-medium">{t2('dash.col.status')}</th>
                        <th className="px-5 py-2.5 font-medium">{t2('dash.col.host')}</th>
                        <th className="px-5 py-2.5 font-medium">{t2('dash.col.proto')}</th>
                        <th className="px-5 py-2.5 font-medium">{t2('dash.col.seen')}</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-[var(--border-subtle)]">
                      {nodes.map((n) => (
                        <tr key={n.id} className="transition-colors hover:bg-[var(--bg-hover)]/50">
                          <td className="px-5 py-3">
                            <a href={`/admin/node-v2?id=${n.id}`} className="flex items-center gap-2 hover:underline">
                              <span className="font-semibold">{n.name}</span>
                              <span className="tabular-nums rounded bg-[var(--bg-hover)] px-1.5 py-0.5 text-[10px] text-[var(--text-muted)]">
                                {textVal(n.region) || t2('dash.global')}
                              </span>
                            </a>
                          </td>
                          <td className="px-5 py-3">
                            <StatusDot status={textVal(n.status)} />
                          </td>
                          <td className="tabular-nums px-5 py-3 text-[var(--text-secondary)]">
                            {n.endpoint || '—'}
                          </td>
                          <td className="px-5 py-3 font-mono text-[var(--text-muted)]">
                            WG, AWG, VLESS
                          </td>
                          <td className="tabular-nums px-5 py-3 text-[var(--text-muted)]">
                            {n.last_heartbeat != null ? timeAgo(n.last_heartbeat, lang) : t2('dash.never')}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <EmptyState
                  title={t2('dash.fleet.empty')}
                  hint=""
                />
              )}
            </Card>
          </section>
          </>
          )}
        </main>
        <CopilotDrawer open={copilot} onClose={() => setCopilot(false)} initial={copilotQ} />
        <CmdK open={cmdk} onClose={() => setCmdk(false)} />
      </div>
    </div>
  );
}
