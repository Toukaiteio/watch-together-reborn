param(
  [AllowEmptyString()]
  [ValidateSet("", "windows", "linux", "darwin")]
  [string]$Platform = "",
  [string]$SourceDir = "build/ffmpeg/current",
  [string]$BinDir = "build/bin"
)

$ErrorActionPreference = "Stop"

if (-not $Platform) {
  if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)) { $Platform = "windows" }
  elseif ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Linux)) { $Platform = "linux" }
  elseif ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)) { $Platform = "darwin" }
  else { throw "Unable to detect platform. Pass -Platform explicitly." }
}

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
$binaryName = if ($Platform -eq "windows") { "ffmpeg.exe" } else { "ffmpeg" }
$source = Join-Path (Join-Path $root $SourceDir) $binaryName

if (-not (Test-Path -LiteralPath $source)) {
  throw "Prepared ffmpeg not found at $source. Run scripts/prepare-ffmpeg.ps1 first."
}

if ($Platform -eq "darwin") {
  $app = Get-ChildItem -LiteralPath (Join-Path $root $BinDir) -Filter "*.app" -Directory | Select-Object -First 1
  if (-not $app) {
    throw "No .app bundle found in $BinDir"
  }
  $targetDir = Join-Path $app.FullName "Contents/MacOS"
} else {
  $targetDir = Join-Path $root $BinDir
}

New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
$target = Join-Path $targetDir $binaryName
Copy-Item -LiteralPath $source -Destination $target -Force
if ($Platform -ne "windows") {
  chmod +x $target
}

Write-Host "Copied ffmpeg to $target"
