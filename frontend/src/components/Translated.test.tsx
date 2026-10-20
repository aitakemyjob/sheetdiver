import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../lib/translate', async (orig) => ({
  ...(await orig<typeof import('../lib/translate')>()),
  translateTexts: vi.fn(),
}));

import { translateTexts } from '../lib/translate';
import { SectionText } from './SectionText';
import { T, TranslateProvider } from './Translated';

const mocked = vi.mocked(translateTexts);

beforeEach(() => {
  mocked.mockReset();
  mocked.mockImplementation(async (texts: string[]) => new Map(texts.map((t) => [t.trim(), `EN:${t.trim()}`])));
});

describe('TranslateProvider', () => {
  it('shows the original until translation is on', async () => {
    const { rerender } = render(
      <TranslateProvider lang="zh" on={false}>
        <T text="静态电流" />
      </TranslateProvider>,
    );
    expect(screen.getByText('静态电流')).toBeInTheDocument();
    expect(mocked).not.toHaveBeenCalled();
    rerender(
      <TranslateProvider lang="zh" on>
        <T text="静态电流" />
      </TranslateProvider>,
    );
    // The original shows on hover.
    await waitFor(() => expect(screen.getByText('EN:静态电流')).toHaveAttribute('title', '静态电流'));
  });

  it('batches requests and skips English', async () => {
    render(
      <TranslateProvider lang="zh" on>
        <T text="特点" />
        <T text="应用" />
        <T text="VIN" />
      </TranslateProvider>,
    );
    await waitFor(() => expect(screen.getByText('EN:应用')).toBeInTheDocument());
    expect(mocked).toHaveBeenCalledTimes(1);
    expect(mocked.mock.calls[0][0]).toEqual(['特点', '应用']);
    expect(screen.getByText('VIN')).toBeInTheDocument();
  });

  it('translates formatted section text piece by piece', async () => {
    render(
      <TranslateProvider lang="zh" on>
        <SectionText text={'### 充电终止\n\n• 待机电流\n\n| 符号 | 参数 |\n| --- | --- |\n| ICC | 电源电流 |\n\n[Figure: 内部框图]'} />
      </TranslateProvider>,
    );
    await waitFor(() => expect(screen.getByRole('heading', { name: 'EN:充电终止' })).toBeInTheDocument());
    expect(screen.getByRole('listitem')).toHaveTextContent('EN:待机电流');
    expect(screen.getByRole('columnheader', { name: 'EN:符号' })).toBeInTheDocument();
    expect(screen.getByRole('cell', { name: 'EN:电源电流' })).toBeInTheDocument();
    expect(screen.getByText('Figure: EN:内部框图')).toBeInTheDocument();
    // Markup never goes to the model.
    const asked = mocked.mock.calls.flatMap((c) => c[0]);
    expect(asked.some((t) => /[|#•[]/.test(t))).toBe(false);
  });

  it('reports failures and asks again later', async () => {
    mocked.mockRejectedValueOnce(new Error('model missing'));
    const onError = vi.fn();
    render(
      <TranslateProvider lang="zh" on onError={onError}>
        <T text="静态电流" />
      </TranslateProvider>,
    );
    await waitFor(() => expect(onError).toHaveBeenCalledWith(new Error('model missing')));
    expect(screen.getByText('静态电流')).toBeInTheDocument();
  });
});
