const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = process.cwd();
const settings = fs.readFileSync(path.join(root, 'web/static/js/settings.js'), 'utf8');
const template = fs.readFileSync(path.join(root, 'web/templates/index.html'), 'utf8');
const zh = JSON.parse(fs.readFileSync(path.join(root, 'web/static/i18n/zh-CN.json'), 'utf8'));
const en = JSON.parse(fs.readFileSync(path.join(root, 'web/static/i18n/en-US.json'), 'utf8'));

test('Pi model retry/failover settings page read/write path is complete', () => {
    [
        'pi-model-retry-max-retries',
        'pi-model-retry-max-backoff-sec',
        'pi-model-failover-channels',
        'pi-model-failover-max-retries'
    ].forEach((id) => {
        assert.match(template, new RegExp(`id="${id}"`));
        assert.match(settings, new RegExp(`getElementById\\('${id}'\\)`));
    });

    [
        'model_retry_max_retries',
        'model_retry_max_backoff_sec',
        'model_failover_channels',
        'model_failover_max_retries'
    ].forEach((key) => {
        assert.match(settings, new RegExp(`${key}`));
    });

    assert.match(settings, /failoverChannelsRaw\.split\(\s*\/\[\\n,，\]\//);
    assert.match(settings, /Array\.from\(new Set\(/);
});

test('Pi model retry/failover settings have Chinese and English text', () => {
    [
        'piModelRetryMaxRetries',
        'piModelRetryMaxRetriesHint',
        'piModelRetryMaxBackoffSec',
        'piModelRetryMaxBackoffSecHint',
        'piModelFailoverChannels',
        'piModelFailoverChannelsPlaceholder',
        'piModelFailoverChannelsHint',
        'piModelFailoverMaxRetries',
        'piModelFailoverMaxRetriesHint'
    ].forEach((key) => {
        assert.equal(typeof zh.settingsBasic[key], 'string', `zh ${key}`);
        assert.ok(zh.settingsBasic[key].length > 0, `zh ${key} is empty`);
        assert.equal(typeof en.settingsBasic[key], 'string', `en ${key}`);
        assert.ok(en.settingsBasic[key].length > 0, `en ${key} is empty`);
    });
});

test('AI channel auto-detects the official DeepSeek route before saving', () => {
    assert.match(settings, /function\s+isOfficialDeepSeekBaseURL/);
    assert.match(settings, /api\.deepseek\.com/);
    assert.match(settings, /profile:\s*'deepseek'/);
    assert.match(settings, /normalizeAIChannelProviderProfile\(\{/);
    assert.match(settings, /normalizeAIConfigProviderProfiles\(currentConfig\.ai\)/);
    assert.match(template, /<option value="deepseek">deepseek<\/option>/);
});

test('switching or adding an AI channel refreshes the reasoning route dropdown', () => {
    assert.match(settings, /const profileEl = document\.getElementById\('openai-reasoning-profile'\);/);
    assert.match(settings, /syncSettingsCustomSelect\(profileEl\);/);
    assert.match(settings, /reasoning:\s*\{\s*mode:\s*'auto',\s*effort:\s*'',\s*profile:\s*'auto'/);
});

test('saving and probing keep existing config after the context profile is hidden', async () => {
    assert.doesNotMatch(template, /openai-context-profile|settingsBasic\.contextProfile/);
    const formCode = settings.slice(settings.indexOf('function normalizeAIChannelId('), settings.indexOf('function writeAIChannelToMainForm('));
    const probeCode = settings.slice(settings.indexOf('async function detectAIContext('), settings.indexOf('// Save tool config (standalone function'));
    const fields = {
        'openai-provider': 'openai_compatible',
        'openai-base-url': 'https://gateway.example/v1',
        'openai-api-key': 'fixture-key',
        'openai-model': 'deepseek-flash'
    };
    for (const profile of [undefined, 'local', 'local_auto', 'economy', 'custom']) {
        const requests = [];
        const context = {
            URL,
            document: { getElementById: id => Object.hasOwn(fields, id) ? { value: fields[id] } : null },
            currentConfig: { ai: { channels: { main: { context_profile: profile, provider: fields['openai-provider'], base_url: fields['openai-base-url'], model: fields['openai-model'] } } } },
            selectedAIChannelId: 'main',
            aiChannelProbeResults: {},
            window: {},
            settingsT: (_, fallback) => fallback,
            syncSelectedAIChannelUI() {},
            updateAIChannelSelectOption() {},
            setConnectionTestResult() {},
            applyDetectedContext() {},
            apiFetch: async (url, options) => {
                requests.push(JSON.parse(options.body));
                return { json: async () => ({ success: true, context_window: 131072 }) };
            }
        };
        vm.createContext(context);
        vm.runInContext(formCode + '\n' + probeCode, context);
        const saved = context.readAIChannelFromMainForm('main');
        assert.equal(saved.context_profile, profile);
        await context.detectAIContext();
        await context.testOpenAIConnection();
        assert.equal(requests.length, 2);
        for (const request of requests) assert.equal(request.context_profile, profile || '');
    }
});

test('saving a channel auto-detects local services, and switching service clears the auto-detection', async () => {
    const formCode = settings.slice(settings.indexOf('function normalizeAIChannelId('), settings.indexOf('function writeAIChannelToMainForm('));
    const detectionCode = settings.slice(settings.indexOf('async function detectChannelContextForSave('), settings.indexOf('async function persistAIConfigOnlyToServer('));
    const fields = { 'openai-provider': 'openai_compatible', 'openai-base-url': 'http://127.0.0.1:8000/v1', 'openai-model': 'local-model' };
    let probes = 0;
    let result = { success: true, context_profile: 'local_auto', context_window: 65536 };
    const context = {
        URL, AbortSignal,
        document: { getElementById: id => Object.hasOwn(fields, id) ? { value: fields[id] } : null },
        currentConfig: { ai: { channels: {} } },
        apiFetch: async () => {
            probes++;
            return { ok: true, json: async () => result };
        }
    };
    vm.createContext(context);
    vm.runInContext(formCode + '\n' + detectionCode, context);
    const channel = context.readAIChannelFromMainForm('main');
    await context.detectChannelContextForSave('main', channel);
    assert.equal(channel.context_profile, 'local_auto');
    assert.equal(channel.context_window, 65536);
    assert.equal(probes, 1);
    context.currentConfig.ai.channels.main = channel;
    fields['openai-base-url'] = 'https://gateway.example/v1';
    const changed = context.readAIChannelFromMainForm('main');
    assert.equal(changed.context_profile, '');

    for (const profile of ['local', 'custom', 'economy']) {
        const manual = { ...channel, context_profile: profile };
        await context.detectChannelContextForSave('main', manual);
        assert.equal(manual.context_profile, profile);
    }
    assert.equal(probes, 1);
    const manualWindow = { ...changed, context_window: 16384 };
    await context.detectChannelContextForSave('main', manualWindow);
    assert.equal(manualWindow.context_window, 16384);
    const oversizedWindow = { ...changed, context_window: 131072 };
    await context.detectChannelContextForSave('main', oversizedWindow);
    assert.equal(oversizedWindow.context_window, 65536);
    result = { success: true, context_profile: 'local_auto', context_window: 262144, context_runtime_unknown: true };
    const unknownRuntime = { ...changed, context_window: 0 };
    await context.detectChannelContextForSave('main', unknownRuntime);
    assert.equal(unknownRuntime.context_profile, 'local_auto');
    assert.equal(unknownRuntime.context_window, 0);

    context.apiFetch = async () => { throw new Error('offline'); };
    const offline = { ...changed };
    await context.detectChannelContextForSave('main', offline);
    assert.equal(offline.context_profile, '');
});
