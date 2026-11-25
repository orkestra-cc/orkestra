export declare const devProxy: (
  target: string | undefined,
  storage?: { target?: string; prefix?: string }
) => Record<string, { target: string; ws?: boolean }> | undefined;
