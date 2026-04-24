# 06 - Frontend Handoff Docs and Dead Code Removal

## 概要

フロントエンド開発者に本バックエンドを引き渡すための「プロダクト概要 / API リファレンス / AuthCore 連携」ドキュメント一式を整備する。ただしドキュメント整備の **前** に、現状リポジトリに残っている旧仕様（OAuth / セッション Cookie / Redis / BIGINT ID / `username`&`email`&`avatar_url` スキーマ / `comments` テーブル等）由来の **未使用コードおよび未使用 config / docker-compose 定義を先に削除** し、整合の取れた状態に畳んでから書き起こす。

## 対象読者

- **フロントエンド開発者**（`openapi-typescript` / `orval` で型生成して利用する想定） — Phase 2 以降の swagger / プロダクト概要 / AuthCore 連携ドキュメント
- **本リポジトリ保守者** — Phase 1 の不要コード削除、および Phase 5 で既存ドキュメント更新

## 前提 / 依存タスク

- `01-rewrite-domain-to-authcore-alignment.md` 完了済み（AuthCore 準拠 / ULID 化確定）
- `02-implement-post-feature.md`, `02-implement-badge.md` 完了済み
- `03-implement-follow-and-timeline.md`, `03-implement-ogp-fetcher.md` 完了済み
- `04-fix-ci-lint-and-pragent.md`, `05-fix-ogp-attach-position.md` 完了済み（最新ブランチ `feat/fix-ogp-attach-position`）
- すなわち 01〜05 の実装は **動いている** 状態が前提。本タスクは実装修正ではなくドキュメント整備＋枯れた設定の剪定のみ。
- PR base は **develop**（プロジェクトの標準運用）。

## スコープ

### 含む

- `docs/swagger.yaml` を現 API 実装と完全一致する OpenAPI 3.0 仕様に全面刷新（`openapi-typescript` で破綻なく型生成できるレベル = 全 schema / required / nullable / error code / tag / operationId / security が明示）
- プロダクト概要ドキュメント（新規）
- AuthCore 連携ドキュメント（新規、FE 開発者向け）
- `README.md` / `docs/IMPLEMENTATION_GUIDE.md` / `docs/architecture.md` / `docs/DATABASE_SETUP.md` の旧記述（OAuth flow / Session Cookie / Redis session / BIGINT ID / `username` & `email` / `comments`）を現仕様に合わせて修正
- 旧仕様の痕跡である **未使用 config フィールド / docker-compose サービス / .env.example 項目 / コメント** の削除
- 生成ツール（redocly CLI, openapi-typescript）による検証コマンドの用意

### 含まない

- 新機能の実装 / バグ修正（見つけても本 PR では直さず、別タスクに切り出す）
- 実 DB 配線（現状 in-memory のまま。Postgres ドライバ導入は別タスク）
- Redis 再導入（削除対象。将来必要になったら別タスク）
- i18n（ドキュメントは日本語ベースで問題ない方針）
- swagger から型生成したクライアント SDK のコミット（FE 側リポジトリで生成する想定）

## Phase 構成

**Phase 1 = 不要コード / 設定の削除を先行**。これによって以降のドキュメント作業が「実装に残っている影」に引っ張られず、実装が正とされた状態で書ける。

---

## Phase 1: 不要コード・設定の削除（先行）

各ステップは独立した小コミットに分ける。1 ステップずつ順に `/start-with-plan` で実行可能。

### 1-1. `config.Config.RedisURL` と関連 loader / Validate を削除

- 対象: `config/config.go`
  - `RedisURL string` フィールド
  - `Load()` 内の `RedisURL: getEnv("REDIS_URL", "")`
  - `Validate()` 内の `REDIS_URL is required` チェック
- 根拠: `grep -r "RedisURL\|go-redis\|redis\." --include="*.go"` したところ参照はすべて `config/config.go` 内のみで、他コードから使われていない（Go コードには Redis クライアントが一切存在しない）。
- 注意: 将来再導入するときのために削除理由を commit message に残す。

### 1-2. `config.Config` の未使用フィールド整理

- 対象: `config/config.go`
  - `FrontendURL` フィールド（Go コード内参照ゼロ）
  - `DBMaxConn` / `DBMinConn`（Go コード内参照ゼロ。DB 配線自体が未実装なため）
- 判断: `DBHost` / `DBPort` / `DBName` / `DBUser` / `DBPassword` は **今は削除しない**。マイグレーションスクリプト（`db/init.sh`）と `Makefile` の `db-shell` / `db-init` / `docker-compose` の postgres サービスが参照しており、DB を立てる運用は生きているため。Go プロセスは使わないが `.env.example` と docker-compose からは残す。
- コメント修正: 「DB 接続は現状 in-memory リポジトリで未使用。migrations 適用先として定義のみ保持」と `config.go` 上部に 1 行注記。

### 1-3. `docker-compose.yml` から Redis サービスを削除

- 対象: `/home/sheep/dev/fuju/backend/docker-compose.yml`
  - `services.redis` ブロック全削除
  - `services.backend.environment.REDIS_URL`（22 行目） 削除
  - `services.backend.depends_on.redis` 削除
  - `volumes.redis_data` 削除
  - 出力メッセージ類は Phase 1-5 の Makefile 更新で合わせる
- 根拠: Go からの Redis 参照ゼロ。AuthCore introspection キャッシュは in-memory（`pkg/authcore/cache.go`）。

### 1-4. `.env.example` から Redis / 旧 OAuth コメントを除去

- 対象: `/home/sheep/dev/fuju/backend/.env.example`
  - `REDIS_URL=...`（19 行目） 削除
  - 24 行目コメント `# OAuth2 client credentials registered on AuthCore for this backend` を `# AuthCore client credentials (RFC 6749 confidential client, used for RFC 7662 introspection)` に書き換え
  - `FRONTEND_URL=...`（54-55 行目） 削除（1-2 で Go 側から消す場合）
- 確認: `.env.example` で残す AuthCore / DB / R2 / CORS / OGP 系はそのまま。

### 1-5. `Makefile` から Redis 関連出力を除去

- 対象: `/home/sheep/dev/fuju/backend/Makefile`
  - `db-up` ターゲット内 `@echo "Redis: localhost:6379"`（115 行目） 削除
- 機能変更なし（Redis コンテナを上げていない旨を反映するだけ）。

### 1-6. `docs/DATABASE_SETUP.md` の旧仕様記述を削除

- 対象: `/home/sheep/dev/fuju/backend/docs/DATABASE_SETUP.md`
  - Redis 前提（`Redis 7+`, `brew services start redis`, `docker run redis`, `REDIS_URL=...`, 「Ensure PostgreSQL and Redis are running」など）→ 全削除
  - 旧 User スキーマ説明（`Uses OAuth provider and ID for authentication`, 「OAuth States Table」セクション, 「OAuth state cleanup」, `oauth_provider` インデックス言及）→ 全削除
  - `Tracks likes_count and comments_count` / 「Stores comments on posts」→ Post 機能が reply 統合済みなので `comments` 言及を削除し、最新の `posts.reply_to_post_id` + `likes` 独立テーブル構成を 1 段落で反映
  - 「Session management: `user_id`, `expires_at`」インデックス言及 → 削除
- 目的: DB セットアップドキュメントを現状の migrations (`001`〜`008`) と整合させる。

### 1-7. `docs/architecture.md` の旧記述削除

- 対象: `/home/sheep/dev/fuju/backend/docs/architecture.md`
  - `redis.go                   # Redis client wrapper`（123 行目付近） 削除
  - 「Caching: Redis (frequently accessed data; session state lives in AuthCore)」（203 行目付近） → 「Caching: 現状なし。AuthCore introspection は in-memory キャッシュ（`pkg/authcore/cache.go`、TTL=30s）。」 に修正
  - 「Redis-backed introspection cache: move the 30s AuthCore cache out of process」（494 行目付近、将来改善セクション）は **残す** — Future Work として妥当。
- 注意: このファイルは Phase 4 (AuthCore 連携) や Phase 3 (プロダクト概要) から参照されるので、先にクリーンにしておくと後段が書きやすい。

### 1-8. `.agent.md` の旧 AuthN 記述を現仕様に修正

- 対象: `/home/sheep/dev/fuju/backend/.agent.md`
  - 10 行目 `Authentication: OAuth2.0 + JWT (mobile) + Session Cookies (web)` → `Authentication: Bearer JWT access token issued by AuthCore (see pkg/authcore). Backend validates via RFC 7662 introspection.`
  - 12 行目 `Cache: Redis` → `Cache: in-memory (AuthCore introspection TTL=30s). No Redis dependency.`
  - 59 行目 `Redis client: github.com/redis/go-redis (necessary for caching)` → 行削除
  - 60 行目 `OAuth2: golang.org/x/oauth2 (for OAuth2 integration)` → 行削除
  - 132 行目付近 `Use real PostgreSQL/Redis for integration tests` → `Use real PostgreSQL for integration tests` に修正
  - 171 行目以降「Web Application (Session-based)」「OAuth2 callback creates HttpOnly, Secure cookie」「Session stored in Redis with TTL」「SameSite=Strict cookie policy」 → ブロックごと削除し、代わりに「All clients pass the AuthCore-issued access token as `Authorization: Bearer ...`. The backend introspects it per request (cached 30s in-process). Session lifetime is owned by AuthCore.」の 1 段落に置換
  - 224 行目「Redis Usage」節および `session:{session_id}` 行 → 節ごと削除
  - 308-344 行目あたり「oauth_callback.go / feat(auth): Add OAuth2 callback handler」サンプル → 別サンプルに置換（例: `feat(posts): add reply listing handler` のような現行ドメインに即した例へ）
  - 501 行目 / 517-518 行目 commit / PR サンプルの OAuth 文言も同様に置換
- 目的: agent が参照するプロジェクトメタを現仕様に揃える。

### 1-9. swagger.yaml の旧パス削除（Phase 2 へ引き継ぐため、まず物理削除のみ先行）

- 対象: `/home/sheep/dev/fuju/backend/docs/swagger.yaml`
  - `/auth/oauth/authorize` / `/auth/oauth/callback` / `/auth/refresh` / `/auth/logout` パス全削除
  - `components.securitySchemes.CookieAuth` と参照している `- CookieAuth: []` を全削除
  - 残った `/users` / `/posts` などの旧定義も Phase 2 で全面書き直すのでこの時点では **触らない**（Phase 2 で上から書き下ろす方が速い）
- 方針: Phase 1 の責務は「削除」に限定。Phase 2 で正しい形を書くので整形や追加はしない。

### 1-10. `docs/IMPLEMENTATION_GUIDE.md` 内の `comments` 暫定記述を整理

- 対象: `/home/sheep/dev/fuju/backend/docs/IMPLEMENTATION_GUIDE.md`
  - 163 行目 `- comments - 暫定（post 機能タスクで post reply に統合され削除予定）` → 行削除（reply 統合済み）
  - 587 行目 `POST /posts/{id}/comments / DELETE /posts/{post_id}/comments/{comment_id}` セクション → 「現行 API では reply は `POST /posts`（`reply_to_post_id` 指定）/ `DELETE /posts/{id}` / `GET /posts/{id}/replies` で実現」と書き換え
  - 869 行目 `CommentsCount int64 // コメント数（暫定、reply 統合で削除予定）` → 削除
  - 897 行目以降「Comment ドメインモデル」節 → 節ごと削除
  - 936 行目 `file_size BIGINT NOT NULL,` は Image テーブル定義の SQL 片なので内容は OK（ID ではなく file_size）。触らない。
- 根拠: `internal/domain/` 配下に `comment.go` は存在せず、Post の reply 機構で統合済み。

### 1-11. `README.md` の旧 AuthN / Redis / OAuth 記述を削除

- 対象: `/home/sheep/dev/fuju/backend/README.md`
  - 8 行目 `Authentication: OAuth2.0 + JWT (mobile) + Session Cookies (web)` → `Authentication: Bearer JWT (AuthCore-issued) validated via RFC 7662 introspection`
  - 10 行目 `Caching: Redis for sessions and frequently accessed data` → 行削除
  - 64-68 行目 `OAUTH_CLIENT_ID` / `OAUTH_CLIENT_SECRET` / `OAUTH_REDIRECT_URL` / `SESSION_SECRET` 群 → 現行 `AUTHCORE_*` 変数リストに置換（`.env.example` と揃える）
  - 173-183 行目「User initiates OAuth2 login」以下のログインフロー説明 → 「Clients obtain an access token from AuthCore (out of scope for this repo). All API calls include `Authorization: Bearer <token>`. The backend introspects the token per request.」に簡潔化
  - 179 行目 `Security: CSRF tokens, HttpOnly cookies, SameSite=Strict` → 削除（Cookie を発行しないため）
  - 206 行目 `users: User profiles and OAuth information` → `users: SNS-local mirror cache of AuthCore profile + bio / banner / is_admin`
  - 209 行目 `sessions: Active user sessions (also in Redis)` → 行削除
- 目的: プロジェクトのトップ README を現仕様に揃える（次の Phase で FE が最初に読むファイル）。

### 1-12. `docs/tasks/01-rewrite-domain-to-authcore-alignment.md` の旧仕様 grep ヒットは **触らない**

- 理由: このタスクドキュメント自体が「旧 sessions / oauth_states を削除する計画書」なので、削除対象の用語が登場するのは正しい。Phase 1 のクリーンアップ対象から明示的に除外する。
- `docs/tasks/README.md` も同様の理由で触らない。
- Phase 1 の grep 検証 (1-13) で残存をチェックする際、このファイル群はホワイトリストに入れる。

### 1-13. Phase 1 の検証

- 以下を実行してゼロ件または想定外の残りがないことを確認:
  - `grep -rni "oauth\|session\|cookie" --include="*.md" --include="*.go" --include="*.yaml" --include="*.yml" --include=".env.example" docs/ pkg/ internal/ cmd/ config/ .env.example README.md .agent.md docker-compose.yml Makefile` — 残ってよいのは:
    - `pkg/authcore/*`（AuthCore 側 OAuth2 client_credentials / session introspection の正当な言及）
    - `internal/middleware/middleware.go` / `middleware_test.go` / `internal/usecase/user/usecase_test.go` / `pkg/authcore/cache.go` / `client_test.go`（AuthCore Session 型）
    - `docs/tasks/01-*` / `docs/tasks/README.md` / `docs/tasks/04-*`（旧仕様の削除計画を記述している）
    - `.github/workflows/pr-review.yml`（レビューコメントのコンテキスト文言として OK、内容確認の上残す）
  - `grep -rni "redis\|REDIS_URL" --include="*.md" --include="*.go" --include="*.yml" --include="*.yaml" --include="Makefile" --include=".env.example" .` — ゼロ件が目標（Phase 1-1 〜 1-8 でクリア）。`docs/architecture.md` の Future Work 行は **残す**。
  - `grep -ni "avatar_url\|username\|email" docs/swagger.yaml` — Phase 2 で書き直すので Phase 1 時点では残っていてよい。
- 結果を PR 本文に貼り、レビュアが残存を確認できるようにする。

---

## Phase 2: swagger.yaml の刷新（厳密型生成対応）

### 2-1. OpenAPI 3.0 / 3.1 と構成方針の確定

- 採用: **OpenAPI 3.0.3**
  - 理由: `openapi-typescript` / `orval` ともに 3.0 完全対応、3.1 は `orval` で一部要 plugin。FE 側の自由度を優先。
  - `nullable: true` を使う（3.1 の `type: [string, null]` は不可）。
- トップレベル:
  - `info.version`: プロジェクト本体の semver に揃える（未整備なら `0.1.0` を仮置き）
  - `info.title`: `FUJU Backend API`
  - `info.description`: 「FUJU SNS backend. Authenticated via AuthCore-issued Bearer JWT. See /docs/authcore-integration.md for the token lifecycle.」
  - `servers`: `http://localhost:8080`（Development）/ `https://api.fuju.example.com`（Production、実ドメイン未定なら TBD コメント付き）。**`/v1` プレフィックスは付けない** — `cmd/server/main.go` のルーティングはルート直下 + 一部 `/v1/images` `/v1/admin/...` の混在で、一括プレフィックスを切っていないため。
- `tags`: `Health`, `Me`, `Users`, `Posts`, `Likes`, `Follows`, `Timelines`, `Images`, `Badges (Admin)`
- `operationId` 命名規則: `{tag}_{verb}{Resource}` のスネークケースではなく **キャメル**（`orval` が関数名に使うため）。例: `posts_listPosts` ではなく `postsListPosts`、`postsCreatePost`, `postsGetPost`, `postsDeletePost`, `postsLikePost`, `postsUnlikePost`, `postsListReplies`, `usersGetUser`, `usersListUsers`, `usersUpdateUser`, `meGet`, `followsFollow`, `followsUnfollow`, `followsListFollowers`, `followsListFollowing`, `timelinesHome`, `timelinesUser`, `timelinesGlobal`, `imagesUpload`, `imagesListMine`, `imagesDelete`, `adminBadgesList`, `adminBadgesCreate`, `adminBadgesUpdate`, `adminBadgesGrant`, `adminBadgesRevoke`, `healthCheck`。

### 2-2. securitySchemes と共通 components 整備

- `components.securitySchemes`:
  ```yaml
  BearerAuth:
    type: http
    scheme: bearer
    bearerFormat: JWT
    description: AuthCore-issued access token. Validated per request via RFC 7662 introspection.
  ```
- `security` はルート指定せず、**各 operation で個別に指定**（公開 GET があるため）。
- `components.schemas` に共通型を定義（後続ステップで積み上げ）:
  - `Error` — `{ code: string, message: string }` 両 required
  - `PageMeta` — `{ limit: integer, offset: integer, total: integer, has_more: boolean }`（実装に合わせ unused フィールドは含めない）
  - `ULID` — `type: string, pattern: '^[0-9A-HJKMNP-TV-Z]{26}$', minLength: 26, maxLength: 26, example: '01HZXYABCDEFGHJKMNPQRSTVWX'`
  - `Timestamp` — `type: string, format: date-time`
- `components.responses` に共通エラーレスポンスを定義し、各 operation から `$ref` で引く（重複を削減し、FE の型生成も安定）:
  - `BadRequest` (400) / `Unauthorized` (401) / `Forbidden` (403) / `NotFound` (404) / `Conflict` (409) / `UnprocessableEntity` (422) / `TooManyRequests` (429) / `InternalServerError` (500) / `ServiceUnavailable` (503)
  - 各 `content: application/json: schema: $ref: '#/components/schemas/Error'`

### 2-3. ドメインスキーマ定義

`internal/domain/*.go` を一次資料として、以下を `components.schemas` に定義:

- `User` — `{ sub: ULID, display_name: string, display_id: string, icon_url: string, bio: string, banner_url: string, is_admin: boolean, followers_count: integer (int64), following_count: integer (int64), created_at: Timestamp, updated_at: Timestamp }`
  - 注: SQL 側の `*_cached` サフィックスは API では落とす（FE からは SNS mirror であることは隠蔽）。`internal/handler/*.go` のレスポンス shape を必ず突き合わせること。
- `UpdateUserProfileRequest` — `{ bio?: string (maxLength 500), banner_url?: string (maxLength 1024) }`、`additionalProperties: false`
- `Post` — ID / 著者 sub / content / reply_to_post_id (nullable) / tags / images / ogp_preview (nullable) / liked_by_viewer / following_author / likes_count / replies_count / created_at / updated_at。`internal/usecase/post/hydrate.go` の出力を必ず確認して一致させる。
- `PostCreateRequest` — `{ content: string (maxLength 120), reply_to_post_id?: ULID, image_ids?: ULID[] (maxItems TBD) }`、`additionalProperties: false`
- `Image` — `{ id: ULID, user_id: ULID, url: string (format uri), mime_type: string, file_size: integer (int64), width?: integer, height?: integer, created_at: Timestamp }`
- `Tag` — `{ name: string }`（実装側に ID があれば追加）
- `OGPPreview` — `{ url: string, title?: string, description?: string, image_url?: string, fetched_at: Timestamp, status: enum [pending, ok, failed] }`（`internal/domain/ogp.go` と突き合わせ）
- `Badge` — `{ id: ULID, key: string, label: string, description: string, color: enum [blue, gold], priority: integer, created_at: Timestamp, updated_at: Timestamp }`
- `UserBadge` — `{ user_sub: ULID, badge: Badge, granted_at: Timestamp }`
- `BadgeCreateRequest` / `BadgeUpdateRequest` / `BadgeGrantRequest` — handler 実装 (`internal/handler/badge.go`) と一致
- `FollowTarget` — フォロー一覧レスポンス形。`internal/handler/follow.go` の shape に合わせる。
- `TimelinePage` — `{ posts: Post[], next_cursor?: string }` 等（cursor の有無を `internal/handler/timeline.go` で確認）

すべての schema に `required` を明示し、optional フィールドは `required` に含めない。null 許容は `nullable: true` で表現。

### 2-4. パス / operation 定義（handler ↔ swagger を 1:1 で書く）

`cmd/server/main.go` 155 行目以降の `mux.HandleFunc` / `mux.Handle` を上から順に全エンドポイントを書き下ろす。以下 28 エンドポイント：

- `GET /health` — 公開、`security: []`
- `GET /me` — 要 Bearer
- `GET /users` / `GET /users/{sub}` / `PUT /users/{sub}`
- `GET /posts` / `POST /posts` / `GET /posts/{id}` / `GET /posts/{id}/replies` / `DELETE /posts/{id}` / `POST /posts/{id}/like` / `DELETE /posts/{id}/like`
- `POST /v1/images` / `GET /v1/images` / `DELETE /v1/images/{id}`（R2 未設定時は 404 になるので description に明記）
- `POST /users/{sub}/follow` / `DELETE /users/{sub}/follow` / `GET /users/{sub}/followers` / `GET /users/{sub}/following`
- `GET /timeline/home` / `GET /timeline/user/{sub}` / `GET /timeline/global`
- `GET /v1/admin/badges` / `POST /v1/admin/badges` / `PUT /v1/admin/badges/{id}` / `POST /v1/admin/users/{sub}/badges` / `DELETE /v1/admin/users/{sub}/badges/{badge_id}`

各 operation に:
- `tags`
- `operationId`（2-1 の命名規則）
- `summary` / `description`（英語、1-2 文）
- `parameters` — パス / クエリは schema 付き、`required` 明示、`in: query` で limit/offset は `default` と `minimum`/`maximum` を書く（handler の実装値と必ず一致）
- `requestBody` — 必要な場合。`application/json` 基本、`POST /v1/images` は `multipart/form-data`
- `responses` — 成功 + エラーは共通 `$ref: '#/components/responses/...'` を使用。ただし 400 は operation 固有の理由（バリデーション）なので `description` を上書きする
- `security: [{ BearerAuth: [] }]` または `security: []`（公開 GET）

### 2-5. handler ↔ swagger 網羅チェック

- `grep -hE 'mux\.(HandleFunc|Handle)\(' cmd/server/main.go | sed ...` で出した path list と、swagger 内 `grep -E '^  /' docs/swagger.yaml` で出した path list を突合
- 見落としゼロになるまで 2-4 を繰り返す
- 差分は PR 本文にチェックリストで貼る

### 2-6. 妥当性検証

- `npx @redocly/cli lint docs/swagger.yaml` がエラーゼロ（warn は許容、ただし内容を確認）
- `npx swagger-cli validate docs/swagger.yaml` が成功
- これらは Go リポジトリなので、検証は `README.md` の「Contributing」節に `npx` ワンライナーを添える形で残す（CI 追加は別タスク）

### 2-7. 型生成 smoke test

- 使い捨てディレクトリで `npx openapi-typescript docs/swagger.yaml -o /tmp/fuju-api.d.ts` を実行、エラーなく `.d.ts` が生成できることを確認（PR 本文にコマンドと成功ログを貼る）
- `orval` も同様に `npx orval --input docs/swagger.yaml --output /tmp/fuju-api.ts --client axios` を試行（任意、FE 側の生成パイプを想定した動作確認）

---

## Phase 3: プロダクト概要ドキュメント（新規）

### 3-1. `docs/product-overview.md` を新規作成

構成:
1. **プロダクト名と 1 行サマリ** — 「FUJU は AuthCore を認証基盤とする小規模 SNS のバックエンド」
2. **主要ユースケース** — 投稿 / 返信 / いいね / タグ / 画像添付 / OGP プレビュー / フォロー / タイムライン (home, user, global) / バッジ (admin 付与) / プロフィール編集
3. **ユーザーモデルの考え方** — `sub` が AuthCore 由来 ULID で不変 ID、FE では `display_id` (@handle 相当) を URL に使ってよい点を明記
4. **ID 体系** — 全リソース ID は ULID (CHAR(26))。旧 BIGINT は廃止済み。FE は文字列として扱う。
5. **タイムラインの並び順 / ページング** — home / user / global それぞれの実装挙動（cursor or offset、`internal/usecase/timeline/usecase.go` を一次資料に書く）
6. **OGP の非同期性** — post 作成直後は `ogp_preview.status=pending`。別タスク `03-implement-ogp-fetcher.md` 参照。
7. **画像アップロードの前提** — R2 設定時のみ有効、未設定時 404 の旨、サイズ上限 5MB
8. **Admin / バッジ** — `users.is_admin = true` のユーザーのみ `/v1/admin/*` が叩ける。初回 admin は Migration 005 で seed。
9. **エラーモデル** — `{ code, message }` の共通形、主要 `code` 一覧（handler 実装から抽出）
10. **バージョニング** — URL パスは混在中 (`/posts` ルート直下 + `/v1/images` + `/v1/admin/*`)。将来のマイグレーション方針は別タスクで議論。

### 3-2. 読み手ルート図を冒頭に配置

- 「FE が最初に読むべき順」: `README.md` → `docs/product-overview.md` → `docs/swagger.yaml`（型生成）→ `docs/authcore-integration.md`（認証フロー）
- `README.md` にも同じ導線を足す（Phase 5 で実施）

---

## Phase 4: AuthCore 連携ドキュメント（新規、FE 向け）

### 4-1. `docs/authcore-integration.md` を新規作成

**プロダクト概要と分ける理由**: FE の実装者が「認証だけ」「全体像だけ」を個別に読めるようにする。認証は詳細度が高く、プロダクト概要に混ぜると肥大化する。

構成:
1. **全体像** — AuthCore が identity provider、FUJU バックエンドは resource server。クライアント (web/mobile) は AuthCore から access token を取得し、FUJU へは Bearer で付与。
2. **トークン取得フロー (FE 視点)** — 詳細な OAuth 2.1 の仕様は AuthCore 側ドキュメントに委譲。FE が知るべきは「AuthCore の `/login` にリダイレクト → コード受領 → アクセストークン化（ここまで AuthCore 側）」と「FUJU API コールには必ず `Authorization: Bearer <access_token>`」の 2 点。
3. **トークン失効時の挙動** — 401 `code=unauthorized` → FE はトークン refresh または再ログインへ
4. **introspection キャッシュ** — バックエンド側 30s in-memory。FE からは透過だが、ログイン直後の短時間テストでは「直近の失効が反映されないことがある」旨を注記。
5. **`/me` と hydrate フロー** — FE が最初に叩くべきは `GET /me`。バックエンドが AuthCore profile を取得して users 行を upsert する（`internal/middleware/middleware.go`, `internal/usecase/user/usecase.go` を参照）。
6. **AuthCore profile の mirror TTL (1h)** — `display_name` 等が古い可能性がある旨、最新化は次回アクセス or `AUTHCORE_PROFILE_TTL` 再設定で
7. **Admin 判定** — AuthCore 側ではなく FUJU の `users.is_admin`。FE は `GET /me` の `is_admin` を見る。
8. **セキュリティ上の FE 責務** — トークンの保存先（推奨: メモリ + refresh は httpOnly cookie by AuthCore）、CSRF、XSS。
9. **CORS** — `CORS_ALLOWED_ORIGINS` 環境変数で許可。開発中は `*`、本番は必ず限定。
10. **既知の制約** — Phase 2 時点で未配線な機能（DB, Redis, rate limit 等）を明示し、FE の期待値を揃える。

### 4-2. 「旧 Cookie セッション方式ではない」ことの明記

- 過去の README / IMPLEMENTATION_GUIDE には OAuth2 + Session Cookie の記述があったが、現行は AuthCore 発行 Bearer JWT のみ。ドキュメント冒頭で「本リポはバックエンドが Cookie を書き込まない。`Set-Cookie` は返らない」と注記。
- 既存の `docs/IMPLEMENTATION_GUIDE.md` 内 AuthCore セクション（220 行目以降）は内容が古い（「Cookie `authcore_session`」）。Phase 5 で削除 or リンク張り替え。

---

## Phase 5: 既存ドキュメントの旧記述更新

Phase 1 では「旧用語の削除」をしたが、置換コピーの完成版は Phase 3 / 4 の新規ドキュメントに集中させ、既存ドキュメントからは「詳細はこちらへ」リンクを張るのが整備の主眼。

### 5-1. `README.md` のエントリポイント化

- 「Quick Links」節を冒頭近くに新設:
  - `docs/product-overview.md` — プロダクト概要
  - `docs/authcore-integration.md` — 認証フロー（FE 開発者向け）
  - `docs/swagger.yaml` — API リファレンス（型生成対応）
  - `docs/architecture.md` — 内部アーキテクチャ（保守者向け）
- Phase 1-11 で書き換えた AuthCore 記述は残しつつ、詳細は authcore-integration.md へのリンクに短縮
- 「Getting Started」の手順から Redis 項目を除去（Phase 1-4 の `.env.example` と整合）

### 5-2. `docs/IMPLEMENTATION_GUIDE.md` の Cookie 記述の整理

- 220 行目以降の AuthCore 連携節（「Cookie `authcore_session`」「Cookie 仕様」等）を削除、「Bearer JWT + RFC 7662 introspection。詳細は `docs/authcore-integration.md`」に差し替え
- 740-744 行目の middleware 疑似コード（`cookie.Value` を使っている箇所）も現行実装（Authorization ヘッダ）に合わせて書き換え
- 1033 行目以降の「Cookie 値を毎リクエスト AuthCore の introspection エンドポイントで検証」等 → 「Authorization: Bearer を毎リクエスト AuthCore で検証」に
- 1040 / 1045-1053 の Cookie 属性節 → 削除または「AuthCore 側責務」と一行で示す
- 1102 / 1122-1123 の OAuth / Cookie ログ記述 → 「アクセストークン値をログに出さない」に一般化

### 5-3. `docs/architecture.md` のメンテナンス

- Phase 1-7 のクリーンアップで Redis 記述は消えた前提
- 「Middleware チェーン」節があれば現行 `main.go` 142-153 行目の順序（AuthMiddleware → HydrateUserMiddleware → (Admin)）を反映
- 「Future Work」節は既存通り残す

### 5-4. `docs/tasks/README.md` の追記

- 本タスク 06 を表に追加:
  ```
  | 06 | 06-frontend-handoff-docs-and-dead-code-removal.md | 01-05 | 確定済み |
  ```
- 「マイグレーション番号の割り当て」表には本タスクで追加する migration はないので触らない

---

## Phase 6: 検証

### 6-1. OpenAPI 妥当性

- `npx @redocly/cli lint docs/swagger.yaml` — エラーゼロ
- `npx swagger-cli validate docs/swagger.yaml` — OK
- PR 本文に出力を貼る

### 6-2. 型生成 smoke test

- `npx openapi-typescript docs/swagger.yaml -o /tmp/fuju-api.d.ts && wc -l /tmp/fuju-api.d.ts`
- 生成 `.d.ts` を目視で主要な型 (`User`, `Post`, `Badge`, `components["schemas"]["Error"]`) が存在することを確認
- 任意: `npx orval` も試行

### 6-3. ドキュメント内リンクチェック

- `grep -rn '\[.*\](.*\.md)' docs/ README.md .agent.md` で相対リンクを列挙し、`[ -f ... ]` で物理存在確認
- リンク切れゼロを確認

### 6-4. 旧用語の残存再チェック（Phase 1-13 の最終リプレイ）

- `grep -rni "oauth\|session\|cookie\|redis" --include="*.md" --include="*.yaml" --include="*.yml" --include=".env.example" --include="Makefile" docs/ README.md .agent.md docker-compose.yml Makefile .env.example` — ホワイトリスト（`pkg/authcore` / `docs/tasks/01-*` / `docs/tasks/README.md` / `docs/tasks/04-*` / `docs/architecture.md` の Future Work 節）以外ヒットゼロ
- 差分が出たらドキュメント修正 or ホワイトリスト追記

### 6-5. handler 実装との最終照合

- `internal/handler/*.go` の各 handler が返す JSON フィールド名 / 型が swagger schema と完全一致することを目視確認
- 照合用スクリプトは不要（28 endpoint、1 時間程度）

---

## ドキュメント構成案（最終形）

### 新規作成

| パス | 役割 | 主読者 |
|---|---|---|
| `docs/product-overview.md` | プロダクト概要・主要ユースケース・ID 体系・エラーモデル | FE 開発者、新規保守者 |
| `docs/authcore-integration.md` | AuthCore 連携の FE 向け詳細（トークン / 失効 / CORS / admin） | FE 開発者 |

### 全面刷新

| パス | 変更内容 |
|---|---|
| `docs/swagger.yaml` | OpenAPI 3.0.3 で 28 エンドポイント + 共通 schema / responses を厳密定義 |

### 部分修正

| パス | 変更内容 |
|---|---|
| `README.md` | Phase 1-11（旧 AuthN 削除）+ Phase 5-1（Quick Links 追加） |
| `docs/IMPLEMENTATION_GUIDE.md` | Phase 1-10（comments 削除）+ Phase 5-2（Cookie 節削除） |
| `docs/architecture.md` | Phase 1-7（Redis 削除） |
| `docs/DATABASE_SETUP.md` | Phase 1-6（Redis / OAuth states / comments 削除） |
| `.agent.md` | Phase 1-8（AuthN / Redis / OAuth サンプル） |
| `docs/tasks/README.md` | Phase 5-4（本タスク追記） |
| `config/config.go` | Phase 1-1, 1-2（RedisURL / FrontendURL / DBMaxConn / DBMinConn 削除） |
| `.env.example` | Phase 1-4 |
| `docker-compose.yml` | Phase 1-3 |
| `Makefile` | Phase 1-5 |

---

## swagger 網羅チェック手順（再掲・運用手順として）

1. handler 側の path 一覧を抽出:
   ```
   grep -hE 'mux\.(HandleFunc|Handle)\(' cmd/server/main.go \
     | sed -E 's/.*"(GET|POST|PUT|DELETE|PATCH) ([^"]+)".*/\1 \2/' \
     | sort -u
   ```
2. swagger 側の path 一覧を抽出:
   ```
   grep -E '^  /' docs/swagger.yaml | sed -E 's/^  //; s/:.*$//'
   ```
3. 2 の path を method 付きに展開（`get:` / `post:` 等）して 1 と突合
4. 差分ゼロを PR に貼付

---

## 削除候補一覧（Phase 1 確定版、実装前にレビュー対象）

| # | 種別 | 対象 | 根拠 |
|---|---|---|---|
| D1 | Go config | `config.Config.RedisURL` + loader + Validate | Go 側参照ゼロ |
| D2 | Go config | `config.Config.FrontendURL` | Go 側参照ゼロ |
| D3 | Go config | `config.Config.DBMaxConn` / `DBMinConn` | Go 側参照ゼロ（DB 未配線） |
| D4 | compose | `services.redis` / `redis_data` volume / `backend.REDIS_URL` / `backend.depends_on.redis` | Redis 未使用 |
| D5 | env | `.env.example` `REDIS_URL` | D1 連動 |
| D6 | env | `.env.example` `FRONTEND_URL` | D2 連動 |
| D7 | env | `.env.example` の `# OAuth2 client credentials...` コメント文言 | AuthCore 現仕様に合わせて訂正 |
| D8 | Makefile | `db-up` の `Redis: localhost:6379` echo | D4 連動 |
| D9 | swagger | `/auth/oauth/authorize`, `/auth/oauth/callback`, `/auth/refresh`, `/auth/logout` パス / `CookieAuth` security scheme | 実装に存在しない |
| D10 | doc | `docs/DATABASE_SETUP.md` Redis / OAuth States / sessions / oauth_provider 記述 | 実装に存在しない |
| D11 | doc | `docs/architecture.md` Redis client wrapper / `Caching: Redis` 記述 | Redis 未使用 |
| D12 | doc | `.agent.md` 「OAuth2 + JWT + Session Cookies」「Redis Usage」「oauth_callback.go」サンプル | 現仕様と不整合 |
| D13 | doc | `docs/IMPLEMENTATION_GUIDE.md` `comments` 暫定節 / `POST /posts/{id}/comments` / `CommentsCount` / 「Comment ドメインモデル」節 | reply 統合済み |
| D14 | doc | `docs/IMPLEMENTATION_GUIDE.md` Cookie 仕様 / `cookie.Value` 疑似コード | Bearer 方式に移行済み |
| D15 | doc | `README.md` 旧 `OAUTH_*` / `SESSION_SECRET` / Session Cookie / Redis 記述 | 現仕様と不整合 |

**保守者レビュー観点**: 上記 D1〜D15 すべて実装への影響ゼロで削除可能。DB_HOST 系は残す（migrations / docker-compose postgres サービスを生かすため）。`docs/architecture.md` 494 行目の「Future Work: Redis-backed introspection cache」は設計メモとして残す。

---

## 検証 / テスト方針

- **OpenAPI validity**: `npx @redocly/cli lint` / `npx swagger-cli validate` の両方通過
- **型生成 smoke**: `npx openapi-typescript` で `.d.ts` が生成でき、主要型が出ていること
- **Go ビルド / テスト**: `make build && make test` が通る（Phase 1 の config 削除で `go vet` / `go build` が壊れないこと）
- **lint**: `make lint` 通過（config フィールド削除後、`config_test.go` があれば更新）
- **リンクチェック**: `grep` による相対リンクの物理存在確認
- **旧用語残存 grep**: Phase 6-4 のホワイトリスト付き grep がゼロ件

## スコープ外

- 実 Postgres ドライバ配線（`internal/repository/postgres/` 新設） — 別タスク
- Redis 再導入（introspection cache の分散化等） — 別タスク
- OpenAPI から Go server stub 生成 (`oapi-codegen` 等) — 別タスク
- CI への OpenAPI lint 組み込み — 別タスク
- swagger を OpenAPI 3.1 に上げる — 別タスク（orval 側対応待ち）
- 英語ドキュメント化 — 別タスク（現状の日本語優先方針に従う）
- FE 側リポジトリでの SDK 生成コミット — FE 側タスク

## 技術的な補足

- **handler が返す JSON shape は handler 実装を一次資料とする**。`internal/domain/*.go` ではなく `internal/handler/*.go` および `internal/usecase/post/hydrate.go` のレスポンス構築部を最終的な正とする。両者に差分があれば Phase 2-3 中に別タスク起票して本タスクはドキュメント側を実装に合わせる（実装修正はスコープ外）。
- **`/v1` の混在**: 画像 (`/v1/images`) と admin バッジ (`/v1/admin/*`) のみ `/v1` プレフィックス、他はルート直下。これは現行実装の事実であり swagger にもそのまま反映する。統一は別タスクで議論。
- **Phase 間の PR 分割**: Phase 1 だけで 1 PR、Phase 2 で 1 PR、Phase 3+4+5+6 で 1 PR、の 3 分割が目安。Phase 1 は削除のみで差分が明瞭なのでレビュー負荷が低い。
- **develop ブランチ**: PR ターゲットは `develop`（main 直行しない）。
- **`[未確定]` マーカー**: 本ドキュメント内にはなし。Phase 2 着手時に handler 実装と domain で shape 差分が見つかった場合のみ該当箇所に `[未確定]` を打ち、レビュー対象に上げる。
