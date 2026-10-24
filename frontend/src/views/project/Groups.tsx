import { useState } from 'react';
import { Plus, X } from 'lucide-react';
import type { Group, ProjectDetail } from '../../lib/types';
import { assignmentOf, change, endpoints, inferredGroup, inferredSupport } from './connections';

/**
 * The project's functional groups and where each part belongs: its group
 * (drawn framed on the block diagram, with the group's description and
 * notes) and the part it supports (drawn beside it).
 */
export function Groups({ d, onChange }: { d: ProjectDetail; onChange: () => void }) {
  const project = d.project.name;
  const groups = d.connections.groups;
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const all = endpoints(d);
  const assign = (part: string, field: 'group' | 'supports' | 'cluster', value: string) => {
    change(project, { assign: [{ part, [field]: value }] }, onChange, assignedMessage(part, field, value));
  };
  return (
    <div className="card col" style={{ gap: 8 }}>
      <h3 style={{ margin: 0 }}>Groups and support</h3>
      <div className="muted">
        Put parts in functional groups (Power, MCU, Sensors) to draw each group framed, with its description and notes; wires stay inside a group, and a net that goes
        to another group ends in its name. Say which part a passive supports (a decoupling capacitor and its IC) to draw it beside that part; left unset, a passive
        is guessed from its nets: the IC it feeds, or for a protection diode, its connector (shown as auto). Give parts the same subgroup name (VDD for the
        decoupling, BTN for a button and its pull-up) to lay them out and wire them up as one unit, framed inside the group; parts hanging off one net are
        already drawn so together.
      </div>
      {groups.map((g) => (
        <GroupRow key={g.id} g={g} others={groups.filter((o) => o.id !== g.id)} project={project} onChange={onChange} />
      ))}
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) change(project, { groups: [{ name: name.trim(), description: description.trim() }] }, onChange);
          setName('');
          setDescription('');
        }}
      >
        <input className="input" style={{ width: 140 }} placeholder="Power, MCU…" aria-label="New group" value={name} onChange={(e) => setName(e.target.value)} />
        <input className="input grow" placeholder="What it does" aria-label="New group's description" value={description} onChange={(e) => setDescription(e.target.value)} />
        <button className="btn sm" type="submit" disabled={!name.trim()}>
          <Plus size={13} /> Group
        </button>
      </form>
      {all.length > 0 && (
        <table className="dtable" aria-label="Groups and support">
          <thead>
            <tr>
              <th>Part</th>
              <th>Group</th>
              <th>Supports</th>
              <th>Subgroup</th>
            </tr>
          </thead>
          <tbody>
            {all.map((part) => {
              const a = assignmentOf(d, part);
              const auto = inferredSupport(d, part);
              const autoGroup = a?.group ? '' : inferredGroup(d, part);
              return (
                <tr key={part}>
                  <td className="mono">{part}</td>
                  <td>
                    <select className="input" aria-label={`${part}'s group`} value={a?.group ?? ''} onChange={(e) => assign(part, 'group', e.target.value)}>
                      <option value="">{autoGroup ? `auto (${autoGroup})` : 'none'}</option>
                      {groups.map((g) => (
                        <option key={g.id} value={g.name}>
                          {g.name}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td>
                    <select className="input" aria-label={`What ${part} supports`} value={a?.supportsEndpoint ?? ''} onChange={(e) => assign(part, 'supports', e.target.value)}>
                      <option value="">{auto ? `auto (${auto})` : 'none'}</option>
                      {all
                        .filter((o) => o !== part)
                        .map((o) => (
                          <option key={o} value={o}>
                            {o}
                          </option>
                        ))}
                    </select>
                  </td>
                  <td>
                    <ClusterInput part={part} value={a?.cluster ?? ''} onChange={(value) => assign(part, 'cluster', value)} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}

/**
 * A group: its name, its description and notes, saved when edited, and
 * the group it's drawn below (none: a column of its own).
 */
function GroupRow({ g, others, project, onChange }: { g: Group; others: Group[]; project: string; onChange: () => void }) {
  const save = (field: 'description' | 'notes', value: string) => {
    if (value.trim() !== (g[field] ?? '')) change(project, { groups: [{ name: g.name, [field]: value.trim() }] }, onChange);
  };
  const place = (below: string) => change(project, { groups: [{ name: g.name, below }] }, onChange, below ? `${g.name} is drawn below ${below}` : `${g.name} is drawn in a column of its own`);
  return (
    <div className="row" data-group={g.name}>
      <b style={{ width: 140 }}>{g.name}</b>
      <input className="input grow" placeholder="What it does" aria-label={`${g.name}'s description`} defaultValue={g.description ?? ''} onBlur={(e) => save('description', e.target.value)} />
      <input className="input grow" placeholder="Notes" aria-label={`${g.name}'s notes`} defaultValue={g.notes ?? ''} onBlur={(e) => save('notes', e.target.value)} />
      <select className="input" style={{ width: 150 }} aria-label={`What ${g.name} is drawn below`} title="Draw this group under another, in its column" value={g.below ?? ''} onChange={(e) => place(e.target.value)}>
        <option value="">its own column</option>
        {others.map((o) => (
          <option key={o.id} value={o.name}>
            below {o.name}
          </option>
        ))}
      </select>
      <button className="btn ghost icon sm" title={`Remove ${g.name} (its parts are drawn ungrouped)`} onClick={() => change(project, { groups: [{ name: g.name, remove: true }] }, onChange)}>
        <X size={11} />
      </button>
    </div>
  );
}

/** What a toast says once an assignment field is set or cleared. */
function assignedMessage(part: string, field: 'group' | 'supports' | 'cluster', value: string): string {
  switch (field) {
    case 'group':
      return value ? `${part} is in ${value}` : `${part} is in no group`;
    case 'supports':
      return value ? `${part} supports ${value}` : `${part} no longer has a set support: it's guessed from its nets`;
    default:
      return value ? `${part} is wired up in ${value}` : `${part} is in no subgroup`;
  }
}

/** A part's subgroup name, sent when it's left or entered. */
function ClusterInput({ part, value, onChange }: { part: string; value: string; onChange: (value: string) => void }) {
  const [draft, setDraft] = useState(value);
  const [was, setWas] = useState(value);
  if (was !== value) {
    setWas(value);
    setDraft(value);
  }
  const commit = () => {
    if (draft.trim() !== value) onChange(draft.trim());
  };
  return (
    <input
      className="input"
      style={{ width: 110 }}
      placeholder="none"
      aria-label={`${part}'s subgroup`}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit();
      }}
    />
  );
}
