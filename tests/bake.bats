#!/usr/bin/env bats

load helpers

@test "bake builds selected targets with inherited arguments and tags" {
    local contextdir="$TEST_SCRATCH_DIR/bake"
    mkdir -p "$contextdir"
    cat > "$contextdir/Dockerfile" <<'EOF'
FROM scratch AS api
ARG VERSION=default
LABEL version=$VERSION role=api
FROM scratch AS worker
LABEL role=worker
EOF
    cat > "$contextdir/docker-bake.hcl" <<EOF
group "default" { targets = ["api", "worker"] }
target "base" {
  context = "$contextdir"
  args = { VERSION = "inherited" }
}
target "api" {
  inherits = ["base"]
  target = "api"
  tags = ["localhost/bake-api:test", "localhost/bake-api:alias"]
}
target "worker" {
  inherits = ["base"]
  target = "worker"
  tags = ["localhost/bake-worker:test"]
}
EOF
    run_buildah bake -f "$contextdir/docker-bake.hcl" --set api.args.VERSION=override
    run_buildah inspect --type image --format '{{index .Docker.Config.Labels "version"}}' localhost/bake-api:test
    expect_output "override"
    run_buildah inspect --type image --format '{{index .Docker.Config.Labels "role"}}' localhost/bake-worker:test
    expect_output "worker"
    run_buildah images -q localhost/bake-api:test
    local imageid="$output"
    run_buildah images -q localhost/bake-api:alias
    expect_output "$imageid"
}

@test "bake rejects unsupported targets before building any images" {
    local contextdir="$TEST_SCRATCH_DIR/bake"
    mkdir -p "$contextdir"
    printf 'FROM scratch\n' > "$contextdir/Dockerfile"
    cat > "$contextdir/docker-bake.hcl" <<EOF
group "default" { targets = ["a", "z"] }
target "a" {
  context = "$contextdir"
  tags = ["localhost/bake-not-built:test"]
}
target "z" { platforms = ["linux/arm64"] }
EOF
    run_buildah 125 bake -f "$contextdir/docker-bake.hcl"
    expect_output --substring 'bake target "z": unsupported setting "platforms"'
    run_buildah images -q localhost/bake-not-built:test
    expect_output ""
}

@test "bake print resolves unsupported settings without building" {
    local bakefile="$TEST_SCRATCH_DIR/docker-bake.hcl"
    printf 'target "default" { platforms = ["linux/arm64"] }\n' > "$bakefile"
    run_buildah bake -f "$bakefile" --print
    expect_output --substring '"linux/arm64"'
}
