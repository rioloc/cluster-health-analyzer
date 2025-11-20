package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcptypes "github.com/openshift/cluster-health-analyzer/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// AuthRoundTripper holds the transport to use and the auth token.
type AuthRoundTripper struct {
	// The next RoundTripper in the chain. Usually http.DefaultTransport.
	Transport http.RoundTripper
	AuthToken string
}

// RoundTrip implements the http.RoundTripper interface.
func (art *AuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// 1. Pre-process/Modify the request: Add the Authorization header
	req.Header.Set("kubernetes-authorization", fmt.Sprintf("Bearer %s", art.AuthToken))

	// 2. Delegate the request to the underlying transport
	return art.Transport.RoundTrip(req)
}

type MCPIncidentsResponse struct {
	Incidents  mcptypes.Incidents `json:"incidents"`
	NextCursor string             `json:"nextCursor"`
}

func loadScenarioToPromcker(ctx context.Context, scenarioName string) error {
	// Read scenario file from integration/testdata
	scenarioPath := filepath.Join("testdata", scenarioName)
	scenarioData, err := os.ReadFile(scenarioPath)
	if err != nil {
		return fmt.Errorf("failed to read scenario file %s: %w", scenarioPath, err)
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Send POST request to promcker
	req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost:8888/api/v1/scenario", bytes.NewReader(scenarioData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/yaml")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send scenario to promcker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("promcker returned status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("Successfully loaded scenario to promcker")
	return nil
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	promckerC := setupPromcker(ctx)
	incidentsMcpC := setupIncidentsMCP(ctx)

	// load scenario from testdata to promcker
	if err := loadScenarioToPromcker(ctx, "etcd_scheduler_degradation.yaml"); err != nil {
		panic(fmt.Sprintf("failed to load scenario to promcker: %s", err.Error()))
	}

	exitVal := m.Run()

	defer func() {
		err := promckerC.Terminate(ctx)
		if err != nil {
			panic(err)
		}
		err = incidentsMcpC.Terminate(ctx)
		if err != nil {
			panic(err)
		}
		os.Exit(exitVal)
	}()
}

func setupPromcker(ctx context.Context) testcontainers.Container {
	promckerC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:         "promcker",
			Networks:     []string{"testcontainers"},
			Image:        "quay.io/rh-ee-criolo/promcker:latest",
			ExposedPorts: []string{"9090/tcp", "9093/tcp", "8888/tcp"},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.PortBindings = map[nat.Port][]nat.PortBinding{
					"8888/tcp": {{HostIP: "0.0.0.0", HostPort: "8888"}},
				}
			},
			WaitingFor: wait.ForLog("Starting Scenario Manager server on :8888"),
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to start promcker container: %s", err.Error()))
	}
	return promckerC
}

func setupIncidentsMCP(ctx context.Context) testcontainers.Container {
	// Building this using podman cli due to some limitation in the integration between Testcontainers and rootless Podman when the user UID is too high and can't be mapped by rootless podman range within the namespace.
	cmd := exec.Command("podman", "build", "-t", "cluster-health-analyzer:integration", "-f", "Dockerfile", "..")
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

	incidentsMcpC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:       "cluster-health-analyzer-mcp-integration",
			Networks:   []string{"testcontainers"},
			Image:      "cluster-health-analyzer:integration",
			WaitingFor: wait.ForLog("INFO Starting MCP server on  address=:8085"),
			Env: map[string]string{
				"PROM_URL":         "http://promcker:9090",
				"ALERTMANAGER_URL": "http://promcker:9093",
			},
			Cmd:          []string{"mcp"},
			ExposedPorts: []string{"8085/tcp"},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.PortBindings = map[nat.Port][]nat.PortBinding{
					"8085/tcp": {{HostIP: "0.0.0.0", HostPort: "8085"}},
				}
			},
		},
		Started: true,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to start cluster-health-analyzer container: %s", err.Error()))
	}

	return incidentsMcpC
}

func Test_IncidentsHandler(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	transport := &mcp.StreamableClientTransport{
		Endpoint: "http://localhost:8085",
		HTTPClient: &http.Client{
			Transport: &AuthRoundTripper{
				Transport: http.DefaultTransport,
				AuthToken: "test-token",
			},
		},
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fail()
	}
	assert.Nil(t, err)
	defer cs.Close()

	response, err := getIncidentsToolCall(ctx, cs, t, "")
	if err != nil {
		log.Println("err", err.Error())
		t.Fail()
	}
	assert.Equal(t, 2, response.Incidents.Total)

	for {
		if response.NextCursor == "" {
			break
		}
		response, err = getIncidentsToolCall(ctx, cs, t, response.NextCursor)
		if err != nil {
			t.Fail()
		}
		assert.Equal(t, 2, response.Incidents.Total)
	}

}

func getIncidentsToolCall(ctx context.Context, cs *mcp.ClientSession, t *testing.T, nextCursor string) (MCPIncidentsResponse, error) {
	params := fmt.Sprintf(`{"time_range": 360, "min_severity": "info", "next_cursor": "%s"}`, nextCursor)
	log.Println(params)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_incidents",
		Arguments: json.RawMessage(params),
	})
	if err != nil {
		return MCPIncidentsResponse{}, err
	}

	textContent, ok := res.Content[0].(*mcp.TextContent)
	assert.True(t, ok)
	// The regular expression to extract the JSON
	re := regexp.MustCompile(`<DATA>\s*([\s\S]*?)\s*</DATA>`)
	text := textContent.Text
	match := re.FindStringSubmatch(text)
	assert.NotEmpty(t, match)
	assert.Len(t, match, 2, "Expected regex to capture JSON content")
	jsonContent := match[1]
	var response MCPIncidentsResponse
	err = json.Unmarshal([]byte(jsonContent), &response)
	return response, err
}
