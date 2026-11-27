import type {ScratchHost} from '../shared/scratch';
import {paintStrings as STRINGS} from './strings';

export function createLocale(Scratch: ScratchHost, store: () => any) {
    var lang = 'en';
    var isRtl = false;

    function normalizeLang (code: string) {
        const language = String(code || '').toLowerCase().split(/[-_]/)[0];
        return language in STRINGS ? language : 'en';
    }
    function t (key) {
        var dict = STRINGS[lang] || STRINGS.en;
        return dict[key] || STRINGS.en[key] || key;
    }
    function nameOf (item) {
        return lang === 'tr' ? item.label : item.en;
    }
    function readLocale () {
        var s = store();
        var loc = s ? (s.getState().locales || {}) : {};
        lang = normalizeLang(loc.locale || Scratch.translate.language || document.documentElement.lang || navigator.language);
        isRtl = !!loc.isRtl;
    }
    return {
        t,
        nameOf,
        readLocale,
        get lang() { return lang; },
        get isRtl() { return isRtl; },
    };
}
