# Implement Badge

## 概要

ユーザーにバッジを付与する機能を実装する。バッジは「著名人認証済み」「開発者」などのプロフィール表示用識別子で、ユーザー取得 API のレスポンスに常に含まれる形で返す。MVP では **運営手動付与** とし、バッジ獲得条件の自動判定は将来拡張とする。

MVP で用意するバッジは **2 種類** に確定している:

- **青バッジ `verified_celebrity`（著名人認証）**
- **金バッジ `developer`（開発者）**

Organization 概念は MVP では導入しない（当該タスクは廃案・削除済み）。そのため本タスクに「Organization 識別」という副次目的は一切なく、純粋にプロフィール装飾 + 権限表示のためのバッジ機能である。

## 前提（依存タスク）

- **`01-rewrite-domain-to-authcore-alignment.md` 完了必須**
  - `users` テーブルが `sub CHAR(26)` PK になっていること
  - `users.is_admin BOOLEAN NOT NULL DEFAULT false` カラムが追加されていること（admin 判定の真実源）
  - 認証済み Context（`auth.GetSubFromContext`）が使えること
- 本タスクは `02-implement-post-feature.md` と **並行可能**（互いに依存しない）

## 確定仕様サマリ

| 項目 | 確定内容 |
|---|---|
| **真実源** | SNS 内完結（`badges` マスタ + `user_badges` 中間テーブル） |
| **付与主体** | 運営手動のみ（admin API） |
| **初期バッジ種別** | **`verified_celebrity`（青・著名人認証）** と **`developer`（金・開発者）** の 2 種類のみ |
| **バッジ priority** | `developer` = **5**（最上位）、`verified_celebrity` = **10** |
| **表示方法** | ユーザー取得 API のレスポンス（`GET /users/{sub}`, `GET /me`）に `badges: [...]` を**常に**含める。空配列でもキーは必ず出す |
| **N+1 回避** | リスト系 API ではバッチ取得（`ListByUserIDs`） |
| **管理 API** | `POST /admin/users/{sub}/badges` / `DELETE /admin/users/{sub}/badges/{badge_id}` / `GET\|POST\|PUT /admin/badges` |
| **admin 判定** | **`users.is_admin = true` の boolean カラムで判定**（env `ADMIN_SUBS` は採用しない） |
| **初回 admin** | **Migration 005 内で `UPDATE users SET is_admin = true` を同梱**（一本化。手動 UPDATE 方式は廃止） |
| **バッジアイコン画像** | マスタの `icon_url` カラム。MVP は外部 URL 許容 |
| **有効期限** | `user_badges.expires_at` nullable。参照時に `NOW() > expires_at` はフィルタ（イベントバッジ用途） |

## スコープ

### 含む

- `badges` マスタ + `user_badges` 中間テーブル（多対多）
- 運営用 admin API（付与・剥奪・バッジ定義の追加/編集）
- 公開 API: ユーザー取得系レスポンスへの `badges` 埋め込み
- N+1 回避のバッチ取得メソッド
- 表示順・有効期限サポート
- **初期マスタ seed**: `verified_celebrity`, `developer` の 2 種類
- **初回 admin の seed**: `UPDATE users SET is_admin = true WHERE sub = '<initial_admin_sub>'` を Migration 005 に同梱

### 含まない（スコープ外）

- **自動付与条件判定**（投稿数 X 以上で自動など）
- **バッジ交換 / コレクション**
- **AuthCore を真実源とする同期パス**（将来拡張）
- **admin role の正式 RBAC**（`is_admin` 単一 boolean で MVP は十分。階層的なロールは別タスク）
- **Organization 機能**（廃案）

## Domain モデル定義

```go
// internal/domain/badge.go

type Badge struct {
    ID          string    // ULID
    Key         string    // 機械可読キー（例: "verified_celebrity", "developer"）UNIQUE
    Label       string    // 表示名（例: "著名人認証済み"）
    Description string
    IconURL     string
    Color       string    // "blue" / "gold" など（UI ヒント、任意）
    Priority    int32     // 表示順（昇順）
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type UserBadge struct {
    UserID    string     // sub (ULID)
    BadgeID   string     // ULID
    GrantedAt time.Time
    GrantedBy string     // 付与した admin の sub
    ExpiresAt *time.Time // nullable
    Reason    string     // 付与理由（admin メモ、任意）
}

type GrantBadgeRequest struct {
    BadgeKey  string     `json:"badge_key"`          // "verified_celebrity" / "developer" 等
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
    Reason    string     `json:"reason,omitempty"`
}
```

### Validate

```go
func (b *Badge) Validate() error {
    if b.Key == "" || len(b.Key) > 64 { return NewValidationError("key must be 1..64 chars") }
    if b.Label == "" || utf8.RuneCountInString(b.Label) > 64 { return NewValidationError("label must be 1..64 chars") }
    // icon_url / color は任意
    return nil
}
```

## DB Schema（Migration DDL）

**パス**: `db/migrations/005_implement_badges.sql`（3 桁連番・既存 `db/migrations/` 直下）

```sql
-- db/migrations/005_implement_badges.sql

CREATE TABLE badges (
    id          CHAR(26) PRIMARY KEY,
    key         VARCHAR(64)  NOT NULL UNIQUE,   -- 機械可読キー
    label       VARCHAR(64)  NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    icon_url    VARCHAR(1024) NOT NULL DEFAULT '',
    color       VARCHAR(16)  NOT NULL DEFAULT '',
    priority    INT          NOT NULL DEFAULT 100,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE user_badges (
    user_id    CHAR(26)   NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
    badge_id   CHAR(26)   NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    granted_by CHAR(26)   NOT NULL,             -- 付与 admin の sub
    expires_at TIMESTAMPTZ NULL,
    reason     VARCHAR(255) NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, badge_id)
);

CREATE INDEX idx_user_badges_user_id ON user_badges(user_id);
CREATE INDEX idx_user_badges_badge_id ON user_badges(badge_id);
CREATE INDEX idx_user_badges_expires_at ON user_badges(expires_at) WHERE expires_at IS NOT NULL;

-- MVP 用の初期マスタ投入（seed）
-- developer（金）が最上位、verified_celebrity（青）がその次
-- ULID は Crockford Base32（0-9, A-Z のうち I/L/O/U を除く）26 文字。1 文字目は 0-7。
INSERT INTO badges (id, key, label, description, color, priority) VALUES
  ('01HXBADGE00000000000000DEV', 'developer',          '開発者',         'Fuju の開発者',              'gold', 5),
  ('01HXBADGE0000000000VER1FY0', 'verified_celebrity', '著名人認証済み', '運営が本人性を確認した著名人', 'blue', 10);
-- 上記 seed ULID は Crockford Base32 仕様（I/L/O/U 禁止）を満たす。
-- なお文字列としての「意味付け」は便宜上のもので、単なる識別子。
-- 実運用で「本物の時刻埋め込み ULID」が必要ならここを発行し直す。

-- 初回 admin の投入
-- TODO: deploy 前に本番 admin sub を差し替える
UPDATE users SET is_admin = true WHERE sub = '01HXADMIN00000000000000000';
```

### seed migration 運用メモ

- `users.is_admin` カラム自体の追加は **`01-rewrite-domain-to-authcore-alignment.md`** 側で実施済み前提
- 本タスクの 005 マイグレーションで「初回 admin を `is_admin = true` に設定する UPDATE」を **必ず同梱**。手動 UPDATE 運用は採用しない
- 運営は事前に自分の AuthCore sub を把握し、各環境（dev / stg / prod）で本ファイルの `01HXADMIN...` を実 sub に差し替えてから apply する
- migration 適用時点で当該 sub の `users` 行がまだ存在しない可能性がある（lazy create 前）。この場合 `UPDATE` は 0 行更新となり失敗はしないが、その admin が初回ログインして lazy create された直後に再度 `UPDATE` を手動で流す必要がある点に注意（運用手順として README に別途記載）

## Repository インターフェース

```go
// internal/repository/repository.go に追加

type BadgeRepository interface {
    // マスタ側
    ListAll(ctx context.Context) ([]*domain.Badge, error)
    GetByKey(ctx context.Context, key string) (*domain.Badge, error)
    GetByID(ctx context.Context, id string) (*domain.Badge, error)
    Create(ctx context.Context, badge *domain.Badge) (*domain.Badge, error)
    Update(ctx context.Context, badge *domain.Badge) (*domain.Badge, error)

    // ユーザー側（付与・剥奪・参照）
    Grant(ctx context.Context, userID, badgeID, grantedBy string, expiresAt *time.Time, reason string) error
    Revoke(ctx context.Context, userID, badgeID string) error
    ListByUserID(ctx context.Context, userID string) ([]*domain.Badge, error)

    // N+1 回避: 一覧 API でユーザー一覧に badges を埋め込むときに使う
    ListByUserIDs(ctx context.Context, userIDs []string) (map[string][]*domain.Badge, error)
}
```

`ListByUserID` / `ListByUserIDs` は **現在有効** なバッジのみ返す（`expires_at IS NULL OR expires_at > NOW()`）。

## Usecase の主要フロー

### GetUserBadgesUseCase

```
Execute(ctx, sub):
  return badgeRepo.ListByUserID(ctx, sub)  // 有効期限内のみ、priority 昇順
```

### GrantBadgeUseCase (admin)

```
Execute(ctx, adminSub, targetSub, req):
  if !isAdmin(ctx, adminSub): return Forbidden
  badge = badgeRepo.GetByKey(ctx, req.BadgeKey)
  if badge == nil: return NotFound
  user = userRepo.GetBySub(ctx, targetSub)
  if user == nil: return NotFound
  return badgeRepo.Grant(ctx, targetSub, badge.ID, adminSub, req.ExpiresAt, req.Reason)
```

### RevokeBadgeUseCase (admin)

```
Execute(ctx, adminSub, targetSub, badgeID):
  if !isAdmin(ctx, adminSub): return Forbidden
  return badgeRepo.Revoke(ctx, targetSub, badgeID)
```

### ユーザー取得 API での埋め込み

`GetUserUseCase` / `GetMeUseCase` / `ListUsersUseCase` のレスポンス組み立てで:

```
user = userRepo.GetBySub(ctx, sub)
badges = badgeRepo.ListByUserID(ctx, sub)
return UserDetail{User: user, Badges: badges}
```

一覧系 API では `badgeRepo.ListByUserIDs(ctx, subs)` でバッチ取得 → user にマージ。

### admin 判定

**`users.is_admin` カラムで判定する**。env による sub ホワイトリスト（旧案の `ADMIN_SUBS`）は採用しない。

```go
// internal/usecase/admin/checker.go（またはミドルウェア層）
type AdminChecker struct {
    userRepo repository.UserRepository
}
func (a *AdminChecker) IsAdmin(ctx context.Context, sub string) (bool, error) {
    user, err := a.userRepo.GetBySub(ctx, sub)
    if err != nil { return false, err }
    if user == nil { return false, nil }
    return user.IsAdmin, nil
}
```

- `User` ドメイン構造体には `IsAdmin bool` フィールドが追加済みである前提（`01` タスク側で対応）
- パフォーマンス懸念: admin API は低頻度なので DB 1 アクセス追加は許容。必要ならコンテキストキャッシュで重複取得を避ける
- admin 権限の付与/剥奪は当面 **DB 直接 UPDATE** で運用する（MVP）。将来 RBAC を整備したら専用 API を用意

## Handler（エンドポイント設計）

### 公開 API（レスポンスへの埋め込み）

- `GET /v1/users/{sub}` / `GET /v1/me`: レスポンス JSON に `badges: [...]` を追加（空でもキー必須）

```json
{
  "user": {
    "sub": "01HX...",
    "display_name_cached": "Alice",
    "bio": "...",
    "badges": [
      {"id": "01HXBADGE...", "key": "verified_celebrity", "label": "著名人認証済み",
       "icon_url": "https://...", "color": "blue", "priority": 10}
    ]
  }
}
```

### admin API

| メソッド | パス | 用途 |
|---|---|---|
| GET | `/v1/admin/badges` | バッジマスタ一覧 |
| POST | `/v1/admin/badges` | バッジマスタ作成 |
| PUT | `/v1/admin/badges/{id}` | バッジマスタ更新 |
| POST | `/v1/admin/users/{sub}/badges` | ユーザーにバッジ付与 |
| DELETE | `/v1/admin/users/{sub}/badges/{badge_id}` | バッジ剥奪 |

admin API は `AdminMiddleware` で `AdminChecker.IsAdmin` を通過させる（`users.is_admin = true` で判定）。

### Request / Response 例

#### POST /v1/admin/users/{sub}/badges

Request:
```json
{
  "badge_key": "verified_celebrity",
  "expires_at": null,
  "reason": "本人確認書類を確認済み（チケット #1234）"
}
```

Response (201):
```json
{"status": "granted", "user_id": "01HX...", "badge": {"id": "...", "key": "verified_celebrity", ...}}
```

## 実装ステップ順

1. **Migration**（`db/migrations/005_implement_badges.sql` + 初期マスタ seed。`verified_celebrity` と `developer` の 2 種類。加えて初回 admin への `UPDATE users SET is_admin = true` を同梱。TODO コメントで本番 sub 差し替えを明示）
2. **Domain 層**: `internal/domain/badge.go`
3. **Repository 層**: `BadgeRepository` interface + inmemory 実装
4. **Usecase 層**: `internal/usecase/badge/usecase.go`（Grant / Revoke / GetUserBadges / CRUD master）
5. **AdminChecker**（`internal/usecase/admin/checker.go` or `internal/middleware/admin.go`）。`users.is_admin` を見るだけ
6. **Handler 層**:
   - `internal/handler/badge.go`（admin API）
   - `internal/handler/user.go` のレスポンスに `badges` 追加
7. **ルーティング** (`cmd/server/main.go`): `/admin/*` を `AuthMiddleware` + `AdminMiddleware` の後段に登録
8. **テスト**

## テスト方針

### 単体テスト

- `internal/domain/badge_test.go`: Validate（Key 長さ、Label 必須）
- `internal/usecase/badge/usecase_test.go`:
  - Grant: 非 admin（`is_admin=false`）→ Forbidden
  - Grant: 存在しない BadgeKey → NotFound
  - Grant: 存在しないユーザー → NotFound
  - Revoke: 非 admin → Forbidden
  - Revoke: 既に無いバッジを revoke しても 2xx（冪等性の是非は議論余地あり、MVP は冪等）
  - GetUserBadges: `expires_at` が過去のバッジは返さない
- `internal/repository/inmemory/inmemory_test.go`:
  - ListByUserIDs のバッチ取得が N+1 相当にならないこと
- `internal/handler/badge_test.go`:
  - `is_admin=false` のユーザーは 403
  - `is_admin=true` のユーザーは Grant → GetUser の badges に反映される smoke test

### 統合テスト

- `cmd/server/` で「admin（`is_admin=true`）が badge を grant → 通常 user が `/me` を叩く → badges に乗る」フロー
- 各バッジ種別（`verified_celebrity`, `developer`）のレスポンス差分確認

## 運用メモ

- 初期マスタ（`verified_celebrity`, `developer`）は seed 投入。運用で追加する際は `POST /v1/admin/badges` または SQL 直接実行
- **admin 権限付与**: 初回は Migration 005 内で `UPDATE users SET is_admin = true WHERE sub = '...'`。以降は DB 直接 UPDATE または将来の RBAC 整備タスクで API 化
- バッジアイコンは当面外部 URL 許容。将来 R2 ミラーリングする場合は Image ドメインとの統合を検討

## スコープ外（明示）

- 自動付与条件判定（投稿数 X 以上で自動付与など）
- バッジ交換 / コレクション機能
- AuthCore を真実源とする同期パス（将来拡張）
- admin の階層ロール（`is_admin` 単一 boolean で MVP は十分）
- Organization 機能（廃案）
