package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openshift/cluster-health-analyzer/pkg/alertmanager"
	"github.com/openshift/cluster-health-analyzer/pkg/processor"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
	"github.com/openshift/cluster-health-analyzer/pkg/test/mocks"
	"github.com/prometheus/alertmanager/api/v2/models"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestIncidentTool_IncidentsHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseTime := model.Now()

	type args struct {
		ctx      context.Context
		request  *mcp.CallToolRequest
		params   GetIncidentsParams
		pageSize int
	}

	tests := []struct {
		name           string
		promLoader     prom.Loader
		amLoader       alertmanager.Loader
		args           args
		expectedResult *mcp.CallToolResult
		expectedErr    error
	}{
		{
			name: "happy path",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)

				mocked.EXPECT().LoadVectorRange(gomock.Any(), processor.ClusterHealthComponentsMap, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"group_id":      "123",
							"src_alertname": "ClusterOperatorDown",
							"src_namespace": "openshift-monitoring",
							"src_severity":  "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"group_id":      "123",
							"src_alertname": "UpdateAvailable",
							"src_namespace": "openshift-monitoring",
							"src_severity":  "info",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-1 * time.Minute),
							},
						},
					},
				}, nil)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "ClusterOperatorDown",
							"namespace":  "openshift-monitoring",
							"severity":   "warning",
							"pod":        "bar",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-15 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "ClusterOperatorDown",
							"namespace":  "openshift-monitoring",
							"severity":   "warning",
							"pod":        "foo",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-15 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "UpdateAvailable",
							"namespace":  "openshift-monitoring",
							"severity":   "info",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-20 * time.Minute),
							},
						},
					},
				}, nil)

				mocked.EXPECT().LoadQuery(gomock.Any(), `console_url`, gomock.Any()).Return(
					[]model.LabelSet{
						{model.LabelName("url"): model.LabelValue("test.url")},
					}, nil)
				return mocked
			}(),
			amLoader: func() alertmanager.Loader {
				silencedAlerts := []models.Alert{
					{
						Labels: map[string]string{
							"alertname": "ClusterOperatorDown",
							"namespace": "openshift-monitoring",
							"severity":  "warning",
							"pod":       "foo",
						},
					},
					{
						Labels: map[string]string{
							"alertname": "UpdateAvailable",
							"namespace": "openshift-monitoring",
							"severity":  "info",
						},
					},
				}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silencedAlerts, nil)
				return mocked
			}(),
			args: args{
				ctx:     context.WithValue(t.Context(), authHeaderStr, "test"),
				request: &mcp.CallToolRequest{},
				params: GetIncidentsParams{
					TimeRange:   uint(300),
					MinSeverity: processor.Healthy.String(),
				},
			},
			expectedResult: func() *mcp.CallToolResult {
				r := Response{
					Incidents: Incidents{
						Total: 1,
						Incidents: []Incident{
							{
								GroupId:            "123",
								Severity:           "warning",
								Status:             "firing",
								StartTime:          baseTime.Add(-1 * time.Minute).Time().Format(time.RFC3339),
								AffectedComponents: []string{""},
								URL:                "test.url/monitoring/incidents?groupId=123",
								Alerts: []model.LabelSet{
									{
										"name":       "UpdateAvailable",
										"namespace":  "openshift-monitoring",
										"severity":   "info",
										"status":     "resolved",
										"silenced":   "true",
										"start_time": model.LabelValue(baseTime.Add(-20 * time.Minute).Time().Format(time.RFC3339)),
										"end_time":   model.LabelValue(baseTime.Add(-20 * time.Minute).Time().Format(time.RFC3339)),
									},
									{
										"name":       "ClusterOperatorDown",
										"namespace":  "openshift-monitoring",
										"severity":   "warning",
										"status":     "resolved",
										"silenced":   "false",
										"start_time": model.LabelValue(baseTime.Add(-15 * time.Minute).Time().Format(time.RFC3339)),
										"end_time":   model.LabelValue(baseTime.Add(-15 * time.Minute).Time().Format(time.RFC3339)),
									},
								},
							},
						},
					},
				}
				data, _ := json.Marshal(r)
				response := fmt.Sprintf(getIncidentsResponseTemplate, string(data))
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: response},
					},
				}
			}(),
		},
		{
			name: "happy path - warning min_severity",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)

				mocked.EXPECT().LoadVectorRange(gomock.Any(), processor.ClusterHealthComponentsMap, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"group_id":      "123",
							"src_alertname": "ClusterOperatorDown",
							"src_namespace": "openshift-monitoring",
							"src_severity":  "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"group_id":      "123",
							"src_alertname": "UpdateAvailable",
							"src_namespace": "openshift-monitoring",
							"src_severity":  "info",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"group_id":      "456",
							"src_alertname": "UpdateAvailable",
							"src_namespace": "openshift-monitoring",
							"src_severity":  "info",
						},
						Samples: []model.SamplePair{
							{
								Value:     0,
								Timestamp: baseTime.Add(-1 * time.Minute),
							},
						},
					},
				}, nil)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "ClusterOperatorDown",
							"namespace":  "openshift-monitoring",
							"severity":   "warning",
							"pod":        "bar",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-15 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "ClusterOperatorDown",
							"namespace":  "openshift-monitoring",
							"severity":   "warning",
							"pod":        "foo",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-15 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "UpdateAvailable",
							"namespace":  "openshift-monitoring",
							"severity":   "info",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: baseTime.Add(-20 * time.Minute),
							},
						},
					},
				}, nil)

				mocked.EXPECT().LoadQuery(gomock.Any(), `console_url`, gomock.Any()).Return(
					[]model.LabelSet{
						{model.LabelName("url"): model.LabelValue("test.url")},
					}, nil)
				return mocked
			}(),
			amLoader: func() alertmanager.Loader {
				silencedAlerts := []models.Alert{
					{
						Labels: map[string]string{
							"alertname": "ClusterOperatorDown",
							"namespace": "openshift-monitoring",
							"severity":  "warning",
							"pod":       "foo",
						},
					},
					{
						Labels: map[string]string{
							"alertname": "UpdateAvailable",
							"namespace": "openshift-monitoring",
							"severity":  "info",
						},
					},
				}
				mocked := mocks.NewMockAlertManagerLoader(ctrl)
				mocked.EXPECT().SilencedAlerts().Return(silencedAlerts, nil)
				return mocked
			}(),
			args: args{
				ctx:      context.WithValue(t.Context(), authHeaderStr, "test"),
				pageSize: DefaultGetIncidentsLimit,
				request:  &mcp.CallToolRequest{},
				params: GetIncidentsParams{
					TimeRange:   uint(300),
					MinSeverity: processor.Warning.String(),
				},
			},
			expectedResult: func() *mcp.CallToolResult {
				r := Response{
					Incidents: Incidents{
						Total: 1,
						Incidents: []Incident{
							{
								GroupId:            "123",
								Severity:           "warning",
								Status:             "firing",
								StartTime:          baseTime.Add(-1 * time.Minute).Time().Format(time.RFC3339),
								AffectedComponents: []string{""},
								URL:                "test.url/monitoring/incidents?groupId=123",
								Alerts: []model.LabelSet{
									{
										"name":       "UpdateAvailable",
										"namespace":  "openshift-monitoring",
										"severity":   "info",
										"status":     "resolved",
										"silenced":   "true",
										"start_time": model.LabelValue(baseTime.Add(-20 * time.Minute).Time().Format(time.RFC3339)),
										"end_time":   model.LabelValue(baseTime.Add(-20 * time.Minute).Time().Format(time.RFC3339)),
									},
									{
										"name":       "ClusterOperatorDown",
										"namespace":  "openshift-monitoring",
										"severity":   "warning",
										"status":     "resolved",
										"silenced":   "false",
										"start_time": model.LabelValue(baseTime.Add(-15 * time.Minute).Time().Format(time.RFC3339)),
										"end_time":   model.LabelValue(baseTime.Add(-15 * time.Minute).Time().Format(time.RFC3339)),
									},
								},
							},
						},
					},
				}
				data, _ := json.Marshal(r)
				response := fmt.Sprintf(getIncidentsResponseTemplate, string(data))
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: response},
					},
				}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := IncidentTool{
				Tool: defaultMcpGetIncidentsTool,
				cfg: incidentToolCfg{
					incidentsPageSize: 2,
				},
				getPrometheusLoaderFn: func(url, _ string) (prom.Loader, error) {
					return tt.promLoader, nil
				},
				getAlertManagerLoaderFn: func(url, token string) (alertmanager.Loader, error) {
					return tt.amLoader, nil
				},
			}
			got, _, err := tool.IncidentsHandler(tt.args.ctx, tt.args.request, tt.args.params)

			assert.Equal(t, tt.expectedResult, got)
			assert.Equal(t, tt.expectedErr, err)
		})
	}

}

func TestTransformPromValueToIncident(t *testing.T) {
	tests := []struct {
		name              string
		testInput         prom.RangeVector
		expectedIncidents map[string]Incident
	}{
		{
			name: "Two alerts with same group_id are one incident",
			testInput: prom.RangeVector{
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
			},
			expectedIncidents: map[string]Incident{
				"1": {
					GroupId:            "1",
					Severity:           processor.Healthy.String(),
					Status:             "firing",
					StartTime:          time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"console", "monitoring"},
					ComponentsSet:      map[string]struct{}{"monitoring": {}, "console": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "openshift-monitoring", "severity": "warning"},
						{"alertname": "Alert2", "namespace": "openshift-console", "severity": "warning"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert2\", namespace=\"openshift-console\", severity=\"warning\"}":    {},
						"{alertname=\"Alert1\", namespace=\"openshift-monitoring\", severity=\"warning\"}": {},
					},
				},
			},
		},
		{
			name: "Two alerts with same group_id and same component are one incident",
			testInput: prom.RangeVector{
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
			},
			expectedIncidents: map[string]Incident{
				"1": {
					GroupId:            "1",
					Severity:           processor.Warning.String(),
					Status:             "firing",
					StartTime:          time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"monitoring"},
					ComponentsSet:      map[string]struct{}{"monitoring": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "openshift-monitoring", "severity": "warning"},
						{"alertname": "Alert2", "namespace": "openshift-monitoring", "severity": "warning"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert1\", namespace=\"openshift-monitoring\", severity=\"warning\"}": {},
						"{alertname=\"Alert2\", namespace=\"openshift-monitoring\", severity=\"warning\"}": {},
					},
				},
			},
		},
		{
			name: "Two different incidents and alert with severity=None is ignored",
			testInput: prom.RangeVector{
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert2",
						"group_id":      "1",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Samples: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-25 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert3",
						"group_id":      "2",
						"src_severity":  "none",
						"component":     "none",
					},
					Samples: []model.SamplePair{
						{
							Value:     0,
							Timestamp: model.Now().Add(-1 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert1",
						"group_id":      "1",
						"src_severity":  "critical",
						"component":     "monitoring",
						"src_namespace": "openshift-monitoring",
					},
					Samples: []model.SamplePair{
						{
							Value:     2,
							Timestamp: model.Now().Add(-25 * time.Minute),
						},
						{
							Value:     2,
							Timestamp: model.Now().Add(-11 * time.Minute),
						},
					},
				},
				{
					Metric: model.LabelSet{
						"src_alertname": "Alert4",
						"group_id":      "2",
						"src_severity":  "warning",
						"component":     "console",
						"src_namespace": "openshift-console",
					},
					Samples: []model.SamplePair{
						{
							Value:     1,
							Timestamp: model.Now().Add(-15 * time.Minute),
						},
					},
				},
			},
			expectedIncidents: map[string]Incident{
				"1": {
					GroupId:            "1",
					Severity:           "critical",
					Status:             "resolved",
					StartTime:          time.Now().Add(-25 * time.Minute).Format(time.RFC3339),
					EndTime:            time.Now().Add(-11 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"console", "monitoring"},
					ComponentsSet:      map[string]struct{}{"monitoring": {}, "console": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert2", "namespace": "openshift-console", "severity": "warning"},
						{"alertname": "Alert1", "namespace": "openshift-monitoring", "severity": "critical"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert2\", namespace=\"openshift-console\", severity=\"warning\"}":     {},
						"{alertname=\"Alert1\", namespace=\"openshift-monitoring\", severity=\"critical\"}": {},
					},
				},
				"2": {
					GroupId:            "2",
					Severity:           "warning",
					Status:             "resolved",
					StartTime:          time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
					EndTime:            time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
					AffectedComponents: []string{"console"},
					ComponentsSet:      map[string]struct{}{"console": {}},
					Alerts: []model.LabelSet{
						{"alertname": "Alert4", "namespace": "openshift-console", "severity": "warning"},
					},
					AlertsSet: map[string]struct{}{
						"{alertname=\"Alert4\", namespace=\"openshift-console\", severity=\"warning\"}": {},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTool := IncidentTool{}
			incidents, err := testTool.transformPromValueToIncident(tt.testInput, v1.Range{
				Start: time.Now().Add(-30 * time.Minute),
				End:   time.Now(),
				Step:  300 * time.Second,
			}, nil)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedIncidents, incidents)
		})
	}
}

func TestGetAlertDataForIncidents(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name              string
		promLoader        prom.Loader
		incidentsMap      map[string]Incident
		silencedAlerts    []models.Alert
		expectedIncidents []Incident
	}{
		{
			name: "Same alerts in different namespace are matched correctly",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-25 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"alertstate": "firing",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-24 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			silencedAlerts: []models.Alert{
				{
					Labels: map[string]string{
						"alertname": "Alert1",
						"namespace": "foo",
					},
				},
			},
			incidentsMap: map[string]Incident{
				"1": {
					GroupId: "1",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo"},
						{"alertname": "Alert1", "namespace": "bar"},
					},
				},
			},
			expectedIncidents: []Incident{
				{
					GroupId: "1",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"status":     "firing",
							"silenced":   "true",
							"start_time": model.LabelValue(model.Now().Add(-25 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"status":     "firing",
							"silenced":   "false",
							"start_time": model.LabelValue(model.Now().Add(-24 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
		{
			name: "Same alert in more incidents",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"alertstate": "resolved",
						},
						Samples: []model.SamplePair{
							{
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"alertstate": "resolved",
						},
						Samples: []model.SamplePair{
							{
								Timestamp: model.Now().Add(-19 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert2",
							"namespace":  "bar",
							"alertstate": "resolved",
						},
						Samples: []model.SamplePair{
							{
								Timestamp: model.Now().Add(-19 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			incidentsMap: map[string]Incident{
				"1": {
					GroupId: "1",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo"},
						{"alertname": "Alert1", "namespace": "bar"},
					},
				},
				"2": {
					GroupId: "2",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo"},
						{"alertname": "Alert2", "namespace": "bar"},
					},
				},
			},
			silencedAlerts: []models.Alert{
				{
					Labels: map[string]string{
						"alertname": "Alert1",
						"namespace": "foo",
					},
				},
			},
			expectedIncidents: []Incident{
				{
					GroupId: "1",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"status":     "resolved",
							"silenced":   "true",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"status":     "resolved",
							"silenced":   "false",
							"start_time": model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
				{
					GroupId: "2",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"status":     "resolved",
							"silenced":   "true",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert2",
							"namespace":  "bar",
							"status":     "resolved",
							"silenced":   "false",
							"start_time": model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
							"end_time":   model.LabelValue(model.Now().Add(-19 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
		{
			name: "Alerts are correctly marked as silenced",
			// three alerts with the same name
			// A. Alert1, namespace=foo, pod=red
			// B. Alert1, namespace=foo, pod=blue (same alertname and namespace with A. but differend pod name)
			// C. Alert1, namespace=bar, pod=red (same alertname and pod name with A. but different namespace)
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"pod":        "red",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"pod":        "blue",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"pod":        "red",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"pod":        "green",
							"alertstate": "firing",
							"severity":   "warning",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			incidentsMap: map[string]Incident{
				"1": {
					GroupId: "1",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo", "severity": "warning"},
						{"alertname": "Alert1", "namespace": "bar", "severity": "warning"},
					},
				},
			},
			silencedAlerts: []models.Alert{
				{
					Labels: map[string]string{
						"alertname": "Alert1",
						"namespace": "foo",
						"severity":  "warning",
						"pod":       "red",
					},
				},
				{
					Labels: map[string]string{
						"alertname": "Alert1",
						"namespace": "bar",
						"severity":  "warning",
						"pod":       "red",
					},
				},
				{
					Labels: map[string]string{
						"alertname": "Alert1",
						"namespace": "bar",
						"severity":  "warning",
						"pod":       "green",
					},
				},
			},
			expectedIncidents: []Incident{
				{
					GroupId: "1",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"status":     "firing",
							"silenced":   "false",
							"severity":   "warning",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"status":     "firing",
							"silenced":   "true",
							"severity":   "warning",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
		{
			name: "Alerts are correctly matched in multicluster environment",
			promLoader: func() prom.Loader {
				mocked := mocks.NewMockPrometheusLoader(ctrl)
				mocked.EXPECT().LoadVectorRange(gomock.Any(), `ALERTS{alertstate!="pending"}`, gomock.Any(), gomock.Any(), gomock.Any()).Return(prom.RangeVector{
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"pod":        "red",
							"alertstate": "firing",
							"severity":   "warning",
							"clusterID":  "1111",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "foo",
							"pod":        "red",
							"alertstate": "firing",
							"severity":   "warning",
							"clusterID":  "2222",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"pod":        "blue",
							"alertstate": "firing",
							"severity":   "critical",
							"clusterID":  "1111",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
					{
						Metric: model.LabelSet{
							"alertname":  "Alert1",
							"namespace":  "bar",
							"pod":        "blue",
							"alertstate": "firing",
							"severity":   "critical",
							"clusterID":  "2222",
						},
						Samples: []model.SamplePair{
							{
								Value:     1,
								Timestamp: model.Now().Add(-20 * time.Minute),
							},
							{
								Value:     1,
								Timestamp: model.Now().Add(-1 * time.Minute),
							},
						},
					},
				}, nil)
				return mocked
			}(),
			incidentsMap: map[string]Incident{
				"1": {
					GroupId:   "1",
					ClusterID: "1111",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo", "severity": "warning"},
						{"alertname": "Alert1", "namespace": "bar", "severity": "critical"},
					},
				},
				"2": {
					GroupId:   "2",
					ClusterID: "2222",
					Alerts: []model.LabelSet{
						{"alertname": "Alert1", "namespace": "foo", "severity": "warning"},
						{"alertname": "Alert1", "namespace": "bar", "severity": "critical"},
					},
				},
			},
			silencedAlerts: []models.Alert{},
			expectedIncidents: []Incident{
				{
					GroupId:   "1",
					ClusterID: "1111",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"status":     "firing",
							"silenced":   "false",
							"severity":   "warning",
							"cluster_id": "1111",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"status":     "firing",
							"silenced":   "false",
							"severity":   "critical",
							"cluster_id": "1111",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
				{
					GroupId:   "2",
					ClusterID: "2222",
					Alerts: []model.LabelSet{
						{
							"name":       "Alert1",
							"namespace":  "foo",
							"status":     "firing",
							"silenced":   "false",
							"severity":   "warning",
							"cluster_id": "2222",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
						{
							"name":       "Alert1",
							"namespace":  "bar",
							"status":     "firing",
							"silenced":   "false",
							"severity":   "critical",
							"cluster_id": "2222",
							"start_time": model.LabelValue(model.Now().Add(-20 * time.Minute).Time().Format(time.RFC3339)),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			incidents := getAlertDataForIncidents(ctx, tt.incidentsMap, tt.silencedAlerts, tt.promLoader, v1.Range{
				Start: time.Now().Add(-30 * time.Minute),
				End:   time.Now(),
				Step:  300 * time.Second,
			})

			// Sort the actual and expected alerts slices before comparing to avoid test flakyness
			for i := range incidents {
				sortAlerts(incidents[i].Alerts)
			}

			for i := range tt.expectedIncidents {
				sortAlerts(tt.expectedIncidents[i].Alerts)
			}

			assert.ElementsMatch(t, tt.expectedIncidents, incidents)
		})
	}
}

func TestGetConsoleURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	tests := []struct {
		name           string
		promLoader     prom.Loader
		expectedResult map[string]string
		expectedErr    error
	}{
		{
			name: "console url not found in metrics",
			promLoader: func() prom.Loader {
				mockPromLoader := mocks.NewMockPrometheusLoader(ctrl)
				mockPromLoader.EXPECT().LoadQuery(t.Context(), "console_url", gomock.Any()).Return(
					[]model.LabelSet{}, nil)
				return mockPromLoader
			}(),
			expectedErr:    fmt.Errorf("console_url not found"),
			expectedResult: nil,
		},
		{
			name: "console url metric has clusterID label",
			promLoader: func() prom.Loader {
				mockPromLoader := mocks.NewMockPrometheusLoader(ctrl)
				mockPromLoader.EXPECT().LoadQuery(t.Context(), "console_url", gomock.Any()).Return(
					[]model.LabelSet{
						{
							model.LabelName("url"):        model.LabelValue("test-a.url"),
							model.LabelName(clusterIDStr): model.LabelValue("A"),
						},
						{
							model.LabelName("url"):        model.LabelValue("test-b.url"),
							model.LabelName(clusterIDStr): model.LabelValue("B"),
						},
					}, nil)
				return mockPromLoader
			}(),
			expectedResult: map[string]string{
				"A": "test-a.url",
				"B": "test-b.url",
			},
			expectedErr: nil,
		},
		{
			name: "console url metric has no clusterID label",
			promLoader: func() prom.Loader {
				mockPromLoader := mocks.NewMockPrometheusLoader(ctrl)
				mockPromLoader.EXPECT().LoadQuery(t.Context(), "console_url", gomock.Any()).Return(
					[]model.LabelSet{
						{
							model.LabelName("url"): model.LabelValue("test.url"),
						},
					}, nil)
				return mockPromLoader
			}(),
			expectedResult: map[string]string{
				defaultStr: "test.url",
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := getConsoleURL(t.Context(), tt.promLoader)
			assert.Equal(t, tt.expectedErr, err)
			assert.Equal(t, tt.expectedResult, r)
		})
	}
}

func Test_getIncidentsPageResult(t *testing.T) {
	startTime := time.Now()
	endTime := startTime.Add(5 * time.Minute)

	incidents := []Incident{
		{
			GroupId:   "uuid-1",
			StartTime: formatToRFC3339(startTime),
			EndTime:   formatToRFC3339(startTime.Add(1 * time.Minute)),
		},
		{
			GroupId:   "uuid-2",
			StartTime: formatToRFC3339(startTime.Add(1 * time.Minute)),
			EndTime:   formatToRFC3339(startTime.Add(2 * time.Minute)),
		},
		// simulate 3 and 4 at the same start time
		{
			GroupId:   "uuid-3",
			StartTime: formatToRFC3339(startTime.Add(2 * time.Minute)),
			EndTime:   formatToRFC3339(startTime.Add(3 * time.Minute)),
		},
		{
			GroupId:   "uuid-4",
			StartTime: formatToRFC3339(startTime.Add(2 * time.Minute)),
			EndTime:   formatToRFC3339(startTime.Add(4 * time.Minute)),
		},
		{
			GroupId:   "uuid-5",
			StartTime: formatToRFC3339(startTime.Add(3 * time.Minute)),
			EndTime:   formatToRFC3339(startTime.Add(5 * time.Minute)),
		},
	}

	type args struct {
		queryTimeRange v1.Range
		pageSize       int
		requestCursor  *PaginationCursor
		incidents      []Incident
	}
	tests := []struct {
		name         string
		args         args
		wantResponse mcp.CallToolResult
		wantErr      error
	}{
		{
			name: "first request, no request cursor, first page",
			args: args{
				queryTimeRange: v1.Range{
					Start: startTime,
					End:   endTime,
					Step:  300,
				},
				pageSize: 2,
				incidents: func(origin []Incident) []Incident {
					copied := make([]Incident, len(origin))
					copy(copied, origin)
					return copied
				}(incidents),
			},
			wantResponse: func() mcp.CallToolResult {
				reqCursor := PaginationCursor{
					TimeStart: startTime.Unix(),
					TimeLast:  startTime.Add(2 * time.Minute).Unix(), // inc 4 startTime
					GroupLast: "uuid-4",
				}
				nextCursor, _ := reqCursor.Encode()
				r := Response{
					NextCursor: nextCursor,
					Incidents: Incidents{
						Total:     2,
						Incidents: []Incident{incidents[4], incidents[3]},
					},
				}
				data, _ := json.Marshal(r)
				response := fmt.Sprintf(getIncidentsResponseTemplate, string(data))
				return mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: response,
						},
					},
				}
			}(),
		},
		{
			name: "second request w request cursor, second page",
			args: args{
				queryTimeRange: v1.Range{
					Start: startTime,
					End:   startTime.Add(2 * time.Minute), // inc 4 startTime
					Step:  300,
				},
				pageSize: 2,
				requestCursor: &PaginationCursor{
					TimeStart: startTime.Unix(),
					TimeLast:  startTime.Add(2 * time.Minute).Unix(), // inc 4 startTime
					GroupLast: "uuid-4",
				},
				incidents: func(origin []Incident) []Incident {
					copied := make([]Incident, len(origin))
					// simulate 3 and 4 at the same start time
					copy(copied, origin)
					return copied[0:4]
				}(incidents),
			},
			wantResponse: func() mcp.CallToolResult {
				reqCursor := PaginationCursor{
					TimeStart: startTime.Unix(),
					TimeLast:  startTime.Add(1 * time.Minute).Unix(), // inc 2 startTime
					GroupLast: "uuid-2",
				}
				nextCursor, _ := reqCursor.Encode()
				r := Response{
					NextCursor: nextCursor,
					Incidents: Incidents{
						Total:     2,
						Incidents: []Incident{incidents[2], incidents[1]},
					},
				}
				data, _ := json.Marshal(r)
				response := fmt.Sprintf(getIncidentsResponseTemplate, string(data))
				return mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: response,
						},
					},
				}
			}(),
		},
		{
			name: "third request w request cursor, third page",
			args: args{
				queryTimeRange: v1.Range{
					Start: startTime,
					End:   endTime,
					Step:  300,
				},
				pageSize: 2,
				requestCursor: &PaginationCursor{
					TimeStart: startTime.Unix(),
					TimeLast:  startTime.Add(2 * time.Minute).Unix(),
					GroupLast: "uuid-4",
				},
				incidents: func(origin []Incident) []Incident {
					copied := make([]Incident, len(origin))
					copy(copied, origin)
					return []Incident{copied[0]}
				}(incidents),
			},
			wantResponse: func() mcp.CallToolResult {
				r := Response{
					Incidents: Incidents{
						Total:     1,
						Incidents: []Incident{incidents[0]},
					},
				}
				data, _ := json.Marshal(r)
				response := fmt.Sprintf(getIncidentsResponseTemplate, string(data))
				return mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: response,
						},
					},
				}
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getIncidentsPageResult(tt.args.requestCursor, 2, tt.args.queryTimeRange, tt.args.incidents)
			assert.Equal(t, tt.wantResponse, *got)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

func sortAlerts(alerts []model.LabelSet) {
	sort.Slice(alerts, func(i, j int) bool {
		a := alerts[i]
		b := alerts[j]

		// First, sort by 'name'
		if a["name"] != b["name"] {
			return a["name"] < b["name"]
		}

		// Then, sort by 'namespace' if names are the same
		if a["namespace"] != b["namespace"] {
			return a["namespace"] < b["namespace"]
		}

		// Finally, sort by 'pod' or another unique label to guarantee stability
		return a["pod"] < b["pod"]
	})
}
