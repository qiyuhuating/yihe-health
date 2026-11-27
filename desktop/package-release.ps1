$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'validate-version.ps1')
$project = Split-Path $PSScriptRoot -Parent
$repo = $project # desktop/ is directly under the repository root.
$meta = Get-Content (Join-Path $PSScriptRoot 'version.json') -Raw | ConvertFrom-Json
$version = $meta.version
$channel = if ($env:YIHE_RELEASE_CHANNEL) { $env:YIHE_RELEASE_CHANNEL } else { [string]$meta.channel }
if ($channel -notin @('preview', 'stable')) { throw 'Invalid release channel' }
$out = Join-Path $project "outputs/v$version-$channel"
$release = Join-Path $out 'release'
$portable = Join-Path $out 'portable'
$target = Join-Path $PSScriptRoot 'target'
if ($env:CARGO_TARGET_DIR) {
    $target = if ([IO.Path]::IsPathRooted($env:CARGO_TARGET_DIR)) { $env:CARGO_TARGET_DIR } else { Join-Path $PSScriptRoot $env:CARGO_TARGET_DIR }
}
New-Item -ItemType Directory -Force $release, $portable | Out-Null
$setup = Join-Path $out "Yihe-Setup-$version-win-x64.exe"
Copy-Item -LiteralPath $setup -Destination $release
Copy-Item -LiteralPath (Join-Path $target 'release/yihe.exe') -Destination $portable
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'binaries/yihe-server-x86_64-pc-windows-msvc.exe') -Destination (Join-Path $portable 'yihe-server.exe')
Copy-Item -LiteralPath (Join-Path $PSScriptRoot '使用说明.txt') -Destination $portable
if ((Get-Item (Join-Path $portable 'yihe.exe')).VersionInfo.ProductVersion -notlike "$version*") { throw 'Application version mismatch' }
$backendHash = (Get-FileHash (Join-Path $portable 'yihe-server.exe')).Hash.ToLowerInvariant()
if ($backendHash -ne (Get-Content (Join-Path $PSScriptRoot 'backend-sha256.txt') -Raw).Trim()) { throw 'Sidecar hash mismatch' }
$portableZip = Join-Path $release "Yihe-Portable-$version-win-x64.zip"
Compress-Archive -Path (Join-Path $portable '*') -DestinationPath $portableZip -Force
$commit = (git -C $repo rev-parse HEAD).Trim()
$sourceZip = Join-Path $release "Yihe-Source-$version.zip"
git -C $repo archive --format=zip --prefix="Yihe-$version/" --output=$sourceZip $commit
if ($LASTEXITCODE -ne 0) { throw 'Source archive failed' }
$notesPath = Join-Path $repo "docs/releases/$version.md"
$notes = if (Test-Path -LiteralPath $notesPath -PathType Leaf) {
    Get-Content -LiteralPath $notesPath -Raw
} else {
    $channelMarker = if ($channel -eq 'stable') { '这是稳定渠道发布。' } else { '这是预览渠道发布。' }
    "# 颐和 $version`n`n$channelMarker`n`n源码提交：$commit`n`n本版本未提供单独的发布说明。`n"
}
if ($channel -eq 'stable') {
    if ($notes.Contains('这是预览渠道发布。')) {
        $notes = $notes.Replace('这是预览渠道发布。', '这是稳定渠道发布。')
    } elseif (!$notes.Contains('这是稳定渠道发布。')) {
        throw 'Stable release notes marker missing'
    }
}
[IO.File]::WriteAllText((Join-Path $release 'Release-Notes-zh-CN.md'), $notes, [Text.UTF8Encoding]::new($false))

# Run against disposable directories only; no operator database or user profile.
$qaRoot = Join-Path ([IO.Path]::GetTempPath()) "yihe-release-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory $qaRoot | Out-Null
$env:YIHE_QA_HEADLESS = '1'
$env:YIHE_MYSQL_DSN = ''
$env:YIHE_PG_ANALYTICS_URL = ''
$env:MYSQL_DSN = ''
$env:PG_ANALYTICS_URL = ''
function Test-Application([string]$executable, [string]$label) {
    $env:YIHE_USER_DATA = Join-Path $qaRoot $label
    $application = Start-Process -FilePath $executable -PassThru -WindowStyle Hidden
    $backendPid = $null
    $secondInstance = $null
    try {
        $discovery = Join-Path $env:YIHE_USER_DATA 'runtime/server.json'
        $deadline = [DateTime]::UtcNow.AddSeconds(45)
        while (!(Test-Path -LiteralPath $discovery)) {
            if ($application.HasExited) { throw "$label exited before ready" }
            if ([DateTime]::UtcNow -gt $deadline) { throw "$label readiness timed out" }
            Start-Sleep -Milliseconds 200
        }
        $server = Get-Content -LiteralPath $discovery -Raw | ConvertFrom-Json
        if ($server.desktopPid -ne $application.Id) { throw "$label desktop PID mismatch" }
        $backendPid = [int]$server.pid
        $page = Invoke-WebRequest "http://127.0.0.1:$($server.port)/index.html" -TimeoutSec 10
        if ($page.StatusCode -ne 200 -or $page.Content -notmatch '<html') { throw "$label embedded UI missing" }
        $secondInstance = Start-Process -FilePath $executable -PassThru -WindowStyle Hidden
        if (!$secondInstance.WaitForExit(15000) -or $secondInstance.ExitCode -ne 0) {
            throw "$label second instance did not hand off to the running application"
        }
        $running = Get-Content -LiteralPath $discovery -Raw | ConvertFrom-Json
        if ($application.HasExited -or $running.desktopPid -ne $application.Id -or $running.pid -ne $backendPid) {
            throw "$label second instance replaced the running application or sidecar"
        }
        # Simulate parent termination and verify the sidecar's orphan watchdog.
        Stop-Process -Id $application.Id
        $application.WaitForExit()
        $deadline = [DateTime]::UtcNow.AddSeconds(20)
        while (Get-Process -Id $backendPid -ErrorAction SilentlyContinue) {
            if ([DateTime]::UtcNow -gt $deadline) { throw "$label orphan sidecar did not exit" }
            Start-Sleep -Milliseconds 200
        }
        return @{ distribution = $label; nativeStartup = $true; embeddedUI = $true; singleInstance = $true; parentTerminationCleanup = $true }
    } finally {
        if ($secondInstance -and !$secondInstance.HasExited) { Stop-Process -Id $secondInstance.Id -ErrorAction SilentlyContinue }
        if (!$application.HasExited) { Stop-Process -Id $application.Id -ErrorAction SilentlyContinue }
        if ($backendPid) { Stop-Process -Id $backendPid -ErrorAction SilentlyContinue }
    }
}
$extracted = Join-Path $qaRoot 'portable-extracted'
Expand-Archive -LiteralPath $portableZip -DestinationPath $extracted
$checks = @(Test-Application (Join-Path $extracted 'yihe.exe') 'portable')
$installDir = Join-Path $qaRoot 'installed'
$installer = Start-Process -FilePath $setup -ArgumentList @('/S', "/D=$installDir") -Wait -PassThru -WindowStyle Hidden
if ($installer.ExitCode -ne 0) { throw "Setup failed: $($installer.ExitCode)" }
try {
    $installedApp = Join-Path $installDir 'yihe.exe'
    if ((Get-FileHash $installedApp).Hash -ne (Get-FileHash (Join-Path $portable 'yihe.exe')).Hash) { throw 'Installed app differs from portable app' }
    $checks += Test-Application $installedApp 'installed'
    $sentinel = Join-Path $env:YIHE_USER_DATA 'preserve-on-uninstall.txt'
    Set-Content -LiteralPath $sentinel -Value 'test user data'
} finally {
    $uninstaller = Get-ChildItem -LiteralPath $installDir -Filter '*uninstall*.exe' | Select-Object -First 1
    if (!$uninstaller) { throw 'Uninstaller missing' }
    $uninstall = Start-Process -FilePath $uninstaller.FullName -ArgumentList @('/S', "_?=$installDir") -Wait -PassThru -WindowStyle Hidden
    if ($uninstall.ExitCode -ne 0) { throw "Uninstall failed: $($uninstall.ExitCode)" }
}
if (Test-Path -LiteralPath $installedApp) { throw 'Uninstall left application executable' }
if (!(Test-Path -LiteralPath $sentinel)) { throw 'Uninstall removed user data' }
$releaseBranch = if ($channel -eq 'stable') { 'main' } else { 'preview' }
$signatures = [ordered]@{}
foreach ($entry in @(
    @{ Name = 'setup'; Path = $setup },
    @{ Name = 'desktop'; Path = (Join-Path $portable 'yihe.exe') },
    @{ Name = 'backend'; Path = (Join-Path $portable 'yihe-server.exe') }
)) {
    $signature = Get-AuthenticodeSignature -LiteralPath $entry.Path
    if ($signature.Status -notin @('NotSigned', 'NotTrusted', 'Valid')) {
        throw "Unexpected signature status for $($entry.Name): $($signature.Status)"
    }
    $signatures[$entry.Name] = [ordered]@{
        status = $signature.Status.ToString()
        thumbprint = if ($signature.SignerCertificate) { $signature.SignerCertificate.Thumbprint } else { $null }
        trustedOnBuildMachine = ($signature.Status -eq 'Valid')
    }
}
$signed = @($signatures.Values | Where-Object { $_.thumbprint })
if ($signed.Count -ne 0 -and $signed.Count -ne $signatures.Count) {
    throw 'Mixed signed and unsigned Windows executables in the same release'
}
if (@($signed | Select-Object -ExpandProperty thumbprint -Unique).Count -gt 1) {
    throw 'Windows release executables were signed using different certificates'
}
$manifest = [ordered]@{
    version = $version; sourceCommit = $commit; branch = $releaseBranch; channel = $channel; signing = $signatures
    workflowRun = $env:GITHUB_RUN_ID; backendSHA256 = $backendHash
    checks = $checks; silentInstall = $true; uninstallPreservesData = $true
    limitations = @('Headless startup, not visual UI acceptance', 'No real devices or SMS gateway', 'WebView2 missing-runtime bootstrap not tested', 'No production migration or sustained HTTP load test')
}
$manifest['distributionBytes'] = [ordered]@{
    setup = (Get-Item -LiteralPath (Join-Path $release (Split-Path $setup -Leaf))).Length
    portable = (Get-Item -LiteralPath $portableZip).Length
    source = (Get-Item -LiteralPath $sourceZip).Length
    desktopExecutable = (Get-Item -LiteralPath (Join-Path $portable 'yihe.exe')).Length
    backendExecutable = (Get-Item -LiteralPath (Join-Path $portable 'yihe-server.exe')).Length
}
$native = Get-ChildItem -LiteralPath $release -File -Filter 'Yihe-Native-NoFonts-*'
if (@($native).Count -ne 3) { throw 'Expected exactly three font-free native distributions' }
$manifest['fontFreeNativeBytes'] = [ordered]@{}
foreach ($binary in $native) {
    $manifest['fontFreeNativeBytes'][$binary.Name] = $binary.Length
}
$manifest['distributionBytes'] | Format-Table -AutoSize | Out-Host
$manifest | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8NoBOM (Join-Path $release 'release-manifest.json')
Get-ChildItem -LiteralPath $release -File | Where-Object Name -ne 'SHA256SUMS.txt' | Sort-Object Name | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant(), $_.Name
} | Set-Content -Encoding utf8NoBOM (Join-Path $release 'SHA256SUMS.txt')
Write-Host "Verified release files: $release (commit $commit)"
