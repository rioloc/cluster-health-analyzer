package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/utils"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	getIncidentsToolName        = "get_incidents"
	getIncidentsToolDescription = `List the current firing incidents in the cluster.
		One incident is a group of related alerts that are likely triggered by the same root cause.
		Use this tool to analyze the cluster health status and determine why a component is failing or degraded.`
)

var (
	getIncidentsMCPTool = mcp.Tool{
		Name:        getIncidentsToolName,
		Description: getIncidentsToolDescription,
		Annotations: &mcp.ToolAnnotations{
			Title:        "Provides information about Incidents in the cluster",
			ReadOnlyHint: true,
		},
		InputSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"max_age_hours": {
					Type:        "number",
					Description: "Maximum age of incidents to include in hours (max 360 for 15 days). Default: 360",
					Minimum:     utils.Ptr(float64(1)),
					Maximum:     utils.Ptr(float64(360)),
				},
			},
		},
	}
)

type IncidentTool struct {
	mcp.Tool
	consoleURL string
}

// NewIncidentTool creates a new MCP tool for the incidents
func NewIncidentTool() IncidentTool {
	consoleURL, err := getConsoleURL()
	if err != nil {
		slog.Error("Failed to obtain cluster console URL", "error", err)
	}
	return IncidentTool{
		getIncidentsMCPTool,
		consoleURL,
	}
}

type GetIncidentsParams struct {
	MaxAgeHours uint `json:"max_age_hours"`
}

// GetIncidentsHandler is the main handler for the get_incidents tool. It connects to the
// in-cluster Prometheus and queries the Incidents metrics.
func (i *IncidentTool) GetIncidentsHandler(ctx context.Context, request *mcp.CallToolRequest, params GetIncidentsParams) (*mcp.CallToolResult, any, error) {
	slog.Info("Incidents tool received request with ", "params", request.Params, "and arguments ", request.Params.Arguments)
	token, err := getTokenFromCtx(ctx)
	if err != nil {
		slog.Error(err.Error())
		return nil, nil, err
	}

	promURL := os.Getenv("PROM_URL")
	promClient, err := prom.NewPrometheusClientWithToken(promURL, token)
	if err != nil {
		slog.Error("Failed to initialize Prometheus client", "error", err)
		return nil, nil, err
	}

	// default value is 15 days
	maxAgeHours := 360
	if params.MaxAgeHours > 0 {
		maxAgeHours = int(params.MaxAgeHours)
	}

	promAPI := v1.NewAPI(promClient)
	timeNow := time.Now()
	queryTimeRange := v1.Range{
		Start: timeNow.Add(-time.Duration(maxAgeHours) * time.Hour),
		End:   timeNow,
		Step:  300 * time.Second,
	}
	val, warning, err := promAPI.QueryRange(ctx, processor.ClusterHealthComponentsMap, queryTimeRange)
	if err != nil {
		slog.Error("Recieved error response from Prometheus", "error", err)
		return nil, nil, err
	}
	if warning != nil {
		slog.Warn("Prometheus query response", "warning", warning)
	}

	incidentsMap, err := i.transformPromValueToIncident(val, queryTimeRange)
	if err != nil {
		slog.Error("Failed to transform metric data", "error", err)
		return nil, nil, err
	}

	incidents := getAlertDataForIncidents(ctx, incidentsMap, promAPI, queryTimeRange)
	r := Response{
		Incidents: Incidents{
			Total:     len(incidents),
			Incidents: incidents,
		},
	}

	data, err := json.Marshal(r)
	if err != nil {
		slog.Error("Failed to marshal the Incident data", "error", err)
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}, nil, nil
}

// formatToRFC3339 formats a time to RFC3339 string, returns empty string for zero time
func formatToRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// processSampleTime calculates the delta between the two samples and if it's greater
// than the range step then the endTime is set, otherwise it returns zero endTime
func processSampleTime(firstSample, lastSample model.SamplePair, qRange v1.Range) (time.Time, time.Time) {
	startTime := firstSample.Timestamp.Time()
	var endTime time.Time

	if qRange.End.Sub(lastSample.Timestamp.Time()).Seconds() > qRange.Step.Seconds() {
		endTime = lastSample.Timestamp.Time()
	}
	return startTime, endTime
}

// transformPromValueToIncident transforms the metrics data to map of incidents
func (i *IncidentTool) transformPromValueToIncident(data model.Value, qRange v1.Range) (map[string]Incident, error) {
	dataVec, ok := data.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("cannot convert data to Prometheus model.Vector type")
	}

	incidents := make(map[string]Incident, len(dataVec))
	for _, v := range dataVec {
		alertSeverity := v.Metric["src_severity"]
		alertName := v.Metric["src_alertname"]
		if alertSeverity == "none" {
			slog.Debug("Skipping unknown severity ", "alert", alertName, "severity", alertSeverity)
			continue
		}

		lastSample := v.Values[len(v.Values)-1]
		firstSample := v.Values[0]
		startTime, endTime := processSampleTime(firstSample, lastSample, qRange)

		labels := common.SrcLabels(v.Metric)
		healthyVal := processor.HealthValue(lastSample.Value)
		groupId := string(v.Metric["group_id"])
		component := string(v.Metric["component"])

		if existingInc, ok := incidents[groupId]; ok {
			existingInc.ComponentsSet[component] = struct{}{}
			existingInc.AffectedComponents = slices.Collect(maps.Keys(existingInc.ComponentsSet))
			sort.Strings(existingInc.AffectedComponents)

			if _, ok := existingInc.AlertsSet[labels.String()]; !ok {
				existingInc.AlertsSet[labels.String()] = struct{}{}
				existingInc.Alerts = append(existingInc.Alerts, labels)
			}

			if healthyVal > processor.ParseHealthValue(existingInc.Severity) {
				existingInc.Severity = healthyVal.String()
			}
			err := existingInc.UpdateStartTime(startTime)
			if err != nil {
				slog.Error("Failed to parse the start time of an incident ", "error", err)
				continue
			}
			err = existingInc.UpdateEndTime(endTime)
			if err != nil {
				slog.Error("Failed to parse the end time of an incident ", "error", err)
				continue
			}
			existingInc.UpdateStatus()
			incidents[existingInc.GroupId] = existingInc
		} else {
			incident := Incident{
				GroupId:   string(groupId),
				Severity:  healthyVal.String(),
				StartTime: formatToRFC3339(startTime),
				EndTime:   formatToRFC3339(endTime),
				ComponentsSet: map[string]struct{}{
					component: {},
				},
				AffectedComponents: []string{component},
				Alerts:             []model.LabelSet{labels},
				AlertsSet: map[string]struct{}{
					labels.String(): {},
				},
			}
			if i.consoleURL != "" {
				incident.URL = fmt.Sprintf("%s/monitoring/incidents?groupId=%s", i.consoleURL, groupId)
			}
			incident.UpdateStatus()
			incidents[groupId] = incident
		}
	}
	return incidents, nil
}

// getTokenFromCtx gets the authorization header from the
// provided context
func getTokenFromCtx(ctx context.Context) (string, error) {
	k8sToken := ctx.Value(authHeaderStr)
	k8TokenStr, ok := k8sToken.(string)
	if !ok {
		return "", fmt.Errorf("failed to convert the authorization token to string")
	}
	return k8TokenStr, nil
}

// getAlertDataForIncidents queries Prometheus for firing alerts from the last 15 days (to have
// some starting time) and then maps (the alert identifier is composed by name and namespace)
// the active alerts to the provided map of incidents. It returns slice of the incidents.
func getAlertDataForIncidents(ctx context.Context, incidents map[string]Incident, promAPI v1.API, qRange v1.Range) []Incident {
	v, _, err := promAPI.QueryRange(ctx, `ALERTS{alertstate!="pending"}`, qRange)
	if err != nil {
		slog.Error("Failed to query firing alerts", "error", err)
		return nil
	}
	alertData, ok := v.(model.Matrix)
	if !ok {
		slog.Error("Failed to convert alert data")
		return nil
	}

	var alerts []model.LabelSet
	for i := range alertData {
		sample := alertData[i]
		metric := model.LabelSet(sample.Metric)
		firstSample := sample.Values[0]
		lastSample := sample.Values[len(sample.Values)-1]
		startTime, endTime := processSampleTime(firstSample, lastSample, qRange)

		metric["start_time"] = model.LabelValue(formatToRFC3339(startTime))
		if !endTime.IsZero() {
			metric["end_time"] = model.LabelValue(formatToRFC3339(endTime))
			metric["alertstate"] = "resolved"
		} else {
			metric["alertstate"] = "firing"
		}
		alerts = append(alerts, metric)
	}

	var incidentsSlice []Incident
	for _, inc := range incidents {
		var updatedAlerts []model.LabelSet
		for _, alertInIncident := range inc.Alerts {
			subsetMatcher := common.LabelsSubsetMatcher{Labels: alertInIncident}
			for _, firingAlert := range alerts {
				match, _ := subsetMatcher.Matches(firingAlert)
				if match {
					updatedAlerts = append(updatedAlerts, cleanupLabels(firingAlert))
				}
			}
		}
		inc.Alerts = updatedAlerts
		incidentsSlice = append(incidentsSlice, inc)
	}
	return incidentsSlice
}

// cleanupLabels removes and renames some of the
// labels from the set and returns new LabelSet
func cleanupLabels(m model.LabelSet) model.LabelSet {
	updatedLS := m.Clone()
	updatedLS["status"] = updatedLS["alertstate"]
	updatedLS["name"] = updatedLS["alertname"]
	delete(updatedLS, "__name__")
	delete(updatedLS, "prometheus")
	delete(updatedLS, "alertstate")
	delete(updatedLS, "alertname")
	return updatedLS
}

// getConsoleURL tries to read consoleURL from the "cluster" consoles.config.openshift.io
// resource
func getConsoleURL() (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", err
	}
	cli, err := dynamic.NewForConfig(config)
	if err != nil {
		return "", err
	}

	unstConsole, err := cli.Resource(
		schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "consoles"}).
		Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	consoleURL, ok, err := unstructured.NestedString(unstConsole.Object, "status", "consoleURL")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("cannot find consoleURL attribute in the 'cluster' console.config.openshift.io resource")
	}

	return consoleURL, nil
}
