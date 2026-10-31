import { useSyncExternalStore } from 'react';

/** A store the editors' controllers are. */
interface Store<T> {
  subscribe(fn: () => void): () => void;
  getSnapshot(): T;
}

/** The current snapshot of an editor's controller. */
export function useStoreOf<T>(store: Store<T>): T {
  return useSyncExternalStore(store.subscribe, store.getSnapshot);
}
