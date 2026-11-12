---
title: Public PostgreSQL artifacts
parent: PostgreSQL
nav_order: 0.25
---

# Public PostgreSQL artifacts

The PostgreSQL artifact release contains the backend operators, database images,
connection pooler, and backup components used with the Postgres Operator.
Use this guide to download the public images and charts and verify their
signatures and checksums.
The Splunk Operator itself is distributed separately; see the
[Postgres Operator quickstart](quickstart.md) for its installation, prerequisites,
and feature-gate configuration.

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
Use the complete reference for the required component from the release manifest;
do not infer tags or use `latest`.

For example, `docker.io/splunk/postgresok-cnpg` is the familiar CNPG image
reference. `registry-1.docker.io/splunk/postgresok-cnpg` identifies the same
repository through Docker Hub's registry endpoint. Release manifest references
may use that endpoint.

### Chart package names

The published package names are `postgresok-helm-cloudnative-pg` and
`postgresok-helm-plugin-barman-cloud`. They match the selected repository names,
so standard Helm OCI naming applies. The logical release manifest IDs in the
table identify the corresponding artifacts in the release manifest.

For example, `chart-cloudnative-pg` is the release manifest ID and
`postgresok-helm-cloudnative-pg` is the chart package name. Chart package
versions are independent of PostgreSQL image versions.

## Published release

The **November 6, 2026 release** is available under `releases/postgresok-2026-11-06/`.
Its build release identifier is `postgresok-ac78e560-43371564`; retain that
identifier when reporting an issue.
It contains six images and two Helm charts. Each image includes `linux/amd64`
and `linux/arm64`.

| Component | Published version tag | Docker Hub |
| --- | --- | --- |
| CNPG | `v1-30-1-splunk.1-ci.a1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-cnpg/tags) |
| PostgreSQL 17 | `17.10-splunk.1-ci.a1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-postgresql/tags) |
| PostgreSQL 18 | `18.1-splunk.1-ci.a1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-postgresql/tags) |
| PgBouncer | `pgbouncer-1-26-0-splunk.1-ci.a1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-pgbouncer/tags) |
| Barman controller | `v0-15-1-splunk.1-ci.a1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-plugin-barman-cloud/tags) |
| Barman sidecar | `v0-15-1-splunk.1-ci.a1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-plugin-barman-cloud-sidecar/tags) |
| CNPG chart | `0.29.1-splunk.1.p43368666.ga1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-helm-cloudnative-pg/tags) |
| Barman chart | `0.8.1-splunk.1.p43368666.ga1b7ebc9e8ad` | [Tags](https://hub.docker.com/r/splunk/postgresok-helm-plugin-barman-cloud/tags) |

The published image tags include a `-ci.` suffix. Use the complete tags unchanged,
together with the digests in the release manifest.

## Select and verify a release

The public [release manifest](releases/postgresok-2026-11-06/release-manifest.json),
[production signing public key](releases/postgresok-2026-11-06/postgresok-production-signing.pub),
and [key fingerprint](releases/postgresok-2026-11-06/postgresok-production-signing.spki.sha256)
are linked above. Save all three files in the same directory. The manifest covers
all six images and both charts. This public catalog retains artifact versions,
source revisions, public references, digests, and chart package checksums.
It omits internal registry locations and CI evidence links and uses the
`public-release` manifest format, version 1. Its SHA-256 is:

```text
d5926a0cc4cbc9c5ca30aab5879a22ab2d29743b216a53c85aac4f3ac1f72840
```

From that directory, check the manifest against the SHA-256 above:

```bash
set -euo pipefail
export RELEASE_MANIFEST="$PWD/release-manifest.json"
MANIFEST_SHA256="$(shasum -a 256 "$RELEASE_MANIFEST" | awk '{print $1}')"
test "$MANIFEST_SHA256" = \
  "d5926a0cc4cbc9c5ca30aab5879a22ab2d29743b216a53c85aac4f3ac1f72840"
```

The checksum detects changed bytes; it does not authenticate the publisher or
replace signature verification. Use the manifest's `public` references to
download artifacts. Downloading and verifying these artifacts requires only
public registry access and the verification material linked above.

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

The release uses Cosign signatures stored in each public Docker Hub repository
under `sha256-<artifact-digest>.sig`. It does not include a transparency-log
bundle. The command therefore uses `--insecure-ignore-tlog=true` specifically
for this signing format. Cryptographic signature verification and digest-claim
validation remain enabled; this procedure does not verify transparency-log
inclusion or a trusted signing timestamp. Cosign prints a warning about that
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

A chart manifest digest identifies the OCI artifact, while `package_sha256`
identifies the downloaded `.tgz` bytes; they are different checksums. These
checks establish byte integrity, not the publisher's signing identity.

## Use with Splunk Operator

Install CNPG and Barman through the
[Splunk Operator Helm chart](../deploy/Helm.md#postgresql-backend-controllers).
Those instructions cover prerequisites, feature gates, and existing backends.

The backend chart packages preserve their qualified image defaults. The SOK
chart integration must configure their public controller and Barman sidecar image
references from the same release manifest.

For an existing Postgres Operator installation, configure database and pooler
images on `PostgresCluster.spec.images` using the public PostgreSQL 17 or 18 and
PgBouncer references from the same manifest; see
[Create a PostgreSQL cluster](quickstart.md#3-create-a-postgresql-cluster).
Preserve numeric-leading PostgreSQL tags and the complete digest. There is no
automatic image discovery or fallback. Use `PostgresClusterClass`,
`PostgresCluster`, and `PostgresDatabase` for database provisioning.

For object-storage credentials and backup configuration, see
[Automated backups via object storage](backup-object-storage.md).
