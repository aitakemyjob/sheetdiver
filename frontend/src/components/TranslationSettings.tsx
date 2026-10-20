import { Download, Languages, Trash2 } from 'lucide-react';
import { api } from '../lib/api';
import { sizeText, useStore } from '../lib/store';
import { resetEngine } from '../lib/translate';
import { ErrorBox } from './ui';

/**
 * Settings for offline translation: download or remove each language's
 * model. Translation itself runs on this machine; downloading a model is
 * the only network use, and only when the user asks.
 */
export function TranslationSettings() {
  const s = useStore();
  const t = s.translation;
  const busy = !!t?.progress;
  const fail = (e: unknown) =>
    s.toast({
      kind: 'err',
      title: 'Translation model',
      body: (e as Error).message,
    });
  return (
    <div className="card col">
      <h3>
        <Languages size={14} /> Translation
      </h3>
      <div className="muted" style={{ fontSize: 12 }}>
        Translate Chinese datasheets into English on this computer, with Mozilla&apos;s{' '}
        <a href="https://github.com/mozilla/firefox-translations-models" target="_blank" rel="noreferrer">
          Firefox Translations
        </a>{' '}
        models (MPL-2.0). The model is downloaded once, only when you ask; after that nothing you translate leaves this computer. Translations are saved in your library.
      </div>
      {t?.models.map((m) => (
        <div key={m.from + m.to} className="row" style={{ flexWrap: 'wrap', gap: 8 }}>
          <b>{m.label}</b>
          {m.installed ? <span className="badge ok">installed</span> : <span className="faint">Not downloaded ({sizeText(m.sizeBytes)})</span>}
          <div className="grow" />
          {m.installed ? (
            <button
              className="btn ghost"
              disabled={busy}
              onClick={async () => {
                const st = await api.translateRemove(m.from, m.to).catch((e) => (fail(e), t));
                resetEngine();
                useStore.setState({ translation: st });
              }}
            >
              <Trash2 size={13} /> Remove
            </button>
          ) : (
            <button
              className="btn primary"
              disabled={busy}
              onClick={async () =>
                useStore.setState({
                  translation: await api.translateDownload(m.from, m.to).catch((e) => (fail(e), t)),
                })
              }
            >
              <Download size={13} /> Download
            </button>
          )}
        </div>
      ))}
      {busy && (
        <div>
          <div className="faint">
            Downloading {t.progress!.model}… {sizeText(t.progress!.done)} of {sizeText(t.progress!.total)}
          </div>
          <div className="bar">
            <i
              style={{
                width: `${t.progress!.total ? (100 * t.progress!.done) / t.progress!.total : 3}%`,
              }}
            />
          </div>
        </div>
      )}
      {t?.error && !busy && <ErrorBox error={new Error(t.error)} />}
    </div>
  );
}
