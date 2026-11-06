---
title: Copyright and License Notices
parent: Develop & Contribute
nav_order: 10
---

# Copyright and License Notices

Use this page as the reference when adding or updating copyright and license
notices. It provides the repository's block-comment template for new
hand-written Go source and rules for preserving existing attribution. It does
not authorize changing existing headers as part of unrelated work.

## Hand-written Go source template

Copy this form for new SOK-authored Go files that use the repository's block
header. Replace `<year>` with the year the file is first authored. Use the
adjacent file's established notice style for other source types.

```go
/*
Copyright <year>.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
```

For an existing file, preserve its first-authored year and attribution. Extend
a year range only when the adjacent header uses a range and authored changes
are made in a later year. Do not replace the original owner or claim
authorship of upstream code.

## Generated files

Generated Go headers are sourced from
[`hack/boilerplate.go.txt`](../../hack/boilerplate.go.txt). Update generated
headers through the owning generation target (`make generate`), not by editing
generated files directly. Keep the template consistent with the intended
repository policy. Changing it affects future generated output.

## Third-party and project-wide notices

- Preserve upstream copyright and license notices in vendored, copied, or
  adapted code. Follow the upstream license terms for modifications.
- Do not infer attribution or license requirements from `go.mod` alone.
- Update `LICENSE` or third-party credit files only from a complete, reviewed
  license inventory.
- If ownership, attribution, or redistribution obligations are unclear, ask
  the repository owner rather than guessing.

## Existing headers

Do not perform broad copyright-year or attribution edits during unrelated
changes. When changing an existing file, preserve its established notice and
make only a specifically justified correction. If a correction is needed,
review its provenance and affected files with the repository owner.
