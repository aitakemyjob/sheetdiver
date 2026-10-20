import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./api', () => ({
  api: {
    translations: vi.fn(),
    saveTranslations: vi.fn(),
    translationSources: vi.fn(),
  },
}));

import { api } from './api';
import { resetEngine, setEngine, translateDatasheet, translateTexts } from './translate';

const mocked = vi.mocked(api);
const engine = { translate: vi.fn(async (r: { text: string }) => ({ target: { text: `EN(${r.text})` } })) };

beforeEach(() => {
  vi.clearAllMocks();
  resetEngine();
  setEngine(engine);
  mocked.translations.mockResolvedValue({ 已保存: 'Saved before' });
  mocked.saveTranslations.mockResolvedValue(null);
});

describe('translateTexts', () => {
  it('uses the glossary, then saved translations, then the engine', async () => {
    const out = await translateTexts(['极限参数 （注 1）', '已保存', 'BAT 脚电压', 'VIN 4.2V', '已保存'], 'zh');
    expect(out.get('极限参数 （注 1）')).toBe('Absolute maximum ratings (Note 1)');
    expect(out.get('已保存')).toBe('Saved before');
    // Terms the model gets wrong are swapped in before translating.
    expect(engine.translate).toHaveBeenCalledTimes(1);
    expect(engine.translate).toHaveBeenCalledWith({ from: 'zh', to: 'en', text: 'BAT 引脚电压', html: false });
    expect(out.get('BAT 脚电压')).toBe('EN(BAT 引脚电压)');
    // English text isn't translated.
    expect(out.has('VIN 4.2V')).toBe(false);
    // Saved for next time and for extraction: the engine's and the glossary's.
    expect(mocked.saveTranslations).toHaveBeenCalledWith([
      { lang: 'zh', source: '极限参数 （注 1）', text: 'Absolute maximum ratings (Note 1)' },
      { lang: 'zh', source: 'BAT 脚电压', text: 'EN(BAT 引脚电压)' },
    ]);
  });

  it("doesn't start the engine when nothing is left to translate", async () => {
    await translateTexts(['已保存', '电气特性'], 'zh');
    expect(engine.translate).not.toHaveBeenCalled();
  });

  it('still translates when saved translations are unavailable', async () => {
    mocked.translations.mockRejectedValue(new Error('offline'));
    const out = await translateTexts(['静止'], 'zh');
    expect(out.get('静止')).toBe('EN(静止)');
  });
});

describe('translateDatasheet', () => {
  it('translates every line in chunks and reports progress', async () => {
    const texts = Array.from({ length: 45 }, (_, i) => `第${i}行`);
    mocked.translationSources.mockResolvedValue({ lang: 'zh', texts });
    mocked.translations.mockResolvedValue({});
    const progress: [number, number][] = [];
    const n = await translateDatasheet(7, (d, t) => progress.push([d, t]));
    expect(n).toBe(45);
    expect(mocked.translationSources).toHaveBeenCalledWith(7);
    expect(progress).toEqual([
      [0, 45],
      [20, 45],
      [40, 45],
      [45, 45],
    ]);
    expect(engine.translate).toHaveBeenCalledTimes(45);
  });
});
