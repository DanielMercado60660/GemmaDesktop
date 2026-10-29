"""Local Git/filesystem fixtures for build input provenance; no app compilation."""
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("gemma_build_info", Path(__file__).with_name("build-info.py"))
build_info = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build_info)


class BuildInfoTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.git("init", "-q")
        self.write(".gitignore", "dist/\nfrontend/dist/\n")
        self.write("main.go", "package main\n")
        self.write("docs/note.md", "excluded documentation\n")
        self.git("add", ".gitignore", "main.go", "docs/note.md")
        self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
        self.write("frontend/dist/index.html", "<script src='assets/app.js'></script>")
        self.write("frontend/dist/assets/app.js", "console.log('fixture')")
        self.write("dist/Gemma Desktop.app/Contents/MacOS/gemma-desktop", "fixture binary")

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, stderr=subprocess.STDOUT)

    def write(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def test_matching_inputs_include_ignored_frontend_and_signed_binary_digest(self):
        before = build_info.capture(self.root)
        self.assertIn("frontend/dist/assets/app.js", before["sourceFiles"])
        self.assertNotIn("docs/note.md", before["sourceFiles"])
        # Signing can change binary bytes; the manifest records the final bytes.
        self.write("dist/Gemma Desktop.app/Contents/MacOS/gemma-desktop", "signed fixture binary")
        result = build_info.verify(self.root)
        self.assertTrue(result["inputsVerifiedUnchanged"])
        self.assertEqual(before["sourceFingerprint"], result["sourceFingerprint"])
        self.assertEqual(before["frontendFingerprint"], result["frontendFingerprint"])
        self.assertEqual(build_info.file_digest(self.root / "dist/Gemma Desktop.app/Contents/MacOS/gemma-desktop"), result["binarySHA256"])
        self.assertEqual(result, json.loads((self.root / "dist/build-info.json").read_text()))

    def assert_drift(self, mutation):
        build_info.capture(self.root)
        self.write("dist/build-info.json", '{"stale":true}')
        mutation()
        with self.assertRaisesRegex(RuntimeError, "drift detected"):
            build_info.verify(self.root)
        self.assertFalse((self.root / "dist/build-info.json").exists())

    def test_source_edit_is_rejected(self):
        self.assert_drift(lambda: self.write("main.go", "package changed\n"))

    def test_generated_frontend_edit_is_rejected(self):
        self.assert_drift(lambda: self.write("frontend/dist/assets/app.js", "different code"))

    def test_untracked_source_addition_is_rejected(self):
        self.assert_drift(lambda: self.write("new.go", "package main\n"))

    def test_source_deletion_is_rejected(self):
        self.assert_drift(lambda: (self.root / "main.go").unlink())

    def test_head_change_is_rejected_even_with_identical_input_files(self):
        self.assert_drift(lambda: self.git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "new head"))

    def test_documentation_drift_does_not_claim_different_compiled_inputs(self):
        build_info.capture(self.root)
        self.write("docs/note.md", "new documentation")
        result = build_info.verify(self.root)
        self.assertFalse(result["sourceDirty"])
        self.assertTrue(result["sourceDirtyAfterBuild"])

    def test_capture_is_required_and_clears_stale_manifest(self):
        self.write("dist/build-info.json", '{"stale":true}')
        with self.assertRaisesRegex(RuntimeError, "not captured"):
            build_info.verify(self.root)
        self.assertFalse((self.root / "dist/build-info.json").exists())
        self.write("dist/build-info.json", '{"stale":true}')
        build_info.capture(self.root)
        self.assertFalse((self.root / "dist/build-info.json").exists())

    def test_missing_frontend_cannot_be_captured(self):
        (self.root / "frontend/dist/index.html").unlink()
        with self.assertRaisesRegex(RuntimeError, "build the frontend"):
            build_info.capture(self.root)

    def test_packaged_harness_must_match_source(self):
        self.write("scripts/harness/upstream_adapter.py", "print('adapter')")
        self.write("third_party/harness2/LICENSE", "upstream license")
        build_info.capture(self.root)
        with self.assertRaisesRegex(RuntimeError, "Packaged Harness resources"):
            build_info.verify(self.root)
        self.write("dist/Gemma Desktop.app/Contents/Resources/scripts/harness/upstream_adapter.py", "print('adapter')")
        self.write("dist/Gemma Desktop.app/Contents/Resources/third_party/harness2/LICENSE", "upstream license")
        verified = build_info.verify(self.root)
        self.assertEqual(len(verified["harnessResourceFiles"]), 2)
        self.write("dist/Gemma Desktop.app/Contents/Resources/scripts/harness/upstream_adapter.py", "print('changed')")
        with self.assertRaisesRegex(RuntimeError, "Packaged Harness resources"):
            build_info.verify(self.root)


if __name__ == "__main__":
    unittest.main()
