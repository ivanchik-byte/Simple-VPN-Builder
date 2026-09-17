import { useEffect, useState } from 'react';
import { useLang } from '../lang';
import { Card, EmptyState, TableSkeleton } from '../components';
import { formatBytes, fetchRole } from '../api';

interface PlanRow {
  id: string;
  name: string;
  price_str: string;
  price_3m_str: string;
  price_6m_str: string;
  price_12m_str: string;
  price_stars: number | null;
  traffic_bytes: number;
  traffic_gb: number;
  max_devices: number;
  is_trial: boolean | null;
  trial_duration_hours: number | null;
  protocols: string[];
}

async function postForm(action: string, fields: Record<string, string | string[]>): Promise<void> {
  const tokenRes = await fetch('/admin/csrf-token', { credentials: 'include' });
  if (!tokenRes.ok) throw new Error('csrf');
  const { csrf_token } = (await tokenRes.json()) as { csrf_token: string };
  const body = new URLSearchParams();
  body.set('csrf_token', csrf_token);
  for (const [k, v] of Object.entries(fields)) {
    if (Array.isArray(v)) v.forEach((x) => body.append(k, x));
    else body.set(k, v);
  }
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

function hasProto(p: PlanRow, proto: string): boolean {
  return (p.protocols ?? []).includes(proto);
}

function ProtoBadges({ plan }: { plan: PlanRow }) {
  return (
    <div className="mt-2.5 flex flex-wrap gap-1.5">
      {hasProto(plan, 'wireguard') && (
        <span className="tabular-nums rounded border border-blue-500/20 bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-bold text-blue-600 dark:text-blue-400">WireGuard</span>
      )}
      {hasProto(plan, 'amneziawg') && (
        <span className="tabular-nums rounded border border-purple-500/20 bg-purple-500/10 px-1.5 py-0.5 text-[10px] font-bold text-purple-600 dark:text-purple-400">AmneziaWG</span>
      )}
      {hasProto(plan, 'vless') && (
        <span className="tabular-nums rounded border border-cyan-500/20 bg-cyan-500/10 px-1.5 py-0.5 text-[10px] font-bold text-cyan-600 dark:text-cyan-400">VLESS Reality</span>
      )}
    </div>
  );
}

interface Editing {
  id: string;
  name: string;
  max_devices: number;
  traffic_limit_gb: number;
  price_1m: string;
  price_3m: string;
  price_6m: string;
  price_12m: string;
  price_stars: number;
  is_trial: boolean;
  trial_duration_hours: number;
  has_wg: boolean;
  has_awg: boolean;
  has_vless: boolean;
}

const blankEditing: Editing = {
  id: '', name: '', max_devices: 5, traffic_limit_gb: 250,
  price_1m: '9.99', price_3m: '', price_6m: '', price_12m: '',
  price_stars: 250, is_trial: false, trial_duration_hours: 24,
  has_wg: true, has_awg: true, has_vless: true,
};

function PlanForm({
  initial, submitLabel, onSubmit, onClose,
}: {
  initial: Editing;
  submitLabel: string;
  onSubmit: (fields: Record<string, string | string[]>) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useLang();
  const [f, setF] = useState<Editing>(initial);
  const set = <K extends keyof Editing>(k: K, v: Editing[K]) => setF((p) => ({ ...p, [k]: v }));
  const inputCls =
    'w-full rounded border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-1.5 font-mono text-[var(--text-primary)]';

  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const protos: string[] = [];
    if (f.has_wg) protos.push('wireguard');
    if (f.has_awg) protos.push('amneziawg');
    if (f.has_vless) protos.push('vless');
    await onSubmit({
      name: f.name,
      protocols: protos,
      traffic_limit_gb: String(f.traffic_limit_gb),
      max_devices: String(f.max_devices),
      is_trial: f.is_trial ? 'true' : 'false',
      trial_duration_hours: String(f.trial_duration_hours),
      price_1m: f.price_1m,
      price_3m: f.price_3m,
      price_6m: f.price_6m,
      price_12m: f.price_12m,
      price_stars: String(f.price_stars),
    });
  };

  const protoBox = (label: string, checked: boolean, onFlip: () => void) => (
    <label className="flex cursor-pointer items-center gap-2 rounded border border-[var(--border-subtle)] bg-[var(--bg-canvas)] p-2">
      <input type="checkbox" checked={checked} onChange={onFlip} className="rounded border-[var(--border-default)]" />
      <span className="font-medium text-[var(--text-primary)]">{label}</span>
    </label>
  );

  return (
    <form onSubmit={(e) => void submit(e)} className="space-y-4 text-xs">
      <div>
        <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('plans.c.name')}</label>
        <input value={f.name} onChange={(e) => set('name', e.target.value)} required placeholder="Pro Unlimited" className={inputCls} />
      </div>
      <div>
        <label className="mb-1.5 block font-medium text-[var(--text-secondary)]">{t('plans.c.protos')}</label>
        <div className="grid grid-cols-3 gap-2">
          {protoBox('WireGuard', f.has_wg, () => set('has_wg', !f.has_wg))}
          {protoBox('AmneziaWG', f.has_awg, () => set('has_awg', !f.has_awg))}
          {protoBox('VLESS Reality', f.has_vless, () => set('has_vless', !f.has_vless))}
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('plans.c.limit')}</label>
          <input type="number" value={f.traffic_limit_gb} min={0} onChange={(e) => set('traffic_limit_gb', Number(e.target.value))} className={inputCls} />
          <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">{t('plans.c.limitHint')}</p>
        </div>
        <div>
          <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('plans.c.devices')}</label>
          <input type="number" value={f.max_devices} min={1} max={100} onChange={(e) => set('max_devices', Number(e.target.value))} className={inputCls} />
          <p className="mt-0.5 text-[10px] text-[var(--text-muted)]">{t('plans.c.devicesHint')}</p>
        </div>
      </div>
      <div className="space-y-2 rounded border border-[var(--border-subtle)] bg-[var(--bg-canvas)] p-3">
        <label className="flex cursor-pointer items-center gap-2">
          <input type="checkbox" checked={f.is_trial} onChange={(e) => set('is_trial', e.target.checked)} className="rounded border-[var(--border-default)]" />
          <span className="font-medium text-[var(--text-primary)]">{t('plans.c.trial')}</span>
        </label>
        {f.is_trial && (
          <div>
            <label className="mb-1 block font-medium text-[var(--text-muted)]">{t('plans.c.trialHours')}</label>
            <input type="number" value={f.trial_duration_hours} onChange={(e) => set('trial_duration_hours', Number(e.target.value))} className="w-full rounded border border-[var(--border-default)] bg-[var(--bg-surface)] px-3 py-1.5 font-mono text-[var(--text-primary)]" />
            <p className="mt-1 text-[10px] text-[var(--text-muted)]">{t('plans.c.trialHint')}</p>
          </div>
        )}
      </div>
      {!f.is_trial && (
        <div className="space-y-3">
          <div className="space-y-2 rounded border border-[var(--border-subtle)] bg-[var(--bg-canvas)] p-3">
            <div className="mb-1 text-xs font-medium text-[var(--text-primary)]">{t('plans.c.pricing')}</div>
            <div className="grid grid-cols-2 gap-2">
              {([
                [t('plans.c.p1'), 'price_1m', f.price_1m, true],
                [t('plans.c.p3'), 'price_3m', f.price_3m, false],
                [t('plans.c.p6'), 'price_6m', f.price_6m, false],
                [t('plans.c.p12'), 'price_12m', f.price_12m, false],
              ] as const).map(([label, key, val]) => (
                <div key={key}>
                  <label className="mb-0.5 block text-[10px] text-[var(--text-muted)]">{label}</label>
                  <input
                    type="number" step="0.01" value={val}
                    onChange={(e) => set(key, e.target.value)}
                    placeholder={t('plans.c.auto')}
                    className="w-full rounded border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1 font-mono text-xs text-[var(--text-primary)]"
                  />
                </div>
              ))}
            </div>
          </div>
          <div>
            <label className="mb-1 block font-medium text-[var(--text-secondary)]">{t('plans.c.stars')}</label>
            <input type="number" value={f.price_stars} onChange={(e) => set('price_stars', Number(e.target.value))} className={inputCls} />
          </div>
        </div>
      )}
      {f.is_trial && (
        <div className="tabular-nums rounded border border-emerald-500/20 bg-emerald-500/5 p-3 text-xs text-emerald-400">
          {t('plans.c.free')}
        </div>
      )}
      <div className="flex justify-end space-x-2 pt-3">
        <button
          type="button" onClick={onClose}
          className="rounded border border-[var(--border-default)] bg-[var(--bg-hover)] px-3 py-1.5 text-[var(--text-secondary)]"
        >
          {t('plans.c.cancel')}
        </button>
        <button type="submit" className="rounded bg-zinc-900 px-3 py-1.5 font-medium text-zinc-100 dark:bg-zinc-100 dark:text-zinc-950">
          {submitLabel}
        </button>
      </div>
    </form>
  );
}

export function PlansPage() {
  const { t } = useLang();
  const [plans, setPlans] = useState<PlanRow[] | null>(null);
  const [error, setError] = useState('');
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [role, setRole] = useState('');

  const load = async () => {
    try {
      const res = await fetch('/admin/plans-data', { credentials: 'include' });
      if (res.status === 401) {
        window.location.href = '/admin/login';
        return;
      }
      const data = (await res.json()) as { plans: PlanRow[] };
      setPlans(data.plans ?? []);
    } catch {
      setError(t('plans.err.load'));
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const remove = async (p: PlanRow) => {
    if (!window.confirm(`${t('plans.delTitle')}\n${t('plans.deleteConfirm', { name: p.name })}`)) return;
    try {
      await postForm(`/admin/plans/${p.id}/delete`, {});
      await load();
    } catch {
      setError(t('plans.err.delete'));
    }
  };

  const modalShell = (title: string, sub: string, body: React.ReactNode, onClose: () => void) => (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-6 shadow-2xl">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{title}</h3>
            <p className="tabular-nums mt-0.5 text-xs text-[var(--text-muted)]">{sub}</p>
          </div>
          <button onClick={onClose} aria-label="Close dialog" className="text-[var(--text-muted)] hover:text-[var(--text-primary)]">
            &times;
          </button>
        </div>
        {body}
      </div>
    </div>
  );

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{t('plans.title')}</h1>
        <p className="tabular-nums mt-1 text-xs text-[var(--text-muted)]">{t('plans.subtitle')}</p>
      </div>

      {error && (
        <div className="tabular-nums flex items-center justify-between rounded border border-rose-500/30 bg-rose-500/10 p-4 text-xs text-rose-500">
          <span>{error}</span>
          <button onClick={() => setError('')} aria-label="Dismiss error" className="text-sm font-bold text-rose-400 hover:text-rose-200">
            &times;
          </button>
        </div>
      )}

      <div className="flex items-center justify-between">
        <div className="tabular-nums text-xs text-[var(--text-muted)]">
          {t('plans.count')} <span className="font-semibold text-[var(--text-primary)]">{plans?.length ?? '—'}</span>
        </div>
{Boolean(role) && role !== 'admin' && (
        <button
          onClick={() => setCreating(true)}
          className="rounded bg-zinc-900 px-3 py-1.5 text-xs font-medium uppercase tracking-wide text-zinc-100 transition-colors hover:bg-zinc-800 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-200"
        >
          {t('plans.new')}
        </button>
        )}
      </div>

      {plans === null ? (
        <TableSkeleton rows={3} />
      ) : plans.length > 0 ? (
        <div className="grid grid-cols-1 gap-5 md:grid-cols-3">
          {plans.map((p) => (
            <div key={p.id} className="flex flex-col justify-between rounded border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5">
              <div>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">{p.name}</h3>
                    {p.is_trial && (
                      <span className="tabular-nums rounded border border-amber-500/20 bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-bold text-amber-500">{t('plans.trial')}</span>
                    )}
                  </div>
                  <div className="text-right">
                    <span className="tabular-nums text-xs font-bold text-emerald-500">${p.price_str}{t('plans.perMonth')}</span>
                    {p.price_stars != null && (
                      <div className="tabular-nums text-[10px] text-[var(--text-muted)]">{p.price_stars} Stars</div>
                    )}
                  </div>
                </div>
                <ProtoBadges plan={p} />
                <div className="mt-4 space-y-2 border-t border-[var(--border-subtle)] pt-3 font-mono text-xs">
                  <div className="flex justify-between text-[var(--text-secondary)]">
                    <span>{t('plans.bandwidth')}</span>
                    <span className="font-semibold text-[var(--text-primary)]">
                      {p.traffic_bytes === 0 ? t('plans.unlimited') : `${formatBytes(p.traffic_bytes)} (${p.traffic_gb} GB)`}
                    </span>
                  </div>
                  <div className="flex justify-between text-[var(--text-secondary)]">
                    <span>{t('plans.devices')}</span>
                    <span className="font-semibold text-[var(--text-primary)]">{p.max_devices} {t('plans.devicesUnit')}</span>
                  </div>
                  {p.is_trial && (
                    <div className="flex justify-between text-[var(--text-secondary)]">
                      <span>{t('plans.trialDur')}</span>
                      <span className="font-semibold text-amber-500">{p.trial_duration_hours ?? 24} {t('plans.hours')}</span>
                    </div>
                  )}
                  <div className="mt-2 border-t border-[var(--border-subtle)] pt-2">
                    <span className="mb-1 block text-[10px] uppercase tracking-wider text-[var(--text-muted)]">{t('plans.durations')}</span>
                    <div className="grid grid-cols-2 gap-1 text-[11px]">
                      <span className="text-[var(--text-secondary)]">{t('plans.d1')} <strong className="text-[var(--text-primary)]">${p.price_str}</strong></span>
                      <span className="text-[var(--text-secondary)]">{t('plans.d3')} <strong className="text-[var(--text-primary)]">{p.price_3m_str ? `$${p.price_3m_str}` : t('plans.dynamic')}</strong></span>
                      <span className="text-[var(--text-secondary)]">{t('plans.d6')} <strong className="text-[var(--text-primary)]">{p.price_6m_str ? `$${p.price_6m_str}` : t('plans.dynamic')}</strong></span>
                      <span className="text-[var(--text-secondary)]">{t('plans.d12')} <strong className="text-[var(--text-primary)]">{p.price_12m_str ? `$${p.price_12m_str}` : t('plans.dynamic')}</strong></span>
                    </div>
                  </div>
                </div>
              </div>
              <div className="mt-5 flex items-center justify-between border-t border-[var(--border-subtle)] pt-3">
                {Boolean(role) && role !== 'admin' ? (
                <>
                <button
                  onClick={() =>
                    setEditing({
                      id: p.id,
                      name: p.name,
                      max_devices: p.max_devices,
                      traffic_limit_gb: p.traffic_gb,
                      price_1m: p.price_str,
                      price_3m: p.price_3m_str,
                      price_6m: p.price_6m_str,
                      price_12m: p.price_12m_str,
                      price_stars: p.price_stars ?? 0,
                      is_trial: !!p.is_trial,
                      trial_duration_hours: p.trial_duration_hours ?? 24,
                      has_wg: (p.protocols ?? []).includes('wireguard'),
                      has_awg: (p.protocols ?? []).includes('amneziawg'),
                      has_vless: (p.protocols ?? []).includes('vless'),
                    })
                  }
                  className="tabular-nums rounded bg-[var(--bg-hover)] px-2.5 py-1 text-[11px] text-[var(--text-primary)] transition-colors hover:bg-[var(--border-subtle)]"
                >
                  {t('plans.edit')}
                </button>
                <button
                  onClick={() => void remove(p)}
                  className="tabular-nums cursor-pointer rounded bg-rose-500/10 px-2.5 py-1 text-[11px] text-rose-500 transition-colors hover:bg-rose-500/20"
                >
                  {t('plans.remove')}
                </button>
                </>
                ) : (
                  <span className="text-[11px] font-mono text-[var(--text-muted)]">{t('plans.readonly')}</span>
                )}
              </div>
            </div>
          ))}
        </div>
      ) : (
        <Card className="p-8 text-center font-mono text-xs text-[var(--text-muted)]">
          {t('plans.empty')}
        </Card>
      )}

      {creating &&
        modalShell(t('plans.c.title'), t('plans.c.sub'),
          <PlanForm
            initial={{
              id: '', name: '', max_devices: 5, traffic_limit_gb: 250,
              price_1m: '9.99', price_3m: '', price_6m: '', price_12m: '',
              price_stars: 250, is_trial: false, trial_duration_hours: 24,
              has_wg: true, has_awg: true, has_vless: true,
            }}
            submitLabel={t('plans.c.save')}
            onClose={() => setCreating(false)}
            onSubmit={async (fields) => {
              await postForm('/admin/plans', fields);
              setCreating(false);
              await load();
            }}
          />,
          () => setCreating(false),
        )}

      {editing &&
        modalShell(t('plans.e.title'), `${t('plans.e.updating')}${editing.name}`,
          <PlanForm
            initial={editing}
            submitLabel={t('plans.e.update')}
            onClose={() => setEditing(null)}
            onSubmit={async (fields) => {
              await postForm(`/admin/plans/${editing.id}`, fields);
              setEditing(null);
              await load();
            }}
          />,
          () => setEditing(null),
        )}
    </div>
  );
}
