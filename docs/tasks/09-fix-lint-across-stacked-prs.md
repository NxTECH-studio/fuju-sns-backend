# Fix Lint Across Stacked PRs (#19, #26-#32)

## 概要

現在オープン中の 8 本の PR（#19, #26〜#32）で CI の lint チェックが失敗している。
本タスクは、`#19` を起点とする stacked PR 群に対して lint violation を最小コストで解消し、
指定の順序 `#19 → #32 → #26〜#31` で merge 可能な状態に揃える。

## 前提（依存タスク）

- **`01-rewrite-domain-to-authcore-alignment.md` 完了済み**（= PR #19 の元タスク）
- 本タスクは task 04〜08（PR #25〜#31 が実装するもの）が **未 merge** の状況下でのリカバリ作業である
- `04` の番号は廃案だが、PR #25 が「task 04 相当の AuthCore 実装続き」として別途存在する（#19 merge 待ち）
- 本計画書は既存の実装計画とは独立したオペレーションタスクであり、既存の依存グラフ（README のタスク一覧表）には乗らない

## 背景・目的

### 状況

| PR  | ブランチ                                          | ベース    | 役割                                       |
| --- | ------------------------------------------------- | --------- | ------------------------------------------ |
| #19 | `feat/rewrite-domain-to-authcore-alignment`       | `develop` | 基盤タスク。最優先で merge 必要            |
| #26 | `feat/fix-ogp-attach-position`                    | `develop` | OGP attach position 修正（#19 の上に積載） |
| #27 | `chore/remove-dead-code`                          | `develop` | dead code 削除（task 06 Phase 1）          |
| #28 | `docs/frontend-handoff-narrative`                 | `develop` | swagger + frontend handoff（task 06 Ph2-6）|
| #29 | `fix/image-response-snake-case`                   | `develop` | Image レスポンスの snake_case 化           |
| #30 | `fix/image-drop-storage-key`                      | `develop` | Image DTO から storage_key 除去            |
| #31 | `docs/add-task-plans-07-08`                       | `develop` | task plans 07, 08 追加                     |
| #32 | `fix/ci-go-version`                               | `develop` | `GO_VERSION` を 1.25 に揃える（独立）      |

8 本すべてで CI の `lint` / `test` / `build` 系チェックが失敗している。
既知の lint violation は以下の 2 種類（事前調査で判明した範囲）。

### 既知の lint violation

| ID  | 内容                                                                                    | 該当箇所                       | 影響する PR                   |
| --- | --------------------------------------------------------------------------------------- | ------------------------------ | ----------------------------- |
| L1  | `revive: redefines-builtin-id` - `copy` 組込関数の再定義                               | `pkg/authcore/cache.go:73`     | #19, #26, #27, #28, #29, #30, #31, #32 |
| L2  | `revive` - `exported var Version/BuildTime should have comment`                         | `cmd/server/main.go:27-28`     | #19, #32（他 PR は #32 の fix を取り込み済みの可能性あり） |

#### L1（`copy` 再定義）の詳細

```go
// pkg/authcore/cache.go:73
copy := *call.session   // ← revive が組込 `copy` の shadowing として検出
return &copy, nil
```

直線的な fix:

```go
sessionCopy := *call.session
return &sessionCopy, nil
```

この違反は **develop HEAD にも残っている**（= #19 以降のすべての branch が継承）。

#### L2（`Version` / `BuildTime` の exported doc comment）

現在 HEAD では既に doc comment が付与されている（下記参照）。`#32` の fix commit がこのコメントを追加した想定。

```go
// Version is the release identifier, overridden at build time via
// `-ldflags "-X main.Version=..."`. Defaults to "dev" for local builds.
// BuildTime is the UTC timestamp of the binary's build, also injected
// via ldflags. "unknown" until set by the release pipeline.
var (
    Version   = "dev"
    BuildTime = "unknown"
)
```

したがって:
- **#32 にはこの doc comment が含まれる**（独立 fix PR なので）
- **#19 にはおそらく含まれない**（`#32` が後発の独立派生のため、#19 元ブランチには反映されていない可能性が高い）
- #26-#31 は #19 を起点とする stacked なので、取り込み状況は branch の rebase 履歴次第

### 他のチェックの失敗

事前把握では `test` job で「covdata missing」系の失敗も報告されているが、これは Go 1.24 環境で `-covermode=atomic` を走らせた際のランタイム互換性問題で、`#32` の `GO_VERSION: "1.25"` 化で解消する見込み。
本計画では `#32` merge 後に test ジョブも緑化することを前提とし、もしそれでも残る test 失敗があれば別タスクで切る（スコープ外）。

### 目的

- 最小数の commit / PR で 8 本全部の CI を緑にする
- stacked 構造を壊さず、rebase 伝播で下流 PR の修正コストをゼロに近づける
- fix 作業自体が新たなレビュー負担を生まないよう、変更は機械的・最小差分に留める

## 影響範囲

### 変更対象ファイル

- `pkg/authcore/cache.go`（L1 fix）
- `cmd/server/main.go`（L2 fix。対象 branch に doc comment が無い場合のみ追記）
- `docs/tasks/09-fix-lint-across-stacked-prs.md`（本計画書）
- `docs/tasks/README.md`（本計画書への参照追加）

### 破壊的変更

なし。すべて機械的な変数リネーム + コメント追記。動作への影響なし。

## 依存関係 / 推奨 merge 順

ユーザー指定順: `#19 → #32 → #26 → #27 → #28 → #29 → #30 → #31`

```
develop
  ├─ #19 (基盤)                                 ← 最初にここへ L1 fix を commit & merge
  │    ├─ #26 (OGP attach pos) ─────────────┐
  │    ├─ #27 (remove dead code)            │
  │    ├─ #28 (frontend handoff docs)       │ #19 merge 後、これらは develop rebase
  │    ├─ #29 (image snake_case)            │ で自動的に L1 fix を取り込む
  │    ├─ #30 (image drop storage_key)      │
  │    └─ #31 (task plans 07-08)            ┘
  └─ #32 (ci go-version + L2 fix, 独立派生) ← #19 と並列で merge 可能
```

- `#32` は develop 直派生なので、`#19` より先でも後でも merge 可能
- ユーザー指定順では `#19 → #32` なので、`#19` merge → `#32` rebase → `#32` merge の順で進める
- `#26〜#31` は `#19` の merge commit を develop に取り込むため rebase が必須

## 対応方針の判断

3 つの選択肢を検討した。

### 選択肢 A: 各 PR に個別に fix commit を push

- **長所**: 即効。どの PR も独立して緑化できる
- **短所**: 同じ fix commit が 8 本の branch に散らばり、merge 時に conflict / 重複 commit が多発。stacked 履歴が汚れる

### 選択肢 B: `#32` と同様に develop 直派生の fix PR を新規作成

- **長所**: 独立した最小 PR として履歴が綺麗
- **短所**: 手順が多い。#19 の後に別 PR を挟むと stacked 順序の説明コストが上がる

### 選択肢 C（採用）: #19 に fix commit を追加、merge 後に下流を rebase

- **長所**: fix commit が 1 箇所に集約される。下流 #26〜#31 は `git rebase develop` するだけで L1 fix を取り込める。stacked 構造が崩れない
- **短所**: #19 のレビュー状態が「approve 済み → 再レビュー」に戻る可能性。fix 内容が機械的かつ 1 行なのでレビュー負担は最小
- **判断根拠**: L1 fix は **AuthCore 実装のローカル変数名変更のみ**。#19 のスコープ（AuthCore domain rewrite）と自然に整合し、別 PR に切るより親和性が高い

### `#32` の扱い

- `#32` は develop 直派生で `#19` には依存しない
- `#32` 自体にも L1 違反が残るので、`#19` merge → `#32` rebase develop で L1 fix を取り込む
- `#32` が抱える L2 fix は `#32` のスコープ内なのでそのまま
- ユーザー指定順 `#19 → #32 → #26〜#31` に従い、`#19` merge 直後に `#32` を rebase → merge する

## 実装ステップ

### 事前調査（着手時に必ず実施）

**1 step = 1 調査コマンド or 1 commit / 1 rebase 操作** の粒度。

1. **各 PR の最新 CI ログを取得し、L1/L2 以外の violation がないことを確認**
   ```bash
   for pr in 19 26 27 28 29 30 31 32; do
     echo "=== PR #$pr ==="
     gh pr checks $pr
   done
   # lint job 失敗のものは run-id を控え、詳細を参照
   gh run view --log-failed <run-id>
   ```
   - 予期外の violation が見つかった場合は、本計画書に追記して実装方針を更新してから進める

2. **`#32` ブランチにも L2 doc comment が含まれているかを確認**
   ```bash
   git fetch origin fix/ci-go-version
   git show origin/fix/ci-go-version:cmd/server/main.go | sed -n '20,35p'
   ```
   - コメントが入っていればそのまま。入っていなければ `#32` 側でも L2 fix を追加する必要あり（下記 Step 4 を調整）

### fix commit 作業

3. **`#19` ブランチに L1 fix を追加**
   ```bash
   git checkout feat/rewrite-domain-to-authcore-alignment
   git pull --ff-only
   # pkg/authcore/cache.go:73 の `copy` を `sessionCopy` にリネーム
   # （L2 doc comment が無ければ cmd/server/main.go にも同時に追加）
   git commit -am "fix(lint): rename copy to sessionCopy to satisfy revive redefines-builtin-id"
   git push
   ```
   - 1 commit。diff は `cache.go` の 2 行のみが理想

4. **`#19` の CI が緑化することを確認し、承認 & merge**
   ```bash
   gh pr checks 19 --watch
   gh pr merge 19 --squash   # or --merge、リポジトリの既定戦略に合わせる
   ```

5. **`#32` を develop に rebase**
   ```bash
   git checkout fix/ci-go-version
   git pull --ff-only
   git rebase origin/develop
   # conflict があれば解決（L1 fix 差分が入るだけのはずで conflict しないのが期待）
   git push --force-with-lease
   ```

6. **`#32` の CI が緑化することを確認し、承認 & merge**
   ```bash
   gh pr checks 32 --watch
   gh pr merge 32 --squash
   ```

7. **`#26` を develop に rebase → 緑化確認 → merge**
   ```bash
   git checkout feat/fix-ogp-attach-position
   git pull --ff-only
   git rebase origin/develop
   git push --force-with-lease
   gh pr checks 26 --watch
   gh pr merge 26 --squash
   ```

8. **`#27` を develop に rebase → 緑化確認 → merge**（手順は Step 7 と同形）

9. **`#28` を develop に rebase → 緑化確認 → merge**

10. **`#29` を develop に rebase → 緑化確認 → merge**

11. **`#30` を develop に rebase → 緑化確認 → merge**

12. **`#31` を develop に rebase → 緑化確認 → merge**
    - `#31` には task plans 07/08 + 本計画書 09 が含まれる（Step 13 で追加）

### ドキュメント更新

13. **本計画書と README 更新**
    - 本計画書 `docs/tasks/09-fix-lint-across-stacked-prs.md` を `#31` ブランチ（`docs/add-task-plans-07-08`）に含めて commit
    - `docs/tasks/README.md` の「タスク一覧（表）」に 09 を追記:
      ```markdown
      | 09 | [09-fix-lint-across-stacked-prs.md](./09-fix-lint-across-stacked-prs.md) | 01 | 確定済み。PR #19, #26-#32 の lint violation を #19 fix commit + 下流 rebase で解消するオペレーションタスク |
      ```
    - 備考: このドキュメントは「実装計画」というより「リカバリ作業手順」のため、README の依存グラフ図には追加しない。表のみ追記

## 検証方針

### 各ステップの緑化確認

- **Step 4（#19 merge 前）**: `gh pr checks 19` で `lint`, `test`, `build`, `security` がすべて `pass`
- **Step 6（#32 merge 前）**: 同上
- **Step 7〜12（#26〜#31 merge 前）**: 同上

### ローカル事前検証（push 前に推奨）

```bash
# lint
golangci-lint run --timeout=5m

# format / vet
gofmt -s -l .
go vet ./...

# test（DB は起動していなくてもユニットだけは通るように書かれている想定）
go test ./...
```

### 最終確認

すべての PR が merge された後、develop 上で `gh run list --branch develop` を見て最新 run がすべて緑になっていることを確認。

## ロールバック / 障害時手順

### rebase で conflict が出た場合

- **#26〜#31 の rebase 時に `pkg/authcore/cache.go` で conflict**:
  - 通常 #19 merge の変更と stacked branch 側の変更が重なった場合に発生
  - `ours` が stacked branch 側、`theirs` が develop（= #19 の fix）側
  - stacked branch が `cache.go` を触っていないなら `theirs` を採用する → `git checkout --theirs pkg/authcore/cache.go && git add`
  - stacked branch が `cache.go` を触っている場合は手動解決。必ず修正後の `sessionCopy` 名を残すこと

- **conflict 解決に失敗 / わからない場合**:
  - `git rebase --abort` で rebase 中止
  - ユーザー（= 実装者本人）に対応を仰ぐ（stacked branch のオーナー確認が必要）

### #19 merge 後に新たな lint violation が顕在化した場合

- 事前調査 Step 1 で想定外の violation が見落とされていた場合
- 発見次第、**develop 直派生の fix PR を追加作成**（= 選択肢 B を局所的に採用）し、mergeポイントを #32 と同格で扱う
- 本計画書の「実装ステップ」を修正し、新 PR の位置を明記する

### #32 の rebase で失敗した場合

- `#32` は CI 設定ファイル（`.github/workflows/ci.yml`）を触るだけの小 PR
- conflict が起きる可能性は極小。万一起きた場合は `--abort` → 手動で `ci.yml` に `GO_VERSION: "1.25"` を書き直すだけで良い

### merge 順を入れ替える必要が出た場合

- `#32` を `#19` より先に merge したい場合（ユーザー指定順と異なるが、技術的には可能）
  - `#32` は独立派生なので順序依存なし
  - ただしその場合 `#19` は `#32` merge 後の develop に rebase する必要がある
  - ユーザー指定順を優先し、特段の理由がない限り順序変更はしない

## スコープ外

- `pkg/authcore/cache.go` の `copy` 以外のリファクタ（singleflight への置換等）
- 他のパッケージに潜在する lint violation の事前掃除（= 本タスクで顕在化していないもの）
- CI 設定の変更（`.golangci.yml` の linter セット調整、timeout 延長等）
- `test` job の `covdata missing` エラーが `#32` 適用後も残る場合の深追い（別タスクで扱う）
- merge 戦略（squash / merge / rebase）の見直し。既定戦略に従う
- 本計画書が扱う 8 本以外の PR（たとえば `#25`）の取り扱い

## 技術的な補足

### L1 fix の命名選択

`copy` → 置換候補:

| 候補           | 評価                                    |
| -------------- | --------------------------------------- |
| `sessionCopy`  | **採用**。意味明瞭、短い                |
| `copied`       | 動詞過去形で Go 慣習的ではない          |
| `sessionDup`   | 略語でやや読みづらい                    |
| `snapshot`     | 意味が広すぎる                          |

### L2 fix が #19 側でも必要かの判定

- 事前調査 Step 2 で `#32` branch の `cmd/server/main.go` を確認し、doc comment の有無を確定する
- 本計画の想定: `#32` には既にコメントあり、`#19` には無し → `#19` の Step 3 commit に `main.go` の doc comment 追加も含める
- もし `#32` に doc comment が無い（= 未実装）なら、`#32` 側で Step 5 の rebase と同時に `cmd/server/main.go` への doc comment 追加 commit を足す

### golangci-lint v2 系の挙動

- 本リポジトリは `.golangci.yml` で `version: "2"` を宣言
- `revive` はデフォルトで `redefines-builtin-id` 有効
- ローカルで未検出の場合は `golangci-lint --version` で v2.11.4 を使っているか確認

### stacked PR ベースブランチの再設定

- 本計画では `#26〜#31` のベースはすべて `develop` 想定
- 実際に `gh pr view N --json baseRefName` で確認し、もし別の stacked 親（たとえば `#26` が `feat/rewrite-domain-to-authcore-alignment` をベースにしているなど）になっていた場合:
  - `gh pr edit N --base develop` で develop に切り替えた上で rebase
  - または親ブランチ merge を待ってから順に rebase（本計画の順序通り）
