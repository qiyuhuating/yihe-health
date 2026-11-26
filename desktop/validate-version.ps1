$ErrorActionPreference='Stop'
$project=Split-Path $PSScriptRoot -Parent
$version=[string](Get-Content (Join-Path $PSScriptRoot 'version.json') -Raw|ConvertFrom-Json).version
$values=@(
 [string](Get-Content (Join-Path $project 'backend/internal/apidoc/openapi-template.json') -Raw|ConvertFrom-Json).info.version,
 [string](Get-Content (Join-Path $PSScriptRoot 'tauri.conf.json') -Raw|ConvertFrom-Json).version,
 [string](Get-Content (Join-Path $PSScriptRoot 'package.json') -Raw|ConvertFrom-Json).version,
 [string](Get-Content (Join-Path $project 'frontend/package.json') -Raw|ConvertFrom-Json).version,
 [string](Get-Content (Join-Path $project 'frontend/package-lock.json') -Raw|ConvertFrom-Json -AsHashtable).version,
 [string](Get-Content (Join-Path $PSScriptRoot 'package-lock.json') -Raw|ConvertFrom-Json -AsHashtable).version,
 [string](Get-Content (Join-Path $PSScriptRoot 'package-lock.json') -Raw|ConvertFrom-Json -AsHashtable).packages[''].version,
 [string](Get-Content (Join-Path $project 'frontend/package-lock.json') -Raw|ConvertFrom-Json -AsHashtable).packages[''].version,
 (Get-Content (Join-Path $project 'backend/VERSION') -Raw).Trim(),
 [regex]::Match((Get-Content (Join-Path $PSScriptRoot 'Cargo.toml') -Raw),'(?m)^version = "([^"]+)"').Groups[1].Value
)
$cargoLock=Get-Content (Join-Path $PSScriptRoot 'Cargo.lock') -Raw
$lockedVersion=[regex]::Match($cargoLock,'(?m)^name = "yihe"\r?\nversion = "([^"]+)"').Groups[1].Value
$values += $lockedVersion
if ($version -notmatch '^\d+\.\d+\.\d+$') { throw 'Invalid canonical release version' }
foreach($value in $values) { if($value -ne $version) { throw "Version mismatch: $value != $version" } }
Write-Host "All product versions agree: $version"
