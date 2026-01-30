package natsauth

import (
	"context"
	"log"

	"opspilot-backend/internal/models"
	"opspilot-backend/internal/storage"
)

type ConflictService struct {
	store *storage.Storage
}

func NewConflictService(store *storage.Storage) *ConflictService {
	return &ConflictService{store: store}
}

func (s *ConflictService) OnAgentConnect(ctx context.Context, agentID, remoteIP, hostname, natsClientID string) {
	existing, err := s.store.GetActiveConnection(ctx, agentID)
	if err != nil {
		log.Printf("ERROR conflict check failed: %v", err)
	}

	if existing != nil && existing.RemoteIP != remoteIP {
		conflict := models.AgentConflict{
			AgentID:          agentID,
			ExistingIP:       existing.RemoteIP,
			NewIP:            remoteIP,
			ExistingHostname: existing.Hostname,
			NewHostname:      hostname,
			Resolution:       "pending",
		}
		if err := s.store.RecordAgentConflict(ctx, conflict); err != nil {
			log.Printf("ERROR conflict record failed: %v", err)
		}

		if agent, err := s.store.GetAgentByAgentID(agentID); err == nil && agent != nil && agent.OrgID != "" {
			PublishConflictEvent(agent.OrgID, conflict)
		}
	}

	if err := s.store.RecordAgentConnection(ctx, models.AgentConnection{
		AgentID:      agentID,
		NATSClientID: natsClientID,
		RemoteIP:     remoteIP,
		Hostname:     hostname,
	}); err != nil {
		log.Printf("ERROR conflict connection record failed: %v", err)
	}
}

func (s *ConflictService) OnAgentDisconnect(ctx context.Context, agentID string, reason string) {
	if err := s.store.RecordAgentDisconnect(ctx, agentID, reason); err != nil {
		log.Printf("ERROR conflict disconnect record failed: %v", err)
	}
}
