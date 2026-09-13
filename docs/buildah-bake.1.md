% buildah-bake 1

## NAME
buildah-bake - Build images from a Bake definition

## SYNOPSIS
**buildah bake** [*options*] [*target* ...]

## DESCRIPTION

Resolve a Docker Bake definition and build the selected targets using Buildah.
Buildx's Bake resolver handles HCL, JSON and Compose files, variables,
inheritance, groups, matrices and overrides. No Docker or BuildKit daemon is
required. With no targets specified, the **default** group or target is selected.

Targets are built sequentially in alphabetical order, stopping at the first
failure. All selected targets are checked for unsupported settings, and their
build options and local Dockerfile paths are checked, before any image is built.
This does not guarantee that every Dockerfile instruction will succeed. Images
from targets completed before a build failure remain in local storage.

Supported target settings are **context**, **dockerfile**, **args**, **labels**,
**tags**, **target**, **pull**, and **no-cache**. The context must be a local
directory; relative contexts are resolved from the current working directory.
Relative Dockerfile paths are resolved from the target's context. Bake defaults
to **Dockerfile**, even when a Containerfile is present. Null arguments and
labels are omitted; empty string values are preserved. Images are stored locally.
With **pull=true**, base images are always pulled; **pull=false** uses the
**missing** pull policy. Other build defaults follow **buildah-build(1)**.

Other execution settings are currently rejected, including platforms, named
contexts and **target:** dependencies, cache imports/exports, output exporters,
secrets, SSH forwarding, network settings, entitlements, attestations, and inline
Dockerfiles. Remote Bake files and remote build contexts are not supported.
This command does not provide BuildKit's cross-target work deduplication.

## OPTIONS

#### **--file**, **-f**=*file*

Read a Bake definition. May be repeated to merge files in the order supplied.
Use **-** to read a definition from standard input. Without this option, use
Buildx's discovery of Compose files, **docker-bake.json**, **docker-bake.hcl**,
and their **docker-bake.override.json** and **docker-bake.override.hcl** overrides.

#### **--print**

Print the resolved definition as JSON without building images. This operation
does not open image storage or enter a user namespace. It also prints settings
which are not yet supported for execution, allowing definitions to be inspected
before contexts exist locally.

#### **--set**=*target.key=value*

Override a target setting. May be repeated. Target patterns and append syntax
are interpreted by Buildx, for example **--set '*.args.VERSION=1.0'**.
Bake variables can also be supplied through environment variables.

## EXAMPLE

Given this **docker-bake.hcl**:

```hcl
variable "VERSION" {
  default = "dev"
}
group "default" {
  targets = ["api", "worker"]
}
target "base" {
  context = "."
  dockerfile = "Containerfile"
}
target "api" {
  inherits = ["base"]
  target = "api"
  tags = ["localhost/api:${VERSION}"]
}
target "worker" {
  inherits = ["base"]
  target = "worker"
  tags = ["localhost/worker:${VERSION}"]
}
```

Build both targets:

```
VERSION=1.0 buildah bake
```

Inspect one target or override a build argument:

```
buildah bake --print api
buildah bake --set 'api.args.MODE=release' api
```

## SEE ALSO
buildah(1), buildah-build(1)
