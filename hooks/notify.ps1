# Claude Controller - Notification Hook (Windows)
# Fire-and-forget: posts notification to server.

$ErrorActionPreference = "SilentlyContinue"

$input_data = $input | Out-String | ConvertFrom-Json
if (-not $input_data) { exit 0 }

$message = $input_data.message
$cwd = $input_data.cwd

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

try {
    Invoke-RestMethod -Uri "$server_url/api/status" -Headers $headers -TimeoutSec 2 | Out-Null
} catch {
    exit 0
}

$register_body = @{ computer_name = $computer_name; project_path = $cwd } | ConvertTo-Json
try {
    $session = Invoke-RestMethod -Method Post -Uri "$server_url/api/sessions/register" -Headers $headers -Body $register_body -TimeoutSec 5
} catch {
    exit 0
}

$prompt_body = @{ session_id = $session.id; claude_message = $message; type = "notification" } | ConvertTo-Json
try {
    Invoke-RestMethod -Method Post -Uri "$server_url/api/prompts" -Headers $headers -Body $prompt_body -TimeoutSec 5 | Out-Null
} catch { }

exit 0
