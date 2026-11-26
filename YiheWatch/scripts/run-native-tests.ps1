#requires -Version 7.0
param(
  [Parameter(Mandatory)][string]$BuildReport,
  [Parameter(Mandatory)][string]$CommandLineHome,
  [string]$Target = '127.0.0.1:5555',
  [ValidateSet('system', 'backend')][string]$Suite = 'system',
  [string]$FixtureToken = '',
  [string]$ReportPath = '',
  [switch]$AllowDirty
)
$ErrorActionPreference = 'Stop'
if (!$ReportPath) { $ReportPath = Join-Path (Split-Path $PSScriptRoot -Parent) 'artifacts/native-tests.json' }
$ReportPath = [IO.Path]::GetFullPath($ReportPath)
New-Item -ItemType Directory -Path (Split-Path $ReportPath -Parent) -Force | Out-Null
$report = [ordered]@{ status = 'blocked'; source = ''; dirty = $false; target = $Target; suite = $Suite; artifactSha256 = ''; passed = 0; signatureVerified = $false; error = '' }
try {
  if ($Suite -eq 'backend' -and $FixtureToken -notmatch '^[a-f0-9]{64}$') { throw '后端原生测试需要独立夹具令牌' }
  $build = Get-Content -LiteralPath $BuildReport -Raw | ConvertFrom-Json
  $report.source = $build.source; $report.dirty = $build.dirty
  if ($build.status -ne 'passed' -or $build.target -ne 'ohosTest' -or !$build.artifact.path) { throw '需要成功的 ohosTest 构建报告' }
  if ($build.dirty -and !$AllowDirty) { throw '正式原生证据需要干净源码；调试运行需显式 AllowDirty 并保留 dirty 标记' }
  $hap = $build.artifact.path
  $hash = (Get-FileHash -LiteralPath $hap -Algorithm SHA256).Hash
  if ($hash -ine $build.artifact.sha256) { throw '测试 HAP 与构建摘要不一致' }
  $report.artifactSha256 = $hash
  $zip = [IO.Compression.ZipFile]::OpenRead($hap)
  try {
    $entry = $zip.GetEntry('module.json')
    if (!$entry) { throw '测试 HAP 缺少 module.json' }
    $reader = [IO.StreamReader]::new($entry.Open())
    try { $manifest = $reader.ReadToEnd() | ConvertFrom-Json } finally { $reader.Dispose() }
  } finally { $zip.Dispose() }
  if ($manifest.app.bundleName -ne 'com.yihe.watch' -or $manifest.module.name -ne 'entry_test' -or $manifest.module.type -ne 'feature') {
    throw '仅允许安装独立 entry_test 模块，拒绝生产或其他 HAP'
  }
  $hdc = Join-Path $CommandLineHome 'sdk/default/openharmony/toolchains/hdc.exe'
  if (!(Test-Path -LiteralPath $hdc -PathType Leaf)) { throw '找不到指定工具链的 HDC' }
  $targets = & $hdc list targets
  if ($LASTEXITCODE -ne 0 -or $targets -notcontains $Target) { throw '指定测试目标未连接；不会另起或重置模拟器' }
  $report.status = 'failed'
  $install = (& $hdc -t $Target install $hap 2>&1) -join "`n"
  $install | Set-Content -LiteralPath ($ReportPath + '.install.log') -Encoding utf8
  if ($LASTEXITCODE -ne 0 -or $install -notmatch 'install bundle successfully') { throw '独立测试模块安装失败' }
  $arguments = @('-t', $Target, 'shell', 'aa', 'test', '-b', 'com.yihe.watch', '-m', 'entry_test', '-s', 'unittest')
  if ($Suite -eq 'backend') {
    $arguments += @('NativeBackendRunner', '-s', 'fixtureToken', $FixtureToken, '-s', 'timeout', '90000', '-w', '180')
  } else { $arguments += @('OpenHarmonyTestRunner', '-s', 'timeout', '30000', '-w', '120') }
  & $hdc @arguments 2>&1 | Tee-Object -FilePath ($ReportPath + '.log')
  if ($LASTEXITCODE -ne 0) { throw '原生测试命令失败' }
  $output = Get-Content -LiteralPath ($ReportPath + '.log') -Raw
  $expectedCount = if ($Suite -eq 'backend') { 1 } else { 3 }
  $expectedNames = if ($Suite -eq 'backend') { @('native_backend_pipeline') } else { @('native_signing', 'native_vault_authentication', 'native_store_disk_reload') }
  if ($output -notmatch "Tests run:\s*$expectedCount,\s*Failure:\s*0,\s*Error:\s*0,\s*Pass:\s*$expectedCount,\s*Ignore:\s*0") { throw '原生测试必须完整通过且无错误/跳过' }
  foreach ($name in $expectedNames) {
    if ($output -notmatch ('test=' + [regex]::Escape($name) + '(?:\r?\n|\s)')) { throw "原生测试缺失：$name" }
  }
  if ($Suite -eq 'backend') {
    # The Node host additionally requires every phase's real database oracle.
    $report.passed = 1; $report.status = 'passed'
    return
  }
  # Public fixed test input only; no real device credential enters evidence.
  $proofs = [regex]::Matches($output, 'SIGNATURE_PROOF:(\{[^\r\n]+\})')
  if (!$proofs.Count) { throw '缺少当前运行的原生签名证明' }
  $raw = '{"deviceNo":"WATCH_NATIVE_TEST","secret":"public-native-test-secret-0123456789","name":"田风","pad":"' + ('Native汉' * 6000) + '"}'
  $digest = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($raw))).ToLowerInvariant()
  foreach ($proof in $proofs) {
    $signed = $proof.Groups[1].Value | ConvertFrom-Json
    if ($signed.nonce -notmatch '^[a-f0-9]{32}$' -or $signed.timestamp -notmatch '^\d+$') { throw '原生签名证明格式无效' }
    $message = 'WATCH_NATIVE_TEST:' + $signed.timestamp + ':' + $signed.nonce + ':' + $digest
    $mac = [Security.Cryptography.HMACSHA256]::new([Text.Encoding]::UTF8.GetBytes('public-native-test-secret-0123456789'))
    try { $expected = [Convert]::ToHexString($mac.ComputeHash([Text.Encoding]::UTF8.GetBytes($message))).ToLowerInvariant() } finally { $mac.Dispose() }
    if ($signed.signature -cne $expected) { throw '系统 HMAC 与主机独立校验结果不一致' }
  }
  $report.signatureVerified = $true; $report.passed = 3; $report.status = 'passed'
} catch { $report.error = $_.Exception.Message; throw }
finally { $report | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $ReportPath -Encoding utf8 }
