import test from "node:test";
import assert from "node:assert/strict";
import {
  checkContract,
  maxCheckArguments,
  parseCheckCommand,
  shellOperatorError,
} from "../src/lib/checkCommand.ts";

const argv = (text) => {
  const result = parseCheckCommand(text);
  assert.equal(result.ok, true, result.error);
  return result.argv;
};
const error = (text) => {
  const result = parseCheckCommand(text);
  assert.equal(result.ok, false, `expected ${JSON.stringify(text)} to fail`);
  return result.error;
};

test("empty or whitespace check commands mean no check", () => {
  assert.deepEqual(argv(""), []);
  assert.deepEqual(argv("   \t "), []);
});

test("splits words on whitespace without a shell", () => {
  assert.deepEqual(argv("  python3   -m unittest -v "), [
    "python3",
    "-m",
    "unittest",
    "-v",
  ]);
  assert.deepEqual(argv("go test ./..."), ["go", "test", "./..."]);
});

test("single quotes are literal and double quotes honour shell escapes", () => {
  assert.deepEqual(argv(`pytest -k 'not slow and "quoted"'`), [
    "pytest",
    "-k",
    'not slow and "quoted"',
  ]);
  assert.deepEqual(argv(`echo 'a\\b'`), ["echo", "a\\b"]);
  assert.deepEqual(argv(`echo "say \\"hi\\" \\\\ \\$HOME \\n"`), [
    "echo",
    'say "hi" \\ $HOME \\n',
  ]);
  assert.deepEqual(argv(`pre"mid dle"'post fix'`), ["premid dlepost fix"]);
  assert.deepEqual(argv(`node '' ""`), ["node", "", ""]);
});

test("unquoted backslashes escape the next character", () => {
  assert.deepEqual(argv("cat my\\ file.txt \\'x\\'"), [
    "cat",
    "my file.txt",
    "'x'",
  ]);
  assert.deepEqual(argv("grep a\\|b log"), ["grep", "a|b", "log"]);
  assert.match(error("pytest tests\\"), /escape/);
});

test("rejects unbalanced quotes", () => {
  assert.match(error(`pytest -k 'slow`), /single quote/);
  assert.match(error(`pytest -k "slow`), /double quote/);
  assert.match(error(`echo "a\\"`), /double quote/);
});

test("rejects shell operators but allows them inside quotes", () => {
  for (const operator of ["|", "||", "&&", ";", ">", ">>", "<", "&"])
    assert.equal(error(`make test ${operator} echo done`), shellOperatorError);
  assert.equal(error("pytest|tee log"), shellOperatorError);
  assert.equal(error("pytest 2>&1"), shellOperatorError);
  assert.equal(error("make test;"), shellOperatorError);
  assert.deepEqual(argv(`grep -E 'a|b' "x && y" "1;2" '<>'`), [
    "grep",
    "-E",
    "a|b",
    "x && y",
    "1;2",
    "<>",
  ]);
});

test("requires a program and bounds the argument count", () => {
  assert.match(error(`"" -v`), /program/);
  assert.match(error(`'  ' -v`), /program/);
  const limit = ["true", ...Array(maxCheckArguments - 1).fill("x")];
  assert.equal(argv(limit.join(" ")).length, maxCheckArguments);
  assert.match(error([...limit, "x"].join(" ")), /at most 257/);
  assert.match(error("echo 'a\0b'"), /unsupported/);
});

test("rejects a leading variable assignment that only a shell would apply", () => {
  assert.match(error("CI=1 npm test"), /variable assignments need a shell/);
  assert.match(error("PYTHONPATH=src python3 -m pytest"), /program/);
  assert.deepEqual(argv("env CI=1 npm test"), ["env", "CI=1", "npm", "test"]);
  assert.deepEqual(argv("pytest --maxfail=1"), ["pytest", "--maxfail=1"]);
  assert.deepEqual(argv("./a=b/run"), ["./a=b/run"]);
});

test("builds the host contract from a fresh copy of argv", () => {
  const command = ["python3", "-m", "unittest"];
  const contract = checkContract(command);
  command.push("mutated");
  assert.deepEqual(contract, {
    command: ["python3", "-m", "unittest"],
    workingDirectory: "",
    protectedPaths: [],
    requireRegressionTest: false,
  });
});
