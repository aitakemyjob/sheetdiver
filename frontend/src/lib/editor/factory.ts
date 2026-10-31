import { FootprintDocument, SymbolDocument, type EditDocument } from './documents';
import type { FootprintModel, SymbolModel } from './model';

// Which document edits what: a factory from the kind being edited.

/** What can be edited in the 2D editor. */
export type EditKind = 'symbol' | 'footprint';

/** The document for a symbol or footprint. */
export function documentFor(kind: EditKind, model: SymbolModel | FootprintModel): EditDocument {
  return kind === 'symbol' ? SymbolDocument.of(model as SymbolModel) : FootprintDocument.of(model as FootprintModel);
}
