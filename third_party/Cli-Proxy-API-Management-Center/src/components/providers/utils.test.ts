import { describe, expect, it } from 'vitest';
import type { OpenAIProviderConfig } from '@/types';
import type { UsageDetail } from '@/utils/usage';
import { indexUsageDetailsByAuthIndex, indexUsageDetailsBySource } from '@/utils/usageIndex';
import {
  collectOpenAIProviderUsageDetails,
  collectUsageDetailsForIdentity,
  getOpenAIProviderStats,
  getStatsForIdentity,
} from './utils';

const createDetail = (overrides: Partial<UsageDetail>): UsageDetail => ({
  timestamp: '2026-04-25T12:00:00.000Z',
  source: '',
  auth_index: null,
  service_tier: 'default',
  requested_fast_mode: false,
  tokens: {
    input_tokens: 0,
    output_tokens: 0,
    reasoning_tokens: 0,
    cached_tokens: 0,
    total_tokens: 0,
  },
  failed: false,
  ...overrides,
});

const indexUsageDetails = (usageDetails: UsageDetail[]) => ({
  usageDetailsBySource: indexUsageDetailsBySource(usageDetails),
  usageDetailsByAuthIndex: indexUsageDetailsByAuthIndex(usageDetails),
});

describe('provider usage aggregation helpers', () => {
  it('includes both auth-index and legacy source records for the same identity', () => {
    const legacyDetail = createDetail({
      source: 't:gemini-legacy',
      failed: false,
    });
    const freshDetail = createDetail({
      source: 'k:fresh',
      auth_index: 'gemini-auth-1',
      failed: true,
    });

    const { usageDetailsBySource, usageDetailsByAuthIndex } = indexUsageDetails([
      legacyDetail,
      freshDetail,
    ]);

    const usageDetails = collectUsageDetailsForIdentity(
      { authIndex: 'gemini-auth-1', prefix: 'gemini-legacy' },
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );
    const stats = getStatsForIdentity(
      { authIndex: 'gemini-auth-1', prefix: 'gemini-legacy' },
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );

    expect(usageDetails).toHaveLength(2);
    expect(usageDetails).toContain(legacyDetail);
    expect(usageDetails).toContain(freshDetail);
    expect(stats).toEqual({ success: 1, failure: 1 });
  });

  it('dedupes overlapping openai provider records by object reference', () => {
    const overlapDetail = createDetail({
      source: 't:openai-prefix',
      auth_index: 'openai-entry-1',
      failed: false,
    });
    const provider: OpenAIProviderConfig = {
      name: 'OpenAI',
      baseUrl: 'https://example.com',
      prefix: 'openai-prefix',
      apiKeyEntries: [{ apiKey: 'sk-entry-1', authIndex: 'openai-entry-1' }],
    };

    const { usageDetailsBySource, usageDetailsByAuthIndex } = indexUsageDetails([overlapDetail]);

    const usageDetails = collectOpenAIProviderUsageDetails(
      provider,
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );
    const stats = getOpenAIProviderStats(provider, usageDetailsBySource, usageDetailsByAuthIndex);

    expect(usageDetails).toEqual([overlapDetail]);
    expect(stats).toEqual({ success: 1, failure: 0 });
  });

  it('keeps openai entry stats isolated while provider stats use the deduped union', () => {
    const sharedDetail = createDetail({
      source: 't:openai-prefix',
      auth_index: 'openai-entry-1',
      failed: false,
    });
    const entryOneFailure = createDetail({
      source: 'k:entry-one',
      auth_index: 'openai-entry-1',
      failed: true,
    });
    const entryTwoSuccess = createDetail({
      source: 'k:entry-two',
      auth_index: 'openai-entry-2',
      failed: false,
    });
    const prefixOnlyLegacy = createDetail({
      source: 't:openai-prefix',
      failed: true,
    });
    const provider: OpenAIProviderConfig = {
      name: 'OpenAI',
      baseUrl: 'https://example.com',
      prefix: 'openai-prefix',
      apiKeyEntries: [
        { apiKey: 'sk-entry-1', authIndex: 'openai-entry-1' },
        { apiKey: 'sk-entry-2', authIndex: 'openai-entry-2' },
      ],
    };

    const { usageDetailsBySource, usageDetailsByAuthIndex } = indexUsageDetails([
      sharedDetail,
      entryOneFailure,
      entryTwoSuccess,
      prefixOnlyLegacy,
    ]);

    const entryOneStats = getStatsForIdentity(
      { authIndex: 'openai-entry-1', apiKey: 'sk-entry-1' },
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );
    const entryTwoStats = getStatsForIdentity(
      { authIndex: 'openai-entry-2', apiKey: 'sk-entry-2' },
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );
    const providerUsageDetails = collectOpenAIProviderUsageDetails(
      provider,
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );
    const providerStats = getOpenAIProviderStats(
      provider,
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );

    expect(entryOneStats).toEqual({ success: 1, failure: 1 });
    expect(entryTwoStats).toEqual({ success: 1, failure: 0 });
    expect(providerUsageDetails).toHaveLength(4);
    expect(providerStats).toEqual({ success: 2, failure: 2 });
  });

  it('keeps prefix-only legacy openai traffic visible when api key entries exist', () => {
    const prefixOnlyLegacy = createDetail({
      source: 't:openai-prefix',
      failed: false,
    });
    const provider: OpenAIProviderConfig = {
      name: 'OpenAI',
      baseUrl: 'https://example.com',
      prefix: 'openai-prefix',
      apiKeyEntries: [{ apiKey: 'sk-entry-1', authIndex: 'openai-entry-1' }],
    };

    const { usageDetailsBySource, usageDetailsByAuthIndex } = indexUsageDetails([prefixOnlyLegacy]);

    const usageDetails = collectOpenAIProviderUsageDetails(
      provider,
      usageDetailsBySource,
      usageDetailsByAuthIndex
    );
    const stats = getOpenAIProviderStats(provider, usageDetailsBySource, usageDetailsByAuthIndex);

    expect(usageDetails).toEqual([prefixOnlyLegacy]);
    expect(stats).toEqual({ success: 1, failure: 0 });
  });
});
