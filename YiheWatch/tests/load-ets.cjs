const fs = require('node:fs');
const path = require('node:path');
const Module = require('node:module');
const ts = require('typescript');

// Only the SDK boundary is substituted. Execute the production ArkTS model and
// transport as JavaScript; this is not an ArkTS compiler or hardware validator.
module.exports = function load(relative, mocks = {}) {
  const root = path.resolve(__dirname, '../entry/src/main/ets');
  const file = path.resolve(root, relative + '.ets');
  const mod = new Module(file, module);
  mod.require = name => {
    if (name in mocks) return mocks[name];
    if (name.startsWith('.')) return load(path.relative(root, path.resolve(path.dirname(file), name)), mocks);
    return require(name);
  };
  const source = ts.transpileModule(fs.readFileSync(file, 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS }
  }).outputText;
  // Keep syscap checks at the explicit SDK boundary, including unsupported tests.
  const prelude = 'const canIUse = module.__syscap || (() => true);\n';
  mod.__syscap = mocks.canIUse;
  mod._compile(prelude + source, file);
  return mod.exports;
};
