import { parse } from 'vue/compiler-sfc'
import type { SFCScriptBlock } from 'vue/compiler-sfc'

// Use Vue's grammar for comments, attributes and closing-tag whitespace.
// Both source gates must inspect the same blocks that the compiler accepts.
export function inlineScripts(source: string, filename: string): SFCScriptBlock[] {
  const { descriptor, errors } = parse(source, { filename })
  if (errors.length) throw new Error(`Invalid SFC ${filename}: ${errors.map(String).join('; ')}`)
  const blocks = [descriptor.script, descriptor.scriptSetup]
    .filter((block): block is SFCScriptBlock => block !== null)
  if (blocks.some(block => block.src)) throw new Error(`External SFC script requires review: ${filename}`)
  return blocks
}
