#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import shutil
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def utc_now() -> str:
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def bool_env(name: str, default: bool = False) -> bool:
    value = os.getenv(name, "").strip().lower()
    if not value:
        return default
    return value in {"1", "true", "yes", "on"}


def csv_ints(name: str) -> list[int]:
    raw = os.getenv(name, "")
    rows: list[int] = []
    for item in raw.split(","):
        cleaned = item.strip()
        if not cleaned:
            continue
        rows.append(int(cleaned))
    return rows


def gitlab_api_headers() -> dict[str, str]:
    private_token = os.getenv("PIPELINE_GITLAB_API_TOKEN", "").strip()
    if private_token:
        return {"PRIVATE-TOKEN": private_token}
    job_token = os.getenv("CI_JOB_TOKEN", "").strip()
    if job_token:
        return {"JOB-TOKEN": job_token}
    raise RuntimeError("Missing PIPELINE_GITLAB_API_TOKEN or CI_JOB_TOKEN for GitLab API access")


def gitlab_remote_url() -> str:
    host = os.getenv("CI_SERVER_URL", "https://cd.splunkdev.com").rstrip("/")
    project_path = os.environ["CI_PROJECT_PATH"]
    private_token = os.getenv("PIPELINE_GITLAB_API_TOKEN", "").strip()
    if private_token:
        return f"https://oauth2:{private_token}@{host.removeprefix('https://').removeprefix('http://')}/{project_path}.git"
    job_token = os.getenv("CI_JOB_TOKEN", "").strip()
    if job_token:
        return f"https://gitlab-ci-token:{job_token}@{host.removeprefix('https://').removeprefix('http://')}/{project_path}.git"
    raise RuntimeError("Missing PIPELINE_GITLAB_API_TOKEN or CI_JOB_TOKEN for Git remote access")


def github_headers() -> dict[str, str]:
    headers = {
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": "sok-gitlab-github-intake-backfill",
    }
    token = os.getenv("PIPELINE_GITHUB_INTAKE_TOKEN", "").strip()
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def api_request_json(method: str, url: str, headers: dict[str, str], payload: dict | None = None) -> dict | list:
    data = None
    request_headers = dict(headers)
    if payload is not None:
        request_headers["Content-Type"] = "application/json"
        data = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(url, method=method, headers=request_headers, data=data)
    with urllib.request.urlopen(request) as response:
        body = response.read()
        if not body:
            return {}
        return json.loads(body)


def gitlab_api_url(path: str, params: dict[str, Any] | None = None) -> str:
    api = os.getenv("CI_API_V4_URL", "https://cd.splunkdev.com/api/v4").rstrip("/")
    project_id = os.environ["CI_PROJECT_ID"]
    query = ""
    if params:
        query = "?" + urllib.parse.urlencode(params, doseq=True)
    return f"{api}/projects/{project_id}{path}{query}"


def github_api_url(path: str) -> str:
    api = os.getenv("PIPELINE_GITHUB_API_URL", "https://api.github.com").rstrip("/")
    return f"{api}{path}"


def paged_gitlab_get(path: str, params: dict[str, Any] | None = None) -> list[dict[str, Any]]:
    headers = gitlab_api_headers()
    page = 1
    rows: list[dict[str, Any]] = []
    base_params = dict(params or {})
    while True:
        query = {**base_params, "per_page": 100, "page": page}
        batch = api_request_json("GET", gitlab_api_url(path, query), headers)
        if not isinstance(batch, list) or not batch:
            break
        rows.extend(batch)
        if len(batch) < 100:
            break
        page += 1
    return rows


def gitlab_post(path: str, payload: dict[str, Any]) -> dict[str, Any]:
    response = api_request_json("POST", gitlab_api_url(path), gitlab_api_headers(), payload)
    if not isinstance(response, dict):
        raise RuntimeError(f"Unexpected GitLab response for POST {path}")
    return response


def gitlab_try_get(path: str) -> dict[str, Any] | None:
    try:
        response = api_request_json("GET", gitlab_api_url(path), gitlab_api_headers())
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return None
        raise
    if not isinstance(response, dict):
        raise RuntimeError(f"Unexpected GitLab response for GET {path}")
    return response


def gh_issue(repo: str, number: int) -> dict[str, Any]:
    response = api_request_json("GET", github_api_url(f"/repos/{repo}/issues/{number}"), github_headers())
    if not isinstance(response, dict):
        raise RuntimeError(f"Unexpected GitHub issue response for #{number}")
    return response


def gh_pull(repo: str, number: int) -> dict[str, Any]:
    response = api_request_json("GET", github_api_url(f"/repos/{repo}/pulls/{number}"), github_headers())
    if not isinstance(response, dict):
        raise RuntimeError(f"Unexpected GitHub pull response for #{number}")
    return response


def paged_github_get(path: str, params: dict[str, Any] | None = None) -> list[dict[str, Any]]:
    headers = github_headers()
    page = 1
    rows: list[dict[str, Any]] = []
    base_params = dict(params or {})
    while True:
        query = {**base_params, "per_page": 100, "page": page}
        response = api_request_json("GET", github_api_url(path) + "?" + urllib.parse.urlencode(query, doseq=True), headers)
        if not isinstance(response, list) or not response:
            break
        rows.extend(response)
        if len(response) < 100:
            break
        page += 1
    return rows


def marker_issue(repo: str, number: int) -> str:
    return f"github-intake:issue:{repo}#{number}"


def marker_pr(repo: str, number: int) -> str:
    return f"github-intake:pr:{repo}#{number}"


def find_issue_by_marker(marker: str) -> dict[str, Any] | None:
    rows = paged_gitlab_get("/issues", {"state": "all", "search": marker})
    for row in rows:
        if marker in (row.get("description") or ""):
            return row
    return None


def find_mr_by_marker(marker: str) -> dict[str, Any] | None:
    rows = paged_gitlab_get("/merge_requests", {"state": "all", "search": marker})
    for row in rows:
        if marker in (row.get("description") or ""):
            return row
    return None


def branch_exists(branch: str) -> bool:
    encoded = urllib.parse.quote_plus(branch)
    return gitlab_try_get(f"/repository/branches/{encoded}") is not None


def ensure_clone(repo: str) -> tuple[Path, bool]:
    tempdir = Path(tempfile.mkdtemp(prefix="gitlab-github-intake-"))
    subprocess.run(["git", "clone", "--quiet", f"https://github.com/{repo}.git", str(tempdir)], check=True)
    subprocess.run(["git", "remote", "add", "gitlab", gitlab_remote_url()], cwd=tempdir, check=True)
    return tempdir, True


def push_branch(repo_dir: Path, branch: str) -> str:
    remote_ref = f"refs/heads/{branch}:refs/remotes/origin/{branch}"
    subprocess.run(["git", "fetch", "--quiet", "origin", remote_ref], cwd=repo_dir, check=True)
    subprocess.run(["git", "push", "gitlab", f"refs/remotes/origin/{branch}:refs/heads/{branch}"], cwd=repo_dir, check=True)
    result = subprocess.run(
        ["git", "rev-parse", f"refs/remotes/origin/{branch}"],
        cwd=repo_dir,
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout.strip()


def issue_description(repo: str, issue: dict[str, Any]) -> str:
    labels = ", ".join(label["name"] for label in issue.get("labels", [])) or "-"
    body = issue.get("body") or "_No body provided on GitHub._"
    marker = marker_issue(repo, int(issue["number"]))
    return "\n".join(
        [
            f"<!-- {marker} -->",
            f"Backfilled from GitHub issue #{issue['number']}",
            "",
            f"- Original URL: {issue['html_url']}",
            f"- Original author: {issue['user']['login']}",
            f"- Original state: {issue['state']}",
            f"- Labels: `{labels}`",
            "",
            "## Original Body",
            "",
            body,
        ]
    )


def pr_mr_description(repo: str, pr: dict[str, Any]) -> str:
    body = pr.get("body") or "_No body provided on GitHub._"
    marker = marker_pr(repo, int(pr["number"]))
    return "\n".join(
        [
            f"<!-- {marker} -->",
            f"Backfilled from GitHub PR #{pr['number']}",
            "",
            f"- Original URL: {pr['html_url']}",
            f"- Original author: {pr['user']['login']}",
            f"- Source branch: `{pr['head']['ref']}`",
            f"- Target branch: `{pr['base']['ref']}`",
            f"- Draft: `{pr['draft']}`",
            "",
            "## Original Body",
            "",
            body,
        ]
    )


def pr_issue_description(repo: str, pr: dict[str, Any], reason: str) -> str:
    body = pr.get("body") or "_No body provided on GitHub._"
    marker = marker_pr(repo, int(pr["number"]))
    return "\n".join(
        [
            f"<!-- {marker} -->",
            f"Backfilled from GitHub PR #{pr['number']}",
            "",
            f"- Original URL: {pr['html_url']}",
            f"- Original author: {pr['user']['login']}",
            f"- Source branch: `{pr['head']['ref']}`",
            f"- Target branch: `{pr['base']['ref']}`",
            f"- Draft: `{pr['draft']}`",
            f"- Intake mode: `issue-record`",
            f"- Reason: {reason}",
            "",
            "## Original Body",
            "",
            body,
        ]
    )


def issue_title(prefix: str, number: int, title: str) -> str:
    return f"[GitHub {prefix} #{number}] {title}"


def workflow_slug() -> str:
    return os.getenv("WORKFLOW_SLUG", "github-intake-backfill")


def ci_output_path(name: str) -> Path:
    return Path.cwd() / "ci-output" / f"{workflow_slug()}-{name}"


def render_markdown(report: dict[str, Any]) -> str:
    lines = ["# GitHub Intake Backfill", ""]
    lines.append(f"- repository: `{report['github_repo']}`")
    lines.append(f"- apply: `{report['apply']}`")
    lines.append(f"- auto discover: `{report['auto_discover']}`")
    lines.append(f"- lookback days: `{report['lookback_days']}`")
    lines.append(f"- requested issues: `{len(report['requested_issues'])}`")
    lines.append(f"- requested PRs: `{len(report['requested_prs'])}`")
    lines.append(f"- discovered issues: `{len(report['discovered_issues'])}`")
    lines.append(f"- discovered PRs: `{len(report['discovered_prs'])}`")
    lines.append(f"- effective issues: `{len(report['issues'])}`")
    lines.append(f"- effective PRs: `{len(report['prs'])}`")
    lines.append(f"- overall status: `{report['status']}`")
    lines.append("")
    lines.append("| Kind | GitHub | Action | GitLab | Detail |")
    lines.append("|---|---|---|---|---|")
    for row in report["rows"]:
        gitlab_ref = "-"
        if row.get("gitlab_iid"):
            prefix = "!" if row.get("gitlab_kind") == "mr" else "#"
            gitlab_ref = f"`{prefix}{row['gitlab_iid']}`"
        detail = (row.get("detail") or "").replace("\n", " ").replace("|", "/")
        lines.append(
            f"| `{row['kind']}` | `#{row['github_number']}` | `{row['action']}` | {gitlab_ref} | {detail or '-'} |"
        )
    return "\n".join(lines) + "\n"


def write_outputs(report: dict[str, Any]) -> None:
    ci_output_dir = Path.cwd() / "ci-output"
    ci_output_dir.mkdir(parents=True, exist_ok=True)
    ci_output_path("result.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    ci_output_path("result.md").write_text(render_markdown(report), encoding="utf-8")
    ci_output_path("summary.txt").write_text(
        "\n".join(
            [
                f"github_repo={report['github_repo']}",
                f"apply={str(report['apply']).lower()}",
                f"auto_discover={str(report['auto_discover']).lower()}",
                f"lookback_days={report['lookback_days']}",
                f"status={report['status']}",
                f"requested_issue_inputs={len(report['requested_issues'])}",
                f"requested_pr_inputs={len(report['requested_prs'])}",
                f"discovered_issues={len(report['discovered_issues'])}",
                f"discovered_prs={len(report['discovered_prs'])}",
                f"requested_issues={len(report['issues'])}",
                f"requested_prs={len(report['prs'])}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )
    ci_output_path("runtime-context.txt").write_text(
        "\n".join(
            [
                f"observed_at_utc={report['observed_at_utc']}",
                f"github_repo={report['github_repo']}",
                f"apply={str(report['apply']).lower()}",
                f"auto_discover={str(report['auto_discover']).lower()}",
                f"lookback_days={report['lookback_days']}",
                f"gitlab_auth_mode={report['gitlab_auth_mode']}",
                f"github_token_present={str(report['github_token_present']).lower()}",
                f"requested_issue_inputs={','.join(str(item) for item in report['requested_issues']) or 'none'}",
                f"requested_pr_inputs={','.join(str(item) for item in report['requested_prs']) or 'none'}",
                f"discovered_issue_numbers={','.join(str(item) for item in report['discovered_issues']) or 'none'}",
                f"discovered_pr_numbers={','.join(str(item) for item in report['discovered_prs']) or 'none'}",
                f"requested_issues={','.join(str(item) for item in report['issues']) or 'none'}",
                f"requested_prs={','.join(str(item) for item in report['prs']) or 'none'}",
            ]
        )
        + "\n",
        encoding="utf-8",
    )


def gitlab_auth_mode() -> str:
    if os.getenv("PIPELINE_GITLAB_API_TOKEN", "").strip():
        return "private-token"
    if os.getenv("CI_JOB_TOKEN", "").strip():
        return "job-token"
    return "missing"


def int_env(name: str, default: int) -> int:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    return int(raw)


def github_since_iso(lookback_days: int) -> str:
    cutoff = datetime.now(timezone.utc).timestamp() - (lookback_days * 86400)
    return datetime.fromtimestamp(cutoff, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def discover_issue_numbers(repo: str, since_iso: str) -> list[int]:
    rows = paged_github_get(
        f"/repos/{repo}/issues",
        {"state": "all", "sort": "updated", "direction": "desc", "since": since_iso},
    )
    results: list[int] = []
    for row in rows:
        if not isinstance(row, dict):
            continue
        if row.get("pull_request"):
            continue
        updated_at = str(row.get("updated_at") or "")
        if updated_at and updated_at < since_iso:
            continue
        results.append(int(row["number"]))
    return results


def discover_pr_numbers(repo: str, since_iso: str) -> list[int]:
    rows = paged_github_get(
        f"/repos/{repo}/pulls",
        {"state": "all", "sort": "updated", "direction": "desc"},
    )
    results: list[int] = []
    for row in rows:
        if not isinstance(row, dict):
            continue
        updated_at = str(row.get("updated_at") or "")
        if updated_at and updated_at < since_iso:
            continue
        results.append(int(row["number"]))
    return results


def find_branch_pair_mr(source_branch: str, target_branch: str) -> dict[str, Any] | None:
    rows = paged_gitlab_get("/merge_requests", {"state": "opened", "source_branch": source_branch, "target_branch": target_branch})
    for row in rows:
        if row.get("source_branch") == source_branch and row.get("target_branch") == target_branch:
            return row
    return None


def main() -> int:
    repo = os.getenv("PIPELINE_GITHUB_INTAKE_REPOSITORY", "splunk/splunk-operator").strip()
    requested_issues = csv_ints("PIPELINE_GITHUB_INTAKE_ISSUES")
    requested_prs = csv_ints("PIPELINE_GITHUB_INTAKE_PRS")
    auto_discover = bool_env("PIPELINE_GITHUB_INTAKE_AUTO_DISCOVER", False)
    lookback_days = int_env("PIPELINE_GITHUB_INTAKE_LOOKBACK_DAYS", 7)
    discovered_issues: list[int] = []
    discovered_prs: list[int] = []
    if auto_discover:
        since_iso = github_since_iso(lookback_days)
        discovered_issues = discover_issue_numbers(repo, since_iso)
        discovered_prs = discover_pr_numbers(repo, since_iso)
    issues = sorted({*requested_issues, *discovered_issues})
    prs = sorted({*requested_prs, *discovered_prs})
    apply_changes = not bool_env("PIPELINE_GITHUB_INTAKE_DRY_RUN", False)
    report: dict[str, Any] = {
        "observed_at_utc": utc_now(),
        "github_repo": repo,
        "requested_issues": requested_issues,
        "requested_prs": requested_prs,
        "auto_discover": auto_discover,
        "lookback_days": lookback_days,
        "discovered_issues": discovered_issues,
        "discovered_prs": discovered_prs,
        "issues": issues,
        "prs": prs,
        "apply": apply_changes,
        "gitlab_auth_mode": gitlab_auth_mode(),
        "github_token_present": bool(os.getenv("PIPELINE_GITHUB_INTAKE_TOKEN", "").strip()),
        "status": "no-input",
        "rows": [],
    }

    if not issues and not prs:
        write_outputs(report)
        print(ci_output_path("result.md"))
        return 0

    repo_dir: Path | None = None
    repo_dir_created = False
    try:
        for issue_number in issues:
            issue = gh_issue(repo, issue_number)
            if issue.get("pull_request"):
                raise RuntimeError(f"GitHub item #{issue_number} is a PR. Use PIPELINE_GITHUB_INTAKE_PRS instead.")
            marker = marker_issue(repo, issue_number)
            existing = find_issue_by_marker(marker)
            action = "already-present"
            gitlab_iid = existing["iid"] if existing else None
            gitlab_kind = "issue" if existing else None
            detail = issue["html_url"]
            if not existing:
                action = "create-issue-record"
                if apply_changes:
                    created = gitlab_post(
                        "/issues",
                        {
                            "title": issue_title("Issue", issue_number, issue["title"]),
                            "description": issue_description(repo, issue),
                            "labels": "github-intake,github-intake::issue",
                        },
                    )
                    gitlab_iid = created["iid"]
                    gitlab_kind = "issue"
                    action = "issue-record-created"
            report["rows"].append(
                {
                    "kind": "issue",
                    "github_number": issue_number,
                    "action": action,
                    "gitlab_iid": gitlab_iid,
                    "gitlab_kind": gitlab_kind,
                    "detail": detail,
                }
            )

        for pr_number in prs:
            pr = gh_pull(repo, pr_number)
            marker = marker_pr(repo, pr_number)
            source_branch = pr["head"]["ref"]
            target_branch = pr["base"]["ref"]
            same_repo = (
                pr.get("head", {}).get("repo") is not None
                and pr["head"]["repo"]["full_name"] == repo
                and pr.get("base", {}).get("repo") is not None
                and pr["base"]["repo"]["full_name"] == repo
            )
            existing_mr = find_mr_by_marker(marker)
            existing_issue = find_issue_by_marker(marker)
            branch_pair_mr = find_branch_pair_mr(source_branch, target_branch)
            gitlab_iid = None
            gitlab_kind = None
            action = "already-present"
            detail = pr["html_url"]

            if existing_mr:
                gitlab_iid = existing_mr["iid"]
                gitlab_kind = "mr"
            elif existing_issue:
                gitlab_iid = existing_issue["iid"]
                gitlab_kind = "issue"
            elif same_repo and branch_pair_mr:
                gitlab_iid = branch_pair_mr["iid"]
                gitlab_kind = "mr"
                action = "branch-pair-conflict"
                detail = (
                    f"GitLab MR !{branch_pair_mr['iid']} already uses "
                    f"{source_branch}->{target_branch} without the GitHub intake marker"
                )
            elif same_repo and branch_exists(target_branch):
                action = "push-branch-and-create-mr" if not branch_exists(source_branch) else "create-mr"
                if apply_changes:
                    try:
                        if repo_dir is None:
                            repo_dir, repo_dir_created = ensure_clone(repo)
                        if not branch_exists(source_branch):
                            push_branch(repo_dir, source_branch)
                        created = gitlab_post(
                            "/merge_requests",
                            {
                                "source_branch": source_branch,
                                "target_branch": target_branch,
                                "title": pr["title"] if not pr["draft"] else f"Draft: {pr['title']}",
                                "description": pr_mr_description(repo, pr),
                                "remove_source_branch": False,
                            },
                        )
                        gitlab_iid = created["iid"]
                        gitlab_kind = "mr"
                        action = "mr-created"
                    except (subprocess.CalledProcessError, urllib.error.URLError, urllib.error.HTTPError) as exc:
                        reason = f"same-repo PR could not be promoted to MR automatically: {exc}"
                        created = gitlab_post(
                            "/issues",
                            {
                                "title": issue_title("PR", pr_number, pr["title"]),
                                "description": pr_issue_description(repo, pr, reason),
                                "labels": "github-intake,github-intake::pr",
                            },
                        )
                        gitlab_iid = created["iid"]
                        gitlab_kind = "issue"
                        action = "pr-issue-record-created"
                        detail = reason
            else:
                action = "create-pr-issue-record"
                if apply_changes:
                    reason = "cross-repo PR" if not same_repo else "target branch missing in GitLab"
                    created = gitlab_post(
                        "/issues",
                        {
                            "title": issue_title("PR", pr_number, pr["title"]),
                            "description": pr_issue_description(repo, pr, reason),
                            "labels": "github-intake,github-intake::pr",
                        },
                    )
                    gitlab_iid = created["iid"]
                    gitlab_kind = "issue"
                    action = "pr-issue-record-created"
                    detail = reason

            report["rows"].append(
                {
                    "kind": "pr",
                    "github_number": pr_number,
                    "action": action,
                    "gitlab_iid": gitlab_iid,
                    "gitlab_kind": gitlab_kind,
                    "detail": detail,
                }
            )
    finally:
        if repo_dir_created and repo_dir:
            shutil.rmtree(repo_dir, ignore_errors=True)

    if any(row["action"] == "branch-pair-conflict" for row in report["rows"]):
        report["status"] = "apply-complete-with-attention" if apply_changes else "dry-run-complete-with-attention"
    else:
        report["status"] = "apply-complete" if apply_changes else "dry-run-complete"
    write_outputs(report)
    print(ci_output_path("result.md"))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # noqa: BLE001
        report = {
            "observed_at_utc": utc_now(),
            "github_repo": os.getenv("PIPELINE_GITHUB_INTAKE_REPOSITORY", "splunk/splunk-operator").strip(),
            "requested_issues": csv_ints("PIPELINE_GITHUB_INTAKE_ISSUES"),
            "requested_prs": csv_ints("PIPELINE_GITHUB_INTAKE_PRS"),
            "auto_discover": bool_env("PIPELINE_GITHUB_INTAKE_AUTO_DISCOVER", False),
            "lookback_days": int_env("PIPELINE_GITHUB_INTAKE_LOOKBACK_DAYS", 7),
            "discovered_issues": [],
            "discovered_prs": [],
            "issues": csv_ints("PIPELINE_GITHUB_INTAKE_ISSUES"),
            "prs": csv_ints("PIPELINE_GITHUB_INTAKE_PRS"),
            "apply": not bool_env("PIPELINE_GITHUB_INTAKE_DRY_RUN", False),
            "gitlab_auth_mode": gitlab_auth_mode(),
            "github_token_present": bool(os.getenv("PIPELINE_GITHUB_INTAKE_TOKEN", "").strip()),
            "status": "failed",
            "rows": [],
            "error": str(exc),
        }
        write_outputs(report)
        print(str(exc), file=os.sys.stderr)
        raise SystemExit(1)
