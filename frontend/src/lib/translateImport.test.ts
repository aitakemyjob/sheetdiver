import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./translate', async (orig) => ({
  ...(await orig<typeof import('./translate')>()),
  translateDatasheet: vi.fn(async () => 3),
}));

import { api } from './api';
import { useStore } from './store';
import { translateDatasheet } from './translate';
import type { ImportJob, TranslateStatus } from './types';

const job = (p: Partial<ImportJob>): ImportJob => ({ id: 1, name: 'tp4056.pdf', source: '', status: 'running', done: 0, total: 0, started: '', finished: '', ...p });
const model = (installed: boolean): TranslateStatus => ({ models: [{ from: 'zh', to: 'en', label: 'Chinese → English', version: '2.1', installed, downloading: false, sizeBytes: 1 }] });

/** Runs a job from running to done through the event stream. */
async function finish(p: Partial<ImportJob>) {
  const st = useStore.getState();
  st.handleEvent({ type: 'import', time: '', data: job({ ...p, status: 'running' }) });
  st.handleEvent({ type: 'import', time: '', data: job({ ...p, status: 'done' }) });
  await new Promise((r) => setTimeout(r, 0));
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(api, 'reextract').mockResolvedValue({} as never);
  useStore.setState({ jobs: {}, toasts: [] });
});

describe('importing a datasheet in another language', () => {
  it('translates it and re-extracts from the English when the model is installed', async () => {
    useStore.setState({ translation: model(true) });
    await finish({ id: 2, datasheetId: 9, language: 'zh', parts: ['TP4056'] });
    await vi.waitFor(() => expect(api.reextract).toHaveBeenCalledWith(9, true));
    expect(translateDatasheet).toHaveBeenCalledWith(9, expect.any(Function));
    expect(useStore.getState().toasts.at(-1)?.title).toBe('Translated TP4056');
  });

  it('says how to get the model when it is missing', async () => {
    useStore.setState({ translation: model(false) });
    await finish({ id: 3, datasheetId: 9, language: 'zh' });
    expect(translateDatasheet).not.toHaveBeenCalled();
    const t = useStore.getState().toasts.at(-1);
    expect(t?.title).toBe('tp4056.pdf is in Chinese');
    expect(t?.action?.label).toBe('Settings');
  });

  it('leaves English and already translated datasheets alone', async () => {
    useStore.setState({ translation: model(true) });
    await finish({ id: 4, datasheetId: 9, language: 'en' });
    await finish({ id: 5, datasheetId: 9, language: 'zh', translated: true });
    expect(translateDatasheet).not.toHaveBeenCalled();
  });

  it('reports a failed translation', async () => {
    useStore.setState({ translation: model(true) });
    vi.mocked(translateDatasheet).mockRejectedValueOnce(new Error('engine failed'));
    await finish({ id: 6, datasheetId: 9, language: 'zh' });
    await vi.waitFor(() => expect(useStore.getState().toasts.at(-1)?.title).toBe('Could not translate tp4056.pdf'));
    expect(api.reextract).not.toHaveBeenCalled();
  });
});

describe('translation model download events', () => {
  it('announces the result', () => {
    const st = useStore.getState;
    useStore.setState({ translation: { ...model(false), progress: { model: 'Chinese → English', done: 1, total: 2 } } });
    st().handleEvent({ type: 'translate', time: '', data: model(true) });
    expect(st().toasts.at(-1)?.title).toBe('Offline translation ready');
    useStore.setState({ translation: { ...model(false), progress: { model: 'x', done: 1, total: 2 } } });
    st().handleEvent({ type: 'translate', time: '', data: { ...model(false), error: 'checksum' } });
    expect(st().toasts.at(-1)?.title).toBe('Translation model download failed');
  });
});
