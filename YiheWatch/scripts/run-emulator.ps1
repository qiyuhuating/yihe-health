param(
  [string]$DevEcoHome = 'D:\Huawei\DevEco Studio',
  [string]$Target = '127.0.0.1:5555',
  [int]$BackendPort = 8796,
  [switch]$Build
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
if ($Build) { & (Join-Path $PSScriptRoot 'build.ps1') -DevEcoHome $DevEcoHome }
$hdc = Join-Path $DevEcoHome 'sdk\default\openharmony\toolchains\hdc.exe'
$hap = Join-Path $projectRoot 'entry\build\default\outputs\default\entry-default-unsigned.hap'
if (!(Test-Path -LiteralPath $hap)) { throw '请先运行 scripts\build.ps1' }
$targets = & $hdc list targets
if ($targets -notcontains $Target) { throw "未连接目标 $Target。请在 DevEco Device Manager 启动 Wearable 模拟器，运行 hdc list targets 确认地址。" }
$forwardTasks = (& $hdc fport ls 2>&1) -join "`n"
$expectedForward = [regex]::Escape($Target) + '\s+tcp:18796 tcp:' + $BackendPort + '\s+\[Reverse\]'
if ($forwardTasks -notmatch $expectedForward) {
  if ($forwardTasks -match 'tcp:18796\s') { throw '18796 已有不同转发，请先核对现有映射；脚本不会移除其他转发。' }
  $forwardResult = (& $hdc -t $Target rport tcp:18796 "tcp:$BackendPort" 2>&1) -join "`n"
  Write-Output $forwardResult
  if ($LASTEXITCODE -ne 0 -or $forwardResult -notmatch 'result:OK') { throw '端口反向转发失败' }
} else { Write-Output '复用现有 18796 → 后端转发。' }
$installResult = (& $hdc -t $Target install $hap 2>&1) -join "`n"
Write-Output $installResult
if ($LASTEXITCODE -ne 0 -or $installResult -notmatch 'install bundle successfully') { throw '安装失败（真机需要个人开发者签名）' }
& $hdc -t $Target shell aa start -a EntryAbility -b com.yihe.watch
if ($LASTEXITCODE -ne 0) { throw '启动失败' }
Write-Output '已请求启动颐和守护。后端默认 http://127.0.0.1:18796（经 HDC 转发到电脑）。'
