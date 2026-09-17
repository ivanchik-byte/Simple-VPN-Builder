async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(path, { credentials: 'include' });
  if (res.status === 401) {
    window.location.href = '/admin/login';
    throw new Error('Unauthorized');
  }
  if (!res.ok) {
    throw new Error(`Request failed: ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export type PgText = string | { String: string; Valid: boolean } | null;

export function textVal(v: PgText): string {
  if (v == null) return '';
  if (typeof v === 'string') return v;
  return v.Valid ? v.String : '';
}

export interface Overview {
  total_rx_bytes: number;
  total_tx_bytes: number;
}

export interface Page<T> {
  items: T[];
  pagination: { page: number; per_page: number; total_items: number; total_pages: number };
}

export interface ApiNode {
  id: string;
  name: string;
  endpoint: string | null;
  grpc_endpoint: string | null;
  public_key: string | null;
  region: string | null;
  status: string | null;
  last_heartbeat: string | null;
}

export interface Telemetry {
  cpu_percent: number;
  cpu_model?: string;
  ram_percent: number;
  ram_used: number;
  ram_total: number;
  disk_percent: number;
  disk_used: number;
  disk_total: number;
  rx_speed: number;
  tx_speed: number;
  history: { timestamp: string; cpu_percent: number; ram_percent: number; disk_percent: number; rx_speed: number; tx_speed: number }[];
}

export interface ApiCredential {
  id: string;
  user_id: string;
  node_id: string;
  protocol: string;
  ipv4?: string | null;
  public_key?: string | null;
  status?: string | null;
}

export interface ApiUser {
  id: string;
  username: string;
  email: string | null;
  status: string | null;
  traffic_used: number | null;
}

export function numVal(v: number | { Int64: number; Valid: boolean } | null | undefined): number {
  if (v == null) return 0;
  if (typeof v === 'number') return v;
  return v.Valid ? v.Int64 : 0;
}

export const api = {
  overview: () => apiGet<Overview>('/api/v1/analytics/overview'),
  nodes: () => apiGet<Page<ApiNode>>('/api/v1/nodes?per_page=50'),
  onlineNodes: () => apiGet<Page<ApiNode>>('/api/v1/nodes?status=online&per_page=1'),
  users: () => apiGet<Page<ApiUser>>('/api/v1/users?per_page=100'),
  activeUsers: () => apiGet<Page<unknown>>('/api/v1/users?status=active&per_page=1'),
  credentials: () => apiGet<ApiCredential[]>('/api/v1/credentials'),
  telemetry: () => apiGet<Telemetry>('/api/v1/system/telemetry?limit=40'),
};

export function formatBytes(n: number): string {
  if (!n || n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatRate(bps: number): string {
  return `${formatBytes(bps)}/s`;
}

export function timeAgo(iso: string | null, lang: 'en' | 'ru' = 'ru'): string {
  if (!iso) return lang === 'ru' ? 'Никогда' : 'Never';
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 5) return lang === 'ru' ? 'только что' : 'just now';
  if (s < 60) return lang === 'ru' ? `${s} с назад` : `${s}s ago`;
  if (s < 3600) return lang === 'ru' ? `${Math.floor(s / 60)} мин назад` : `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return lang === 'ru' ? `${Math.floor(s / 3600)} ч назад` : `${Math.floor(s / 3600)}h ago`;
  return lang === 'ru' ? `${Math.floor(s / 86400)} д назад` : `${Math.floor(s / 86400)}d ago`;
}

export async function logout(): Promise<void> {
  await fetch('/admin/logout', { method: 'POST', credentials: 'include' }).catch(() => {});
  window.location.href = '/admin/login';
}

let roleCache: Promise<string> | null = null;

export function fetchRole(): Promise<string> {
  if (!roleCache) {
    roleCache = fetch('/admin/settings-data', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { role: '' }))
      .then((d) => (d as { role?: string }).role ?? '')
      .catch(() => '');
  }
  return roleCache;
}

export interface CallerPerms {
  role: string;
  perms: Record<string, boolean>;
}

let permsCache: Promise<CallerPerms> | null = null;

export function fetchPerms(): Promise<CallerPerms> {
  if (!permsCache) {
    permsCache = fetch('/admin/settings-data', { credentials: 'include' })
      .then((r) => (r.ok ? r.json() : { role: '', perms: {} }))
      .then((d) => ({ role: (d as CallerPerms).role ?? '', perms: (d as CallerPerms).perms ?? {} }))
      .catch(() => ({ role: '', perms: {} }));
  }
  return permsCache;
}
