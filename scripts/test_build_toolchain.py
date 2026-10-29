"""Offline fixtures only: no downloads, Docker invocation, or image builds."""
import base64
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("build_toolchain", Path(__file__).with_name("build-toolchain.py"))
setup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(setup)


class Response(io.BytesIO):
    status = 200

    def __init__(self, data, headers=None, url=setup.TYPESCRIPT_URL):
        super().__init__(data)
        self.headers = headers or {}
        self.url = url

    def geturl(self):
        return self.url


def integrity(data):
    return "sha512-" + base64.b64encode(hashlib.sha512(data).digest()).decode("ascii")


class ToolchainSetupTests(unittest.TestCase):
    def test_checked_in_recipe_has_exact_reviewed_archive_and_official_image_pins(self):
        recipe = Path(__file__).resolve().parents[1] / "internal/execution/toolchain.Dockerfile"
        data = recipe.read_bytes()
        self.assertEqual(setup.verify_dockerfile(data), setup.TYPESCRIPT_SRI)
        for changed in [data.replace(setup.OFFICIAL_IMAGES[0].encode(), b"node:latest"), data.replace(setup.TYPESCRIPT_SRI.encode(), b"sha512-forged"), data + b"\nRUN --network=host echo unsafe\n"]:
            with self.assertRaises(RuntimeError):
                setup.verify_dockerfile(changed)

    def test_archive_is_returned_only_after_exact_hash_validation(self):
        data = b"bounded archive fixture\x00\xff"
        self.assertEqual(setup.read_verified_archive(Response(data), integrity(data)), data)
        with self.assertRaisesRegex(RuntimeError, "SHA512 integrity"):
            setup.read_verified_archive(Response(data + b"tampered"), integrity(data))

    def test_declared_or_streamed_oversize_is_rejected_before_buffering(self):
        declared = Response(b"not read", {"Content-Length": "9"})
        with self.assertRaisesRegex(RuntimeError, "size limit"):
            setup.read_verified_archive(declared, integrity(b"not read"), limit=8)
        self.assertEqual(declared.tell(), 0)
        streamed = Response(b"123456789EXCESS")
        with self.assertRaisesRegex(RuntimeError, "size limit"):
            setup.read_verified_archive(streamed, integrity(b"123456789EXCESS"), limit=8)
        self.assertEqual(streamed.tell(), 9)

    def test_wrong_origin_encoding_and_redirect_are_rejected(self):
        for response in [Response(b"x", url="https://other.invalid/file"), Response(b"x", {"Content-Encoding": "gzip"})]:
            with self.assertRaises(RuntimeError):
                setup.read_verified_archive(response, integrity(b"x"))
            self.assertEqual(response.tell(), 0)
        with self.assertRaisesRegex(RuntimeError, "redirects"):
            setup.NoRedirect().redirect_request(None, None, 302, "Found", {}, "https://other.invalid/file")

    def test_build_context_has_only_two_explicit_files_and_network_none(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "host-credential-canary").write_text("never copied")
            context = setup.prepare_context(root, b"FROM fixture\n", b"verified archive")
            self.assertEqual(sorted(p.name for p in context.iterdir()), ["Dockerfile", "typescript.tgz"])
            self.assertEqual((context / "typescript.tgz").read_bytes(), b"verified archive")
            command = setup.build_command("/docker", "unix:///explicit/socket", context, root / "image-id")
            self.assertIn("--platform=linux/arm64", command)
            self.assertIn("--network=none", command)
            self.assertEqual(command[-1], str(context))
            self.assertEqual(command[:3], ["/docker", "--host", "unix:///explicit/socket"])
            self.assertNotIn("--secret", command)
            self.assertNotIn("--ssh", command)

    def test_output_identity_must_match_inspected_platform_label_and_id(self):
        image_id = "sha256:" + "a" * 64
        value = {"Id": image_id, "Architecture": "arm64", "Os": "linux", "Config": {"Labels": {"org.opencontainers.image.title": setup.IMAGE_TITLE}}}
        self.assertEqual(setup.validate_inspected_image(value, image_id), image_id)
        for changed in [{**value, "Id": "sha256:" + "b" * 64}, {**value, "Architecture": "amd64"}, {**value, "Config": {}}, {**value, "Os": "darwin"}]:
            with self.assertRaises(RuntimeError):
                setup.validate_inspected_image(changed, image_id)
        with self.assertRaises(RuntimeError):
            setup.validate_inspected_image(value, "gemma:latest")

    def test_docker_environment_uses_only_temporary_config_and_bundled_plugin(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            env = setup.docker_environment(root)
            self.assertEqual(set(env), {"PATH", "HOME", "DOCKER_CONFIG", "DOCKER_BUILDKIT", "LANG"})
            self.assertEqual(Path(env["HOME"]).parent, root)
            config = json.loads((Path(env["DOCKER_CONFIG"]) / "config.json").read_text())
            self.assertEqual(config, {"cliPluginsExtraDirs": ["/Applications/Docker.app/Contents/Resources/cli-plugins"]})
            self.assertEqual(list(Path(env["HOME"]).iterdir()), [])


if __name__ == "__main__":
    unittest.main()
