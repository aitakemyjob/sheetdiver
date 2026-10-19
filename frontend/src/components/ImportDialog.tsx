import { useRef, useState } from 'react';
import { FileUp, Link2 } from 'lucide-react';
import { api } from '../lib/api';
import { categories } from '../lib/format';
import { useStore } from '../lib/store';
import { Modal } from './ui';

/** Import datasheets: drop or pick PDFs, or give a URL, with optional overrides. */
export function ImportDialog() {
  const s = useStore();
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const [url, setUrl] = useState('');
  const [mpns, setMpns] = useState('');
  const [mfr, setMfr] = useState('');
  const [cat, setCat] = useState('');
  const [busy, setBusy] = useState(false);
  const opts = { mpns: mpns.trim() || undefined, manufacturer: mfr.trim() || undefined, category: cat || undefined };
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    s.setImportOpen(false);
    try {
      await fn();
    } finally {
      setBusy(false);
    }
  };
  const pick = async () => {
    if (s.app?.canPickFile) {
      const r = await api.pickPDFs();
      if (r.paths?.length) run(() => s.importPaths(r.paths!, { ...opts, mpns: opts.mpns?.split(/[\s,]+/) }));
    } else {
      input.current?.click();
    }
  };
  return (
    <Modal title="Import datasheets" icon={<FileUp size={16} style={{ color: 'var(--accent)' }} />} onClose={() => s.setImportOpen(false)}>
      <div
        className={`dropzone ${over ? 'over' : ''}`}
        onClick={pick}
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setOver(false);
          const files = Array.from(e.dataTransfer.files);
          if (files.length) run(() => s.importFiles(files, opts));
        }}
      >
        <FileUp size={26} />
        <div style={{ marginTop: 8, fontWeight: 600 }}>Drop PDF datasheets here, or click to choose</div>
        <div className="faint" style={{ marginTop: 4 }}>Part numbers, variants, sections, figures, spec tables and pins are extracted automatically.</div>
      </div>
      <input
        ref={input}
        type="file"
        accept="application/pdf,.pdf"
        multiple
        hidden
        onChange={(e) => {
          const files = Array.from(e.target.files ?? []);
          if (files.length) run(() => s.importFiles(files, opts));
          e.target.value = '';
        }}
      />
      <div className="row">
        <Link2 size={15} className="faint" />
        <input className="input grow" placeholder="…or a URL to a PDF datasheet" value={url} onChange={(e) => setUrl(e.target.value)} />
        <button className="btn primary" disabled={!url.trim() || busy} onClick={() => run(() => s.importPaths([url.trim()], { ...opts, url: true, mpns: opts.mpns?.split(/[\s,]+/) }))}>
          Fetch
        </button>
      </div>
      <details>
        <summary className="muted" style={{ cursor: 'pointer' }}>Overrides (optional)</summary>
        <div className="col" style={{ marginTop: 10 }}>
          <div className="field">
            <label>Part numbers (comma-separated; replaces detection)</label>
            <input className="input" placeholder="e.g. TPS563200, TPS562200" value={mpns} onChange={(e) => setMpns(e.target.value)} />
          </div>
          <div className="row">
            <div className="field grow">
              <label>Manufacturer</label>
              <input className="input" value={mfr} onChange={(e) => setMfr(e.target.value)} />
            </div>
            <div className="field grow">
              <label>Category</label>
              <select className="input select" value={cat} onChange={(e) => setCat(e.target.value)}>
                <option value="">Detect</option>
                {Object.entries(categories).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </div>
      </details>
    </Modal>
  );
}
