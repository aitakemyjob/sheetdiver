import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { needsTranslation, translateTexts } from '../lib/translate';

/** What a translated view shares: its language, whether translation is on, and the translations so far. */
interface TranslateState {
  lang: string;
  on: boolean;
  map: Map<string, string>;
  request: (texts: string[]) => void;
  error?: string;
}

const TranslateContext = createContext<TranslateState>({
  lang: '',
  on: false,
  map: new Map(),
  request: () => {},
});

/**
 * Translates the texts rendered with <T> below it into English while on
 * is set: requests are batched, and each text is translated once (the
 * glossary, then the library's saved translations, then the engine).
 */
export function TranslateProvider({ lang, on, onError, children }: { lang: string; on: boolean; onError?: (e: Error) => void; children: ReactNode }) {
  const [map, setMap] = useState(() => new Map<string, string>());
  const pending = useRef(new Set<string>());
  const asked = useRef(new Set<string>());
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const flush = useCallback(() => {
    const texts = [...pending.current];
    pending.current.clear();
    if (!texts.length) return;
    translateTexts(texts, lang).then(
      (got) => setMap((m) => new Map([...m, ...got])),
      (e: Error) => {
        texts.forEach((t) => asked.current.delete(t));
        onError?.(e);
      },
    );
  }, [lang, onError]);
  const request = useCallback(
    (texts: string[]) => {
      for (const t of texts) {
        const k = t.trim();
        if (!k || asked.current.has(k) || !needsTranslation(k, lang)) continue;
        asked.current.add(k);
        pending.current.add(k);
      }
      clearTimeout(timer.current);
      timer.current = setTimeout(flush, 30);
    },
    [flush, lang],
  );
  useEffect(() => () => clearTimeout(timer.current), []);
  const value = useMemo(() => ({ lang, on, map, request }), [lang, on, map, request]);
  return <TranslateContext.Provider value={value}>{children}</TranslateContext.Provider>;
}

/** Returns a text's English while translation is on (the original until it arrives). */
export function useTranslated(text: string | undefined): string {
  const { on, map, request } = useContext(TranslateContext);
  useEffect(() => {
    if (on && text) request(text.split('\n').flatMap((l) => linePieces(l).texts));
  }, [on, text, request]);
  if (!on || !text) return text ?? '';
  // Section text is translated piece by piece: paragraphs, headings and
  // list items without their markers, table cells, figure captions.
  return text
    .split('\n')
    .map((l) => {
      const { texts, build } = linePieces(l);
      return build(texts.map((t) => map.get(t.trim()) ?? t));
    })
    .join('\n');
}

/** Splits a section-text line into its marker ("### ", "• ") and text. */
export function splitMarker(line: string): [string, string] {
  const m = /^(### |• )?(.*)$/s.exec(line)!;
  return [m[1] ?? '', m[2]];
}

/**
 * The pieces of a section-text line to translate, and how to put the
 * translations back: a table row's cells, a reference's caption, or the
 * line without its marker.
 */
export function linePieces(line: string): { texts: string[]; build: (tr: string[]) => string } {
  if (line.startsWith('|') && line.endsWith('|')) {
    const cells = line.slice(1, -1).split(/(?<!\\)\|/);
    return { texts: cells, build: (tr) => '|' + tr.map((c, i) => (c === cells[i] ? c : ` ${c.trim()} `)).join('|') + '|' };
  }
  const ref = /^\[((?:Figure|Table)[^:]*: )(.*)\]$/.exec(line);
  if (ref) return { texts: [ref[2]], build: (tr) => `[${ref[1]}${tr[0]}]` };
  const [marker, rest] = splitMarker(line);
  return { texts: [rest], build: (tr) => marker + tr[0] };
}

/** Renders a text in English while translation is on; the original shows on hover. */
export function T({ text }: { text: string | undefined }) {
  const out = useTranslated(text);
  return out !== (text ?? '') ? <span title={text}>{out}</span> : <>{text}</>;
}
