# 12 - Image Upload Hardening + Frontend Handoff Docs

## 概要

Cloudflare R2 を使った画像アップロード機能（POST/GET/DELETE `/v1/images`）は既にコードが揃っている。本タスクは「実装をプロダクション品質まで引き上げる」+「フロントエンドエンジニアが迷わず使える実装指示書を整備する」の 2 軸で完了させる。新規機能追加ではなく、**既存実装の堅牢化（テスト・設定統合）と FE 向けハンドオフ** が中心。

画像の変換処理（リサイズ／フォーマット変換／EXIF 除去等）は **行わない**。受け取ったバイナリをそのまま R2 にアップロードする現状の挙動を維持する。

## 対象読者

- **バックエンド保守者**（Phase 1〜2 のテスト追加・config 整理）
- **フロントエンドエンジニア**（Phase 3 の `docs/frontend/image-upload.md` を読んで実装する）

## 前提 / 依存タスク

- `01-rewrite-domain-to-authcore-alignment.md` 完了済み（`images.id` / `images.user_id` の ULID 化、`users(sub)` への FK）
- `02-implement-post-feature.md` 完了済み（`post_images` join テーブル、`Post.image_ids` 経由の post 添付フロー）
- `06-frontend-handoff-docs-and-dead-code-removal.md` 完了済み（`docs/swagger.yaml` に `/v1/images` 系エンドポイントが既に記載されている前提）
- `08-wire-postgres-repository.md` 完了済み（`postgres.ImageRepository` がプロダクションで使われている）
- PR base は **develop**

## 現状把握（実装済みの内容）

| レイヤ | ファイル | 状態 |
|---|---|---|
| Storage | `pkg/storage/r2.go` | R2 (S3 互換) クライアント。`Upload` / `Delete` / `GetPublicURL` 実装済み。`os.Getenv` 直読み |
| Domain | `internal/domain/image.go` | `Image` / `UploadImageRequest` / `StorageService` interface |
| UseCase | `internal/usecase/image/usecase.go` | `Upload` / `GetUserImages` / `Delete` 実装済み |
| Handler | `internal/handler/image.go` | `POST /v1/images` (multipart), `GET /v1/images`, `DELETE /v1/images/{id}`。5MiB 上限、MIME 二重チェック実装済み |
| Repository (PG) | `internal/repository/postgres/image.go` | `Create` / `GetByID` / `GetByUserID` / `Delete` / `ListByPostID(s)` 実装済み |
| Repository (InMem) | `internal/repository/inmemory/inmemory.go` | テスト用実装あり |
| Migration | `db/migrations/002_add_images_table.sql` | `images` テーブル（ULID PK, user_id FK, soft delete）作成済み |
| Routing | `cmd/server/main.go` (252-256) | R2 設定が読めたときだけルートを登録 |
| OpenAPI | `docs/swagger.yaml` (349-449, 1199-1235) | `Image` / `ImageEnvelope` / `ImageListResponse` / 3 エンドポイント定義済み |

つまり「機能はもう動く」。本タスクは以下の足りない部分を埋める。

## スコープ

### 含む

1. **R2 設定を `config.Config` に統合**（現状は `pkg/storage/r2.go` 内で `os.Getenv` を直読みしており、テストや起動エラー診断がやりづらい）
2. **テスト追加**（usecase / handler。R2 と postgres は fake / inmemory で差し替え）
3. **フロントエンド向け実装指示書** `docs/frontend/image-upload.md` を新規作成
4. README / `.env.example` への R2 環境変数の追記

### 含まない

- 画像変換処理（リサイズ / WebP 変換 / EXIF 除去）— 要件で明示的に「行わない」
- Pre-signed URL によるダイレクトアップロード方式への切り替え — 現行のサーバー経由 multipart アップロードを維持
- CDN / カスタムドメイン設計 — 既に `R2_PUBLIC_DOMAIN` 環境変数で運用側に委譲されている
- 投稿への画像紐付けフロー（`POST /posts` の `image_ids`）— 既に `02-implement-post-feature.md` で実装済み
- 画像一覧のページネーション — 現状は全件返し（`docs/swagger.yaml` でも `limit=100, offset=0, total=count` と明記済み）。FE から要望が出たら別タスク

## 影響範囲

| ファイル | 種別 | 概要 |
|---|---|---|
| `config/config.go` | 修正 | `R2Endpoint` / `R2BucketName` / `R2PublicDomain` / `R2AccessKeyID` / `R2SecretAccessKey` フィールド追加。`R2Enabled()` メソッド（5 つ全部揃ったら true）を提供 |
| `config/config_test.go` | 修正 | `R2Enabled()` の境界値テスト追加 |
| `pkg/storage/r2.go` | 修正 | `NewR2Service(cfg *config.Config)` に変更（または `NewR2ServiceFromEnv()` を残しつつ `NewR2Service(...)` を主入口に）。`os.Getenv` 直読みを廃止 |
| `cmd/server/main.go` | 修正 | `cfg.R2Enabled()` で gating し、enabled なら `storage.NewR2Service(cfg)` を呼ぶ。disabled の場合は `log.Info` で「Image upload disabled (R2 not configured)」と出して image route を登録しない |
| `internal/usecase/image/usecase_test.go` | **新規** | Upload / Delete / GetUserImages の usecase テスト。`StorageService` を fake で差し替え、`ImageRepository` は `inmemory` を使う |
| `internal/handler/image_test.go` | **新規** | multipart リクエスト組み立て → 200/201/400/401/403/404 系、5MiB 超過、MIME 偽装、未認証など |
| `pkg/storage/r2_test.go` | **新規** | `sanitizeFilename` の単体テストのみ（R2 通信は integration test 対象外） |
| `docs/frontend/image-upload.md` | **新規** | FE エンジニア向け実装指示書（後述のテンプレートに従う） |
| `.env.example` | 修正 | `R2_*` 5 項目を追加（既に存在すればコメント整理） |
| `README.md` | 修正 | 「Optional features」セクションに R2 セットアップ手順を追記 |

破壊的変更:
- `storage.NewR2Service()` のシグネチャを変えると **外部利用者はいない**（cmd/server からのみ呼ばれている）ので問題なし。
- API スキーマ（`/v1/images` 系）には変更なし。**FE 側はリリース後の差し替え不要**。

## 実装ステップ

### Phase 1: Config 統合（1 PR）

1. `config/config.go` に以下を追加
   - フィールド: `R2Endpoint`, `R2BucketName`, `R2PublicDomain`, `R2AccessKeyID`, `R2SecretAccessKey`
   - `Load()` で `getEnv("R2_ENDPOINT", "")` などを読み込み
   - メソッド `R2Enabled() bool`: 必須 3 つ（`R2Endpoint`, `R2BucketName`, `R2PublicDomain`）が揃っていれば true。アクセスキーは AWS SDK 側のデフォルトクレデンシャルチェーン経由でも来るため必須にしない、という方針も検討 → **最初は 5 つ全部揃った場合のみ enabled** で実装し、ドキュメントに明記する
   - `Validate()`: 部分指定（一部のみ設定）は明示エラーにする（誤設定の早期検出）
2. `config/config_test.go` に `R2Enabled()` の真偽値テストを追加
3. `pkg/storage/r2.go`
   - `NewR2Service(cfg *config.Config) (*R2Service, error)` に変更
   - `os.Getenv` の直読みを削除
4. `cmd/server/main.go`
   - 既存の `storage.NewR2Service()` 呼び出しを `cfg.R2Enabled()` ガード付きで `storage.NewR2Service(cfg)` に変更
   - disabled の場合は `log.Info(ctx, "Image upload disabled: R2 is not configured")` を出す（現状の `log.Warn` から info に格下げ。disabled は異常ではない）
5. `.env.example` / `README.md` に R2 環境変数の説明を追加

### Phase 2: テスト追加（1 PR）

1. `internal/usecase/image/usecase_test.go` を新規作成
   - **fakeStorage** を用意（Upload/Delete/GetPublicURL を実装する struct で in-memory に key を記録）
   - inmemory.NewImageRepository を使う
   - テストケース:
     - `Upload`: 正常系（戻り値の `StorageKey` / `PublicURL` / DB 保存内容を検証）
     - `Upload`: `req == nil` → InvalidRequest
     - `Upload`: `len(FileData) == 0` → InvalidRequest
     - `Upload`: `len(FileData) > 5MiB` → InvalidRequest
     - `Upload`: storage 側エラー → エラーが透過
     - `Upload`: storage 成功後の repo.Create 失敗 → DB エラー（ここは現状コードだと R2 にゴミが残る可能性があるが、本タスクでは挙動変更しない。コメントで TODO を残す）
     - `GetUserImages`: 正常系 / 空 / 別ユーザーの画像が混ざらない
     - `Delete`: 正常系 / 他人の画像 → Forbidden / 存在しない → NotFound / R2 削除失敗でも DB 行は soft-delete される
2. `internal/handler/image_test.go` を新規作成（`handler_test.go` の `setupTestServer` パターンに合わせる）
   - multipart body のヘルパー関数を用意
   - テストケース:
     - `POST /v1/images`: 正常 (201) + 返却 JSON 形状（snake_case, `storage_key` が漏れていないこと）
     - `POST /v1/images`: 未認証 (401)
     - `POST /v1/images`: file フィールドなし (400)
     - `POST /v1/images`: 5MiB 超 (400)
     - `POST /v1/images`: HTML を `image/jpeg` と詐称 (400, sniff チェック)
     - `POST /v1/images`: text/plain (400)
     - `GET /v1/images`: 正常 (200, list shape)
     - `GET /v1/images`: 未認証 (401)
     - `DELETE /v1/images/{id}`: 正常 (204)
     - `DELETE /v1/images/{id}`: 他人の画像 (403)
     - `DELETE /v1/images/{id}`: 存在しない id (404)
     - `DELETE /v1/images/{id}`: 不正な ULID (400)
3. `pkg/storage/r2_test.go` で `sanitizeFilename` のエッジケース（`../foo`, `\\windows\\path`, `""`, `"."`, `"/"`, 日本語ファイル名）

### Phase 3: フロントエンド向け実装指示書（1 PR）

`docs/frontend/image-upload.md` を新規作成。後述の **FE 実装指示書テンプレート** をそのまま埋めて配置する。書き終えたら以下も更新:
- `docs/tasks/README.md` の表に行追加（番号 12）
- `README.md` の「Frontend integration」節から `docs/frontend/image-upload.md` への導線を貼る

## テスト要件

- `go test ./...` がグリーン
- 新規テストカバレッジ: `internal/usecase/image` および `internal/handler` の image 関連ハンドラで主要分岐がカバーされていること
- `golangci-lint run` がグリーン（`pkg/storage/r2.go` の `//nolint:staticcheck` ディレクティブは AWS SDK v2 の deprecated EndpointResolver 由来なので残してよい）

## 技術的な補足

### R2 環境変数の意味

| 変数 | 例 | 用途 |
|---|---|---|
| `R2_ENDPOINT` | `https://<account_id>.r2.cloudflarestorage.com` | S3 互換 API のエンドポイント。Cloudflare ダッシュボードから取得 |
| `R2_BUCKET_NAME` | `fuju-images-prod` | バケット名。事前に Cloudflare 側で作成しておく |
| `R2_PUBLIC_DOMAIN` | `https://images.fuju.example.com` | 公開アクセス用ドメイン。R2 の Public Bucket 機能 or カスタムドメイン経由 |
| `R2_ACCESS_KEY_ID` | （シークレット） | R2 の API トークンから払い出されたアクセスキー ID |
| `R2_SECRET_ACCESS_KEY` | （シークレット） | 同シークレットアクセスキー |

### ストレージキーの設計

`pkg/storage/r2.go` の `Upload` で生成されるキーは:

```
images/{userID}/{ulid}/{safeFilename}
```

- `userID` は `users.sub`（ULID）
- `ulid` はアップロードごとに新規発行（`oklog/ulid/v2`）
- `safeFilename` は `sanitizeFilename` で path traversal を除去した後のもの（空なら `file`）

このキーは **public_url 構築のためにのみ使用** し、API レスポンスには露出させない（既に `publicImageView` で `storage_key` を除外済み）。

### CORS と Public Bucket

R2 バケットの公開設定および CORS は **R2 側の管理画面で設定する** 範疇で、本リポジトリのコード変更は不要。FE 向けドキュメントには「画像 URL は別オリジンになるので CORS が必要なケース（canvas で読むなど）は別途設定すること」と注記する。

### なぜ Pre-signed URL ではなくサーバー経由なのか

- MVP ではサーバー側で MIME 検証（advertised vs sniffed）と認証（AuthCore Bearer）を一発でかけられる方が安全
- 5MiB 上限なら Cloudflare Workers / Edge / 自前サーバーいずれでもメモリ負荷は小さい
- Pre-signed URL に切り替えるなら別タスク（`13-presigned-direct-upload.md` 等）として起票する

---

## FE 実装指示書テンプレート（`docs/frontend/image-upload.md` の中身）

> **注**: 以下はそのまま新規ファイル `docs/frontend/image-upload.md` に書き起こす内容。task-planner の出力ではなく、Phase 3 の成果物そのもの。

````markdown
# 画像アップロード — フロントエンド実装ガイド

このドキュメントは FUJU バックエンドの画像アップロード API を **フロントエンドから呼ぶための実装指示書** です。バックエンドの内部設計（R2 / S3 互換 / Postgres）は知らなくても、本ドキュメントだけで実装できます。

## TL;DR

- エンドポイントは 3 つ: `POST /v1/images` / `GET /v1/images` / `DELETE /v1/images/{id}`
- 認証は AuthCore Bearer トークン（`Authorization: Bearer ...`）または httpOnly Cookie（`fuju_access`）
- `multipart/form-data` で `file` フィールドに画像を 1 枚乗せて POST
- 上限 5 MiB / `image/*` のみ
- 投稿に紐付けたい場合は、まず画像をアップロード → 返却された `id` を `POST /posts` の `image_ids` に渡す（2 ステップ）

## エンドポイント一覧

| Method | Path | 認証 | 用途 |
|---|---|---|---|
| `POST` | `/v1/images` | 必須 | 1 枚アップロード |
| `GET` | `/v1/images` | 必須 | 自分がアップロード済みの画像一覧 |
| `DELETE` | `/v1/images/{id}` | 必須 (所有者のみ) | 1 枚削除 |

> R2 がサーバー側で未設定の環境（一部の dev 環境）では 3 つとも 404 を返します。FE 側はこれを「機能が無効化されている」シグナルとして扱ってください。

## 1. アップロード — `POST /v1/images`

### リクエスト

- `Content-Type: multipart/form-data`
- フィールド `file`: 画像バイナリ（必須、1 枚のみ）

### 制約

- 最大ファイルサイズ: **5 MiB**（5 * 1024 * 1024 bytes）
- MIME タイプ: **`image/*` のみ**（`image/jpeg`, `image/png`, `image/gif`, `image/webp` 等）
- サーバー側で「アップロード時の Content-Type」と「先頭バイトの sniff 結果」を二重チェックします。HTML を `image/jpeg` と詐称すると 400 で弾かれます。

### レスポンス（201 Created）

```json
{
  "data": {
    "id": "01HXYZ...",
    "file_name": "cat.jpg",
    "mime_type": "image/jpeg",
    "file_size": 123456,
    "public_url": "https://images.fuju.example.com/images/01HUSER.../01HOBJ.../cat.jpg",
    "user_id": "01HUSER...",
    "created_at": "2026-05-09T12:34:56Z",
    "updated_at": "2026-05-09T12:34:56Z"
  }
}
```

- `id`: 画像の ULID。投稿に紐付けるとき (`POST /posts` の `image_ids`) で使う。
- `public_url`: そのまま `<img src=...>` に渡せる URL。ブラウザから直接 GET 可能。
- `storage_key` などの内部キーは **返却されません**。FE で URL を組み立てる必要はなし。

### エラー

| HTTP | 条件 | UI 上のおすすめ表示 |
|---|---|---|
| 400 | `file` フィールド無し | 「ファイルを選択してください」 |
| 400 | サイズ > 5 MiB | 「5 MB 以下の画像を選んでください」 |
| 400 | `image/*` 以外 / sniff 不一致 | 「画像ファイルを選んでください」 |
| 401 | 未認証 / トークン期限切れ | ログイン画面へリダイレクト |
| 404 | サーバーで R2 未設定 | 機能フラグで画像 UI を非表示化 |
| 500 | R2 / DB の障害 | 「アップロードに失敗しました。時間をおいてお試しください」 |

### 実装例（fetch）

```ts
async function uploadImage(file: File, accessToken: string): Promise<UploadedImage> {
  if (file.size > 5 * 1024 * 1024) {
    throw new Error("5MB を超える画像はアップロードできません");
  }
  if (!file.type.startsWith("image/")) {
    throw new Error("画像ファイルを選んでください");
  }

  const formData = new FormData();
  formData.append("file", file);

  const res = await fetch("/v1/images", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${accessToken}`,
      // Content-Type は **指定しない**。fetch が boundary 付きで自動設定する。
    },
    body: formData,
  });

  if (res.status === 401) {
    // ログイン UI へ
    throw new Unauthorized();
  }
  if (res.status === 404) {
    throw new Error("画像アップロード機能が利用できません");
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err?.error?.message ?? "アップロードに失敗しました");
  }

  const json = await res.json();
  return json.data;
}
```

### 実装例（axios）

```ts
import axios from "axios";

const formData = new FormData();
formData.append("file", file);

const { data } = await axios.post("/v1/images", formData, {
  headers: { Authorization: `Bearer ${token}` },
  // Content-Type は axios 側で multipart/form-data + boundary を自動付与
});

return data.data; // { id, public_url, ... }
```

### 進捗表示（任意）

`fetch` には進捗イベントが無いので、進捗バーが要るなら `XMLHttpRequest` の `upload.onprogress`、または axios の `onUploadProgress` を使う。

```ts
await axios.post("/v1/images", formData, {
  headers: { Authorization: `Bearer ${token}` },
  onUploadProgress: (e) => {
    if (e.total) setProgress(Math.round((e.loaded * 100) / e.total));
  },
});
```

## 2. 一覧取得 — `GET /v1/images`

自分がアップロードした画像（soft delete されていないもの）を **作成日時降順** で全件返します。ページネーションは現状ありません（`limit=100, offset=0, total=件数` 固定）。件数が増えてページングが必要になったらバックエンドへ別途相談してください。

### レスポンス（200 OK）

```json
{
  "data": [
    { "id": "01H...", "public_url": "https://...", "file_name": "...", ... },
    ...
  ],
  "limit": 100,
  "offset": 0,
  "total": 12
}
```

## 3. 削除 — `DELETE /v1/images/{id}`

- 所有者本人のみ削除可（他人の画像は 403）
- ソフトデリート: DB 上の `deleted_at` が立つ。R2 オブジェクト削除はベストエフォート（失敗しても 204 を返す）
- レスポンスは **204 No Content**（ボディなし）

### エラー

| HTTP | 条件 |
|---|---|
| 400 | `id` が ULID 形式ではない |
| 401 | 未認証 |
| 403 | 自分の画像ではない |
| 404 | 画像が存在しない（または既に削除済み） |

## 4. 投稿に画像を紐付ける（典型フロー）

1 枚または複数の画像を投稿に貼るには、**先に画像をアップロード → 取得した `id` を投稿作成時に渡す** という 2 ステップを踏みます。

```ts
// Step 1: 画像を順にアップロード（並列でも可）
const uploaded = await Promise.all(files.map(f => uploadImage(f, token)));
const imageIds = uploaded.map(u => u.id);

// Step 2: 投稿作成
await fetch("/posts", {
  method: "POST",
  headers: {
    Authorization: `Bearer ${token}`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    content: "今日のごはん",
    image_ids: imageIds, // ← ここに画像 ID を配列で
  }),
});
```

`image_ids` の **配列順** が投稿表示時の画像並び順になります（`post_images.position`）。

## 5. 注意事項

- **画像変換は行いません**: バックエンドはアップロードされたバイナリをそのまま R2 に保存します。リサイズ・WebP 変換・EXIF 除去等が必要なら **FE 側でやってから** アップロードしてください（プライバシー上の理由で EXIF を落としたいケースは特に）。
- **Content-Type を手動指定しない**: `multipart/form-data` 送信時、`Content-Type` ヘッダは fetch / axios に任せる。手で `multipart/form-data` を書くと boundary がずれて 400 になります。
- **public_url は CDN 経由になる場合がある**: ドメインは `R2_PUBLIC_DOMAIN` 環境変数で運用側が設定するもので、`api.fuju.example.com` とは別オリジンになる可能性があります。`<img>` タグへの埋め込みは問題なし。canvas で読み出すなど CORS が要るケースは個別に R2 側 CORS を相談してください。
- **アップロード後の画像は誰でも見られる**: 公開バケット運用前提なので、`public_url` を知っている人は誰でも GET できます。プライベート画像が必要な要件が出たら別途設計が必要です。

## 6. swagger.yaml との対応

OpenAPI スキーマは `docs/swagger.yaml` の `/v1/images`（349-449 行目あたり）と `Image` / `ImageEnvelope` / `ImageListResponse`（1199-1235, 1340-1346, 1404-1420 行目あたり）を参照。型生成は `openapi-typescript` または `orval` でこの YAML から行う想定です。

````

---

## 完了条件

- [ ] Phase 1: `config.Config` に R2_* が統合され、`pkg/storage/r2.go` が `os.Getenv` 直読みでなくなる
- [ ] Phase 1: `cfg.R2Enabled()` で route gating ＋ disabled 時のログメッセージが info レベル
- [ ] Phase 2: `internal/usecase/image/usecase_test.go` 追加 (主要分岐カバー)
- [ ] Phase 2: `internal/handler/image_test.go` 追加 (multipart の正常系 + 401/400/403/404 系)
- [ ] Phase 2: `pkg/storage/r2_test.go` の `sanitizeFilename` テスト追加
- [ ] Phase 3: `docs/frontend/image-upload.md` 新規作成（上記テンプレートをそのまま投入）
- [ ] Phase 3: `README.md` から `docs/frontend/image-upload.md` への導線
- [ ] Phase 3: `docs/tasks/README.md` の表に 12 を追加
- [ ] `.env.example` に `R2_*` 5 項目
- [ ] `go test ./...` / `golangci-lint run` グリーン
- [ ] PR base = `develop`
