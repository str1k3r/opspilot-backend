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

func (c *OpenRouterClient) AnalyzeIncident(incident *models.Incident) (*models.AIAnalysis, error) {
	prompt := c.buildPrompt(incident)

	req := OpenRouterRequest{
		Model: "qwen/qwen-2.5-coder-32b-instruct",
		Messages: []Message{
			{
				Role:    "system",
				Content: "You are a Senior Linux SRE. Analyze the error and provide a solution. Output only valid JSON: {\"cause\": \"short explanation\", \"fix_cmd\": \"command or null\"}",
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

	var analysis models.AIAnalysis
	if err := json.Unmarshal([]byte(orResp.Choices[0].Message.Content), &analysis); err != nil {
		return c.fallbackAnalysis(incident), nil
	}

	return &analysis, nil
}

func (c *OpenRouterClient) buildPrompt(incident *models.Incident) string {
	switch incident.Type {
	case "systemd", "docker":
		return fmt.Sprintf(`Service: %s just crashed.
Logs:
%s
Error: %s
Exit Code: %d

Task:
Analyze the root cause in 1 short sentence.
Suggest a fix command (only if safe).
Output JSON: { "cause": "...", "fix_cmd": "systemctl restart nginx" }`,
			incident.Source, incident.RawError, incident.RawError, 1)

	case "host":
		return fmt.Sprintf(`Server is under high load.
Details: %s

Task:
Is this critical? Should we kill any process?
Output JSON: { "cause": "...", "fix_cmd": "kill -9 PID" }`,
			incident.RawError)

	default:
		return fmt.Sprintf(`Error occurred: %s
Source: %s
Type: %s

Task:
Analyze the root cause and suggest a solution.
Output JSON: { "cause": "...", "fix_cmd": "..." }`,
			incident.RawError, incident.Source, incident.Type)
	}
}

func (c *OpenRouterClient) fallbackAnalysis(incident *models.Incident) *models.AIAnalysis {
	cause := "Service crashed unexpectedly"
	fixCmd := ""

	if strings.Contains(incident.RawError, "bind") || strings.Contains(incident.RawError, "port") {
		cause = "Port already in use or binding failed"
		fixCmd = fmt.Sprintf("systemctl restart %s", incident.Source)
	} else if strings.Contains(incident.RawError, "permission") {
		cause = "Permission denied"
		fixCmd = "check permissions and restart"
	}

	return &models.AIAnalysis{
		Cause:  cause,
		FixCmd: fixCmd,
	}
}
