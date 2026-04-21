# Fix OGP attach position for multi-URL posts

## 概要

`03-implement-ogp-fetcher.md` の follow-up。投稿本文に複数 URL が含まれる場合に、OGP worker が常に `position=0` で `AttachOGP` を呼んでしまい、`UNIQUE(post_id, position)` 制約で無音に落ちる問題を解消する。

## 前提（依存タスク）

- `03-implement-ogp-fetcher.md` 完了済み（`ogp_cache` / `post_ogp` / `ogp_jobs` テーブル、`OGPJobQueue` interface、`Enqueuer` / `Worker` 実装）

## 背景

### 現状の挙動

- `internal/usecase/ogp/enqueue.go:55-87` — `EnqueueForPost` は本文から抽出した URL を先頭から順に `position = 0, 1, 2, ...` で処理する:
  - キャッシュヒット時: `posts.AttachOGP(ctx, postID, urlHash, position)` で **実 position** で attach
  - キャッシュミス時: `queue.Enqueue(ctx, id, urlHash, normalized, postID)` で job を投入（**position は job に乗らない**）
- `internal/usecase/ogp/worker.go:106` — worker が fetch 完了後、`posts.AttachOGP(ctx, job.PostID, preview.URLHash, 0)` と **position=0 ハードコード** で attach
- `internal/domain/ogp.go:42-53` — `OGPJob` 構造体に `Position` フィールドがない
- `db/migrations/007_implement_ogp_cache.sql:188-200` — `ogp_jobs` テーブルに `position` カラムがない

### 問題シナリオ

`post_ogp` の `UNIQUE(post_id, position)` 制約 + inmemory 実装の first-writer-wins セマンティクス:

| ケース | URL0 | URL1 | 結果 |
|---|---|---|---|
| 両方キャッシュヒット | attach@0 | attach@1 | ✅ 両方 attach |
| 両方キャッシュミス | queue → attach@0 | queue → attach@0 | ❌ 後勝ち（worker 順序依存）で 1 個だけ残る |
| URL0 ミス / URL1 ヒット | queue → attach@0 | attach@1 | ✅ 両方 attach |
| URL0 ヒット / URL1 ミス | attach@0 | queue → attach@0 | ❌ URL1 の attach が無音に落ちる |

MVP 表示は **先頭 1 つのみ**（`03` 計画 L46）なので通常ユースでは隠蔽されるが:
- キャッシュ状態次第で「表示される URL が先頭と一致しないケース」が発生する
- タイムライン全体 (`ListByPostIDs`) で preview 順が乱れる
- 将来 "先頭 N 件表示" に拡張する時点で表面化する

## スコープ

### 含む

- `OGPJob` ドメインモデルに `Position int` を追加
- `ogp_jobs` テーブルに `position SMALLINT NOT NULL DEFAULT 0` カラムを追加（新規 migration）
- `OGPJobQueue.Enqueue` シグネチャに `position` を追加（既存呼び出し側 2 箇所を修正）
- `Enqueuer.EnqueueForPost` が `position` を job に渡す
- `Worker.process` が `job.Position` で attach
- inmemory 実装の追従
- テストの更新（既存 `enqueue_test` / `worker_test`）
- **マルチ URL のシナリオテストを 1 ケース追加**（position=0, 1 がそれぞれ正しく attach されることを確認）

### 含まない

- OGP プレビューの "先頭 N 件表示" 対応（表示は引き続き先頭 1 つのみ）
- Redis / 外部 queue への切り替え
- 既にキューに残っている古い `ogp_jobs` レコードの backfill（`DEFAULT 0` で運用上は害がない前提）

## 実装ステップ

### Step 1. Migration 追加

ファイル: `db/migrations/008_add_ogp_job_position.sql`

```sql
ALTER TABLE ogp_jobs
  ADD COLUMN position SMALLINT NOT NULL DEFAULT 0;
```

- `NOT NULL DEFAULT 0` で既存行を無害にマイグレーション
- インデックス不要（Claim は `status, enqueued_at` で走査）

### Step 2. Domain モデル拡張

`internal/domain/ogp.go`:

```go
type OGPJob struct {
    ID         string
    URLHash    string
    URL        string
    PostID     string
    Position   int          // ← 追加
    EnqueuedAt time.Time
    ...
}
```

### Step 3. Repository interface 拡張

`internal/repository/repository.go`:

```go
type OGPJobQueue interface {
    Enqueue(ctx context.Context, id, urlHash, url, postID string, position int) error
    ...
}
```

### Step 4. inmemory 実装の追従

`internal/repository/inmemory/inmemory.go`:

- `OGPJobQueue.Enqueue` 実装を `position` 受け取りに変更、`OGPJob` に格納
- `Claim` は既存ロジックのまま（Position を読み出すだけ）

### Step 5. Enqueuer 修正

`internal/usecase/ogp/enqueue.go:84`:

```go
if err := e.queue.Enqueue(ctx, id, urlHash, normalized, post.ID, position); err != nil {
```

### Step 6. Worker 修正

`internal/usecase/ogp/worker.go:106`:

```go
if attachErr := w.posts.AttachOGP(ctx, job.PostID, preview.URLHash, job.Position); attachErr != nil {
```

### Step 7. テスト更新

- `internal/usecase/ogp/worker_test.go` — 既存 fixture の `Enqueue` 呼び出しに `position` 引数を追加
- `internal/usecase/ogp/enqueue_test.go` が無ければ簡易なマルチ URL ケースを追加:
  - 投稿に 2 URL、両方ミス → 2 jobs が別 position で enqueue される
  - worker が両方 process 後、`post_ogp` に 2 行 (position=0, 1) attach される

### Step 8. 検証

- `make lint` 通過
- `go test -race ./...` 通過
- 既存テストが壊れていないこと

## 影響範囲

- **破壊的変更なし**（HTTP API / DB スキーマは拡張のみ。既存列は無変更）
- `OGPJobQueue.Enqueue` のシグネチャ変更は内部 interface のみ。呼び出し側は本リポジトリ内 1 箇所 (`Enqueuer`) + テスト + main.go の配線なし（`Enqueuer` が内部で呼ぶので外部配線は影響なし）

## テスト方針

- 単体: enqueuer のマルチ URL ケース（position が 0, 1, ... で渡ること）
- 単体: worker の `job.Position` 反映（fixture を拡張）
- 統合: 既存の post 作成 → hydrate 経路で preview 順序が保たれること

## 参考

- コードレビュー指摘元: `04-fix-ci-lint-and-pragent.md` 完了後のレビュー Warning 1 件
- 該当コード:
  - `internal/usecase/ogp/worker.go:106`
  - `internal/usecase/ogp/enqueue.go:55-87`
  - `internal/domain/ogp.go:42-53`
  - `db/migrations/007_implement_ogp_cache.sql:188-200`
