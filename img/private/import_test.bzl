"""Analysis tests for importing OCI image indexes."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("@bazel_skylib//rules:write_file.bzl", "write_file")
load("//img/private:import.bzl", "image_import")
load("//img/private/providers:index_info.bzl", "ImageIndexInfo")

_INDEX_DIGEST = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
_PLATFORM_MANIFEST_DIGEST = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
_PLATFORM_CONFIG_DIGEST = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
_ATTESTATION_MANIFEST_DIGEST = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
_ATTESTATION_CONFIG_DIGEST = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

_INDEX = """{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "%s",
      "size": 123,
      "platform": {"os": "linux", "architecture": "amd64"}
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "%s",
      "size": 456,
      "annotations": {"vnd.docker.reference.type": "attestation-manifest"},
      "platform": {"os": "unknown", "architecture": "unknown"}
    }
  ]
}""" % (_PLATFORM_MANIFEST_DIGEST, _ATTESTATION_MANIFEST_DIGEST)

_PLATFORM_MANIFEST = """{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {"digest": "%s"},
  "layers": []
}""" % _PLATFORM_CONFIG_DIGEST

_PLATFORM_CONFIG = """{
  "architecture": "amd64",
  "os": "linux",
  "rootfs": {"type": "layers", "diff_ids": []}
}"""

# Buildx attestations are manifest-shaped but carry no importable rootfs diff IDs.
_ATTESTATION_MANIFEST = """{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {"digest": "%s"},
  "layers": [{"digest": "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "mediaType": "application/vnd.oci.image.layer.v1.tar", "size": 1}]
}""" % _ATTESTATION_CONFIG_DIGEST

_ATTESTATION_CONFIG = "{}"

def _skips_attestation_manifest_impl(ctx):
    env = analysistest.begin(ctx)
    manifests = analysistest.target_under_test(env)[ImageIndexInfo].manifests

    asserts.equals(env, 1, len(manifests))
    asserts.equals(env, "linux", manifests[0].os)
    asserts.equals(env, "amd64", manifests[0].architecture)
    return analysistest.end(env)

_skips_attestation_manifest_test = analysistest.make(_skips_attestation_manifest_impl)

def import_test_suite(name):
    """Declares the image-index import analysis test suite.

    Args:
      name: Name for the wrapping test suite.
    """
    index_file = name + "_index_file"
    platform_manifest_file = name + "_platform_manifest_file"
    platform_config_file = name + "_platform_config_file"
    attestation_manifest_file = name + "_attestation_manifest_file"
    attestation_config_file = name + "_attestation_config_file"

    write_file(
        name = index_file,
        out = name + "_index.json",
        content = [_INDEX],
        tags = ["manual"],
    )
    write_file(
        name = platform_manifest_file,
        out = name + "_platform_manifest.json",
        content = [_PLATFORM_MANIFEST],
        tags = ["manual"],
    )
    write_file(
        name = platform_config_file,
        out = name + "_platform_config.json",
        content = [_PLATFORM_CONFIG],
        tags = ["manual"],
    )
    write_file(
        name = attestation_manifest_file,
        out = name + "_attestation_manifest.json",
        content = [_ATTESTATION_MANIFEST],
        tags = ["manual"],
    )
    write_file(
        name = attestation_config_file,
        out = name + "_attestation_config.json",
        content = [_ATTESTATION_CONFIG],
        tags = ["manual"],
    )

    subject = name + "_subject"
    image_import(
        name = subject,
        data = {
            _INDEX_DIGEST: _INDEX,
            _PLATFORM_MANIFEST_DIGEST: _PLATFORM_MANIFEST,
            _PLATFORM_CONFIG_DIGEST: _PLATFORM_CONFIG,
            _ATTESTATION_MANIFEST_DIGEST: _ATTESTATION_MANIFEST,
            _ATTESTATION_CONFIG_DIGEST: _ATTESTATION_CONFIG,
        },
        digest = _INDEX_DIGEST,
        files = {
            _INDEX_DIGEST: ":" + index_file,
            _PLATFORM_MANIFEST_DIGEST: ":" + platform_manifest_file,
            _PLATFORM_CONFIG_DIGEST: ":" + platform_config_file,
            _ATTESTATION_MANIFEST_DIGEST: ":" + attestation_manifest_file,
            _ATTESTATION_CONFIG_DIGEST: ":" + attestation_config_file,
        },
        registries = ["registry.example.com"],
        repository = "example/image",
        tags = ["manual"],
    )

    test = name + "_skips_attestation_manifest_test"
    _skips_attestation_manifest_test(
        name = test,
        size = "small",
        target_under_test = ":" + subject,
    )

    native.test_suite(
        name = name,
        tests = [":" + test],
    )
