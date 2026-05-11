const toISODate = (d: Date): string => d.toISOString().slice(0, 10);

export const ytdRange = (now: Date = new Date()): { fromDate: string; toDate: string } => ({
  fromDate: toISODate(new Date(Date.UTC(now.getUTCFullYear(), 0, 1))),
  toDate: toISODate(now),
});

export const lastYearRange = (now: Date = new Date()): { fromDate: string; toDate: string } => {
  const from = new Date(Date.UTC(now.getUTCFullYear() - 1, now.getUTCMonth(), now.getUTCDate()));
  return { fromDate: toISODate(from), toDate: toISODate(now) };
};
