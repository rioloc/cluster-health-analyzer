package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var incidentRuleGVR = schema.GroupVersionResource{
	Group:    "monitoring.openshift.io",
	Version:  "v1alpha1",
	Resource: "incidentrules",
}

type RulesWatcher struct {
	mu       sync.RWMutex
	snapshot *RulesSnapshot
	informer cache.SharedIndexInformer
}

func NewRulesWatcher(config *rest.Config) (*RulesWatcher, error) {
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return newRulesWatcher(client)
}

func newRulesWatcher(client dynamic.Interface) (*RulesWatcher, error) {
	factory := dynamicinformer.NewDynamicSharedInformerFactory(client, 0)
	watcher := &RulesWatcher{informer: factory.ForResource(incidentRuleGVR).Informer()}
	_, err := watcher.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { watcher.rebuild() },
		UpdateFunc: func(_, _ interface{}) { watcher.rebuild() },
		DeleteFunc: func(_ interface{}) { watcher.rebuild() },
	})
	if err != nil {
		return nil, err
	}
	return watcher, nil
}

// Start waits for the initial list before allowing incident processing to begin.
func (rw *RulesWatcher) Start(ctx context.Context) error {
	go rw.informer.Run(ctx.Done())
	syncContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !cache.WaitForCacheSync(syncContext.Done(), rw.informer.HasSynced) {
		return fmt.Errorf("failed to synchronize IncidentRules")
	}
	rw.rebuild()
	if snapshot := rw.GetSnapshot(); snapshot == nil || len(snapshot.rules) == 0 {
		return fmt.Errorf("no valid IncidentRules loaded")
	}
	return nil
}

func (rw *RulesWatcher) GetSnapshot() *RulesSnapshot {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return rw.snapshot
}

func (rw *RulesWatcher) rebuild() {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	var rules []*parsedRule
	for _, item := range rw.informer.GetStore().List() {
		obj, ok := item.(*unstructured.Unstructured)
		if !ok {
			slog.Error("Cannot load IncidentRule: unexpected object type")
			continue
		}
		name := obj.GetName()
		raw, found, err := unstructured.NestedMap(obj.Object, "spec")
		if err != nil || !found {
			slog.Error("Cannot load invalid IncidentRule: missing spec", "name", name)
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			slog.Error("Cannot load invalid IncidentRule", "name", name, "error", err)
			continue
		}
		spec, err := decodeRuleSpec(data)
		if err != nil {
			slog.Error("Cannot load invalid IncidentRule", "name", name, "error", err)
			continue
		}
		rule, err := parseRule(name, spec)
		if err != nil {
			slog.Error("Cannot load invalid IncidentRule", "name", name, "error", err)
			continue
		}
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		return rules[i].Name < rules[j].Name
	})
	snapshot := &RulesSnapshot{rules: rules}
	// Metadata-only updates must not invalidate stateful matchers.
	if !reflect.DeepEqual(rw.snapshot, snapshot) {
		rw.snapshot = snapshot
		slog.Info("Loaded IncidentRules", "count", len(rules))
	}
}
