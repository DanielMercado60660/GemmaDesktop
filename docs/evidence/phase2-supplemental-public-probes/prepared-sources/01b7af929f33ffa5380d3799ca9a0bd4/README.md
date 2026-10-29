# Aggregate bounded usage windows

Repair this go service module and add regression tests.

Return per-user total units for events in the closed time interval [now-window, now]. Include zero-unit users when an event qualifies. Exclude future and older events. Units are nonnegative integers. Do not mutate input.

The CLI accepts one JSON array argument and prints one JSON array of corresponding results, with no extra stdout. Preserve this interface and the separate logic module. Run `go test ./...` and report actual results. No external dependencies are needed.

Example input:
```json
{
  "events": [
    {
      "user": "a",
      "at": 90,
      "units": 3
    },
    {
      "user": "a",
      "at": 100,
      "units": 2
    },
    {
      "user": "b",
      "at": 101,
      "units": 9
    }
  ],
  "now": 100,
  "window": 10
}
```
Expected result:
```json
{
  "a": 5
}
```
