export function assetCache<T>(sources: Record<string, () => Promise<T>>) {
  const values = new Map<string, T>();
  const pending = new Map<string, Promise<T>>();
  return {
    has: (id: string) => Object.hasOwn(sources, id),
    get: (id: string) => values.get(id),
    load(id: string): Promise<T | undefined> {
      if (values.has(id)) return Promise.resolve(values.get(id));
      const existing = pending.get(id);
      if (existing) return existing;
      if (!Object.hasOwn(sources, id)) return Promise.resolve(undefined);
      const next = Promise.resolve().then(sources[id]).then((value) => {
        values.set(id, value);
        pending.delete(id);
        return value;
      }, (error) => { pending.delete(id); throw error; });
      pending.set(id, next);
      return next;
    },
  };
}
