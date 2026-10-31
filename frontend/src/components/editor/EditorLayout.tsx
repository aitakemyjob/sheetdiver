import type { ReactNode } from 'react';

/**
 * The frame every CAD editor sits in: a title line with its actions, a
 * toolbar, the drawing with a side panel, and a status line.
 */
export function EditorLayout({ title, actions, toolbar, side, status, children }: { title: ReactNode; actions: ReactNode; toolbar: ReactNode; side: ReactNode; status: ReactNode; children: ReactNode }) {
  return (
    <div className="cad-editor">
      <div className="cad-editor-head row">
        <div className="cad-editor-title">{title}</div>
        <span className="grow" />
        {actions}
      </div>
      <div className="cad-editor-tools row">{toolbar}</div>
      <div className="cad-editor-main">
        <div className="cad-editor-canvas">{children}</div>
        <aside className="cad-editor-side">{side}</aside>
      </div>
      <div className="cad-editor-status muted">{status}</div>
    </div>
  );
}
