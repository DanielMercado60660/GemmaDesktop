package main

func solve(x map[string]any) any {
	out := make(map[string]int)
	now := x["now"].(float64)
	window := x["window"].(float64)
	events := x["events"].([]any)
	for _, raw := range events {
		e := raw.(map[string]any)
		at := e["at"].(float64)
		if at >= now-window && at <= now {
			user := e["user"].(string)
			units := int(e["units"].(float64))
			out[user] += units
		}
	}
	return out
}
