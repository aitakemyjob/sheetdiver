import { closeTab, focusToTab, openTab, tabKey, type Tab } from './tabs';

describe('tabs', () => {
  it('reuses a tab for the same part regardless of case', () => {
    let { tabs } = openTab([], { kind: 'part', title: 'TPS563200', params: { part: 'TPS563200' } });
    ({ tabs } = openTab(tabs, { kind: 'part', title: 'tps563200', params: { part: 'tps563200', tab: 'figures' } }));
    expect(tabs).toHaveLength(1);
    expect(tabs[0].params.tab).toBe('figures');
  });

  it('keeps one page tab per datasheet and moves it to the new page', () => {
    const first = openTab([], { kind: 'page', title: 'p1', params: { ds: 3, page: 0 } });
    const { tabs, active } = openTab(first.tabs, { kind: 'page', title: 'p7', params: { ds: 3, page: 6 } });
    expect(tabs).toHaveLength(1);
    expect(active).toBe(tabKey('page', { ds: 3 }));
    expect(tabs[0].params.page).toBe(6);
  });

  it('keeps one tab per component type', () => {
    let { tabs } = openTab([], { kind: 'category', title: 'Buck', params: { category: 'buck' } });
    ({ tabs } = openTab(tabs, { kind: 'category', title: 'Buck', params: { category: 'buck', highlight: 'X' } }));
    ({ tabs } = openTab(tabs, { kind: 'category', title: 'LDO', params: { category: 'ldo' } }));
    expect(tabs.map((t) => t.id)).toEqual(['category:buck', 'category:ldo']);
  });

  it('activates a neighbour when closing the active tab', () => {
    const tabs: Tab[] = [
      { id: 'a', kind: 'search', title: 'a', params: {} },
      { id: 'b', kind: 'settings', title: 'b', params: {} },
    ];
    expect(closeTab(tabs, 'b', 'b').active).toBe('a');
    expect(closeTab(tabs, 'a', 'b').active).toBe('b');
  });

  it('maps agent focus requests to tabs', () => {
    expect(focusToTab({ kind: 'figure', figureId: 9 })?.params.id).toBe(9);
    expect(focusToTab({ kind: 'page', datasheetId: 2, page: 4, part: 'X' })?.title).toBe('X p5');
    expect(focusToTab({ kind: 'part' })).toBeUndefined();
    expect(focusToTab({ kind: 'category', query: 'buck' })?.params.category).toBe('buck');
    expect(focusToTab({ kind: 'category' })).toBeUndefined();
  });
});
