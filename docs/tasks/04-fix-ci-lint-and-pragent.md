# Fix CI lint failures and PRAgent commit-format false positive

## 概要

`feat/implement-ogp-fetcher` ブランチで CI が失敗している 2 件を解消する。

1. `golangci-lint v2.11.4` で検出された 18 件の違反を修正
2. `pr-review.yml` の "Check commit message format" ステップ（PRAgent）の誤検知・不整合を修正

## 背景・目的

- CI の lint ジョブが red のためマージできない。
- 違反の大半は linter のバージョンアップ（v1 → v2.11.4）で新たに有効化されたルール（`revive` の `exported` スタッター検出, `gocritic` の `appendAssign` 等）と、OGP 実装で追加されたコード（`pkg/ogp/*`, `internal/usecase/ogp/worker_test.go`）由来。
- PRAgent はコミット履歴が Conventional Commits に準拠していても "コミットメッセージ形式のエラー" を投げる状況がある。ロジックが「PR レンジの先頭 1 行だけ」を見る実装（`git log --format="%B" origin/main..HEAD | head -1`）で、実質上 **最新コミットの subject 1 行のみ** をチェックしており、
  - そもそも PR 全体をチェックできていない（履歴中に違反コミットが混ざっても見逃す）
  - `fetch-depth` や `origin/main` 解決失敗時にサイレントに誤判定する余地がある
  - `perf` タイプ（`pull_request_template.md` の選択肢には載っている）を受け付けない
    という構造的な穴がある。PR テンプレート / Conventional Commits 標準にも合わせて堅くする。

## 影響範囲

### ソースコード（lint 修正）

- `pkg/ogp/fetcher.go`
- `pkg/ogp/fetcher_test.go`
- `pkg/ogp/parser.go`
- `pkg/authcore/cache.go`
- `internal/domain/post_test.go`
- `internal/usecase/timeline/usecase.go`
- `internal/usecase/follow/usecase.go`
- `internal/usecase/ogp/worker_test.go`
- `internal/usecase/post/hydrate.go`
- `internal/usecase/post/usecase.go`
- `internal/handler/handler.go`
- `internal/tagextractor/regex.go`

### 型名リネームに伴う連鎖修正（内部のみ・外部 API 破壊なし）

- `follow.FollowResult` → `follow.Result`
  - 参照: `internal/handler/follow.go`, `internal/usecase/follow/usecase.go`, `internal/usecase/follow/usecase_test.go`
- `follow.FollowUseCase` → `follow.UseCase`
  - 参照: `internal/handler/follow.go`（フィールド / コンストラクタ引数）, `cmd/server/main.go`（配線箇所があれば）
- `post.PostDetail` → `post.Detail`
  - 参照: `internal/handler/post.go`, `internal/usecase/timeline/usecase.go`, `internal/usecase/post/*.go`
- `post.PostCommitHook` → `post.CommitHook`
  - 参照: `internal/usecase/post/usecase.go`, `internal/usecase/ogp/*`（フック注入側があれば）, `cmd/server/main.go`

HTTP のリクエスト / レスポンス JSON スキーマや DB スキーマ、URL パス、環境変数は **一切変更しない**。破壊的変更は Go のパッケージ内部シンボル名のみで、バイナリ互換・API 互換への影響はない。

### CI / ワークフロー

- `.github/workflows/pr-review.yml`（コミットメッセージ検査ステップのロジックを作り直す）

## 前提 / 方針（デフォルト想定）

- ユーザー未回答の項目は以下のデフォルトで進める。反対意見があれば都度修正する:
  - **revive exported stutter**: API 変更を許容する。パッケージ内シンボルのみのリネームで、この PR のスコープに閉じる。
  - **unparam**: `RegexTagExtractor.Extract` 内部クロージャ `add` の `bool` 戻り値は未使用なので **削除** する（テストには影響しない）。`parseSubFromPath` の `name string` は常に `"sub"` で呼ばれているが、**引数は残しつつ `//nolint:unparam` で明示抑制** する（将来 `"followee_sub"` 等に拡張する余地を残すため — 気が変わったら引数を落とす）。
  - **goconst**: `pkg/ogp/` 配下で `"http"` が 3 箇所以上出るので、パッケージ内定数 `schemeHTTP` / `schemeHTTPS` を `pkg/ogp/normalizer.go` など既存の共通ファイルに定義する。
  - **errcheck (`resp.Body.Close()`)**: `defer func() { _ = resp.Body.Close() }()` で明示的に破棄（ログ不要、HTTP レスポンス Close の失敗は無視するのが一般的）。
  - **gocritic appendAssign**: `ulids5 := append(ulids4, ...)` / `authorSubs := append(followees, mySub)` は **新しい slice を作る意図** があり、元 slice の backing array を変更したくない。`make + copy + append` に書き換える（または `slices.Concat(followees, []string{mySub})`）。
  - **revive redefines-builtin-id (`copy`)**: `pkg/authcore/cache.go:73` の `copy := *call.session` はローカル変数。`sessionCopy` にリネーム。
  - **revive unused-parameter (`r *http.Request`)**: テスト中の HTTP ハンドラで `r` を使わないものは `_` にリネーム。

## 実装ステップ

### Phase 1: 非破壊的な lint 修正（リネーム不要）

**Step 1. `pkg/ogp/fetcher.go` の `resp.Body.Close` エラーを破棄**

`fetcher.go:120` を以下に変更:

```go
defer func() { _ = resp.Body.Close() }()
```

**Step 2. `pkg/ogp/parser.go` の `"http"` 文字列リテラルを定数化**

- `pkg/ogp/` 配下で `"http"` / `"https"` が 3 箇所（`parser.go`, `normalizer.go`, `safe_http.go`）に散っているので、共通の定数を追加する。
- 追加先: `pkg/ogp/normalizer.go` の末尾（schemes マップの近くに置く）、もしくは新規の `pkg/ogp/consts.go`（どちらでもよいが、ここでは既存ファイルに追記で済ませる）。

```go
const (
    schemeHTTP  = "http"
    schemeHTTPS = "https"
)
```

- `parser.go:201`, `normalizer.go:65`, `safe_http.go:162` のリテラル比較を `u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS` に置換。
- `normalizer.go:38-39` のマップキーは map リテラル側なので、素直に `schemeHTTP: "80", schemeHTTPS: "443"` に置換。

**Step 3. `internal/domain/post_test.go` の `appendAssign` 解消**

`ulids5 := append(ulids4, "01HXPST000000000000000EEEE")` を、`ulids4` の backing array を触らないように書き換える:

```go
ulids5 := make([]string, 0, len(ulids4)+1)
ulids5 = append(ulids5, ulids4...)
ulids5 = append(ulids5, "01HXPST000000000000000EEEE")
```

**Step 4. `internal/usecase/timeline/usecase.go` の `appendAssign` 解消**

`authorSubs := append(followees, mySub)` を同様に:

```go
authorSubs := make([]string, 0, len(followees)+1)
authorSubs = append(authorSubs, followees...)
authorSubs = append(authorSubs, mySub)
```

**Step 5. `internal/usecase/ogp/worker_test.go` の unused-parameter 解消**

`worker_test.go:62, 97, 121` の `http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {...})` で `r` を使っていない箇所を `_ *http.Request` に変更。

**Step 6. `pkg/ogp/fetcher_test.go` の unused-parameter 解消**

`fetcher_test.go:50, 66, 82, 98` で同様に `r` を `_` に変更。

**Step 7. `pkg/authcore/cache.go` の `copy` シャドウ解消**

`cache.go:73-74`:

```go
sessionCopy := *call.session
return &sessionCopy, nil
```

**Step 8. `internal/handler/handler.go:207` の `parseSubFromPath` unparam 抑制**

関数シグネチャ直前に `//nolint:unparam // name may vary when additional ULID path params are added`（or equivalent）を付ける。コメントは日本語でもよい。

> NOTE: もし「将来拡張する予定はない。引数を落として良い」と判断する場合は、`parseSubFromPath(w, r)` に変更し、7 箇所の呼び出し側（`internal/handler/follow.go` 4 箇所, `internal/handler/handler.go` 2 箇所, `internal/handler/badge.go` 2 箇所, `internal/handler/timeline.go` 1 箇所）を書き換える選択肢もある。こちらを選ぶ場合はこのステップを差し替える。

**Step 9. `internal/tagextractor/regex.go:71` の `add` クロージャの戻り値削除**

`add := func(name string) bool { ... return true }` の戻り値 `bool` はどこでも使われていないため、以下に変更:

```go
add := func(name string) {
    norm := strings.ToLower(strings.TrimSpace(name))
    if norm == "" {
        return
    }
    if len(norm) > domain.MaxTagNameLen {
        return
    }
    if _, dup := seen[norm]; dup {
        return
    }
    seen[norm] = struct{}{}
    out = append(out, norm)
}
```

呼び出し側 (`add(m[1])`, `add(kw.name)`) はそのまま動く（戻り値を受けていないため）。

### Phase 2: スタッター解消のためのリネーム（内部 API 変更）

**Step 10. `follow.FollowResult` → `follow.Result`**

- `internal/usecase/follow/usecase.go` で `FollowResult` 構造体を `Result` にリネーム。
- 同ファイル内の戻り値型 (`*FollowResult`) を `*Result` に置換。
- 参照箇所を grep して `followusecase.FollowResult` / `follow.FollowResult` → `followusecase.Result` / `follow.Result` に置換:
  - `internal/handler/follow.go`
  - `internal/usecase/follow/usecase_test.go`

**Step 11. `follow.FollowUseCase` → `follow.UseCase`**

- `internal/usecase/follow/usecase.go` で `FollowUseCase` 構造体・コンストラクタ `NewFollowUseCase` をリネーム。
  - `type FollowUseCase struct` → `type UseCase struct`
  - `func NewFollowUseCase(...)` → `func NewUseCase(...)`
  - メソッド `(uc *FollowUseCase) Execute` → `(uc *UseCase) Execute`
- 参照箇所を置換:
  - `internal/handler/follow.go`（フィールド `follow *followusecase.FollowUseCase` → `*followusecase.UseCase`、コンストラクタ引数も同様）
  - `cmd/server/main.go`（`followusecase.NewFollowUseCase(...)` があればここも）
  - テスト (`internal/usecase/follow/usecase_test.go`)

> NOTE: 同パッケージには `UnfollowUseCase`, `ListFollowersUseCase`, `ListFollowingUseCase` もあるが、これらは `unfollow.` / `listfollowers.` のような独立パッケージになっていないので `Follow*` とのスタッターのみが lint で検出されている。他の `*UseCase` 名は残して OK（revive も警告を出していない）。

**Step 12. `post.PostDetail` → `post.Detail`**

- `internal/usecase/post/hydrate.go` で `PostDetail` → `Detail`。
- 同パッケージ内の全参照 (`*PostDetail`, `[]*PostDetail`, `&PostDetail{}`) を `Detail` に置換:
  - `internal/usecase/post/usecase.go`
  - `internal/usecase/post/hydrate.go`
- パッケージ外参照:
  - `internal/usecase/timeline/usecase.go` の `[]*postusecase.PostDetail` → `[]*postusecase.Detail`
  - `internal/handler/post.go` の `*postusecase.PostDetail` → `*postusecase.Detail`（`toPostDetailView` のシグネチャ内等）
- テストファイル (`internal/usecase/post/*_test.go`, `internal/usecase/timeline/*_test.go`, `internal/handler/*_test.go`) も grep して置換。

**Step 13. `post.PostCommitHook` → `post.CommitHook`**

- `internal/usecase/post/usecase.go` の型 `PostCommitHook` とメソッド `WithPostCommitHook` をリネーム。
  - `type PostCommitHook func(...)` → `type CommitHook func(...)`
  - `onCommit PostCommitHook` → `onCommit CommitHook`
  - `func (uc *CreatePostUseCase) WithPostCommitHook(hook PostCommitHook)` → `WithCommitHook(hook CommitHook)`
- 参照箇所:
  - `internal/usecase/ogp/` 配下で OGP enqueuer を post commit hook として配線している箇所 (`internal/usecase/ogp/enqueue.go` 等があれば)
  - `cmd/server/main.go`
  - 関連テスト

### Phase 3: `make lint` でローカル検証

**Step 14. `make lint` の実行**

- `make setup` 済みでなければ `make setup` で `golangci-lint` をインストール（CI と同じ `v2.11.4` が望ましいが、`Makefile` は最新を入れる）。
- `make lint` を実行し、18 件の違反がすべて消えていることを確認。
- `make test` / `go vet ./...` / `gofmt -s -l .` が新規差分を生まないことも確認。

### Phase 4: PRAgent の再実装

**Step 15. `.github/workflows/pr-review.yml` のコミットメッセージ検査を全コミット対象に修正**

現状:

```yaml
COMMIT_MSG=$(git log --format="%B" origin/main..HEAD 2>/dev/null | head -1)
if [ -z "$COMMIT_MSG" ]; then
  echo "invalid=false" >> $GITHUB_OUTPUT
elif ! echo "$COMMIT_MSG" | grep -E "^(feat|fix|docs|test|refactor|chore|add|ci)(\(.+\))?:" >/dev/null 2>&1; then
  echo "invalid=true" >> $GITHUB_OUTPUT
else
  echo "invalid=false" >> $GITHUB_OUTPUT
fi
```

問題点:

1. `head -1` が「範囲全体の出力の先頭 1 行」を取るため、**PR の最新コミット subject しか見ていない**。古いコミットに違反があっても素通り。
2. `origin/main` が解決できないと silent に「違反なし」扱いになる（`2>/dev/null` と空分岐）。fail してほしいケースで気づかない。
3. `head -1` の結果が空でもなく、改行を含む（`%B` は本文込み）ので、multiline な commit で 1 行目以外がマッチ対象になる可能性がある（実際には先頭だけ見るので OK だが、意図が分かりにくい）。
4. 許可タイプが `perf` を含まない（PR テンプレートには `perf` がある）。
5. ベースブランチが `develop` の PR だと `origin/main` と比較してしまい範囲が誤る。

修正案:

```yaml
- name: Check commit message format
  id: commit-format
  env:
    BASE_REF: ${{ github.event.pull_request.base.ref }}
  run: |
    set -e
    # Ensure the base ref is fetched; fail loud if it cannot be resolved.
    git fetch origin "${BASE_REF}" --depth=0 || git fetch origin "${BASE_REF}"

    # Enumerate subject lines for each commit in the PR range, one per line.
    # %s is the subject (first line only), so we do not have to deal with %B bodies.
    mapfile -t SUBJECTS < <(git log --format=%s "origin/${BASE_REF}..HEAD")

    if [ "${#SUBJECTS[@]}" -eq 0 ]; then
      echo "No commits in PR range; skipping format check"
      echo "invalid=false" >> "$GITHUB_OUTPUT"
      exit 0
    fi

    PATTERN='^(feat|fix|docs|test|refactor|chore|add|ci|perf|style|build)(\([^)]+\))?(!)?: .+'
    BAD=()
    for subject in "${SUBJECTS[@]}"; do
      if ! printf '%s' "$subject" | grep -Eq "$PATTERN"; then
        BAD+=("$subject")
      fi
    done

    if [ "${#BAD[@]}" -gt 0 ]; then
      {
        echo "invalid=true"
        echo "bad_subjects<<EOF"
        printf '%s\n' "${BAD[@]}"
        echo "EOF"
      } >> "$GITHUB_OUTPUT"
    else
      echo "invalid=false" >> "$GITHUB_OUTPUT"
    fi
```

- ベース ref を明示 (`github.event.pull_request.base.ref`) して `main` / `develop` 両対応。
- `%s` で subject のみ取得。全コミット subject を検査。
- 許可タイプに `perf`, `style`, `build` を追加（Conventional Commits 標準 + PR テンプレに合わせる）。`!` 付きの破壊的変更マーカーも許容。
- scope の内側は `(` `)` を含めない (`[^)]+`)。
- type の後ろは必ずスペース 1 文字 + 説明が続くことを要求 (`: .+`)。

**Step 16. 違反時のコメント内容に違反コミット subject を含める**

既存の "Comment on commit format" ステップのボディに、検出した違反 subject を載せる（デバッグ性を上げる）。

```yaml
- name: Comment on commit format
  if: steps.commit-format.outputs.invalid == 'true'
  uses: actions/github-script@v7
  env:
    BAD_SUBJECTS: ${{ steps.commit-format.outputs.bad_subjects }}
  with:
    script: |
      const bad = process.env.BAD_SUBJECTS || '';
      const list = bad.split('\n').filter(Boolean).map(s => `- \`${s}\``).join('\n');
      const body = [
        '### ❌ コミットメッセージ形式のエラー',
        '',
        'Conventional Commits 形式に従ってください：',
        '```',
        'type(scope): 説明',
        '',
        'Body（オプション）',
        'Footer（オプション）',
        '```',
        '',
        'Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `add`, `ci`, `perf`, `style`, `build`',
        '',
        '**違反コミット:**',
        list || '(不明)',
      ].join('\n');
      github.rest.issues.createComment({
        issue_number: context.issue.number,
        owner: context.repo.owner,
        repo: context.repo.repo,
        body,
      });
```

**Step 17. PR テンプレート `.github/pull_request_template.md` とタイプ一覧を揃える**

- テンプレートに現在ある `perf` は workflow 側にもある状態にしたので OK。
- workflow 側で増やした `style`, `build` をテンプレートにも追加するかは任意（気にしなければテンプレートはそのまま）。

### Phase 5: 検証

**Step 18. ローカル検証**

- `make lint` が 0 件で通る。
- `make test` が通る。
- `go vet ./...` / `gofmt -s -l .` が空。

**Step 19. CI 確認**

- push して CI の `lint` ジョブが green であることを確認。
- PRAgent（`AI-Powered PR Review` workflow）の commit format チェックが誤検知しないこと（今回のブランチ履歴は全コミット Conventional）を確認。
- 意図的に bad subject を混ぜた検証は別 PR でやる（本 PR のスコープ外）。

## テスト要件

- 既存テスト（`go test -race ./...`）が全て pass。
- 今回の変更はリネームと lint 違反解消のみなので、**新規テストは追加しない**。
- ただし `internal/tagextractor/regex.go` の `add` クロージャ戻り値削除は挙動変化なしなので既存テストで十分（`regex_test.go` に case がある）。

## 技術的な補足

### スタッター解消のスコープ

- 今回 revive が指摘した 4 型 (`FollowResult`, `FollowUseCase`, `PostDetail`, `PostCommitHook`) のみリネームする。他の `CreatePostUseCase` / `GetPostUseCase` / `UnfollowUseCase` 等にも同種のスタッターは存在するが、**revive が指摘していないため触らない**。
  - 理由: スコープを広げると PR が肥大化し、依存タスクや進行中ブランチとのコンフリクトが増える。revive の判定基準は「同パッケージ内に同種の型が複数ある / exported 型の先頭が package 名と一致する」などの閾値があるので、指摘されたものだけ直すので十分。

### `//nolint` の扱い

- 基本は lint 違反を「直す」のが望ましい。`//nolint` は `parseSubFromPath` のように意図的に将来拡張用の引数を残したいケースに限定する。
- `//nolint` を使う場合は必ず理由をコメントで併記 (`//nolint:unparam // reason: ...`)。

### PRAgent のベースブランチ解決

- `github.event.pull_request.base.ref` は PR コンテキストでは必ず存在する。`push` イベントでは存在しないが、`pr-review.yml` は `pull_request` のみにトリガーされるので問題ない。
- `fetch-depth: 0` はすでに上段 (`actions/checkout@v4`) で指定済みなので、`git fetch origin $BASE_REF` は redundant だが、PR が進むにつれてベース ref が更新されるケースに備えて残す。

### 破壊的変更の有無

- **なし**（HTTP API / DB / バイナリ互換すべて保持）。
- Go パッケージの exported シンボル 4 件の改名のみ。本リポジトリ以外からインポートされていないことを `grep` で確認済み。

## 参考

- golangci-lint v2 migration: https://golangci-lint.run/product/migration-guide/
- Conventional Commits: https://www.conventionalcommits.org/
- revive `exported` ルール: https://github.com/mgechev/revive/blob/master/RULES_DESCRIPTIONS.md#exported
