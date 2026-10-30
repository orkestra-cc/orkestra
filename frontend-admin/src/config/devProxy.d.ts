export declare const devProxy: (
  target: string | undefined
) => Record<string, { target: string; ws?: boolean }> | undefined;
