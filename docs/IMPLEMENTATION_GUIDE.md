# FUJU Backend - 実装ガイド

**最終更新: 2026年4月16日**
**バージョン: 1.0**

---

## 目次

1. [システム概要](#システム概要)
2. [アーキテクチャ](#アーキテクチャ)
3. [環境構成](#環境構成)
4. [認証とセッション管理](#認証とセッション管理)
5. [API エンドポイント仕様](#api-エンドポイント仕様)
6. [ミドルウェア](#ミドルウェア)
7. [データモデル](#データモデル)
8. [エラーハンドリング](#エラーハンドリング)
9. [セキュリティ考慮事項](#セキュリティ考慮事項)
10. [開発ワークフロー](#開発ワークフロー)

---

## システム概要

### 概要

FUJUは、ソーシャルメディアプラットフォーム向けのバックエンドアプリケーションです。Goで実装され、以下の主要機能を提供します：

- **ユーザー管理**: AuthCore と同期した鏡像プロフィール + SNS 固有属性（Bio/Banner）
- **投稿機能**: 投稿の作成、読取、削除
- **画像管理**: Cloudflare R2 へのアップロード
- **認証**: AuthCore Introspection（外部認証基盤へ委譲）
- **CORS サポート**: フロントエンドとの通信を許可

### 主要な技術スタック

| コンポーネント | 技術 | バージョン |
|---|---|---|
| **言語** | Go | 1.24 |
| **データベース** | PostgreSQL | 16-Alpine |
| **キャッシュ** | Redis | 7-Alpine |
| **コンテナ化** | Docker Compose | 3.8 |
| **ロギング** | structured logging | - |
| **認証** | AuthCore Introspection | - |
| **ストレージ** | Cloudflare R2 | - |

---

## アーキテクチャ

### システム構成

```
┌─────────────────────────────────────────────────────────┐
│                    Frontend (React)                      │
│              http://localhost:5173                       │
└─────────────────────────────────────────────────────────┘
                          │
                 AuthCore session cookie
                          │
┌─────────────────────────────────────────────────────────┐
│                Backend API Server                        │
│              http://localhost:8080                       │
├─────────────────────────────────────────────────────────┤
│  HTTP Router (Go 1.24)                                   │
│  ├── /health                                             │
│  ├── /me                                                 │
│  ├── /users/*                                            │
│  ├── /posts/*                                            │
│  └── /v1/images/*                                        │
└─────────────────────────────────────────────────────────┘
          │           │           │              │
          ▼           ▼           ▼              ▼
    ┌───────────┬───────────┬─────────────┬─────────────┐
    │PostgreSQL │   Redis   │  AuthCore   │ Cloudflare  │
    │(Port 5432)│(Port 6379)│(introspect) │   R2 API    │
    └───────────┴───────────┴─────────────┴─────────────┘
```

### レイヤー構造

```
┌─────────────────────────────────────────┐
│         HTTP Handler Layer              │
│  ├── user.go                            │
│  ├── post.go                            │
│  └── image.go                           │
└─────────────────────────────────────────┘
            ↓
┌─────────────────────────────────────────┐
│   Middleware Layer                      │
│  ├── CORS Middleware                    │
│  ├── Auth Middleware (AuthCore)         │
│  ├── Hydrate User Middleware            │
│  ├── Logging Middleware                 │
│  └── Recovery Middleware                │
└─────────────────────────────────────────┘
            ↓
┌─────────────────────────────────────────┐
│   Use Case Layer (Business Logic)       │
│  ├── user/usecase.go                    │
│  ├── post/usecase.go                    │
│  └── image/usecase.go                   │
└─────────────────────────────────────────┘
            ↓
┌─────────────────────────────────────────┐
│   Repository Layer (Data Access)        │
│  └── inmemory/inmemory.go              │
│      (将来: PostgreSQL adapter)          │
└─────────────────────────────────────────┘
            ↓
┌─────────────────────────────────────────┐
│   Domain Layer (Entities)               │
│  ├── user.go                            │
│  ├── post.go                            │
│  └── image.go                           │
└─────────────────────────────────────────┘
            ↓
┌─────────────────────────────────────────┐
│   External Clients                      │
│  └── pkg/authcore (Introspect / Profile)│
└─────────────────────────────────────────┘
```

---

## 環境構成

### Docker Compose サービス

#### 1. Backend Service (`fuju-backend`)

```yaml
service: backend
image: Go 1.24 -> Alpine 3.20 runtime
port: 8080
healthcheck: GET /health (30秒間隔)
```

**環境変数パッシング**:
- docker-compose.ymlが`.env`ファイルから環境変数を読み込み
- すべての設定はバックエンドコンテナに渡される

#### 2. PostgreSQL Service (`fuju-postgres`)

```yaml
image: postgres:16-alpine
port: 5432
volumes:
  - postgres_data (永続化)
  - ./db/migrations (初期化スクリプト)
healthcheck: pg_isready (10秒間隔)
```

**初期化フロー**:
1. コンテナ起動時、`/docker-entrypoint-initdb.d`内のSQLファイルを実行
2. migrations/ディレクトリ内のファイルが自動実行
3. ユーザー、テーブルが自動作成

**主要テーブル**:
- `users` - AuthCore 鏡像 + SNS 固有プロフィール（Bio/Banner、`is_admin`）
- `posts` - 投稿
- `comments` - 暫定（post 機能タスクで post reply に統合され削除予定）
- `images` - 画像メタデータ

#### 3. Redis Service (`fuju-redis`)

```yaml
image: redis:7-alpine
port: 6379
command: redis-server --appendonly yes
volumes:
  - redis_data (永続化)
healthcheck: PING command (10秒間隔)
```

**用途**:
- アプリ内キャッシング
- 将来: リアルタイム通知、AuthCore Introspection キャッシュの外部化

#### 4. Adminer Service (`fuju-adminer`)

```yaml
image: adminer:latest
port: 8081
```

**用途**: PostgreSQL の Web ベースの データベース管理ツール

### ネットワーク

すべてのサービスが`fuju-network`内で通信:
- サービス名で DNS 解決
- 例: `postgres:5432`, `redis:6379`

### 起動コマンド

```bash
# 全サービス起動
docker compose up -d

# ログ確認
docker compose logs -f backend

# 特定サービスの再起動
docker compose restart backend

# 全サービス停止
docker compose down

# ボリュームも削除（リセット）
docker compose down -v
```

---

## 認証とセッション管理

認証の真実源は外部の AuthCore サービス。SNS バックエンドは毎リクエスト、
AuthCore の introspection エンドポイントに Cookie を渡して `sub`（ULID）
を解決する。ここでは credential 発行・保管・失効を一切行わない。

### AuthCore Introspection フロー

```
[Browser]                           [SNS Backend]                    [AuthCore]
   │ Cookie: authcore_session=...       │                                │
   │───────────────────────────────────▶│                                │
   │                                    │ 1. AuthMiddleware              │
   │                                    │    Cookie を取得               │
   │                                    │    30s キャッシュを参照        │
   │                                    │    ↓ (miss 時)                 │
   │                                    │ POST /internal/introspect      │
   │                                    │ Authorization: Bearer <svc>    │
   │                                    │───────────────────────────────▶│
   │                                    │                                │
   │                                    │   { sub, expires_at }          │
   │                                    │◀───────────────────────────────│
   │                                    │ 2. ctx に sub をセット         │
   │                                    │                                │
   │                                    │ 3. HydrateUserMiddleware       │
   │                                    │    users 行を取得              │
   │                                    │    - 行が無い: GetProfile      │
   │                                    │      して lazy-create          │
   │                                    │    - profile_refreshed_at が   │
   │                                    │      1h 超: GetProfile して    │
   │                                    │      *_cached を更新           │
   │                                    │    - それ以外: そのまま使う    │
   │                                    │                                │
   │                                    │ 4. ctx に *domain.User をセット│
   │                                    │                                │
   │                                    │ 5. handler 実行                │
   │     response                       │                                │
   │◀───────────────────────────────────│                                │
```

### 二層構造

| 層 | 目的 | 頻度 | TTL |
|---|---|---|---|
| Introspection | Cookie が有効か / sub は誰か | 毎リクエスト | **30 秒**（in-memory キャッシュ） |
| Profile Hydration | DisplayName / DisplayID / IconURL の取得 | users 行の lazy create 時 または 1h 経過後 | **1 時間**（`profile_refreshed_at` で管理） |

Introspection は **fail-closed**。AuthCore が不達なら 503、AuthCore が 401/403 を返したら 401。
Profile Hydration は **fail-open**。失敗時は既存の `*_cached` をそのまま返し、
`profile_refreshed_at` は更新しない（次回リクエストで再試行）。

### AuthCore クライアント

- `pkg/authcore.Client` インターフェース
  - `Introspect(ctx, sessionToken) (*Session, error)`
  - `GetProfile(ctx, sub) (*Profile, error)`
- `pkg/authcore.HTTPClient` が具象実装（`net/http`）
- `pkg/authcore.IntrospectCache` が上記を 30s TTL でラップ
- エラー区別: `ErrInvalidSession`（401 扱い） / `ErrUpstream`（503 扱い） / `ErrNotFound`

### Cookie 仕様

AuthCore 側で発行される不透明（opaque）セッショントークン。
SNS バックエンドは Cookie 名 (`AUTHCORE_SESSION_COOKIE_NAME`, default
`authcore_session`) を設定経由で指定して読むだけで、値の生成・回転・破棄には関与しない。

### ログアウト

AuthCore 側のエンドポイントで行う。SNS バックエンドは専用ルートを持たない。

---

## API エンドポイント仕様

### 1. ヘルスチェック

#### `GET /health`

ヘルスチェックエンドポイント。アプリケーションが稼働しているか確認。

**認証**: 不要

**レスポンス** (200 OK):
```json
{
  "status": "ok",
  "timestamp": "2026-04-16T10:30:45Z",
  "version": "dev"
}
```

---

### 2. 自分自身エンドポイント

#### `GET /me`

認証済みユーザー自身のレコードを返す。行が無ければ lazy-create され、
`*_cached` フィールドは AuthCore から 1h TTL でハイドレートされる。

**認証**: 必須（AuthCore セッション Cookie）

**レスポンス** (200 OK):
```json
{
  "sub": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
  "display_name": "John Doe",
  "display_id": "john_doe",
  "icon_url": "https://example.com/icon.jpg",
  "bio": "Software Engineer",
  "banner_url": "https://example.com/banner.jpg",
  "is_admin": false,
  "created_at": "2026-04-16T10:00:00Z",
  "updated_at": "2026-04-16T10:00:00Z"
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 401 Unauthorized | Cookie 欠落 / 無効セッション |
| 503 Service Unavailable | AuthCore 到達不能（introspection 失敗） |

---

### 3. ユーザーエンドポイント

#### `GET /users`

すべてのユーザー一覧取得

**認証**: 不要

**クエリパラメータ**:
```
page: ページ番号（デフォルト: 1）
limit: 1 ページあたりの件数（デフォルト: 10）
```

**レスポンス** (200 OK):
```json
{
  "data": [
    {
      "sub": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
      "display_name": "John Doe",
      "display_id": "john_doe",
      "icon_url": "https://example.com/icon.jpg",
      "bio": "Software Engineer",
      "banner_url": "https://example.com/banner.jpg",
      "created_at": "2026-04-16T10:00:00Z",
      "updated_at": "2026-04-16T10:00:00Z"
    }
  ],
  "total": 100,
  "page": 1,
  "limit": 10
}
```

---

#### `GET /users/{sub}`

特定ユーザー取得

**認証**: 不要

**パスパラメータ**:
```
sub: AuthCore sub (ULID, 26 文字)
```

**レスポンス** (200 OK):
```json
{
  "sub": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
  "display_name": "John Doe",
  "display_id": "john_doe",
  "icon_url": "https://example.com/icon.jpg",
  "bio": "Software Engineer",
  "banner_url": "https://example.com/banner.jpg",
  "created_at": "2026-04-16T10:00:00Z",
  "updated_at": "2026-04-16T10:00:00Z"
}
```

他ユーザーの `GET /users/{sub}` レスポンスには `is_admin` は含まれない。

**エラー**:
| ステータス | 説明 |
|---|---|
| 404 Not Found | ユーザーが見つからない |

---

#### `PUT /users/{sub}`

ユーザープロフィール更新

**認証**: 必須（AuthCore セッション Cookie）

本人のみが自身のプロフィール更新可能。SNS 固有属性（`bio` / `banner_url`）
のみ更新可能。identity / AuthCore 鏡像フィールドはここからは変更できない。

**リクエストボディ**:
```json
{
  "bio": "Senior Software Engineer",
  "banner_url": "https://example.com/banner.jpg"
}
```

**バリデーション**:
- `bio`: 500 文字以内
- `banner_url`: 1024 文字以内

**レスポンス** (200 OK):
```json
{
  "sub": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
  "display_name": "John Doe",
  "display_id": "john_doe",
  "icon_url": "https://example.com/icon.jpg",
  "bio": "Senior Software Engineer",
  "banner_url": "https://example.com/banner.jpg",
  "updated_at": "2026-04-16T10:30:45Z"
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 403 Forbidden | 他ユーザーの更新を試行 |
| 404 Not Found | ユーザーが見つからない |
| 401 Unauthorized | Cookie 欠落 / 無効セッション |

---

### 4. 投稿エンドポイント

#### `GET /posts`

投稿一覧取得

**認証**: 不要

**クエリパラメータ**:
```
page: ページ番号（デフォルト: 1）
limit: 1 ページあたりの件数（デフォルト: 20）
user_id: 特定ユーザーの sub に限定（オプション、ULID）
```

**レスポンス** (200 OK):
```json
{
  "data": [
    {
      "id": "01HX4Y7A1Z0K8P3N5M2Q4V9B7C",
      "user_id": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
      "content": "Hello, FUJU!",
      "image_urls": ["https://example.com/image1.jpg"],
      "likes_count": 10,
      "created_at": "2026-04-16T10:00:00Z",
      "updated_at": "2026-04-16T10:00:00Z"
    }
  ],
  "total": 250,
  "page": 1,
  "limit": 20
}
```

---

#### `POST /posts`

新規投稿作成

**認証**: 必須（AuthCore セッション Cookie）

**リクエストボディ**:
```json
{
  "content": "Hello, FUJU! This is my first post.",
  "image_urls": [
    "https://example.com/image1.jpg",
    "https://example.com/image2.jpg"
  ]
}
```

**バリデーション**:
- `content`: 1-5000 文字
- `image_urls`: 最大 10 枚

**レスポンス** (201 Created):
```json
{
  "id": "01HX4Y7A1Z0K8P3N5M2Q4V9B7C",
  "user_id": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
  "content": "Hello, FUJU! This is my first post.",
  "image_urls": [...],
  "likes_count": 0,
  "created_at": "2026-04-16T10:30:45Z",
  "updated_at": "2026-04-16T10:30:45Z"
}
```

---

#### `GET /posts/{id}`

特定投稿取得

**認証**: 不要

**パスパラメータ**:
```
id: 投稿 ID (ULID)
```

**レスポンス** (200 OK):
```json
{
  "id": "01HX4Y7A1Z0K8P3N5M2Q4V9B7C",
  "user_id": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
  "content": "Hello, FUJU!",
  ...
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 404 Not Found | 投稿が見つからない |

---

#### `DELETE /posts/{id}`

投稿削除

**認証**: 必須（AuthCore セッション Cookie）

投稿作成者のみが削除可能。

**パスパラメータ**:
```
id: 投稿 ID (ULID)
```

**レスポンス** (204 No Content)

**エラー**:
| ステータス | 説明 |
|---|---|
| 403 Forbidden | 他ユーザーの投稿を削除しようとした |
| 404 Not Found | 投稿が見つからない |
| 401 Unauthorized | Cookie 欠落 / 無効セッション |

---

### 5. コメントエンドポイント（暫定）

> コメントは Post 機能タスク（`02-implement-post-feature.md`）で
> Post の reply に統合される予定。新規実装は reply 側に寄せる想定で、
> ここは既存 API の暫定仕様として残す。

`POST /posts/{id}/comments` / `DELETE /posts/{post_id}/comments/{comment_id}`
の ID はすべて ULID 文字列。認証は AuthCore セッション Cookie 必須。

---

### 6. 画像エンドポイント

> **注**: Cloudflare R2 サービスが設定されている場合のみ利用可能

#### `POST /v1/images`

画像アップロード

**認証**: 必須（AuthCore セッション Cookie）

**リクエスト形式**: `multipart/form-data`

**フォームフィールド**:
```
file: 画像ファイル（必須）
  - 形式: JPEG, PNG, GIF, WebP
  - 最大サイズ: 5 MB（デフォルト、設定可能）
title: 画像タイトル（オプション）
description: 画像説明（オプション）
```

**レスポンス** (201 Created):
```json
{
  "id": "01HX4Y8B3K0L9M2P5N7Q4V8B6D",
  "user_id": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
  "filename": "abc123.jpg",
  "url": "https://images.example.com/abc123.jpg",
  "title": "My Photo",
  "description": "Sunset photo",
  "file_size": 2048576,
  "mime_type": "image/jpeg",
  "created_at": "2026-04-16T10:40:00Z",
  "updated_at": "2026-04-16T10:40:00Z"
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 400 Bad Request | ファイルが見つからない |
| 413 Payload Too Large | ファイルサイズ超過 |
| 415 Unsupported Media Type | 対応していないファイル形式 |
| 401 Unauthorized | Cookie 欠落 / 無効セッション |
| 503 Service Unavailable | R2 サービスに接続できない |

---

#### `GET /v1/images`

ユーザーの画像一覧取得

**認証**: 必須（AuthCore セッション Cookie）

**クエリパラメータ**:
```
page: ページ番号（デフォルト: 1）
limit: 1 ページあたりの件数（デフォルト: 20）
```

**レスポンス** (200 OK):
```json
{
  "data": [
    {
      "id": "01HX4Y8B3K0L9M2P5N7Q4V8B6D",
      "user_id": "01HX4Y6Q9M8T3F2B1C0D7R5A9K",
      "filename": "abc123.jpg",
      "url": "https://images.example.com/abc123.jpg",
      ...
    }
  ],
  "total": 45,
  "page": 1,
  "limit": 20
}
```

---

#### `DELETE /v1/images/{id}`

画像削除

**認証**: 必須（AuthCore セッション Cookie）

画像の所有者のみが削除可能。

**パスパラメータ**:
```
id: 画像 ID (ULID)
```

**レスポンス** (204 No Content)

R2 バケットからも削除される。

**エラー**:
| ステータス | 説明 |
|---|---|
| 403 Forbidden | 他ユーザーの画像を削除しようとした |
| 404 Not Found | 画像が見つからない |
| 401 Unauthorized | Cookie 欠落 / 無効セッション |

---

## ミドルウェア

### 1. CORS ミドルウェア

**ファイル**: `internal/middleware/middleware.go`

**機能**: クロスオリジンリクエストを制御

**動作**:
1. リクエストの `Origin` ヘッダーを検証
2. `CORS_ALLOWED_ORIGINS` に含まれるか確認（カンマ区切り）
3. 許可オリジンの場合、適切なヘッダーを追加

**レスポンスヘッダー**（許可時）:
```
Access-Control-Allow-Origin: http://localhost:5173
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
Access-Control-Allow-Credentials: true
Access-Control-Max-Age: 3600
```

**設定**:
```bash
# .env
CORS_ALLOWED_ORIGINS=http://localhost:5173,http://localhost:3000
```

**ワイルドカード対応**:
```bash
# すべてのオリジンを許可（開発環境のデフォルト）
CORS_ALLOWED_ORIGINS=*
```

### 2. 認証ミドルウェア（AuthMiddleware）

**ファイル**: `internal/middleware/middleware.go`

**機能**: AuthCore セッション Cookie を introspection で検証し、
`sub`（ULID）をリクエストコンテキストに保存する。

**処理フロー**:
1. Cookie `authcore_session`（`AUTHCORE_SESSION_COOKIE_NAME` で変更可）を取得
2. `authcore.Client.Introspect(ctx, cookie.Value)` を呼ぶ
   - `IntrospectCache` 経由で 30s 以内の結果はローカルキャッシュを使用
3. `ErrInvalidSession` → 401、`ErrUpstream` → 503
4. 成功時は `auth.SetSubInContext(ctx, session.Sub)` で sub を保存

**エラーレスポンス** (401 Unauthorized):
```json
{
  "code": "UNAUTHORIZED",
  "message": "authentication required",
  "timestamp": "2026-04-16T10:45:12Z"
}
```

### 3. ハイドレートユーザーミドルウェア（HydrateUserMiddleware）

**ファイル**: `internal/middleware/middleware.go`

**機能**: `AuthMiddleware` で確定した sub をもとに `users` 行を取得し、
`*domain.User` を context にセットする。lazy-create と 1h TTL 更新を担う。

**処理フロー**:
1. ctx から sub を取得（`auth.GetSubFromContext`）
2. `GetOrHydrateUserUseCase.Execute(ctx, sub)` を呼ぶ
   - 行が無い場合: `authcore.GetProfile` を呼び、`*_cached` を埋めて INSERT
   - `profile_refreshed_at` が 1h を超えている場合: `GetProfile` を呼び、
     `*_cached` を更新（fail-open）
   - それ以外: 既存行をそのまま返す
3. `auth.SetCurrentUserInContext(ctx, user)` で User を保存

**コンテキスト保存**:
```go
// ハンドラー内でのアクセス
sub, ok    := auth.GetSubFromContext(r.Context())
user, ok   := auth.GetCurrentUserFromContext(r.Context())
```

### 4. ロギングミドルウェア

**ファイル**: `internal/middleware/middleware.go`

**機能**: すべての HTTP リクエスト/レスポンスをログ

**ログ情報**:
```
method: GET
path: /posts
status: 200
duration_ms: 45
timestamp: 2026-04-16T10:45:12Z
```

**ログレベル**:
- ステータス 5xx: ERROR
- ステータス 4xx: WARN
- ステータス 2xx: INFO

### 5. リカバリーミドルウェア

**機能**: パニックハンドリング

**処理**: 未処理のパニックをキャッチしてログ、500 エラーを返す

---

## データモデル

### User ドメインモデル

**ファイル**: `internal/domain/user.go`

```go
type User struct {
  Sub                string         // AuthCore sub (ULID, 26 文字, 主キー)
  DisplayNameCached  string         // AuthCore 由来、1h TTL キャッシュ
  DisplayIDCached    string         // @handle 相当、1h TTL キャッシュ
  IconURLCached      string         // アイコン URL、1h TTL キャッシュ
  ProfileRefreshedAt time.Time      // *_Cached の最終取得時刻
  Bio                string         // SNS 固有（500 文字以内）
  BannerURL          string         // SNS 固有（1024 文字以内）
  IsAdmin            bool           // SNS 運営フラグ（自己書き換え不可）
  CreatedAt          time.Time
  UpdatedAt          time.Time
  DeletedAt          *time.Time
}

type UpdateUserProfileRequest struct {
  Bio       *string
  BannerURL *string
}
```

**バリデーション**:
- `Bio`: 500 文字以内
- `BannerURL`: 1024 文字以内
- AuthCore 由来フィールド（`Sub` / `DisplayName*` / `DisplayID*` / `IconURL*`）は
  AuthCore 側で検証されるためここでは再検証しない

**DB スキーマ**:
```sql
CREATE TABLE users (
  sub                   CHAR(26)      PRIMARY KEY,
  display_name_cached   VARCHAR(255)  NOT NULL DEFAULT '',
  display_id_cached     VARCHAR(64)   NOT NULL DEFAULT '',
  icon_url_cached       VARCHAR(1024) NOT NULL DEFAULT '',
  profile_refreshed_at  TIMESTAMPTZ   NOT NULL DEFAULT '1970-01-01',
  bio                   TEXT          NOT NULL DEFAULT '',
  banner_url            VARCHAR(1024) NOT NULL DEFAULT '',
  is_admin              BOOLEAN       NOT NULL DEFAULT false,
  created_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  updated_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  deleted_at            TIMESTAMPTZ   NULL
);
```

---

### Post ドメインモデル

**ファイル**: `internal/domain/post.go`

```go
type Post struct {
  ID            string         // ULID（主キー）
  UserID        string         // 投稿者 sub（ULID、外部キー）
  Content       string         // 投稿内容（1-5000 文字）
  ImageURLs     []string       // 画像 URL リスト（最大 10 枚）
  LikesCount    int64          // いいね数
  CommentsCount int64          // コメント数（暫定、reply 統合で削除予定）
  CreatedAt     time.Time
  UpdatedAt     time.Time
  DeletedAt     *time.Time
}
```

**バリデーション**:
- `Content`: 1-5000 文字
- `ImageURLs`: 最大 10 枚
- `UserID`: 必須

**DB スキーマ**:
```sql
CREATE TABLE posts (
  id          CHAR(26)    PRIMARY KEY,
  user_id     CHAR(26)    NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  content     TEXT        NOT NULL,
  image_urls  JSONB       NOT NULL DEFAULT '[]'::jsonb,
  likes_count INT         NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at  TIMESTAMPTZ NULL
);
```

---

### Comment ドメインモデル

> コメントは Post 機能タスク（`02-implement-post-feature.md`）で
> Post の reply に統合され、ドメインモデルごと削除される。
> 現行の `internal/domain/comment.go` は ID/UserID/PostID を ULID 化した
> 暫定シェイプとして残しているのみ。新規実装の依存先にしないこと。

---

### Image ドメインモデル

**ファイル**: `internal/domain/image.go`

```go
type Image struct {
  ID         string     // ULID（アプリ層で採番）
  StorageKey string     // R2 オブジェクトキー
  FileName   string     // 元のファイル名
  MimeType   string     // MIME タイプ
  FileSize   int64      // バイト数
  PublicURL  string     // 公開 URL
  UserID     string     // アップロード者 sub（ULID）
  CreatedAt  time.Time
  UpdatedAt  time.Time
  DeletedAt  *time.Time
}
```

**バリデーション**:
- `MimeType`: image/jpeg, image/png, image/gif, image/webp のみ許可
- `FileSize`: 最大 5 MB（設定可能）

**DB スキーマ**:
```sql
CREATE TABLE images (
  id          CHAR(26)      PRIMARY KEY,
  storage_key VARCHAR(2000) NOT NULL UNIQUE,
  file_name   VARCHAR(500)  NOT NULL,
  mime_type   VARCHAR(100)  NOT NULL,
  file_size   BIGINT        NOT NULL,
  public_url  VARCHAR(2000) NOT NULL,
  user_id     CHAR(26)      NOT NULL REFERENCES users(sub) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
  deleted_at  TIMESTAMPTZ   NULL
);
```

---

## エラーハンドリング

### エラーレスポンス形式

すべてのエラーレスポンスは統一されたフォーマット:

```json
{
  "code": "ERROR_CODE",
  "message": "人間が読める説明",
  "timestamp": "2026-04-16T10:45:12Z",
  "details": {
    "field": "description of the issue"
  }
}
```

### エラーコード（HTTP ステータスとマッピング）

| HTTP ステータス | エラーコード | 説明 | 例 |
|---|---|---|---|
| 400 | BAD_REQUEST | リクエストボディの形式が不正 | 無効な JSON |
| 400 | VALIDATION_ERROR | バリデーション失敗 | Username が短すぎる |
| 401 | UNAUTHORIZED | 認証失敗 | トークン欠落/無効 |
| 403 | FORBIDDEN | 権限がない | 他ユーザーのリソースにアクセス |
| 404 | NOT_FOUND | リソースが見つからない | ユーザー/投稿/コメント |
| 409 | CONFLICT | リソースが既に存在 | ユーザー名重複 |
| 413 | PAYLOAD_TOO_LARGE | ペイロードサイズ超過 | 画像ファイルが大きすぎる |
| 415 | UNSUPPORTED_MEDIA_TYPE | サポートされていないメディアタイプ | アップロード形式が対応なし |
| 500 | INTERNAL_SERVER_ERROR | サーバーエラー | 予期しないエラー |
| 503 | SERVICE_UNAVAILABLE | サービス利用不可 | R2 サービス接続不可 |

### エラー処理の実装

**ハンドラーでの使用例**:

```go
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
  sub := r.PathValue("sub")

  user, err := h.getUser.Execute(r.Context(), sub)
  if err != nil {
    WriteErrorResponse(w, err)
    return
  }

  // Success response
  w.Header().Set("Content-Type", "application/json")
  json.NewEncoder(w).Encode(user)
}
```

**WriteErrorResponse ヘルパー**:

```go
func WriteErrorResponse(w http.ResponseWriter, err error) {
  w.Header().Set("Content-Type", "application/json")

  // エラータイプに応じた HTTP ステータスコード決定
  statusCode := http.StatusInternalServerError
  if _, ok := err.(*ValidationError); ok {
    statusCode = http.StatusBadRequest
  } else if _, ok := err.(*NotFoundError); ok {
    statusCode = http.StatusNotFound
  } else if _, ok := err.(*UnauthorizedError); ok {
    statusCode = http.StatusUnauthorized
  } else if _, ok := err.(*ForbiddenError); ok {
    statusCode = http.StatusForbidden
  }

  w.WriteHeader(statusCode)
  json.NewEncoder(w).Encode(ErrorResponse{
    Code:      getErrorCode(err),
    Message:   err.Error(),
    Timestamp: time.Now(),
  })
}
```

---

## セキュリティ考慮事項

### 1. セッション検証（AuthCore Introspection）

**検証方法**:
- Cookie 値を毎リクエスト AuthCore の introspection エンドポイントで検証
- service-to-service bearer token（`AUTHCORE_SERVICE_TOKEN`）で保護
- 30s の in-memory キャッシュでスパイクを吸収

**失敗時の挙動**:
- AuthCore が 401/403 → SNS も 401
- AuthCore が到達不能 / 5xx → SNS は 503
- Cookie 欠落 → 401
- 署名・有効期限・revoke 判定はすべて AuthCore 側が行う

### 2. CSRF 保護

- OAuth / CSRF 用の state トークン発行は AuthCore 側の責務
- SNS バックエンドは AuthCore が発行した不透明セッション Cookie を
  そのまま読み、`SameSite` 属性も AuthCore 側で設定される

### 3. XSS 対策

**Cookie 属性**:
- AuthCore が `HttpOnly` / `Secure` / `SameSite` を設定する前提
- SNS バックエンドは Cookie を書き込まない

**コンテンツの検証**:
- 投稿コンテンツは HTML エスケープして保存

### 4. SQLi 対策

**パラメータ化クエリ**:
```go
// 良い：パラメータ化
db.QueryRow("SELECT * FROM users WHERE id = $1", userID)

// 悪い：文字列連結
db.QueryRow("SELECT * FROM users WHERE id = " + userID)
```

### 5. 認可

**エンドポイントレベル**:
- 一部エンドポイントは認証必須（`AuthMiddleware` + `HydrateUserMiddleware`）
- ユーザーは自分のリソースのみ操作可能
- admin 判定は `users.is_admin`（ユーザー自身では書き換え不可）

**チェック例**:
```go
// 投稿削除時、所有者確認
sub, _ := auth.GetSubFromContext(ctx)
if post.UserID != sub {
  return errors.Forbidden("can only delete own posts")
}
```

### 6. レート制限

**将来の実装**: Redis を利用したレート制限

```go
// 例：1 分間に 60 リクエストまで
sub, _ := auth.GetSubFromContext(ctx)
key := fmt.Sprintf("ratelimit:%s:%d", sub, time.Now().Minute())
count, _ := redis.Get(ctx, key)
if count >= 60 {
  return http.StatusTooManyRequests
}
```

### 7. 認証の委譲

本サービスは credential を一切保持しない。identity / パスワード /
OAuth プロバイダー連携はすべて AuthCore 側の責務。

### 8. 環境変数の管理

**秘密情報の環境変数化**:
```bash
# .env（ローカル開発用）
AUTHCORE_SERVICE_TOKEN=your_service_token   # AuthCore から払い出し
DB_PASSWORD=your_db_password
R2_SECRET_ACCESS_KEY=your_r2_secret
```

**本番環境**:
- 環境変数は CI/CD または シークレット管理ツールで提供
- `.env` は リポジトリに含めない

### 9. ロギング

**機密情報の除外**:
```go
// 悪い：Cookie 値をログ
log.Info("Auth attempt", "cookie", cookieValue)

// 良い：sub のみをログ
log.Info("Auth attempt", "sub", sub)
```

---

## 開発ワークフロー

### ローカル開発セットアップ

#### 1. 環境構築

```bash
# リポジトリクローン
git clone https://github.com/your-org/fuju-backend.git
cd backend

# 依存関係のインストール
go mod download

# .env ファイルの用意
cp .env.example .env
```

#### 2. Docker Compose 起動

```bash
# すべてのサービスを起動
docker compose up -d

# ログ確認
docker compose logs -f backend

# ヘルスチェック
curl http://localhost:8080/health

# Adminer でデータベース確認
# ブラウザで http://localhost:8081 にアクセス
```

#### 3. ローカル実行（オプション）

```bash
# ホスト環境で直接実行（Docker デーモンは起動したままにする）
go run ./cmd/server

# または
make run
```

### ブランチゴイングとプルリクエスト

#### ブランチ名規約

```
feature/[機能名]            機能追加
fix/[バグ名]                バグ修正
refactor/[内容]             リファクタリング
docs/[ドキュメント名]      ドキュメント更新
```

#### フロー

1. **ブランチ作成**
   ```bash
   git checkout -b feature/authcore-hydrate
   ```

2. **コミット**
   ```bash
   git add .
   git commit -m "feat(auth): Add AuthCore hydrate middleware"
   ```

   **コミットメッセージ規約**:
   ```
   <type>(<scope>): <subject>

   type: feat, fix, refactor, docs, test
   scope: auth, user, post, etc.
   subject: 英語で簡潔に
   ```

3. **プッシュ**
   ```bash
   git push origin feature/authcore-hydrate
   ```

4. **プルリクエスト作成（GitHub UI または CLI）**
   ```bash
   gh pr create --title "Add AuthCore hydrate middleware" \
               --body "Lazy-create users row on first request and refresh cached profile fields on 1h TTL"
   ```

5. **CI/CD チェック確認**
   - lint（golangci-lint）
   - security（gosec）
   - test（go test）
   - build（マルチプラットフォーム）

6. **マージ**
   ```bash
   gh pr merge <pr-number> --squash --delete-branch
   ```

### コード品質チェック

#### ローカルリント実行

```bash
# すべてのリント検査
golangci-lint run ./...

# 特定ルールのチェック
golangci-lint run --no-config --disable-all --enable=errcheck ./...
```

#### テスト実行

```bash
# すべてのテスト
go test ./...

# カバレッジ測定
go test -cover ./...

# カバレッジ詳細
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

#### ビルド検証

```bash
# ローカルビルド
go build -o fuju-backend ./cmd/server

# クロスコンパイル
GOOS=linux GOARCH=amd64 go build -o fuju-backend-linux ./cmd/server
GOOS=darwin GOARCH=arm64 go build -o fuju-backend-darwin ./cmd/server
```

---

## トラブルシューティング

### Docker コンテナが起動しない

```bash
# ログ確認
docker compose logs backend

# コンテナの状態確認
docker compose ps

# すべてリセットして再起動
docker compose down -v
docker compose up -d
```

### データベース接続エラー

```bash
# PostgreSQL が起動しているか確認
docker compose ps postgres

# PostgreSQL ヘルスチェック
docker compose logs postgres

# 接続テスト
docker exec fuju-postgres pg_isready -U fuju_user
```

### CORS エラー

```bash
# 設定確認
docker exec fuju-backend env | grep CORS

# 環境変数を確認または docker-compose.yml で確認
cat .env | grep CORS_ALLOWED_ORIGINS
```

### ポートが既に使用されている

```bash
# ポート確認
lsof -i :8080

# 別のポートから起動
SERVER_PORT=9000 docker compose up -d
```

---

## 参考資料

- [Go Documentation](https://golang.org/doc)
- [PostgreSQL Documentation](https://www.postgresql.org/docs/)
- [ULID Specification](https://github.com/ulid/spec)
- [OWASP Security Guidelines](https://owasp.org/)
- [Docker Documentation](https://docs.docker.com/)

---

**このドキュメントは随時更新されます。最終更新日を確認してください。**
