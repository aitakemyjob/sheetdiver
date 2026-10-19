import { useState } from 'react';
import { Trash2 } from 'lucide-react';
import { api } from '../lib/api';
import { relTime } from '../lib/format';
import { useStore } from '../lib/store';
import { ErrorBox, Markdown, SourceBadge, useAsync } from '../components/ui';

/** Notes on a target (part, figure, project…), with an editor to add one. */
export function NotesPanel({ kind, id, datasheetId }: { kind: string; id: string; datasheetId?: number }) {
  const s = useStore();
  const [text, setText] = useState('');
  const [v, setV] = useState(0);
  const notes = useAsync(() => api.notes(kind, id), [kind, id, v, s.libraryVersion, s.projectVersion]);
  const add = async () => {
    if (!text.trim()) return;
    try {
      await api.addNote(kind, id, text, datasheetId);
      setText('');
      setV(v + 1);
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not add note', body: (e as Error).message });
    }
  };
  return (
    <div className="col">
      <ErrorBox error={notes.error} />
      {notes.data?.map((n) => (
        <div key={n.id} className="note">
          <div className="meta">
            <SourceBadge source={n.author === 'agent' ? 'agent' : 'user'} />
            {relTime(n.createdAt)}
            <button
              className="btn ghost icon sm"
              style={{ marginLeft: 'auto' }}
              title="Delete note"
              onClick={async () => {
                await api.deleteNote(n.id);
                setV(v + 1);
              }}
            >
              <Trash2 size={12} />
            </button>
          </div>
          <Markdown src={n.body} />
        </div>
      ))}
      <textarea
        className="input"
        placeholder="Add a note (markdown): design decisions, errata, test results… Ctrl+Enter to save"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === 'Enter' && add()}
      />
      <div>
        <button className="btn primary sm" disabled={!text.trim()} onClick={add}>
          Add note
        </button>
      </div>
    </div>
  );
}
