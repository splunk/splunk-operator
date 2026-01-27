#!/usr/bin/env python3
import argparse
import ast
import json
import os
import re
import sys
from pathlib import Path

FIXTURE_FUNCS = {
    "gen_streaming_input_log_fixture": "streaming",
    "gen_forwarding_input_log_fixture": "forwarding",
    "gen_monitor_input_log_fixture": "monitor",
    "gen_oneshot_input_log_fixture": "oneshot",
}

DEFAULT_BUCKET = "splk-new-test-data"
FIXTURE_NAME_REGEX = re.compile(r"[-/.]")


def parse_args():
    parser = argparse.ArgumentParser(
        description="Extract DATF dataset definitions from core_datf conftests."
    )
    parser.add_argument(
        "--qa-root",
        required=True,
        help="Path to the splunkd/qa root.",
    )
    parser.add_argument(
        "--tests-root",
        default="core_datf/functional/backend/tests",
        help="Relative path under qa-root to scan for conftest.py files.",
    )
    parser.add_argument(
        "--output",
        required=True,
        help="Path to write the generated dataset registry YAML.",
    )
    parser.add_argument(
        "--bucket-env",
        default="DATF_S3_BUCKET",
        help="Environment variable name for dataset bucket.",
    )
    parser.add_argument(
        "--prefix-env",
        default="DATF_S3_PREFIX",
        help="Environment variable name for dataset prefix.",
    )
    return parser.parse_args()


def call_name(node):
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        base = call_name(node.value)
        if base:
            return base + "." + node.attr
    return ""


def join_path(parts):
    cleaned = []
    for part in parts:
        if part is None:
            return None
        if not isinstance(part, str):
            return None
        cleaned.append(part.strip("/").replace("\\", "/"))
    return "/".join([p for p in cleaned if p])


def eval_expr(node, consts):
    if isinstance(node, ast.Constant):
        if isinstance(node.value, (str, int, float, bool)):
            return node.value
        return None
    if isinstance(node, ast.Name):
        return consts.get(node.id)
    if isinstance(node, ast.Dict):
        result = {}
        for key_node, val_node in zip(node.keys, node.values):
            key = eval_expr(key_node, consts)
            val = eval_expr(val_node, consts)
            if key is None or val is None:
                return None
            result[key] = val
        return result
    if isinstance(node, (ast.List, ast.Tuple)):
        items = []
        for item in node.elts:
            value = eval_expr(item, consts)
            if value is None:
                return None
            items.append(value)
        return items
    if isinstance(node, ast.UnaryOp) and isinstance(node.op, ast.USub):
        value = eval_expr(node.operand, consts)
        if isinstance(value, (int, float)):
            return -value
        return None
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
        left = eval_expr(node.left, consts)
        right = eval_expr(node.right, consts)
        if isinstance(left, str) and isinstance(right, str):
            return left + right
        return None
    if isinstance(node, ast.JoinedStr):
        parts = []
        for value in node.values:
            if isinstance(value, ast.Str):
                parts.append(value.s)
            elif isinstance(value, ast.Constant) and isinstance(value.value, str):
                parts.append(value.value)
            else:
                return None
        return "".join(parts)
    if isinstance(node, ast.Call):
        name = call_name(node.func)
        if name in ("os.path.join", "path.join", "posixpath.join"):
            args = [eval_expr(arg, consts) for arg in node.args]
            return join_path(args)
        if isinstance(node.func, ast.Attribute) and node.func.attr == "format":
            base = eval_expr(node.func.value, consts)
            if isinstance(base, str):
                try:
                    args = [eval_expr(arg, consts) for arg in node.args]
                    kwargs = {
                        kw.arg: eval_expr(kw.value, consts)
                        for kw in node.keywords
                        if kw.arg
                    }
                    if any(arg is None for arg in args):
                        return base
                    if any(value is None for value in kwargs.values()):
                        return base
                    return base.format(*args, **kwargs)
                except Exception:
                    return base
        return None
    return None


def expr_text(node, source):
    try:
        text = ast.get_source_segment(source, node)
    except Exception:
        text = None
    if text is None:
        return ""
    return text.strip()


def sanitize_fixture_name(value):
    return FIXTURE_NAME_REGEX.sub("_", value)


def find_gen_input_types(tree):
    types = set()
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        if call_name(node.func) != "gen_input":
            continue
        if not node.args:
            continue
        arg_name = call_name(node.args[0])
        fixture_type = FIXTURE_FUNCS.get(arg_name)
        if fixture_type:
            types.add(fixture_type)
    return types


def extract_dataset(call, fixture_type, source, consts, relpath):
    values = {}
    exprs = {}
    for kw in call.keywords:
        if kw.arg is None:
            continue
        value = eval_expr(kw.value, consts)
        if value is not None:
            values[kw.arg] = value
        else:
            exprs[kw.arg] = expr_text(kw.value, source)

    logfile_name = values.get("logfile_name")
    if not isinstance(logfile_name, str) or not logfile_name:
        return None, "missing logfile_name"

    fixture_name = values.get("fixture_name")
    if not isinstance(fixture_name, str) or not fixture_name:
        fixture_name = sanitize_fixture_name(logfile_name)

    index = values.get("index") if isinstance(values.get("index"), str) else ""
    sourcetype = values.get("srctype") if isinstance(values.get("srctype"), str) else ""
    count_val = values.get("event_count")
    count = count_val if isinstance(count_val, int) else 0

    settings = {
        "origin": "%s:%s" % (relpath, getattr(call, "lineno", 0)),
        "fixture_type": fixture_type,
    }

    bucket_name = values.get("bucket_name")
    if isinstance(bucket_name, str) and bucket_name:
        settings["origin_bucket"] = bucket_name
    else:
        settings["origin_bucket"] = DEFAULT_BUCKET

    for key, label in [
        ("index", "index_expr"),
        ("srctype", "sourcetype_expr"),
        ("event_count", "count_expr"),
        ("logfile_name", "file_expr"),
    ]:
        if key in exprs and exprs[key]:
            settings[label] = exprs[key]

    for key in ("scope", "index_wait", "times"):
        if key in values:
            settings[key] = str(values[key])
        elif key in exprs and exprs[key]:
            settings["%s_expr" % key] = exprs[key]

    for key in ("index_settings", "srctype_settings"):
        if key in values:
            settings[key] = json.dumps(values[key], sort_keys=True)
        elif key in exprs and exprs[key]:
            settings["%s_expr" % key] = exprs[key]

    return {
        "name": fixture_name,
        "file": logfile_name,
        "index": index,
        "sourcetype": sourcetype,
        "count": count,
        "settings": settings,
    }, None


def dataset_signature(entry):
    settings = dict(entry.get("settings") or {})
    settings.pop("origin", None)
    signature_payload = {
        "file": entry.get("file", ""),
        "index": entry.get("index", ""),
        "sourcetype": entry.get("sourcetype", ""),
        "count": entry.get("count", 0),
        "settings": settings,
    }
    return json.dumps(signature_payload, sort_keys=True)


def unique_key(base, relpath, lineno):
    suffix = relpath.replace(os.sep, "/")
    suffix = suffix.replace("core_datf/functional/backend/tests/", "")
    suffix = suffix.replace("conftest.py", "")
    suffix = re.sub(r"[^A-Za-z0-9]+", "_", suffix).strip("_")
    if suffix:
        return "%s__%s_%s" % (base, suffix, lineno)
    return "%s__%s" % (base, lineno)


def yaml_quote(value):
    text = str(value)
    text = text.replace("\\", "\\\\")
    text = text.replace("\"", "\\\"")
    text = text.replace("\n", "\\n")
    text = text.replace("\t", "\\t")
    return "\"%s\"" % text


def write_yaml(path, datasets, bucket_env, prefix_env):
    lines = [
        "# Code generated by e2e/tools/datf_extract.py; DO NOT EDIT.",
        "datasets:",
    ]
    for key in sorted(datasets.keys()):
        entry = datasets[key]
        lines.append("  %s:" % yaml_quote(key))
        lines.append("    name: %s" % yaml_quote(entry["name"]))
        lines.append("    source: %s" % yaml_quote("s3"))
        lines.append("    bucket: %s" % yaml_quote("${%s}" % bucket_env))
        lines.append(
            "    file: %s"
            % yaml_quote("${%s}%s" % (prefix_env, entry["file"]))
        )
        lines.append("    index: %s" % yaml_quote(entry.get("index", "")))
        lines.append("    sourcetype: %s" % yaml_quote(entry.get("sourcetype", "")))
        lines.append("    count: %s" % entry.get("count", 0))
        settings = entry.get("settings") or {}
        if settings:
            lines.append("    settings:")
            for skey in sorted(settings.keys()):
                lines.append(
                    "      %s: %s" % (yaml_quote(skey), yaml_quote(settings[skey]))
                )
    Path(path).write_text("\n".join(lines) + "\n", encoding="utf-8")


def main():
    args = parse_args()
    qa_root = Path(args.qa_root).expanduser().resolve()
    tests_root = (qa_root / args.tests_root).resolve()
    if not tests_root.exists():
        print("tests root does not exist: %s" % tests_root, file=sys.stderr)
        return 2

    conftests = sorted(tests_root.rglob("conftest.py"))
    datasets = {}
    signatures = {}
    skipped = 0

    for conftest in conftests:
        source = conftest.read_text(encoding="utf-8")
        try:
            tree = ast.parse(source)
        except SyntaxError as exc:
            print("skip %s: %s" % (conftest, exc), file=sys.stderr)
            skipped += 1
            continue

        consts = {}
        for node in tree.body:
            if not isinstance(node, ast.Assign):
                continue
            if len(node.targets) != 1:
                continue
            target = node.targets[0]
            if not isinstance(target, ast.Name):
                continue
            value = eval_expr(node.value, consts)
            if value is not None:
                consts[target.id] = value

        gen_input_types = find_gen_input_types(tree)
        if gen_input_types:
            fixture_type = "|".join(sorted(gen_input_types))
        else:
            fixture_type = "dynamic"

        relpath = str(conftest.relative_to(qa_root))

        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            name = call_name(node.func)
            if name == "gen_func":
                dataset, reason = extract_dataset(
                    node, fixture_type, source, consts, relpath
                )
            elif name in FIXTURE_FUNCS:
                dataset, reason = extract_dataset(
                    node, FIXTURE_FUNCS[name], source, consts, relpath
                )
            else:
                continue
            if dataset is None:
                skipped += 1
                continue

            base_key = dataset["name"]
            sig = dataset_signature(dataset)
            if base_key in datasets:
                existing_sig = signatures.get(base_key)
                if existing_sig == sig:
                    existing = datasets[base_key]
                    origin = existing.get("settings", {}).get("origin", "")
                    new_origin = dataset.get("settings", {}).get("origin", "")
                    if new_origin and new_origin not in origin:
                        merged = ", ".join([o for o in [origin, new_origin] if o])
                        if "settings" not in existing:
                            existing["settings"] = {}
                        existing["settings"]["origin"] = merged
                    continue
                unique = unique_key(
                    base_key, relpath, getattr(node, "lineno", 0)
                )
                datasets[unique] = dataset
                signatures[unique] = sig
            else:
                datasets[base_key] = dataset
                signatures[base_key] = sig

    write_yaml(args.output, datasets, args.bucket_env, args.prefix_env)
    print("datasets: %d" % len(datasets))
    if skipped:
        print("skipped: %d" % skipped)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
