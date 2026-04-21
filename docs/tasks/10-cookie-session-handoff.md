# Cookie-Based Session Handoff from AuthCore

## 概要

AuthCore が発行する access token を **HttpOnly Cookie (`fuju_access`)** として
FUJU backend 側に預け替える session handoff エンドポイントを新設し、`AuthMiddleware`
を Cookie 対応に拡張する。server-side session store は導入せず、**Cookie の値 =
access token そのもの** という最小構成で進める。ブラウザが Cookie を自動付与する
ことで、frontend の auth-component から access token を直接触れないままでも
FUJU API が認証付きで叩けるようになる。

本タスクは backend 単体で完結するが、frontend 側 `src/auth-component/` の
改修仕様も本ドキュメント内に **contract spec** として固め、frontend チームが
別 PR でそれに従って実装できる状態にする。

## 前提（依存タスク）

- `01-rewrite-domain-to-authcore-alignment.md` 完了済み（AuthCore 連携 / `AuthMiddleware` の現行実装が前提）
- `06-frontend-handoff-docs-and-dead-code-removal.md` 完了済み（`docs/authcore-integration.md` / swagger が本ドキュメントの更新対象として存在）
- `07-fix-ci-go-version-and-lint-hygiene.md` 完了済み（CI green が merge 条件）
- `08-wire-postgres-repository.md` は本タスクと独立。並行着手可
- `09-fix-lint-across-stacked-prs.md` 完了済み（lint violation が残らない状態で着手）
- PR base は **develop**

## 背景・目的

### 現状

- `AuthMiddleware`（`internal/middleware/middleware.go:68-109`）は
  `Authorization: Bearer <access_token>` ヘッダしか受け付けない。
  `extractBearerToken` が失敗するとそのまま 401。
- `CORSMiddleware`（`internal/middleware/middleware.go:181-205`）は
  `Access-Control-Allow-Credentials: true` を**許可オリジン一致時にのみ**
  返すが、`Access-Control-Allow-Origin: *` のケースでは `Allow-Credentials`
  を返していない。Cookie 運用に切り替える場合はオリジン一致経路を必ず通す
  必要がある。
- `cmd/server/main.go` の wiring（216-221 行目）では `CORSMiddleware` /
  `RecoveryMiddleware` / `LoggingMiddleware` / `ContextTimeoutMiddleware`
  が全ルートに対してラップされているだけで、session handoff 用のルートは存在しない。
- frontend の `src/auth-component/` は `AuthStore.accessToken: string | null`
  を **private** に抱えており、`useAuth()` 戻り値にも `index.ts` のエクスポート
  にも access token を外に出していない（`src/auth-component/src/store/AuthStore.ts:47`,
  `416-419`）。hook の外から `Authorization: Bearer ...` ヘッダを組み立てる
  手段がない。
- AuthCore (`/home/sheep/dev/fuju/auth`) は access token を
  `POST /v1/auth/login` / `POST /v1/auth/refresh` / `POST /v1/auth/mfa/verify` /
  social callback の JSON body で返す。refresh token のみ HttpOnly Cookie
  （`refresh_token`, `Path=/v1/auth`）として発行される。
- AuthCore の `POST /v1/auth/introspect` は access token 専用
  （`usecase/introspect_uc/service.go:64` で `TokenTypeAccess` 固定）。
  refresh token を投げても `{active: false}` が返るだけ。

### ゴール

1. frontend が access token を公開状態で触らずに、FUJU backend を認証付きで
   叩ける。ブラウザに Cookie を預けるだけで済む。
2. 既存の `Authorization: Bearer` 経路は壊さない。後方互換を保ったまま
   Cookie 経路を追加する。モバイル / server-side renderer / CLI などから
   Bearer を直接付けるユースケースは引き続き通る。
3. ログアウト時に Cookie が確実に破棄される。backend 側にセッションテーブルが
   無くても、Cookie さえ消えれば以後のリクエストは 401 に落ちる。
4. 開発環境（`http://localhost:3000` ↔ `http://localhost:8080`）と
   production 環境（`https://app.fuju.example.com` ↔ `https://api.fuju.example.com`）
   の両方で動く Cookie 属性 / CORS 設定を提供する。
5. frontend チームが本 backend 変更の merge 後、明確な contract（本ドキュメント
   §「auth-component 改修 contract spec」）に従って実装すれば追加設計なしで
   結合できる状態にする。

### なぜ必要か

- auth-component の設計上、access token は「メモリ内 private フィールド + silent
  refresh でローテーション」の方針を既に取っており、これを露出させるのは
  セキュリティ方針と矛盾する。
- `localStorage` に access token を置く選択肢（旧 README 案）は XSS で盗まれる
  懸念から明確に却下されている（`docs/authcore-integration.md` §8）。
- Bearer header を fetch wrapper から自動付与するには access token の public
  化が必要になり、上記方針と衝突する。
- Cookie handoff 方式なら、ブラウザが自動付与するため「access token をアプリ
  コードが触らない」「XSS で `document.cookie` からも読めない」が成立する
  （HttpOnly のため）。
- server-side session table を入れると状態の二重管理（AuthCore 側の token 有効期限
  ＋ backend 側の session レコード TTL）になり、rotation / logout / revocation
  の整合性を自前で保たないといけなくなる。**Cookie = access token** にすれば
  introspect 結果がそのまま正になるので、その重複を避けられる。

## 解決方針（確定）

```
[Login / Refresh / MFA verify / Social callback]
  AuthCore → { access_token, expires_in } →
    auth-component.adoptTokens() →
      POST https://<fuju-backend>/v1/auth/session
        Authorization: Bearer <access_token>
        backend: introspect → 成功なら
          Set-Cookie: fuju_access=<access_token>; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=<expires_in>
                      失敗なら 401、Cookie は発行しない

[Subsequent FE requests]
  fetch('/posts', { credentials: 'include' })
    → ブラウザが fuju_access cookie を自動付与
    → backend AuthMiddleware: Authorization header → 無ければ Cookie を読む
    → どちらから取った token でも introspect に投げる経路は同じ

[Logout]
  auth-component.clear() / logout →
    DELETE https://<fuju-backend>/v1/auth/session
    → backend: Set-Cookie: fuju_access=; Max-Age=0; Path=/; ...
    → Cookie が無くても 204（idempotent）
```

## 影響範囲

### 新規追加

- `internal/handler/session.go` — `SessionHandler` の実装
  （`POST /v1/auth/session`, `DELETE /v1/auth/session`）
- `internal/handler/session_test.go` — handler の unit test
- `internal/middleware/middleware_cookie_test.go` — AuthMiddleware の Cookie 経路 test
  （既存 `middleware_test.go` に追記でも可。新規 / 追記は実装時に判断）
- `pkg/cookie/cookie.go`（新規パッケージ）— Cookie 属性のビルダー
  （`Name`, `Secure`, `SameSite`, `Path`, `Max-Age` を config から組み立てる）
- `integration_test/session_handoff_test.go`（または既存の統合テストディレクトリ配下）
  — round-trip 検証（`POST /v1/auth/session` → `GET /me` を Cookie だけで通す）

### 変更

- `internal/middleware/middleware.go`
  - `AuthMiddleware`: `Authorization` header が無ければ `fuju_access` Cookie
    から token を取り出し、同じ introspect 経路に乗せる。
  - `CORSMiddleware`: `Access-Control-Allow-Credentials: true` をオリジン一致時
    に確実に返す形を維持しつつ、`Access-Control-Allow-Origin: *` かつ
    `Allow-Credentials: true` の組み合わせはブラウザが受け付けないので
    `allowedOrigins == "*"` 時は `Allow-Credentials` を付けない挙動を明示化
    する。コメント追記。
- `cmd/server/main.go`
  - `SessionHandler` を wire し、`POST /v1/auth/session` / `DELETE /v1/auth/session`
    を mux に登録する。
  - `POST /v1/auth/session` のみ `authMW` でラップし、`hydrateMW` は通さない
    （introspect が通った時点で handler に入る。user hydrate は handoff には不要）。
  - `DELETE /v1/auth/session` は **認証不要**（Cookie / Bearer のいずれも必須
    にしない）。既に session が無い端末からの logout も 204 で受けるため。
- `config/config.go`
  - `SessionCookieName`（既定 `"fuju_access"`）
  - `SessionCookieSecure`（既定 `true`、dev は env で `false` にできる）
  - `SessionCookieSameSite`（既定 `"Lax"`、必要なら `"Strict"` / `"None"` に
    切り替え可能）
  - `SessionCookieDomain`（optional、未指定なら Set-Cookie の `Domain` 属性は
    出さない＝request host に紐付く）
- `.env.example` — 上記 env の記載
- `docs/authcore-integration.md` — Cookie handoff 方式に追記
  （§1 フロー図、§8 「セキュリティ上の FE 責務」、§9 CORS のうち当該箇所）
- `docs/swagger.yaml` — `POST /v1/auth/session` / `DELETE /v1/auth/session` の
  operation 追加、`cookieAuth` security scheme 追加、既存認証必須 endpoint の
  `security` を `[{ BearerAuth: [] }, { cookieAuth: [] }]` の OR 条件に更新
- `docs/tasks/README.md` — 本タスク 10 を表と依存グラフに追記

### 破壊的変更

- **なし**（既存 `Authorization: Bearer` 経路は完全後方互換）。
- swagger の `security` を OR 条件化するのは仕様追加であり、既存クライアントの
  挙動は変わらない。
- Cookie 名 `fuju_access` は新規。旧 `authcore_session` Cookie は `01-rewrite-...`
  で既に撤去済みなので衝突しない。

## スコープ

### 含む

- `POST /v1/auth/session` ハンドラ:
  - `Authorization: Bearer` を受けて introspect → 成功なら
    `Set-Cookie: fuju_access=...` を返す
  - introspect 失敗時は 401、Cookie をセットしない
  - `Max-Age` は `session.ExpiresAt - time.Now()` を秒で渡す
    （`exp` が無い場合は config の `SessionCookieMaxAge` フォールバック、既定 1h）
- `DELETE /v1/auth/session` ハンドラ:
  - Cookie 破棄（`Max-Age=0` + 空値）
  - 認証不要、idempotent、常に 204
- `AuthMiddleware` 拡張:
  - header 優先、無ければ Cookie
  - 両方ある場合は **header を優先**（明示指定を尊重）、ドキュメントに記載
  - Cookie から token を取り出す際は `r.Cookie(cfg.SessionCookieName)`
    で取得後、空文字 / 取得失敗は header 無しと同じ 401 経路
- CORS:
  - `Access-Control-Allow-Credentials: true` を許可オリジン一致時に確実に返す
  - `allowedOrigins == "*"` かつ credentials の組み合わせは不可のため、本番設定
    では明示オリジン列を要求する旨を `.env.example` / ドキュメントに書く
- Cookie 属性の config 化（開発: `Secure=false` 許容、本番: `Secure=true` 必須）
- `internal/handler/session.go` 新設 + `cmd/server/main.go` wiring
- unit test（handler / middleware の cookie 経路）
- integration test（login 模擬 → POST /v1/auth/session → GET /me を cookie jar 経由で確認）
- `.env.example` / `docs/authcore-integration.md` / `docs/swagger.yaml` への追記
- auth-component 改修の **contract spec**（実装本体は frontend 側の別 PR、
  本タスクは仕様記載のみ）

### 含まない

- **server-side session table / session repository**
  （cookie = access token なので不要。DB 変更なし、migration 追加なし）
- **AuthCore 側の変更**
  （introspect 拡張 / refresh token endpoint 追加などは無し。token 流通経路を
  変えるだけ）
- **auth-component 実装本体**
  （別 PR スコープ、本タスク merge 後 or 並行）
- **本番ドメイン / DNS 設定**
- **CSRF 対策の本格導入**
  （`SameSite=Lax` で MVP 対応とし、後続タスクで double-submit token / Origin
  ヘッダ検証を検討）
- **session revocation**（ログアウト以外）
- **token rotation**
  （アクセストークン自体の回転は AuthCore 側ロジック。本タスクでは新しい
  access token を受け取った auth-component が `POST /v1/auth/session` を
  再度叩き、Cookie を上書きするだけ）
- **cross-subdomain cookie の共有**
  （Domain 属性の wildcard はスコープ外。必要になったら config で対応）
- **`fuju_access` 以外の追加クレーム**
  （csrf_token / nonce などを Cookie に乗せない。あくまで access token 生文字列）

## 確定事項

| 項目 | 値 | 備考 |
|---|---|---|
| Cookie 名 | `fuju_access` | config 化する (`SESSION_COOKIE_NAME`)、既定値がこれ |
| Cookie 属性 | `HttpOnly`, `Secure` (config), `SameSite=Lax`, `Path=/` | `Lax` は top-level GET で Cookie を送る。login 直後の redirect 遷移でも乗る |
| Max-Age | `expires_in` 秒（= `session.ExpiresAt - now()`） | `exp` claim が無い場合は config `SESSION_COOKIE_FALLBACK_MAX_AGE` (既定 `1h`) |
| ミドルウェア優先順位 | **`Authorization` header 優先**、無ければ Cookie | 両方ある場合も header 優先（明示 > 暗黙） |
| CORS | `Access-Control-Allow-Credentials: true` (オリジン一致時のみ) | `Allow-Origin: *` + credentials はブラウザが拒否。本番は必ず明示列挙 |
| `POST /v1/auth/session` failure | introspect 失敗 → 401、Cookie なし | `ErrUpstream` は 503 にマッピング（既存 `AuthMiddleware` と同等） |
| `DELETE /v1/auth/session` | 認証不要 / idempotent / 常に 204 | Cookie が無くても OK、Set-Cookie で `Max-Age=0` を返す |
| Cookie 経路の Logging | middleware の既存 log に `auth_source=header\|cookie` を追加 | debug 可観測性、PII になるので token 本体は絶対に出さない |
| openapi security | 既存 `BearerAuth` に加え `cookieAuth` を追加し OR 条件化 | 既存クライアントの表示挙動は変えない |

## Phase 構成と PR 分割

実装ボリュームは中程度で、他タスクからの割り込みに強い独立性が求められる
ため **2 Phase ≒ 2 PR** に畳む（3 Phase 案もあったが、Phase 2 と 3 を無理に
分割するメリットが薄いので統合）。各 Phase の終端で CI green を確認してから
次に進む。

| Phase | 範囲 | PR 粒度 |
|---|---|---|
| 1 | backend 本体: handler 新設 + AuthMiddleware 拡張 + CORS 補正 + config + unit test | 中 |
| 2 | integration test + `.env.example` / docs / swagger 更新 + auth-component contract spec 明文化 | 小〜中 |

---

## Phase 1: backend 本体

### 1-1. `config/config.go` に Cookie 関連フィールドを追加

変更対象: `/home/sheep/dev/fuju/backend/config/config.go`

追加フィールド:
```go
// Session cookie (handoff flow: POST /v1/auth/session → Set-Cookie: fuju_access=...).
// The cookie value is the AuthCore access token verbatim; there is no
// server-side session store.
SessionCookieName          string        // default "fuju_access"
SessionCookieSecure        bool          // default true; dev may set false
SessionCookieSameSite      string        // "Lax" | "Strict" | "None"; default "Lax"
SessionCookieDomain        string        // optional; empty = host-only cookie
SessionCookieFallbackMaxAge time.Duration // default 1h; used when access token lacks `exp`
```

対応する `Load` の行:
```go
SessionCookieName:           getEnv("SESSION_COOKIE_NAME", "fuju_access"),
SessionCookieSecure:         getEnvBool("SESSION_COOKIE_SECURE", true),
SessionCookieSameSite:       getEnv("SESSION_COOKIE_SAMESITE", "Lax"),
SessionCookieDomain:         getEnv("SESSION_COOKIE_DOMAIN", ""),
SessionCookieFallbackMaxAge: getEnvDuration("SESSION_COOKIE_FALLBACK_MAX_AGE", time.Hour),
```

`getEnvBool` ヘルパーが未存在なら同ファイル末尾に追加（`"true"|"1"|"yes"` を
true、空 / parse エラーは defaultValue）。

`Validate` 追加:
- `SessionCookieSameSite` が `"Lax"|"Strict"|"None"` のいずれでもなければエラー
- `SessionCookieSameSite == "None"` かつ `SessionCookieSecure == false` はブラウザが
  拒否するのでエラー（早期検知）

責務: Cookie 属性を環境ごとに切り替えられるようにする単一のソース。handler /
middleware は直接 `os.Getenv` を見ず、Config 経由でアクセスする。

### 1-2. `pkg/cookie/cookie.go` 新設

新規ファイル: `/home/sheep/dev/fuju/backend/pkg/cookie/cookie.go`

責務:
- `*http.Cookie` を config から組み立てる純関数を提供し、handler 側のテスト性を
  上げる。
- `SameSite` 文字列 → `http.SameSite` の変換を 1 箇所に閉じ込める。

API:
```go
package cookie

import (
    "net/http"
    "time"
)

// Attrs captures the policy knobs driving a single Set-Cookie.
type Attrs struct {
    Name     string
    Value    string
    MaxAge   time.Duration  // negative → immediate expiry (delete); zero → session cookie
    Secure   bool
    SameSite http.SameSite
    Domain   string
    Path     string
}

// Build converts Attrs into an *http.Cookie ready for http.SetCookie.
func Build(a Attrs) *http.Cookie { ... }

// ParseSameSite turns the string form used in config/env into http.SameSite.
// Unknown values return http.SameSiteLaxMode and a sentinel error so callers
// can surface validation failures at boot, not at request time.
func ParseSameSite(s string) (http.SameSite, error) { ... }
```

実装メモ:
- `MaxAge` の秒換算は `int(a.MaxAge.Seconds())`、負なら `MaxAge = -1`
  （`net/http` では `-1` で即時削除）。
- `Path` が空なら `"/"` にフォールバック。
- token 値の sanitize は行わない（AuthCore 発行の JWT は `http.cookiejar`
  が受け付ける ASCII 範囲に収まる前提。万一の異常値は handler 側でバリデート）。

### 1-3. `internal/handler/session.go` 新設

新規ファイル: `/home/sheep/dev/fuju/backend/internal/handler/session.go`

責務:
- `POST /v1/auth/session`: `auth.GetAccessTokenFromContext(r.Context())`（= `AuthMiddleware`
  が context にセット済み）から access token を取り、`auth.GetSubFromContext` から
  sub を取り、その token をそのまま Cookie に書く。`Max-Age` は AuthMiddleware で
  得た `Session.ExpiresAt` を context に渡すか、handler 側で再度 introspect を
  呼ぶかのどちらか。**option A（context に ExpiresAt を載せる）** を採用する
  理由: 追加 introspect RTT を避けつつ、`AuthMiddleware` が既に検証済みの情報を
  再利用できるため（Step 1-5 で auth パッケージに helper 追加）。
- `DELETE /v1/auth/session`: 204 + `Set-Cookie: fuju_access=; Max-Age=-1; Path=/`。
  認証不要なので handler はそのまま mux に登録（middleware chain の外）。

interface:
```go
package handler

type SessionHandler struct {
    cookieCfg SessionCookieConfig  // 1-1 で追加する type を参照
}

func NewSessionHandler(cfg SessionCookieConfig) *SessionHandler { ... }

func (h *SessionHandler) Issue(w http.ResponseWriter, r *http.Request)  // POST
func (h *SessionHandler) Revoke(w http.ResponseWriter, r *http.Request) // DELETE
```

レスポンス仕様:
- `Issue` 成功: `204 No Content` + `Set-Cookie` 1 本。body 無し。
  （handler 内で token や sub を echo しない — Cookie が正）
- `Issue` 失敗: 401 ではなく、**middleware で既に 401 に落ちている前提**。
  handler に到達した時点で introspect 済みなので handler 側での失敗分岐は
  「context に access_token や exp が無い = middleware バグ」のみ。この
  internal error は 500 で返す。
- `Revoke` 成功: 204 + 削除 Cookie 1 本。body 無し。

### 1-4. `auth.GetExpiresAtFromContext` helper 追加

変更対象: `/home/sheep/dev/fuju/backend/pkg/auth/context.go`（既存、`GetSubFromContext` /
`GetAccessTokenFromContext` がある場所。実装時に path を確認する）

既存パターンに倣って:
```go
type expiresAtKey struct{}

func SetExpiresAtInContext(ctx context.Context, t time.Time) context.Context { ... }
func GetExpiresAtFromContext(ctx context.Context) (time.Time, bool) { ... }
```

### 1-5. `AuthMiddleware` 拡張

変更対象: `/home/sheep/dev/fuju/backend/internal/middleware/middleware.go:68-109`

変更内容:
1. token 取得を 2 段階に分離:
   ```go
   token, source, ok := extractAuthToken(r, cookieName)
   ```
   `extractAuthToken` は新規ヘルパー。`source` は `"header"` / `"cookie"` /
   `""`（どちらも無し）の 3 値。
2. 取得失敗時の 401 は現行と同じ。
3. introspect 成功後に `auth.SetExpiresAtInContext(ctx, session.ExpiresAt)` を追加。
4. `LoggingMiddleware` のログに `auth_source` フィールドを（現在は middleware
   がバラバラに log しないので）残すかどうかは、将来のデバッグ性のため
   `AuthMiddleware` 内で `log.Debug` を 1 行足す形にする（log を受け取る
   シグネチャに変更が必要なら、`AuthMiddleware` の signature を
   `func(client authcore.Client, cookieName string, log *logger.Logger)` に
   拡張する）。

`AuthMiddleware` の signature 変更:
```go
// Before
func AuthMiddleware(client authcore.Client) func(http.Handler) http.Handler

// After
func AuthMiddleware(client authcore.Client, cfg AuthMiddlewareConfig) func(http.Handler) http.Handler

type AuthMiddlewareConfig struct {
    CookieName string
    Logger     *logger.Logger // optional; nil 可
}
```

`cmd/server/main.go` の呼び出し箇所（156 行目）を更新。

### 1-6. `CORSMiddleware` の明示化

変更対象: `/home/sheep/dev/fuju/backend/internal/middleware/middleware.go:181-220`

変更内容:
1. `allowedOrigins == "*"` 経路に `Access-Control-Allow-Credentials` を**付けない**
   ことをコメントで明示化（現状の実装と挙動は一致しているがコメントで意図を残す）。
2. 明示オリジン経路では従来通り `Allow-Credentials: true` を返す（既に実装済み）。
3. `Access-Control-Allow-Headers` に `Cookie` を追加する必要は無い（ブラウザは
   Cookie を自動送信し、CORS preflight で `Access-Control-Request-Headers` に
   `Cookie` は出ない）。既存の `Content-Type, Authorization` のまま維持。

### 1-7. `cmd/server/main.go` wiring

変更対象: `/home/sheep/dev/fuju/backend/cmd/server/main.go`

1. `authMW := middleware.AuthMiddleware(cachedAuthCore)` を
   `authMW := middleware.AuthMiddleware(cachedAuthCore, middleware.AuthMiddlewareConfig{
       CookieName: cfg.SessionCookieName,
       Logger:     log,
   })` に置き換える。
2. `SessionHandler` を wire:
   ```go
   sessionCfg := handler.SessionCookieConfig{
       Name:             cfg.SessionCookieName,
       Secure:           cfg.SessionCookieSecure,
       SameSite:         sameSite,  // ParseSameSite 済み
       Domain:           cfg.SessionCookieDomain,
       Path:             "/",
       FallbackMaxAge:   cfg.SessionCookieFallbackMaxAge,
   }
   sessionHandler := handler.NewSessionHandler(sessionCfg)
   ```
3. mux 登録:
   ```go
   // Session handoff: POST requires a valid Bearer token (introspect runs
   // in AuthMiddleware). DELETE is intentionally public and idempotent so
   // stale tabs can log out without needing a live token.
   mux.Handle("POST /v1/auth/session", authMW(http.HandlerFunc(sessionHandler.Issue)))
   mux.HandleFunc("DELETE /v1/auth/session", sessionHandler.Revoke)
   ```
   `hydrateMW` はかけない。`AuthMiddleware` で introspect が通った時点で
   Cookie 発行の前提条件は満たされている。

### 1-8. Phase 1 の unit test

- `internal/handler/session_test.go`:
  - `Issue` — context に access_token / exp がセットされた `httptest.NewRequest`
    を渡し、`Set-Cookie` が期待属性で出ることを確認（`HttpOnly`, `Secure`,
    `SameSite=Lax`, `Path=/`, `Max-Age=<計算値>`）
  - `Issue` — context に access_token が無い場合 500（middleware bug 用 guard）
  - `Revoke` — Cookie 有りでも無しでも 204 + `Max-Age=-1` の削除 Set-Cookie が返る
- `internal/middleware/middleware_test.go`（既存 or 追記）:
  - `AuthMiddleware` — header only（既存テスト）
  - `AuthMiddleware` — cookie only（新規）
  - `AuthMiddleware` — header + cookie 両方ある場合 header を使う（新規）
  - `AuthMiddleware` — どちらも無い場合 401（既存）
  - `AuthMiddleware` — Cookie 名が違う場合は無視（新規）
- `pkg/cookie/cookie_test.go`:
  - `Build` — 各属性が期待通りに設定される
  - `Build` — `MaxAge` 負値で削除モード
  - `ParseSameSite` — `"Lax"|"Strict"|"None"` 正常、未知値はエラー

### 1-9. Phase 1 の検証

- `make lint` 通過
- `make test` 通過（inmemory、DB 不要）
- ローカル `docker-compose up -d` で AuthCore を起動し、curl で
  `POST /v1/auth/session` → `Set-Cookie` 確認 → `GET /me` を `--cookie` で通す
  の手順を 1 回踏む（smoke、手動）
- PR 上で CI の `lint` / `test` / `build` / `security` が green

---

## Phase 2: integration test + docs + contract spec

### 2-1. integration test

新規: `/home/sheep/dev/fuju/backend/integration_test/session_handoff_test.go`
（または既存の統合テストディレクトリの命名規則に合わせる）

ビルドタグ: `//go:build integration`

ケース:
1. **handoff round-trip**: AuthCore を **fake**（`httptest.Server` で `/v1/auth/introspect`
   を模擬）し、
   - 任意の access token を作って `POST /v1/auth/session` に `Authorization: Bearer`
     で渡す
   - 返却された `Set-Cookie` を `http.CookieJar` で拾い、`GET /me` を Cookie だけで
     叩く
   - 200 が返り、`is_admin` / `sub` が期待値と一致することを確認
2. **header + cookie 併存時の header 優先**: 同一クライアントが Bearer header と
   Cookie の両方を持って `GET /me` を叩き、header 側の sub が使われることを確認
3. **invalid token で 401 + Cookie 非発行**: fake introspect が `active: false` を返す
   ケースで `POST /v1/auth/session` が 401 を返し、`Set-Cookie` が存在しない
4. **logout で Cookie 削除**: `DELETE /v1/auth/session` が 204 + `Max-Age=-1` の
   Set-Cookie を返すこと
5. **AuthCore 上流 503 → backend 503**: fake introspect が 500 を返すと
   `POST /v1/auth/session` が 503 を返すこと

fake AuthCore は本タスクで新規に書くが、将来の統合テスト拡張でも流用できる
よう `internal/authcoretest/` （新規、optional）に切り出す。Phase 1 で既存の
`pkg/authcore/client_test.go` が使っている `httptest` パターンを踏襲。

### 2-2. `.env.example` 更新

変更対象: `/home/sheep/dev/fuju/backend/.env.example`

追記ブロック（既存 CORS 設定の直後、新規セクション）:
```
# Session Cookie (AuthCore access-token handoff; see docs/authcore-integration.md §Cookie Handoff)
# Cookie name. Frontend fetch must include `credentials: 'include'`; the
# Authorization header is still accepted, but Cookie is the recommended path
# for browser contexts so the access token never touches JavaScript.
SESSION_COOKIE_NAME=fuju_access

# Secure flag. MUST be true in any HTTPS deployment; the only legitimate reason
# to set false is a local http://localhost dev environment.
SESSION_COOKIE_SECURE=true

# SameSite attribute. Lax works for the typical FE-at-one-origin, API-at-another
# setup. Use None + Secure=true if you deliberately need cross-site POSTs and
# have CSRF defences elsewhere.
SESSION_COOKIE_SAMESITE=Lax

# Optional Domain attribute. Empty means a host-only cookie (recommended for
# MVP). Set to ".fuju.example.com" only when FE and API share a parent domain
# and you intentionally want the cookie on all subdomains.
# SESSION_COOKIE_DOMAIN=

# Fallback Max-Age when the access token lacks an `exp` claim. Under normal
# AuthCore operation `exp` is always present and this fallback is unused.
SESSION_COOKIE_FALLBACK_MAX_AGE=1h
```

また、既存の `CORS_ALLOWED_ORIGINS` の注釈を更新して「`*` と credentials は
両立しないため、Cookie 方式を使うなら必ず明示オリジンを列挙すること」を追記。

### 2-3. `docs/authcore-integration.md` 更新

変更対象: `/home/sheep/dev/fuju/backend/docs/authcore-integration.md`

更新方針:
- §1 全体像のシーケンス図に **「(1') POST /v1/auth/session → Set-Cookie」** 線を追加
- §2 トークン取得フロー: Cookie handoff を推奨ルートとして記載、Bearer は引き続き
  許容と明記
- §8 セキュリティ上の FE 責務: 「access token の保存先 = メモリ」推奨は維持し
  つつ、Cookie handoff を使えば `document.cookie` からも触れないので XSS 耐性が
  さらに上がる旨を追記
- §9 CORS: `credentials: 'include'` を使うなら `CORS_ALLOWED_ORIGINS` は明示
  オリジン必須（`*` 不可）を明記
- **新セクション §11 「Cookie Handoff」** を追加:
  - フロー図（本ドキュメント「解決方針」セクションの疑似コード）
  - Cookie 属性仕様
  - FE の fetch wrapper 例（後述 auth-component contract spec を参照）
  - logout 動作

### 2-4. `docs/swagger.yaml` 更新

変更対象: `/home/sheep/dev/fuju/backend/docs/swagger.yaml`

- `components.securitySchemes` に `cookieAuth` を追加:
  ```yaml
  cookieAuth:
    type: apiKey
    in: cookie
    name: fuju_access
    description: |
      HttpOnly cookie issued by POST /v1/auth/session. The cookie value is
      the AuthCore access token verbatim; the browser attaches it
      automatically on every request made with credentials: 'include'.
  ```
- 既存の認証必須 operation の `security` を OR 条件化:
  ```yaml
  security:
    - BearerAuth: []
    - cookieAuth: []
  ```
- 新規 paths:
  - `POST /v1/auth/session`: request は `BearerAuth`、response 204 + `Set-Cookie`
    header description
  - `DELETE /v1/auth/session`: no security、response 204 + Cookie 削除
- 既存の `GET /me` 等、public でも viewer context を拾う endpoint の `security`
  も OR 条件化して Cookie 経路を書き加える

### 2-5. auth-component contract spec（backend ドキュメント内に記載）

本タスク内に明文化するが、**実装は frontend チームの別 PR**。本セクションは
frontend チームが単独で実装に着手できる粒度で書く。

出力先: 本ドキュメント内の独立セクション（下記 §「auth-component 改修 contract spec」）

### 2-6. `docs/tasks/README.md` 更新

- タスク一覧表の末尾に 10 を追加:
  ```markdown
  | 10 | [10-cookie-session-handoff.md](./10-cookie-session-handoff.md) | 01, 06, 07, 09 | 確定済み。Cookie = access token 方式の session handoff エンドポイント (`POST/DELETE /v1/auth/session`) を新設、`AuthMiddleware` を Cookie 対応に拡張。2 Phase ≒ 2 PR。auth-component 改修 contract spec を内包し frontend 別 PR 向けに receiver ready |
  ```
- 依存グラフ図末尾に 10 を追加:
  ```
  09-fix-lint-across-stacked-prs
     |
  10-cookie-session-handoff  (Cookie 方式の session handoff、backend 単体で完結)
  ```

### 2-7. Phase 2 の検証

- `make lint` / `make test` / `make test-integration` 全て通過
- CI で `lint` / `test` / `integration-test` / `build` / `security` 全て green
- swagger lint（`redocly lint` 等、既存 CI 設定があれば）通過
- `docs/authcore-integration.md` を手動で読み直し、既存 §1-10 との整合性確認
- frontend チームに本ドキュメントをレビュー依頼し、contract spec で実装可能と
  confirm を取る（Phase 2 merge の前に実施、out-of-band）

---

## auth-component 改修 contract spec

> **この節は本タスクの実装対象外**（実装は frontend 別 PR）。backend が提供する
> handoff エンドポイントに対して auth-component 側が何を作ればよいかを、
> frontend チームが単独で着手できる粒度で固める。

### C-1. `AuthConfig` の拡張

変更対象: `/home/sheep/dev/fuju/frontend/src/auth-component/src/types.ts`

追加フィールド:
```ts
export interface AuthConfig {
  // ...既存フィールド...

  /**
   * FUJU backend の session handoff エンドポイント。指定すると、アクセストークン
   * が発行/更新されるたびに auth-component が以下を叩き、Cookie を預ける。
   *
   *   POST  <sessionHandoffURL>   Authorization: Bearer <access_token>
   *   DELETE <sessionHandoffURL>  logout / clear 時
   *
   * 例: `https://api.fuju.example.com/v1/auth/session`。
   *
   * 未指定 / undefined のとき:
   *   - backend への POST / DELETE は行わない（従来通り「access token を
   *     auth-component 内に保持するだけ」の挙動）
   *   - ホスト側アプリは引き続き自前で Bearer ヘッダを組み立てる必要がある
   *
   * 指定したとき:
   *   - ブラウザが `fuju_access` Cookie を自動付与するため、ホスト側の fetch
   *     は `{ credentials: 'include' }` だけで認証が通る
   *   - Bearer ヘッダを手で組み立てる必要はない（付けてもよい = OR 条件）
   */
  sessionHandoffURL?: string;
}
```

### C-2. `AuthStore.adoptTokens` の拡張

変更対象: `/home/sheep/dev/fuju/frontend/src/auth-component/src/store/AuthStore.ts:416-419`

現状:
```ts
private adoptTokens(accessToken: string, expiresInSec: number): void {
  this.accessToken = accessToken;
  this.accessExp = Math.floor(Date.now() / 1000) + expiresInSec;
}
```

改修後（概念）:
```ts
private adoptTokens(accessToken: string, expiresInSec: number): void {
  this.accessToken = accessToken;
  this.accessExp = Math.floor(Date.now() / 1000) + expiresInSec;
  // Fire-and-forget: handoff failure must not break login/refresh itself.
  // The user is already authenticated at AuthCore level; Cookie is a
  // convenience layer for FUJU backend. Failing open here means the app
  // falls back to (manual) Bearer header usage, which is still supported.
  void this.handoffCookie(accessToken);
}

private async handoffCookie(accessToken: string): Promise<void> {
  const url = this.config.sessionHandoffURL;
  if (!url) return;
  try {
    const fetchImpl = this.config.fetch ?? globalThis.fetch;
    await fetchImpl(url, {
      method: 'POST',
      credentials: 'include',
      headers: { Authorization: `Bearer ${accessToken}` },
    });
    // 4xx/5xx は errors.warn レベルで記録するが throw はしない
  } catch (e) {
    // Network errors are swallowed: AuthCore login already succeeded.
    // eslint-disable-next-line no-console
    console.warn('[auth-component] session handoff failed', e);
  }
}
```

ポイント:
- 失敗しても login 全体を失敗させない（Cookie は「あると便利」であって必須ではない）
- `credentials: 'include'` 必須（backend が `Set-Cookie` を返しても Same-Site cookie
  ポリシーで受け付けられない可能性があるが、`SameSite=Lax` なら同一 host / 別 host
  でも top-level POST response は許容される）
- 既存 `adoptTokens` の呼び出し 5 箇所（login / refresh / mfa verify / social
  callback / resetSession 等、`AuthStore.ts:109, 155, 210, 251, 363` で grep）
  はシグネチャを変えないので自動的に handoff される

### C-3. `AuthStore.clear` / logout の拡張

変更対象: `/home/sheep/dev/fuju/frontend/src/auth-component/src/store/AuthStore.ts` の
`clearSession` / logout 系メソッド

logout フロー時（ユーザーが logout を明示した時、または refresh 失敗で無効化する時）に:
```ts
private async revokeCookie(): Promise<void> {
  const url = this.config.sessionHandoffURL;
  if (!url) return;
  try {
    const fetchImpl = this.config.fetch ?? globalThis.fetch;
    await fetchImpl(url, {
      method: 'DELETE',
      credentials: 'include',
    });
  } catch {
    // Ignore: DELETE is idempotent and a failure here just leaves a stale
    // cookie that will expire naturally at Max-Age.
  }
}
```

呼び出しタイミング:
- 明示的な logout API を叩く直前 / 直後（順序は frontend チーム判断、`finally` 内が安全）
- silent refresh が最終的に失敗し `clearSession` に落ちる経路
- `clearSession` 自体から呼ぶと silent refresh 失敗時にも毎回 DELETE が飛んで
  冗長なため、**logout API の明示呼び出しに紐付ける**のが推奨

### C-4. ホストアプリ側の fetch wrapper 所望形

frontend 側アプリケーション（auth-component の consumer）が FUJU backend を叩く
際の典型例:

```ts
// NG（旧方式 / 不可能）:
//   fetch('/posts', { headers: { Authorization: `Bearer ${accessToken}` } })
//   ← auth-component は accessToken を外に出さないので組み立てられない
//
// OK（新方式）:
export async function fujuFetch(input: RequestInfo, init: RequestInit = {}) {
  return fetch(input, {
    ...init,
    credentials: 'include',  // ← 重要。これで fuju_access cookie が自動付与される
  });
}
```

auth-component 側は `useAuth()` 戻り値やコンテキストを変更**しない**
（access token の public 化はしない）。consumer に露出する API は既存のまま。

### C-5. frontend 側の受け入れ条件

- `AuthConfig.sessionHandoffURL` を指定した状態で login 完了後、ブラウザ devtools
  の Application タブに `fuju_access` HttpOnly Cookie が表示される
- `fetch(<backend>, { credentials: 'include' })` で backend の認証必須 endpoint が
  200 を返す
- logout 後、Cookie が消滅し、同じ fetch が 401 を返す
- `sessionHandoffURL` を指定しない場合、既存の auth-component の挙動に変化がない
  （後方互換）

---

## 検証 / テスト方針

### 単体テスト

- `pkg/cookie/cookie_test.go`: 全分岐カバー（Build / ParseSameSite）
- `internal/handler/session_test.go`: Issue / Revoke の成功・失敗パス
- `internal/middleware/middleware_test.go` に追加:
  - cookie only 経路
  - header + cookie 両方の優先順位
  - Cookie 名不一致で無視

### 統合テスト

- `integration_test/session_handoff_test.go`（`//go:build integration`）で
  fake AuthCore + `http.CookieJar` を使った round-trip
- CI の既存 integration test job にこのファイルも含める（`-tags=integration` で全走）

### smoke / 手動確認

- ローカル `docker-compose up -d` で AuthCore + backend 起動
- `curl -c jar.txt -H "Authorization: Bearer <token>" -X POST http://localhost:8080/v1/auth/session -i`
  で 204 + `Set-Cookie: fuju_access=...; HttpOnly; ...`
- `curl -b jar.txt http://localhost:8080/me` で 200
- `curl -b jar.txt -X DELETE http://localhost:8080/v1/auth/session -i` で 204 +
  `Set-Cookie: fuju_access=; Max-Age=0`
- 再度 `curl -b jar.txt http://localhost:8080/me` で 401

### FE 結合（Phase 2 完了後、別 PR の検証範囲）

- auth-component 改修 PR の受け入れ条件（本ドキュメント §C-5）をそのまま適用

## リスクと緩和

| リスク | 緩和 |
|---|---|
| `SameSite=Lax` では一部のクロスサイト POST（例: FE が別 domain の iframe 内）で Cookie が送られない | MVP は同一 organization 配下の domain 分け運用を前提にする。必要なら `SameSite=None` + `Secure=true` に切り替え可能な config にしてある |
| access token が長い JWT の場合、Cookie サイズ上限（4KB）超過 | AuthCore 発行の access token は RS256 で 1-2KB 程度の想定。超える場合は AuthCore 側の claim を削る話になり、本タスクのスコープ外。test で長さを assertion して早期検知する |
| `Authorization` header と Cookie 両方が来る状況で検証ロジックが分岐 | header を優先、Cookie は fallback、両方あっても introspect は 1 回だけ。middleware で token source を切り替えるだけなので分岐コストは低 |
| introspect キャッシュ (30s) と Cookie の `Max-Age` の不整合 | Cookie の `Max-Age` は AuthCore の `exp` に揃える。introspect キャッシュ (`AUTHCORE_INTROSPECT_CACHE_TTL=30s`) は独立に動き、revoke 反映は最大 30s ラグという既存仕様のまま（`docs/authcore-integration.md` §4）。本タスクでは既存挙動を踏襲 |
| `POST /v1/auth/session` の handoff リクエストが何らかの理由で失敗し、Cookie が無い状態で FE が以後の通信を試みる | FE 側（contract spec §C-2）で fire-and-forget にし、Bearer header 経路が引き続き動く。Cookie 無しでも Bearer でアクセスできれば UX に影響しない |
| CSRF: Cookie 認証方式に切り替えたことで、同一オリジン JS からの自動送信に脆弱性が増える | `SameSite=Lax` が MVP の最低ライン。将来タスクで double-submit token / Origin ヘッダ検証を追加する。現段階では `docs/authcore-integration.md` §8 に限界を明記 |
| `Secure=false` のまま本番に出てしまう | `config.Validate` で `SessionCookieSameSite=None` かつ `Secure=false` は boot 時に reject する。`Secure=false` 単独は dev で許容せざるを得ないので完全には防げないが、`.env.example` のコメントで強く牽制 |
| auth-component 側の実装遅延で backend merge 後に結合できない期間が発生 | backend 単体で既存 Bearer 経路は壊さないので、auth-component 未改修でもアプリは動く。merge タイミングは独立にできる |

## 参考

- 既存 AuthMiddleware: `/home/sheep/dev/fuju/backend/internal/middleware/middleware.go:68-109`
- 既存 CORSMiddleware: `/home/sheep/dev/fuju/backend/internal/middleware/middleware.go:181-220`
- AuthCore client: `/home/sheep/dev/fuju/backend/pkg/authcore/client.go`
- AuthCore introspect 仕様: RFC 7662 (https://www.rfc-editor.org/rfc/rfc7662)
- AuthCore 側の access-token-only introspect 実装: `/home/sheep/dev/fuju/auth/usecase/introspect_uc/service.go:64`
- 既存 AuthCore 連携ガイド（FE 向け）: `/home/sheep/dev/fuju/backend/docs/authcore-integration.md`
- auth-component: `/home/sheep/dev/fuju/frontend/src/auth-component/src/store/AuthStore.ts:47`, `:109`, `:155`, `:210`, `:251`, `:363`, `:416-419`
- auth-component types: `/home/sheep/dev/fuju/frontend/src/auth-component/src/types.ts:82-124`
- Cookie / SameSite MDN: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Set-Cookie/SameSite
- `net/http.Cookie`: https://pkg.go.dev/net/http#Cookie
