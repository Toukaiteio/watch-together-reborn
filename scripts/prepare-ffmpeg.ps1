param(
  [AllowEmptyString()]
  [ValidateSet("", "windows", "linux", "darwin")]
  [string]$Platform = "",
  [AllowEmptyString()]
  [ValidateSet("", "amd64", "arm64")]
  [string]$Arch = "",
  [string[]]$Url = @(),
  [string]$OutDir = "build/ffmpeg/current",
  [string]$ExistingPath = "",
  [switch]$PreferSystem,
  [switch]$NoProbe,
  [int]$TimeoutSec = 180
)

$ErrorActionPreference = "Stop"

if (-not $Platform) {
  if ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Windows)) { $Platform = "windows" }
  elseif ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::Linux)) { $Platform = "linux" }
  elseif ([System.Runtime.InteropServices.RuntimeInformation]::IsOSPlatform([System.Runtime.InteropServices.OSPlatform]::OSX)) { $Platform = "darwin" }
  else { throw "Unable to detect platform. Pass -Platform explicitly." }
}

if (-not $Arch) {
  if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") {
    $Arch = "arm64"
  } else {
    $Arch = "amd64"
  }
}

if (-not $Url -or $Url.Count -eq 0) {
  if ($Platform -eq "windows" -and $Arch -eq "amd64") {
    $Url = @(
      "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip",
      "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-win64-gpl.zip"
    )
  } elseif ($Platform -eq "windows" -and $Arch -eq "arm64") {
    $Url = @("https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/ffmpeg-master-latest-winarm64-gpl.zip")
  } elseif ($Platform -eq "linux" -and $Arch -eq "amd64") {
    $Url = @("https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz")
  } elseif ($Platform -eq "linux" -and $Arch -eq "arm64") {
    $Url = @("https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz")
  } elseif ($Platform -eq "darwin") {
    $Url = @("https://evermeet.cx/ffmpeg/getrelease/zip")
  } else {
    throw "No default ffmpeg download URL for $Platform/$Arch. Pass -Url explicitly."
  }
}

$root = Resolve-Path (Join-Path $PSScriptRoot "..")
$outPath = Join-Path $root $OutDir
$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("wt-ffmpeg-" + [guid]::NewGuid().ToString("N"))
$archive = Join-Path $temp "ffmpeg.archive"
$extract = Join-Path $temp "extract"
$binaryName = if ($Platform -eq "windows") { "ffmpeg.exe" } else { "ffmpeg" }

function Copy-PreparedFFmpeg {
  param([string]$Path)

  if (-not (Test-Path -LiteralPath $Path)) {
    throw "ffmpeg not found at $Path"
  }
  if (Test-Path -LiteralPath $outPath) {
    Remove-Item -LiteralPath $outPath -Recurse -Force
  }
  New-Item -ItemType Directory -Path $outPath | Out-Null
  Copy-Item -LiteralPath $Path -Destination (Join-Path $outPath $binaryName) -Force
  if ($Platform -ne "windows") {
    chmod +x (Join-Path $outPath $binaryName)
  }
  Write-Host "Prepared ffmpeg from existing binary at $outPath"
}

function Save-UrlWithProgress {
  param(
    [string]$Uri,
    [string]$Destination,
    [int]$TimeoutSeconds
  )

  Add-Type -AssemblyName System.Net.Http
  $handler = [System.Net.Http.HttpClientHandler]::new()
  $handler.AllowAutoRedirect = $true
  $client = [System.Net.Http.HttpClient]::new($handler)
  $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)

  $response = $null
  $inputStream = $null
  $outputStream = $null
  try {
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Get, $Uri)
    $response = $client.SendAsync(
      $request,
      [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead
    ).GetAwaiter().GetResult()
    $response.EnsureSuccessStatusCode() | Out-Null

    $total = $response.Content.Headers.ContentLength
    $inputStream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    $outputStream = [System.IO.File]::Open($Destination, [System.IO.FileMode]::Create, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)

    $buffer = New-Object byte[] (1024 * 1024)
    $downloaded = 0L
    $lastReport = [DateTime]::UtcNow.AddSeconds(-2)

    while ($true) {
      $read = $inputStream.Read($buffer, 0, $buffer.Length)
      if ($read -le 0) { break }
      $outputStream.Write($buffer, 0, $read)
      $downloaded += $read

      $now = [DateTime]::UtcNow
      if (($now - $lastReport).TotalSeconds -ge 1) {
        if ($total) {
          $percent = [Math]::Round(($downloaded * 100.0) / $total, 1)
          Write-Host ("Downloaded {0:N1} MB / {1:N1} MB ({2}%)" -f ($downloaded / 1MB), ($total / 1MB), $percent)
        } else {
          Write-Host ("Downloaded {0:N1} MB" -f ($downloaded / 1MB))
        }
        $lastReport = $now
      }
    }

    Write-Host ("Download complete: {0:N1} MB" -f ($downloaded / 1MB))
  } finally {
    if ($outputStream) { $outputStream.Dispose() }
    if ($inputStream) { $inputStream.Dispose() }
    if ($response) { $response.Dispose() }
    $client.Dispose()
    $handler.Dispose()
  }
}

function Test-UrlPrefix {
  param(
    [string]$Uri,
    [int]$Bytes = 1048576,
    [int]$TimeoutSeconds = 30
  )

  Add-Type -AssemblyName System.Net.Http
  $handler = [System.Net.Http.HttpClientHandler]::new()
  $handler.AllowAutoRedirect = $true
  $client = [System.Net.Http.HttpClient]::new($handler)
  $client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)

  $response = $null
  $stream = $null
  try {
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Get, $Uri)
    $request.Headers.Range = [System.Net.Http.Headers.RangeHeaderValue]::new(0, $Bytes - 1)
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $response = $client.SendAsync(
      $request,
      [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead
    ).GetAwaiter().GetResult()
    $response.EnsureSuccessStatusCode() | Out-Null

    $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
    $buffer = New-Object byte[] 65536
    $downloaded = 0
    while ($downloaded -lt $Bytes) {
      $read = $stream.Read($buffer, 0, [Math]::Min($buffer.Length, $Bytes - $downloaded))
      if ($read -le 0) { break }
      $downloaded += $read
    }
    $sw.Stop()

    if ($downloaded -le 0) {
      throw "received no data"
    }

    return [pscustomobject]@{
      Url = $Uri
      Bytes = $downloaded
      ElapsedMs = [Math]::Max(1, [int]$sw.Elapsed.TotalMilliseconds)
    }
  } finally {
    if ($stream) { $stream.Dispose() }
    if ($response) { $response.Dispose() }
    $client.Dispose()
    $handler.Dispose()
  }
}

if ($ExistingPath) {
  Copy-PreparedFFmpeg -Path $ExistingPath
  return
}

if ($PreferSystem) {
  $systemFFmpeg = Get-Command ffmpeg -ErrorAction SilentlyContinue
  if ($systemFFmpeg) {
    Copy-PreparedFFmpeg -Path $systemFFmpeg.Source
    return
  }
}

New-Item -ItemType Directory -Path $temp, $extract | Out-Null
try {
  if ($Url.Count -gt 1 -and -not $NoProbe) {
    Write-Host "Probing ffmpeg download mirrors..."
    $probeResults = @()
    foreach ($candidateUrl in $Url) {
      try {
        $probe = Test-UrlPrefix -Uri $candidateUrl -TimeoutSeconds ([Math]::Min($TimeoutSec, 30))
        $mbps = [Math]::Round(($probe.Bytes / 1MB) / ($probe.ElapsedMs / 1000.0), 2)
        Write-Host ("Mirror OK: {0} ms, {1} MB/s - {2}" -f $probe.ElapsedMs, $mbps, $candidateUrl)
        $probeResults += $probe
      } catch {
        Write-Warning "Mirror probe failed for ${candidateUrl}: $($_.Exception.Message)"
      }
    }
    if ($probeResults.Count -gt 0) {
      $Url = @($probeResults | Sort-Object ElapsedMs | ForEach-Object { $_.Url })
      Write-Host "Selected primary mirror: $($Url[0])"
    }
  }

  $downloaded = $false
  $lastError = $null
  foreach ($candidateUrl in $Url) {
    try {
      Write-Host "Downloading ffmpeg for $Platform/$Arch"
      Write-Host "Source: $candidateUrl"
      Save-UrlWithProgress -Uri $candidateUrl -Destination $archive -TimeoutSeconds $TimeoutSec
      $downloaded = $true
      $Url = $candidateUrl
      break
    } catch {
      $lastError = $_
      Write-Warning "ffmpeg download failed from ${candidateUrl}: $($_.Exception.Message)"
      if (Test-Path -LiteralPath $archive) {
        Remove-Item -LiteralPath $archive -Force
      }
    }
  }

  if (-not $downloaded) {
    throw "Unable to download ffmpeg. Last error: $lastError"
  }

  if ($Url.EndsWith(".zip") -or $Platform -in @("windows", "darwin")) {
    Expand-Archive -LiteralPath $archive -DestinationPath $extract -Force
  } else {
    tar -xf $archive -C $extract
  }

  $ffmpeg = Get-ChildItem -LiteralPath $extract -Recurse -File |
    Where-Object { $_.Name -eq $binaryName } |
    Select-Object -First 1

  if (-not $ffmpeg) {
    throw "Downloaded archive did not contain $binaryName"
  }

  if (Test-Path -LiteralPath $outPath) {
    Remove-Item -LiteralPath $outPath -Recurse -Force
  }
  New-Item -ItemType Directory -Path $outPath | Out-Null
  Copy-Item -LiteralPath $ffmpeg.FullName -Destination (Join-Path $outPath $binaryName) -Force

  if ($Platform -ne "windows") {
    chmod +x (Join-Path $outPath $binaryName)
  }

  Write-Host "Prepared ffmpeg at $outPath"
} finally {
  if (Test-Path -LiteralPath $temp) {
    Remove-Item -LiteralPath $temp -Recurse -Force
  }
}
