# Builds an Ebur release: the three programs, a portable zip and, when Inno
# Setup is installed, the installer. Run from anywhere:
#
#   powershell -ExecutionPolicy Bypass -File build\build.ps1 -Version 0.1.0
#
# Output goes to dist\ (ignored by git).
param(
    [string]$Version = "0.1.0-dev",
    [switch]$SkipTests
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo
$dist = Join-Path $repo 'dist'
$stage = Join-Path $dist "ebur-$Version"

function Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Check($what) { if ($LASTEXITCODE -ne 0) { throw "$what failed (exit $LASTEXITCODE)" } }

Step "Frontend"
Push-Location app\frontend
npm ci --no-fund --no-audit; Check 'npm ci'
npm run build; Check 'npm run build'
Pop-Location

if (-not $SkipTests) {
    Step "Tests"
    go test ./...; Check 'go test'
}

Step "Programs (version $Version)"
Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory $stage | Out-Null
$ld = "-s -w -X main.version=$Version"
go build -trimpath -ldflags $ld -o "$stage\ebur.exe" ./cli; Check 'build ebur.exe'
go build -trimpath -ldflags "$ld -H windowsgui" -o "$stage\ebur-app.exe" ./app; Check 'build ebur-app.exe'
go build -trimpath -ldflags "-s -w" -o "$stage\ebur-helper.exe" ./helper; Check 'build ebur-helper.exe'
Copy-Item THIRD_PARTY.md, build\ebur.ico $stage

Step "Portable zip"
$zip = Join-Path $dist "ebur-$Version-windows-x64.zip"
Remove-Item $zip -ErrorAction SilentlyContinue
Compress-Archive -Path "$stage\*" -DestinationPath $zip

$iscc = @(
    (Get-Command iscc.exe -ErrorAction SilentlyContinue).Source,
    "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
    "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe"
) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
$setup = $null
if ($iscc) {
    Step "Installer (Inno Setup)"
    $num = ($Version -split '[-+]')[0]   # "0.1.0-dev" -> "0.1.0" for Windows version fields
    & $iscc /Q "/DAppVersion=$Version" "/DNumVersion=$num" "/DStageDir=$stage" "/DOutDir=$dist" build\installer.iss; Check 'ISCC'
    $setup = Join-Path $dist "ebur-setup-$Version.exe"
} else {
    Write-Host "Inno Setup not found: skipping the installer (the zip is complete)." -ForegroundColor Yellow
}

Step "Sizes (NFR-01: download <= 20 MB)"
$items = @(Get-ChildItem "$stage\*.exe") + @(Get-Item $zip)
if ($setup) { $items += Get-Item $setup }
foreach ($f in $items) { "{0,-40} {1,6:N1} MB" -f $f.Name, ($f.Length / 1MB) }
$download = if ($setup) { (Get-Item $setup).Length } else { (Get-Item $zip).Length }
if ($download -gt 20MB) { Write-Host "Over the 20 MB target." -ForegroundColor Red } else { Write-Host "Within the 20 MB target." -ForegroundColor Green }
