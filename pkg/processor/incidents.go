package processor

import (
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/common/model"

	"github.com/openshift/cluster-health-analyzer/pkg/common"
	"github.com/openshift/cluster-health-analyzer/pkg/prom"
)

type Interval struct {
	Metric model.LabelSet
	Start  model.Time
	End    model.Time
}

func (i Interval) String() string {
	return fmt.Sprintf("(%s -> %s): %s", i.Start.Time(), i.End.Time(), i.Metric)
}

type GroupedInterval struct {
	Interval
	GroupMatcher *GroupMatcher
}

func (gi GroupedInterval) String() string {
	return fmt.Sprintf("%s: %s", gi.GroupMatcher.RootGroupID, gi.Interval)
}

type Change struct {
	Timestamp model.Time
	Intervals []Interval
}

func (c Change) String() string {
	var str string
	str += fmt.Sprintf("Timestamp: %s\n", c.Timestamp.Time())
	for _, i := range c.Intervals {
		str += fmt.Sprintln(i)
	}
	return str
}

type ChangeSet []Change

func MetricsIntervals(rangeVector prom.RangeVector) []Interval {
	if len(rangeVector) == 0 {
		return nil
	}
	step := rangeVector[0].Step

	ret := make([]Interval, 0)
	for _, r := range rangeVector {
		if len(r.Samples) == 0 {
			continue
		}
		start := r.Samples[0].Timestamp
		end := start

		for i := 1; i < len(r.Samples); i++ {
			sample := r.Samples[i]
			if sample.Timestamp.Sub(end) > step {
				// The end of the previous interval.
				ret = append(ret, Interval{Metric: r.Metric, Start: start, End: end})
				// Start of the new interval.
				start = sample.Timestamp
				end = start
			} else {
				// Current interval continues.
				end = sample.Timestamp
			}
		}
		// The last interval.
		ret = append(ret, Interval{Metric: r.Metric, Start: start, End: end})
	}
	return ret
}

// MetricsChanges returns a list of changes in the alerts.
//
// The changes are grouped by the timestamp of the change and sorted
// by the timestamp.
func MetricsChanges(rangeVector prom.RangeVector) ChangeSet {
	intervals := MetricsIntervals(rangeVector)
	if len(intervals) == 0 {
		return nil
	}

	var ret ChangeSet

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start.Before(intervals[j].Start)
	})
	currGroup := make([]Interval, 0)
	currTime := intervals[0].Start

	for _, i := range intervals {
		if currTime == i.Start {
			// The same start - group together.
			currGroup = append(currGroup, i)
		} else {
			// Different start: save the current group and start a new one.
			ret = append(ret, Change{Timestamp: currTime, Intervals: currGroup})

			currGroup = []Interval{i}
			currTime = i.Start
		}
	}
	// Save the last group.
	ret = append(ret, Change{Timestamp: currTime, Intervals: currGroup})
	return ret
}

func (ch ChangeSet) String() string {
	var str string
	for i, c := range ch {
		if i > 0 {
			str += "\n"
		}
		str += c.String()
	}
	return str
}

// Group Matchers

type GroupMatcher struct {
	GroupID     string
	RootGroupID string
	Start       model.Time
	Modified    model.Time
	End         model.Time

	Rule     *ParsedRule
	Matchers []common.LabelsMatcher
}

func (g GroupMatcher) String() string {
	ruleName := "<nil>"
	if g.Rule != nil {
		ruleName = g.Rule.Name
	}
	return fmt.Sprintf("GroupID: %s, RootGroupID: %s, Start: %s, Modified: %s, End: %s, Rule: %s, Matchers: %v",
		g.GroupID, g.RootGroupID, g.Start.Time(), g.Modified.Time(), g.End.Time(), ruleName, g.Matchers)
}

func (g GroupMatcher) isSubsetOf(other *GroupMatcher) bool {
	if g.Rule != other.Rule {
		return false
	}

	for _, m := range g.Matchers {
		contains := false
		for _, om := range other.Matchers {
			if om.Equals(m) {
				contains = true
				break
			}
		}
		if !contains {
			return false
		}
	}
	return true
}

func (g *GroupMatcher) expandMatchers(matchers []common.LabelsMatcher) {
	for _, m := range matchers {
		found := false
		for _, gm := range g.Matchers {
			if gm.Equals(m) {
				found = true
				break
			}
		}
		if !found {
			g.Matchers = append(g.Matchers, m)
		}
	}
}

type match struct {
	GroupMatcher *GroupMatcher
	TimeDist     time.Duration
}

// timeProximityRule is an internal sentinel rule for root groups created by batch grouping.
var timeProximityRule = &ParsedRule{
	Name:     "time-proximity",
	Priority: 100,
	Within:   15 * time.Minute,
}

type GroupsCollection struct {
	Groups      []*GroupMatcher
	rulesSource func() *ParsedRulesSnapshot
}

func NewGroupsCollection(rulesSource func() *ParsedRulesSnapshot) *GroupsCollection {
	return &GroupsCollection{rulesSource: rulesSource}
}

func (gc *GroupsCollection) snapshot() *ParsedRulesSnapshot {
	if gc.rulesSource != nil {
		return gc.rulesSource()
	}
	return DefaultSnapshot()
}

func (gc *GroupsCollection) AddGroup(g *GroupMatcher) {
	gc.Groups = append(gc.Groups, g)
}

func (gc *GroupsCollection) ProcessIntervalsBatch(intervals []Interval) []GroupedInterval {
	snap := gc.snapshot()
	slog.Info("Processing", "intervals", len(intervals), "groups", len(gc.Groups), "rules", len(snap.Rules))
	groupedIntervals, unmatched := gc.tryMatchIntervals(intervals, snap)

	if len(unmatched) > 0 {
		newGroupedIntervals := gc.addIntervalsGroups(unmatched, nil, snap)
		groupedIntervals = append(groupedIntervals, newGroupedIntervals...)
	}

	return groupedIntervals
}

func (gc *GroupsCollection) processHistoricalAlerts(alertsRange prom.RangeVector) {
	changes := MetricsChanges(alertsRange)

	for _, change := range changes {
		gc.ProcessIntervalsBatch(change.Intervals)
	}
}

func (gc *GroupsCollection) ProcessAlertsBatch(alerts []model.LabelSet, timestamp time.Time) []model.LabelSet {
	modelT := model.TimeFromUnixNano(timestamp.UnixNano())

	intervals := make([]Interval, 0, len(alerts))
	for _, a := range alerts {
		intervals = append(intervals, Interval{
			Metric: a,
			Start:  modelT,
			End:    modelT,
		})
	}

	groupedIntervals := gc.ProcessIntervalsBatch(intervals)

	ret := make([]model.LabelSet, 0, len(alerts))
	for _, gi := range groupedIntervals {
		alert := gi.Metric
		if gi.GroupMatcher != nil {
			alert["group_id"] = model.LabelValue(gi.GroupMatcher.RootGroupID)
			if gi.GroupMatcher.Rule != nil {
				alert["group_rule"] = model.LabelValue(gi.GroupMatcher.Rule.Name)
			}
		}
		ret = append(ret, alert)
	}
	return ret
}

// PruneGroups removes groups that can't be matched anymore.
func (gc *GroupsCollection) PruneGroups(t time.Time) {
	newGroups := make([]*GroupMatcher, 0, len(gc.Groups))
	for _, g := range gc.Groups {
		within := 24 * time.Hour
		if g.Rule != nil {
			within = g.Rule.Within
		}
		threshold := model.TimeFromUnixNano(t.Add(-within).UnixNano())
		if g.Modified.Before(threshold) {
			continue
		}
		newGroups = append(newGroups, g)
	}
	gc.Groups = newGroups
}

func (gc *GroupsCollection) tryMatchIntervals(intervals []Interval, snap *ParsedRulesSnapshot) ([]GroupedInterval, []Interval) {
	var ret []GroupedInterval
	var unmatched []Interval
	for _, i := range intervals {
		matchedGroup := gc.bestMatch(i)
		if matchedGroup == nil {
			unmatched = append(unmatched, i)
			continue
		}

		if matchedGroup.Rule == nil || matchedGroup.Rule.Priority > 0 {
			matchedGroup.Modified = i.Start
		}
		matchedGroup.End = max(matchedGroup.End, i.End)

		newGroupedIntervals := gc.addIntervalsGroups([]Interval{i}, matchedGroup, snap)
		ret = append(ret, newGroupedIntervals...)
	}
	return ret, unmatched
}

func (gc *GroupsCollection) newRootGroup(i Interval, inactive bool) *GroupMatcher {
	rootGroupID := uuid.New().String()

	ret := GroupMatcher{
		GroupID:     rootGroupID,
		RootGroupID: rootGroupID,
		Start:       i.Start,
		Modified:    i.Start,
		End:         i.End,
		Rule:        timeProximityRule,
	}
	if inactive {
		ret.Modified = 0
	}

	gc.AddGroup(&ret)
	return &ret
}

func (gc *GroupsCollection) addIntervalsGroups(intervals []Interval, groupMatcher *GroupMatcher, snap *ParsedRulesSnapshot) []GroupedInterval {
	if len(intervals) == 0 {
		return nil
	}
	newGc := &GroupsCollection{}

	ret := make([]GroupedInterval, 0, len(intervals))

	// Phase 1: Rule-based grouping takes priority.
	// Each interval tries to match by rules first. Only intervals with no
	// rule matchers fall through to batch grouping.
	var batchLeftovers []Interval
	for _, i := range intervals {
		iGroupMatcher := groupMatcher

		if iGroupMatcher == nil {
			iGroupMatcher = newGc.bestMatch(i)
		}

		newGroupCands := alertGroupMatchersFromRules(i, snap.Rules)

		if iGroupMatcher == nil {
			if len(newGroupCands) > 0 {
				iGroupMatcher = newGc.newRootGroup(i, true)
			} else {
				batchLeftovers = append(batchLeftovers, i)
				continue
			}
		}

		if iGroupMatcher.Rule == nil || iGroupMatcher.Rule.Priority > 0 {
			for _, g := range newGroupCands {
				if g.Rule == iGroupMatcher.Rule && iGroupMatcher.isSubsetOf(g) {
					iGroupMatcher.expandMatchers(g.Matchers)
					if iGroupMatcher.Rule == nil || iGroupMatcher.Rule.Priority > 0 {
						iGroupMatcher.Modified = i.Start
					}
					iGroupMatcher.End = max(iGroupMatcher.End, i.End)
				} else {
					g.RootGroupID = iGroupMatcher.RootGroupID
					newGc.AddGroup(g)
				}
			}
		}

		ret = append(ret, GroupedInterval{i, iGroupMatcher})
	}

	// Phase 2: Batch grouping for intervals with no rule-based matchers.
	if len(batchLeftovers) > 0 {
		batchRoot := newGc.newRootGroup(batchLeftovers[0], false)
		for _, i := range batchLeftovers {
			ret = append(ret, GroupedInterval{i, batchRoot})
		}
	}

	for _, g := range newGc.Groups {
		gc.AddGroup(g)
	}
	return ret
}

func (gc *GroupsCollection) bestMatch(interval Interval) *GroupMatcher {
	candidates := gc.matches(interval)
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].TimeDist < candidates[j].TimeDist
	})

	var best *match
	for i := range candidates {
		m := &candidates[i]
		if best == nil {
			best = m
			continue
		}
		mPriority := rulePriority(m.GroupMatcher)
		bestPriority := rulePriority(best.GroupMatcher)
		if mPriority < bestPriority {
			best = m
		}
	}

	if best != nil {
		return best.GroupMatcher
	}
	return nil
}

func rulePriority(g *GroupMatcher) int {
	if g.Rule != nil {
		return g.Rule.Priority
	}
	return 1000
}

func (gc *GroupsCollection) matches(interval Interval) []match {
	var ret []match
	labels := interval.Metric
	for _, g := range gc.Groups {
		if interval.Start < g.Start {
			continue
		}

		var timeDist time.Duration
		if g.Rule != nil && g.Rule.Match.Exact {
			timeDist = interval.Start.Sub(g.End)
		} else {
			timeDist = interval.Start.Sub(g.Modified)
		}

		within := 24 * time.Hour
		if g.Rule != nil {
			within = g.Rule.Within
		}
		if timeDist > within {
			continue
		}

		if len(g.Matchers) == 0 {
			ret = append(ret, match{g, timeDist})
			continue
		}

		for _, m := range g.Matchers {
			if matched, _ := m.Matches(labels); matched {
				ret = append(ret, match{g, timeDist})
				break
			}
		}
	}
	return ret
}

/// Previous Incidents Matcher
///
/// The previous incidents matcher is used to match the current groups
/// with the previous incidents. This allows preserving the group UUIDs
/// after the restart of the analyzer.

type previousIncident struct {
	matcher *common.LabelsSubsetMatcher
	uuid    string
	start   model.Time
	end     model.Time
}

const previousIncidentsTolerance = 10 * time.Minute

type previousIncidentsMatcher struct {
	incidentsByStart []*previousIncident
	tolerance        time.Duration
}

func (pim *previousIncidentsMatcher) atTime(t model.Time) []*previousIncident {
	ret := make([]*previousIncident, 0)
	// Add some tolerance when comparing the start time.
	startT := t.Add(pim.tolerance)
	// For end time, subtract the tolerance, in case the incident ended
	// before the current time.
	endT := t.Add(-pim.tolerance)

	startIdx := sort.Search(len(pim.incidentsByStart), func(i int) bool {
		// find first incident that started after the given time.
		return !pim.incidentsByStart[i].start.Before(startT)
	})
	// We want to include the incident that started just before the current time.
	startIdx = max(0, startIdx)
	for i := 0; i < startIdx; i++ {
		incident := pim.incidentsByStart[i]
		if incident.end.After(endT) {
			ret = append(ret, incident)
		}
	}

	return ret
}

func (pim *previousIncidentsMatcher) match(labels model.LabelSet, time model.Time) *previousIncident {
	candidates := pim.atTime(time)

	for _, c := range candidates {
		ok, _ := c.matcher.Matches(labels)

		if ok {
			return c
		}
	}
	return nil
}

func newPreviousIncidentsMatcher(healthMapRV prom.RangeVector) *previousIncidentsMatcher {
	componentsMapChanges := MetricsChanges(healthMapRV)
	prevIncidents := make([]*previousIncident, 0, len(componentsMapChanges))
	for _, change := range componentsMapChanges {
		for _, interval := range change.Intervals {
			labels := interval.Metric
			prevIncidents = append(prevIncidents, &previousIncident{
				matcher: &common.LabelsSubsetMatcher{Labels: common.SrcLabels(model.Metric(labels))},
				uuid:    string(labels["group_id"]),
				start:   interval.Start,
				end:     interval.End,
			})
		}
	}

	incidentsByStart := slices.Clone(prevIncidents)
	sort.Slice(incidentsByStart, func(i, j int) bool {
		return incidentsByStart[i].start.Before(incidentsByStart[j].start)
	})

	return &previousIncidentsMatcher{
		incidentsByStart: incidentsByStart,
		tolerance:        previousIncidentsTolerance,
	}
}

func (gc *GroupsCollection) UpdateGroupUUIDs(healthMapRV prom.RangeVector) {
	unmappedGroups := make(map[string][]*GroupMatcher)
	mappedGroupIDs := make(map[string]struct{})

	// Prepare map of groups by the root group ID.
	for _, g := range gc.Groups {
		groups, ok := unmappedGroups[g.RootGroupID]
		if !ok {
			unmappedGroups[g.RootGroupID] = []*GroupMatcher{g}
		} else {
			unmappedGroups[g.RootGroupID] = append(groups, g)
		}
	}

	prevIncidentsMatcher := newPreviousIncidentsMatcher(healthMapRV)

	for _, g := range gc.Groups {
		// Check if the group is still unmapped.
		if _, ok := unmappedGroups[g.RootGroupID]; !ok {
			continue
		}

		for _, m := range g.Matchers {
			sm, ok := m.(common.LabelsSubsetMatcher)
			if !ok {
				continue
			}
			prevIncident := prevIncidentsMatcher.match(sm.Labels, g.End)
			if prevIncident != nil {
				newGroupID := prevIncident.uuid
				oldGroupID := g.RootGroupID
				// Replace all occurrences of old group ID with the new one and.
				for _, g := range unmappedGroups[oldGroupID] {
					g.RootGroupID = newGroupID
					mappedGroupIDs[newGroupID] = struct{}{}
				}
				// Remove the old group from the list of unmapped groups.
				delete(unmappedGroups, oldGroupID)
				break
			}
		}
	}
}
