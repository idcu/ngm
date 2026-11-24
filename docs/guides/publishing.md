# 发布指南

ngm 管依赖**来源**（Git 仓库 / 本地目录），不管发布**去向**（npm registry / JSR）。发布交给 npm / pnpm / yarn + changesets。

## 发布前门禁

```bash
ngm install --frozen-lockfile --offline
ngm verify
ngm audit
```

- `ngm verify` 确保 ref 仍指向锁定 commit；加 `--content` 做逐字节重放。
- `ngm audit` 检查已知漏洞：有超阈值漏洞 exit 1，查询失败 exit 4，不会把失败伪装成"干净"。
- 提交 `ngm.json` 与 `ngm.lock` 后再发版：`git add ngm.json ngm.lock && git commit -m "chore: lock dependencies"`。

## 用 changesets 发布

```bash
pnpm changeset
pnpm changeset version
pnpm changeset publish
```

CI 里把 ngm 门禁排在 publish 之前：

```yaml
- uses: actions/checkout@v4
- run: ngm install --frozen-lockfile --offline
- run: pnpm install --frozen-lockfile
- run: ngm verify
- run: ngm audit
- run: ngm build --engine=esbuild
- run: pnpm test
- run: pnpm changeset publish
  env:
    NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
```

## vendor 的发布策略

默认不提交 `ngm.vendor/`（`.gitignore` 排除它），CI 每次 `ngm install` 重新拉取。需要离线交付、审计门禁、镜像仓库或对 Git host 可用性不信任时，在 Git 侧把 `ngm.vendor/` 纳入版本控制——这**不是配置项**，`vendor.commit` 已移除。代价是仓库体积与 clone 时间增长、依赖更新 diff 巨大。

## 作为 Git 依赖被别人引用

需要三件事：

1. **可用的 tag**：`git tag v1.2.3 && git push --tags`。
2. **明确的入口**：在 `ngm.json` 或 `package.json` 里给 `main` / `types` / `exports["."]`。推断顺序是 ngm.json → package.json → `index.*` 约定。
3. **仓库可被访问**：公开仓库任何人可 clone；私有仓库需要使用者配置 Git 凭证。

## 边界

- ngm 不做 publish，也不生成 registry 包；它提供的是发布前的依赖证明。
- 不支持 git submodule（gitlink）与 LFS 实体内容；它们会被拒绝，不会 stealth 哈希一个指针。
- `ngm attest` 产出的是未签名语句，签名与验证用 cosign 等标准工具（见[签名配方](./signing.md)）。

## 相关文档

- [依赖管理](./dependency-management.md) · [签名配方](./signing.md) · [迁移指南](./migration.md)
- [供应链防护](../architecture/supply-chain.md) · [生态定位](./positioning.md)
