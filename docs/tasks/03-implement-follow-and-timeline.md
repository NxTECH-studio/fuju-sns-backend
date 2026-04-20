# Implement Follow and Timeline

## 概要

ユーザー間のフォロー関係を管理し、フォロー中ユーザーの投稿を時系列で配信するタイムライン機能を実装する。ホームタイムライン（自分のフォロー）、ユーザータイムライン（特定ユーザー）、グローバルタイムライン（探索）の 3 種類を提供する。

## 前提（依存タスク）

- **`01-rewrite-domain-to-authcore-alignment.md` 完了必須**
  - User が `sub CHAR(26)` ベース
- **`02-implement-post-feature.md` 完了必須**
  - Post ID が ULID 化されていること
  - `PostRepository.ListByUserIDs(ctx, userIDs, cursor, limit)` が提供されていること
  - 投稿のレスポンス DTO（images, tags, liked_by_viewer 含む）が確立していること
- 本タスクは `03-implement-ogp-fetcher.md` と **並行可能**（互いに依存しない）
- ブロック / ミュート / 鍵アカウントは **別タスク** に切り出し（将来拡張）

## スコープ

### 含む

- Follow / Unfollow API（冪等、自己フォロー禁止）
- フォロワー一覧 / フォロー中一覧 API
- **ホームタイムライン**: 自分がフォローしているユーザーの投稿を新しい順
- **ユーザータイムライン**: 特定ユーザーの投稿（`02` の `GET /posts?user_id=xxx` をラップして整形）
- **グローバルタイムライン**: 全ユーザーの最新投稿（探索フィード）
- **ページネーション**: カーソルベース（ULID 順、フォロー一覧は複合カーソル）
- **fan-out on read（Pull 型）** で実装。Push 型への移行余地は残す
- **`users.followers_count` / `users.following_count` カラムの追加**（01 のスコープ外だったものを本タスクで ALTER TABLE）

### 含まない（スコープ外）

- 通知（フォロー通知、タイムライン新着通知）
- **ブロック / ミュート**（別タスク）
- **鍵アカウント / 承認制フォロー**（別タスク）
- **fan-out on write（Push 型）** / Redis キャッシュによるタイムライン事前構築
- おすすめユーザー / おすすめ投稿
- リスト機能（自作タイムライン）

## 設計判断

### fan-out on read vs on write

**結論: MVP は fan-out on read（Pull 型）**

| 観点 | on read (Pull) | on write (Push) |
|---|---|---|
| 実装コスト | 低 | 高（各フォロワーのタイムライン領域に書き込む worker が必要） |
| 読み込み時コスト | フォロー数が多いと重い | 軽い（自分のタイムラインを読むだけ） |
| 書き込み時コスト | 軽い | フォロワー数 × 書き込み |
| 運用複雑度 | 低 | 高（Redis / queue / 整合性管理） |

MVP 段階でフォロー数が小規模な想定のため **Pull 型で十分**。将来、ヘビーユーザーのフォロー数が数千を超えたらハイブリッドへ。`TimelineUseCase` を interface 化しておいて、実装差し替えができるようにする。

### タイムライン取得クエリ

```sql
-- ホームタイムライン（my_sub が自分）
SELECT p.*
FROM posts p
WHERE p.deleted_at IS NULL
  AND p.user_id IN (
    SELECT followee_sub FROM follows WHERE follower_sub = $1   -- my_sub
    UNION ALL
    SELECT $1                                                   -- 自分自身の投稿も含む
  )
  AND ($2::CHAR(26) IS NULL OR p.id < $2)                       -- cursor
ORDER BY p.id DESC
LIMIT $3;
```

- ULID の辞書順 = 時刻順なので `ORDER BY id DESC` で OK
- フォロー集合のサブクエリは 1 クエリで済む（JOIN でも OK）
- リプライを TL に含めるか: **MVP では含める**（`parent_post_id IS NOT NULL` も表示）。将来設定可能にする

### ページネーション

- **投稿系カーソル（TL）**: 最後に受け取った post の ID（ULID）。`next_cursor = lastPost.ID`。末尾なら null
- **フォロー一覧カーソル**: follows 行は `created_at` と (相手側の) sub の複合で順序付け。`(created_at DESC, sub DESC)` で並べ、カーソルには両方を含める必要がある
- limit は 1..50、デフォルト 20

### フォロー一覧の複合カーソル仕様

- **エンコード形式**: `base64url( created_at_rfc3339_nano + "|" + peer_sub )`
  - 例: `base64url("2026-04-20T10:00:00.123456789Z|01HXPEER...")`
  - 区切り文字 `|` は RFC3339 にも ULID にも現れないので衝突しない
- **デコード**: base64url decode → `|` で split → `[0]` を `time.Parse(time.RFC3339Nano, ...)`、`[1]` を ULID として扱う
- **クエリ適用**:
  ```sql
  WHERE (created_at, peer_sub) < ($cursor_time, $cursor_sub)
  ORDER BY created_at DESC, peer_sub DESC
  LIMIT $limit
  ```
  - `peer_sub` = followers 一覧なら `follower_sub`、following 一覧なら `followee_sub`
- Repository インターフェースの `ListFollowers` / `ListFollowing` の doc コメントに上記仕様を明記する

## Domain モデル定義

```go
// internal/domain/follow.go

type Follow struct {
    FollowerSub string    // フォローする側 (ULID)
    FolloweeSub string    // フォローされる側 (ULID)
    CreatedAt   time.Time
}

func (f *Follow) Validate() error {
    if f.FollowerSub == "" || f.FolloweeSub == "" {
        return NewValidationError("follower_sub and followee_sub are required")
    }
    if f.FollowerSub == f.FolloweeSub {
        return NewValidationError("cannot follow yourself")
    }
    return nil
}
```

タイムラインは専用ドメイン entity を持たず、`[]*Post` + `next_cursor` の DTO を返す。

## DB Schema（Migration DDL）

**パス**: `db/migrations/006_implement_follow_and_timeline.sql`（3 桁連番・既存 `db/migrations/` 直下）

```sql
-- db/migrations/006_implement_follow_and_timeline.sql

CREATE TABLE follows (
    follower_sub CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
    followee_sub CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_sub, followee_sub),
    CHECK (follower_sub <> followee_sub)
);

-- フォロワー一覧（A の follower 一覧 = followee_sub = A の行を引く）
-- 複合カーソル (created_at DESC, follower_sub DESC) での走査に備えて複合 index
CREATE INDEX idx_follows_followee_sub_created_at ON follows(followee_sub, created_at DESC, follower_sub DESC);
-- フォロー中一覧（A のフォロー中 = follower_sub = A の行を引く）
CREATE INDEX idx_follows_follower_sub_created_at ON follows(follower_sub, created_at DESC, followee_sub DESC);

-- 非正規化カウンタ（users テーブルに追加）
-- 01 のスコープ外としていた followers_count / following_count をここで導入する
ALTER TABLE users
    ADD COLUMN followers_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN following_count BIGINT NOT NULL DEFAULT 0;
```

## Repository インターフェース

```go
// internal/repository/repository.go に追加

type FollowRepository interface {
    // 冪等: 既存の follow があっても error にしない
    Create(ctx context.Context, followerSub, followeeSub string) (inserted bool, err error)
    Delete(ctx context.Context, followerSub, followeeSub string) (deleted bool, err error)
    IsFollowing(ctx context.Context, followerSub, followeeSub string) (bool, error)

    // followerSub が follow している全 followee の sub（タイムライン集計用）
    ListFollowingSubs(ctx context.Context, followerSub string) ([]string, error)

    // UI 用の一覧（複合カーソル）。
    // cursor は base64url(created_at_rfc3339_nano + "|" + peer_sub) 形式。nil なら先頭から。
    // 並び順: (created_at DESC, peer_sub DESC)。next_cursor は最後の行の (created_at, peer_sub) を同形式で返す。
    // 末尾なら next_cursor = "".
    ListFollowers(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.User, string /*nextCursor*/, error)
    ListFollowing(ctx context.Context, sub string, cursor *string, limit int) ([]*domain.User, string, error)

    // viewer が対象の sub 群を follow しているかの一括判定（投稿一覧等で使う）
    AreFollowing(ctx context.Context, viewerSub string, targetSubs []string) (map[string]bool, error)
}
```

`PostRepository` は `02` で追加済みの以下を使う:

```go
ListByUserIDs(ctx context.Context, userIDs []string, cursor *string, limit int) ([]*domain.Post, string, error)
List(ctx context.Context, userID *string, cursor *string, limit int) ([]*domain.Post, string, error)  // userID=nil でグローバル
```

## Usecase の主要フロー

### FollowUseCase

```
Execute(ctx, followerSub, followeeSub):
  if followerSub == followeeSub: return BadRequest("cannot follow yourself")
  target = userRepo.GetBySub(ctx, followeeSub)
  if target == nil: return NotFound
  inserted, err = followRepo.Create(ctx, followerSub, followeeSub)
  if inserted:
    userRepo.IncrementFollowingCount(ctx, followerSub)
    userRepo.IncrementFollowersCount(ctx, followeeSub)
  return { following: true }   // 冪等（既に follow でも 200）
```

### UnfollowUseCase

```
Execute(ctx, followerSub, followeeSub):
  deleted, err = followRepo.Delete(ctx, followerSub, followeeSub)
  if deleted:
    userRepo.DecrementFollowingCount(ctx, followerSub)
    userRepo.DecrementFollowersCount(ctx, followeeSub)
  return { following: false }
```

### HomeTimelineUseCase

```
Execute(ctx, mySub, cursor *string, limit int):
  subs = followRepo.ListFollowingSubs(ctx, mySub)
  subs = append(subs, mySub)                             // 自分の投稿も含める
  posts, nextCursor = postRepo.ListByUserIDs(ctx, subs, cursor, limit)
  return hydratePostDetails(ctx, posts, viewerSub=mySub), nextCursor
```

`hydratePostDetails` は `02` の post 一覧と同じロジック（images / tags / liked_by_viewer のバッチ取得 + User の鏡像キャッシュ埋め込み）。

### UserTimelineUseCase

```
Execute(ctx, targetSub, cursor *string, limit int, viewerSub *string):
  posts, nextCursor = postRepo.List(ctx, &targetSub, cursor, limit)
  return hydratePostDetails(ctx, posts, viewerSub), nextCursor
```

### GlobalTimelineUseCase

```
Execute(ctx, cursor *string, limit int, viewerSub *string):
  posts, nextCursor = postRepo.List(ctx, nil, cursor, limit)
  return hydratePostDetails(ctx, posts, viewerSub), nextCursor
```

### 投稿者情報の hydrate との整合

- タイムライン返却時、各 Post の投稿者プロフィール（DisplayName, IconURL）が必要
- `01` の lazy hydrate は「current user」だけを対象にしており、他ユーザーは `users.*_cached` をそのまま使う（1h TTL 外でもフォールバック）
- **方針**: TL ごとのユーザー情報はキャッシュ値をそのまま返す。hydrate の再トリガは発生させない（タイムラインは大量ユーザーを並べるので、AuthCore コールで重くしない）
- AuthCore 情報が古い可能性はあるが、1h の TTL なので許容範囲

### N+1 回避

TL の 1 ページ分（例: 20 投稿）を返すときの取得フロー:

```
posts                = postRepo.ListByUserIDs(...)              // 1 クエリ
postIDs              = map(posts, .ID)
authorSubs           = unique(map(posts, .UserID))
authors              = userRepo.ListBySubs(ctx, authorSubs)     // 1 クエリ（新規）
imagesByPost         = imageRepo.ListByPostIDs(ctx, postIDs)    // 1 クエリ
tagsByPost           = tagRepo.ListByPostIDs(ctx, postIDs)      // 1 クエリ
likedByViewer        = likeRepo.ListLikedPostIDsByUser(viewer, postIDs)  // 1 クエリ
followingByViewer    = followRepo.AreFollowing(viewer, authorSubs)       // 1 クエリ
```

→ 1 ページ分で高々 6 クエリ。投稿 N に対して O(1)。

`UserRepository.ListBySubs(ctx, subs []string) (map[string]*User, error)` は本タスクで新規追加する（`01` の interface に生えていなければ）。

## Handler（エンドポイント設計）

ベース URL: `/v1`

| メソッド | パス | 用途 | 認証 |
|---|---|---|---|
| POST | `/users/{sub}/follow` | フォロー（冪等） | 必須 |
| DELETE | `/users/{sub}/follow` | アンフォロー（冪等） | 必須 |
| GET | `/users/{sub}/followers` | フォロワー一覧（複合カーソル） | 任意 |
| GET | `/users/{sub}/following` | フォロー中一覧（複合カーソル） | 任意 |
| GET | `/timeline/home` | ホームタイムライン | 必須 |
| GET | `/timeline/user/{sub}` | ユーザータイムライン | 任意 |
| GET | `/timeline/global` | グローバルタイムライン | 任意 |

### Request / Response 例

#### POST /v1/users/{sub}/follow

Response (200):
```json
{"following": true, "followers_count": 42}
```

#### GET /v1/timeline/home?cursor=01HX...&limit=20

Response (200):
```json
{
  "data": [
    {
      "id": "01HXPOST...",
      "user_id": "01HXUSER...",
      "content": "...",
      "author": {"sub": "01HXUSER...", "display_name_cached": "Alice", "icon_url_cached": "https://..."},
      "images": [...],
      "tags": [...],
      "likes_count": 3,
      "replies_count": 1,
      "liked_by_viewer": false,
      "following_author": true,
      "created_at": "2026-04-20T10:00:00Z"
    }
  ],
  "next_cursor": "01HXPOST..."
}
```

## 実装ステップ順

1. **Migration**（`db/migrations/006_implement_follow_and_timeline.sql`）
2. **Domain 層**: `internal/domain/follow.go`
3. **Repository 層**:
   - `FollowRepository` の interface + inmemory 実装（複合カーソル encode/decode ヘルパ込み）
   - `UserRepository.ListBySubs` を追加（なければ）
   - `UserRepository.IncrementFollowingCount` / `Increment/DecrementFollowersCount` を追加
4. **Usecase 層**:
   - `internal/usecase/follow/usecase.go`（Follow / Unfollow / ListFollowers / ListFollowing）
   - `internal/usecase/timeline/usecase.go`（HomeTimeline / UserTimeline / GlobalTimeline）
   - `hydratePostDetails` ヘルパ（`02` の post handler と共有できるよう `internal/usecase/post/hydrate.go` に切り出し）
5. **Handler 層**:
   - `internal/handler/follow.go`
   - `internal/handler/timeline.go`
6. **ルーティング**（`cmd/server/main.go`）
7. **テスト**

## テスト方針

### 単体テスト

- `internal/domain/follow_test.go`:
  - Validate: 同一 sub → error / 空文字 → error
- `internal/usecase/follow/usecase_test.go`:
  - Follow: 自己フォロー → BadRequest
  - Follow: 存在しない user → NotFound
  - Follow 冪等性: 同じペアを 2 回実行しても DB 1 行、カウンタも 1 加算のみ
  - Unfollow 冪等性: 無い relation を unfollow しても 2xx
- `internal/usecase/timeline/usecase_test.go`:
  - Home: 誰もフォローしていない → 自分の投稿のみ
  - Home: フォロー + 自分のミックスで時刻順にソート
  - カーソル境界（先頭 / 途中 / 末尾で next_cursor = null）
  - User Timeline: 削除済み投稿は除外
- `internal/repository/inmemory/inmemory_test.go`:
  - `ListByUserIDs` が空集合でも safely 空を返す
  - 複合カーソル encode/decode ラウンドトリップ

### 統合テスト

- `cmd/server/` smoke test:
  - A が B を follow → B が post → A の Home TL に B の投稿が出る
  - A が unfollow → 以降の post は A の Home TL に出ない

### 性能テスト（任意）

- フォロー 500 人 × 各 100 投稿 = 5 万投稿のシード
- Home TL p95 < 100ms（目標）
- 達成できなければ `follows` に `(follower_sub, followee_sub, created_at)` の covering index を検討

## スコープ外（明示）

- 通知（フォロー通知、タイムライン新着通知）
- ブロック / ミュート
- 鍵アカウント / 承認制フォロー
- fan-out on write（Push 型）/ Redis キャッシュによる事前構築
- おすすめユーザー / おすすめ投稿
- リスト機能（自作タイムライン）
- リプライをホーム TL から外す設定
- タイムライン内の OGP プレビュー埋め込み（`03-implement-ogp-fetcher.md` と連携するが、本タスクでは「post の持つ OGP フィールドをそのまま返す」で完結）
