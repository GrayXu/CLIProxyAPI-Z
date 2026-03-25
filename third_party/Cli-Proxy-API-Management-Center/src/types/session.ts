export interface SessionCapabilities {
  allowed_routes: string[];
  config_edit: boolean;
  usage_export: boolean;
  usage_import: boolean;
  logs_download: boolean;
  logs_clear: boolean;
  error_logs: boolean;
  request_log_download: boolean;
  dashboard_sensitive: boolean;
  system_models: boolean;
}

export interface SessionResponse {
  role: 'admin' | 'viewer';
  capabilities: SessionCapabilities;
}

export interface SessionState {
  role: 'admin' | 'viewer' | null;
  allowedRoutes: string[];
  capabilities: SessionCapabilities | null;
  loading: boolean;
  error: string | null;
  initialized: boolean;
}
