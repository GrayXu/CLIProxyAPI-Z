import { Navigate, useRoutes, type Location, type RouteObject } from 'react-router-dom';
import { DashboardPage } from '@/pages/DashboardPage';
import { AiProvidersPage } from '@/pages/AiProvidersPage';
import { AiProvidersAmpcodeEditPage } from '@/pages/AiProvidersAmpcodeEditPage';
import { AiProvidersClaudeEditLayout } from '@/pages/AiProvidersClaudeEditLayout';
import { AiProvidersClaudeEditPage } from '@/pages/AiProvidersClaudeEditPage';
import { AiProvidersClaudeModelsPage } from '@/pages/AiProvidersClaudeModelsPage';
import { AiProvidersCodexEditPage } from '@/pages/AiProvidersCodexEditPage';
import { AiProvidersGeminiEditPage } from '@/pages/AiProvidersGeminiEditPage';
import { AiProvidersOpenAIEditLayout } from '@/pages/AiProvidersOpenAIEditLayout';
import { AiProvidersOpenAIEditPage } from '@/pages/AiProvidersOpenAIEditPage';
import { AiProvidersOpenAIModelsPage } from '@/pages/AiProvidersOpenAIModelsPage';
import { AiProvidersVertexEditPage } from '@/pages/AiProvidersVertexEditPage';
import { AuthFilesPage } from '@/pages/AuthFilesPage';
import { AuthFilesOAuthExcludedEditPage } from '@/pages/AuthFilesOAuthExcludedEditPage';
import { AuthFilesOAuthModelAliasEditPage } from '@/pages/AuthFilesOAuthModelAliasEditPage';
import { OAuthPage } from '@/pages/OAuthPage';
import { QuotaPage } from '@/pages/QuotaPage';
import { UsagePage } from '@/pages/UsagePage';
import { ConfigPage } from '@/pages/ConfigPage';
import { LogsPage } from '@/pages/LogsPage';
import { SystemPage } from '@/pages/SystemPage';

type RouteConfig = {
  path?: string;
  index?: boolean;
  element?: RouteObject['element'];
  children?: RouteConfig[];
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
    return (
      normalizedAllowedRoutes.includes('/') ||
      normalizedAllowedRoutes.includes('/dashboard')
    );
  }
  return normalizedAllowedRoutes.includes(normalizedRequiredRoute);
};

const mainRoutes: RouteConfig[] = [
  { path: '/', element: <DashboardPage />, requiredRoute: '/dashboard' },
  { path: '/dashboard', element: <DashboardPage />, requiredRoute: '/dashboard' },
  { path: '/settings', element: <Navigate to="/config" replace />, requiredRoute: '/config' },
  { path: '/api-keys', element: <Navigate to="/config" replace />, requiredRoute: '/config' },
  { path: '/ai-providers/gemini/new', element: <AiProvidersGeminiEditPage />, requiredRoute: '/ai-providers' },
  { path: '/ai-providers/gemini/:index', element: <AiProvidersGeminiEditPage />, requiredRoute: '/ai-providers' },
  { path: '/ai-providers/codex/new', element: <AiProvidersCodexEditPage />, requiredRoute: '/ai-providers' },
  { path: '/ai-providers/codex/:index', element: <AiProvidersCodexEditPage />, requiredRoute: '/ai-providers' },
  {
    path: '/ai-providers/claude/new',
    element: <AiProvidersClaudeEditLayout />,
    requiredRoute: '/ai-providers',
    children: [
      { index: true, element: <AiProvidersClaudeEditPage /> },
      { path: 'models', element: <AiProvidersClaudeModelsPage /> },
    ],
  },
  {
    path: '/ai-providers/claude/:index',
    element: <AiProvidersClaudeEditLayout />,
    requiredRoute: '/ai-providers',
    children: [
      { index: true, element: <AiProvidersClaudeEditPage /> },
      { path: 'models', element: <AiProvidersClaudeModelsPage /> },
    ],
  },
  { path: '/ai-providers/vertex/new', element: <AiProvidersVertexEditPage />, requiredRoute: '/ai-providers' },
  { path: '/ai-providers/vertex/:index', element: <AiProvidersVertexEditPage />, requiredRoute: '/ai-providers' },
  {
    path: '/ai-providers/openai/new',
    element: <AiProvidersOpenAIEditLayout />,
    requiredRoute: '/ai-providers',
    children: [
      { index: true, element: <AiProvidersOpenAIEditPage /> },
      { path: 'models', element: <AiProvidersOpenAIModelsPage /> },
    ],
  },
  {
    path: '/ai-providers/openai/:index',
    element: <AiProvidersOpenAIEditLayout />,
    requiredRoute: '/ai-providers',
    children: [
      { index: true, element: <AiProvidersOpenAIEditPage /> },
      { path: 'models', element: <AiProvidersOpenAIModelsPage /> },
    ],
  },
  { path: '/ai-providers/ampcode', element: <AiProvidersAmpcodeEditPage />, requiredRoute: '/ai-providers' },
  { path: '/ai-providers', element: <AiProvidersPage />, requiredRoute: '/ai-providers' },
  { path: '/ai-providers/*', element: <AiProvidersPage />, requiredRoute: '/ai-providers' },
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

const filterRoutes = (routes: RouteConfig[], allowedRoutes: string[]): RouteObject[] =>
  routes
    .filter((route) => isAllowedRoute(allowedRoutes, route.requiredRoute))
    .map(({ requiredRoute: _requiredRoute, children, ...route }) => {
      if (route.index) {
        return {
          index: true,
          element: route.element
        } satisfies RouteObject;
      }

      return {
        path: route.path,
        element: route.element,
        children: children ? filterRoutes(children, allowedRoutes) : undefined
      } satisfies RouteObject;
    });

export function MainRoutes({
  location,
  allowedRoutes
}: {
  location?: Location;
  allowedRoutes: string[];
}) {
  return useRoutes(filterRoutes(mainRoutes, allowedRoutes), location);
}
