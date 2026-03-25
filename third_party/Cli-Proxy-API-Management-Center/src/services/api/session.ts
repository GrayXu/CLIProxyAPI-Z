import { apiClient } from './client';
import type { SessionResponse } from '@/types';

export const sessionApi = {
  getSession: () => apiClient.get<SessionResponse>('/session')
};
