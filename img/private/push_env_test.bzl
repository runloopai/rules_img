"""Analysis tests for image_push environment variables."""

load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts")
load("@bazel_skylib//rules:write_file.bzl", "write_file")
load("//img:image.bzl", "image_manifest")
load("//img:layer.bzl", "image_layer")
load("//img:multi_deploy.bzl", "multi_deploy")
load("//img:push.bzl", "image_push")

_TEST_ENV = {
    "AWS_PROFILE": "test-profile",
    "IMG_INSECURE": "custom-insecure-value",
}
_TEST_INHERITED_ENV = "test-inherited-value"
_TEST_INHERITED_PROFILE = "inherited-profile"
_TEST_REGISTRY_AUTH_FILE = "/tmp/test-docker-config.json"

def _push_env_test_impl(ctx):
    env = analysistest.begin(ctx)
    target = analysistest.target_under_test(env)

    asserts.equals(env, _TEST_ENV["AWS_PROFILE"], target[RunEnvironmentInfo].environment.get("AWS_PROFILE"))
    asserts.equals(env, _TEST_ENV["IMG_INSECURE"], target[RunEnvironmentInfo].environment.get("IMG_INSECURE"))
    asserts.equals(env, _TEST_REGISTRY_AUTH_FILE, target[RunEnvironmentInfo].environment.get("REGISTRY_AUTH_FILE"))
    asserts.false(env, "IMG_INSECURE" in target[RunEnvironmentInfo].inherited_environment)

    actions = [action for action in analysistest.target_actions(env) if action.mnemonic == "PushImage"]
    asserts.true(env, len(actions) > 0, "expected at least one PushImage action")
    for action in actions:
        asserts.equals(env, _TEST_ENV["AWS_PROFILE"], action.env.get("AWS_PROFILE"))
        asserts.equals(env, _TEST_ENV["IMG_INSECURE"], action.env.get("IMG_INSECURE"))
        asserts.equals(env, _TEST_REGISTRY_AUTH_FILE, action.env.get("REGISTRY_AUTH_FILE"))
        asserts.equals(env, _TEST_INHERITED_ENV, action.env.get("RULES_IMG_TEST_INHERITED_ENV"))

    return analysistest.end(env)

_push_env_test = analysistest.make(
    _push_env_test_impl,
    config_settings = {
        # buildifier: disable=canonical-repository
        "@@//img/settings:docker_config_path": _TEST_REGISTRY_AUTH_FILE,
        # buildifier: disable=canonical-repository
        "@@//img/settings:insecure": "enabled",
        "//command_line_option:action_env": [
            "AWS_PROFILE=" + _TEST_INHERITED_PROFILE,
            "RULES_IMG_TEST_INHERITED_ENV=" + _TEST_INHERITED_ENV,
        ],
    },
)

def _multi_deploy_env_test_impl(ctx):
    env = analysistest.begin(ctx)
    run_environment = analysistest.target_under_test(env)[RunEnvironmentInfo]

    asserts.equals(env, _TEST_ENV["AWS_PROFILE"], run_environment.environment.get("AWS_PROFILE"))
    asserts.equals(env, _TEST_ENV["IMG_INSECURE"], run_environment.environment.get("IMG_INSECURE"))
    asserts.false(env, "IMG_INSECURE" in run_environment.inherited_environment)

    return analysistest.end(env)

_multi_deploy_env_test = analysistest.make(_multi_deploy_env_test_impl)

def push_env_test_suite(name):
    """Declare image_push environment analysis tests.

    Args:
        name: Name for the test suite.
    """
    content = name + "_content"
    write_file(
        name = content,
        out = content + ".txt",
        content = ["test content\n"],
        tags = ["manual"],
    )

    layer = name + "_layer"
    image_layer(
        name = layer,
        srcs = {"/test.txt": ":" + content},
        tags = ["manual"],
    )

    image = name + "_image"
    image_manifest(
        name = image,
        layers = [":" + layer],
        tags = ["manual"],
    )

    subject = name + "_subject"
    image_push(
        name = subject,
        env = _TEST_ENV,
        image = ":" + image,
        push_at_build_time = "enabled",
        push_at_build_time_content = "blobs_and_manifests",
        registry = "registry.example.com",
        repository = "example/image",
        tags = ["manual"],
    )

    test = name + "_push_analysis_test"
    _push_env_test(
        name = test,
        size = "small",
        target_under_test = ":" + subject,
    )

    multi_subject = name + "_multi_subject"
    multi_deploy(
        name = multi_subject,
        env = _TEST_ENV,
        operations = [":" + subject],
        tags = ["manual"],
    )

    multi_test = name + "_multi_analysis_test"
    _multi_deploy_env_test(
        name = multi_test,
        size = "small",
        target_under_test = ":" + multi_subject,
    )

    native.test_suite(
        name = name,
        tests = [
            ":" + multi_test,
            ":" + test,
        ],
    )
