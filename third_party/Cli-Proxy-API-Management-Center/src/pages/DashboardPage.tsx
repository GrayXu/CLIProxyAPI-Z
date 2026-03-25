import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  IconKey,
  IconBot,
  IconFileText,
  IconSatellite
} from '@/components/ui/icons';
import { useAuthStore, useConfigStore, useModelsStore, useSessionStore } from '@/stores';
import { apiKeysApi, providersApi, authFilesApi } from '@/services/api';
import styles from './DashboardPage.module.scss';

interface QuickStat {
  label: string;
  value: number | string;
  icon: React.ReactNode;
  path: string;
  loading?: boolean;
  sublabel?: string;
}

interface ProviderStats {
  gemini: number | null;
  codex: number | null;
  claude: number | null;
  openai: number | null;
}

const getArrayLength = (value: unknown): number | null =>
  Array.isArray(value) ? value.length : null;

const getAuthFilesLength = (value: unknown): number | null => {
  if (!value || typeof value !== 'object') return null;
  const files = (value as { files?: unknown }).files;
  return Array.isArray(files) ? files.length : null;
};

export function DashboardPage() {
  const { t, i18n } = useTranslation();
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const serverVersion = useAuthStore((state) => state.serverVersion);
  const serverBuildDate = useAuthStore((state) => state.serverBuildDate);
  const apiBase = useAuthStore((state) => state.apiBase);
  const config = useConfigStore((state) => state.config);
  const canViewConfig = useSessionStore((state) => state.isRouteAllowed('/config'));
  const canViewProviders = useSessionStore((state) => state.isRouteAllowed('/ai-providers'));
  const canViewAuthFiles = useSessionStore((state) => state.isRouteAllowed('/auth-files'));
  const canViewSystemModels = useSessionStore((state) => state.hasCapability('system_models'));
  const canViewDashboardSensitive = useSessionStore((state) =>
    state.hasCapability('dashboard_sensitive')
  );

  const models = useModelsStore((state) => state.models);
  const modelsLoading = useModelsStore((state) => state.loading);
  const fetchModelsFromStore = useModelsStore((state) => state.fetchModels);

  const [stats, setStats] = useState<{
    apiKeys: number | null;
    authFiles: number | null;
  }>({
    apiKeys: null,
    authFiles: null
  });

  const [providerStats, setProviderStats] = useState<ProviderStats>({
    gemini: null,
    codex: null,
    claude: null,
    openai: null
  });

  const [loading, setLoading] = useState(true);

  const apiKeysCache = useRef<string[]>([]);

  useEffect(() => {
    apiKeysCache.current = [];
  }, [apiBase, config?.apiKeys]);

  const normalizeApiKeyList = (input: unknown): string[] => {
    if (!Array.isArray(input)) return [];
    const seen = new Set<string>();
    const keys: string[] = [];

    input.forEach((item) => {
      const record =
        item !== null && typeof item === 'object' && !Array.isArray(item)
          ? (item as Record<string, unknown>)
          : null;
      const value =
        typeof item === 'string'
          ? item
          : record
            ? (record['api-key'] ?? record['apiKey'] ?? record.key ?? record.Key)
            : '';
      const trimmed = String(value ?? '').trim();
      if (!trimmed || seen.has(trimmed)) return;
      seen.add(trimmed);
      keys.push(trimmed);
    });

    return keys;
  };

  const resolveApiKeysForModels = useCallback(async () => {
    if (!canViewSystemModels) {
      return [];
    }
    if (apiKeysCache.current.length) {
      return apiKeysCache.current;
    }

    const configKeys = normalizeApiKeyList(config?.apiKeys);
    if (configKeys.length) {
      apiKeysCache.current = configKeys;
      return configKeys;
    }

    try {
      const list = await apiKeysApi.list();
      const normalized = normalizeApiKeyList(list);
      if (normalized.length) {
        apiKeysCache.current = normalized;
      }
      return normalized;
    } catch {
      return [];
    }
  }, [canViewSystemModels, config?.apiKeys]);

  const fetchModels = useCallback(async () => {
    if (!canViewSystemModels || connectionStatus !== 'connected' || !apiBase) {
      return;
    }

    try {
      const apiKeys = await resolveApiKeysForModels();
      const primaryKey = apiKeys[0];
      await fetchModelsFromStore(apiBase, primaryKey);
    } catch {
      // Ignore model fetch errors on dashboard
    }
  }, [
    apiBase,
    canViewSystemModels,
    connectionStatus,
    resolveApiKeysForModels,
    fetchModelsFromStore
  ]);

  useEffect(() => {
    const fetchStats = async () => {
      setLoading(true);
      try {
        const [keysRes, filesRes, geminiRes, codexRes, claudeRes, openaiRes] = await Promise.allSettled([
          canViewDashboardSensitive && canViewConfig ? apiKeysApi.list() : Promise.resolve(null),
          canViewAuthFiles ? authFilesApi.list() : Promise.resolve(null),
          canViewProviders ? providersApi.getGeminiKeys() : Promise.resolve(null),
          canViewProviders ? providersApi.getCodexConfigs() : Promise.resolve(null),
          canViewProviders ? providersApi.getClaudeConfigs() : Promise.resolve(null),
          canViewProviders ? providersApi.getOpenAIProviders() : Promise.resolve(null)
        ]);

        setStats({
          apiKeys:
            canViewDashboardSensitive && canViewConfig && keysRes.status === 'fulfilled'
              ? getArrayLength(keysRes.value)
              : null,
          authFiles:
            canViewAuthFiles && filesRes.status === 'fulfilled'
              ? getAuthFilesLength(filesRes.value)
              : null
        });

        setProviderStats({
          gemini:
            canViewProviders && geminiRes.status === 'fulfilled'
              ? getArrayLength(geminiRes.value)
              : null,
          codex:
            canViewProviders && codexRes.status === 'fulfilled'
              ? getArrayLength(codexRes.value)
              : null,
          claude:
            canViewProviders && claudeRes.status === 'fulfilled'
              ? getArrayLength(claudeRes.value)
              : null,
          openai:
            canViewProviders && openaiRes.status === 'fulfilled'
              ? getArrayLength(openaiRes.value)
              : null
        });
      } finally {
        setLoading(false);
      }
    };

    if (connectionStatus === 'connected') {
      fetchStats();
      if (canViewSystemModels) {
        fetchModels();
      }
    } else {
      setLoading(false);
    }
  }, [
    canViewAuthFiles,
    canViewConfig,
    canViewDashboardSensitive,
    canViewProviders,
    canViewSystemModels,
    connectionStatus,
    fetchModels
  ]);

  // Calculate total provider keys only when all provider stats are available.
  const providerStatsReady =
    providerStats.gemini !== null &&
    providerStats.codex !== null &&
    providerStats.claude !== null &&
    providerStats.openai !== null;
  const hasProviderStats =
    providerStats.gemini !== null ||
    providerStats.codex !== null ||
    providerStats.claude !== null ||
    providerStats.openai !== null;
  const totalProviderKeys = providerStatsReady
    ? (providerStats.gemini ?? 0) +
      (providerStats.codex ?? 0) +
      (providerStats.claude ?? 0) +
      (providerStats.openai ?? 0)
    : 0;

  const quickStats: QuickStat[] = [
    ...(canViewDashboardSensitive && canViewConfig
      ? [
          {
            label: t('dashboard.management_keys'),
            value: stats.apiKeys ?? '-',
            icon: <IconKey size={24} />,
            path: '/config',
            loading: loading && stats.apiKeys === null,
            sublabel: t('nav.config_management')
          }
        ]
      : []),
    ...(canViewProviders
      ? [
          {
            label: t('nav.ai_providers'),
            value: loading ? '-' : providerStatsReady ? totalProviderKeys : '-',
            icon: <IconBot size={24} />,
            path: '/ai-providers',
            loading: loading,
            sublabel: hasProviderStats
              ? t('dashboard.provider_keys_detail', {
                  gemini: providerStats.gemini ?? '-',
                  codex: providerStats.codex ?? '-',
                  claude: providerStats.claude ?? '-',
                  openai: providerStats.openai ?? '-'
                })
              : undefined
          }
        ]
      : []),
    ...(canViewAuthFiles
      ? [
          {
            label: t('nav.auth_files'),
            value: stats.authFiles ?? '-',
            icon: <IconFileText size={24} />,
            path: '/auth-files',
            loading: loading && stats.authFiles === null,
            sublabel: t('dashboard.oauth_credentials')
          }
        ]
      : []),
    ...(canViewSystemModels
      ? [
          {
            label: t('dashboard.available_models'),
            value: modelsLoading ? '-' : models.length,
            icon: <IconSatellite size={24} />,
            path: '/system',
            loading: modelsLoading,
            sublabel: t('dashboard.available_models_desc')
          }
        ]
      : [])
  ];

  const routingStrategyRaw = config?.routingStrategy?.trim() || '';
  const routingStrategyDisplay = !routingStrategyRaw
    ? '-'
    : routingStrategyRaw === 'round-robin'
      ? t('basic_settings.routing_strategy_round_robin')
      : routingStrategyRaw === 'fill-first'
        ? t('basic_settings.routing_strategy_fill_first')
        : routingStrategyRaw === 'quota-sticky'
          ? t('basic_settings.routing_strategy_quota_sticky')
        : routingStrategyRaw;
  const routingStrategyBadgeClass = !routingStrategyRaw
    ? styles.configBadgeUnknown
    : routingStrategyRaw === 'round-robin'
      ? styles.configBadgeRoundRobin
      : routingStrategyRaw === 'fill-first'
        ? styles.configBadgeFillFirst
        : styles.configBadgeUnknown;

  return (
    <div className={styles.dashboard}>
      <div className={styles.header}>
        <h1 className={styles.title}>{t('dashboard.title')}</h1>
        <p className={styles.subtitle}>{t('dashboard.subtitle')}</p>
      </div>

      <div className={styles.connectionCard}>
        <div className={styles.connectionStatus}>
          <span
            className={`${styles.statusDot} ${
              connectionStatus === 'connected'
                ? styles.connected
                : connectionStatus === 'connecting'
                  ? styles.connecting
                  : styles.disconnected
            }`}
          />
          <span className={styles.statusText}>
            {t(
              connectionStatus === 'connected'
                ? 'common.connected'
                : connectionStatus === 'connecting'
                  ? 'common.connecting'
                  : 'common.disconnected'
            )}
          </span>
        </div>
        <div className={styles.connectionInfo}>
          <span className={styles.serverUrl}>{apiBase || '-'}</span>
          {serverVersion && (
            <span className={styles.serverVersion}>
              v{serverVersion.trim().replace(/^[vV]+/, '')}
            </span>
          )}
          {serverBuildDate && (
            <span className={styles.buildDate}>
              {new Date(serverBuildDate).toLocaleDateString(i18n.language)}
            </span>
          )}
        </div>
      </div>

      {quickStats.length > 0 && (
        <div className={styles.statsGrid}>
          {quickStats.map((stat) => (
            <Link key={stat.path} to={stat.path} className={styles.statCard}>
              <div className={styles.statIcon}>{stat.icon}</div>
              <div className={styles.statContent}>
                <span className={styles.statValue}>{stat.loading ? '...' : stat.value}</span>
                <span className={styles.statLabel}>{stat.label}</span>
                {stat.sublabel && !stat.loading && (
                  <span className={styles.statSublabel}>{stat.sublabel}</span>
                )}
              </div>
            </Link>
          ))}
        </div>
      )}

      {config && (
        <div className={styles.section}>
          <h2 className={styles.sectionTitle}>{t('dashboard.current_config')}</h2>
          <div className={styles.configGrid}>
            <div className={styles.configItem}>
              <span className={styles.configLabel}>{t('basic_settings.debug_enable')}</span>
              <span className={`${styles.configValue} ${config.debug ? styles.enabled : styles.disabled}`}>
                {config.debug ? t('common.yes') : t('common.no')}
              </span>
            </div>
            <div className={styles.configItem}>
              <span className={styles.configLabel}>{t('basic_settings.usage_statistics_enable')}</span>
              <span className={`${styles.configValue} ${config.usageStatisticsEnabled ? styles.enabled : styles.disabled}`}>
                {config.usageStatisticsEnabled ? t('common.yes') : t('common.no')}
              </span>
            </div>
            <div className={styles.configItem}>
              <span className={styles.configLabel}>{t('basic_settings.logging_to_file_enable')}</span>
              <span className={`${styles.configValue} ${config.loggingToFile ? styles.enabled : styles.disabled}`}>
                {config.loggingToFile ? t('common.yes') : t('common.no')}
              </span>
            </div>
            <div className={styles.configItem}>
              <span className={styles.configLabel}>{t('basic_settings.retry_count_label')}</span>
              <span className={styles.configValue}>{config.requestRetry ?? 0}</span>
            </div>
            <div className={styles.configItem}>
              <span className={styles.configLabel}>{t('basic_settings.ws_auth_enable')}</span>
              <span className={`${styles.configValue} ${config.wsAuth ? styles.enabled : styles.disabled}`}>
                {config.wsAuth ? t('common.yes') : t('common.no')}
              </span>
            </div>
            <div className={styles.configItem}>
              <span className={styles.configLabel}>{t('dashboard.routing_strategy')}</span>
              <span className={`${styles.configBadge} ${routingStrategyBadgeClass}`}>
                {routingStrategyDisplay}
              </span>
            </div>
            {config.proxyUrl && (
              <div className={`${styles.configItem} ${styles.configItemFull}`}>
                <span className={styles.configLabel}>{t('basic_settings.proxy_url_label')}</span>
                <span className={styles.configValueMono}>{config.proxyUrl}</span>
              </div>
            )}
          </div>
          {canViewConfig && (
            <Link to="/config" className={styles.viewMoreLink}>
              {t('dashboard.edit_settings')} →
            </Link>
          )}
        </div>
      )}
    </div>
  );
}
