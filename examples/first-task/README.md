# First agent task

This fixture is intentionally broken: all three tests should fail initially.
Copy the Python files into a disposable directory before giving the agent a task.
Follow the [README demo](../../README.md#try-a-small-task).

The target is to fix `calculator.py` while leaving `test_calculator.py` unchanged,
then run `python3 -m unittest -v`. This is a small demonstration of governed file
editing and command execution, not a coding benchmark.
