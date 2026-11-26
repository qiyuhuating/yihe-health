import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { renderOpenApiTypes } from '../tools/openapi/index.mjs'

export const generatedPath = fileURLToPath(new URL('../src/api/generated-api.d.ts', import.meta.url))

export function exportDocument() {
  return JSON.parse(execFileSync('go', ['run', './cmd/openapi'], {
    cwd: fileURLToPath(new URL('../../backend/', import.meta.url)),
    encoding: 'utf8', maxBuffer: 8 * 1024 * 1024
  }))
}

function trimGenerated(source) {
  const header = 'export interface paths {\n';
  const pathsStart = source.indexOf(header);
  const firstPath = source.indexOf('    "/', pathsStart + header.length);
  const pathsEnd = source.indexOf('\n}\nexport type webhooks', firstPath);
  if (pathsStart < 0 || firstPath < 0 || pathsEnd < 0) throw new Error('Unexpected generated OpenAPI paths structure');
  const routeStart = source.indexOf('    "/api/v1/health/latest": {', firstPath);
  const nextPath = routeStart < 0 ? -1 : source.indexOf('\n    "/', routeStart + 1);
  const routeEnd = nextPath >= 0 && nextPath < pathsEnd ? nextPath : pathsEnd;
  const route = routeStart >= 0 && routeStart < pathsEnd ? source.slice(routeStart, routeEnd) : '';
  const kept = source.slice(0, firstPath) + route + source.slice(pathsEnd);
  const componentStart = kept.indexOf('export interface components {\n');
  const componentEnd = kept.indexOf('\n}\nexport type $defs', componentStart);
  if (componentStart < 0 || componentEnd < 0) throw new Error('Unexpected generated OpenAPI components structure');
  const section = kept.slice(componentStart, componentEnd);
  const groups = ['schemas', 'responses', 'parameters', 'requestBodies', 'headers', 'pathItems'];
  const members = new Map();
  const retained = new Map(groups.map(group => [group, new Set()]));
  for (const group of groups) {
    const marker = `    ${group}: {\n`;
    const groupStart = section.indexOf(marker);
    if (groupStart < 0) { members.set(group, new Map()); continue; }
    const groupEnd = section.indexOf('\n    };', groupStart + marker.length);
    if (groupEnd < 0) throw new Error(`Cannot parse ${group} components`);
    const groupBody = section.slice(groupStart + marker.length, groupEnd);
    const keys = [...groupBody.matchAll(/^        ([A-Za-z_$][\w$]*):/gm)];
    const found = new Map();
    keys.forEach((m, i) => found.set(m[1], groupBody.slice(m.index, i + 1 < keys.length ? keys[i + 1].index : groupBody.length).trimEnd() + '\n'));
    members.set(group, found);
  }
  const queue = [route];
  while (queue.length) {
    const block = queue.pop();
    for (const ref of block.matchAll(/components\["([^"]+)"\]\["([^"]+)"\]/g)) {
      const group = ref[1], name = ref[2];
      const selected = retained.get(group);
      const value = members.get(group)?.get(name);
      if (!selected || value === undefined) throw new Error(`Missing referenced API component: ${group}.${name}`);
      if (!selected.has(name)) { selected.add(name); queue.push(value); }
    }
  }
  let reducedComponents = 'export interface components {\n';
  for (const group of groups) {
    const names = retained.get(group);
    if (names.size === 0) { reducedComponents += `    ${group}: never;\n`; continue; }
    reducedComponents += `    ${group}: {\n`;
    for (const [name, value] of members.get(group)) if (names.has(name)) reducedComponents += value;
    reducedComponents += '    };\n';
  }
  return kept.slice(0, componentStart) + reducedComponents + kept.slice(componentEnd);
}

export async function generateTypes(document) {
  const source = await renderOpenApiTypes(document)
  return '// Generated from the published Go OpenAPI by npm run api:generate. Do not edit.\n' + trimGenerated(source)
}

export async function generate(check) {
  const document = exportDocument()
  const text = await generateTypes(document)
  if (check) {
    if (readFileSync(generatedPath, 'utf8') !== text) {
      throw new Error('Generated API types differ from the published Go contract; run npm run api:generate')
    }
  } else writeFileSync(generatedPath, text)
  console.log(`Published OpenAPI → TypeScript: ${Object.keys(document.paths).length} paths${check ? ' checked' : ' generated'}`)
}
