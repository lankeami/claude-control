# Claude Controller Hook Installer (Windows)
# Sets up the hooks in Claude Code settings and creates the config file.

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

Write-Host "=== Claude Controller Hook Installer ==="
Write-Host ""

# Claude settings location
$DefaultClaudeDir = Join-Path $env:USERPROFILE ".claude"
if ($env:CLAUDE_DIR) { $DefaultClaudeDir = $env:CLAUDE_DIR }
$DefaultSettings = Join-Path $DefaultClaudeDir "settings.json"

$inputSettings = Read-Host "Claude settings file [$DefaultSettings]"
$SettingsFile = if ($inputSettings) { $inputSettings } else { $DefaultSettings }

# Config file location
$DefaultConfig = Join-Path $env:USERPROFILE ".claude-controller.json"
$inputConfig = Read-Host "Controller config file [$DefaultConfig]"
$ConfigFile = if ($inputConfig) { $inputConfig } else { $DefaultConfig }

# Computer name
$DefaultName = $env:COMPUTERNAME
$inputName = Read-Host "Computer name [$DefaultName]"
$ComputerName = if ($inputName) { $inputName } else { $DefaultName }

# Server port and URL
$inputPort = Read-Host "Server port [8080]"
$Port = if ($inputPort) { $inputPort } else { "8080" }
$DefaultURL = "http://localhost:$Port"
$inputURL = Read-Host "Server URL [$DefaultURL]"
$ServerURL = if ($inputURL) { $inputURL } else { $DefaultURL }

# API key
$APIKey = Read-Host "API key (from QR code or server output)"

# Write config JSON
$config = [ordered]@{
    server_url    = $ServerURL
    computer_name = $ComputerName
    api_key       = $APIKey
}
$config | ConvertTo-Json | Set-Content -Path $ConfigFile -Encoding UTF8
Write-Host "Config written to $ConfigFile"

# Ensure settings directory and file exist
$SettingsDir = Split-Path -Parent $SettingsFile
if (-not (Test-Path $SettingsDir)) {
    New-Item -ItemType Directory -Force -Path $SettingsDir | Out-Null
}
if (-not (Test-Path $SettingsFile)) {
    Set-Content -Path $SettingsFile -Value "{}" -Encoding UTF8
}

# Build hook commands — embed config path if non-default
$StopHook = Join-Path $ScriptDir "stop.ps1"
$NotifyHook = Join-Path $ScriptDir "notify.ps1"

if ($ConfigFile -eq $DefaultConfig) {
    $StopCmd = "pwsh -NonInteractive -File `"$StopHook`""
    $NotifyCmd = "pwsh -NonInteractive -File `"$NotifyHook`""
} else {
    $StopCmd = "`$env:CLAUDE_CONTROLLER_CONFIG='$ConfigFile'; pwsh -NonInteractive -File `"$StopHook`""
    $NotifyCmd = "`$env:CLAUDE_CONTROLLER_CONFIG='$ConfigFile'; pwsh -NonInteractive -File `"$NotifyHook`""
}

# Patch Claude Code settings.json
$settings = Get-Content $SettingsFile -Raw | ConvertFrom-Json

# Ensure hooks property exists
if (-not $settings.PSObject.Properties['hooks']) {
    $settings | Add-Member -MemberType NoteProperty -Name 'hooks' -Value ([PSCustomObject]@{})
}

$stopEntry = [PSCustomObject]@{
    hooks = @([PSCustomObject]@{ type = "command"; command = $StopCmd })
}
$notifyEntry = [PSCustomObject]@{
    hooks = @([PSCustomObject]@{ type = "command"; command = $NotifyCmd })
}

$settings.hooks | Add-Member -MemberType NoteProperty -Name 'Stop' -Value @($stopEntry) -Force
$settings.hooks | Add-Member -MemberType NoteProperty -Name 'Notification' -Value @($notifyEntry) -Force

$settings | ConvertTo-Json -Depth 10 | Set-Content -Path $SettingsFile -Encoding UTF8
Write-Host "Hooks registered in $SettingsFile"
Write-Host ""
Write-Host "Done! Restart any running Claude Code sessions for hooks to take effect."
