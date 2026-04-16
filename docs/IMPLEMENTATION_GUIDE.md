# FUJU Backend - 実装ガイド

**最終更新: 2026年4月16日**
**バージョン: 1.0**

---

## 目次

1. [システム概要](#システム概要)
2. [アーキテクチャ](#アーキテクチャ)
3. [環境構成](#環境構成)
4. [認証とセッション管理](#認証とセッション管理)
5. [API エンドポイント仕様](#apiエンドポイント仕様)
6. [ミドルウェア](#ミドルウェア)
7. [データモデル](#データモデル)
8. [エラーハンドリング](#エラーハンドリング)
9. [セキュリティ考慮事項](#セキュリティ考慮事項)
10. [開発ワークフロー](#開発ワークフロー)

---

## システム概要

### 概要

FUJUは、ソーシャルメディアプラットフォーム向けのバックエンドアプリケーションです。Goで実装され、以下の主要機能を提供します：

- **ユーザー管理**: ユーザー登録、プロフィール管理
- **投稿機能**: 投稿の作成、読取、削除
- **コメント機能**: 投稿へのコメント追加/削除
- **画像管理**: Cloudflare R2へのアップロード
- **認証**: OAuth2（Google、GitHub）サポート、JWT トークンベースの認証
- **CORS サポート**: フロントエンドとの通信を許可

### 主要な技術スタック

| コンポーネント | 技術 | バージョン |
|---|---|---|
| **言語** | Go | 1.24 |
| **データベース** | PostgreSQL | 16-Alpine |
| **キャッシュ/セッション** | Redis | 7-Alpine |
| **コンテナ化** | Docker Compose | 3.8 |
| **ロギング** | structured logging | - |
| **認証** | JWT + OAuth2 | - |
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
                 OAuth Redirect Flow
                          │
┌─────────────────────────────────────────────────────────┐
│                Backend API Server                        │
│              http://localhost:8080                       │
├─────────────────────────────────────────────────────────┤
│  HTTP Router (Go 1.24)                                   │
│  ├── /health                                             │
│  ├── /auth/oauth/*                                       │
│  ├── /users/*                                            │
│  ├── /posts/*                                            │
│  └── /v1/images/*                                        │
└─────────────────────────────────────────────────────────┘
          │              │              │
          ▼              ▼              ▼
    ┌─────────────┬─────────────┬─────────────┐
    │ PostgreSQL  │    Redis    │ Cloudflare  │
    │ (Port 5432) │ (Port 6379) │   R2 API    │
    └─────────────┴─────────────┴─────────────┘
```

### レイヤー構造

```
┌─────────────────────────────────────────┐
│         HTTP Handler Layer              │
│  ├── auth.go                            │
│  ├── user.go                            │
│  ├── post.go                            │
│  ├── comment.go                         │
│  └── image.go                           │
└─────────────────────────────────────────┘
            ↓
┌─────────────────────────────────────────┐
│   Middleware Layer                      │
│  ├── CORS Middleware                    │
│  ├── Authentication Middleware          │
│  ├── Logging Middleware                 │
│  └── Recovery Middleware                │
└─────────────────────────────────────────┘
            ↓
┌─────────────────────────────────────────┐
│   Use Case Layer (Business Logic)       │
│  ├── user/usecase.go                    │
│  ├── post/usecase.go                    │
│  ├── comment/usecase.go                 │
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
│  ├── comment.go                         │
│  └── image.go                           │
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
- `users` - ユーザー情報
- `posts` - 投稿
- `comments` - コメント
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
- セッションストア
- キャッシング
- 将来: リアルタイム通知

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

### OAuth2 フロー

#### 1. 認可要求フェーズ

**エンドポイント**: `POST /auth/oauth/authorize`

**リクエスト**:
```json
{
  "provider": "google",  // or "github"
  "redirect_uri": "http://localhost:8080/auth/oauth/callback"  // オプション
}
```

**処理フロー**:
1. Provider を検証
2. 16 バイトのランダム CSRF 保護トークン生成（hex エンコード）
3. OAuth URL を構築:
   ```
   https://accounts.google.com/o/oauth2/v2/auth?
     client_id={CLIENT_ID}
     &redirect_uri=http://localhost:8080/auth/oauth/callback
     &response_type=code
     &scope=openid+email+profile
     &state={STATE_TOKEN}
   ```
4. リダイレクト URL を レスポンス

**レスポンス**:
```json
{
  "redirect_url": "https://accounts.google.com/o/oauth2/v2/auth?...",
  "redirect_uri": "http://localhost:8080/auth/oauth/callback"
}
```

#### 2. コールバック処理フェーズ

**エンドポイント**:
- `GET /auth/oauth/callback` - OAuth プロバイダーからのリダイレクト
- `POST /auth/oauth/callback` - フロントエンドからの直接送信（レガシー）

**GET 処理（プロバイダーリダイレクト）**:

クエリパラメータ:
```
code: OAuth プロバイダーから取得
state: CSRF トークン（検証用）
```

処理フロー:
1. `code` と `state` を検証
2. `return_to` パラメータからリダイレクト先 URL を取得（未指定時は`FRONTEND_URL`）
3. フロントエンドへリダイレクト:
   ```
   http://localhost:5173/?code=4/0Aci98E--...&state=508f4e9f...
   ```

**POST 処理（レガシー対応）**:

リクエスト:
```json
{
  "code": "4/0Aci98E--LlB8rdAuUbw9oh5m0I6Da4keyz9lvGShY3BgSaLlwxFRiv1ItoXZoiY8PoXtiw",
  "state": "508f4e9fbbdbf7df62e1d3333d45bc6f",
  "device_type": "web"  // オプション
}
```

レスポンス:
```json
{
  "success": true,
  "code": "4/0Aci98E--LlB8rdAuUbw9oh5m0I6Da4keyz9lvGShY3BgSaLlwxFRiv1ItoXZoiY8PoXtiw",
  "state": "508f4e9fbbdbf7df62e1d3333d45bc6f"
}
```

### JWT トークン管理

#### トークン生成

**エンドポイント**: `/auth/oauth/callback` または フロントエンド側で処理

**トークンペイロード**:
```json
{
  "user_id": 123,
  "email": "user@example.com",
  "exp": 1713298800,  // 現在 + 1800秒（30分）
  "iat": 1713296800
}
```

**トークン仕様**:
- **署名アルゴリズム**: HS256
- **秘密鍵**: `JWT_SECRET` 環境変数
- **有効期限**: デフォルト 1800 秒（30分）*設定可能*
- **リフレッシュトークン有効期限**: 3600 秒（60分）

#### トークン検証プロセス

1. Authorization ヘッダーから取得: `Bearer {token}`
2. 署名を検証
3. 有効期限をチェック
4. ペイロードから `user_id` を抽出

#### リフレッシュトークンエンドポイント

**エンドポイント**: `POST /auth/refresh`

**リクエスト**:
```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**処理フロー**:
1. リフレッシュトークン検証
2. 新しいアクセストークン生成
3. 新しいリフレッシュトークン生成

**レスポンス**:
```json
{
  "access_token": "new_jwt_token",
  "refresh_token": "new_refresh_token",
  "expires_in": 1800
}
```

#### ログアウト

**エンドポイント**: `POST /auth/logout`

**前提条件**: 認証済み（Authorization ヘッダー必須）

**処理フロー**:
1. セッションクッキーをクリア
2. Cookie 属性:
   - `HttpOnly`: XSS 攻撃対策
   - `Secure`: HTTPS のみ
   - `SameSite=Strict`: CSRF 攻撃対策

**レスポンス**:
```json
{
  "message": "success"
}
```

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

### 2. 認証エンドポイント

#### `POST /auth/oauth/authorize`

OAuth 認可 URL を生成

**認証**: 不要

**リクエストボディ**:
```json
{
  "provider": "google",  // "google" または "github"
  "redirect_uri": "http://localhost:8080/auth/oauth/callback"  // オプション
}
```

**レスポンス** (200 OK):
```json
{
  "redirect_url": "https://accounts.google.com/o/oauth2/v2/auth?...",
  "redirect_uri": "http://localhost:8080/auth/oauth/callback"
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 400 Bad Request | 無効なリクエストボディ |
| 400 Bad Request | `provider` が必須 |
| 500 Internal Server Error | State トークン生成失敗 |

---

#### `GET /auth/oauth/callback`

OAuth プロバイダーからのリダイレクト先

**認証**: 不要

**クエリパラメータ**:
```
code: OAuth プロバイダーから取得したコード
state: CSRF 保護トークン
return_to: (オプション) リダイレクト先 URL
```

**処理**:
フロントエンドへリダイレクト（302 Found）:
```
Location: http://localhost:5173/?code={code}&state={state}
```

**エラー**:
- `code` / `state` 欠落時: `?error=code_and_state_are_required`
- State トークン不一致時: `?error=state_mismatch`

---

#### `POST /auth/oauth/callback`

OAuth コールバック（レガシー JSON レスポンス）

**認証**: 不要

**リクエストボディ**:
```json
{
  "code": "4/0Aci98E--LlB8rdAuUbw9oh5m0I6Da4keyz...",
  "state": "508f4e9fbbdbf7df62e1d3333d45bc6f",
  "device_type": "web"  // オプション
}
```

**レスポンス** (200 OK):
```json
{
  "success": true,
  "code": "4/0Aci98E--LlB8rdAuUbw9oh5m0I6Da4keyz...",
  "state": "508f4e9fbbdbf7df62e1d3333d45bc6f"
}
```

---

#### `POST /auth/refresh`

トークンリフレッシュ

**認証**: 不要

**リクエストボディ**:
```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**レスポンス** (200 OK):
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 1800
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 400 Bad Request | `refresh_token` が必須 |
| 401 Unauthorized | 無効なリフレッシュトークン |

---

#### `POST /auth/logout`

ログアウト

**認証**: 必須（Bearer トークン）

**リクエストヘッダー**:
```
Authorization: Bearer {access_token}
```

**レスポンス** (200 OK):
```json
{
  "message": "success"
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 401 Unauthorized | 認証ヘッダー欠落/無効 |

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
      "id": 1,
      "username": "john_doe",
      "email": "john@example.com",
      "display_name": "John Doe",
      "bio": "Software Engineer",
      "avatar_url": "https://example.com/avatar.jpg",
      "oauth_provider": "google",
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

#### `POST /users`

新規ユーザー作成

**認証**: 必須（Bearer トークン）

**リクエストボディ**:
```json
{
  "username": "john_doe",
  "email": "john@example.com",
  "display_name": "John Doe",
  "bio": "Software Engineer",
  "avatar_url": "https://example.com/avatar.jpg",
  "oauth_provider": "google",
  "oauth_id": "107891234567890123456"
}
```

**バリデーション**:
- `username`: 3-50 文字
- `email`: 有効なメールアドレス
- `oauth_id`: プロバイダー側の ID

**レスポンス** (201 Created):
```json
{
  "id": 1,
  "username": "john_doe",
  "email": "john@example.com",
  ...
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 400 Bad Request | 無効なリクエストボディ |
| 409 Conflict | ユーザーが既に存在 |
| 401 Unauthorized | 認証ヘッダー欠落/無効 |

---

#### `GET /users/{id}`

特定ユーザー取得

**認証**: 不要

**パスパラメータ**:
```
id: ユーザー ID
```

**レスポンス** (200 OK):
```json
{
  "id": 1,
  "username": "john_doe",
  "email": "john@example.com",
  "display_name": "John Doe",
  ...
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 404 Not Found | ユーザーが見つからない |

---

#### `PUT /users/{id}`

ユーザープロフィール更新

**認証**: 必須（Bearer トークン）

本人のみが自身のプロフィール更新可能。

**リクエストボディ**:
```json
{
  "display_name": "John Updated",
  "bio": "Senior Software Engineer",
  "avatar_url": "https://example.com/new_avatar.jpg"
}
```

**レスポンス** (200 OK):
```json
{
  "id": 1,
  "username": "john_doe",
  ...
  "display_name": "John Updated",
  "bio": "Senior Software Engineer",
  "avatar_url": "https://example.com/new_avatar.jpg",
  "updated_at": "2026-04-16T10:30:45Z"
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 403 Forbidden | 他ユーザーの更新を試行 |
| 404 Not Found | ユーザーが見つからない |
| 401 Unauthorized | 認証ヘッダー欠落/無効 |

---

### 4. 投稿エンドポイント

#### `GET /posts`

投稿一覧取得

**認証**: 不要

**クエリパラメータ**:
```
page: ページ番号（デフォルト: 1）
limit: 1 ページあたりの件数（デフォルト: 20）
user_id: 特定ユーザーの投稿に限定（オプション）
```

**レスポンス** (200 OK):
```json
{
  "data": [
    {
      "id": 1,
      "user_id": 1,
      "content": "Hello, FUJU!",
      "image_urls": ["https://example.com/image1.jpg"],
      "likes_count": 10,
      "comments_count": 2,
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

**認証**: 必須（Bearer トークン）

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
  "id": 123,
  "user_id": 1,
  "content": "Hello, FUJU! This is my first post.",
  "image_urls": [...],
  "likes_count": 0,
  "comments_count": 0,
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
id: 投稿 ID
```

**レスポンス** (200 OK):
```json
{
  "id": 123,
  "user_id": 1,
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

**認証**: 必須（Bearer トークン）

投稿作成者のみが削除可能。

**パスパラメータ**:
```
id: 投稿 ID
```

**レスポンス** (204 No Content)

**エラー**:
| ステータス | 説明 |
|---|---|
| 403 Forbidden | 他ユーザーの投稿を削除しようとした |
| 404 Not Found | 投稿が見つからない |
| 401 Unauthorized | 認証ヘッダー欠落/無効 |

---

### 5. コメントエンドポイント

#### `POST /posts/{id}/comments`

投稿にコメント追加

**認証**: 必須（Bearer トークン）

**パスパラメータ**:
```
id: 投稿 ID
```

**リクエストボディ**:
```json
{
  "content": "Great post!"
}
```

**バリデーション**:
- `content`: 1-1000 文字

**レスポンス** (201 Created):
```json
{
  "id": 456,
  "post_id": 123,
  "user_id": 2,
  "content": "Great post!",
  "created_at": "2026-04-16T10:35:00Z",
  "updated_at": "2026-04-16T10:35:00Z"
}
```

**エラー**:
| ステータス | 説明 |
|---|---|
| 404 Not Found | 投稿が見つからない |
| 400 Bad Request | 無効なリクエストボディ |
| 401 Unauthorized | 認証ヘッダー欠落/無効 |

---

#### `DELETE /posts/{post_id}/comments/{comment_id}`

コメント削除

**認証**: 必須（Bearer トークン）

コメント作成者のみが削除可能。

**パスパラメータ**:
```
post_id: 投稿 ID
comment_id: コメント ID
```

**レスポンス** (204 No Content)

**エラー**:
| ステータス | 説明 |
|---|---|
| 403 Forbidden | 他ユーザーのコメントを削除しようとした |
| 404 Not Found | コメント/投稿が見つからない |
| 401 Unauthorized | 認証ヘッダー欠落/無効 |

---

### 6. 画像エンドポイント

> **注**: Cloudflare R2 サービスが設定されている場合のみ利用可能

#### `POST /v1/images`

画像アップロード

**認証**: 必須（Bearer トークン）

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
  "id": 789,
  "user_id": 1,
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
| 401 Unauthorized | 認証ヘッダー欠落/無効 |
| 503 Service Unavailable | R2 サービスに接続できない |

---

#### `GET /v1/images`

ユーザーの画像一覧取得

**認証**: 必須（Bearer トークン）

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
      "id": 789,
      "user_id": 1,
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

**認証**: 必須（Bearer トークン）

画像の所有者のみが削除可能。

**パスパラメータ**:
```
id: 画像 ID
```

**レスポンス** (204 No Content)

R2 バケットからも削除される。

**エラー**:
| ステータス | 説明 |
|---|---|
| 403 Forbidden | 他ユーザーの画像を削除しようとした |
| 404 Not Found | 画像が見つからない |
| 401 Unauthorized | 認証ヘッダー欠落/無効 |

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

### 2. 認証ミドルウェア

**ファイル**: `internal/middleware/middleware.go`

**機能**: JWT トークン検証と認証ユーザーの抽出

**処理フロー**:
1. `Authorization` ヘッダーから `Bearer {token}` を取得
2. トークン署名検証
3. 有効期限チェック
4. `user_id` をコンテキストに保存
5. 無効な場合は 401 Unauthorized を返す

**トークン形式**:
```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

**エラーレスポンス** (401 Unauthorized):
```json
{
  "code": "UNAUTHORIZED",
  "message": "Missing or invalid authentication",
  "timestamp": "2026-04-16T10:45:12Z"
}
```

**コンテキスト保存**:
```go
// ハンドラー内でのアクセス
userID, ok := auth.GetUserIDFromContext(r.Context())
```

### 3. ロギングミドルウェア

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

### 4. リカバリーミドルウェア

**機能**: パニックハンドリング

**処理**: 未処理のパニックをキャッチしてログ、500 エラーを返す

---

## データモデル

### User ドメインモデル

**ファイル**: `internal/domain/user.go`

```go
type User struct {
  ID            int64          // ユーザー ID（主キー）
  Username      string         // ユーザー名（3-50 文字）
  Email         string         // メールアドレス（ユニーク）
  DisplayName   string         // 表示名
  Bio           string         // プロフィール説明
  AvatarURL     string         // プロフィール画像 URL
  OAuthProvider string         // OAuth プロバイダー（"google", "github" など）
  OAuthID       string         // プロバイダー側の ID
  CreatedAt     time.Time      // 作成日時
  UpdatedAt     time.Time      // 更新日時
  DeletedAt     *time.Time     // 削除日時（ソフトデリート）
}
```

**バリデーション**:
- `Username`: 3-50 文字、英数字とアンダースコアのみ
- `Email`: 有効なメールアドレス形式
- `OAuthProvider`: "google" または "github"

**DB スキーマ**:
```sql
CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  username VARCHAR(50) UNIQUE NOT NULL,
  email VARCHAR(255) UNIQUE NOT NULL,
  display_name VARCHAR(100),
  bio TEXT,
  avatar_url TEXT,
  oauth_provider VARCHAR(20),
  oauth_id VARCHAR(255) UNIQUE,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP
);
```

---

### Post ドメインモデル

**ファイル**: `internal/domain/post.go`

```go
type Post struct {
  ID            int64          // 投稿 ID（主キー）
  UserID        int64          // 投稿者 ID（外部キー）
  Content       string         // 投稿内容（1-5000 文字）
  ImageURLs     []string       // 画像 URL リスト（最大 10 枚）
  LikesCount    int64          // いいね数
  CommentsCount int64          // コメント数
  CreatedAt     time.Time      // 作成日時
  UpdatedAt     time.Time      // 更新日時
  DeletedAt     *time.Time     // 削除日時（ソフトデリート）
}
```

**バリデーション**:
- `Content`: 1-5000 文字
- `ImageURLs`: 最大 10 枚
- `UserID`: 必須

**DB スキーマ**:
```sql
CREATE TABLE posts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  content TEXT NOT NULL,
  image_urls TEXT[] DEFAULT ARRAY[]::TEXT[],
  likes_count BIGINT DEFAULT 0,
  comments_count BIGINT DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP
);
```

---

### Comment ドメインモデル

**ファイル**: `internal/domain/comment.go`

```go
type Comment struct {
  ID        int64          // コメント ID（主キー）
  PostID    int64          // 投稿 ID（外部キー）
  UserID    int64          // コメント投稿者 ID（外部キー）
  Content   string         // コメント内容（1-1000 文字）
  CreatedAt time.Time      // 作成日時
  UpdatedAt time.Time      // 更新日時
  DeletedAt *time.Time     // 削除日時（ソフトデリート）
}
```

**バリデーション**:
- `Content`: 1-1000 文字
- `PostID` / `UserID`: 必須

**DB スキーマ**:
```sql
CREATE TABLE comments (
  id BIGSERIAL PRIMARY KEY,
  post_id BIGINT NOT NULL REFERENCES posts(id),
  user_id BIGINT NOT NULL REFERENCES users(id),
  content TEXT NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP
);
```

---

### Image ドメインモデル

**ファイル**: `internal/domain/image.go`

```go
type Image struct {
  ID        int64          // 画像 ID（主キー）
  UserID    int64          // アップロード者 ID（外部キー）
  Filename  string         // ファイル名（R2 内の キー）
  URL       string         // 公開 URL
  Title     string         // 画像タイトル
  Description string       // 説明
  FileSize  int64          // ファイルサイズ（バイト）
  MimeType  string         // MIME タイプ（"image/jpeg" など）
  CreatedAt time.Time      // 作成日時
  UpdatedAt time.Time      // 更新日時
  DeletedAt *time.Time     // 削除日時（ソフトデリート）
}
```

**バリデーション**:
- `MimeType`: image/jpeg, image/png, image/gif, image/webp のみ許可
- `FileSize`: 最大 5 MB（設定可能）

**DB スキーマ**:
```sql
CREATE TABLE images (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id),
  filename VARCHAR(255) NOT NULL,
  url TEXT NOT NULL,
  title VARCHAR(255),
  description TEXT,
  file_size BIGINT NOT NULL,
  mime_type VARCHAR(50) NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP
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
  userID, err := h.getUserIDFromPath(w, r)
  if err != nil {
    WriteErrorResponse(w, err)
    return
  }

  user, err := h.getUser.Execute(r.Context(), userID)
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

### 1. JWT トークンセキュリティ

**署名検証**:
- すべてのトークンは `JWT_SECRET` で HS256 署名
- 署名検証に失敗した場合は 401 を返す

**有効期限**:
- デフォルト: 1800 秒（30分）
- 有効期限切れトークンは無効

**リフレッシュトークン**:
- 有効期限: 3600 秒（60分）
- 新しいアクセストークンの取得に使用

### 2. CSRF 保護

**State トークン**:
- OAuth フローで 16 バイトのランダムトークン生成
- State 値のエコーバック検証により CSRF を防止

**SameSite クッキー属性**:
```go
cookie := &http.Cookie{
  Name:     "session",
  HttpOnly: true,
  Secure:   true,
  SameSite: http.SameSiteLax,
}
```

### 3. XSS 対策

**HttpOnly クッキー**:
```go
cookie.HttpOnly = true  // JavaScript からのアクセス不可
```

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
- 一部エンドポイントは認証必須（AuthMiddleware）
- ユーザーは自分のリソースのみ操作可能

**チェック例**:
```go
// 投稿削除時、所有者確認
if post.UserID != userID {
  return errors.Forbidden("can only delete own posts")
}
```

### 6. レート制限

**将来の実装**: Redis を利用したレート制限

```go
// 例：1 分間に 60 リクエストまで
key := fmt.Sprintf("ratelimit:%d:%d", userID, time.Now().Minute())
count, _ := redis.Get(ctx, key)
if count >= 60 {
  return http.StatusTooManyRequests
}
```

### 7. パスワードレス認証

現在は OAuth2 のみサポート。パスワードは保存していない。

### 8. 環境変数の管理

**秘密情報の環境変数化**:
```bash
# .env（ローカル開発用）
JWT_SECRET=your_secret_key (本番環境では強力な値)
OAUTH_CLIENT_SECRET=your_secret (GitHub/Google より取得)
DB_PASSWORD=your_db_password
```

**本番環境**:
- 環境変数は CI/CD または シークレット管理ツールで提供
- `.env` は リポジトリに含めない

### 9. ロギング

**機密情報の除外**:
```go
// 悪い：パスワードをログ
log.Info("User login", "password", password)

// 良い：機密情報除外
log.Info("User login", "user_id", userID)
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
   git checkout -b feature/oauth-redirect
   ```

2. **コミット**
   ```bash
   git add .
   git commit -m "feat(auth): Implement OAuth redirect to frontend"
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
   git push origin feature/oauth-redirect
   ```

4. **プルリクエスト作成（GitHub UI または CLI）**
   ```bash
   gh pr create --title "OAuth callback redirect to frontend" \
               --body "Enables OAuth callback to redirect to frontend with code/state parameters"
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
- [OAuth 2.0 Specification](https://tools.ietf.org/html/rfc6749)
- [JWT (JSON Web Token) Specification](https://tools.ietf.org/html/rfc7519)
- [OWASP Security Guidelines](https://owasp.org/)
- [Docker Documentation](https://docs.docker.com/)

---

**このドキュメントは随時更新されます。最終更新日を確認してください。**
