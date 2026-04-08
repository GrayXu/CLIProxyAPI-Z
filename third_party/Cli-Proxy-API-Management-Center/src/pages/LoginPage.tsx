import React, { useEffect, useMemo, useState, useCallback } from 'react';
import { Navigate, useNavigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import axios from 'axios';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { SelectionCheckbox } from '@/components/ui/SelectionCheckbox';
import { IconEye, IconEyeOff } from '@/components/ui/icons';
import { useAuthStore, useLanguageStore, useNotificationStore } from '@/stores';
import { issueApiKeyApi } from '@/services/api';
import { detectApiBaseFromLocation, normalizeApiBase } from '@/utils/connection';
import { LANGUAGE_LABEL_KEYS, LANGUAGE_ORDER } from '@/utils/constants';
import { isSupportedLanguage } from '@/utils/language';
import { copyToClipboard } from '@/utils/clipboard';
import { INLINE_LOGO_JPEG } from '@/assets/logoInline';
import type { ApiError, IssueApiKeyResponse } from '@/types';
import styles from './LoginPage.module.scss';

/**
 * 将 API 错误转换为本地化的用户友好消息
 */
type RedirectState = { from?: { pathname?: string } };

const ISSUE_LOGIN_RETRY_DELAYS_MS = [0, 200, 400, 800, 1200, 1600];

const wait = (ms: number) => new Promise<void>((resolve) => window.setTimeout(resolve, ms));

function getLocalizedErrorMessage(error: unknown, t: (key: string) => string): string {
  let status: number | undefined;
  let code: string | undefined;
  let message = '';

  if (axios.isAxiosError(error)) {
    status = error.response?.status;
    code = error.code;
    message =
      typeof error.response?.data?.error === 'string'
        ? error.response.data.error
        : typeof error.response?.data?.message === 'string'
          ? error.response.data.message
          : error.message;
  } else {
    const apiError = error as Partial<ApiError>;
    status = typeof apiError.status === 'number' ? apiError.status : undefined;
    code = typeof apiError.code === 'string' ? apiError.code : undefined;
    message =
      error instanceof Error
        ? error.message
        : typeof apiError.message === 'string'
          ? apiError.message
          : typeof error === 'string'
            ? error
            : '';
  }

  // 根据 HTTP 状态码判断
  if (status === 401) {
    return t('login.error_unauthorized');
  }
  if (status === 403) {
    return t('login.error_forbidden');
  }
  if (status === 404) {
    return t('login.error_not_found');
  }
  if (status && status >= 500) {
    return t('login.error_server');
  }

  // 根据 axios 错误码判断
  if (code === 'ECONNABORTED' || message.toLowerCase().includes('timeout')) {
    return t('login.error_timeout');
  }
  if (code === 'ERR_NETWORK' || message.toLowerCase().includes('network error')) {
    return t('login.error_network');
  }
  if (code === 'ERR_CERT_AUTHORITY_INVALID' || message.toLowerCase().includes('certificate')) {
    return t('login.error_ssl');
  }

  // 检查 CORS 错误
  if (message.toLowerCase().includes('cors') || message.toLowerCase().includes('cross-origin')) {
    return t('login.error_cors');
  }

  // 默认错误消息
  return t('login.error_invalid');
}

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { showNotification } = useNotificationStore();
  const language = useLanguageStore((state) => state.language);
  const setLanguage = useLanguageStore((state) => state.setLanguage);
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const login = useAuthStore((state) => state.login);
  const restoreSession = useAuthStore((state) => state.restoreSession);
  const storedBase = useAuthStore((state) => state.apiBase);
  const storedKey = useAuthStore((state) => state.managementKey);
  const storedRememberPassword = useAuthStore((state) => state.rememberPassword);

  const [apiBase, setApiBase] = useState('');
  const [managementKey, setManagementKey] = useState('');
  const [issuePassword, setIssuePassword] = useState('');
  const [showCustomBase, setShowCustomBase] = useState(false);
  const [showKey, setShowKey] = useState(false);
  const [showIssuePassword, setShowIssuePassword] = useState(false);
  const [rememberPassword, setRememberPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [issueLoading, setIssueLoading] = useState(false);
  const [issueConfirmLoading, setIssueConfirmLoading] = useState(false);
  const [autoLoading, setAutoLoading] = useState(true);
  const [autoLoginSuccess, setAutoLoginSuccess] = useState(false);
  const [error, setError] = useState('');
  const [issuedKeyResult, setIssuedKeyResult] = useState<IssueApiKeyResponse | null>(null);

  const detectedBase = useMemo(() => detectApiBaseFromLocation(), []);
  const languageOptions = useMemo(
    () =>
      LANGUAGE_ORDER.map((lang) => ({
        value: lang,
        label: t(LANGUAGE_LABEL_KEYS[lang])
      })),
    [t]
  );
  const handleLanguageChange = useCallback(
    (selectedLanguage: string) => {
      if (!isSupportedLanguage(selectedLanguage)) {
        return;
      }
      setLanguage(selectedLanguage);
    },
    [setLanguage]
  );

  useEffect(() => {
    const init = async () => {
      try {
        const autoLoggedIn = await restoreSession();
        if (autoLoggedIn) {
          setAutoLoginSuccess(true);
          // 延迟跳转，让用户看到成功动画
          setTimeout(() => {
            const redirect = (location.state as RedirectState | null)?.from?.pathname || '/';
            navigate(redirect, { replace: true });
          }, 1500);
        } else {
          setApiBase(storedBase || detectedBase);
          setManagementKey(storedKey || '');
          setRememberPassword(storedRememberPassword || Boolean(storedKey));
        }
      } finally {
        if (!autoLoginSuccess) {
          setAutoLoading(false);
        }
      }
    };

    init();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const finalizeLogin = useCallback(
    async (baseToUse: string, key: string, enableRetry: boolean) => {
      const delays = enableRetry ? ISSUE_LOGIN_RETRY_DELAYS_MS : [0];
      let lastError: unknown = null;

      for (const delayMs of delays) {
        if (delayMs > 0) {
          await wait(delayMs);
        }
        try {
          await login({
            apiBase: baseToUse,
            managementKey: key,
            rememberPassword
          });
          return;
        } catch (err: unknown) {
          lastError = err;
        }
      }

      throw lastError;
    },
    [login, rememberPassword]
  );

  const handleSubmit = useCallback(async () => {
    if (!managementKey.trim()) {
      setError(t('login.error_required'));
      return;
    }

    const baseToUse = apiBase ? normalizeApiBase(apiBase) : detectedBase;
    setLoading(true);
    setError('');
    try {
      await finalizeLogin(baseToUse, managementKey.trim(), false);
      showNotification(t('common.connected_status'), 'success');
      navigate('/', { replace: true });
    } catch (err: unknown) {
      const message = getLocalizedErrorMessage(err, t);
      setError(message);
      showNotification(`${t('notification.login_failed')}: ${message}`, 'error');
    } finally {
      setLoading(false);
    }
  }, [apiBase, detectedBase, finalizeLogin, managementKey, navigate, showNotification, t]);

  const handleIssueAndLogin = useCallback(async () => {
    if (!issuePassword.trim()) {
      setError(t('login.issue_password_required'));
      return;
    }

    const baseToUse = apiBase ? normalizeApiBase(apiBase) : detectedBase;
    setIssueLoading(true);
    setError('');
    try {
      const issued = await issueApiKeyApi.issue(baseToUse, { password: issuePassword.trim() });
      setManagementKey(issued.api_key);
      setIssuedKeyResult(issued);
      showNotification(t('login.issue_success'), 'success');
    } catch (err: unknown) {
      const message =
        err instanceof Error && err.message === t('login.issue_login_pending')
          ? err.message
          : getLocalizedErrorMessage(err, t);
      setError(message);
      showNotification(`${t('notification.login_failed')}: ${message}`, 'error');
    } finally {
      setIssueLoading(false);
    }
  }, [apiBase, detectedBase, issuePassword, showNotification, t]);

  const handleConfirmIssuedKey = useCallback(async () => {
    if (!issuedKeyResult) {
      return;
    }
    const baseToUse = apiBase ? normalizeApiBase(apiBase) : detectedBase;
    setIssueConfirmLoading(true);
    setError('');
    try {
      try {
        await finalizeLogin(baseToUse, issuedKeyResult.api_key, true);
      } catch {
        throw new Error(t('login.issue_login_pending'));
      }
      setIssuedKeyResult(null);
      navigate('/', { replace: true });
    } catch (err: unknown) {
      const message =
        err instanceof Error && err.message === t('login.issue_login_pending')
          ? err.message
          : getLocalizedErrorMessage(err, t);
      setError(message);
      showNotification(`${t('notification.login_failed')}: ${message}`, 'error');
    } finally {
      setIssueConfirmLoading(false);
    }
  }, [apiBase, detectedBase, finalizeLogin, issuedKeyResult, navigate, showNotification, t]);

  const handleCopyIssuedKey = useCallback(async () => {
    if (!issuedKeyResult?.api_key) {
      return;
    }
    const copied = await copyToClipboard(issuedKeyResult.api_key);
    showNotification(
      copied
        ? t('notification.link_copied', { defaultValue: 'Copied to clipboard' })
        : t('notification.copy_failed', { defaultValue: 'Copy failed' }),
      copied ? 'success' : 'error'
    );
  }, [issuedKeyResult, showNotification, t]);

  const handleSubmitKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === 'Enter' && !loading) {
        event.preventDefault();
        handleSubmit();
      }
    },
    [loading, handleSubmit]
  );

  const handleIssueKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === 'Enter' && !issueLoading) {
        event.preventDefault();
        handleIssueAndLogin();
      }
    },
    [handleIssueAndLogin, issueLoading]
  );

  if (isAuthenticated && !autoLoading && !autoLoginSuccess) {
    const redirect = (location.state as RedirectState | null)?.from?.pathname || '/';
    return <Navigate to={redirect} replace />;
  }

  // 显示启动动画（自动登录中或自动登录成功）
  const showSplash = autoLoading || autoLoginSuccess;

  return (
    <div className={styles.container}>
      {/* 左侧品牌展示区 */}
      <div className={styles.brandPanel}>
        <div className={styles.brandContent}>
          <span className={styles.brandWord}>CLI</span>
          <span className={styles.brandWord}>PROXY</span>
          <span className={styles.brandWord}>API</span>
        </div>
      </div>

      {/* 右侧功能交互区 */}
      <div className={styles.formPanel}>
        {showSplash ? (
          /* 启动动画 */
          <div className={styles.splashContent}>
            <img src={INLINE_LOGO_JPEG} alt="CPAMC" className={styles.splashLogo} />
            <h1 className={styles.splashTitle}>{t('splash.title')}</h1>
            <p className={styles.splashSubtitle}>{t('splash.subtitle')}</p>
            <div className={styles.splashLoader}>
              <div className={styles.splashLoaderBar} />
            </div>
          </div>
        ) : (
          /* 登录表单 */
          <div className={styles.formContent}>
            {/* Logo */}
            <img src={INLINE_LOGO_JPEG} alt="Logo" className={styles.logo} />

            {/* 登录表单卡片 */}
            <div className={styles.loginCard}>
              <div className={styles.loginHeader}>
                <div className={styles.titleRow}>
                  <div className={styles.title}>{t('title.login')}</div>
                  <Select
                    className={styles.languageSelect}
                    value={language}
                    options={languageOptions}
                    onChange={handleLanguageChange}
                    fullWidth={false}
                    ariaLabel={t('language.switch')}
                  />
                </div>
                <a
                  className={styles.forkLink}
                  href="https://github.com/GrayXu/CLIProxyAPI-Z"
                  target="_blank"
                  rel="noreferrer"
                >
                  GrayXu/CLIProxyAPI-Z
                </a>
                <div className={styles.subtitle}>{t('login.subtitle')}</div>
              </div>

              <div className={styles.connectionBox}>
                <div className={styles.label}>{t('login.connection_current')}</div>
                <div className={styles.value}>{apiBase || detectedBase}</div>
                <div className={styles.hint}>{t('login.connection_auto_hint')}</div>
              </div>

              <div className={styles.toggleAdvanced}>
                <SelectionCheckbox
                  checked={showCustomBase}
                  onChange={setShowCustomBase}
                  ariaLabel={t('login.custom_connection_label')}
                  label={t('login.custom_connection_label')}
                  labelClassName={styles.toggleLabel}
                />
              </div>

              {showCustomBase && (
                <Input
                  label={t('login.custom_connection_label')}
                  placeholder={t('login.custom_connection_placeholder')}
                  value={apiBase}
                  onChange={(e) => setApiBase(e.target.value)}
                  hint={t('login.custom_connection_hint')}
                />
              )}

              <Input
                autoFocus
                label={t('login.management_key_label')}
                placeholder={t('login.management_key_placeholder')}
                type={showKey ? 'text' : 'password'}
                value={managementKey}
                onChange={(e) => setManagementKey(e.target.value)}
                onKeyDown={handleSubmitKeyDown}
                rightElement={
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => setShowKey((prev) => !prev)}
                    aria-label={
                      showKey
                        ? t('login.hide_key', { defaultValue: '隐藏密钥' })
                        : t('login.show_key', { defaultValue: '显示密钥' })
                    }
                    title={
                      showKey
                        ? t('login.hide_key', { defaultValue: '隐藏密钥' })
                        : t('login.show_key', { defaultValue: '显示密钥' })
                    }
                  >
                    {showKey ? <IconEyeOff size={16} /> : <IconEye size={16} />}
                  </button>
                }
              />

              <div className={styles.divider}>{t('login.issue_section_divider')}</div>

              <Input
                label={t('login.issue_password_label')}
                placeholder={t('login.issue_password_placeholder')}
                hint={t('login.issue_password_hint')}
                type={showIssuePassword ? 'text' : 'password'}
                value={issuePassword}
                onChange={(e) => setIssuePassword(e.target.value)}
                onKeyDown={handleIssueKeyDown}
                rightElement={
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => setShowIssuePassword((prev) => !prev)}
                    aria-label={
                      showIssuePassword
                        ? t('login.hide_key', { defaultValue: '隐藏密钥' })
                        : t('login.show_key', { defaultValue: '显示密钥' })
                    }
                    title={
                      showIssuePassword
                        ? t('login.hide_key', { defaultValue: '隐藏密钥' })
                        : t('login.show_key', { defaultValue: '显示密钥' })
                    }
                  >
                    {showIssuePassword ? <IconEyeOff size={16} /> : <IconEye size={16} />}
                  </button>
                }
              />

              <div className={styles.toggleAdvanced}>
                <SelectionCheckbox
                  checked={rememberPassword}
                  onChange={setRememberPassword}
                  ariaLabel={t('login.remember_password_label')}
                  label={t('login.remember_password_label')}
                  labelClassName={styles.toggleLabel}
                />
              </div>

              <Button fullWidth onClick={handleSubmit} loading={loading}>
                {loading ? t('login.submitting') : t('login.submit_button')}
              </Button>

              <Button fullWidth variant="secondary" onClick={handleIssueAndLogin} loading={issueLoading}>
                {issueLoading ? t('login.submitting') : t('login.issue_button')}
              </Button>

              {error && <div className={styles.errorBox}>{error}</div>}
            </div>
          </div>
        )}
      </div>
      <Modal
        open={issuedKeyResult !== null}
        onClose={() => {
          if (issueConfirmLoading) return;
          setIssuedKeyResult(null);
        }}
        closeDisabled={issueConfirmLoading}
        title={t('login.issue_result_title')}
        footer={
          <div className={styles.issueResultFooter}>
            <Button variant="secondary" onClick={handleCopyIssuedKey} disabled={issueConfirmLoading}>
              {t('common.copy')}
            </Button>
            <Button onClick={handleConfirmIssuedKey} loading={issueConfirmLoading}>
              {t('common.confirm')}
            </Button>
          </div>
        }
      >
        <div className={styles.issueResultBody}>
          <p className={styles.issueResultDescription}>{t('login.issue_result_desc')}</p>
          <div className={styles.issueResultField}>
            <div className={styles.issueResultLabel}>{t('login.issue_result_key_label')}</div>
            <code className={styles.issueResultValue}>{issuedKeyResult?.api_key ?? ''}</code>
          </div>
          <div className={styles.issueResultField}>
            <div className={styles.issueResultLabel}>{t('login.issue_result_role_label')}</div>
            <div className={styles.issueResultMeta}>{issuedKeyResult?.role ?? 'viewer'}</div>
          </div>
        </div>
      </Modal>
    </div>
  );
}
