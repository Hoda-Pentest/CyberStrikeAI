const fs = require('node:fs');
const test = require('node:test');
const assert = require('node:assert/strict');

const chat = fs.readFileSync('web/static/js/chat.js', 'utf8');
const hitl = fs.readFileSync('web/static/js/hitl.js', 'utf8');

function functionSource(source, name, nextName) {
    const start = source.indexOf(`function ${name}(`);
    const end = source.indexOf(`function ${nextName}(`, start);
    assert.notEqual(start, -1, `${name} should exist`);
    assert.notEqual(end, -1, `${nextName} should follow ${name}`);
    return source.slice(start, end);
}

test('an existing conversation without local config does not inherit the latest approval settings of other conversations', () => {
    const source = functionSource(chat, 'getHitlConfigForConversation', 'setHitlReviewerUI');
    const existingConversationBranch = source.slice(source.indexOf('const key = getHitlStorageKeyByConversation(cid)'));

    assert.doesNotMatch(existingConversationBranch, /getHitlLastGlobalConfig/);
    assert.match(existingConversationBranch, /if \(!raw\) \{\s*return fallback;/);
    assert.match(existingConversationBranch, /catch \(e\) \{\s*return fallback;/);
});

test('the server default reviewer only updates the default value and does not override the latest conversation choice', () => {
    const source = functionSource(hitl, 'applyHitlDefaultReviewerFromServer', 'fetchHitlDefaultReviewer');

    assert.match(source, /return applyHitlDefaultConfigFromServer\(\{ defaultReviewer: reviewer \}\)/);
    const configSource = functionSource(hitl, 'applyHitlDefaultConfigFromServer', 'fetchHitlDefaultConfig');
    assert.match(configSource, /const reviewer = hitlReviewerNormalize\(src\.defaultReviewer \|\| src\.reviewer \|\| 'human'\)/);
    assert.match(configSource, /window\.csaiHitlDefaultReviewer = reviewer/);
    assert.doesNotMatch(source, /saveHitlLastGlobalConfig/);
    assert.doesNotMatch(configSource, /saveHitlLastGlobalConfig|saveHitlConversationConfig/);
});

test('restoring a conversation approval config keeps that conversation own reviewer', () => {
    const source = functionSource(hitl, 'syncHitlConfigFromServer', 'syncHitlConfigToServerByCurrentConversation');

    assert.match(source, /const localReviewer = hitlReviewerNormalize\(local && local\.reviewer\)/);
    assert.match(source, /merged = \{[\s\S]*?reviewer: localReviewer/);
    assert.match(source, /saveHitlConversationConfig\(conversationId, \{[\s\S]*?reviewer: localReviewer/);
    assert.doesNotMatch(source, /getHitlLastGlobalConfig/);
});

test('async sync may only refresh the approval UI that still belongs to the current conversation', () => {
    const source = functionSource(hitl, 'syncHitlConfigFromServer', 'syncHitlConfigToServerByCurrentConversation');

    assert.match(source, /getCurrentConversationIdForHitl\(\) === conversationId[\s\S]*?window\.applyHitlConfigToUI\(normalizedCfg\)/);
});
