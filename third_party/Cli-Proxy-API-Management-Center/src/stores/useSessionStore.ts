import { create } from 'zustand';
import { sessionApi } from '@/services/api/session';
import type { SessionCapabilities, SessionResponse, SessionState } from '@/types';

const normalizePath = (value: string): string => {
  const trimmed = String(value || '').trim();
  if (!trimmed) return '/';
  const withoutHash = trimmed.replace(/^#+/, '');
  const normalized = withoutHash.startsWith('/') ? withoutHash : `/${withoutHash}`;
  return normalized.length > 1 ? normalized.replace(/\/+$/, '') : normalized;
};

const matchesAllowedRoute = (pathname: string, allowedRoute: string): boolean => {
  const normalizedPath = normalizePath(pathname);
  const normalizedAllowedRoute = normalizePath(allowedRoute);
  if (normalizedAllowedRoute === '/') {
    return normalizedPath === '/' || normalizedPath === '/dashboard';
  }
  return (
    normalizedPath === normalizedAllowedRoute ||
    normalizedPath.startsWith(`${normalizedAllowedRoute}/`)
  );
};

let inFlightSessionRequest: Promise<SessionResponse> | null = null;

interface SessionStoreState extends SessionState {
  fetchSession: (force?: boolean) => Promise<SessionResponse>;
  clearSession: () => void;
  isRouteAllowed: (pathname: string) => boolean;
  hasCapability: (capability: keyof SessionCapabilities) => boolean;
}

export const useSessionStore = create<SessionStoreState>((set, get) => ({
  role: null,
  allowedRoutes: [],
  capabilities: null,
  loading: false,
  error: null,
  initialized: false,

  fetchSession: async (force = false) => {
    const state = get();
    if (!force && state.initialized && state.capabilities) {
      return {
        role: state.role ?? 'viewer',
        capabilities: {
          ...state.capabilities,
          allowed_routes: [...state.allowedRoutes]
        }
      };
    }

    if (inFlightSessionRequest) {
      return inFlightSessionRequest;
    }

    set({ loading: true, error: null });

    const requestPromise = sessionApi.getSession();
    inFlightSessionRequest = requestPromise;

    try {
      const response = await requestPromise;
      const allowedRoutes = Array.isArray(response.capabilities?.allowed_routes)
        ? response.capabilities.allowed_routes.map((route) => normalizePath(route))
        : [];

      set({
        role: response.role,
        allowedRoutes,
        capabilities: response.capabilities,
        loading: false,
        error: null,
        initialized: true
      });

      return response;
    } catch (error: unknown) {
      const message =
        error instanceof Error
          ? error.message
          : typeof error === 'string'
            ? error
            : 'Failed to fetch session';
      set({
        role: null,
        allowedRoutes: [],
        capabilities: null,
        loading: false,
        error: message,
        initialized: false
      });
      throw error;
    } finally {
      if (inFlightSessionRequest === requestPromise) {
        inFlightSessionRequest = null;
      }
    }
  },

  clearSession: () => {
    inFlightSessionRequest = null;
    set({
      role: null,
      allowedRoutes: [],
      capabilities: null,
      loading: false,
      error: null,
      initialized: false
    });
  },

  isRouteAllowed: (pathname: string) => {
    const { allowedRoutes, initialized } = get();
    if (!initialized) return false;
    return allowedRoutes.some((route) => matchesAllowedRoute(pathname, route));
  },

  hasCapability: (capability) => {
    const capabilities = get().capabilities;
    return capabilities?.[capability] === true;
  }
}));
