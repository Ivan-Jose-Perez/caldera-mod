package contact

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/mitre/gocat/output"
)

// TCP communication
type TCP struct {
	name   string
	socket string
}

func init() {
	CommunicationChannels["TCP"] = &TCP{name: "TCP"}
}

// GetBeaconBytes sends a beacon and returns instructions
func (t *TCP) GetBeaconBytes(profile map[string]interface{}) []byte {
	conn, err := net.Dial("tcp", t.socket)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] TCP connection error: %s", err))
		return nil
	}
	defer conn.Close()

	// Send profile as beacon
	data, err := json.Marshal(profile)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Cannot marshal profile: %s", err.Error()))
		return nil
	}

	// Send profile for handshake
	conn.Write(data)
	conn.Write([]byte("\n"))

	// Read paw response
	pawData := make([]byte, 512)
	n, err := conn.Read(pawData)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Error reading paw: %s", err.Error()))
		return nil
	}
	paw := string(pawData[:n])
	profile["paw"] = strings.TrimSpace(paw)
	conn.Write([]byte("\n"))

	// Wait for instructions
	response, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Error reading instructions: %s", err.Error()))
		return nil
	}

	return response
}

// Other required methods for the Contact interface...
func (t *TCP) GetPayloadBytes(profile map[string]interface{}, payload string) ([]byte, string) {
	// Connect to server via TCP
	conn, err := net.Dial("tcp", t.socket)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] TCP connection error when fetching payload: %s", err))
		return nil, ""
	}
	defer conn.Close()

	// Create a payload request message
	payloadRequest := map[string]interface{}{
		"type":     "payload_request",
		"payload":  payload,
		"platform": profile["platform"],
		"paw":      profile["paw"],
	}

	reqData, err := json.Marshal(payloadRequest)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Cannot marshal payload request: %s", err.Error()))
		return nil, ""
	}

	// Send payload request
	conn.Write(reqData)
	conn.Write([]byte("\n"))

	// Read response (payload content)
	reader := bufio.NewReader(conn)
	responseBytes, err := reader.ReadBytes('\n')
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Error reading payload response: %s", err.Error()))
		return nil, ""
	}

	// Response should be a JSON with payload data and name
	var payloadResponse struct {
		PayloadName string `json:"payload_name"`
		PayloadData []byte `json:"payload_data"`
	}

	if err := json.Unmarshal(responseBytes, &payloadResponse); err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Error unmarshaling payload response: %s", err.Error()))
		return nil, ""
	}

	output.VerbosePrint(fmt.Sprintf("[+] Successfully retrieved payload %s (size: %d bytes)",
		payloadResponse.PayloadName, len(payloadResponse.PayloadData)))

	return payloadResponse.PayloadData, payloadResponse.PayloadName
}

func (t *TCP) C2RequirementsMet(profile map[string]interface{}, criteria map[string]string) (bool, map[string]string) {
	config := make(map[string]string)

	// Check if socket address is provided in criteria
	if socketAddr, ok := criteria["socket"]; ok && len(socketAddr) > 0 {
		t.socket = socketAddr
	} else if serverAddr, ok := criteria["server"]; ok && len(serverAddr) > 0 {
		// Try to extract host:port from server address
		// This handles cases where server is specified as http://host:port
		url := strings.TrimPrefix(serverAddr, "http://")
		url = strings.TrimPrefix(url, "https://")

		// Extract the host part (before any path)
		if pathIndex := strings.Index(url, "/"); pathIndex != -1 {
			url = url[:pathIndex]
		}

		// Add default port if not specified
		if !strings.Contains(url, ":") {
			url = url + ":7010" // Default TCP port for Caldera
		}

		t.socket = url
	} else {
		output.VerbosePrint("[!] No valid socket or server address provided for TCP communication")
		return false, nil
	}

	// Test connection to verify requirements are met
	conn, err := net.DialTimeout("tcp", t.socket, 3*time.Second)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[!] Failed to connect to TCP socket %s: %s", t.socket, err.Error()))
		return false, nil
	}
	conn.Close()

	// If paw is not set, generate a random one
	if len(profile["paw"].(string)) == 0 {
		config["paw"] = generateRandomID()
	}

	output.VerbosePrint(fmt.Sprintf("[+] TCP connection to %s verified", t.socket))
	return true, config
}

// Helper function to generate random ID for paw
func generateRandomID() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	rand.Seed(time.Now().UnixNano())

	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func (t *TCP) SetUpstreamDestAddr(upstreamDestAddr string) {
	t.socket = upstreamDestAddr
}

func (t *TCP) SendExecutionResults(profile map[string]interface{}, result map[string]interface{}) {
	conn, err := net.Dial("tcp", t.socket)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] TCP connection error: %s", err))
		return
	}
	defer conn.Close()

	// Format and send results
	profileCopy := make(map[string]interface{})
	for k, v := range profile {
		profileCopy[k] = v
	}
	results := [1]map[string]interface{}{result}
	profileCopy["results"] = results

	data, err := json.Marshal(profileCopy)
	if err != nil {
		output.VerbosePrint(fmt.Sprintf("[-] Cannot marshal results: %s", err.Error()))
		return
	}

	conn.Write(data)
}

func (t *TCP) GetName() string {
	return t.name
}

func (t *TCP) UploadFileBytes(profile map[string]interface{}, uploadName string, data []byte) error {
	// Connect to server via TCP
	conn, err := net.Dial("tcp", t.socket)
	if err != nil {
		return fmt.Errorf("TCP connection error when uploading file: %s", err)
	}
	defer conn.Close()

	// Create file upload message
	uploadRequest := map[string]interface{}{
		"type":             "file_upload",
		"file_name":        uploadName,
		"file_data_base64": base64.StdEncoding.EncodeToString(data),
		"paw":              profile["paw"],
		"platform":         profile["platform"],
	}

	reqData, err := json.Marshal(uploadRequest)
	if err != nil {
		return fmt.Errorf("cannot marshal file upload request: %s", err.Error())
	}

	// Send upload request
	_, err = conn.Write(reqData)
	if err != nil {
		return fmt.Errorf("error sending upload request: %s", err.Error())
	}
	conn.Write([]byte("\n"))

	// Read response
	reader := bufio.NewReader(conn)
	responseBytes, err := reader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("error reading upload response: %s", err.Error())
	}

	// Parse response to check success
	var uploadResponse struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(responseBytes, &uploadResponse); err != nil {
		return fmt.Errorf("error unmarshaling upload response: %s", err.Error())
	}

	if !uploadResponse.Success {
		return fmt.Errorf("server rejected file upload: %s", uploadResponse.Message)
	}

	output.VerbosePrint(fmt.Sprintf("[+] Successfully uploaded file %s (size: %d bytes)",
		uploadName, len(data)))

	return nil
}

func (t *TCP) SupportsContinuous() bool {
	return true
}
