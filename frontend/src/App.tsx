import { useEffect, useState } from 'react';
import { useStore } from './lib/store';
import { TitleBar } from './components/TitleBar';
import { ActivityBar } from './components/ActivityBar';
import { Sidebar } from './components/Sidebar';
import { TabsBar } from './components/TabsBar';
import { StatusBar } from './components/StatusBar';
import { Toasts } from './components/Toasts';
import { ConnectAgent } from './components/ConnectAgent';
import { ImportDialog } from './components/ImportDialog';
import { ImportQueue } from './components/ImportQueue';
import { Spinner } from './components/ui';
import { ViewHost } from './views/ViewHost';
import { WalkthroughBar } from './components/WalkthroughBar';

/**
 * Root component: boots the store, binds global shortcuts and file drops,
 * and lays out the title bar, activity bar, sidebar, tabs and status bar.
 */
export function App() {
  const s = useStore();
  const [dragging, setDragging] = useState(false);

  useEffect(() => {
    useStore.getState().init();
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod) return;
      const st = useStore.getState();
      const k = e.key.toLowerCase();
      if (k === 'k' || k === 'p') {
        e.preventDefault();
        st.open({ kind: 'search', title: 'Search', params: { query: '' } });
      } else if (k === 'b') {
        e.preventDefault();
        st.toggleSidebar();
      } else if (k === 'o') {
        e.preventDefault();
        st.setImportOpen(true);
      } else if (k === 'w' && st.active) {
        e.preventDefault();
        st.close(st.active);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Browser mode: PDFs dropped anywhere are uploaded. (The desktop app
  // receives native file drops as paths instead.)
  useEffect(() => {
    const over = (e: DragEvent) => {
      if (e.dataTransfer?.types.includes('Files')) {
        e.preventDefault();
        setDragging(true);
      }
    };
    const leave = (e: DragEvent) => {
      if (!e.relatedTarget) setDragging(false);
    };
    const drop = (e: DragEvent) => {
      setDragging(false);
      const files = Array.from(e.dataTransfer?.files ?? []);
      if (!files.length || useStore.getState().importOpen) return;
      e.preventDefault();
      useStore.getState().importFiles(files);
    };
    window.addEventListener('dragover', over);
    window.addEventListener('dragleave', leave);
    window.addEventListener('drop', drop);
    return () => {
      window.removeEventListener('dragover', over);
      window.removeEventListener('dragleave', leave);
      window.removeEventListener('drop', drop);
    };
  }, []);

  if (!s.ready) {
    return (
      <div className="app">
        <Spinner label="Starting SheetDiver…" />
      </div>
    );
  }
  if (s.fatal) {
    return (
      <div className="app" style={{ display: 'grid', placeItems: 'center' }}>
        <div className="error-box" style={{ maxWidth: 520 }}>
          <b>SheetDiver could not reach its backend.</b>
          <div className="muted" style={{ marginTop: 6 }}>{s.fatal}</div>
        </div>
      </div>
    );
  }
  return (
    <div className="app">
      <TitleBar />
      <div className="workbench">
        <ActivityBar />
        {s.sidebarOpen && <Sidebar />}
        <div className="center">
          {s.tabs.length > 0 && <TabsBar />}
          <ViewHost />
          <WalkthroughBar />
        </div>
      </div>
      <StatusBar />
      <Toasts />
      {s.connectOpen && <ConnectAgent />}
      {s.queueOpen && !s.importOpen && <ImportQueue />}
      {s.importOpen && <ImportDialog />}
      {dragging && !s.importOpen && (
        <div className="drop-overlay">
          <div>Drop datasheet PDFs or folders to import</div>
        </div>
      )}
    </div>
  );
}
