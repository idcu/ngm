# ADR-002：为什么直接拉 Git 仓库

- **状态**：已定
- **日期**：2026-09-29
- **范围**：依赖来源策略

---

## 背景

主流包管理器（npm / pnpm / Yarn）从 registry 拉 tarball，Git 依赖是二等公民（Git URL 只记录 commit，不强制 refType，archive 级策略需自行拼接）。

ngm 的目标用户场景：私有 fork、上游 commit 追溯、跨团队可信组件协作、离线交付、审计门禁。

---

## 决策

**ngm 直接拉取 Git 仓库作为依赖来源，不把 npm registry 当作主路径。**

---

## 理由

### 支持点

1. **精确追溯**：Git commit 是不可变锚点（前提是不 force push），可以回答"我审过的代码是不是还在"
2. **私有仓库友好**：企业内部 fork、私有 GitLab、Gitee 等场景，Git 协议比 registry 更通用
3. **离线/镜像场景**：Git 仓库可 mirror，vendor 可提交，审计可追溯
4. **跨语言通用**：Git 不绑定 JS 生态，理论上可扩展到其他语言的依赖管理
5. **与 registry 包互补**：ngm 管 Git 依赖，pnpm 管 registry 依赖，mappings 协议桥接

### 代价与诚实说明

1. **不"去中心化"**：仍需面对 Git host（GitHub/Gitee/GitLab）、凭证、force push、私有 repo
2. **"无 registry"≠"去信任"**：信任转移到了 Git host 和仓库维护者
3. **Git 协议复杂度**：https/ssh/git/shorthand 四种协议归一化、认证、浅克隆、monorepo 子路径都需要处理
4. **没有 registry 的版本元数据**：需要自己解析 tag 语义化版本、changelog、发布时间
5. **与 npm 生态割裂**：registry 包的传递依赖无法由 ngm 管理

---

## 对比参考

| 工具 | Git 依赖支持 | 局限 |
|------|-------------|------|
| npm | `git+https://` 记录 commit | 不强制 refType，无 archiveDigest |
| pnpm | Git dependencies | 无 archive 级策略 |
| Yarn | approvedGitRepositories | 无 archiveDigest |
| Bun | Git 依赖 | 安全/审计不按 ngm 语义对齐 |

---

## 后果

- ngm 只管 Git 来源的依赖
- registry 包交给 pnpm/npm/yarn
- 混合项目：ngm 生成 mappings，构建工具（Vite/esbuild/Deno）同时读 node_modules 和 ngm.vendor
- 需要定义 4 种协议的归一化规则与认证机制

---

## 相关文档

- [依赖解析](../architecture/dependency-resolution.md)
- [信任模型](../architecture/trust-model.md)
- [ADR-004：为什么声明用 refType 锁定用 commit](./adr-004-reftype.md)
