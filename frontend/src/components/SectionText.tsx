import { Fragment, type ReactNode } from 'react';
import { useTranslated } from './Translated';

/** A block of formatted section text. */
export type SectionBlock =
  | { kind: 'h'; text: string }
  | { kind: 'p'; text: string }
  | { kind: 'ul'; items: string[] }
  | { kind: 'table'; header: string[]; rows: string[][] }
  | { kind: 'ref'; text: string };

/** Splits a markdown table row into its cells. */
function tableCells(line: string): string[] {
  return line
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split(/(?<!\\)\|/)
    .map((c) => c.trim().replace(/\\\|/g, '|'));
}

/**
 * Parses section text as the extractor formats it: paragraphs separated
 * by blank lines, "### " sub-headings, "• " list items, markdown tables
 * and "[Figure: …]" / "[Table …]" references.
 */
export function sectionBlocks(text: string): SectionBlock[] {
  const out: SectionBlock[] = [];
  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line) continue;
    const last = out[out.length - 1];
    if (line.startsWith('|')) {
      if (/^\|(\s*-+\s*\|)+$/.test(line)) continue;
      if (last?.kind === 'table') last.rows.push(tableCells(line));
      else out.push({ kind: 'table', header: tableCells(line), rows: [] });
    } else if (/^\[(Figure|Table)\b.*\]$/.test(line)) out.push({ kind: 'ref', text: line.slice(1, -1) });
    else if (line.startsWith('### ')) out.push({ kind: 'h', text: line.slice(4) });
    else if (line.startsWith('• ')) {
      if (last?.kind === 'ul') last.items.push(line.slice(2));
      else out.push({ kind: 'ul', items: [line.slice(2)] });
    } else out.push({ kind: 'p', text: line });
  }
  return out;
}

/** Shows a leading short label ("Charge termination:") in bold. */
function labelled(text: string): ReactNode {
  const m = /^([^:：]{1,40}[:：])(\s*\S[\s\S]*)$/.exec(text);
  return m ? (
    <>
      <b>{m[1]}</b>
      {m[2]}
    </>
  ) : (
    text
  );
}

/** Renders a section's text with its sub-headings, paragraphs and lists, translated while translation is on. */
export function SectionText({ text }: { text: string }) {
  const shown = useTranslated(text);
  return (
    <div className="sec-text sec-doc" title={shown !== text ? 'Translated on this computer' : undefined}>
      {sectionBlocks(shown).map((b, i) => (
        <Fragment key={i}>
          {b.kind === 'h' && <h4>{b.text}</h4>}
          {b.kind === 'p' && <p>{labelled(b.text)}</p>}
          {b.kind === 'ref' && <div className="sec-ref">{b.text}</div>}
          {b.kind === 'table' && (
            <div className="sec-table">
              <table>
                <thead>
                  <tr>
                    {b.header.map((h, j) => (
                      <th key={j}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {b.rows.map((row, j) => (
                    <tr key={j}>
                      {row.map((c, k) => (
                        <td key={k}>{c}</td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {b.kind === 'ul' && (
            <ul>
              {b.items.map((it, j) => (
                <li key={j}>{labelled(it)}</li>
              ))}
            </ul>
          )}
        </Fragment>
      ))}
    </div>
  );
}
