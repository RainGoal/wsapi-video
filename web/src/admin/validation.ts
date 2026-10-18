export function decimalFen(value: string): number | null {
  if (!/^\d+(\.\d{1,2})?$/.test(value.trim())) return null;
  const [whole, fraction = ""] = value.trim().split(".");
  const fen = Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
  return Number.isSafeInteger(fen) && fen > 0 && fen <= 100000000 ? fen : null;
}

export function settingsPayload(
  draft: Record<string, Record<string, unknown>>,
  secrets: Record<string, string | undefined>,
  cleared: string[],
) {
  const settings = structuredClone(draft);
  for (const group of Object.values(settings))
    for (const key of Object.keys(group))
      if (key.endsWith("_configured") || key === "location_locked")
        delete group[key];
  if (Array.isArray(settings.media?.output_origins))
    settings.media.output_origins = settings.media.output_origins
      .map((value) => String(value).trim())
      .filter(Boolean);
  for (const [name, value] of Object.entries(secrets)) {
    if (!value || cleared.includes(name)) continue;
    const [group, key] = name.split(".");
    if (settings[group]) settings[group][key] = value;
  }
  return { settings, clear_secrets: [...new Set(cleared)] };
}
