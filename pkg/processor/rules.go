package processor

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/prometheus/common/model"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
)

// IncidentAggregationRuleSpec is the spec portion of the IAR CRD.
type IncidentAggregationRuleSpec struct {
	Priority int                    `json:"priority"`
	Within   string                 `json:"within"`
	Scope    map[string]ScopeFilter `json:"scope,omitempty"`
	Match    MatchCriteria          `json:"match"`
}

type ScopeFilter struct {
	Equals string   `json:"equals,omitempty"`
	In     []string `json:"in,omitempty"`
	NotIn  []string `json:"notIn,omitempty"`
	Regex  string   `json:"regex,omitempty"`
	Exists *bool    `json:"exists,omitempty"`
}

type MatchCriteria struct {
	Exact bool            `json:"exact,omitempty"`
	AllOf []string        `json:"allOf,omitempty"`
	AnyOf []string        `json:"anyOf,omitempty"`
	NOf   *NOfCriteria    `json:"nOf,omitempty"`
	And   []MatchCriteria `json:"and,omitempty"`
}

func (m MatchCriteria) isEmpty() bool {
	return !m.Exact && len(m.AllOf) == 0 && len(m.AnyOf) == 0 && m.NOf == nil && len(m.And) == 0
}

type NOfCriteria struct {
	Keys []string `json:"keys"`
	Min  int      `json:"min"`
}

// ParsedRule is the internal representation after parsing a CRD resource.
type ParsedRule struct {
	Name     string
	Priority int
	Within   time.Duration
	Scope    []parsedScopeFilter
	Match    MatchCriteria
}

type parsedScopeFilter struct {
	Key    string
	Equals string
	In     map[string]struct{}
	NotIn  map[string]struct{}
	Regex  *regexp.Regexp
	Exists *bool
}

// ParsedRulesSnapshot is an immutable snapshot of all parsed rules.
type ParsedRulesSnapshot struct {
	Rules []*ParsedRule
}

func DefaultRules() []*ParsedRule {
	return []*ParsedRule{
		{
			Name:     "exact-match",
			Priority: 100,
			Within:   120 * time.Hour,
			Match:    MatchCriteria{Exact: true},
		},
		{
			Name:     "subset-match",
			Priority: 100,
			Within:   24 * time.Hour,
			Match:    MatchCriteria{AllOf: []string{"namespace", "alertname", "service", "job", "container"}},
		},
		{
			Name:     "fuzzy-match",
			Priority: 100,
			Within:   24 * time.Hour,
			Scope: []parsedScopeFilter{
				{
					Key:   "alertname",
					NotIn: map[string]struct{}{"Watchdog": {}, "AlertmanagerReceiversNotConfigured": {}},
				},
			},
			Match: MatchCriteria{AnyOf: []string{"alertname", "namespace"}},
		},
		{
			Name:     "time-proximity",
			Priority: 100,
			Within:   15 * time.Minute,
			Match:    MatchCriteria{},
		},
	}
}

func DefaultSnapshot() *ParsedRulesSnapshot {
	return &ParsedRulesSnapshot{
		Rules: DefaultRules(),
	}
}

func ParseRule(name string, spec IncidentAggregationRuleSpec) (*ParsedRule, error) {
	within, err := time.ParseDuration(spec.Within)
	if err != nil {
		return nil, fmt.Errorf("invalid within %q: %w", spec.Within, err)
	}

	scope, err := parseScopeFilters(spec.Scope)
	if err != nil {
		return nil, fmt.Errorf("invalid scope: %w", err)
	}

	return &ParsedRule{
		Name:     name,
		Priority: spec.Priority,
		Within:   within,
		Scope:    scope,
		Match:    spec.Match,
	}, nil
}

func parseScopeFilters(scope map[string]ScopeFilter) ([]parsedScopeFilter, error) {
	if len(scope) == 0 {
		return nil, nil
	}

	filters := make([]parsedScopeFilter, 0, len(scope))
	for key, sf := range scope {
		f := parsedScopeFilter{
			Key:    key,
			Equals: sf.Equals,
			Exists: sf.Exists,
		}
		if len(sf.In) > 0 {
			f.In = make(map[string]struct{}, len(sf.In))
			for _, v := range sf.In {
				f.In[v] = struct{}{}
			}
		}
		if len(sf.NotIn) > 0 {
			f.NotIn = make(map[string]struct{}, len(sf.NotIn))
			for _, v := range sf.NotIn {
				f.NotIn[v] = struct{}{}
			}
		}
		if sf.Regex != "" {
			re, err := regexp.Compile(sf.Regex)
			if err != nil {
				return nil, fmt.Errorf("invalid regex for key %q: %w", key, err)
			}
			f.Regex = re
		}
		filters = append(filters, f)
	}
	return filters, nil
}

func matchesScope(labels model.LabelSet, scope []parsedScopeFilter) bool {
	for _, f := range scope {
		val, exists := labels[model.LabelName(f.Key)]

		if f.Exists != nil {
			if *f.Exists != exists {
				return false
			}
			if !*f.Exists {
				continue
			}
		}

		v := string(val)

		if f.Equals != "" && v != f.Equals {
			return false
		}
		if f.In != nil {
			if _, ok := f.In[v]; !ok {
				return false
			}
		}
		if f.NotIn != nil {
			if _, ok := f.NotIn[v]; ok {
				return false
			}
		}
		if f.Regex != nil && !f.Regex.MatchString(v) {
			return false
		}
	}
	return true
}

// generateMatchers creates LabelsMatcher instances from the match criteria and the alert's labels.
func generateMatchers(labels model.LabelSet, match MatchCriteria) []common.LabelsMatcher {
	if match.Exact {
		return []common.LabelsMatcher{common.LabelsSubsetMatcher{Labels: labels}}
	}

	if len(match.AllOf) > 0 {
		subset := getMapSubset(labels, toLabelNames(match.AllOf)...)
		return []common.LabelsMatcher{common.LabelsSubsetMatcher{Labels: subset}}
	}

	if len(match.AnyOf) > 0 {
		var matchers []common.LabelsMatcher
		for _, key := range match.AnyOf {
			k := model.LabelName(key)
			if v, ok := labels[k]; ok {
				matchers = append(matchers, common.LabelsSubsetMatcher{
					Labels: model.LabelSet{k: v},
				})
			}
		}
		return matchers
	}

	if match.NOf != nil {
		keys := toLabelNames(match.NOf.Keys)
		subset := getMapSubset(labels, keys...)
		return []common.LabelsMatcher{common.LabelsNOfMatcher{
			Labels: subset,
			Keys:   keys,
			Min:    match.NOf.Min,
		}}
	}

	if len(match.And) > 0 {
		return generateAndMatchers(labels, match.And)
	}

	return nil
}

func generateAndMatchers(labels model.LabelSet, criteria []MatchCriteria) []common.LabelsMatcher {
	if len(criteria) == 0 {
		return nil
	}

	matcherSets := make([][]common.LabelsMatcher, len(criteria))
	for i, c := range criteria {
		matcherSets[i] = generateMatchers(labels, c)
		if len(matcherSets[i]) == 0 {
			return nil
		}
	}

	return crossProduct(matcherSets)
}

func crossProduct(sets [][]common.LabelsMatcher) []common.LabelsMatcher {
	if len(sets) == 0 {
		return nil
	}
	if len(sets) == 1 {
		return sets[0]
	}

	result := sets[0]
	for i := 1; i < len(sets); i++ {
		var combined []common.LabelsMatcher
		for _, a := range result {
			for _, b := range sets[i] {
				merged := mergeSubsetMatchers(a, b)
				if merged != nil {
					combined = append(combined, merged)
				}
			}
		}
		result = combined
	}
	return result
}

func mergeSubsetMatchers(a, b common.LabelsMatcher) common.LabelsMatcher {
	as, aOk := a.(common.LabelsSubsetMatcher)
	bs, bOk := b.(common.LabelsSubsetMatcher)
	if !aOk || !bOk {
		return nil
	}

	merged := make(model.LabelSet, len(as.Labels)+len(bs.Labels))
	for k, v := range as.Labels {
		merged[k] = v
	}
	for k, v := range bs.Labels {
		merged[k] = v
	}
	return common.LabelsSubsetMatcher{Labels: merged}
}

func toLabelNames(keys []string) []model.LabelName {
	names := make([]model.LabelName, len(keys))
	for i, k := range keys {
		names[i] = model.LabelName(k)
	}
	return names
}

// alertGroupMatchersFromRules generates GroupMatchers for an alert interval based on parsed rules.
func alertGroupMatchersFromRules(interval Interval, rules []*ParsedRule) []*GroupMatcher {
	labels := interval.Metric
	var groups []*GroupMatcher

	for _, rule := range rules {
		if !matchesScope(labels, rule.Scope) {
			continue
		}

		if rule.Match.isEmpty() {
			continue
		}

		matchers := generateMatchers(labels, rule.Match)
		if len(matchers) == 0 {
			continue
		}

		for _, m := range matchers {
			g := &GroupMatcher{
				Start:    interval.Start,
				Modified: interval.Start,
				End:      interval.End,
				Rule:     rule,
				Matchers: []common.LabelsMatcher{m},
			}
			groups = append(groups, g)
		}
	}

	return groups
}

func SortRulesByPriority(rules []*ParsedRule) {
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Priority < rules[j].Priority
	})
}
