import { useState, type MouseEvent } from 'react';
import { Bot, ChevronRight } from 'lucide-react';
import { encodeAgentLinks, openAgentLink } from '../lib/agentLinks';
import { Markdown } from './ui';

/**
 * The agent's explanation beside what it showed: markdown whose links to
 * parts, pages, figures, sections and projects open them. It can be
 * folded away.
 */
export function AgentPanel({ explain }: { explain: string }) {
  const [open, setOpen] = useState(true);
  const onClick = (e: MouseEvent) => {
    const a = (e.target as HTMLElement).closest('a');
    const href = a?.getAttribute('href') ?? '';
    if (!href.startsWith('#')) return;
    e.preventDefault();
    void openAgentLink(href);
  };
  if (!open) {
    return (
      <button className="agent-panel-tab" title="Show the agent's explanation" onClick={() => setOpen(true)}>
        <Bot size={14} />
      </button>
    );
  }
  return (
    <aside className="agent-panel" aria-label="Agent's explanation">
      <div className="row agent-panel-head">
        <Bot size={14} /> <b className="grow">From your agent</b>
        <button className="btn ghost icon sm" title="Hide" onClick={() => setOpen(false)}>
          <ChevronRight size={14} />
        </button>
      </div>
      <div onClick={onClick}>
        <Markdown src={encodeAgentLinks(explain)} />
      </div>
    </aside>
  );
}
