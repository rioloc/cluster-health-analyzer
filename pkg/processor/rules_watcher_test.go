package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func testRuleObject(t *testing.T, name, specJSON string) *unstructured.Unstructured {
	t.Helper()
	var spec map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(specJSON), &spec))
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "monitoring.openshift.io/v1alpha1",
		"kind":       "IncidentRule",
		"metadata":   map[string]interface{}{"name": name},
		"spec":       spec,
	}}
}

func testRulesWatcher(t *testing.T, objects ...runtime.Object) (*RulesWatcher, *fake.FakeDynamicClient) {
	t.Helper()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		incidentRuleGVR: "IncidentRuleList",
	}, objects...)
	watcher, err := newRulesWatcher(client)
	require.NoError(t, err)
	return watcher, client
}

func TestRulesWatcherStartRequiresValidRules(t *testing.T) {
	for _, name := range []string{"missing", "invalid", "valid"} {
		t.Run(name, func(t *testing.T) {
			var objects []runtime.Object
			if name == "invalid" {
				objects = append(objects, testRuleObject(t, "invalid", `{"strategies":[{"match":{"exact":true,"allOf":["node"]},"within":"24h","from":"end"}]}`))
			}
			if name == "valid" {
				objects = append(objects, testRuleObject(t, "custom", `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`))
			}
			watcher, _ := testRulesWatcher(t, objects...)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := watcher.Start(ctx)
			if name == "valid" {
				require.NoError(t, err)
				require.Len(t, watcher.GetSnapshot().rules, 1)
			} else {
				require.ErrorContains(t, err, "no valid IncidentRules")
			}
		})
	}
}

func TestRulesWatcherRebuildInvalidatesChangedDefinitions(t *testing.T) {
	watcher, _ := testRulesWatcher(t)
	store := watcher.informer.GetStore()
	obj := testRuleObject(t, "custom", `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	require.NoError(t, store.Add(obj))
	watcher.rebuild()
	original := watcher.GetSnapshot()
	require.Len(t, original.rules, 1)

	metadataOnly := obj.DeepCopy()
	metadataOnly.SetAnnotations(map[string]string{"description": "updated"})
	require.NoError(t, store.Update(metadataOnly))
	watcher.rebuild()
	require.Same(t, original, watcher.GetSnapshot())

	changed := testRuleObject(t, "custom", `{"strategies":[{"match":{"allOf":["namespace"]},"within":"24h","from":"start"}]}`)
	require.NoError(t, store.Update(changed))
	watcher.rebuild()
	require.NotSame(t, original, watcher.GetSnapshot())
	require.Equal(t, "namespace", string(watcher.GetSnapshot().rules[0].Strategies[0].Keys[0]))
	// Previously published snapshots remain immutable while a processor uses them.
	require.Equal(t, "node", string(original.rules[0].Strategies[0].Keys[0]))

	var log bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	defer slog.SetDefault(previousLogger)
	invalid := testRuleObject(t, "custom", `{"strategies":[{"match":{"anyOf":[]},"within":"24h","from":"start"}]}`)
	require.NoError(t, store.Update(invalid))
	watcher.rebuild()
	require.Empty(t, watcher.GetSnapshot().rules)
	require.Contains(t, log.String(), "Cannot load invalid IncidentRule")
	require.Contains(t, log.String(), "name=custom")
}

func TestRulesWatcherObservesUpdatesAndDeletion(t *testing.T) {
	obj := testRuleObject(t, "custom", `{"strategies":[{"match":{"allOf":["node"]},"within":"24h","from":"start"}]}`)
	watcher, client := testRulesWatcher(t, obj)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, watcher.Start(ctx))
	original := watcher.GetSnapshot()
	changed := testRuleObject(t, "custom", `{"strategies":[{"match":{"anyOf":["node"]},"within":"24h","from":"start"}]}`)
	_, err := client.Resource(incidentRuleGVR).Update(ctx, changed, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return watcher.GetSnapshot() != original && watcher.GetSnapshot().rules[0].Strategies[0].operator == "anyOf"
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, client.Resource(incidentRuleGVR).Delete(ctx, "custom", metav1.DeleteOptions{}))
	require.Eventually(t, func() bool { return len(watcher.GetSnapshot().rules) == 0 }, time.Second, 10*time.Millisecond)
}

func TestRulesWatcherUpdatesWindowAndPriority(t *testing.T) {
	watcher, _ := testRulesWatcher(t)
	store := watcher.informer.GetStore()
	original := testRuleObject(t, "z-rule", `{"strategies":[{"match":{"allOf":["node"]},"within":"1h","from":"start"}]}`)
	require.NoError(t, store.Add(original))
	require.NoError(t, store.Add(testRuleObject(t, "a-rule", `{"priority":10,"strategies":[{"match":{},"within":"15m","from":"start"}]}`)))
	watcher.rebuild()
	snapshot := watcher.GetSnapshot()
	require.Equal(t, "z-rule", snapshot.rules[0].Name)
	changed := testRuleObject(t, "z-rule", `{"priority":20,"strategies":[{"match":{"allOf":["node"]},"within":"48h","from":"end"}]}`)
	require.NoError(t, store.Update(changed))
	watcher.rebuild()
	updated := watcher.GetSnapshot()
	require.NotSame(t, snapshot, updated)
	require.Equal(t, "a-rule", updated.rules[0].Name)
	strategy := updated.rules[1].Strategies[0]
	require.Equal(t, 48*time.Hour, strategy.Within)
	require.Equal(t, "end", strategy.From)
	require.EqualValues(t, 20, strategy.Priority)
	require.Equal(t, time.Hour, snapshot.rules[0].Strategies[0].Within)
	require.Equal(t, "start", snapshot.rules[0].Strategies[0].From)
}
