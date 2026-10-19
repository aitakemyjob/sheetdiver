import { openPDF } from './api';
import { setTransport, type Transport } from './transport';

/** A transport that records calls. */
function fakeTransport(calls: [string, unknown][]): Transport {
  return {
    kind: 'desktop',
    async invoke<T>(method: string, args?: unknown): Promise<T> {
      calls.push([method, args]);
      return { path: '/lib/datasheets/x.pdf' } as T;
    },
    subscribe: () => () => {},
  };
}

describe('openPDF', () => {
  afterEach(() => setTransport(undefined));

  it('asks the desktop app to open the PDF in the system viewer', async () => {
    const calls: [string, unknown][] = [];
    setTransport(fakeTransport(calls));
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    await openPDF(7, true);
    expect(calls).toEqual([['datasheet.open', { id: 7 }]]);
    expect(open).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it('opens a new tab in the browser', async () => {
    const calls: [string, unknown][] = [];
    setTransport(fakeTransport(calls));
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    await openPDF(7, false);
    expect(open).toHaveBeenCalledWith('/api/datasheet.pdf?sd=1&ds=7', '_blank', 'noopener');
    expect(calls).toEqual([]);
    open.mockRestore();
  });
});

describe('shop links', () => {
  afterEach(() => setTransport(undefined));

  it('builds LCSC and JLCPCB part pages from the LCSC code only', async () => {
    const { shopURL } = await import('./api');
    expect(shopURL('C25744', 'lcsc')).toBe('https://www.lcsc.com/product-detail/C25744.html');
    expect(shopURL(' c25744 ', 'jlcpcb')).toBe('https://jlcpcb.com/partdetail/C25744');
    expect(() => shopURL('https://evil.example', 'lcsc')).toThrow();
  });

  it('asks the desktop app to open the system browser', async () => {
    const { openShop } = await import('./api');
    const calls: [string, unknown][] = [];
    setTransport(fakeTransport(calls));
    await openShop('C25744', 'lcsc', true);
    expect(calls).toEqual([['catalog.openShop', { lcsc: 'C25744', shop: 'lcsc' }]]);
  });

  it('opens a new tab in the browser', async () => {
    const { openShop } = await import('./api');
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    await openShop('C25744', 'jlcpcb', false);
    expect(open).toHaveBeenCalledWith('https://jlcpcb.com/partdetail/C25744', '_blank', 'noopener');
    open.mockRestore();
  });
});
