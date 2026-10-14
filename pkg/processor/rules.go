package processor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/prometheus/common/model"
	"go.yaml.in/yaml/v3"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

// IncidentRuleSpec configures matcher generation, precedence, and time windows.
type IncidentRuleSpec struct {
	Priority   int32          `json:"priority,omitempty" yaml:"priority,omitempty"`
	Strategies []StrategySpec `json:"strategies" yaml:"strategies"`
}

type StrategySpec struct {
	Within        string                 `json:"within" yaml:"within"`
	From          string                 `json:"from" yaml:"from"`
	ExcludeAlerts []model.LabelSet       `json:"excludeAlerts,omitempty" yaml:"excludeAlerts,omitempty"`
	Match         map[string]interface{} `json:"match" yaml:"match"`
}

const (
	windowFromEnd         = "end"
	windowFromStart       = "start"
	withinDurationPattern = `^([0-9]+(\.[0-9]+)?(h|m|s))+$`
)

var withinDurationRE = regexp.MustCompile(withinDurationPattern)

type parsedStrategy struct {
	Within    time.Duration
	Retention time.Duration
	From      string
	Priority  int32
	RuleName  string
	Order     int
	Expand    bool
	operator  string
	Keys      []model.LabelName
	Excluded  []common.LabelsSubsetMatcher
}

func (s *parsedStrategy) allows(labels model.LabelSet) bool {
	for _, excluded := range s.Excluded {
		if matched, _ := excluded.Matches(labels); matched {
			return false
		}
	}
	return true
}

type parsedRule struct {
	Name       string
	Priority   int32
	Strategies []*parsedStrategy
}

// RulesSnapshot is immutable after publication by the watcher.
type RulesSnapshot struct {
	rules []*parsedRule
}

func parseRule(name string, spec IncidentRuleSpec) (*parsedRule, error) {
	if name == "" {
		return nil, fmt.Errorf("rule name is required")
	}
	if len(spec.Strategies) == 0 {
		return nil, fmt.Errorf("at least one strategy is required")
	}

	rule := &parsedRule{Name: name, Priority: spec.Priority}
	for i, strategy := range spec.Strategies {
		if strategy.Match == nil {
			return nil, fmt.Errorf("strategy %d: match is required", i)
		}
		if len(strategy.Match) > 1 {
			return nil, fmt.Errorf("strategy %d: match operators cannot be combined", i)
		}
		if len(strategy.Within) > 32 || !withinDurationRE.MatchString(strategy.Within) {
			return nil, fmt.Errorf("strategy %d: within must be a positive duration using h, m, or s", i)
		}
		within, err := time.ParseDuration(strategy.Within)
		if err != nil || within <= 0 {
			return nil, fmt.Errorf("strategy %d: within must be a positive duration", i)
		}
		if strategy.From != windowFromEnd && strategy.From != windowFromStart {
			return nil, fmt.Errorf("strategy %d: from must be end or start", i)
		}
		parsed := &parsedStrategy{
			Within: within, Retention: max(24*time.Hour, within), From: strategy.From, Priority: spec.Priority,
			RuleName: name, Order: i, Expand: true,
		}
		for operator, value := range strategy.Match {
			parsed.operator = operator
			switch operator {
			case "exact":
				if exact, ok := value.(bool); !ok || !exact {
					return nil, fmt.Errorf("strategy %d: exact must be true", i)
				}
				parsed.Expand = false
				parsed.Retention = max(120*time.Hour, within)
			case "allOf", "anyOf":
				keys, ok := value.([]interface{})
				if !ok || len(keys) == 0 {
					return nil, fmt.Errorf("strategy %d: label keys must be a nonempty array", i)
				}
				seen := make(map[string]bool, len(keys))
				for _, value := range keys {
					key, ok := value.(string)
					if !ok || !model.UTF8Validation.IsValidLabelName(key) || seen[key] {
						return nil, fmt.Errorf("strategy %d: label keys must be valid and unique strings", i)
					}
					seen[key] = true
					parsed.Keys = append(parsed.Keys, model.LabelName(key))
				}
			default:
				return nil, fmt.Errorf("strategy %d: unsupported match operator", i)
			}
		}
		if parsed.operator == "" && len(strategy.ExcludeAlerts) > 0 {
			return nil, fmt.Errorf("strategy %d: temporal strategy cannot filter individual alerts", i)
		}
		for _, labels := range strategy.ExcludeAlerts {
			if len(labels) == 0 {
				return nil, fmt.Errorf("strategy %d: excluded alert selector cannot be empty", i)
			}
			for key := range labels {
				if !model.UTF8Validation.IsValidLabelName(string(key)) {
					return nil, fmt.Errorf("strategy %d: excluded alert selector has an invalid label key", i)
				}
			}
			parsed.Excluded = append(parsed.Excluded, common.LabelsSubsetMatcher{Labels: labels})
		}
		rule.Strategies = append(rule.Strategies, parsed)
	}
	return rule, nil
}

func decodeRuleSpec(data []byte) (IncidentRuleSpec, error) {
	var spec IncidentRuleSpec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&spec)
	return spec, err
}

// LoadRulesFile loads one explicit IncidentRule manifest for offline simulation and tests.
func LoadRulesFile(path string) (*RulesSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var resource struct {
		APIVersion string                 `yaml:"apiVersion"`
		Kind       string                 `yaml:"kind"`
		Metadata   map[string]interface{} `yaml:"metadata"`
		Spec       IncidentRuleSpec       `yaml:"spec"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&resource); err != nil {
		return nil, fmt.Errorf("invalid IncidentRule manifest: %w", err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected a single IncidentRule manifest")
	}
	if resource.APIVersion != "monitoring.openshift.io/v1alpha1" || resource.Kind != "IncidentRule" {
		return nil, fmt.Errorf("expected monitoring.openshift.io/v1alpha1 IncidentRule")
	}
	name, _ := resource.Metadata["name"].(string)
	rule, err := parseRule(name, resource.Spec)
	if err != nil {
		return nil, err
	}
	return &RulesSnapshot{rules: []*parsedRule{rule}}, nil
}

func NewGroupsCollection(rules *RulesSnapshot) (*GroupsCollection, error) {
	if rules == nil || len(rules.rules) == 0 {
		return nil, fmt.Errorf("no valid IncidentRules loaded")
	}
	return &GroupsCollection{rules: rules}, nil
}

// rootGroupMatchers attaches temporal matchers to a newly created incident.
func (rules *RulesSnapshot) rootGroupMatchers(interval Interval, rootGroupID string) []*GroupMatcher {
	var groups []*GroupMatcher
	for _, rule := range rules.rules {
		for _, strategy := range rule.Strategies {
			if strategy.operator != "" {
				continue
			}
			groups = append(groups, &GroupMatcher{
				GroupID: rootGroupID, RootGroupID: rootGroupID,
				matcherTimeline: &matcherTimeline{Start: interval.Start, Modified: interval.Start, End: interval.End},
				strategy:        strategy,
			})
		}
	}
	return groups
}

func (rules *RulesSnapshot) alertGroupMatchers(interval Interval) []*GroupMatcher {
	var groups []*GroupMatcher
	for _, rule := range rules.rules {
		// Equivalent exact strategies share observations so fallback windows stay current.
		exactTimelines := make(map[string]*matcherTimeline)
		for _, strategy := range rule.Strategies {
			if strategy.operator == "" || !strategy.allows(interval.Metric) {
				continue
			}
			var labels []model.LabelSet
			switch strategy.operator {
			case "exact":
				labels = append(labels, interval.Metric)
			case "allOf":
				labels = append(labels, getMapSubset(interval.Metric, strategy.Keys...))
			case "anyOf":
				// Keep the legacy map iteration semantics for equally ranked matchers.
				for key, value := range getMapSubset(interval.Metric, strategy.Keys...) {
					labels = append(labels, model.LabelSet{key: value})
				}
			}
			for _, selector := range labels {
				timeline := &matcherTimeline{Start: interval.Start, Modified: interval.Start, End: interval.End}
				if strategy.operator == "exact" {
					key := strategy.From + fmt.Sprint(strategy.Excluded)
					if previous := exactTimelines[key]; previous != nil {
						timeline = previous
					} else {
						exactTimelines[key] = timeline
					}
				}
				groups = append(groups, &GroupMatcher{
					matcherTimeline: timeline,
					Matchers:        []common.LabelsSubsetMatcher{{Labels: selector}},
					strategy:        strategy,
				})
			}
		}
	}
	return groups
}
