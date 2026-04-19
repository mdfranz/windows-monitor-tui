package main

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
)

type WinEvent struct {
	TimeCreated  string `json:"TimeCreated"`
	ProviderName string `json:"ProviderName"`
	Message      string `json:"Message"`
	Id           int    `json:"Id"`
	Level        int    `json:"Level"`
}

// FetchErrors returns all error strings found in the last 60 minutes
func FetchErrors() []WinEvent {
	// Use PowerShell with ISO date formatting for Go compatibility
	psCommand := `
$since = (Get-Date).AddMinutes(-60)
$logs = @('Application', 'System', 'Setup', 'Microsoft-Windows-CodeIntegrity/Operational', 'Microsoft-Windows-AppLocker/EXE and DLL')
$events = foreach ($log in $logs) {
    Get-WinEvent -LogName $log -ErrorAction SilentlyContinue | Where-Object { $_.TimeCreated -ge $since -and ($_.Level -le 4) }
}
if (-not $events) { exit 0 }
$events | Select-Object @{Name='TimeCreated';Expression={$_.TimeCreated.ToString('yyyy-MM-ddTHH:mm:ssZ')}}, ProviderName, Message, Id, Level | ConvertTo-Json
`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCommand)
	out, err := cmd.Output()
	if err != nil {
		slog.Error("powershell_execution_failed", "error", err)
		return nil
	}

	trimmedOut := strings.TrimSpace(string(out))
	if len(trimmedOut) == 0 || trimmedOut == "null" {
		return nil
	}

	var rawEvents []WinEvent
	if trimmedOut[0] == '{' {
		var e WinEvent
		if err := json.Unmarshal([]byte(trimmedOut), &e); err == nil {
			rawEvents = append(rawEvents, e)
		} else {
			slog.Error("json_unmarshal_single_failed", "error", err, "output", trimmedOut)
		}
	} else {
		if err := json.Unmarshal([]byte(trimmedOut), &rawEvents); err != nil {
			slog.Error("json_unmarshal_list_failed", "error", err, "output", trimmedOut)
			return nil
		}
	}

	return rawEvents
}

