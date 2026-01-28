package services

import (
	"log"

	"opspilot-backend/internal/models"
)

type SlackClient struct {
	enabled bool
}

func NewSlackClient() *SlackClient {
	return &SlackClient{enabled: false}
}

func (s *SlackClient) SendAlert(incident *models.Incident, agent *models.Agent) error {
	if !s.enabled {
		agentID := "unknown"
		if agent != nil {
			agentID = agent.AgentID
		}
		log.Printf("Slack disabled, would send alert: incident=%d agent=%s", incident.ID, agentID)
		return nil
	}
	return nil
}

func (s *SlackClient) IsEnabled() bool {
	return s.enabled
}
