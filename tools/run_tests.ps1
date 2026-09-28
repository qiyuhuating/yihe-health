<#
.SYNOPSIS
    跑全部校验：静态检查 + CSS 等价性 + 文档数字 + 凭据 + 浏览器回归。

.DESCRIPTION
    优先使用仓库内 .venv（由 tools/setup_test_env.ps1 创建）。
    若没有 .venv，回退到系统 Python，并在浏览器回归阶段明确报错，
    **不会**假装通过。这是刻意的：本地跑不了就说跑不了。

    使用与固定 Playwright 版本匹配的浏览器；YIHE_BROWSER_PATH 可显式覆盖。

.PARAMETER StaticOnly
    只跑不需要浏览器的检查。任何 CI 之外的临时提交都应至少跑这一档。

.NOTES
    本仓库原先有一个 run_tests_local.py 硬编码了个人机器上的
    chromium 绝对路径，且被 .gitignore 排除 —— 等于该 workaround
    无法分享。已由本脚本取代。
#>
[CmdletBinding()]
param(
    [switch]$StaticOnly,
    [ValidateSet('chromium','firefox','webkit','all')]
    [string]$Browser = 'chromium'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$VenvPy = Join-Path $Root '.venv\Scripts\python.exe'
$Py = if (Test-Path $VenvPy) { $VenvPy } else { 'python' }
$failed = @()

function Invoke-Step {
    param([string]$Name, [scriptblock]$Action)
    Write-Host ""
    Write-Host "=== $Name ===" -ForegroundColor Cyan
    & $Action
    if ($LASTEXITCODE -ne 0) { $script:failed += $Name }
}

Push-Location $Root
try {
    Invoke-Step '静态检查（语法/ID/引用/脚本顺序/innerHTML 转义）' { & $Py tests/check_static.py }
    Invoke-Step 'CSS 反混淆等价性' { & $Py tools/check_css.py }
    Invoke-Step '文档数字一致性' { & $Py tools/check_docs.py }
    Invoke-Step '凭据配置一致性' { & node tools/check_credentials.js }
    Invoke-Step '事件状态与持久化' { & node --test tests/care-domain.test.cjs }

    if (-not $StaticOnly) {
        Write-Host ""
        Write-Host "=== 浏览器回归 ===" -ForegroundColor Cyan
        if (-not (Test-Path $VenvPy)) {
            Write-Host "跳过：未找到 .venv\Scripts\python.exe" -ForegroundColor Yellow
            Write-Host "请先执行： powershell tools/setup_test_env.ps1" -ForegroundColor Yellow
            $failed += '浏览器回归（环境缺失，未执行）'
        } else {
            if ($Browser -eq 'all' -and $env:YIHE_BROWSER_PATH) { throw '多浏览器验收请先移除 YIHE_BROWSER_PATH，避免三个引擎误用同一可执行文件。' }
            $engines = if ($Browser -eq 'all') { @('chromium','firefox','webkit') } else { @($Browser) }
            foreach ($engine in $engines) {
                $env:YIHE_BROWSER = $engine
                & $Py -X utf8 -m unittest discover -s tests -v
                if ($LASTEXITCODE -ne 0) { $failed += "浏览器回归 ($engine)" }
            }
        }
    }
} finally {
    Pop-Location
}

Write-Host ""
if ($failed.Count -gt 0) {
    Write-Host "FAILED: $($failed -join ', ')" -ForegroundColor Red
    exit 1
}
if ($StaticOnly) {
    Write-Host "OK: 静态检查全部通过（浏览器回归未执行）" -ForegroundColor Green
} else {
    Write-Host "OK: 全部检查通过" -ForegroundColor Green
}
