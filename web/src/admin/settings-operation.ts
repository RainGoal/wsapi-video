type SettingsOperation<T> = {
  request: () => Promise<T>;
  onStart: () => void;
  onSuccess: (value: T) => void;
  onError: (failure: unknown) => void;
  onFinish: () => void;
};

export function createSettingsOperationScope() {
  let lifetime: object | null = null;
  let pending: object | null = null;
  const available = () => lifetime !== null && pending === null;
  return {
    available,
    mount() {
      const mounted = {};
      lifetime = mounted;
      pending = null;
      return () => {
        if (lifetime !== mounted) return;
        lifetime = null;
        pending = null;
      };
    },
    async run<T>(operation: SettingsOperation<T>) {
      if (!available()) return;
      const mounted = lifetime;
      const request = {};
      pending = request;
      const current = () => lifetime === mounted && pending === request;
      try {
        operation.onStart();
        const value = await operation.request();
        if (current()) operation.onSuccess(value);
      } catch (failure) {
        if (current()) operation.onError(failure);
      } finally {
        if (current()) {
          pending = null;
          operation.onFinish();
        }
      }
    },
  };
}
