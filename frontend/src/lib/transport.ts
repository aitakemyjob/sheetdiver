// Transport abstracts how the UI reaches the Go backend:
//   - desktop (Wails): window.go.main.Desktop.Invoke + runtime events
//   - browser (`sheetdiver serve`): POST /api/invoke + Server-Sent Events
import type { BusEvent } from './types';

type Listener = (ev: BusEvent) => void;

interface WailsWindow {
  go?: { main?: { Desktop?: { Invoke(method: string, args: string): Promise<string> } } };
  runtime?: { EventsOn(name: string, cb: (data: unknown) => void): () => void };
}

export interface Transport {
  readonly kind: 'desktop' | 'browser';
  /** Calls a backend method with JSON-serializable args. */
  invoke<T>(method: string, args?: unknown): Promise<T>;
  /** Listens for backend events; returns an unsubscribe function. */
  subscribe(fn: Listener): () => void;
}

export class ApiError extends Error {
  /** Creates an error for a failed backend call. */
  constructor(message: string, readonly method: string) {
    super(message);
  }
}

/** The window with Wails' injected globals. */
function wails(): WailsWindow {
  return window as unknown as WailsWindow;
}

/** Whether the Wails desktop bindings are present. */
export function desktopAvailable(): boolean {
  return !!wails().go?.main?.Desktop;
}

class DesktopTransport implements Transport {
  readonly kind = 'desktop' as const;
  /** Calls the bound Desktop.Invoke Go method. */
  async invoke<T>(method: string, args?: unknown): Promise<T> {
    try {
      const out = await wails().go!.main!.Desktop!.Invoke(method, JSON.stringify(args ?? null));
      return (out ? JSON.parse(out) : null) as T;
    } catch (e) {
      throw new ApiError(typeof e === 'string' ? e : (e as Error).message, method);
    }
  }
  /** Subscribes to the sd:event Wails runtime event. */
  subscribe(fn: Listener): () => void {
    return wails().runtime!.EventsOn('sd:event', (data) => fn(data as BusEvent));
  }
}

export class BrowserTransport implements Transport {
  readonly kind = 'browser' as const;
  private listeners = new Set<Listener>();
  private source?: EventSource;

  constructor(private base = '') {}

  /** POSTs to /api/invoke with the CSRF header. */
  async invoke<T>(method: string, args?: unknown): Promise<T> {
    const res = await fetch(`${this.base}/api/invoke`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-SheetDiver': '1' },
      body: JSON.stringify({ method, args: args ?? null }),
    });
    if (!res.ok) throw new ApiError(`${res.status} ${await res.text()}`, method);
    const body = (await res.json()) as { result?: T; error?: string };
    if (body.error) throw new ApiError(body.error, method);
    return body.result as T;
  }

  /**
   * Shares one EventSource across listeners, closing it when the last
   * listener leaves.
   */
  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    if (!this.source) {
      this.source = new EventSource(`${this.base}/api/events?sd=1`);
      this.source.onmessage = (m) => {
        try {
          const ev = JSON.parse(m.data) as BusEvent;
          this.listeners.forEach((l) => l(ev));
        } catch {
          /* ignore malformed */
        }
      };
    }
    return () => {
      this.listeners.delete(fn);
      if (this.listeners.size === 0) {
        this.source?.close();
        this.source = undefined;
      }
    };
  }
}

let current: Transport | undefined;

/** Resolves the transport, giving Wails a moment to inject its bindings. */
export async function getTransport(): Promise<Transport> {
  if (current) return current;
  const inWebview = location.protocol === 'wails:' || location.hostname === 'wails.localhost' || location.port === '34115';
  if (inWebview) {
    for (let i = 0; i < 40 && !desktopAvailable(); i++) {
      await new Promise((r) => setTimeout(r, 50));
    }
  }
  current = desktopAvailable() ? new DesktopTransport() : new BrowserTransport();
  return current;
}

/** For tests. */
export function setTransport(t: Transport | undefined) {
  current = t;
}
