import type { VerificationContract } from "../types";

export type CheckCommandResult =
  | { ok: true; argv: string[] }
  | { ok: false; error: string };

// Mirrors the host contract bounds so an invalid command is explained here
// instead of failing run admission.
export const maxCheckArguments = 257;
const maxArgumentBytes = 256 << 10;
const operatorCharacters = new Set(["|", "&", ";", "<", ">"]);
const doubleQuoteEscapes = new Set(['"', "\\", "$", "`"]);
export const shellOperatorError =
  "Use a single command — shells aren't available.";

const fail = (error: string): CheckCommandResult => ({ ok: false, error });

// The check runs as argv without a shell, so anything a shell would treat as
// an operator is rejected rather than silently passed as a literal argument.
export function parseCheckCommand(text: string): CheckCommandResult {
  const argv: string[] = [];
  let word = "",
    inWord = false,
    i = 0;
  while (i < text.length) {
    const c = text[i];
    if (/\s/.test(c)) {
      if (inWord) argv.push(word);
      word = "";
      inWord = false;
      i++;
    } else if (c === "'") {
      const end = text.indexOf("'", i + 1);
      if (end < 0) return fail("Close the single quote in the check command.");
      word += text.slice(i + 1, end);
      inWord = true;
      i = end + 1;
    } else if (c === '"') {
      inWord = true;
      for (i++; ; i++) {
        if (i >= text.length)
          return fail("Close the double quote in the check command.");
        if (text[i] === '"') break;
        if (text[i] === "\\" && doubleQuoteEscapes.has(text[i + 1])) i++;
        word += text[i];
      }
      i++;
    } else if (c === "\\") {
      if (i + 1 >= text.length)
        return fail("Finish the escape at the end of the check command.");
      word += text[i + 1];
      inWord = true;
      i += 2;
    } else if (operatorCharacters.has(c)) {
      return fail(shellOperatorError);
    } else {
      word += c;
      inWord = true;
      i++;
    }
  }
  if (inWord) argv.push(word);
  if (argv.length > maxCheckArguments)
    return fail(`Use at most ${maxCheckArguments} arguments in the check.`);
  if (argv.length && !argv[0].trim())
    return fail("Start the check with the program to run.");
  // `NAME=value cmd` sets a variable only in a shell; as argv the host would
  // look for a program named "NAME=value" and the check could never pass.
  if (argv.length && /^[A-Za-z_][A-Za-z0-9_]*=/.test(argv[0]))
    return fail(
      "Start the check with the program to run; variable assignments need a shell.",
    );
  const encoder = new TextEncoder();
  if (
    argv.some(
      (arg) =>
        arg.includes("\0") || encoder.encode(arg).length > maxArgumentBytes,
    )
  )
    return fail("The check command contains an unsupported argument.");
  return { ok: true, argv };
}

export function checkContract(argv: string[]): VerificationContract {
  return {
    command: [...argv],
    workingDirectory: "",
    protectedPaths: [],
    requireRegressionTest: false,
  };
}
