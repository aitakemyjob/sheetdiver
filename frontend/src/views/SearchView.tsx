import { useEffect, useRef, useState } from 'react';
import { Search } from 'lucide-react';
import { api } from '../lib/api';
import { useStore } from '../lib/store';
import type { Hit } from '../lib/types';
import { ErrorBox, useAsync } from '../components/ui';

const kinds = ['part', 'figure', 'table', 'parameter', 'section', 'pin', 'register', 'recommendation', 'note', 'page'];

/** Escapes HTML and turns «» snippet markers into <mark>. */
export function snippetHTML(s: string): string {
  const esc = s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  return esc.replace(/«/g, '<mark>').replace(/»/g, '</mark>');
}

/** Opens whatever a search hit points at. */
export async function openHit(h: Hit) {
  const s = useStore.getState();
  const part = h.parts?.[0] ?? '';
  switch (h.kind) {
    case 'part':
      s.open({ kind: 'part', title: h.title, params: { part: h.title } });
      return;
    case 'figure':
      s.open({ kind: 'figure', title: h.title.slice(0, 24), params: { id: Number(h.ref) } });
      return;
    case 'parameter': {
      if (!h.datasheetId) return;
      const ps = await api.parameters(h.datasheetId, {});
      const p = ps.find((x) => x.id === Number(h.ref));
      s.open({ kind: 'page', title: `${part} p${h.page + 1}`, params: { ds: h.datasheetId, page: h.page, box: p?.box ?? undefined, part, nonce: Date.now() } });
      return;
    }
    case 'table':
      s.open({ kind: 'part', title: part, params: { part, tab: 'tables', nonce: Date.now() } });
      return;
    case 'pin':
      s.open({ kind: 'part', title: part, params: { part, tab: 'pins' } });
      return;
    case 'register':
      // The register map is the agent's to read: a hit opens its page.
      if (h.datasheetId) s.open({ kind: 'page', title: `${part} p${h.page + 1}`, params: { ds: h.datasheetId, page: h.page, part, nonce: Date.now() } });
      else s.open({ kind: 'part', title: part, params: { part } });
      return;
    case 'recommendation':
      s.open({ kind: 'part', title: part, params: { part, tab: 'overview' } });
      return;
    case 'note':
      if (part) s.open({ kind: 'part', title: part, params: { part, tab: 'notes' } });
      return;
    default:
      if (h.datasheetId) s.open({ kind: 'page', title: `${part} p${h.page + 1}`, params: { ds: h.datasheetId, page: h.page, part, nonce: Date.now() } });
  }
}

/** Full-text search across the library. */
export function SearchView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const [q, setQ] = useState(String(params.query ?? ''));
  const [kind, setKind] = useState('');
  const [debounced, setDebounced] = useState(q);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (params.query) setQ(String(params.query));
  }, [params.query]);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q), 180);
    return () => clearTimeout(t);
  }, [q]);
  useEffect(() => input.current?.focus(), []);
  const hits = useAsync(() => (debounced.trim() ? api.search(debounced, { kinds: kind ? [kind] : undefined, limit: 80 }) : Promise.resolve([])), [debounced, kind, s.libraryVersion]);
  return (
    <div className="view">
      <div className="toolbar">
        <Search size={16} className="faint" />
        <input ref={input} className="input grow" style={{ maxWidth: 640 }} placeholder="efficiency, VFB, gate charge, TPS563200, I2C address…" value={q} onChange={(e) => setQ(e.target.value)} />
        {['', ...kinds].map((k) => (
          <button key={k} className={`chip ${kind === k ? 'on' : ''}`} onClick={() => setKind(k)}>
            {k || 'all'}
          </button>
        ))}
      </div>
      <div className="scroll pad col" style={{ flex: 1 }}>
        <ErrorBox error={hits.error} />
        {debounced && hits.data?.length === 0 && <div className="muted">No results for “{debounced}”.</div>}
        {hits.data?.map((h) => (
          <div key={`${h.kind}${h.ref}${h.datasheetId}`} className="search-hit" onClick={() => openHit(h)}>
            <div className="row">
              <span className="badge">{h.kind}</span>
              <b>{h.title}</b>
              <span className="faint">
                {h.parts?.join(', ')}
                {h.kind !== 'part' && h.kind !== 'note' ? ` · p${h.page + 1}` : ''}
              </span>
            </div>
            <div className="snip" dangerouslySetInnerHTML={{ __html: snippetHTML(h.snippet) }} />
          </div>
        ))}
      </div>
    </div>
  );
}
