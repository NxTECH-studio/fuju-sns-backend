# Rewrite Domain to AuthCore Alignment

## 概要

User ドメインを AuthCore（外部の統合認証基盤）前提に再設計する。SNS 側は認証の真実源を AuthCore に委譲し、`users` テーブルは AuthCore からの鏡像キャッシュ + SNS 固有プロフィール属性のみを保持する。既存の OAuth / JWT 自前実装は削除する。併せて **既存の BIGSERIAL / UUID 主キーをすべて ULID(CHAR(26)) に統一**する。

## 前提（依存タスク）

- **なし**（全タスクのルート）
- 本タスクは単独で完了可能。他タスクはすべて本タスク完了後に着手する。

## 背景・目的

現状、本リポジトリは OAuth2 + 自前 JWT 発行という「SNS 単独の認証実装」を前提に設計されている（`internal/handler/auth.go`, `pkg/auth/auth.go`, `internal/middleware/middleware.go` の `AuthMiddleware`、`users` テーブルの `oauth_provider` / `oauth_id` / `email` 列など）。

しかし最終形では、認証は別サービス **AuthCore** に集約し、SNS バックエンドは以下を前提に動く:

- ユーザーの identity（sub = ULID）、DisplayName、DisplayId、IconURL は AuthCore が所有
- 認証セッションは AuthCore が発行した Cookie ベース（SNS バックエンドは毎リクエスト Introspection で検証）
- SNS バックエンドは AuthCore から取得したプロフィール情報を **1h TTL の鏡像キャッシュ** として `users` テーブルに保持
- SNS 固有属性（Bio、Banner、ユーザーが書き換え可能な表示用補助情報）のみを SNS バックエンドが真の所有者として管理

この変更により:

- identity/認証責務の二重管理をなくす（`oauth_provider + oauth_id + email` の自前保持を廃止）
- 他機能（投稿・フォロー・通知など）すべてを AuthCore sub(ULID) ベースで書けるようになる
- 後続タスク（Post / Follow / Badge / OGP）の前提となる「認証済みユーザーの文脈」が確立する

本タスクは **疎結合の土台** であり、他のタスク（Post 機能等）に依存しない単独実行可能な形で完了させる。

## マイグレーション方針（重要）

本番 DB は未稼働である前提で、**既存 `db/migrations/001_initial_schema.sql` と `db/migrations/002_add_images_table.sql` を破壊的に直接書き換える**方針を採用する。新しい 003 以降を重ねて差分 ALTER する案は採らない。理由:

- 本番未稼働なので破壊的変更による dev data 消失のみが影響。運用への影響なし
- `id BIGSERIAL` → `sub CHAR(26)` / `id UUID` → `id CHAR(26)` の型変換は、同じ空間で差分 ALTER を書くより一から定義し直したほうが可読性が高い
- Migration ファイル数を最小に保てる

### 具体的な操作

- **001 を書き換える**: 新しい `users` スキーマ（`sub CHAR(26)` PK, `is_admin` カラム含む）を定義する。旧 `sessions` / `oauth_states` テーブルは **削除**（AuthCore 移譲で不要）。旧 `likes` テーブルは **削除**（02-post の 004 で `user_id CHAR(26)` 版として再定義）。旧 `comments` テーブルは 01 時点では残置（02-post で `DROP` する）し、`user_id` / `post_id` を `CHAR(26)` 型に合わせる。
- **002 を書き換える**: `images.id` を `UUID DEFAULT gen_random_uuid()` → `CHAR(26)`（アプリ側で ULID 生成）、`images.user_id` を `BIGINT` → `CHAR(26)` に変更し、FK を `users(sub)` に張り直す。
- **003 以降は新規ファイル** として追加する（本タスクで単独の 003 ファイルは作らず、001/002 の直接書き換えで完結させる）。

## 影響範囲

### 変更対象

| 種類 | パス | 変更内容 |
|---|---|---|
| domain | `internal/domain/user.go` | User エンティティの再設計（PK を ULID 文字列に、`OAuthProvider/OAuthID/Email/Username` 廃止、`IsAdmin bool` を追加） |
| domain | `internal/domain/errors.go` | `DuplicateUsernameError` / `DuplicateEmailError` 廃止 or 意味変更 |
| repository | `internal/repository/repository.go` | `UserRepository` の key を `int64` → `string`(ULID) に。`GetByOAuthID` / `GetByUsername` / `GetByEmail` を削除、`Upsert` / `GetBySub` / `TouchProfileRefreshed` を追加 |
| repository | `internal/repository/inmemory/inmemory.go` | 上記インターフェース変更への追随 |
| usecase | `internal/usecase/user/usecase.go` | `CreateUserUseCase` を削除（lazy create に統合）、`GetOrHydrateUserUseCase` を新設、`UpdateUserUseCase` は SNS 固有属性（Bio/Banner）のみ対象に縮退 |
| handler | `internal/handler/user.go` | `POST /users` を廃止（lazy create）、`PUT /users/{id}` は Bio/Banner のみ |
| handler | `internal/handler/auth.go` | **全削除**（OAuth authorize / callback / refresh / logout すべて AuthCore に移譲） |
| middleware | `internal/middleware/middleware.go` | `AuthMiddleware` を差し替え（Cookie → AuthCore Introspection → sub を Context に格納） |
| pkg | `pkg/auth/auth.go` | **全削除**（JWT 発行・検証は不要に） |
| config | `config/config.go` | `OAuth*`, `JWT*`, `SessionSecret` を削除。`AUTHCORE_BASE_URL`, `AUTHCORE_INTROSPECT_PATH`, `AUTHCORE_SERVICE_TOKEN`, `AUTHCORE_PROFILE_TTL`（デフォルト 1h）を追加 |
| entry | `cmd/server/main.go` | ルーティングから `/auth/*` を削除、`AuthCoreClient` の DI、`AuthMiddleware` の差し替え |
| DB | `db/migrations/001_initial_schema.sql` | **破壊的書き換え**（上記「マイグレーション方針」参照） |
| DB | `db/migrations/002_add_images_table.sql` | **破壊的書き換え**（`images.id` / `images.user_id` を ULID 化） |
| 新規 | `pkg/authcore/client.go` | AuthCore への HTTP クライアント（Introspection / GetProfile） |
| 新規 | `pkg/authcore/cache.go` | Introspection の短寿命キャッシュ（後述） |

### 破壊的変更

- `users.id` が廃止され `sub CHAR(26)` PK に変わる。**既存の開発用 in-memory / Postgres データは全廃棄**。
- `images.id` が `UUID` → `CHAR(26)`(ULID) に変わる。`gen_random_uuid()` デフォルトは廃止し、アプリ層で ULID を生成する。
- `POST /users` と `POST /auth/*` エンドポイントが廃止される。フロントエンドへの影響あり（別途周知）。
- `Post.UserID` / `Comment.UserID` / `Image.UserID` もすべて `string`(ULID) になる。本タスクでは User / Image 周辺を書き換え、Post / Comment の型は **「同じ PR 内で最小限に追随」** する（全 `int64` 参照を `string` に機械的に置換）。Post のロジック的な深掘り（ULID 化、リプライ、いいねなど）は `02-implement-post-feature.md` で扱う。

## 目標状態の差分（Before / After）

### users テーブル

**Before** (`db/migrations/001_initial_schema.sql` / `internal/domain/user.go`):

```
users(
  id BIGSERIAL PK,
  username UNIQUE,
  email,
  display_name, bio, avatar_url,
  oauth_provider, oauth_id,
  created_at, updated_at, deleted_at
)
```

**After**（`db/migrations/001_initial_schema.sql` を直接書き換え）:

```sql
CREATE TABLE users (
  sub                   CHAR(26)     PRIMARY KEY,         -- AuthCore の ULID（真実源は AuthCore）
  -- AuthCore からの鏡像キャッシュ（1h TTL で再取得）
  display_name_cached   VARCHAR(255) NOT NULL DEFAULT '',
  display_id_cached     VARCHAR(64)  NOT NULL DEFAULT '', -- @handle 相当
  icon_url_cached       VARCHAR(1024) NOT NULL DEFAULT '',
  profile_refreshed_at  TIMESTAMPTZ  NOT NULL DEFAULT '1970-01-01',
  -- SNS 固有属性（SNS が真の所有者）
  bio                   TEXT         NOT NULL DEFAULT '',
  banner_url            VARCHAR(1024) NOT NULL DEFAULT '',
  -- 権限フラグ
  is_admin              BOOLEAN      NOT NULL DEFAULT false,  -- 運営 admin フラグ。badge タスクの admin API で参照される
  -- メタ
  created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
  deleted_at            TIMESTAMPTZ  NULL
);

CREATE INDEX idx_users_profile_refreshed_at ON users(profile_refreshed_at);
CREATE INDEX idx_users_display_id_cached   ON users(display_id_cached);
```

方針:

- `sub` は ULID（26 文字, Crockford Base32）。PK を文字列にすることの性能懸念はあるが、規模とジョインパターンを踏まえ許容する
- `*_cached` サフィックスで「AuthCore のスナップショット」であることを明示
- `bio` と `banner_url` のみ SNS 固有。編集はこの 2 つに対してのみ許可
- `is_admin` は SNS 運営上のフラグ（AuthCore とは独立）。デフォルト `false`。**本タスクではカラム追加のみ行い、admin 付与ロジック（どの sub を true にするか）は `02-implement-badge.md` の Migration 005 で seed UPDATE を打つ**

### images テーブル

**Before** (`db/migrations/002_add_images_table.sql`):

```sql
CREATE TABLE images (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  ...
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  ...
);
```

**After**:

```sql
CREATE TABLE images (
  id           CHAR(26)     PRIMARY KEY,              -- ULID（アプリ層で生成）
  storage_key  VARCHAR(2000) NOT NULL UNIQUE,
  file_name    VARCHAR(500)  NOT NULL,
  mime_type    VARCHAR(100)  NOT NULL,
  file_size    BIGINT        NOT NULL,
  public_url   VARCHAR(2000) NOT NULL,
  user_id      CHAR(26)      NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
  deleted_at   TIMESTAMPTZ   NULL
);
CREATE INDEX idx_images_user_id     ON images(user_id);
CREATE INDEX idx_images_created_at  ON images(created_at DESC);
CREATE INDEX idx_images_storage_key ON images(storage_key);
CREATE INDEX idx_images_deleted_at  ON images(deleted_at);
```

- `gen_random_uuid()` による DB 側自動採番は廃止。**ULID はアプリ層で `ulid.Make().String()` により生成**して INSERT する。
- `Image` domain の `ID string` / `UserID string` を本タスクで確定する（02-post の `post_images.image_id CHAR(26)` との整合が取れる）。

### User ドメイン（目標シグネチャ）

```go
type User struct {
    Sub                string    // AuthCore sub (ULID)
    DisplayNameCached  string
    DisplayIDCached    string
    IconURLCached      string
    ProfileRefreshedAt time.Time
    Bio                string
    BannerURL          string
    IsAdmin            bool      // 運営 admin フラグ（badge の admin API 等で参照）
    CreatedAt, UpdatedAt time.Time
    DeletedAt *time.Time
}

// SNS が書き換え可能な属性はこれだけ
type UpdateUserProfileRequest struct {
    Bio       *string
    BannerURL *string
}
```

注意:
- `IsAdmin` は **ユーザーによる自己書き換え不可**。`UpdateUserProfileRequest` には含めない
- hydrate（lazy create / refresh）フローでも `IsAdmin` は AuthCore から取得しない（SNS 固有属性）。初回 INSERT 時は DEFAULT false、以降は DB 直接 UPDATE または将来の admin 管理タスクで変更

## 二層構造：認証 vs プロフィール

このタスクで導入する **最重要の設計判断** は、AuthCore との通信を二層に切り分けることである。

| 層 | 目的 | 頻度 | TTL |
|---|---|---|---|
| **Introspection** | Cookie が有効か／sub が誰か | **毎リクエスト**（= auth middleware） | 短寿命キャッシュ（30 秒、in-memory）で AuthCore 負荷を抑制 |
| **Profile Hydration** | DisplayName / DisplayId / IconURL の取得 | **users テーブル書き込み時のみ**（lazy） | **1 時間**（`profile_refreshed_at` で管理） |

この分離により:

- 認証そのもの（sub の確定）は常に最新を保証（revoke に追随）
- プロフィール表示は 1h まで古くても UX 上問題ない → AuthCore への N+1 的な問い合わせを回避

### Introspection 短寿命キャッシュ

- Key: `sessionCookieValue`（不透明トークン）
- Value: `{sub, expiresAt}`
- TTL: 30 秒 or `expiresAt - now` の小さい方
- 実装: in-memory（`sync.Map` + 単純な TTL eviction）で十分。Redis 導入は後続タスクで検討

### AuthCore ダウン時の挙動

- **Introspection 失敗時**:
  - **Fail-closed**（401 を返す）を原則とする。ただし直近 30 秒以内の短寿命キャッシュがあればそれを使う（一時的なブリップに耐える）
  - これは「認証」なので保守的に
- **Profile Hydration 失敗時**:
  - **Fail-open**。既存の `*_cached` をそのまま返し、`profile_refreshed_at` は更新しない（次リクエスト時に再試行）
  - 初回 hydrate（= lazy create 時）に失敗した場合は空文字列のまま INSERT してよい。後続リクエストで自動的にリトライされる

## lazy create フロー

AuthCore で認証されたが SNS 側の `users` 行がまだない場合、ミドルウェアの後段（あるいはユースケース層の入口）で自動作成する。

```
AuthMiddleware:
  1. Cookie 取得
  2. Introspection → sub 確定
  3. Context に sub をセット
  4. next.ServeHTTP

各ユースケースの入口（または dedicated "hydrate" middleware）:
  GetOrHydrateUser(ctx, sub):
    user = repo.GetBySub(sub)
    if user == nil:
        profile = authcore.GetProfile(sub)  // AuthCore から取得
        user = &User{Sub: sub, DisplayNameCached: profile.DisplayName, ...
                     IsAdmin: false,  // 新規行は常に false
                     ProfileRefreshedAt: now}
        repo.Upsert(user)
    else if now - user.ProfileRefreshedAt > 1h:
        profile, err = authcore.GetProfile(sub)
        if err == nil:
            user.DisplayNameCached = profile.DisplayName
            user.DisplayIDCached   = profile.DisplayID
            user.IconURLCached     = profile.IconURL
            user.ProfileRefreshedAt = now
            // IsAdmin は hydrate では更新しない（SNS 固有属性）
            repo.Upsert(user)
        // err != nil は fail-open（古い値を使う）
    return user
```

設計メモ:

- `GetOrHydrateUser` は **全認証必須エンドポイントで最初に呼ばれる共通フック**。各ユースケースの先頭で呼ぶか、認証ミドルウェアの直後に hydrate ミドルウェアを挟むか、どちらかを選ぶ（後者のほうが漏れがなく推奨）
- 公開エンドポイント（`GET /users/{sub}` 等）で他人のプロフィールを見る場合も同じフックを使えるよう、sub を引数に取る設計にする
- `Upsert` は `IsAdmin` を保持する（既存行がある場合は既存の `is_admin` を維持し、hydrate で上書きしない）。SQL 実装では `ON CONFLICT (sub) DO UPDATE SET display_name_cached = EXCLUDED.display_name_cached, ... （is_admin は更新対象外）` の形にする

## 実装ステップ

Clean Architecture の外→内の順で書き始めたくなるが、型が伝播するため **内側（domain）から** 始めて外に出るのが安全。

### Step 1: DB Migration（既存 001 / 002 の破壊的書き換え）

- `db/migrations/001_initial_schema.sql` を上記「After」スキーマに全面書き換え
  - 新 `users`（`sub CHAR(26)` PK, `is_admin` カラム含む）
  - `posts` テーブルは `id CHAR(26)` / `user_id CHAR(26)` のプレースホルダとして最小列のみ残す（本格的な列追加は 02-post の 004 で ALTER）
  - `comments` テーブルは `id / post_id / user_id` を CHAR(26) 化して残置（02-post で DROP）
  - 旧 `sessions` / `oauth_states` / 旧 `likes` は **削除**
- `db/migrations/002_add_images_table.sql` を上記「After」スキーマに書き換え（`id CHAR(26)` / `user_id CHAR(26)`、FK は `users(sub)`）
- **admin 付与の seed SQL は本タスクでは打たない**。`02-implement-badge.md` 側の migration（005）で打つ

### Step 2: Domain 層

- `internal/domain/user.go`:
  - `User` 構造体の刷新（上記シグネチャ。`IsAdmin bool` を含む）
  - `Validate()` から `Username`/`Email`/`OAuthProvider`/`OAuthID` 検証を削除、`Bio`（500 文字以内）と `BannerURL`（1024 文字以内）のみ残す
  - `UpdateUserRequest` → `UpdateUserProfileRequest` に改名、編集対象を Bio/Banner のみに絞る（`IsAdmin` は含めない）
- `internal/domain/errors.go`:
  - `DuplicateUsernameError` / `DuplicateEmailError` を削除（identity は AuthCore の責務）
- `internal/domain/image.go`:
  - `Image.ID` / `Image.UserID` を `string` 化。ID はアプリ層で ULID を採番する責務を持つ（repository.Create の前で生成 or domain コンストラクタで生成）
- `Post` / `Comment` の `ID` / `UserID` を `int64` → `string` に変更（最小限の追随）

### Step 3: Repository 層

- `internal/repository/repository.go`:
  - `UserRepository` インターフェースを次のように変更:
    ```go
    type UserRepository interface {
        GetBySub(ctx, sub string) (*User, error)
        Upsert(ctx, user *User) (*User, error)            // lazy create / refresh で使う。is_admin は既存値を保持
        UpdateProfile(ctx, sub string, req *UpdateUserProfileRequest) (*User, error) // Bio/Banner 編集
        List(ctx, limit, offset int) ([]*User, int, error)
        Delete(ctx, sub string) error
    }
    ```
  - `GetByUsername` / `GetByEmail` / `GetByOAuthID` / `Create` を削除
  - `ImageRepository` / `Post`/`Comment` のリポジトリ内の `ID` / `UserID` 参照型も追随
- `internal/repository/inmemory/inmemory.go`:
  - map key を `int64` → `string` に変更
  - `idSeq` を廃止（sub は AuthCore 由来 or caller が ULID を渡す）
  - `Upsert` 実装では、既存行があれば `IsAdmin` を保持する

### Step 4: AuthCore Client（新規）

- `pkg/authcore/client.go`:
  ```go
  type Client interface {
      Introspect(ctx context.Context, sessionCookie string) (*Session, error)
      GetProfile(ctx context.Context, sub string) (*Profile, error)
  }
  type Session struct { Sub string; ExpiresAt time.Time }
  type Profile struct { Sub, DisplayName, DisplayID, IconURL string }
  ```
  - 実装は `net/http` で十分。タイムアウトは Introspect=500ms, GetProfile=1s を目安
  - エラー型を分ける: `ErrInvalidSession`（401 扱い）vs `ErrUpstream`（一時障害扱い）
- `pkg/authcore/cache.go`:
  - Introspection 結果の 30 秒 in-memory キャッシュ（`sync.Map` + goroutine で定期 eviction）
- `pkg/authcore/client_test.go`:
  - `httptest.Server` でモックし、正常系・401・500・タイムアウトを網羅

### Step 5: Usecase 層

- `internal/usecase/user/usecase.go`:
  - `CreateUserUseCase` を **削除**
  - `GetOrHydrateUserUseCase` を新設（上述 lazy create フロー）
  - `GetUserUseCase` は `sub` を受け取る形に変更（他者プロフィール閲覧用）
  - `UpdateUserUseCase` → `UpdateUserProfileUseCase` に改名、Bio/Banner のみ
  - `ListUsersUseCase` は一旦残す（影響小）
- 依存: `AuthCoreClient` を `GetOrHydrateUserUseCase` に DI する

### Step 6: Handler 層

- `internal/handler/auth.go`: **ファイルごと削除**
- `internal/handler/user.go`:
  - `POST /users` ハンドラ（`CreateUser`）を削除
  - `GetUser(w, r)` は `r.PathValue("sub")` で sub（文字列）を取るよう変更
  - `UpdateUser` は `UpdateUserProfileUseCase` を呼ぶ形に
  - `GET /me`（自分自身を返す）を新設。内部で `GetOrHydrateUserUseCase` を呼ぶ
  - レスポンスに `is_admin` を含めるかは要検討（自分自身については `GET /me` で返す、他人の `GET /users/{sub}` では返さない、が無難）
- 型の機械的追随: `parseUserIDFromPath` を `parseSubFromPath` に（validation: 長さ 26, Crockford Base32 の正規表現チェック）

### Step 7: Middleware 層

- `internal/middleware/middleware.go`:
  - 既存 `AuthMiddleware(tokenManager)` を削除
  - `AuthMiddleware(authcoreClient)` を新実装:
    - `r.Cookie("authcore_session")` を読む（Cookie 名は AuthCore 仕様に合わせる、config で可変に）
    - 空なら 401
    - `authcoreClient.Introspect(ctx, cookie.Value)` → sub 取得（内部で短寿命キャッシュ）
    - 失敗種別を見て 401 or 503
    - `ctx = context.WithValue(ctx, subKey, sub)` して next
  - `HydrateUserMiddleware(useCase)` を新実装（上記 lazy create をここに挟む選択肢）:
    - 認証後、`GetOrHydrateUserUseCase.Execute(ctx, sub)` を呼ぶ
    - 結果を `ctx` に `currentUserKey` で格納
- ヘルパの入れ替え:
  - `auth.GetUserIDFromContext(ctx) (int64, bool)` → `auth.GetSubFromContext(ctx) (string, bool)` / `auth.GetCurrentUserFromContext(ctx) (*domain.User, bool)`
  - `pkg/auth/auth.go` は **Context キーのヘルパだけ残して JWT 関連は全削除**、または `pkg/session/context.go` に改名（小さいのでファイル移動で OK）

### Step 8: Config & Entry

- `config/config.go`:
  - 削除: `OAuthClientID`, `OAuthClientSecret`, `OAuthRedirectURL`, `JWTSecret`, `JWTExpiration`, `SessionSecret`, `SessionDuration`
  - 追加: `AuthCoreBaseURL`, `AuthCoreServiceToken`, `AuthCoreSessionCookieName`（default: `authcore_session`）, `AuthCoreProfileTTL`（default: `1h`）, `AuthCoreIntrospectCacheTTL`（default: `30s`）
  - **`ADMIN_SUBS` 環境変数は追加しない**（admin 判定は `users.is_admin` で行う。`02-implement-badge.md` 参照）
- `cmd/server/main.go`:
  - `authcoreClient := authcore.New(cfg.AuthCoreBaseURL, cfg.AuthCoreServiceToken)`
  - `AuthMiddleware` / `HydrateUserMiddleware` の差し替え
  - `/auth/*` ルート削除、`/users/*` ルートの一部変更、`/me` 追加

### Step 9: 削除対象リスト（明示）

- `internal/handler/auth.go`（全行）
- `pkg/auth/auth.go` の JWT 部分（`TokenManager`, `UserClaims`, `GenerateAccessToken`, `GenerateRefreshToken`, `ValidateToken`）
- `config.Config` の OAuth/JWT/Session 関連フィールドと検証
- `go.mod` から `github.com/golang-jwt/jwt/v5` を削除（他で使っていなければ）
- `POST /auth/*` / `POST /users` ルート登録

### Step 10: 既存ドキュメントの更新（01 完了時に一括）

本タスクの末尾で以下のドキュメントを AuthCore 前提・ULID 前提・reply 統合（Comment 廃止）に書き換える。具体的差分はここには書かないが、**書き換え対象のチェックリスト** を残す:

- `docs/architecture.md`
  - [ ] OAuth2 / 自前 JWT を前提にした図・解説を「AuthCore + Introspection」に書き換え
  - [ ] `users` / `posts` / `comments` テーブル DDL の `BIGINT` / `BIGSERIAL` を `CHAR(26)` に
  - [ ] `oauth_provider` / `oauth_id` / `email` / `username` の記述を削除（AuthCore 委譲）
  - [ ] `/auth/oauth/*` エンドポイントの記載を全削除、`/me` 追加
  - [ ] Comment エンドポイントの記載を削除（02-post で Post の reply に統合される旨の注記に差し替え）
- `docs/IMPLEMENTATION_GUIDE.md`
  - [ ] アーキテクチャ冒頭の「認証: OAuth2（Google/GitHub）+ JWT」を「認証: AuthCore Introspection」に変更
  - [ ] `User.ID int64` / `Post.UserID int64` / `Comment` の記述を `sub string(ULID)` / `string(ULID)` / 削除に
  - [ ] OAuth フロー図・JWT トークン管理セクションを削除、代わりに「AuthCore Introspection フロー」「lazy create + 1h hydrate」セクションを追加
  - [ ] `JWT_SECRET` 等の env 記述を削除、`AUTHCORE_*` 系に書き換え
  - [ ] Comment ドメインモデルのセクションを削除（Post の reply に統合される旨の一文で置換）
  - [ ] `oauth_provider VARCHAR(20)` / `oauth_id` のスキーマ記述を削除
  - [ ] サンプルレスポンスの `"oauth_provider": "google"` / `"comments_count"` を削除 or 新スキーマへ
- 実装時点の git diff に上記を含めて同一 PR でコミットする

## テスト方針

### 単体テスト

- `pkg/authcore/client_test.go`:
  - `httptest.Server` で AuthCore をモック
  - Introspect: 正常 / 401 / 500 / タイムアウト
  - GetProfile: 正常 / 404 / 500
- `internal/usecase/user/usecase_test.go`:
  - `GetOrHydrateUserUseCase`: 初回 lazy create / 1h TTL 内はキャッシュ使用 / 1h 経過後に再 hydrate / AuthCore 失敗時の fail-open
  - `Upsert` で既存行の `IsAdmin` が保持されること
  - テーブル駆動テスト（Go 標準スタイル）
  - AuthCore クライアントは interface モック
- `internal/middleware/middleware_test.go`:
  - AuthMiddleware: Cookie なし / 無効 Cookie / 有効 Cookie / Introspection 失敗 / キャッシュヒット

### 統合テスト

- `cmd/server` レベルでは最小限の smoke test（`/me` が Cookie で通ること）のみ。AuthCore は docker-compose に追加するか、fake server を testfixture として同梱

### 既存テストの扱い

- `internal/handler/handler_test.go` は JWT 前提のテストが含まれる可能性が高いため、本タスクで全面書き換え

## 実装順序（推奨）

1. Migration（Step 1）- 001/002 の破壊的書き換えを先に済ませてスキーマを決める
2. Domain（Step 2）
3. Repository（Step 3, inmemory 差し替えまで）
4. AuthCore Client（Step 4）
5. Usecase（Step 5）
6. Middleware（Step 7）
7. Handler（Step 6）
8. Config & Entry（Step 8）
9. Delete 既存コード（Step 9）← コンパイルが通る状態を保つため最後にまとめて
10. テスト追加 / 書き換え
11. ドキュメント更新（Step 10）

各ステップで `go build ./...` が通ることを確認しながら進める。

## 技術的な補足

### ULID 採用の根拠

- Unix 時間 ms (48bit) + ランダム (80bit) の 128bit 固定長
- Crockford Base32 で 26 文字、URL セーフ、時刻順ソート可能
- AuthCore 側でも ULID を sub に採用する前提のため、SNS 内でも統一
- Go 実装: `github.com/oklog/ulid/v2`（十分枯れている）を採用

### AuthCore 仕様の前提

本タスクは AuthCore 側 API が以下のシェイプで既に用意されているか、並行で用意されることを前提とする:

- `POST {AUTHCORE_BASE_URL}/internal/introspect`
  - Header: `Authorization: Bearer {SERVICE_TOKEN}`
  - Body: `{"session_token": "<cookie value>"}`
  - 200: `{"sub": "01HX...", "expires_at": "2026-04-20T12:00:00Z"}`
  - 401: 無効セッション
- `GET {AUTHCORE_BASE_URL}/internal/users/{sub}`
  - Header: `Authorization: Bearer {SERVICE_TOKEN}`
  - 200: `{"sub": "...", "display_name": "...", "display_id": "...", "icon_url": "..."}`

上記シェイプが未確定なら、本タスクに着手する前に AuthCore 側の I/F を FIX させること。実装上は `pkg/authcore` に interface だけ先に切り、具象は I/F 確定後に埋める進め方でもよい。

### 疎結合性（他タスクへの影響）

- 本タスクは **User ドメイン単体で完結**
- 他タスク（Post / Follow / Badge / OGP）は本タスク完了後にスタートすることで以下が担保される:
  - 認証 Context（`GetSubFromContext` / `GetCurrentUserFromContext`）の前提が固まっている
  - ID 型（ULID 文字列）の前提が固まっている
  - Bio/Banner 以外のプロフィール属性は AuthCore に問い合わせるという方針が確立している
  - `users.is_admin` カラムが存在し、`User.IsAdmin` で admin 判定ができる
- 本タスク完了前に他タスクに着手する場合は、User 周辺 API だけ mock で進めてマージ時に整合させる方針が必要

### 後続タスクへの橋渡し

| 後続タスク | 本タスクが提供するもの |
|---|---|
| `02-implement-post-feature.md` | `UserID string(ULID)`、`GetCurrentUserFromContext`、ULID 化済みの `images.id` / `images.user_id` |
| `02-implement-badge.md` | `sub` PK の `users` テーブル、`users.is_admin` カラム、認証済み Context |
| `03-implement-follow-and-timeline.md` | Post 型が ULID 化されている前提 + sub ベースの User |
| `03-implement-ogp-fetcher.md` | Post 側は直接依存しないが、認証済み Context 前提 |

### スコープ外（明示）

- レート制限、監査ログ、metric（別タスク）
- SNS 固有属性の拡張（興味タグ、ピン止め投稿など）（別タスク）
- AuthCore 側の実装（本リポジトリ外）
- Redis への Introspection キャッシュ移設（将来タスク）
- **admin 付与ロジックそのもの**（`02-implement-badge.md` の Migration 005 で扱う）
- 階層的な RBAC（`is_admin` 単一 boolean で MVP は十分）
- **`users.followers_count` / `users.following_count` カラム**: 本タスクでは追加しない。**`03-implement-follow-and-timeline.md` の Migration 006 で ALTER TABLE により追加する**
