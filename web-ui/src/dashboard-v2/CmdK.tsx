import { useEffect, useMemo, useState } from 'react';
import { useLang } from './lang';

interface Hit {
  kind: string;
  label: string;
  sub: string;
  href: string;
  run?: () => void;
}

export function CmdK({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useLang();
  const [q, setQ] = useState('');
  const [nodes, setNodes] = useState<Hit[]>([]);
  const [users, setUsers] = useState<Hit[]>([]);
  const [plans, setPlans] = useState<Hit[]>([]);
  const [cursor, setCursor] = useState(0);

  useEffect(() => {
    if (!open) {
      setQ('');
      setCursor(0);
      return;
    }
    let live = true;
    (async () => {
      try {
        const [n, u, p] = await Promise.all([
          fetch('/api/v1/nodes?per_page=20', { credentials: 'include' }).then((r) => (r.ok ? r.json() : { items: [] })),
          fetch('/api/v1/users?per_page=20', { credentials: 'include' }).then((r) => (r.ok ? r.json() : { items: [] })),
          fetch('/api/v1/plans', { credentials: 'include' }).then((r) => (r.ok ? r.json() : [])),
        ]);
        if (!live) return;
        setNodes(
          ((n.items ?? []) as { id: string; name: string }[]).map((x) => ({
            kind: t('cmdk.nodes'),
            label: x.name,
            sub: x.id.slice(0, 8),
            href: '/admin/nodes-v2',
          })),
        );
        setUsers(
          ((u.items ?? []) as { id: string; username: string }[]).map((x) => ({
            kind: t('cmdk.users'),
            label: x.username,
            sub: x.id.slice(0, 8),
            href: '/admin/users-v2',
          })),
        );
        const arr = (Array.isArray(p) ? p : []) as { id: string; name: string }[];
        setPlans(arr.map((x) => ({ kind: t('cmdk.plans'), label: x.name, sub: x.id.slice(0, 8), href: '/admin/plans-v2' })));
      } catch {
        /* offline */
      }
    })();
    return () => {
      live = false;
    };
  }, [open, t]);

  const commands = useMemo(
    () => [
      { id: 'node', label: t('cmdk.cmd.addNode'), run: () => (window.location.href = '/admin/nodes-v2') },
      { id: 'user', label: t('cmdk.cmd.createUser'), run: () => (window.location.href = '/admin/users-v2') },
      { id: 'copilot', label: t('cmdk.cmd.copilot'), run: () => window.dispatchEvent(new CustomEvent('open-copilot')) },
      {
        id: 'theme',
        label: t('cmdk.cmd.theme'),
        run: () =>
          (
            document.querySelector<HTMLButtonElement>('[title="Toggle theme"]') ||
            document.querySelector<HTMLButtonElement>('[title="Toggle color theme"]') ||
            document.querySelector<HTMLButtonElement>('[title="Сменить тему оформления"]')
          )?.click(),
      },
    ],
    [t],
  );

  const query = q.trim().toLowerCase();
  const all: Hit[] = [
    ...commands
      .filter((c) => !query || c.label.toLowerCase().includes(query))
      .map((c) => ({ kind: t('cmdk.commands'), label: c.label, sub: '', href: '', run: c.run })),
    ...[...nodes, ...users, ...plans].filter(
      (h) => !query || h.label.toLowerCase().includes(query) || h.sub.includes(query),
    ),
  ].slice(0, 12);

  useEffect(() => setCursor(0), [query]);

  if (!open) return null;

  const go = (h: Hit) => {
    onClose();
    if (h.run) h.run();
    else if (h.href) window.location.href = h.href;
  };

  return (
    <div
      className="fixed inset-0 z-50"
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
        if (e.key === 'ArrowDown') {
          e.preventDefault();
          setCursor((c) => Math.min(all.length - 1, c + 1));
        }
        if (e.key === 'ArrowUp') {
          e.preventDefault();
          setCursor((c) => Math.max(0, c - 1));
        }
        if (e.key === 'Enter' && all[cursor]) go(all[cursor]);
      }}
    >
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div className="absolute left-1/2 top-24 w-full max-w-lg -translate-x-1/2 overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface-elevated)] shadow-2xl">
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder={t('cmdk.placeholder')}
          className="w-full border-b border-[var(--border-subtle)] bg-transparent px-5 py-4 text-sm text-[var(--text-primary)] focus:outline-none"
        />
        <div className="max-h-80 overflow-y-auto p-2">
          {all.length === 0 && <div className="px-4 py-6 text-center text-xs text-[var(--text-muted)]">—</div>}
          {all.map((h, i) => (
            <button
              key={`${h.kind}-${h.label}-${i}`}
              onMouseEnter={() => setCursor(i)}
              onClick={() => go(h)}
              className={`flex w-full items-center gap-3 rounded-lg px-4 py-2.5 text-left text-xs transition-colors ${
                i === cursor ? 'bg-[var(--bg-hover)]' : ''
              }`}
            >
              <span className="tabular-nums w-20 shrink-0 uppercase text-[10px] text-[var(--text-muted)]">{h.kind}</span>
              <span className="truncate font-medium text-[var(--text-primary)]">{h.label}</span>
              {h.sub && <span className="tabular-nums ml-auto text-[10px] text-[var(--text-muted)]">{h.sub}</span>}
            </button>
          ))}
        </div>
        <div className="tabular-nums border-t border-[var(--border-subtle)] px-5 py-2 text-[10px] text-[var(--text-muted)]">
          {t('cmdk.go')} ↑↓ + Enter · Esc
        </div>
      </div>
    </div>
  );
}
