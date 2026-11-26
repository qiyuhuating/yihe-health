param(
  [string]$DevEcoHome = $env:DEVECO_STUDIO_HOME,
  [string]$CommandLineHome = $env:HARMONY_COMMANDLINE_HOME,
  [string]$SdkHome = $env:DEVECO_SDK_HOME,
  [ValidateSet('default', 'ohosTest')][string]$BuildTarget = 'default',
  [ValidateSet('debug', 'release')][string]$BuildMode = 'debug',
  [ValidateSet('default', 'quality')][string]$Product = 'default',
  [string]$ReportPath = ''
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
if (!$ReportPath) { $ReportPath = Join-Path $projectRoot 'artifacts\native-build.json' }
$ReportPath = [IO.Path]::GetFullPath($ReportPath)
New-Item -ItemType Directory -Path (Split-Path $ReportPath -Parent) -Force | Out-Null
$report = [ordered]@{ status = 'blocked'; source = ''; dirty = $false; target = $BuildTarget; buildMode = $BuildMode; product = $Product; toolchain = ''; sdk = ''; versions = $null; artifact = $null; error = ''; startedAt = [DateTime]::UtcNow.ToString('o') }
$savedEnvironment = @{}
foreach ($name in @('NODE_PATH', 'NODE_HOME', 'DEVECO_NODE_HOME', 'DEVECO_SDK_HOME', 'JAVA_HOME', 'PATH')) {
  $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
Push-Location $projectRoot
try {
  $report.source = (& git rev-parse HEAD).Trim()
  $report.dirty = [bool](& git status --porcelain -- .)
  if ($CommandLineHome -and $DevEcoHome) { throw '请仅指定 CommandLineHome 或 DevEcoHome，避免混用工具链' }
  $prefix = @()
  if ($CommandLineHome) {
    $toolRoot = (Resolve-Path -LiteralPath $CommandLineHome).Path
    # The official CLI distribution owns its Node, Java and plugin resolution.
    $runner = @('bin\hvigorw.bat', 'bin\hvigorw.cmd') | ForEach-Object { Join-Path $toolRoot $_ } | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
    if (!$runner) { throw "未找到 Command Line Tools 的 bin\hvigorw.bat/cmd：$toolRoot" }
    $nodeHome = Join-Path $toolRoot 'tool/node'
    if (!(Test-Path -LiteralPath (Join-Path $nodeHome 'node.exe') -PathType Leaf)) { throw "工具包缺少内置 Node：$nodeHome" }
    $env:DEVECO_NODE_HOME = $nodeHome
    $env:NODE_HOME = $nodeHome
    $env:NODE_PATH = Join-Path $toolRoot 'hvigor/node_modules'
    $env:PATH = $nodeHome + [IO.Path]::PathSeparator + $env:PATH
  } else {
    if (!$DevEcoHome) { $DevEcoHome = 'D:\Huawei\DevEco Studio' }
    $toolRoot = [IO.Path]::GetFullPath($DevEcoHome)
    $runner = Join-Path $toolRoot 'tools\node\node.exe'
    $hvigor = Join-Path $toolRoot 'tools\hvigor\hvigor\bin\hvigor.js'
    $java = Join-Path $toolRoot 'jbr'
    foreach ($required in @($runner, $hvigor, (Join-Path $java 'bin\java.exe'))) {
      if (!(Test-Path -LiteralPath $required -PathType Leaf)) { throw "缺少原生构建工具：$required；请指定 DevEcoHome 或 CommandLineHome" }
    }
    if (!$SdkHome) { $SdkHome = Join-Path $toolRoot 'sdk' }
    # Reject an incompatible IDE before creating project dependency junctions.
    $report.versions = & (Join-Path $PSScriptRoot 'check-toolchain.ps1') -ToolRoot $toolRoot -SdkHome $SdkHome
    $moduleRoot = Join-Path $projectRoot 'node_modules\@ohos'
    foreach ($package in @('hvigor', 'hvigor-ohos-plugin')) {
      $target = Join-Path $toolRoot "tools\hvigor\$package"
      if (!(Test-Path -LiteralPath $target -PathType Container)) { throw "缺少 bundled 插件：$target" }
    }
    New-Item -ItemType Directory -Path $moduleRoot -Force | Out-Null
    foreach ($package in @('hvigor', 'hvigor-ohos-plugin')) {
      $link = Join-Path $moduleRoot $package
      $target = Join-Path $toolRoot "tools\hvigor\$package"
      if (Test-Path -LiteralPath $link) {
        $item = Get-Item -LiteralPath $link
        if (!$item.Target -or [IO.Path]::GetFullPath([string](@($item.Target)[0])) -ne [IO.Path]::GetFullPath($target)) { throw "现有插件与指定工具链不一致：$link；脚本不会覆盖它" }
      } else { New-Item -ItemType Junction -Path $link -Target $target | Out-Null }
    }
    $prefix = @($hvigor)
    $env:NODE_PATH = Join-Path $projectRoot 'node_modules'
    $env:JAVA_HOME = $java
    $env:PATH = (Split-Path $runner -Parent) + [IO.Path]::PathSeparator + $env:PATH
  }
  if (!$SdkHome) { $SdkHome = Join-Path $toolRoot 'sdk' }
  if (!(Test-Path -LiteralPath (Join-Path $SdkHome 'default\openharmony\ets') -PathType Container)) { throw "缺少 SDK ETS 编译组件：$SdkHome" }
  $env:DEVECO_SDK_HOME = [IO.Path]::GetFullPath($SdkHome)
  $report.toolchain = $toolRoot
  $report.sdk = $env:DEVECO_SDK_HOME
  if ($null -eq $report.versions) {
    $report.versions = & (Join-Path $PSScriptRoot 'check-toolchain.ps1') -ToolRoot $toolRoot -SdkHome $env:DEVECO_SDK_HOME -CommandLine:([bool]$CommandLineHome)
  }
  $report.status = 'failed'
  if ($BuildTarget -eq 'ohosTest') {
    $lock = Join-Path $projectRoot 'oh-package-lock.json5'
    if (!(Test-Path -LiteralPath $lock -PathType Leaf)) { throw '原生测试缺少已提交的依赖锁文件' }
    $lockedContent = [IO.File]::ReadAllText($lock).Replace("`r`n", "`n")
    $ohpm = if ($CommandLineHome) { Join-Path $toolRoot 'bin/ohpm.bat' } else { Join-Path $toolRoot 'tools/ohpm/bin/ohpm.bat' }
    if (!(Test-Path -LiteralPath $ohpm -PathType Leaf)) { throw '原生测试缺少官方 ohpm' }
    & $ohpm install --all 2>&1 | Tee-Object -FilePath ($ReportPath + '.log') -Append
    if ($LASTEXITCODE -ne 0) { throw "ohpm 测试依赖安装失败（$LASTEXITCODE）" }
    if ([IO.File]::ReadAllText($lock).Replace("`r`n", "`n") -cne $lockedContent) { throw '原生测试依赖发生锁文件漂移，拒绝构建' }
  }
  # A clean SDK build prevents an old HAP from satisfying this run's gate.
  foreach ($task in @('clean', 'assembleHap')) {
    & $runner @prefix $task --mode module -p "module=entry@$BuildTarget" -p "product=$Product" -p "buildMode=$BuildMode" --no-daemon 2>&1 | Tee-Object -FilePath ($ReportPath + '.log') -Append
    if ($LASTEXITCODE -ne 0) { throw "Hvigor $task 失败（$LASTEXITCODE）" }
  }
  $hap = Join-Path $projectRoot "entry/build/$Product/outputs/$BuildTarget/entry-$BuildTarget-unsigned.hap"
  if (!(Test-Path -LiteralPath $hap)) { throw '编译未生成预期 HAP' }
  $report.artifact = @{ path = $hap; sha256 = (Get-FileHash -LiteralPath $hap -Algorithm SHA256).Hash; bytes = (Get-Item -LiteralPath $hap).Length; signed = $false }
  $report.status = 'passed'
  Write-Output "原生未签名 $BuildMode 包：$hap（不代表已安装或通过传感器验证）"
} catch {
  $report.error = $_.Exception.Message
  throw
} finally {
  foreach ($name in $savedEnvironment.Keys) {
    if ($null -eq $savedEnvironment[$name]) { Remove-Item -LiteralPath "Env:\$name" -ErrorAction SilentlyContinue }
    else { [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process') }
  }
  $report | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $ReportPath -Encoding utf8
  Pop-Location
}
