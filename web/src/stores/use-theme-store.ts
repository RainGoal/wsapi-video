// The platform has one approved studio theme; upstream's canvas consumes this selector.
export function useThemeStore<T>(selector: (state: { theme: "dark" }) => T): T {
  return selector({ theme: "dark" });
}
