"""Collect complete, cursor-paged conversation evidence from the app API."""

def conversation(api, session_id):
    result, events, cursor = None, [], 0
    for _ in range(1000):
        page = api("session.get", {"id": session_id, "after": cursor, "limit": 1000})
        result = page
        events.extend(page.get("events", []))
        if not page.get("hasMore"):
            result["events"] = events
            return result
        next_cursor = page.get("eventCursor", 0)
        if next_cursor <= cursor:
            raise RuntimeError("Conversation cursor did not advance")
        cursor = next_cursor
    raise RuntimeError("Conversation exceeded evidence page limit")
