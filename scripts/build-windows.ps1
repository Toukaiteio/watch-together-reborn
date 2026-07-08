param(
  [string]$Platform = "windows/amd64",
  [switch]$Clean
)

$ErrorActionPreference = "Stop"

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Push-Location $root
try {
  ./scripts/prepare-ffmpeg.ps1 -Platform windows -Arch amd64 -PreferSystem
  if ($Clean) {
    wails build -platform $Platform -clean -skipbindings
  } else {
    wails build -platform $Platform -skipbindings
  }
  Push-Location ./torrent-helper
  try {
    $oldCgo = $env:CGO_ENABLED
    $env:CGO_ENABLED = "0"
    try {
      go build -trimpath -ldflags="-s -w" -o ../build/bin/wt-torrent-helper.exe .
    } finally {
      $env:CGO_ENABLED = $oldCgo
    }
  } finally {
    Pop-Location
  }
  ./scripts/copy-ffmpeg-to-package.ps1 -Platform windows

  $packagePath = Join-Path $root "build\watch-together-reborn-windows-amd64.zip"
  if (Test-Path $packagePath) {
    try {
      Remove-Item -LiteralPath $packagePath -Force
    } catch {
      $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
      $packagePath = Join-Path $root "build\watch-together-reborn-windows-amd64-$stamp.zip"
      Write-Warning "Default package zip is locked; writing $packagePath instead."
    }
  }
  $packageFiles = @(
    "build\bin\watch-together-reborn.exe",
    "build\bin\wt-torrent-helper.exe",
    "build\bin\ffmpeg.exe"
  )
  foreach ($file in $packageFiles) {
    if (-not (Test-Path (Join-Path $root $file))) {
      throw "Missing package file: $file"
    }
  }
  Compress-Archive -Path ($packageFiles | ForEach-Object { Join-Path $root $_ }) -DestinationPath $packagePath -Force
  Write-Host "Created package $packagePath"
} finally {
  Pop-Location
}
