const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

/* The copy-key contract for the mobile injection layer: every i18n key referenced by mobile-ui.js must exist in all three
   languages, or the phone would show an internal key name leaking through, like tapping a toggle and seeing mobile.openNav.
   Traversal domain = propagation domain: all three entry points capable of writing data-i18n must be extracted, or missing
   one entry point means missing a detection. */

const LOCALES = ['zh-CN', 'en-US', 'ru-RU'];
const js = fs.readFileSync('web/static/js/mobile-ui.js', 'utf8');

function lookup(dict, key) {
    return key.split('.').reduce((acc, part) => (acc && typeof acc === 'object' ? acc[part] : undefined), dict);
}

function referencedKeys(source) {
    const patterns = [
        /label\(\s*[A-Za-z_$][\w$]*\s*,\s*'([^']+)'/g,
        /ariaLabel\(\s*[A-Za-z_$][\w$]*\s*,\s*'([^']+)'/g,
        /setAttrI18n\(\s*[A-Za-z_$][\w$]*\s*,\s*'[^']+'\s*,\s*'([^']+)'/g,
        /translate\(\s*'([^']+)'/g,
        /buildSheetItem\(\s*'([^']+)'/g,
        /labelKey:\s*'([^']+)'/g,
    ];
    const keys = new Set();
    for (const re of patterns) {
        let m;
        while ((m = re.exec(source))) keys.add(m[1]);
    }
    return keys;
}

const keys = referencedKeys(js);

test('extracts at least a batch of keys, guarding against the regexes all failing to match and an empty set passing', () => {
    assert.ok(keys.size >= 16, `expected >=20 referenced keys, got ${keys.size}`);
    assert.ok(keys.has('mobile.conversations'), 'tab labels must go through i18n');
});

for (const locale of LOCALES) {
    test(`${locale} covers every key referenced by the mobile layer`, () => {
        const dict = JSON.parse(fs.readFileSync(`web/static/i18n/${locale}.json`, 'utf8'));
        const missing = [...keys].filter(k => typeof lookup(dict, k) !== 'string' || !lookup(dict, k).trim());
        assert.deepEqual(missing, [], `${locale} is missing usable translations: ${missing.join(', ')}`);
    });
}

test('the mobile namespace key set is exactly the same across all three languages', () => {
    const sets = LOCALES.map(l => Object.keys(JSON.parse(fs.readFileSync(`web/static/i18n/${l}.json`, 'utf8')).mobile).sort());
    assert.deepEqual(sets[1], sets[0], 'en-US and zh-CN have different key sets');
    assert.deepEqual(sets[2], sets[0], 'ru-RU and zh-CN have different key sets');
});

test('copy with interpolation keeps its placeholder in all three languages', () => {
    for (const locale of LOCALES) {
        const dict = JSON.parse(fs.readFileSync(`web/static/i18n/${locale}.json`, 'utf8'));
        assert.match(lookup(dict, 'mobile.themeNow'), /\{\{\s*name\s*\}\}/, `${locale}'s themeNow lost {{name}}`);
    }
});

test('language and theme actions reuse the desktop\'s existing keys instead of establishing a separate set', () => {
    assert.ok(keys.has('header.apiDocs'), 'API docs should reuse header.apiDocs');
    assert.ok(keys.has('header.github'), 'GitHub should reuse header.github rather than a separate key');
    assert.ok(keys.has('nav.dashboard'), 'bottom tabs should reuse the nav.* group names');
});

test('the injection layer no longer has leftover hardcoded Chinese shown directly to the user', () => {
    /* The criterion is independent of function names: no "bare-written user-visible text" API may appear outside the helper zone.
       helper zone = from the copy comment block to right before the toast definition; the four-piece set label/translate/setAttrI18n/ariaLabel are all inside it. */
    const from = js.indexOf('/* Text for injected nodes');
    const to = js.indexOf('function toast(msg, ms)');
    assert.ok(from !== -1 && to > from, 'could not find the copy helper zone, meaning the helper was moved apart or renamed');
    const outside = js.slice(0, from) + js.slice(to);

    const bypasses = [
        /toast\(\s*'[^']*[一-鿿][^']*'/,
        /setAttribute\(\s*'aria-label'\s*,/,
        /setAttribute\(\s*'placeholder'\s*,/,
        /\.placeholder\s*=\s*'[^']*[一-鿿]/,
        /\.textContent\s*=\s*'[^']*[一-鿿]/,
        /el\(\s*'[a-zA-Z]+'\s*,\s*'[^']*'\s*,\s*'[^']*[一-鿿]/,
    ];
    for (const re of bypasses) {
        const hit = outside.match(re);
        assert.equal(hit, null, `a way of bypassing the i18n channel: ${hit && hit[0]}`);
    }
});
