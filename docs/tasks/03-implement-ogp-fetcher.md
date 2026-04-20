# Implement OGP Fetcher

## 概要

投稿本文に含まれる URL から OGP（Open Graph Protocol）メタ情報を取得し、投稿にプレビューカードを添付できるようにする。取得は **非同期ワーカー**（DB-backed job queue or in-process worker）で実行し、投稿レスポンスへの影響を最小化する。取得結果は PostgreSQL にキャッシュして再利用する。SSRF 対策は必須。

## 前提（依存タスク）

- **`02-implement-post-feature.md` 完了必須**
  - 投稿本文から URL を抽出する主体（Post）が存在すること
  - `Post` のレスポンス DTO 組み立てロジックがあること（OGP をぶら下げる場所）
  - Post 作成 usecase の **post commit 後フック点** が存在すること（02 の CreatePost Step 9）
- **`01-rewrite-domain-to-authcore-alignment.md`** は厳密な前提ではないが、同時期に完了している想定
- 本タスクは `03-implement-follow-and-timeline.md` と **並行可能**（互いに依存しない）

## スコープ

### 含む

- 投稿作成時に本文から URL を抽出
- **非同期ワーカー** で OGP を取得（`og:title`, `og:description`, `og:image`, `og:site_name`, `og:url`）
- 取得結果を **PostgreSQL にキャッシュ**（URL 正規化 + SHA256 ハッシュをキーに）
- 投稿取得 API のレスポンスに OGP プレビューを含める
- **SSRF 対策**: プライベート IP への取得禁止、リダイレクト回数制限、レスポンスサイズ上限、タイムアウト
- タイムライン等でのバッチ取得（N+1 回避）

### 含まない（スコープ外）

- `og:image` の R2 ミラーリング（ホットリンク対応は別タスク）
- Redis や外部 job queue の導入（MVP は DB-backed / in-process）
- URL 短縮サービスの展開
- 動画 / iframe 埋め込み（YouTube 等）
- 複数言語の OGP（`og:locale` の扱い）

## 確定仕様サマリ

| 項目 | 確定内容 |
|---|---|
| **キャッシュストレージ** | **PostgreSQL**（`ogp_cache` テーブル）。MVP は DB で十分。Redis 導入は将来 |
| **キャッシュ TTL（成功時）** | **3 日** |
| **キャッシュ TTL（エラー時）** | **30 分**（短期。再試行誘発しつつスパム時の過剰アクセスを抑制） |
| **同期 vs 非同期** | **非同期**（DB-backed job queue）。投稿レスポンスへの影響を切り離す |
| **job queue 実装** | **DB-backed（`ogp_jobs` テーブル） + 単一の in-process worker goroutine**。将来 Redis Streams / RabbitMQ に切り替え可能な interface 設計 |
| **トランザクション境界** | **Post commit 後に best-effort で enqueue**。Post 作成トランザクションには含めない。enqueue 失敗はログのみで Post 作成は常に成功扱い |
| **複数 URL があった場合** | **先頭 1 つのみ OGP 表示**（Twitter 風）。将来ユーザー設定で増やせるよう post_ogp は多対多で設計 |
| **リクエストヘッダの User-Agent** | **config 値化**（env `OGP_USER_AGENT` で注入。default: `FujuBot/1.0 (+https://fuju.example.com/bot)`） |
| **取得タイムアウト** | 5 秒（合計） |
| **レスポンスサイズ上限** | 5 MB |
| **リダイレクト上限** | 5 回 |
| **末尾スラッシュ** | **同一視**（`/path/sub` と `/path/sub/` を同じ url_hash に正規化。ただし root `/` は残す） |

## 設計判断

### なぜ非同期にするか

- 投稿作成 API のレイテンシに外部サイトの応答が乗ると UX が悪化
- 外部サイトの 5xx / タイムアウトが投稿失敗に直結してしまう
- 非同期なら投稿は即座に成功し、OGP は「遅れて付く」挙動になる（これは Twitter / Bluesky も同様）

### post commit 後 best-effort enqueue

- `CreatePostUseCase` の中で、**Post の INSERT / 関連テーブル書き込みをコミット後** に enqueue する
- enqueue は `ogpJobQueue.Enqueue(ctx, ...)` の単純呼び出し。エラーが起きてもログに残すだけで Post 作成は成功扱い
- 実装パターン:
  ```go
  // Post commit 後
  if err := h.ogpEnqueuer.EnqueueForPost(ctx, post); err != nil {
      log.Warn("ogp enqueue failed (best-effort)", "post_id", post.ID, "err", err)
      // 返り値は返さない。Post 作成は成功を返す
  }
  ```
- Post 作成トランザクションに含めない理由: OGP job queue テーブルへの書き込み失敗が Post 作成の可否に影響してはならない。ユーザー視点では「OGP プレビューが後から付く / 付かないことがある」は許容範囲、「投稿自体に失敗する」は許容不可

### job queue を DB-backed にする理由

- Redis や外部 queue を増やさない
- 1 つの goroutine worker で十分さばける規模を想定
- Postgres の `SELECT ... FOR UPDATE SKIP LOCKED` で安全にジョブを取れる
- 将来のスケール時は `OGPJobQueue` interface を Redis Streams 実装に差し替え

### URL 正規化ルール

同一コンテンツの URL が微妙に違う形式で投稿されることが多く、正規化なしではキャッシュヒット率が極端に下がる。以下のルールを適用する:

1. **Scheme**: 小文字化（`HTTP` → `http`）
2. **Host**: 小文字化、末尾の `.` を除去
3. **Path**:
   - 連続スラッシュは 1 つに圧縮
   - **末尾スラッシュを削除**（`/path/sub/` → `/path/sub`。ただし root `/` はそのまま残す）
4. **Port**: デフォルトポート（80/443）は削除
5. **Fragment** (`#...`): 削除
6. **Query**:
   - キー名でソート
   - **トラッキングパラメータを除去**: `utm_source`, `utm_medium`, `utm_campaign`, `utm_term`, `utm_content`, `gclid`, `fbclid`, `ref`, `mc_cid`, `mc_eid`
7. **punycode**: 非 ASCII ホスト名は punycode 化

例:
```
入力:  HTTPS://Example.COM:443/path//sub/?utm_source=tw&b=2&a=1#frag
出力:  https://example.com/path/sub?a=1&b=2

入力:  https://example.com/
出力:  https://example.com/           (root のスラッシュは保持)
```

キャッシュキーは正規化後 URL の SHA256 hex 文字列（`CHAR(64)`）。

### SSRF 対策

- HTTP クライアントの `DialContext` を差し替え、名前解決結果の IP が以下に該当したら接続拒否:
  - ループバック (`127.0.0.0/8`, `::1/128`)
  - リンクローカル (`169.254.0.0/16`, `fe80::/10`)
  - プライベート (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`)
  - AWS メタデータ (`169.254.169.254`)
  - ブロードキャスト / 未指定 / マルチキャスト
- リダイレクトの都度、リダイレクト先 URL を再チェック（CheckRedirect で同じバリデータを通す）
- レスポンスサイズは `http.MaxBytesReader(..., 5MB)` で打ち切り
- スキームは `http`/`https` のみ許可

## Domain モデル定義

```go
// internal/domain/ogp.go

type OGPPreview struct {
    URLHash      string    // SHA256 hex (正規化後 URL)
    URL          string    // 正規化後 URL
    Title        string
    Description  string
    ImageURL     string
    SiteName     string
    CanonicalURL string    // og:url があれば
    FetchedAt    time.Time
    ExpiresAt    time.Time
    Status       string    // "ok" / "error" / "pending"
    ErrorReason  string    // 失敗時のメモ
}

type OGPJob struct {
    ID          string    // ULID
    URLHash     string
    URL         string
    PostID      string    // 紐付く投稿（= 再取得時の通知先等に使える）
    EnqueuedAt  time.Time
    StartedAt   *time.Time
    FinishedAt  *time.Time
    Status      string    // "queued" / "running" / "done" / "failed"
    Attempts    int
    LastError   string
}
```

## DB Schema（Migration DDL）

**パス**: `db/migrations/007_implement_ogp_cache.sql`（3 桁連番・既存 `db/migrations/` 直下）

```sql
-- db/migrations/007_implement_ogp_cache.sql

CREATE TABLE ogp_cache (
    url_hash      CHAR(64) PRIMARY KEY,          -- SHA256 hex of normalized URL
    url           VARCHAR(2048) NOT NULL,        -- 正規化後 URL（デバッグ用に保持）
    title         VARCHAR(512) NOT NULL DEFAULT '',
    description   VARCHAR(1024) NOT NULL DEFAULT '',
    image_url     VARCHAR(2048) NOT NULL DEFAULT '',
    site_name     VARCHAR(255)  NOT NULL DEFAULT '',
    canonical_url VARCHAR(2048) NOT NULL DEFAULT '',
    fetched_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    status        VARCHAR(16)  NOT NULL DEFAULT 'ok',  -- ok/error/pending
    error_reason  VARCHAR(255) NOT NULL DEFAULT ''
);

CREATE INDEX idx_ogp_cache_expires_at ON ogp_cache(expires_at);

-- 投稿と OGP プレビューの関連（多対多）
-- 一投稿が複数 URL を含んでも対応できるようにしておく（MVP 表示は先頭 1 つ）
CREATE TABLE post_ogp (
    post_id  CHAR(26) NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    url_hash CHAR(64) NOT NULL REFERENCES ogp_cache(url_hash) ON DELETE RESTRICT,
    position SMALLINT NOT NULL,
    PRIMARY KEY (post_id, url_hash),
    UNIQUE (post_id, position)
);
CREATE INDEX idx_post_ogp_post_id ON post_ogp(post_id);

-- ジョブキュー
CREATE TABLE ogp_jobs (
    id           CHAR(26) PRIMARY KEY,
    url_hash     CHAR(64) NOT NULL,
    url          VARCHAR(2048) NOT NULL,
    post_id      CHAR(26) NULL REFERENCES posts(id) ON DELETE SET NULL,
    enqueued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at   TIMESTAMPTZ NULL,
    finished_at  TIMESTAMPTZ NULL,
    status       VARCHAR(16) NOT NULL DEFAULT 'queued', -- queued/running/done/failed
    attempts     INT         NOT NULL DEFAULT 0,
    last_error   VARCHAR(512) NOT NULL DEFAULT ''
);
CREATE INDEX idx_ogp_jobs_status_enqueued_at ON ogp_jobs(status, enqueued_at);
```

## Repository インターフェース

```go
// internal/repository/repository.go に追加

type OGPCacheRepository interface {
    Get(ctx context.Context, urlHash string) (*domain.OGPPreview, error)            // nil なら miss
    Upsert(ctx context.Context, preview *domain.OGPPreview) error
    // タイムライン等で post 群の OGP を一括取得
    ListByPostIDs(ctx context.Context, postIDs []string) (map[string][]*domain.OGPPreview, error)
}

type OGPJobQueue interface {
    Enqueue(ctx context.Context, urlHash, url, postID string) error
    // FOR UPDATE SKIP LOCKED で 1 件取得（取ったジョブは status=running にする）
    Claim(ctx context.Context, workerID string) (*domain.OGPJob, error)
    MarkDone(ctx context.Context, jobID string) error
    MarkFailed(ctx context.Context, jobID string, reason string, retriable bool) error
}
```

## パッケージ設計

```
pkg/ogp/
├── normalizer.go        // URL 正規化
├── normalizer_test.go
├── extractor.go         // 本文からの URL 抽出
├── extractor_test.go
├── safe_http.go         // SSRF-safe HTTP client（DialContext 差し替え）
├── safe_http_test.go
├── parser.go            // HTML パース（og:* / twitter:* / <title> / meta description）
├── parser_test.go
├── fetcher.go           // 正規化 → safe_http → parser のオーケストレーション
└── fetcher_test.go

internal/usecase/ogp/
├── enqueue.go           // 投稿作成 commit 後に URL を抽出して enqueue（best-effort）
├── worker.go            // goroutine worker。Claim → Fetch → Upsert → MarkDone
└── worker_test.go
```

## Usecase の主要フロー

### 投稿作成からの enqueue（post commit 後 best-effort）

```
CreatePost の commit 後フック（02 の CreatePost Step 9）:

  urls = ogp.ExtractURLs(post.Content)        // 本文から URL 抽出（max 5 個程度）
  for each url in urls:
      normalized = ogp.Normalize(url)
      urlHash    = sha256hex(normalized)
      preview    = ogpCacheRepo.Get(ctx, urlHash)
      if preview != nil && preview.ExpiresAt > now:
          // キャッシュヒット: 即 post_ogp に紐付け
          postRepo.AttachOGP(ctx, post.ID, urlHash, position)
      else:
          // キャッシュなし or 期限切れ: ジョブ enqueue
          err := ogpJobQueue.Enqueue(ctx, urlHash, normalized, post.ID)
          if err != nil:
              log.Warn("ogp enqueue failed", ...)   // best-effort、投稿は成功扱い
```

### Worker ループ

```
for {
  job = ogpJobQueue.Claim(ctx, workerID)
  if job == nil:
      sleep(1s); continue
  preview, err = fetcher.Fetch(ctx, job.URL)
  if err != nil:
      if retriable(err) && job.Attempts < 3:
          ogpJobQueue.MarkFailed(ctx, job.ID, err.Error(), retriable=true)  // 再キュー
      else:
          ogpCacheRepo.Upsert(ctx, &OGPPreview{
             URLHash: job.URLHash, URL: job.URL,
             Status: "error", ErrorReason: err.Error(),
             FetchedAt: now, ExpiresAt: now + 30m,   // エラー時は短期キャッシュ
          })
          ogpJobQueue.MarkFailed(ctx, job.ID, err.Error(), retriable=false)
      continue
  preview.ExpiresAt = now + 3d                        // 成功時 TTL = 3 日
  preview.Status    = "ok"
  ogpCacheRepo.Upsert(ctx, preview)
  if job.PostID != "":
      postRepo.AttachOGP(ctx, job.PostID, preview.URLHash, position=0 /*または追記位置*/)
  ogpJobQueue.MarkDone(ctx, job.ID)
}
```

### Fetcher 本体

```
func Fetch(ctx, rawURL) (*OGPPreview, error):
  normalized := Normalize(rawURL)
  ctx = timeout(ctx, 5s)
  req = NewRequest("GET", normalized, UserAgent=cfg.OGPUserAgent, Accept-Language: "en, ja")
  resp, err = safeClient.Do(req)
  if err != nil: return nil, err
  defer resp.Body.Close()
  if resp.StatusCode != 200: return nil, fmt.Errorf("status %d", resp.StatusCode)
  ct := resp.Header.Get("Content-Type")
  if !isHTML(ct): return nil, ErrNotHTML
  body = http.MaxBytesReader(resp.Body, 5MB)
  meta, err := Parse(body)
  return &OGPPreview{
    URLHash: sha256hex(normalized), URL: normalized,
    Title: meta.Title, Description: meta.Description,
    ImageURL: meta.ImageURL, SiteName: meta.SiteName,
    CanonicalURL: meta.CanonicalURL, FetchedAt: now,
  }, nil
```

### Parser

- `golang.org/x/net/html` でパース（net/http は標準パッケージに含まれる、net/html は準標準）
- 優先順:
  1. `<meta property="og:*">`
  2. `<meta name="twitter:*">`（fallback）
  3. `<title>` / `<meta name="description">`（最後の fallback）
- `og:image` は **絶対 URL 化**（相対 URL なら `base URL` と結合）

### アプリケーション層のキャッシュ（タイムラインでの付与）

`03-implement-follow-and-timeline.md` の `hydratePostDetails` に `ogpCacheRepo.ListByPostIDs` を追加:

```
ogpByPost = ogpCacheRepo.ListByPostIDs(ctx, postIDs)  // 1 クエリ
for each post: post.OGPPreviews = ogpByPost[post.ID]
```

投稿詳細（`GET /posts/{id}`）でも同様にバッチ取得（N=1 でも同じ関数を使う）。

## 実装ステップ順

1. **Migration**（`db/migrations/007_implement_ogp_cache.sql`）
2. **Domain 層**: `internal/domain/ogp.go`
3. **`pkg/ogp/` 実装**:
   - `normalizer.go`（純関数。まず単体テスト中心で書く。末尾スラッシュは削除、root `/` は保持）
   - `extractor.go`（regex ベース）
   - `safe_http.go`（SSRF 対策の `DialContext`）
   - `parser.go`（og:* / twitter:* / title / meta description）
   - `fetcher.go`（上記を統合）
4. **Repository 層**:
   - `OGPCacheRepository` / `OGPJobQueue` interface + inmemory 実装
   - `PostRepository.AttachOGP` を `02` の repo に追加
5. **Usecase 層**:
   - `internal/usecase/ogp/enqueue.go`（投稿作成 commit 後フック、best-effort）
   - `internal/usecase/ogp/worker.go`（Claim → Fetch → Upsert）
6. **投稿作成フローへのフック**:
   - `02` の `CreatePostUseCase` の commit 後に observer / hook を挿す（OGP enqueue を呼ぶ。失敗はログのみ）
7. **Handler / Response への反映**:
   - `hydratePostDetails` に OGP のバッチ取得を追加（`03-follow-and-timeline` と同一のヘルパ）
8. **cmd/server/main.go**:
   - OGP worker を goroutine で起動（graceful shutdown と連動させる）
   - **`OGP_USER_AGENT` を config から読み取り、`pkg/ogp` に DI**（default: `FujuBot/1.0 (+https://fuju.example.com/bot)`）
9. **テスト**

## テスト方針

### 単体テスト

- `pkg/ogp/normalizer_test.go`:
  - スキーマ / ホスト大文字小文字 / 末尾スラッシュ削除（root `/` は保持）/ ポート / fragment / utm_* 除去 / クエリソート
  - `/path/sub` と `/path/sub/` が同じ url_hash になること
  - テーブル駆動で網羅
- `pkg/ogp/extractor_test.go`:
  - 複数 URL / URL の前後に記号 / 日本語を含む URL / スキームなしはスキップ
- `pkg/ogp/safe_http_test.go`:
  - `127.0.0.1` / `10.0.0.1` / `169.254.169.254` / `fc00::` への接続が拒否
  - リダイレクトが private IP を向いた場合も拒否
- `pkg/ogp/parser_test.go`:
  - `og:*` 優先、無ければ `twitter:*`、無ければ `<title>` + `<meta name="description">`
  - 相対 URL の `og:image` 解決
  - 不正 HTML でもクラッシュしない
- `pkg/ogp/fetcher_test.go`:
  - `httptest.Server` でタイトル付きの HTML を返す → 正常系
  - 5MB 超 → 打ち切り error
  - 5 秒超 → timeout error
  - 5xx → error
  - **User-Agent ヘッダに config 値が乗っていること**
- `internal/usecase/ogp/worker_test.go`:
  - 正常系: Claim → Fetch → Upsert(ExpiresAt=+3d) → MarkDone
  - Fetch 失敗 + retriable → MarkFailed（再キュー）
  - Fetch 失敗 + non-retriable → エラーキャッシュ（ExpiresAt=+30m）+ MarkFailed
- `internal/usecase/post/createpost_test.go`（02 と相乗り）:
  - enqueue 失敗時も Post 作成が成功を返すこと

### 統合テスト

- `cmd/server/` smoke test:
  - 投稿作成 → 1 秒待つ → `GET /posts/{id}` で `ogp_previews` が返る
  - OGP サイト側は `httptest.Server` でモック

## 運用メモ

- **Worker の shutdown**: main.go で `signal.NotifyContext(os.Interrupt)` を受け、context cancel で worker を停止させる
- **ジョブの再キュー間隔**: MVP は実装せず「failed の行はそのまま残す」。手動再キューで十分
- **Graceful degradation**: OGP が付かなくても投稿は成立する。フロントは preview なしで描画できる必要がある
- **キャッシュ TTL のメンテ**: 期限切れ行は worker が再取得時に上書きする。定期クリーンアップは別タスク

## スコープ外（明示）

- `og:image` の R2 ミラーリング（ホットリンク対応）
- Redis / 外部 job queue（RabbitMQ, NATS, SQS）への移設
- URL 短縮サービスの展開（`t.co`, `bit.ly` 等）
- 動画 / iframe 埋め込み（YouTube, Vimeo 等のプレビュー）
- 投稿編集時の OGP 再取得（編集機能自体が 02 でスコープ外）
- CSP / referrer policy の厳密設定
- OGP プレビューのユーザー側無効化設定
