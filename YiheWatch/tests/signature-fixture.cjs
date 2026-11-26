const assert = require('node:assert/strict');
const crypto = require('node:crypto');

// Only the CryptoArchitectureKit boundary is replaced. No signing algorithm or
// canonicalization is reimplemented in the production service for these tests.
function signatureBoundary() {
  const state = { random: undefined, beforeRandom: undefined, fail: '', imported: [], cleared: 0 };
  const framework = {
    createRandom: () => ({ generateRandom: async size => {
      if (state.beforeRandom) await state.beforeRandom;
      if (state.fail === 'random') throw new Error('secure RNG unavailable');
      return { data: new Uint8Array(state.random || crypto.randomBytes(size)) };
    } }),
    createMd: algorithm => {
      assert.equal(algorithm, 'SHA256'); const hash = crypto.createHash('sha256');
      return { update: async ({ data }) => { assert.ok(data.length <= 16384); hash.update(data); },
        digest: async () => ({ data: new Uint8Array(hash.digest()) }) };
    },
    createSymKeyGenerator: algorithm => {
      assert.equal(algorithm, 'HMAC');
      return { convertKey: async ({ data }) => {
        if (state.fail === 'key') throw new Error('key import unavailable');
        const copy = Buffer.from(data); state.imported.push(data);
        return { bytes: copy, clearMem: () => { copy.fill(0); state.cleared++; } };
      } };
    },
    createMac: algorithm => {
      assert.equal(algorithm, 'SHA256'); let mac;
      return { init: async key => { mac = crypto.createHmac('sha256', key.bytes); },
        update: async ({ data }) => mac.update(data), doFinal: async () => {
          if (state.fail === 'mac') throw new Error('MAC unavailable');
          return { data: new Uint8Array(mac.digest()) };
        } };
    }
  };
  class Encoder { encodeInto(text) { return new Uint8Array(Buffer.from(text, 'utf8')); } }
  return { state, framework, mocks: { '@kit.CryptoArchitectureKit': { cryptoFramework: framework }, '@kit.ArkTS': { util: { TextEncoder: Encoder } } } };
}
module.exports = { signatureBoundary };
