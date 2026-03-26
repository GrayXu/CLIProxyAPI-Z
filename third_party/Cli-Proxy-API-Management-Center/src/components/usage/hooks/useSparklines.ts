import { useCallback, useMemo } from 'react';
import {
  collectUsageDetails,
  extractTotalTokens,
  type UsageTimeRange
} from '@/utils/usage';
import type { UsagePayload } from './useUsageData';

export interface SparklineData {
  labels: string[];
  datasets: [
    {
      data: number[];
      borderColor: string;
      backgroundColor: string;
      fill: boolean;
      tension: number;
      pointRadius: number;
      borderWidth: number;
    }
  ];
}

export interface SparklineBundle {
  data: SparklineData;
}

export interface UseSparklinesOptions {
  usage: UsagePayload | null;
  loading: boolean;
  nowMs: number;
  timeRange: UsageTimeRange;
}

export interface UseSparklinesReturn {
  requestsSparkline: SparklineBundle | null;
  tokensSparkline: SparklineBundle | null;
  rpmSparkline: SparklineBundle | null;
  tpmSparkline: SparklineBundle | null;
  costSparkline: SparklineBundle | null;
}

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;
const MAX_ALL_BUCKETS = 60;
const SPARKLINE_TENSION = 0;

const formatHourLabel = (value: number): string => {
  const date = new Date(value);
  const h = date.getHours().toString().padStart(2, '0');
  return `${h}:00`;
};

const formatDayLabel = (value: number): string => {
  const date = new Date(value);
  const month = (date.getMonth() + 1).toString().padStart(2, '0');
  const day = date.getDate().toString().padStart(2, '0');
  return `${month}-${day}`;
};

export function useSparklines({
  usage,
  loading,
  nowMs,
  timeRange
}: UseSparklinesOptions): UseSparklinesReturn {
  const sparklineSeries = useMemo(() => {
    if (!usage) return { labels: [], requests: [], tokens: [] };

    const details = collectUsageDetails(usage);
    if (!details.length) return { labels: [], requests: [], tokens: [] };

    const validTimestamps = details
      .map((detail) => detail.__timestampMs ?? Date.parse(detail.timestamp))
      .filter((timestamp) => Number.isFinite(timestamp)) as number[];

    if (!validTimestamps.length) {
      return { labels: [], requests: [], tokens: [] };
    }

    const isHourlyRange = timeRange === '7h' || timeRange === '24h';
    let bucketSizeMs = isHourlyRange ? HOUR_MS : DAY_MS;
    const labelFormatter = isHourlyRange ? formatHourLabel : formatDayLabel;

    let bucketCount: number;
    let bucketStart: number;

    if (timeRange === '7h') {
      bucketCount = 7;
      const bucketEnd = Math.floor(nowMs / HOUR_MS) * HOUR_MS;
      bucketStart = bucketEnd - (bucketCount - 1) * bucketSizeMs;
    } else if (timeRange === '24h') {
      bucketCount = 24;
      const bucketEnd = Math.floor(nowMs / HOUR_MS) * HOUR_MS;
      bucketStart = bucketEnd - (bucketCount - 1) * bucketSizeMs;
    } else if (timeRange === '7d') {
      bucketCount = 7;
      const bucketEnd = Math.floor(nowMs / DAY_MS) * DAY_MS;
      bucketStart = bucketEnd - (bucketCount - 1) * bucketSizeMs;
    } else if (timeRange === '30d') {
      bucketCount = 30;
      const bucketEnd = Math.floor(nowMs / DAY_MS) * DAY_MS;
      bucketStart = bucketEnd - (bucketCount - 1) * bucketSizeMs;
    } else {
      const latestTimestamp = Math.max(
        Number.isFinite(nowMs) && nowMs > 0 ? nowMs : 0,
        ...validTimestamps
      );
      const earliestTimestamp = Math.min(...validTimestamps);
      const normalizedLatest = Math.floor(latestTimestamp / DAY_MS) * DAY_MS;
      const normalizedEarliest = Math.floor(earliestTimestamp / DAY_MS) * DAY_MS;
      const totalDayBuckets = Math.max(
        1,
        Math.floor((normalizedLatest - normalizedEarliest) / DAY_MS) + 1
      );

      bucketCount = Math.min(MAX_ALL_BUCKETS, totalDayBuckets);
      bucketSizeMs = Math.ceil(totalDayBuckets / bucketCount) * DAY_MS;
      bucketStart = normalizedEarliest;
    }

    if (!Number.isFinite(bucketStart) || bucketCount <= 0 || bucketSizeMs <= 0) {
      return { labels: [], requests: [], tokens: [] };
    }

    const labels = Array.from({ length: bucketCount }, (_, index) =>
      labelFormatter(bucketStart + index * bucketSizeMs)
    );
    const requestBuckets = new Array(bucketCount).fill(0);
    const tokenBuckets = new Array(bucketCount).fill(0);
    const timestampNormalizer = isHourlyRange
      ? (timestamp: number) => Math.floor(timestamp / HOUR_MS) * HOUR_MS
      : (timestamp: number) => Math.floor(timestamp / DAY_MS) * DAY_MS;
    const bucketEndExclusive = bucketStart + bucketCount * bucketSizeMs;

    details.forEach((detail) => {
      const timestamp = detail.__timestampMs ?? Date.parse(detail.timestamp);
      if (!Number.isFinite(timestamp)) {
        return;
      }

      const normalizedTimestamp = timestampNormalizer(timestamp);
      if (normalizedTimestamp < bucketStart || normalizedTimestamp >= bucketEndExclusive) {
        return;
      }

      const bucketIndex = Math.floor((normalizedTimestamp - bucketStart) / bucketSizeMs);
      if (bucketIndex < 0 || bucketIndex >= bucketCount) {
        return;
      }

      requestBuckets[bucketIndex] += 1;
      tokenBuckets[bucketIndex] += extractTotalTokens(detail);
    });

    return { labels, requests: requestBuckets, tokens: tokenBuckets };
  }, [nowMs, timeRange, usage]);

  const buildSparkline = useCallback(
    (
      series: { labels: string[]; data: number[] },
      color: string,
      backgroundColor: string
    ): SparklineBundle | null => {
      if (loading || !series?.data?.length) {
        return null;
      }
      const sliceStart = Math.max(series.data.length - 60, 0);
      const labels = series.labels.slice(sliceStart);
      const points = series.data.slice(sliceStart);
      return {
        data: {
          labels,
          datasets: [
            {
              data: points,
              borderColor: color,
              backgroundColor,
              fill: true,
              tension: SPARKLINE_TENSION,
              pointRadius: 0,
              borderWidth: 2
            }
          ]
        }
      };
    },
    [loading]
  );

  const requestsSparkline = useMemo(
    () =>
      buildSparkline(
        { labels: sparklineSeries.labels, data: sparklineSeries.requests },
        '#8b8680',
        'rgba(139, 134, 128, 0.18)'
      ),
    [buildSparkline, sparklineSeries.labels, sparklineSeries.requests]
  );

  const tokensSparkline = useMemo(
    () =>
      buildSparkline(
        { labels: sparklineSeries.labels, data: sparklineSeries.tokens },
        '#8b5cf6',
        'rgba(139, 92, 246, 0.18)'
      ),
    [buildSparkline, sparklineSeries.labels, sparklineSeries.tokens]
  );

  const rpmSparkline = useMemo(
    () =>
      buildSparkline(
        { labels: sparklineSeries.labels, data: sparklineSeries.requests },
        '#22c55e',
        'rgba(34, 197, 94, 0.18)'
      ),
    [buildSparkline, sparklineSeries.labels, sparklineSeries.requests]
  );

  const tpmSparkline = useMemo(
    () =>
      buildSparkline(
        { labels: sparklineSeries.labels, data: sparklineSeries.tokens },
        '#f97316',
        'rgba(249, 115, 22, 0.18)'
      ),
    [buildSparkline, sparklineSeries.labels, sparklineSeries.tokens]
  );

  const costSparkline = useMemo(
    () =>
      buildSparkline(
        { labels: sparklineSeries.labels, data: sparklineSeries.tokens },
        '#f59e0b',
        'rgba(245, 158, 11, 0.18)'
      ),
    [buildSparkline, sparklineSeries.labels, sparklineSeries.tokens]
  );

  return {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    costSparkline
  };
}
