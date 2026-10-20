import { useState } from 'react';
import { Languages, RefreshCw } from 'lucide-react';
import { api } from '../lib/api';
import { plural } from '../lib/format';
import { useStore } from '../lib/store';
import { translateDatasheet } from '../lib/translate';
import type { Datasheet } from '../lib/types';
import { ErrorBox, Modal, Switch } from './ui';

/**
 * Re-extracts a datasheet that isn't in English, optionally from its
 * English translation (made on this computer): headings, table headers
 * and parameter names are then found as in English datasheets, and the
 * part opens translated.
 */
export function ReextractDialog({ ds, lang, onClose, onDone }: { ds: Datasheet; lang: string; onClose: () => void; onDone: (translated: boolean) => void }) {
  const s = useStore();
  const model = s.translation?.models.find((m) => m.from === lang && m.to === 'en');
  const [translate, setTranslate] = useState(!!model?.installed);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState<unknown>();
  const run = async () => {
    setError(undefined);
    try {
      if (translate) {
        setBusy('Translating…');
        await translateDatasheet(ds.id, (done, total) => setBusy(`Translating ${done} of ${plural(total, 'line')}…`));
      }
      setBusy('Extracting…');
      await api.reextract(ds.id, translate);
      s.toast({ kind: 'ok', title: `Re-extracted ${ds.filename}`, body: translate ? 'From its English translation' : undefined });
      s.refreshLibrary();
      onDone(translate);
    } catch (e) {
      setError(e);
      setBusy('');
    }
  };
  return (
    <Modal
      title={`Re-extract ${ds.filename}?`}
      icon={<RefreshCw size={16} style={{ color: 'var(--accent)' }} />}
      onClose={busy ? () => {} : onClose}
      footer={
        <>
          <span className="faint grow">{busy}</span>
          <button className="btn sm" onClick={onClose} disabled={!!busy}>
            Cancel
          </button>
          <button className="btn sm primary" onClick={run} disabled={!!busy} autoFocus>
            <RefreshCw size={13} /> Re-extract
          </button>
        </>
      }
    >
      <ErrorBox error={error} />
      <div className="col">
        <div>Redoes automatic extraction. What you and agents recorded is kept.</div>
        <label className="row" style={{ gap: 8, alignItems: 'flex-start' }}>
          <Switch on={translate} onChange={setTranslate} label="Translate first" disabled={!model?.installed || !!busy} />
          <span>
            <b>
              <Languages size={13} /> Translate into English first
            </b>
            <div className="faint" style={{ fontSize: 12 }}>
              {model?.installed ? (
                'Extracts from the English translation, so sections, tables and parameters are found as in English datasheets. Translated on this computer; the part opens translated.'
              ) : (
                <>
                  Needs the {model?.label ?? 'translation'} model:{' '}
                  <a href="#" onClick={(e) => (e.preventDefault(), onClose(), s.open({ kind: 'settings', title: 'Settings', params: {} }))}>
                    download it in Settings
                  </a>
                  .
                </>
              )}
            </div>
          </span>
        </label>
      </div>
    </Modal>
  );
}
