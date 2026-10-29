package main

func solve(x map[string]any) any {
	out := make(map[string]int)
	events := x["events"].([]any)
	now := x["now"].(float64)
	window := x["window"].(float64)
	lowerBound := now - window

	for _, raw := range events {
		e := raw.(map[string]any)
		at := e["at"].(float64)
		user := e["user"].(string)
		units := int(e["units"].(float64))

		if at >= lowerBound && at <= now {
			out[user] += units
		}
	}
	return out
}
