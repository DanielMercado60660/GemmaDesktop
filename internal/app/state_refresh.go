package app

// RefreshState separates frequently changing service state from persistent
// entity deltas. Gaps and uncommon management changes request a full resync.
func (c *Core) refreshState(p Doc) (any, error) {
	changes, err := c.Store.EntityChanges(int64(number(p, "after", 0)), number(p, "limit", 200))
	if err != nil {
		return nil, err
	}
	for i := range changes.Changes {
		change := &changes.Changes[i]
		if change.Kind == "workflow" && change.Entity != nil {
			change.Entity["revision"] = workflowRevision(change.Entity)
		}
	}
	c.mu.Lock()
	runtime := clone(c.runtime)
	models := append([]Doc{}, c.models...)
	pulls := []Doc{}
	for _, p := range c.pullState {
		pulls = append(pulls, clone(p))
	}
	c.mu.Unlock()
	runtime["pulls"] = pulls
	live := Doc{"runtime": runtime, "models": models}
	profiles, err := c.qualifiedProfileSummariesV2()
	if err != nil {
		return nil, err
	}
	live["effectiveProfiles"] = profiles
	if c.managed != nil {
		live["managedRuntime"] = c.managed.PresentationSnapshot()
	}
	liveCursor := contentDigest(live)
	if str(p, "liveCursor") == liveCursor {
		live = Doc{}
	}
	activity, err := c.Store.ActivitySince(int64(number(p, "eventAfter", 0)), 100)
	if err != nil {
		return nil, err
	}
	return Doc{"changes": changes.Changes, "cursor": changes.Cursor, "hasMore": changes.HasMore, "resync": changes.Resync, "live": live, "liveCursor": liveCursor, "events": activity.Events, "eventCursor": activity.Cursor, "eventsHaveMore": activity.HasMore}, nil
}
