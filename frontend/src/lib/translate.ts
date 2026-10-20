// Offline translation of datasheet text into English. Mozilla's Bergamot
// engine runs here, in the UI, with models the backend downloaded on
// request and serves locally; nothing being translated leaves the
// machine. Translations are saved in the library, so each text is
// translated once.
import { api } from './api';
import { getTransport } from './transport';

/**
 * Datasheet terms the general-purpose model gets wrong, by exact text.
 * Headings use the English names the extractor recognises, so a
 * datasheet extracted from its translation gets its sections, tables and
 * parameters.
 */
export const glossary: Record<string, Record<string, string>> = {
  zh: {
    概述: 'Description',
    产品概述: 'Description',
    特点: 'Features',
    产品特点: 'Features',
    特性: 'Features',
    应用: 'Applications',
    应用领域: 'Applications',
    典型应用电路: 'Typical application circuit',
    典型应用: 'Typical application',
    管脚: 'Pin configuration',
    引脚: 'Pin configuration',
    管脚排列: 'Pin configuration',
    引脚排列: 'Pin configuration',
    管脚定义: 'Pin configuration',
    引脚定义: 'Pin configuration',
    管脚描述: 'Pin description',
    引脚描述: 'Pin description',
    管脚说明: 'Pin description',
    引脚说明: 'Pin description',
    管脚功能: 'Pin functions',
    引脚功能: 'Pin functions',
    定购信息: 'Ordering information',
    订购信息: 'Ordering information',
    订货信息: 'Ordering information',
    极限参数: 'Absolute maximum ratings',
    绝对最大额定值: 'Absolute maximum ratings',
    最大额定值: 'Absolute maximum ratings',
    极限额定值: 'Absolute maximum ratings',
    推荐工作条件: 'Recommended operating conditions',
    推荐工作范围: 'Recommended operating conditions',
    电气特性: 'Electrical characteristics',
    电特性: 'Electrical characteristics',
    电气参数: 'Electrical characteristics',
    电参数: 'Electrical characteristics',
    电性参数: 'Electrical characteristics',
    热特性: 'Thermal information',
    热阻: 'Thermal information',
    典型特性: 'Typical characteristics',
    典型特性曲线: 'Typical characteristics',
    典型性能特征: 'Typical characteristics',
    内部框图: 'Functional block diagram',
    功能框图: 'Functional block diagram',
    原理框图: 'Functional block diagram',
    框图: 'Block diagram',
    工作原理: 'Detailed description',
    功能描述: 'Detailed description',
    应用说明: 'Application information',
    应用信息: 'Application information',
    封装信息: 'Package information',
    封装尺寸: 'Package dimensions',
    封装外形尺寸: 'Package dimensions',
    封装: 'Package',
    静态电流: 'Quiescent current',
    待机电流: 'Standby current',
    关断电流: 'Shutdown current',
    输入电压: 'Input voltage',
    输入电源电压: 'Input supply voltage',
    输出电压: 'Output voltage',
    输出电流: 'Output current',
    充电电流: 'Charge current',
    导通电阻: 'On-resistance',
    开关频率: 'Switching frequency',
    参考电压: 'Reference voltage',
    反馈电压: 'Feedback voltage',
    工作温度: 'Operating temperature',
    工作环境温度: 'Operating temperature',
    存储温度: 'Storage temperature',
    储存温度: 'Storage temperature',
    结温: 'Junction temperature',
    最小值: 'Min',
    典型值: 'Typ',
    最大值: 'Max',
    最小: 'Min',
    典型: 'Typ',
    最大: 'Max',
    单位: 'Unit',
    参数: 'Parameter',
    参数名称: 'Parameter',
    符号: 'Symbol',
    额定值: 'Rating',
    数值: 'Value',
    测试条件: 'Test conditions',
    条件: 'Conditions',
    名称: 'Name',
    序号: 'No.',
    管脚号: 'Pin',
    引脚号: 'Pin',
    管脚名称: 'Name',
    引脚名称: 'Name',
    功能: 'Function',
    描述: 'Description',
    说明: 'Description',
  },
};

/**
 * Looks a text up in the glossary: exactly, or with a heading number in
 * front ("3. 电气特性"), a note after it ("极限参数 （注 1）") or a
 * trailing colon.
 */
export function glossaryLookup(text: string, lang: string): string | undefined {
  const terms = glossary[lang];
  if (!terms) return undefined;
  const m = /^((?:\d+(?:\.\d+)*\.?|[一二三四五六七八九十]+[、.．])\s*)?(.+?)\s*(?:[（(]\s*注\s*([\d\s,，、]+)[)）])?\s*[:：]?$/.exec(text.trim());
  if (!m || !terms[m[2]]) return undefined;
  const num = m[1] && /\d/.test(m[1]) ? m[1].trim() + ' ' : '';
  const notes = m[3] ? m[3].trim().split(/[\s,，、]+/).filter(Boolean) : [];
  return num + terms[m[2]] + (notes.length ? ` (Note${notes.length > 1 ? 's' : ''} ${notes.join(', ')})` : '');
}

/**
 * Datasheet terms the model mistranslates inside longer text ("闭锁" as
 * "Lock", "迟滞" as "retarded", "脚" as "foot"), replaced before
 * translating: with the usual Chinese word where the model knows it, or
 * with the English term, which the model keeps.
 */
export const preTerms: Record<string, [RegExp, string][]> = {
  zh: [
    [/(?<![引管])脚/g, '引脚'],
    [/闭锁/g, ' lockout '],
    [/门限|阈值/g, ' threshold '],
    [/迟滞|滞回/g, ' hysteresis '],
    [/涓流/g, ' trickle '],
    [/浮充/g, ' float '],
    [/翻转/g, ' trip '],
    [/再充电/g, ' recharge '],
    [/欠压/g, ' undervoltage '],
    [/过压/g, ' overvoltage '],
    [/上拉/g, ' pull-up '],
    [/下拉/g, ' pull-down '],
    [/导通电阻/g, ' on-resistance '],
  ],
};

/** Applies preTerms to a text before it goes to the model. */
export function prepare(text: string, lang: string): string {
  let out = text;
  for (const [re, to] of preTerms[lang] ?? []) out = out.replace(re, to);
  return out.replace(/\s{2,}/g, ' ').trim();
}

/** Reports whether a text has characters of a language worth translating. */
export function needsTranslation(text: string, lang: string): boolean {
  if (lang === 'zh' || lang === 'ja') return /[぀-ヿ㐀-鿿]/.test(text);
  if (lang === 'ko') return /[가-힯]/.test(text);
  return false;
}

/** The engine's translator (created once, on first use). */
export type Engine = {
  translate(r: { from: string; to: string; text: string; html: boolean }): Promise<{ target: { text: string } }>;
};
let engine: Promise<Engine> | undefined;

/** Starts (or reuses) the translation engine. */
function getEngine(): Promise<Engine> {
  if (!engine) {
    engine = (async () => {
      // The engine is served with the app (public/bergamot); its models come
      // from the backend, which only serves downloaded, checksummed files.
      const url = new URL('bergamot/translator.js', document.baseURI).href;
      const mod = await import(/* @vite-ignore */ url);
      const desktop = (await getTransport()).kind === 'desktop';
      return new mod.LatencyOptimisedTranslator({
        registryUrl: new URL(`api/translation/registry.json${desktop ? '' : '?sd=1'}`, document.baseURI).href,
        pivotLanguage: null,
        downloadTimeout: 0,
      }) as Engine;
    })();
    engine.catch(() => (engine = undefined));
  }
  return engine;
}

/** Forgets the engine (after a model is downloaded or removed). */
export function resetEngine() {
  engine = undefined;
}

/** Uses a given engine instead of loading Bergamot (tests). */
export function setEngine(e: Engine) {
  engine = Promise.resolve(e);
}

/**
 * Translates texts from lang into English: the glossary first, then saved
 * translations, then the engine for the rest (saved afterwards). Texts
 * without that language's characters come back unchanged.
 */
export async function translateTexts(texts: string[], lang: string): Promise<Map<string, string>> {
  const out = new Map<string, string>();
  const todo = [...new Set(texts.map((t) => t.trim()).filter((t) => t && needsTranslation(t, lang)))];
  // Glossary terms are saved too, so extraction from a translation sees them.
  const fresh = todo.flatMap((t) => {
    const en = glossaryLookup(t, lang);
    return en ? [{ lang, source: t, text: en }] : [];
  });
  fresh.forEach((f) => out.set(f.source, f.text));
  const rest = todo.filter((t) => !out.has(t));
  const saved = rest.length ? await api.translations(lang, rest).catch(() => ({}) as Record<string, string>) : {};
  const missing = rest.filter((t) => {
    if (saved[t]) out.set(t, saved[t]);
    return !saved[t];
  });
  if (missing.length) {
    const tr = await getEngine();
    for (const t of missing) {
      const r = await tr.translate({ from: lang, to: 'en', text: prepare(t, lang), html: false });
      out.set(t, r.target.text);
      fresh.push({ lang, source: t, text: r.target.text });
    }
  }
  if (fresh.length) await api.saveTranslations(fresh).catch(() => {});
  return out;
}

/**
 * Translates every line of a datasheet that isn't in English and saves
 * the translations, so it can be re-extracted from English. Reports
 * progress as lines done of total.
 */
export async function translateDatasheet(id: number, progress: (done: number, total: number) => void): Promise<number> {
  const { lang, texts } = await api.translationSources(id);
  const chunk = 20;
  for (let i = 0; i < texts.length; i += chunk) {
    progress(i, texts.length);
    await translateTexts(texts.slice(i, i + chunk), lang);
  }
  progress(texts.length, texts.length);
  return texts.length;
}
