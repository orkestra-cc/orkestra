import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { FetchArgs } from '@reduxjs/toolkit/query';
import {
  asStructuredError,
  fileDownload,
  type CsvBaseQuery
} from './fileDownload';
import { saveBlob } from './saveBlob';

vi.mock('./saveBlob', () => ({ saveBlob: vi.fn() }));

describe('fileDownload', () => {
  beforeEach(() => vi.mocked(saveBlob).mockReset());

  it('goes through the injected baseQuery with a Blob response handler', async () => {
    const blob = new Blob(['a,b\n'], { type: 'text/csv' });
    const baseQuery = vi.fn<CsvBaseQuery>().mockResolvedValue({ data: blob });

    const result = await fileDownload(
      baseQuery,
      { url: '/v1/things/export', method: 'POST', body: { ids: ['1'] } },
      'things.csv'
    );

    expect(result).toEqual({ data: { success: true } });
    const arg = baseQuery.mock.calls[0][0] as FetchArgs;
    expect(arg).toMatchObject({
      url: '/v1/things/export',
      method: 'POST',
      body: { ids: ['1'] }
    });
    expect(typeof arg.responseHandler).toBe('function');
    expect(saveBlob).toHaveBeenCalledWith(blob, 'things.csv');
  });

  it('defaults to GET', async () => {
    const baseQuery = vi
      .fn<CsvBaseQuery>()
      .mockResolvedValue({ data: new Blob([]) });
    await fileDownload(baseQuery, { url: '/v1/things/1/pdf' }, 'thing.pdf');
    expect((baseQuery.mock.calls[0][0] as FetchArgs).method).toBe('GET');
  });

  it('returns the structured error body and saves nothing on failure', async () => {
    const body = {
      status: 403,
      title: 'Forbidden',
      code: 'things.export_denied'
    };
    const baseQuery = vi.fn<CsvBaseQuery>().mockResolvedValue({
      error: {
        status: 403,
        data: new Blob([JSON.stringify(body)], { type: 'application/json' })
      }
    });

    const result = await fileDownload(
      baseQuery,
      { url: '/v1/things/export' },
      'x.csv'
    );

    expect(result).toEqual({ error: { status: 403, data: body } });
    expect(saveBlob).not.toHaveBeenCalled();
  });
});

describe('asStructuredError', () => {
  it('leaves a non-JSON blob untouched', async () => {
    const data = new Blob(['oops'], { type: 'text/plain' });
    const error = { status: 500, data } as const;
    expect(await asStructuredError(error)).toBe(error);
  });

  it('leaves a blob that claims JSON but is not parseable untouched', async () => {
    const data = new Blob(['not json'], { type: 'application/json' });
    const error = { status: 500, data } as const;
    expect(await asStructuredError(error)).toBe(error);
  });
});
