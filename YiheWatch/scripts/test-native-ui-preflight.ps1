#requires -Version 7.0
param([string]$ReportDirectory = '')
$ErrorActionPreference = 'Stop'
if (!$ReportDirectory) { $ReportDirectory = Join-Path $PSScriptRoot '../../evidence/ci/watch-preflight/ui' }
$ReportDirectory = [IO.Path]::GetFullPath($ReportDirectory)
New-Item -ItemType Directory -Path $ReportDirectory -Force | Out-Null
$fixtures = Join-Path $ReportDirectory ('fixtures-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixtures | Out-Null
function New-Fixture([string]$name, [string]$target, [string]$bundle = 'com.yihe.watch.quality', [string]$type = '') {
  $module = if ($target -eq 'default') { 'entry' } else { 'entry_test' }
  if (!$type) { $type = if ($target -eq 'default') { 'entry' } else { 'feature' } }
  $hap = Join-Path $fixtures ($name + '.hap')
  $zip = [IO.Compression.ZipFile]::Open($hap, [IO.Compression.ZipArchiveMode]::Create)
  try {
    $entry = $zip.CreateEntry('module.json')
    $writer = [IO.StreamWriter]::new($entry.Open())
    try { $writer.Write((@{ app = @{ bundleName = $bundle }; module = @{ name = $module; type = $type } } | ConvertTo-Json -Depth 5)) }
    finally { $writer.Dispose() }
  } finally { $zip.Dispose() }
  return @{ status = 'passed'; source = ('f' * 40); dirty = $false; product = 'quality'; target = $target;
    artifact = @{ path = $hap; sha256 = (Get-FileHash -LiteralPath $hap -Algorithm SHA256).Hash } }
}
$results = @()
foreach ($case in @('token', 'source', 'product', 'bundle', 'hash', 'module', 'dirty', 'valid-boundary')) {
  $app = New-Fixture ($case + '-app') default
  $test = New-Fixture ($case + '-test') ohosTest
  $token = 'a' * 64
  $expected = switch ($case) {
    'token' { $token = 'invalid'; '夹具令牌' }
    'source' { $test.source = 'e' * 40; '同一源码' }
    'product' { $app.product = 'default'; 'quality 产品' }
    'bundle' { $app = New-Fixture ($case + '-production') default 'com.yihe.watch'; '拒绝安装生产包' }
    'hash' { $app.artifact.sha256 = '0' * 64; '构建摘要不符' }
    'module' { $app = New-Fixture ($case + '-wrong-module') default 'com.yihe.watch.quality' feature; '模块不符' }
    'dirty' { $app.dirty = $true; '干净源码' }
    'valid-boundary' { '找不到指定 HDC' }
  }
  $appFile = Join-Path $fixtures ($case + '-app.json')
  $testFile = Join-Path $fixtures ($case + '-test.json')
  $output = Join-Path $ReportDirectory ($case + '.json')
  $app | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $appFile -Encoding utf8
  $test | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $testFile -Encoding utf8
  $rejected = $false
  try {
    & (Join-Path $PSScriptRoot 'run-native-ui-tests.ps1') -AppBuildReport $appFile -BuildReport $testFile -FixtureToken $token -CommandLineHome (Join-Path $fixtures 'missing-tools') -ReportPath $output
  } catch {
    if ($_.Exception.Message -notmatch $expected) { throw }
    $rejected = $true
  }
  $report = Get-Content -LiteralPath $output -Raw | ConvertFrom-Json
  if (!$rejected -or $report.status -ne 'blocked' -or $report.passed -ne 0 -or (Test-Path -LiteralPath ($output + '.install.log'))) {
    throw "UI preflight must reject before installing any package: $case"
  }
  if ($case -eq 'valid-boundary' -and $report.artifacts.Count -ne 2) { throw 'Both valid fixture HAP boundaries must be checked' }
  $results += @{ case = $case; rejectedBeforeInstall = $true }
}
@{ scope = 'Synthetic manifest/metadata rejection tests only; no SDK compilation, HDC or native UI execution'; cases = $results } |
  ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $ReportDirectory 'summary.json') -Encoding utf8
Write-Output '8 UI preflight boundaries passed; no package installed or UI test executed.'
