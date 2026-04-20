# Implement Post Feature

## 概要

SNS のコアとなる投稿機能を、AuthCore 連携済みの User ドメインを前提に実装する。投稿の作成・取得・削除・一覧（自分 / 他ユーザー）に加えて、スレッド構造（リプライ）、いいね、画像添付、自動タグ付け（差し替え可能な interface 設計）を含む。

## 前提（依存タスク）

- **`01-rewrite-domain-to-authcore-alignment.md` 完了必須**
  - `User.Sub` が `CHAR(26)` の ULID になっていること
  - `auth.GetSubFromContext(ctx)` / `auth.GetCurrentUserFromContext(ctx)` が使えること
  - `Post.UserID` が `string(ULID)` に機械的追随済みであること（01 の Step 2）
  - `images.id` / `images.user_id` が `CHAR(26)` 化済みであること（01 の Step 1 の 002 書き換え）
- 本タスクは `02-implement-badge.md` と **並行可能**（互いに依存しない）
- 後続の `03-implement-follow-and-timeline.md` / `03-implement-ogp-fetcher.md` が本タスクに依存する

## スコープ

### 含む

- 投稿 ID の **ULID 化**（`01` で型変更済み、本タスクで生成ロジックを実装）
- 投稿の CRUD のうち **C / R / D**（Update は含まない、後述）
- **スレッド構造**（`parent_post_id` nullable によるリプライ）
- **画像添付**（既存 Image ドメインを参照。中間テーブルで多対多）
- **いいね**（Like）
- **自動タグ付け**（正規表現・キーワードベース → `TagExtractor` interface で将来 Janome/LLM 差し替え可能に）
- **カーソルベースのページネーション**（ULID 辞書順 = 時刻順）
- **可視性は `public` のみ**（MVP）
- **旧 `comments` テーブル / `Comment` ドメインの削除**（Post の reply に統合）

### 含まない（スコープ外）

- **編集機能**: 編集不可。修正したいユーザーは `delete → re-post` で対応
- **リポスト / 引用リポスト**: 将来タスク
- **ブックマーク / シェア**
- **フォロワー限定公開 / 鍵アカウント**
- **通知連携**（別タスク）
- **タイムライン生成**（`03-implement-follow-and-timeline.md` で扱う）
- **OGP プレビュー**（`03-implement-ogp-fetcher.md` で扱う）
- **動画添付**（将来拡張）

## 確定仕様サマリ

| 項目 | 確定内容 |
|---|---|
| **本文の最大文字数** | **120 文字（`utf8.RuneCountInString`）** |
| いいねの取り消し API | `POST /posts/{id}/like` / `DELETE /posts/{id}/like`（冪等） |
| 画像の最大枚数 | **4 枚**（X 互換） |

## Domain モデル定義

### Post

```go
type Post struct {
    ID           string     // ULID (CHAR(26))
    UserID       string     // ULID (AuthCore sub)
    Content      string     // 本文（最大 120 文字）
    ParentPostID *string    // NULL 可。設定されていればリプライ
    RootPostID   *string    // 会話の最上位 Post ID（スレッド取得高速化用、オプション）
    LikesCount   int64      // 非正規化カウンタ（likes テーブルからトリガ or app 層で整合）
    RepliesCount int64      // 非正規化カウンタ
    Visibility   string     // "public" のみ（MVP）。カラムは後方互換のために用意
    CreatedAt    time.Time
    UpdatedAt    time.Time
    DeletedAt    *time.Time
}

type CreatePostRequest struct {
    Content      string   `json:"content"`
    ImageIDs     []string `json:"image_ids,omitempty"`      // 既にアップロード済み Image の ID
    ParentPostID *string  `json:"parent_post_id,omitempty"` // リプライの場合のみ
}

const MaxContentLen = 120  // domain 定数として公開し、handler / validation / DDL コメントと揃える

func (p *Post) Validate() error {
    if p.Content == "" || utf8.RuneCountInString(p.Content) > MaxContentLen {
        return NewValidationError(fmt.Sprintf("content must be 1..%d chars", MaxContentLen))
    }
    if p.UserID == "" {
        return NewValidationError("user_id is required")
    }
    if p.ParentPostID != nil && *p.ParentPostID == "" {
        return NewValidationError("parent_post_id must be non-empty if present")
    }
    return nil
}
```

### Like

```go
type Like struct {
    UserID    string    // ULID
    PostID    string    // ULID
    CreatedAt time.Time
}
```

### Tag（自動タグ付け）

```go
type Tag struct {
    ID   string  // ULID
    Name string  // 正規化済み（小文字、前後空白除去）
}

type PostTag struct {
    PostID string
    TagID  string
}

// 差し替え可能な抽象
type TagExtractor interface {
    // 本文からタグ候補を抽出する。
    // MVP は RegexTagExtractor（#ハッシュタグ + キーワード辞書）を実装。
    // 将来は JanomeTagExtractor / LLMTagExtractor に差し替え可能。
    Extract(ctx context.Context, content string) ([]string, error)
}
```

### Post と Image の関連

- 多対多。**中間テーブル `post_images`** を採用（`image_ids` 配列方式は順序保持とクエリ性能のトレードオフがあるため採用しない）
- 既存 `Image.UserID` / `Image.ID` は `01` で `string(ULID, CHAR(26))` に書き換え済み
- アップロード済み Image ID のリストを投稿作成 API で受け取り、サーバ側で「その Image の UserID == 認証ユーザーの sub」であることを検証

## DB Schema（Migration DDL）

**パス**: `db/migrations/004_implement_posts.sql`（3 桁連番・既存 `db/migrations/` 直下）

```sql
-- db/migrations/004_implement_posts.sql
-- 前提: 01 で 001/002 が書き換え済み（posts.id / posts.user_id / images.id / images.user_id が CHAR(26)）

-- 01 で用意済みの posts テーブルをリプライ・いいね等に拡張
ALTER TABLE posts
    DROP COLUMN IF EXISTS image_urls,                    -- 旧スキーマ残骸の除去
    ADD COLUMN parent_post_id CHAR(26) NULL REFERENCES posts(id) ON DELETE SET NULL,
    ADD COLUMN root_post_id   CHAR(26) NULL REFERENCES posts(id) ON DELETE SET NULL,
    ADD COLUMN replies_count  BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN visibility     VARCHAR(16) NOT NULL DEFAULT 'public';

-- posts.content の文字数上限チェック（rune ベースは DB では素直に書けないので byte 上限で緩めに取る + app 層で rune ベースの厳密チェック）
-- ULID は 4byte/char のサロゲートにならないので content に影響なし。120 rune は UTF-8 max で 480 byte。
ALTER TABLE posts ADD CONSTRAINT posts_content_len CHECK (char_length(content) <= 120 AND char_length(content) >= 1);

-- posts.id は 01 で CHAR(26) に変更済み前提
CREATE INDEX IF NOT EXISTS idx_posts_user_id_id_desc ON posts(user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_posts_parent_post_id ON posts(parent_post_id);
CREATE INDEX IF NOT EXISTS idx_posts_root_post_id   ON posts(root_post_id);
CREATE INDEX IF NOT EXISTS idx_posts_id_desc        ON posts(id DESC) WHERE deleted_at IS NULL;

-- 中間テーブル: 投稿と画像
CREATE TABLE post_images (
    post_id      CHAR(26) NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    image_id     CHAR(26) NOT NULL REFERENCES images(id) ON DELETE CASCADE,
    position     SMALLINT NOT NULL,        -- 表示順（0..3）
    PRIMARY KEY (post_id, image_id),
    UNIQUE (post_id, position),
    CHECK (position >= 0 AND position < 4)
);
CREATE INDEX idx_post_images_post_id ON post_images(post_id);

-- いいね（01 で旧 likes は削除済み。ここで ULID 版を新規作成）
CREATE TABLE likes (
    user_id    CHAR(26) NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
    post_id    CHAR(26) NOT NULL REFERENCES posts(id)  ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, post_id)
);
CREATE INDEX idx_likes_post_id ON likes(post_id);
CREATE INDEX idx_likes_user_id_created_at ON likes(user_id, created_at DESC);

-- タグ
CREATE TABLE tags (
    id         CHAR(26) PRIMARY KEY,
    name       VARCHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE post_tags (
    post_id CHAR(26) NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    tag_id  CHAR(26) NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX idx_post_tags_tag_id ON post_tags(tag_id);

-- comments は Post の reply に統合するため DROP（01 で CHAR(26) 化しただけで残置していたものを破棄）
DROP TABLE IF EXISTS comments;
```

### images.user_id の FK 整合

- `01` で `images.user_id` が `CHAR(26)` に、`FK references users(sub)` が張られている前提。不足があれば本タスク冒頭で追記してもよい。

## Repository インターフェース

```go
// internal/repository/repository.go に追加

type PostRepository interface {
    GetByID(ctx context.Context, id string) (*domain.Post, error)
    Create(ctx context.Context, post *domain.Post, imageIDs []string, tagIDs []string) (*domain.Post, error)
    Delete(ctx context.Context, id string) error // soft delete

    // カーソルベース: cursor は ULID。nil なら先頭から。limit 件返す。
    // userID 指定でユーザータイムライン、未指定でグローバル探索。
    List(ctx context.Context, userID *string, cursor *string, limit int) ([]*domain.Post, string /*nextCursor*/, error)

    // 03-follow-and-timeline で使う（本タスクで先に切っておく）
    ListByUserIDs(ctx context.Context, userIDs []string, cursor *string, limit int) ([]*domain.Post, string, error)

    // スレッド取得: parent_post_id = postID のもの
    ListReplies(ctx context.Context, postID string, cursor *string, limit int) ([]*domain.Post, string, error)

    IncrementRepliesCount(ctx context.Context, postID string) error
    DecrementRepliesCount(ctx context.Context, postID string) error
    IncrementLikesCount(ctx context.Context, postID string) error
    DecrementLikesCount(ctx context.Context, postID string) error
}

type LikeRepository interface {
    Create(ctx context.Context, userID, postID string) (inserted bool, err error) // 冪等: 既存なら false
    Delete(ctx context.Context, userID, postID string) (deleted bool, err error)  // 冪等: 無ければ false
    IsLikedBy(ctx context.Context, userID, postID string) (bool, error)
    ListLikedPostIDsByUser(ctx context.Context, userID string, postIDs []string) (map[string]bool, error) // 一覧取得の N+1 回避
}

type TagRepository interface {
    UpsertByNames(ctx context.Context, names []string) ([]*domain.Tag, error) // 一括。存在しなければ作る
    ListByPostID(ctx context.Context, postID string) ([]*domain.Tag, error)
    ListByPostIDs(ctx context.Context, postIDs []string) (map[string][]*domain.Tag, error) // N+1 回避
}
```

inmemory 実装は同じシグネチャで map ベースに追加。

## Usecase の主要フロー

### CreatePost

```
CreatePostUseCase.Execute(ctx, sub, req):
  1. req.Validate()（120 文字上限チェック）
  2. imageIDs が指定されていれば: 所有者検証 + 枚数上限 (<=4) 検証
     for each imgID in req.ImageIDs:
       img = imageRepo.GetByID(ctx, imgID)
       if img == nil || img.UserID != sub: return Forbidden
  3. parent_post_id が指定されていれば: 親の存在チェック + root_post_id 決定
     if req.ParentPostID != nil:
       parent = postRepo.GetByID(ctx, *req.ParentPostID)
       if parent == nil || parent.DeletedAt != nil: return NotFound
       rootID = parent.RootPostID if set else parent.ID
  4. tagNames = tagExtractor.Extract(ctx, req.Content)
  5. tags = tagRepo.UpsertByNames(ctx, tagNames)
  6. post = &Post{
       ID: ulid.Make().String(),
       UserID: sub,
       Content: req.Content,
       ParentPostID: req.ParentPostID,
       RootPostID: rootID,
       Visibility: "public",
     }
  7. postRepo.Create(ctx, post, req.ImageIDs, tagIDsOf(tags))  // 1 トランザクション内で挿入
  8. if parent != nil: postRepo.IncrementRepliesCount(ctx, parent.ID)
  9. post commit 後、best-effort で OGP enqueue hook を呼ぶ（03-ogp タスク参照。失敗はログのみ）
  10. return post (tags, images と共にレスポンス DTO へマップ)
```

### GetPost

```
GetPostUseCase.Execute(ctx, postID, viewerSub *string):
  post = postRepo.GetByID(ctx, postID)
  if post == nil || post.DeletedAt != nil: return NotFound
  images = imageRepo.ListByPostID(ctx, postID)     // 新規メソッド
  tags   = tagRepo.ListByPostID(ctx, postID)
  liked  = false
  if viewerSub != nil: liked = likeRepo.IsLikedBy(ctx, *viewerSub, postID)
  return PostDetail{Post: post, Images: images, Tags: tags, LikedByViewer: liked}
```

### DeletePost

```
DeletePostUseCase.Execute(ctx, postID, sub):
  post = postRepo.GetByID(ctx, postID)
  if post == nil: return NotFound
  if post.UserID != sub: return Forbidden
  postRepo.Delete(ctx, postID)                     // soft delete
  if post.ParentPostID != nil:
    postRepo.DecrementRepliesCount(ctx, *post.ParentPostID)
```

### ListPosts（カーソル）

```
ListPostsUseCase.Execute(ctx, userID *string, cursor *string, limit int, viewerSub *string):
  if limit <= 0 || limit > 50: limit = 20
  posts, nextCursor = postRepo.List(ctx, userID, cursor, limit)
  postIDs = map(posts, .ID)
  imagesByPost = imageRepo.ListByPostIDs(ctx, postIDs)
  tagsByPost   = tagRepo.ListByPostIDs(ctx, postIDs)
  likedByMe    = {}
  if viewerSub != nil:
    likedByMe = likeRepo.ListLikedPostIDsByUser(ctx, *viewerSub, postIDs)
  return list of PostDetail, nextCursor
```

### Like / Unlike

```
LikePostUseCase.Execute(ctx, sub, postID):
  post = postRepo.GetByID(ctx, postID)
  if post == nil: return NotFound
  inserted, err = likeRepo.Create(ctx, sub, postID)
  if inserted: postRepo.IncrementLikesCount(ctx, postID)
  // 冪等: 既にある場合も 200 を返す

UnlikePostUseCase.Execute(ctx, sub, postID):
  deleted, err = likeRepo.Delete(ctx, sub, postID)
  if deleted: postRepo.DecrementLikesCount(ctx, postID)
  // 冪等
```

### 設計判断: Comment vs Reply

既存の `Comment` ドメインを使うか、Post の reply に統合するか:

- **結論: Post の reply に統合する**（Comment ドメインは本タスクで廃止）
- 理由:
  - X/Mastodon/Bluesky など現代の SNS はスレッド型（Post にリプライ Post が付く）が主流
  - `parent_post_id` だけで両者を表現でき、認可・配信・通知・検索のモデルが一本化される
  - Comment を独立ドメインで維持すると、画像添付・リプライへのリプライ・いいねなど機能を二重実装することになる
- マイグレーション:
  - 既存 `comments` テーブルと `internal/domain/comment.go` / `internal/handler/comment.go` / `internal/usecase/comment/usecase.go` を **本タスクで削除**
  - `internal/repository/repository.go` から `CommentRepository` インターフェースを削除
  - 開発データは本番未稼働のため移行不要

## Handler（エンドポイント設計）

ベース URL: `/v1`

| メソッド | パス | 用途 | 認証 |
|---|---|---|---|
| POST | `/posts` | 投稿作成（リプライも同じ） | 必須 |
| GET | `/posts/{id}` | 投稿取得 | 任意（viewer sub で liked 判定） |
| DELETE | `/posts/{id}` | 投稿削除 | 必須（所有者のみ） |
| GET | `/posts` | 一覧（`user_id` クエリでフィルタ、`cursor` でページング） | 任意 |
| GET | `/posts/{id}/replies` | スレッドのリプライ一覧 | 任意 |
| POST | `/posts/{id}/like` | いいね（冪等） | 必須 |
| DELETE | `/posts/{id}/like` | いいね解除（冪等） | 必須 |

### リクエスト / レスポンス例

#### POST /posts

Request:
```json
{
  "content": "こんにちは #fuju",
  "image_ids": ["01HXABC...", "01HXDEF..."],
  "parent_post_id": null
}
```

Response (201):
```json
{
  "post": {
    "id": "01HXPOST...",
    "user_id": "01HXUSER...",
    "content": "こんにちは #fuju",
    "parent_post_id": null,
    "root_post_id": null,
    "likes_count": 0,
    "replies_count": 0,
    "visibility": "public",
    "created_at": "2026-04-20T10:00:00Z",
    "images": [
      {"id": "01HXABC...", "public_url": "https://...", "position": 0}
    ],
    "tags": [{"id": "01HXTAG...", "name": "fuju"}],
    "liked_by_viewer": false
  }
}
```

#### GET /posts?user_id=...&cursor=...&limit=20

Response (200):
```json
{
  "data": [ /* PostDetail[] */ ],
  "next_cursor": "01HXPOST..."   // null なら末尾
}
```

## 自動タグ付け（TagExtractor）

### RegexTagExtractor（MVP）

- 正規表現で `#(\p{L}[\p{L}\p{N}_]{0,63})` 形式のハッシュタグを抽出
- キーワード辞書（`configs/tag_keywords.json` などで管理）に含まれる単語があれば追加
- 抽出後、`strings.ToLower(strings.TrimSpace(name))` で正規化
- 重複除去、最大 **10 個/投稿** に制限

### 将来の差し替え

- `JanomeTagExtractor`: 日本語形態素解析ベース（別プロセス or サイドカー）
- `LLMTagExtractor`: LLM 呼び出し（コスト・レイテンシを踏まえ非同期で書き戻し）
- 差し替え時は `TagExtractor` interface の実装を切り替えるだけで済む設計

## 実装ステップ順

内側（domain）→ 外側（handler）の順で進める。

1. **Migration**（`db/migrations/004_implement_posts.sql`）
2. **Domain 層**
   - `internal/domain/post.go`: ID/UserID/ParentPostID を string(ULID) に、`Visibility`・`ParentPostID`・`RootPostID`・`RepliesCount` を追加、`Validate()` を 120 文字上限で更新、`MaxContentLen` 定数を公開
   - `internal/domain/like.go`（新規）
   - `internal/domain/tag.go`（新規）+ `TagExtractor` interface
   - `internal/domain/comment.go` の **削除**
3. **Repository 層**
   - `PostRepository` / `LikeRepository` / `TagRepository` のインターフェース追加
   - `CommentRepository` の **削除**
   - inmemory 実装を書き換え
4. **Usecase 層**
   - `internal/usecase/post/usecase.go`: Create / Get / Delete / List / LikePost / UnlikePost / ListReplies
   - `TagExtractor` の MVP 実装（`internal/tagextractor/regex.go` など）
   - `internal/usecase/comment/` の **削除**
5. **Handler 層**
   - `internal/handler/post.go`: ULID 対応、カーソルページング、リプライ、いいね
   - `internal/handler/comment.go` の **削除**
6. **ルーティング**（`cmd/server/main.go`）
   - `/posts/{id}/replies` と `/posts/{id}/like` を追加
   - `/posts/{id}/comments` 系ルートの削除
7. **テスト**: 単体・結合（後述）

各ステップで `go build ./...` が通ること。

## テスト方針

### 単体テスト（Go 標準 + テーブル駆動）

- `internal/domain/post_test.go`:
  - Validate: 0 文字 / 121 文字 / parent_post_id の空文字 / 正常系（120 文字ちょうど）
- `internal/usecase/post/usecase_test.go`:
  - CreatePost: 画像の所有者検証（他人の画像を添付 → Forbidden）
  - CreatePost: 親投稿なし → RootPostID = 自身の ID（あるいは NULL で自身を root と扱う運用）
  - CreatePost: 親投稿がリプライ → RootPostID が親の root を継承
  - DeletePost: 他人の投稿 → Forbidden
  - List: カーソル境界（先頭・途中・末尾）
  - LikePost / UnlikePost: 冪等性（2 回実行しても DB 行は 1 / 0）
  - TagExtractor を interface モックで差し替え
- `internal/tagextractor/regex_test.go`:
  - `#tag` 抽出、キーワード辞書ヒット、重複除去、正規化
- `internal/handler/post_test.go`:
  - 認証なしで POST → 401
  - 他人の投稿 DELETE → 403
  - ULID 以外の path param → 400
  - ページネーションの next_cursor 形式

### 結合テスト

- `cmd/server/` レベルで `/posts` フロー（作成 → 取得 → いいね → リプライ → 一覧）の smoke test
- inmemory リポジトリを使う（本タスクのスコープでは PostgreSQL 具象実装は任意）

## 性能・運用メモ

- **カーソルページング**: `WHERE id < ? ORDER BY id DESC LIMIT N` で動く。ULID の辞書順 = 時系列なのでオフセットより安定（途中で新規投稿が入っても重複・漏れが起きにくい）
- **非正規化カウンタ**: `likes_count` / `replies_count` はアプリ層で整合。ズレ検知用の定期 reconcile スクリプトは別タスク
- **`ListByUserIDs`** は `03-implement-follow-and-timeline.md` 用の伏線。本タスクで interface だけ切っておく（in-memory 実装は提供、具象 DB 実装は Follow タスクで完成させてもよい）

## スコープ外（明示）

- 投稿の編集
- リポスト / 引用リポスト
- フォロワー限定公開
- 動画添付
- 全文検索（`tsvector` 列は本タスクでは追加しない）
- 通知連携
- メンション（`@displayid`）の名前解決・リンク化
- タイムライン生成（Follow タスクで）
- OGP プレビュー（OGP タスクで）
