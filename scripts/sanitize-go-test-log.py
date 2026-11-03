#!/usr/bin/env python3
"""把 go test 日志里的环境相关文本替换掉，再交给公开 artifact。

为什么要有这一步：artifact 在公开仓库上同样可下载，而 `go test` 的输出里
经常带着 HOME、runner 临时目录、workspace 路径，以及（某些失败转储里）
`Bearer` / `access_token` 形态的文本。上传前替换掉这些是流程的一部分。

用法：
    python3 scripts/sanitize-go-test-log.py <in> <out>

退出码：0 = 已写出脱敏文件；2 = 参数/读文件失败（调用方应把 job 判红，
**不**上传原始日志）。
"""
import os
import re
import sys

# 这些环境变量的**值**会在日志里被替换成 <NAME>。只替换长度 >= 4 的值，
# 避免把 "true"/"1" 这类短值误替换成噪声。
ENV_KEYS = (
    "HOME",
    "USERPROFILE",
    "RUNNER_TEMP",
    "GITHUB_WORKSPACE",
    "GITEE_TOKEN",
    "GITHUB_TOKEN",
    "ACTIONS_RUNTIME_TOKEN",
    "ACTIONS_ID_TOKEN_REQUEST_TOKEN",
)


def main(argv):
    if len(argv) != 3:
        print("usage: sanitize-go-test-log.py <in> <out>", file=sys.stderr)
        return 2
    src, dst = argv[1], argv[2]
    try:
        text = open(src, encoding="utf-8", errors="replace").read()
    except OSError as exc:
        print("cannot read %s: %s" % (src, exc), file=sys.stderr)
        return 2

    # ANSI 颜色/光标控制：artifact 里留着只会让阅读器显示乱码。
    text = re.sub(r"\x1b\[[0-9;]*[A-Za-z]", "", text)

    for key in ENV_KEYS:
        value = os.environ.get(key)
        if value and len(value) >= 4:
            text = text.replace(value, "<" + key + ">")

    # 即使某个环境变量没有传进来，路径形态本身也可能暴露用户名。
    text = re.sub(r"([A-Za-z]:\\Users\\)[^\\\s\"']+", r"\1<user>", text)
    text = re.sub(r"(/home/)[^/\s\"']+", r"\1<user>", text)
    text = re.sub(r"(/Users/)[^/\s\"']+", r"\1<user>", text)

    # 常见的凭据形态：宁可多替换一行，也不把 token 留在公开 artifact 里。
    text = re.sub(r"(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+", r"\1<redacted>", text)
    text = re.sub(r"(?i)(access_token=)[^&\s\"']+", r"\1<redacted>", text)
    text = re.sub(r"(?i)((?:token|password|secret)[\"']?\s*[:=]\s*)[^\s\"',;}]+", r"\1<redacted>", text)
    text = re.sub(r"ghp_[A-Za-z0-9]+", "ghp_<redacted>", text)
    text = re.sub(r"github_pat_[A-Za-z0-9_]+", "github_pat_<redacted>", text)
    text = re.sub(r"glpat-[A-Za-z0-9_-]+", "glpat-<redacted>", text)

    try:
        with open(dst, "w", encoding="utf-8") as f:
            f.write(text)
    except OSError as exc:
        print("cannot write %s: %s" % (dst, exc), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
