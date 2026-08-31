package processor

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var iarGVR = schema.GroupVersionResource{
	Group:    "monitoring.openshift.io",
	Version:  "v1alpha1",
	Resource: "incidentaggregationrules",
}

type RulesWatcher struct {
	mu       sync.RWMutex
	snapshot *ParsedRulesSnapshot
	informer cache.SharedIndexInformer
}

func NewRulesWatcher(restConfig *rest.Config) (*RulesWatcher, error) {
	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynClient, 0)
	informer := factory.ForResource(iarGVR).Informer()

	rw := &RulesWatcher{
		snapshot: DefaultSnapshot(),
		informer: informer,
	}

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ interface{}) { rw.rebuild() },
		UpdateFunc: func(_, _ interface{}) { rw.rebuild() },
		DeleteFunc: func(_ interface{}) { rw.rebuild() },
	}
	informer.AddEventHandler(handler)

	return rw, nil
}

func (rw *RulesWatcher) Start(ctx context.Context) error {
	slog.Info("Starting IAR rules watcher")
	rw.informer.Run(ctx.Done())
	return nil
}

func (rw *RulesWatcher) GetSnapshot() *ParsedRulesSnapshot {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return rw.snapshot
}

func (rw *RulesWatcher) rebuild() {
	items := rw.informer.GetStore().List()
	if len(items) == 0 {
		slog.Info("No IAR resources found, using defaults")
		rw.mu.Lock()
		rw.snapshot = DefaultSnapshot()
		rw.mu.Unlock()
		return
	}

	var rules []*ParsedRule

	for _, item := range items {
		obj, ok := item.(*unstructured.Unstructured)
		if !ok {
			slog.Warn("Unexpected object type in IAR informer")
			continue
		}

		name := obj.GetName()
		specRaw, found, err := unstructured.NestedMap(obj.Object, "spec")
		if err != nil || !found {
			slog.Warn("IAR resource missing spec", "name", name, "error", err)
			continue
		}

		specJSON, err := json.Marshal(specRaw)
		if err != nil {
			slog.Warn("Failed to marshal IAR spec", "name", name, "error", err)
			continue
		}

		var spec IncidentAggregationRuleSpec
		if err := json.Unmarshal(specJSON, &spec); err != nil {
			slog.Warn("Failed to parse IAR spec", "name", name, "error", err)
			continue
		}

		rule, err := ParseRule(name, spec)
		if err != nil {
			slog.Warn("Failed to parse IAR rule", "name", name, "error", err)
			continue
		}

		rules = append(rules, rule)
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})

	snap := &ParsedRulesSnapshot{
		Rules: rules,
	}

	rw.mu.Lock()
	rw.snapshot = snap
	rw.mu.Unlock()

	slog.Info("IAR rules updated", "count", len(rules))
}
