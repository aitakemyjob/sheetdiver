import { useEffect, useMemo, useState } from 'react';
import { Calculator, CloudDownload, ExternalLink, FileText, FolderPlus, Languages, RefreshCw, Trash2 } from 'lucide-react';
import { ReextractDialog } from '../components/ReextractDialog';
import { SectionText } from '../components/SectionText';
import { T, TranslateProvider } from '../components/Translated';
import { needsTranslation } from '../lib/translate';
import { api, figureImageURL, openPDF, tableImageURL } from '../lib/api';
import { categories, figureKinds, headline, plural, relTime, sectionLabels, sig3 } from '../lib/format';
import { useStore } from '../lib/store';
import type { Figure, Parameter, PartDetail, Section } from '../lib/types';
import { Menu } from '../components/Menu';
import { discouraged, FavoriteStar, StatusBadge, StatusPicker } from '../components/PartStatus';
import { CategoryBadge, Empty, ErrorBox, Markdown, SourceBadge, Spinner, useAsync } from '../components/ui';
import { NotesPanel } from './NotesPanel';
import { Alternatives } from './Alternatives';
import { PartCadPanel } from './PartCad';
import { PartPackagesPanel } from './PartPackages';
import { UpdateDialog } from '../components/UpdateDialog';
import { DeleteDialog } from '../components/DeleteDialog';

type Sub = 'overview' | 'parameters' | 'figures' | 'pins' | 'package' | 'cad' | 'sections' | 'registers' | 'tables' | 'alternatives' | 'notes' | 'variants';

/** Calculator suggested for a category. */
export function calcFor(category: string): string | undefined {
  if (category === 'buck' || category === 'power') return 'buck';
  if (category === 'boost') return 'boost';
  if (category === 'ldo') return 'ldo';
  if (category.startsWith('mosfet')) return 'gate_drive';
  return undefined;
}

/** A part: header with actions and sub-tabs for everything extracted. */
export function PartView({ params }: { params: Record<string, unknown> }) {
  const s = useStore();
  const ref = String(params.part ?? '');
  const [sub, setSub] = useState<Sub>((params.tab as Sub) || 'overview');
  const [updating, setUpdating] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [translating, setTranslating] = useState(false);
  const [reextracting, setReextracting] = useState(false);
  useEffect(() => {
    if (params.tab) setSub(params.tab as Sub);
  }, [params.tab, params.nonce]);
  const detail = useAsync(() => api.part(ref), [ref, s.libraryVersion]);
  const d = detail.data;
  // Datasheets extracted from their translation open translated.
  const translatedDs = d?.datasheet?.meta?.translated ? d.datasheet.id : 0;
  useEffect(() => {
    if (translatedDs) setTranslating(true);
  }, [translatedDs]);
  useEffect(() => {
    if (d) api.uiContext({ view: `part/${sub}`, part: d.part.mpn, datasheetId: d.datasheet?.id }).catch(() => {});
  }, [d, sub]);
  if (detail.loading && !d) return <Spinner label={`Loading ${ref}…`} />;
  if (detail.error) return <ErrorBox error={detail.error} />;
  if (!d) return null;
  const ds = d.datasheet;
  const c = d.counts ?? {};
  const tabs: [Sub, string, number?][] = [
    ['overview', 'Overview'],
    ['parameters', 'Parameters', c.parameters],
    ['figures', 'Figures', c.figures],
    ['pins', 'Pins', c.pins],
    ['package', 'Package'],
    ['cad', 'CAD'],
    ['sections', 'Sections', c.sections],
    ['registers', 'Registers', c.registers],
    ['tables', 'Tables', c.tables],
    ['alternatives', 'Alternatives'],
    ['notes', 'Notes', d.notes?.length],
    ['variants', 'Variants', d.variants?.length],
  ];
  const calc = calcFor(d.part.category);
  // Datasheets imported before languages were recorded: guess from the text.
  const lang = ds?.meta?.language ?? (needsTranslation(`${ds?.title ?? ''} ${d.part.description}`, 'zh') ? 'zh' : 'en');
  const model = s.translation?.models.find((m) => m.from === lang && m.to === 'en');
  return (
    <TranslateProvider lang={lang} on={translating} onError={(e) => (setTranslating(false), s.toast({ kind: 'err', title: 'Could not translate', body: e.message }))}>
    <div className="view">
      <div className="part-head">
        <h1>
          <FavoriteStar part={d.part} size={17} />
          {d.part.mpn}
          {d.part.category && (
            <button className="btn ghost sm" style={{ padding: 0, height: 'auto' }} title={`Compare all ${categories[d.part.category] ?? d.part.category} parts`} onClick={() => s.open({ kind: 'category', title: categories[d.part.category] ?? d.part.category, params: { category: d.part.category, highlight: d.part.mpn } })}>
              <CategoryBadge category={d.part.category} label={categories[d.part.category]} />
            </button>
          )}
          {d.via !== 'mpn' && d.via !== 'id' && d.matched && <span className="badge violet">matched {d.matched} by {d.via}</span>}
          {discouraged(d.part.status) && <StatusBadge status={d.part.status} />}
        </h1>
        <div className="sub">
          {d.part.manufacturer && <b>{d.part.manufacturer} · </b>}
          <T text={d.part.description || ds?.title} />
        </div>
        <div className="row" style={{ flexWrap: 'wrap' }}>
          {d.siblings.length > 0 && (
            <>
              <span className="faint">Same datasheet:</span>
              {d.siblings.map((m) => (
                <button key={m} className="chip" onClick={() => s.open({ kind: 'part', title: m, params: { part: m } })}>
                  {m}
                </button>
              ))}
            </>
          )}
          {ds?.meta?.revision && (
            <button className="badge" style={{ border: 0, cursor: 'pointer' }} title={`Datasheet revision${ds.meta.previousRevision ? ` (updated from ${ds.meta.previousRevision})` : ''}. Click to check for a newer one.`} onClick={() => setUpdating(true)}>
              {ds.meta.revision}
            </button>
          )}
          {ds?.families?.map((f) => (
            <span key={f} className="badge" title="Family pattern: x stands for any character">
              {f}
            </span>
          ))}
          <div className="grow" />
          {ds && (
            <>
              <button className="btn sm" onClick={() => s.open({ kind: 'page', title: `${d.part.mpn} p1`, params: { ds: ds.id, page: 0, part: d.part.mpn } })}>
                <FileText size={13} /> Pages
              </button>
              <button
                className="btn sm"
                title={s.app?.canOpenFile ? 'Open the PDF in your PDF viewer' : 'Open the PDF in a new tab'}
                onClick={() => openPDF(ds.id, !!s.app?.canOpenFile).catch((e) => s.toast({ kind: 'err', title: 'Could not open the PDF', body: (e as Error).message }))}
              >
                <ExternalLink size={13} /> PDF
              </button>
            </>
          )}
          <StatusPicker part={d.part} />
          <AddToProject mpn={d.part.mpn} />
          {calc && (
            <button className="btn sm" onClick={() => s.open({ kind: 'calc', title: `${calc} ${d.part.mpn}`, params: { kind: calc, part: d.part.mpn } })}>
              <Calculator size={13} /> Calculate
            </button>
          )}
          {ds && (
            <button className="btn sm ghost" title="Check the manufacturer for a newer revision of this datasheet" onClick={() => setUpdating(true)}>
              <CloudDownload size={13} />
            </button>
          )}
          {ds && (
            <button
              className="btn sm ghost"
              title="Redo automatic extraction (what you and the agent recorded is kept)"
              onClick={async () => {
                if (lang !== 'en') return setReextracting(true);
                try {
                  await api.reextract(ds.id);
                  s.toast({ kind: 'ok', title: `Re-extracted ${ds.filename}` });
                  s.refreshLibrary();
                } catch (e) {
                  s.toast({ kind: 'err', title: 'Re-extraction failed', body: (e as Error).message });
                }
              }}
            >
              <RefreshCw size={13} />
            </button>
          )}
          {model && (
            <button
              className={`btn sm ${translating ? 'primary' : 'ghost'}`}
              title={model.installed ? (translating ? 'Show the original text' : `Translate this datasheet into English on this computer (${model.label})`) : 'Download the translation model in Settings → Translation'}
              onClick={() => (model.installed ? setTranslating(!translating) : s.open({ kind: 'settings', title: 'Settings', params: {} }))}
            >
              <Languages size={13} /> {translating ? 'Original' : 'Translate'}
            </button>
          )}
          <button className="btn sm ghost danger" title={ds ? 'Delete this datasheet and its parts from the library…' : 'Delete this part from the library…'} onClick={() => setDeleting(true)}>
            <Trash2 size={13} />
          </button>
        </div>
        <div className="subtabs">
          {tabs.map(([k, label, n]) => (
            <button key={k} className={sub === k ? 'on' : ''} onClick={() => setSub(k)}>
              {label}
              {n ? <span className="n">{n}</span> : null}
            </button>
          ))}
        </div>
      </div>
      <div className="scroll" style={{ flex: 1 }}>
        {!ds && sub !== 'notes' && sub !== 'variants' && sub !== 'alternatives' ? (
          <Empty icon={<FileText size={28} />} title="No datasheet">
            This part has no datasheet in the library yet. Import one to extract its parameters and figures.
          </Empty>
        ) : (
          <>
            {sub === 'overview' && <Overview d={d} onSub={setSub} />}
            {sub === 'parameters' && ds && <Parameters d={d} query={String(params.query ?? '')} />}
            {sub === 'figures' && ds && <Figures d={d} />}
            {sub === 'pins' && ds && <Pins d={d} />}
            {sub === 'package' && ds && (
              <div className="pad">
                <PartPackagesPanel
                  part={d.part.mpn}
                  onPage={(page) => s.open({ kind: 'page', title: `${d.part.mpn} p${page + 1}`, params: { ds: ds.id, page, part: d.part.mpn, nonce: Date.now() } })}
                />
              </div>
            )}
            {sub === 'cad' && <PartCadPanel part={d.part.mpn} />}
            {sub === 'sections' && ds && <Sections d={d} />}
            {sub === 'registers' && ds && <Registers d={d} />}
            {sub === 'tables' && ds && <Tables d={d} />}
          </>
        )}
        {sub === 'notes' && (
          <div className="pad">
            <NotesPanel kind="part" id={d.part.mpn} datasheetId={ds?.id} />
          </div>
        )}
        {sub === 'variants' && <Variants d={d} />}
        {sub === 'alternatives' && <Alternatives d={d} />}
      </div>
      {reextracting && ds && <ReextractDialog ds={ds} lang={lang} onClose={() => setReextracting(false)} onDone={(tr) => (setReextracting(false), setTranslating(tr))} />}
      {updating && ds && <UpdateDialog ds={ds} onClose={() => setUpdating(false)} />}
      {deleting && (
        <DeleteDialog
          part={d.part.mpn}
          onClose={() => setDeleting(false)}
          onDeleted={(removed) => {
            setDeleting(false);
            // Close the tabs of the parts that no longer exist.
            const gone = new Set(removed.map((m) => m.toUpperCase()));
            for (const t of s.tabs) if (t.kind === 'part' && gone.has(String(t.params.part ?? '').toUpperCase())) s.close(t.id);
            if (!gone.has(d.part.mpn.toUpperCase())) s.refreshLibrary();
          }}
        />
      )}
    </div>
    </TranslateProvider>
  );
}

/** Menu adding the part to a project. */
function AddToProject({ mpn }: { mpn: string }) {
  const s = useStore();
  const projects = useAsync(() => api.projects(), [s.projectVersion]);
  return (
    <Menu
      align="right"
      width={260}
      trigger={() => (
        <button className="btn sm">
          <FolderPlus size={13} /> Add to project
        </button>
      )}
      items={[
        ...(projects.data ?? []).map((p) => ({
          key: String(p.id),
          label: p.name,
          hint: plural(p.parts, 'part'),
          onSelect: async () => {
            try {
              await api.addProjectPart(p.name, mpn, { qty: 1 });
              s.toast({ kind: 'ok', title: `Added ${mpn} to ${p.name}`, action: { label: 'Open project', run: () => s.open({ kind: 'project', title: p.name, params: { project: p.name } }) } });
              useStore.setState({ projectVersion: s.projectVersion + 1 });
            } catch (e) {
              s.toast({ kind: 'err', title: 'Could not add part', body: (e as Error).message });
            }
          },
        })),
        ...(projects.data?.length ? [{ key: 'sep', label: '', separator: true }] : []),
        { key: 'new', label: 'New project…', onSelect: () => s.setSidebar('projects') },
      ]}
    />
  );
}

/** Opens a parameter's location on its page. */
function openParam(p: Parameter, mpn: string) {
  useStore.getState().open({ kind: 'page', title: `${mpn} p${p.page + 1}`, params: { ds: p.datasheetId, page: p.page, box: p.box ?? undefined, part: mpn, nonce: Date.now() } });
}

/** Key parameters, recommended parts, highlights and the outline. */
function Overview({ d, onSub }: { d: PartDetail; onSub: (s: Sub) => void }) {
  const s = useStore();
  const ds = d.datasheet!;
  const figs = useAsync(() => api.figures(ds.id, { mpn: d.part.mpn }), [ds.id, s.libraryVersion]);
  const highlights = useMemo(() => {
    const all = figs.data ?? [];
    const pick = (k: string, n: number) => all.filter((f) => f.kind === k).slice(0, n);
    return [...pick('schematic', 2), ...pick('graph', 4), ...pick('waveform', 2)];
  }, [figs.data]);
  const keys = d.keyParameters ?? [];
  return (
    <div className="pad col" style={{ gap: 16 }}>
      <div className="card">
        <h3>
          Key parameters <span className="faint" style={{ textTransform: 'none', letterSpacing: 0, fontWeight: 500 }}>best value per key · click to see it on the page</span>
        </h3>
        {keys.length === 0 ? (
          <div className="muted">No keyed parameters yet. Ask your agent to read the specification tables and record them, or add them under Parameters.</div>
        ) : (
          <div className="keygrid">
            {keys.map((k) => (
              <div key={k.key} className="keycell" onClick={() => openParam(k.param, d.part.mpn)} title={`${k.param.symbol ?? ''} ${k.param.name}\n${k.param.conditions ?? ''}`}>
                <div className="k">
                  {k.key}
                  {k.rating && <span className="badge warn">rating</span>}
                  <span style={{ marginLeft: 'auto' }}>
                    <SourceBadge source={k.param.source} />
                  </span>
                </div>
                <div className="v">{headline(k.param)}</div>
                <div className="l">{k.label}</div>
              </div>
            ))}
          </div>
        )}
      </div>
      {(d.recommended?.length ?? 0) > 0 && (
        <div className="card">
          <h3>Recommended components</h3>
          <table className="dtable">
            <thead>
              <tr>
                <th>Role</th>
                <th>Value</th>
                <th>Part</th>
                <th>Conditions</th>
                <th>Page</th>
              </tr>
            </thead>
            <tbody>
              {d.recommended!.map((r) => (
                <tr key={r.id}>
                  <td>{r.role}</td>
                  <td className="mono">{r.value}</td>
                  <td className="mono">{[r.manufacturer, r.mpn].filter(Boolean).join(' ')}</td>
                  <td className="cond">{r.conditions}</td>
                  <td>{r.page >= 0 ? r.page + 1 : ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {highlights.length > 0 && (
        <div className="card">
          <h3>
            Highlights
            <button className="btn ghost sm" style={{ marginLeft: 'auto' }} onClick={() => onSub('figures')}>
              All {d.counts.figures} figures
            </button>
          </h3>
          <FigureGrid figures={highlights} />
        </div>
      )}
      <div className="grid2">
        <div className="card">
          <h3>Datasheet</h3>
          <dl className="kv">
            <dt>File</dt>
            <dd>{ds.filename}</dd>
            <dt>Title</dt>
            <dd style={{ fontFamily: 'var(--font-ui)' }}><T text={ds.title} /></dd>
            <dt>Pages</dt>
            <dd>{ds.pages}</dd>
            <dt>Imported</dt>
            <dd>{relTime(ds.importedAt)}</dd>
            {ds.sourceUrl && (
              <>
                <dt>Source</dt>
                <dd className="ellipsis">
                  <a href={ds.sourceUrl} target="_blank" rel="noreferrer">
                    {ds.sourceUrl}
                  </a>
                </dd>
              </>
            )}
            {d.projects && d.projects.length > 0 && (
              <>
                <dt>Projects</dt>
                <dd style={{ fontFamily: 'var(--font-ui)' }}>
                  {d.projects.map((p) => (
                    <a key={p.id} style={{ marginRight: 8, cursor: 'pointer' }} onClick={() => s.open({ kind: 'project', title: p.name, params: { project: p.name } })}>
                      {p.name}
                    </a>
                  ))}
                </dd>
              </>
            )}
          </dl>
        </div>
        <div className="card">
          <h3>Outline</h3>
          <div className="toc" style={{ maxHeight: 320, overflow: 'auto' }}>
            {(d.sections ?? [])
              .filter((x) => x.level <= 2)
              .map((x) => (
                <a key={x.id} style={{ paddingLeft: 8 + (x.level - 1) * 14 }} onClick={() => s.open({ kind: 'page', title: `${d.part.mpn} p${x.page + 1}`, params: { ds: ds.id, page: x.page, part: d.part.mpn, nonce: Date.now() } })}>
                  {x.num} <T text={x.title} /> <span className="faint">p{x.page + 1}</span>
                </a>
              ))}
          </div>
        </div>
      </div>
      {(d.notes?.length ?? 0) > 0 && (
        <div className="card">
          <h3>Notes</h3>
          {d.notes!.slice(-3).map((n) => (
            <div key={n.id} className="note" style={{ marginBottom: 8 }}>
              <div className="meta">
                <SourceBadge source={n.author === 'agent' ? 'agent' : 'user'} /> {relTime(n.createdAt)}
              </div>
              <Markdown src={n.body} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/** A grid of figure thumbnails. */
export function FigureGrid({ figures }: { figures: Figure[] }) {
  const s = useStore();
  return (
    <div className="fig-grid">
      {figures.map((f) => (
        <div key={f.id} className="fig-card" onClick={() => s.open({ kind: 'figure', title: `Fig ${f.number}`, params: { id: f.id } })}>
          <div className="thumb">
            <img src={figureImageURL(f.id, 60)} alt={f.caption} loading="lazy" />
          </div>
          <div className="cap">
            <b>
              {f.number && `${f.number} · `}
              <T text={f.title || f.caption} />
            </b>
            <div className="row faint" style={{ fontSize: 11 }}>
              <span>{figureKinds[f.kind] ?? f.kind}</span>
              <span>p{f.page + 1}</span>
              {f.source !== 'auto' && <SourceBadge source={f.source} />}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

/** Figures with kind filters. */
function Figures({ d }: { d: PartDetail }) {
  const s = useStore();
  const [kind, setKind] = useState('');
  const [q, setQ] = useState('');
  const figs = useAsync(() => api.figures(d.datasheet!.id, { kind, query: q, mpn: d.part.mpn }), [d.datasheet!.id, kind, q, s.libraryVersion]);
  return (
    <div className="pad col">
      <div className="row" style={{ flexWrap: 'wrap' }}>
        <input className="input" style={{ width: 280 }} placeholder="Filter captions, axes, descriptions" value={q} onChange={(e) => setQ(e.target.value)} />
        <button className={`chip ${kind === '' ? 'on' : ''}`} onClick={() => setKind('')}>
          All
        </button>
        {Object.entries(d.figureKinds ?? {})
          .sort((a, b) => b[1] - a[1])
          .map(([k, n]) => (
            <button key={k} className={`chip ${kind === k ? 'on' : ''}`} onClick={() => setKind(kind === k ? '' : k)}>
              {figureKinds[k] ?? k} <span className="faint">{n}</span>
            </button>
          ))}
      </div>
      <ErrorBox error={figs.error} />
      {figs.data && <FigureGrid figures={figs.data} />}
    </div>
  );
}

/** Editable parameter table, grouped by section. */
function Parameters({ d, query }: { d: PartDetail; query: string }) {
  const s = useStore();
  const ds = d.datasheet!;
  const [q, setQ] = useState(query);
  const [sec, setSec] = useState('');
  const params = useAsync(() => api.parameters(ds.id, { query: q, section: sec, mpn: d.part.mpn }), [ds.id, q, sec, s.libraryVersion]);
  const keys = s.app?.keys ?? {};
  const groups = useMemo(() => {
    const g = new Map<string, Parameter[]>();
    for (const p of params.data ?? []) {
      if (!g.has(p.section)) g.set(p.section, []);
      g.get(p.section)!.push(p);
    }
    return [...g.entries()];
  }, [params.data]);
  const save = async (p: Parameter, patch: Partial<Parameter>) => {
    try {
      await api.recordParameters(ds.id, [{ ...p, ...patch }]);
      s.refreshLibrary();
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save parameter', body: (e as Error).message });
    }
  };
  const num = (v: string) => (v.trim() === '' ? null : Number(v));
  const add = async () => {
    await api.recordParameters(ds.id, [{ name: 'New parameter', section: sec || 'electrical', page: -1 }]);
    s.refreshLibrary();
  };
  return (
    <div className="pad col">
      <div className="row" style={{ flexWrap: 'wrap' }}>
        <input className="input" style={{ width: 280 }} placeholder="Filter by name, symbol, conditions or key" value={q} onChange={(e) => setQ(e.target.value)} />
        {['', 'abs_max', 'recommended', 'electrical', 'timing', 'thermal'].map((x) => (
          <button key={x} className={`chip ${sec === x ? 'on' : ''}`} onClick={() => setSec(x)}>
            {x ? sectionLabels[x] : 'All'}
          </button>
        ))}
        <div className="grow" />
        <button className="btn sm" onClick={add}>
          Add parameter
        </button>
      </div>
      <div className="muted" style={{ fontSize: 12 }}>
        Automatically extracted rows (marked <SourceBadge source="auto" />) can be wrong. Edit any cell to correct it; your values take precedence and survive re-extraction.
      </div>
      <ErrorBox error={params.error} />
      <table className="dtable">
        <thead>
          <tr>
            <th style={{ width: 110 }}>Key</th>
            <th style={{ width: 90 }}>Symbol</th>
            <th>Parameter</th>
            <th>Conditions</th>
            <th style={{ width: 70, textAlign: 'right' }}>Min</th>
            <th style={{ width: 70, textAlign: 'right' }}>Typ</th>
            <th style={{ width: 70, textAlign: 'right' }}>Max</th>
            <th style={{ width: 60 }}>Unit</th>
            <th style={{ width: 44 }}>Page</th>
            <th style={{ width: 60 }} />
          </tr>
        </thead>
        <tbody>
          {groups.map(([section, ps]) => (
            <ParamGroup key={section} section={section} params={ps} keys={keys} onSave={save} num={num} mpn={d.part.mpn} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** One section's rows of the parameter table. */
function ParamGroup(props: { section: string; params: Parameter[]; keys: Record<string, string>; onSave: (p: Parameter, patch: Partial<Parameter>) => void; num: (v: string) => number | null; mpn: string }) {
  const s = useStore();
  const { section, params, keys, onSave, num, mpn } = props;
  return (
    <>
      <tr className="group">
        <td colSpan={10}>{sectionLabels[section] ?? section}</td>
      </tr>
      {params.map((p) => (
        <tr key={p.id + p.updatedAt}>
          <td>
            <select className="input select" style={{ height: 24, fontSize: 11.5 }} value={p.key ?? ''} onChange={(e) => onSave(p, { key: e.target.value })}>
              <option value="">—</option>
              {Object.keys(keys)
                .sort()
                .map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
            </select>
          </td>
          <td>
            <input className="cell" defaultValue={p.symbol} onBlur={(e) => e.target.value !== (p.symbol ?? '') && onSave(p, { symbol: e.target.value })} />
          </td>
          <td>
            <input className="cell" defaultValue={p.name} onBlur={(e) => e.target.value !== p.name && onSave(p, { name: e.target.value })} />
          </td>
          <td className="cond">
            <input className="cell" defaultValue={p.conditions} onBlur={(e) => e.target.value !== (p.conditions ?? '') && onSave(p, { conditions: e.target.value })} />
          </td>
          {(['min', 'typ', 'max'] as const).map((f) => (
            <td key={f} className="num">
              <input className="cell" defaultValue={p[f] === null || p[f] === undefined ? '' : sig3(p[f]!)} onBlur={(e) => num(e.target.value) !== (p[f] ?? null) && onSave(p, { [f]: num(e.target.value) })} />
            </td>
          ))}
          <td>
            <input className="cell" defaultValue={p.unit} onBlur={(e) => e.target.value !== (p.unit ?? '') && onSave(p, { unit: e.target.value })} />
          </td>
          <td>{p.page >= 0 && <a onClick={() => openParam(p, mpn)} style={{ cursor: 'pointer' }}>{p.page + 1}</a>}</td>
          <td>
            <div className="row" style={{ gap: 2 }}>
              <SourceBadge source={p.source} />
              <button
                className="btn ghost icon sm"
                title="Delete"
                onClick={async () => {
                  await api.deleteParameters(p.datasheetId, [p.id]);
                  s.refreshLibrary();
                }}
              >
                <Trash2 size={12} />
              </button>
            </div>
          </td>
        </tr>
      ))}
    </>
  );
}

/** Pin table. */
function Pins({ d }: { d: PartDetail }) {
  const s = useStore();
  // This part's: a datasheet of a 2-pin and a 3-pin header has a table for each.
  const pins = useAsync(() => api.pins(d.datasheet!.id, d.part.id), [d.datasheet!.id, d.part.id, s.libraryVersion]);
  const pinSec = (d.sections ?? []).find((x) => x.kind === 'pins');
  if (pins.data?.length === 0)
    return (
      <Empty icon={<FileText size={28} />} title="No pin table extracted">
        {pinSec ? (
          <>
            The pin description is in section <b>{pinSec.num} {pinSec.title}</b> (page {pinSec.page + 1}). Ask your agent to record it with record_pins.
          </>
        ) : (
          'Ask your agent to read the pin description and record it.'
        )}
      </Empty>
    );
  return (
    <div className="pad">
      <table className="dtable">
        <thead>
          <tr>
            <th>Package</th>
            <th>No.</th>
            <th>Name</th>
            <th>Type</th>
            <th>Description</th>
            <th>Page</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {pins.data?.map((p) => (
            <tr key={p.id}>
              <td>{p.package}</td>
              <td className="mono">{p.number}</td>
              <td className="mono">
                <b>{p.name}</b>
              </td>
              <td className="mono">{p.type}</td>
              <td><T text={p.description} /></td>
              <td>{p.page + 1}</td>
              <td>
                <SourceBadge source={p.source} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Section outline and text. */
function Sections({ d }: { d: PartDetail }) {
  const s = useStore();
  const [sel, setSel] = useState<Section | undefined>(d.sections?.[0]);
  const text = useAsync(() => (sel ? api.section(sel.id) : Promise.resolve(undefined)), [sel?.id]);
  return (
    <div className="split" style={{ height: '100%' }}>
      <div className="side toc" style={{ padding: 8 }}>
        {(d.sections ?? []).map((x) => (
          <a key={x.id} className={sel?.id === x.id ? 'on' : ''} style={{ paddingLeft: 8 + (x.level - 1) * 12 }} onClick={() => setSel(x)}>
            {x.num} <T text={x.title} />
          </a>
        ))}
      </div>
      <div className="main scroll pad">
        {sel && (
          <>
            <div className="row" style={{ marginBottom: 10 }}>
              <h3 style={{ margin: 0 }}>
                {sel.num} <T text={sel.title} />
              </h3>
              <span className="badge">{sel.kind}</span>
              <div className="grow" />
              <button className="btn sm" onClick={() => s.open({ kind: 'page', title: `${d.part.mpn} p${sel.page + 1}`, params: { ds: d.datasheet!.id, page: sel.page, part: d.part.mpn, nonce: Date.now() } })}>
                <FileText size={13} /> Page {sel.page + 1}
              </button>
            </div>
            {text.data && (text.data.text ? <SectionText text={text.data.text} /> : <div className="sec-text faint">(no text under this heading; see its subsections)</div>)}
          </>
        )}
      </div>
    </div>
  );
}

/** Recorded registers and the register-map sections. */
function Registers({ d }: { d: PartDetail }) {
  const s = useStore();
  const regs = useAsync(() => api.registers(d.datasheet!.id), [d.datasheet!.id, s.libraryVersion]);
  const secs = (d.sections ?? []).filter((x) => x.kind === 'registers');
  return (
    <div className="pad col">
      {regs.data?.length === 0 && (
        <div className="muted">
          {secs.length ? 'Register-map sections are listed below. Ask your agent to record the registers (record_registers) to make them searchable and structured.' : 'No register map found in this datasheet.'}
        </div>
      )}
      {regs.data?.map((r) => (
        <div key={r.id} className="card">
          <h3 style={{ textTransform: 'none', letterSpacing: 0, fontSize: 13, color: 'var(--text)' }}>
            <span className="mono">{r.address}</span> {r.name}
            <span className="faint" style={{ fontWeight: 500 }}>
              {r.width ? `${r.width}-bit` : ''} {r.reset ? `reset ${r.reset}` : ''} {r.access}
            </span>
            <span style={{ marginLeft: 'auto' }}>
              <SourceBadge source={r.source} />
            </span>
          </h3>
          {r.description && <div className="muted"><T text={r.description} /></div>}
          {(r.fields?.length ?? 0) > 0 && (
            <table className="dtable" style={{ marginTop: 8 }}>
              <thead>
                <tr>
                  <th>Bits</th>
                  <th>Field</th>
                  <th>Access</th>
                  <th>Reset</th>
                  <th>Description</th>
                </tr>
              </thead>
              <tbody>
                {r.fields!.map((f, i) => (
                  <tr key={i}>
                    <td className="mono">{f.bits}</td>
                    <td className="mono">{f.name}</td>
                    <td className="mono">{f.access}</td>
                    <td className="mono">{f.reset}</td>
                    <td><T text={f.description} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      ))}
      {secs.length > 0 && (
        <div className="card">
          <h3>Register sections</h3>
          {secs.map((x) => (
            <div key={x.id} className="row" style={{ padding: '4px 0' }}>
              <span>
                {x.num} {x.title}
              </span>
              <span className="faint">pages {x.page + 1}–{x.endPage + 1}</span>
              <div className="grow" />
              <button className="btn sm" onClick={() => s.open({ kind: 'page', title: `${d.part.mpn} p${x.page + 1}`, params: { ds: d.datasheet!.id, page: x.page, part: d.part.mpn, nonce: Date.now() } })}>
                Open
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/** Detected tables: each with its image, and the cells as read (or as transcribed). */
function Tables({ d }: { d: PartDetail }) {
  const s = useStore();
  const tables = useAsync(() => api.tables(d.datasheet!.id), [d.datasheet!.id, s.libraryVersion]);
  const [kind, setKind] = useState('');
  const shown = (tables.data ?? []).filter((t) => !kind || (kind === 'irregular' ? t.irregular : t.kind === kind));
  const kinds = [...new Set((tables.data ?? []).map((t) => t.kind))];
  const irregular = (tables.data ?? []).filter((t) => t.irregular).length;
  return (
    <div className="pad col">
      <div className="row" style={{ flexWrap: 'wrap' }}>
        <button className={`chip ${kind === '' ? 'on' : ''}`} onClick={() => setKind('')}>
          All
        </button>
        {kinds.map((k) => (
          <button key={k} className={`chip ${kind === k ? 'on' : ''}`} onClick={() => setKind(kind === k ? '' : k)}>
            {k}
          </button>
        ))}
        {irregular > 0 && (
          <button className={`chip ${kind === 'irregular' ? 'on' : ''}`} onClick={() => setKind(kind === 'irregular' ? '' : 'irregular')}>
            irregular <span className="faint">{irregular}</span>
          </button>
        )}
      </div>
      {irregular > 0 && (
        <div className="muted" style={{ fontSize: 12 }}>
          Irregular tables (selection matrices, grouped rows, multi-row headers) can't be read cell by cell; their image is the reference. Ask your agent to transcribe them
          with record_table to make them searchable.
        </div>
      )}
      {shown.map((t) => (
        <details key={t.id} className="card" open={!!t.irregular || t.source !== 'auto'}>
          <summary style={{ cursor: 'pointer' }}>
            <b>
              {t.number ? `Table ${t.number} ` : ''}
              <T text={t.title || t.caption || t.section} />
            </b>{' '}
            <span className="badge">{t.kind}</span> {t.irregular && <span className="badge warn" title={t.irregular}>irregular</span>}{' '}
            {t.source !== 'auto' && <SourceBadge source={t.source} />} <span className="faint">p{t.page + 1} · {t.rows?.length ?? 0} rows</span>
            <a
              style={{ marginLeft: 10, cursor: 'pointer' }}
              onClick={(e) => {
                e.preventDefault();
                s.open({ kind: 'page', title: `${d.part.mpn} p${t.page + 1}`, params: { ds: t.datasheetId, page: t.page, box: t.box, part: d.part.mpn, nonce: Date.now() } });
              }}
            >
              show on page
            </a>
          </summary>
          {t.description && <div className="muted" style={{ marginTop: 8 }}><T text={t.description} /></div>}
          {(t.irregular || t.source === 'auto') && (
            <div className="fig-image" style={{ marginTop: 8 }}>
              <img src={tableImageURL(t.id, 110, t.updatedAt)} alt={t.caption} loading="lazy" />
            </div>
          )}
          {t.irregular && t.source === 'auto' ? (
            <details style={{ marginTop: 8 }}>
              <summary className="faint" style={{ cursor: 'pointer' }}>Cells as read ({t.irregular})</summary>
              <TableCells header={t.header} rows={t.rows} />
            </details>
          ) : (
            <TableCells header={t.header} rows={t.rows} />
          )}
        </details>
      ))}
    </div>
  );
}

/** A table's cells. */
function TableCells({ header, rows }: { header?: string[] | null; rows?: string[][] | null }) {
  return (
    <table className="dtable" style={{ marginTop: 8 }}>
      <thead>
        <tr>
          {header?.map((h, i) => (
            <th key={i}>{h}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows?.map((r, i) => (
          <tr key={i}>
            {r.map((c, j) => (
              <td key={j}>{c}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** Orderable variants. */
function Variants({ d }: { d: PartDetail }) {
  const s = useStore();
  const [v, setV] = useState('');
  return (
    <div className="pad col">
      <div className="muted">Orderable codes resolve to {d.part.mpn} everywhere: search, MCP tools and projects.</div>
      <div className="row">
        <input className="input" style={{ width: 280 }} placeholder="Add an orderable code, e.g. TPS563200DDCR" value={v} onChange={(e) => setV(e.target.value)} />
        <button
          className="btn"
          disabled={!v.trim()}
          onClick={async () => {
            await api.setVariants(d.part.id, [{ mpn: v.trim() }]);
            setV('');
            s.refreshLibrary();
          }}
        >
          Add
        </button>
      </div>
      <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>
        {(d.variants ?? []).map((x) => (
          <span key={x.mpn} className="chip" title={x.source}>
            {x.mpn}
          </span>
        ))}
      </div>
    </div>
  );
}
