// What the project's connection tabs share.
import { api } from '../../lib/api';
import { useStore } from '../../lib/store';
import type { ConnectionsChange } from '../../lib/types';

/** Applies a change to the connection table, telling the user what couldn't be done. */
export async function change(project: string, ch: ConnectionsChange, done: () => void) {
  const s = useStore.getState();
  try {
    const { problems } = await api.changeConnections(project, ch);
    if (problems.length) s.toast({ kind: 'warn', title: 'Not everything was applied', body: problems.join('\n') });
  } catch (e) {
    s.toast({ kind: 'err', title: 'Could not change the connections', body: (e as Error).message });
  }
  done();
}

/** Whether the agent's highlight names this node or link. */
export function marked(hl: string | undefined, ...names: (string | undefined)[]): boolean {
  return !!hl && names.some((n) => n?.toLowerCase() === hl);
}
