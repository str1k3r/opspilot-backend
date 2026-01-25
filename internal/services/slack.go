package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type SlackClient struct {
	webhookURL string
	client     *http.Client
}

type SlackMessage struct {
	Blocks []Block `json:"blocks"`
}

type Block struct {
	Type      string        `json:"type"`
	Text      *Text         `json:"text,omitempty"`
	Elements  []interface{} `json:"elements,omitempty"`
	Accessory *Accessory    `json:"accessory,omitempty"`
}

type Text struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

type Accessory struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url,omitempty"`
	AltText  string `json:"alt_text,omitempty"`
}

type Action struct {
	Type       string `json:"type"`
	Text       Text   `json:"text"`
	Value      string `json:"value"`
	ActionType string `json:"action_type,omitempty"`
}

type SectionBlock struct {
	Type      string     `json:"type"`
	Text      *Text      `json:"text,omitempty"`
	Fields    []*Text    `json:"fields,omitempty"`
	Accessory *Accessory `json:"accessory,omitempty"`
}

func NewSlackClient() *SlackClient {
	webhookURL := os.Getenv("SLACK_WEBHOOK_URL")
	return &SlackClient{
		webhookURL: webhookURL,
		client:     &http.Client{},
	}
}

func (c *SlackClient) SendAlert(agentHostname, incidentSource, incidentType, aiAnalysis, aiSolution string) error {
	if c.webhookURL == "" {
		fmt.Println("No SLACK_WEBHOOK_URL configured, skipping alert")
		return nil
	}

	message := c.buildAlertMessage(agentHostname, incidentSource, incidentType, aiAnalysis, aiSolution)
	return c.sendMessage(message)
}

func (c *SlackClient) buildAlertMessage(hostname, source, incidentType, aiAnalysis, aiSolution string) SlackMessage {
	emoji := "🚨"
	if incidentType == "host" {
		emoji = "⚡"
	}

	analysisText := aiAnalysis
	if aiAnalysis == "" {
		analysisText = "Analysis pending..."
	}

	solutionText := aiSolution
	if aiSolution == "" {
		solutionText = "No solution available"
	}

	return SlackMessage{
		Blocks: []Block{
			{
				Type: "header",
				Text: &Text{
					Type:  "plain_text",
					Text:  fmt.Sprintf("%s Service Down: %s (%s)", emoji, source, hostname),
					Emoji: true,
				},
			},
			{
				Type: "section",
				Text: &Text{
					Type: "mrkdwn",
					Text: "*AI Analysis:*\n" + analysisText,
				},
			},
			{
				Type: "section",
				Text: &Text{
					Type: "mrkdwn",
					Text: "*AI Solution:*\n" + solutionText,
				},
			},
			{
				Type: "actions",
				Elements: []interface{}{
					map[string]interface{}{
						"type":  "button",
						"text":  map[string]interface{}{"type": "plain_text", "text": "🟢 Restart", "emoji": true},
						"value": fmt.Sprintf("restart_%s", source),
					},
					map[string]interface{}{
						"type":  "button",
						"text":  map[string]interface{}{"type": "plain_text", "text": "📜 Logs", "emoji": true},
						"value": fmt.Sprintf("logs_%s", source),
					},
					map[string]interface{}{
						"type":  "button",
						"text":  map[string]interface{}{"type": "plain_text", "text": "🚫 Ignore", "emoji": true},
						"value": fmt.Sprintf("ignore_%d", 0),
					},
				},
			},
		},
	}
}

func (c *SlackClient) sendMessage(message SlackMessage) error {
	reqBody, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	resp, err := c.client.Post(c.webhookURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("post error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack error: %s", string(body))
	}

	return nil
}
