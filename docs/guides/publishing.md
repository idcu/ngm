# 发布指南

> ngm 不管理 npm registry 发布。发布交给 npm/pnpm/yarn + changesets。

---

## ngm 的角色

ngm 管依赖**来源**（Git 仓库），不管发布**去向**（npm registry）。

---

## 发布流程

### 1. 锁定依赖

```bash
ngm install
git add ngm.json ngm.lock
git commit -m "chore: lock dependencies"
```

### 2. 检查漂移

```bash
ngm verify
```

确保 ref 仍指向锁定 commit。

### 3. 审计

```bash
ngm audit
```

检查已知漏洞。

### 4. 用 changesets 管理版本

```bash
# 生成 changeset
pnpm changeset

# 版本 bump
pnpm changeset version

# 发布到 npm
pnpm changeset publish
```

### 5. CI 流水线

```yaml
# .github/workflows/release.yml
name: Release
on:
  push:
    branches: [main]

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-node@v4
        with:
          node-version: 22

      - name: 安装 ngm
        run: |
          # 预编译二进制（v0.1.0 起可用）。国内把前缀换成 Gitee 发行版：
          #   https://gitee.com/idcu/ngm/releases/download/v0.1.0
          # 两个源的产物字节相同，共用同一份 SHA256SUMS。
          BASE=https://github.com/idcu/ngm/releases/latest/download
          curl -L "$BASE/ngm-linux-amd64" -o /usr/local/bin/ngm
          chmod +x /usr/local/bin/ngm

      - name: 安装 Git 依赖
        # --frozen-lockfile / --offline 属 v0.2；v0.1 的 ngm install 只有 --dir / --digest
        run: ngm install

      - name: 安装 registry 依赖
        run: pnpm install --frozen-lockfile

      - name: verify
        run: ngm verify

      # v0.2 起再加一步「ngm audit」（OSV 漏洞扫描）。
      # 现在写上去会让流水线以 exit 3 失败——那是"未实现"，不是"没问题"。

      - name: build
        run: ngm build --engine=esbuild

      - name: test
        run: pnpm test

      - name: changeset publish
        run: pnpm changeset publish
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
```

---

## vendor 的发布策略

### 不提交 vendor/（推荐）

```gitignore
ngm.vendor/
```

CI 每次 `ngm install` 重新拉取。

### 提交 vendor/（审计/离线场景）

```json
{
  "vendor": {
    "mode": "local",
    "commit": true
  }
}
```

适合：

- 离线交付
- 审计门禁
- 镜像仓库
- 对 Git host 可用性不信任

---

## Git 依赖的来源要求

如果你的项目**作为 Git 依赖**被别人引用，需要满足：

### 1. 有可用的 tag

```bash
git tag v1.2.3
git push --tags
```

### 2. 提供明确的入口

```json
// ngm.json 或 package.json
{
  "main": "./index.js",
  "types": "./index.d.ts",
  "exports": {
    ".": "./index.js"
  }
}
```

入口推断顺序（ngm.json → package.json → index 约定）见[配置详解](./configuration.md)。

### 3. 仓库可被访问

- 公开仓库：任何人可 clone
- 私有仓库：需要用户配置 Git 凭证

---

## 诚实说明

1. **ngm 不做发布**：发布到 npm/JSR 用专门工具
2. **ngm 的价值在发布前的依赖证明**：确保发布时用的依赖是可证明的
3. **Git 依赖的发布门槛**：需要 tag + 入口 + 可访问性

---

## 相关文档

- [依赖管理](./dependency-management.md)
- [架构：供应链防护](../architecture/supply-chain.md)
- [迁移指南](./migration.md)
