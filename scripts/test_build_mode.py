"""Exercise the source-build entry point with mocked toolchains, without inference."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


class BuildModeTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / "scripts").mkdir()
        for name in ("build.sh", "env.sh", "package_harness.py", "runtime-package.py", "runtime_resources.py", "build-info.py"):
            shutil.copy2(Path(__file__).parent / name, self.root / "scripts" / name)
        self.write(".gitignore", "dist/\nfrontend/dist/\ntools.log\n.cache/\n")
        self.write("main.go", "package main\n")
        self.write("frontend/package.json", "{}")
        self.write("packaging/Info.plist", "fixture metadata")
        self.write("scripts/harness/fixture.py", "# packaged harness fixture\n")
        self.write("third_party/harness2/LICENSE", "fixture license\n")
        for name in ("go", "npm", "codesign"):
            stub = self.write("stubbin/" + name, "#!" + sys.executable + "\n" + '''
import json
from pathlib import Path
import sys
root = Path(__file__).resolve().parents[1]
with (root / "tools.log").open("a") as log:
    log.write(json.dumps([Path(__file__).name, *sys.argv[1:]]) + "\\n")
if Path(__file__).name == "npm" and sys.argv[1:] == ["run", "build"]:
    path = root / "frontend/dist/index.html"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("<html>mock frontend</html>")
if Path(__file__).name == "go" and sys.argv[1] == "build":
    path = Path(sys.argv[sys.argv.index("-o") + 1])
    path.write_text("mock compiled and signed binary")
''')
            stub.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root / "stubbin") + os.pathsep + os.environ["PATH"])
        self.env.pop("GEMMA_RUNTIME_BUNDLES", None)
        self.git("init", "-q")
        self.git("add", ".")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")

    def write(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def git(self, *args):
        subprocess.run(["git", *args], cwd=self.root, check=True, capture_output=True)

    def build(self, *args):
        return subprocess.run(["/bin/zsh", "scripts/build.sh", *args], cwd=self.root,
                              env=self.env, capture_output=True, text=True, timeout=30)

    def test_explicit_ollama_only_clean_checkout_build_captures_and_verifies_mode(self):
        # A previous full build must not silently contribute a runtime catalog.
        self.write("dist/Gemma Desktop.app/Contents/Resources/runtimes/catalog.json", "stale")
        result = self.build("--ollama-only")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        info = json.loads((self.root / "dist/build-info.json").read_text())
        self.assertEqual(info["buildMode"], "ollama-only")
        self.assertFalse(info["runtimeResourcesIncluded"])
        self.assertFalse(info["runtimeResourcesVerified"])
        self.assertTrue(info["inputsVerifiedUnchanged"])
        self.assertTrue(info["harnessResourceFiles"])
        calls = [json.loads(line) for line in (self.root / "tools.log").read_text().splitlines()]
        self.assertIn(["npm", "ci"], calls)
        self.assertIn(["codesign", "--verify", "--deep", "--strict", "dist/Gemma Desktop.app"], calls)
        self.assertFalse((self.root / "dist/Gemma Desktop.app/Contents/Resources/runtimes").exists())

    def test_default_full_installer_build_still_fails_without_private_runtime_inputs(self):
        self.write("dist/build-info.json", '{"stale":true}')
        self.write("dist/build-inputs.json", '{"stale":true}')
        result = self.build()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("catalog is missing", result.stderr)
        self.assertFalse((self.root / "dist/build-info.json").exists())
        self.assertFalse((self.root / "dist/build-inputs.json").exists())
        self.assertNotIn('"codesign"', (self.root / "tools.log").read_text())

    def test_unknown_argument_is_rejected_before_toolchains_or_packaging(self):
        for args in (("--unknown",), ("--ollama-only", "--ollama-only")):
            with self.subTest(args=args):
                result = self.build(*args)
                self.assertEqual(result.returncode, 2)
                self.assertIn("Usage:", result.stderr)
                self.assertFalse((self.root / "tools.log").exists())


if __name__ == "__main__":
    unittest.main()
