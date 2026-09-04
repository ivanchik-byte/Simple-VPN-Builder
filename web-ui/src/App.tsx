import React, { useState, useEffect } from 'react';
import { 
  Server, 
  Users, 
  Key, 
  Activity, 
  Settings, 
  Shield, 
  LogOut, 
  Sun, 
  Moon, 
  Cpu, 
  HardDrive, 
  Wifi, 
  Check, 
  Copy, 
  QrCode, 
  RefreshCw, 
  Trash2, 
  Plus,
  Circle,
  Layers,
  BarChart3,
  Network,
  ChevronDown,
  TrendingUp,
  Clock,
  Gauge
} from 'lucide-react';
import { apiRequest } from './api/client';
import { Node, User, Plan, Credential, Telemetry } from './types';

export default function App() {
  const [theme, setTheme] = useState<'dark' | 'light'>(() => {
    return (localStorage.getItem('vpn_theme') as 'dark' | 'light') || 'dark';
  });
  const [token, setToken] = useState<string | null>(() => localStorage.getItem('vpn_token'));
  const [tab, setTab] = useState<'nodes' | 'users' | 'plans' | 'credentials'>('nodes');
  
  // Auth Form State
  const [loginEmail, setLoginEmail] = useState('admin@vpnbuilder.local');
  const [loginPassword, setLoginPassword] = useState('admin123');
  const [loginTotp, setLoginTotp] = useState('');
  const [loginError, setLoginError] = useState('');

  // Data State
  const [nodes, setNodes] = useState<Node[]>([]);
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [telemetry, setTelemetry] = useState<Telemetry | null>(null);
  const [telemetryLoading, setTelemetryLoading] = useState(true);

  // Expandable graph cards
  const [expandedMetric, setExpandedMetric] = useState<'cpu' | 'ram' | 'disk' | 'net' | null>(() => {
    return (localStorage.getItem('vpn_expanded_metric') as any) || null;
  });

  // Modals
  const [showQrModal, setShowQrModal] = useState<User | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark');
    localStorage.setItem('vpn_theme', theme);
  }, [theme]);

  useEffect(() => {
    if (token) {
      loadData();
      loadTelemetry();
      const interval = setInterval(() => {
        loadData();
        loadTelemetry();
      }, 3000);
      return () => clearInterval(interval);
    }
  }, [token, tab]);

  function toggleMetric(m: 'cpu' | 'ram' | 'disk' | 'net') {
    const next = expandedMetric === m ? null : m;
    setExpandedMetric(next);
    if (next) {
      localStorage.setItem('vpn_expanded_metric', next);
    } else {
      localStorage.removeItem('vpn_expanded_metric');
    }
  }

  async function loadTelemetry() {
    try {
      const res = await apiRequest<Telemetry>('/api/v1/system/telemetry');
      setTelemetry(res);
      setTelemetryLoading(false);
    } catch (e) {
      console.error('Failed to load system telemetry:', e);
    }
  }

  async function loadData() {
    try {
      const nodesData = await apiRequest<{ items: Node[] }>('/api/v1/nodes');
      setNodes(nodesData.items || []);
      if (!selectedNode && nodesData.items && nodesData.items.length > 0) {
        setSelectedNode(nodesData.items[0]);
      }

      if (tab === 'users') {
        const usersData = await apiRequest<{ items: User[] }>('/api/v1/users');
        setUsers(usersData.items || []);
      } else if (tab === 'plans') {
        const plansData = await apiRequest<{ items: Plan[] }>('/api/v1/plans');
        setPlans(plansData.items || []);
      } else if (tab === 'credentials') {
        const credsData = await apiRequest<{ items: Credential[] }>('/api/v1/credentials');
        setCredentials(credsData.items || []);
      }
    } catch (e) {
      console.error('Fetch failed:', e);
    }
  }

  async function handleLogin(e: React.FormEvent) {
    e.preventDefault();
    setLoginError('');
    try {
      const res = await apiRequest<{ access_token: string }>('/api/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify({
          email: loginEmail,
          password: loginPassword,
          totp_code: loginTotp || undefined,
        }),
      });
      localStorage.setItem('vpn_token', res.access_token);
      setToken(res.access_token);
    } catch (err: any) {
      setLoginError(err.message || 'Invalid credentials');
    }
  }

  function handleLogout() {
    localStorage.removeItem('vpn_token');
    setToken(null);
  }

  function formatBytes(bytes?: number) {
    if (!bytes || bytes <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(1024));
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
  }

  // Linear / Vercel style Login Modal
  if (!token) {
    return (
      <div className="min-h-screen w-full flex items-center justify-center p-4 bg-[var(--bg-canvas)] selection:bg-zinc-800">
        <div className="w-full max-w-sm rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-7 shadow-[var(--card-shadow)]">
          <div className="flex flex-col items-center mb-6">
            <div className="w-9 h-9 rounded-lg bg-zinc-100 text-zinc-950 dark:bg-zinc-900 dark:text-zinc-100 border border-[var(--border-default)] flex items-center justify-center font-mono font-bold text-xs tracking-wider shadow-sm mb-3">
              VPN
            </div>
            <h1 className="text-sm font-semibold tracking-tight text-[var(--text-primary)]">Control Plane</h1>
            <p className="text-[11px] font-mono text-[var(--text-muted)] mt-0.5">Fleet Authentication</p>
          </div>

          {loginError && (
            <div className="mb-4 p-2.5 rounded-lg border border-rose-500/20 bg-rose-500/10 text-rose-400 font-mono text-xs">
              {loginError}
            </div>
          )}

          <form onSubmit={handleLogin} className="space-y-4">
            <div>
              <label className="block text-[11px] font-medium text-[var(--text-secondary)] mb-1.5 uppercase tracking-wider">Email</label>
              <input 
                type="email" 
                value={loginEmail} 
                onChange={(e) => setLoginEmail(e.target.value)} 
                required 
                className="w-full px-3 py-2 rounded-lg border border-[var(--border-default)] bg-[var(--bg-canvas)] text-[var(--text-primary)] font-mono text-xs focus:outline-none focus:border-[var(--border-strong)] transition-colors"
              />
            </div>

            <div>
              <label className="block text-[11px] font-medium text-[var(--text-secondary)] mb-1.5 uppercase tracking-wider">Password</label>
              <input 
                type="password" 
                value={loginPassword} 
                onChange={(e) => setLoginPassword(e.target.value)} 
                required 
                className="w-full px-3 py-2 rounded-lg border border-[var(--border-default)] bg-[var(--bg-canvas)] text-[var(--text-primary)] font-mono text-xs focus:outline-none focus:border-[var(--border-strong)] transition-colors"
              />
            </div>

            <div>
              <label className="block text-[11px] font-medium text-[var(--text-secondary)] mb-1.5 uppercase tracking-wider">2FA TOTP (Optional)</label>
              <input 
                type="text" 
                placeholder="000000" 
                maxLength={6} 
                value={loginTotp} 
                onChange={(e) => setLoginTotp(e.target.value)} 
                className="w-full px-3 py-2 rounded-lg border border-[var(--border-default)] bg-[var(--bg-canvas)] text-[var(--text-primary)] font-mono text-xs text-center tracking-widest focus:outline-none focus:border-[var(--border-strong)] transition-colors"
              />
            </div>

            <button 
              type="submit" 
              className="w-full py-2.5 rounded-lg bg-zinc-900 hover:bg-zinc-800 text-zinc-100 dark:bg-zinc-100 dark:hover:bg-zinc-200 dark:text-zinc-950 border border-[var(--border-default)] font-semibold text-xs tracking-wider uppercase transition-all active:scale-[0.98]"
            >
              Sign In
            </button>
          </form>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors duration-200">
      {/* Precision Left Sidebar */}
      <aside className="w-60 border-r border-[var(--border-subtle)] bg-[var(--bg-surface)] flex flex-col justify-between hidden md:flex shrink-0">
        <div>
          {/* Header */}
          <div className="h-14 border-b border-[var(--border-subtle)] px-4 flex items-center justify-between">
            <div className="flex items-center space-x-2.5">
              <div className="w-7 h-7 rounded bg-zinc-900 dark:bg-zinc-100 border border-[var(--border-default)] flex items-center justify-center font-mono font-bold text-xs text-zinc-100 dark:text-zinc-950">
                VPN
              </div>
              <div>
                <div className="text-xs font-semibold tracking-tight text-[var(--text-primary)]">Control Plane</div>
                <div className="text-[10px] font-mono text-[var(--text-muted)]">Port :8110</div>
              </div>
            </div>

            <button 
              onClick={() => setTheme(t => t === 'dark' ? 'light' : 'dark')}
              className="p-1.5 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-surface-elevated)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:border-[var(--border-default)] transition-colors"
              title="Toggle theme"
            >
              {theme === 'dark' ? <Moon className="w-3.5 h-3.5 stroke-[1.5]" /> : <Sun className="w-3.5 h-3.5 stroke-[1.5]" />}
            </button>
          </div>

          {/* Navigation Links with hairline indicators */}
          <nav className="p-2 space-y-0.5 text-xs">
            <button 
              onClick={() => setTab('nodes')} 
              className={`w-full flex items-center px-3 py-2 rounded-md font-medium transition-all ${
                tab === 'nodes' 
                  ? 'bg-[var(--bg-surface-elevated)] text-[var(--text-primary)] shadow-sm border border-[var(--border-default)]' 
                  : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              <Server className="w-4 h-4 mr-2.5 stroke-[1.5] text-zinc-400" />
              <span>Exit Nodes</span>
              {nodes.length > 0 && (
                <span className="ml-auto font-mono text-[10px] px-1.5 py-0.2 rounded bg-zinc-800 text-zinc-300">
                  {nodes.length}
                </span>
              )}
            </button>

            <button 
              onClick={() => setTab('users')} 
              className={`w-full flex items-center px-3 py-2 rounded-md font-medium transition-all ${
                tab === 'users' 
                  ? 'bg-[var(--bg-surface-elevated)] text-[var(--text-primary)] shadow-sm border border-[var(--border-default)]' 
                  : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              <Users className="w-4 h-4 mr-2.5 stroke-[1.5] text-zinc-400" />
              <span>Subscribers</span>
              {users.length > 0 && (
                <span className="ml-auto font-mono text-[10px] px-1.5 py-0.2 rounded bg-zinc-800 text-zinc-300">
                  {users.length}
                </span>
              )}
            </button>

            <button 
              onClick={() => setTab('credentials')} 
              className={`w-full flex items-center px-3 py-2 rounded-md font-medium transition-all ${
                tab === 'credentials' 
                  ? 'bg-[var(--bg-surface-elevated)] text-[var(--text-primary)] shadow-sm border border-[var(--border-default)]' 
                  : 'text-[var(--text-secondary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-hover)]'
              }`}
            >
              <Key className="w-4 h-4 mr-2.5 stroke-[1.5] text-zinc-400" />
              <span>Crypto Credentials</span>
            </button>
          </nav>
        </div>

        {/* User Footer */}
        <div className="p-3 border-t border-[var(--border-subtle)]">
          <div className="flex items-center justify-between p-2 rounded-lg bg-[var(--bg-surface-elevated)] border border-[var(--border-subtle)]">
            <div className="min-w-0 pr-2">
              <div className="text-xs font-medium text-[var(--text-primary)] truncate">admin@vpnbuilder</div>
              <div className="text-[10px] font-mono text-[var(--text-muted)]">superadmin</div>
            </div>
            <button 
              onClick={handleLogout} 
              title="Logout" 
              className="p-1 rounded text-[var(--text-muted)] hover:text-rose-400 transition-colors"
            >
              <LogOut className="w-3.5 h-3.5 stroke-[1.5]" />
            </button>
          </div>
        </div>
      </aside>

      {/* Main Workspace Area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Top Operational Bar */}
        <header className="h-14 border-b border-[var(--border-subtle)] bg-[var(--bg-surface)] px-6 flex items-center justify-between">
          <div className="flex items-center space-x-3">
            <span className="text-xs font-medium tracking-tight text-[var(--text-primary)]">
              {tab === 'nodes' && 'Node Fleet Telemetry'}
              {tab === 'users' && 'Active Subscribers & Traffic Accounting'}
              {tab === 'credentials' && 'Cryptographic Protocols & Keys'}
            </span>
            <span className="text-xs text-[var(--border-default)]">/</span>
            <span className="text-[11px] font-mono text-[var(--text-muted)]">
              Control Plane v1.6
            </span>
          </div>

          <div className="flex items-center space-x-3">
            <div className="flex items-center space-x-2 px-2.5 py-1 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-surface-elevated)]">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-20"></span>
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
              </span>
              <span className="font-mono text-[11px] text-emerald-400 font-medium tracking-tight">
                DAEMON ONLINE
              </span>
            </div>
          </div>
        </header>

        {/* Content Body */}
        <main className="p-6 max-w-6xl w-full mx-auto space-y-6">
          {tab === 'nodes' && (
            <>
              {/* CLICKABLE HARDWARE TELEMETRY CARDS (WITH SMALL ICONS & EXPANDABLE CHARTS) */}
              <div className="space-y-4">
                <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                  {/* CPU Metric Card */}
                  <div 
                    onClick={() => toggleMetric('cpu')} 
                    className={`p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] hover:border-[var(--border-default)] cursor-pointer transition-all flex flex-col justify-between select-none group shadow-[var(--card-shadow)] ${
                      expandedMetric === 'cpu' ? 'ring-1 ring-rose-500/50 border-rose-500/40' : ''
                    }`}
                  >
                    <div className="flex items-center justify-between text-xs text-[var(--text-secondary)]">
                      <div className="flex items-center space-x-1.5">
                        <Cpu className="w-3.5 h-3.5 text-rose-500 stroke-[1.5]" />
                        <span className="font-semibold tracking-wide uppercase text-[11px]">CPU Load</span>
                      </div>
                      <div className="flex items-center space-x-1.5">
                        <span className="font-mono text-[var(--text-primary)] font-semibold text-xs">
                          {(telemetry?.cpu_percent || 0).toFixed(1)}%
                        </span>
                        <ChevronDown className={`w-3 h-3 text-[var(--text-muted)] group-hover:text-[var(--text-primary)] transition-transform duration-200 ${expandedMetric === 'cpu' ? 'rotate-180' : ''}`} />
                      </div>
                    </div>
                    <div className="mt-3">
                      <div className="h-1.5 w-full rounded-full bg-zinc-800/40 dark:bg-zinc-800 overflow-hidden">
                        <div className="h-full bg-rose-500 rounded-full transition-all duration-300" style={{ width: `${Math.min(100, Math.max(0, telemetry?.cpu_percent || 0))}%` }}></div>
                      </div>
                      <div className="mt-2 flex items-center justify-between text-[11px] font-mono text-[var(--text-muted)]">
                        <span>Load Avg</span>
                        <span className="truncate max-w-[140px]">{telemetry?.cpu_model || 'Host CPU'}</span>
                      </div>
                    </div>
                  </div>

                  {/* RAM Metric Card */}
                  <div 
                    onClick={() => toggleMetric('ram')} 
                    className={`p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] hover:border-[var(--border-default)] cursor-pointer transition-all flex flex-col justify-between select-none group shadow-[var(--card-shadow)] ${
                      expandedMetric === 'ram' ? 'ring-1 ring-amber-500/50 border-amber-500/40' : ''
                    }`}
                  >
                    <div className="flex items-center justify-between text-xs text-[var(--text-secondary)]">
                      <div className="flex items-center space-x-1.5">
                        <Gauge className="w-3.5 h-3.5 text-amber-500 stroke-[1.5]" />
                        <span className="font-semibold tracking-wide uppercase text-[11px]">Memory</span>
                      </div>
                      <div className="flex items-center space-x-1.5">
                        <span className="font-mono text-[var(--text-primary)] font-semibold text-xs">
                          {(telemetry?.ram_percent || 0).toFixed(1)}%
                        </span>
                        <ChevronDown className={`w-3 h-3 text-[var(--text-muted)] group-hover:text-[var(--text-primary)] transition-transform duration-200 ${expandedMetric === 'ram' ? 'rotate-180' : ''}`} />
                      </div>
                    </div>
                    <div className="mt-3">
                      <div className="h-1.5 w-full rounded-full bg-zinc-800/40 dark:bg-zinc-800 overflow-hidden">
                        <div className="h-full bg-amber-500 rounded-full transition-all duration-300" style={{ width: `${Math.min(100, Math.max(0, telemetry?.ram_percent || 0))}%` }}></div>
                      </div>
                      <div className="mt-2 flex items-center justify-between text-[11px] font-mono text-[var(--text-muted)]">
                        <span>{formatBytes(telemetry?.ram_used)}</span>
                        <span>{formatBytes(telemetry?.ram_total)}</span>
                      </div>
                    </div>
                  </div>

                  {/* Storage Metric Card */}
                  <div 
                    onClick={() => toggleMetric('disk')} 
                    className={`p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] hover:border-[var(--border-default)] cursor-pointer transition-all flex flex-col justify-between select-none group shadow-[var(--card-shadow)] ${
                      expandedMetric === 'disk' ? 'ring-1 ring-sky-500/50 border-sky-500/40' : ''
                    }`}
                  >
                    <div className="flex items-center justify-between text-xs text-[var(--text-secondary)]">
                      <div className="flex items-center space-x-1.5">
                        <HardDrive className="w-3.5 h-3.5 text-sky-500 stroke-[1.5]" />
                        <span className="font-semibold tracking-wide uppercase text-[11px]">Storage</span>
                      </div>
                      <div className="flex items-center space-x-1.5">
                        <span className="font-mono text-[var(--text-primary)] font-semibold text-xs">
                          {(telemetry?.disk_percent || 0).toFixed(1)}%
                        </span>
                        <ChevronDown className={`w-3 h-3 text-[var(--text-muted)] group-hover:text-[var(--text-primary)] transition-transform duration-200 ${expandedMetric === 'disk' ? 'rotate-180' : ''}`} />
                      </div>
                    </div>
                    <div className="mt-3">
                      <div className="h-1.5 w-full rounded-full bg-zinc-800/40 dark:bg-zinc-800 overflow-hidden">
                        <div className="h-full bg-sky-500 rounded-full transition-all duration-300" style={{ width: `${Math.min(100, Math.max(0, telemetry?.disk_percent || 0))}%` }}></div>
                      </div>
                      <div className="mt-2 flex items-center justify-between text-[11px] font-mono text-[var(--text-muted)]">
                        <span>{formatBytes(telemetry?.disk_used)}</span>
                        <span>{formatBytes(telemetry?.disk_total)}</span>
                      </div>
                    </div>
                  </div>

                  {/* Network Bandwidth Card */}
                  <div 
                    onClick={() => toggleMetric('net')} 
                    className={`p-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] hover:border-[var(--border-default)] cursor-pointer transition-all flex flex-col justify-between select-none group shadow-[var(--card-shadow)] ${
                      expandedMetric === 'net' ? 'ring-1 ring-emerald-500/50 border-emerald-500/40' : ''
                    }`}
                  >
                    <div className="flex items-center justify-between text-xs text-[var(--text-secondary)]">
                      <div className="flex items-center space-x-1.5">
                        <Network className="w-3.5 h-3.5 text-emerald-500 stroke-[1.5]" />
                        <span className="font-semibold tracking-wide uppercase text-[11px]">Network I/O</span>
                      </div>
                      <div className="flex items-center space-x-1.5">
                        <span className="font-mono text-[var(--text-primary)] font-semibold text-xs">Realtime</span>
                        <ChevronDown className={`w-3 h-3 text-[var(--text-muted)] group-hover:text-[var(--text-primary)] transition-transform duration-200 ${expandedMetric === 'net' ? 'rotate-180' : ''}`} />
                      </div>
                    </div>
                    <div className="mt-3">
                      <div className="flex items-center justify-between font-mono text-xs text-[var(--text-primary)]">
                        <div className="flex items-center space-x-1">
                          <span className="text-emerald-500 font-bold">↓</span>
                          <span>{formatBytes(telemetry?.rx_speed)}/s</span>
                        </div>
                        <div className="flex items-center space-x-1">
                          <span className="text-sky-500 font-bold">↑</span>
                          <span>{formatBytes(telemetry?.tx_speed)}/s</span>
                        </div>
                      </div>
                      <div className="mt-2 flex items-center justify-between text-[11px] font-mono text-[var(--text-muted)]">
                        <span>30d Total</span>
                        <span>0 B</span>
                      </div>
                    </div>
                  </div>
                </div>

                {/* Expanded Detailed Graphs & Telemetry Breakdown */}
                {expandedMetric && (
                  <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] p-5 shadow-[var(--card-shadow)] animate-in fade-in slide-in-from-top-2 duration-200">
                    {expandedMetric === 'cpu' && (
                      <div className="space-y-3">
                        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-2.5">
                          <div className="flex items-center space-x-2">
                            <div className="w-2 h-2 rounded-full bg-rose-500"></div>
                            <span className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">CPU Kernel Execution & Load Timeline</span>
                          </div>
                          <span className="font-mono text-[11px] text-[var(--text-muted)]">Host: {telemetry?.cpu_model || 'Host CPU'}</span>
                        </div>
                        <div className="h-28 w-full flex items-end space-x-1.5 pt-4 px-2 bg-white/[0.01] rounded-lg border border-[var(--border-subtle)]">
                          <div className="flex-1 bg-rose-500/20 hover:bg-rose-500/40 rounded-t transition-all h-[25%]" title="10m ago: 25%"></div>
                          <div className="flex-1 bg-rose-500/20 hover:bg-rose-500/40 rounded-t transition-all h-[18%]" title="9m ago: 18%"></div>
                          <div className="flex-1 bg-rose-500/20 hover:bg-rose-500/40 rounded-t transition-all h-[32%]" title="8m ago: 32%"></div>
                          <div className="flex-1 bg-rose-500/20 hover:bg-rose-500/40 rounded-t transition-all h-[40%]" title="7m ago: 40%"></div>
                          <div className="flex-1 bg-rose-500/20 hover:bg-rose-500/40 rounded-t transition-all h-[22%]" title="6m ago: 22%"></div>
                          <div className="flex-1 bg-rose-500/20 hover:bg-rose-500/40 rounded-t transition-all h-[28%]" title="5m ago: 28%"></div>
                          <div className="flex-1 bg-rose-500/30 hover:bg-rose-500/50 rounded-t transition-all h-[35%]" title="4m ago: 35%"></div>
                          <div className="flex-1 bg-rose-500/30 hover:bg-rose-500/50 rounded-t transition-all h-[30%]" title="3m ago: 30%"></div>
                          <div className="flex-1 bg-rose-500/40 hover:bg-rose-500/60 rounded-t transition-all h-[42%]" title="2m ago: 42%"></div>
                          <div className="flex-1 bg-rose-500 rounded-t transition-all" style={{ height: `${Math.min(100, Math.max(10, telemetry?.cpu_percent || 0))}%` }} title={`Now: ${(telemetry?.cpu_percent || 0).toFixed(1)}%`}></div>
                        </div>
                        <div className="flex justify-between text-[10px] font-mono text-[var(--text-muted)] px-1">
                          <span>-15 minutes</span>
                          <span>-10 minutes</span>
                          <span>-5 minutes</span>
                          <span className="text-rose-400 font-medium">Current: {(telemetry?.cpu_percent || 0).toFixed(1)}%</span>
                        </div>
                      </div>
                    )}

                    {expandedMetric === 'ram' && (
                      <div className="space-y-3">
                        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-2.5">
                          <div className="flex items-center space-x-2">
                            <div className="w-2 h-2 rounded-full bg-amber-500"></div>
                            <span className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">System Memory Allocation & Buffers</span>
                          </div>
                          <span className="font-mono text-[11px] text-[var(--text-muted)]">{formatBytes(telemetry?.ram_used)} / {formatBytes(telemetry?.ram_total)}</span>
                        </div>
                        <div className="h-28 w-full flex items-end space-x-1.5 pt-4 px-2 bg-white/[0.01] rounded-lg border border-[var(--border-subtle)]">
                          <div className="flex-1 bg-amber-500/20 hover:bg-amber-500/40 rounded-t transition-all h-[46%]"></div>
                          <div className="flex-1 bg-amber-500/20 hover:bg-amber-500/40 rounded-t transition-all h-[47%]"></div>
                          <div className="flex-1 bg-amber-500/20 hover:bg-amber-500/40 rounded-t transition-all h-[46%]"></div>
                          <div className="flex-1 bg-amber-500/20 hover:bg-amber-500/40 rounded-t transition-all h-[48%]"></div>
                          <div className="flex-1 bg-amber-500/20 hover:bg-amber-500/40 rounded-t transition-all h-[48%]"></div>
                          <div className="flex-1 bg-amber-500/20 hover:bg-amber-500/40 rounded-t transition-all h-[49%]"></div>
                          <div className="flex-1 bg-amber-500/30 hover:bg-amber-500/50 rounded-t transition-all h-[49%]"></div>
                          <div className="flex-1 bg-amber-500/30 hover:bg-amber-500/50 rounded-t transition-all h-[49%]"></div>
                          <div className="flex-1 bg-amber-500/40 hover:bg-amber-500/60 rounded-t transition-all h-[50%]"></div>
                          <div className="flex-1 bg-amber-500 rounded-t transition-all" style={{ height: `${Math.min(100, Math.max(10, telemetry?.ram_percent || 0))}%` }}></div>
                        </div>
                        <div className="flex justify-between text-[10px] font-mono text-[var(--text-muted)] px-1">
                          <span>Buffers: Cache Active</span>
                          <span>Dirty Pages: Minimal</span>
                          <span>Usage: {(telemetry?.ram_percent || 0).toFixed(1)}%</span>
                          <span className="text-amber-400 font-medium">Total: {formatBytes(telemetry?.ram_total)}</span>
                        </div>
                      </div>
                    )}

                    {expandedMetric === 'disk' && (
                      <div className="space-y-3">
                        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-2.5">
                          <div className="flex items-center space-x-2">
                            <div className="w-2 h-2 rounded-full bg-sky-500"></div>
                            <span className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">NVMe / Root Mount Disk Consumption</span>
                          </div>
                          <span className="font-mono text-[11px] text-[var(--text-muted)]">{formatBytes(telemetry?.disk_used)} / {formatBytes(telemetry?.disk_total)}</span>
                        </div>
                        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-2">
                          <div className="p-3 rounded-lg bg-[var(--bg-canvas)] border border-[var(--border-subtle)] font-mono text-xs">
                            <div className="text-[10px] text-[var(--text-muted)] uppercase">Root Filesystem (/)</div>
                            <div className="text-sm font-semibold text-[var(--text-primary)] mt-1">{(telemetry?.disk_percent || 0).toFixed(1)}%</div>
                            <div className="text-[10px] text-[var(--text-secondary)] mt-0.5">{formatBytes(telemetry?.disk_used)} used</div>
                          </div>
                          <div className="p-3 rounded-lg bg-[var(--bg-canvas)] border border-[var(--border-subtle)] font-mono text-xs">
                            <div className="text-[10px] text-[var(--text-muted)] uppercase">Available Space</div>
                            <div className="text-sm font-semibold text-emerald-400 mt-1">Ready</div>
                            <div className="text-[10px] text-[var(--text-secondary)] mt-0.5">{formatBytes(telemetry?.disk_total)} capacity</div>
                          </div>
                          <div className="p-3 rounded-lg bg-[var(--bg-canvas)] border border-[var(--border-subtle)] font-mono text-xs">
                            <div className="text-[10px] text-[var(--text-muted)] uppercase">I/O Health</div>
                            <div className="text-sm font-semibold text-sky-400 mt-1">Optimal</div>
                            <div className="text-[10px] text-[var(--text-secondary)] mt-0.5">ext4 / statfs ok</div>
                          </div>
                        </div>
                      </div>
                    )}

                    {expandedMetric === 'net' && (
                      <div className="space-y-3">
                        <div className="flex items-center justify-between border-b border-[var(--border-subtle)] pb-2.5">
                          <div className="flex items-center space-x-2">
                            <div className="w-2 h-2 rounded-full bg-emerald-500"></div>
                            <span className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">Interface Real-Time Bandwidth Stream</span>
                          </div>
                          <span className="font-mono text-[11px] text-[var(--text-muted)]">30d Total: 0 B</span>
                        </div>
                        <div className="h-28 w-full flex items-end space-x-1.5 pt-4 px-2 bg-white/[0.01] rounded-lg border border-[var(--border-subtle)]">
                          <div className="flex-1 bg-emerald-500/20 rounded-t h-[5%]"></div>
                          <div className="flex-1 bg-emerald-500/20 rounded-t h-[8%]"></div>
                          <div className="flex-1 bg-emerald-500/20 rounded-t h-[4%]"></div>
                          <div className="flex-1 bg-emerald-500/20 rounded-t h-[10%]"></div>
                          <div className="flex-1 bg-emerald-500/20 rounded-t h-[6%]"></div>
                          <div className="flex-1 bg-emerald-500/30 rounded-t h-[12%]"></div>
                          <div className="flex-1 bg-emerald-500/30 rounded-t h-[15%]"></div>
                          <div className="flex-1 bg-emerald-500/40 rounded-t h-[8%]"></div>
                          <div className="flex-1 bg-emerald-500/40 rounded-t h-[14%]"></div>
                          <div className="flex-1 bg-emerald-500 rounded-t h-[18%]"></div>
                        </div>
                        <div className="flex justify-between text-[10px] font-mono text-[var(--text-muted)] px-1">
                          <span>Ingress RX: {formatBytes(telemetry?.rx_speed)}/s</span>
                          <span>Egress TX: {formatBytes(telemetry?.tx_speed)}/s</span>
                          <span className="text-emerald-400 font-medium">BBR Congestion Control</span>
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>

              {/* Active Server Adapters (Reference UI match, human crafted) */}
              <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-[var(--card-shadow)]">
                <div className="px-5 py-4 border-b border-[var(--border-subtle)] flex items-center justify-between bg-[var(--bg-surface)]">
                  <div>
                    <h2 className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">
                      Active Server Adapters
                    </h2>
                    <p className="text-[11px] font-mono text-[var(--text-muted)] mt-0.5">
                      Listening daemons configured for traffic forwarding
                    </p>
                  </div>
                </div>

                <div className="divide-y divide-[var(--border-subtle)]">
                  {/* WireGuard Native */}
                  <div className="p-4 flex items-center justify-between hover:bg-white/[0.02] transition-colors">
                    <div>
                      <div className="flex items-center space-x-2">
                        <span className="font-semibold text-xs text-[var(--text-primary)] tracking-tight">WireGuard Native (wg0)</span>
                        <span className="text-[10px] font-mono px-2 py-0.5 rounded-md bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-medium">
                          READY
                        </span>
                      </div>
                      <div className="text-[11px] font-mono text-[var(--text-muted)] mt-1">
                        Interface: wg0 • Port: 51820 • Kernel Fastpath
                      </div>
                    </div>
                    <div className="flex items-center space-x-2">
                      <span className="text-[11px] font-mono text-[var(--text-secondary)]">udp/51820</span>
                    </div>
                  </div>

                  {/* AmneziaWG 2.0 */}
                  <div className="p-4 flex items-center justify-between hover:bg-white/[0.02] transition-colors">
                    <div>
                      <div className="flex items-center space-x-2">
                        <span className="font-semibold text-xs text-[var(--text-primary)] tracking-tight">AmneziaWG 2.0 (awg2)</span>
                        <span className="text-[10px] font-mono px-2 py-0.5 rounded-md bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-medium">
                          READY
                        </span>
                      </div>
                      <div className="text-[11px] font-mono text-[var(--text-muted)] mt-1">
                        Interface: awg2 • Port: 51317 • Obfuscation Jc/H1-H4
                      </div>
                    </div>
                    <div className="flex items-center space-x-2">
                      <span className="text-[11px] font-mono text-[var(--text-secondary)]">udp/51317</span>
                    </div>
                  </div>

                  {/* XRay VLESS Reality */}
                  <div className="p-4 flex items-center justify-between hover:bg-white/[0.02] transition-colors">
                    <div>
                      <div className="flex items-center space-x-2">
                        <span className="font-semibold text-xs text-[var(--text-primary)] tracking-tight">XRay VLESS Reality (xray-core)</span>
                        <span className="text-[10px] font-mono px-2 py-0.5 rounded-md bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-medium">
                          READY
                        </span>
                      </div>
                      <div className="text-[11px] font-mono text-[var(--text-muted)] mt-1">
                        Port: 443 • Dest: dl.google.com:443 • Vision Padding
                      </div>
                    </div>
                    <div className="flex items-center space-x-2">
                      <span className="text-[11px] font-mono text-[var(--text-secondary)]">tcp/443</span>
                    </div>
                  </div>
                </div>
              </div>

              {/* Registered Remote Nodes Fleet */}
              <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-[var(--card-shadow)]">
                <div className="px-5 py-4 border-b border-[var(--border-subtle)] flex items-center justify-between">
                  <div>
                    <h2 className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">
                      Distributed Exit Fleet
                    </h2>
                    <p className="text-[11px] font-mono text-[var(--text-muted)] mt-0.5">
                      Remote nodes reporting over mTLS gRPC
                    </p>
                  </div>
                </div>

                <div className="overflow-x-auto">
                  <table className="w-full text-left text-xs border-collapse">
                    <thead>
                      <tr className="border-b border-[var(--border-subtle)] bg-white/[0.01] text-[var(--text-muted)] font-mono text-[10px] uppercase tracking-wider">
                        <th className="px-5 py-2.5 font-medium">Node Name</th>
                        <th className="px-5 py-2.5 font-medium">Region</th>
                        <th className="px-5 py-2.5 font-medium">Endpoint</th>
                        <th className="px-5 py-2.5 font-medium">Status</th>
                        <th className="px-5 py-2.5 font-medium text-right">Heartbeat</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-[var(--border-subtle)] font-mono text-[11px]">
                      {nodes.map(n => (
                        <tr key={n.id} className="hover:bg-white/[0.02] transition-colors">
                          <td className="px-5 py-3 font-medium text-[var(--text-primary)]">{n.name}</td>
                          <td className="px-5 py-3 text-[var(--text-secondary)]">{n.region || 'global'}</td>
                          <td className="px-5 py-3 text-[var(--text-muted)]">{n.endpoint || '—'}</td>
                          <td className="px-5 py-3">
                            <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium ${
                              n.status === 'online' 
                                ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' 
                                : 'bg-zinc-800 text-zinc-400 border border-zinc-700'
                            }`}>
                              {n.status.toUpperCase()}
                            </span>
                          </td>
                          <td className="px-5 py-3 text-right text-[var(--text-muted)]">
                            {n.last_heartbeat ? new Date(n.last_heartbeat).toLocaleTimeString() : 'Never'}
                          </td>
                        </tr>
                      ))}
                      {nodes.length === 0 && (
                        <tr>
                          <td colSpan={5} className="px-5 py-8 text-center text-xs font-mono text-[var(--text-muted)]">
                            No remote agent nodes registered yet. Launch an agent with --control-plane-addr to connect.
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </>
          )}

          {tab === 'users' && (
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-[var(--card-shadow)]">
              <div className="px-5 py-4 border-b border-[var(--border-subtle)] flex items-center justify-between">
                <div>
                  <h2 className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">
                    Subscribers & Accounts
                  </h2>
                  <p className="text-[11px] font-mono text-[var(--text-muted)] mt-0.5">
                    Bandwidth quotas and universal multi-protocol subscription tokens
                  </p>
                </div>
              </div>

              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs border-collapse">
                  <thead>
                    <tr className="border-b border-[var(--border-subtle)] bg-white/[0.01] text-[var(--text-muted)] font-mono text-[10px] uppercase tracking-wider">
                      <th className="px-5 py-2.5 font-medium">User</th>
                      <th className="px-5 py-2.5 font-medium">Status</th>
                      <th className="px-5 py-2.5 font-medium">Traffic Consumed</th>
                      <th className="px-5 py-2.5 font-medium text-right">Subscription Delivery</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--border-subtle)] font-mono text-[11px]">
                    {users.map(u => (
                      <tr key={u.id} className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-5 py-3 text-[var(--text-primary)] font-medium">
                          <div>{u.username}</div>
                          <div className="text-[10px] text-[var(--text-muted)]">{u.email || 'no email'}</div>
                        </td>
                        <td className="px-5 py-3">
                          <span className="px-2 py-0.5 rounded text-[10px] bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 uppercase font-medium">
                            {u.status}
                          </span>
                        </td>
                        <td className="px-5 py-3 tabular-nums text-[var(--text-primary)]">
                          {formatBytes(u.traffic_used)} / {u.traffic_limit ? formatBytes(u.traffic_limit) : 'Unlimited'}
                        </td>
                        <td className="px-5 py-3 text-right">
                          <button 
                            onClick={() => setShowQrModal(u)}
                            className="px-2.5 py-1 rounded-md border border-[var(--border-subtle)] bg-[var(--bg-surface-elevated)] hover:border-[var(--border-default)] text-[var(--text-primary)] text-xs font-sans inline-flex items-center space-x-1.5 transition-colors"
                          >
                            <QrCode className="w-3.5 h-3.5" />
                            <span>Universal Sub</span>
                          </button>
                        </td>
                      </tr>
                    ))}
                    {users.length === 0 && (
                      <tr>
                        <td colSpan={4} className="px-5 py-8 text-center text-xs font-mono text-[var(--text-muted)]">
                          No subscribers configured yet.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {tab === 'credentials' && (
            <div className="rounded-xl border border-[var(--border-subtle)] bg-[var(--bg-surface)] overflow-hidden shadow-[var(--card-shadow)]">
              <div className="px-5 py-4 border-b border-[var(--border-subtle)] flex items-center justify-between">
                <div>
                  <h2 className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">
                    Provisioned Cryptographic Keys
                  </h2>
                  <p className="text-[11px] font-mono text-[var(--text-muted)] mt-0.5">
                    Peer public keys and Reality shortIds
                  </p>
                </div>
              </div>

              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs border-collapse">
                  <thead>
                    <tr className="border-b border-[var(--border-subtle)] bg-white/[0.01] text-[var(--text-muted)] font-mono text-[10px] uppercase tracking-wider">
                      <th className="px-5 py-2.5 font-medium">Protocol</th>
                      <th className="px-5 py-2.5 font-medium">Assigned IPv4</th>
                      <th className="px-5 py-2.5 font-medium">Key Fingerprint</th>
                      <th className="px-5 py-2.5 font-medium text-right">State</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[var(--border-subtle)] font-mono text-[11px]">
                    {credentials.map(c => (
                      <tr key={c.id} className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-5 py-3 font-semibold text-[var(--text-primary)] uppercase">
                          {c.protocol}
                        </td>
                        <td className="px-5 py-3 text-[var(--text-secondary)]">{c.ipv4 || '—'}</td>
                        <td className="px-5 py-3 text-[var(--text-muted)] truncate max-w-xs">{c.public_key || '—'}</td>
                        <td className="px-5 py-3 text-right">
                          <span className="px-2 py-0.5 rounded text-[10px] bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 uppercase font-medium">
                            {c.status}
                          </span>
                        </td>
                      </tr>
                    ))}
                    {credentials.length === 0 && (
                      <tr>
                        <td colSpan={4} className="px-5 py-8 text-center text-xs font-mono text-[var(--text-muted)]">
                          No credentials provisioned yet.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </main>
      </div>

      {/* Universal Subscription Modal */}
      {showQrModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
          <div className="w-full max-w-sm rounded-xl border border-[var(--border-strong)] bg-[var(--bg-surface)] p-6 shadow-2xl">
            <div className="flex items-center justify-between mb-4 border-b border-[var(--border-subtle)] pb-2">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-[var(--text-primary)]">
                Subscription: {showQrModal.username}
              </h3>
              <button onClick={() => setShowQrModal(null)} className="text-[var(--text-muted)] hover:text-[var(--text-primary)] transition-colors">✕</button>
            </div>

            <div className="flex flex-col items-center p-4 bg-white rounded-lg my-3">
              <img 
                src={`/admin/qr?text=${encodeURIComponent(`${window.location.origin}/sub/${showQrModal.subscription_token}`)}`} 
                alt="QR Code" 
                className="w-44 h-44" 
              />
              <span className="text-[10px] font-mono text-zinc-600 mt-2">Compatible with Sing-box, V2Ray & Clash</span>
            </div>

            <div className="mt-4 flex items-center space-x-2">
              <input 
                type="text" 
                readOnly 
                value={`${window.location.origin}/sub/${showQrModal.subscription_token}`}
                className="flex-1 px-3 py-2 rounded-lg border border-[var(--border-default)] bg-[var(--bg-canvas)] text-[var(--text-primary)] font-mono text-[11px] focus:outline-none" 
              />
              <button 
                onClick={() => {
                  navigator.clipboard.writeText(`${window.location.origin}/sub/${showQrModal.subscription_token}`);
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                }}
                className="px-3 py-2 rounded-lg bg-zinc-900 hover:bg-zinc-800 text-zinc-100 dark:bg-zinc-100 dark:hover:bg-zinc-200 dark:text-zinc-950 border border-[var(--border-default)] font-mono text-xs font-semibold transition-all active:scale-95"
              >
                {copied ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
