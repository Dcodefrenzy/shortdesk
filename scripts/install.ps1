# Installs the latest Shortdesk on Windows. In PowerShell:
#   irm https://raw.githubusercontent.com/Dcodefrenzy/shortdesk/main/scripts/install.ps1 | iex
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$repo = 'Dcodefrenzy/shortdesk'
$base = if ($env:SHORTDESK_DOWNLOAD_URL) { $env:SHORTDESK_DOWNLOAD_URL } else { "https://github.com/$repo/releases/latest/download" }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$dir = Join-Path $env:LOCALAPPDATA 'Programs\shortdesk'
$zip = Join-Path $env:TEMP "shortdesk_windows_$arch.zip"

Write-Host "Downloading Shortdesk for windows/$arch..."
Invoke-WebRequest "$base/shortdesk_windows_$arch.zip" -OutFile $zip -UseBasicParsing
New-Item -ItemType Directory -Force $dir | Out-Null
Expand-Archive $zip -DestinationPath $dir -Force
Remove-Item $zip

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($userPath -split ';') -contains $dir)) {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
  $env:Path += ";$dir"
}
Write-Host "Installed $(& "$dir\shortdesk.exe" --version) to $dir"
Write-Host ''
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
  Write-Host 'Shortdesk needs Docker Desktop. Install it first: https://docs.docker.com/desktop/setup/install/windows-install/'
  Write-Host 'Then start Docker Desktop and run:  shortdesk up'
} else {
  Write-Host 'Next: make sure Docker Desktop is running, then run:  shortdesk up'
}
