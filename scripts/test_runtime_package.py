"""Runtime packaging contracts; synthetic archives only, no downloads or inference."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("runtime_package", Path(__file__).with_name("runtime-package.py"))
runtime_package = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime_package)


class RuntimePackageTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.resources = self.root / "dist/Gemma Desktop.app/Contents/Resources"
        self.resources.mkdir(parents=True)

    def valid_runtime(self):
        source = self.root / "dist/runtime-resources"
        source.mkdir()
        payload = b"synthetic executable"
        bundle = io.BytesIO()
        with tarfile.open(fileobj=bundle, mode="w:gz") as archive:
            member = tarfile.TarInfo("bin/runtime")
            member.size, member.mode = len(payload), 0o755
            archive.addfile(member, io.BytesIO(payload))
        archive_bytes = bundle.getvalue()
        catalog = {"version": 1, "runtimes": [{"version": 1, "id": "fixture", "provider": "omlx",
            "bundle": {"bundledPath": "runtime.tar.gz", "size": len(archive_bytes), "sha256": hashlib.sha256(archive_bytes).hexdigest()},
            "files": [{"path": "bin/runtime", "size": len(payload), "sha256": hashlib.sha256(payload).hexdigest(), "executable": True}],
            "executable": "bin/runtime"}], "models": []}
        (source / "catalog.json").write_text(json.dumps(catalog))
        (source / "runtime.tar.gz").write_bytes(archive_bytes)
        return source

    def test_default_full_build_requires_runtime_inputs_and_preserves_prior_bundle(self):
        prior = self.resources / "runtimes"
        prior.mkdir()
        (prior / "sentinel").write_text("prior bundle")
        with self.assertRaisesRegex(RuntimeError, "catalog is missing"):
            runtime_package.package(self.root)
        self.assertTrue((prior / "sentinel").exists())

    def test_full_build_copies_only_verified_inventory_and_rejects_corruption(self):
        source = self.valid_runtime()
        expected = runtime_package.package(self.root)
        self.assertEqual(set(expected), {"catalog.json", "runtime.tar.gz"})
        self.assertEqual(runtime_package.resource_inventory(self.resources / "runtimes"), expected)
        (source / "runtime.tar.gz").write_bytes(b"corrupt")
        with self.assertRaisesRegex(RuntimeError, "digest differs"):
            runtime_package.package(self.root)
        self.assertEqual(runtime_package.resource_inventory(self.resources / "runtimes"), expected)

    def test_ollama_only_removes_owned_runtime_outputs_but_keeps_inputs_and_other_resources(self):
        source = self.valid_runtime()
        for name in ("runtimes", "runtimes-previous", "scripts"):
            (self.resources / name).mkdir()
            (self.resources / name / "sentinel").write_text(name)
        self.assertEqual(runtime_package.package(self.root, "ollama-only"), {})
        self.assertFalse((self.resources / "runtimes").exists())
        self.assertFalse((self.resources / "runtimes-previous").exists())
        self.assertEqual((self.resources / "scripts/sentinel").read_text(), "scripts")
        self.assertTrue((source / "runtime.tar.gz").exists())

    def test_owned_runtime_symlink_is_unlinked_without_touching_target(self):
        outside = self.root / "unrelated"
        outside.mkdir()
        (outside / "sentinel").write_text("preserve")
        (self.resources / "runtimes").symlink_to(outside, target_is_directory=True)
        runtime_package.package(self.root, "ollama-only")
        self.assertEqual((outside / "sentinel").read_text(), "preserve")
        self.assertFalse((self.resources / "runtimes").is_symlink())

    def test_linked_bundle_ancestor_is_rejected_without_touching_target(self):
        other = self.root / "other-resources"
        other.mkdir()
        (other / "runtimes").mkdir()
        self.resources.rmdir()
        self.resources.symlink_to(other, target_is_directory=True)
        with self.assertRaisesRegex(RuntimeError, "must not be symlinks"):
            runtime_package.package(self.root, "ollama-only")
        self.assertTrue((other / "runtimes").is_dir())

    def test_interrupted_stage_is_rejected_without_deleting_unknown_residue(self):
        stage = self.resources / "runtimes-stage-interrupted"
        stage.mkdir()
        with self.assertRaisesRegex(RuntimeError, "contains managed runtime"):
            runtime_package.package(self.root, "ollama-only")
        self.assertTrue(stage.exists())

    def test_unknown_mode_fails_before_altering_outputs(self):
        (self.resources / "runtimes").mkdir()
        with self.assertRaisesRegex(ValueError, "Unknown build mode"):
            runtime_package.package(self.root, "fallback")
        self.assertTrue((self.resources / "runtimes").exists())


if __name__ == "__main__":
    unittest.main()
