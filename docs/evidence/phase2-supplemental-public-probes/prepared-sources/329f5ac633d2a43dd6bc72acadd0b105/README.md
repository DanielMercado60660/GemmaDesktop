# Apply layered service configuration

Repair this node service module and add regression tests.

Merge layers from left to right recursively for objects. Null deletes a key. Arrays and other values replace earlier values; false, zero and empty strings remain valid. Return a new object without mutating any layer.

The CLI accepts one JSON array argument and prints one JSON array of corresponding results, with no extra stdout. Preserve this interface and the separate logic module. Run `node public_test.cjs` and report actual results. No external dependencies are needed.

Example input:
```json
{
  "layers": [
    {
      "service": {
        "port": 80,
        "tls": true
      },
      "tags": [
        "old"
      ]
    },
    {
      "service": {
        "port": 0
      },
      "tags": []
    }
  ]
}
```
Expected result:
```json
{
  "service": {
    "port": 0,
    "tls": true
  },
  "tags": []
}
```
