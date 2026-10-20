import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../lib/translate', async (orig) => ({
  ...(await orig<typeof import('../lib/translate')>()),
  translateDatasheet: vi.fn(async (_id: number, progress: (d: number, t: number) => void) => {
    progress(20, 40);
    return 40;
  }),
}));

import { api } from '../lib/api';
import { useStore } from '../lib/store';
import { translateDatasheet } from '../lib/translate';
import type { Datasheet, TranslateStatus } from '../lib/types';
import { ReextractDialog } from './ReextractDialog';
import { TranslationSettings } from './TranslationSettings';

const model = (installed: boolean, extra: Partial<TranslateStatus> = {}): TranslateStatus => ({
  models: [{ from: 'zh', to: 'en', label: 'Chinese → English', version: '2.1', installed, downloading: false, sizeBytes: 54556676 }],
  ...extra,
});
const ds = { id: 4, filename: 'tp4056.pdf' } as Datasheet;

beforeEach(() => {
  vi.clearAllMocks();
  useStore.setState({ toasts: [] });
});

describe('TranslationSettings', () => {
  it('downloads and removes a model', async () => {
    useStore.setState({ translation: model(false) });
    const download = vi.spyOn(api, 'translateDownload').mockResolvedValue(model(false, { progress: { model: 'Chinese → English', done: 0, total: 1 } }));
    const { rerender } = render(<TranslationSettings />);
    expect(screen.getByText(/Not downloaded \(52 MB\)/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Download/ }));
    await waitFor(() => expect(download).toHaveBeenCalledWith('zh', 'en'));
    useStore.setState({ translation: model(false, { progress: { model: 'Chinese → English', done: 26 << 20, total: 52 << 20 } }) });
    rerender(<TranslationSettings />);
    expect(screen.getByText(/Downloading Chinese → English… 26 MB of 52 MB/)).toBeInTheDocument();
    useStore.setState({ translation: model(true) });
    rerender(<TranslationSettings />);
    const remove = vi.spyOn(api, 'translateRemove').mockResolvedValue(model(false));
    fireEvent.click(screen.getByRole('button', { name: /Remove/ }));
    await waitFor(() => expect(remove).toHaveBeenCalledWith('zh', 'en'));
  });
});

describe('ReextractDialog', () => {
  it('translates first, then re-extracts from the English', async () => {
    useStore.setState({ translation: model(true) });
    const reextract = vi.spyOn(api, 'reextract').mockResolvedValue({} as never);
    const onDone = vi.fn();
    render(<ReextractDialog ds={ds} lang="zh" onClose={() => {}} onDone={onDone} />);
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true');
    fireEvent.click(screen.getByRole('button', { name: /Re-extract/ }));
    await waitFor(() => expect(onDone).toHaveBeenCalledWith(true));
    expect(translateDatasheet).toHaveBeenCalledWith(4, expect.any(Function));
    expect(reextract).toHaveBeenCalledWith(4, true);
  });

  it('re-extracts as is without the model', async () => {
    useStore.setState({ translation: model(false) });
    const reextract = vi.spyOn(api, 'reextract').mockResolvedValue({} as never);
    const onDone = vi.fn();
    render(<ReextractDialog ds={ds} lang="zh" onClose={() => {}} onDone={onDone} />);
    expect(screen.getByRole('switch')).toBeDisabled();
    expect(screen.getByText('download it in Settings')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Re-extract/ }));
    await waitFor(() => expect(onDone).toHaveBeenCalledWith(false));
    expect(translateDatasheet).not.toHaveBeenCalled();
    expect(reextract).toHaveBeenCalledWith(4, false);
  });
});
