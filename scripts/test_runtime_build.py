import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("runtime_builder", Path(__file__).with_name("runtime-build.py"))
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class RuntimeBuildTests(unittest.TestCase):
    def test_archive_reproducibility_and_immutable_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tree = root / "tree"
            tree.mkdir()
            (tree / "mlx-serve").write_bytes(b"fixture executable")
            (tree / "mlx-serve").chmod(0o755)
            previous = builder.RESOURCES
            try:
                hashes = []
                for destination in ("first", "second"):
                    builder.RESOURCES = root / destination
                    with contextlib.redirect_stdout(io.StringIO()):
                        builder.package_tree(tree, "mlx_serve", "fixture-v1", "a" * 40, "26.2")
                    catalog = json.loads((builder.RESOURCES / "catalog.json").read_text())
                    hashes.append(catalog["runtimes"][0]["bundle"]["sha256"])
                self.assertEqual(hashes[0], hashes[1])
                manifest_before = (builder.RESOURCES / "catalog.json").read_bytes()
                with contextlib.redirect_stdout(io.StringIO()):
                    builder.package_tree(tree, "mlx_serve", "fixture-v1", "a" * 40, "26.2")
                self.assertEqual(manifest_before, (builder.RESOURCES / "catalog.json").read_bytes())
                (tree / "mlx-serve").write_bytes(b"changed executable")
                with self.assertRaisesRegex(ValueError, "immutable"):
                    builder.package_tree(tree, "mlx_serve", "fixture-v1", "a" * 40, "26.2")
                self.assertEqual(manifest_before, (builder.RESOURCES / "catalog.json").read_bytes())
            finally:
                builder.RESOURCES = previous

    def test_build_identifier_cannot_escape_resources(self):
        with self.assertRaisesRegex(ValueError, "Build ID"):
            builder.package_tree(Path("."), "omlx", "../escape", "a" * 40, "15.0")


if __name__ == "__main__":
    unittest.main()
