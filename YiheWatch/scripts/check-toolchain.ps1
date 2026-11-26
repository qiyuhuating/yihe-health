param(
  [Parameter(Mandatory)][string]$ToolRoot,
  [Parameter(Mandatory)][string]$SdkHome,
  [switch]$CommandLine
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$expected = Get-Content -LiteralPath (Join-Path $projectRoot 'toolchain.json') -Raw | ConvertFrom-Json
foreach ($relative in @('oh-package.json5', 'entry/oh-package.json5', 'hvigor/hvigor-config.json5')) {
  $project = Get-Content -LiteralPath (Join-Path $projectRoot $relative) -Raw | ConvertFrom-Json
  if ($project.modelVersion -cne $expected.modelVersion) { throw "Project modelVersion differs from toolchain.json: $relative" }
}
$profile = Get-Content -LiteralPath (Join-Path $projectRoot 'build-profile.json5') -Raw | ConvertFrom-Json
foreach ($product in $profile.app.products) {
  if ($product.compileSdkVersion -cne $expected.modelVersion) { throw "compileSdkVersion differs from toolchain.json: $($product.name)" }
}
$hvigorRoot = if ($CommandLine) { Join-Path $ToolRoot 'hvigor' } else { Join-Path $ToolRoot 'tools/hvigor' }
foreach ($package in @('hvigor', 'hvigor-ohos-plugin')) {
  $path = Join-Path $hvigorRoot "$package/package.json"
  if (!(Test-Path -LiteralPath $path -PathType Leaf)) { throw "Incomplete toolchain; missing $path" }
  $actual = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
  if ($actual.version -cne $expected.hvigorVersion) { throw "$package $($actual.version) cannot satisfy the pinned toolchain $($expected.hvigorVersion); use Command Line Tools $($expected.commandLineVersion)" }
}
$metadata = Join-Path $SdkHome 'default/openharmony/ets/oh-uni-package.json'
if (!(Test-Path -LiteralPath $metadata -PathType Leaf)) { throw "Incomplete SDK; missing $metadata" }
$sdk = Get-Content -LiteralPath $metadata -Raw | ConvertFrom-Json
if ($sdk.version -cne $expected.sdkVersion -or [string]$sdk.apiVersion -cne $expected.apiVersion -or $sdk.platformVersion -cne $expected.modelVersion) {
  throw "SDK $($sdk.version) API $($sdk.apiVersion) cannot satisfy SDK $($expected.sdkVersion) API $($expected.apiVersion)"
}
if ($CommandLine) {
  $versionFile = Join-Path $ToolRoot 'version.txt'
  $text = Get-Content -LiteralPath $versionFile -Raw
  $version = [regex]::Match($text, '(?m)^# Version:\s*(\S+)\s*$').Groups[1].Value
  if ($version -cne $expected.commandLineVersion) { throw "Expected Command Line Tools $($expected.commandLineVersion); found '$version'" }
}
[ordered]@{ commandLine = [bool]$CommandLine; commandLineVersion = $(if ($CommandLine) { $version } else { $null }); hvigorVersion = $expected.hvigorVersion; sdkVersion = $sdk.version; apiVersion = [string]$sdk.apiVersion; modelVersion = $sdk.platformVersion }
