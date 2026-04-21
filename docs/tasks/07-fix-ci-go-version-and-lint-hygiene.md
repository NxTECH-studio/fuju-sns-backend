# Fix CI Go version drift and lint hygiene

## 概要

`feat/implement-ogp-fetcher` 以降、全 PR で CI の `test` ジョブが失敗している根本原因を塞ぐ。`go.mod` が `go 1.25.0` を要求しているのに対し、`.github/workflows/ci.yml` の `GO_VERSION` が `"1.22"` に固定されており、`go test -race -coverprofile ...` 実行時に `go: no such tool "covdata"` で落ちる。合わせて `cmd/server/main.go` の exported var に doc comment が欠けているために revive の `exported` ルールが lint ジョブ（`golangci-lint v2.11.4`）で発火している（= lint ジョブが通過しないため `test` まで到達しないケースもある）問題を解消する。

## 前提（依存タスク）

- `04-fix-ci-lint-and-pragent.md` 完了済み（lint 違反大半の解消と `golangci-lint v2.11.4` への統一）
- `05-fix-ogp-attach-position.md` 完了済み（migration `008` 追加）
- `06-frontend-handoff-docs-and-dead-code-removal.md` とは直接の依存関係なし（並行可）
- PR base は **develop**（プロジェクトの標準運用）

## 背景・目的

### 現状の不具合

1. **Go toolchain とモジュール要求のドリフト**
   - `go.mod:3` が `go 1.25.0` を宣言（`golang.org/x/net v0.53.0` ほか新しい `go` 要件を持つ依存が入ったため）。
   - 一方 `.github/workflows/ci.yml:15` は `GO_VERSION: "1.22"` のまま。
   - `actions/setup-go@v4` は toolchain の自動アップグレードを行わないため、`go 1.22` 環境で `go test -race -coverprofile=coverage.out -covermode=atomic ./...` を実行した際に `covdata` ツールを呼び出す過程で `go: no such tool "covdata"` で失敗する（Go 1.22 の coverage tool path と 1.25 の module 要求が噛み合わない）。
   - 結果、`test` ジョブが常時 red となり、PR がマージできない。

2. **`cmd/server/main.go` の exported var に doc comment 無し**

   ```go
   // cmd/server/main.go:32-35
   var (
       Version   = "dev"
       BuildTime = "unknown"
   )
   ```

   - revive v2.11.4 の `exported` ルール (`var-naming` / `exported comment`) で警告。ローカル `golangci-lint run ./...` でも同じ違反が再現する。
   - `04-fix-ci-lint-and-pragent.md` で他の lint 違反はすべて潰したが、この 2 行は見落とされている。

### ゴール

- CI の `lint` / `test` ジョブが両方 green に戻り、`develop` / `main` 向け PR がブロックなくマージできる状態にする。
- 今後 `go.mod` と CI の Go バージョンがずれることを防ぐ軽い仕組みを入れる（Makefile / README 1 行追記）。

## 影響範囲

### CI / ワークフロー

- `.github/workflows/ci.yml`
  - 15 行目 `GO_VERSION: "1.22"` → `"1.25"`
  - 他の `actions/setup-go` 呼び出し（`setup` / `lint` / `test` / `build` / `security` 各ジョブ）は `${{ env.GO_VERSION }}` 経由なので、この一箇所の書き換えで全ジョブに反映される

### Go ソース（lint hygiene）

- `cmd/server/main.go:32-35` — `Version` / `BuildTime` の exported var に doc comment を追加
- その他 exported シンボルで同種の違反が残っていれば同 PR で潰す（想定では追加ヒットなし。Step 3 で最終確認）

### 軽い運用メモ

- `Makefile` または `README.md` に「CI の `GO_VERSION` は `go.mod` と同期させる」旨の 1 行コメントを追加（optional、PR レビューで不要と判断されたら削る）

### 破壊的変更

- **なし**。HTTP API / DB / バイナリ / Go パッケージ exported シンボルすべて無変更。CI の Go バージョン bump のみ。
  - `go 1.22 → 1.25` はモジュール側の要求に CI 環境を揃えるだけで、新機能を使うわけではない。ローカル開発者は `go.mod` 要求どおりの Go をすでに使っている前提（そうでなければ `go build` 自体が通らない）。

## スコープ

### 含む

- `.github/workflows/ci.yml` の `GO_VERSION` を `"1.25"` に揃える
- `cmd/server/main.go` の exported var に doc comment を付与
- `golangci-lint run ./...` をローカルで走らせ、他に revive `exported` 違反が残っていないかを確認。あれば同 PR で修正
- `Makefile` or `README.md` に 1 行メモ（Go バージョン同期について。optional）

### 含まない

- **Go の本格 upgrade（1.22 → 1.25）に伴う機能活用**（例: `for range int`, `slices.All`, `maps.All` 等への書き換え）。あくまで toolchain のバージョンを `go.mod` の要求に合わせるだけ。
- **CI パイプラインの構造変更**（既存 `setup → lint → test → build → security → deploy-staging / notify` の依存グラフは触らない）
- **`actions/setup-go@v4` → `@v5` 等のアクションバージョン更新**（別 PR）
- **`golangci-lint` の config 変更 / ルール追加**（別 PR）
- **`govulncheck` や `gosec` の `continue-on-error` を外す強化**（別 PR）
- **`.go-version` ファイルの新設**（Makefile/README の 1 行追記で足りる前提。必要なら別タスク化）

## 実装ステップ

### Step 1. CI の `GO_VERSION` を `1.25` に bump

対象: `/home/sheep/dev/fuju/backend/.github/workflows/ci.yml` 15 行目

```yaml
# before
env:
    GO_VERSION: "1.22"
    GOLANGCI_LINT_VERSION: "v2.11.4"

# after
env:
    GO_VERSION: "1.25"
    GOLANGCI_LINT_VERSION: "v2.11.4"
```

- `setup` / `lint` / `test` / `build` / `security` すべて `${{ env.GO_VERSION }}` 参照なので、この 1 行で全ジョブに伝播する。
- `actions/setup-go@v4` は `"1.25"` を「major.minor の最新 patch」として解決するため、`1.25.x` の最新パッチが自動で選ばれる。`go.mod` が `go 1.25.0` を指定しているが、これは下限要求であり `1.25.x` で満たされる。

### Step 2. `cmd/server/main.go` の exported var に doc comment を付与

対象: `/home/sheep/dev/fuju/backend/cmd/server/main.go` 32-35 行目

```go
// before
var (
    Version   = "dev"
    BuildTime = "unknown"
)

// after
var (
    // Version is the binary version, populated at build time via
    // -ldflags "-X main.Version=...". Defaults to "dev" for local builds.
    Version = "dev"
    // BuildTime is the UTC build timestamp, populated at build time via
    // -ldflags "-X main.BuildTime=...". Defaults to "unknown".
    BuildTime = "unknown"
)
```

- `Version` は `.github/workflows/ci.yml:134` の `-ldflags="-X main.Version=${{ github.sha }}"` と `Makefile:7` の `LDFLAGS=-ldflags "-X main.Version=$(shell git describe --tags --always --dirty)"` で上書きされる。doc にその事実を書き残す。
- `BuildTime` は現状 build 時に上書きする仕組みは入っていないが、慣用的に残っているフィールドなので、将来 ldflags で流し込む前提のコメントにしておく。

> NOTE: 変数を増やす（例: `GitCommit`）のは本タスクのスコープ外。今ある 2 つに comment を付けるだけに留める。

### Step 3. `golangci-lint run ./...` で残違反の最終確認

- ローカルで `make setup` 済みの環境で `golangci-lint run --timeout=5m ./...` を実行し、違反ゼロ件を確認する。
- 万が一 Step 2 以外の revive `exported` / `package-comments` 違反が検出されたら、同 PR で doc comment 追加のみで対応する（関数シグネチャやパッケージ構造は変えない）。
- 追加ヒットがなければこの Step は通過確認のみ。

### Step 4.（optional）Go バージョン同期の 1 行メモを追加

どちらか一方で足りる。両方入れる必要はない。

**案 A: `Makefile` の `help` セクションに注記**

```makefile
# Note: CI の .github/workflows/ci.yml の GO_VERSION は go.mod の `go X.Y` に追随すること
```

**案 B: `README.md` の開発手順節に 1 行**

```md
> Note: `.github/workflows/ci.yml` の `GO_VERSION` は `go.mod` の `go X.Y` 行と同じ値に揃える。
```

- レビューで「冗長」と判断されたら削除して Step 1-3 だけで PR を上げる。`[未確定]`: 案 A / 案 B / 省略 のいずれにするかは PR 作成時に決める。

### Step 5. ローカル検証

- `go mod download` が成功すること（Go 1.25 がローカルに入っていない場合は `gvm` / `asdf` / `go install golang.org/dl/go1.25@latest` 等で導入）
- `go build ./...` が通ること
- `go test -race -coverprofile=coverage.out -covermode=atomic ./...` が通ること（CI と同じコマンド）
- `gofmt -s -l .` が空
- `go vet ./...` が通る
- `golangci-lint run --timeout=5m ./...` が通る（Step 3 と重複するが最終確認として再掲）

### Step 6. CI 上で検証

- push 後、`.github/workflows/ci.yml` 全ジョブが green であることを確認する:
  - `setup` ✓
  - `lint` ✓（Step 2 の doc comment で revive 違反ゼロ）
  - `test` ✓（Step 1 の Go version bump で `covdata` エラー解消）
  - `build`（matrix 5 組）✓
  - `security` ✓（`continue-on-error: true` なのでそもそも fail しない想定だが、ログで異常がないことは確認）
- PR 本文に CI run の URL を貼り、レビュアが一目で green を確認できるようにする。

## 検証 / テスト方針

- 既存テスト（`go test -race ./...`）が全 pass
- 新規テスト追加は **なし**（コードロジックは触らない）
- CI パイプラインで `lint` / `test` / `build` 全ジョブが green
- `PRAgent` (`.github/workflows/pr-review.yml`) の commit format check は `04-fix-ci-lint-and-pragent.md` で既に修正済みのため、本 PR では再確認のみ

## 技術的な補足

### なぜ Go 1.22 と 1.25 のズレが `covdata` エラーを生むのか

- `actions/setup-go@v4` は `go-version: "1.22"` を指定すると `1.22.x` の Go distribution を単独でインストールする（toolchain の自動アップグレード機能は未使用、または指定した線側の toolchain は取りに行かない）。
- `go 1.21+` では、`go.mod` の `go X.Y` が実行環境の Go より新しい場合に、`GOTOOLCHAIN` の解決で「より新しい toolchain を透過的に取ってきて再実行する」仕組みが入っている。
- しかし CI 環境では `GOTOOLCHAIN=local` 相当の挙動になりやすく（ネットワーク / cache の制約）、`go test -race -coverprofile=... -covermode=atomic` が `covdata` 内部ツールを探す段階で不整合を起こす。
- 明示的に `GO_VERSION: "1.25"` を指定して setup-go に正しい Go を入れさせるのが最も素直。`go.mod` の `go` 行を下げる方向は依存が追随せず現実的でない（依存が既に `go 1.25` 要求）。

### 代替案と不採用理由

- **A. `GOTOOLCHAIN=auto` を CI で明示** — 動く可能性はあるが、network / cache に依存して flaky になりやすい。明示 bump の方が決定的で安価。
- **B. `go.mod` の `go` 行を `1.22` に下げる** — 依存 (`golang.org/x/net v0.53.0` 等) が `go 1.23+` 等を要求するため不可能。
- **C. `actions/setup-go@v5` に更新** — `v5` で setup ロジックが改善されているが、現環境で `v4` のまま bump 値だけ上げれば十分。アクションバージョン更新は別 PR に分離してレビュー負荷を下げる。

### 将来の改善（本タスクのスコープ外）

- `.go-version` ファイルを導入し `setup-go.with.go-version-file: .go-version` を使う運用にすれば同期忘れがなくなる（別タスク）。
- Go 1.25 の新機能を活用する refactor（例: `slices.Concat` で `04` 計画の `appendAssign` 回避ロジックを簡素化）も別タスク。

## 参考

- Go toolchain の仕組み: https://go.dev/doc/toolchain
- `actions/setup-go` の `go-version` 解決: https://github.com/actions/setup-go#getting-started
- revive `exported` ルール: https://github.com/mgechev/revive/blob/master/RULES_DESCRIPTIONS.md#exported
- 失敗時の実際の CI ログ（参考）: `test` ジョブの `Run unit tests` step に `go: no such tool "covdata"`

## `[未確定]` マーカー

- **Step 4 の optional 追加をどうするか** — 以下のどれかを選ぶ。PR 作成時にレビュー観点で決める。
  - 案 A（Makefile に注記のみ）
  - 案 B（README.md に注記のみ）
  - 案 C（両方追加する）
  - 案 D（省略する）
  - デフォルト想定: **案 B**（README 冒頭近くに 1 行、開発者が最初に読む場所に置く）
