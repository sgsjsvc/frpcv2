param(
    [string]$Version = "1.0.0"
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$serviceRoot = Join-Path $repoRoot "..\src"
$frontendRoot = Join-Path $repoRoot "frontend"
$binRoot = Join-Path $repoRoot "build\bin"
$windowsBuildRoot = Join-Path $repoRoot "build\windows"
$distRoot = Join-Path $repoRoot "dist"
$portableRoot = Join-Path $distRoot "FRPCClient-portable"
$nsisDir = "C:\Program Files (x86)\NSIS"

if (Test-Path (Join-Path $nsisDir "makensis.exe")) {
    $env:Path = $nsisDir + ";" + $env:Path
}

New-Item -ItemType Directory -Force -Path $binRoot | Out-Null
New-Item -ItemType Directory -Force -Path $windowsBuildRoot | Out-Null
New-Item -ItemType Directory -Force -Path $distRoot | Out-Null

Write-Host "1/5 Build frontend..."
Push-Location $frontendRoot
npm run build
Pop-Location

Write-Host "2/5 Build service host..."
Push-Location $serviceRoot
go build -ldflags "-H=windowsgui" -o (Join-Path $windowsBuildRoot "frpc-service.exe") .\cmd\frpc
Pop-Location

Write-Host "3/5 Build Wails app + NSIS installer..."
Push-Location $repoRoot
wails build -clean -nsis -webview2 embed -o "FRPCClient.exe"
Pop-Location

$guiExe = Join-Path $binRoot "FRPCClient.exe"
$guiNoExt = Join-Path $binRoot "FRPCClient"
if (-not (Test-Path $guiExe) -and (Test-Path $guiNoExt)) {
    Move-Item -LiteralPath $guiNoExt -Destination $guiExe
}

Write-Host "4/5 Build portable bundle..."
if (Test-Path $portableRoot) {
    Remove-Item -LiteralPath $portableRoot -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $portableRoot | Out-Null
Copy-Item -LiteralPath $guiExe -Destination $portableRoot
Copy-Item -LiteralPath (Join-Path $windowsBuildRoot "frpc-service.exe") -Destination $portableRoot

@"
First run:
1. Launch FRPCClient.exe
2. Allow WebView2 installation if prompted
3. Install the Windows service in the app and save your config

Files:
- GUI app: FRPCClient.exe
- Service host: frpc-service.exe
"@ | Set-Content -LiteralPath (Join-Path $portableRoot "README.txt") -Encoding UTF8

$portableZip = Join-Path $distRoot ("FRPCClient-portable-" + $Version + ".zip")
if (Test-Path $portableZip) {
    Remove-Item -LiteralPath $portableZip -Force
}
Compress-Archive -Path (Join-Path $portableRoot "*") -DestinationPath $portableZip -Force

Write-Host "5/5 Collect artifacts..."
$installer = Get-ChildItem -LiteralPath $binRoot -Filter "*installer.exe" | Sort-Object LastWriteTime -Descending | Select-Object -First 1
if ($null -ne $installer) {
    Copy-Item -LiteralPath $installer.FullName -Destination (Join-Path $distRoot ("FRPCClient-installer-" + $Version + ".exe")) -Force
}

Write-Host ""
Write-Host "Artifacts:"
Get-ChildItem -LiteralPath $distRoot | Select-Object Name, Length, LastWriteTime
