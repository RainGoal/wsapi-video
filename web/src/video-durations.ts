const validDuration = (value: number) =>
  Number.isSafeInteger(value) && value >= 1 && value <= 600;

export function parseVideoDurations(text: string): number[] | null {
  if (!text.trim()) return [];
  const durations = new Set<number>();
  for (const token of text.split(/[,，\n]/)) {
    const part = token.trim();
    const match = /^(\d+)(?:\s*[-–~]\s*(\d+))?$/.exec(part);
    if (!match) return null;
    const first = Number(match[1]);
    const last = match[2] === undefined ? first : Number(match[2]);
    if (!validDuration(first) || !validDuration(last) || first > last)
      return null;
    for (let value = first; value <= last; value++) {
      durations.add(value);
      if (durations.size > 64) return null;
    }
  }
  return [...durations];
}

export function videoDurationRange(
  durations: readonly number[],
): { min: number; max: number } | null {
  if (!durations.length || durations.some((value) => !validDuration(value)))
    return null;
  const sorted = [...new Set(durations)].sort((a, b) => a - b);
  if (
    sorted.length < 2 ||
    sorted.some((value, index) => index > 0 && value !== sorted[index - 1] + 1)
  )
    return null;
  return { min: sorted[0], max: sorted[sorted.length - 1] };
}
