const { spawnSync } = require('node:child_process')
const path = require('node:path')
for (const name of ['browser', 'controls', 'forms', 'lifecycle', 'screen', 'setup-browser', 'issues', 'elder-delete', 'database-availability', 'ui-refinement', 'architecture']) {
  const result = spawnSync(process.execPath, [path.join(__dirname, name + '-acceptance.cjs')], { stdio: 'inherit', env: process.env })
  if (result.error) throw result.error
  if (result.status !== 0) { process.exitCode = result.status || 1; break }
}
