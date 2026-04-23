# Fix CI lint on PR #33 / #37

## 概要

オープン中の 2 本の PR（#33 `feat/wire-postgres-phase1`、#37 `feat/cookie-session-handoff-phase1`）で
`CI/CD Pipeline / lint` ジョブが FAILURE を出している。いずれも `golangci-lint run --timeout=5m` の
違反（gocyclo / goconst / gocritic / unparam）で止まっており、ロジックは健全。
本タスクでこれらを最小コストで解消し、両 PR を再び green に戻す。

## 背景・目的

### 状況

| PR  | branch                               | base      | lint の状態 |
| --- | ------------------------------------ | --------- | ----------- |
| #33 | `feat/wire-postgres-phase1`          | `develop` | FAILURE (10 issues) |
| #37 | `feat/cookie-session-handoff-phase1` | `develop` | FAILURE (2 issues)  |

`setup` / `security` / `review` などの他ジョブは両 PR とも SUCCESS。
`test` / `build` / `deploy-staging` は lint の FAILURE によりスキップされているだけ。
=> lint を通せば CI は green に戻る見込み。

### PR #33 の lint 違反（10 件）

golangci-lint の出力（Actions run 24733687504）:

| #   | 場所                                                 | linter    | 内容                                                |
| --- | ---------------------------------------------------- | --------- | --------------------------------------------------- |
| 1   | `cmd/server/main.go:64`                              | gocritic  | `exitAfterDefer`: `os.Exit(1)` で `defer cancelBackground()` が走らない |
| 2   | `internal/repository/testsupport/contract.go:1077`   | goconst   | `"verified"` が 5 回出現                            |
| 3   | `internal/repository/testsupport/contract.go:1085`   | goconst   | `"developer"` が 3 回出現                           |
| 4   | `internal/repository/testsupport/contract_ogp.go:91` | goconst   | `"updated"` が 3 回出現                             |
| 5   | `internal/repository/testsupport/contract.go:69`     | gocyclo   | `RunUserRepositoryContract` 複雑度 63 (>30)         |
| 6   | `internal/repository/testsupport/contract.go:381`    | gocyclo   | `RunPostRepositoryContract` 複雑度 53 (>30)         |
| 7   | `internal/repository/testsupport/contract.go:707`    | gocyclo   | `RunFollowRepositoryContract` 複雑度 44 (>30)       |
| 8   | `internal/repository/testsupport/contract.go:1005`   | gocyclo   | `RunBadgeRepositoryContract` 複雑度 52 (>30)        |
| 9   | `internal/repository/testsupport/contract_image.go:22` | gocyclo | `RunImageRepositoryContract` 複雑度 37 (>30)        |
| 10  | `internal/repository/testsupport/contract_ogp.go:136` | gocyclo  | `RunOGPJobQueueContract` 複雑度 41 (>30)            |

### PR #37 の lint 違反（2 件）

golangci-lint の出力（Actions run 24736107294）:

| #   | 場所                                  | linter   | 内容                                                                    |
| --- | ------------------------------------- | -------- | ----------------------------------------------------------------------- |
| 11  | `config/config_test.go:44`            | goconst  | `"None"` が 5 回出現                                                    |
| 12  | `internal/handler/session_test.go:12` | unparam  | `newIssueRequest` の `token` 引数が常に `"at-abc"` で呼ばれる           |

`newIssueRequest` 呼び出し箇所（4 箇所すべて `"at-abc"`）:
- `session_test.go:36`, `:74`, `:94`, `:111`

## 影響範囲

### PR #33 側

- `.golangci.yml`（testsupport の gocyclo 除外ルール追加）
- `cmd/server/main.go`（`os.Exit` 呼び出し前に deferred cleanup を手動実行）
- `internal/repository/testsupport/contract.go`（文字列リテラルを定数化）
- `internal/repository/testsupport/contract_ogp.go`（文字列リテラルを定数化）

### PR #37 側

- `config/config_test.go`（`"None"` を定数化）
- `internal/handler/session_test.go`（`newIssueRequest` の `token` 引数を削除）

破壊的変更なし（test helper / 起動シーケンスの内部整理のみ）。

## 実装ステップ

### Step 0: 前準備

- 両 PR ともすでに CI は一通り走っているので、ローカルで `golangci-lint run --timeout=5m`
  が通ることを確認するまでは push しない
- `golangci-lint` v2.11.4 をローカルで使えるようにしておく（バージョン差異で誤検知が変わるため）

### Step 1: PR #33 のリント違反を解消

ブランチ切り替え:

```bash
git fetch origin feat/wire-postgres-phase1
git switch feat/wire-postgres-phase1
git pull --ff-only
```

#### 1-A. `.golangci.yml` に testsupport の gocyclo 除外ルールを追加

ユーザー判断により、`internal/repository/testsupport/` 配下の `Run*Contract` 系は
「リポジトリ契約を網羅的に検証する単一関数」が設計意図のため、
gocyclo のみピンポイントで除外する。goconst / 他の linter は引き続き有効。

v2 フォーマットの書き方（既存 `.golangci.yml` が `version: "2"`）:

```yaml
issues:
  exclude-rules:
    - path: internal/repository/testsupport/
      linters:
        - gocyclo
  max-same-issues: 10
```

※ v2 フォーマットで `linters.exclusions.rules` が正式 API の場合はそちらを使う。
実装時に `golangci-lint config verify` で確認する。

#### 1-B. `cmd/server/main.go:64` の `exitAfterDefer` を修正

現状（#57-65 あたり）:

```go
ctx, cancelBackground := context.WithCancel(context.Background())
defer cancelBackground()
// ...
repos, cleanupRepos, err := newRepositorySet(ctx, cfg, log)
if err != nil {
    fmt.Fprintf(os.Stderr, "Failed to initialize repositories: %v\n", err)
    os.Exit(1)   // <- この時点で defer cancelBackground() が走らない
}
defer cleanupRepos()
```

修正方針: `os.Exit(1)` の前に `cancelBackground()` を明示的に呼ぶ。
この時点では `cleanupRepos` はまだ `defer` に積まれていない（`err != nil` で return 前）ので、
気にするのは `cancelBackground` のみ。

```go
repos, cleanupRepos, err := newRepositorySet(ctx, cfg, log)
if err != nil {
    fmt.Fprintf(os.Stderr, "Failed to initialize repositories: %v\n", err)
    cancelBackground()
    os.Exit(1)
}
defer cleanupRepos()
```

同じ関数内で他にも `os.Exit` → gocritic の対象になる箇所があれば同様に対処する
（初期ロード `config.Load()` の `os.Exit(1)` は defer が積まれる前なので対象外）。

#### 1-C. `contract.go` / `contract_ogp.go` の goconst を解消

該当ファイルの先頭に private const を宣言して、各リテラルを置き換える:

```go
// contract.go
const (
    badgeKeyVerified  = "verified"
    badgeKeyDeveloper = "developer"
)

// contract_ogp.go
const ogpTitleUpdated = "updated"
```

呼び出し側の `"verified"` / `"developer"` / `"updated"` をすべて定数参照に置換する。
goconst の検出閾値は「3 回以上」なので、残り 2 回以下は文字列リテラルのままで OK。

### Step 2: PR #33 の検証

```bash
golangci-lint run --timeout=5m
go build ./...
go test ./...
```

グリーンを確認したら commit & push:

```bash
git add .golangci.yml cmd/server/main.go internal/repository/testsupport/contract.go internal/repository/testsupport/contract_ogp.go
git commit -m "fix(lint): resolve gocyclo/goconst/gocritic on feat/wire-postgres-phase1"
git push
```

GitHub Actions の lint ジョブが green になることを確認。

### Step 3: PR #37 のリント違反を解消

ブランチ切り替え:

```bash
git fetch origin feat/cookie-session-handoff-phase1
git switch feat/cookie-session-handoff-phase1
git pull --ff-only
```

#### 3-A. `config/config_test.go` の goconst を解消

`"None"` が 5 回出現している。ファイル先頭に private const を追加して置換。

```go
const sameSiteNone = "None"
```

テスト中の `c.SessionCookieSameSite = "None"` 等を `sameSiteNone` に置き換える。

#### 3-B. `internal/handler/session_test.go:12` の unparam を解消

`newIssueRequest(token string, exp time.Time)` の `token` はすべての呼び出しで
`"at-abc"` が渡されている（4 箇所）。引数を削除してリテラルをヘルパ内部に埋め込む:

```go
func newIssueRequest(exp time.Time) *http.Request {
    const token = "at-abc"
    r := httptest.NewRequest(http.MethodPost, "/v1/auth/session", nil)
    ctx := auth.SetAccessTokenInContext(r.Context(), token)
    if !exp.IsZero() {
        ctx = auth.SetExpiresAtInContext(ctx, exp)
    }
    return r.WithContext(ctx)
}
```

呼び出し側 4 箇所を `newIssueRequest(exp)` に更新:
- `session_test.go:36` `newIssueRequest("at-abc", exp)` → `newIssueRequest(exp)`
- `session_test.go:74` `newIssueRequest("at-abc", time.Time{})` → `newIssueRequest(time.Time{})`
- `session_test.go:94` `newIssueRequest("at-abc", time.Now().Add(500*time.Millisecond))` → `newIssueRequest(time.Now().Add(500*time.Millisecond))`
- `session_test.go:111` `newIssueRequest("at-abc", time.Now().Add(-time.Hour))` → `newIssueRequest(time.Now().Add(-time.Hour))`

### Step 4: PR #37 の検証

```bash
golangci-lint run --timeout=5m
go build ./...
go test ./...
```

グリーン確認後 commit & push:

```bash
git add config/config_test.go internal/handler/session_test.go
git commit -m "fix(lint): resolve goconst/unparam on feat/cookie-session-handoff-phase1"
git push
```

### Step 5: 最終確認

両 PR の GitHub Actions lint ジョブが green になり、続いて `test` / `build` が走って
それも green になることを `gh pr checks 33` / `gh pr checks 37` で確認する。

## テスト要件

- ローカル `golangci-lint run --timeout=5m` が両ブランチで 0 issue
- `go test ./...` が両ブランチで pass
- `go build ./...` が両ブランチで成功
- GitHub Actions: 両 PR の `CI/CD Pipeline / lint` が SUCCESS

## 技術的な補足

### gocyclo 除外の方針

今回 `internal/repository/testsupport/` 以下の 6 関数を gocyclo 除外にしている。
理由はユーザー判断どおり「契約テストは 1 関数にまとめて順序・網羅性を読むのが設計意図」
であり、サブテスト関数に機械的に分割すると可読性が下がる。
ただしこれは **test helper パッケージに限った許容**であり、production code の
gocyclo は引き続き有効。もし今後 `testsupport/` 以外の test helper でも同じ
問題が出たら、exclude-rules に個別にパスを追加する形で対応する。

### `.golangci.yml` v2 のフォーマット注意

既存 `.golangci.yml` は `version: "2"`。golangci-lint v2 系では
`issues.exclude-rules` が deprecate されて `linters.exclusions.rules` に移行している
可能性がある。実装時に以下で検証する:

```bash
golangci-lint config verify
golangci-lint run --timeout=5m
```

### PR #33 の `exitAfterDefer`

`cmd/server/main.go` の `main` 内には `os.Exit(1)` が複数ある。
- `config.Load` 失敗時: defer 未登録なので gocritic 対象外（たぶん検出されていない）
- `newRepositorySet` 失敗時: `defer cancelBackground()` 登録済みなので gocritic 対象（今回の #1）
- その後にも `os.Exit(1)` があるならすべて cleanup 明示呼び出しが必要

実装時に `grep -n 'os.Exit' cmd/server/main.go` で全箇所を洗って、
defer が積まれた後のものはすべて同じパターンで修正する。

### PR 順序・ベース

両 PR とも `develop` をベースにしている。stacked 関係ではないので、
独立に fix 可能。他のオープン PR への影響もない。

### 既存 task 09 との関係

`docs/tasks/09-fix-lint-across-stacked-prs.md` は過去の別ラウンド
（PR #19 の stack 系）のリカバリで、現在は対象 PR すべて merge 済み。
今回の task 12 はその後に新規で開いた PR #33 / #37 に対する別対応であり、
依存関係はない。
