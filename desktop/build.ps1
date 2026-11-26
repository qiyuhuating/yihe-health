param([switch]$SelfSign)
$ErrorActionPreference='Stop'
$project=Split-Path $PSScriptRoot -Parent
$cargoTarget=Join-Path $PSScriptRoot 'target'
if($env:CARGO_TARGET_DIR){
 $cargoTarget=if([IO.Path]::IsPathRooted($env:CARGO_TARGET_DIR)){$env:CARGO_TARGET_DIR}else{Join-Path $PSScriptRoot $env:CARGO_TARGET_DIR}
 $cargoTarget=[IO.Path]::GetFullPath($cargoTarget)
}
& (Join-Path $PSScriptRoot 'validate-version.ps1')
$meta=Get-Content (Join-Path $PSScriptRoot 'version.json') -Raw|ConvertFrom-Json
$channel = if ($env:YIHE_RELEASE_CHANNEL) { $env:YIHE_RELEASE_CHANNEL } else { [string]$meta.channel }
if ($channel -notin @('preview', 'stable')) { throw 'Invalid release channel' }
$out=Join-Path $project ("outputs/v{0}-{1}" -f $meta.version,$channel)
New-Item -ItemType Directory -Force -Path $out | Out-Null
Push-Location (Join-Path $project 'frontend')
try {
 npm ci
 if($LASTEXITCODE -ne 0){throw 'Frontend dependency installation failed'}
 npm run build
 if($LASTEXITCODE -ne 0){throw 'Frontend production build failed'}
} finally { Pop-Location }
$web=Join-Path $project 'backend/internal/handlers/web'
Copy-Item -Recurse -Force (Join-Path $project 'frontend/dist/*') $web
$bin=Join-Path $PSScriptRoot 'binaries/yihe-server-x86_64-pc-windows-msvc.exe'
New-Item -ItemType Directory -Force (Split-Path $bin -Parent)|Out-Null
Push-Location (Join-Path $project 'backend')
try {
 go test -count=1 ./...
 if($LASTEXITCODE -ne 0){throw 'Backend tests failed'}
 go vet ./...
 if($LASTEXITCODE -ne 0){throw 'Backend static checks failed'}
 $env:CGO_ENABLED='0'
 go build -trimpath  -ldflags '-s -w' -o $bin ./cmd/server
 if($LASTEXITCODE -ne 0){throw 'Go sidecar build failed'}
} finally { Pop-Location }
if($SelfSign){& (Join-Path $PSScriptRoot 'sign.ps1') -FilePath $bin | Set-Content (Join-Path $out 'backend-signature.json')}
$hash=(Get-FileHash $bin -Algorithm SHA256).Hash.ToLowerInvariant()
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'backend-sha256.txt'),$hash+"`n",[Text.UTF8Encoding]::new($false))
Push-Location $PSScriptRoot
try {
 npm ci
 if($LASTEXITCODE -ne 0){throw 'Tauri tooling installation failed'}
 node (Join-Path $PSScriptRoot 'node_modules/@tauri-apps/cli/tauri.js') build --no-bundle --ci -- --locked
 if($LASTEXITCODE -ne 0){throw 'Tauri native build failed'}
 if($SelfSign){& (Join-Path $PSScriptRoot 'sign.ps1') -FilePath (Join-Path $cargoTarget 'release/yihe.exe') | Set-Content (Join-Path $out 'application-signature.json')}
 $previousTemp=$env:TEMP
 $previousTmp=$env:TMP
 $bundleTemp=Join-Path $out 'bundle-temp'
 New-Item -ItemType Directory -Force $bundleTemp|Out-Null
 try {
  $env:TEMP=$bundleTemp
  $env:TMP=$bundleTemp
  node (Join-Path $PSScriptRoot 'node_modules/@tauri-apps/cli/tauri.js') bundle --bundles nsis --ci --no-binary-patching
  if($LASTEXITCODE -ne 0){throw 'Tauri Windows installer bundling failed'}
 } finally {
  $env:TEMP=$previousTemp
  $env:TMP=$previousTmp
 }
} finally { Pop-Location }
$setup=Get-ChildItem (Join-Path $cargoTarget 'release/bundle/nsis') -Filter '*-setup.exe'|Sort-Object LastWriteTime -Descending|Select-Object -First 1
if(!$setup){throw 'NSIS installer missing'}
$destination=Join-Path $out ("Yihe-Setup-{0}-win-x64.exe" -f $meta.version)
Copy-Item -LiteralPath $setup.FullName -Destination $destination -Force
if($SelfSign){& (Join-Path $PSScriptRoot 'sign.ps1') -FilePath $destination | Set-Content (Join-Path $out 'installer-signature.json')}
Get-FileHash -LiteralPath $destination -Algorithm SHA256|ForEach-Object {"{0}  {1}" -f $_.Hash.ToLowerInvariant(),(Split-Path $_.Path -Leaf)}|Set-Content (Join-Path $out 'SHA256SUMS.txt')
Write-Host "Built Windows installer: $destination"
