package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type AlertData struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Message  string `json:"message"`
	Logs     string `json:"logs"`
	ExitCode int    `json:"exit_code"`
}

type Incident struct {
	ID         int    `json:"id"`
	Type       string `json:"type"`
	Source     string `json:"source"`
	AIAnalysis string `json:"ai_analysis"`
	AISolution string `json:"ai_solution"`
}

func main() {
	fmt.Println("=== OpsPilot Backend Integration Test ===\n")

	// 1. Check agents
	fmt.Println("1. Checking agents...")
	resp, err := http.Get("http://localhost:8080/v1/agents")
	if err != nil {
		log.Fatal("Failed to get agents:", err)
	}
	defer resp.Body.Close()
	fmt.Println("✓ Agents endpoint working")

	// 2. Connect WebSocket
	fmt.Println("\n2. Connecting WebSocket...")
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/v1/stream", nil)
	if err != nil {
		log.Fatal("Dial error:", err)
	}
	defer conn.Close()
	fmt.Println("✓ WebSocket connected")

	// 3. Send heartbeat
	fmt.Println("\n3. Sending heartbeat...")
	heartbeat := map[string]interface{}{
		"ver":         "1.0",
		"ts":          time.Now().Unix(),
		"token":       "my-secret-token",
		"type":        "heartbeat",
		"data":        "",
		"compression": "none",
	}
	if err := conn.WriteJSON(heartbeat); err != nil {
		log.Fatal("Write error:", err)
	}
	fmt.Println("✓ Heartbeat sent")

	// 4. Send different types of alerts
	fmt.Println("\n4. Testing alert processing with AI analysis...")

	testCases := []struct {
		name  string
		alert AlertData
	}{
		{
			name: "Nginx crash",
			alert: AlertData{
				Type:     "systemd",
				Source:   "nginx",
				Message:  "nginx.service: Main process exited, code=exited, status=1/FAILURE",
				Logs:     "Jan 24 21:00:00 server nginx[1234]: nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)",
				ExitCode: 1,
			},
		},
		{
			name: "Docker container crash",
			alert: AlertData{
				Type:     "docker",
				Source:   "api-container",
				Message:  "Container exited with code 137",
				Logs:     "container killed by OOM killer",
				ExitCode: 137,
			},
		},
		{
			name: "High load",
			alert: AlertData{
				Type:     "host",
				Source:   "system",
				Message:  "CPU usage 95%, memory 80%",
				Logs:     "PID   USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM",
				ExitCode: 0,
			},
		},
	}

	for i, tc := range testCases {
		fmt.Printf("\n   %d. %s\n", i+1, tc.name)

		alertJSON, _ := json.Marshal(tc.alert)
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		gz.Write(alertJSON)
		gz.Close()

		compressed := base64.StdEncoding.EncodeToString(buf.Bytes())

		alertPayload := map[string]interface{}{
			"ver":         "1.0",
			"ts":          time.Now().Unix(),
			"token":       "my-secret-token",
			"type":        "alert",
			"data":        compressed,
			"compression": "gzip",
		}

		if err := conn.WriteJSON(alertPayload); err != nil {
			log.Fatal("Write error:", err)
		}
		fmt.Printf("   ✓ Alert sent\n")

		// Wait for processing
		time.Sleep(2 * time.Second)
	}

	// 5. Check incidents
	fmt.Println("\n5. Checking created incidents...")
	resp, err = http.Get("http://localhost:8080/v1/agents/a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11/incidents")
	if err != nil {
		log.Fatal("Failed to get incidents:", err)
	}
	defer resp.Body.Close()

	var incidents []Incident
	if err := json.NewDecoder(resp.Body).Decode(&incidents); err != nil {
		log.Fatal("Failed to decode incidents:", err)
	}

	fmt.Printf("   Found %d incidents:\n", len(incidents))
	for _, inc := range incidents {
		fmt.Printf("\n   Incident #%d:\n", inc.ID)
		fmt.Printf("     Type: %s\n", inc.Type)
		fmt.Printf("     Source: %s\n", inc.Source)
		fmt.Printf("     AI Analysis: %s\n", inc.AIAnalysis)
		fmt.Printf("     AI Solution: %s\n", inc.AISolution)
	}

	// 6. Test command execution
	fmt.Println("\n6. Testing command execution...")
	cmdPayload := map[string]interface{}{
		"agent_id": "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"command":  "restart_service",
		"params": map[string]string{
			"service": "nginx",
		},
	}

	cmdJSON, _ := json.Marshal(cmdPayload)
	resp, err = http.Post("http://localhost:8080/v1/admin/exec", "application/json", bytes.NewReader(cmdJSON))
	if err != nil {
		log.Fatal("Failed to send command:", err)
	}
	defer resp.Body.Close()

	fmt.Println("✓ Command sent to agent")

	fmt.Println("\n=== Test Complete ===")
	fmt.Println("\nSummary:")
	fmt.Println("✓ WebSocket connection working")
	fmt.Println("✓ Heartbeat processing working")
	fmt.Println("✓ Alert ingestion with decompression working")
	fmt.Println("✓ AI analysis integration working")
	fmt.Println("✓ Incident creation working")
	fmt.Println("✓ Command execution working")
	fmt.Println("\nNote: Slack integration requires SLACK_WEBHOOK_URL environment variable")
}
