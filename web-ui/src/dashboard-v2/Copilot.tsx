import { useEffect, useRef, useState } from 'react';
import { Bot, X } from 'lucide-react';
import { useLang } from './lang';

interface ChatMsg {
  role: 'user' | 'assistant';
  content: string;
}

interface Proposal {
  action_name: string;
  confirmation_token: string;
  expires_in_seconds: number;
  target_summary: string;
  impact_summary?: string;
  parameters: unknown;
  warning_message: string;
}

interface ChatEvent {
  type: string;
  content?: string;
  proposal?: Proposal;
}

export function CopilotDrawer({ open, onClose, initial }: { open: boolean; onClose: () => void; initial: string }) {
  useEffect(() => {
    if (open && initial) {
      const hash = initial.lastIndexOf('#');
      const q = hash > 0 ? initial.slice(0, hash).trim() : initial;
      setInput('');
      void sendRef.current(q);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initial]);
  const { t } = useLang();
  const [msgs, setMsgs] = useState<ChatMsg[]>([]);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [proposal, setProposal] = useState<Proposal | null>(null);
  const [ttl, setTtl] = useState(0);
  const [notice, setNotice] = useState('');
  const [provider, setProvider] = useState('');
  const bottom = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottom.current?.scrollIntoView({ behavior: 'smooth' });
  }, [msgs, proposal]);

  useEffect(() => {
    if (!proposal) return;
    setTtl(proposal.expires_in_seconds ?? 300);
    const iv = window.setInterval(() => {
      setTtl((v) => {
        if (v <= 1) {
          window.clearInterval(iv);
          setProposal(null);
          return 0;
        }
        return v - 1;
      });
    }, 1000);
    return () => window.clearInterval(iv);
  }, [proposal]);

  useEffect(() => {
    if (!open) return;
    (async () => {
      try {
        const res = await fetch('/api/v1/ai/settings', { credentials: 'include' });
        if (res.ok) {
          const s = (await res.json()) as { base_url?: string; model?: string };
          if (s.base_url || s.model) setProvider([s.model, s.base_url].filter(Boolean).join(' · '));
        }
      } catch {
        /* settings hidden without access */
      }
    })();
  }, [open ]);

  const sendRef = useRef<(text: string) => Promise<void>>(async () => {});
  useEffect(() => {
    sendRef.current = send;
  });
  async function send(text: string) {
    const q = text.trim();
    if (!q || busy) return;
    setInput('');
    setNotice('');
    setProposal(null);
    const history = [...msgs, { role: 'user', content: q } as ChatMsg];
    setMsgs(history);
    const screen = { route: window.location.pathname, selected_id: new URLSearchParams(window.location.search).get('id') ?? '' };
    setBusy(true);
    try {
      const res = await fetch('/api/v1/ai/chat', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ messages: history.map((m) => ({ role: m.role, content: m.content })), screen }),
      });
      if (res.status === 403) {
        setNotice(t('ai.denied'));
        setBusy(false);
        return;
      }
      if (!res.ok || !res.body) throw new Error('chat failed');
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      let draft = '';
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const parts = buf.split('\n\n');
        buf = parts.pop() ?? '';
        for (const part of parts) {
          const line = part.trim();
          if (!line.startsWith('data:')) continue;
          try {
            const ev = JSON.parse(line.slice(5)) as ChatEvent;
            if (ev.type === 'token' && ev.content) {
              draft += ev.content;
              const d = draft;
              setMsgs((prev) => {
                const next = [...prev];
                const last = next[next.length - 1];
                if (last && last.role === 'assistant') next[next.length - 1] = { role: 'assistant', content: d };
                else next.push({ role: 'assistant', content: d });
                return next;
              });
            } else if (ev.type === 'proposal' && ev.proposal) {
              setProposal(ev.proposal);
              setTtl(ev.proposal.expires_in_seconds ?? 300);
            } else if (ev.type === 'error' && ev.content) {
              setNotice(ev.content);
            }
          } catch {
            /* partial chunk */
          }
        }
      }
    } catch {
      setNotice(t('ai.offline'));
    } finally {
      setBusy(false);
    }
  };

  const decide = async (ok: boolean) => {
    if (!proposal) return;
    if (!ok) {
      setProposal(null);
      setNotice(t('ai.cancelled'));
      return;
    }
    try {
      // Send token in request body to avoid URL logging.
      const res = await fetch(`/api/v1/ai/actions/execute`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          action_name: proposal.action_name,
          parameters: proposal.parameters ?? {},
          confirmation_token: proposal.confirmation_token,
        }),
      });
      setNotice(res.ok ? t('ai.executed') : `${t('ai.execFailed')} ${res.status}`);
    } catch {
      setNotice(t('ai.offline'));
    } finally {
      setProposal(null);
    }
  };

  if (!open) return null;

  return (
    <div role="dialog" aria-modal="true" aria-label="Infra Copilot" className="fixed inset-0 z-50">
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div className="absolute bottom-0 right-0 top-0 flex w-full max-w-md flex-col border-l border-[var(--border-subtle)] bg-[var(--bg-surface-elevated)] shadow-2xl">
        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] px-5 py-4">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-violet-500/15 text-violet-400">
              <Bot size={16} />
            </span>
            <div>
              <div className="text-sm font-semibold text-[var(--text-primary)]">{t('ai.title')}</div>
              {provider && (
                <div className="tabular-nums max-w-[280px] truncate text-[10px] text-[var(--text-muted)]">{provider}</div>
              )}
            </div>
          </div>
          <button onClick={onClose} aria-label="Close Copilot" className="text-[var(--text-muted)] hover:text-[var(--text-primary)]">
            <X size={16} />
          </button>
        </div>

        <div className="flex-1 space-y-3 overflow-y-auto px-5 py-4">
          {msgs.length === 0 && (
            <div className="space-y-2">
              <div className="text-[11px] uppercase tracking-wider text-[var(--text-muted)]">{t('ai.providers')}</div>
              {[t('ai.chip1'), t('ai.chip2'), t('ai.chip3')].map((c) => (
                <button
                  key={c}
                  onClick={() => void send(c)}
                  className="block w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-left text-xs text-[var(--text-secondary)] transition-colors hover:border-[var(--border-default)] hover:text-[var(--text-primary)]"
                >
                  {c}
                </button>
              ))}
            </div>
          )}
          {msgs.map((m, i) => (
            <div key={i} className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}>
              <div
                className={`max-w-[85%] whitespace-pre-wrap rounded-xl px-3 py-2 text-xs leading-relaxed ${
                  m.role === 'user'
                    ? 'bg-emerald-500/15 text-[var(--text-primary)]'
                    : 'border border-[var(--border-subtle)] bg-[var(--bg-surface)] text-[var(--text-secondary)]'
                }`}
              >
                {m.content}
              </div>
            </div>
          ))}
          {proposal && (
            <div className="rounded-xl border border-amber-500/30 bg-amber-500/5 p-4">
              <div className="text-xs font-semibold text-[var(--text-primary)]">{proposal.target_summary}</div>
              {proposal.impact_summary && (
                <div className="tabular-nums mt-1 text-[11px] text-sky-300">{proposal.impact_summary}</div>
              )}
              <div className="mt-1 text-[11px] text-amber-400">{proposal.warning_message}</div>
              <div className="tabular-nums mt-1.5 text-[10px] text-[var(--text-muted)]">
                expires in {Math.floor(ttl / 60)}:{String(ttl % 60).padStart(2, '0')}
              </div>
              <div className="tabular-nums mt-1 text-[10px] text-[var(--text-muted)]">
                {proposal.action_name} · {t('ai.expiresIn')} {proposal.expires_in_seconds}s
              </div>
              <div className="mt-3 flex gap-2">
                <button
                  onClick={() => void decide(true)}
                  className="flex-1 rounded-lg bg-emerald-500/20 px-3 py-1.5 text-xs font-semibold text-emerald-300 transition-colors hover:bg-emerald-500/30"
                >
                  {t('ai.confirm')}
                </button>
                <button
                  onClick={() => void decide(false)}
                  className="flex-1 rounded-lg bg-rose-500/10 px-3 py-1.5 text-xs font-semibold text-rose-300 transition-colors hover:bg-rose-500/20"
                >
                  {t('ai.reject')}
                </button>
              </div>
            </div>
          )}
          {notice && (
            <div className="rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-surface)] px-3 py-2 text-[11px] text-[var(--text-secondary)]">
              {notice}
            </div>
          )}
          <div ref={bottom} />
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            void send(input);
          }}
          className="flex gap-2 border-t border-[var(--border-subtle)] p-4"
        >
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={t('ai.askPh')}
            aria-label={t('ai.ask')}
            className="flex-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-canvas)] px-3 py-2 text-xs text-[var(--text-primary)] focus:outline-none"
          />
          <button
            type="submit"
            disabled={busy}
            className="rounded-lg bg-zinc-100 px-4 py-2 text-xs font-semibold text-zinc-950 transition-opacity hover:opacity-90 disabled:opacity-40 dark:bg-zinc-100"
          >
            {t('ai.send')}
          </button>
        </form>
      </div>
    </div>
  );
}
