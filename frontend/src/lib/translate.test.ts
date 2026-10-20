import { glossaryLookup, needsTranslation, prepare } from './translate';

describe('glossaryLookup', () => {
  it('maps headings to the names the extractor recognises', () => {
    expect(glossaryLookup('极限参数', 'zh')).toBe('Absolute maximum ratings');
    expect(glossaryLookup('极限参数 （注 1）', 'zh')).toBe('Absolute maximum ratings (Note 1)');
    expect(glossaryLookup('电气参数（注 2，3）', 'zh')).toBe('Electrical characteristics (Notes 2, 3)');
    expect(glossaryLookup('3.1 电气特性', 'zh')).toBe('3.1 Electrical characteristics');
    expect(glossaryLookup('二、管脚描述', 'zh')).toBe('Pin description');
    expect(glossaryLookup('管脚：', 'zh')).toBe('Pin configuration');
  });
  it('leaves other text to the model', () => {
    expect(glossaryLookup('TP4056是一款充电器', 'zh')).toBeUndefined();
    expect(glossaryLookup('极限参数很重要', 'zh')).toBeUndefined();
    expect(glossaryLookup('极限参数', 'ja')).toBeUndefined();
  });
});

describe('needsTranslation', () => {
  it('finds CJK text', () => {
    expect(needsTranslation('静态电流 40uA', 'zh')).toBe(true);
    expect(needsTranslation('VIN 4.2V', 'zh')).toBe(false);
    expect(needsTranslation('静态电流', 'en')).toBe(false);
  });
});

describe('prepare', () => {
  it('gives the model the terms it mistranslates', () => {
    expect(prepare('BAT 脚电压', 'zh')).toBe('BAT 引脚电压');
    expect(prepare('BAT 引脚电压', 'zh')).toBe('BAT 引脚电压');
    expect(prepare('VCC-VBAT 闭锁电压', 'zh')).toBe('VCC-VBAT lockout 电压');
    expect(prepare('涓流充电门限电压', 'zh')).toBe('trickle 充电 threshold 电压');
    expect(prepare('Hello', 'en')).toBe('Hello');
  });
});
