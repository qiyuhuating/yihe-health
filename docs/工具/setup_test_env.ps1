<#
.SYNOPSIS
    建立仓库内的隔离测试环境（.venv），用于跑浏览器回归。

.DESCRIPTION
    解决一个具体的可复现性缺口：浏览器回归依赖 Playwright，而不少机器的
    系统 Python 没有 pip，或没有装 playwright，导致"本地跑不了测试"，
    只好各自写临时脚本绕过（这些脚本通常硬编码个人绝对路径，不可分享）。

    本脚本只做三件事，全部在仓库目录内完成，不污染系统环境：
        1. 建 .venv（若已存在则复用）
        2. 装 requirements-test.txt（已固定版本）
        3. 装指定浏览器二进制（默认 chromium；-AllBrowsers 安装三个引擎）

    需要联网。若无网络，请改用 CI 结果作为事实来源，
    并在 PR 里注明"本地未复现"。

.NOTES
    前置条件：机器上有 Python 3.12+ 且能创建 venv。
    如果 py 启动器不可用，脚本会尝试 python / python3。
#>
[CmdletBinding()]
param(
    [switch]$Force,   # 删掉现有 .venv 重建
    [switch]$AllBrowsers
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$Venv = Join-Path $Root '.venv'

function Resolve-Python {
    if (Get-Command py -ErrorAction SilentlyContinue) { return @('py', '-3') }
    foreach ($name in @('python', 'python3')) {
        if (Get-Command $name -ErrorAction SilentlyContinue) { return @($name) }
    }
    throw "找不到 Python。请先安装 Python 3.12 或更高版本。"
}

if ($Force -and (Test-Path $Venv)) {
    $resolvedVenv = (Resolve-Path -LiteralPath $Venv).Path
    if ($resolvedVenv -ne (Join-Path (Resolve-Path -LiteralPath $Root).Path '.venv')) { throw '虚拟环境路径超出仓库。' }
    if ((Get-Item -LiteralPath $Venv).Attributes -band [IO.FileAttributes]::ReparsePoint) { throw '不删除链接目录。' }
    Write-Host "移除现有 .venv ..." -ForegroundColor Yellow
    Remove-Item -LiteralPath $resolvedVenv -Recurse -Force
}

$launcher = @(Resolve-Python)

if (-not (Test-Path $Venv)) {
    Write-Host "创建 .venv ..." -ForegroundColor Cyan
    if ($launcher[0] -eq 'py') { & py -3 -m venv $Venv }
    else { & $launcher[0] -m venv $Venv }
    if ($LASTEXITCODE -ne 0) { throw "创建 venv 失败。" }
}

$Py = Join-Path $Venv 'Scripts\python.exe'
if (-not (Test-Path $Py)) { throw "找不到 $Py" }

Write-Host "安装测试依赖 ..." -ForegroundColor Cyan
& $Py -m pip install --index-url https://pypi.org/simple -r (Join-Path $Root 'requirements-test.txt')
if ($LASTEXITCODE -ne 0) { throw "安装依赖失败（可能无网络）。" }

$env:PLAYWRIGHT_BROWSERS_PATH = Join-Path $Root '.pw-browsers'
$engines = if ($AllBrowsers) { @('chromium','firefox','webkit') } else { @('chromium') }
Write-Host "安装浏览器 $engines ..." -ForegroundColor Cyan
& $Py -m playwright install @engines
if ($LASTEXITCODE -ne 0) { throw "安装浏览器失败（可能无网络）。" }

Write-Host ""
Write-Host "完成。后续可用：" -ForegroundColor Green
Write-Host "  powershell 工具/run_tests.ps1        # 跑全部检查 + 浏览器回归"
Write-Host "  powershell 工具/run_tests.ps1 -StaticOnly   # 只跑静态检查（不需要浏览器）"
