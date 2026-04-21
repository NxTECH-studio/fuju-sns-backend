# FUJU プロダクト概要

FUJU は AuthCore を認証基盤とする小規模 SNS のバックエンドです。

このドキュメントは、バックエンドにはじめて触れるフロントエンド開発者を主な
読者として想定しています。API の詳細仕様（リクエスト / レスポンス型）は
[`docs/swagger.yaml`](./swagger.yaml) を一次資料として参照してください。認証の
具体的な取り扱いは [`docs/authcore-integration.md`](./authcore-integration.md)
に分けています。

---

## 読み手ルート

フロントエンド実装者が最初に追うべきドキュメントの順は以下です:

1. [`README.md`](../README.md) — リポジトリの位置づけ、ローカル起動手順
2. **本ドキュメント** — プロダクトのモデル / ユースケース / ID 体系
3. [`docs/swagger.yaml`](./swagger.yaml) — OpenAPI 3.0.3 スペック（`openapi-typescript` などで型生成）
4. [`docs/authcore-integration.md`](./authcore-integration.md) — トークン取得と失効の扱い、CORS

バックエンド保守側の詳細は [`docs/architecture.md`](./architecture.md) / 
[`docs/IMPLEMENTATION_GUIDE.md`](./IMPLEMENTATION_GUIDE.md) を参照。

---

## 1. 1 行サマリ

FUJU は AuthCore を認証基盤とする小規模 SNS のバックエンドです。
短文投稿 / 返信 / いいね / フォロー / タイムライン / 画像添付 / OGP プレビュー /
バッジ（管理者付与）を提供します。

## 2. 主要ユースケース

| 機能 | 概要 |
|---|---|
| 投稿 | 本文 **最大 120 文字**の短文投稿。画像最大 4 枚、タグ最大 10 個を添付できる |
| 返信 | 既存投稿の ID を `parent_post_id` に指定して作成する。`GET /posts/{id}/replies` で直接返信を取得 |
| いいね | 投稿への `POST /posts/{id}/like` / `DELETE /posts/{id}/like`。どちらも冪等 |
| タグ | 投稿本文中のタグを抽出してインデックス化（API はタグ配列として返す） |
| 画像添付 | Cloudflare R2 に事前アップロードした画像 ID を `image_ids` として指定 |
| OGP プレビュー | 投稿本文中の URL から OGP を**非同期**に取得。成功したもののみレスポンスに載る |
| フォロー | `POST /users/{sub}/follow` / `DELETE /users/{sub}/follow`。自己フォロー不可 |
| タイムライン | `home`（自分＋フォロー先）/ `user`（指定ユーザーの投稿）/ `global`（全公開投稿） |
| バッジ | `users.is_admin=true` のみが `/v1/admin/*` を叩ける。初回 admin は seed migration で投入 |
| プロフィール編集 | 自分自身のみ `bio` / `banner_url` を更新可能（他のフィールドは AuthCore 側管理） |

## 3. ユーザーモデル

バックエンド側の「ユーザー」は AuthCore 由来の不変 ID である `sub`（ULID, 26 文字）
をキーに持つローカルミラーレコードです。

- `sub` — AuthCore が発行した不変 ID。URL パラメータ（例: `/users/{sub}`）や
  フォーリンキーには必ずこれを使う。
- `display_id` — AuthCore 由来の `@handle` 相当。**可変**（AuthCore 側で変更可能）。
  フロントエンドがユーザーに見せる URL に `/@display_id` を使うのは構わないが、
  内部的には必ず `sub` で同一性判定すること。
- `display_name` / `icon_url` — AuthCore 由来の表示情報。バックエンドは 1 時間
  TTL でキャッシュ（`profile_refreshed_at` でタイムスタンプ追跡）。
- `bio` / `banner_url` / `is_admin` — **SNS 側が所有**するフィールド。

`PUT /users/{sub}` で更新できるのは `bio` / `banner_url` のみです。identity 系
（`display_name` / `display_id` / `icon_url`）は AuthCore 側の変更が次回 hydrate
で反映されます。`is_admin` は自己書き換え不可。

## 4. ID 体系

全リソース ID は **ULID (CHAR(26), Crockford Base32)**。旧 BIGINT ID は廃止済み。

- 例: `01HZXYABCDEFGHJKMNPQRSTVWX`
- 正規表現: `^[0-9A-HJKMNP-TV-Z]{26}$`
- フロントエンドは **常に文字列**として扱ってください。数値 / int64 化は絶対に
  しないこと（JavaScript の `Number` は 53bit までしか正確に扱えず、桁落ちします）。

対象リソース: `users.sub` / `posts.id` / `posts.parent_post_id` /
`posts.root_post_id` / `likes.*` / `follows.*` / `images.id` / `badges.id` /
`user_badges.*` など。

## 5. タイムラインの並び順 / ページング

一次資料: [`internal/usecase/timeline/usecase.go`](../internal/usecase/timeline/usecase.go)

3 本のタイムラインすべて **カーソルページング**で、レスポンス shape は共通の
`PostListResponse`（`{ data: Post[], next_cursor: string | null }`）です。

| エンドポイント | 認証 | ソース | 並び順 |
|---|---|---|---|
| `GET /timeline/home` | 必須 | 自分 + フォロー先の投稿 | `created_at DESC`（ULID カーソル） |
| `GET /timeline/user/{sub}` | 不要 | 指定 `sub` の top-level 投稿 | `created_at DESC` |
| `GET /timeline/global` | 不要 | 全公開投稿 | `created_at DESC` |

- `limit` の既定値は **20**、上限 **50**。範囲外（`<=0` または `>50`）は既定値に
  フォールバック（`post.NormalizeLimit` 参照）。
- `cursor` は前ページの `next_cursor` をそのまま送ってください。`post` 系のカーソル
  は ULID 形式ですが、内部表現はいずれ変わる可能性があるため **opaque 扱い**で
  OK。parseable でない値はサーバ側が「先頭から」として処理します。
- `next_cursor` が `null` のときはこれ以上ページがありません。
- 公開タイムラインでも Bearer トークンが付いていれば、各 `Post` の
  `liked_by_viewer` / `following_author` が呼び出し元視点で埋まります。

## 6. OGP の非同期性

一次資料: [`docs/tasks/03-implement-ogp-fetcher.md`](./tasks/03-implement-ogp-fetcher.md)

投稿作成 (`POST /posts`) 時、本文中に URL が含まれていても **OGP メタデータは
即座には返りません**。バックエンドは post commit 後に best-effort で OGP ジョブを
enqueue し、バックグラウンドワーカーが取得・キャッシュします。

- 成功した OGP のみ `Post.ogp_previews` 配列に載ります（`status=ok` のエントリ）。
- `pending` / `failed` のエントリは API レスポンスに出現しません（フロント側で
  「まだ取得中」を区別する必要はなく、単に空配列として扱えば良い）。
- キャッシュ TTL は 3 日。末尾スラッシュの有無は同一 URL として扱われます。

フロント UX としては「post 作成直後は OGP カードが無い → 数秒〜数十秒後に
再取得すると出る」前提で設計してください。

## 7. 画像アップロードの前提

- エンドポイント: `POST /v1/images` / `GET /v1/images` / `DELETE /v1/images/{id}`
- **R2 設定時のみ有効**。`R2_*` 環境変数が揃っていない場合、これらのルートは
  そもそも登録されず **404** を返します（オンデマンドに「R2 not configured」を
  返すのではなく、ハンドラー自体が無い）。
- アップロードは `multipart/form-data`、`file` フィールド必須。
- **最大サイズ 5 MiB**（`R2_MAX_FILE_SIZE`、デフォルト `5242880` bytes）。超過時
  400 `INVALID_REQUEST: "file size exceeds 5MB limit"`。
- `image/*` の MIME タイプに限定。サーバは先頭バイトを `http.DetectContentType`
  で検査し、申告された Content-Type と一致することも求めます。
- 投稿への紐付けは `POST /posts` 時の `image_ids` 配列で行います。アップロード済み
  画像のうち自分の所有物でないものが含まれていれば 400 を返します。
- レスポンスは他エンドポイントと同じ `snake_case`（`id`, `public_url`,
  `file_name`, `mime_type`, `file_size`, `created_at`, ...）。内部の R2 object
  key（`storage_key`）はレスポンスに含まれません。

## 8. Admin / バッジ

- `/v1/admin/*` 配下はすべて `users.is_admin=true` のユーザーのみ呼び出し可能
  （`AdminMiddleware` が 403 を返す）。
- 初回 admin は `db/migrations/005_implement_badges.sql` の seed で
  `UPDATE users SET is_admin=true` により付与されます。新しい admin を増やす
  のは現状 DB 直接操作のみで、API はありません。
- バッジ master は青（`verified_celebrity`）/ 金（`developer`）の 2 種が seed 済。
  admin は `POST /v1/admin/badges` で追加可能。
- ユーザーへの付与は `POST /v1/admin/users/{sub}/badges`、剥奪は
  `DELETE /v1/admin/users/{sub}/badges/{badge_id}`。剥奪は冪等。
- ユーザープロフィール (`GET /users/{sub}`) のレスポンスにはその人が保持している
  バッジが `badges: Badge[]` として載ります。

## 9. エラーモデル

一次資料: [`pkg/errors/errors.go`](../pkg/errors/errors.go), 各 `internal/handler/*.go`

すべてのエラーレスポンスは共通 envelope です:

```json
{
  "code": "INVALID_REQUEST",
  "message": "content is required",
  "timestamp": "2026-04-21T10:30:00Z"
}
```

| code | HTTP | 主な発生シーン |
|---|---|---|
| `INVALID_REQUEST` | 400 | JSON 壊れている / バリデーション失敗 / ULID が parse できない / file size 超過 |
| `UNAUTHORIZED` | 401 | Bearer トークン欠落 / introspection が `active=false` |
| `FORBIDDEN` | 403 | 他ユーザーの投稿 / 画像を消そうとした、admin 限定ルートへの非 admin アクセス |
| `NOT_FOUND` | 404 | 対象ユーザー / 投稿 / 画像 / バッジが存在しない（または R2 未設定で画像系ルートが未登録） |
| `CONFLICT` | 409 | 既に存在するバッジ key、重複したフォロー関係など |
| `VALIDATION_FAILED` | 400 | `code` 単位でのドメイン固有バリデーション（個別フィールドメッセージを含む） |
| `DATABASE_ERROR` | 500 | DB 層起因のエラー（現状 in-memory なので実際には起きにくい） |
| `INTERNAL_ERROR` | 500 | 予期しない panic / 想定外エラー |
| `EXTERNAL_SERVICE_ERROR` | 502 | AuthCore や R2 など上流の 5xx |

`code` は安定した機械可読文字列。フロント側の i18n や判定ロジックは `code` を
基準にしてください。`message` は人間向けで、予告なく文面が変わることがあります。

## 10. バージョニング

現状の URL パスは **`/v1` あり / なしが混在**しています:

- `/v1/images`, `/v1/admin/*` — `/v1` プレフィックスあり
- `/posts`, `/users`, `/timeline/*`, `/me`, `/health` — プレフィックスなし

これは `cmd/server/main.go` のルーティングを事実として素直に反映したものです。
統一 (`/v1` への寄せ、あるいは全撤去) は別タスクで議論します。フロントは swagger
を型生成して `operationId` ベースで呼ぶことを推奨（パスを直接書かない）。

---

## 参考: 関連ドキュメント

- [`docs/authcore-integration.md`](./authcore-integration.md) — 認証フロー（FE 視点）
- [`docs/swagger.yaml`](./swagger.yaml) — OpenAPI 3.0.3 スペック
- [`docs/architecture.md`](./architecture.md) — 保守者向けアーキテクチャ
- [`docs/IMPLEMENTATION_GUIDE.md`](./IMPLEMENTATION_GUIDE.md) — バックエンド実装詳細
- [`docs/tasks/`](./tasks/) — 各機能タスク計画書
