package app

import "testing"

func TestUnchangedLiveStateStillDeliversApprovalAndEntityUpdates(t *testing.T) {
	c := editorTestCore(t)
	c.runtime = Doc{"available": true}
	c.models = []Doc{{"name": "fixture"}}
	first, err := c.refreshState(Doc{})
	if err != nil {
		t.Fatal(err)
	}
	a := obj(first)
	if str(a, "liveCursor") == "" || len(obj(a["live"])) == 0 {
		t.Fatal("missing initial live state")
	}
	if err = c.Store.Put("approval", Doc{"id": "approval", "status": "pending"}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Store.Event(Doc{"type": "approval", "title": "Review action"}); err != nil {
		t.Fatal(err)
	}
	second, err := c.refreshState(Doc{"after": a["cursor"], "eventAfter": a["eventCursor"], "liveCursor": a["liveCursor"]})
	if err != nil {
		t.Fatal(err)
	}
	b := obj(second)
	if len(obj(b["live"])) != 0 || str(a, "liveCursor") != str(b, "liveCursor") {
		t.Fatal("unchanged live values retransmitted")
	}
	// Normalize to JSON just as the desktop bridge does.
	if len(b["changes"].([]any)) == 0 || len(b["events"].([]any)) == 0 {
		t.Fatal("live cursor suppressed consequential update", b)
	}
	c.runtime["available"] = false
	third, err := c.refreshState(Doc{"liveCursor": b["liveCursor"]})
	if err != nil {
		t.Fatal(err)
	}
	if len(obj(obj(third)["live"])) == 0 {
		t.Fatal("changed runtime hidden")
	}
}
