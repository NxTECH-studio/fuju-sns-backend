# AuthCore 連携ガイド（フロントエンド向け）

本リポジトリは、認証基盤として別サービスである **AuthCore** を利用します。
FUJU バックエンド自身は credential を保持しません。フロントエンドからは
「AuthCore で access token を取得 → FUJU API には Bearer で付与」の 2 ステップ
で動きます。

> **本リポは Cookie セッション方式ではありません。**
> 旧 README / 旧実装ガイドには `authcore_session` Cookie に関する記述が
> 残っていたことがありますが、現行は **AuthCore 発行の Bearer JWT（access token）
> のみ**で動作します。バックエンドは `Set-Cookie` ヘッダを返しません。

---

## 1. 全体像

```
[Client (web / mobile)]             [AuthCore]                [FUJU Backend]
        |                                |                           |
        | (1) login flow (redirect /     |                           |
        |     OIDC / 2FA etc.)           |                           |
        |-------------------------------->                           |
        |      access_token              |                           |
        |<-------------------------------|                           |
        |                                |                           |
        | (2) Authorization: Bearer <token>                          |
        |------------------------------------------------------------>|
        |                                | POST /v1/auth/introspect  |
        |                                |<--------------------------|
        |                                | { active, sub, exp, ... } |
        |                                |-------------------------->|
        |                         response (200 / 401 / 503)         |
        |<------------------------------------------------------------|
```

- **AuthCore** は identity provider。ログイン / MFA / token 発行 / rotation /
  revoke を責任を持って行います。
- **FUJU Backend** は resource server。毎リクエスト、受け取った access token を
  AuthCore の introspection エンドポイント (RFC 7662) に送って有効性を確認
  します。

## 2. トークン取得フロー（FE 視点）

詳細な OAuth 2.1 / OIDC の仕様は AuthCore 側のドキュメントを参照してください。
**FE が最低限知るべきことは 2 つだけ**です:

1. AuthCore の認可エンドポイントへユーザーをリダイレクトしてログインさせ、
   発行された **access token**（および必要なら refresh token）を取得する。
2. FUJU のすべての API 呼び出しに `Authorization: Bearer <access_token>` を
   付ける。

どのエンドポイントが認証必須かは [`docs/swagger.yaml`](./swagger.yaml) の各
operation の `security` を見てください。`security: [{ BearerAuth: [] }]` が
認証必須、`security: []` が公開です。公開エンドポイントでも Bearer を付ければ
`liked_by_viewer` / `following_author` などの呼び出し元固有フィールドが埋まり
ます（付けなくても 200 は返ります）。

## 3. トークン失効時の挙動

access token が期限切れ、もしくは AuthCore 側で revoke された場合、FUJU は
401 `UNAUTHORIZED` を返します:

```json
{
  "code": "UNAUTHORIZED",
  "message": "authentication required",
  "timestamp": "2026-04-21T10:30:00Z"
}
```

FE の期待される動作:

1. 401 を受けたら、保持している refresh token で AuthCore にリトライ → 新しい
   access token を取得。
2. refresh も失敗したら、ログイン画面へ誘導。
3. 401 ループにならないよう、refresh 試行は 1 リクエストにつき 1 回まで。

上流 (AuthCore) が到達不能な場合は **503** が返ります。FE はリトライしても
しばらく不通であることをユーザーに示してください。

## 4. Introspection キャッシュ

バックエンドは introspection の結果を **30 秒の in-memory キャッシュ**に保持
します（`AUTHCORE_INTROSPECT_CACHE_TTL`、既定 `30s`）。これは AuthCore への
負荷を下げるための最適化で、FE からは透過です。

ただし次の挙動に注意:

- ログイン直後にユーザーをサーバサイドで即ログアウトさせるようなテストケース
  では、**revoke が反映されるまで最大 30 秒のラグ**があります。
- 本番運用では問題にはなりません（通常の期限切れや再認証は ms オーダーでの
  即時反映を要求しない）が、E2E テストで「logout 直後に 401 を期待」のような
  ケースではキャッシュ TTL を短く設定するか、別アカウントに切り替えてください。

## 5. `/me` と hydrate フロー

一次資料: [`internal/middleware/middleware.go`](../internal/middleware/middleware.go),
[`internal/usecase/user/usecase.go`](../internal/usecase/user/usecase.go)

FE はログイン後、**最初に `GET /me` を叩いて自身のユーザーレコードを取得**
することを推奨します。理由:

- バックエンドの `users` テーブルは AuthCore のミラー。初回アクセス時に
  `HydrateUserMiddleware` がそのユーザーのレコードを lazy-create します。
- このタイミングで `display_name` / `display_id` / `icon_url` が AuthCore から
  `GetProfile` で取得され、`*_cached` カラムに格納されます。
- `is_admin` はここで決まる（DB 初期値は `false`、変更は DB 直接操作のみ）。
- 以後、バックエンド発行のユーザー情報はすべて `/me` と同じ shape で返ります。

`GET /me` のレスポンスには **`is_admin` フラグ**が入るので、admin UI を出すか
どうかの判定はこれで行ってください。他人の `GET /users/{sub}` には
`is_admin` は入りません。

## 6. AuthCore profile の mirror TTL

- `display_name_cached` / `display_id_cached` / `icon_url_cached` は **1 時間
  TTL**（`AUTHCORE_PROFILE_TTL`、既定 `1h`）。
- TTL 切れ後、該当ユーザーへの次回アクセス時に自動で再取得 (`GetProfile`) と
  upsert が走ります。
- **すぐに反映したい場合**でも、現状は手動で trigger する API はありません。
  TTL を短く設定するか、`PUT /users/{sub}` で bio を空更新することで
  `updated_at` は動きますが、profile 自体は別経路で更新されます。

AuthCore 側で display_name を変えた直後にフロントに即反映したい UX を実装する
場合は、FE 側で local の state を上書きするフォールバックを用意しておくと
スムーズです。

## 7. Admin 判定

**AuthCore 側のスコープではなく、FUJU 側の `users.is_admin` で判定**します。

- FE は `GET /me` の `is_admin` フラグを見て admin UI を出す。
- admin 付与は DB 直接操作のみ。初回 admin は `db/migrations/005_implement_badges.sql`
  の seed で `UPDATE users SET is_admin=true` により付与されます。
- `/v1/admin/*` 配下へのアクセスは `AdminMiddleware` が 403 を返します
  （非 admin が叩いた場合）。

## 8. セキュリティ上の FE 責務

FUJU バックエンドは Cookie を発行しないため、トークン保存戦略は FE 側の責任
です。

| 懸念 | 推奨 |
|---|---|
| access token の保存先 | **メモリ**（JS 変数 / Redux / Pinia 等）。`localStorage` は XSS で盗まれるので避ける |
| refresh token の保存先 | AuthCore 側が `HttpOnly; Secure; SameSite=Strict` Cookie で発行する想定。FE は触らない |
| XSS 対策 | ユーザー投稿の HTML エスケープ、CSP ヘッダ、依存ライブラリの脆弱性管理 |
| CSRF 対策 | access token を Bearer ヘッダで送る方式なので、CSRF の主要リスクはない。ただし refresh エンドポイント (AuthCore 側) は通常 CSRF トークンを要求する |
| トークンを URL に載せない | `?token=...` のような query parameter に入れない（サーバログ / referer に残る） |

## 9. CORS

バックエンドは `CORS_ALLOWED_ORIGINS` 環境変数（カンマ区切り）で許可オリジンを
指定します:

- **開発**: 既定 `*`（どこからでも OK）
- **本番**: 必ず具体的な origin 列を明示（`https://app.fuju.example.com,https://admin.fuju.example.com`）

許可されていない origin からのリクエストは CORS preflight で弾かれます。
`Authorization` ヘッダは `Access-Control-Allow-Headers` で許可済みです。

## 10. 既知の制約

本ドキュメント時点（2026-04-21）で、FE 実装者が想定しておくべき制約:

- **DB 未配線**: 現状リポジトリ層は in-memory 実装。サーバを再起動すると
  すべてのデータが消えます。Postgres 配線は別タスクで予定（migrations は整備済み）。
- **rate limit 未実装**: `429 Too Many Requests` は現時点で発生しません。将来
  導入時は swagger に反映されます。
- **WebSocket / SSE 未実装**: リアルタイム通知はありません。タイムラインは
  FE 側のポーリングで更新してください。
- **メール / プッシュ通知なし**: 外部通知チャネルは未配線。
- **OGP は best-effort**: §6 で述べた通り、取得失敗は FE からは単に「OGP 無し」
  として見えます。
- **Image レスポンスのキーは PascalCase**: `GET /v1/images` / `POST /v1/images`
  のレスポンスのみ Go のデフォルト shape のまま（[`docs/product-overview.md`](./product-overview.md)
  §7 参照）。
- **Redis なし**: 分散キャッシュは使用していません。AuthCore introspection
  キャッシュも in-memory です。

---

## 参考: バックエンド側実装

FE からは直接は触りませんが、動作を裏取りしたい場合の参照先:

- `pkg/authcore/client.go` — AuthCore HTTP クライアント（`Introspect` / `GetProfile`）
- `pkg/authcore/cache.go` — 30s in-memory introspection キャッシュ
- `internal/middleware/middleware.go` — `AuthMiddleware` / `HydrateUserMiddleware` / `AdminMiddleware`
- `internal/usecase/user/usecase.go` — `GetOrHydrateUserUseCase`（lazy-create + 1h TTL refresh）

## 参考: 関連ドキュメント

- [`docs/product-overview.md`](./product-overview.md) — プロダクトの機能概要
- [`docs/swagger.yaml`](./swagger.yaml) — OpenAPI 3.0.3 スペック
- [`docs/architecture.md`](./architecture.md) — 保守者向けアーキテクチャ
