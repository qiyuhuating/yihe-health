$ErrorActionPreference = 'Stop'
$expected = Get-Content -LiteralPath (Join-Path (Split-Path $PSScriptRoot -Parent) 'toolchain.json') -Raw | ConvertFrom-Json
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('yihe-sdk-contract-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixture | Out-Null
# Metadata fixtures exercise preflight only; they are never executable SDKs.
function Write-Fixture {
  foreach ($package in @('hvigor', 'hvigor-ohos-plugin')) {
    $directory = Join-Path $fixture "hvigor/$package"
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
    @{ version = $expected.hvigorVersion } | ConvertTo-Json | Set-Content (Join-Path $directory 'package.json')
  }
  $directory = Join-Path $fixture 'sdk/default/openharmony/ets'
  New-Item -ItemType Directory -Path $directory -Force | Out-Null
  @{ version = $expected.sdkVersion; apiVersion = $expected.apiVersion; platformVersion = $expected.modelVersion } | ConvertTo-Json | Set-Content (Join-Path $directory 'oh-uni-package.json')
  "# Version: $($expected.commandLineVersion)" | Set-Content (Join-Path $fixture 'version.txt')
}
function Assert-Rejected([string]$Case) {
  $rejected = $false
  try { & (Join-Path $PSScriptRoot 'check-toolchain.ps1') -ToolRoot $fixture -SdkHome (Join-Path $fixture 'sdk') -CommandLine | Out-Null }
  catch { $rejected = $true }
  if (!$rejected) { throw "Preflight accepted incompatible metadata: $Case" }
  Write-Output "Rejected: $Case"
}
Write-Fixture
$result = & (Join-Path $PSScriptRoot 'check-toolchain.ps1') -ToolRoot $fixture -SdkHome (Join-Path $fixture 'sdk') -CommandLine
if ($result.sdkVersion -cne $expected.sdkVersion) { throw 'Matching metadata was not accepted' }
foreach ($package in @('hvigor', 'hvigor-ohos-plugin')) {
  Write-Fixture
  @{ version = '6.24.2' } | ConvertTo-Json | Set-Content (Join-Path $fixture "hvigor/$package/package.json")
  Assert-Rejected "old $package"
}
Write-Fixture
@{ version = $expected.sdkVersion; apiVersion = '24'; platformVersion = $expected.modelVersion } | ConvertTo-Json | Set-Content (Join-Path $fixture 'sdk/default/openharmony/ets/oh-uni-package.json')
Assert-Rejected 'SDK API does not match its version'
Write-Fixture
'# Version: 6.1.1.280' | Set-Content (Join-Path $fixture 'version.txt')
Assert-Rejected 'old CLI distribution'
Write-Fixture
Remove-Item -LiteralPath (Join-Path $fixture 'hvigor/hvigor/package.json')
Assert-Rejected 'incomplete extraction'
Write-Output 'Toolchain metadata contract passed; native compilation has not run.'
