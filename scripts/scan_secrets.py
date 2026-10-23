#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""轻量密钥扫描器：复刻 GitGuardian 的两类通用检测规则。

1. Generic High Entropy Secret —— 字符串字面量香农熵 >= 3.5 且长度 >= 10
2. Generic Password          —— 赋值给 password/secret/token/api_key 等名字的非占位值

用法: python3 scripts/scan_secrets.py [项目根目录]
只扫描 git 已跟踪的文本文件。
"""
import math
import os
import re
import subprocess
import sys

PLACEHOLDER = {
    "", "xxx", "xxx,xxx,xxx", "your_token", "your-token", "token", "password",
    "changeme", "todo", "demo", "test", "example", "none", "null", "nil", "",
    "secret", "newpass", "demo-pass-1", "demo-pass-2", "secret-pass", "pw",
    "true", "false", "yes", "no", "on", "off",
}

# 名字里出现这些词 -> 视为凭据类赋值
CRED_NAME = re.compile(
    r"(password|passwd|pwd|secret|token|apikey|api_key|authorization|"
    r"credential|private_key|access_key|auth)", re.I)

# 各类字符串字面量
LITERAL = re.compile(r'"(?:[^"\\]|\\.){6,}"' r"|'(?:[^'\\]|\\.){6,}'" r"|`[^`]{6,}`")

ASSIGN = re.compile(
    r"^\s*(?:var|const|let|final)?\s*[A-Za-z_][\w\.\[\]\"']*\s*[:=]{1,2}\s*"
    r"(\"[^\"]{3,}\"|'[^']{3,}')", re.M)

# 明显不是密钥的：URL / 正则 / 中文文案 / 版本号 / 路径 / 模板占位 / 包完整性哈希
NOISE = re.compile(
    r"^(https?://|//|\./|/|\\|[a-z]+://)|[\u4e00-\u9fff]|^\^|^\$$|^\d+[\d\.\-]*$"
    r"|\{\{|^sha\d+-|^git\+|@\d+\.\d+\.\d+")

# 这些文件里的高熵串是构建产物/依赖锁/模板，不是凭据
EXCLUDE_FILES = {
    "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "Cargo.lock",
    "poetry.lock", "info.json", "info.dev.json", "SHA256SUMS.txt",
}


def looks_like_secret(val: str, min_entropy: float = 3.8) -> bool:
    """GitGuardian Generic High Entropy 的近似判据。

    必须同时满足：长度 >= 10、熵 >= min_entropy、无空格、
    且「大小写混合」且「含数字或符号」。这样能滤掉普通英文短语、路径与文档句子。
    """
    if len(val) < 10 or " " in val:
        return False
    if NOISE.search(val):
        return False
    if shannon(val) < min_entropy:
        return False
    has_upper = any(c.isupper() for c in val)
    has_lower = any(c.islower() for c in val)
    has_digit = any(c.isdigit() for c in val)
    has_symbol = any(not c.isalnum() for c in val)
    return has_upper and has_lower and (has_digit or has_symbol)


def shannon(s: str) -> float:
    if not s:
        return 0.0
    counts = {}
    for ch in s:
        counts[ch] = counts.get(ch, 0) + 1
    n = len(s)
    return -sum((c / n) * math.log2(c / n) for c in counts.values())


def is_text(path: str) -> bool:
    ext = os.path.splitext(path)[1].lower()
    return ext in {
        ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".md", ".json", ".ini",
        ".yaml", ".yml", ".toml", ".txt", ".cfg", ".sh", ".html", ".css", ".xml",
    } or ext == ""


def unquote(s: str) -> str:
    return s.strip().strip('"').strip("'").strip("`")


def scan(root: str):
    try:
        files = subprocess.run(
            ["git", "-C", root, "ls-files"],
            capture_output=True, text=True, check=True).stdout.split()
    except subprocess.CalledProcessError as exc:
        print(f"git ls-files 失败: {exc}", file=sys.stderr)
        return []

    findings = []
    for rel in files:
        path = os.path.join(root, rel)
        if not os.path.isfile(path) or not is_text(path):
            continue
        if os.path.basename(rel) in EXCLUDE_FILES:
            continue
        try:
            with open(path, "r", encoding="utf-8", errors="ignore") as fp:
                lines = fp.readlines()
        except OSError:
            continue

        for lineno, line in enumerate(lines, 1):
            stripped = line.strip()
            if stripped.startswith(("//", "#", "*", ";")):
                # 注释里的常量说明不算泄露（但仍会记录低优先级的）
                pass

            # 规则 2：凭据类赋值
            m = ASSIGN.match(line)
            if m and CRED_NAME.search(line):
                val = unquote(m.group(1))
                if val.lower() not in PLACEHOLDER and not NOISE.search(val):
                    findings.append((rel, lineno, "Generic Password", val, line.strip()[:140]))

            # 规则 1：高熵字面量
            for lit in LITERAL.findall(line):
                val = unquote(lit)
                if val.lower() in PLACEHOLDER:
                    continue
                if looks_like_secret(val):
                    findings.append((rel, lineno, "Generic High Entropy Secret", val, line.strip()[:140]))
    return findings


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else "."
    findings = scan(root)

    seen = set()
    uniq = []
    for f in findings:
        key = (f[2], f[3])
        if key in seen:
            continue
        seen.add(key)
        uniq.append(f)

    print(f"扫描文件数: git 跟踪的文本文件")
    print(f"去重后命中: {len(uniq)}\n")
    for rel, lineno, rule, val, ctx in sorted(uniq, key=lambda x: (x[2], x[0])):
        print(f"[{rule}] {rel}:{lineno}")
        print(f"    值: {val!r}  (长度 {len(val)}, 熵 {shannon(val):.2f})")
        print(f"    行: {ctx}")
        print()
    return 0


if __name__ == "__main__":
    sys.exit(main())
