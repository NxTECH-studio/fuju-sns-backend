# Wire PostgreSQL repository layer (production DB backing)

## 概要

`internal/repository/` に現状 in-memory 実装しか存在しない状態を脱し、本番 DB（PostgreSQL）向けの実装を新設してプロダクション投入可能な状態にする。`db/migrations/001〜008` で定義された本番スキーマに対応する Go 側の repository 実装を揃え、`cmd/server/main.go` で環境変数 `DATABASE_URL`（または既存の `DB_*` 環境変数群）経由で接続先を切り替えられるようにする。

## 前提（依存タスク）

- `01-rewrite-domain-to-authcore-alignment.md` 完了済み（ULID / `users` スキーマ確定）
- `02-implement-post-feature.md` / `02-implement-badge.md` 完了済み（migrations 004 / 005）
- `03-implement-follow-and-timeline.md` / `03-implement-ogp-fetcher.md` 完了済み（migrations 006 / 007）
- `05-fix-ogp-attach-position.md` 完了済み（migration 008）
- `06-frontend-handoff-docs-and-dead-code-removal.md` 完了済み（swagger が正、dead code クリア後）
- `07-fix-ci-go-version-and-lint-hygiene.md` 完了済み（**CI が緑でないと本タスクの PR も merge が進まない**）
- PR base は **develop**

## 背景・目的

### 現状

- `internal/repository/repository.go` に以下 **10 種** の interface が定義済み（ユーザー事前調査で「11 種」としていたが、実際は `BadgeRepository` が user_badges 操作を包含しているため独立 `UserBadgeRepository` は存在しない。`TimelineRepository` も存在せず、タイムライン取得は `PostRepository.List` / `PostRepository.ListByUserIDs` の組み合わせで実現されている）:
  1. `UserRepository`
  2. `FollowRepository`
  3. `PostRepository`
  4. `OGPCacheRepository`
  5. `OGPJobQueue`
  6. `LikeRepository`
  7. `TagRepository`
  8. `ImageRepository`
  9. `BadgeRepository`（badges master + user_badges grant/revoke を同居）
  10. （`LinkStore` は inmemory 実装専用の補助。interface ではないので postgres 側で必要にならない）

- `internal/repository/inmemory/inmemory.go` に上記すべての in-memory 実装あり。テストの基盤になっている。
- `db/migrations/001` 〜 `008` まで本番スキーマを整備済み:
  - `001_initial_schema.sql` — users（AuthCore sub CHAR(26) PK, is_admin 含む）
  - `002_add_images_table.sql` — images（ULID 化済み）
  - `004_implement_posts.sql` — posts / post_images / likes / tags / post_tags
  - `005_implement_badges.sql` — badges / user_badges（seed 含む）
  - `006_implement_follow_and_timeline.sql` — follows + `users.followers_count` / `following_count` ALTER
  - `007_implement_ogp_cache.sql` — ogp_cache / post_ogp / ogp_jobs
  - `008_add_ogp_job_position.sql` — ogp_jobs.position 追加
- `config/config.go` に `DBHost` / `DBPort` / `DBName` / `DBUser` / `DBPassword` は存在するが、現状 Go プロセスからは使われていない（コメントに「migrations / docker-compose 用途で保持」と明記済み）。
- `pkg/db/` ディレクトリは存在しない。コネクションプール抽象が未整備。
- `cmd/server/main.go` 54-65 行目で `inmemory.New*` を直接呼び出して配線している。

### ゴール

1. 本番環境で `docker-compose up -d` + `make db-init` で立てた PostgreSQL に対して、既存のすべてのユースケースが in-memory と同じ振る舞いで動く。
2. 統合テスト（`postgres` サービスを立ち上げた CI ジョブ）で全 repository 実装が contract を満たすことを検証できる。
3. `DATABASE_URL` / `DB_*` の有無で inmemory / postgres を切り替えられる（dev ローカルは inmemory で素早く起動、本番 / staging は postgres）。
4. 将来 repository interface を変更したときに、inmemory 実装と postgres 実装の両方に追従し忘れない仕組み（共通 contract test）を用意する。

### なぜ必要か

- FE との結合を開始した時点で、SNS の性質上「サーバ再起動で全投稿が消える」inmemory 運用は許容されない。
- `06` までで swagger / product docs / AuthCore 連携ドキュメントは整ったが、**実 DB 永続化** が入らない限り production ready とは呼べない。
- migration は用意済みなので、あとは Go 側実装を埋めるだけ。先送りのリスク（スキーマと Go コードの drift、ULID cursor 実装差、SQL UNIQUE 制約のセマンティクス差）が時間とともに増大するので、ここで畳む。

## 影響範囲

### 新規追加

- `pkg/db/pgx.go`（or `pkg/db/pool.go`）— `*pgxpool.Pool` の初期化 / ヘルスチェック / Close。
- `pkg/db/tx.go` — `BeginTx` / `WithTx` 等のトランザクション helper（`PostRepository.Create` で post + post_images + post_tags を 1 トランザクションで書くために必要）。
- `internal/repository/postgres/` 配下に各 repository 実装:
  - `postgres.go`（共通型 / コンストラクタ / 共通エラーハンドリング）
  - `user.go` / `follow.go` / `post.go` / `like.go` / `tag.go` / `image.go` / `badge.go` / `ogp_cache.go` / `ogp_job_queue.go`
- `internal/repository/testsupport/`（新規、optional）— inmemory / postgres 共通の contract test helpers。
- `.github/workflows/ci.yml` に integration test job を追加（`postgres` service は既に 77 行目以降で立ち上がっているので、そこに `-tags=integration` のテスト step を足す形を基本線とする）。

### 変更

- `cmd/server/main.go` — 54-65 行目の inmemory 直呼び出しを、設定値で分岐する factory 経由に置き換え。`REPO_BACKEND` 明示指定を最優先、未指定時は `Environment == "development"` なら inmemory、それ以外は postgres を選択（Step 0-2 案 Z 確定）。
- `config/config.go` — `DBHost` / `DBPort` / `DBName` / `DBUser` / `DBPassword` を Go プロセスで利用するようにコメントアウトを解除し、`DATABASE_URL` 形式に組み立てるヘルパー `DSN()` を追加（または `DATABASE_URL` 環境変数を直接追加して従来の個別フィールドと併存させる）。
- `go.mod` / `go.sum` — `github.com/jackc/pgx/v5` を追加（ORM 不採用の場合。採用 ORM は Step 0 で確定）。
- `Makefile` — `test-integration` ターゲット追加（`go test -tags=integration ./...`）。

### 破壊的変更

- **HTTP API への影響なし**（repository interface シグネチャは無変更、実装差し替えのみ）。
- **ULID / cursor / UNIQUE 制約のセマンティクス** は inmemory と postgres で完全一致させる。`repository.OGPJobQueue.Enqueue` コメントや `PostRepository.AttachOGP` コメントに既に仕様が書かれているので、それを postgres 側で忠実に実装する。
- **default の backing store** は Step 0-2 で案 Z（Environment で自動選択、`REPO_BACKEND` で上書き可）に確定。dev は inmemory default、prod / staging は postgres default。

## スコープ

### 含む

- `pkg/db` の pgxpool ラッパ
- `internal/repository/postgres/` に全 interface の実装
- contract test（inmemory / postgres 共通のテスト関数群を `//go:build integration` タグで切り替え）
- `cmd/server/main.go` の配線切り替え
- `config/config.go` の DB 接続値活用
- CI の integration test job 追加
- `docs/DATABASE_SETUP.md` の内容を postgres 配線が入った現実に合わせて追記

### 含まない

- **ORM / クエリビルダの本格導入** — Step 0 で比較検討して選ぶが、原則は **標準 `database/sql` 互換 + pgx 直書き**。`sqlc` / `squirrel` / `ent` / `gorm` は導入候補として議論したうえで、この PR では採用しない場合も多い。`[未確定]` マーカーを置く。
- **Connection pool の細かいチューニング**（`MaxConns` / `MinConns` / `MaxConnLifetime` 等）— 既定値 + 環境変数で上書きできる形だけ用意する
- **pgbouncer / PgBouncer mode （transaction / session）の考慮**
- **read replica / write-after-read consistency**
- **multi-tenant schema / row-level security**
- **migration 実行の自動化 / ロールバック機構**（現状 `make db-init` が shell スクリプトで前進専用。それで足りる）
- **inmemory 実装の削除**（`[未確定]`。当面は両方保守する方針をデフォルトに置く）
- **OGP job worker のスケーリング**（multi-worker / leader election）
- **Observability（metrics / tracing）** — 別タスク
- **CockroachDB 等、他 SQL エンジン対応**

## Phase 構成と PR 分割

実装ボリュームが大きいため **4 Phase ≒ 4 PR** に分割する。各 Phase の間で CI が緑であることを確認してから次に進む。各 Phase 内の Step は `/start-with-plan` で 1 つずつ実行できる粒度で書く。

| Phase | 範囲 | PR 粒度 |
|---|---|---|
| 1 | `pkg/db` + `postgres` 実装: User / Post / Like | 中 |
| 2 | `postgres` 実装: Follow / Tag / Badge | 中 |
| 3 | `postgres` 実装: Image / OGPCache / OGPJobQueue | 中 |
| 4 | `cmd/server/main.go` 配線 + integration test CI job + docs 更新 | 小〜中 |

**Phase 0** として、Phase 1 着手前に ORM / default backend / inmemory の扱いを確定する「準備 / 方針決定」を置く。

---

## Step 0（Phase 0）: 方針の確定 ✅ 確定済み

### 0-1. クエリ記述手段の選定 → **A. pgx/v5 直書き**

- 最小依存、contract test で挙動検証
- 理由: `internal/domain/*.go` の型を一次資料にしている現状と整合、`List*` 系の cursor クエリは数が限定的で動的ビルダの恩恵が小さい
- 不採用（本 PR で）: B (sqlc), C (squirrel), D (gorm/ent)

### 0-2. default backing store → **Z. Environment で自動選択、`REPO_BACKEND` で上書き可**

- `REPO_BACKEND=inmemory|postgres` 明示 → それを使う
- 未指定: `Environment == "development"` なら inmemory、それ以外は postgres
- 理由: dev 体験（inmemory で高速起動）と prod 安全（postgres 強制）を両立

### 0-3. inmemory 実装の扱い → **P. 残す**

- 単体テストは高速な inmemory、統合テストは postgres、というレイヤ分け
- contract test で drift を検出
- 本 PR のゴールは「postgres を追加する」であり「inmemory を消す」ではない

### 0-4. 方針の文書化

- 上記 3 項目を本ドキュメントに反映済み
- `docs/architecture.md` の Repository 層説明は Phase 4 で追記

---

## Phase 1: pkg/db + User / Post / Like の postgres 実装

### 1-1. `pkg/db/pgx.go` の新設

新規ファイル: `/home/sheep/dev/fuju/backend/pkg/db/pgx.go`

責務:
- `pgxpool.Pool` の初期化 (`NewPool(ctx, cfg) (*pgxpool.Pool, error)`)
- ヘルスチェック (`Ping(ctx)`)
- Close フック

DSN の組み立て:
```go
// config.Config に以下の helper を追加（config/config.go 側）
func (c *Config) DSN() string {
    if v := os.Getenv("DATABASE_URL"); v != "" {
        return v
    }
    return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
        c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}
```

- `sslmode=disable` は dev のみ。production では `DATABASE_URL` で `sslmode=require` を明示する想定。
- pool 初期化オプション（`MaxConns` 等）は環境変数 `DB_MAX_CONNS` / `DB_MIN_CONNS` で上書き可。デフォルトは pgx のデフォルトに任せる。

### 1-2. `pkg/db/tx.go` のトランザクション helper

```go
// WithTx wraps fn in a pgx transaction. Commits on nil error, rolls back otherwise.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error { ... }
```

- `PostRepository.Create` / `TagRepository.UpsertByNames` 等で使う。
- 関数内で `tx.Rollback` を `defer` するが、commit 成功時は no-op になるパターン（pgx の標準イディオム）。

### 1-3. `internal/repository/postgres/postgres.go` の共通基盤

```go
// Package postgres implements repository.* interfaces backed by PostgreSQL.
package postgres

import "github.com/jackc/pgx/v5/pgxpool"

// Store aggregates all repository implementations so cmd/server/main.go
// can wire them in a single line.
type Store struct {
    Users    *UserRepository
    Posts    *PostRepository
    Likes    *LikeRepository
    Tags     *TagRepository
    Images   *ImageRepository
    Badges   *BadgeRepository
    Follows  *FollowRepository
    OGPCache *OGPCacheRepository
    OGPJobs  *OGPJobQueue
}

func New(pool *pgxpool.Pool) *Store { ... }
```

共通エラーマッピング:
- `pgx.ErrNoRows` → `(nil, nil)` （repository 契約: `GetByID` 等で not-found は `(nil, nil)` を返す）
- UNIQUE 制約違反 (`23505`) → 各 repository で個別にハンドル（例: `likes.Create` は `(false, nil)` に変換）

### 1-4. `UserRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:13-42`

実装メソッド（全 10 個）:
- `GetBySub` — `SELECT ... FROM users WHERE sub = $1 AND deleted_at IS NULL`
- `Upsert` — `INSERT ... ON CONFLICT (sub) DO UPDATE SET ...`（**`is_admin` は更新対象から除外**、repository コメント L17-L20 の契約を守る）
- `UpdateProfile` — `UPDATE users SET bio = $2, banner_url = $3, updated_at = now() WHERE sub = $1 RETURNING ...`
- `List` — `SELECT ... ORDER BY created_at DESC LIMIT $1 OFFSET $2` + 別クエリで `COUNT(*)` を取る（total 返却のため）
- `ListBySubs` — `SELECT ... WHERE sub = ANY($1)`
- `Increment/Decrement FollowersCount / FollowingCount` — `UPDATE users SET followers_count = followers_count ± 1 WHERE sub = $1`
- `Delete` — `UPDATE users SET deleted_at = now() WHERE sub = $1`

テスト:
- `internal/repository/postgres/user_test.go`（`//go:build integration`）
- 各メソッド 1-2 ケース、`TestMain` で migration 適用済み postgres に接続 / 各テスト冒頭で `TRUNCATE users CASCADE`

### 1-5. `PostRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:79-113`

特に注意:
- `Create` は **1 トランザクション** で posts 本体 + post_images + post_tags を INSERT（`WithTx` 使用）
- `AttachOGP` は `INSERT ... ON CONFLICT (post_id, position) DO NOTHING` + `INSERT ... ON CONFLICT (post_id, url_hash) DO NOTHING` — 契約 (L109-L113) の「first writer winning」セマンティクスを SQL で再現
- `List` / `ListByUserIDs` / `ListReplies` は ULID の性質を利用して `WHERE id < $cursor ORDER BY id DESC LIMIT $limit` で cursor 実装
- soft-delete のフィルタを全 query に入れる (`WHERE deleted_at IS NULL`)
- `IncrementLikesCount` 等のカウンタ更新はアトミック UPDATE

### 1-6. `LikeRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:162-169`

- `Create` — `INSERT ... ON CONFLICT (user_id, post_id) DO NOTHING RETURNING xmax` で「実際に行が作られたか」を判定。`xmax = 0` で新規作成。
- `Delete` — `DELETE ... RETURNING 1`。返り行数 > 0 で `(true, nil)`、ゼロで `(false, nil)`
- `IsLikedBy` — `SELECT EXISTS(...)`
- `ListLikedPostIDsByUser` — `SELECT post_id FROM likes WHERE user_id = $1 AND post_id = ANY($2)`

### 1-7. Phase 1 の contract test 整備

新規: `internal/repository/testsupport/contract.go`

```go
// RunUserRepositoryContract asserts that the given UserRepository honors the
// interface contract. Invoked from both inmemory and postgres test packages.
func RunUserRepositoryContract(t *testing.T, newRepo func(t *testing.T) repository.UserRepository) { ... }
```

- 既存 `inmemory` 側のユニットテストを contract 関数に抽出
- postgres 側は `//go:build integration` タグ付きで `contract.RunUserRepositoryContract(t, newPostgresUserRepo)` を呼ぶ
- User / Post / Like の 3 つで試し、Phase 2 以降も同パターンを踏襲

### 1-8. Phase 1 の検証

- `make lint` 通過
- `make test` 通過（inmemory のみ）
- `make test-integration`（新規ターゲット）が docker-compose の postgres に対して通過
- PR 上で CI の `lint` / `test` が green

---

## Phase 2: Follow / Tag / Badge の postgres 実装

### 2-1. `FollowRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:57-73`

特に注意:
- `Create` / `Delete` の冪等性: `INSERT ... ON CONFLICT DO NOTHING` / `DELETE ... RETURNING 1` で「状態遷移があったか」の bool を返す
- cursor 仕様（repository.go:50-56 に明記）:
  ```
  base64url(RFC3339Nano(created_at) + "|" + peer_sub)
  ORDER BY created_at DESC, peer_sub DESC
  ```
  — encode / decode helper を `internal/repository/postgres/cursor.go` に置く。inmemory 側と同一 encoder を共有する（`internal/repository/sharedcursor/` 等の新パッケージに切り出す）
- カウンタ更新は usecase 側で `User.Increment/DecrementFollowersCount` を呼ぶ前提。Repository 内で自動更新しない（契約: repository.go:33-38 コメント）
- `AreFollowing` — `SELECT followee_sub FROM follows WHERE follower_sub = $1 AND followee_sub = ANY($2)` → map 化

### 2-2. `TagRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:171-181`

- `UpsertByNames` — `INSERT INTO tags (id, name) VALUES ... ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id, name, created_at`（`DO UPDATE` で常に RETURNING を走らせる）
  - ID は ULID なので caller が生成してもよいし、`tag_id` default を使ってもよい。inmemory 実装に合わせる
- `ListByPostID` / `ListByPostIDs` — `SELECT t.* FROM tags t JOIN post_tags pt ON pt.tag_id = t.id WHERE pt.post_id = ANY($1)` → postID でグルーピング

### 2-3. `BadgeRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:200-215`

- master 操作 (`ListAll` / `GetByKey` / `GetByID` / `Create` / `Update`)
- user_badges 操作:
  - `Grant` — `INSERT ... ON CONFLICT (user_id, badge_id) DO UPDATE SET expires_at = EXCLUDED.expires_at, reason = EXCLUDED.reason`（renewable）
  - `Revoke` — `DELETE FROM user_badges WHERE user_id = $1 AND badge_id = $2`
  - `ListByUserID` / `ListByUserIDs` — **active grant のみ** フィルタ: `WHERE expires_at IS NULL OR expires_at > now()`（契約 L198-L199）

### 2-4. Phase 2 の contract test 追加

- `RunFollowRepositoryContract` / `RunTagRepositoryContract` / `RunBadgeRepositoryContract` を追加
- inmemory / postgres 両方で走らせる

### 2-5. Phase 2 の検証

- Phase 1 と同じ（lint / test / integration test / CI green）

---

## Phase 3: Image / OGPCache / OGPJobQueue の postgres 実装

### 3-1. `ImageRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:184-195`

- `ListByPostID` / `ListByPostIDs` — `post_images.position` ASC で並び替え
- `Delete` は soft delete 該当カラムが migration にあればそれを使う。無ければ hard delete（`002_add_images_table.sql` を要確認）

### 3-2. `OGPCacheRepository` の postgres 実装

対象 interface: `internal/repository/repository.go:117-132`

- `Get` — `SELECT ... FROM ogp_cache WHERE url_hash = $1`、`pgx.ErrNoRows` → `(nil, nil)`
- `Upsert` — `INSERT ... ON CONFLICT (url_hash) DO UPDATE SET ...`
- `ListByPostIDs` — `post_ogp` JOIN `ogp_cache`、ORDER BY `post_ogp.position` ASC、postID でグルーピング

### 3-3. `OGPJobQueue` の postgres 実装（最重要）

対象 interface: `internal/repository/repository.go:137-156`

- `Enqueue` — `INSERT INTO ogp_jobs (id, url_hash, url, post_id, position, status, enqueued_at) VALUES ($1, $2, $3, $4, $5, 'queued', now())`
- `Claim` — `SELECT ... FROM ogp_jobs WHERE status = 'queued' ORDER BY enqueued_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED` の結果を `UPDATE ... SET status = 'running', claimed_by = $1, claimed_at = now() WHERE id = $jobID RETURNING *`
  - 契約 (L134): `SELECT ... FOR UPDATE SKIP LOCKED` 必須
  - トランザクション内で SELECT + UPDATE を実行
  - 空キュー時は `(nil, nil)`
- `MarkDone` — `UPDATE ... SET status = 'done', finished_at = now() WHERE id = $1`
- `MarkFailed` — retriable に応じて `status = 'queued'`（再試行） or `'failed'`（確定） に遷移。`reason` カラムに記録

テスト観点:
- 並行 Claim（2 worker が同時に `Claim` を呼んでも同一 job を 2 回 claim しない）
- 並行 Enqueue → Claim の FIFO 順序
- retriable failed → 再 claim で同じ job を拾えること

### 3-4. Phase 3 の contract test 追加

- Image / OGPCache / OGPJobQueue の contract
- **OGPJobQueue は並行性契約が重要なので、goroutine × 2 での concurrent claim テストを inmemory / postgres 両方で走らせる**

### 3-5. Phase 3 の検証

- Phase 1/2 と同じ

---

## Phase 4: 配線 / CI / ドキュメント

### 4-1. `config/config.go` の拡張

- `DATABASE_URL` を optional に追加（`DBHost` 系と併存）
- `DSN()` method を追加（Step 1-1 で記載）
- `DBMaxConns` / `DBMinConns` を optional に追加（無ければ pgx default）
- `REPO_BACKEND` を追加（値: `"inmemory"` / `"postgres"` / 未指定なら `Environment` で自動選択、Step 0-2 に従う）
- `Validate()` に `REPO_BACKEND == "postgres"` の場合の DB_* 必須チェックを追加

### 4-2. `cmd/server/main.go` の配線切り替え

現状 (54-65 行目):

```go
links := inmemory.NewLinkStore()
userRepo := inmemory.NewUserRepository()
// ...
```

変更後:

```go
repoSet, cleanup, err := newRepositorySet(ctx, cfg, log)
if err != nil {
    fmt.Fprintf(os.Stderr, "Failed to initialize repositories: %v\n", err)
    os.Exit(1)
}
defer cleanup()

userRepo := repoSet.Users
postRepo := repoSet.Posts
// ...
```

`newRepositorySet` は `cmd/server/repos.go`（新規）で以下を実装:

```go
func newRepositorySet(ctx context.Context, cfg *config.Config, log *logger.Logger) (*RepoSet, func(), error) {
    switch cfg.RepoBackend() {
    case "postgres":
        pool, err := db.NewPool(ctx, cfg)
        if err != nil { return nil, nil, err }
        store := postgres.New(pool)
        return store.asRepoSet(), pool.Close, nil
    case "inmemory":
        fallthrough
    default:
        links := inmemory.NewLinkStore()
        return &RepoSet{
            Users: inmemory.NewUserRepository(),
            Posts: inmemory.NewPostRepository(links),
            // ...
        }, func(){}, nil
    }
}
```

### 4-3. CI の integration test job 追加

`.github/workflows/ci.yml` の `test` ジョブに step を追加、または独立 job `integration-test` を追加:

```yaml
- name: Run integration tests (postgres)
  run: go test -tags=integration -race ./internal/repository/postgres/...
  env:
    DATABASE_URL: postgres://fuju:testpass@localhost:5432/fuju_test?sslmode=disable
    REPO_BACKEND: postgres
```

- 既に `test` ジョブに `services.postgres` が定義済み（ci.yml:62-75）。DB は立っているので step 追加だけで済む。
- migration 適用が必要。`db/init.sh` を CI で走らせる step を追加（`psql -h localhost -U fuju -d fuju_test -f db/migrations/001_...` を順に実行する方式、または `db/init.sh` に `DATABASE_URL` 受け取り口を足す）。

### 4-4. `Makefile` の `test-integration` ターゲット

```makefile
test-integration:
	@echo "Running integration tests against postgres..."
	DATABASE_URL="postgres://fuju_user:fuju_password@localhost:5432/fuju?sslmode=disable" \
	REPO_BACKEND=postgres \
	$(GO) test -tags=integration -race ./internal/repository/postgres/...
```

### 4-5. `docs/DATABASE_SETUP.md` の更新

- 「Go プロセスが実 DB を使う」前提で書き直す:
  - `DATABASE_URL` 指定時は postgres backend が使われる
  - 未指定時は `Environment=development` なら inmemory、それ以外なら起動失敗（または postgres 強制、Step 0-2 に従う）
  - migration 適用手順 (`make db-up && make db-init`)
  - integration test の走らせ方

### 4-6. `docs/architecture.md` の更新

- Repository 層の節に postgres backend が追加された旨を追記
- inmemory と postgres の使い分け方針を 1 段落で説明
- contract test の位置づけを説明

### 4-7. `docs/tasks/README.md` への追記

- 本タスク 08 を表に追加
- Migration 番号テーブルは本 PR では migration を追加しないので触らない（`008` が既存、本タスクで `009` 以降を追加する予定は今のところなし）

### 4-8. Phase 4 の検証

- `make lint` / `make test` / `make test-integration` すべて通過
- CI で `lint` / `test` / `integration-test` / `build` / `security` すべて green
- ローカル `docker-compose up -d && make db-init && REPO_BACKEND=postgres go run ./cmd/server` でサーバが起動、最小シナリオ（`GET /me`, `POST /posts`, `GET /posts/{id}`, `POST /posts/{id}/like`, `GET /timeline/global`）が成功
- ローカル `go run ./cmd/server`（DATABASE_URL なし、Environment=development）で inmemory backend で起動できる

---

## 検証 / テスト方針

### 単体テスト

- inmemory 側は既存テストを維持
- postgres 側は `//go:build integration` タグで contract test を走らせる
- contract test 関数は inmemory / postgres で共有

### 統合テスト

- CI の `test` ジョブ内で `services.postgres` を立ち上げ、migration 適用後に `go test -tags=integration ./internal/repository/postgres/...` を実行
- 全 10 repository interface が contract を満たすことを確認
- `OGPJobQueue` は並行 Claim テストを含む

### E2E / smoke

- 本 PR のスコープ外（別タスク）。ただし Phase 4 ローカル検証で主要エンドポイントを手動で叩いて成功を確認する

## 技術的な補足

### cursor セマンティクスの inmemory/postgres 一致

- `PostRepository.List` の cursor は ULID を文字列比較する素朴実装で両実装とも完結する（ULID の lexicographic ordering ≒ 時系列順）
- `FollowRepository.ListFollowers` の cursor は `base64url(RFC3339Nano|peer_sub)` 形式 — encode / decode 関数を共有パッケージに切り出す前提にしておく

### 並行制御の契約

- `LikeRepository.Create` — `INSERT ON CONFLICT DO NOTHING` の返り値判定は `xmax` or `RETURNING` 行数のどちらかで行う。pgx 5 では `CommandTag.RowsAffected()` で確認可能
- `OGPJobQueue.Claim` — `FOR UPDATE SKIP LOCKED` を transaction 内で実行。pgx の `tx.QueryRow` を使う
- `FollowRepository.Create` / `Delete` の bool 返却 — 状態遷移があった場合のみ true。inmemory の既存実装のテストを contract 化して postgres で再利用

### エラーマッピング方針

- `pgx.ErrNoRows` → repository 契約が `(nil, nil)` を求めている箇所（`UserRepository.GetBySub` 等）では黙って `nil, nil` を返す
- `*pgconn.PgError` (code `23505` UNIQUE 違反) → 各 repository で個別にハンドル。LikeRepository は `(false, nil)`、PostRepository.AttachOGP は `nil`（冪等扱い）
- その他 DB エラーは wrap してログ / error として伝播

### migration のドリフト防止

- 本 PR では自動化しないが、`docs/architecture.md` に「新しい migration を追加したら postgres repository 側でも対応実装を更新すること」を記載
- 将来的に `sqlc`（Step 0-1 案 B）を導入すれば schema から Go 型を再生成できるので drift が検出しやすくなる — 別タスク

### inmemory と postgres の drift を contract test で防ぐ

- `internal/repository/testsupport/contract.go` に各 interface の契約テスト関数を置く
- inmemory 側テスト: `TestUserRepository_Contract_InMemory` が `contract.RunUserRepositoryContract(t, newInMemoryUser)` を呼ぶ
- postgres 側テスト: `TestUserRepository_Contract_Postgres` が `contract.RunUserRepositoryContract(t, newPostgresUser)` を呼ぶ
- 同じテスト関数が両 backend に対して走るので、契約違反は必ず片方で fail する

### dev ローカルと CI の差分

- dev: `docker-compose up -d` で postgres、`make db-init` で migration、`REPO_BACKEND=postgres go run ./cmd/server` で起動
- CI: `services.postgres` で立ち上げ、migration 適用 step 追加、`REPO_BACKEND=postgres` の env で integration test
- `REPO_BACKEND` 未指定の既存 unit test は `inmemory` で動くまま（後方互換維持）

### リスクと緩和

| リスク | 緩和 |
|---|---|
| postgres 実装と inmemory 実装の挙動差 | contract test で同一テストを両 backend に走らせる |
| migration スキーマと Go の domain struct の drift | PR ごとに migration + domain + repository をセットで更新する運用ルール（`docs/tasks/README.md` に注記） |
| `OGPJobQueue.Claim` の `FOR UPDATE SKIP LOCKED` が期待通り動かない | concurrent claim test を必ず含める |
| dev 体験の悪化（`docker-compose up` 忘れで起動できない） | `REPO_BACKEND=inmemory` を dev default のままにする（Step 0-2 案 Z） |
| PR が肥大化する | Phase 1-4 に分割 |
| integration test が flaky | migration 適用を `TestMain` 側で冪等に行う（`CREATE TABLE IF NOT EXISTS` ではなく、`TRUNCATE ... CASCADE` を各テスト開始時に実行） |

## 確定事項（Phase 0 で決定）

- **クエリ記述手段**: **A (pgx 直書き)** 確定
- **default backing store**: **Z (Environment 分岐、`REPO_BACKEND` で上書き可)** 確定
- **inmemory 実装の扱い**: **P (残す)** 確定
- **migration 実行の CI ステップ**: `db/init.sh` を CI から呼び出す方針（Phase 4 で実装）
- **`GitCommit` var の追加是非**: 本タスクではスコープ外、必要なら別タスクで対応

## 参考

- pgx v5 ドキュメント: https://pkg.go.dev/github.com/jackc/pgx/v5
- pgxpool ベストプラクティス: https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
- PostgreSQL `FOR UPDATE SKIP LOCKED`: https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE
- 既存 repository 契約の一次資料: `internal/repository/repository.go`
- 既存 in-memory 実装: `internal/repository/inmemory/inmemory.go`
- 本番スキーマ: `db/migrations/001_initial_schema.sql`〜`008_add_ogp_job_position.sql`
