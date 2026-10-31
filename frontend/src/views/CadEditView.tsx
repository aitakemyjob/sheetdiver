import { DrawingEditor } from '../components/editor/DrawingEditor';
import { Model3DEditor } from '../components/editor/Model3DEditor';
import { ErrorBox, Spinner, useAsync } from '../components/ui';
import { api } from '../lib/api';
import { problemsLine } from '../lib/cad';
import { useStore } from '../lib/store';

/**
 * A tab editing one of a part's symbol, footprint or 3D model in
 * SheetDiver's own editors.
 */
export function CadEditView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const part = String(params.part ?? '');
  const pkg = String(params.package ?? '');
  const what = String(params.what ?? 'footprint') as 'symbol' | 'footprint' | '3d';
  const m = useAsync(() => api.cadModel(part, pkg), [part, pkg]);
  const cad = useAsync(() => api.partCad(part), [part, s.libraryVersion]);
  if (m.loading && !m.data) return <Spinner label="Opening…" />;
  if (m.error) return <ErrorBox error={m.error} />;
  if (!m.data) return null;
  const onError = (e: Error) => s.toast({ kind: 'err', title: 'Could not save', body: e.message });
  const saved = (msg: string) => s.toast({ kind: 'ok', title: `${part} ${what === '3d' ? '3D model' : what} saved`, body: msg });
  if (what === '3d') {
    const fp = cad.data?.packages.find((p) => p.package === m.data!.package)?.footprint;
    if (!m.data.shapes?.length) return <div className="pad muted">This package has no 3D model to edit yet: it comes with its footprint's package outline.</div>;
    return <Model3DEditor part={part} pkg={m.data.package} shapes={m.data.shapes} edited={!!fp?.model3dEdited} onSaved={() => saved('Written to the library.')} onError={onError} />;
  }
  const model = what === 'symbol' ? m.data.symbol : m.data.footprint;
  if (!model) return <div className="pad muted">There's no {what} to edit yet for this package.</div>;
  return <DrawingEditor part={part} pkg={m.data.package} kind={what} model={model} onSaved={(ps) => saved(ps.length ? `Saved, with warnings: ${problemsLine(ps)}` : 'Written to the library.')} onError={onError} />;
}
