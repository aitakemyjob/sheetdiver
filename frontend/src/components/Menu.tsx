import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

export interface MenuItem {
  key: string;
  label: ReactNode;
  hint?: ReactNode;
  icon?: ReactNode;
  active?: boolean;
  onSelect?: () => void;
  separator?: boolean;
}

/**
 * Dropdown anchored under its trigger. The panel is rendered into a portal
 * with fixed positioning so containers with overflow clipping or
 * backdrop-filter (like the title bar) can never hide it.
 */
export function Menu({ trigger, items, align = 'left', width = 260 }: { trigger: (open: boolean) => ReactNode; items: MenuItem[]; align?: 'left' | 'right'; width?: number }) {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left: number }>();
  const triggerRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);

  // Position the panel under the trigger, kept inside the viewport.
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const r = triggerRef.current?.getBoundingClientRect();
      if (!r) return;
      const w = Math.min(width, window.innerWidth - 16);
      const left = align === 'right' ? r.right - w : r.left;
      setPos({ top: r.bottom + 6, left: Math.max(8, Math.min(left, window.innerWidth - w - 8)) });
    };
    place();
    window.addEventListener('resize', place);
    return () => window.removeEventListener('resize', place);
  }, [open, align, width]);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!triggerRef.current?.contains(t) && !panelRef.current?.contains(t)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    window.addEventListener('mousedown', close);
    window.addEventListener('keydown', esc);
    return () => {
      window.removeEventListener('mousedown', close);
      window.removeEventListener('keydown', esc);
    };
  }, [open]);

  return (
    <>
      <div ref={triggerRef} onClick={() => setOpen((o) => !o)}>
        {trigger(open)}
      </div>
      {open &&
        pos &&
        createPortal(
          <div
            ref={panelRef}
            role="menu"
            className="modal"
            style={{ position: 'fixed', top: pos.top, left: pos.left, width: Math.min(width, window.innerWidth - 16), maxHeight: 420, padding: 6, zIndex: 60, animationDuration: '.12s' }}
          >
            <div style={{ overflow: 'auto' }}>
              {items.map((it) =>
                it.separator ? (
                  <div key={it.key} style={{ height: 1, background: 'var(--border)', margin: '6px 4px' }} />
                ) : (
                  <div
                    key={it.key}
                    role="menuitem"
                    className={`palette-item ${it.active ? 'on' : ''}`}
                    onClick={() => {
                      setOpen(false);
                      it.onSelect?.();
                    }}
                  >
                    {it.icon && <span className="pi-icon">{it.icon}</span>}
                    <span className="ellipsis">{it.label}</span>
                    {it.hint && <span className="pi-sub ellipsis">{it.hint}</span>}
                  </div>
                ),
              )}
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
