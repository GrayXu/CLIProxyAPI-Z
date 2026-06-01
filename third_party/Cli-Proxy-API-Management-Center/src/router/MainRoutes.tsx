import { Navigate, useRoutes, type Location, type RouteObject } from 'react-router-dom';
import { DashboardPage } from '@/pages/DashboardPage';
import { ProvidersWorkbenchPage } from '@/features/providers/ProvidersWorkbenchPage';
import { AuthFilesPage } from '@/pages/AuthFilesPage';
import { AuthFilesOAuthExcludedEditPage } from '@/pages/AuthFilesOAuthExcludedEditPage';
import { AuthFilesOAuthModelAliasEditPage } from '@/pages/AuthFilesOAuthModelAliasEditPage';
import { OAuthPage } from '@/pages/OAuthPage';
import { QuotaPage } from '@/pages/QuotaPage';
import { UsagePage } from '@/pages/UsagePage';
import { ConfigPage } from '@/pages/ConfigPage';
import { LogsPage } from '@/pages/LogsPage';
import { SystemPage } from '@/pages/SystemPage';

type RouteConfig = RouteObject & {
  requiredRoute?: string;
};

const normalizePath = (value: string): string => {
  const trimmed = String(value || '').trim();
  if (!trimmed) return '/';
  return trimmed.length > 1 ? trimmed.replace(/\/+$/, '') : trimmed;
};

const isAllowedRoute = (allowedRoutes: string[], requiredRoute?: string): boolean => {
  if (!requiredRoute) return true;
  const normalizedAllowedRoutes = allowedRoutes.map((route) => normalizePath(route));
  const normalizedRequiredRoute = normalizePath(requiredRoute);
  if (normalizedRequiredRoute === '/dashboard') {
    return normalizedAllowedRoutes.includes('/') || normalizedAllowedRoutes.includes('/dashboard');
  }
  return normalizedAllowedRoutes.includes(normalizedRequiredRoute);
};

const mainRoutes: RouteConfig[] = [
  { path: '/', element: <DashboardPage />, requiredRoute: '/dashboard' },
  { path: '/dashboard', element: <DashboardPage />, requiredRoute: '/dashboard' },
  { path: '/settings', element: <Navigate to="/config" replace />, requiredRoute: '/config' },
  { path: '/api-keys', element: <Navigate to="/config" replace />, requiredRoute: '/config' },
  { path: '/ai-providers', element: <ProvidersWorkbenchPage />, requiredRoute: '/ai-providers' },
  { path: '/ai-providers/*', element: <Navigate to="/ai-providers" replace />, requiredRoute: '/ai-providers' },
  { path: '/auth-files', element: <AuthFilesPage />, requiredRoute: '/auth-files' },
  { path: '/auth-files/oauth-excluded', element: <AuthFilesOAuthExcludedEditPage />, requiredRoute: '/auth-files' },
  { path: '/auth-files/oauth-model-alias', element: <AuthFilesOAuthModelAliasEditPage />, requiredRoute: '/auth-files' },
  { path: '/oauth', element: <OAuthPage />, requiredRoute: '/oauth' },
  { path: '/quota', element: <QuotaPage />, requiredRoute: '/quota' },
  { path: '/usage', element: <UsagePage />, requiredRoute: '/usage' },
  { path: '/config', element: <ConfigPage />, requiredRoute: '/config' },
  { path: '/logs', element: <LogsPage />, requiredRoute: '/logs' },
  { path: '/system', element: <SystemPage />, requiredRoute: '/system' },
  { path: '*', element: <Navigate to="/" replace /> },
];

export function MainRoutes({
  location,
  allowedRoutes = []
}: {
  location?: Location;
  allowedRoutes?: string[];
}) {
  const routes =
    allowedRoutes.length > 0
      ? mainRoutes.filter((route) => isAllowedRoute(allowedRoutes, route.requiredRoute))
      : mainRoutes;
  return useRoutes(routes, location);
}
