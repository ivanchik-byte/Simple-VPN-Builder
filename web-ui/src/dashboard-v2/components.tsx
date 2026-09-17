import type { ReactNode } from 'react';
import { useState } from 'react';
import { clsx } from 'clsx';
import { ChevronDown, Moon, Sun } from 'lucide-react';
import { useLang } from './lang';

function formatCompact(n: number): string {
  if (!n || n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** i).toFixed(1)} ${units[i]}`;
}

export function Card({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={clsx(
        'rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)]',
        'shadow-[var(--card-shadow)] transition-colors duration-200 hover:border-[var(--border-default)]',
        className,
      )}
    >
      {children}
    </div>
  );
}

export function StatCard({
  label,
  value,
  sub,
  icon,
  loading,
}: {
  label: string;
  value: string;
  sub: string;
  icon: ReactNode;
  loading?: boolean;
}) {
  return (
    <Card className="p-5 transition-transform duration-200 active:scale-[0.98]">
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wider text-[var(--text-secondary)]">
          {label}
        </span>
        <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-[var(--bg-hover)] text-[var(--text-secondary)]">
          {icon}
        </span>
      </div>
      {loading ? (
        <div className="skeleton-shimmer mt-3 h-8 rounded-md" />
      ) : (
        <div className="tabular-nums mt-2 text-[32px] font-bold leading-none tracking-tight text-[var(--text-primary)]">
          {value}
        </div>
      )}
      <div className="tabular-nums mt-2 text-[11px] font-medium text-[var(--text-muted)]">{sub}</div>
    </Card>
  );
}

export function StatusDot({ status }: { status: string }) {
  const { t } = useLang();
  const s = status.toLowerCase();
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        className={clsx(
          'h-1.5 w-1.5 rounded-full',
          s === 'online' && 'animate-pulse bg-emerald-500',
          s === 'degraded' && 'bg-amber-500',
          s !== 'online' && s !== 'degraded' && 'bg-rose-500',
        )}
      />
      <span className="tabular-nums text-[11px] font-medium uppercase tracking-wide text-[var(--text-secondary)]">
        {status || t('dash.unknown')}
      </span>
    </span>
  );
}

export function Sparkline({
  points,
  width = 560,
  height = 120,
}: {
  points: number[];
  width?: number;
  height?: number;
}) {
  if (points.length < 2) {
    return <div className="skeleton-shimmer h-[120px] w-full rounded-lg" />;
  }
  const max = Math.max(...points, 1);
  const step = width / (points.length - 1);
  const d = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${(height - (p / max) * (height - 8) - 4).toFixed(1)}`)
    .join(' ');
  const area = `${d} L${width},${height} L0,${height} Z`;
  const id = 'spark-fill';
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="h-[120px] w-full" preserveAspectRatio="none">
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#34d399" stopOpacity="0.35" />
          <stop offset="100%" stopColor="#34d399" stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#${id})`} />
      <path d={d} fill="none" stroke="#34d399" strokeWidth="1.5" strokeLinejoin="round" />
      <circle cx={width} cy={height - (points[points.length - 1] / max) * (height - 8) - 4} r="2.5" fill="#34d399" />
    </svg>
  );
}

function smoothPath(pts: number[], width: number, height: number, max: number): string {
  const n = pts.length;
  const step = width / (n - 1);
  const px = (i: number) => i * step;
  const py = (i: number) => height - (pts[i] / max) * (height - 14) - 7;
  let d = `M${px(0).toFixed(1)},${py(0).toFixed(1)}`;
  for (let i = 0; i < n - 1; i++) {
    const x0 = px(i);
    const x1 = px(i + 1);
    const mx = (x0 + x1) / 2;
    d += ` C${mx.toFixed(1)},${py(i).toFixed(1)} ${mx.toFixed(1)},${py(i + 1).toFixed(1)} ${x1.toFixed(1)},${py(i + 1).toFixed(1)}`;
  }
  return d;
}

export function DualSparkline({
  rx,
  tx,
  width = 640,
  height = 180,
}: {
  rx: number[];
  tx: number[];
  width?: number;
  height?: number;
}) {
  const { t } = useLang();
  const [hover, setHover] = useState<number | null>(null);
  if (rx.length < 2 || tx.length < 2) {
    return <div className="skeleton-shimmer h-[180px] w-full rounded-lg" />;
  }
  const max = Math.max(...rx, ...tx, 1);
  const top = max * 1.15;
  const nicemax = top >= 1024 * 1024 ? `${(top / 1024 / 1024).toFixed(1)} MB/s` : `${Math.max(1, Math.round(top / 1024))} KB/s`;
  const line = (pts: number[], color: string, gid: string) => {
    const d = smoothPath(pts, width, height, top);
    return (
      <>
        <path d={`${d} L${width},${height} L0,${height} Z`} fill={`url(#${gid})`} />
        <path d={d} fill="none" stroke={color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
      </>
    );
  };
  const n = Math.max(rx.length, tx.length);
  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * width;
    setHover(Math.max(0, Math.min(n - 1, Math.round(x / (width / (n - 1))))));
  };
  const py = (v: number) => height - (v / top) * (height - 14) - 7;
  const px = (i: number) => (i * width) / (n - 1);
  return (
    <div className="relative">
      <div className="tabular-nums pointer-events-none absolute right-1 top-0 text-[10px] text-[var(--text-muted)]">
        {nicemax}
      </div>
      {hover != null && (
        <div className="tabular-nums pointer-events-none absolute left-1 top-0 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-surface-elevated)] px-2 py-1 text-[10px] text-[var(--text-secondary)]">
          {t('dash.in')} {formatCompact(rx[hover] ?? 0)} · {t('dash.out')} {formatCompact(tx[hover] ?? 0)}
        </div>
      )}
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="h-[180px] w-full cursor-crosshair"
        preserveAspectRatio="none"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
      >
        <defs>
          <linearGradient id="dual-rx" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#34d399" stopOpacity="0.45" />
            <stop offset="100%" stopColor="#34d399" stopOpacity="0.02" />
          </linearGradient>
          <linearGradient id="dual-tx" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#22d3ee" stopOpacity="0.4" />
            <stop offset="100%" stopColor="#22d3ee" stopOpacity="0.02" />
          </linearGradient>
        </defs>
        {[0.25, 0.5, 0.75].map((f) => (
          <line key={f} x1="0" x2={width} y1={height * f} y2={height * f} stroke="var(--border-subtle)" strokeWidth="1" strokeDasharray="4 4" />
        ))}
        {line(tx, '#06b6d4', 'dual-tx')}
        {line(rx, '#10b981', 'dual-rx')}
        {hover != null && (
          <>
            <line x1={px(hover)} x2={px(hover)} y1="0" y2={height} stroke="var(--border-strong)" strokeWidth="1" strokeDasharray="3 3" />
            <circle cx={px(hover)} cy={py(rx[hover] ?? 0)} r="3" fill="#10b981" />
            <circle cx={px(hover)} cy={py(tx[hover] ?? 0)} r="3" fill="#06b6d4" />
          </>
        )}
      </svg>
      <div className="tabular-nums pointer-events-none absolute bottom-0 left-1 text-[10px] text-[var(--text-muted)]">
        {t('dash.samplesLive')}
      </div>
    </div>
  );
}

export const PROTO_META: Record<string, { label: string; color: string }> = {
  wireguard: { label: 'WireGuard', color: 'var(--proto-wireguard)' },
  amneziawg: { label: 'AmneziaWG', color: 'var(--proto-amnezia)' },
  vless: { label: 'VLESS', color: 'var(--proto-vless)' },
};

export function ProtoPill({ protocol }: { protocol: string }) {
  const key = protocol.toLowerCase();
  const meta = PROTO_META[key] ?? { label: protocol.toUpperCase(), color: 'var(--text-muted)' };
  const short = key === 'wireguard' ? 'WG' : key === 'amneziawg' ? 'AWG' : meta.label;
  return (
    <span
      className="tabular-nums inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-semibold"
      style={{ color: meta.color, backgroundColor: 'color-mix(in srgb, currentColor 12%, transparent)' }}
    >
      <span className="h-1 w-1 rounded-full" style={{ backgroundColor: 'currentColor' }} />
      {short}
    </span>
  );
}

export function ProtoShare({ shares }: { shares: { key: string; count: number }[] }) {
  const total = shares.reduce((a, s) => a + s.count, 0);
  if (!total) return <div className="skeleton-shimmer h-16 rounded-lg" />;
  return (
    <div>
      <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-[var(--bg-hover)]">
        {shares.map((s) => (
          <div
            key={s.key}
            style={{
              width: `${(s.count / total) * 100}%`,
              backgroundColor: (PROTO_META[s.key] ?? { color: 'var(--text-muted)' }).color,
            }}
          />
        ))}
      </div>
      <div className="mt-3 space-y-2">
        {shares.map((s) => {
          const meta = PROTO_META[s.key] ?? { label: s.key, color: 'var(--text-muted)' };
          return (
            <div key={s.key} className="flex items-center gap-2 text-xs">
              <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: meta.color }} />
              <span className="text-[var(--text-secondary)]">{meta.label}</span>
              <span className="tabular-nums ml-auto text-[var(--text-primary)]">{s.count}</span>
              <span className="tabular-nums w-10 text-right text-[var(--text-muted)]">
                {Math.round((s.count / total) * 100)}%
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function TableSkeleton({ rows = 4 }: { rows?: number }) {  return (
    <div className="divide-y divide-[var(--border-subtle)]">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-center gap-4 px-5 py-3.5">
          <div className="skeleton-shimmer h-4 w-40 rounded" />
          <div className="skeleton-shimmer h-4 w-20 rounded" />
          <div className="skeleton-shimmer ml-auto h-4 w-24 rounded" />
        </div>
      ))}
    </div>
  );
}

export function EmptyState({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="px-5 py-12 text-center">
      <div className="text-sm font-medium text-[var(--text-primary)]">{title}</div>
      <div className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{hint}</div>
    </div>
  );
}

export function MeterCard({
  label,
  value,
  subLeft,
  subRight,
  percent,
  color,
  icon,
  loading,
  expanded,
  onToggle,
}: {
  label: string;
  value: string;
  subLeft: string;
  subRight: string;
  percent: number;
  color: string;
  icon: ReactNode;
  loading?: boolean;
  expanded?: boolean;
  onToggle?: () => void;
}) {
  return (
    <Card
      className={clsx(
        'cursor-pointer select-none p-6 transition-all duration-200 active:scale-[0.99]',
        expanded && 'border-[var(--border-strong)]',
      )}
    >
      <button onClick={onToggle} className="block w-full text-left" aria-expanded={expanded}>
        <div className="flex items-center justify-between gap-2">
          <span className="flex items-center gap-2.5 text-xs font-semibold uppercase tracking-wider text-[var(--text-secondary)]">
            <span className="flex h-6 w-6 items-center justify-center rounded-md bg-black/6 dark:bg-white/10" style={{ color }}>{icon}</span>
            {label}
          </span>
          <span className="flex items-center gap-2">
            {loading ? (
              <div className="skeleton-shimmer h-5 w-14 rounded" />
            ) : (
              <span className="tabular-nums text-xl font-bold tracking-tight text-[var(--text-primary)]">{value}</span>
            )}
            <ChevronDown size={14} className={clsx('text-[var(--text-muted)] transition-transform duration-200', expanded && 'rotate-180')} />
          </span>
        </div>
        <div className="mt-4 h-2 overflow-hidden rounded-full bg-black/8 dark:bg-white/10 shadow-inner">
          <div
            className="h-full rounded-full transition-[width] duration-500"
            style={{ width: `${Math.min(100, Math.max(0, percent))}%`, backgroundColor: color }}
          />
        </div>
        <div className="tabular-nums mt-2.5 flex justify-between text-[11px] text-[var(--text-muted)]">
          <span>{subLeft}</span>
          <span>{subRight}</span>
        </div>
      </button>
    </Card>
  );
}

export function HistoryChart({
  series,
  color,
  format,
  height = 220,
}: {
  series: number[];
  color: string;
  format: (v: number) => string;
  height?: number;
}) {
  const width = 900;
  const { t } = useLang();
  const [hover, setHover] = useState<number | null>(null);
  if (series.length < 2) {
    return <div className="skeleton-shimmer w-full rounded-lg" style={{ height }} />;
  }
  const max = Math.max(...series, 1) * 1.15;
  const d = smoothPath(series, width, height, max);
  const gid = `hist-${color.replace('#', '')}`;
  const last = series[series.length - 1];
  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * width;
    setHover(Math.max(0, Math.min(series.length - 1, Math.round(x / (width / (series.length - 1))))));
  };
  const px = (i: number) => (i * width) / (series.length - 1);
  const py = (v: number) => height - (v / max) * (height - 14) - 7;
  return (
    <div className="relative">
      <div className="tabular-nums pointer-events-none absolute right-1 top-0 text-[11px] text-[var(--text-muted)]">
        {t('dash.chartMax')} {format(Math.max(...series))} · {t('dash.chartNow')} {format(last)}
      </div>
      {hover != null && (
        <div className="tabular-nums pointer-events-none absolute left-1 top-0 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-surface-elevated)] px-2 py-1 text-[11px] text-[var(--text-secondary)]">
          {format(series[hover] ?? 0)}
        </div>
      )}
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="w-full cursor-crosshair"
        style={{ height }}
        preserveAspectRatio="none"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
      >
        <defs>
          <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.45" />
            <stop offset="100%" stopColor={color} stopOpacity="0.02" />
          </linearGradient>
        </defs>
        {[0.25, 0.5, 0.75].map((f) => (
          <line key={f} x1="0" x2={width} y1={height * f} y2={height * f} stroke="var(--border-subtle)" strokeWidth="1" strokeDasharray="4 4" />
        ))}
        <path d={`${d} L${width},${height} L0,${height} Z`} fill={`url(#${gid})`} />
        <path d={d} fill="none" stroke={color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
        {hover != null && (
          <>
            <line x1={px(hover)} x2={px(hover)} y1="0" y2={height} stroke="var(--border-strong)" strokeWidth="1" strokeDasharray="3 3" />
            <circle cx={px(hover)} cy={py(series[hover] ?? 0)} r="3.5" fill={color} />
          </>
        )}
      </svg>
    </div>
  );
}

export function ThemeToggle() {
  const { t } = useLang();
  const [theme, setTheme] = useState(() => {
    const m = document.cookie.match(/(?:^|;\s*)vpn_theme=([^;]+)/);
    const saved = (m && m[1]) || localStorage.getItem('vpn_theme') || 'dark';
    return saved === 'light' ? 'light' : 'dark';
  });

  const apply = (next: string) => {
    setTheme(next);
    document.documentElement.classList.toggle('dark', next === 'dark');
    localStorage.setItem('vpn_theme', next);
    document.cookie = `vpn_theme=${next}; path=/; max-age=31536000; SameSite=Lax`;
  };

  return (
    <button
      onClick={() => apply(theme === 'dark' ? 'light' : 'dark')}
      title={t('dash.themeToggle')}
      className="flex h-7 w-7 items-center justify-center rounded-lg border border-[var(--border-subtle)] text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]"
    >
      {theme === 'dark' ? <Sun size={14} /> : <Moon size={14} />}
    </button>
  );
}
