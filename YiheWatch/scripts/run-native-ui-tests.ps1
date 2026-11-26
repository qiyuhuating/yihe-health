#requires -Version 7.0
param(
  [Parameter(Mandatory)][string]$BuildReport,
  [Parameter(Mandatory)][string]$AppBuildReport,
  [Parameter(Mandatory)][string]$CommandLineHome,
  [Parameter(Mandatory)][string]$FixtureToken,
  [Parameter(Mandatory)][string]$ReportPath,
  [ValidateSet('initial', 'restart')][string]$Scenario = 'initial',
  [string]$Target = '127.0.0.1:5555',
  [switch]$AllowDirty
)
$ErrorActionPreference = 'Stop'
$ReportPath = [IO.Path]::GetFullPath($ReportPath)
New-Item -ItemType Directory -Path (Split-Path $ReportPath -Parent) -Force | Out-Null
$report = [ordered]@{ status = 'blocked'; source = ''; dirty = $false; target = $Target; scenario = $Scenario; product = 'quality'; bundle = 'com.yihe.watch.quality'; fixtureReset = $false; artifacts = @(); passed = 0; error = '' }
try {
  if ($FixtureToken -notmatch '^[a-f0-9]{64}$') { throw 'UI 验收需要专用一次性夹具令牌' }
  $builds = @((Get-Content -LiteralPath $AppBuildReport -Raw | ConvertFrom-Json), (Get-Content -LiteralPath $BuildReport -Raw | ConvertFrom-Json))
  $report.source = $builds[0].source
  if ($report.source -notmatch '^[a-f0-9]{40}$') { throw 'UI 构建报告缺少明确的源码 SHA' }
  foreach ($build in $builds) {
    if ($build.status -ne 'passed' -or $build.product -ne 'quality' -or $build.source -cne $report.source -or !$build.artifact.path) {
      throw 'UI 验收只接受同一源码的独立 quality 产品与测试构建'
    }
    $report.dirty = $report.dirty -or $build.dirty
    $hash = (Get-FileHash -LiteralPath $build.artifact.path -Algorithm SHA256).Hash
    if ($hash -ine $build.artifact.sha256) { throw 'UI HAP 与构建摘要不符' }
    $zip = [IO.Compression.ZipFile]::OpenRead($build.artifact.path)
    try {
      $entry = $zip.GetEntry('module.json')
      if (!$entry) { throw 'UI HAP 缺少清单' }
      $reader = [IO.StreamReader]::new($entry.Open())
      try { $manifest = $reader.ReadToEnd() | ConvertFrom-Json } finally { $reader.Dispose() }
    } finally { $zip.Dispose() }
    if ($manifest.app.bundleName -cne $report.bundle) { throw '拒绝安装生产包或其他包名，避免触碰已有数据' }
    $expectedModule = if ($build.target -eq 'ohosTest') { 'entry_test' } elseif ($build.target -eq 'default') { 'entry' } else { throw '未知 UI HAP 目标' }
    $expectedType = if ($build.target -eq 'ohosTest') { 'feature' } else { 'entry' }
    if ($manifest.module.name -cne $expectedModule -or $manifest.module.type -cne $expectedType) { throw 'UI HAP 模块不符' }
    $report.artifacts += @{ module = $expectedModule; sha256 = $hash }
  }
  if ($builds[0].target -ne 'default' -or $builds[1].target -ne 'ohosTest') { throw '需要产品和独立 ohosTest 模块' }
  if ($report.dirty -and !$AllowDirty) { throw '正式 UI 验收需要干净源码' }
  $hdc = Join-Path $CommandLineHome 'sdk/default/openharmony/toolchains/hdc.exe'
  if (!(Test-Path -LiteralPath $hdc -PathType Leaf)) { throw 'UI 验收找不到指定 HDC' }
  $targets = & $hdc list targets
  if ($LASTEXITCODE -ne 0 -or $targets -notcontains $Target) { throw '指定 UI 目标未连接，不会重置模拟器' }
  $report.status = 'failed'
  foreach ($build in $builds) {
    $installed = (& $hdc -t $Target install $build.artifact.path 2>&1) -join "`n"
    $installed | Add-Content -LiteralPath ($ReportPath + '.install.log') -Encoding utf8
    if ($LASTEXITCODE -ne 0 -or $installed -notmatch 'install bundle successfully') { throw '独立 quality HAP 安装失败' }
  }
  if ($Scenario -eq 'initial') {
    # Only the fixed test-owned bundle is reset, after both real manifests were
    # verified. The restart scenario deliberately preserves its data.
    $reset = (& $hdc -t $Target shell bm clean -n com.yihe.watch.quality -d 2>&1) -join "`n"
    $reset | Set-Content -LiteralPath ($ReportPath + '.fixture-reset.log') -Encoding utf8
    if ($LASTEXITCODE -ne 0 -or $reset -notmatch 'clean bundle data files successfully') { throw '独立 quality 夹具初始化失败' }
    $report.fixtureReset = $true
  }
  & $hdc -t $Target shell aa test -b $report.bundle -m entry_test -s unittest NativeUIRunner -s fixtureToken $FixtureToken -s scenario $Scenario -s timeout 360000 -w 420 2>&1 | Tee-Object -FilePath ($ReportPath + '.log')
  if ($LASTEXITCODE -ne 0) { throw 'UI 原生测试命令失败' }
  $output = Get-Content -LiteralPath ($ReportPath + '.log') -Raw
  $name = if ($Scenario -eq 'restart') { 'native_ui_restart' } else { 'native_ui_lifecycle' }
  if ($output -notmatch 'Tests run:\s*1,\s*Failure:\s*0,\s*Error:\s*0,\s*Pass:\s*1,\s*Ignore:\s*0' -or $output -notmatch ('test=' + $name + '(?:\r?\n|\s)')) {
    throw 'UI 原生用例必须完整通过且无错误或跳过'
  }
  $report.status = 'passed'; $report.passed = 1
} catch { $report.error = $_.Exception.Message; throw }
finally { $report | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $ReportPath -Encoding utf8 }
