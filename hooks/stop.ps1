# Claude Controller - Stop Hook (Windows)
# Reads Claude's stop event, posts to local server, long-polls for response.

$ErrorActionPreference = "SilentlyContinue"

$input_data = $input | Out-String | ConvertFrom-Json
if (-not $input_data) { exit 0 }

$hook_event = $input_data.hook_event_name
$stop_hook_active = $input_data.stop_hook_active
$cwd = $input_data.cwd
$transcript_path = $input_data.transcript_path

# Load config
$config_path = Join-Path $env:USERPROFILE ".claude-controller.json"
if (-not (Test-Path $config_path)) { exit 0 }

$config = Get-Content $config_path | ConvertFrom-Json
$server_url = if ($config.server_url) { $config.server_url } else { "http://localhost:8080" }
$computer_name = if ($config.computer_name) { $config.computer_name } else { $env:COMPUTERNAME }
$api_key = $config.api_key

$headers = @{
    "Authorization" = "Bearer $api_key"
    "Content-Type" = "application/json"
}

# Check server reachable
try {
    Invoke-RestMethod -Uri "$server_url/api/status" -Headers $headers -TimeoutSec 2 | Out-Null
} catch {
    exit 0
}

# Register session
$register_body = @{ computer_name = $computer_name; project_path = $cwd } | ConvertTo-Json
try {
    $session = Invoke-RestMethod -Method Post -Uri "$server_url/api/sessions/register" -Headers $headers -Body $register_body -TimeoutSec 5
} catch {
    exit 0
}

$session_id = $session.id

if ($stop_hook_active -eq $true) {
    # Check for queued instructions only
    try {
        $instr = Invoke-RestMethod -Uri "$server_url/api/sessions/$session_id/instructions" -Headers $headers -TimeoutSec 5
        if ($instr -and $instr.message) {
            $result = @{ decision = "block"; reason = "User instruction: $($instr.message)" } | ConvertTo-Json -Compress
            Write-Output $result
            exit 0
        }
    } catch { }
    exit 0
}

# Extract Claude's last message from transcript
$claude_msg = "Claude is waiting for input"
if ($transcript_path -and (Test-Path $transcript_path)) {
    try {
        $lines = Get-Content $transcript_path -Tail 20
        foreach ($line in ($lines | Sort-Object -Descending)) {
            $entry = $line | ConvertFrom-Json
            if ($entry.type -eq "assistant" -and $entry.message.content) {
                $content = $entry.message.content
                if ($content -is [array]) {
                    $claude_msg = ($content | Where-Object { $_.type -eq "text" } | ForEach-Object { $_.text }) -join "`n"
                } elseif ($content -is [string]) {
                    $claude_msg = $content
                }
                break
            }
        }
    } catch { }
}

# Post prompt
$prompt_body = @{ session_id = $session_id; claude_message = $claude_msg; type = "prompt" } | ConvertTo-Json
try {
    $prompt = Invoke-RestMethod -Method Post -Uri "$server_url/api/prompts" -Headers $headers -Body $prompt_body -TimeoutSec 5
} catch {
    exit 0
}

$prompt_id = $prompt.id

# Long-poll for response
while ($true) {
    try {
        $poll = Invoke-RestMethod -Uri "$server_url/api/prompts/$prompt_id/response" -Headers $headers -TimeoutSec 35
        if ($poll.status -eq "answered") {
            $response = $poll.response
            $result = @{ decision = "block"; reason = "User responded: $response" } | ConvertTo-Json -Compress
            Write-Output $result
            exit 0
        }
    } catch { }
    Start-Sleep -Seconds 1
}
