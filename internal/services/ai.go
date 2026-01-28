package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"opspilot-backend/internal/models"
)

type OpenRouterClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

type OpenRouterRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenRouterResponse struct {
	ID      string   `json:"id"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Message Message `json:"message"`
}

func NewOpenRouterClient() *OpenRouterClient {
	apiKey := os.Getenv("OPENROUTER_KEY")
	if apiKey == "" {
		apiKey = "dummy-key"
	}

	return &OpenRouterClient{
		apiKey:  apiKey,
		baseURL: "https://openrouter.ai/api/v1",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

const systemPrompt = `You are an expert Linux SRE (Site Reliability Engineer) & DevOps assistant.
Your goal is to analyze system alerts, identify the root cause, and recommend a SAFE solution.

INPUT DATA:
You will receive an incident report containing:
1. Type (systemd, docker, host_cpu, host_ram, log, etc.)
2. Source (service name, container name, file path, or hostname)
3. Logs (tail of the error logs)
4. Context (rich telemetry: top_processes, load_avg, disk_stats, etc.)

AVAILABLE TOOLS (Actions):
You can suggest ONLY the following actions. If no action is safe/applicable, return null for "suggested_action".

- restart_service (args: {"service": "service_name"})
  Use ONLY if service is stuck or failed cleanly.
  DO NOT use if logs show configuration syntax errors (like "nginx: configuration file test failed").

- docker_restart (args: {"container": "container_name"})
  Use for crashed containers that exited unexpectedly.
  DO NOT use if container exited due to application error that will repeat.

- kill_process (args: {"pid": "pid_number"})
  Use ONLY if a specific process is consuming >90% CPU/RAM for extended time.
  NEVER kill critical system processes: init, systemd, dockerd, sshd, kernel threads (kworker, ksoftirqd).

SAFETY RULES:
1. If logs contain "syntax error", "configuration error", "parse error" - DO NOT suggest restart. Human must fix config first.
2. If the top CPU consumer is a system process (PID 1, kworker, systemd) - DO NOT suggest killing it.
3. When in doubt, set suggested_action to null and explain in analysis what human should check.

OUTPUT FORMAT (Strict JSON only, no markdown):
{
  "analysis": "Brief RCA (Root Cause Analysis). Mention specific log lines or metrics that led to this conclusion.",
  "is_critical": true or false,
  "suggested_action": {
    "cmd": "tool_name",
    "args": {"param_name": "value"},
    "label": "Short button text for UI (e.g., 'Restart nginx', 'Kill PID 1234')"
  }
}

EXAMPLES:
- For kill_process: {"cmd": "kill_process", "args": {"pid": "1234"}, "label": "Kill PID 1234"}
- For restart_service: {"cmd": "restart_service", "args": {"service": "nginx"}, "label": "Restart nginx"}
- For docker_restart: {"cmd": "docker_restart", "args": {"container": "web-app"}, "label": "Restart web-app container"}

If no action is needed or safe, omit "suggested_action" field entirely.`

func (c *OpenRouterClient) AnalyzeIncident(incident *models.Incident) (*models.AIAnalysis, error) {
	prompt := c.buildPrompt(incident)

	fmt.Printf("[AI] Sending prompt:\n%s\n", prompt)

	req := OpenRouterRequest{
		Model: "qwen/qwen-2.5-coder-32b-instruct",
		Messages: []Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://opspilot.local")
	httpReq.Header.Set("X-Title", "OpsPilot")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return c.fallbackAnalysis(incident), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("OpenRouter error: %s\n", string(body))
		return c.fallbackAnalysis(incident), nil
	}

	var orResp OpenRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&orResp); err != nil {
		return c.fallbackAnalysis(incident), nil
	}

	if len(orResp.Choices) == 0 {
		return c.fallbackAnalysis(incident), nil
	}

	aiContent := orResp.Choices[0].Message.Content
	fmt.Printf("[AI] Raw response: %s\n", aiContent)

	// Попытка извлечь JSON из ответа (AI может добавить текст вокруг JSON)
	jsonStr := extractJSON(aiContent)

	var analysis models.AIAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		fmt.Printf("[AI] JSON parse error: %v, using fallback\n", err)
		return c.fallbackAnalysis(incident), nil
	}

	fmt.Printf("[AI] Parsed: analysis=%s, is_critical=%v, suggested_action=%+v\n",
		analysis.Analysis, analysis.IsCritical, analysis.SuggestedAction)
	return &analysis, nil
}

// extractJSON пытается извлечь JSON объект из текста
func extractJSON(s string) string {
	// Ищем первую { и последнюю }
	start := -1
	end := -1
	depth := 0

	for i, c := range s {
		if c == '{' {
			if start == -1 {
				start = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}

	if start >= 0 && end > start {
		return s[start:end]
	}
	return s
}

func (c *OpenRouterClient) buildPrompt(incident *models.Incident) string {
	var sb strings.Builder

	// Header
	sb.WriteString("INCIDENT REPORT\n")
	sb.WriteString(fmt.Sprintf("Type: %s\n", incident.Type))
	sb.WriteString(fmt.Sprintf("Source: %s\n", incident.Source))

	// Context section (most important for host metrics)
	if incident.Context != nil && len(incident.Context) > 0 {
		sb.WriteString("\n[SYSTEM CONTEXT]\n")
		contextBytes, err := json.MarshalIndent(incident.Context, "", "  ")
		if err == nil {
			sb.Write(contextBytes)
			sb.WriteString("\n")
		}
	}

	// Logs section (most important for systemd/docker)
	if incident.RawError != "" {
		sb.WriteString("\n[ERROR LOGS]\n")
		sb.WriteString(incident.RawError)
		sb.WriteString("\n")
	}

	// Final instruction
	sb.WriteString("\nAnalyze the provided Context and Logs. Return JSON with your diagnosis and recommended action (if safe).")

	return sb.String()
}

func (c *OpenRouterClient) fallbackAnalysis(incident *models.Incident) *models.AIAnalysis {
	analysis := "AI service unavailable. Manual analysis required."
	isCritical := false

	// Basic pattern matching for common issues
	rawLower := strings.ToLower(incident.RawError)

	if strings.Contains(rawLower, "bind") || strings.Contains(rawLower, "address already in use") {
		analysis = "Port binding failed - likely another process is using the port. Check with 'ss -tlnp' or 'netstat -tlnp'."
		isCritical = true
	} else if strings.Contains(rawLower, "permission denied") {
		analysis = "Permission denied error. Check file/directory ownership and permissions."
		isCritical = false
	} else if strings.Contains(rawLower, "out of memory") || strings.Contains(rawLower, "oom") {
		analysis = "Out of memory condition detected. Check memory usage and consider adding swap or killing memory-heavy processes."
		isCritical = true
	} else if strings.Contains(rawLower, "disk full") || strings.Contains(rawLower, "no space left") {
		analysis = "Disk space exhausted. Free up space or extend the volume."
		isCritical = true
	} else if strings.Contains(rawLower, "syntax error") || strings.Contains(rawLower, "configuration") {
		analysis = "Configuration syntax error detected. DO NOT restart - fix the configuration file first."
		isCritical = false
	}

	return &models.AIAnalysis{
		Analysis:   analysis,
		IsCritical: isCritical,
		// No suggested action in fallback - human should decide
	}
}
