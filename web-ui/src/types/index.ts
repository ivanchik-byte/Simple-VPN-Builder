export interface Node {
  id: string;
  name: string;
  endpoint: string;
  grpc_endpoint: string;
  region: string;
  capacity_gbps?: number;
  status: 'online' | 'degraded' | 'offline';
  public_key: string;
  last_heartbeat?: string;
}

export interface User {
  id: string;
  username: string;
  email?: string;
  status: 'active' | 'suspended' | 'expired';
  traffic_limit?: number;
  traffic_used: number;
  subscription_token: string;
  expires_at?: string;
  created_at: string;
}

export interface Plan {
  id: string;
  name: string;
  monthly_price: string;
  traffic_limit: number;
  device_limit: number;
  protocols: string[];
}

export interface Credential {
  id: string;
  user_id: string;
  node_id: string;
  protocol: 'wireguard' | 'amneziawg' | 'vless';
  public_key?: string;
  ipv4?: string;
  status: string;
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
}
