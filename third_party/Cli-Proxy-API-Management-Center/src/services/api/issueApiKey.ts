import axios from 'axios';
import type { IssueApiKeyRequest, IssueApiKeyResponse } from '@/types';
import { REQUEST_TIMEOUT_MS } from '@/utils/constants';
import { computeApiUrl } from '@/utils/connection';

export const issueApiKeyApi = {
  async issue(apiBase: string, payload: IssueApiKeyRequest): Promise<IssueApiKeyResponse> {
    const url = `${computeApiUrl(apiBase)}/public/issue-api-key`;
    const response = await axios.post<IssueApiKeyResponse>(url, payload, {
      timeout: REQUEST_TIMEOUT_MS,
      headers: {
        'Content-Type': 'application/json'
      }
    });
    return response.data;
  }
};
