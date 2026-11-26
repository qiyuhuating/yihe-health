const crypto = require('node:crypto');
const load = require('./load-ets.cjs');

// SDK-boundary adapter uses real Node AES-256-GCM. It does not prove HUKS on a device.
function cryptoBoundary() {
  const keys = new Map();
  const sessions = new Map();
  const calls = [];
  let next = 0;
  const tags = Object.fromEntries(['ALGORITHM', 'KEY_SIZE', 'PURPOSE', 'PADDING', 'BLOCK_MODE', 'NONCE', 'ASSOCIATED_DATA', 'AE_TAG'].map(tag => ['HUKS_TAG_' + tag, tag]));
  const huks = {
    HuksTag: tags, HuksKeyAlg: { HUKS_ALG_AES: 'AES' }, HuksKeySize: { HUKS_AES_KEY_SIZE_256: 256 },
    HuksKeyPurpose: { HUKS_KEY_PURPOSE_ENCRYPT: 1, HUKS_KEY_PURPOSE_DECRYPT: 2 },
    HuksKeyPadding: { HUKS_PADDING_NONE: 'NONE' }, HuksCipherMode: { HUKS_MODE_GCM: 'GCM' },
    isKeyItemExist: async alias => keys.has(alias),
    generateKeyItem: async (alias, options) => {
      assertOptions(options); if (keys.has(alias)) throw new Error('Alias already exists');
      keys.set(alias, crypto.randomBytes(32)); calls.push('generate');
    },
    initSession: async (alias, options) => {
      assertOptions(options);
      const p = Object.fromEntries(options.properties.map(item => [item.tag, item.value]));
      const encrypt = p.PURPOSE === 1;
      const cipher = encrypt ? crypto.createCipheriv('aes-256-gcm', keys.get(alias), p.NONCE) : crypto.createDecipheriv('aes-256-gcm', keys.get(alias), p.NONCE);
      cipher.setAAD(p.ASSOCIATED_DATA);
      if (!encrypt) cipher.setAuthTag(p.AE_TAG);
      const handle = ++next; sessions.set(handle, { cipher, encrypt }); calls.push('init'); return { handle };
    },
    updateSession: async (handle, options) => {
      if (options.inData.length > 16384) throw new Error('Oversized HUKS chunk');
      return { outData: new Uint8Array(sessions.get(handle).cipher.update(options.inData)) };
    },
    finishSession: async handle => {
      const session = sessions.get(handle); const final = session.cipher.final();
      const output = session.encrypt ? Buffer.concat([final, session.cipher.getAuthTag()]) : final;
      sessions.delete(handle); calls.push('finish'); return output.length ? { outData: new Uint8Array(output) } : null;
    },
    abortSession: async handle => { sessions.delete(handle); calls.push('abort'); }
  };
  function assertOptions(options) {
    const p = Object.fromEntries(options.properties.map(item => [item.tag, item.value]));
    if (p.ALGORITHM !== 'AES' || p.KEY_SIZE !== 256 || p.BLOCK_MODE !== 'GCM' || p.PADDING !== 'NONE') throw new Error('Unexpected crypto configuration');
  }
  class Encoder { encodeInto(text) { return new Uint8Array(Buffer.from(text, 'utf8')); } }
  class Decoder { static create(_encoding, options) { const decoder = new TextDecoder('utf-8', options); return { decodeToString: data => decoder.decode(data) }; } }
  class Base64 { encodeToStringSync(data) { return Buffer.from(data).toString('base64'); } decodeSync(text) { return new Uint8Array(Buffer.from(text, 'base64')); } }
  return { keys, calls, sessions, mocks: {
    '@kit.UniversalKeystoreKit': { huks }, '@kit.CryptoArchitectureKit': { cryptoFramework: { createRandom: () => ({ generateRandom: async size => ({ data: new Uint8Array(crypto.randomBytes(size)) }) }) } },
    '@kit.ArkTS': { util: { TextEncoder: Encoder, TextDecoder: Decoder, Base64Helper: Base64 } }, '@kit.BasicServicesKit': {}
  } };
}

function storeFixture(values = {}, boundary = cryptoBoundary()) {
  let onFlush = async () => {};
  const prefs = { hasSync: key => Object.hasOwn(values, key), getSync: (key, fallback) => values[key] ?? fallback, putSync: (key, value) => { values[key] = value; },
    deleteSync: key => { delete values[key]; }, flush: () => onFlush() };
  const { WatchStore } = load('services/WatchStore', { ...boundary.mocks, '@kit.ArkData': { preferences: { getPreferencesSync: () => prefs } } });
  return { values, boundary, WatchStore, prefs, setFlush: callback => { onFlush = callback; }, open: () => WatchStore.open({}) };
}

module.exports = { cryptoBoundary, storeFixture };
