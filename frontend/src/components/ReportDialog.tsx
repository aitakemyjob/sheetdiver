import { useEffect, useState } from 'react';
import { Download, FileText } from 'lucide-react';
import { api, saveText } from '../lib/api';
import { useStore } from '../lib/store';
import { CopyButton, ErrorBox, Markdown, Modal, Spinner } from './ui';

/** File name for a project's report. */
export function reportFilename(project: string): string {
  return `${project.replace(/[^\w.-]+/g, '-').replace(/^-+|-+$/g, '') || 'project'}-design-review.md`;
}

/** A project's design-review report, rendered, with copy and save. */
export function ReportDialog({ project, onClose }: { project: string; onClose: () => void }) {
  const s = useStore();
  const [md, setMd] = useState<string>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    api.projectReport(project).then((r) => setMd(r.markdown), setError);
  }, [project]);
  const save = async () => {
    try {
      const where = await saveText(reportFilename(project), md ?? '', 'text/markdown', !!s.app?.canSaveFile);
      if (where) s.toast({ kind: 'ok', title: 'Report saved', body: where });
    } catch (e) {
      s.toast({ kind: 'err', title: 'Could not save the report', body: (e as Error).message });
    }
  };
  return (
    <Modal
      wide
      title={`Design review · ${project}`}
      icon={<FileText size={16} style={{ color: 'var(--accent)' }} />}
      onClose={onClose}
      footer={
        md ? (
          <>
            <span className="faint grow">Markdown: paste it into a PR, wiki or review doc.</span>
            <CopyButton text={md} />
            <button className="btn sm primary" onClick={save}>
              <Download size={13} /> Save .md
            </button>
          </>
        ) : undefined
      }
    >
      <ErrorBox error={error} />
      {!md && !error && <Spinner label="Building the report…" />}
      {md && (
        <div style={{ maxHeight: '62vh', overflow: 'auto' }}>
          <Markdown src={md} className="report" />
        </div>
      )}
    </Modal>
  );
}
