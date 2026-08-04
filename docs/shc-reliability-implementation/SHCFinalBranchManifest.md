# SHC reliability final branch manifest

This manifest records how the Search Head Cluster reliability work was
assembled for final review. It distinguishes accepted branch history from
older experimental tips that must not be merged merely to make every ref an
ancestor. A branch is accepted when its exact tip is an ancestor of the final
branch, its change is patch-equivalent to an accepted commit, or a later
qualified implementation explicitly supersedes it.

## Final review branches

| Repository | Final branch | Frozen source before this manifest | Status |
| --- | --- | --- | --- |
| Splunk Operator | `feature/shc-kubernetes-reliability` | `9aaab1ec2f50ff51134032ebafa9211597019049` | Accepted production, qualification, and evidence history through SHC-118 is assembled and repository gates pass. |
| Docker-Splunk | `feature/shc-kubernetes-reliability` | `0604eeb293832f9c0c19a24d7de8706b63c65031` | Accepted runtime history through SHC-104 and SHC-98 is already cumulative. |
| Splunk Ansible | `feature/shc-kubernetes-reliability` | `9dff0999c93fd129d31ba08609423ac2bd600aeb` | This is the exact source pinned by the final Docker-Splunk branch. |

The Operator hash above is the assembled source, test, and evidence tip before
this manifest was committed. The pushed branch ref is authoritative for the
final documentation commit.

## Operator production ancestry

The cumulative SHC-118 production tip
`8152fc042e1da814cc37238b7a9eb4cf22b76222` was fast-forwarded into the final
branch. The following accepted source tips are all ancestors of the final
branch:

| Work item | Accepted branch tip |
| --- | --- |
| SHC-105 App Framework requeue boundary | `0e638dac45458519e7daae10235527b85af1be6f` |
| SHC-106 Deployer/member coordination | `a6cda92a393174d08c6c1199b45128cb058837dc` |
| SHC-112 indexer search-peer advancement gate | `79f751075a00d3d14188c7709f5a404032793c7e` |
| SHC-113 REST response ownership | `c700a077e1b7beba00556733a5d35a57f2deab6d` |
| SHC-114 bounded peer observation | `5440b8c2e6ceafc38bcd1c647317d27eebf295fd` |
| SHC-115 short-lived REST transport ownership | `cd3498393d2801d8498ca3dc9a2e20e4c30edcf8` |
| SHC-116 indexer endpoint withdrawal | `96c83dcadc25e6034ba2a41898c84ed1b255b570` |
| SHC-118 Search Head endpoint withdrawal | `8152fc042e1da814cc37238b7a9eb4cf22b76222` |

Earlier accepted SHC work through SHC-104 was already contained by the prior
final feature tip and remains in ancestry.

## Operator qualification and evidence ancestry

The following exact qualification tips are ancestors of the final branch:

| Scope | Accepted branch tip | Integration commit |
| --- | --- | --- |
| SHC-106 Deployer observation | `0e9cb18ee113451a2308c24a6c95df87cf274748` | `1dc82fadb` |
| SHC-107 persistent client and SHC-111 protocols | `de6f5f8e3dc85e8200a312c7255e66cc666408a6` | `a1bd7a884` |
| SHC-117 extended indexer roll | `cd522e119ef7113b4605c42e5a9624febce3ca49` | `3a8dfd981` |
| SHC-118 Search Head endpoint withdrawal | `7363f71a90a026b3137c333020422968f6453c8c` | `0e9057621` |

The SHC-107 tip `3e9f47751e439f7a1de49633616ef995f950f111`
is itself an ancestor of the SHC-111 qualification tip.

The cumulative documentation tip
`fd2afd72c63258ebd43b2d1f1d9a796730f6fd67` was integrated at
`ada4b7d0c`. The final branch also contains these exact evidence tips:

| Evidence | Accepted tip |
| --- | --- |
| SHC-105 | `d8d5eba9ce95031c478988478a6b36c2f397af13` |
| SHC-106 | `270486c1ddc62e01b4263b6ade9bbf9a195dcb96` |
| SHC-107 | `d3c82c2a1823ccc5dee7d5c1015b8ee1110a76ad` |
| SHC-108 | `58405bd6df7676973faaac4b27b18524562b5292` |
| SHC-109 | `6b10b820b22c3c32c59345f0450803f7b9dbbc02` |
| SHC-110 | `558af538d6964ce5cc6f576d1bfff2dff098e639` |
| SHC-111 | `77819783dfc928f6f1f71a814e66c9d3d5066f97` |
| SHC-112 | `056ffd004edec1ab8789f4f44004610b23c34be6` |
| SHC-113 | `2ac869dfd879bb61190faff425a24cd7d7e539a1` |
| SHC-114 | `3cc71cb3236a75ba4113cc71cb797c082ff6f134` |
| SHC-115 | `4268ccd5d5cc68530adee811e26b78298b12b787` |
| SHC-116 | `e8d63b9fe69c3539eee8273fb7f81ce12785cbb6` |
| SHC-117 | `7707628d50187733e8011d157007bc29bcb1e7ba` |
| SHC-118 | `fd2afd72c63258ebd43b2d1f1d9a796730f6fd67` |

## Non-ancestor branch dispositions

An audit of every local and fetched `codex/shc-*` Operator tip found only the
following older tips outside final ancestry. They are intentionally not
merged because doing so would reintroduce an older history or obsolete
documents without adding accepted behavior.

| Branch tip | Disposition |
| --- | --- |
| `43de58f865c755a6ff585e62e63a11e67bdc9df4` (SHC-58) | Patch-equivalent accepted change is already in the cumulative lifecycle history. |
| `8ba676b1d01520b781e5d3a7b888984e174d076b` (SHC-59) | Patch-equivalent accepted change is already in the cumulative lifecycle history. |
| `0e3864f1e571a026ac4ae6e78313452d22aa6977` (SHC-60) | Patch-equivalent accepted change is already in the cumulative lifecycle history. |
| `207d0958b6555257adf3d90f41e999b1b692d91c` (SHC-82 indexer evidence) | Patch-equivalent evidence is already present in the final documents. |
| `d61d2480561aa02fa8132f25db8665a28fa2b229` (SHC-85 ready replacement) | Its `578447335` lifecycle-hold implementation is superseded by the later qualified cumulative implementation at `5dbe7dac8`; its evidence was expanded in later records. |
| `d4dd4d70b19bc261bb48a884b7aaa12d2842fcea` (SHC-85 withdrawal evidence) | Patch-equivalent evidence is already present in the final documents. |
| `4b6c6480bf8c9bafc85f0d62bb60d6d95793fb31` (SHC-99 exact process match) | Production commit `184061106` is patch-equivalent in the final branch; the branch-tip plan is older than the accepted expanded SHC-99 record. |

For Docker-Splunk, `codex/shc-runtime-container-qualification` at
`648028591529908c40739adeebf3ad9c7dc8b088`,
`codex/shc-98-stable-indexer-search-address` at
`6ee266c14e25a1d5849a3d5b96cdaf155b09c696`, and
`codex/shc-104-docker-test-bootstrap` at
`0604eeb293832f9c0c19a24d7de8706b63c65031` are all ancestors of the final
runtime branch. The older `spike/shc-runtime-lifecycle` and
`codex/shc-84-startup-term-qualification` tips are not merged: their shutdown
changes are already represented, their Python-key and Ansible-pin changes are
superseded by the verified final variants, and their documentation predates
the accepted runtime qualification.

## Reproducible validation

The assembled Operator source at `9aaab1ec2` passed:

- `make fmt manifests generate`, leaving the generated tree clean;
- `make vet build`;
- `make test`: 43 Ginkgo suites passed with 78.8 percent composite coverage;
- `make helm-check`: chart lint passed, with 60 Operator and 90 Universal
  Forwarder tests passing;
- `make shc82-monitor-check shc98-monitor-check
  shc107-persistent-client-check shc118-monitor-check`: shell validation,
  19 SHC-107/111 client tests, and Kubernetes manifest dry-run passed; and
- `git diff --check`.

These gates validate the cumulative source and tests. They do not replace the
separately recorded immutable-image EKS campaigns or turn explicitly open
Splunk Enterprise boundaries into completed claims.

The final Docker-Splunk source at `0604eeb` also passed its repository-owned
bounded gates: 15 shutdown tests, four exact-Ansible-ref tests, one base-image
key-verification test, five deterministic test-bootstrap tests, shell syntax,
ShellCheck for the owned shutdown helper, and `git diff --check`. The exact
Splunk Ansible source consumed by that branch, `9dff0999`, passed
`make shc-check`: focused Ansible lint and playbook syntax, 62 clustering
environment tests, seven task-rendering tests, eight executable behavior
tests, and two bounded startup tests.
