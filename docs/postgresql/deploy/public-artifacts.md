---
title: Public PostgreSQL artifacts
parent: Deploy PostgreSQL
nav_order: 0
---

# Public PostgreSQL artifacts

Start with [controller installation](install.md). Use this page for the exact versions, signatures, and checksums of the backend operators, database images, pooler, and backup components. The official Splunk Operator chart includes its backend packages. Download a backend package separately only if you need it outside the packaged chart.

## Artifacts in a release

| Release manifest ID | Kind | Public Docker Hub repository |
| --- | --- | --- |
| `cloudnative-pg` | Image | `splunk/postgresok-cnpg` |
| `postgresql-17` | Image | `splunk/postgresok-postgresql` |
| `postgresql-18` | Image | `splunk/postgresok-postgresql` |
| `pgbouncer` | Image | `splunk/postgresok-pgbouncer` |
| `plugin-barman-cloud` | Image | `splunk/postgresok-plugin-barman-cloud` |
| `plugin-barman-cloud-sidecar` | Image | `splunk/postgresok-plugin-barman-cloud-sidecar` |
| `chart-cloudnative-pg` | Helm chart | `splunk/postgresok-helm-cloudnative-pg` |
| `chart-barman-cloud` | Helm chart | `splunk/postgresok-helm-plugin-barman-cloud` |

All artifacts use Docker Hub, with separate repositories for the components and
`postgresok-helm-` names for the two chart repositories. PostgreSQL 17 and 18
share `splunk/postgresok-postgresql`, with distinct numeric-leading version tags.
This gives seven repositories for eight release artifacts: five image
repositories and two chart repositories.
Use the complete component reference from the release manifest. Do not guess tags or use `latest`.

For example, `docker.io/splunk/postgresok-cnpg` is the familiar CNPG image
reference. `registry-1.docker.io/splunk/postgresok-cnpg` identifies the same
repository through Docker Hub's registry endpoint. The release manifest can use that endpoint.

### Chart package names

The published package names are `postgresok-helm-cloudnative-pg` and
`postgresok-helm-plugin-barman-cloud`. They match the selected repository names,
so standard Helm OCI naming applies. The logical release manifest IDs in the
table identify the corresponding artifacts in the release manifest.

For example, `chart-cloudnative-pg` is the release manifest ID and
`postgresok-helm-cloudnative-pg` is the chart package name. Chart package
versions are independent of PostgreSQL image versions.

## Published release

The November 27, 2026 release is in `releases/postgresok-2026-11-27/`. Include the build identifier `postgresok-4a5880a4-44271065` if you report an issue. The [November 6 manifest](../releases/postgresok-2026-11-06/release-manifest.json) remains available.
It contains six images and two Helm charts. Each image includes `linux/amd64`
and `linux/arm64`.

| Component | Published version tag | Docker Hub |
| --- | --- | --- |
| CNPG | `v1-30-1-splunk.3-ci.e3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-cnpg/tags) |
| PostgreSQL 17 | `17.12-splunk.4-ci.e3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-postgresql/tags) |
| PostgreSQL 18 | `18.1-splunk.4-ci.e3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-postgresql/tags) |
| PgBouncer | `pgbouncer-1-26-0-splunk.4-ci.e3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-pgbouncer/tags) |
| Barman controller | `v0-15-1-splunk.10-ci.e3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-plugin-barman-cloud/tags) |
| Barman sidecar | `v0-15-1-splunk.10-ci.e3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-plugin-barman-cloud-sidecar/tags) |
| CNPG chart | `0.29.1-splunk.1.p44238692.ge3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-helm-cloudnative-pg/tags) |
| Barman chart | `0.8.1-splunk.1.p44238692.ge3f9db841236` | [Tags](https://hub.docker.com/r/splunk/postgresok-helm-plugin-barman-cloud/tags) |

The published image tags include a `-ci.` suffix. Use the complete tags unchanged,
together with the digests in the release manifest.

## Select and verify a release

The public [release manifest](../releases/postgresok-2026-11-27/release-manifest.json),
[production signing public key](../releases/postgresok-2026-11-27/postgresok-production-signing.pub),
and [key fingerprint](../releases/postgresok-2026-11-27/postgresok-production-signing.spki.sha256)
are linked above. Save all three files in the same directory. The manifest covers
all six images and both charts. This public catalog retains artifact versions,
source revisions, public references, digests, and chart package checksums.
It omits internal registry locations and CI evidence links and uses the
`public-release` manifest format, version 1. Its SHA-256 is:

```text
17a9be298863578c2b3fa93147009f00c8b3b13f4cce6865d61b72a8ee49b9d6
```

From that directory, check the manifest against the SHA-256 above:

```bash
set -euo pipefail
export RELEASE_MANIFEST="$PWD/release-manifest.json"
MANIFEST_SHA256="$(shasum -a 256 "$RELEASE_MANIFEST" | awk '{print $1}')"
test "$MANIFEST_SHA256" = \
  "17a9be298863578c2b3fa93147009f00c8b3b13f4cce6865d61b72a8ee49b9d6"
```

The checksum detects changes to the file. It does not identify the publisher or replace signature verification. Use the manifest's `public` references to download artifacts. You need only public registry access and the verification files above.

Before using a release:

1. Download the `release-manifest.json` linked above.
   It must cover all six images and both charts. For each artifact, inspect its
   `id`, `version`, `sources`, and `public.reference` / `public.digest` fields.
   Chart entries also provide `package_sha256`.
2. Verify the signatures for each public image and chart digest using the
   published verification instructions and trusted signing identity or trust
   material. Confirm that the signature is valid for the exact selected digest.
3. Download the charts from their manifest references and compare each package's
   SHA-256 with `package_sha256`. Check that the OCI manifest digest matches the
   recorded digest as well.
4. Retain the verified manifest and use its complete `tag@sha256` image references
   when configuring workloads.

### Verify all image and chart signatures

Use [Cosign v3.1.4](https://github.com/sigstore/cosign/releases/tag/v3.1.4),
OpenSSL, `jq`, and Bash for the commands below. Cosign's official release page
provides binaries and checksums for supported platforms. These commands were
tested with Cosign v3.1.4 on macOS ARM64, using an empty Docker credential
configuration. No Docker daemon, Kubernetes cluster, or internal service access
is needed.

The expected signing identity is the Splunk production release key
supplied above. Its fingerprint is the SHA-256 of its DER-encoded
SubjectPublicKeyInfo, not the checksum of its PEM text:

```text
e95c18590701d61869f9e9c1ae1e7d2921a2c108e6c840a0b8e3ed07356392fa
```

Obtain the key and this fingerprint from the official Splunk documentation or
repository. A key supplied by an untrusted artifact source does not establish
the publisher's identity. This key is pinned for the release documented here;
future key rotation must include a new published key and fingerprint.

Check the key before verifying artifacts:

```bash
set -euo pipefail
: "${RELEASE_MANIFEST:?Set the path to the release-manifest.json}"
export SIGNING_PUBLIC_KEY="$(dirname "$RELEASE_MANIFEST")/postgresok-production-signing.pub"

openssl pkey -pubin -in "$SIGNING_PUBLIC_KEY" -outform DER > signing-key.der
SIGNING_KEY_SHA256="$(shasum -a 256 signing-key.der | awk '{print $1}')"
test "$SIGNING_KEY_SHA256" = \
  "e95c18590701d61869f9e9c1ae1e7d2921a2c108e6c840a0b8e3ed07356392fa"
rm signing-key.der
```

Each public Docker Hub repository stores its Cosign signatures under `sha256-<artifact-digest>.sig`. The release does not include a transparency-log bundle. Use `--insecure-ignore-tlog=true` for this signing format. Cosign still checks cryptographic signatures and digest claims. It does not check the transparency log or a trusted signing timestamp. Cosign prints a warning about that
limitation. See [Cosign verification](https://docs.sigstore.dev/cosign/verifying/verify/)
for its verification model.

Verify every image and chart from the same manifest:

```bash
set -euo pipefail
: "${RELEASE_MANIFEST:?Set the path to the release-manifest.json}"
: "${SIGNING_PUBLIC_KEY:?Check and set the production public key first}"

jq -er '
  .artifacts[]
  | .public.reference
  | sub(":[^/:@]+@"; "@")
' "$RELEASE_MANIFEST" > release-public-references.txt

mkdir -p signature-verification
while IFS= read -r reference; do
  digest="${reference##*@sha256:}"
  cosign verify --key "$SIGNING_PUBLIC_KEY" \
    --check-claims=true --insecure-ignore-tlog=true \
    "$reference" > "signature-verification/$digest.json"
done < release-public-references.txt
```

The loop stops with a nonzero status if any artifact fails verification. On
success, each output contains the validated signature claims, including
`critical.image.docker-manifest-digest`. Retain these records with the manifest.
The public chart OCI digests use the same verification procedure as images;
the chart `.tgz` byte checks below are an additional check.

The published artifacts retain the qualified image index digests and chart
package bytes.

## Pull a selected image

After obtaining and verifying the public release manifest, set `RELEASE_MANIFEST`
to its local path. The examples below require `jq` in addition to Docker or Helm.
The chart commands were checked with Helm 4.2.3 and use OCI digest references;
see [Helm's OCI registry documentation](https://helm.sh/docs/topics/registries/).
The checksum examples use `shasum`. This example pulls the exact PostgreSQL 17
image, including its recorded tag and digest, without deploying a database:

```bash
: "${RELEASE_MANIFEST:?Set the path to the verified release-manifest.json}"

POSTGRESQL17_IMAGE="$(jq -er '
  .artifacts[]
  | select(.id == "postgresql-17" and .kind == "image")
  | .public.reference
' "$RELEASE_MANIFEST")" || exit 1
docker pull "$POSTGRESQL17_IMAGE"
```

Use the corresponding manifest ID from the table for another component. Image
availability does not establish a complete release: obtain all required image
and chart references from the same completed release manifest.

## Download the selected chart packages

These commands use the selected public repositories and exact chart OCI digests.
Read each chart's `public.digest` from the verified release manifest, including
its `sha256:` prefix:

```bash
: "${RELEASE_MANIFEST:?Set the path to the verified release-manifest.json}"

CNPG_CHART_DIGEST="$(jq -er '
  .artifacts[]
  | select(.id == "chart-cloudnative-pg" and .kind == "chart")
  | .public.digest
' "$RELEASE_MANIFEST")" || exit 1
BARMAN_CHART_DIGEST="$(jq -er '
  .artifacts[]
  | select(.id == "chart-barman-cloud" and .kind == "chart")
  | .public.digest
' "$RELEASE_MANIFEST")" || exit 1

mkdir -p charts/cnpg charts/barman
helm pull "oci://registry-1.docker.io/splunk/postgresok-helm-cloudnative-pg@$CNPG_CHART_DIGEST" \
  --destination ./charts/cnpg
helm pull "oci://registry-1.docker.io/splunk/postgresok-helm-plugin-barman-cloud@$BARMAN_CHART_DIGEST" \
  --destination ./charts/barman

CNPG_CHART_PACKAGES=(./charts/cnpg/*.tgz)
BARMAN_CHART_PACKAGES=(./charts/barman/*.tgz)
[ "${#CNPG_CHART_PACKAGES[@]}" -eq 1 ] && [ -f "${CNPG_CHART_PACKAGES[0]}" ] || exit 1
[ "${#BARMAN_CHART_PACKAGES[@]}" -eq 1 ] && [ -f "${BARMAN_CHART_PACKAGES[0]}" ] || exit 1
export CNPG_CHART_PACKAGE="${CNPG_CHART_PACKAGES[0]}"
export BARMAN_CHART_PACKAGE="${BARMAN_CHART_PACKAGES[0]}"
```

Use a separate chart directory for each release, with one downloaded package in
each subdirectory. Helm's output filenames can depend on whether a tag or digest
was used; the commands select the actual downloaded files. Check their exact
bytes against the manifest:

```bash
set -euo pipefail
jq -er --arg cnpg "$CNPG_CHART_PACKAGE" --arg barman "$BARMAN_CHART_PACKAGE" '
  .artifacts[]
  | select(.kind == "chart")
  | (if .id == "chart-cloudnative-pg" then $cnpg
     elif .id == "chart-barman-cloud" then $barman
     else error("Unexpected chart in release manifest") end) as $path
  | "\(.package_sha256)  \($path)"
' "$RELEASE_MANIFEST" | shasum -a 256 -c -
```

The chart manifest digest identifies the OCI artifact. The `package_sha256` field identifies the downloaded `.tgz` file. These sums check the bytes, not the publisher. Use signature verification to check who published the chart.

## Continue the deployment

If your Helm values are ready, [apply the Helm configuration](install.md#apply-the-helm-configuration). If you are preparing a mirror, first add the [mirror image references and pull Secrets](install.md#use-a-mirror) to those values.

If your controllers are ready, return to [enable shared PostgreSQL](provision.md#enable-the-shared-helm-cluster). To use a mirror, keep the numeric-leading PostgreSQL tags and full digests in `sharedPostgres.images`.
