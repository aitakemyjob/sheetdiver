// A fake backend for view tests: it answers API calls with responses the
// Go backend really sent (fixtures.json, recorded by
// internal/api/fixtures_test.go), so views are tested against real data
// shapes. Tests can override methods and check what was called.
import { setTransport, type Transport } from '../lib/transport';
import { useStore } from '../lib/store';
import type { AppState, BusEvent } from '../lib/types';
import fixtures from './fixtures.json';

type Fixture = { method: string; args: unknown; result: unknown };
type Handler = (args: unknown) => unknown;

/** Deep equality for JSON values. */
function same(a: unknown, b: unknown): boolean {
  return JSON.stringify(a ?? null) === JSON.stringify(b ?? null);
}

/** A recorded response for a call: the same arguments, else any call of the method. */
export function recorded(method: string, args?: unknown): unknown {
  const all = (fixtures as Fixture[]).filter((f) => f.method === method);
  return (all.find((f) => same(f.args, args)) ?? all[0])?.result;
}

/** The fake backend. */
export class FakeBackend implements Transport {
  readonly kind = 'browser' as const;
  /** Every call made, in order. */
  calls: { method: string; args: unknown }[] = [];
  private overrides = new Map<string, Handler>();
  private listeners = new Set<(ev: BusEvent) => void>();

  /** Answers a method with a handler instead of the recording. */
  on(method: string, h: Handler) {
    this.overrides.set(method, h);
    return this;
  }

  /** Calls made to a method. */
  callsTo(method: string) {
    return this.calls.filter((c) => c.method === method).map((c) => c.args);
  }

  async invoke<T>(method: string, args?: unknown): Promise<T> {
    this.calls.push({ method, args });
    const h = this.overrides.get(method);
    if (h) return (await h(args)) as T;
    const r = recorded(method, args);
    // Writes and unrecorded calls succeed with nothing.
    return (r === undefined ? null : structuredClone(r)) as T;
  }

  subscribe(fn: (ev: BusEvent) => void) {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  /** Sends a backend event to the UI. */
  emit(ev: BusEvent) {
    this.listeners.forEach((l) => l(ev));
  }
}

/** Installs a fresh fake backend and a store primed with the recorded app state. */
export function fakeBackend(): FakeBackend {
  const b = new FakeBackend();
  setTransport(b);
  useStore.setState({
    app: recorded('app.state') as AppState,
    ready: true,
    fatal: undefined,
    tabs: [],
    toasts: [],
    jobs: {},
    catalog: recorded('catalog.status') as never,
    translation: recorded('translate.status') as never,
  });
  return b;
}
