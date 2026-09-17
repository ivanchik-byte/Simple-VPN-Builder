import {
  Bot,
  ChartColumn,
  KeyRound,
  Layers,
  LayoutDashboard,
  LogOut,
  Megaphone,
  ScrollText,
  Server,
  Settings,
  Users,
} from 'lucide-react';
import { clsx } from 'clsx';
import { logout } from './api';
import { useLang, type Key } from './lang';

const items: { icon: typeof Server; key: Key; href: string; active?: boolean }[] = [
  { icon: LayoutDashboard, key: 'nav.dashboard', href: '/admin/dashboard-v2', active: true },
  { icon: Users, key: 'nav.users', href: '/admin/users-v2' },
  { icon: Server, key: 'nav.nodes', href: '/admin/nodes-v2' },
  { icon: Layers, key: 'nav.plans', href: '/admin/plans-v2' },
  { icon: KeyRound, key: 'nav.credentials', href: '/admin/credentials-v2' },
  { icon: ChartColumn, key: 'nav.analytics', href: '/admin/analytics-v2' },
  { icon: Megaphone, key: 'nav.broadcast', href: '/admin/broadcast-v2' },
  { icon: ScrollText, key: 'nav.audit', href: '/admin/audit-v2' },
  { icon: Settings, key: 'nav.settings', href: '/admin/settings-v2' },
];

export function Sidebar({ active }: { active?: string }) {
  const { t } = useLang();
  return (
    <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-[var(--border-subtle)] bg-[var(--bg-surface)] md:flex">
      <div className="flex items-center gap-2.5 px-5 pb-5 pt-6">
        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-emerald-500/15 text-emerald-400">
          <Server size={17} strokeWidth={2} />
        </span>
        <div className="leading-tight">
          <div className="text-[13px] font-semibold tracking-tight text-[var(--text-primary)]">
            {t('app.brand')}
          </div>
          <div className="tabular-nums text-[10px] text-[var(--text-muted)]">{t('app.version')}</div>
        </div>
      </div>
      <nav className="flex-1 space-y-0.5 px-3">
        {items.map((it) => {
          const isActive = active ? it.href.endsWith(active) : it.active;
          return (
            <a
              key={it.key}
              href={it.href}
              className={clsx(
                'relative flex items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium transition-colors',
                isActive
                  ? 'bg-[var(--bg-hover)] text-[var(--text-primary)]'
                  : 'text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text-primary)]',
              )}
            >
              {isActive && (
                <span className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-full bg-emerald-500" />
              )}
              <it.icon size={16} strokeWidth={isActive ? 2.2 : 1.8} />
              {t(it.key)}
              {isActive && <span className="ml-auto h-1.5 w-1.5 rounded-full bg-emerald-500" />}
            </a>
          );
        })}
      </nav>
      <div className="space-y-2 px-3 pb-2">
        <button
          onClick={() => window.dispatchEvent(new CustomEvent('open-copilot'))}
          className="flex w-full items-center gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-hover)] px-3 py-2.5 text-left transition-colors hover:border-[var(--border-default)]"
        >
          <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-violet-500/15 text-violet-400">
            <Bot size={15} strokeWidth={2} />
          </span>
          <span className="leading-tight">
            <span className="block text-xs font-semibold text-[var(--text-primary)]">{t('nav.copilot')}</span>
            <span className="tabular-nums block text-[10px] text-[var(--text-muted)]">{t('nav.copilot.sub')}</span>
          </span>
        </button>
        <a
          href="https://t.me/ivanchik_byte"
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-3 rounded-xl border border-sky-500/25 bg-sky-500/10 px-3 py-2.5 transition-colors hover:border-sky-500/40"
        >
          <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-sky-500/15 text-sky-400">
            <Megaphone size={15} strokeWidth={2} />
          </span>
          <span className="leading-tight">
            <span className="block text-xs font-semibold text-[var(--text-primary)]">@ivanchik_byte</span>
            <span className="block text-[10px] text-[var(--text-muted)]">{t('nav.official')}</span>
          </span>
        </a>
        <button
          onClick={() => void logout()}
          title={t('nav.logout')}
          className="flex w-full items-center gap-3 rounded-xl px-3 py-2 text-[13px] font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--bg-hover)] hover:text-rose-400"
        >
          <LogOut size={16} strokeWidth={1.8} />
          {t('nav.logout')}
        </button>
      </div>
      <div className="px-5 py-4">
        <div className="tabular-nums text-[10px] leading-relaxed text-[var(--text-muted)]">
          {t('app.version')}
        </div>
      </div>
    </aside>
  );
}
